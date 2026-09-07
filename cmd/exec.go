package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"
)

var (
	execTags        string
	execHosts       string
	execConcurrency int
	execFormat      string
)

type execResult struct {
	Host     string `json:"host"`
	OK       bool   `json:"ok"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Error    string `json:"error,omitempty"`
}

var execCmd = &cobra.Command{
	Use:   "exec [--tags tag1,tag2 | --hosts a,b] -- <command...>",
	Short: "Run a remote command on multiple SSH hosts",
	Long: `Execute a remote command on multiple hosts selected by tags and/or host names.

Selection (at least one required):
  --tags prod,web   hosts that have ANY of the listed tags
  --hosts a,b,c     explicit Host aliases

Default concurrency is 8 (--concurrency). Human-readable output by default;
--format json emits an array of {host,ok,exit_code,stdout,stderr}.

Aggregate exit status is 0 if and only if every host succeeded.

Examples:
  ctty exec --tags prod -- uptime
  ctty exec --hosts web1,web2 -- df -h
  ctty exec --tags api --hosts bastion --format json -- systemctl is-active nginx`,
	Args: cobra.ArbitraryArgs,
	Run:  runBatchExec,
}

func runBatchExec(cmd *cobra.Command, args []string) {
	remoteCmd := args
	if cmd.ArgsLenAtDash() >= 0 {
		remoteCmd = args[cmd.ArgsLenAtDash():]
	}
	if len(remoteCmd) == 0 {
		fmt.Fprintf(os.Stderr, "Error: remote command required (use -- before the command)\n")
		os.Exit(1)
	}

	tagList := parseTagsCSV(execTags)
	hostList := splitCSV(execHosts)
	if len(tagList) == 0 && len(hostList) == 0 {
		fmt.Fprintf(os.Stderr, "Error: specify --tags and/or --hosts\n")
		os.Exit(1)
	}

	hosts, err := loadSSHHosts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading SSH config: %v\n", err)
		os.Exit(1)
	}

	selected := selectHosts(hosts, tagList, hostList)
	if len(selected) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no hosts matched the selection\n")
		os.Exit(1)
	}

	if execConcurrency < 1 {
		execConcurrency = 8
	}

	results := runOnHosts(selected, remoteCmd, execConcurrency)

	allOK := true
	for _, r := range results {
		if !r.OK {
			allOK = false
			break
		}
	}

	if execFormat == "json" {
		b, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(b))
	} else {
		for _, r := range results {
			status := "OK"
			if !r.OK {
				status = fmt.Sprintf("FAIL (exit %d)", r.ExitCode)
				if r.Error != "" {
					status = fmt.Sprintf("FAIL (%s)", r.Error)
				}
			}
			fmt.Printf("=== %s: %s ===\n", r.Host, status)
			if r.Stdout != "" {
				fmt.Print(r.Stdout)
				if !strings.HasSuffix(r.Stdout, "\n") {
					fmt.Println()
				}
			}
			if r.Stderr != "" {
				fmt.Fprint(os.Stderr, r.Stderr)
				if !strings.HasSuffix(r.Stderr, "\n") {
					fmt.Fprintln(os.Stderr)
				}
			}
		}
	}

	if !allOK {
		os.Exit(1)
	}
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loadSSHHosts() ([]config.SSHHost, error) {
	if configFile != "" {
		return config.ParseSSHConfigFile(configFile)
	}
	return config.ParseSSHConfig()
}

func selectHosts(hosts []config.SSHHost, tags, names []string) []config.SSHHost {
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	var selected []config.SSHHost
	seen := make(map[string]bool)

	for _, h := range hosts {
		match := false
		if len(names) > 0 && nameSet[h.Name] {
			match = true
		}
		if len(tags) > 0 && config.HostHasAnyTag(h.Tags, tags) {
			match = true
		}
		// If only tags were given, names empty — tag match is enough.
		// If only names — name match.
		// If both — OR semantics (either).
		if !match {
			continue
		}
		if seen[h.Name] {
			continue
		}
		seen[h.Name] = true
		selected = append(selected, h)
	}
	return selected
}

func runOnHosts(hosts []config.SSHHost, remoteCmd []string, concurrency int) []execResult {
	results := make([]execResult, len(hosts))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, h := range hosts {
		wg.Add(1)
		go func(i int, host config.SSHHost) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = runSSHCommand(host.Name, remoteCmd)
		}(i, h)
	}
	wg.Wait()
	return results
}

// runSSHCommand runs a remote command via the system ssh client (same path as
// ctty <host> <cmd>), capturing stdout/stderr instead of replacing the process.
func runSSHCommand(hostName string, remoteCmd []string) execResult {
	res := execResult{Host: hostName, ExitCode: 1}

	var args []string
	if configFile != "" {
		args = append(args, "-F", configFile)
	}
	args = append(args, hostName)
	args = append(args, remoteCmd...)

	sshCmd := exec.Command("ssh", args...)
	env := os.Environ()
	if pass, ok := credential.GetPassword(hostName); ok && pass != "" {
		selfPath, err := os.Executable()
		if err == nil {
			env = append(env,
				"SSH_ASKPASS="+selfPath,
				"SSH_ASKPASS_REQUIRE=force",
				"CTTY_ASKPASS_TOKEN="+base64.StdEncoding.EncodeToString([]byte(pass)),
				"DISPLAY=ctty:0",
			)
		}
	}
	sshCmd.Env = env

	var stdout, stderr bytes.Buffer
	sshCmd.Stdout = &stdout
	sshCmd.Stderr = &stderr

	err := sshCmd.Run()
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()

	if err == nil {
		res.OK = true
		res.ExitCode = 0
		return res
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			res.ExitCode = status.ExitStatus()
		}
		return res
	}
	res.Error = err.Error()
	return res
}

func init() {
	RootCmd.AddCommand(execCmd)
	execCmd.Flags().StringVar(&execTags, "tags", "", "Comma-separated tags; hosts matching ANY tag are selected")
	execCmd.Flags().StringVar(&execHosts, "hosts", "", "Comma-separated Host aliases")
	execCmd.Flags().IntVar(&execConcurrency, "concurrency", 8, "Max parallel SSH sessions")
	execCmd.Flags().StringVar(&execFormat, "format", "", "Output format: json for machine-readable array")
}
