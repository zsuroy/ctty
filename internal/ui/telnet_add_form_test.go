package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/telnetconfig"
	"github.com/zsuroy/ctty/internal/telnetcred"
)

func TestTelnetAddForm_Basic(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(80)

	form := newTelnetAddForm(styles, 80, 24, nil)
	if form == nil {
		t.Fatal("expected newTelnetAddForm to return non-nil")
	}

	out := form.View()
	if !strings.Contains(out, "Add Telnet") {
		t.Fatalf("expected View to contain 'Add Telnet', got:\n%s", out)
	}

	// Esc cancels
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !form.cancelled {
		t.Fatal("expected form to be cancelled on Esc")
	}
}

func TestTelnetAddForm_EditMode(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(80)

	initial := &telnetconfig.TelnetHost{
		Name: "router-gw",
		Host: "192.168.1.254",
		Port: 2323,
		Tags: []string{"network", "lab"},
	}

	form := newTelnetAddForm(styles, 80, 24, initial)
	if form.nameVal != "router-gw" {
		t.Fatalf("expected nameVal router-gw, got %s", form.nameVal)
	}
	if form.portVal != "2323" {
		t.Fatalf("expected portVal 2323, got %s", form.portVal)
	}

	out := form.View()
	if !strings.Contains(out, "Edit Telnet") {
		t.Fatalf("expected View to contain 'Edit Telnet', got:\n%s", out)
	}
}

func TestTelnetAddForm_TabNavigation(t *testing.T) {
	styles := NewStyles(80)
	form := newTelnetAddForm(styles, 80, 24, nil)

	// Focus initially on first field (name)
	if form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected focus on name, got %s", form.form.GetFocusedField().GetKey())
	}

	// Tab advances
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	if form.form.GetFocusedField().GetKey() != "host" {
		t.Fatalf("expected focus on host, got %s", form.form.GetFocusedField().GetKey())
	}

	// Shift+Tab returns
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected focus back on name, got %s", form.form.GetFocusedField().GetKey())
	}
}

func TestTelnetAddForm_Submit(t *testing.T) {
	// confirmVal defaults to true, so the final Enter really submits and
	// writes both telnet.json and the credential vault. Keep both isolated.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	credential.ResetStoreForTest()
	t.Cleanup(credential.ResetStoreForTest)

	styles := NewStyles(80)
	form := newTelnetAddForm(styles, 80, 24, nil)
	_ = form.Init()

	// Enter name
	form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("myrouter")})
	// Press Enter to go to host
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if form.form.GetFocusedField().GetKey() != "host" {
		t.Fatalf("expected focus on host, got %s", form.form.GetFocusedField().GetKey())
	}

	// Enter host
	form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("10.0.0.1")})
	// Press Enter to go to port
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if form.form.GetFocusedField().GetKey() != "port" {
		t.Fatalf("expected focus on port, got %s", form.form.GetFocusedField().GetKey())
	}

	// Press Enter to go to user
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if form.form.GetFocusedField().GetKey() != "user" {
		t.Fatalf("expected focus on user, got %s", form.form.GetFocusedField().GetKey())
	}

	// Enter user
	form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pi")})

	// Press Enter to go to password
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if form.form.GetFocusedField().GetKey() != "password" {
		t.Fatalf("expected focus on password, got %s", form.form.GetFocusedField().GetKey())
	}

	// Enter password
	form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s3cret")})

	// Press Enter to go to tags
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if form.form.GetFocusedField().GetKey() != "tags" {
		t.Fatalf("expected focus on tags, got %s", form.form.GetFocusedField().GetKey())
	}

	// Enter tags
	form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("lab,cisco")})
	// Press Enter to go to confirm
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if form.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected focus on confirm, got %s", form.form.GetFocusedField().GetKey())
	}

	// Now press Enter on confirm
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	t.Logf("done: %v, cancelled: %v, err: %q, nameVal: %q, hostVal: %q, tagsVal: %q, confirmVal: %v",
		form.done, form.cancelled, form.err, form.nameVal, form.hostVal, form.tagsVal, form.confirmVal)
}

func TestTelnetAddForm_ConfirmNavigationAndAutoScroll(t *testing.T) {
	i18n.SetLang("zh")
	styles := NewStyles(80)
	// Short terminal height to force viewport scrolling
	form := newTelnetAddForm(styles, 80, 14, nil)
	_ = form.Init()

	// Advance through all fields to confirm:
	// name -> host -> port -> user -> password -> tags -> confirm
	for i := 0; i < 6; i++ {
		form.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if form.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected focus on confirm, got %s", form.form.GetFocusedField().GetKey())
	}

	// View should have auto-scrolled so Save button is visible
	view := form.View()
	if !strings.Contains(view, i18n.T("form.btn_save")) {
		t.Fatalf("expected view to contain Save button, got:\n%s", view)
	}

	// Up key on confirm should move back to tags
	form.Update(tea.KeyMsg{Type: tea.KeyUp})
	if form.form.GetFocusedField().GetKey() != "tags" {
		t.Fatalf("expected Up on confirm to go to tags, got %s", form.form.GetFocusedField().GetKey())
	}

	// Down key on tags should move back to confirm
	form.Update(tea.KeyMsg{Type: tea.KeyDown})
	if form.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected Down on tags to go to confirm, got %s", form.form.GetFocusedField().GetKey())
	}

	// Down key on confirm should wrap around to name
	form.Update(tea.KeyMsg{Type: tea.KeyDown})
	if form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected Down on confirm to wrap to name, got %s", form.form.GetFocusedField().GetKey())
	}
}

func TestTelnetAddForm_ValidationErrorUsesTelnetLocale(t *testing.T) {
	i18n.SetLang("zh")
	styles := NewStyles(80)
	form := newTelnetAddForm(styles, 80, 24, nil)
	_ = form.Init()

	// Submit with empty name and host
	form.submit()
	if form.err != i18n.T("telnet.err_name_host_req") {
		t.Fatalf("expected telnet error %q, got %q", i18n.T("telnet.err_name_host_req"), form.err)
	}
	if strings.Contains(form.err, "站点") {
		t.Fatalf("telnet error should not mention FTP site '站点': %s", form.err)
	}
}

// TestTelnetAddForm_SubmitStoresCredentials covers issue #46: submitting
// the form persists the user name to telnet.json and the password to the
// encrypted vault (never to the JSON file), and a rename migrates the vault
// entry so auto-login keeps working.
func TestTelnetAddForm_SubmitStoresCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	credential.ResetStoreForTest()
	t.Cleanup(credential.ResetStoreForTest)
	i18n.SetLang("en")

	form := newTelnetAddForm(NewStyles(100), 100, 30, nil)
	form.nameVal = "rpi"
	form.hostVal = "10.0.0.5"
	form.portVal = "23"
	form.userVal = "der"
	form.passwordVal = "s3cret"
	form.submit()

	if !form.done {
		t.Fatalf("submit did not complete: err=%q", form.err)
	}
	hosts, err := telnetconfig.Load()
	if err != nil || len(hosts) != 1 {
		t.Fatalf("Load = %d hosts, %v", len(hosts), err)
	}
	if hosts[0].User != "der" {
		t.Fatalf("User=%q want %q", hosts[0].User, "der")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "ctty", "telnet.json"))
	if err != nil {
		t.Fatalf("read telnet.json: %v", err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Fatal("password must never be written to telnet.json")
	}
	pass, ok := telnetcred.GetPassword("rpi")
	if !ok || pass != "s3cret" {
		t.Fatalf("vault password = %q,%v want %q,true", pass, ok, "s3cret")
	}

	// Editing with a rename keeps the password reachable under the new name.
	existing, _ := telnetconfig.Find("rpi")
	edit := newTelnetAddForm(NewStyles(100), 100, 30, &existing)
	if edit.passwordVal != "s3cret" {
		t.Fatalf("edit form password prefill=%q want the vault password", edit.passwordVal)
	}
	edit.nameVal = "rpi-board"
	edit.submit()
	if !edit.done {
		t.Fatalf("rename submit failed: err=%q", edit.err)
	}
	if pass, ok := telnetcred.GetPassword("rpi-board"); !ok || pass != "s3cret" {
		t.Fatalf("vault after rename = %q,%v want %q,true", pass, ok, "s3cret")
	}
	if _, ok := telnetcred.GetPassword("rpi"); ok {
		t.Fatal("old vault entry should be gone after a rename")
	}

	// Clearing the password removes the vault entry (password-only or
	// manual entry is a supported choice).
	renamed, _ := telnetconfig.Find("rpi-board")
	clear := newTelnetAddForm(NewStyles(100), 100, 30, &renamed)
	clear.passwordVal = ""
	clear.submit()
	if _, ok := telnetcred.GetPassword("rpi-board"); ok {
		t.Fatal("clearing the password should delete the vault entry")
	}
}

// TestTelnetAddForm_RenameHonoursPasswordEdits is a regression test: a
// rename used to overwrite the password field with the previously stored
// value, silently discarding both a rotated and a cleared password.
func TestTelnetAddForm_RenameHonoursPasswordEdits(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	credential.ResetStoreForTest()
	t.Cleanup(credential.ResetStoreForTest)

	add := newTelnetAddForm(NewStyles(100), 100, 30, nil)
	add.nameVal = "rpi"
	add.hostVal = "10.0.0.5"
	add.portVal = "23"
	add.userVal = "pi"
	add.passwordVal = "OLD-pass"
	add.submit()
	if !add.done {
		t.Fatalf("initial submit failed: %q", add.err)
	}

	// Rename and rotate the password at the same time.
	dev, _ := telnetconfig.Find("rpi")
	rotate := newTelnetAddForm(NewStyles(100), 100, 30, &dev)
	rotate.nameVal = "rpi2"
	rotate.passwordVal = "NEW-pass"
	rotate.submit()
	if !rotate.done {
		t.Fatalf("rename+rotate failed: %q", rotate.err)
	}
	if got, ok := telnetcred.GetPassword("rpi2"); !ok || got != "NEW-pass" {
		t.Fatalf("vault after rename+rotate = %q,%v want NEW-pass,true", got, ok)
	}
	if _, ok := telnetcred.GetPassword("rpi"); ok {
		t.Fatal("old vault key should be gone after a rename")
	}

	// Rename and clear: auto-login must be removable while renaming too.
	renamed, _ := telnetconfig.Find("rpi2")
	clear := newTelnetAddForm(NewStyles(100), 100, 30, &renamed)
	clear.nameVal = "rpi3"
	clear.passwordVal = ""
	clear.submit()
	if !clear.done {
		t.Fatalf("rename+clear failed: %q", clear.err)
	}
	if _, ok := telnetcred.GetPassword("rpi3"); ok {
		t.Fatal("clearing the password on rename must remove the vault entry")
	}
	if _, ok := telnetcred.GetPassword("rpi2"); ok {
		t.Fatal("old vault key should be gone after a rename+clear")
	}
	if _, ok := telnetconfig.Find("rpi3"); !ok {
		t.Fatal("renamed device should still be saved")
	}
}

// TestTelnetAddFormVaultFailureRollsBackConfig covers a vault that cannot be
// written: telnet.json must be restored so the user can fix the problem and
// save again instead of hitting "a telnet host named ... already exists".
func TestTelnetAddFormVaultFailureRollsBackConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	credential.ResetStoreForTest()
	t.Cleanup(credential.ResetStoreForTest)

	// Occupy the vault path with a directory so reads/writes of
	// credentials.json fail deterministically on every platform.
	vaultPath := filepath.Join(dir, "ctty", "credentials.json")
	if err := os.MkdirAll(vaultPath, 0700); err != nil {
		t.Fatalf("block vault: %v", err)
	}

	form := newTelnetAddForm(NewStyles(100), 100, 30, nil)
	form.nameVal = "rpi"
	form.hostVal = "10.0.0.5"
	form.portVal = "23"
	form.userVal = "pi"
	form.passwordVal = "s3cret"
	form.submit()

	if form.done {
		t.Fatal("a failed vault write must not complete the form")
	}
	if form.err == "" {
		t.Fatal("a failed vault write must be reported to the user")
	}
	hosts, err := telnetconfig.Load()
	if err != nil {
		t.Fatalf("Load after rollback: %v", err)
	}
	if len(hosts) != 0 {
		t.Fatalf("telnet.json was not rolled back: %d hosts saved", len(hosts))
	}

	// Repair the vault and submit again through the same form.
	if err := os.Remove(vaultPath); err != nil {
		t.Fatalf("repair vault: %v", err)
	}
	credential.ResetStoreForTest()
	form.submit()
	if !form.done {
		t.Fatalf("retry after vault repair failed: %q", form.err)
	}
	if form.err != "" {
		t.Fatalf("stale error after a successful retry: %q", form.err)
	}
	hosts, err = telnetconfig.Load()
	if err != nil || len(hosts) != 1 || hosts[0].Name != "rpi" {
		t.Fatalf("after retry hosts=%d err=%v", len(hosts), err)
	}
	if pass, ok := telnetcred.GetPassword("rpi"); !ok || pass != "s3cret" {
		t.Fatalf("after retry vault password=%q,%v want s3cret,true", pass, ok)
	}
}
