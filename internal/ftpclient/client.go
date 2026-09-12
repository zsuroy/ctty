// Package ftpclient wraps a maintained FTP library for ctty's FTP TUI/CLI.
//
// This is a separate transport from SFTP (no x/crypto/ssh, no pkg/sftp).
// Explicit FTPS/TLS is reserved for a follow-up flag.
package ftpclient

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/ftpcred"
)

// RemoteEntry is a remote file or directory listing row.
type RemoteEntry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// Client is a serialized FTP session used by the dual-pane TUI.
//
// Control-connection ops (List/CWD/…) take mu briefly. Long transfer IO does
// not hold mu; xferActive blocks overlapping control ops. AbortTransfer wakes a
// blocked data Read via SetDeadline without taking mu (same class as the old
// SFTP cancel deadlock).
type Client struct {
	conn *ftp.ServerConn
	mu   sync.Mutex
	site ftpconfig.FTPSite

	xferActive bool // under mu: transfer in progress (IO may run unlocked)

	dataMu     sync.Mutex
	activeResp *ftp.Response  // RETR data connection; Abort SetDeadline-wakes Read
	activePipe *io.PipeWriter // STOR pipe; Abort CloseWithError wakes writer
}

// Connect dials and logs into the given saved FTP site.
// password may be empty to try ftpcred, then anonymous empty password.
func Connect(site ftpconfig.FTPSite, password string) (*Client, error) {
	if password == "" {
		if saved, ok := ftpcred.GetPassword(site.Name); ok {
			password = saved
		}
	}
	user := site.User
	if user == "" {
		user = "anonymous"
	}

	conn, err := ftp.Dial(site.Addr(), ftp.DialWithTimeout(15*time.Second))
	if err != nil {
		return nil, fmt.Errorf("ftp dial %s: %w", site.Addr(), err)
	}
	if err := conn.Login(user, password); err != nil {
		_ = conn.Quit()
		return nil, fmt.Errorf("ftp login as %s: %w", user, err)
	}
	return &Client{conn: conn, site: site}, nil
}

// Close logs out and closes the control connection.
func (c *Client) Close() error {
	c.AbortTransfer()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Quit()
	c.conn = nil
	return err
}

// AbortTransfer wakes a blocked transfer Read/Write so cancel can finish.
// Safe to call without holding mu (and from the UI cancel path).
func (c *Client) AbortTransfer() {
	c.dataMu.Lock()
	resp := c.activeResp
	pw := c.activePipe
	c.dataMu.Unlock()
	if resp != nil {
		_ = resp.SetDeadline(time.Now())
	}
	if pw != nil {
		_ = pw.CloseWithError(context.Canceled)
	}
}

func (c *Client) setActiveResp(resp *ftp.Response) {
	c.dataMu.Lock()
	c.activeResp = resp
	c.dataMu.Unlock()
}

func (c *Client) clearActiveResp(resp *ftp.Response) {
	c.dataMu.Lock()
	if c.activeResp == resp {
		c.activeResp = nil
	}
	c.dataMu.Unlock()
}

func (c *Client) setActivePipe(pw *io.PipeWriter) {
	c.dataMu.Lock()
	c.activePipe = pw
	c.dataMu.Unlock()
}

func (c *Client) clearActivePipe(pw *io.PipeWriter) {
	c.dataMu.Lock()
	if c.activePipe == pw {
		c.activePipe = nil
	}
	c.dataMu.Unlock()
}

func (c *Client) beginTransfer() error {
	if c.conn == nil {
		return fmt.Errorf("ftp: not connected")
	}
	if c.xferActive {
		return fmt.Errorf("ftp: transfer in progress")
	}
	c.xferActive = true
	return nil
}

func (c *Client) endTransfer() {
	c.mu.Lock()
	c.xferActive = false
	c.mu.Unlock()
}

// CurrentDir returns the remote working directory.
func (c *Client) CurrentDir() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.xferActive {
		return "", fmt.Errorf("ftp: transfer in progress")
	}
	return c.conn.CurrentDir()
}

// ChangeDir changes the remote working directory.
func (c *Client) ChangeDir(dir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.xferActive {
		return fmt.Errorf("ftp: transfer in progress")
	}
	return c.conn.ChangeDir(dir)
}

// ListDir lists entries in a remote directory (absolute or relative).
func (c *Client) ListDir(dir string) ([]RemoteEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.xferActive {
		return nil, fmt.Errorf("ftp: transfer in progress")
	}
	entries, err := c.conn.List(dir)
	if err != nil {
		return nil, fmt.Errorf("ftp list %s: %w", dir, err)
	}
	out := make([]RemoteEntry, 0, len(entries))
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		out = append(out, RemoteEntry{
			Name:    e.Name,
			IsDir:   e.Type == ftp.EntryTypeFolder,
			Size:    int64(e.Size),
			ModTime: e.Time,
		})
	}
	return out, nil
}

// DownloadWithProgressCtx downloads a remote file to a local path.
//
// Fail-closed: the local destination is removed on any error (including
// cancel). When the server reports SIZE, the byte count must match exactly
// (SIZE==0 empty files are allowed). When SIZE is unsupported, a clean
// transfer that yields 0 bytes is still treated as failure — only a verified
// SIZE==0 empty remote is accepted as an empty local file. Transfer errors
// reported by the data-connection Close are also failures.
//
// The client mutex is not held across the data Read loop; AbortTransfer can
// wake a blocked Read via SetDeadline.
func (c *Client) DownloadWithProgressCtx(ctx context.Context, remotePath, localPath string, progress func(done, total int64)) error {
	c.mu.Lock()
	if err := c.beginTransfer(); err != nil {
		c.mu.Unlock()
		return err
	}

	var (
		expected  int64
		sizeKnown bool
	)
	if sz, err := c.conn.FileSize(remotePath); err == nil {
		expected = sz
		sizeKnown = true
	}

	resp, err := c.conn.Retr(remotePath)
	if err != nil {
		c.xferActive = false
		c.mu.Unlock()
		return fmt.Errorf("ftp retr %s: %w", remotePath, err)
	}
	c.setActiveResp(resp)
	c.mu.Unlock() // do not hold lock across long transfer IO

	defer func() {
		c.clearActiveResp(resp)
		c.endTransfer()
	}()

	// If ctx is already/soon canceled, wake a blocked Read even between chunks.
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-ctx.Done():
			c.AbortTransfer()
		case <-stopWatch:
		}
	}()

	return copyDownloadFailClosed(ctx, resp, resp, localPath, expected, sizeKnown, progress)
}

// copyDownloadFailClosed copies src into localPath with fail-closed semantics.
// closer is always closed; its error is checked after a clean read loop.
// Tests inject fake readers via this seam (no live FTP server required).
func copyDownloadFailClosed(
	ctx context.Context,
	src io.Reader,
	closer io.Closer,
	localPath string,
	expected int64,
	sizeKnown bool,
	progress func(done, total int64),
) (err error) {
	progressTotal := int64(0)
	if sizeKnown {
		progressTotal = expected
	}

	f, createErr := os.Create(localPath)
	if createErr != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return createErr
	}
	created := true
	defer func() {
		_ = f.Close()
		if err != nil && created {
			_ = os.Remove(localPath)
		}
	}()

	buf := make([]byte, 32*1024)
	var done int64
	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			if closer != nil {
				_ = closer.Close()
			}
			return err
		default:
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				err = werr
				if closer != nil {
					_ = closer.Close()
				}
				return err
			}
			done += int64(n)
			if progress != nil {
				progress(done, progressTotal)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			// Prefer ctx cancel when AbortTransfer woke a blocked Read.
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				err = rerr
			}
			if closer != nil {
				_ = closer.Close()
			}
			return err
		}
	}

	if closer != nil {
		if cerr := closer.Close(); cerr != nil {
			err = fmt.Errorf("ftp data close: %w", cerr)
			return err
		}
	}

	if sizeKnown {
		if done != expected {
			err = fmt.Errorf("ftp download size mismatch: got %d bytes, want %d", done, expected)
			return err
		}
		return nil
	}

	// SIZE unsupported: accept non-empty clean transfers; refuse silent empty.
	if done == 0 {
		err = fmt.Errorf("ftp download got 0 bytes and SIZE unavailable; refusing empty file")
		return err
	}
	return nil
}

// UploadWithProgressCtx uploads a local file to a remote path.
// Mutex is not held across the local-read / pipe-write loop.
func (c *Client) UploadWithProgressCtx(ctx context.Context, localPath, remotePath string, progress func(done, total int64)) error {
	c.mu.Lock()
	if err := c.beginTransfer(); err != nil {
		c.mu.Unlock()
		return err
	}

	f, err := os.Open(localPath)
	if err != nil {
		c.xferActive = false
		c.mu.Unlock()
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		c.xferActive = false
		c.mu.Unlock()
		return err
	}
	total := st.Size()

	pr, pw := io.Pipe()
	c.setActivePipe(pw)
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.conn.Stor(remotePath, pr)
	}()
	c.mu.Unlock() // do not hold lock across long transfer IO

	defer func() {
		c.clearActivePipe(pw)
		c.endTransfer()
	}()

	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-ctx.Done():
			c.AbortTransfer()
		case <-stopWatch:
		}
	}()

	buf := make([]byte, 32*1024)
	var done int64
	var copyErr error
	for {
		select {
		case <-ctx.Done():
			_ = pw.CloseWithError(ctx.Err())
			<-errCh
			return ctx.Err()
		default:
		}
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := pw.Write(buf[:n]); werr != nil {
				if ctx.Err() != nil {
					copyErr = ctx.Err()
				} else {
					copyErr = werr
				}
				break
			}
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			copyErr = rerr
			break
		}
	}
	if copyErr != nil {
		_ = pw.CloseWithError(copyErr)
		<-errCh
		return copyErr
	}
	_ = pw.Close()
	if err := <-errCh; err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ftp stor %s: %w", remotePath, err)
	}
	return nil
}

// MakeDir creates a remote directory.
func (c *Client) MakeDir(dir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.xferActive {
		return fmt.Errorf("ftp: transfer in progress")
	}
	return c.conn.MakeDir(dir)
}

// Delete removes a remote file.
func (c *Client) Delete(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.xferActive {
		return fmt.Errorf("ftp: transfer in progress")
	}
	return c.conn.Delete(path)
}

// Rename moves a remote file or directory to a new path (RNFR/RNTO).
func (c *Client) Rename(oldPath, newPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.xferActive {
		return fmt.Errorf("ftp: transfer in progress")
	}
	return c.conn.Rename(oldPath, newPath)
}

// JoinRemote joins FTP path segments with forward slashes.
func JoinRemote(elem ...string) string {
	return path.Join(elem...)
}

// TransferActive reports whether a transfer is in progress (for tests).
func (c *Client) TransferActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.xferActive
}
