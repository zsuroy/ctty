package ui

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/telnetclient"
	"github.com/zsuroy/ctty/internal/telnetconfig"
)

// fakeConn only implements the calls the assistant uses; the embedded
// interface satisfies the rest of net.Conn.
type fakeConn struct {
	net.Conn
	writes [][]byte
	closed bool
}

func (p *fakeConn) Write(b []byte) (int, error) {
	p.writes = append(p.writes, append([]byte(nil), b...))
	return len(b), nil
}

func (p *fakeConn) Read(b []byte) (int, error) {
	return 0, io.EOF
}

func (p *fakeConn) Close() error {
	p.closed = true
	return nil
}

func (p *fakeConn) SetReadDeadline(time.Time) error { return nil }

func telnetNewTestMonitor(width, height int) *telnetMonitorModel {
	dev := telnetconfig.TelnetHost{
		Name: "console",
		Host: "10.0.0.5",
		Port: 23,
	}
	return newTelnetMonitor(NewStyles(width), width, height, dev)
}

// testQuickCmds mirrors the shape of the shipped defaults the tests were
// written against: text slots around one binary frame at index 5 (slot 5,
// the digit-5 hotkey). The telnet product defaults are pure ASCII, so tests
// that exercise a HEX slot pin this fixture instead of the real defaults.
func testQuickCmds() []telnetconfig.TelnetQuickCmd {
	return []telnetconfig.TelnetQuickCmd{
		{Label: "AT", Value: "AT"},
		{Label: "RST", Value: "AT+RST"},
		{Label: "ls", Value: "ls"},
		{Label: "ver", Value: "show version"},
		{Label: "MODBUS", Value: "01 03 00 00 00 01 84 0A", Hex: true},
		{Label: "pwd", Value: "pwd"},
		{Label: "stat", Value: "show status"},
		{Label: "exit", Value: "exit"},
	}
}

func TestTelnetMonitorTextSendAndHistoryNavigation(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port

	for _, text := range []string{"hello", "world"} {
		m.input.SetValue(text)
		m.sendCurrent()
	}
	// Each text line carries the NVT CRLF: "hello\r\n"+"world\r\n" = 14 bytes.
	if m.txCount != 14 || len(port.writes) != 2 {
		t.Fatalf("txCount=%d writes=%d, want 14/2", m.txCount, len(port.writes))
	}
	m.historyPrev()
	if got := m.input.Value(); got != "world" {
		t.Fatalf("first history prev = %q, want world", got)
	}
	m.historyPrev()
	if got := m.input.Value(); got != "hello" {
		t.Fatalf("second history prev = %q, want hello", got)
	}
	m.historyNext()
	if got := m.input.Value(); got != "world" {
		t.Fatalf("history next = %q, want world", got)
	}
	m.historyNext()
	if got := m.input.Value(); got != "" {
		t.Fatalf("history next at end = %q, want empty", got)
	}
}

func TestTelnetMonitorReceiveFormatting(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	m.appendRx([]byte("AB\n\x00C\n"))
	if len(m.rxLines) != 2 || m.rxLines[0] != "AB" || m.rxLines[1] != ".C" {
		t.Fatalf("ascii receive lines = %q", m.rxLines)
	}
	if m.rxCount != 6 {
		t.Fatalf("rxCount = %d, want 6", m.rxCount)
	}

	m.hexView = true
	m.showTime = true
	m.appendRx([]byte{0x01, 0xfe})
	display := m.rx.View()
	if !strings.Contains(display, "[") || !strings.Contains(display, "01 FE") {
		t.Fatalf("receive pane should show a timestamped hex line, got %q", display)
	}
	for _, c := range []byte(display) {
		if c > 0x7e {
			t.Fatalf("receive pane contains non-ASCII byte %d: %q", c, display)
			break
		}
	}
}

func TestTelnetMonitorTEMarkOnSend(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port
	m.input.SetValue("AT")
	m.sendCurrent()
	if len(m.rxLines) != 1 || m.rxLines[0] != "AT" {
		t.Fatalf("send should echo a plain TX line by default, got %q", m.rxLines)
	}
	m.showPrefix = true
	m.rebuildRx()
	if len(m.rxLines) != 1 || !strings.HasPrefix(m.rxLines[0], txMark) {
		t.Fatalf("send should echo a TX-marked line when the prefix is on, got %q", m.rxLines)
	}
	if !strings.Contains(m.rxLines[0], "AT") {
		t.Fatalf("TX line should carry payload, got %q", m.rxLines[0])
	}
}

func TestTelnetAltQuickIndex(t *testing.T) {
	for i := 1; i <= 8; i++ {
		idx, ok := telnetAltQuickIndex("alt+" + string(rune('0'+i)))
		if !ok || idx != i-1 {
			t.Fatalf("alt+%d => %d,%v", i, idx, ok)
		}
	}
	if _, ok := telnetAltQuickIndex("alt+9"); ok {
		t.Fatal("alt+9 must not map to a slot")
	}
}

func TestTelnetMonitorEditorToggleAndSave(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)

	// openEditor reloads slots from disk, so the fixture goes in after it.
	m.openEditor()
	m.quickCmds = testQuickCmds()
	m.clampQuick()
	if !m.quickEditing {
		t.Fatal("F9 should open the editor")
	}
	// F2 flips the slot's encoding — same key as the monitor's send mode —
	// and rewrites the text so the bytes it sends stay identical. The line
	// ending ASCII send appends has to be folded into the hex text, otherwise
	// the slot would send two fewer bytes after the switch.
	m.updateEditor(tea.KeyMsg{Type: tea.KeyF2})
	if !m.quickCmds[0].Hex || m.quickCmds[0].Value != "41 54 0D 0A" {
		t.Fatalf("to HEX = %+v, want hex text 41 54 0D 0A", m.quickCmds[0])
	}
	m.updateEditor(tea.KeyMsg{Type: tea.KeyF2})
	if m.quickCmds[0].Hex || m.quickCmds[0].Value != "AT" {
		t.Fatalf("to ASCII = %+v, want AT", m.quickCmds[0])
	}
	// Tab edits the label: it is a name, not a payload, so it has no mode.
	m.updateEditor(tea.KeyMsg{Type: tea.KeyTab})
	if !m.quickEditOn || !m.quickEditLabel {
		t.Fatal("Tab should start label editing")
	}
	m.updateEditor(tea.KeyMsg{Type: tea.KeyEsc})
	// Enter starts editing the value, typing replaces it, Enter commits.
	m.updateEditor(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.quickEditOn || m.quickEditLabel {
		t.Fatal("Enter should start value editing")
	}
	m.quickInput.SetValue("AT+NEW")
	m.updateEditor(tea.KeyMsg{Type: tea.KeyEnter})
	if m.quickCmds[0].Value != "AT+NEW" || m.quickEditOn {
		t.Fatalf("edit commit = %q, on=%v", m.quickCmds[0].Value, m.quickEditOn)
	}
	if got := m.quickInput.Value(); got != "" {
		t.Fatalf("quick input should be cleared after commit, got %q", got)
	}
	// Edits made here are live but unwritten; leaving must not silently throw
	// them away, so the first Esc asks and the second one obeys.
	m.updateEditor(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.quickEditing {
		t.Fatal("first Esc must not discard unsaved edits")
	}
	if !strings.Contains(m.quickMsg, i18n.T("telnet.quick.unsaved")) {
		t.Fatalf("status = %q, want the unsaved hint", m.quickMsg)
	}
	m.updateEditor(tea.KeyMsg{Type: tea.KeyEsc})
	if m.quickEditing {
		t.Fatal("second Esc should close the editor")
	}
}

// The editor shows the label and the value as two separate fields, and the
// ASCII/HEX tag belongs to the value only — a label is always plain text.
func TestTelnetQuickEditorSeparatesLabelAndValue(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = testQuickCmds()
	m.clampQuick()
	m.quickEditSel = 2 // "ls" slot: label and payload are identical text

	frame := m.editorView()
	labelName := i18n.T("telnet.quick.field_label")
	valueName := i18n.T("telnet.quick.field_value")
	var labelLine, valueLine string
	for _, ln := range strings.Split(frame, "\n") {
		if strings.Contains(ln, "Enter:") || strings.Contains(ln, "Ctrl+S:") {
			continue // help rows mention the field names too
		}
		if strings.Contains(ln, labelName) && labelLine == "" {
			labelLine = ln
		}
		if strings.Contains(ln, valueName) {
			valueLine = ln
		}
	}
	if labelLine == "" || valueLine == "" {
		t.Fatalf("editor must show both fields:\n%s", frame)
	}
	// The two halves are separate form fields: the label line shows only the
	// name, the value line shows only the payload text. Neither carries an
	// encoding tag any more — telnet sends everything as text.
	if strings.Contains(labelLine, "[HEX]") || strings.Contains(labelLine, "[ASCII]") ||
		strings.Contains(valueLine, "[HEX]") || strings.Contains(valueLine, "[ASCII]") {
		t.Fatalf("fields must not carry an encoding tag:\nlabel=%q\nvalue=%q", labelLine, valueLine)
	}
	if !strings.Contains(valueLine, "ls") {
		t.Fatalf("value should show the payload: %q", valueLine)
	}
}

// Toggling a text slot to HEX must keep the same bytes on the wire, and the
// live byte count must follow the draft while it is typed.
func TestTelnetQuickEditorEncodingRoundTrip(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = []telnetconfig.TelnetQuickCmd{{Label: "RST", Value: "AT+RST"}}
	m.clampQuick()
	m.quickEditSel = 0

	m.updateEditor(tea.KeyMsg{Type: tea.KeyF2})
	if !m.quickCmds[0].Hex || m.quickCmds[0].Value != "41 54 2B 52 53 54 0D 0A" {
		t.Fatalf("hex text = %+v", m.quickCmds[0])
	}
	b, err := m.quickCmds[0].Payload()
	if err != nil || string(b) != "AT+RST\r\n" {
		t.Fatalf("hex payload = %q,%v, want AT+RST+CRLF (the bytes ASCII send emits)", b, err)
	}
	m.updateEditor(tea.KeyMsg{Type: tea.KeyF2})
	if m.quickCmds[0].Hex || m.quickCmds[0].Value != "AT+RST" {
		t.Fatalf("ascii text = %+v", m.quickCmds[0])
	}
	// A slot whose value is empty sends its label; converting must preserve
	// that payload instead of turning the slot into an empty one.
	m.quickCmds = []telnetconfig.TelnetQuickCmd{{Label: "PING"}}
	m.quickEditSel = 0
	m.updateEditor(tea.KeyMsg{Type: tea.KeyF2})
	if !m.quickCmds[0].Hex {
		t.Fatal("slot should be HEX now")
	}
	if b, err := m.quickCmds[0].Payload(); err != nil || string(b) != "PING\r\n" {
		t.Fatalf("label fallback payload = %q,%v, want PING+CRLF", b, err)
	}
}

func TestTelnetQuickCmdPayloadIndependentHex(t *testing.T) {
	if b, err := (telnetconfig.TelnetQuickCmd{Value: "AB"}).Payload(); err != nil || string(b) != "AB" {
		t.Fatalf("ASCII slot AB = %q,%v — must not be hex-decoded", b, err)
	}
	if b, err := (telnetconfig.TelnetQuickCmd{Value: "AB", Hex: true}).Payload(); err != nil || len(b) != 1 || b[0] != 0xAB {
		t.Fatalf("HEX slot AB = %v,%v", b, err)
	}
	if n := (telnetconfig.TelnetQuickCmd{Value: "ZZ", Hex: true}).ByteCount(); n != -1 {
		t.Fatalf("malformed hex byte count = %d, want -1", n)
	}
}

func telnetEqualBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTelnetMonitorRepeatTick(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	m.setLineEnd("none")
	port := &fakeConn{}
	m.conn = port
	m.lastPayload = []byte("P")
	m.repeat = true
	m.interval = 100 * time.Millisecond

	updated, cmd := m.Update(telnetTickMsg{})
	if updated.txCount != 1 || len(port.writes) != 1 {
		t.Fatalf("repeat tick should send the last payload: %d/%d", updated.txCount, len(port.writes))
	}
	if cmd == nil {
		t.Fatal("repeat should schedule the next tick")
	}

	updated.repeat = false
	updated, cmd = updated.Update(telnetTickMsg{})
	if updated.txCount != 1 || cmd != nil {
		t.Fatal("stopped repeat should neither send nor schedule")
	}
}

func TestTelnetMonitorFrameFitsCanvas(t *testing.T) {
	dev := telnetconfig.TelnetHost{Name: "console", Host: "10.0.0.5"}
	for _, size := range []struct{ w, h int }{{80, 24}, {60, 20}, {120, 30}, {40, 15}} {
		m := newTelnetMonitor(NewStyles(size.w), size.w, size.h, dev)
		frame := RenderCanvas(size.w, size.h, m.View())
		lines := strings.Split(frame, "\n")
		if len(lines) > size.h {
			t.Fatalf("%dx%d: frame has %d rows", size.w, size.h, len(lines))
		}
		for _, line := range lines {
			if w := ansi.StringWidth(line); w > size.w {
				t.Fatalf("%dx%d: row width %d exceeds canvas", size.w, size.h, w)
			}
		}
	}
}

func TestTelnetMonitorTitleHasSingleCttyBrand(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	// This test asserts the Alt wording. Pin the resolved modifier: on macOS
	// "auto" resolves to Option, which legitimately renders "Option+" instead.
	m.quickKey = telnetconfig.QuickKeyAlt
	frame := RenderCanvas(80, 24, m.View())
	if !strings.Contains(frame, "[ctty]") {
		t.Fatal("telnet monitor should show the ctty badge")
	}
	if n := strings.Count(strings.ToLower(frame), "ctty"); n != 1 {
		t.Fatalf("telnet monitor should show ctty exactly once, got %d", n)
	}
	pageHint := strings.Contains(frame, "PAGE") || strings.Contains(frame, "翻页")
	if !strings.Contains(frame, "Alt+") || !pageHint || !strings.Contains(frame, "Esc:") {
		t.Fatal("telnet monitor help should advertise Alt shortcuts and quick-page switching")
	}
}

func TestTelnetConnectModeResolution(t *testing.T) {
	// CLI flag > device mode > global setting > per-entry default, mirrored
	// from telnetconfig.ResolveMode so the CLI and the TUI cannot disagree.
	dev := telnetconfig.TelnetHost{Name: "console", Host: "10.0.0.5"}

	m := Model{}
	if got := m.telnetConnectMode(dev); got != telnetconfig.ModeAssistant {
		t.Fatalf("TUI default = %q, want assistant", got)
	}
	m.telnetOnly = true
	if got := m.telnetConnectMode(dev); got != telnetconfig.ModeRaw {
		t.Fatalf("standalone default = %q, want raw", got)
	}
	m.appConfig = &config.AppConfig{TelnetStartMode: config.TelnetStartAssistant}
	if got := m.telnetConnectMode(dev); got != telnetconfig.ModeAssistant {
		t.Fatalf("global assistant = %q, want assistant", got)
	}
	m.appConfig.TelnetStartMode = config.TelnetStartRaw
	if got := m.telnetConnectMode(dev); got != telnetconfig.ModeRaw {
		t.Fatalf("global raw = %q, want raw", got)
	}
	dev.Mode = telnetconfig.ModeAssistant
	if got := m.telnetConnectMode(dev); got != telnetconfig.ModeAssistant {
		t.Fatalf("device mode should beat the global setting, got %q", got)
	}
	m.telnetForceMode = telnetconfig.ModeRaw
	if got := m.telnetConnectMode(dev); got != telnetconfig.ModeRaw {
		t.Fatalf("CLI override should beat the device mode, got %q", got)
	}
}

func TestTelnetConnectHonoursRawMode(t *testing.T) {
	// A raw-mode connection must hand the terminal to the passthrough bridge
	// instead of opening the monitor, without touching the port here.
	dev := telnetconfig.TelnetHost{Name: "console", Host: "10.0.0.5"}
	m := Model{styles: NewStyles(80), width: 80, height: 24, viewMode: ViewTelnet,
		appConfig: &config.AppConfig{TelnetStartMode: config.TelnetStartRaw}}
	updated, cmd := m.Update(telnetConnectMsg{host: dev})
	um := updated.(Model)
	if um.telnetMonitor != nil || um.viewMode == ViewTelnetMonitor {
		t.Fatal("raw mode must not open the assistant monitor")
	}
	if cmd == nil {
		t.Fatal("raw mode should start the passthrough command")
	}
}

func TestTelnetConnectEntersMonitorFromTUI(t *testing.T) {
	dev := telnetconfig.TelnetHost{Name: "console", Host: "10.0.0.5"}
	m := Model{styles: NewStyles(80), width: 80, height: 24, viewMode: ViewTelnet}
	updated, _ := m.Update(telnetConnectMsg{host: dev})
	um := updated.(Model)
	if um.viewMode != ViewTelnetMonitor || um.telnetMonitor == nil {
		t.Fatalf("TUI connect should open the assistant, got view %v", um.viewMode)
	}

	updated, _ = um.Update(telnetMonitorDoneMsg{})
	um = updated.(Model)
	if um.viewMode != ViewTelnet || um.telnetMonitor != nil || um.telnetForm == nil {
		t.Fatal("closing the assistant should return to the telnet list")
	}

	um.telnetMonitor = telnetNewTestMonitor(80, 24)
	updated, cmd := um.Update(telnetMonitorSwitchMsg{})
	um = updated.(Model)
	if um.viewMode != ViewList || cmd == nil {
		t.Fatal("passthrough switch should return to the list and start an Exec command")
	}
}

func TestTelnetOnlyEscFromMonitorQuitsProgram(t *testing.T) {
	// 'ctty telnet' starts directly in the assistant (telnetOnly). Esc on the
	// monitor must quit the whole program, not drop back to a device list the
	// user never opened. Regression: a missing telnetOnly check used to leave
	// the process hanging after Esc.
	m := Model{styles: NewStyles(80), width: 80, height: 24,
		viewMode: ViewTelnetMonitor, telnetOnly: true, telnetMonitor: telnetNewTestMonitor(80, 24)}
	updated, cmd := m.Update(telnetMonitorDoneMsg{})
	um := updated.(Model)
	if um.telnetMonitor != nil {
		t.Fatal("Esc must close the monitor before quitting")
	}
	if cmd == nil {
		t.Fatal("telnetOnly Esc must emit a quit command")
	}
	// The quit may travel inside a Batch (with mouse-report cleanup); what
	// matters is that one of the batched commands actually quits.
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		quits := false
		for _, c := range batch {
			if _, ok := c().(tea.QuitMsg); ok {
				quits = true
			}
		}
		if !quits {
			t.Fatal("telnetOnly Esc batch does not include a quit")
		}
		return
	}
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("telnetOnly Esc should quit, got %T", msg)
	}
}

func TestTelnetSettingsOpenEditAndApply(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	// F10 opens the settings page without touching the live session.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	if !m.settingsOpen {
		t.Fatal("F10 should open settings")
	}
	// Row 0 is the display mode (ASCII by default). Flip it in the draft only.
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if !m.prefs.HexView {
		t.Fatal("Right on display-mode row should draft HEX")
	}
	if m.hexView {
		t.Fatal("draft change must not affect the live session before save")
	}
	// Row 1 toggles the TX/RX prefix, row 2 the timestamp.
	for i := 0; i < telnetSettingShowTime; i++ {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if !m.prefs.ShowTime {
		t.Fatal("Right on timestamp row should enable it in the draft")
	}
	// Move to the last row (max buffer lines), step it up, then save.
	for m.settingsSel < telnetSettingMaxLines {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsSel != telnetSettingMaxLines {
		t.Fatalf("selection = %d, want maxLines %d", m.settingsSel, telnetSettingMaxLines)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if m.prefs.MaxLines != telnetconfig.DefaultMaxLines+100 {
		t.Fatalf("maxLines draft = %d, want %d", m.prefs.MaxLines, telnetconfig.DefaultMaxLines+100)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	if m.settingsOpen {
		t.Fatal("Enter should close settings")
	}
	if !m.hexView || !m.showTime || m.maxLines != m.prefs.MaxLines {
		t.Fatalf("saved prefs not applied: hexView=%v showTime=%v maxLines=%d", m.hexView, m.showTime, m.maxLines)
	}
	// Reload from disk to confirm persistence.
	got, err := telnetconfig.LoadTelnetPrefs()
	if err != nil {
		t.Fatalf("reload prefs: %v", err)
	}
	if !got.ShowTime || got.MaxLines != m.prefs.MaxLines {
		t.Fatalf("persisted prefs mismatch: %+v", got)
	}
}

func TestTelnetSettingsCancelLeavesSessionUntouched(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight}) // toggle HexView draft
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsOpen {
		t.Fatal("Esc should close settings")
	}
	if m.hexView || m.prefs.HexView {
		t.Fatal("Esc must discard the draft")
	}
	got, err := telnetconfig.LoadTelnetPrefs()
	if err != nil || got.HexView {
		t.Fatalf("Esc must not write the file: %+v err=%v", got, err)
	}
}

func TestTelnetSettingsBufferCapApplied(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.maxLines = 3
	m.appendRx([]byte("1\n2\n3\n4\n5\n"))
	if len(m.rxLines) != 3 || !strings.HasSuffix(m.rxLines[0], "3") {
		t.Fatalf("buffer cap not applied: %q", m.rxLines)
	}
}

func TestTelnetMonitorFollowPauseOnScroll(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.appendRx([]byte("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"))
	if !m.rxFollow {
		t.Fatal("new monitor should follow by default")
	}
	if !m.rx.AtBottom() {
		t.Fatal("following monitor should be at bottom")
	}

	// Scrolling up pauses auto-follow.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.rxFollow {
		t.Fatal("PgUp away from tail should pause follow")
	}
	yBefore := m.rx.YOffset

	// New data must NOT yank the view back to the bottom while paused.
	m.appendRx([]byte("line11\nline12\n"))
	if m.rx.YOffset != yBefore {
		t.Fatalf("paused view moved on new data: y=%d want %d", m.rx.YOffset, yBefore)
	}
	if m.rxFollow {
		t.Fatal("new data must not silently resume follow")
	}

	// End jumps to newest and resumes follow.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyEnd})
	if !m.rxFollow || !m.rx.AtBottom() {
		t.Fatal("End should resume follow and land at bottom")
	}
}

func TestTelnetMonitorClearResumesFollow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.appendRx([]byte("a\nb\nc\n"))
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyPgUp})
	m.rxFollow = false
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF4})
	if !m.rxFollow {
		t.Fatal("F4 clear should resume follow")
	}
	if len(m.rxLines) != 0 || m.rxCount != 0 {
		t.Fatalf("F4 should clear rx: lines=%v count=%d", m.rxLines, m.rxCount)
	}
}

func TestTelnetFitStatusRowTwoPanes(t *testing.T) {
	// Feedback must stay on the right and never overflow the pane width.
	row := telnetFitStatusRow("[LIVE] INPUT RX 0 TX 0 RX:ASCII TX:ASCII TIME:OFF LOOP:OFF 1s", "Saved and applied.", 70)
	if w := ansi.StringWidth(row); w != 70 {
		t.Fatalf("status row width %d, want 70: %q", w, row)
	}
	if !strings.HasSuffix(row, "Saved and applied.") {
		t.Fatalf("feedback not right-aligned: %q", row)
	}
	if !strings.HasPrefix(row, "[LIVE] INPUT") {
		t.Fatalf("left counters lost: %q", row)
	}
	// No feedback: plain state, still exact width.
	row2 := telnetFitStatusRow("[LIVE] INPUT RX 0", "", 40)
	if w := ansi.StringWidth(row2); w > 40 {
		t.Fatalf("no-feedback row overflow: %d", w)
	}
}

func TestTelnetQuickCmdLabelShownInTxLine(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}
	m.quickCmds = []telnetconfig.TelnetQuickCmd{
		{Label: "AT+RST", Value: "AT+RST"},
		{Label: "MODBUS", Value: "01 03", Hex: true},
	}
	m.fireQuick(0)
	if len(m.rxLines) != 1 || !strings.Contains(m.rxLines[0], "[AT+RST]") {
		t.Fatalf("quick label missing from TX line: %q", m.rxLines)
	}
	m.fireQuick(1)
	if len(m.rxLines) != 2 || !strings.Contains(m.rxLines[1], "[MODBUS]") {
		t.Fatalf("second quick label missing: %q", m.rxLines)
	}
}

func TestTelnetQuickLabelSwitchHidesBracketPrefix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}
	m.quickCmds = []telnetconfig.TelnetQuickCmd{{Label: "AT+RST", Value: "AT+RST"}}
	if !m.showQuickLabel {
		t.Fatal("quick-command labels must be on by default")
	}
	m.fireQuick(0)
	if !strings.Contains(m.rxLines[0], "[AT+RST]") {
		t.Fatalf("default TX line = %q, want [AT+RST]", m.rxLines)
	}

	m.showQuickLabel = false
	m.fireQuick(0)
	if strings.Contains(m.rxLines[1], "[AT+RST]") {
		t.Fatalf("TX line with label hidden = %q", m.rxLines[1])
	}
	if !strings.Contains(m.rxLines[1], "AT+RST") {
		t.Fatalf("payload itself was dropped: %q", m.rxLines[1])
	}
	// The switch redraws entries already on screen, like F1/F3 do.
	m.rebuildRx()
	for _, line := range m.rxLines {
		if strings.Contains(line, "[AT+RST]") {
			t.Fatalf("stale label survived rebuild: %q", m.rxLines)
		}
	}
}

func TestTelnetQuickLabelSettingAppliesAndPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	for i := 0; i < telnetSettingShowQuickLabel; i++ {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsSel != telnetSettingShowQuickLabel {
		t.Fatalf("selection = %d, want row %d", m.settingsSel, telnetSettingShowQuickLabel)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if m.prefs.ShowQuickLabel || !m.showQuickLabel {
		t.Fatalf("draft = %v, live = %v; draft must toggle off while live stays on",
			m.prefs.ShowQuickLabel, m.showQuickLabel)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	got, err := telnetconfig.LoadTelnetPrefs()
	if err != nil || got.ShowQuickLabel {
		t.Fatalf("persisted show_quick_label = %v err=%v, want false", got.ShowQuickLabel, err)
	}
	if m.showQuickLabel {
		t.Fatal("applied quick-label switch must turn it off")
	}
}

func TestTelnetEmptyHintAndHistPreview(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dev := telnetconfig.TelnetHost{Name: "console", Host: "10.0.0.5"}
	m := newTelnetMonitor(NewStyles(80), 80, 24, dev)
	m.conn = &fakeConn{}
	// Empty receive pane shows the guidance hint.
	frame := RenderCanvas(80, 24, m.View())
	if !strings.Contains(frame, "Waiting") && !strings.Contains(frame, "等待") {
		t.Fatal("empty receive pane should show guidance")
	}
	// History position appears while browsing and is bounded in width.
	m.history = []string{"AT", "AT+GMR", "AT+RST"}
	m.histIdx = 1
	m.input.SetValue("AT+GMR")
	box := m.sendBoxContent(70)
	if !strings.Contains(box, "HIST 2/3") && !strings.Contains(box, "历史 2/3") {
		t.Fatalf("history hint missing: %q", box)
	}
	if w := ansi.StringWidth(box); w != 70 {
		t.Fatalf("send box width %d, want 70: %q", w, box)
	}
}

func telnetMakeQuickLabels(n int) []telnetconfig.TelnetQuickCmd {
	cmds := make([]telnetconfig.TelnetQuickCmd, n)
	for i := range cmds {
		cmds[i] = telnetconfig.TelnetQuickCmd{Label: fmt.Sprintf("Q%d", i+1), Value: fmt.Sprintf("V%d", i+1)}
	}
	return cmds
}

func TestTelnetQuickPagingNavigation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	// Pin Alt mode: this test asserts Ctrl+arrow is ignored when the page key
	// is Alt. On macOS the default resolves to Option, where ctrl+arrow IS the
	// Option+arrow encoding and must page (covered elsewhere).
	m.quickKey = telnetconfig.QuickKeyAlt
	m.quickCmds = telnetMakeQuickLabels(20) // 3 pages: 0-7, 8-15, 16-19
	m.clampQuick()

	if got := m.quickPageCount(); got != 3 {
		t.Fatalf("page count = %d, want 3", got)
	}
	if m.quickPage != 0 || m.quickSel != 0 {
		t.Fatalf("initial page/sel = %d/%d", m.quickPage, m.quickSel)
	}

	// Enter quick-focus mode, then page right: selection should land on the
	// same column of the next page (slot 0 -> 8).
	m.quickFocus = true
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRight})
	if m.quickPage != 1 || m.quickSel != 8 {
		t.Fatalf("after right: page/sel = %d/%d, want 1/8", m.quickPage, m.quickSel)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRight})
	if m.quickPage != 2 || m.quickSel != 16 {
		t.Fatalf("after 2nd right: page/sel = %d/%d, want 2/16", m.quickPage, m.quickSel)
	}
	// Wrapping forward from the last page goes to page 0.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRight})
	if m.quickPage != 0 {
		t.Fatalf("wrap right page = %d, want 0", m.quickPage)
	}
	// Wrapping backward from page 0 goes to the last page, same column (slot
	// 16).
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyLeft})
	if m.quickPage != 2 || m.quickSel != 16 {
		t.Fatalf("wrap left: page/sel = %d/%d, want 2/16", m.quickPage, m.quickSel)
	}

	// Up/down move the selection globally and auto-sync the page.
	m.setQuickSel(7)
	m.quickPage = 0
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyDown}) // 7 -> 8, page -> 1
	if m.quickSel != 8 || m.quickPage != 1 {
		t.Fatalf("down across boundary: page/sel = %d/%d, want 1/8", m.quickPage, m.quickSel)
	}

	// Alt+Left/Right pages from input focus too; plain arrows stay with the
	// text input and must not page while the user is editing a payload.
	m.quickFocus = false
	m.setQuickSel(0)
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.quickPage != 1 || m.quickSel != 8 {
		t.Fatalf("Alt+Right: page/sel = %d/%d, want 1/8", m.quickPage, m.quickSel)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	if m.quickPage != 0 || m.quickSel != 0 {
		t.Fatalf("Alt+Left: page/sel = %d/%d, want 0/0", m.quickPage, m.quickSel)
	}
	// With the default Alt setting, Ctrl arrows do nothing; plain arrows stay
	// with the text cursor while the input has focus.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if m.quickPage != 0 {
		t.Fatalf("Ctrl+Right must be ignored with page key=alt, got page %d", m.quickPage)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRight})
	if m.quickPage != 0 {
		t.Fatalf("plain Right in input focus must not page, got page %d", m.quickPage)
	}
}

func TestTelnetQuickPageKeySwitch(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	// Pin Alt mode: the baseline here is "Ctrl ignored, Alt pages". On macOS
	// the default resolves to Option, where ctrl+arrow is a real Option+arrow
	// and pages regardless of this setting.
	m.quickKey = telnetconfig.QuickKeyAlt
	m.quickCmds = telnetMakeQuickLabels(20)
	m.clampQuick()
	m.quickFocus = false

	// Default: Alt pages, Ctrl ignored, help advertises Alt.
	if m.pageKey != "alt" {
		t.Fatalf("default page key = %q, want alt", m.pageKey)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.quickPage != 1 {
		t.Fatalf("Alt+Right with page key=alt should page, got %d", m.quickPage)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if m.quickPage != 1 {
		t.Fatalf("Ctrl+Left with page key=alt should be ignored, got %d", m.quickPage)
	}
	if frame := RenderCanvas(80, 24, m.View()); !strings.Contains(frame, "Alt+←/→") {
		t.Fatal("help should show Alt page hint with page key=alt")
	}

	// Toggle the setting via F10 to Ctrl, save, then only Ctrl pages.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	for i := 0; i < telnetSettingPageKey; i++ {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsSel != telnetSettingPageKey {
		t.Fatalf("selection = %d, want %d", m.settingsSel, telnetSettingPageKey)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if m.prefs.PageKey != "ctrl" {
		t.Fatalf("page key draft = %q, want ctrl", m.prefs.PageKey)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	if m.pageKey != "ctrl" {
		t.Fatalf("applied page key = %q, want ctrl", m.pageKey)
	}
	// Persisted across a reload.
	got, err := telnetconfig.LoadTelnetPrefs()
	if err != nil || got.PageKey != "ctrl" {
		t.Fatalf("persisted page key = %q, err=%v, want ctrl", got.PageKey, err)
	}

	m.setQuickSel(0)
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.quickPage != 0 {
		t.Fatalf("Alt+Right with page key=ctrl should be ignored, got %d", m.quickPage)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if m.quickPage != 1 {
		t.Fatalf("Ctrl+Right with page key=ctrl should page, got %d", m.quickPage)
	}
	if frame := RenderCanvas(80, 24, m.View()); !strings.Contains(frame, "Ctrl+←/→") {
		t.Fatal("help should show Ctrl page hint after switching")
	}
}

func TestTelnetAltQuickFiresCurrentPage(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port
	m.quickCmds = telnetMakeQuickLabels(20)
	m.clampQuick()

	checkLastWrite := func(want string) {
		t.Helper()
		if len(port.writes) == 0 {
			t.Fatal("no write recorded")
		}
		last := string(port.writes[len(port.writes)-1])
		if last != want {
			t.Fatalf("last write = %q, want %q", last, want)
		}
	}

	// Alt+1 on page 0 fires slot 0 (V1) with the NVT CRLF.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	checkLastWrite("V1\r\n")

	m.setQuickSel(8) // page 1
	// Alt+1 on page 1 fires slot 8 (V9).
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	checkLastWrite("V9\r\n")
}

func TestTelnetQuickBarPageLocalNumbersAndBlankSlots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.quickCmds = telnetMakeQuickLabels(20) // 3 pages: 8 + 8 + 4
	m.clampQuick()

	const inner = 76
	cellW := inner / quickCmdCols

	// Every page numbers its slots [1]..[8] to match Alt+1..8; no global [9].
	rows := m.quickBarRows(inner)
	bar := rows[0] + rows[1]
	for n := 1; n <= 8; n++ {
		if !strings.Contains(bar, fmt.Sprintf("[%d]", n)) {
			t.Fatalf("page 0 missing [%d]: %q", n, bar)
		}
	}
	if strings.Contains(bar, "[9]") {
		t.Fatalf("page 0 must not show global [9]: %q", bar)
	}

	// Last page has 4 real commands (17..20): badges [1]..[4], rest blank.
	m.setQuickSel(16)
	rows = m.quickBarRows(inner)
	bar = rows[0] + rows[1]
	for n := 1; n <= 4; n++ {
		if !strings.Contains(bar, fmt.Sprintf("[%d]", n)) {
			t.Fatalf("last page missing [%d]: %q", n, bar)
		}
	}
	if strings.Contains(bar, "[5]") || strings.Contains(bar, "[9]") {
		t.Fatalf("last page must not number empty slots: %q", bar)
	}
	row1 := rows[1]
	for c := 4; c < quickCmdCols; c++ {
		cell := row1[c*cellW : (c+1)*cellW]
		if strings.TrimSpace(cell) != "" {
			t.Fatalf("empty slot %d should be blank, got %q", c, cell)
		}
	}
}

func TestTelnetQuickEditorAddDelete(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.quickCmds = telnetMakeQuickLabels(3)
	m.clampQuick()
	m.openEditor()
	m.quickCmds = telnetMakeQuickLabels(3) // openEditor reloads from disk (defaults)
	m.clampQuick()

	// 'n' appends a blank slot and enters label editing.
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if len(m.quickCmds) != 4 { // 3 seeded + 1
		t.Fatalf("after add len = %d, want 4", len(m.quickCmds))
	}
	if !m.quickEditOn || !m.quickEditLabel {
		t.Fatal("n should enter label edit mode")
	}
	// Type a label, Tab to value, type value, Enter to commit.
	for _, r := range "PING" {
		m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'8'}})
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyEnter})
	last := m.quickCmds[len(m.quickCmds)-1]
	if last.Label != "PING" || last.Value != "8" {
		t.Fatalf("added cmd = %+v, want label=PING value=8", last)
	}
	if got := m.quickInput.Value(); got != "" {
		t.Fatalf("quick input should be cleared after added cmd is committed, got %q", got)
	}
	if got := m.quickLabelIn.Value(); got != "" {
		t.Fatalf("label input should be cleared too, got %q", got)
	}
	if h := lipgloss.Height(m.editorView()); h > 24 {
		t.Fatalf("editor view should stay within 24 rows, got %d", h)
	}

	// 'x' deletes the selected slot.
	before := len(m.quickCmds)
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if len(m.quickCmds) != before-1 {
		t.Fatalf("after delete len = %d, want %d", len(m.quickCmds), before-1)
	}
}

func TestTelnetQuickEditorWindowScroll(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.quickCmds = telnetMakeQuickLabels(30)
	m.clampQuick()
	m.openEditor()
	m.quickCmds = telnetMakeQuickLabels(30) // reload from disk overrides; set again
	m.quickEditSel = 0
	m.quickEditTop = 0
	m.clampEditor()

	vis := m.editorListRows()
	if vis >= 30 {
		t.Fatalf("window should be smaller than list: vis=%d", vis)
	}
	// Move selection past the visible window; top should scroll to keep it in view.
	for i := 0; i < vis+3; i++ {
		m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.quickEditSel < vis {
		t.Fatalf("selection did not advance: %d", m.quickEditSel)
	}
	if m.quickEditSel >= m.quickEditTop+vis || m.quickEditSel < m.quickEditTop {
		t.Fatalf("selection outside window: sel=%d top=%d vis=%d", m.quickEditSel, m.quickEditTop, vis)
	}
}

// --- Round 6: CRLF / log save / RX rate / search highlight / loop-Enter ---

func TestTelnetRawRxLogToggleWritesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	m := telnetNewTestMonitor(80, 24)

	matches, _ := filepath.Glob(filepath.Join(home, "ctty-telnet-*.log"))
	if len(matches) != 0 {
		t.Fatalf("stale log files: %v", matches)
	}
	m.toggleRxLog()
	if !m.logRecording {
		t.Fatal("toggle should start raw recording")
	}
	m.appendRx([]byte("hello\n"))
	m.toggleRxLog()
	if m.logRecording {
		t.Fatal("second toggle should stop recording")
	}
	matches, _ = filepath.Glob(filepath.Join(home, "ctty-telnet-*.log"))
	if len(matches) != 1 {
		t.Fatalf("log files = %v, want 1", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("raw log = %q, want %q", data, "hello\n")
	}
	if strings.Contains(m.status, "%!") || !strings.Contains(m.status, matches[0]) {
		t.Fatalf("log status mismatch: %q", m.status)
	}
}

func TestTelnetSearchHighlightWidthNeutral(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	m.searchQuery = "AT"
	lines := []string{"TX AT+RST ok", "nothing here", "AT again"}
	out := m.highlightLines(lines)
	// Matched lines gain zero-width ANSI codes but keep display width.
	for i := range lines {
		if got, want := ansi.StringWidth(out[i]), ansi.StringWidth(lines[i]); got != want {
			t.Fatalf("line %d width = %d, want %d: %q", i, got, want, out[i])
		}
	}
	if !strings.Contains(out[0], "\x1b[7m") {
		t.Fatalf("match not highlighted: %q", out[0])
	}
	if strings.Contains(out[1], "\x1b[7m") {
		t.Fatalf("non-match line highlighted: %q", out[1])
	}
	// Empty query is a no-op.
	m.searchQuery = ""
	if got := m.highlightLines(lines); len(got) != len(lines) || got[0] != lines[0] {
		t.Fatalf("empty query altered lines: %v", got)
	}
}

func TestTelnetRXRateAccumulates(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	m.appendRx([]byte("hello"))
	if m.rxRateBytes != 5 {
		t.Fatalf("rxRateBytes = %d, want 5", m.rxRateBytes)
	}
	m.appendRx([]byte("world!"))
	if m.rxRateBytes != 11 {
		t.Fatalf("rxRateBytes = %d, want 11", m.rxRateBytes)
	}
}

func TestTelnetMonitorSearchJump(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	m.rxLines = []string{"p0", "AT first", "p2", "p3", "p4", "p5", "p6", "p7", "AT second", "p9"}
	m.searchQuery = "AT"
	m.refreshRxContent()
	if len(m.searchMatches) != 2 {
		t.Fatalf("matches = %v, want 2 entries", m.searchMatches)
	}
	m.jumpSearch(1)
	if !strings.Contains(m.status, "2/2") {
		t.Fatalf("jump status = %q, want 2/2", m.status)
	}
	if m.rx.YOffset == 0 {
		t.Fatal("jump should scroll to the later match")
	}
}

func TestTelnetMonitorHexAsciiView(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	m.showTime = false
	m.hexAscii = true
	m.appendRx([]byte{0x41, 0x0d, 0x0a, 0x42, 0x43, 0x44, 0x45, 0x46, 0x0d, 0x0a})
	// Entries follow device lines in every view, so the LF closes the first
	// row; the offsets keep counting across rows.
	if len(m.rxLines) != 2 {
		t.Fatalf("dual-view lines = %v, want one row per received line", m.rxLines)
	}
	for _, want := range []string{"0000", "41 0D 0A", "|A..|"} {
		if !strings.Contains(m.rxLines[0], want) {
			t.Fatalf("dual-view line %q missing %q", m.rxLines[0], want)
		}
	}
	for _, want := range []string{"0003", "42 43 44 45 46 0D 0A", "|BCDEF..|"} {
		if !strings.Contains(m.rxLines[1], want) {
			t.Fatalf("dual-view line %q missing %q", m.rxLines[1], want)
		}
	}
}

func TestTelnetMonitorReceiveRowsAt80x24(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	if m.rx.Height < 6 {
		t.Fatalf("compact rx height = %d, want at least 6", m.rx.Height)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF12})
	if !m.helpExpanded {
		t.Fatal("F12 should expand help")
	}
	if m.rx.Height < 4 {
		t.Fatalf("expanded-help rx height = %d, want at least 4", m.rx.Height)
	}
	if h := lipgloss.Height(m.View()); h > 24 {
		t.Fatalf("expanded frame height = %d, want <=24", h)
	}
}

func TestTelnetEscChainFromMonitorExitsProgram(t *testing.T) {
	// Full exit path in normal mode (entered from the main TUI): Esc on the
	// monitor disconnects back to the telnet device list, Esc there returns
	// to the main list, and Esc on the main list finally quits the program.
	// A dead key anywhere in this chain strands the process with no way out
	// from the keyboard, which users experience as a "crash on exit".
	cfg := &config.AppConfig{KeyBindings: config.KeyBindings{QuitKeys: []string{"q", "ctrl+c"}}}
	m := Model{styles: NewStyles(80), width: 80, height: 24, appConfig: cfg,
		viewMode: ViewTelnetMonitor, telnetMonitor: telnetNewTestMonitor(80, 24)}

	// 1. Esc on the monitor closes the port and emits the done message.
	_, doneCmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if doneCmd == nil {
		t.Fatal("Esc on monitor must emit the disconnect command")
	}
	// 2. Processing it returns to the telnet device list.
	m2, _ := m.Update(doneCmd())
	um := m2.(Model)
	if um.telnetMonitor != nil || um.viewMode != ViewTelnet {
		t.Fatalf("disconnect must land on the telnet list, got mode %v monitor=%v", um.viewMode, um.telnetMonitor != nil)
	}
	// 3. Esc on the telnet device list returns to the main list.
	_, leaveCmd := um.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if leaveCmd == nil {
		t.Fatal("Esc on telnet list must emit the leave command")
	}
	m3, _ := um.Update(leaveCmd())
	um2 := m3.(Model)
	if um2.viewMode != ViewList {
		t.Fatalf("leaving telnet must land on the main list, got %v", um2.viewMode)
	}
	// 4. Esc on the main list quits the program.
	_, quitCmd := um2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if quitCmd == nil {
		t.Fatal("Esc on main list must quit; a dead key here strands the process")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("Esc on main list should quit, got %T", quitCmd())
	}
}

// fakeBusyConn mimics a telnet host whose Read is parked — the exact state
// a real connection is in when the user hits Esc while the remote is silent.
type fakeBusyConn struct {
	net.Conn
	entered     chan struct{}
	release     chan struct{}
	closeCalled chan struct{}
}

func (p *fakeBusyConn) Read(b []byte) (int, error) {
	close(p.entered)
	<-p.release
	return 0, fmt.Errorf("device gone")
}

func (p *fakeBusyConn) Close() error {
	close(p.closeCalled)
	return nil
}

func (p *fakeBusyConn) SetReadDeadline(time.Time) error { return nil }

func TestClosePortUnblocksInFlightRead(t *testing.T) {
	// closePort runs on the UI goroutine while telnetReadCmd's read sits in
	// flight. A net.Conn must let Close proceed without waiting for (or
	// deadlocking behind) that read — the mirror image of the serial
	// assistant's Close-vs-Read regression, solved by the net.Conn contract
	// instead of an I/O lock.
	m := telnetNewTestMonitor(80, 24)
	fp := &fakeBusyConn{
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
		closeCalled: make(chan struct{}),
	}
	m.conn = fp
	m.session = telnetclient.NewSession(fp)

	cmd := telnetReadCmd(fp, m.session, m.auto)
	msgCh := make(chan tea.Msg, 1)
	go func() { msgCh <- cmd() }()
	<-fp.entered // the read is now parked in the fake connection

	done := make(chan struct{})
	go func() { m.closePort(); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closePort deadlocked behind the in-flight read")
	}
	select {
	case <-fp.closeCalled:
	default:
		t.Fatal("connection was never closed")
	}
	// The parked read finishes and hands back a read error, which Update
	// must swallow silently because the session is already closed.
	close(fp.release)
	select {
	case msg := <-msgCh:
		if _, ok := msg.(telnetReadErrMsg); !ok {
			t.Fatalf("parked read should surface a read error, got %T", msg)
		}
		um, cmd := m.Update(msg)
		if um != m {
			t.Fatal("Update should return the same model")
		}
		if cmd != nil {
			t.Fatal("a read error after an intentional close must not re-fire disconnect")
		}
	case <-time.After(time.Second):
		t.Fatal("parked read never returned")
	}
}

func TestTelnetMonitorFrameStaysAlignedWithMixedContent(t *testing.T) {
	// The 80x24 frame is the hard budget: after any mix of receives, TX
	// echoes, mode switches and hints, every painted row must be exactly 80
	// cells and the frame exactly 24 rows — one overflowing row pushes the
	// borders apart and the layout collapses.
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(path, []byte{0x01, 0x02, 0x03}, 0644); err != nil {
		t.Fatal(err)
	}

	for round := 0; round < 4; round++ {
		m := telnetNewTestMonitor(80, 24)
		m.conn = &fakeConn{}
		m.appendRx([]byte("hello world\r\nsecond line\n\x01\x02bin"))
		m.input.SetValue("AT+RST?")
		m.sendCurrent()
		m.input.SetValue(path) // file-send hint sits in the send row
		switch round {
		case 1:
			m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1}) // HEX
		case 2:
			m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1}) // HEX+ASCII
			m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF3}) // +timestamps
		case 3:
			m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF3}) // timestamps only
		}
		view := strings.Split(m.View(), "\n")
		if len(view) != 24 {
			t.Fatalf("round %d: frame has %d rows, want 24", round, len(view))
		}
		for i, ln := range view {
			if w := ansi.StringWidth(ln); w != 80 {
				t.Fatalf("round %d row %d: width %d, want 80\n%s", round, i, w, strings.Join(view, "\n"))
			}
		}
		if !strings.Contains(view[len(view)-1], "╯") {
			t.Fatalf("round %d: bottom border missing", round)
		}
	}
}

func TestTelnetMonitorQuestionMarkIsPayloadNotHelp(t *testing.T) {
	// "?" is a payload character in a telnet assistant (AT+GMR?, AT+BAUD?),
	// so it must reach the send box. Help lives on F12 instead.
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port

	for _, r := range "AT+GMR?" {
		m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.helpExpanded {
		t.Fatal("typing ? must not toggle help")
	}
	if got := m.input.Value(); got != "AT+GMR?" {
		t.Fatalf("input = %q, want %q", got, "AT+GMR?")
	}
	m.sendCurrent()
	if len(port.writes) != 1 || !strings.Contains(string(port.writes[0]), "AT+GMR?") {
		t.Fatalf("writes = %q, want an AT+GMR? payload", port.writes)
	}
}

// telnetFillRx pushes n newline-terminated lines through the normal receive path.
func telnetFillRx(m *telnetMonitorModel, from, to int) {
	var buf []byte
	for i := from; i < to; i++ {
		buf = append(buf, []byte(fmt.Sprintf("line %04d\r\n", i))...)
	}
	m.appendRx(buf)
}

// The complaint this covers: once the log outgrew the pane, scrolling felt
// dead because plain Up/Down recall send history. Page keys, wheel and
// Alt+arrows all have to move the viewport, and new bytes must not drag the
// reader back to the tail.
func TestTelnetMonitorScrollKeysMoveAndHold(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}

	telnetFillRx(m, 0, 200)
	if !m.rxFollow {
		t.Fatal("a live pane should follow the tail")
	}
	bottom := m.rx.YOffset

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.rxFollow {
		t.Fatal("PgUp should park the view")
	}
	if m.rx.YOffset >= bottom {
		t.Fatalf("PgUp did not scroll up: %d >= %d", m.rx.YOffset, bottom)
	}
	parked := m.rx.YOffset

	// Incoming data while parked: no yank, but the count is tracked.
	telnetFillRx(m, 200, 210)
	if m.rx.YOffset != parked {
		t.Fatalf("new data moved a parked viewport: %d != %d", m.rx.YOffset, parked)
	}
	if m.unseen == 0 {
		t.Fatal("parked view should count lines that arrived")
	}

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyEnd})
	if !m.rxFollow || m.unseen != 0 || !m.rx.AtBottom() {
		t.Fatalf("End should resume follow: follow=%v unseen=%d", m.rxFollow, m.unseen)
	}
}

func TestTelnetMonitorWheelScrollsReceivePane(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}
	telnetFillRx(m, 0, 200)
	bottom := m.rx.YOffset

	m, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if m.rxFollow || m.rx.YOffset != bottom-3 {
		t.Fatalf("wheel up: follow=%v offset=%d want %d", m.rxFollow, m.rx.YOffset, bottom-3)
	}
	m, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.rx.YOffset != bottom {
		t.Fatalf("wheel down should come back: offset=%d want %d", m.rx.YOffset, bottom)
	}
	if !m.rxFollow {
		t.Fatal("wheel back to the bottom should re-arm follow")
	}
}

func TestTelnetMonitorAltArrowsScrollOneLine(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}
	telnetFillRx(m, 0, 200)
	bottom := m.rx.YOffset

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	if m.rxFollow || m.rx.YOffset != bottom-1 {
		t.Fatalf("alt+up: follow=%v offset=%d want %d", m.rxFollow, m.rx.YOffset, bottom-1)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	if !m.rxFollow || m.rx.YOffset != bottom {
		t.Fatalf("alt+down: follow=%v offset=%d want %d", m.rxFollow, m.rx.YOffset, bottom)
	}
}

// Tab moves focus to the quick bar; before this change that focus ate
// PgUp/PgDn, so a user with saved quick commands could not scroll the log at
// all. Scrolling must not depend on which widget owns focus.
func TestTelnetMonitorScrollWorksWithQuickBarFocused(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}
	m.quickCmds = []telnetconfig.TelnetQuickCmd{{Label: "AT", Value: "AT"}, {Label: "GMR", Value: "AT+GMR"}}
	telnetFillRx(m, 0, 200)
	bottom := m.rx.YOffset

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyTab})
	if !m.quickFocus {
		t.Fatal("Tab should focus the quick bar")
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.rx.YOffset >= bottom {
		t.Fatalf("PgUp ignored while quick bar focused: %d >= %d", m.rx.YOffset, bottom)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyEnd})
	if !m.rxFollow || m.rx.YOffset != bottom {
		t.Fatalf("End should jump live from quick focus: follow=%v offset=%d want %d", m.rxFollow, m.rx.YOffset, bottom)
	}
}

// Adding a slot with "N" leaves the user on the label field; before this fix
// such a command fired zero bytes and echoed a bare "[label]" tag, which read
// as a rendering bug next to the built-in slots that show their hex bytes.
func TestTelnetMonitorLabelOnlyQuickCommandSendsLabel(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port

	m.openEditor()
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	// "n" lands on the label field; both fields are live inputs now.
	m.quickLabelIn.SetValue("AT+CFUN=1")
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyEnter})
	m.quickEditing = false

	idx := len(m.quickCmds) - 1
	if m.quickCmds[idx].Value != "" {
		t.Fatalf("precondition: value should still be empty, got %q", m.quickCmds[idx].Value)
	}
	m.fireQuick(idx)

	if len(port.writes) != 1 || string(port.writes[0]) != "AT+CFUN=1\r\n" {
		t.Fatalf("writes = %q, want the label plus CRLF", port.writes)
	}
	last := m.rxEntries[len(m.rxEntries)-1]
	if !last.tx || string(last.raw) != "AT+CFUN=1\r\n" {
		t.Fatalf("tx echo raw = %q, want the label bytes plus CRLF", last.raw)
	}
}

func TestTelnetMonitorEmptyQuickCommandSendsNothing(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port
	m.quickCmds = []telnetconfig.TelnetQuickCmd{{}}

	m.fireQuick(0)
	if len(port.writes) != 0 {
		t.Fatalf("blank slot must not write: %q", port.writes)
	}
	if m.status != i18n.T("telnet.quick.no_payload") {
		t.Fatalf("status = %q, want the empty-command hint", m.status)
	}
}

// Switching to HEX send and back must be visible in the log. The ASCII view
// used to dump the whole payload as hex whenever a single control byte showed
// up — and the default CRLF suffix guaranteed one — so every send looked like
// the mode switch had been ignored.
func TestTelnetMonitorAsciiViewEchoesTextNotHex(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}
	m.setLineEnd("none")
	m.hexSend = true

	m.input.SetValue("41 42 0D 0A")
	m.sendCurrent()
	last := m.rxEntries[len(m.rxEntries)-1]
	row := m.renderTxEntry(last)[0]
	if !strings.Contains(row, "AB\\r\\n") {
		t.Fatalf("ascii-view echo = %q, want decoded text with escapes", row)
	}
	if strings.Contains(row, "41 42") {
		t.Fatalf("ascii-view echo fell back to hex: %q", row)
	}

	// UTF-8 payloads stay readable too.
	m.hexSend = false
	m.input.SetValue("中文测试")
	m.sendCurrent()
	last = m.rxEntries[len(m.rxEntries)-1]
	if row := m.renderTxEntry(last)[0]; !strings.Contains(row, "中文测试") {
		t.Fatalf("cjk echo = %q, want the text", row)
	}
}

// A CRLF the user typed as hex is part of the payload, so the ASCII echo must
// not silently trim it back off.
func TestTelnetMonitorAsciiEchoKeepsTypedControlBytes(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}
	m.setLineEnd("crlf")
	m.hexSend = true

	m.input.SetValue("41 42 0D 0A")
	m.sendCurrent()
	last := m.rxEntries[len(m.rxEntries)-1]
	if row := m.renderTxEntry(last)[0]; !strings.HasSuffix(row, "AB\\r\\n") {
		t.Fatalf("echo = %q, want the typed CRLF kept as escapes", row)
	}

	// Plain ASCII send, no line-ending noise in the echo.
	m.hexSend = false
	m.input.SetValue("AT+GMR")
	m.sendCurrent()
	last = m.rxEntries[len(m.rxEntries)-1]
	if row := m.renderTxEntry(last)[0]; !strings.HasSuffix(row, "AT+GMR") {
		t.Fatalf("echo = %q, want bare AT+GMR", row)
	}
}

func TestTelnetMonitorHexSendRejectsTextWithHint(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port
	m.hexSend = true

	m.input.SetValue("AT+GMR")
	m.sendCurrent()
	if len(port.writes) != 0 {
		t.Fatalf("invalid hex must not reach the port: %q", port.writes)
	}
	if m.status != i18n.T("telnet.monitor.badhex_hint") {
		t.Fatalf("status = %q, want the hex hint", m.status)
	}
}

// The ASCII view groups lines by LF and used to throw the CRLF away. Those
// bytes are real: switching to HEX then showed "41 54" for a frame that was
// actually "41 54 0D 0A", so the pane disagreed with the RX counter.
func TestTelnetMonitorQuickSlotSendsWithLineEnding(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port
	m.setLineEnd("crlf")
	m.quickCmds = testQuickCmds()

	m.fireQuick(0) // "AT" slot
	if len(port.writes) != 1 {
		t.Fatalf("writes = % X", port.writes)
	}
	want := "AT\r\n"
	if string(port.writes[0]) != want {
		t.Fatalf("wire = %q, want %q", port.writes[0], want)
	}
	if m.txCount != len(want) {
		t.Fatalf("txCount = %d, want %d", m.txCount, len(want))
	}

	m.hexView = true
	m.rebuildRx()
	row := m.rxLines[len(m.rxLines)-1]
	if !strings.Contains(row, "41 54") {
		t.Fatalf("echo = %q", row)
	}
	if !strings.Contains(row, "0D 0A") {
		t.Fatalf("hex echo should show the appended CRLF: %q", row)
	}
}

// Receiving while a HEX view is active used to group bytes by pane width.
// Switching back to ASCII then painted "AT..OK..ERR" on a single row because
// the breaks were no longer recognizable. Entries now follow device lines in
// every mode, so only the rendering changes.
func TestTelnetMonitorCrOnlyStreamGroupsLines(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}
	m.appendRx([]byte("AAA\rBBB\rCCC\r"))

	want := []string{"AAA", "BBB"}
	if len(m.rxLines) != len(want) {
		t.Fatalf("rows = %q, want %q", m.rxLines, want)
	}
	for i := range want {
		if m.rxLines[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, m.rxLines[i], want[i])
		}
	}
	// The last line has no terminating CR yet, so it is still pending.
	if string(m.pendingBytes) != "CCC\r" {
		t.Fatalf("pending = %q, want %q", m.pendingBytes, "CCC\r")
	}

	// A CRLF split across two reads must count as one break, not two.
	m2 := telnetNewTestMonitor(80, 24)
	m2.conn = &fakeConn{}
	m2.appendRx([]byte("OK\r"))
	m2.appendRx([]byte("\nnext\r\n"))
	if len(m2.rxLines) != 2 || m2.rxLines[0] != "OK" || m2.rxLines[1] != "next" {
		t.Fatalf("split CRLF rows = %q", m2.rxLines)
	}
}

// F2 must not change what goes on the wire: ASCII send appends CRLF, so the
// hex the box shows after the switch has to carry that suffix.
func TestTelnetMonitorHexInputAcceptsCommonSpellings(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	cases := []struct {
		in   string
		want []byte
	}{
		{"01 03 00 00", []byte{1, 3, 0, 0}},
		{"01,03,00,00", []byte{1, 3, 0, 0}},
		{"01-03-00-00", []byte{1, 3, 0, 0}},
		{"01:03", []byte{1, 3}},
		{"0x01 0x03", []byte{1, 3}},
		{"\\x01\\x03", []byte{1, 3}},
		{"0103\n", []byte{1, 3}},
	}
	for _, c := range cases {
		got, err := m.payloadBytes(c.in, true)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if !bytes.Equal(got, c.want) {
			t.Fatalf("%q -> % X, want % X", c.in, got, c.want)
		}
	}
	if _, err := m.payloadBytes("AT+GMR", true); err == nil {
		t.Fatal("letters outside hex digits must still be rejected")
	}
}

// A long command used to be cut at the pane edge with no sign that the rest
// was sent. It now wraps, so the echo matches the byte count in the status.
func TestTelnetMonitorTxEchoWrapsInsteadOfTruncating(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(40, 24)
	m.conn = &fakeConn{}
	m.setLineEnd("none")
	long := "0123456789012345678901234567890123456789ABCDEFGHIJ"
	m.input.SetValue(long)
	m.sendCurrent()

	rows := m.renderTxEntry(m.rxEntries[len(m.rxEntries)-1])
	if len(rows) < 2 {
		t.Fatalf("echo rows = %q, want a wrapped echo", rows)
	}
	// Continuation rows are indented under the head, so the payload pieces
	// are compared without that padding.
	joined := strings.ReplaceAll(strings.Join(rows, ""), " ", "")
	if !strings.Contains(joined, long) {
		t.Fatalf("echo %q lost part of %q", joined, long)
	}
	for _, r := range rows {
		if w := ansi.StringWidth(r); w > m.rx.Width {
			t.Fatalf("row %q width %d > %d", r, w, m.rx.Width)
		}
	}
}

// A dropped file is echoed as a bounded prefix plus a byte count instead of
// being rendered row by row into the pane.
func TestTelnetMonitorFileEchoIsBounded(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}
	path := filepath.Join(t.TempDir(), "blob.bin")
	payload := bytes.Repeat([]byte{0x41, 0x42, 0x43, 0x44}, 300) // 1200 bytes
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	m.sendFile(path)

	last := m.rxEntries[len(m.rxEntries)-1]
	rows := m.renderTxEntry(last)
	if len(rows) > 24 {
		t.Fatalf("file echo painted %d rows", len(rows))
	}
	if !strings.Contains(strings.Join(rows, " "), "+688 B") {
		t.Fatalf("file echo should report the hidden tail: %q", rows)
	}
	if m.txCount != len(payload) {
		t.Fatalf("txCount = %d, want %d", m.txCount, len(payload))
	}
}

// The echo is a record of the wire, not of the keyboard: in HEX view an ASCII
// send must show the appended CRLF (4 bytes for "AT"), while the text view
// hides it so the row reads like what was typed.
func TestTelnetMonitorAsciiEchoKeepsTextForControlBytes(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.conn = &fakeConn{}
	m.setLineEnd("crlf")

	m.input.SetValue("AT+GMR")
	m.sendCurrent()
	row := m.renderTxEntry(m.rxEntries[len(m.rxEntries)-1])[0]
	if !strings.Contains(row, "AT+GMR") || strings.Contains(row, "41 54") {
		t.Fatalf("text echo = %q, want the readable command", row)
	}
}

// The send box has to look like the mode it is in. The mode used to live only
// in the status row's "TX:" token, so a box still saying "Type data" next to
// "TX:HEX" read as if the switch had been ignored.
func TestTelnetQuickEditorBothFieldsLive(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = []telnetconfig.TelnetQuickCmd{{Label: "OLD", Value: "AT"}}
	m.clampQuick()
	m.quickEditSel = 0

	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyEnter}) // focus value
	for _, r := range "+GMR" {
		m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyTab}) // focus label
	for _, r := range "2" {
		m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.quickCmds[0].Value; got != "AT+GMR" {
		t.Fatalf("value after label edit = %q, want AT+GMR", got)
	}
	if got := m.quickCmds[0].Label; got != "OLD2" {
		t.Fatalf("label = %q, want OLD2", got)
	}
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyEnter}) // commit and leave
	if m.quickEditOn {
		t.Fatal("Enter should close the fields")
	}
	if m.quickCmds[0].Label != "OLD2" || m.quickCmds[0].Value != "AT+GMR" {
		t.Fatalf("committed slot = %+v", m.quickCmds[0])
	}
	// Rows show both stored halves once the fields close.
	frame := m.editorView()
	if !strings.Contains(frame, "OLD2") || !strings.Contains(frame, "AT+GMR") {
		t.Fatalf("editor should list name and payload:/n%s", frame)
	}
}

// The slot list has to be scannable: a stock slot stores the same text as its
// name and value, and printing both made every row read "AT  AT" — looked like
// a bug, and buried the payload of the slots that really differ.
func TestTelnetQuickListDoesNotEchoIdenticalPayload(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = []telnetconfig.TelnetQuickCmd{
		{Label: "AT", Value: "AT"},
		{Label: "reboot", Value: "AT+RST"},
	}
	m.clampQuick()
	m.quickEditSel = 0

	var atRow, rebootRow string
	for _, ln := range strings.Split(ansi.Strip(m.editorView()), "\n") {
		if strings.Contains(ln, " 1 ") && strings.Contains(ln, "AT") && atRow == "" {
			atRow = ln
		}
		if strings.Contains(ln, "reboot") {
			rebootRow = ln
		}
	}
	if strings.Count(atRow, "AT") != 1 {
		t.Fatalf("identical payload echoed again: %q", atRow)
	}
	if !strings.Contains(rebootRow, "AT+RST") {
		t.Fatalf("different payload should show: %q", rebootRow)
	}
}

// A blank slot is a normal state right after "N"; showing nothing at all made
// the new row look like a rendering failure instead of "type a name here".
func TestTelnetQuickListMarksEmptySlot(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = []telnetconfig.TelnetQuickCmd{{Label: "", Value: ""}}
	m.clampQuick()
	if !strings.Contains(ansi.Strip(m.editorView()), i18n.T("telnet.quick.unnamed")) {
		t.Fatal("empty slot should be labelled, not left as a gap")
	}
}

// While typing, Enter commits and Esc cancels — the footer has to say so, or
// the same key silently means two different things.
func TestTelnetQuickEditorSwapsFooterWhileTyping(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = testQuickCmds()
	m.clampQuick()

	browse := ansi.Strip(m.editorView())
	if !strings.Contains(browse, i18n.T("telnet.quick.help1")) {
		t.Fatal("browse footer should describe opening a field")
	}
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyEnter})
	typing := ansi.Strip(m.editorView())
	if strings.Contains(typing, i18n.T("telnet.quick.help1")) {
		t.Fatal("footer still advertises the browse binding while typing")
	}
	if !strings.Contains(typing, i18n.T("telnet.quick.edit_help1")) {
		t.Fatalf("typing footer missing:/n%s", typing)
	}
}

// Switching the global send mode has to retarget the saved quick slots too.
// Before this, the header said TX:HEX while slot 1 still held "AT" as ASCII —
// the two disagreed, which read as "the mode switch did nothing to quick
// commands". The bytes each slot sends must stay identical across the switch.
func TestTelnetQuickEditorToggleMatchesGlobal(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := telnetNewTestMonitor(80, 24)
	m.openEditor()
	// openEditor reloads from disk; install the fixture afterwards.
	m.quickCmds = []telnetconfig.TelnetQuickCmd{{Label: "AT", Value: "41 54 0D 0A", Hex: true}}
	m.clampQuick()
	m.quickEditSel = 0

	// Global mode is ASCII here, so a hex slot is the exception; editor F2
	// brings it back to text with the line ending stripped.
	m.updateEditor(tea.KeyMsg{Type: tea.KeyF2})
	if m.quickCmds[0].Hex || m.quickCmds[0].Value != "AT" {
		t.Fatalf("editor F2 = %+v, want AT", m.quickCmds[0])
	}
}

func TestQuickKeyMacOptionGlyphs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port
	m.quickCmds = telnetMakeQuickLabels(8)
	m.clampQuick()
	m.quickFocus = false

	// Strict alt mode ignores the plain glyphs a stock macOS layout emits.
	m.quickKey = telnetconfig.QuickKeyAlt
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'¡'}})
	if len(port.writes) != 0 {
		t.Fatalf("alt mode must ignore plain glyph, writes=%v", port.writes)
	}

	// Option mode fires each slot from its default ⌥N glyph, no terminal
	// configuration required.
	glyphs := []rune{'¡', '™', '£', '¢', '∞', '§', '¶', '•'}
	m.quickKey = telnetconfig.QuickKeyOption
	for i, g := range glyphs {
		m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{g}})
		want := fmt.Sprintf("V%d\r\n", i+1)
		if len(port.writes) != i+1 || string(port.writes[i]) != want {
			t.Fatalf("glyph %q (slot %d) writes = %v, want %q", g, i+1, port.writes, want)
		}
	}

	// A real Alt+digit keeps working in option mode (terminals set to Esc+).
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	if len(port.writes) != 9 || string(port.writes[8]) != "V1\r\n" {
		t.Fatalf("option mode alt+1 writes = %v, want V1+CRLF appended", port.writes)
	}
}

func TestQuickKeySettingSwitch(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)

	if m.prefs.QuickKey != telnetconfig.QuickKeyAuto {
		t.Fatalf("default quick key = %q, want auto", m.prefs.QuickKey)
	}

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	for i := 0; i < telnetSettingQuickKey; i++ {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsSel != telnetSettingQuickKey {
		t.Fatalf("selection = %d, want %d", m.settingsSel, telnetSettingQuickKey)
	}
	// The setting cycles auto -> alt -> option -> auto.
	for _, want := range []string{
		telnetconfig.QuickKeyAlt,
		telnetconfig.QuickKeyOption,
		telnetconfig.QuickKeyAuto,
		telnetconfig.QuickKeyAlt,
	} {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
		if m.prefs.QuickKey != want {
			t.Fatalf("draft quick key = %q, want %q", m.prefs.QuickKey, want)
		}
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	if m.quickKey != telnetconfig.QuickKeyAlt {
		t.Fatalf("applied quick key = %q, want alt", m.quickKey)
	}
	got, err := telnetconfig.LoadTelnetPrefs()
	if err != nil || got.QuickKey != telnetconfig.QuickKeyAlt {
		t.Fatalf("persisted quick key = %q, err=%v, want alt", got.QuickKey, err)
	}
}

func TestQuickKeyHelpFollowsSetting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)

	m.quickKey = telnetconfig.QuickKeyAlt
	if frame := RenderCanvas(80, 24, m.View()); !strings.Contains(frame, "Alt+#") {
		t.Fatal("help should show Alt+# in alt mode")
	}
	m.quickKey = telnetconfig.QuickKeyOption
	if frame := RenderCanvas(80, 24, m.View()); !strings.Contains(frame, "Option+#") {
		t.Fatal("help should show Option+# in option mode")
	}
}

// Telnet is a byte-stuffed protocol: a data 0xFF in anything the monitor
// sends must travel as IAC IAC, never as a bare command-start byte. This is
// the regression that a raw conn.Write in the monitor would reintroduce.
func TestTelnetMonitorOutboundStuffsIAC(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port

	// writePayload is the single send sink (commands, quick slots, loops).
	m.writePayload([]byte{'a', 0xFF, 'b'}, false)
	if len(port.writes) != 1 || !bytes.Equal(port.writes[0], []byte{'a', 0xFF, 0xFF, 'b'}) {
		t.Fatalf("payload wire = % X, want a FF FF b", port.writes)
	}

	// writeRaw (the file path) must stuff too.
	m.writeRaw([]byte{0xFF, 0x01})
	if len(port.writes) != 2 || !bytes.Equal(port.writes[1], []byte{0xFF, 0xFF, 0x01}) {
		t.Fatalf("raw wire = % X, want FF FF 01", port.writes)
	}
}

// A typed command is a text line on the NVT: the wire carries the text plus
// CRLF, regardless of any legacy line-ending value left in an old config.
func TestTelnetMonitorTextCommandUsesCRLF(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port
	m.lineEnd = "none" // stale value from an old config must be ignored

	m.input.SetValue("show version")
	m.sendCurrent()
	if len(port.writes) != 1 || string(port.writes[0]) != "show version\r\n" {
		t.Fatalf("wire = %q, want \"show version\\r\\n\"", port.writes)
	}
	if m.txCount != len("show version\r\n") {
		t.Fatalf("txCount = %d, want %d", m.txCount, len("show version\r\n"))
	}
}

// Quick slots are plain text commands in the telnet assistant: no hex frames,
// and the same CRLF line ending a typed command gets.
func TestTelnetMonitorQuickSlotSendsTextWithCRLF(t *testing.T) {
	m := telnetNewTestMonitor(80, 24)
	port := &fakeConn{}
	m.conn = port
	m.quickCmds = []telnetconfig.TelnetQuickCmd{{Label: "show version", Value: "show version"}}

	m.fireQuick(0)
	if len(port.writes) != 1 || string(port.writes[0]) != "show version\r\n" {
		t.Fatalf("quick wire = %q, want \"show version\\r\\n\"", port.writes)
	}
}

func TestTelnetDisplayPrefsDefaultASCIIAndNoPrefix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	if m.hexView || m.showPrefix || !m.ansiColor {
		t.Fatalf("defaults = hex:%v prefix:%v ansi:%v, want ASCII / no prefix / ANSI on", m.hexView, m.showPrefix, m.ansiColor)
	}
	rows := m.settingsRows()
	if len(rows) != telnetSettingsCount {
		t.Fatalf("settings rows = %d, want %d", len(rows), telnetSettingsCount)
	}
	if rows[0].value != "ASCII" || rows[1].value != "OFF" || rows[2].value != "ON" {
		t.Fatalf("first three rows = %+v", rows[:3])
	}
	m.appendRx([]byte("OK\n"))
	if len(m.rxLines) != 1 || m.rxLines[0] != "OK" {
		t.Fatalf("default RX row = %q", m.rxLines)
	}
}

func TestTelnetDisplayPrefsSwitchAndPersist(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	// Row 0: display mode ASCII -> HEX.
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	// Row 1: TX/RX prefix off -> on.
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if !m.prefs.HexView || !m.prefs.ShowPrefix {
		t.Fatalf("draft = hex:%v prefix:%v, want both on", m.prefs.HexView, m.prefs.ShowPrefix)
	}
	if m.hexView || m.showPrefix {
		t.Fatal("draft must not touch the live session before save")
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.hexView || !m.showPrefix {
		t.Fatalf("live = hex:%v prefix:%v, want both on", m.hexView, m.showPrefix)
	}
	got, err := telnetconfig.LoadTelnetPrefs()
	if err != nil || !got.HexView || !got.ShowPrefix {
		t.Fatalf("persisted = %+v err=%v", got, err)
	}
	m.appendRx([]byte{0x01, 0x02, '\r', '\n'})
	if len(m.rxLines) == 0 || !strings.HasPrefix(m.rxLines[0], rxMark+"01 02") {
		t.Fatalf("HEX row with prefix = %q", m.rxLines)
	}
}

// ANSI control sequences can straddle reads. Only SGR survives; erase-line,
// cursor/OSC strings must never leak literal "[0K" text into the pane.
func TestTelnetAnsiFilterStreaming(t *testing.T) {
	f := telnetAnsiFilter{keepSGR: true}
	var got []byte
	chunks := [][]byte{
		[]byte("A\x1b[3"),            // CSI split after the parameter
		[]byte("1mB\x1b[0"),          // SGR completes; erase-line starts
		[]byte("KC\x1b]52;c;Z\x07D"), // ESC[0K dropped, OSC dropped
		[]byte("\x1b[?25hE"),         // cursor visibility dropped
	}
	for _, chunk := range chunks {
		for _, b := range chunk {
			got = append(got, f.feed(b)...)
		}
	}
	want := []byte("A\x1b[31mBCDE")
	if !bytes.Equal(got, want) {
		t.Fatalf("filtered = %q, want %q", got, want)
	}
}

func TestTelnetMonitorRendersAnsiColorsInASCII(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.appendRx([]byte("\x1b[0K\x1b[31mRED\x1b[0m normal\r\n"))
	if len(m.rxLines) != 1 {
		t.Fatalf("rows = %q, want one coloured row", m.rxLines)
	}
	line := m.rxLines[0]
	if !strings.Contains(line, "\x1b[31mRED\x1b[0m normal") {
		t.Fatalf("SGR colour was not rendered: %q", line)
	}
	if strings.Contains(line, "0K") || strings.Contains(line, "?25") {
		t.Fatalf("control sequence leaked as text: %q", line)
	}
	if !strings.HasSuffix(line, "\x1b[0m") {
		t.Fatalf("colour must be reset before the next row: %q", line)
	}
	if w := ansi.StringWidth(line); w > m.rx.Width {
		t.Fatalf("row width %d exceeds pane %d", w, m.rx.Width)
	}
}

func TestTelnetMonitorHEXKeepsAnsiBytes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.hexView = true
	m.appendRx([]byte("\x1b[31mOK\r\n"))
	joined := strings.Join(m.rxLines, " ")
	if !strings.Contains(joined, "1B 5B 33 31 6D") {
		t.Fatalf("HEX view lost the wire bytes: %q", joined)
	}
}

func TestTelnetSearchSpansColourBoundary(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.appendRx([]byte("\x1b[31mAB\x1b[0mCD\r\n"))
	m.searchQuery = "ABCD"
	m.refreshRxContent()
	if len(m.searchMatches) != 1 {
		t.Fatalf("matches = %v, want the coloured row", m.searchMatches)
	}
	h := m.highlightLines(m.rxLines)
	if len(h) != 1 || !strings.Contains(h[0], "\x1b[7mAB") || !strings.Contains(h[0], "CD\x1b[27m") {
		t.Fatalf("highlight across SGR = %q", h)
	}
}

func TestTelnetAnsiColorSettingToggleOff(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.appendRx([]byte("\x1b[31mRED\x1b[0m now\r\n"))
	if !strings.Contains(m.rxLines[0], "\x1b[31m") {
		t.Fatalf("default view should carry SGR: %q", m.rxLines)
	}

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	for i := 0; i < telnetSettingAnsiColor; i++ {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsSel != telnetSettingAnsiColor {
		t.Fatalf("selection = %d, want ansi row %d", m.settingsSel, telnetSettingAnsiColor)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if m.prefs.AnsiColor || !m.ansiColor {
		t.Fatal("arrow key must only change the draft, not the live session")
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	if m.ansiColor || m.ansiFilter.keepSGR {
		t.Fatalf("saved setting not applied: live=%v filter=%v", m.ansiColor, m.ansiFilter.keepSGR)
	}
	got, err := telnetconfig.LoadTelnetPrefs()
	if err != nil || got.AnsiColor {
		t.Fatalf("persisted ansiColor = %v err=%v, want false", got.AnsiColor, err)
	}

	if strings.Contains(m.rxLines[0], "\x1b[") || !strings.Contains(m.rxLines[0], "RED now") {
		t.Fatalf("existing rows were not repainted in plain text: %q", m.rxLines)
	}
	m.appendRx([]byte("\x1b[32mGREEN\x1b[0M later\r\n"))
	last := m.rxLines[len(m.rxLines)-1]
	if strings.Contains(last, "\x1b[") || !strings.Contains(last, "GREEN later") {
		t.Fatalf("incoming row with colours off = %q", last)
	}
}

func TestTelnetSettingsRowsFitMonitor(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.settingsOpen = true
	view := ansi.Strip(m.settingsView())
	lines := strings.Split(view, "\n")
	if len(lines) > 24 {
		t.Fatalf("settings panel has %d rows, exceeds 24-line monitor", len(lines))
	}
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > 80 {
			t.Fatalf("settings row wraps at 80 cells: %q", line)
		}
	}
	for _, row := range m.settingsRows() {
		if !strings.Contains(view, row.label) {
			t.Fatalf("settings panel is missing row %q", row.label)
		}
	}
}

func TestTelnetMacOptionArrowPagesQuickBar(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.quickCmds = make([]telnetconfig.TelnetQuickCmd, 9) // two pages
	m.pageKey = "alt"
	m.quickKey = telnetconfig.QuickKeyOption // what "auto" resolves to on macOS

	// Stock macOS text editing sends CSI 1;5 C/D for Option+arrow, which
	// Bubble Tea reports as ctrl+right/left.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if m.quickPage != 1 {
		t.Fatalf("Option+Right (ctrl+right token) left page at %d, want 1", m.quickPage)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if m.quickPage != 0 {
		t.Fatalf("Option+Left (ctrl+left token) left page at %d, want 0", m.quickPage)
	}

	// Terminals set to "Option as Meta" send alt+right/left instead.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.quickPage != 1 {
		t.Fatalf("Option+Right (alt+right token) left page at %d, want 1", m.quickPage)
	}

	// In Alt quick-key mode ctrl+arrow is not an Option alias.
	m.quickKey = telnetconfig.QuickKeyAlt
	m.quickPage = 0
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if m.quickPage != 0 {
		t.Fatalf("ctrl+right should not page in Alt mode, got page %d", m.quickPage)
	}

	// With Ctrl as the page modifier, ctrl+arrow stays a real Ctrl binding.
	m.quickKey = telnetconfig.QuickKeyOption
	m.pageKey = "ctrl"
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if m.quickPage != 1 {
		t.Fatalf("Ctrl page mode ignored ctrl+right, page = %d", m.quickPage)
	}
}

func TestTelnetMacOptionArrowAlsoMapsScrollKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.quickKey = telnetconfig.QuickKeyOption
	m.pageKey = "alt"
	for in, want := range map[string]string{
		"ctrl+left": "alt+left", "ctrl+right": "alt+right",
		"ctrl+up": "alt+up", "ctrl+down": "alt+down",
		"alt+b": "alt+left", "alt+f": "alt+right",
		"ctrl+c": "ctrl+c",
	} {
		if got := m.macOptionArrow(in); got != want {
			t.Fatalf("macOptionArrow(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTelnetPageKeyLabelFollowsQuickModifier(t *testing.T) {
	if got := pageKeyLabel("alt", telnetconfig.QuickKeyOption); got != "Option" {
		t.Fatalf("Option mode page key = %q, want Option", got)
	}
	if got := pageKeyLabel("alt", telnetconfig.QuickKeyAlt); got != "Alt" {
		t.Fatalf("Alt mode page key = %q, want Alt", got)
	}
	if got := pageKeyLabel("ctrl", telnetconfig.QuickKeyOption); got != "Ctrl" {
		t.Fatalf("Ctrl page key = %q, want Ctrl", got)
	}
}

// With "Option as Meta" enabled, macOS terminals prefix the Option glyph
// with ESC. Bubble Tea reports that as alt+<glyph>, not a bare rune or an
// ASCII alt+digit. All three encodings must fire the same quick slot.
func TestTelnetOptionQuickAcceptsAltGlyphEncoding(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := telnetNewTestMonitor(80, 24)
	m.quickKey = telnetconfig.QuickKeyOption
	port := &fakeConn{}
	m.conn = port
	m.quickCmds = []telnetconfig.TelnetQuickCmd{
		{Label: "one", Value: "V1"},
		{Label: "two", Value: "V2"},
	}

	// Plain glyph (default macOS text editing).
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'¡'}})
	// ESC-prefixed glyph ("Option as Meta").
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'™'}, Alt: true})
	// ESC+ASCII digit (ASCII layout / Esc+ mode).
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})

	want := []string{"V1\r\n", "V2\r\n", "V1\r\n"}
	if len(port.writes) != len(want) {
		t.Fatalf("writes = %q, want %q", port.writes, want)
	}
	for i := range want {
		if string(port.writes[i]) != want[i] {
			t.Fatalf("write %d = %q, want %q", i, port.writes[i], want[i])
		}
	}
}

// ConHost can report Alt+digit with a raw Type byte and no Runes, in which
// case Key.String() is empty. The raw-key path must still fire the slot.
func TestTelnetAltDigitFromRawKeyType(t *testing.T) {
	for _, tc := range []struct {
		key  tea.KeyMsg
		want int
		ok   bool
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}, Alt: true}, 2, true},
		{tea.KeyMsg{Type: tea.KeyType('8'), Alt: true}, 7, true},
		{tea.KeyMsg{Type: tea.KeyType('9'), Alt: true}, 0, false},
		{tea.KeyMsg{Type: tea.KeyType('1')}, 0, false},
	} {
		idx, ok := telnetQuickDigitFromKey(tc.key)
		if idx != tc.want || ok != tc.ok {
			t.Fatalf("key %+v -> (%d,%v), want (%d,%v)", tc.key, idx, ok, tc.want, tc.ok)
		}
	}
}
