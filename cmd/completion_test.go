package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCompletionCommand(t *testing.T) {
	if completionCmd.Use != "completion [bash|zsh|fish|powershell]" {
		t.Errorf("Expected Use 'completion [bash|zsh|fish|powershell]', got '%s'", completionCmd.Use)
	}

	if completionCmd.Short != "Generate shell completion script" {
		t.Errorf("Expected Short description, got '%s'", completionCmd.Short)
	}
}

func TestCompletionCommandValidArgs(t *testing.T) {
	expected := []string{"bash", "zsh", "fish", "powershell"}

	if len(completionCmd.ValidArgs) != len(expected) {
		t.Errorf("Expected %d valid args, got %d", len(expected), len(completionCmd.ValidArgs))
	}

	for i, arg := range expected {
		if completionCmd.ValidArgs[i] != arg {
			t.Errorf("Expected ValidArgs[%d] to be '%s', got '%s'", i, arg, completionCmd.ValidArgs[i])
		}
	}
}

func TestCompletionCommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range RootCmd.Commands() {
		if cmd.Name() == "completion" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected 'completion' command to be registered")
	}
}

func TestCompletionBashOutput(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"completion", "bash"})
	err := RootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Errorf("Expected no error for bash completion, got %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "bash completion") || !strings.Contains(output, "ctty") {
		t.Error("Bash completion output should contain bash completion markers and ctty")
	}
}

func TestCompletionZshOutput(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"completion", "zsh"})
	err := RootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Errorf("Expected no error for zsh completion, got %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "compdef") || !strings.Contains(output, "ctty") {
		t.Error("Zsh completion output should contain compdef and ctty")
	}
}

func TestCompletionFishOutput(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"completion", "fish"})
	err := RootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Errorf("Expected no error for fish completion, got %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "complete") || !strings.Contains(output, "ctty") {
		t.Error("Fish completion output should contain complete command and ctty")
	}
}

func TestCompletionPowershellOutput(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	RootCmd.SetArgs([]string{"completion", "powershell"})
	err := RootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Errorf("Expected no error for powershell completion, got %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !strings.Contains(output, "Register-ArgumentCompleter") || !strings.Contains(output, "ctty") {
		t.Error("PowerShell completion output should contain Register-ArgumentCompleter and ctty")
	}
}

func TestCompletionInvalidShell(t *testing.T) {
	RootCmd.SetArgs([]string{"completion", "invalid"})
	err := RootCmd.Execute()

	if err == nil {
		t.Error("Expected error for invalid shell type")
	}
}

func TestCompletionNoArgs(t *testing.T) {
	RootCmd.SetArgs([]string{"completion"})
	err := RootCmd.Execute()

	if err == nil {
		t.Error("Expected error when no shell type provided")
	}
}

func TestValidArgsFunction(t *testing.T) {
	if RootCmd.ValidArgsFunction == nil {
		t.Fatal("Expected ValidArgsFunction to be set on RootCmd")
	}
}

func TestValidArgsFunctionWithSSHConfig(t *testing.T) {
	tmpDir := t.TempDir()
	testConfigFile := filepath.Join(tmpDir, "config")

	sshConfig := `Host prod-server
    HostName 192.168.1.1
    User admin

Host dev-server
    HostName 192.168.1.2
    User developer

Host staging-db
    HostName 192.168.1.3
    User dbadmin
`
	err := os.WriteFile(testConfigFile, []byte(sshConfig), 0600)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	originalConfigFile := configFile
	defer func() { configFile = originalConfigFile }()
	configFile = testConfigFile

	tests := []struct {
		name       string
		toComplete string
		args       []string
		wantCount  int
		wantHosts  []string
	}{
		{
			name:       "empty prefix returns all hosts",
			toComplete: "",
			args:       []string{},
			wantCount:  3,
			wantHosts:  []string{"prod-server", "dev-server", "staging-db"},
		},
		{
			name:       "prefix filters hosts",
			toComplete: "prod",
			args:       []string{},
			wantCount:  1,
			wantHosts:  []string{"prod-server"},
		},
		{
			name:       "prefix case insensitive",
			toComplete: "DEV",
			args:       []string{},
			wantCount:  1,
			wantHosts:  []string{"dev-server"},
		},
		{
			name:       "no match returns empty",
			toComplete: "nonexistent",
			args:       []string{},
			wantCount:  0,
			wantHosts:  []string{},
		},
		{
			name:       "already has host arg returns nothing",
			toComplete: "",
			args:       []string{"existing-host"},
			wantCount:  0,
			wantHosts:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completions, directive := RootCmd.ValidArgsFunction(RootCmd, tt.args, tt.toComplete)

			if len(completions) != tt.wantCount {
				t.Errorf("Expected %d completions, got %d: %v", tt.wantCount, len(completions), completions)
			}

			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Errorf("Expected ShellCompDirectiveNoFileComp, got %v", directive)
			}

			for _, wantHost := range tt.wantHosts {
				found := false
				for _, comp := range completions {
					if comp == wantHost {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected completion '%s' not found in %v", wantHost, completions)
				}
			}
		})
	}
}

func TestValidArgsFunctionWithNonExistentConfig(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentConfig := filepath.Join(tmpDir, "nonexistent")

	originalConfigFile := configFile
	defer func() { configFile = originalConfigFile }()
	configFile = nonExistentConfig

	completions, directive := RootCmd.ValidArgsFunction(RootCmd, []string{}, "")

	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("Expected ShellCompDirectiveNoFileComp for non-existent config, got %v", directive)
	}

	if len(completions) != 0 {
		t.Errorf("Expected empty completions for non-existent config, got %v", completions)
	}
}
