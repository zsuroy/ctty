// Package sessionsnippet lets the user insert a saved command into a live
// telnet or serial session. Both bridges pump stdin themselves, so they run
// each keyboard chunk through a Picker before writing it to the device, and
// send device output through an OutputGate so the list is not overwritten
// while it is open.
package sessionsnippet

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/snippets"
)

// Hotkey opens the picker: Ctrl+\ (0x1c). Ctrl+] stays the disconnect key.
const Hotkey byte = 0x1c

// HotkeyLabel is the user-facing name of Hotkey.
const HotkeyLabel = `Ctrl+\`

// maxBuffered caps device output held while the picker is open; older
// bytes are dropped first.
const maxBuffered = 256 << 10

// OutputGate forwards device output to the terminal, holding it while the
// picker is on screen and flushing it once the picker closes.
type OutputGate struct {
	mu     sync.Mutex
	w      io.Writer
	paused bool
	buf    bytes.Buffer
}

// NewOutputGate wraps the terminal writer.
func NewOutputGate(w io.Writer) *OutputGate { return &OutputGate{w: w} }

// Write implements io.Writer.
func (g *OutputGate) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused {
		g.buf.Write(p)
		if over := g.buf.Len() - maxBuffered; over > 0 {
			g.buf.Next(over)
		}
		return len(p), nil
	}
	return g.w.Write(p)
}

func (g *OutputGate) pause() {
	g.mu.Lock()
	g.paused = true
	g.mu.Unlock()
}

func (g *OutputGate) resume() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.paused = false
	if g.buf.Len() > 0 {
		_, _ = g.w.Write(g.buf.Bytes())
		g.buf.Reset()
	}
}

// direct writes picker UI to the terminal regardless of the pause state.
func (g *OutputGate) direct(s string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, _ = io.WriteString(g.w, s)
}

// Picker is the keyboard-side state machine. It is used only from the
// stdin goroutine.
type Picker struct {
	gate   *OutputGate
	load   func() []snippets.Snippet
	active bool
	list   []snippets.Snippet
	input  []byte
	esc    escState
}

// escState tracks a terminal escape sequence (arrow keys send ESC [ A,
// some terminals ESC O A) so it is swallowed whole while the list is open
// instead of leaking "[A" to the device. A lone Esc cancels.
type escState int

const (
	escNone  escState = iota
	escStart          // saw ESC, sequence or lone Esc not yet known
	escCSI            // inside ESC [ ... until a final byte 0x40-0x7e
	escSS3            // ESC O, one more byte
)

// NewPicker returns a picker that lists the user's saved snippets.
func NewPicker(gate *OutputGate) *Picker {
	return &Picker{gate: gate, load: snippets.Load}
}

// Active reports whether the list is open.
func (p *Picker) Active() bool { return p.active }

// Process consumes one stdin chunk and returns the bytes to send to the
// device. Outside the picker it passes everything through except Hotkey.
// The chosen command is inserted without a trailing newline so the user
// can review it on the device prompt and press Enter themselves.
func (p *Picker) Process(chunk []byte) []byte {
	var out []byte
	for _, c := range chunk {
		if p.esc != escNone {
			if p.consumeEscape(c) {
				continue
			}
		}
		if !p.active {
			if c == Hotkey {
				p.open()
				continue
			}
			out = append(out, c)
			continue
		}
		switch {
		case c == 0x1b:
			p.esc = escStart
		case c == Hotkey || c == 0x03: // Hotkey, Ctrl+C
			p.close("")
		case c == '\r' || c == '\n':
			out = append(out, p.choose()...)
		case c == 0x7f || c == 0x08:
			if len(p.input) > 0 {
				p.input = p.input[:len(p.input)-1]
				p.gate.direct("\b \b")
			}
		case c >= '0' && c <= '9' && len(p.input) < 4:
			p.input = append(p.input, c)
			p.gate.direct(string(c))
		}
		// Any other byte (letters, punctuation) is ignored while open.
	}
	// ESC as the last byte of a read is the Esc key on its own. Keep
	// escStart so a sequence tail split into the next read is still dropped.
	if p.active && p.esc == escStart {
		p.close("")
	}
	return out
}

// consumeEscape advances the escape state for c and reports whether c was
// part of the sequence (and so must not be sent or interpreted).
func (p *Picker) consumeEscape(c byte) bool {
	switch p.esc {
	case escStart:
		switch c {
		case '[':
			p.esc = escCSI
			return true
		case 'O':
			p.esc = escSS3
			return true
		}
		// ESC followed by an ordinary key: the ESC was a lone Esc.
		p.esc = escNone
		if p.active {
			p.close("")
			return true
		}
		return false
	case escCSI:
		if c >= 0x40 && c <= 0x7e {
			p.esc = escNone
		}
		return true
	case escSS3:
		p.esc = escNone
		return true
	}
	return false
}

// Close restores output if the session ends with the picker open.
func (p *Picker) Close() {
	if p.active {
		p.close("")
	}
}

func (p *Picker) open() {
	p.list = p.load()
	if len(p.list) == 0 {
		p.gate.direct("\r\n" + i18n.T("session_snippet.empty", snippets.Path()) + "\r\n")
		return
	}
	p.active = true
	p.input = p.input[:0]
	p.gate.pause()
	var b bytes.Buffer
	b.WriteString("\r\n" + i18n.T("session_snippet.title", HotkeyLabel) + "\r\n")
	for i, s := range p.list {
		fmt.Fprintf(&b, "  %2d) %s  %s\r\n", i+1, truncate(s.Name, 24), truncate(s.Command, 60))
	}
	b.WriteString(i18n.T("session_snippet.prompt"))
	p.gate.direct(b.String())
}

func (p *Picker) choose() []byte {
	n, err := strconv.Atoi(string(p.input))
	if err != nil || n < 1 || n > len(p.list) {
		p.close(i18n.T("session_snippet.cancelled"))
		return nil
	}
	cmd := p.list[n-1].Command
	p.close("")
	return []byte(cmd)
}

func (p *Picker) close(msg string) {
	p.active = false
	p.input = p.input[:0]
	if msg != "" {
		p.gate.direct("\r\n" + msg)
	}
	p.gate.direct("\r\n")
	p.gate.resume()
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
