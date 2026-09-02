// Package telnetclient implements a minimal RFC 854 telnet client:
// byte-stuffing aware IAC parsing, a conservative negotiation policy
// (refuse everything except SGA/BINARY/ECHO where accepting avoids
// protocol bytes leaking to the terminal), and an interactive bridge
// that connects a net.Conn to stdin/stdout in raw terminal mode.
//
// Telnet transmits credentials in cleartext; this client exists for
// lab equipment, console servers, and legacy devices — not for
// untrusted networks.
package telnetclient

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/zsuroy/ctty/internal/rawterm"
	"github.com/zsuroy/ctty/internal/telnetconfig"
)

// TELNET protocol commands (RFC 854).
const (
	iacSE   = 240 // End of subnegotiation parameters
	iacNOP  = 241
	iacDM   = 242
	iacBRK  = 243
	iacIP   = 244 // Interrupt process
	iacAO   = 245 // Abort output
	iacAYT  = 246 // Are you there
	iacEC   = 247 // Erase character
	iacEL   = 248 // Erase line
	iacGA   = 249 // Go ahead
	iacSB   = 250 // Subnegotiation parameter follows
	iacWILL = 251
	iacWONT = 252
	iacDO   = 253
	iacDONT = 254
	iacIAC  = 255 // Data byte 255
)

// Well-known option numbers referenced by the negotiation policy.
const (
	optECHO     = 1
	optSGA      = 3 // Suppress Go Ahead
	optSTATUS   = 5
	optTTYPE    = 24 // Terminal type
	optNAWS     = 31 // Negotiate about window size
	optTSPEED   = 32
	optRFC      = 33 // Remote flow control
	optLINEMODE = 34
	optXDISPLOC = 35
	optNEWENV   = 39 // NEW-ENVIRON
)

// ConnectTimeout bounds the TCP dial when connecting interactively.
const ConnectTimeout = 10 * time.Second

// Dial opens a TCP connection to addr ("host:port").
func Dial(addr string) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, ConnectTimeout)
}

// Bridge is an interactive telnet session: it filters outgoing IAC
// escapes typed by the user, negotiates inbound options, and pumps
// both directions until the remote side closes or the user hits the
// disconnect key (Ctrl-], mirroring classic telnet).
type Bridge struct {
	conn net.Conn

	// exit is set once the user requests disconnection locally; both
	// pump goroutines observe it to stop promptly instead of blocking
	// forever on io.Copy.
	exit atomic.Bool
}

// NewBridge wraps conn in an interactive telnet bridge.
func NewBridge(conn net.Conn) *Bridge {
	return &Bridge{conn: conn}
}

// Run drives the session until it ends. Terminal raw mode setup/reuse
// delegates to serialconfig's x/term helpers so serial and telnet share
// one implementation across all platforms.
func (b *Bridge) Run(stdin io.Reader, stdout, stderr io.Writer) error {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	oldState, err := rawterm.MakeRaw()
	if err != nil {
		return fmt.Errorf("setting raw mode: %w", err)
	}
	defer rawterm.Restore(oldState)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	done := make(chan struct{}, 2)

	// stdin -> network. The writer loop owns all writes to conn.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := stdin.Read(buf)
			if n > 0 {
				if b.containsDisconnect(buf[:n]) || b.exit.Load() {
					b.exit.Store(true)
					_ = b.conn.SetReadDeadline(time.Now())
					done <- struct{}{}
					return
				}
				if _, werr := b.conn.Write(filterIAC(buf[:n])); werr != nil {
					b.exit.Store(true)
					done <- struct{}{}
					return
				}
			}
			if rerr != nil {
				b.exit.Store(true)
				done <- struct{}{}
				return
			}
		}
	}()

	// network -> stdout, with inline IAC handling.
	go func() {
		defer func() { done <- struct{}{} }()
		parser := &parser{bridge: b}
		buf := make([]byte, 8192)
		for !b.exit.Load() {
			n, err := b.conn.Read(buf)
			if n > 0 {
				if out := parser.feed(buf[:n], stdout); len(out) > 0 {
					if _, werr := stdout.Write(out); werr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-done:
	case <-sigCh:
	}

	fmt.Fprintln(stderr, "\nDisconnected.")
	return nil
}

// containsDisconnect reports whether the chunk carries Ctrl-] (0x1d),
// the classic telnet escape character.
func (b *Bridge) containsDisconnect(chunk []byte) bool {
	for _, c := range chunk {
		if c == 0x1d {
			return true
		}
	}
	return false
}

// filterIAC strips IAC sequences a user could not plausibly intend to
// send manually: doubled IAC becomes a single literal 0xFF, any other
// escape prefix typed by accident is dropped.
func filterIAC(chunk []byte) []byte {
	out := chunk[:0]
	for i := 0; i < len(chunk); i++ {
		c := chunk[i]
		if c != iacIAC {
			out = append(out, c)
			continue
		}
		if i+1 < len(chunk) {
			next := chunk[i+1]
			if next == iacIAC {
				out = append(out, iacIAC)
				i++
				continue
			}
			if next >= iacSE { // command byte: swallow the whole pair
				i++
				continue
			}
		}
		// Trailing lone IAC: swallowed.
	}
	return out
}

// parser accumulates a partial inbound stream and emits display-ready
// bytes while consuming and answering telnet control sequences.
type parser struct {
	bridge *Bridge
	state  parserState
	cmd    byte   // pending IAC command byte (WILL/WONT/DO/DONT/SB)
	opt    byte   // option being negotiated or subnegotiated
	iacBuf []byte // subnegotiation accumulator
	sawCR  bool
}

type parserState int

const (
	stateData parserState = iota
	stateIAC
	stateOpt
	stateSB
	stateSBIAC
)

// feed consumes a network chunk and returns the printable remainder.
func (p *parser) feed(chunk []byte, stderr io.Writer) []byte {
	out := make([]byte, 0, len(chunk))
	for i := range chunk {
		c := chunk[i]
		switch p.state {
		case stateData:
			switch c {
			case iacIAC:
				p.state = stateIAC
			default:
				// RFC 854: CR followed by anything other than LF NUL
				// still terminates the line; pass CR/LF through as-is.
				if p.sawCR && c == 0 && c != '\n' {
					p.sawCR = false
					continue
				}
				p.sawCR = c == '\r'
				out = append(out, c)
			}
		case stateIAC:
			switch c {
			case iacIAC:
				out = append(out, iacIAC)
				p.state = stateData
			case iacWILL, iacWONT, iacDO, iacDONT:
				p.cmd = c
				p.state = stateOpt
			case iacSB:
				p.iacBuf = p.iacBuf[:0]
				p.state = stateSB
			default:
				// Single-byte commands (NOP, GA, AYT, BRK, IP...): ignore.
				p.state = stateData
			}
		case stateOpt:
			p.answer(p.cmd, c, stderr)
			p.state = stateData
		case stateSB:
			if c == iacIAC {
				p.state = stateSBIAC
			} else {
				p.iacBuf = append(p.iacBuf, c)
			}
		case stateSBIAC:
			switch c {
			case iacSE:
				p.handleSubneg(p.iacBuf, stderr)
				p.state = stateData
			case iacIAC:
				p.iacBuf = append(p.iacBuf, iacIAC)
				p.state = stateSB
			default: // malformed: abandon the subnegotiation
				p.state = stateData
			}
		default:
			p.state = stateData
		}
	}
	return out
}

// answer implements the refusal-first negotiation policy:
//   - DO SGA / WILL SGA: accept — without it servers that emit Go Ahead
//     still work, but agreeing suppresses stray IAC GA on some hosts.
//   - WILL ECHO: accept — lets the server echo, avoiding double echo.
//   - DO/WILL BINARY(0): accept — passes 8-bit data untouched.
//   - Everything else: refuse with WONT/DONT so the server falls back
//     to plain NVT behavior we can safely render.
func (p *parser) answer(cmd, opt byte, stderr io.Writer) {
	accept := false
	switch opt {
	case optSGA:
		accept = cmd == iacDO || cmd == iacWILL
	case optECHO:
		accept = cmd == iacWILL
	case 0: // BINARY
		accept = true
	}

	var reply []byte
	switch cmd {
	case iacDO:
		if accept {
			reply = []byte{iacIAC, iacWILL, opt}
		} else {
			reply = []byte{iacIAC, iacWONT, opt}
		}
	case iacWILL:
		if accept {
			reply = []byte{iacIAC, iacDO, opt}
		} else {
			reply = []byte{iacIAC, iacDONT, opt}
		}
	case iacDONT, iacWONT:
		// Peer revoking: acknowledge politely.
		resp := byte(iacWONT)
		if cmd == iacWONT {
			resp = iacDONT
		}
		reply = []byte{iacIAC, resp, opt}
	}
	if reply != nil {
		_, _ = p.bridge.conn.Write(reply)
	}
	_ = stderr // reserved for negotiation debug tracing
}

// handleSubnegotiation consumes SB ... SE payloads. Only NAWS matters:
// answering it keeps some console servers from stalling at login. All
// other subnegotiations (TTYPE, NEW-ENVIRON probes...) are ignored,
// which is valid because we refused their enabling options.
func (p *parser) handleSubneg(buf []byte, stderr io.Writer) {
	if len(buf) == 0 {
		return
	}
	switch buf[0] {
	case optNAWS:
		// Report a conventional 80x24 window. Widths/heights are sent
		// as 16-bit values where 255 must be escaped as IAC IAC.
		w, h := encodeNAWS(80), encodeNAWS(24)
		payload := append([]byte{iacIAC, iacSB, optNAWS}, w...)
		payload = append(payload, h...)
		payload = append(payload, iacIAC, iacSE)
		_, _ = p.bridge.conn.Write(payload)
	}
	_ = stderr
}

// encodeNAWS renders one NAWS dimension with mandatory IAC escaping.
func encodeNAWS(v int) []byte {
	hi, lo := byte(v>>8)&0xff, byte(v)&0xff
	esc := func(b byte) []byte {
		if b == iacIAC {
			return []byte{iacIAC, iacIAC}
		}
		return []byte{b}
	}
	out := esc(hi)
	return append(out, esc(lo)...)
}

// ParseHostPort splits "host" or "host:port" into address parts,
// applying defaultPort when absent. Bracketed IPv6 literals supported.
func ParseHostPort(s string, defaultPort int) (string, int, error) {
	host := s
	port := defaultPort
	switch {
	case strings.HasPrefix(s, "["):
		// Bracketed literal: optional ":port" after the closing bracket.
		if idx := lastColonOutsideBrackets(s); idx >= 0 {
			p := s[idx+1:]
			n, err := strconv.Atoi(p)
			if err != nil || n <= 0 || n > 65535 {
				return "", 0, fmt.Errorf("invalid port %q", p)
			}
			host = s[:idx]
			port = n
		}
	case strings.Count(s, ":") > 1:
		// Bare IPv6 literal (::1): a port suffix requires brackets,
		// so every colon belongs to the address.
	default:
		if idx := strings.LastIndex(s, ":"); idx >= 0 {
			p := s[idx+1:]
			n, err := strconv.Atoi(p)
			if err != nil || n <= 0 || n > 65535 {
				return "", 0, fmt.Errorf("invalid port %q", p)
			}
			host = s[:idx]
			port = n
		}
	}
	host = trimBrackets(host)
	if host == "" {
		return "", 0, fmt.Errorf("host is required")
	}
	return host, port, nil
}

func lastColonOutsideBrackets(s string) int {
	depth := 0
	last := -1
	for i, c := range s {
		switch c {
		case '[':
			depth++
		case ']':
			depth--
		case ':':
			if depth == 0 {
				last = i
			}
		}
	}
	return last
}

func trimBrackets(s string) string {
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		return s[1 : len(s)-1]
	}
	return s
}

// telnetExecCommand implements tea.ExecCommand so the TUI can suspend
// itself (tea.Exec) while the interactive telnet bridge owns the
// terminal, then resume when the session ends.
type telnetExecCommand struct {
	host   telnetconfig.TelnetHost
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// NewExecCommand creates an ExecCommand that dials and bridges the
// given telnet host. Pass the result to tea.Exec() — Bubble Tea calls
// SetStdin/SetStdout/SetStderr with the real terminal handles, then Run().
func NewExecCommand(host telnetconfig.TelnetHost) *telnetExecCommand {
	return &telnetExecCommand{host: host}
}

func (c *telnetExecCommand) SetStdin(r io.Reader)  { c.stdin = r }
func (c *telnetExecCommand) SetStdout(w io.Writer) { c.stdout = w }
func (c *telnetExecCommand) SetStderr(w io.Writer) { c.stderr = w }

// Run dials the host and drives the interactive bridge until disconnect.
func (c *telnetExecCommand) Run() error {
	addr := net.JoinHostPort(c.host.Host, strconv.Itoa(c.host.Port))
	conn, err := Dial(addr)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()

	out := c.stdout
	if out == nil {
		out = os.Stdout
	}
	errOut := c.stderr
	if errOut == nil {
		errOut = os.Stderr
	}

	fmt.Fprintf(errOut, "Connected to %s (%s). Press Ctrl+] to disconnect.\n",
		c.host.Name, addr)

	bridge := NewBridge(conn)
	if err := bridge.Run(c.stdin, out, errOut); err != nil {
		return fmt.Errorf("telnet session: %w", err)
	}
	fmt.Fprintln(errOut, "\nDisconnected.")
	return nil
}
