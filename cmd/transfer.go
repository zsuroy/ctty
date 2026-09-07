package cmd

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/sftpconfig"
)

// ParseTransferTarget splits an scp-style "host:path" token.
// Returns host, remotePath, true when the token looks like host:path
// (host has no slash; path may be empty → ".").
func ParseTransferTarget(token string) (host, remotePath string, ok bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", false
	}
	// Windows drive letters: C:\... is local, not host:path
	if len(token) >= 2 && token[1] == ':' && ((token[0] >= 'A' && token[0] <= 'Z') || (token[0] >= 'a' && token[0] <= 'z')) {
		if len(token) == 2 || token[2] == '/' || token[2] == '\\' {
			return "", "", false
		}
	}
	idx := strings.Index(token, ":")
	if idx <= 0 {
		return "", "", false
	}
	host = token[:idx]
	remotePath = token[idx+1:]
	if strings.Contains(host, "/") || strings.Contains(host, `\`) {
		return "", "", false
	}
	if remotePath == "" {
		remotePath = "."
	}
	return host, remotePath, true
}

func progressPrinter(label string) func(transferred, total int64) {
	return func(transferred, total int64) {
		if total <= 0 {
			fmt.Fprintf(os.Stderr, "\r%s %d bytes", label, transferred)
			return
		}
		pct := float64(transferred) * 100 / float64(total)
		fmt.Fprintf(os.Stderr, "\r%s %d/%d (%.0f%%)", label, transferred, total, pct)
	}
}

func finishProgress() {
	fmt.Fprintln(os.Stderr)
}

func connectTransferClient(hostName string) (*sftpconfig.SFTPClient, error) {
	client, err := sftpconfig.ConnectWithPassword(hostName, configFile, "")
	if err != nil {
		return nil, err
	}
	return client, nil
}

var putCmd = &cobra.Command{
	Use:   "put <host> <local> <remote>",
	Short: "Upload a local file or directory to a remote host via SFTP",
	Long: `Upload a local file or directory to a remote host using the built-in SFTP client.

Progress is written to stderr. Directories are transferred recursively.
Exit status is non-zero on failure.

Example:
  ctty put web1 ./deploy.sh /tmp/deploy.sh
  ctty put web1 ./out/ /opt/app/`,
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: hostCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		runPut(args[0], args[1], args[2])
	},
}

var getCmd = &cobra.Command{
	Use:   "get <host> <remote> <local>",
	Short: "Download a remote file or directory via SFTP",
	Long: `Download a remote file or directory using the built-in SFTP client.

Progress is written to stderr. Directories are transferred recursively.
Exit status is non-zero on failure.

Example:
  ctty get web1 /var/log/app.log ./app.log
  ctty get web1 /opt/app/ ./app-backup/`,
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: hostCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		runGet(args[0], args[1], args[2])
	},
}

var scpCmd = &cobra.Command{
	Use:   "scp <src> <dst>",
	Short: "SCP-style file transfer (host:path ↔ local)",
	Long: `Transfer files using scp-like host:path notation via the built-in SFTP client.

Forms:
  ctty scp ./local web1:/remote     # upload (put)
  ctty scp web1:/remote ./local     # download (get)

Progress on stderr; non-zero exit on failure. Directories are recursive.`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		runSCP(args[0], args[1])
	},
}

func hostCompletionFirstArg(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return RootCmd.ValidArgsFunction(cmd, args, toComplete)
}

func runPut(hostName, localPath, remotePath string) {
	client, err := connectTransferClient(hostName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	info, err := os.Stat(localPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// If remote ends with / or local is a dir without explicit remote file name, join basename.
	if info.IsDir() {
		remotePath = strings.TrimRight(remotePath, "/")
	} else if strings.HasSuffix(remotePath, "/") {
		remotePath = path.Join(remotePath, filepath.Base(localPath))
	}

	label := fmt.Sprintf("put %s → %s:%s", localPath, hostName, remotePath)
	progress := progressPrinter(label)
	if err := client.UploadPath(context.Background(), localPath, remotePath, progress); err != nil {
		finishProgress()
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	finishProgress()
	fmt.Printf("Uploaded %s to %s:%s\n", localPath, hostName, remotePath)
}

func runGet(hostName, remotePath, localPath string) {
	client, err := connectTransferClient(hostName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	info, err := client.Stat(remotePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if info.IsDir() {
		localPath = strings.TrimRight(localPath, string(os.PathSeparator))
	} else if strings.HasSuffix(localPath, "/") || strings.HasSuffix(localPath, string(os.PathSeparator)) {
		localPath = filepath.Join(localPath, path.Base(remotePath))
	}

	label := fmt.Sprintf("get %s:%s → %s", hostName, remotePath, localPath)
	progress := progressPrinter(label)
	if err := client.DownloadPath(context.Background(), remotePath, localPath, progress); err != nil {
		finishProgress()
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	finishProgress()
	fmt.Printf("Downloaded %s:%s to %s\n", hostName, remotePath, localPath)
}

func runSCP(src, dst string) {
	srcHost, srcRemote, srcIsRemote := ParseTransferTarget(src)
	dstHost, dstRemote, dstIsRemote := ParseTransferTarget(dst)

	switch {
	case !srcIsRemote && dstIsRemote:
		runPut(dstHost, src, dstRemote)
	case srcIsRemote && !dstIsRemote:
		runGet(srcHost, srcRemote, dst)
	case srcIsRemote && dstIsRemote:
		fmt.Fprintf(os.Stderr, "Error: remote-to-remote copy is not supported\n")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "Error: one of src or dst must be host:path\n")
		os.Exit(1)
	}
}

func init() {
	RootCmd.AddCommand(putCmd)
	RootCmd.AddCommand(getCmd)
	RootCmd.AddCommand(scpCmd)
}
