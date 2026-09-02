package telnetclient

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// chanConn adapts a channel of byte slices to net.Conn so tests can
// capture what the parser "sends over the network" without blocking:
// net.Pipe is synchronous and deadlocks against parser.answer.
type chanConn struct {
	out chan []byte
}

func newChanConn() *chanConn { return &chanConn{out: make(chan []byte, 8)} }

func (c *chanConn) Read(b []byte) (int, error)         { return 0, io.EOF }
func (c *chanConn) Write(b []byte) (int, error)        { c.out <- append([]byte{}, b...); return len(b), nil }
func (c *chanConn) Close() error                       { return nil }
func (c *chanConn) LocalAddr() net.Addr                { return nil }
func (c *chanConn) RemoteAddr() net.Addr               { return nil }
func (c *chanConn) SetDeadline(t time.Time) error      { return nil }
func (c *chanConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *chanConn) SetWriteDeadline(t time.Time) error { return nil }

// expectReply reads one reply, failing if none arrives within a second.
func expectReply(t *testing.T, ch <-chan []byte, want []byte, label string) {
	t.Helper()
	select {
	case got := <-ch:
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: reply = %v, want %v", label, got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("%s: no reply within 1s", label)
	}
}

// expectSilence asserts the parser sent nothing.
func expectSilence(t *testing.T, ch <-chan []byte, label string) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("%s: unexpected reply %v", label, got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestFeedPassthrough(t *testing.T) {
	p := &parser{}
	got := p.feed([]byte("login: "), nil)
	if want := "login: "; string(got) != want {
		t.Fatalf("feed passthrough = %q, want %q", got, want)
	}
}

func TestFeedIACUnescape(t *testing.T) {
	p := &parser{}
	got := p.feed([]byte{'a', iacIAC, iacIAC, 'b'}, nil)
	if len(got) != 3 || got[0] != 'a' || got[1] != 0xFF || got[2] != 'b' {
		t.Fatalf("IAC IAC not unescaped to single 0xFF: %v", got)
	}
}

func TestFeedSplitSequenceAcrossChunks(t *testing.T) {
	conn := newChanConn()
	p := &parser{bridge: NewBridge(conn)}
	if out := p.feed([]byte{'x', iacIAC}, nil); string(out) != "x" {
		t.Fatalf("first chunk should hold only display bytes, got %q", out)
	}
	if out := p.feed([]byte{iacWILL, optECHO}, nil); len(out) != 0 {
		t.Fatalf("negotiation bytes must not leak to display, got %q", out)
	}
	if p.state != stateData {
		t.Fatalf("parser stuck in state %v", p.state)
	}
	expectReply(t, conn.out, []byte{iacIAC, iacDO, optECHO}, "split WILL ECHO")
}

func TestAnswerWILLECHOAccepted(t *testing.T) {
	conn := newChanConn()
	p := &parser{bridge: NewBridge(conn)}
	p.answer(iacWILL, optECHO, nil)
	expectReply(t, conn.out, []byte{iacIAC, iacDO, optECHO}, "WILL ECHO")
}

func TestAnswerDOSGAAccepted(t *testing.T) {
	conn := newChanConn()
	p := &parser{bridge: NewBridge(conn)}
	p.answer(iacDO, optSGA, nil)
	expectReply(t, conn.out, []byte{iacIAC, iacWILL, optSGA}, "DO SGA")
}

func TestAnswerDOUnknownRefused(t *testing.T) {
	conn := newChanConn()
	p := &parser{bridge: NewBridge(conn)}
	p.answer(iacDO, optTTYPE, nil)
	expectReply(t, conn.out, []byte{iacIAC, iacWONT, optTTYPE}, "DO TTYPE")
}

func TestAnswerWILLUnknownRefused(t *testing.T) {
	conn := newChanConn()
	p := &parser{bridge: NewBridge(conn)}
	p.answer(iacWILL, optNEWENV, nil)
	expectReply(t, conn.out, []byte{iacIAC, iacDONT, optNEWENV}, "WILL NEW-ENV")
}

func TestSubnegSwallowedAndNAWSAnswered(t *testing.T) {
	conn := newChanConn()
	p := &parser{bridge: NewBridge(conn)}

	// TTYPE subnegotiation: swallowed silently, no reply sent.
	seq := []byte{iacIAC, iacSB, optTTYPE, 0, 'x', 't', 'e', 'r', 'm', iacIAC, iacSE}
	if out := p.feed(seq, nil); len(out) != 0 {
		t.Fatalf("subnegotiation leaked %q to display", out)
	}
	expectSilence(t, conn.out, "TTYPE")

	// NAWS: answered with conventional 80x24 (IAC-escaped payload).
	if out := p.feed([]byte{iacIAC, iacSB, optNAWS}, nil); len(out) != 0 {
		t.Fatalf("SB header leaked %q", out)
	}
	if out := p.feed([]byte{iacIAC, iacSE}, nil); len(out) != 0 {
		t.Fatalf("SE leaked %q", out)
	}
	expectReply(t, conn.out,
		[]byte{iacIAC, iacSB, optNAWS, 0x00, 80, 0x00, 24, iacIAC, iacSE},
		"NAWS")
}

func TestEncodeNAWS(t *testing.T) {
	if got := encodeNAWS(80); !bytes.Equal(got, []byte{0x00, 80}) {
		t.Fatalf("encodeNAWS(80) = %v", got)
	}
	// 255 must be escaped as IAC IAC per RFC 1073.
	if got := encodeNAWS(255); !bytes.Equal(got, []byte{0x00, iacIAC, iacIAC}) {
		t.Fatalf("encodeNAWS(255) = %v, want escaped 0xFF", got)
	}
}

func TestFilterIAC(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"plain text", []byte("hi"), []byte("hi")},
		{"doubled IAC becomes literal", []byte{'a', 0xFF, 0xFF, 'b'}, []byte{'a', 0xFF, 'b'}},
		{"lone trailing IAC dropped", []byte{'a', 0xFF}, []byte{'a'}},
		{"escape prefix dropped", []byte{'a', 0xFF, iacDO, 0x01}, []byte{'a', 0x01}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterIAC(append([]byte{}, tt.in...))
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("filterIAC(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestContainsDisconnect(t *testing.T) {
	b := &Bridge{}
	if b.containsDisconnect([]byte("abc")) {
		t.Fatal("plain text must not trigger disconnect")
	}
	if !b.containsDisconnect([]byte{'x', 0x1d}) {
		t.Fatal("Ctrl-] (0x1d) must trigger disconnect")
	}
}

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		in      string
		def     int
		host    string
		port    int
		wantErr bool
	}{
		{"192.168.1.1", 23, "192.168.1.1", 23, false},
		{"192.168.1.1:2001", 23, "192.168.1.1", 2001, false},
		{"::1", 23, "::1", 23, false},
		{"[::1]:2323", 23, "::1", 2323, false},
		{"console.lab", 9100, "console.lab", 9100, false},
		{"host:badport", 23, "", 0, true},
		{"host:", 23, "", 0, true},
		{"host:70000", 23, "", 0, true},
		{":23", 23, "", 0, true},
		{"", 23, "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			host, port, err := ParseHostPort(tt.in, tt.def)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseHostPort(%q) expected error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHostPort(%q): %v", tt.in, err)
			}
			if host != tt.host || port != tt.port {
				t.Fatalf("ParseHostPort(%q) = (%q, %d), want (%q, %d)",
					tt.in, host, port, tt.host, tt.port)
			}
		})
	}
}
