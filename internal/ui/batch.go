package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
)

// BatchResult holds the execution result for one host.
type BatchResult struct {
	HostName string
	Output   string
	Err      error
	Duration time.Duration
}

// batchExecDoneMsg is sent when all parallel executions complete.
type batchExecDoneMsg struct {
	command string
	results []BatchResult
}

// batchExecCmd executes a remote command on multiple hosts concurrently.
func batchExecCmd(hosts []config.SSHHost, configFile, command string) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		results := make([]BatchResult, len(hosts))

		for i, h := range hosts {
			wg.Add(1)
			go func(idx int, target config.SSHHost) {
				defer wg.Done()
				start := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()

				var args []string
				if configFile != "" {
					args = append(args, "-F", configFile)
				}
				if target.Port != "" && target.Port != "22" {
					args = append(args, "-p", target.Port)
				}
				if target.Identity != "" {
					args = append(args, "-i", target.Identity)
				}
				if target.ProxyJump != "" {
					args = append(args, "-J", target.ProxyJump)
				}
				args = append(args,
					"-o", "ConnectTimeout=5",
					"-o", "StrictHostKeyChecking=accept-new",
				)

				targetHost := target.Name
				if target.Hostname != "" && target.Hostname != target.Name {
					if target.User != "" {
						targetHost = fmt.Sprintf("%s@%s", target.User, target.Hostname)
					} else {
						targetHost = target.Hostname
					}
				} else if target.User != "" {
					targetHost = fmt.Sprintf("%s@%s", target.User, target.Name)
				}
				args = append(args, targetHost, command)

				cmd := exec.CommandContext(ctx, "ssh", args...)
				cmd.Env = buildSSHEnv(target.Name)
				out, err := cmd.CombinedOutput()

				results[idx] = BatchResult{
					HostName: target.Name,
					Output:   strings.TrimSpace(string(out)),
					Err:      err,
					Duration: time.Since(start),
				}
			}(i, h)
		}

		wg.Wait()
		return batchExecDoneMsg{command: command, results: results}
	}
}

// renderBatchModal renders the batch execution progress and results modal.
func (m Model) renderBatchModal() string {
	title := m.styles.FocusedLabel.Bold(true).Render(
		fmt.Sprintf("📦 "+i18n.T("batch.title"), m.batchCommand, m.batchHostCount),
	)

	var rows []string
	rows = append(rows, title, "")

	if m.batchRunning {
		loadingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Italic(true)
		rows = append(rows, loadingStyle.Render(fmt.Sprintf("  ⏳ "+i18n.T("batch.running"), m.batchHostCount)), "")
		rows = append(rows, m.styles.HelpText.Render("  Esc: "+i18n.T("pf.action_cancel")))
		return renderCardBox(m.styles.FormContainer, m.width, rows...)
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	errStyle := m.styles.ErrorText
	subStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	var contentLines []string
	for _, res := range m.batchResults {
		if res.Err == nil {
			statusTag := successStyle.Render("✓ " + i18n.T("batch.success"))
			durTag := subStyle.Render(fmt.Sprintf("(%s)", res.Duration.Round(time.Millisecond)))
			contentLines = append(contentLines, fmt.Sprintf("● [%s]  %s %s", res.HostName, statusTag, durTag))
		} else {
			statusTag := errStyle.Render("✗ " + i18n.T("batch.failed"))
			durTag := subStyle.Render(fmt.Sprintf("(%s)", res.Duration.Round(time.Millisecond)))
			contentLines = append(contentLines, fmt.Sprintf("● [%s]  %s %s: %s", res.HostName, statusTag, durTag, res.Err.Error()))
		}

		if res.Output != "" {
			for _, line := range strings.Split(res.Output, "\n") {
				contentLines = append(contentLines, "    "+line)
			}
		}
		contentLines = append(contentLines, "")
	}

	// Scroll viewport
	viewportHeight := m.height - 10
	if viewportHeight < 6 {
		viewportHeight = 6
	}

	totalLines := len(contentLines)
	if totalLines > viewportHeight {
		start := m.batchScroll
		if start > totalLines-viewportHeight {
			start = totalLines - viewportHeight
		}
		if start < 0 {
			start = 0
		}
		end := start + viewportHeight
		if end > totalLines {
			end = totalLines
		}
		rows = append(rows, contentLines[start:end]...)
	} else {
		rows = append(rows, contentLines...)
	}

	rows = append(rows, m.styles.HelpText.Render("  "+i18n.T("batch.help")))
	return renderCardBox(m.styles.FormContainer, m.width, rows...)
}
