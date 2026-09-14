package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/serialconfig"
)

func TestSerialAddForm_Basic(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(80)

	ports := []string{"/dev/ttyUSB0", "/dev/ttyUSB1"}
	form := newSerialAddForm(styles, 80, 24, ports)
	if form == nil {
		t.Fatal("expected newSerialAddForm to return non-nil")
	}

	if form.deviceVal != "/dev/ttyUSB0" {
		t.Errorf("expected deviceVal to default to first port, got %q", form.deviceVal)
	}

	view := form.View()
	if !strings.Contains(view, "Add Serial") {
		t.Fatalf("expected view to contain 'Add Serial', got:\n%s", view)
	}

	// Esc cancels
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !form.cancelled {
		t.Fatal("expected form to be cancelled on Esc")
	}
}

func TestDebugSerialFormRender(t *testing.T) {
	i18n.SetLang("zh")
	styles := NewStyles(80)
	form := newSerialAddForm(styles, 80, 24, []string{"/dev/ttyUSB0"})
	_ = form.Init()
	t.Logf("ADD FORM VIEW:\n%s", form.View())

	dev := serialconfig.SerialDevice{
		Name:     "Cisco-Console",
		Device:   "/dev/ttyUSB0",
		BaudRate: 115200,
		DataBits: 8,
		Parity:   "none",
		StopBits: 1,
	}
	conn := newSerialConnectForm(styles, 80, 24, dev)
	_ = conn.Init()
	t.Logf("CONNECT FORM VIEW:\n%s", conn.View())
}

func TestSerialConnectForm_Basic(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(80)

	dev := serialconfig.SerialDevice{
		Name:     "Cisco-Console",
		Device:   "/dev/ttyUSB0",
		BaudRate: 9600,
		DataBits: 8,
		Parity:   "none",
		StopBits: 1,
	}

	form := newSerialConnectForm(styles, 80, 24, dev)
	if form == nil {
		t.Fatal("expected newSerialConnectForm to return non-nil")
	}

	if form.baudVal != "9600" {
		t.Errorf("expected baudVal 9600, got %q", form.baudVal)
	}

	view := form.View()
	if !strings.Contains(view, "Cisco-Console") {
		t.Fatalf("expected view to contain device name, got:\n%s", view)
	}

	// Tab advances through fields
	initialFocus := form.form.GetFocusedField().GetKey()
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	nextFocus := form.form.GetFocusedField().GetKey()
	if initialFocus == nextFocus {
		t.Errorf("expected focus to change on Tab from %s", initialFocus)
	}

	// Esc cancels
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !form.cancelled {
		t.Fatal("expected form to be cancelled on Esc")
	}
}

func TestSerialForms_ResponsiveSizes(t *testing.T) {
	styles := NewStyles(80)
	dev := serialconfig.SerialDevice{
		Name:     "Switch-Console",
		Device:   "/dev/ttyS0",
		BaudRate: 115200,
		DataBits: 8,
		Parity:   "none",
		StopBits: 1,
	}

	sizes := []struct {
		w, h int
	}{
		{80, 24},
		{60, 16},
		{50, 12},
		{100, 35},
	}

	for _, sz := range sizes {
		addForm := newSerialAddForm(styles, sz.w, sz.h, nil)
		addView := addForm.View()
		if addView == "" {
			t.Errorf("size %dx%d: addView is empty", sz.w, sz.h)
		}

		connForm := newSerialConnectForm(styles, sz.w, sz.h, dev)
		connView := connForm.View()
		if connView == "" {
			t.Errorf("size %dx%d: connView is empty", sz.w, sz.h)
		}
	}
}

func TestSerialAddForm_ManualInputAndTabNavigation(t *testing.T) {
	styles := NewStyles(80)
	form := newSerialAddForm(styles, 80, 16, []string{"/dev/ttyUSB0"})
	_ = form.Init()

	// Verify custom baud rate and data bits can be set directly (manual input)
	form.nameVal = "ESP8266-Board"
	form.deviceVal = "/dev/ttyUSB0"
	form.baudVal = "74880"
	form.dataBitsVal = "7"
	form.submit()

	if form.err != "" {
		t.Fatalf("unexpected error on submit: %s", form.err)
	}

	// Clean up added test device
	_ = serialconfig.Delete("ESP8266-Board")

	// Tab navigation in small height (h=16)
	form = newSerialAddForm(styles, 80, 16, []string{"/dev/ttyUSB0"})
	_ = form.Init()

	expectedOrder := []string{"name", "device", "baud", "data", "parity", "stop", "confirm"}
	for _, expectedKey := range expectedOrder {
		currentKey := form.form.GetFocusedField().GetKey()
		if currentKey != expectedKey {
			t.Fatalf("expected focus %q, got %q", expectedKey, currentKey)
		}
		// View must never exceed terminal height
		view := form.View()
		lines := strings.Split(view, "\n")
		if len(lines) > 16 {
			t.Errorf("h=16 on field %s: rendered %d lines, exceeds 16", currentKey, len(lines))
		}
		if !strings.Contains(view, "╰") || !strings.Contains(view, "╯") {
			t.Errorf("h=16 on field %s: missing bottom border corners in view", currentKey)
		}
		_, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	// After confirm, Tab loops back to name
	if form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected loop back to 'name', got %q", form.form.GetFocusedField().GetKey())
	}
}

func TestSerialConnectForm_ManualInputAndTabNavigation(t *testing.T) {
	styles := NewStyles(80)
	dev := serialconfig.SerialDevice{
		Name:     "Custom-Dev",
		Device:   "/dev/ttyS1",
		BaudRate: 115200,
		DataBits: 8,
		Parity:   "none",
		StopBits: 1,
	}
	form := newSerialConnectForm(styles, 80, 16, dev)
	_ = form.Init()

	// Manual input of custom baud rate
	form.baudVal = "250000"
	form.dataBitsVal = "6"
	form.submit()

	if form.device.BaudRate != 250000 {
		t.Fatalf("expected BaudRate 250000, got %d", form.device.BaudRate)
	}
	if form.device.DataBits != 6 {
		t.Fatalf("expected DataBits 6, got %d", form.device.DataBits)
	}

	// Tab navigation in small height (h=16)
	form = newSerialConnectForm(styles, 80, 16, dev)
	_ = form.Init()

	expectedOrder := []string{"baud", "data", "parity", "stop", "confirm"}
	for _, expectedKey := range expectedOrder {
		currentKey := form.form.GetFocusedField().GetKey()
		if currentKey != expectedKey {
			t.Fatalf("expected focus %q, got %q", expectedKey, currentKey)
		}
		view := form.View()
		lines := strings.Split(view, "\n")
		if len(lines) > 16 {
			t.Errorf("h=16 on field %s: rendered %d lines, exceeds 16", currentKey, len(lines))
		}
		if !strings.Contains(view, "╰") || !strings.Contains(view, "╯") {
			t.Errorf("h=16 on field %s: missing bottom border corners in view", currentKey)
		}
		_, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	}

	// After confirm, Tab loops back to baud
	if form.form.GetFocusedField().GetKey() != "baud" {
		t.Fatalf("expected loop back to 'baud', got %q", form.form.GetFocusedField().GetKey())
	}
}

func TestSerialForm_UpDownAndEnterNavigation(t *testing.T) {
	styles := NewStyles(80)
	dev := serialconfig.SerialDevice{
		Name:     "Test-Dev",
		Device:   "/dev/ttyUSB0",
		BaudRate: 115200,
		DataBits: 8,
		Parity:   "none",
		StopBits: 1,
	}

	// 1. Test KeyDown (↓) navigation in serialConnectForm
	form := newSerialConnectForm(styles, 80, 24, dev)
	_ = form.Init()
	expectedOrder := []string{"baud", "data", "parity", "stop", "confirm"}

	for i, expected := range expectedOrder {
		focused := form.form.GetFocusedField().GetKey()
		if focused != expected {
			t.Fatalf("step %d: expected focus %q, got %q", i, expected, focused)
		}
		_, _ = form.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	// Down on confirm loops back to baud
	if form.form.GetFocusedField().GetKey() != "baud" {
		t.Fatalf("expected KeyDown loop back to 'baud', got %q", form.form.GetFocusedField().GetKey())
	}

	// 2. Test KeyUp (↑) navigation in serialConnectForm
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyUp})
	if form.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected KeyUp loop back to 'confirm', got %q", form.form.GetFocusedField().GetKey())
	}

	// 3. Test KeyEnter navigation in serialConnectForm
	form = newSerialConnectForm(styles, 80, 24, dev)
	_ = form.Init()
	for i := 0; i < 4; i++ { // baud -> data -> parity -> stop -> confirm
		_, _ = form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if form.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected focus on 'confirm' after 4 Enters, got %q", form.form.GetFocusedField().GetKey())
	}
	// Enter on confirm submits form
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !form.done {
		t.Fatalf("expected form to be done after Enter on confirm")
	}

	// 4. Test KeyDown (↓) navigation in serialAddForm
	addForm := newSerialAddForm(styles, 80, 24, []string{"/dev/ttyUSB0"})
	_ = addForm.Init()
	if addForm.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected initial focus 'name', got %q", addForm.form.GetFocusedField().GetKey())
	}
	// Press Enter to move from name to device
	_, _ = addForm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if addForm.form.GetFocusedField().GetKey() != "device" {
		t.Fatalf("expected focus 'device' after Enter on name, got %q", addForm.form.GetFocusedField().GetKey())
	}
	// Press Down to move from device to baud
	_, _ = addForm.Update(tea.KeyMsg{Type: tea.KeyDown})
	if addForm.form.GetFocusedField().GetKey() != "baud" {
		t.Fatalf("expected focus 'baud' after Down on device, got %q", addForm.form.GetFocusedField().GetKey())
	}
	// Press Up to move back to device
	_, _ = addForm.Update(tea.KeyMsg{Type: tea.KeyUp})
	if addForm.form.GetFocusedField().GetKey() != "device" {
		t.Fatalf("expected focus 'device' after Up on baud, got %q", addForm.form.GetFocusedField().GetKey())
	}
}

func TestSerialForm_BaudRatePresetsAndCustom(t *testing.T) {
	styles := NewStyles(80)
	dev := serialconfig.SerialDevice{
		Name:     "Dev1",
		Device:   "/dev/ttyUSB0",
		BaudRate: 115200,
		DataBits: 8,
		Parity:   "none",
		StopBits: 1,
	}

	// Default baud rate should be 115200
	form := newSerialConnectForm(styles, 80, 24, dev)
	_ = form.Init()
	if form.baudSelectVal != "115200" {
		t.Fatalf("expected default baudSelectVal '115200', got %q", form.baudSelectVal)
	}

	// View should contain 115200 preset
	view := form.View()
	if !strings.Contains(view, "115200") {
		t.Fatalf("expected view to contain default baud 115200, got:\n%s", view)
	}

	// Switch baudSelectVal to custom
	form.baudSelectVal = "custom"
	form.baudCustomVal = "1500000"
	form.submit()

	if form.device.BaudRate != 1500000 {
		t.Fatalf("expected custom baud rate 1500000, got %d", form.device.BaudRate)
	}
}

func TestSerialList_EditShortcutKey(t *testing.T) {
	styles := NewStyles(80)
	listModel := NewSerialForm(styles, 80, 24)
	// Add a dummy filtered device
	listModel.filteredDevices = []serialconfig.SerialDevice{
		{
			Name:     "Switch-Console",
			Device:   "/dev/ttyUSB0",
			BaudRate: 9600,
			DataBits: 8,
			Parity:   "none",
			StopBits: 1,
		},
	}
	listModel.buildTable()

	// Press 'e' on list item
	_, _ = listModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if listModel.mode != serialConnectSettings {
		t.Fatalf("expected mode serialConnectSettings after 'e', got %d", listModel.mode)
	}
	if listModel.connectForm == nil {
		t.Fatal("expected connectForm to be initialized after 'e'")
	}
	if listModel.connectForm.baudSelectVal != "9600" {
		t.Fatalf("expected connectForm to have baud 9600, got %q", listModel.connectForm.baudSelectVal)
	}
}

func TestSerialAddForm_BaudSwitchDoesNotBounceToFirstName(t *testing.T) {
	styles := NewStyles(80)
	addForm := newSerialAddForm(styles, 80, 24, []string{"/dev/ttyUSB0"})
	_ = addForm.Init()

	// Initial focus is name
	if addForm.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected initial focus 'name', got %q", addForm.form.GetFocusedField().GetKey())
	}

	// Move to device (field 1)
	_, _ = addForm.Update(tea.KeyMsg{Type: tea.KeyDown})
	if addForm.form.GetFocusedField().GetKey() != "device" {
		t.Fatalf("expected focus 'device', got %q", addForm.form.GetFocusedField().GetKey())
	}

	// Move to baud (field 2)
	_, _ = addForm.Update(tea.KeyMsg{Type: tea.KeyDown})
	if addForm.form.GetFocusedField().GetKey() != "baud" {
		t.Fatalf("expected focus 'baud', got %q", addForm.form.GetFocusedField().GetKey())
	}

	// Switch baud preset using right arrow
	initialBaud := addForm.baudSelectVal
	_, _ = addForm.Update(tea.KeyMsg{Type: tea.KeyRight})

	// Focus MUST NOT bounce back to 'name'! It must remain on 'baud'
	focusedAfterRight := addForm.form.GetFocusedField().GetKey()
	if focusedAfterRight != "baud" {
		t.Fatalf("focus bounced from 'baud' to %q when pressing right arrow!", focusedAfterRight)
	}

	// Switch baud preset using left arrow
	_, _ = addForm.Update(tea.KeyMsg{Type: tea.KeyLeft})
	focusedAfterLeft := addForm.form.GetFocusedField().GetKey()
	if focusedAfterLeft != "baud" {
		t.Fatalf("focus bounced from 'baud' to %q when pressing left arrow!", focusedAfterLeft)
	}

	// Switch to custom (which is the last option, reachable via left arrow from 115200)
	_, _ = addForm.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if addForm.baudSelectVal != "custom" {
		// If left didn't wrap to custom, cycle right to custom
		for addForm.baudSelectVal != "custom" {
			_, _ = addForm.Update(tea.KeyMsg{Type: tea.KeyRight})
		}
	}
	focusedAfterCustom := addForm.form.GetFocusedField().GetKey()
	if focusedAfterCustom != "baud" {
		t.Fatalf("expected focus to stay on 'baud' after switching to custom, got %q", focusedAfterCustom)
	}

	// Advance to baud_custom
	_, _ = addForm.Update(tea.KeyMsg{Type: tea.KeyDown})
	if addForm.form.GetFocusedField().GetKey() != "baud_custom" {
		t.Fatalf("expected focus on 'baud_custom', got %q", addForm.form.GetFocusedField().GetKey())
	}

	_ = initialBaud
}

func TestSerialInfoView(t *testing.T) {
	i18n.SetLang("zh")
	styles := NewStyles(80)
	dev := serialconfig.SerialDevice{
		Name:     "TestDevice",
		Device:   "/dev/ttyUSB0",
		BaudRate: 115200,
		DataBits: 8,
		Parity:   "none",
		StopBits: 1,
	}
	m := &serialFormModel{
		styles:          styles,
		width:           80,
		height:          24,
		filteredDevices: []serialconfig.SerialDevice{dev},
		mode:            serialInfo,
		infoIndex:       0,
	}

	view := m.renderInfo()
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╰") {
		t.Errorf("expected rounded border corners in serial info view")
	}
	if !strings.Contains(view, "TestDevice") {
		t.Errorf("missing device name in serial info view")
	}
	if !strings.Contains(view, "/dev/ttyUSB0") {
		t.Errorf("missing device path in serial info view")
	}
	if !strings.Contains(view, "115200") {
		t.Errorf("missing baud rate in serial info view")
	}
	lines := strings.Split(view, "\n")
	if len(lines) > 24 {
		t.Errorf("expected view lines <= 24, got %d", len(lines))
	}

	// Test key exits: esc, q, i
	for _, key := range []string{"esc", "q", "i"} {
		m.mode = serialInfo
		m.infoIndex = 0
		var keyMsg tea.KeyMsg
		if key == "esc" {
			keyMsg = tea.KeyMsg{Type: tea.KeyEsc}
		} else {
			keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		}
		updated, _ := m.handleInfoKeys(keyMsg)
		um := updated.(*serialFormModel)
		if um.mode != serialList {
			t.Errorf("key %s did not return to serialList", key)
		}
	}
}
