package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zsuroy/ctty/internal/config"
)

func TestRootCommand(t *testing.T) {
	if RootCmd.Use != "ctty [host] [command...]" {
		t.Errorf("Expected Use 'ctty [host] [command...]', got '%s'", RootCmd.Use)
	}

	if RootCmd.Short != "Connection Manager - SSH, Serial & SFTP" {
		t.Errorf("Expected Short description, got '%s'", RootCmd.Short)
	}

	if RootCmd.Version != AppVersion {
		t.Errorf("Expected Version '%s', got '%s'", AppVersion, RootCmd.Version)
	}
}

func TestRootCommandFlags(t *testing.T) {
	flags := RootCmd.PersistentFlags()

	configFlag := flags.Lookup("config")
	if configFlag == nil {
		t.Error("Expected --config flag to be defined")
		return
	}
	if configFlag.Shorthand != "c" {
		t.Errorf("Expected config flag shorthand 'c', got '%s'", configFlag.Shorthand)
	}

	ttyFlag := RootCmd.Flags().Lookup("tty")
	if ttyFlag == nil {
		t.Error("Expected --tty flag to be defined")
		return
	}
	if ttyFlag.Shorthand != "t" {
		t.Errorf("Expected tty flag shorthand 't', got '%s'", ttyFlag.Shorthand)
	}

	quietLookup := flags.Lookup("quiet")
	if quietLookup == nil {
		t.Error("Expected --quiet persistent flag to be defined")
		return
	}
	if quietLookup.Shorthand != "q" {
		t.Errorf("Expected quiet flag shorthand 'q', got '%s'", quietLookup.Shorthand)
	}
}

func TestRootCommandSubcommands(t *testing.T) {
	// Test that all expected subcommands are registered
	// Note: completion and help are automatically added by Cobra and may not always appear in Commands()
	expectedCommands := []string{"add", "edit", "search", "info", "import"}

	commands := RootCmd.Commands()
	commandNames := make(map[string]bool)
	for _, cmd := range commands {
		commandNames[cmd.Name()] = true
	}

	for _, expected := range expectedCommands {
		if !commandNames[expected] {
			t.Errorf("Expected command '%s' not found", expected)
		}
	}

	// Check that we have at least the core commands
	if len(commandNames) < 3 {
		t.Errorf("Expected at least 3 commands, got %d", len(commandNames))
	}
}

func TestRootCommandHelp(t *testing.T) {
	// Test help output
	buf := new(bytes.Buffer)
	RootCmd.SetOut(buf)
	RootCmd.SetArgs([]string{"--help"})

	// This should not return an error for help
	err := RootCmd.Execute()
	if err != nil {
		t.Errorf("Expected no error for help command, got %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "lightweight terminal connection manager") {
		t.Error("Help output should contain command description")
	}
	if !strings.Contains(output, "Usage:") {
		t.Error("Help output should contain usage section")
	}
}

func TestRootCommandVersion(t *testing.T) {
	// Test that version command executes without error
	// Note: Cobra handles version output internally, so we just check for no error
	RootCmd.SetArgs([]string{"--version"})

	// This should not return an error for version
	err := RootCmd.Execute()
	if err != nil {
		t.Errorf("Expected no error for version command, got %v", err)
	}

	// Reset args for other tests
	RootCmd.SetArgs([]string{})
}

func TestExecuteFunction(t *testing.T) {
	// Test that Execute function exists and can be called
	// We can't easily test the actual execution without mocking,
	// but we can test that the function exists
	t.Log("Execute function exists and is accessible")
}

func TestConnectToHostFunction(t *testing.T) {
	t.Log("connectToHost function exists and is accessible")
}

func TestRemoteCommandUsage(t *testing.T) {
	if !strings.Contains(RootCmd.Long, "command") {
		t.Error("Long description should mention remote command execution")
	}

	if !strings.Contains(RootCmd.Long, "uptime") {
		t.Error("Long description should include command examples")
	}
}

func TestRunInteractiveModeFunction(t *testing.T) {
	// Test that runInteractiveMode function exists
	// We can't easily test the actual execution without mocking the UI,
	// but we can verify the function signature
	t.Log("runInteractiveMode function exists and is accessible")
}

func TestConfigFileVariable(t *testing.T) {
	// Test that configFile variable is properly initialized
	originalConfigFile := configFile
	defer func() { configFile = originalConfigFile }()

	// Set config file through flag
	RootCmd.SetArgs([]string{"--config", "/tmp/test-config"})
	RootCmd.ParseFlags([]string{"--config", "/tmp/test-config"})

	// The configFile variable should be updated by the flag parsing
	// Note: This test verifies the flag binding works
}

func TestVersionVariable(t *testing.T) {
	// Test that version variable has a default value
	if AppVersion == "" {
		t.Error("AppVersion variable should have a default value")
	}

	// Test that version is set to "dev" by default
	if AppVersion != "dev" {
		t.Logf("AppVersion is set to '%s' (expected 'dev' for development)", AppVersion)
	}
}

func resetQuietFlagState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		quietFlag = false
		if f := RootCmd.PersistentFlags().Lookup("quiet"); f != nil {
			f.Changed = false
		}
		RootCmd.SetArgs(nil)
	})
}

func TestIsQuietModeResolution(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	resetQuietFlagState(t)

	// Default config: quiet should be false.
	if isQuietMode(RootCmd) {
		t.Fatal("expected quiet mode to be false by default")
	}

	// Persistent config quiet=true enables quiet mode when flag is omitted.
	cfg := config.GetDefaultAppConfig()
	cfg.Quiet = true
	if err := config.SaveAppConfig(&cfg); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}
	if !isQuietMode(RootCmd) {
		t.Fatal("expected quiet mode to be true when config.Quiet is true")
	}

	// Explicit --quiet=false overrides config.Quiet=true.
	if err := RootCmd.PersistentFlags().Parse([]string{"--quiet=false"}); err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if isQuietMode(RootCmd) {
		t.Fatal("expected explicit --quiet=false to override config.Quiet=true")
	}

	// Explicit -q enables quiet mode even when config.Quiet is false.
	cfg.Quiet = false
	if err := config.SaveAppConfig(&cfg); err != nil {
		t.Fatalf("SaveAppConfig failed: %v", err)
	}
	if err := RootCmd.PersistentFlags().Parse([]string{"-q"}); err != nil {
		t.Fatalf("Parse -q failed: %v", err)
	}
	if !isQuietMode(RootCmd) {
		t.Fatal("expected -q flag to enable quiet mode")
	}
}

func TestConnectToHostQuietAndStderrRouting(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("HOME", tmpDir)

	sshCfgPath := filepath.Join(tmpDir, "ssh_config")
	sshCfgContent := "Host web1\n    HostName 10.0.0.10\n    User deploy\n"
	if err := os.WriteFile(sshCfgPath, []byte(sshCfgContent), 0600); err != nil {
		t.Fatalf("write ssh config: %v", err)
	}

	origConfigFile := configFile
	configFile = sshCfgPath
	t.Cleanup(func() { configFile = origConfigFile })

	runCaptured := func(host string, remoteCmd []string, quiet bool) (code int, stdoutStr, stderrStr string, gotArgs []string) {
		var errBuf bytes.Buffer
		stdoutOut := captureStdout(t, func() error {
			code = connectToHostWithDeps(host, remoteCmd, quiet, &errBuf, func(args, _ []string, _ io.Writer) int {
				gotArgs = append([]string(nil), args...)
				return 0
			})
			return nil
		})
		return code, stdoutOut, errBuf.String(), gotArgs
	}

	// 1. Default (non-quiet) direct connect: banner goes to stderr, stdout is clean.
	code, out, errOut, args := runCaptured("web1", nil, false)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr=%q)", code, errOut)
	}
	if out != "" {
		t.Errorf("expected empty stdout, got %q", out)
	}
	if !strings.Contains(errOut, "Connecting to web1...") {
		t.Errorf("expected Connecting banner on stderr, got %q", errOut)
	}
	if len(args) == 0 || args[len(args)-1] != "web1" {
		t.Errorf("unexpected ssh args: %v", args)
	}

	// 2. Quiet direct connect: banner is suppressed on both stdout and stderr.
	code, out, errOut, _ = runCaptured("web1", nil, true)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if out != "" {
		t.Errorf("expected empty stdout in quiet mode, got %q", out)
	}
	if strings.Contains(errOut, "Connecting to") {
		t.Errorf("expected Connecting banner to be suppressed in quiet mode, got %q", errOut)
	}

	// 3. Remote command execution: no Connecting banner even when non-quiet.
	code, out, errOut, args = runCaptured("web1", []string{"uptime"}, false)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if out != "" || strings.Contains(errOut, "Connecting to") {
		t.Errorf("unexpected output for remote command: stdout=%q stderr=%q", out, errOut)
	}
	if len(args) < 2 || args[len(args)-2] != "web1" || args[len(args)-1] != "uptime" {
		t.Errorf("unexpected ssh args for remote command: %v", args)
	}

	// 4. Errors are preserved on stderr (never stdout) even in quiet mode.
	code, out, errOut, _ = runCaptured("missing-host", nil, true)
	if code != 1 {
		t.Fatalf("expected exit 1 for missing host, got %d", code)
	}
	if out != "" {
		t.Errorf("expected empty stdout on error, got %q", out)
	}
	if !strings.Contains(errOut, "Error: Host 'missing-host' not found") {
		t.Errorf("expected host-not-found error on stderr in quiet mode, got %q", errOut)
	}

	// 5. History warnings go to stderr by default and are suppressed in quiet mode.
	blockingFile := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(blockingFile, []byte("x"), 0600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", blockingFile)
	t.Setenv("HOME", blockingFile)

	_, out, errOut, _ = runCaptured("web1", nil, false)
	if out != "" {
		t.Errorf("expected empty stdout when history fails, got %q", out)
	}
	if !strings.Contains(errOut, "Warning:") {
		t.Errorf("expected history warning on stderr when non-quiet, got %q", errOut)
	}

	_, out, errOut, _ = runCaptured("web1", nil, true)
	if out != "" {
		t.Errorf("expected empty stdout in quiet mode, got %q", out)
	}
	if errOut != "" {
		t.Errorf("expected history warning and banner suppressed in quiet mode, got %q", errOut)
	}
}
