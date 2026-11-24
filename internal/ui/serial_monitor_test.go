package ui

import (
	"bytes"
	"fmt"
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
	"github.com/zsuroy/ctty/internal/serialconfig"
	"go.bug.st/serial"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ctty-ui-test-")
	if err == nil {
		_ = os.Setenv("XDG_CONFIG_HOME", dir)
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}

// fakeSerialPort only implements the calls the assistant uses; the embedded
// interface satisfies the rest of serial.Port.
type fakeSerialPort struct {
	serial.Port
	writes [][]byte
	closed bool
}

func (p *fakeSerialPort) Write(b []byte) (int, error) {
	p.writes = append(p.writes, append([]byte(nil), b...))
	return len(b), nil
}

func (p *fakeSerialPort) Close() error {
	p.closed = true
	return nil
}

func (p *fakeSerialPort) SetReadTimeout(time.Duration) error { return nil }

func newTestMonitor(width, height int) *serialMonitorModel {
	dev := serialconfig.SerialDevice{
		Name:     "console",
		Device:   "COM3",
		BaudRate: 115200,
		DataBits: 8,
		Parity:   "none",
		StopBits: 1,
	}
	return newSerialMonitor(NewStyles(width), width, height, dev)
}

func TestSerialMonitorHexSendAndHistory(t *testing.T) {
	m := newTestMonitor(80, 24)
	m.setLineEnd("none") // this test asserts raw bytes; CRLF is tested separately
	port := &fakeSerialPort{}
	m.port = port
	m.hexSend = true

	m.input.SetValue("01 03 00 00")
	m.sendCurrent()
	if len(port.writes) != 1 || string(port.writes[0]) != string([]byte{1, 3, 0, 0}) {
		t.Fatalf("hex payload = %v, want [1 3 0 0]", port.writes)
	}
	if m.txCount != 4 {
		t.Fatalf("txCount = %d, want 4", m.txCount)
	}
	if m.input.Value() != "" {
		t.Fatalf("input should clear after send, got %q", m.input.Value())
	}
	if len(m.history) != 1 || m.history[0] != "01 03 00 00" {
		t.Fatalf("history = %v", m.history)
	}

	m.input.SetValue("invalid hex")
	m.sendCurrent()
	if len(port.writes) != 1 {
		t.Fatal("invalid hex should not be sent")
	}
	if m.status != i18n.T("serial.monitor.badhex_hint") {
		t.Fatalf("invalid hex should report the hex hint, got %q", m.status)
	}
}

func TestSerialMonitorTextSendAndHistoryNavigation(t *testing.T) {
	m := newTestMonitor(80, 24)
	m.setLineEnd("none")
	port := &fakeSerialPort{}
	m.port = port

	for _, text := range []string{"hello", "world"} {
		m.input.SetValue(text)
		m.sendCurrent()
	}
	if m.txCount != 10 || len(port.writes) != 2 {
		t.Fatalf("txCount=%d writes=%d, want 10/2", m.txCount, len(port.writes))
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

func TestSerialMonitorReceiveFormatting(t *testing.T) {
	m := newTestMonitor(80, 24)
	m.appendRx([]byte("AB\n\x00C\n"))
	if len(m.rxLines) != 2 || m.rxLines[0] != rxMark+"AB" || m.rxLines[1] != rxMark+".C" {
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

func TestSerialMonitorTEMarkOnSend(t *testing.T) {
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.input.SetValue("AT")
	m.sendCurrent()
	if len(m.rxLines) != 1 || !strings.HasPrefix(m.rxLines[0], txMark) {
		t.Fatalf("send should echo a TX-marked line, got %q", m.rxLines)
	}
	if !strings.Contains(m.rxLines[0], "AT") {
		t.Fatalf("TX line should carry payload, got %q", m.rxLines[0])
	}
}

func TestSerialMonitorQuickSend(t *testing.T) {
	m := newTestMonitor(80, 24)
	m.setLineEnd("none")
	port := &fakeSerialPort{}
	m.port = port
	m.quickCmds = []serialconfig.QuickCmd{
		{Label: "AT", Value: "AT"},
		{Label: "MODBUS", Value: "01 03 00 00 84 0A", Hex: true},
	}

	// The second slot fires directly as hex, independent of the global
	// hex-send toggle.
	m.fireQuick(1)
	if len(port.writes) != 1 || !equalBytes(port.writes[0], []byte{1, 3, 0, 0, 0x84, 0x0A}) {
		t.Fatalf("hex quick payload = %v", port.writes)
	}
	if m.txCount != 6 {
		t.Fatalf("txCount = %d, want 6", m.txCount)
	}
}

func TestAltQuickIndex(t *testing.T) {
	for i := 1; i <= 8; i++ {
		idx, ok := altQuickIndex("alt+" + string(rune('0'+i)))
		if !ok || idx != i-1 {
			t.Fatalf("alt+%d => %d,%v", i, idx, ok)
		}
	}
	if _, ok := altQuickIndex("alt+9"); ok {
		t.Fatal("alt+9 must not map to a slot")
	}
}

func TestSerialMonitorEditorToggleAndSave(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.quickCmds = serialconfig.DefaultQuickCmds()

	m.openEditor()
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
	if !strings.Contains(m.quickMsg, i18n.T("serial.quick.unsaved")) {
		t.Fatalf("status = %q, want the unsaved hint", m.quickMsg)
	}
	m.updateEditor(tea.KeyMsg{Type: tea.KeyEsc})
	if m.quickEditing {
		t.Fatal("second Esc should close the editor")
	}
}

// The editor shows the label and the value as two separate fields, and the
// ASCII/HEX tag belongs to the value only — a label is always plain text.
func TestSerialQuickEditorSeparatesLabelAndValue(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = serialconfig.DefaultQuickCmds()
	m.clampQuick()
	m.quickEditSel = 4 // MODBUS, a hex frame

	frame := m.editorView()
	labelName := i18n.T("serial.quick.field_label")
	valueName := i18n.T("serial.quick.field_value")
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
	if strings.Contains(labelLine, "[HEX]") || strings.Contains(labelLine, "[ASCII]") {
		t.Fatalf("label must not carry an encoding tag: %q", labelLine)
	}
	if !strings.Contains(valueLine, "[HEX]") {
		t.Fatalf("value should be tagged HEX: %q", valueLine)
	}
	if !strings.Contains(valueLine, "01 03 00 00 00 01 84 0A") {
		t.Fatalf("value should show the frame: %q", valueLine)
	}

	// A binary frame has no text form, so F2 must refuse to make it ASCII
	// instead of silently turning the frame into its own hex spelling.
	m.updateEditor(tea.KeyMsg{Type: tea.KeyF2})
	if !m.quickCmds[4].Hex {
		t.Fatalf("MODBUS slot must stay HEX: %+v", m.quickCmds[4])
	}
	if m.quickMsg != i18n.T("serial.quick.keep_hex") {
		t.Fatalf("msg = %q, want the keep-HEX hint", m.quickMsg)
	}
	if !strings.Contains(m.editorView(), "[HEX]") {
		t.Fatal("view should still show the HEX tag")
	}
}

// Toggling a text slot to HEX must keep the same bytes on the wire, and the
// live byte count must follow the draft while it is typed.
func TestSerialQuickEditorEncodingRoundTrip(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = []serialconfig.QuickCmd{{Label: "RST", Value: "AT+RST"}}
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
	m.quickCmds = []serialconfig.QuickCmd{{Label: "PING"}}
	m.quickEditSel = 0
	m.updateEditor(tea.KeyMsg{Type: tea.KeyF2})
	if !m.quickCmds[0].Hex {
		t.Fatal("slot should be HEX now")
	}
	if b, err := m.quickCmds[0].Payload(); err != nil || string(b) != "PING\r\n" {
		t.Fatalf("label fallback payload = %q,%v, want PING+CRLF", b, err)
	}
}

func TestQuickCmdPayloadIndependentHex(t *testing.T) {
	if b, err := (serialconfig.QuickCmd{Value: "AB"}).Payload(); err != nil || string(b) != "AB" {
		t.Fatalf("ASCII slot AB = %q,%v — must not be hex-decoded", b, err)
	}
	if b, err := (serialconfig.QuickCmd{Value: "AB", Hex: true}).Payload(); err != nil || len(b) != 1 || b[0] != 0xAB {
		t.Fatalf("HEX slot AB = %v,%v", b, err)
	}
	if n := (serialconfig.QuickCmd{Value: "ZZ", Hex: true}).ByteCount(); n != -1 {
		t.Fatalf("malformed hex byte count = %d, want -1", n)
	}
}

func equalBytes(a, b []byte) bool {
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

func TestSerialMonitorRepeatTick(t *testing.T) {
	m := newTestMonitor(80, 24)
	m.setLineEnd("none")
	port := &fakeSerialPort{}
	m.port = port
	m.lastPayload = []byte("P")
	m.repeat = true
	m.interval = 100 * time.Millisecond

	updated, cmd := m.Update(serialTickMsg{})
	if updated.txCount != 1 || len(port.writes) != 1 {
		t.Fatalf("repeat tick should send the last payload: %d/%d", updated.txCount, len(port.writes))
	}
	if cmd == nil {
		t.Fatal("repeat should schedule the next tick")
	}

	updated.repeat = false
	updated, cmd = updated.Update(serialTickMsg{})
	if updated.txCount != 1 || cmd != nil {
		t.Fatal("stopped repeat should neither send nor schedule")
	}
}

func TestSerialMonitorFrameFitsCanvas(t *testing.T) {
	dev := serialconfig.SerialDevice{Name: "console", Device: "COM3", BaudRate: 115200, DataBits: 8, Parity: "none", StopBits: 1}
	for _, size := range []struct{ w, h int }{{80, 24}, {60, 20}, {120, 30}, {40, 15}} {
		m := newSerialMonitor(NewStyles(size.w), size.w, size.h, dev)
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

func TestSerialMonitorTitleHasSingleCttyBrand(t *testing.T) {
	m := newTestMonitor(80, 24)
	// This test asserts the Alt wording; "auto" resolves to Option on macOS.
	m.quickKey = serialconfig.QuickKeyAlt
	frame := RenderCanvas(80, 24, m.View())
	if !strings.Contains(frame, "[ctty]") {
		t.Fatal("serial monitor should show the ctty badge")
	}
	if n := strings.Count(strings.ToLower(frame), "ctty"); n != 1 {
		t.Fatalf("serial monitor should show ctty exactly once, got %d", n)
	}
	pageHint := strings.Contains(frame, "PAGE") || strings.Contains(frame, "翻页")
	if !strings.Contains(frame, "Alt+") || !pageHint || !strings.Contains(frame, "Esc:") {
		t.Fatal("serial monitor help should advertise Alt shortcuts and quick-page switching")
	}
}

func TestSerialConnectModeResolution(t *testing.T) {
	// CLI flag > device mode > global setting > per-entry default, mirrored
	// from serialconfig.ResolveMode so the CLI and the TUI cannot disagree.
	dev := serialconfig.SerialDevice{Name: "console", Device: "COM3"}

	m := Model{}
	if got := m.serialConnectMode(dev); got != serialconfig.ModeAssistant {
		t.Fatalf("TUI default = %q, want assistant", got)
	}
	m.serialOnly = true
	if got := m.serialConnectMode(dev); got != serialconfig.ModeRaw {
		t.Fatalf("standalone default = %q, want raw", got)
	}
	m.appConfig = &config.AppConfig{SerialStartMode: config.SerialStartAssistant}
	if got := m.serialConnectMode(dev); got != serialconfig.ModeAssistant {
		t.Fatalf("global assistant = %q, want assistant", got)
	}
	m.appConfig.SerialStartMode = config.SerialStartRaw
	if got := m.serialConnectMode(dev); got != serialconfig.ModeRaw {
		t.Fatalf("global raw = %q, want raw", got)
	}
	dev.Mode = serialconfig.ModeAssistant
	if got := m.serialConnectMode(dev); got != serialconfig.ModeAssistant {
		t.Fatalf("device mode should beat the global setting, got %q", got)
	}
	m.serialForceMode = serialconfig.ModeRaw
	if got := m.serialConnectMode(dev); got != serialconfig.ModeRaw {
		t.Fatalf("CLI override should beat the device mode, got %q", got)
	}
}

func TestSerialConnectHonoursRawMode(t *testing.T) {
	// A raw-mode connection must hand the terminal to the passthrough bridge
	// instead of opening the monitor, without touching the port here.
	dev := serialconfig.SerialDevice{Name: "console", Device: "COM3", BaudRate: 115200}
	m := Model{styles: NewStyles(80), width: 80, height: 24, viewMode: ViewSerial,
		appConfig: &config.AppConfig{SerialStartMode: config.SerialStartRaw}}
	updated, cmd := m.Update(serialConnectMsg{device: dev})
	um := updated.(Model)
	if um.serialMonitor != nil || um.viewMode == ViewSerialMonitor {
		t.Fatal("raw mode must not open the assistant monitor")
	}
	if cmd == nil {
		t.Fatal("raw mode should start the passthrough command")
	}
}

func TestSerialConnectEntersMonitorFromTUI(t *testing.T) {
	dev := serialconfig.SerialDevice{Name: "console", Device: "COM3", BaudRate: 115200, DataBits: 8, Parity: "none", StopBits: 1}
	m := Model{styles: NewStyles(80), width: 80, height: 24, viewMode: ViewSerial}
	updated, _ := m.Update(serialConnectMsg{device: dev})
	um := updated.(Model)
	if um.viewMode != ViewSerialMonitor || um.serialMonitor == nil {
		t.Fatalf("TUI connect should open the assistant, got view %v", um.viewMode)
	}

	updated, _ = um.Update(serialMonitorDoneMsg{})
	um = updated.(Model)
	if um.viewMode != ViewSerial || um.serialMonitor != nil || um.serialForm == nil {
		t.Fatal("closing the assistant should return to the serial list")
	}

	um.serialMonitor = newTestMonitor(80, 24)
	updated, cmd := um.Update(serialMonitorSwitchMsg{})
	um = updated.(Model)
	if um.viewMode != ViewList || cmd == nil {
		t.Fatal("passthrough switch should return to the list and start an Exec command")
	}
}

func TestSerialOnlyEscFromMonitorQuitsProgram(t *testing.T) {
	// 'ctty serial' starts directly in the assistant (serialOnly). Esc on the
	// monitor must quit the whole program, not drop back to a device list the
	// user never opened. Regression: a missing serialOnly check used to leave
	// the process hanging after Esc.
	m := Model{styles: NewStyles(80), width: 80, height: 24,
		viewMode: ViewSerialMonitor, serialOnly: true, serialMonitor: newTestMonitor(80, 24)}
	updated, cmd := m.Update(serialMonitorDoneMsg{})
	um := updated.(Model)
	if um.serialMonitor != nil {
		t.Fatal("Esc must close the monitor before quitting")
	}
	if cmd == nil {
		t.Fatal("serialOnly Esc must emit a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("serialOnly Esc should quit, got %T", cmd())
	}
}

func TestSerialSettingsOpenEditAndApply(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	// F10 opens the settings page without touching the live session.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	if !m.settingsOpen {
		t.Fatal("F10 should open settings")
	}
	// Select HEX receive (row 0) and toggle it on.
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if !m.prefs.HexView {
		t.Fatal("Right on HEX receive row should enable it in the draft")
	}
	if m.hexView {
		t.Fatal("draft change must not affect the live session before save")
	}
	// Move down to loop interval, step it up, then save. The line-ending,
	// HEX+ASCII, log and page-key rows now sit between timestamp and interval.
	for i := 0; i < settingInterval; i++ {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsSel != settingInterval {
		t.Fatalf("selection = %d, want interval %d", m.settingsSel, settingInterval)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if m.prefs.IntervalMs != serialconfig.DefaultIntervalMs+100 {
		t.Fatalf("interval draft = %d, want %d", m.prefs.IntervalMs, serialconfig.DefaultIntervalMs+100)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	if m.settingsOpen {
		t.Fatal("Enter should close settings")
	}
	if !m.hexView || m.interval != m.prefs.Interval() {
		t.Fatalf("saved prefs not applied: hexView=%v interval=%v", m.hexView, m.interval)
	}
	// Reload from disk to confirm persistence.
	got, err := serialconfig.LoadSerialPrefs()
	if err != nil {
		t.Fatalf("reload prefs: %v", err)
	}
	if !got.HexView || got.IntervalMs != m.prefs.IntervalMs {
		t.Fatalf("persisted prefs mismatch: %+v", got)
	}
}

func TestSerialSettingsCancelLeavesSessionUntouched(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight}) // toggle HexView draft
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsOpen {
		t.Fatal("Esc should close settings")
	}
	if m.hexView || m.prefs.HexView {
		t.Fatal("Esc must discard the draft")
	}
	got, err := serialconfig.LoadSerialPrefs()
	if err != nil || got.HexView {
		t.Fatalf("Esc must not write the file: %+v err=%v", got, err)
	}
}

func TestSerialSettingsBufferCapApplied(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.maxLines = 3
	m.appendRx([]byte("1\n2\n3\n4\n5\n"))
	if len(m.rxLines) != 3 || !strings.HasSuffix(m.rxLines[0], "3") {
		t.Fatalf("buffer cap not applied: %q", m.rxLines)
	}
}

func TestSerialMonitorFollowPauseOnScroll(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
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

func TestSerialMonitorClearResumesFollow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
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

func TestFitStatusRowTwoPanes(t *testing.T) {
	// Feedback must stay on the right and never overflow the pane width.
	row := fitStatusRow("[LIVE] INPUT RX 0 TX 0 RX:ASCII TX:ASCII TIME:OFF LOOP:OFF 1s", "Saved and applied.", 70)
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
	row2 := fitStatusRow("[LIVE] INPUT RX 0", "", 40)
	if w := ansi.StringWidth(row2); w > 40 {
		t.Fatalf("no-feedback row overflow: %d", w)
	}
}

func TestSerialQuickCmdLabelShownInTxLine(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
	m.quickCmds = []serialconfig.QuickCmd{
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

func TestSerialQuickLabelSwitchHidesBracketPrefix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
	m.quickCmds = []serialconfig.QuickCmd{{Label: "AT+RST", Value: "AT+RST"}}
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

func TestSerialQuickLabelSettingAppliesAndPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	for i := 0; i < settingShowQuickLabel; i++ {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsSel != settingShowQuickLabel {
		t.Fatalf("selection = %d, want row %d", m.settingsSel, settingShowQuickLabel)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if m.prefs.ShowQuickLabel || !m.showQuickLabel {
		t.Fatalf("draft = %v, live = %v; draft must toggle off while live stays on",
			m.prefs.ShowQuickLabel, m.showQuickLabel)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	got, err := serialconfig.LoadSerialPrefs()
	if err != nil || got.ShowQuickLabel {
		t.Fatalf("persisted show_quick_label = %v err=%v, want false", got.ShowQuickLabel, err)
	}
	if m.showQuickLabel {
		t.Fatal("applied quick-label switch must turn it off")
	}
}

func TestSerialEmptyHintAndHistPreview(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dev := serialconfig.SerialDevice{Name: "console", Device: "COM3", BaudRate: 115200, DataBits: 8, Parity: "none", StopBits: 1}
	m := newSerialMonitor(NewStyles(80), 80, 24, dev)
	m.port = &fakeSerialPort{}
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

func makeQuickLabels(n int) []serialconfig.QuickCmd {
	cmds := make([]serialconfig.QuickCmd, n)
	for i := range cmds {
		cmds[i] = serialconfig.QuickCmd{Label: fmt.Sprintf("Q%d", i+1), Value: fmt.Sprintf("V%d", i+1)}
	}
	return cmds
}

func TestQuickPagingNavigation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.quickKey = serialconfig.QuickKeyAlt // Ctrl+arrow is an Option alias only in Option mode
	m.quickCmds = makeQuickLabels(20)     // 3 pages: 0-7, 8-15, 16-19
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

func TestQuickPageKeySwitch(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.quickKey = serialconfig.QuickKeyAlt
	m.quickCmds = makeQuickLabels(20)
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
	for i := 0; i < settingPageKey; i++ {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsSel != settingPageKey {
		t.Fatalf("selection = %d, want %d", m.settingsSel, settingPageKey)
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
	got, err := serialconfig.LoadSerialPrefs()
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

func TestAltQuickFiresCurrentPage(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.setLineEnd("none")
	port := &fakeSerialPort{}
	m.port = port
	m.quickCmds = makeQuickLabels(20)
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

	// Alt+1 on page 0 fires slot 0 (V1).
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	checkLastWrite("V1")

	m.setQuickSel(8) // page 1
	// Alt+1 on page 1 fires slot 8 (V9).
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	checkLastWrite("V9")
}

func TestQuickBarPageLocalNumbersAndBlankSlots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.quickCmds = makeQuickLabels(20) // 3 pages: 8 + 8 + 4
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

func TestQuickEditorAddDelete(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.quickCmds = makeQuickLabels(3)
	m.clampQuick()
	m.openEditor()
	m.quickCmds = makeQuickLabels(3) // openEditor reloads from disk (defaults)
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

func TestQuickEditorWindowScroll(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.quickCmds = makeQuickLabels(30)
	m.clampQuick()
	m.openEditor()
	m.quickCmds = makeQuickLabels(30) // reload from disk overrides; set again
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

func TestCRLFAppendASCIIAndHex(t *testing.T) {
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.setLineEnd("crlf")

	// ASCII: payload + literal CRLF on the wire.
	m.input.SetValue("AT")
	m.sendCurrent()
	if len(port.writes) != 1 || string(port.writes[0]) != "AT\r\n" {
		t.Fatalf("ASCII CRLF write = %q, want AT\\r\\n", port.writes)
	}
	if m.txCount != 4 {
		t.Fatalf("txCount = %d, want 4 (2 payload + 2 CRLF)", m.txCount)
	}

	// HEX: exactly the decoded bytes. The CRLF setting belongs to the text
	// send mode; a hex frame that needs 0D 0A spells it out.
	m.hexSend = true
	m.input.SetValue("01 03")
	m.sendCurrent()
	want := []byte{0x01, 0x03}
	if len(port.writes) != 2 || !bytes.Equal(port.writes[1], want) {
		t.Fatalf("HEX write = %v, want %v", port.writes[1], want)
	}
	if m.txCount != 6 {
		t.Fatalf("txCount = %d, want 6 (4 ascii + 2 hex)", m.txCount)
	}

	// Off: no suffix, wireLen matches payload length.
	m.setLineEnd("none")
	m.hexSend = false
	m.input.SetValue("AT")
	m.sendCurrent()
	if string(port.writes[2]) != "AT" {
		t.Fatalf("CRLF off write = %q, want AT", port.writes[2])
	}
	if m.wireLen(2, true) != 2 {
		t.Fatalf("wireLen with CRLF off = %d, want 2", m.wireLen(2, true))
	}
}

func TestLoopEnterInsertKeepsLoopPayload(t *testing.T) {
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.setLineEnd("none")
	m.repeat = true
	m.lastPayload = []byte("LOOP")

	// Pressing Enter during a loop sends the one-off input but must NOT
	// replace what the loop keeps sending.
	m.input.SetValue("ONCE")
	m.sendCurrent()
	if len(port.writes) != 1 || string(port.writes[0]) != "ONCE" {
		t.Fatalf("manual Enter write = %q, want ONCE", port.writes)
	}
	if string(m.lastPayload) != "LOOP" {
		t.Fatalf("loop payload clobbered = %q, want LOOP", m.lastPayload)
	}

	// A loop tick still sends the original payload.
	m.sendRepeat()
	if len(port.writes) != 2 || string(port.writes[1]) != "LOOP" {
		t.Fatalf("repeat tick write = %q, want LOOP", port.writes)
	}
}

func TestRawRxLogToggleWritesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	m := newTestMonitor(80, 24)

	matches, _ := filepath.Glob(filepath.Join(home, "ctty-serial-*.log"))
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
	matches, _ = filepath.Glob(filepath.Join(home, "ctty-serial-*.log"))
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

func TestSearchHighlightWidthNeutral(t *testing.T) {
	m := newTestMonitor(80, 24)
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

func TestRXRateAccumulates(t *testing.T) {
	m := newTestMonitor(80, 24)
	m.appendRx([]byte("hello"))
	if m.rxRateBytes != 5 {
		t.Fatalf("rxRateBytes = %d, want 5", m.rxRateBytes)
	}
	m.appendRx([]byte("world!"))
	if m.rxRateBytes != 11 {
		t.Fatalf("rxRateBytes = %d, want 11", m.rxRateBytes)
	}
}

func TestSerialMonitorLineEndingCycle(t *testing.T) {
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.setLineEnd("lf")
	m.input.SetValue("AT")
	m.sendCurrent()
	if len(port.writes) != 1 || string(port.writes[0]) != "AT\n" {
		t.Fatalf("LF payload = %q, want AT\\n", port.writes)
	}
	if m.txCount != 3 {
		t.Fatalf("txCount = %d, want 3", m.txCount)
	}
	m.setLineEnd("none")
	if got := m.wireLen(5, true); got != 5 {
		t.Fatalf("wireLen(none) = %d, want 5", got)
	}
}

func TestSerialMonitorSearchJump(t *testing.T) {
	m := newTestMonitor(80, 24)
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

func TestSerialMonitorHexAsciiView(t *testing.T) {
	m := newTestMonitor(80, 24)
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

func TestSerialMonitorReceiveRowsAt80x24(t *testing.T) {
	m := newTestMonitor(80, 24)
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

func TestEscChainFromMonitorExitsProgram(t *testing.T) {
	// Full exit path in normal mode (entered from the main TUI): Esc on the
	// monitor disconnects back to the serial device list, Esc there returns
	// to the main list, and Esc on the main list finally quits the program.
	// A dead key anywhere in this chain strands the process with no way out
	// from the keyboard, which users experience as a "crash on exit".
	cfg := &config.AppConfig{KeyBindings: config.KeyBindings{QuitKeys: []string{"q", "ctrl+c"}}}
	m := Model{styles: NewStyles(80), width: 80, height: 24, appConfig: cfg,
		viewMode: ViewSerialMonitor, serialMonitor: newTestMonitor(80, 24)}

	// 1. Esc on the monitor closes the port and emits the done message.
	_, doneCmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if doneCmd == nil {
		t.Fatal("Esc on monitor must emit the disconnect command")
	}
	// 2. Processing it returns to the serial device list.
	m2, _ := m.Update(doneCmd())
	um := m2.(Model)
	if um.serialMonitor != nil || um.viewMode != ViewSerial {
		t.Fatalf("disconnect must land on the serial list, got mode %v monitor=%v", um.viewMode, um.serialMonitor != nil)
	}
	// 3. Esc on the serial device list returns to the main list.
	_, leaveCmd := um.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if leaveCmd == nil {
		t.Fatal("Esc on serial list must emit the leave command")
	}
	m3, _ := um.Update(leaveCmd())
	um2 := m3.(Model)
	if um2.viewMode != ViewList {
		t.Fatalf("leaving serial must land on the main list, got %v", um2.viewMode)
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

// fakeBusyPort mimics a serial device whose Read is parked in a pending
// overlapped I/O, the exact state a real port is in when the user hits Esc.
type fakeBusyPort struct {
	serial.Port
	entered     chan struct{}
	release     chan struct{}
	closeCalled chan struct{}
}

func (p *fakeBusyPort) Read(b []byte) (int, error) {
	close(p.entered)
	<-p.release
	return 0, fmt.Errorf("device gone")
}

func (p *fakeBusyPort) Close() error {
	close(p.closeCalled)
	return nil
}

func TestClosePortDoesNotOverlapInFlightRead(t *testing.T) {
	// go.bug.st/serial v1.8.0 on Windows crashes (Exception 0xc0000008) when
	// CloseHandle runs while an overlapped Read is pending (bugst/go-serial
	// #57, #219). closePort must wait for the in-flight read to leave the
	// port instead of closing under it, and must not deadlock while waiting.
	m := newTestMonitor(80, 24)
	fp := &fakeBusyPort{
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
		closeCalled: make(chan struct{}),
	}
	m.port = fp

	cmd := serialReadCmd(fp, &m.ioMu)
	msgCh := make(chan tea.Msg, 1)
	go func() { msgCh <- cmd() }()
	<-fp.entered // the read now "hangs in the kernel" holding the I/O lock

	done := make(chan struct{})
	go func() { m.closePort(); close(done) }()

	select {
	case <-done:
		t.Fatal("closePort closed the port while a read was still pending")
	case <-time.After(80 * time.Millisecond):
	}
	close(fp.release) // the read completes and releases the lock
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closePort deadlocked behind the in-flight read")
	}
	select {
	case <-fp.closeCalled:
	default:
		t.Fatal("port was never closed")
	}
}

func TestSerialMonitorDragFileSendsRawBytes(t *testing.T) {
	// Other tests in this package switch the UI language; pin English so the
	// hint assertion is stable regardless of test order.
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	// Dropping a file on a terminal pastes its path into the input; Enter
	// must then send the file's raw bytes — no line-ending suffix, no hex
	// framing — and restore the input to a clean line afterwards.
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(path, []byte{0x01, 0x02, 0x03}, 0644); err != nil {
		t.Fatal(err)
	}
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port

	m.input.SetValue(path)
	// The live hint must announce the file while the path sits in the input.
	if row := m.sendBoxContent(70); !strings.Contains(row, "FILE") {
		t.Fatalf("send row = %q, want a FILE hint", row)
	}
	m.sendCurrent()
	if len(port.writes) != 1 || !bytes.Equal(port.writes[0], []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("file payload = %q, want 01 02 03", port.writes)
	}
	if m.txCount != 3 {
		t.Fatalf("file txCount = %d, want 3", m.txCount)
	}
	if m.input.Value() != "" {
		t.Fatalf("input = %q, want cleared after file send", m.input.Value())
	}

	// A quoted path (what Windows Terminal pastes for names with spaces)
	// must work the same way.
	quoted := `"` + path + `"`
	m.input.SetValue(quoted)
	m.sendCurrent()
	if m.txCount != 6 {
		t.Fatalf("quoted-path txCount = %d, want 6", m.txCount)
	}

	// Plain device text must still go out as text, even if a file with the
	// same name exists in the working directory — no separator, no drop.
	port.writes = nil
	m.input.SetValue("hello")
	m.sendCurrent()
	if len(port.writes) != 1 || !strings.HasPrefix(string(port.writes[0]), "hello") {
		t.Fatalf("text send got %q, want a payload starting with \"hello\"", port.writes)
	}
}

func TestSerialMonitorModeSwitchRerendersScreen(t *testing.T) {
	// F1/F3 must re-render the content already on screen, not just style
	// future bytes — the pane stores raw entries and derives display lines.
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}

	m.appendRx([]byte("AB\n"))
	if len(m.rxLines) != 1 || m.rxLines[0] != rxMark+"AB" {
		t.Fatalf("ascii receive = %q", m.rxLines)
	}

	// F1: ASCII -> HEX. The LF that ended the line is wire data, so it has to
	// survive the mode switch; hiding it made the byte count disagree with RX.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1})
	if len(m.rxLines) != 1 || m.rxLines[0] != rxMark+"41 42 0A" {
		t.Fatalf("hex rerender = %q", m.rxLines)
	}
	// F1: HEX -> HEX+ASCII (offset gutter)
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1})
	if len(m.rxLines) != 1 || !strings.Contains(m.rxLines[0], "41 42 0A") || !strings.Contains(m.rxLines[0], "|AB.|") {
		t.Fatalf("hex+ascii rerender = %q", m.rxLines)
	}
	// F1: back to ASCII — same content, same shape.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1})
	if m.rxLines[0] != rxMark+"AB" {
		t.Fatalf("ascii rerender = %q", m.rxLines)
	}

	// TX echoes follow the mode too: text in ASCII, bytes in HEX.
	m.input.SetValue("AT")
	m.sendCurrent()
	last := m.rxLines[len(m.rxLines)-1]
	if !strings.HasPrefix(last, txMark) || !strings.Contains(last, "AT") {
		t.Fatalf("tx echo in ascii = %q", last)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1}) // -> HEX
	last = m.rxLines[len(m.rxLines)-1]
	if !strings.Contains(last, "41 54") {
		t.Fatalf("tx echo in hex = %q", last)
	}

	// F3 stamps stored lines with their receive time.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF3})
	if !m.showTime || !strings.HasPrefix(m.rxLines[0], "[") {
		t.Fatalf("timestamp rerender = %q", m.rxLines[0])
	}
}

func TestSerialMonitorFrameStaysAlignedWithMixedContent(t *testing.T) {
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
		m := newTestMonitor(80, 24)
		m.port = &fakeSerialPort{}
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

func TestSerialMonitorQuestionMarkIsPayloadNotHelp(t *testing.T) {
	// "?" is a payload character in a serial assistant (AT+GMR?, AT+BAUD?),
	// so it must reach the send box. Help lives on F12 instead.
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port

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

// fillRx pushes n newline-terminated lines through the normal receive path.
func fillRx(m *serialMonitorModel, from, to int) {
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
func TestSerialMonitorScrollKeysMoveAndHold(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}

	fillRx(m, 0, 200)
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
	fillRx(m, 200, 210)
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

func TestSerialMonitorWheelScrollsReceivePane(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
	fillRx(m, 0, 200)
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

func TestSerialMonitorAltArrowsScrollOneLine(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
	fillRx(m, 0, 200)
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
func TestSerialMonitorScrollWorksWithQuickBarFocused(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
	m.quickCmds = []serialconfig.QuickCmd{{Label: "AT", Value: "AT"}, {Label: "GMR", Value: "AT+GMR"}}
	fillRx(m, 0, 200)
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
func TestSerialMonitorLabelOnlyQuickCommandSendsLabel(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.setLineEnd("none") // assert the raw payload, not the CRLF terminator

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

	if len(port.writes) != 1 || string(port.writes[0]) != "AT+CFUN=1" {
		t.Fatalf("writes = %q, want the label as payload", port.writes)
	}
	last := m.rxEntries[len(m.rxEntries)-1]
	if !last.tx || string(last.raw) != "AT+CFUN=1" {
		t.Fatalf("tx echo raw = %q, want the label bytes", last.raw)
	}
}

func TestSerialMonitorEmptyQuickCommandSendsNothing(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.quickCmds = []serialconfig.QuickCmd{{}}

	m.fireQuick(0)
	if len(port.writes) != 0 {
		t.Fatalf("blank slot must not write: %q", port.writes)
	}
	if m.status != i18n.T("serial.quick.no_payload") {
		t.Fatalf("status = %q, want the empty-command hint", m.status)
	}
}

// Switching to HEX send and back must be visible in the log. The ASCII view
// used to dump the whole payload as hex whenever a single control byte showed
// up — and the default CRLF suffix guaranteed one — so every send looked like
// the mode switch had been ignored.
func TestSerialMonitorAsciiViewEchoesTextNotHex(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
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
func TestSerialMonitorAsciiEchoKeepsTypedControlBytes(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
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

func TestSerialMonitorF2AnnouncesSendMode(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	// The mode token leads the feedback; the rest reports how many quick
	// commands followed the switch.
	want := fmt.Sprintf(i18n.T("serial.monitor.tx_mode"), "HEX")
	if !strings.HasPrefix(m.status, want) {
		t.Fatalf("status = %q, want prefix %q", m.status, want)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	want = fmt.Sprintf(i18n.T("serial.monitor.tx_mode"), "ASCII")
	if !strings.HasPrefix(m.status, want) {
		t.Fatalf("status = %q, want prefix %q", m.status, want)
	}
}

func TestSerialMonitorHexSendRejectsTextWithHint(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.hexSend = true

	m.input.SetValue("AT+GMR")
	m.sendCurrent()
	if len(port.writes) != 0 {
		t.Fatalf("invalid hex must not reach the port: %q", port.writes)
	}
	if m.status != i18n.T("serial.monitor.badhex_hint") {
		t.Fatalf("status = %q, want the hex hint", m.status)
	}
}

// The ASCII view groups lines by LF and used to throw the CRLF away. Those
// bytes are real: switching to HEX then showed "41 54" for a frame that was
// actually "41 54 0D 0A", so the pane disagreed with the RX counter.
func TestSerialMonitorHexViewKeepsLineEndingBytes(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
	m.appendRx([]byte("AT\r\nOK\r\n"))

	var stored int
	for _, e := range m.rxEntries {
		stored += len(e.raw)
	}
	if stored != m.rxCount || m.rxCount != 8 {
		t.Fatalf("stored=%d rxCount=%d, want 8/8", stored, m.rxCount)
	}

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1}) // -> HEX
	joined := strings.Join(m.rxLines, "\n")
	if !strings.Contains(joined, "41 54 0D 0A") || !strings.Contains(joined, "4F 4B 0D 0A") {
		t.Fatalf("hex view lost line endings: %q", m.rxLines)
	}

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1}) // -> HEX+ASCII
	joined = strings.Join(m.rxLines, "\n")
	if !strings.Contains(joined, "0D 0A") || !strings.Contains(joined, "|AT..|") {
		t.Fatalf("hex+ascii view = %q", m.rxLines)
	}

	// Back to ASCII: the line endings are hidden again, not duplicated.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1}) // -> ASCII
	if len(m.rxLines) != 2 || m.rxLines[0] != rxMark+"AT" || m.rxLines[1] != rxMark+"OK" {
		t.Fatalf("ascii round trip = %q", m.rxLines)
	}
}

// HEX send means "these exact bytes": appending the configured CRLF turned a
// 4-byte frame into 6 bytes on the wire while the echo still showed 4.
func TestSerialMonitorHexSendSendsExactBytes(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.setLineEnd("crlf")
	m.hexSend = true

	m.input.SetValue("01 03 00 00")
	m.sendCurrent()

	if len(port.writes) != 1 || len(port.writes[0]) != 4 {
		t.Fatalf("wire = % X, want exactly 4 bytes", port.writes)
	}
	if m.txCount != 4 {
		t.Fatalf("txCount = %d, want 4", m.txCount)
	}
	// The echo must describe the same four bytes that went out, in HEX view.
	m.hexView = true
	m.rebuildRx()
	if row := m.rxLines[len(m.rxLines)-1]; !strings.Contains(row, "01 03 00 00") || strings.Contains(row, "0D") {
		t.Fatalf("echo = %q", row)
	}
	m.hexView = false
	m.rebuildRx()

	// ASCII send keeps the suffix, so the loop still terminates commands.
	m.hexSend = false
	m.input.SetValue("AT")
	m.sendCurrent()
	if got := port.writes[len(port.writes)-1]; string(got) != "AT\r\n" {
		t.Fatalf("ascii wire = %q, want AT\\r\\n", got)
	}

	// A hex quick slot is byte-exact too; an ASCII slot still gets the CRLF.
	port.writes = nil
	m.quickCmds = []serialconfig.QuickCmd{
		{Label: "MODBUS", Value: "01 03", Hex: true},
		{Label: "GMR", Value: "AT+GMR"},
	}
	m.fireQuick(0)
	m.fireQuick(1)
	if len(port.writes) != 2 {
		t.Fatalf("quick writes = %q", port.writes)
	}
	if string(port.writes[0]) != string([]byte{1, 3}) {
		t.Fatalf("hex slot wire = % X", port.writes[0])
	}
	if string(port.writes[1]) != "AT+GMR\r\n" {
		t.Fatalf("ascii slot wire = %q", port.writes[1])
	}
}

// F2 used to flip only the parser. The box kept its old text, so Enter then
// sent six literal characters ("41 42") instead of the two bytes it meant.
func TestSerialMonitorF2RetargetsInput(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
	m.setLineEnd("none")

	m.input.SetValue("AT+GMR")
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if !m.hexSend || m.input.Value() != "41 54 2B 47 4D 52" {
		t.Fatalf("hex retarget = %q (hexSend=%v)", m.input.Value(), m.hexSend)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if m.hexSend || m.input.Value() != "AT+GMR" {
		t.Fatalf("ascii retarget = %q (hexSend=%v)", m.input.Value(), m.hexSend)
	}

	// Bytes with no text form must not survive as hex spelling: Enter in
	// ASCII mode would put "01 03" on the wire as four characters while the
	// echo still looked like a two-byte hex frame.
	m.hexSend = true
	m.input.SetValue("01 03")
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if m.input.Value() != "" {
		t.Fatalf("unprintable input should be cleared, got %q", m.input.Value())
	}
	if !strings.Contains(m.status, i18n.T("serial.monitor.tx_input_cleared")) {
		t.Fatalf("status = %q, want the cleared hint", m.status)
	}
}

// A half-received line is committed when the view mode changes, so the new
// grouping rules apply to it instead of leaving it in the old buffer shape.
func TestSerialMonitorF1CommitsPendingBytes(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}

	m.appendRx([]byte("AT")) // no LF: still pending
	if len(m.rxEntries) != 0 || len(m.pendingBytes) != 2 {
		t.Fatalf("precondition: entries=%d pending=%d", len(m.rxEntries), len(m.pendingBytes))
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1}) // -> HEX
	if len(m.pendingBytes) != 0 {
		t.Fatalf("pending bytes should be committed: %d", len(m.pendingBytes))
	}
	if len(m.rxLines) != 1 || m.rxLines[0] != rxMark+"41 54" {
		t.Fatalf("hex rerender = %q", m.rxLines)
	}
}

// Alt+5 on the built-in MODBUS slot must put the complete RTU request on the
// wire — eight bytes, no CRLF — and echo the same bytes in HEX view.
func TestSerialMonitorModbusQuickSlotSendsFullFrame(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.setLineEnd("crlf") // ASCII sends append CRLF; this slot must not
	m.quickCmds = serialconfig.DefaultQuickCmds()

	m.fireQuick(4) // MODBUS slot
	if len(port.writes) != 1 {
		t.Fatalf("writes = % X", port.writes)
	}
	want := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0A}
	if !bytes.Equal(port.writes[0], want) {
		t.Fatalf("wire = % X, want % X", port.writes[0], want)
	}
	if m.txCount != 8 {
		t.Fatalf("txCount = %d, want 8", m.txCount)
	}

	m.hexView = true
	m.rebuildRx()
	row := m.rxLines[len(m.rxLines)-1]
	if !strings.Contains(row, "01 03 00 00 00 01 84 0A") {
		t.Fatalf("echo = %q", row)
	}
	if strings.Contains(row, "0D") {
		t.Fatalf("hex slot picked up a line ending: %q", row)
	}
}

// Receiving while a HEX view is active used to group bytes by pane width.
// Switching back to ASCII then painted "AT..OK..ERR" on a single row because
// the breaks were no longer recognizable. Entries now follow device lines in
// every mode, so only the rendering changes.
func TestSerialMonitorHexReceivedLinesRerenderAsAscii(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
	m.hexView = true
	m.appendRx([]byte("AT\r\nOK\r\nERR\r\n"))
	if len(m.rxEntries) != 3 {
		t.Fatalf("entries = %d, want one per line", len(m.rxEntries))
	}

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1}) // -> HEX+ASCII
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1}) // -> ASCII
	want := []string{rxMark + "AT", rxMark + "OK", rxMark + "ERR"}
	if len(m.rxLines) != len(want) {
		t.Fatalf("ascii rows = %q, want %q", m.rxLines, want)
	}
	for i := range want {
		if m.rxLines[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, m.rxLines[i], want[i])
		}
	}
}

// Devices that end lines with a bare CR (no LF) never produced a line break;
// the ASCII pane grew one ever-longer row until the width budget cut it.
func TestSerialMonitorCrOnlyStreamGroupsLines(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
	m.appendRx([]byte("AAA\rBBB\rCCC\r"))

	want := []string{rxMark + "AAA", rxMark + "BBB"}
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
	m2 := newTestMonitor(80, 24)
	m2.port = &fakeSerialPort{}
	m2.appendRx([]byte("OK\r"))
	m2.appendRx([]byte("\nnext\r\n"))
	if len(m2.rxLines) != 2 || m2.rxLines[0] != rxMark+"OK" || m2.rxLines[1] != rxMark+"next" {
		t.Fatalf("split CRLF rows = %q", m2.rxLines)
	}
}

// F2 must not change what goes on the wire: ASCII send appends CRLF, so the
// hex the box shows after the switch has to carry that suffix.
func TestSerialMonitorF2KeepsWireBytes(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.setLineEnd("crlf")

	m.input.SetValue("AT")
	m.sendCurrent()
	asciiWire := string(port.writes[len(port.writes)-1])
	if asciiWire != "AT\r\n" {
		t.Fatalf("ascii wire = %q", asciiWire)
	}

	m.input.SetValue("AT")
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if got := m.input.Value(); got != "41 54 0D 0A" {
		t.Fatalf("hex box = %q, want the full wire bytes", got)
	}
	m.sendCurrent()
	if got := string(port.writes[len(port.writes)-1]); got != asciiWire {
		t.Fatalf("hex wire = %q, want %q", got, asciiWire)
	}

	// Switching back restores the text form (suffix stripped, re-added on send).
	m.input.SetValue("41 54 0D 0A")
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if got := m.input.Value(); got != "AT" {
		t.Fatalf("ascii box = %q, want AT", got)
	}
	if strings.Contains(m.status, i18n.T("serial.monitor.tx_input_cleared")) {
		t.Fatalf("status = %q, text should convert cleanly", m.status)
	}
	m.sendCurrent()
	if got := string(port.writes[len(port.writes)-1]); got != "AT\r\n" {
		t.Fatalf("round-trip wire = %q", got)
	}

	// With the suffix disabled nothing is added in either mode.
	m.setLineEnd("none")
	m.input.SetValue("AT")
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if got := m.input.Value(); got != "41 54" {
		t.Fatalf("no-suffix hex box = %q", got)
	}
}

func TestSerialMonitorHexInputAcceptsCommonSpellings(t *testing.T) {
	m := newTestMonitor(80, 24)
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
func TestSerialMonitorTxEchoWrapsInsteadOfTruncating(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(40, 24)
	m.port = &fakeSerialPort{}
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
func TestSerialMonitorFileEchoIsBounded(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
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
func TestSerialMonitorTxEchoMatchesWireBytes(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.setLineEnd("crlf")

	m.input.SetValue("AT")
	m.sendCurrent()
	entry := m.rxEntries[len(m.rxEntries)-1]
	if len(port.writes) != 1 || string(port.writes[0]) != "AT\r\n" {
		t.Fatalf("wire = %q", port.writes)
	}

	m.hexView = true
	m.rebuildRx()
	row := m.rxLines[len(m.rxLines)-1]
	if !strings.Contains(row, "41 54 0D 0A") {
		t.Fatalf("hex echo = %q, want the appended CRLF visible", row)
	}

	m.hexView = false
	m.rebuildRx()
	row = m.rxLines[len(m.rxLines)-1]
	if !strings.HasSuffix(row, "AT") {
		t.Fatalf("ascii echo = %q, want just AT", row)
	}

	// A hex send wrote no suffix, so nothing may appear in either view.
	m.hexSend = true
	m.input.SetValue("01 03")
	m.sendCurrent()
	entry = m.rxEntries[len(m.rxEntries)-1]
	if string(entry.raw) != string([]byte{1, 3}) {
		t.Fatalf("hex echo raw = % X, want 01 03", entry.raw)
	}
	if entry.asciiTrim != 0 {
		t.Fatalf("hex send should carry no suffix, asciiTrim = %d", entry.asciiTrim)
	}

	// A quick ASCII slot behaves like a typed command; a hex slot does not.
	port.writes = nil
	m.hexSend = false
	m.quickCmds = []serialconfig.QuickCmd{
		{Label: "GMR", Value: "AT+GMR"},
		{Label: "MB", Value: "01 03 00 00 00 01 84 0A", Hex: true},
	}
	m.fireQuick(0)
	m.fireQuick(1)
	if string(port.writes[0]) != "AT+GMR\r\n" {
		t.Fatalf("ascii slot wire = %q", port.writes[0])
	}
	if len(port.writes[1]) != 8 {
		t.Fatalf("hex slot wire = % X", port.writes[1])
	}
	mb := m.rxEntries[len(m.rxEntries)-1]
	if mb.asciiTrim != 0 {
		t.Fatalf("hex slot echo trimmed bytes: %d", mb.asciiTrim)
	}
}

// The quick bar used to swallow every key it did not handle, so after Tab the
// F1/F2 mode switches (and the digits that fire a slot) did nothing while the
// bar owned focus — the status label was the only thing that ever changed.
func TestSerialMonitorQuickBarKeepsHotkeys(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyTab})
	if !m.quickFocus {
		t.Fatal("tab should focus the quick bar")
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF1})
	if !m.hexView {
		t.Fatal("F1 must keep switching the RX view while the quick bar has focus")
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if !m.hexSend {
		t.Fatal("F2 must keep switching the send mode while the quick bar has focus")
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	if len(port.writes) != 1 || len(port.writes[0]) != 8 {
		t.Fatalf("digit 5 should fire the MODBUS slot, wrote % X", port.writes)
	}
}

// A binary frame echoed in the ASCII view must still read as a frame: the
// escaped form ("\x01\x03\x00...") looks like garbage next to the slot that
// fired it, and made the F1 switch look broken.
func TestSerialMonitorQuickHexFrameEchoesAsHexInAsciiView(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.hexView, m.hexAscii, m.hexSend = false, false, false

	m.fireQuick(4) // MODBUS
	if len(port.writes) != 1 || !bytes.Equal(port.writes[0], []byte{0x01, 0x03, 0, 0, 0, 1, 0x84, 0x0A}) {
		t.Fatalf("modbus wire = % X", port.writes)
	}
	row := m.renderTxEntry(m.rxEntries[len(m.rxEntries)-1])[0]
	if !strings.Contains(row, "01 03 00 00 00 01 84 0A") {
		t.Fatalf("ascii-view echo = %q, want the frame bytes", row)
	}
	if strings.Contains(row, `\x01`) {
		t.Fatalf("ascii-view echo escaped the frame: %q", row)
	}
}

// Typed text in the ASCII view must keep its readable form: only frames with
// no text form at all (NUL bytes / invalid UTF-8) fall back to hex.
func TestSerialMonitorAsciiEchoKeepsTextForControlBytes(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}
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
func TestSerialMonitorSendBoxFollowsMode(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.port = &fakeSerialPort{}

	if m.input.Prompt != "> " {
		t.Fatalf("ascii prompt = %q", m.input.Prompt)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if m.input.Prompt != i18n.T("serial.monitor.send_prompt_hex") {
		t.Fatalf("hex prompt = %q", m.input.Prompt)
	}
	if !strings.Contains(m.input.Placeholder, "01 03") {
		t.Fatalf("hex placeholder = %q, want a byte example", m.input.Placeholder)
	}
	if frame := m.View(); !strings.Contains(frame, "hex> ") {
		t.Fatal("send box does not show the HEX prompt")
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if m.input.Prompt != "> " {
		t.Fatalf("ascii prompt after switch back = %q", m.input.Prompt)
	}
}

// Both fields of a quick command are live inputs at once, like the TUI's
// other forms: Tab only moves the caret, so text typed into one field is
// never reloaded away by switching to the other.
func TestSerialQuickEditorBothFieldsLive(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = []serialconfig.QuickCmd{{Label: "OLD", Value: "AT"}}
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
func TestSerialQuickListDoesNotEchoIdenticalPayload(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = []serialconfig.QuickCmd{
		{Label: "AT", Value: "AT"},
		{Label: "reboot", Value: "AT+RST"},
	}
	m.clampQuick()
	m.quickEditSel = 0

	var atRow, rebootRow string
	for _, ln := range strings.Split(ansi.Strip(m.editorView()), "\n") {
		if strings.Contains(ln, " AT") && strings.Contains(ln, "[ASCII]") && atRow == "" {
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
func TestSerialQuickListMarksEmptySlot(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = []serialconfig.QuickCmd{{Label: "", Value: ""}}
	m.clampQuick()
	if !strings.Contains(ansi.Strip(m.editorView()), i18n.T("serial.quick.unnamed")) {
		t.Fatal("empty slot should be labelled, not left as a gap")
	}
}

// While typing, Enter commits and Esc cancels — the footer has to say so, or
// the same key silently means two different things.
func TestSerialQuickEditorSwapsFooterWhileTyping(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.openEditor()
	m.quickCmds = serialconfig.DefaultQuickCmds()
	m.clampQuick()

	browse := ansi.Strip(m.editorView())
	if !strings.Contains(browse, i18n.T("serial.quick.help1")) {
		t.Fatal("browse footer should describe opening a field")
	}
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyEnter})
	typing := ansi.Strip(m.editorView())
	if strings.Contains(typing, i18n.T("serial.quick.help1")) {
		t.Fatal("footer still advertises the browse binding while typing")
	}
	if !strings.Contains(typing, i18n.T("serial.quick.edit_help1")) {
		t.Fatalf("typing footer missing:/n%s", typing)
	}
}

// Switching the global send mode has to retarget the saved quick slots too.
// Before this, the header said TX:HEX while slot 1 still held "AT" as ASCII —
// the two disagreed, which read as "the mode switch did nothing to quick
// commands". The bytes each slot sends must stay identical across the switch.
func TestSerialQuickSlotsFollowGlobalMode(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	port := &fakeSerialPort{}
	m.port = port
	m.quickCmds = serialconfig.DefaultQuickCmds()
	m.clampQuick()

	// Capture the ASCII wire bytes of a text slot and the binary MODBUS slot.
	m.fireQuick(0)
	atWire := append([]byte(nil), port.writes[len(port.writes)-1]...)
	m.fireQuick(4)
	modbusWire := append([]byte(nil), port.writes[len(port.writes)-1]...)

	// F2 to HEX: text slots become hex text, binary frame stays hex.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if !m.hexSend || !m.quickCmds[0].Hex {
		t.Fatal("F2 should put the session and the text slot in HEX")
	}
	if m.quickCmds[0].Value != "41 54 0D 0A" {
		t.Fatalf("slot 1 hex = %q, want 41 54 0D 0A", m.quickCmds[0].Value)
	}
	if !m.quickCmds[4].Hex || m.quickCmds[4].Value != "01 03 00 00 00 01 84 0A" {
		t.Fatalf("MODBUS slot changed: %+v", m.quickCmds[4])
	}
	// The exception marker must not paint the whole bar: in HEX mode no slot
	// disagrees with the global mode, so none should carry "*".
	bar := m.quickBarRows(m.innerWidth - 4)
	if strings.Contains(strings.Join(bar[:], ""), "*") {
		t.Fatalf("HEX bar should have no exception marker: %q", bar)
	}

	// Firing after the switch sends the exact same bytes.
	m.fireQuick(0)
	if got := port.writes[len(port.writes)-1]; !bytes.Equal(got, atWire) {
		t.Fatalf("AT wire changed across switch: % X want % X", got, atWire)
	}
	m.fireQuick(4)
	if got := port.writes[len(port.writes)-1]; !bytes.Equal(got, modbusWire) {
		t.Fatalf("MODBUS wire changed across switch: % X want % X", got, modbusWire)
	}

	// Switch back: text slot returns to readable text, bytes still identical.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2})
	if m.hexSend || m.quickCmds[0].Hex {
		t.Fatal("F2 back should restore ASCII mode and the text slot")
	}
	if m.quickCmds[0].Value != "AT" {
		t.Fatalf("slot 1 ascii = %q, want AT", m.quickCmds[0].Value)
	}
	// Only the binary frame now disagrees, so exactly one slot is starred.
	bar = m.quickBarRows(m.innerWidth - 4)
	if strings.Count(strings.Join(bar[:], ""), "*") != 1 {
		t.Fatalf("ASCII bar should star only the binary frame: %q", bar)
	}
}

// A newly added slot must inherit the active send mode. Otherwise adding a
// slot while TX:HEX immediately produced an ASCII slot that disagreed with the
// header — the same inconsistency the batch retarget was meant to remove.
func TestSerialQuickNewSlotInheritsMode(t *testing.T) {
	m := newTestMonitor(80, 24)
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF2}) // HEX
	if !m.hexSend {
		t.Fatal("F2 should enable HEX")
	}
	m.openEditor()
	// openEditor reloads from disk; set the slots after it so the new slot
	// inherits the session's HEX mode rather than the file's default.
	m.quickCmds = nil
	m.clampQuick()
	m, _ = m.updateEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if len(m.quickCmds) != 1 || !m.quickCmds[0].Hex {
		t.Fatalf("new slot should inherit HEX mode: %+v", m.quickCmds)
	}
}

// The editor's own F2 and the global F2 must follow one rule. A hex slot
// carrying an ASCII command plus CRLF has a text form, so toggling it in the
// editor converts it to "AT" — the same result the global switch produces.
func TestSerialQuickEditorToggleMatchesGlobal(t *testing.T) {
	i18n.SetLang("en")
	defer i18n.SetLang(i18n.LangZHCN)
	m := newTestMonitor(80, 24)
	m.openEditor()
	// openEditor reloads from disk; install the fixture afterwards.
	m.quickCmds = []serialconfig.QuickCmd{{Label: "AT", Value: "41 54 0D 0A", Hex: true}}
	m.clampQuick()
	m.quickEditSel = 0

	// Global mode is ASCII here, so a hex slot is the exception; editor F2
	// brings it back to text with the line ending stripped.
	m.updateEditor(tea.KeyMsg{Type: tea.KeyF2})
	if m.quickCmds[0].Hex || m.quickCmds[0].Value != "AT" {
		t.Fatalf("editor F2 = %+v, want AT", m.quickCmds[0])
	}
}

func TestSerialMacOptionArrowPagesQuickBar(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.quickCmds = makeQuickLabels(9) // two pages
	m.quickKey = serialconfig.QuickKeyOption
	m.pageKey = "alt"

	// Stock macOS text editing sends CSI 1;5 C/D, which Bubble Tea decodes
	// as ctrl+arrow.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if m.quickPage != 1 {
		t.Fatalf("Option+Right (ctrl+right token) left page at %d, want 1", m.quickPage)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if m.quickPage != 0 {
		t.Fatalf("Option+Left (ctrl+left token) left page at %d, want 0", m.quickPage)
	}
	// ESC b/f (word left/right) is another common mapping.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}, Alt: true})
	if m.quickPage != 1 {
		t.Fatalf("ESC f left page at %d, want 1", m.quickPage)
	}
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true})
	if m.quickPage != 0 {
		t.Fatalf("ESC b left page at %d, want 0", m.quickPage)
	}
	// "Option as Meta" sends CSI 1;3, reported as alt+arrow.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.quickPage != 1 {
		t.Fatalf("alt+right left page at %d, want 1", m.quickPage)
	}
	// Ctrl stays Ctrl when the page modifier is set to Ctrl.
	m.pageKey = "ctrl"
	m.quickPage = 0
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyCtrlRight})
	if m.quickPage != 1 {
		t.Fatalf("Ctrl page mode ignored ctrl+right, page = %d", m.quickPage)
	}
}

func TestSerialOptionQuickAcceptsAllDigitEncodings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.setLineEnd("none")
	port := &fakeSerialPort{}
	m.port = port
	m.quickKey = serialconfig.QuickKeyOption
	m.quickCmds = []serialconfig.QuickCmd{
		{Label: "one", Value: "V1"},
		{Label: "two", Value: "V2"},
	}

	// Plain glyph (default macOS), ESC+glyph ("Option as Meta"), and
	// ESC+digit (ASCII layout / Esc+) must all fire the same slots.
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'¡'}})
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'™'}, Alt: true})
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	want := []string{"V1", "V2", "V1"}
	if len(port.writes) != len(want) {
		t.Fatalf("writes = %q, want %q", port.writes, want)
	}
	for i := range want {
		if string(port.writes[i]) != want[i] {
			t.Fatalf("write %d = %q, want %q", i, port.writes[i], want[i])
		}
	}
}

func TestSerialAltDigitFromRawKeyType(t *testing.T) {
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
		idx, ok := serialQuickDigitFromKey(tc.key)
		if idx != tc.want || ok != tc.ok {
			t.Fatalf("key %+v -> (%d,%v), want (%d,%v)", tc.key, idx, ok, tc.want, tc.ok)
		}
	}
}

func TestSerialPrefixSettingDefaultsOnAndToggles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	m.appendRx([]byte("OK\r\n"))
	if len(m.rxLines) == 0 || !strings.HasPrefix(m.rxLines[0], rxMark) {
		t.Fatalf("serial prefixes are on by default, got %q", m.rxLines)
	}
	budgetWithPrefix := m.lineBudget()

	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	for i := 0; i < settingShowPrefix; i++ {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsSel != settingShowPrefix {
		t.Fatalf("selection = %d, want prefix row %d", m.settingsSel, settingShowPrefix)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if m.prefs.ShowPrefix || !m.showPrefix {
		t.Fatal("arrow key must only change the draft, not the live session")
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	if m.showPrefix {
		t.Fatal("saved setting not applied to the live session")
	}
	got, err := serialconfig.LoadSerialPrefs()
	if err != nil || got.ShowPrefix {
		t.Fatalf("persisted show_prefix = %v err=%v, want false", got.ShowPrefix, err)
	}
	if strings.Contains(m.rxLines[0], rxMark) || !strings.Contains(m.rxLines[0], "OK") {
		t.Fatalf("existing rows were not repainted: %q", m.rxLines)
	}
	if m.lineBudget() <= budgetWithPrefix {
		t.Fatalf("line budget must grow when the prefix is hidden: %d -> %d", budgetWithPrefix, m.lineBudget())
	}
	m.appendRx([]byte("NEXT\r\n"))
	if strings.Contains(m.rxLines[len(m.rxLines)-1], rxMark) {
		t.Fatalf("new rows should have no prefix: %q", m.rxLines)
	}
}

func TestSerialQuickKeySettingAppliesImmediately(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
	if m.prefs.QuickKey != serialconfig.QuickKeyAuto {
		t.Fatalf("default quick key = %q, want auto", m.prefs.QuickKey)
	}
	liveBefore := m.quickKey
	m, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyF10})
	for i := 0; i < settingQuickKey; i++ {
		m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsSel != settingQuickKey {
		t.Fatalf("selection = %d, want quick key row %d", m.settingsSel, settingQuickKey)
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyRight})
	if m.prefs.QuickKey != serialconfig.QuickKeyAlt || m.quickKey != liveBefore {
		t.Fatal("draft change must not alter the live session")
	}
	m, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	got, err := serialconfig.LoadSerialPrefs()
	if err != nil || got.QuickKey != serialconfig.QuickKeyAlt {
		t.Fatalf("persisted quick_key = %q err=%v, want alt", got.QuickKey, err)
	}
	if m.quickKey != serialconfig.QuickKeyAlt {
		t.Fatalf("applied quick key = %q, want alt", m.quickKey)
	}
}

func TestSerialSettingsRowsFitMonitorWithNewRows(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newTestMonitor(80, 24)
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
