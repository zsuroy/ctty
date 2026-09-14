package ui

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
)

// HostStats holds parsed health & resource metrics for an SSH host.
type HostStats struct {
	Uptime      string
	Users       string
	Load1       string
	Load5       string
	Load15      string
	MemTotalMB  int
	MemUsedMB   int
	MemPercent  float64
	DiskTotal   string
	DiskUsed    string
	DiskAvail   string
	DiskPercent float64
	RawOutput   string
}

// hostStatsResultMsg is returned when host stats probing finishes.
type hostStatsResultMsg struct {
	hostName string
	stats    *HostStats
	err      error
	raw      string
}

var (
	reLoadAverage = regexp.MustCompile(`load average[s]?:\s*([0-9.]+)[,\s]+([0-9.]+)[,\s]+([0-9.]+)`)
	reUptime      = regexp.MustCompile(`up\s+(\d+\s+days?,\s*[^,]+|[^,]+)`)
	reUsers       = regexp.MustCompile(`(\d+)\s+users?`)
)

// parseHostStats parses output of:
// uptime 2>/dev/null; echo "---"; free -m 2>/dev/null || vm_stat 2>/dev/null; echo "---"; df -h / 2>/dev/null
func parseHostStats(output string) *HostStats {
	stats := &HostStats{RawOutput: strings.TrimSpace(output)}
	sections := strings.Split(output, "---")

	// Section 1: Uptime & Load
	if len(sections) > 0 {
		upLine := sections[0]
		if m := reLoadAverage.FindStringSubmatch(upLine); len(m) >= 4 {
			stats.Load1 = m[1]
			stats.Load5 = m[2]
			stats.Load15 = m[3]
		}
		if m := reUptime.FindStringSubmatch(upLine); len(m) >= 2 {
			stats.Uptime = strings.TrimSpace(m[1])
		}
		if m := reUsers.FindStringSubmatch(upLine); len(m) >= 2 {
			stats.Users = m[1]
		}
	}

	// Section 2: Memory (free -m)
	if len(sections) > 1 {
		memLines := strings.Split(sections[1], "\n")
		for _, line := range memLines {
			fields := strings.Fields(line)
			if len(fields) >= 3 && strings.HasPrefix(fields[0], "Mem:") {
				if total, err := strconv.Atoi(fields[1]); err == nil && total > 0 {
					stats.MemTotalMB = total
					if used, err := strconv.Atoi(fields[2]); err == nil {
						stats.MemUsedMB = used
						stats.MemPercent = float64(used) / float64(total) * 100.0
					}
				}
				break
			}
		}
	}

	// Section 3: Disk (df -h /)
	if len(sections) > 2 {
		diskLines := strings.Split(sections[2], "\n")
		for _, line := range diskLines {
			fields := strings.Fields(line)
			if len(fields) >= 5 && (fields[len(fields)-1] == "/" || strings.HasSuffix(fields[0], "/")) {
				stats.DiskTotal = fields[1]
				stats.DiskUsed = fields[2]
				stats.DiskAvail = fields[3]
				pctStr := strings.TrimSuffix(fields[4], "%")
				if pct, err := strconv.ParseFloat(pctStr, 64); err == nil {
					stats.DiskPercent = pct
				}
				break
			}
		}
	}

	return stats
}

// renderProgressBar formats a visual progress bar e.g. [████████░░░░░░░░] 48.0%
func renderProgressBar(percent float64, barWidth int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	if barWidth <= 0 {
		barWidth = 16
	}

	filledLen := int(percent / 100.0 * float64(barWidth))
	if filledLen > barWidth {
		filledLen = barWidth
	}
	emptyLen := barWidth - filledLen

	// Color gradient based on usage
	var color lipgloss.Color
	switch {
	case percent >= 85:
		color = lipgloss.Color("9") // Red
	case percent >= 70:
		color = lipgloss.Color("11") // Yellow
	default:
		color = lipgloss.Color("10") // Green
	}

	filledStyle := lipgloss.NewStyle().Foreground(color)
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	bar := filledStyle.Render(strings.Repeat("█", filledLen)) +
		emptyStyle.Render(strings.Repeat("░", emptyLen))

	return fmt.Sprintf("[%s] %5.1f%%", bar, percent)
}

// fetchHostStatsCmd executes a fast remote probe to collect health metrics.
func fetchHostStatsCmd(host config.SSHHost, configFile string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()

		var args []string
		if configFile != "" {
			args = append(args, "-F", configFile)
		}
		if host.Port != "" && host.Port != "22" {
			args = append(args, "-p", host.Port)
		}
		if host.Identity != "" {
			args = append(args, "-i", host.Identity)
		}
		if host.ProxyJump != "" {
			args = append(args, "-J", host.ProxyJump)
		}
		args = append(args,
			"-o", "ConnectTimeout=5",
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "BatchMode=yes",
		)

		target := host.Name
		if host.Hostname != "" && host.Hostname != host.Name {
			if host.User != "" {
				target = fmt.Sprintf("%s@%s", host.User, host.Hostname)
			} else {
				target = host.Hostname
			}
		} else if host.User != "" {
			target = fmt.Sprintf("%s@%s", host.User, host.Name)
		}
		args = append(args, target)

		probeScript := `uptime 2>/dev/null; echo "---"; free -m 2>/dev/null || vm_stat 2>/dev/null; echo "---"; df -h / 2>/dev/null`
		args = append(args, probeScript)

		cmd := exec.CommandContext(ctx, "ssh", args...)
		cmd.Env = buildSSHEnv(host.Name)

		out, err := cmd.CombinedOutput()
		if err != nil {
			return hostStatsResultMsg{hostName: host.Name, err: err, raw: string(out)}
		}
		stats := parseHostStats(string(out))
		return hostStatsResultMsg{hostName: host.Name, stats: stats, raw: string(out)}
	}
}

// renderPeekModal renders the centered Quick Peek card box.
func (m Model) renderPeekModal() string {
	if m.peekHost == nil {
		return ""
	}

	hostName := m.peekHost.Name
	title := m.styles.FocusedLabel.Bold(true).Render(fmt.Sprintf("⚡ "+i18n.T("peek.title"), hostName))

	var rows []string
	rows = append(rows, title, "")

	if m.peekLoading {
		loadingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Italic(true)
		rows = append(rows, loadingStyle.Render("  ⏳ "+i18n.T("peek.loading")), "")
		rows = append(rows, m.styles.HelpText.Render("  Esc: "+i18n.T("pf.action_cancel")))
		return renderCardBox(m.styles.FormContainer, m.width, rows...)
	}

	if m.peekErr != "" {
		errStyle := m.styles.ErrorText
		rows = append(rows, errStyle.Render(fmt.Sprintf("  ✗ "+i18n.T("peek.error"), m.peekErr)), "")
		rows = append(rows, m.styles.HelpText.Render("  r: "+i18n.T("peek.refresh_hint")+" • Esc: "+i18n.T("pf.action_cancel")))
		return renderCardBox(m.styles.FormContainer, m.width, rows...)
	}

	if m.peekStats != nil {
		stats := m.peekStats
		labelStyle := m.styles.Label
		valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

		// Uptime & users
		if stats.Uptime != "" {
			usersPart := ""
			if stats.Users != "" {
				usersPart = fmt.Sprintf(" ("+i18n.T("peek.users")+")", stats.Users)
			}
			rows = append(rows, fmt.Sprintf("  %s  %s", labelStyle.Render(padDisplay(i18n.T("peek.uptime")+":", 12)), valStyle.Render("up "+stats.Uptime+usersPart)))
		}

		// Load average
		if stats.Load1 != "" {
			loadStr := fmt.Sprintf("%s, %s, %s", stats.Load1, stats.Load5, stats.Load15)
			rows = append(rows, fmt.Sprintf("  %s  %s", labelStyle.Render(padDisplay(i18n.T("peek.load")+":", 12)), valStyle.Render(loadStr)))
		}

		// Memory
		if stats.MemTotalMB > 0 {
			bar := renderProgressBar(stats.MemPercent, 14)
			memInfo := fmt.Sprintf("%s (%dMB / %dMB)", bar, stats.MemUsedMB, stats.MemTotalMB)
			rows = append(rows, fmt.Sprintf("  %s  %s", labelStyle.Render(padDisplay(i18n.T("peek.memory")+":", 12)), memInfo))
		}

		// Root disk
		if stats.DiskTotal != "" {
			bar := renderProgressBar(stats.DiskPercent, 14)
			diskInfo := fmt.Sprintf("%s (%s / %s)", bar, stats.DiskUsed, stats.DiskTotal)
			rows = append(rows, fmt.Sprintf("  %s  %s", labelStyle.Render(padDisplay(i18n.T("peek.disk")+":", 12)), diskInfo))
		}

		// Fallback if no structured metrics could be parsed
		if stats.Uptime == "" && stats.MemTotalMB == 0 && stats.DiskTotal == "" && stats.RawOutput != "" {
			rawLines := strings.Split(stats.RawOutput, "\n")
			if len(rawLines) > 6 {
				rawLines = rawLines[:6]
			}
			for _, rl := range rawLines {
				if strings.TrimSpace(rl) != "" && strings.TrimSpace(rl) != "---" {
					rows = append(rows, "  "+valStyle.Render(rl))
				}
			}
		}
	}

	rows = append(rows, "", m.styles.HelpText.Render("  "+i18n.T("peek.help")))
	return renderCardBox(m.styles.FormContainer, m.width, rows...)
}
