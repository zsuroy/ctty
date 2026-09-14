package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
)

func TestParseHostStats(t *testing.T) {
	sampleOutput := ` 14:23:45 up 42 days, 3:15,  2 users,  load average: 0.15, 0.22, 0.18
---
              total        used        free      shared  buff/cache   available
Mem:          15920        4820        8100         350        3000       10750
Swap:          2048           0        2048
---
Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        98G   45G   48G  49% /
`
	stats := parseHostStats(sampleOutput)
	if stats == nil {
		t.Fatal("expected non-nil HostStats")
	}

	if stats.Uptime != "42 days, 3:15" {
		t.Errorf("unexpected uptime: got %q, want %q", stats.Uptime, "42 days, 3:15")
	}
	if stats.Users != "2" {
		t.Errorf("unexpected users: got %q, want %q", stats.Users, "2")
	}
	if stats.Load1 != "0.15" || stats.Load5 != "0.22" || stats.Load15 != "0.18" {
		t.Errorf("unexpected load: got %s, %s, %s", stats.Load1, stats.Load5, stats.Load15)
	}
	if stats.MemTotalMB != 15920 {
		t.Errorf("unexpected mem total: got %d, want 15920", stats.MemTotalMB)
	}
	if stats.MemUsedMB != 4820 {
		t.Errorf("unexpected mem used: got %d, want 4820", stats.MemUsedMB)
	}
	expectedMemPct := float64(4820) / float64(15920) * 100.0
	if fmt.Sprintf("%.1f", stats.MemPercent) != fmt.Sprintf("%.1f", expectedMemPct) {
		t.Errorf("unexpected mem percent: got %.1f, want %.1f", stats.MemPercent, expectedMemPct)
	}
	if stats.DiskTotal != "98G" || stats.DiskUsed != "45G" || stats.DiskAvail != "48G" {
		t.Errorf("unexpected disk stats: got %s, %s, %s", stats.DiskTotal, stats.DiskUsed, stats.DiskAvail)
	}
	if stats.DiskPercent != 49 {
		t.Errorf("unexpected disk percent: got %f, want 49", stats.DiskPercent)
	}
}

func TestParseHostStats_PartialAndFallback(t *testing.T) {
	// Test output with only uptime and no free or df output
	out1 := ` 10:00:00 up 5 mins, 1 user, load average: 1.20, 0.80, 0.40
---
command not found: free
---
command not found: df
`
	s1 := parseHostStats(out1)
	if s1.Uptime != "5 mins" {
		t.Errorf("expected uptime 5 mins, got %q", s1.Uptime)
	}
	if s1.Users != "1" {
		t.Errorf("expected users 1, got %q", s1.Users)
	}
	if s1.Load1 != "1.20" {
		t.Errorf("expected load1 1.20, got %q", s1.Load1)
	}
	if s1.MemTotalMB != 0 {
		t.Errorf("expected 0 mem total, got %d", s1.MemTotalMB)
	}

	// Test raw output with no separator
	out2 := `some generic host banner
Linux version 5.15.0`
	s2 := parseHostStats(out2)
	if s2.RawOutput != out2 {
		t.Errorf("expected RawOutput to match, got %q", s2.RawOutput)
	}
}

func TestRenderProgressBar(t *testing.T) {
	bar0 := renderProgressBar(0, 10)
	if !strings.Contains(bar0, "0.0%") {
		t.Errorf("expected 0.0%%, got %s", bar0)
	}

	bar50 := renderProgressBar(50, 10)
	if !strings.Contains(bar50, "50.0%") {
		t.Errorf("expected 50.0%%, got %s", bar50)
	}

	bar90 := renderProgressBar(90, 10)
	if !strings.Contains(bar90, "90.0%") {
		t.Errorf("expected 90.0%%, got %s", bar90)
	}

	// Negative and overflow
	barNeg := renderProgressBar(-10, 10)
	if !strings.Contains(barNeg, "0.0%") {
		t.Errorf("expected 0.0%% for negative, got %s", barNeg)
	}
	barOver := renderProgressBar(120, 10)
	if !strings.Contains(barOver, "100.0%") {
		t.Errorf("expected 100.0%% for overflow, got %s", barOver)
	}
}

func TestMultiSelectOperations(t *testing.T) {
	hosts := []config.SSHHost{
		{Name: "web-01", Hostname: "10.0.0.1"},
		{Name: "web-02", Hostname: "10.0.0.2"},
		{Name: "db-01", Hostname: "10.0.0.3"},
	}

	m := NewModel(hosts, "", false, "dev", true)
	if len(m.selectedHosts) != 0 {
		t.Fatalf("expected 0 selected hosts initially, got %d", len(m.selectedHosts))
	}

	// Toggle web-01
	m.toggleHostSelected("web-01")
	if !m.isHostSelected("web-01") {
		t.Errorf("expected web-01 to be selected")
	}
	if len(m.getSelectedHosts()) != 1 {
		t.Errorf("expected 1 selected host, got %d", len(m.getSelectedHosts()))
	}

	// Toggle web-01 off
	m.toggleHostSelected("web-01")
	if m.isHostSelected("web-01") {
		t.Errorf("expected web-01 to be unselected")
	}

	// Select All visible
	m.toggleSelectAllVisible()
	if len(m.selectedHosts) != 3 {
		t.Errorf("expected 3 selected hosts, got %d", len(m.selectedHosts))
	}

	// Select All again -> should deselect all
	m.toggleSelectAllVisible()
	if len(m.selectedHosts) != 0 {
		t.Errorf("expected 0 selected hosts after toggle, got %d", len(m.selectedHosts))
	}

	// Select some and clear
	m.toggleHostSelected("web-01")
	m.toggleHostSelected("db-01")
	if len(m.selectedHosts) != 2 {
		t.Errorf("expected 2 selected hosts, got %d", len(m.selectedHosts))
	}
	m.clearSelection()
	if len(m.selectedHosts) != 0 {
		t.Errorf("expected 0 selected hosts after clear, got %d", len(m.selectedHosts))
	}
}

func TestMultiSelectKeyboardFlow(t *testing.T) {
	hosts := []config.SSHHost{
		{Name: "srv-a", Hostname: "192.168.1.10"},
		{Name: "srv-b", Hostname: "192.168.1.11"},
	}

	m := NewModel(hosts, "", false, "dev", true)
	m.ready = true
	m.width = 100
	m.height = 30
	m.styles = NewStyles(m.width)
	// Initially, table view should show [ ]
	viewInit := m.renderTableView()
	if !strings.Contains(viewInit, "[ ]") {
		t.Errorf("expected initial view to show [ ] checkbox, got: %s", viewInit)
	}

	// Press Space to select first host
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	mod := newM.(Model)

	if !mod.isHostSelected("srv-a") {
		t.Errorf("expected srv-a to be selected after space")
	}
	// Cursor should have advanced to index 1
	if mod.table.Cursor() != 1 {
		t.Errorf("expected cursor to advance to 1, got %d", mod.table.Cursor())
	}

	// Rendered view must now show [✓] for selected host
	viewAfterSpace := mod.renderTableView()
	if !strings.Contains(viewAfterSpace, "[✓]") {
		t.Errorf("expected view to contain [✓] mark after space, got: %s", viewAfterSpace)
	}

	// Press Ctrl+A to select all visible
	newM, _ = mod.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	mod = newM.(Model)
	if len(mod.selectedHosts) != 2 {
		t.Errorf("expected 2 selected hosts after ctrl+a, got %d", len(mod.selectedHosts))
	}

	// Press Esc to clear selection
	newM, _ = mod.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mod = newM.(Model)
	if len(mod.selectedHosts) != 0 {
		t.Errorf("expected 0 selected hosts after esc, got %d", len(mod.selectedHosts))
	}
	viewAfterEsc := mod.renderTableView()
	if strings.Contains(viewAfterEsc, "[✓]") {
		t.Errorf("expected [✓] to be gone after esc, got: %s", viewAfterEsc)
	}
}

func TestQuickPeekModal(t *testing.T) {
	host := config.SSHHost{Name: "test-box", Hostname: "127.0.0.1"}
	m := NewModel([]config.SSHHost{host}, "", false, "dev", true)
	m.width = 100
	m.height = 30
	m.styles = NewStyles(m.width)

	// Press 'v' to open quick peek
	newM, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	mod := newM.(Model)

	if !mod.peekOpen {
		t.Fatalf("expected peekOpen to be true")
	}
	if mod.peekHost == nil || mod.peekHost.Name != "test-box" {
		t.Errorf("unexpected peekHost: %+v", mod.peekHost)
	}
	if !mod.peekLoading {
		t.Errorf("expected peekLoading to be true")
	}

	// Verify loading modal renders
	view := mod.renderPeekModal()
	if !strings.Contains(view, i18n.T("peek.loading")) {
		t.Errorf("expected view to contain loading text, got: %s", view)
	}

	// Feed hostStatsResultMsg with error
	newM, _ = mod.Update(hostStatsResultMsg{
		hostName: "test-box",
		err:      fmt.Errorf("connection refused"),
	})
	mod = newM.(Model)
	if mod.peekErr != "connection refused" {
		t.Errorf("expected peekErr 'connection refused', got %q", mod.peekErr)
	}
	viewErr := mod.renderPeekModal()
	if !strings.Contains(viewErr, "connection refused") {
		t.Errorf("expected view to contain error message, got: %s", viewErr)
	}

	// Feed hostStatsResultMsg with stats
	sampleStats := &HostStats{
		Uptime:      "10 days",
		Users:       "3",
		Load1:       "0.50",
		Load5:       "0.60",
		Load15:      "0.70",
		MemTotalMB:  8000,
		MemUsedMB:   4000,
		MemPercent:  50.0,
		DiskTotal:   "100G",
		DiskUsed:    "20G",
		DiskAvail:   "80G",
		DiskPercent: 20.0,
	}
	newM, _ = mod.Update(hostStatsResultMsg{
		hostName: "test-box",
		stats:    sampleStats,
	})
	mod = newM.(Model)
	if mod.peekStats == nil || mod.peekStats.Uptime != "10 days" {
		t.Fatalf("expected stats uptime 10 days")
	}
	viewStats := mod.renderPeekModal()
	if !strings.Contains(viewStats, "10 days") || !strings.Contains(viewStats, "50.0%") {
		t.Errorf("expected view to contain stats, got: %s", viewStats)
	}

	// Press Esc to close peek
	newM, _ = mod.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mod = newM.(Model)
	if mod.peekOpen {
		t.Errorf("expected peekOpen to be false after Esc")
	}
}

func TestBatchExecutionModal(t *testing.T) {
	m := NewModel(nil, "", false, "dev", true)
	m.width = 100
	m.height = 30
	m.styles = NewStyles(m.width)

	m.batchResultOpen = true
	m.batchRunning = true
	m.batchCommand = "uname -a"
	m.batchHostCount = 2

	// Render running modal
	runningView := m.renderBatchModal()
	if !strings.Contains(runningView, "uname -a") {
		t.Errorf("expected runningView to contain command, got: %s", runningView)
	}

	// Receive batch results
	results := []BatchResult{
		{
			HostName: "host-1",
			Output:   "Linux host-1 5.15.0",
			Err:      nil,
			Duration: 120 * time.Millisecond,
		},
		{
			HostName: "host-2",
			Output:   "",
			Err:      fmt.Errorf("host unreachable"),
			Duration: 5 * time.Second,
		},
	}

	newM, _ := m.Update(batchExecDoneMsg{
		command: "uname -a",
		results: results,
	})
	mod := newM.(Model)

	if mod.batchRunning {
		t.Errorf("expected batchRunning to be false after done msg")
	}
	if len(mod.batchResults) != 2 {
		t.Fatalf("expected 2 batch results, got %d", len(mod.batchResults))
	}

	doneView := mod.renderBatchModal()
	if !strings.Contains(doneView, "host-1") || !strings.Contains(doneView, "Linux host-1 5.15.0") {
		t.Errorf("expected doneView to contain host-1 output, got: %s", doneView)
	}
	if !strings.Contains(doneView, "host unreachable") {
		t.Errorf("expected doneView to contain host-2 error, got: %s", doneView)
	}

	// Press Esc to close batch results modal
	newM, _ = mod.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mod = newM.(Model)
	if mod.batchResultOpen {
		t.Errorf("expected batchResultOpen to be false after Esc")
	}
}

func TestRenderStatusToast(t *testing.T) {
	// Probing message should have ⏳
	t1 := renderStatusToast("Probing 2 selected hosts...")
	if !strings.Contains(t1, "⏳") {
		t.Errorf("expected ⏳ for probing, got: %s", t1)
	}
	t2 := renderStatusToast("正在探测选中的 2 台主机...")
	if !strings.Contains(t2, "⏳") {
		t.Errorf("expected ⏳ for probing ZH, got: %s", t2)
	}

	// Cleared message should have ℹ
	t3 := renderStatusToast("Selection cleared")
	if !strings.Contains(t3, "ℹ") {
		t.Errorf("expected ℹ for clear, got: %s", t3)
	}
	t4 := renderStatusToast("已清除勾选")
	if !strings.Contains(t4, "ℹ") {
		t.Errorf("expected ℹ for clear ZH, got: %s", t4)
	}

	// Copied message should have ✓
	t5 := renderStatusToast("Copied: ssh host1")
	if !strings.Contains(t5, "✓") {
		t.Errorf("expected ✓ for copied, got: %s", t5)
	}
	t6 := renderStatusToast("已复制: 2 hosts")
	if !strings.Contains(t6, "✓") {
		t.Errorf("expected ✓ for copied ZH, got: %s", t6)
	}

	// Warning/no tags should have ℹ
	t7 := renderStatusToast("No tags found in hosts")
	if !strings.Contains(t7, "ℹ") {
		t.Errorf("expected ℹ for no tags, got: %s", t7)
	}

	// Test renderListView position: status toast should appear after table and before help
	hosts := []config.SSHHost{{Name: "box-1", Hostname: "10.0.0.1"}}
	m := NewModel(hosts, "", false, "dev", true)
	m.width = 100
	m.height = 30
	m.styles = NewStyles(100)
	m.setStatus("正在探测选中的 1 台主机...")

	view := m.renderListView()
	if !strings.Contains(view, "⏳ 正在探测选中的 1 台主机...") {
		t.Errorf("expected view to contain status toast with ⏳, got: %s", view)
	}

	// Verify status toast is rendered BELOW the table and above help
	toastIdx := strings.Index(view, "⏳ 正在探测选中的 1 台主机...")
	tableIdx := strings.Index(view, "box-1")
	if toastIdx < tableIdx {
		t.Errorf("expected toast to appear BELOW table (toastIdx=%d, tableIdx=%d)", toastIdx, tableIdx)
	}
}
