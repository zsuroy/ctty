package sessionsnippet

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zsuroy/ctty/internal/snippets"
)

func newTestPicker(list []snippets.Snippet) (*Picker, *OutputGate, *bytes.Buffer) {
	var term bytes.Buffer
	gate := NewOutputGate(&term)
	p := NewPicker(gate)
	p.load = func() []snippets.Snippet { return list }
	return p, gate, &term
}

var sample = []snippets.Snippet{
	{Name: "vlan", Command: "show vlan brief"},
	{Name: "int", Command: "show interfaces status"},
}

func TestPassthroughWithoutHotkey(t *testing.T) {
	p, _, term := newTestPicker(sample)
	if got := p.Process([]byte("show ver\r")); string(got) != "show ver\r" {
		t.Fatalf("passthrough = %q", got)
	}
	if term.Len() != 0 {
		t.Fatalf("nothing should be drawn, got %q", term.String())
	}
}

func TestSelectInsertsCommandWithoutNewline(t *testing.T) {
	p, _, term := newTestPicker(sample)
	if got := p.Process([]byte{Hotkey}); len(got) != 0 {
		t.Fatalf("hotkey must not reach the device, got %q", got)
	}
	if !p.Active() || !strings.Contains(term.String(), "show interfaces status") {
		t.Fatalf("list not drawn: %q", term.String())
	}
	got := p.Process([]byte("2\r"))
	if string(got) != "show interfaces status" {
		t.Fatalf("inserted = %q", got)
	}
	if p.Active() {
		t.Fatal("picker should close after a choice")
	}
}

func TestBytesAfterChoiceInSameChunkPassThrough(t *testing.T) {
	p, _, _ := newTestPicker(sample)
	got := p.Process([]byte{'a', Hotkey, '1', '\r', 'b'})
	if string(got) != "ashow vlan briefb" {
		t.Fatalf("got %q", got)
	}
}

func TestMultiDigitAndBackspace(t *testing.T) {
	list := make([]snippets.Snippet, 12)
	for i := range list {
		list[i] = snippets.Snippet{Name: "n", Command: "cmd" + string(rune('a'+i))}
	}
	p, _, _ := newTestPicker(list)
	p.Process([]byte{Hotkey})
	got := p.Process([]byte{'1', '3', 0x7f, '2', '\r'})
	if string(got) != "cmdl" {
		t.Fatalf("got %q, want cmdl (#12)", got)
	}
}

func TestCancelKeys(t *testing.T) {
	for _, key := range []byte{Hotkey, 0x03} {
		p, _, _ := newTestPicker(sample)
		p.Process([]byte{Hotkey})
		if got := p.Process([]byte{key, 'x'}); string(got) != "x" {
			t.Fatalf("key %#x: got %q", key, got)
		}
		if p.Active() {
			t.Fatalf("key %#x should close the picker", key)
		}
	}
}

func TestInvalidNumberSendsNothing(t *testing.T) {
	p, _, _ := newTestPicker(sample)
	p.Process([]byte{Hotkey})
	if got := p.Process([]byte("9\r")); len(got) != 0 {
		t.Fatalf("out-of-range choice sent %q", got)
	}
	if p.Active() {
		t.Fatal("picker should close")
	}
}

func TestEmptyListShowsHintAndStaysClosed(t *testing.T) {
	p, _, term := newTestPicker(nil)
	p.Process([]byte{Hotkey})
	if p.Active() {
		t.Fatal("empty list must not open")
	}
	if !strings.Contains(term.String(), "snippets.json") {
		t.Fatalf("hint missing path: %q", term.String())
	}
}

func TestGateHoldsOutputWhileOpen(t *testing.T) {
	p, gate, term := newTestPicker(sample)
	p.Process([]byte{Hotkey})
	drawn := term.Len()
	_, _ = gate.Write([]byte("DEVICE"))
	if term.Len() != drawn {
		t.Fatal("device output leaked while picker open")
	}
	p.Process([]byte{0x1b})
	if !strings.HasSuffix(term.String(), "DEVICE") {
		t.Fatalf("held output not flushed: %q", term.String())
	}
}

func TestGateBufferIsCapped(t *testing.T) {
	var term bytes.Buffer
	g := NewOutputGate(&term)
	g.pause()
	_, _ = g.Write(bytes.Repeat([]byte("a"), maxBuffered))
	_, _ = g.Write([]byte("END"))
	g.resume()
	if term.Len() != maxBuffered || !strings.HasSuffix(term.String(), "END") {
		t.Fatalf("len=%d", term.Len())
	}
}

func TestLoneEscCancels(t *testing.T) {
	p, _, _ := newTestPicker(sample)
	p.Process([]byte{Hotkey})
	if got := p.Process([]byte{0x1b}); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
	if p.Active() {
		t.Fatal("lone Esc should close the picker")
	}
	if got := p.Process([]byte("x")); string(got) != "x" {
		t.Fatalf("typing after cancel = %q", got)
	}
}

func TestEscThenKeyInSameReadCancelsAndDropsKey(t *testing.T) {
	p, _, _ := newTestPicker(sample)
	p.Process([]byte{Hotkey})
	if got := p.Process([]byte{0x1b, 'x', 'y'}); string(got) != "y" {
		t.Fatalf("got %q", got)
	}
	if p.Active() {
		t.Fatal("should be closed")
	}
}

func TestArrowKeysWhileOpenAreSwallowed(t *testing.T) {
	for _, seq := range []string{"\x1b[A", "\x1b[B", "\x1bOA", "\x1b[1;5C", "\x1b[3~"} {
		p, _, _ := newTestPicker(sample)
		p.Process([]byte{Hotkey})
		if got := p.Process([]byte(seq)); len(got) != 0 {
			t.Fatalf("%q leaked %q to the device", seq, got)
		}
		if !p.Active() {
			t.Fatalf("%q must not close the picker", seq)
		}
		if got := p.Process([]byte("1\r")); string(got) != "show vlan brief" {
			t.Fatalf("%q: choice after arrow = %q", seq, got)
		}
	}
}

func TestArrowSplitAcrossReadsIsSwallowed(t *testing.T) {
	p, _, _ := newTestPicker(sample)
	p.Process([]byte{Hotkey})
	p.Process([]byte{0x1b}) // looks like a lone Esc: closes
	if got := p.Process([]byte("[Ash")); string(got) != "sh" {
		t.Fatalf("split tail leaked: %q", got)
	}
}

func TestArrowKeysPassThroughWhenClosed(t *testing.T) {
	p, _, _ := newTestPicker(sample)
	if got := p.Process([]byte("\x1b[A")); string(got) != "\x1b[A" {
		t.Fatalf("history key must reach the device, got %q", got)
	}
}
