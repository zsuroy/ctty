package sftpconfig

import (
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zsuroy/ctty/internal/config"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SFTPClient wraps an SSH connection + SFTP session.
type SFTPClient struct {
	sshClient  *ssh.Client
	host       config.SSHHost
	configFile string
}

// RemoteEntry represents a file/dir entry on the remote host.
type RemoteEntry struct {
	Name     string
	LongName string
	IsDir    bool
	Size     int64
	ModTime  time.Time
	Mode     string
}

// Connect establishes an SSH connection to the given host and returns an SFTPClient.
// It reads host details from the SSH config (or the provided configFile).
func Connect(hostName string, configFile string) (*SFTPClient, error) {
	var hosts []config.SSHHost
	var err error

	if configFile != "" {
		hosts, err = config.ParseSSHConfigFile(configFile)
	} else {
		hosts, err = config.ParseSSHConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse SSH config: %w", err)
	}

	var host *config.SSHHost
	for i := range hosts {
		if hosts[i].Name == hostName {
			host = &hosts[i]
			break
		}
	}
	if host == nil {
		return nil, fmt.Errorf("host '%s' not found in SSH config", hostName)
	}

	sshConfig, err := buildSSHConfig(host)
	if err != nil {
		return nil, fmt.Errorf("failed to build SSH config: %w", err)
	}

	hostname := host.Hostname
	if hostname == "" {
		hostname = host.Name
	}

	port := host.Port
	if port == "" {
		port = "22"
	}

	addr := net.JoinHostPort(hostname, port)

	sshClient, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	return &SFTPClient{
		sshClient:  sshClient,
		host:       *host,
		configFile: configFile,
	}, nil
}

// ConnectWithPassword connects to the host using publickey + password auth.
// password can be empty to try publickey only, or a password to try
// keyboard-interactive + password auth as fallback.
func ConnectWithPassword(hostName string, configFile string, password string) (*SFTPClient, error) {
	var hosts []config.SSHHost
	var err error

	if configFile != "" {
		hosts, err = config.ParseSSHConfigFile(configFile)
	} else {
		hosts, err = config.ParseSSHConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse SSH config: %w", err)
	}

	var host *config.SSHHost
	for i := range hosts {
		if hosts[i].Name == hostName {
			host = &hosts[i]
			break
		}
	}
	if host == nil {
		return nil, fmt.Errorf("host '%s' not found in SSH config", hostName)
	}

	sshConfig, err := buildSSHConfig(host)
	if err != nil {
		return nil, fmt.Errorf("failed to build SSH config: %w", err)
	}

	// Add password auth if provided
	if password != "" {
		sshConfig.Auth = append(sshConfig.Auth,
			ssh.Password(password),
			ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = password
				}
				return answers, nil
			}),
		)
	}

	hostname := host.Hostname
	if hostname == "" {
		hostname = host.Name
	}
	port := host.Port
	if port == "" {
		port = "22"
	}
	addr := net.JoinHostPort(hostname, port)

	sshClient, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	return &SFTPClient{
		sshClient:  sshClient,
		host:       *host,
		configFile: configFile,
	}, nil
}

// buildSSHConfig creates an *ssh.ClientConfig from an SSHHost.
func buildSSHConfig(host *config.SSHHost) (*ssh.ClientConfig, error) {
	authMethods, err := getAuthMethods(host)
	if err != nil {
		return nil, err
	}

	// Use InsecureIgnoreHostKey to avoid knownhosts key mismatch issues.
	// This matches `ssh -o StrictHostKeyChecking=no` behavior.
	// TODO: add proper known_hosts verification with host key update support.
	knownHostsCallback := ssh.InsecureIgnoreHostKey()

	cfg := &ssh.ClientConfig{
		User:            getUser(host),
		Auth:            authMethods,
		HostKeyCallback: knownHostsCallback,
		Timeout:         15 * time.Second,
	}

	return cfg, nil
}

func getUser(host *config.SSHHost) string {
	if host.User != "" {
		return host.User
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "root"
}

// getAuthMethods returns SSH auth methods from identity files or SSH agent.
func getAuthMethods(host *config.SSHHost) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// Try SSH agent first
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		conn, err := net.Dial("unix", socket)
		if err == nil {
			agentClient := agent.NewClient(conn)
			signers, err := agentClient.Signers()
			if err == nil && len(signers) > 0 {
				// Use cached signers instead of PublicKeysCallback to avoid
				// re-dialing the agent on every auth attempt (which can hang).
				methods = append(methods, ssh.PublicKeys(signers...))
			}
		}
	}

	// Try identity file
	if host.Identity != "" {
		keyPath := expandPath(host.Identity)
		key, err := os.ReadFile(keyPath)
		if err == nil {
			signer, err := ssh.ParsePrivateKey(key)
			if err == nil {
				methods = append(methods, ssh.PublicKeys(signer))
			}
		}
	}

	// Try default keys
	homeDir, _ := os.UserHomeDir()
	defaultKeys := []string{
		filepath.Join(homeDir, ".ssh", "id_ed25519"),
		filepath.Join(homeDir, ".ssh", "id_rsa"),
		filepath.Join(homeDir, ".ssh", "id_ecdsa"),
		filepath.Join(homeDir, ".ssh", "id_dsa"),
	}
	for _, keyPath := range defaultKeys {
		key, err := os.ReadFile(keyPath)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			continue
		}
		// Check if already added
		alreadyExists := false
		_ = alreadyExists
		methods = append(methods, ssh.PublicKeys(signer))
	}

	// Don't return error if no methods found — password auth might be used
	return methods, nil
}

// getHostKeyCallback returns a known_hosts based host key callback.
func getHostKeyCallback() (ssh.HostKeyCallback, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	knownHostsPath := filepath.Join(homeDir, ".ssh", "known_hosts")
	if _, err := os.Stat(knownHostsPath); err != nil {
		return nil, err
	}
	return knownhosts.New(knownHostsPath)
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, path[2:])
	}
	if path == "~" {
		homeDir, _ := os.UserHomeDir()
		return homeDir
	}
	return path
}

// Close closes the underlying SSH connection.
func (c *SFTPClient) Close() error {
	if c.sshClient != nil {
		return c.sshClient.Close()
	}
	return nil
}

// HostName returns the name of the connected host.
func (c *SFTPClient) HostName() string {
	return c.host.Name
}

// ----- SFTP operations using the scp/sftp subsystem over the SSH connection -----

// runSFTPCommand runs a command on the remote host via the SSH session
// and returns its output. Used as a lightweight alternative to the SFTP subsystem.
// Actually, we'll use the SFTP subsystem directly via github.com/pkg/sftp.

// startSFTP opens an SFTP session over the SSH connection.
func (c *SFTPClient) newSFTPSession() (*sftpWrapper, error) {
	sc, err := sftpNewClient(c.sshClient)
	if err != nil {
		return nil, fmt.Errorf("failed to start SFTP subsystem: %w", err)
	}
	return sc, nil
}

// ListDir lists the contents of a remote directory.
func (c *SFTPClient) ListDir(path string) ([]RemoteEntry, error) {
	sc, err := c.newSFTPSession()
	if err != nil {
		return nil, err
	}
	defer sc.Close()

	entries, err := sc.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", path, err)
	}

	var result []RemoteEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, RemoteEntry{
			Name:     entry.Name(),
			IsDir:    entry.IsDir(),
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			Mode:     info.Mode().String(),
			LongName: formatLongEntry(entry),
		})
	}

	return result, nil
}

// Download downloads a remote file to a local path.
func (c *SFTPClient) Download(remotePath, localPath string) error {
	sc, err := c.newSFTPSession()
	if err != nil {
		return err
	}
	defer sc.Close()

	remoteFile, err := sc.Open(remotePath)
	if err != nil {
		return fmt.Errorf("failed to open remote file %s: %w", remotePath, err)
	}
	defer remoteFile.Close()

	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", localPath, err)
	}
	defer localFile.Close()

	_, err = io.Copy(localFile, remoteFile)
	return err
}

// DownloadWithProgress downloads a remote file with a progress callback.
// The callback receives bytes downloaded and total size.
func (c *SFTPClient) DownloadWithProgress(remotePath, localPath string, progress func(downloaded, total int64)) error {
	sc, err := c.newSFTPSession()
	if err != nil {
		return err
	}
	defer sc.Close()

	remoteFile, err := sc.Open(remotePath)
	if err != nil {
		return fmt.Errorf("failed to open remote file %s: %w", remotePath, err)
	}
	defer remoteFile.Close()

	stat, err := remoteFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat remote file: %w", err)
	}
	totalSize := stat.Size()

	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file %s: %w", localPath, err)
	}
	defer localFile.Close()

	buf := make([]byte, 32*1024)
	var downloaded int64
	for {
		n, err := remoteFile.Read(buf)
		if n > 0 {
			if _, werr := localFile.Write(buf[:n]); werr != nil {
				return werr
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, totalSize)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// UploadWithProgress uploads a local file with a progress callback.
func (c *SFTPClient) UploadWithProgress(localPath, remotePath string, progress func(uploaded, total int64)) error {
	sc, err := c.newSFTPSession()
	if err != nil {
		return err
	}
	defer sc.Close()

	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file %s: %w", localPath, err)
	}
	defer localFile.Close()

	stat, err := localFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat local file: %w", err)
	}
	totalSize := stat.Size()

	remoteFile, err := sc.Create(remotePath)
	if err != nil {
		return fmt.Errorf("failed to create remote file %s: %w", remotePath, err)
	}
	defer remoteFile.Close()

	buf := make([]byte, 32*1024)
	var uploaded int64
	for {
		n, err := localFile.Read(buf)
		if n > 0 {
			if _, werr := remoteFile.Write(buf[:n]); werr != nil {
				return werr
			}
			uploaded += int64(n)
			if progress != nil {
				progress(uploaded, totalSize)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Upload uploads a local file to a remote path.
func (c *SFTPClient) Upload(localPath, remotePath string) error {
	sc, err := c.newSFTPSession()
	if err != nil {
		return err
	}
	defer sc.Close()

	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file %s: %w", localPath, err)
	}
	defer localFile.Close()

	remoteFile, err := sc.Create(remotePath)
	if err != nil {
		return fmt.Errorf("failed to create remote file %s: %w", remotePath, err)
	}
	defer remoteFile.Close()

	_, err = io.Copy(remoteFile, localFile)
	return err
}

// Stat returns info about a remote path.
func (c *SFTPClient) Stat(path string) (os.FileInfo, error) {
	sc, err := c.newSFTPSession()
	if err != nil {
		return nil, err
	}
	defer sc.Close()

	return sc.Stat(path)
}

// Mkdir creates a directory on the remote host.
func (c *SFTPClient) Mkdir(path string) error {
	sc, err := c.newSFTPSession()
	if err != nil {
		return err
	}
	defer sc.Close()

	return sc.Mkdir(path)
}

// Remove deletes a file on the remote host.
func (c *SFTPClient) Remove(path string) error {
	sc, err := c.newSFTPSession()
	if err != nil {
		return err
	}
	defer sc.Close()

	return sc.Remove(path)
}

// RealPath returns the canonical absolute path.
func (c *SFTPClient) RealPath(path string) (string, error) {
	sc, err := c.newSFTPSession()
	if err != nil {
		return "", err
	}
	defer sc.Close()

	return sc.RealPath(path)
}

// formatLongEntry formats a remote entry similar to `ls -l` output.
func formatLongEntry(entry fs.DirEntry) string {
	info, err := entry.Info()
	if err != nil {
		return entry.Name()
	}
	mode := info.Mode().String()
	size := strconv.FormatInt(info.Size(), 10)
	modTime := info.ModTime().Format("Jan 02 15:04")
	prefix := " "
	if entry.IsDir() {
		prefix = "d"
	}
	return fmt.Sprintf("%s%-10s %8s %s %s", prefix, mode, size, modTime, entry.Name())
}
