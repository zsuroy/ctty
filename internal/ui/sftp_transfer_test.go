package ui

import (
	"strings"
	"testing"

	"github.com/zsuroy/ctty/internal/sftpconfig"
)

func TestSFTPTransferQueueRunsOneFileAtATime(t *testing.T) {
	var q sftpTransferQueue
	first, started := q.startOrEnqueue(sftpTransferJob{filename: "a.bin", isUpload: true})
	if !started || first.filename != "a.bin" {
		t.Fatalf("first start: started=%v job=%+v", started, first)
	}
	cur, total := q.position()
	if cur != 1 || total != 1 {
		t.Fatalf("position after first = %d/%d", cur, total)
	}

	_, started = q.startOrEnqueue(sftpTransferJob{filename: "b.bin", isUpload: true})
	if started {
		t.Fatal("second file must be queued, not started concurrently")
	}
	cur, total = q.position()
	if cur != 1 || total != 2 {
		t.Fatalf("position while first runs = %d/%d, want 1/2", cur, total)
	}
	if q.current().filename != "a.bin" {
		t.Fatalf("current = %q, want a.bin", q.current().filename)
	}

	next, ok := q.finishCurrent()
	if !ok || next.filename != "b.bin" {
		t.Fatalf("advance: ok=%v next=%+v", ok, next)
	}
	cur, total = q.position()
	if cur != 2 || total != 2 {
		t.Fatalf("position for second = %d/%d, want 2/2", cur, total)
	}

	_, ok = q.finishCurrent()
	if ok {
		t.Fatal("queue should be empty after the last file")
	}
	if !q.empty() {
		t.Fatal("expected empty queue")
	}
}

func TestFormatSFTPProgressShowsOnlyCurrentFile(t *testing.T) {
	job := sftpTransferJob{filename: "b.bin", isUpload: true}
	got := formatSFTPProgress(job, 50, 100, 2, 2)
	if strings.Contains(got, "a.bin") {
		t.Fatalf("progress leaked another filename: %q", got)
	}
	if !strings.Contains(got, "b.bin") {
		t.Fatalf("missing current file: %q", got)
	}
	if !strings.Contains(got, "2/2") {
		t.Fatalf("missing queue position: %q", got)
	}
	if !strings.Contains(got, "50%") {
		t.Fatalf("missing percent: %q", got)
	}
}

func TestStaleSFTPProgressIsIgnored(t *testing.T) {
	if !staleSFTPProgress(true, 2, 1) {
		t.Fatal("older generation should be stale")
	}
	if staleSFTPProgress(true, 2, 2) {
		t.Fatal("current generation should be live")
	}
	if !staleSFTPProgress(false, 2, 2) {
		t.Fatal("progress while idle should be stale")
	}
}

func TestSFTPIgnoresStaleProgressTick(t *testing.T) {
	m := NewSFTPForm(NewStyles(80), 80, 24, "host", "")
	m.client = &sftpconfig.SFTPClient{}
	m.loading = true
	m.transferring = true
	m.progressGen = 2
	m.statusMsg = "Uploading b.bin: 0B / 100B (0%)"

	_, cmd := m.Update(sftpProgressMsg{
		gen: 1, filename: "a.bin", downloaded: 50, total: 100, isUpload: true,
	})
	if strings.Contains(m.statusMsg, "a.bin") {
		t.Fatalf("stale tick updated status: %q", m.statusMsg)
	}
	if cmd != nil {
		t.Fatal("stale tick must not reschedule progress")
	}
}

func TestSFTPTransferErrorStaysInSession(t *testing.T) {
	m := NewSFTPForm(NewStyles(80), 80, 24, "host", "")
	m.client = &sftpconfig.SFTPClient{}
	m.ready = true
	m.mode = sftpBrowse
	m.loading = true
	m.transferring = true
	m.progressGen = 1
	m.queue.startOrEnqueue(sftpTransferJob{filename: "b.bin", isUpload: true})

	_, cmd := m.Update(sftpUploadResultMsg{gen: 1, filename: "b.bin", success: false, err: errSFTPTest{}})
	if m.mode == sftpError {
		t.Fatal("transfer failure must not dump the whole SFTP session")
	}
	if m.transferring {
		t.Fatal("transferring should clear after the last file fails")
	}
	if cmd == nil {
		t.Fatal("expected a directory refresh after the queue drains")
	}
}

func TestSFTPIgnoresStaleTransferResult(t *testing.T) {
	m := NewSFTPForm(NewStyles(80), 80, 24, "host", "")
	m.client = &sftpconfig.SFTPClient{}
	m.ready = true
	m.mode = sftpBrowse
	m.loading = true
	m.transferring = true
	m.progressGen = 2
	m.queue.startOrEnqueue(sftpTransferJob{filename: "b.bin", isUpload: true})

	_, cmd := m.Update(sftpUploadResultMsg{gen: 1, filename: "a.bin", success: true})
	if !m.transferring {
		t.Fatal("current transfer must not finish on a stale result")
	}
	if cmd != nil {
		t.Fatal("stale result must not refresh the directory")
	}
	if m.queue.current().filename != "b.bin" {
		t.Fatalf("queue current = %q", m.queue.current().filename)
	}
}

func TestSFTPListErrorKeepsCurrentDirectory(t *testing.T) {
	m := NewSFTPForm(NewStyles(80), 80, 24, "host", "")
	m.client = &sftpconfig.SFTPClient{}
	m.ready = true
	m.mode = sftpBrowse
	m.entries = []sftpconfig.RemoteEntry{{Name: "keep-me"}}

	_, _ = m.Update(sftpEntriesMsg{cwd: "/tmp", err: errSFTPTest{}})
	if m.mode == sftpError {
		t.Fatal("list failure after connect must not exit SFTP")
	}
	if len(m.entries) != 1 || m.entries[0].Name != "keep-me" {
		t.Fatalf("entries = %+v, want keep-me", m.entries)
	}
}

type errSFTPTest struct{}

func (errSFTPTest) Error() string { return "sftp boom" }
