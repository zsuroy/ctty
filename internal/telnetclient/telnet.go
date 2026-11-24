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
	"github.com/zsuroy/ctty/internal/telnetcred"
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

	// session is the shared protocol core. Both pump goroutines use the
	// same instance so inbound negotiation state persists across reads and
	// every outbound byte goes through its IAC-stuffing Write path.
	session *Session

	// auto, when set, answers login prompts from the session's display
	// stream until its rules are exhausted or the deadline passes.
	auto *AutoLogin

	// exit is set once the user requests disconnection locally; both
	// pump goroutines observe it to stop promptly instead of blocking
	// forever on io.Copy.
	exit atomic.Bool
}

// NewBridge wraps conn in an interactive telnet bridge.
func NewBridge(conn net.Conn) *Bridge {
	return &Bridge{conn: conn, session: NewSession(conn)}
}

// WithAutoLogin attaches automatic login-prompt answering to the session.
// A nil AutoLogin (no credentials) leaves the session fully manual.
func (b *Bridge) WithAutoLogin(a *AutoLogin) *Bridge {
	b.auto = a
	return b
}

// AutoLoginFor resolves the automatic login rules for a saved device: its
// configured user plus the password held in the encrypted vault (there is
// none for ad-hoc addresses). Returns nil when the device has nothing to
// send, keeping such sessions fully manual.
func AutoLoginFor(host telnetconfig.TelnetHost) *AutoLogin {
	var password string
	if host.Name != "" {
		password, _ = telnetcred.GetPassword(host.Name)
	}
	return NewAutoLogin(host.User, password)
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
				if _, werr := b.session.Write(buf[:n]); werr != nil {
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
		buf := make([]byte, 8192)
		for !b.exit.Load() {
			n, err := b.conn.Read(buf)
			if n > 0 {
				if out := b.session.Feed(buf[:n]); len(out) > 0 {
					if _, werr := stdout.Write(out); werr != nil {
						return
					}
					// Answer login prompts from what the user sees.
					// Answers go through Session so 0xFF in a credential
					// is stuffed instead of injecting a telnet command.
					b.auto.Observe(out, b.session)
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

// parser accumulates a partial inbound stream and emits display-ready
// bytes while consuming and answering telnet control sequences.
type parser struct {
	reply  io.Writer // negotiation replies go back to the peer here
	state  parserState
	cmd    byte   // pending IAC command byte (WILL/WONT/DO/DONT/SB)
	opt    byte   // option being negotiated or subnegotiated
	iacBuf []byte // subnegotiation accumulator
	sawCR  bool

	// width/height are the window dimensions advertised to the peer in
	// NAWS replies (RFC 1073). Zero values fall back to the conventional
	// 80x24 so a bare parser keeps the historical bridge behavior.
	width  int
	height int
}

// Session negotiates and filters one telnet connection's inbound byte
// stream. It is the shared protocol core of the interactive bridge and
// the assistant monitor: Feed returns display-ready bytes, and every
// negotiation reply (refusal-first policy, plus NAWS at the recorded
// window size) is written back to the connection.
type Session struct {
	parser
}

// NewSession creates a session whose negotiation replies go to conn.
// The advertised window starts at the conventional 80x24; SetSize
// updates it for later NAWS answers.
func NewSession(conn io.Writer) *Session {
	return &Session{parser{reply: conn, width: 80, height: 24}}
}

// Feed consumes one inbound network chunk and returns display bytes.
func (s *Session) Feed(chunk []byte) []byte { return s.parser.feed(chunk) }

// Write sends application data to the peer as RFC 854 NVT data. Every data
// byte 0xFF is stuffed as IAC IAC on the wire so the peer reads one literal
// 0xFF rather than treating it as the start of a telnet command. This is the
// single canonical outbound path: the monitor, automatic login and the raw
// bridge all use it, so no caller can accidentally inject an IAC command.
//
// It deliberately does NOT translate newlines — that is WriteLine's job —
// because raw byte streams may legitimately contain CR/LF that must pass
// through untouched.
func (s *Session) Write(data []byte) (int, error) {
	wire := stuffIAC(data)
	if s.reply == nil {
		return 0, io.ErrClosedPipe
	}
	if _, err := s.reply.Write(wire); err != nil {
		return 0, err
	}
	return len(data), nil
}

// WriteLine sends one line of application data terminated by the NVT line
// ending CRLF (RFC 854). Commands typed into a telnet terminal are lines:
// the peer's shell or CLI only executes them once it sees CR LF. Callers
// should never append their own line ending.
func (s *Session) WriteLine(line []byte) (int, error) {
	frame := make([]byte, 0, len(line)+2)
	frame = append(frame, line...)
	frame = append(frame, '\r', '\n')
	return s.Write(frame)
}

// stuffIAC doubles every data byte 0xFF (IAC) per RFC 854. A lone 0xFF in
// application data must travel as IAC IAC; otherwise the peer consumes the
// following byte as a command. Returns a fresh slice so callers' buffers
// are never mutated.
func stuffIAC(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	extra := 0
	for _, b := range data {
		if b == iacIAC {
			extra++
		}
	}
	if extra == 0 {
		out := make([]byte, len(data))
		copy(out, data)
		return out
	}
	out := make([]byte, 0, len(data)+extra)
	for _, b := range data {
		if b == iacIAC {
			out = append(out, iacIAC)
		}
		out = append(out, b)
	}
	return out
}

// SetSize records the window dimensions advertised via NAWS.
// Non-positive values are ignored so transient layout states cannot
// advertise a zero-size terminal.
func (s *Session) SetSize(w, h int) {
	if w > 0 && h > 0 {
		s.width, s.height = w, h
	}
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
func (p *parser) feed(chunk []byte) []byte {
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
			p.answer(p.cmd, c)
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
				p.handleSubneg(p.iacBuf)
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
func (p *parser) answer(cmd, opt byte) {
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
		_, _ = p.reply.Write(reply)
	}
}

// handleSubnegotiation consumes SB ... SE payloads. Only NAWS matters:
// answering it keeps some console servers from stalling at login. All
// other subnegotiations (TTYPE, NEW-ENVIRON probes...) are ignored,
// which is valid because we refused their enabling options.
func (p *parser) handleSubneg(buf []byte) {
	if len(buf) == 0 {
		return
	}
	switch buf[0] {
	case optNAWS:
		// Report the recorded window size, falling back to the
		// conventional 80x24. Widths/heights are sent as 16-bit values
		// where 255 must be escaped as IAC IAC.
		w, h := p.width, p.height
		if w <= 0 {
			w = 80
		}
		if h <= 0 {
			h = 24
		}
		payload := append([]byte{iacIAC, iacSB, optNAWS}, encodeNAWS(w)...)
		payload = append(payload, encodeNAWS(h)...)
		payload = append(payload, iacIAC, iacSE)
		_, _ = p.reply.Write(payload)
	}
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

	bridge := NewBridge(conn).WithAutoLogin(AutoLoginFor(c.host))
	if err := bridge.Run(c.stdin, out, errOut); err != nil {
		return fmt.Errorf("telnet session: %w", err)
	}
	// The bridge already prints "Disconnected." on its way out; printing it
	// here again showed the line twice on the exec path (monitor F8 switch).
	return nil
}
