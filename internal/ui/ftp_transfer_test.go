package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/ftpclient"
	"github.com/zsuroy/ctty/internal/i18n"
)

func TestFTPTransferQueueRunsOneFileAtATime(t *testing.T) {
	var q ftpTransferQueue
	first, started := q.startOrEnqueue(ftpTransferJob{filename: "a.bin", isUpload: true})
	if !started || first.filename != "a.bin" {
		t.Fatalf("first start: started=%v job=%+v", started, first)
	}
	_, started = q.startOrEnqueue(ftpTransferJob{filename: "b.bin", isUpload: true})
	if started {
		t.Fatal("second file must be queued, not started concurrently")
	}
	if q.current().filename != "a.bin" {
		t.Fatalf("current = %q, want a.bin", q.current().filename)
	}
	next, ok := q.finishCurrent()
	if !ok || next.filename != "b.bin" {
		t.Fatalf("advance: ok=%v next=%+v", ok, next)
	}
	_, ok = q.finishCurrent()
	if ok || !q.empty() {
		t.Fatal("queue should be empty after the last file")
	}
}

func TestFormatFTPProgressShowsPercent(t *testing.T) {
	job := ftpTransferJob{filename: "b.bin", isUpload: false}
	got := formatFTPProgress(job, 50, 100, 2, 2)
	if !strings.Contains(got, "b.bin") || !strings.Contains(got, "50%") || !strings.Contains(got, "2/2") {
		t.Fatalf("progress = %q", got)
	}
}

func TestStaleFTPProgressIsIgnored(t *testing.T) {
	if !staleFTPProgress(true, 2, 1) {
		t.Fatal("older generation should be stale")
	}
	if staleFTPProgress(true, 2, 2) {
		t.Fatal("current generation should be live")
	}
	if !staleFTPProgress(false, 2, 2) {
		t.Fatal("progress while idle should be stale")
	}
}

func TestFTPIgnoresStaleProgressTick(t *testing.T) {
	m := NewFTPForm(NewStyles(80), 80, 24, "site")
	m.client = &ftpclient.Client{}
	m.loading = true
	m.transferring = true
	m.progressGen = 2
	m.statusMsg = "Downloading b.bin: 0B / 100B (0%)"

	_, cmd := m.Update(ftpProgressMsg{
		gen: 1, filename: "a.bin", done: 50, total: 100, isUpload: false,
	})
	if strings.Contains(m.statusMsg, "a.bin") {
		t.Fatalf("stale tick updated status: %q", m.statusMsg)
	}
	if cmd != nil {
		t.Fatal("stale tick must not reschedule progress")
	}
}

func TestFTPProgressTickUpdatesStatus(t *testing.T) {
	m := NewFTPForm(NewStyles(80), 80, 24, "site")
	m.client = &ftpclient.Client{}
	m.loading = true
	m.transferring = true
	m.progressGen = 1
	m.queue.startOrEnqueue(ftpTransferJob{filename: "b.bin", isUpload: false})

	_, cmd := m.Update(ftpProgressMsg{
		gen: 1, filename: "b.bin", done: 50, total: 100, isUpload: false,
	})
	if !strings.Contains(m.statusMsg, "b.bin") || !strings.Contains(m.statusMsg, "50%") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	if cmd == nil {
		t.Fatal("live progress must reschedule tick")
	}
}

func TestFTPNarrowLayoutUsesSinglePane(t *testing.T) {
	m := NewFTPForm(NewStyles(60), 60, 24, "site")
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse
	m.focusLocal = false
	m.entries = []ftpclient.RemoteEntry{{Name: "readme.txt", Size: 10}}
	m.updateRemoteRows()
	if !m.narrow() {
		t.Fatal("expected narrow layout at width 60")
	}
	view := m.View()
	if !strings.Contains(view, "[REMOTE]") {
		t.Fatalf("narrow remote view missing remote label: %q", view)
	}
	m.setFocusLocal(true)
	m.updateLocalRows()
	view = m.View()
	if !strings.Contains(view, "[LOCAL]") {
		t.Fatalf("narrow local view missing local label: %q", view)
	}
}

func TestFTPEnterDownloadsWithConfirm(t *testing.T) {
	m := NewFTPForm(NewStyles(100), 100, 24, "site")
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse
	m.cwd = "/pub"
	m.entries = []ftpclient.RemoteEntry{{Name: "a.bin", Size: 10}}
	m.updateRemoteRows()
	m.remoteTbl.SetCursor(0)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm := updated.(*ftpFormModel)
	if fm.mode != ftpDownloadConfirm {
		t.Fatalf("mode = %v, want download confirm", fm.mode)
	}
}

func TestFTPDeleteKeyOpensConfirm(t *testing.T) {
	m := NewFTPForm(NewStyles(100), 100, 24, "site")
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse
	m.cwd = "/pub"
	m.localCwd = t.TempDir()
	m.entries = []ftpclient.RemoteEntry{{Name: "a.bin", Size: 10}}
	m.updateRemoteRows()
	m.remoteTbl.SetCursor(0)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*ftpFormModel)
	if fm.mode != ftpDeleteConfirm {
		t.Fatalf("mode = %v, want delete confirm", fm.mode)
	}
	if fm.selected == nil || fm.selected.Name != "a.bin" {
		t.Fatalf("selected = %+v, want a.bin", fm.selected)
	}
	if fm.transferring {
		t.Fatal("d must not start a transfer")
	}
}

func TestFTPErrorViewHintsEsc(t *testing.T) {
	m := NewFTPForm(NewStyles(80), 80, 24, "site")
	m.mode = ftpError
	m.loadError = "dial failed"
	view := m.renderErrorView()
	if !strings.Contains(view, "Esc") && !strings.Contains(view, "esc") {
		// i18n may render "Press Esc" or Chinese equivalent; key always embeds Esc in EN default.
		if !strings.Contains(strings.ToLower(view), "esc") {
			t.Fatalf("error view missing Esc hint: %q", view)
		}
	}
}

func TestFTPCancelWaitsForTransferResult(t *testing.T) {
	m := NewFTPForm(NewStyles(100), 100, 24, "site")
	m.client = &ftpclient.Client{}
	m.loading = true
	m.transferring = true
	m.progressGen = 3
	m.progressFile = "big.bin"
	m.queue.startOrEnqueue(ftpTransferJob{filename: "big.bin", isUpload: false})
	ctx, cancel := context.WithCancel(context.Background())
	m.transferCancel = cancel
	_ = ctx

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm := updated.(*ftpFormModel)
	if !fm.transferring {
		t.Fatal("Esc must keep transferring=true until result arrives")
	}
	if !fm.queue.empty() {
		t.Fatal("cancel must clear the transfer queue")
	}
	if !strings.Contains(fm.statusMsg, "Cancelling") {
		t.Fatalf("status = %q, want Cancelling", fm.statusMsg)
	}

	updated, _ = fm.Update(ftpDownloadResultMsg{
		gen: 3, filename: "big.bin", success: false, err: context.Canceled,
	})
	fm = updated.(*ftpFormModel)
	if fm.transferring {
		t.Fatal("result must clear transferring")
	}
	if !strings.Contains(fm.statusMsg, "Cancelled") {
		t.Fatalf("status = %q, want Cancelled", fm.statusMsg)
	}
}

func TestFTPResizeUpdatesSearchAndPaneWidth(t *testing.T) {
	m := NewFTPForm(NewStyles(80), 80, 24, "site")
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse
	oldPW := m.paneWidth()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	fm := updated.(*ftpFormModel)
	if fm.width != 120 || fm.height != 30 {
		t.Fatalf("size = %dx%d", fm.width, fm.height)
	}
	if fm.paneWidth() <= oldPW {
		t.Fatalf("paneWidth did not grow on widen: old=%d new=%d", oldPW, fm.paneWidth())
	}
	if fm.searchInput.Width <= 0 {
		t.Fatal("searchInput.Width must be set on resize")
	}
	view := fm.View()
	// Dual-pane bordered boxes must fit terminal (borders budgeted in paneWidth).
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > fm.width {
			t.Fatalf("line wider than terminal (%d > %d): %q", lipgloss.Width(line), fm.width, line)
		}
	}
}

func TestFTPPaneWidthBudgetsBorders(t *testing.T) {
	for _, term := range []int{60, 79, 80, 100, 120} {
		m := NewFTPForm(NewStyles(term), term, 24, "site")
		m.client = &ftpclient.Client{}
		m.loading = false
		m.mode = ftpBrowse
		pw := m.paneWidth()
		if m.narrow() {
			if pw != term-4 && pw != 20 { // floor at 20 for very small
				if term-4 >= 20 && pw != term-4 {
					t.Fatalf("narrow term=%d paneWidth=%d want %d", term, pw, term-4)
				}
			}
		} else {
			want := (term - 8) / 2
			if want < 20 {
				want = 20
			}
			if pw != want {
				t.Fatalf("dual term=%d paneWidth=%d want %d", term, pw, want)
			}
		}
		view := m.View()
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > term {
				t.Fatalf("term=%d line width %d > %d: %q", term, lipgloss.Width(line), term, line)
			}
		}
	}
}

func TestFTPHelpFooterRendersOnce(t *testing.T) {
	i18n.SetLang("en")
	m := NewFTPForm(NewStyles(100), 100, 40, "site")
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse
	m.focusLocal = false
	view := m.View()
	needle := strings.TrimSpace(i18n.T("ftp.help_remote_2"))
	if c := strings.Count(view, needle); c != 1 {
		t.Fatalf("help_remote_2 count=%d want 1 in view", c)
	}
	// Dedupe helper: consecutive duplicates collapse.
	got := dedupeStrings([]string{"a", "a", "b", "b", ""})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("dedupeStrings=%v", got)
	}
}

func TestHelpFormIncludesFTPCategory(t *testing.T) {
	i18n.SetLang("en")
	help := NewHelpForm(NewStyles(100), 100, 30, "v0.6.3")
	view := help.View()
	if !strings.Contains(view, "FTP") {
		t.Fatalf("help missing FTP category: %q", view)
	}
	i18n.SetLang("zh")
	help = NewHelpForm(NewStyles(100), 100, 30, "v0.6.3")
	view = help.View()
	if !strings.Contains(view, "FTP") {
		t.Fatalf("zh help missing FTP category: %q", view)
	}
	i18n.SetLang("en")
}

func TestFTPRefreshShowsStatus(t *testing.T) {
	i18n.SetLang("en")
	m := NewFTPForm(NewStyles(100), 100, 24, "site")
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse
	m.cwd = "/"

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	fm := updated.(*ftpFormModel)
	if cmd == nil {
		t.Fatal("remote refresh should return a directory-load command")
	}
	if !fm.refreshing || fm.statusMsg != i18n.T("ftp.refreshing") {
		t.Fatalf("refresh state = refreshing:%v status:%q", fm.refreshing, fm.statusMsg)
	}
	if !strings.Contains(fm.View(), i18n.T("ftp.refreshing")) {
		t.Fatal("refreshing status should be visible in the browser")
	}

	updated, _ = fm.Update(ftpEntriesMsg{cwd: "/", entries: nil})
	fm = updated.(*ftpFormModel)
	if fm.refreshing || fm.statusMsg != i18n.T("ftp.refreshed") {
		t.Fatalf("completed refresh state = refreshing:%v status:%q", fm.refreshing, fm.statusMsg)
	}
	if !strings.Contains(fm.View(), i18n.T("ftp.refreshed")) {
		t.Fatal("completed refresh status should be visible in the browser")
	}
	statusGen := fm.statusGen
	fm.statusExpiry = fm.statusExpiry.Add(-5 * time.Second)
	updated, _ = fm.Update(ftpStatusExpiredMsg{gen: statusGen})
	fm = updated.(*ftpFormModel)
	if fm.statusMsg != "" {
		t.Fatalf("expired refresh status = %q, want empty", fm.statusMsg)
	}

	fm.setFocusLocal(true)
	updated, cmd = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	fm = updated.(*ftpFormModel)
	if cmd == nil || fm.statusMsg != i18n.T("ftp.refreshed") {
		t.Fatalf("local refresh = cmd:%v status:%q", cmd != nil, fm.statusMsg)
	}
}
