package sftpconfig

import (
	"fmt"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sftpWrapper wraps github.com/pkg/sftp.Client to provide a clean interface.
type sftpWrapper struct {
	client *sftp.Client
}

// sftpFileWrapper wraps sftp.File to implement io.ReadWriteCloser.
type sftpFileWrapper struct {
	file *sftp.File
}

func (f *sftpFileWrapper) Read(p []byte) (int, error)  { return f.file.Read(p) }
func (f *sftpFileWrapper) Write(p []byte) (int, error) { return f.file.Write(p) }
func (f *sftpFileWrapper) Close() error                { return f.file.Close() }
func (f *sftpFileWrapper) Stat() (os.FileInfo, error) { return f.file.Stat() }

// sftpNewClient creates a new SFTP client over an SSH connection.
// First tries the standard SFTP subsystem; if that fails, falls back
// to exec-ing sftp-server directly (some hosts don't have subsystem configured).
func sftpNewClient(sshClient *ssh.Client) (*sftpWrapper, error) {
	// Try standard subsystem first
	sc, err := sftp.NewClient(sshClient)
	if err == nil {
		return &sftpWrapper{client: sc}, nil
	}

	// Subsystem failed — try exec sftp-server at common paths.
	// Some hosts don't have "Subsystem sftp" in sshd_config.
	paths := []string{
		"/usr/lib/openssh/sftp-server",
		"/usr/libexec/openssh/sftp-server",
		"/usr/libexec/sftp-server",
		"/usr/lib/sftp-server",
		"/usr/local/libexec/sftp-server",
	}
	var lastErr error = err
	for _, p := range paths {
		session, err2 := sshClient.NewSession()
		if err2 != nil {
			lastErr = err2
			continue
		}

		stdin, err2 := session.StdinPipe()
		if err2 != nil {
			session.Close()
			lastErr = err2
			continue
		}

		stdout, err2 := session.StdoutPipe()
		if err2 != nil {
			session.Close()
			lastErr = err2
			continue
		}

		// Start sftp-server
		if err2 := session.Start(p); err2 != nil {
			session.Close()
			lastErr = err2
			continue
		}

		// Create SFTP client over the pipes
		sc, err2 := sftp.NewClientPipe(stdout, stdin)
		if err2 != nil {
			session.Close()
			lastErr = err2
			continue
		}

		return &sftpWrapper{client: sc}, nil
	}

	return nil, fmt.Errorf("failed to start SFTP subsystem: %w", lastErr)
}

func (w *sftpWrapper) ReadDir(path string) ([]os.DirEntry, error) {
	entries, err := w.client.ReadDir(path)
	if err != nil {
		return nil, err
	}
	// Convert []fs.FileInfo to []os.DirEntry
	result := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, &sftpDirEntry{info: entry})
	}
	return result, nil
}

func (w *sftpWrapper) Open(path string) (*sftpFileWrapper, error) {
	f, err := w.client.Open(path)
	if err != nil {
		return nil, err
	}
	return &sftpFileWrapper{file: f}, nil
}

func (w *sftpWrapper) Create(path string) (*sftpFileWrapper, error) {
	f, err := w.client.Create(path)
	if err != nil {
		return nil, err
	}
	return &sftpFileWrapper{file: f}, nil
}

func (w *sftpWrapper) Stat(path string) (os.FileInfo, error) {
	return w.client.Stat(path)
}

func (w *sftpWrapper) Mkdir(path string) error {
	return w.client.Mkdir(path)
}

func (w *sftpWrapper) Remove(path string) error {
	return w.client.Remove(path)
}

func (w *sftpWrapper) RealPath(path string) (string, error) {
	return w.client.RealPath(path)
}

func (w *sftpWrapper) Close() error {
	return w.client.Close()
}

// sftpDirEntry adapts sftp.FileInfo (which is fs.FileInfo) to os.DirEntry.
type sftpDirEntry struct {
	info os.FileInfo
}

func (e *sftpDirEntry) Name() string               { return e.info.Name() }
func (e *sftpDirEntry) IsDir() bool                 { return e.info.IsDir() }
func (e *sftpDirEntry) Type() os.FileMode           { return e.info.Mode().Type() }
func (e *sftpDirEntry) Info() (os.FileInfo, error) { return e.info, nil }
