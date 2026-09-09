package ftpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// errOnClose wraps a reader and returns a fixed error from Close.
type errOnClose struct {
	io.Reader
	closeErr error
	closed   bool
}

func (e *errOnClose) Close() error {
	e.closed = true
	return e.closeErr
}

func TestCopyDownloadFailClosed(t *testing.T) {
	closeFail := errors.New("426 connection closed; transfer aborted")

	tests := []struct {
		name      string
		data      []byte
		closeErr  error
		expected  int64
		sizeKnown bool
		cancel    bool
		wantErr   string
		wantFile  bool // local dest should exist after call
		wantSize  int64
	}{
		{
			name:      "size mismatch removes leftover empty file",
			data:      nil, // EOF immediately, 0 bytes
			expected:  100,
			sizeKnown: true,
			wantErr:   "size mismatch",
			wantFile:  false,
		},
		{
			name:      "size mismatch partial removes leftover",
			data:      []byte("partial"),
			expected:  100,
			sizeKnown: true,
			wantErr:   "size mismatch",
			wantFile:  false,
		},
		{
			name:      "close error after EOF fails and removes file",
			data:      []byte("hello"),
			closeErr:  closeFail,
			expected:  5,
			sizeKnown: true,
			wantErr:   "ftp data close",
			wantFile:  false,
		},
		{
			name:      "successful SIZE==0 empty file",
			data:      nil,
			expected:  0,
			sizeKnown: true,
			wantFile:  true,
			wantSize:  0,
		},
		{
			name:      "successful matching size",
			data:      []byte("hello world"),
			expected:  11,
			sizeKnown: true,
			wantFile:  true,
			wantSize:  11,
		},
		{
			name:      "SIZE unavailable and 0 bytes refuses empty",
			data:      nil,
			sizeKnown: false,
			wantErr:   "SIZE unavailable",
			wantFile:  false,
		},
		{
			name:      "SIZE unavailable non-empty clean transfer ok",
			data:      []byte("payload"),
			sizeKnown: false,
			wantFile:  true,
			wantSize:  7,
		},
		{
			name:      "cancel removes partial",
			data:      []byte("will-not-finish"),
			expected:  100,
			sizeKnown: true,
			cancel:    true,
			wantErr:   "context canceled",
			wantFile:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "out.bin")

			var src io.Reader = bytes.NewReader(tt.data)
			closer := &errOnClose{Reader: src, closeErr: tt.closeErr}

			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			}

			err := copyDownloadFailClosed(ctx, closer, closer, dest, tt.expected, tt.sizeKnown, nil)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}

			st, statErr := os.Stat(dest)
			if tt.wantFile {
				if statErr != nil {
					t.Fatalf("expected dest file to exist: %v", statErr)
				}
				if st.Size() != tt.wantSize {
					t.Fatalf("dest size = %d, want %d", st.Size(), tt.wantSize)
				}
			} else if !os.IsNotExist(statErr) {
				t.Fatalf("expected dest removed, stat err=%v size=%v", statErr, st)
			}

			if !tt.cancel && !closer.closed {
				t.Fatal("closer was not closed")
			}
		})
	}
}

// failAfterN reads n bytes then returns a fixed error (mid-transfer failure).
type failAfterN struct {
	data []byte
	n    int
	err  error
	off  int
}

func (f *failAfterN) Read(p []byte) (int, error) {
	if f.off >= f.n {
		return 0, f.err
	}
	remain := f.n - f.off
	if remain > len(p) {
		remain = len(p)
	}
	copy(p, f.data[f.off:f.off+remain])
	f.off += remain
	if f.off >= f.n {
		return remain, f.err
	}
	return remain, nil
}

func TestCopyDownloadFailClosed_emptyAndMidTransfer(t *testing.T) {
	t.Run("empty response with known SIZE deletes dest", func(t *testing.T) {
		dir := t.TempDir()
		dest := filepath.Join(dir, "empty.bin")
		closer := &errOnClose{Reader: bytes.NewReader(nil)}
		err := copyDownloadFailClosed(context.Background(), closer, closer, dest, 42, true, nil)
		if err == nil || !strings.Contains(err.Error(), "size mismatch") {
			t.Fatalf("want size mismatch, got %v", err)
		}
		if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
			t.Fatalf("dest must be deleted, stat=%v", statErr)
		}
	})

	t.Run("mid-transfer read failure deletes partial", func(t *testing.T) {
		dir := t.TempDir()
		dest := filepath.Join(dir, "partial.bin")
		midErr := errors.New("connection reset")
		src := &failAfterN{data: []byte("abcdefghij"), n: 4, err: midErr}
		closer := &errOnClose{Reader: src}
		err := copyDownloadFailClosed(context.Background(), closer, closer, dest, 10, true, nil)
		if err == nil || !strings.Contains(err.Error(), "connection reset") {
			t.Fatalf("want mid-transfer error, got %v", err)
		}
		if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
			t.Fatalf("partial dest must be deleted, stat=%v", statErr)
		}
		if !closer.closed {
			t.Fatal("data connection must be closed on read failure")
		}
	})
}

func TestCopyDownloadFailClosed_createFailureStillCloses(t *testing.T) {
	closer := &errOnClose{Reader: bytes.NewReader([]byte("x")), closeErr: nil}
	// Path into a non-existent directory so Create fails.
	err := copyDownloadFailClosed(context.Background(), closer, closer, "/no/such/dir/out.bin", 1, true, nil)
	if err == nil {
		t.Fatal("expected create error")
	}
	if !closer.closed {
		t.Fatal("must close data connection even when Create fails")
	}
}

// blockUntilClose blocks Read until Close is called (simulates hung FTP data conn).
type blockUntilClose struct {
	ch     chan struct{}
	closed bool
}

func (b *blockUntilClose) Read(p []byte) (int, error) {
	<-b.ch
	return 0, io.ErrClosedPipe
}

func (b *blockUntilClose) Close() error {
	if !b.closed {
		b.closed = true
		close(b.ch)
	}
	return nil
}

func TestCopyDownloadFailClosed_abortWakesBlockedRead(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	src := &blockUntilClose{ch: make(chan struct{})}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- copyDownloadFailClosed(ctx, src, src, dest, 100, true, nil)
	}()

	// Give the reader time to block inside Read.
	time.Sleep(50 * time.Millisecond)
	cancel()
	_ = src.Close() // mirrors AbortTransfer waking the data connection

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("want context canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Read was not woken by abort/close")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("partial dest must be removed on cancel")
	}
}

func TestAbortTransferNilSafe(t *testing.T) {
	c := &Client{}
	c.AbortTransfer() // must not panic with no active transfer
	if c.TransferActive() {
		t.Fatal("expected inactive")
	}
}
