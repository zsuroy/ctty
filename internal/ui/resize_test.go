package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/sftpconfig"
)

func TestResizeStress(t *testing.T) {
	styles := NewStyles(80)

	// 1. Test Main Model
	hosts := []config.SSHHost{
		{Name: "server1", Hostname: "192.168.1.1", User: "root", Port: "22"},
		{Name: "server2", Hostname: "10.0.0.1", User: "admin", Port: "2222", Tags: []string{"prod", "db"}},
	}
	m := NewModel(hosts, "", false, "v0.4.0", true)

	// 2. Test Serial Form
	sf := NewSerialForm(styles, 80, 24)

	// 3. Test SFTP Form
	sftp := NewSFTPForm(styles, 80, 24, "server1", "")
	sftp.entries = []sftpconfig.RemoteEntry{
		{Name: "file1.txt", Size: 1024, ModTime: time.Now(), IsDir: false},
		{Name: "folder1", Size: 4096, ModTime: time.Now(), IsDir: true},
	}
	sftp.updateTableRows()

	// Test all combinations of width and height
	for w := 5; w <= 160; w += 3 {
		for h := 3; h <= 60; h += 2 {
			sizeMsg := tea.WindowSizeMsg{Width: w, Height: h}

			// Resize main model
			mRes, _ := m.Update(sizeMsg)
			m = mRes.(Model)
			_ = m.View()

			// Resize Serial form
			sfRes, _ := sf.Update(sizeMsg)
			sf = sfRes.(*serialFormModel)
			_ = sf.View()

			// Resize SFTP form
			sftpRes, _ := sftp.Update(sizeMsg)
			sftp = sftpRes.(*sftpFormModel)
			_ = sftp.View()
		}
	}
}
