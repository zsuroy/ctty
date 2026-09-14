package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/serialconfig"
	"github.com/zsuroy/ctty/internal/ui/theme"
)

// Common baud rates selectable in the serial connect form.
var baudRates = []int{9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600}

type serialConnectFormModel struct {
	form     *huh.Form
	viewport viewport.Model
	styles   Styles
	width    int
	height   int
	device   serialconfig.SerialDevice
	err      string

	baudVal       string
	baudSelectVal string
	baudCustomVal string
	dataBitsVal   string
	dataSelectVal string
	dataCustomVal string
	parityVal     string
	stopBitsVal   string
	confirmVal    bool

	done      bool
	cancelled bool
}

func newSerialConnectForm(styles Styles, width, height int, dev serialconfig.SerialDevice) *serialConnectFormModel {
	m := &serialConnectFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		device:     dev,
		confirmVal: true,
	}

	baudStr := strconv.Itoa(dev.BaudRate)
	if dev.BaudRate <= 0 {
		baudStr = "115200"
	}
	m.baudVal = baudStr
	m.baudCustomVal = baudStr

	isStandardBaud := false
	for _, b := range baudRates {
		if strconv.Itoa(b) == baudStr {
			isStandardBaud = true
			break
		}
	}
	if isStandardBaud {
		m.baudSelectVal = baudStr
	} else {
		m.baudSelectVal = "custom"
	}

	dataStr := strconv.Itoa(dev.DataBits)
	if dev.DataBits <= 0 {
		dataStr = "8"
	}
	m.dataBitsVal = dataStr
	m.dataCustomVal = dataStr
	if dataStr == "5" || dataStr == "6" || dataStr == "7" || dataStr == "8" {
		m.dataSelectVal = dataStr
	} else {
		m.dataSelectVal = "custom"
	}

	m.parityVal = dev.Parity
	if m.parityVal == "" {
		m.parityVal = "none"
	}

	m.stopBitsVal = strconv.Itoa(dev.StopBits)
	if m.stopBitsVal == "" || m.stopBitsVal == "0" {
		m.stopBitsVal = "1"
	}

	m.buildForm()
	return m
}

func (m *serialConnectFormModel) buildForm() {
	innerW := formPageInnerWidth(m.width)
	if innerW < 20 {
		innerW = 20
	}

	currentHuhTheme := theme.GetTheme(m.styles.Theme.ID).HuhTheme()

	fields := []huh.Field{
		huh.NewSelect[string]().
			Key("baud").
			Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("serial.field_baud"), ":"))).
			Inline(true).
			Options(
				huh.NewOption("115200", "115200"),
				huh.NewOption("9600", "9600"),
				huh.NewOption("19200", "19200"),
				huh.NewOption("38400", "38400"),
				huh.NewOption("57600", "57600"),
				huh.NewOption("230400", "230400"),
				huh.NewOption("460800", "460800"),
				huh.NewOption("921600", "921600"),
				huh.NewOption(i18n.T("serial.baud_custom"), "custom"),
			).
			Value(&m.baudSelectVal),
	}

	if m.baudSelectVal == "custom" {
		fields = append(fields,
			huh.NewInput().
				Key("baud_custom").
				Title(i18n.T("serial.field_custom_baud")).
				Prompt("> ").
				Placeholder("115200 (e.g. 500000, 1500000)").
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return errors.New("baud rate is required")
					}
					n, err := strconv.Atoi(s)
					if err != nil || n <= 0 {
						return errors.New("baud rate must be a positive integer")
					}
					return nil
				}).
				Value(&m.baudCustomVal),
		)
	}

	fields = append(fields,
		huh.NewSelect[string]().
			Key("data").
			Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("serial.field_data"), ":"))).
			Inline(true).
			Options(
				huh.NewOption("8", "8"),
				huh.NewOption("7", "7"),
				huh.NewOption("6", "6"),
				huh.NewOption("5", "5"),
				huh.NewOption(i18n.T("serial.data_custom"), "custom"),
			).
			Value(&m.dataSelectVal),
	)

	if m.dataSelectVal == "custom" {
		fields = append(fields,
			huh.NewInput().
				Key("data_custom").
				Title(i18n.T("serial.field_custom_data")).
				Prompt("> ").
				Placeholder("8 (5, 6, 7, 8, 9)").
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return errors.New("data bits is required")
					}
					n, err := strconv.Atoi(s)
					if err != nil || n < 5 || n > 9 {
						return errors.New("data bits must be between 5 and 9")
					}
					return nil
				}).
				Value(&m.dataCustomVal),
		)
	}

	fields = append(fields,
		huh.NewSelect[string]().
			Key("parity").
			Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("serial.field_parity"), ":"))).
			Inline(true).
			Options(
				huh.NewOption(i18n.T("serial.parity_none"), "none"),
				huh.NewOption(i18n.T("serial.parity_even"), "even"),
				huh.NewOption(i18n.T("serial.parity_odd"), "odd"),
			).
			Value(&m.parityVal),

		huh.NewSelect[string]().
			Key("stop").
			Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("serial.field_stop"), ":"))).
			Inline(true).
			Options(
				huh.NewOption("1", "1"),
				huh.NewOption("2", "2"),
			).
			Value(&m.stopBitsVal),

		huh.NewConfirm().
			Key("confirm").
			Title("").
			Affirmative(i18n.T("serial.connect_button")).
			Negative(i18n.T("form.btn_cancel")).
			Value(&m.confirmVal),
	)

	m.form = huh.NewForm(
		huh.NewGroup(fields...),
	).WithLayout(huh.LayoutStack).
		WithTheme(currentHuhTheme).
		WithWidth(innerW).
		WithShowHelp(false)
}

func (m *serialConnectFormModel) Init() tea.Cmd {
	if m.form != nil {
		return m.form.Init()
	}
	return nil
}

func (m *serialConnectFormModel) nextField() tea.Cmd {
	if m.form == nil {
		return nil
	}
	focused := m.form.GetFocusedField()
	if focused != nil && focused.GetKey() == "confirm" {
		for m.form.GetFocusedField().GetKey() != "baud" {
			m.form.PrevField()
		}
		return nil
	}
	return m.form.NextField()
}

func (m *serialConnectFormModel) prevField() tea.Cmd {
	if m.form == nil {
		return nil
	}
	focused := m.form.GetFocusedField()
	if focused != nil && focused.GetKey() == "baud" {
		for m.form.GetFocusedField().GetKey() != "confirm" {
			m.form.NextField()
		}
		return nil
	}
	return m.form.PrevField()
}

func (m *serialConnectFormModel) focusField(targetKey string) {
	if m.form == nil || targetKey == "" {
		return
	}
	current := m.form.GetFocusedField()
	if current == nil || current.GetKey() == targetKey {
		return
	}
	for i := 0; i < 20; i++ {
		m.form.NextField()
		focused := m.form.GetFocusedField()
		if focused == nil || focused.GetKey() == targetKey {
			return
		}
	}
}

func (m *serialConnectFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		innerW := formPageInnerWidth(m.width)
		if innerW < 20 {
			innerW = 20
		}
		if m.form != nil {
			m.form.WithWidth(innerW)
		}
		return m, nil

	case tea.KeyMsg:
		switch {
		case msg.String() == "esc" || msg.String() == "ctrl+c":
			m.cancelled = true
			return m, nil
		case msg.String() == "ctrl+s":
			m.submit()
			return m, nil
		case msg.Type == tea.KeyTab || msg.String() == "tab":
			return m, m.nextField()
		case msg.Type == tea.KeyShiftTab || msg.String() == "shift+tab" || msg.String() == "backtab":
			return m, m.prevField()
		case msg.Type == tea.KeyDown || msg.String() == "down":
			return m, m.nextField()
		case msg.Type == tea.KeyUp || msg.String() == "up":
			return m, m.prevField()
		case msg.Type == tea.KeyEnter || msg.String() == "enter":
			if m.form != nil {
				focused := m.form.GetFocusedField()
				if focused != nil && focused.GetKey() == "confirm" {
					if m.confirmVal {
						m.submit()
					} else {
						m.cancelled = true
					}
					return m, nil
				}
			}
			return m, m.nextField()
		}
	}

	if m.form == nil {
		return m, nil
	}

	prevBaudCustom := (m.baudSelectVal == "custom")
	prevDataCustom := (m.dataSelectVal == "custom")

	formModel, cmd := m.form.Update(msg)
	if f, ok := formModel.(*huh.Form); ok {
		m.form = f
	}

	newBaudCustom := (m.baudSelectVal == "custom")
	newDataCustom := (m.dataSelectVal == "custom")

	if prevBaudCustom != newBaudCustom || prevDataCustom != newDataCustom {
		targetFocus := "baud"
		if prevDataCustom != newDataCustom {
			targetFocus = "data"
		}

		m.buildForm()
		_ = m.form.Init()
		m.focusField(targetFocus)
	}

	if m.form.State == huh.StateCompleted {
		if m.confirmVal {
			m.submit()
			return m, nil
		}
		m.cancelled = true
		return m, nil
	}

	if m.form.State == huh.StateAborted {
		m.cancelled = true
		return m, nil
	}

	return m, cmd
}

func (m *serialConnectFormModel) submit() {
	var baud int
	if m.baudVal != "" && m.baudVal != "115200" && m.baudVal != m.baudSelectVal {
		baud, _ = strconv.Atoi(strings.TrimSpace(m.baudVal))
	} else if m.baudSelectVal == "custom" {
		baud, _ = strconv.Atoi(strings.TrimSpace(m.baudCustomVal))
	} else {
		baud, _ = strconv.Atoi(m.baudSelectVal)
	}
	if baud <= 0 {
		baud = 115200
	}
	m.baudVal = strconv.Itoa(baud)

	var dataBits int
	if m.dataBitsVal != "" && m.dataBitsVal != "8" && m.dataBitsVal != m.dataSelectVal {
		dataBits, _ = strconv.Atoi(strings.TrimSpace(m.dataBitsVal))
	} else if m.dataSelectVal == "custom" {
		dataBits, _ = strconv.Atoi(strings.TrimSpace(m.dataCustomVal))
	} else {
		dataBits, _ = strconv.Atoi(m.dataSelectVal)
	}
	if dataBits <= 0 {
		dataBits = 8
	}
	m.dataBitsVal = strconv.Itoa(dataBits)

	stopBits, _ := strconv.Atoi(m.stopBitsVal)
	if stopBits <= 0 {
		stopBits = 1
	}
	parity := strings.TrimSpace(m.parityVal)
	if parity == "" {
		parity = "none"
	}

	m.device.BaudRate = baud
	m.device.DataBits = dataBits
	m.device.Parity = parity
	m.device.StopBits = stopBits
	m.done = true
}

func (m *serialConnectFormModel) getFieldTitle(key string) string {
	switch key {
	case "baud":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("serial.field_baud"), ":"))
	case "baud_custom":
		return i18n.T("serial.field_custom_baud")
	case "data":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("serial.field_data"), ":"))
	case "data_custom":
		return i18n.T("serial.field_custom_data")
	case "parity":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("serial.field_parity"), ":"))
	case "stop":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("serial.field_stop"), ":"))
	case "confirm":
		return i18n.T("serial.connect_button")
	default:
		return ""
	}
}

func (m *serialConnectFormModel) View() string {
	boxWidth := m.width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	container := m.styles.FormContainer
	if m.height < 24 {
		container = container.Padding(0, 1)
	}

	innerW := boxWidth - container.GetHorizontalFrameSize()
	if innerW < 10 {
		innerW = 10
	}

	// Header parts must be width-constrained before lipgloss.Height
	// measures them: device path + name wrap on narrow frames, and the
	// uncounted rows push the bottom border past the terminal.
	titleText := m.styles.Header.Width(innerW).Render(i18n.T("serial.edit_title"))
	devInfo := m.styles.HelpText.Width(innerW).Render(fmt.Sprintf("%s: %s   %s: %s",
		i18n.T("serial.col_device"), m.device.Device,
		i18n.T("serial.col_name"), m.device.Name))
	header := lipgloss.JoinVertical(lipgloss.Left, titleText, devInfo)

	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("serial.help_edit"))

	formView := ""
	if m.form != nil {
		formView = m.form.View()
	}

	frameH := container.GetVerticalFrameSize()
	headerH := lipgloss.Height(header)
	helpH := lipgloss.Height(helpText)

	targetBoxH := m.height
	if m.height >= 14 {
		targetBoxH = m.height - 1
	}

	// Floor of 1: with tall wrapped headers a two-row minimum pushed the
	// frame past the terminal height and clipped the bottom border.
	overhead := frameH + headerH + helpH + 2
	availableH := targetBoxH - overhead
	if availableH < 1 {
		availableH = 1
	}

	formH := lipgloss.Height(formView)
	var bodyView string
	if formH <= availableH {
		bodyView = formView
	} else {
		m.viewport.Width = innerW
		m.viewport.Height = availableH
		m.viewport.SetContent(formView)

		// Auto-scroll viewport to keep focused field in view
		if m.form != nil {
			focused := m.form.GetFocusedField()
			if focused != nil {
				key := focused.GetKey()
				title := m.getFieldTitle(key)
				if title != "" {
					lines := strings.Split(formView, "\n")
					for idx, line := range lines {
						if strings.Contains(line, title) {
							if idx < m.viewport.YOffset {
								m.viewport.SetYOffset(idx)
							} else if idx+2 >= m.viewport.YOffset+availableH {
								m.viewport.SetYOffset(idx + 3 - availableH)
							}
							break
						}
					}
				}
			}
		}
		bodyView = m.viewport.View()
	}

	contentParts := []string{header, ""}
	if m.err != "" {
		contentParts = append(contentParts, m.styles.ErrorText.Width(innerW).MaxHeight(2).Render("❌ "+m.err), "")
	}
	contentParts = append(contentParts, bodyView, "", helpText)

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)
	box := container.Width(boxWidth).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}
