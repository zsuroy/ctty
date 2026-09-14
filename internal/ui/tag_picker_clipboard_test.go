package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/serialconfig"
	"github.com/zsuroy/ctty/internal/telnetconfig"
)

func TestFormatSSHCommand(t *testing.T) {
	tests := []struct {
		name       string
		host       config.SSHHost
		configFile string
		expected   string
	}{
		{
			name: "basic host with user and hostname",
			host: config.SSHHost{
				Name:     "web-prod",
				Hostname: "192.168.1.100",
				User:     "ubuntu",
			},
			configFile: "",
			expected:   "ssh ubuntu@192.168.1.100",
		},
		{
			name: "host with non-default port and identity",
			host: config.SSHHost{
				Name:     "db-prod",
				Hostname: "10.0.0.5",
				Port:     "2222",
				User:     "postgres",
				Identity: "~/.ssh/id_rsa",
			},
			configFile: "",
			expected:   "ssh -p 2222 -i ~/.ssh/id_rsa postgres@10.0.0.5",
		},
		{
			name: "host with config file and jump proxy",
			host: config.SSHHost{
				Name:      "internal-srv",
				Hostname:  "172.16.0.10",
				User:      "admin",
				ProxyJump: "bastion.example.com",
			},
			configFile: "/custom/ssh_config",
			expected:   "ssh -F /custom/ssh_config -J bastion.example.com admin@172.16.0.10",
		},
		{
			name: "name equals hostname without user",
			host: config.SSHHost{
				Name:     "bastion.example.com",
				Hostname: "bastion.example.com",
			},
			configFile: "",
			expected:   "ssh bastion.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSSHCommand(tt.host, tt.configFile)
			if got != tt.expected {
				t.Errorf("FormatSSHCommand() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormatOtherCommands(t *testing.T) {
	// Telnet
	t1 := FormatTelnetCommand(telnetconfig.TelnetHost{Host: "switch.local", Port: 2323})
	if t1 != "telnet switch.local 2323" {
		t.Errorf("FormatTelnetCommand with custom port = %q", t1)
	}
	t2 := FormatTelnetCommand(telnetconfig.TelnetHost{Host: "router.local", Port: 0})
	if t2 != "telnet router.local 23" {
		t.Errorf("FormatTelnetCommand with default port = %q", t2)
	}

	// Serial
	s1 := FormatSerialCommand(serialconfig.SerialDevice{Device: "/dev/ttyUSB0", BaudRate: 115200})
	if s1 != "picocom -b 115200 /dev/ttyUSB0" {
		t.Errorf("FormatSerialCommand = %q", s1)
	}
	s2 := FormatSerialCommand(serialconfig.SerialDevice{Device: "/dev/ttyS0", BaudRate: 0})
	if s2 != "picocom -b 115200 /dev/ttyS0" {
		t.Errorf("FormatSerialCommand with default baud = %q", s2)
	}

	// FTP
	f1 := FormatFTPCommand(ftpconfig.FTPSite{Host: "ftp.debian.org", Port: 21, User: "anonymous"})
	if f1 != "ftp ftp://anonymous@ftp.debian.org:21" {
		t.Errorf("FormatFTPCommand = %q", f1)
	}
	f2 := FormatFTPCommand(ftpconfig.FTPSite{Host: "ftp.test.net", Port: 2121})
	if f2 != "ftp ftp://ftp.test.net:2121" {
		t.Errorf("FormatFTPCommand without user = %q", f2)
	}
}

func TestTagPickerWorkflow(t *testing.T) {
	i18n.SetLang("zh")

	hosts := []config.SSHHost{
		{Name: "web-1", Hostname: "10.0.1.1", Tags: []string{"web", "prod"}},
		{Name: "web-2", Hostname: "10.0.1.2", Tags: []string{"web", "prod"}},
		{Name: "db-1", Hostname: "10.0.2.1", Tags: []string{"db", "prod"}},
		{Name: "dev-1", Hostname: "10.0.3.1", Tags: []string{"dev"}},
		{Name: "legacy", Hostname: "10.0.4.1"},
	}

	m := NewModel(hosts, "", false, "v1.0.0", true)
	m.width = 100
	m.height = 30
	m.styles = NewStyles(100)
	m.ready = true

	// 1. Initial tag counts
	tagItems := m.getTagCounts()
	if len(tagItems) != 4 {
		t.Fatalf("expected 4 unique tags, got %d", len(tagItems))
	}
	// prod has 3 hosts, web has 2 hosts, db and dev have 1 host each
	if tagItems[0].tag != "prod" || tagItems[0].count != 3 {
		t.Errorf("expected tag 0 to be prod(3), got %s(%d)", tagItems[0].tag, tagItems[0].count)
	}
	if tagItems[1].tag != "web" || tagItems[1].count != 2 {
		t.Errorf("expected tag 1 to be web(2), got %s(%d)", tagItems[1].tag, tagItems[1].count)
	}

	// 2. Press 'w' to open tag picker drawer
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = mUpdated.(Model)
	if !m.tagPickerOpen {
		t.Fatalf("expected tagPickerOpen to be true after pressing 'w'")
	}
	viewStr := m.View()
	if !strings.Contains(viewStr, i18n.T("tags.title")) {
		t.Fatalf("view does not contain tag picker title")
	}

	// 3. Move cursor down in tag picker: j
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = mUpdated.(Model)
	if m.tagPickerCursor != 1 {
		t.Errorf("expected tagPickerCursor to be 1, got %d", m.tagPickerCursor)
	}

	// 4. Press Enter to select cursor 1 (which is "prod")
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mUpdated.(Model)
	if m.tagPickerOpen {
		t.Fatalf("expected tagPickerOpen to be false after selecting a tag")
	}
	if m.selectedTag != "prod" {
		t.Fatalf("expected selectedTag to be 'prod', got %q", m.selectedTag)
	}
	if len(m.filteredHosts) != 3 {
		t.Fatalf("expected 3 filtered hosts for 'prod', got %d", len(m.filteredHosts))
	}

	// 5. Verify banner rendered in list view
	listRender := m.View()
	if !strings.Contains(listRender, "prod") {
		t.Errorf("expected banner with tag 'prod' in list view")
	}

	// 6. Press 'c' to clear active tag filter
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = mUpdated.(Model)
	if m.selectedTag != "" {
		t.Fatalf("expected selectedTag to be cleared, got %q", m.selectedTag)
	}
	if len(m.filteredHosts) != 5 {
		t.Fatalf("expected 5 hosts restored after clearing tag filter, got %d", len(m.filteredHosts))
	}

	// 7. Quick selection with number key '2' (which corresponds to index 1: "web")
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = mUpdated.(Model)
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = mUpdated.(Model)
	if m.selectedTag != "web" {
		t.Fatalf("expected selectedTag to be 'web' via shortcut '2', got %q", m.selectedTag)
	}
	if len(m.filteredHosts) != 2 {
		t.Fatalf("expected 2 filtered hosts for 'web', got %d", len(m.filteredHosts))
	}

	// 8. Close with Esc
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = mUpdated.(Model)
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mUpdated.(Model)
	if m.tagPickerOpen {
		t.Fatalf("expected tagPickerOpen to be false after Esc")
	}
	// Filter should be preserved
	if m.selectedTag != "web" {
		t.Fatalf("expected selectedTag to remain 'web', got %q", m.selectedTag)
	}
}

func TestTagPickerEmptyHosts(t *testing.T) {
	i18n.SetLang("en")
	hosts := []config.SSHHost{
		{Name: "host-no-tags", Hostname: "10.0.0.1"},
	}
	m := NewModel(hosts, "", false, "v1.0.0", true)
	m.width = 100
	m.height = 30
	m.styles = NewStyles(100)

	// Press 'w' when no tags exist
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = mUpdated.(Model)
	if m.tagPickerOpen {
		t.Fatalf("tag picker should not open when no tags exist")
	}
	if !m.statusActive() || !strings.Contains(m.statusMessage, "No tags found") {
		t.Fatalf("expected 'No tags found' status toast, got %q", m.statusMessage)
	}
}

func TestCopyKeyBindingSSH(t *testing.T) {
	orig := i18n.CurrentLang()
	defer i18n.SetLang(orig)
	i18n.SetLang("zh")
	hosts := []config.SSHHost{
		{Name: "app-server", Hostname: "192.168.1.50", User: "dev"},
	}
	m := NewModel(hosts, "", false, "v1.0.0", true)
	m.width = 100
	m.height = 30
	m.styles = NewStyles(100)

	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mUpdated.(Model)
	if !m.statusActive() {
		t.Fatalf("expected status toast after pressing 'y'")
	}
	if !strings.Contains(m.statusMessage, "ssh dev@192.168.1.50") {
		t.Fatalf("expected copied status message to contain 'ssh dev@192.168.1.50', got %q", m.statusMessage)
	}
}

func TestCopyKeyBindingTelnet(t *testing.T) {
	orig := i18n.CurrentLang()
	defer i18n.SetLang(orig)
	i18n.SetLang("zh")
	styles := NewStyles(100)
	tf := NewTelnetForm(styles, 100, 30)
	tf.hosts = []telnetconfig.TelnetHost{
		{Name: "core-switch", Host: "10.0.0.254", Port: 2323},
	}
	tf.filtered = tf.hosts
	tf.buildTable()

	mUpdated, _ := tf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	tf = mUpdated.(*telnetFormModel)
	if !tf.statusActive() {
		t.Fatalf("expected telnet status toast active")
	}
	if !strings.Contains(tf.statusMessage, "telnet 10.0.0.254 2323") {
		t.Fatalf("expected copied message to contain 'telnet 10.0.0.254 2323', got %q", tf.statusMessage)
	}
}

func TestCopyKeyBindingSerial(t *testing.T) {
	orig := i18n.CurrentLang()
	defer i18n.SetLang(orig)
	i18n.SetLang("zh")
	styles := NewStyles(100)
	sf := NewSerialForm(styles, 100, 30)
	sf.filteredDevices = []serialconfig.SerialDevice{
		{Name: "router-console", Device: "/dev/ttyUSB0", BaudRate: 115200},
	}
	sf.devices = sf.filteredDevices

	mUpdated, _ := sf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	sf = mUpdated.(*serialFormModel)
	if !sf.statusActive() {
		t.Fatalf("expected serial status toast active")
	}
	if !strings.Contains(sf.statusMessage, "picocom -b 115200 /dev/ttyUSB0") {
		t.Fatalf("expected copied message to contain 'picocom -b 115200 /dev/ttyUSB0', got %q", sf.statusMessage)
	}
}

func TestCopyKeyBindingFTP(t *testing.T) {
	orig := i18n.CurrentLang()
	defer i18n.SetLang(orig)
	i18n.SetLang("zh")
	styles := NewStyles(100)
	ff := NewFTPSitesForm(styles, 100, 30)
	ff.sites = []ftpconfig.FTPSite{
		{Name: "my-ftp", Host: "ftp.example.com", Port: 21, User: "admin"},
	}
	ff.filtered = ff.sites

	mUpdated, _ := ff.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	ff = mUpdated.(*ftpSitesModel)
	if !ff.statusActive() {
		t.Fatalf("expected ftp status toast active")
	}
	if !strings.Contains(ff.statusMessage, "ftp://admin@ftp.example.com:21") {
		t.Fatalf("expected copied message to contain ftp url, got %q", ff.statusMessage)
	}
}
