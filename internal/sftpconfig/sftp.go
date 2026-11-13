package sftpconfig

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SFTPClient wraps an SSH connection + one reused SFTP session.
// All remote ops share that session and are serialized with mu so a
// directory refresh cannot overlap an in-flight upload/download.
type SFTPClient struct {
	sshClient  *ssh.Client
	sftp       *sftpWrapper
	mu         sync.Mutex
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

// effectiveHost holds the connection settings resolved for an SSH host.
// IdentityFiles is ordered: explicit/merged config entries first (as
// OpenSSH resolves them, including Host * defaults), followed by the
// standard default key paths as a fallback.
type effectiveHost struct {
	name          string
	hostname      string
	user          string
	port          string
	identityAgent string
	identityFiles []string
}

// runSSHG shells out to ssh -G so SFTP honors the same effective config as
// the system ssh client (Host * defaults, Include files, multiple
// IdentityFile entries). It is a var for tests.
var runSSHG = func(hostName, configFile string) ([]byte, error) {
	args := []string{"-G"}
	if configFile != "" {
		args = append(args, "-F", configFile)
	}
	args = append(args, hostName)
	return exec.Command("ssh", args...).Output()
}

// resolveEffectiveHost returns connection settings for hostName. When the
// system ssh client is available it uses ssh -G for full OpenSSH-compatible
// resolution (fixes issue #49: IdentityFile from Host * / merged config was
// invisible to ctty's parser-only SFTP path). On failure it falls back to
// ctty's parsed config so existing behavior is preserved.
func resolveEffectiveHost(hostName, configFile string) effectiveHost {
	h := effectiveHost{name: hostName, hostname: hostName, port: "22"}
	// Baseline from ctty's parser.
	if parsed, err := lookupParsedHost(hostName, configFile); err == nil && parsed != nil {
		if parsed.Hostname != "" {
			h.hostname = parsed.Hostname
		}
		h.user = parsed.User
		if parsed.Port != "" {
			h.port = parsed.Port
		}
		h.identityAgent = parsed.IdentityAgent
		if parsed.Identity != "" {
			h.identityFiles = append(h.identityFiles, parsed.Identity)
		}
	}
	if out, err := runSSHG(hostName, configFile); err == nil {
		if g := parseSSHGOutput(string(out)); g.hostname != "" || g.user != "" || len(g.identityFiles) > 0 || g.identityAgent != "" || g.port != "" {
			if g.hostname != "" {
				h.hostname = g.hostname
			}
			if g.user != "" {
				h.user = g.user
			}
			if g.port != "" {
				h.port = g.port
			}
			// ssh -G reports the effective identityagent and the full
			// ordered identityfile list (including defaults).
			if g.identityAgent != "" || g.identityAgentSet {
				h.identityAgent = g.identityAgent
			}
			if len(g.identityFiles) > 0 {
				h.identityFiles = g.identityFiles
			}
		}
	}
	return h
}

// resolveFrom builds an effectiveHost from an already-parsed SSHHost (may be nil)
// and ssh -G output. It exists to avoid duplicating lookup in Connect paths.
func resolveFrom(parsed *config.SSHHost, hostName, configFile string) effectiveHost {
	h := effectiveHost{name: hostName, hostname: hostName, port: "22"}
	if parsed != nil {
		if parsed.Hostname != "" {
			h.hostname = parsed.Hostname
		}
		h.user = parsed.User
		if parsed.Port != "" {
			h.port = parsed.Port
		}
		h.identityAgent = parsed.IdentityAgent
		if parsed.Identity != "" {
			h.identityFiles = append(h.identityFiles, parsed.Identity)
		}
	}
	if out, err := runSSHG(hostName, configFile); err == nil {
		if g := parseSSHGOutput(string(out)); g.hostname != "" || g.user != "" || len(g.identityFiles) > 0 || g.identityAgent != "" || g.port != "" || g.identityAgentSet {
			if g.hostname != "" {
				h.hostname = g.hostname
			}
			if g.user != "" {
				h.user = g.user
			}
			if g.port != "" {
				h.port = g.port
			}
			if g.identityAgent != "" || g.identityAgentSet {
				h.identityAgent = g.identityAgent
			}
			if len(g.identityFiles) > 0 {
				h.identityFiles = g.identityFiles
			}
		}
	}
	return h
}

type sshgResult struct {
	hostname         string
	user             string
	port             string
	identityAgent    string
	identityAgentSet bool
	identityFiles    []string
}

func parseSSHGOutput(out string) sshgResult {
	var r sshgResult
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "hostname":
			r.hostname = val
		case "user":
			r.user = val
		case "port":
			r.port = val
		case "identityagent":
			r.identityAgent = val
			r.identityAgentSet = true
		case "identityfile":
			if val != "" {
				r.identityFiles = append(r.identityFiles, val)
			}
		}
	}
	return r
}

func lookupParsedHost(hostName, configFile string) (*config.SSHHost, error) {
	var hosts []config.SSHHost
	var err error
	if configFile != "" {
		hosts, err = config.ParseSSHConfigFile(configFile)
	} else {
		hosts, err = config.ParseSSHConfig()
	}
	if err != nil {
		return nil, err
	}
	for i := range hosts {
		if hosts[i].Name == hostName {
			return &hosts[i], nil
		}
	}
	return nil, fmt.Errorf("host '%s' not found in SSH config", hostName)
}

// PassphraseRequiredError is returned when an identity file is encrypted
// and no passphrase was provided to decrypt it.
type PassphraseRequiredError struct {
	Path string
}

func (e *PassphraseRequiredError) Error() string {
	return fmt.Sprintf("private key %s is encrypted and requires a passphrase", e.Path)
}

// IsPassphraseRequired reports whether err is or wraps a PassphraseRequiredError.
func IsPassphraseRequired(err error) bool {
	var e *PassphraseRequiredError
	return errors.As(err, &e)
}

// Connect establishes an SSH connection to the given host and returns an SFTPClient.
// It resolves effective SSH settings (ssh -G when available) so IdentityFile
// entries from Host * / merged config work the same as interactive ssh.
func Connect(hostName string, configFile string) (*SFTPClient, error) {
	return ConnectWithAuth(hostName, configFile, "", "")
}

// ConnectWithPassword connects to the host using publickey + password auth.
// password can be empty to try publickey only, or a password to try
// keyboard-interactive + password auth as fallback.
func ConnectWithPassword(hostName string, configFile string, password string) (*SFTPClient, error) {
	return ConnectWithAuth(hostName, configFile, password, "")
}

// ConnectWithAuth is like ConnectWithPassword but also accepts a passphrase
// for encrypted identity files. passphrase may be empty when the key is
// unencrypted or will be provided by an SSH agent.
func ConnectWithAuth(hostName, configFile, password, passphrase string) (*SFTPClient, error) {
	parsed, parsedErr := lookupParsedHost(hostName, configFile)
	_, sshgErr := runSSHG(hostName, configFile)
	if parsedErr != nil && sshgErr != nil {
		return nil, fmt.Errorf("host '%s' not found in SSH config", hostName)
	}
	eff := resolveFrom(parsed, hostName, configFile)

	sshConfig, err := buildSSHConfig(hostName, eff, passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to build SSH config: %w", err)
	}

	// If password not passed explicitly, check credential store
	if password == "" {
		if saved, ok := credential.GetPassword(hostName); ok && saved != "" {
			password = saved
		}
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

	addr := net.JoinHostPort(eff.hostname, eff.port)

	sshClient, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	return &SFTPClient{
		sshClient:  sshClient,
		host:       config.SSHHost{Name: hostName, Hostname: eff.hostname, User: eff.user, Port: eff.port},
		configFile: configFile,
	}, nil
}

// buildSSHConfig creates an *ssh.ClientConfig from resolved host settings.
func buildSSHConfig(hostName string, eff effectiveHost, passphrase string) (*ssh.ClientConfig, error) {
	authMethods, err := getAuthMethods(eff, passphrase)
	if err != nil {
		return nil, err
	}

	knownHostsPath, err := defaultKnownHostsPath()
	if err != nil {
		return nil, fmt.Errorf("failed to prepare host key verification: %w", err)
	}
	knownHostsCallback, err := acceptNewHostKeyCallback(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare host key verification: %w", err)
	}

	dialAddr := net.JoinHostPort(eff.hostname, eff.port)
	algoAddrs := []string{dialAddr}
	// Also consider the Host alias: OpenSSH may have recorded keys under either name.
	if hostName != "" && hostName != eff.hostname {
		algoAddrs = append(algoAddrs, net.JoinHostPort(hostName, eff.port))
	}

	cfg := &ssh.ClientConfig{
		User:              getUser(eff),
		Auth:              authMethods,
		HostKeyCallback:   knownHostsCallback,
		HostKeyAlgorithms: hostKeyAlgorithms(knownHostsPath, algoAddrs...),
		Timeout:           15 * time.Second,
	}

	return cfg, nil
}

func getUser(eff effectiveHost) string {
	if eff.user != "" {
		return eff.user
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "root"
}

// agentAddr returns the SSH agent endpoint to dial, or "" if the agent is
// disabled. IdentityAgent takes precedence over SSH_AUTH_SOCK (OpenSSH).
// On Windows, when both are unset, this falls back to the OpenSSH named pipe.
func agentAddr(identityAgent string) string {
	if identityAgent == "none" {
		return ""
	}
	socket := identityAgent
	if socket == "" || socket == "SSH_AUTH_SOCK" {
		socket = os.Getenv("SSH_AUTH_SOCK")
	} else {
		socket = expandPath(socket)
	}
	if socket == "" {
		socket = defaultSSHAgentAddr()
	}
	return socket
}

// getAuthMethods returns SSH auth methods from the SSH agent and identity
// files. eff.identityFiles is the effective ordered list (from ssh -G when
// available, so Host * defaults and multiple IdentityFile entries are honored).
//
// Encrypted keys require passphrase; missing files are skipped (ssh -G lists
// default key paths even when they do not exist). Other load/parse failures
// are returned when no usable method was found, so callers can show a real
// error instead of falling through to a server-password prompt.
func getAuthMethods(eff effectiveHost, passphrase string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if socket := agentAddr(eff.identityAgent); socket != "" {
		conn, err := dialSSHAgent(socket)
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

	// Try identity files from effective config (deduplicated, in order).
	seen := make(map[string]bool)
	var loadErrs []error
	var passErr *PassphraseRequiredError
	addKey := func(path string) {
		keyPath := expandPath(path)
		if keyPath == "" || seen[keyPath] {
			return
		}
		seen[keyPath] = true
		signer, err := loadIdentitySigner(keyPath, passphrase)
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			var pe *PassphraseRequiredError
			if errors.As(err, &pe) {
				if passErr == nil {
					passErr = pe
				}
				return
			}
			loadErrs = append(loadErrs, err)
			return
		}
		if signer != nil {
			methods = append(methods, ssh.PublicKeys(signer))
		}
	}
	for _, id := range eff.identityFiles {
		addKey(id)
	}
	// Fallback defaults when no effective identity list was resolved.
	if len(eff.identityFiles) == 0 {
		if homeDir, err := os.UserHomeDir(); err == nil {
			for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa", "id_dsa"} {
				addKey(filepath.Join(homeDir, ".ssh", name))
			}
		}
	}

	if len(methods) > 0 {
		return methods, nil
	}
	if passErr != nil {
		return nil, passErr
	}
	if len(loadErrs) > 0 {
		return nil, errors.Join(loadErrs...)
	}
	// Don't return error if no methods found — password auth might be used
	return methods, nil
}

func loadIdentitySigner(keyPath, passphrase string) (ssh.Signer, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err == nil {
		return signer, nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, fmt.Errorf("parse identity file %s: %w", keyPath, err)
	}
	if passphrase == "" {
		return nil, &PassphraseRequiredError{Path: keyPath}
	}
	signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
	if err != nil {
		return nil, fmt.Errorf("decrypt identity file %s: %w", keyPath, err)
	}
	return signer, nil
}

var knownHostsMu sync.Mutex

func defaultKnownHostsPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".ssh", "known_hosts"), nil
}

// getHostKeyCallback returns an accept-new known_hosts callback. Unknown keys
// are recorded on first use; a changed key is rejected on later connections.
func getHostKeyCallback() (ssh.HostKeyCallback, error) {
	knownHostsPath, err := defaultKnownHostsPath()
	if err != nil {
		return nil, err
	}
	return acceptNewHostKeyCallback(knownHostsPath)
}

var (
	probeHostKeyOnce sync.Once
	probeHostKey     ssh.PublicKey
)

func hostKeyProbeKey() ssh.PublicKey {
	probeHostKeyOnce.Do(func() {
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			panic(err)
		}
		key, err := ssh.NewPublicKey(pub)
		if err != nil {
			panic(err)
		}
		probeHostKey = key
	})
	return probeHostKey
}

// hostKeyAlgorithms returns preferred host key algorithms for dial targets that
// already appear in known_hosts. This mirrors OpenSSH: prefer key types we
// already trust so the server does not present a different type that would
// look like a mismatch (golang/go#29286).
func hostKeyAlgorithms(knownHostsPath string, addrs ...string) []string {
	verify, err := knownHostsVerifier(knownHostsPath)
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var algos []string
	add := func(algo string) {
		if _, ok := seen[algo]; ok {
			return
		}
		seen[algo] = struct{}{}
		algos = append(algos, algo)
	}
	probe := hostKeyProbeKey()
	for _, addr := range addrs {
		if addr == "" {
			continue
		}
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		remote := &net.TCPAddr{IP: net.IPv4zero, Port: 0}
		if p, err := strconv.Atoi(port); err == nil {
			remote.Port = p
		}
		err = verify(addr, remote, probe)
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) || len(keyErr.Want) == 0 {
			continue
		}
		for _, known := range keyErr.Want {
			typ := known.Key.Type()
			if typ == ssh.KeyAlgoRSA {
				// RSA host keys may be signed with SHA-2 algorithms.
				add(ssh.KeyAlgoRSASHA512)
				add(ssh.KeyAlgoRSASHA256)
			}
			add(typ)
		}
	}
	return algos
}

func acceptNewHostKeyCallback(knownHostsPath string) (ssh.HostKeyCallback, error) {
	// Read-only setup: do not create, append, or chmod here. Already-trusted
	// hosts must verify even when ~/.ssh is not writable.
	verify, err := knownHostsVerifier(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("parse known_hosts: %w", err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := verify(hostname, remote, key); err == nil {
			return nil
		} else if !isUnknownHostKey(err) {
			return fmt.Errorf("verify host key for %s: %w", hostname, err)
		}

		knownHostsMu.Lock()
		defer knownHostsMu.Unlock()

		// Re-read while holding the append lock. Another connection may have
		// recorded this host after this callback captured its initial snapshot.
		current, err := knownHostsVerifier(knownHostsPath)
		if err != nil {
			return fmt.Errorf("reload known_hosts: %w", err)
		}
		if err := current(hostname, remote, key); err == nil {
			return nil
		} else if !isUnknownHostKey(err) {
			return fmt.Errorf("verify host key for %s: %w", hostname, err)
		}

		return appendKnownHost(knownHostsPath, hostname, key)
	}, nil
}

// knownHostsVerifier returns a callback that checks known_hosts without writing.
// A missing file is treated as "no known keys" so accept-new can append later.
func knownHostsVerifier(knownHostsPath string) (ssh.HostKeyCallback, error) {
	_, err := os.Stat(knownHostsPath)
	if errors.Is(err, fs.ErrNotExist) {
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return &knownhosts.KeyError{}
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return knownhosts.New(knownHostsPath)
}

func appendKnownHost(knownHostsPath, hostname string, key ssh.PublicKey) error {
	if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0o700); err != nil {
		return fmt.Errorf("create known_hosts directory: %w", err)
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	file, err := os.OpenFile(knownHostsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("append known_hosts: %w", err)
	}
	if _, err := fmt.Fprintln(file, line); err != nil {
		_ = file.Close()
		return fmt.Errorf("write known_hosts: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync known_hosts: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close known_hosts: %w", err)
	}
	// Best-effort: do not fail TOFU solely because chmod is denied after a successful write.
	_ = os.Chmod(knownHostsPath, 0o600)
	return nil
}

func isUnknownHostKey(err error) bool {
	var keyErr *knownhosts.KeyError
	return errors.As(err, &keyErr) && len(keyErr.Want) == 0
}

func expandPath(path string) string {
	path = strings.Trim(path, `"'`)
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

// Close closes the SFTP session and the underlying SSH connection.
func (c *SFTPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	if c.sftp != nil {
		err = c.sftp.Close()
		c.sftp = nil
	}
	if c.sshClient != nil {
		if e := c.sshClient.Close(); e != nil && err == nil {
			err = e
		}
		c.sshClient = nil
	}
	return err
}

// HostName returns the name of the connected host.
func (c *SFTPClient) HostName() string {
	return c.host.Name
}

// ----- SFTP operations using the scp/sftp subsystem over the SSH connection -----

// runSFTPCommand runs a command on the remote host via the SSH session
// and returns its output. Used as a lightweight alternative to the SFTP subsystem.
// Actually, we'll use the SFTP subsystem directly via github.com/pkg/sftp.

func (c *SFTPClient) getSession() (*sftpWrapper, error) {
	if c.sftp != nil {
		return c.sftp, nil
	}
	sc, err := sftpNewClient(c.sshClient)
	if err != nil {
		return nil, fmt.Errorf("failed to start SFTP subsystem: %w", err)
	}
	c.sftp = sc
	return sc, nil
}

func (c *SFTPClient) withSession(fn func(*sftpWrapper) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	sc, err := c.getSession()
	if err != nil {
		return err
	}
	return fn(sc)
}

// ListDir lists the contents of a remote directory.
func (c *SFTPClient) ListDir(path string) ([]RemoteEntry, error) {
	var result []RemoteEntry
	err := c.withSession(func(sc *sftpWrapper) error {
		entries, err := sc.ReadDir(path)
		if err != nil {
			return fmt.Errorf("failed to read directory %s: %w", path, err)
		}
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
		return nil
	})
	return result, err
}

// Download downloads a remote file to a local path.
func (c *SFTPClient) Download(remotePath, localPath string) error {
	return c.DownloadWithProgress(remotePath, localPath, nil)
}

// DownloadWithProgress downloads a remote file with a progress callback.
// The callback receives bytes downloaded and total size.
func (c *SFTPClient) DownloadWithProgress(remotePath, localPath string, progress func(downloaded, total int64)) error {
	return c.DownloadWithProgressCtx(context.Background(), remotePath, localPath, progress)
}

// DownloadWithProgressCtx is like DownloadWithProgress but accepts a context for cancellation.
func (c *SFTPClient) DownloadWithProgressCtx(ctx context.Context, remotePath, localPath string, progress func(downloaded, total int64)) error {
	return c.withSession(func(sc *sftpWrapper) error {
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
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
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
	})
}

// UploadWithProgress uploads a local file with a progress callback.
func (c *SFTPClient) UploadWithProgress(localPath, remotePath string, progress func(uploaded, total int64)) error {
	return c.UploadWithProgressCtx(context.Background(), localPath, remotePath, progress)
}

// UploadWithProgressCtx is like UploadWithProgress but accepts a context for cancellation.
func (c *SFTPClient) UploadWithProgressCtx(ctx context.Context, localPath, remotePath string, progress func(uploaded, total int64)) error {
	return c.withSession(func(sc *sftpWrapper) error {
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
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
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
	})
}

// Upload uploads a local file to a remote path.
func (c *SFTPClient) Upload(localPath, remotePath string) error {
	return c.UploadWithProgress(localPath, remotePath, nil)
}

// Stat returns info about a remote path.
func (c *SFTPClient) Stat(path string) (os.FileInfo, error) {
	var info os.FileInfo
	err := c.withSession(func(sc *sftpWrapper) error {
		var err error
		info, err = sc.Stat(path)
		return err
	})
	return info, err
}

// Mkdir creates a directory on the remote host.
func (c *SFTPClient) Mkdir(path string) error {
	return c.withSession(func(sc *sftpWrapper) error {
		return sc.Mkdir(path)
	})
}

// Remove deletes a file on the remote host.
func (c *SFTPClient) Remove(path string) error {
	return c.withSession(func(sc *sftpWrapper) error {
		return sc.Remove(path)
	})
}

// RemoveAll deletes a remote file or directory recursively.
func (c *SFTPClient) RemoveAll(path string) error {
	return c.withSession(func(sc *sftpWrapper) error {
		return sc.RemoveAll(path)
	})
}

// Rename moves a remote file or directory to a new path.
func (c *SFTPClient) Rename(oldPath, newPath string) error {
	return c.withSession(func(sc *sftpWrapper) error {
		return sc.Rename(oldPath, newPath)
	})
}

// RealPath returns the canonical absolute path.
func (c *SFTPClient) RealPath(path string) (string, error) {
	var resolved string
	err := c.withSession(func(sc *sftpWrapper) error {
		var err error
		resolved, err = sc.RealPath(path)
		return err
	})
	return resolved, err
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
