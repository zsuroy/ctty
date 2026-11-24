package ui

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/serialconfig"
	"go.bug.st/serial"
)

const (
	quickCmdCount = 8
	quickCmdCols  = 4
	rxMark        = "RX < "
	txMark        = "TX > "
	settingsCount = 13
)

// Settings rows, in display order. Kept as an index enum so the value editor
// and the renderer cannot drift apart.
const (
	settingHexView = iota
	settingHexSend
	settingShowTime
	settingLineEnd
	settingHexAscii
	settingLogAppend
	settingLogDir
	settingPageKey
	settingQuickKey
	settingShowPrefix
	settingShowQuickLabel
	settingInterval
	settingMaxLines
)

// Messages used by the in-app serial assistant. The read loop emits one
// message per chunk so Bubble Tea keeps owning the event stream; the old
// pass-through bridge (tea.Exec) is untouched for CLI direct connections.
type (
	serialRxMsg      struct{ data []byte }
	serialReadErrMsg struct{ err error }
	serialTickMsg    struct{}
	// serialMonitorDoneMsg closes the monitor and returns to the serial list.
	serialMonitorDoneMsg struct{ err error }
	// serialMonitorSwitchMsg asks the parent to hand the port to the
	// full-screen passthrough bridge.
	serialMonitorSwitchMsg struct{}
)

const (
	serialMonitorReadSize = 1024
	serialReadTimeout     = 100 * time.Millisecond
)

// rxEntry keeps one logical receive/TX line in raw form. Display lines
// (rxLines) are derived from these, so switching the view mode (F1/F3)
// re-renders the whole pane instead of only affecting future data.
type rxEntry struct {
	// raw is the complete wire sequence, line endings included. Grouping
	// keeps the CRLF that terminated an ASCII line: it is real data, so the
	// HEX views must show it, while the ASCII view trims it when rendering.
	// Dropping it here made a mode switch lose two bytes per line and the
	// byte count on screen disagree with the RX counter.
	raw   []byte
	at    time.Time
	label string // TX quick-command label; "" when none
	tx    bool
	off   int // rxOffset at the entry's first byte (HEX+ASCII gutter)
	// total is how many bytes were really written for a TX echo; raw may be
	// a capped prefix of it, so the echo can report the tail it did not paint.
	total int
	// asciiTrim is how many trailing bytes of raw were the line ending the
	// app appended. The HEX echo prints them (they are on the wire) and the
	// ASCII echo hides them (they are Enter's artifact, not user text).
	asciiTrim int
}

type serialMonitorModel struct {
	styles     Styles
	width      int
	height     int
	boxWidth   int
	innerWidth int
	dev        serialconfig.SerialDevice
	port       serial.Port
	closed     bool
	// ioMu serialises port I/O against Close. go.bug.st/serial v1.8.0 has a
	// known race on Windows (bugst/go-serial #57, #219): CloseHandle while an
	// overlapped ReadFile is pending is undefined behaviour and crashes the
	// process with Exception 0xc0000008. Read timeout is 100ms, so a Close
	// waiting on this lock never blocks the UI meaningfully.
	ioMu         sync.Mutex
	rx           viewport.Model
	rxFollow     bool // auto-scroll to newest data; paused when user scrolls up
	unseen       int  // RX lines arrived while the view is parked in history
	input        textinput.Model
	rxLines      []string // rendered display lines (derived from rxEntries)
	rxEntries    []rxEntry
	pendingBytes []byte
	// pendingCR remembers that the last received byte was a bare CR, so the
	// next byte decides whether it was half of a CRLF or a line break on its
	// own. It lives across reads because a chunk can end right between them.
	pendingCR      bool
	rxOffset       int
	pendingAt      time.Time
	rxCount        int
	txCount        int
	hexView        bool
	hexAscii       bool
	hexSend        bool
	showTime       bool
	showPrefix     bool // paint "RX < " / "TX > " on monitor rows
	showQuickLabel bool // paint "[label] " on quick-command TX echoes
	crlf           bool // legacy mirror of lineEnd == "crlf"
	lineEnd        string
	repeat         bool
	interval       time.Duration
	lastPayload    []byte
	// lastAddLineEnding remembers how lastPayload was framed when it was
	// sent. F5 loops replay it, so a hex frame must not pick up a CRLF just
	// because the user switched send mode in the meantime.
	lastAddLineEnding bool
	history           []string
	histIdx           int
	status            string

	// RX rate: byte count over a rolling ~1s window for the status line.
	rxRate      int
	rxRateBytes int
	rxRateSince time.Time

	// Receive-pane keyword highlight (Ctrl+F).
	searchInput   textinput.Model
	searchOn      bool
	searchQuery   string
	searchMatches []int
	searchMatch   int

	// Raw receive capture. Ctrl+L toggles a session log; unlike rxLines it
	// stores bytes exactly as read, so GBK folding never destroys evidence.
	logRecording bool
	logPath      string
	logRaw       []byte

	// Quick-command bar: Alt+1..8 fires a slot on the current page directly;
	// Tab focuses the bar where up/down select and left/right change page;
	// Enter fires. F9 opens the editable list (n add / x delete).
	quickCmds    []serialconfig.QuickCmd
	quickFocus   bool
	quickSel     int // global index into quickCmds, across all pages
	quickPage    int // current page (0-based), 8 slots each
	quickLoadErr error
	pageKey      string // "alt" or "ctrl": which modifier pages the quick bar
	quickKey     string // effective modifier for Alt/Option+1..8, resolved from prefs.QuickKey

	// Quick-command editor (F9).
	quickEditing   bool
	quickEditSel   int
	quickEditTop   int // first visible row in the editor list window
	quickInput     textinput.Model
	quickLabelIn   textinput.Model
	quickEditOn    bool // editing the selected slot's fields
	quickEditLabel bool // when editing: true = label focused, false = value focused
	quickMsg       string
	quickDirty     bool // edits not yet written by Ctrl+S
	quickEscArmed  bool // first Esc warned about unsaved edits

	// Persistent preferences and the F10 settings page.
	prefs        serialconfig.SerialPrefs
	maxLines     int
	settingsOpen bool
	settingsSel  int
	settingsMsg  string

	// helpExpanded trades receive rows for the three-line key map. Default
	// stays compact so an 80x24 cmd window keeps six receive rows.
	helpExpanded bool
}

// serialConnectMode decides which UI a device opens in. The order mirrors
// serialconfig.ResolveMode — CLI override, device mode, global setting,
// built-in per-entry default — so the TUI and the CLI can never disagree.
func (m Model) serialConnectMode(dev serialconfig.SerialDevice) string {
	global := ""
	if m.appConfig != nil {
		global = config.NormalizeSerialStartMode(m.appConfig.SerialStartMode)
	}
	return serialconfig.ResolveMode(m.serialForceMode, dev.Mode, global, m.serialOnly)
}

func newSerialMonitor(styles Styles, width, height int, dev serialconfig.SerialDevice) *serialMonitorModel {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = i18n.T("serial.monitor.placeholder")
	input.CharLimit = 4096
	input.Focus()

	quickInput := textinput.New()
	quickInput.Prompt = ""
	quickInput.CharLimit = 256

	// The label is a field of its own: both it and the value are live inputs
	// while the editor is open, like every other form in the TUI.
	quickLabelIn := textinput.New()
	quickLabelIn.Prompt = ""
	quickLabelIn.CharLimit = 256

	searchInput := textinput.New()
	searchInput.Prompt = "/"
	searchInput.CharLimit = 128

	cmds, err := serialconfig.LoadQuickCmds()
	prefs, prefsErr := serialconfig.LoadSerialPrefs()

	m := &serialMonitorModel{
		styles:         styles,
		dev:            dev,
		rx:             viewport.New(40, 5),
		rxFollow:       true,
		input:          input,
		interval:       prefs.Interval(),
		maxLines:       prefs.MaxLines,
		hexView:        prefs.HexView,
		hexAscii:       prefs.HexAscii,
		hexSend:        prefs.HexSend,
		showTime:       prefs.ShowTime,
		showPrefix:     prefs.ShowPrefix,
		showQuickLabel: prefs.ShowQuickLabel,
		crlf:           prefs.Crlf,
		lineEnd:        prefs.LineEnding,
		pageKey:        prefs.PageKey,
		quickKey:       serialconfig.ResolveQuickKey(prefs.QuickKey, runtime.GOOS),
		status:         i18n.T("serial.monitor.connected"),
		quickCmds:      cmds,
		quickLoadErr:   err,
		quickInput:     quickInput,
		quickLabelIn:   quickLabelIn,
		searchInput:    searchInput,
		rxRateSince:    time.Now(),
		prefs:          prefs,
	}
	if prefsErr != nil {
		// Config file is malformed; defaults are already applied. Surface a
		// short hint but let the assistant open normally.
		m.status = i18n.T("serial.monitor.prefs_load_error")
	}
	m.syncSendPrompt()
	// The send mode is stored separately from the slots. Apply it to the
	// loaded slots at startup too, so a session that ended in HEX does not
	// come back with an ASCII quick bar under a "TX:HEX" header.
	m.retargetQuickCmds(m.hexSend)
	m.setSize(width, height)
	m.clampQuick()
	return m
}

// syncSendPrompt makes the send box itself say which mode it is in. The
// mode used to live only in the tiny "TX:" token of the status row, so a box
// reading "Type data" while TX said HEX looked like the switch had been
// ignored — the prompt and placeholder now follow the mode.
func (m *serialMonitorModel) syncSendPrompt() {
	if m.hexSend {
		m.input.Prompt = i18n.T("serial.monitor.send_prompt_hex")
		m.input.Placeholder = i18n.T("serial.monitor.placeholder_hex")
	} else {
		m.input.Prompt = "> "
		m.input.Placeholder = i18n.T("serial.monitor.placeholder")
	}
	m.fitSendInput()
}

// fitSendInput sizes the field for the current prompt: HEX mode spends three
// extra cells on "hex> ", so subtracting a fixed two left the text spilling
// past the box.
func (m *serialMonitorModel) fitSendInput() {
	if m.rx.Width <= 0 {
		return
	}
	w := m.rx.Width - ansi.StringWidth(m.input.Prompt) - 1 // +1 for the cursor
	if w < 4 {
		w = 4
	}
	m.input.Width = w
}

func (m *serialMonitorModel) setSize(width, height int) {
	m.width, m.height = width, height
	boxW := width - 4
	if boxW < 30 {
		boxW = 30
	}
	m.boxWidth = boxW
	inner := boxW - m.styles.FormContainer.GetHorizontalFrameSize()
	if inner < 20 {
		inner = 20
	}
	m.innerWidth = inner

	rxW := inner - 2 // rounded border
	if rxW < 8 {
		rxW = 8
	}
	targetH := height
	if targetH < 12 {
		targetH = 12
	}
	// Fixed rows around the receive viewport: title(1) + status(1) + blank(1)
	// + rx border(2) + blank(1) + tx box(3) + blank(1) + quick rows(2)
	// + blank(1) + help(1) = 14, plus the container's border(2)+padding(2)=4.
	// Expanded help adds two rows, so an 80x24 window still keeps four RX rows.
	fixed := 18
	if m.helpExpanded {
		fixed += 2
	}
	rxH := targetH - fixed
	if rxH < 3 {
		rxH = 3
	}
	m.rx.Width = rxW
	m.rx.Height = rxH
	// The prompt grows in HEX mode ("hex> "), so the field width follows it
	// instead of assuming the two-cell "> ".
	m.fitSendInput()
	m.quickInput.Width = inner - 2
	if m.quickInput.Width < 8 {
		m.quickInput.Width = 8
	}
	m.quickLabelIn.Width = m.quickInput.Width
	m.refreshRxContent()
}

// Init opens the port. Called explicitly by the parent when the user
// confirms the serial parameters, so an open failure can be shown in-frame.
func (m *serialMonitorModel) Init() tea.Cmd {
	blink := m.input.Focus()
	port, err := serialconfig.Open(m.dev)
	if err != nil {
		m.status = fmt.Sprintf(i18n.T("serial.monitor.open_error"), err)
		return blink
	}
	m.port = port
	_ = port.SetReadTimeout(serialReadTimeout)
	return tea.Batch(blink, serialReadCmd(port, &m.ioMu))
}

func (m *serialMonitorModel) Update(msg tea.Msg) (*serialMonitorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.setSize(msg.Width, msg.Height)
		return m, nil

	case serialRxMsg:
		if m.closed || m.port == nil {
			return m, nil
		}
		m.appendRx(msg.data)
		return m, serialReadCmd(m.port, &m.ioMu)

	case serialReadErrMsg:
		if m.closed || m.port == nil {
			return m, nil
		}
		return m, m.doneCmd(msg.err)

	case serialTickMsg:
		if m.closed || !m.repeat {
			return m, nil
		}
		m.sendRepeat()
		return m, serialTickCmd(m.interval)

	case tea.MouseMsg:
		// Wheel over the workspace scrolls the receive pane by three lines.
		// Text selection on this page therefore needs Shift+drag; the rest of
		// the TUI keeps native selection because mouse reporting is only
		// switched on for this view.
		if m.settingsOpen || m.quickEditing || m.searchOn {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollRx(-3)
		case tea.MouseButtonWheelDown:
			m.scrollRx(3)
		}
		return m, nil
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		if m.settingsOpen {
			return m.updateSettings(key)
		}
		if m.quickEditing {
			return m.updateEditor(key)
		}
		if m.searchOn {
			return m.updateSearch(key)
		}
		return m.updateKeys(key)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *serialMonitorModel) updateKeys(key tea.KeyMsg) (*serialMonitorModel, tea.Cmd) {
	k := m.macOptionArrow(key.String())
	// Alt/Option+1..8 fires a quick command on the current page directly,
	// without moving input focus. Read the raw KeyMsg first: some terminals
	// encode Alt+digit in a way whose String() is empty, and the string path
	// alone would miss it.
	if n, ok := serialQuickDigitFromKey(key); ok {
		m.fireQuick(m.quickPage*quickCmdCount + n)
		return m, nil
	}
	if n, ok := m.serialQuickIndex(k); ok {
		m.fireQuick(m.quickPage*quickCmdCount + n)
		return m, nil
	}
	// Alt/Ctrl+Left/Right pages the quick bar from any focus. Which modifier
	// is active comes from the F10 setting (default Alt); the other is ignored
	// so the setting is a real switch, not two always-on aliases. Plain
	// Left/Right stays with the text cursor / quick-bar focus.
	switch k {
	case "alt+left":
		if m.pageKey == "alt" {
			m.changeQuickPage(-1)
			return m, nil
		}
	case "alt+right":
		if m.pageKey == "alt" {
			m.changeQuickPage(1)
			return m, nil
		}
	case "ctrl+left":
		if m.pageKey == "ctrl" {
			m.changeQuickPage(-1)
			return m, nil
		}
	case "ctrl+right":
		if m.pageKey == "ctrl" {
			m.changeQuickPage(1)
			return m, nil
		}
	}
	// Alt+↑/↓ scrolls the receive pane one line at a time. Plain ↑/↓ stays
	// with the send history: after dozens of commands that history is the
	// thing the arrows must not fight with.
	switch k {
	case "alt+up":
		m.scrollRx(-1)
		return m, nil
	case "alt+down":
		m.scrollRx(1)
		return m, nil
	}

	// Scroll keys work from either focus. The quick bar used to swallow
	// PgUp/PgDn, which made the pane look unscrollable once commands were
	// saved and Tab had moved focus there.
	switch k {
	case "pgup":
		m.scrollRx(-m.rx.Height)
		return m, nil
	case "pgdown":
		m.scrollRx(m.rx.Height)
		return m, nil
	case "home":
		m.scrollRx(-m.rx.TotalLineCount())
		return m, nil
	case "end":
		return m, m.jumpLive()
	}

	if m.quickFocus {
		switch k {
		case "tab":
			m.quickFocus = false
			m.input.Focus()
			return m, nil
		case "up":
			if len(m.quickCmds) > 0 {
				m.setQuickSel((m.quickSel - 1 + len(m.quickCmds)) % len(m.quickCmds))
			}
			return m, nil
		case "down":
			if len(m.quickCmds) > 0 {
				m.setQuickSel((m.quickSel + 1) % len(m.quickCmds))
			}
			return m, nil
		case "left":
			m.changeQuickPage(-1)
			return m, nil
		case "right":
			m.changeQuickPage(1)
			return m, nil
		case "enter":
			m.fireQuick(m.quickSel)
			return m, nil
		}
		// A digit fires the slot wearing that badge, so the bar can be driven
		// from the keyboard once Tab has moved focus onto it.
		if len(k) == 1 && k[0] >= '1' && k[0] <= '8' {
			m.fireQuick(m.quickPage*quickCmdCount + int(k[0]-'1'))
			return m, nil
		}
		// The bar only owns its own navigation keys. Everything else (F1..F12,
		// Ctrl shortcuts, Esc) falls through to the global handler — swallowing
		// them here meant F1/F2 did nothing after Tab, which read as "switching
		// the mode does not work".
	}

	switch k {
	case "esc":
		return m, m.doneCmd(nil)
	case "f8":
		return m, m.switchCmd()
	case "f9":
		m.openEditor()
		return m, nil
	case "f10":
		m.openSettings()
		return m, nil
	case "ctrl+e":
		if m.lineEnd == "crlf" {
			m.setLineEnd("none")
		} else {
			m.setLineEnd("crlf")
		}
		m.prefs.LineEnding = m.lineEnd
		m.status = fmt.Sprintf(i18n.T("serial.monitor.line_end"), lineEndLabel(m.lineEnd))
		return m, nil
	case "ctrl+l":
		m.toggleRxLog()
		return m, nil
	case "ctrl+n":
		m.jumpSearch(1)
		return m, nil
	case "ctrl+p":
		m.jumpSearch(-1)
		return m, nil
	case "ctrl+f":
		m.searchOn = true
		m.searchQuery = ""
		m.searchInput.SetValue("")
		m.input.Blur()
		m.searchInput.Focus()
		return m, m.searchInput.Focus()
	case "tab":
		if len(m.quickCmds) > 0 {
			m.quickFocus = true
			m.input.Blur()
		}
		return m, nil
	case "enter":
		m.sendCurrent()
		return m, nil
	case "up":
		m.historyPrev()
		return m, nil
	case "down":
		m.historyNext()
		return m, nil
	case "f1":
		switch {
		case m.hexAscii:
			m.hexAscii, m.hexView = false, false
		case m.hexView:
			m.hexView, m.hexAscii = false, true
		default:
			m.hexView = true
		}
		// Re-render the whole pane: the mode switch applies to the content
		// already on screen, not just to future bytes. A half-received line
		// is committed first so it is grouped by the new mode's rules too.
		m.flushRxLine()
		m.rebuildRx()
		m.status = fmt.Sprintf(i18n.T("serial.monitor.rx_mode"), m.rxModeLabel())
		return m, nil
	case "f2":
		m.hexSend = !m.hexSend
		// The box must look like the mode it is in ("hex> " + byte example),
		// not just say so in the status row.
		m.syncSendPrompt()
		// Whatever is half-typed in the box was written for the old mode;
		// re-express it, or Enter would quietly send different bytes.
		cleared := m.retargetInput()
		// The quick slots follow the same switch: a mode change rewrites how
		// each command is displayed, so the bar no longer contradicts the
		// "TX:" token above it. Binary frames and hex streams without a line
		// ending stay HEX, and the feedback says how many that was.
		converted, kept := m.retargetQuickCmds(m.hexSend)
		// Mirror F1: say which mode is now live, otherwise the only clue is
		// the tiny TX: field in the status bar.
		m.status = fmt.Sprintf(i18n.T("serial.monitor.tx_mode"), modeWord(m.hexSend, "HEX", "ASCII"))
		if converted > 0 {
			m.status += " · " + fmt.Sprintf(i18n.T("serial.quick.retargeted"), converted)
		}
		if kept > 0 {
			m.status += " · " + fmt.Sprintf(i18n.T("serial.quick.kept_hex"), kept)
		}
		if cleared {
			m.status += " · " + i18n.T("serial.monitor.tx_input_cleared")
		}
		return m, nil
	case "f3":
		m.showTime = !m.showTime
		m.rebuildRx()
		return m, nil
	case "f4":
		m.rxLines = nil
		m.rxEntries = nil
		m.pendingBytes = nil
		m.pendingCR = false
		m.pendingAt = time.Time{}
		// The gutter restarts with the counter, so the HEX+ASCII offsets and
		// the RX byte count describe the same session.
		m.rxOffset = 0
		m.rxCount = 0
		m.txCount = 0
		m.rxFollow = true
		m.unseen = 0
		m.refreshRxContent()
		return m, nil
	case "f12":
		// Help lives on F12, not "?": in a serial assistant the question
		// mark is a payload character (AT+GMR? and friends) and must reach
		// the send box like any other rune.
		m.helpExpanded = !m.helpExpanded
		m.setSize(m.width, m.height)
		m.status = i18n.T("serial.monitor.help_toggle")
		return m, nil
	case "f5":
		m.repeat = !m.repeat
		if m.repeat {
			return m, serialTickCmd(m.interval)
		}
		return m, nil
	case "f6":
		m.stepInterval(-100 * time.Millisecond)
		return m, nil
	case "f7":
		m.stepInterval(100 * time.Millisecond)
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

// jumpLive re-arms auto-scroll and clears the unseen counter.
func (m *serialMonitorModel) jumpLive() tea.Cmd {
	m.rxFollow = true
	m.unseen = 0
	m.rx.GotoBottom()
	return nil
}

// scrollRx moves the receive viewport by delta lines and keeps the follow
// flag honest: reaching the last line re-arms auto-scroll, anything above it
// parks the view so incoming bytes stop competing with what the user is
// reading. SetYOffset clamps, so over-scrolling is harmless.
func (m *serialMonitorModel) scrollRx(delta int) {
	if delta == 0 {
		return
	}
	m.rx.SetYOffset(m.rx.YOffset + delta)
	if m.rx.AtBottom() {
		m.rxFollow = true
		m.unseen = 0
		return
	}
	m.rxFollow = false
}

// quickPageCount is the number of pages in the quick bar. Always at least 1
// so page arithmetic never divides by zero, even with an empty command list.
func (m *serialMonitorModel) quickPageCount() int {
	if len(m.quickCmds) == 0 {
		return 1
	}
	return (len(m.quickCmds) + quickCmdCount - 1) / quickCmdCount
}

// clampQuick keeps page/selection inside the current command list. Called
// after load, add and delete so indices never dangle.
func (m *serialMonitorModel) clampQuick() {
	n := len(m.quickCmds)
	pages := m.quickPageCount()
	if m.quickPage < 0 {
		m.quickPage = 0
	}
	if m.quickPage >= pages {
		m.quickPage = pages - 1
	}
	if n == 0 {
		m.quickSel = 0
		return
	}
	if m.quickSel < 0 {
		m.quickSel = 0
	}
	if m.quickSel >= n {
		m.quickSel = n - 1
	}
}

// setQuickSel selects a global slot index and moves the view to its page.
func (m *serialMonitorModel) setQuickSel(idx int) {
	m.clampQuick()
	if len(m.quickCmds) == 0 {
		return
	}
	m.quickSel = idx
	m.clampQuick()
	m.quickPage = m.quickSel / quickCmdCount
}

// changeQuickPage moves ±1 page, keeping the column when possible, and moves
// the selection onto the new page so Enter/Alt always targets what is shown.
func (m *serialMonitorModel) changeQuickPage(delta int) {
	pages := m.quickPageCount()
	if pages <= 1 {
		return
	}
	p := m.quickPage + delta
	if p < 0 {
		p = pages - 1
	} else if p >= pages {
		p = 0
	}
	col := m.quickSel % quickCmdCount
	first := p * quickCmdCount
	idx := first + col
	if idx >= len(m.quickCmds) {
		idx = len(m.quickCmds) - 1
	}
	m.quickPage = p
	m.setQuickSel(idx)
}

// pageKeyToken is the modifier name shown in the help line's page hint. It
// mirrors the active pageKey setting so the hint never advertises a key that
// is currently ignored.
func (m *serialMonitorModel) pageKeyToken() string {
	return pageKeyLabel(m.pageKey, m.quickKey)
}

// quickKeyToken is the modifier name shown in the help line. It follows the
// resolved quickKey so a Mac reads "Option+#" while Windows/Linux read
// "Alt+#" — the hint never names a key the current mode ignores.
func (m *serialMonitorModel) quickKeyToken() string {
	return pageKeyLabel("alt", m.quickKey)
}

// macOptionArrow normalises the arrow keys macOS terminals emit for
// Option+←/→/↑/↓. Stock text editing sends CSI 1;5 C/D/A/B, which Bubble Tea
// decodes as ctrl+*; "Option as Meta" sends CSI 1;3 and arrives as alt+*.
// Some terminals use ESC b / ESC f for word left/right. In Option mode they
// must all mean the same thing, or paging/scrolling silently does nothing.
func (m *serialMonitorModel) macOptionArrow(k string) string {
	if m.quickKey != serialconfig.QuickKeyOption || m.pageKey == "ctrl" {
		return k // ctrl+arrow is a real binding in these modes
	}
	switch k {
	case "ctrl+left", "alt+b":
		return "alt+left"
	case "ctrl+right", "alt+f":
		return "alt+right"
	case "ctrl+up":
		return "alt+up"
	case "ctrl+down":
		return "alt+down"
	}
	return k
}

// altQuickIndex maps Bubble Tea's Alt+digit key strings to a 0-based slot on
// the current page. ConHost reports these as "alt+1".."alt+8"; some builds
// use an ESC prefix.
func altQuickIndex(k string) (int, bool) {
	if len(k) == 5 && strings.HasPrefix(k, "alt+") {
		if n := k[4] - '0'; n >= 1 && n <= quickCmdCount {
			return int(n) - 1, true
		}
	}
	return 0, false
}

// serialQuickDigitFromKey reads an Alt+digit straight off the KeyMsg instead
// of going through String(). ConHost builds can report the digit as a raw
// Type byte with no Runes, which makes Key.String() return an empty string;
// matching only the rendered form silently drops the shortcut there.
func serialQuickDigitFromKey(key tea.KeyMsg) (int, bool) {
	if !key.Alt {
		return 0, false
	}
	var r rune
	switch {
	case key.Type == tea.KeyRunes && len(key.Runes) == 1:
		r = key.Runes[0]
	case key.Type > 0 && key.Type < 128:
		r = rune(key.Type)
	default:
		return 0, false
	}
	if r >= '1' && r <= rune('0'+quickCmdCount) {
		return int(r - '1'), true
	}
	return 0, false
}

// serialQuickIndex maps a key string to a 0-based slot. "alt" keeps the
// historical behaviour (a real Alt+digit); "option" additionally accepts the
// Unicode glyphs stock macOS sends for ⌥1..⌥8, with or without the ESC
// prefix that "Option as Meta" adds. All encodings fire the same slot.
func (m *serialMonitorModel) serialQuickIndex(k string) (int, bool) {
	if idx, ok := altQuickIndex(k); ok {
		return idx, true
	}
	if m.quickKey == serialconfig.QuickKeyOption {
		if idx, ok := macOptionDigits[k]; ok {
			return idx, true
		}
		if strings.HasPrefix(k, "alt+") {
			if idx, ok := macOptionDigits[strings.TrimPrefix(k, "alt+")]; ok {
				return idx, true
			}
		}
	}
	return 0, false
}

func (m *serialMonitorModel) fireQuick(idx int) {
	if idx < 0 || idx >= len(m.quickCmds) {
		return
	}
	cmd := m.quickCmds[idx]
	payload, err := cmd.Payload()
	if err != nil {
		// Only hex slots can fail here; the raw parser message means nothing
		// to a serial user looking at a slot toggled to HEX by mistake.
		m.status = i18n.T("serial.monitor.badhex_hint")
		return
	}
	if len(payload) == 0 {
		// A blank slot would otherwise "send" nothing and echo a bare tag,
		// which looks like a bug rather than an empty command.
		m.status = i18n.T("serial.quick.no_payload")
		return
	}
	// A quick slot carries its own encoding: ASCII slots are commands that
	// want the line ending, hex slots are frames whose bytes are exact.
	addLineEnding := !cmd.Hex
	m.writePayload(payload, addLineEnding)
	m.txCount += m.wireLen(len(payload), addLineEnding)
	m.appendTxLine(cmd.Label, payload, m.sendSuffix(addLineEnding))
	m.status = fmt.Sprintf(i18n.T("serial.monitor.tx_bytes"), m.wireLen(len(payload), addLineEnding))
	// Slots normally follow the global F2 mode. The only remaining mismatch
	// is a binary frame that had to stay HEX in ASCII mode — say why, instead
	// of leaving the user to think the mode switch was ignored.
	if cmd.Hex && !m.hexSend {
		m.status += " · " + i18n.T("serial.quick.binary_hex")
	}
}

// editorListRows is how many command rows fit in the F9 list without pushing
// the panel past the screen. Fixed overhead: title+blank+rule+label+value+
// blank+help1+help2+msg = 9, plus container border(2)+padding(2) = 4. One more
// row is reserved for the trailing newline the list builder emits.
//
// When the list overflows, the scroll indicator is drawn inside the same
// window, so one row is given up rather than appended — otherwise a short
// terminal pushes the bottom border off screen.
func (m *serialMonitorModel) editorListRows() int {
	rows := m.height - 14
	if rows < 3 {
		rows = 3
	}
	if len(m.quickCmds) > rows {
		rows--
	}
	return rows
}

// clampEditor keeps the editor selection and scroll window inside the list.
func (m *serialMonitorModel) clampEditor() {
	n := len(m.quickCmds)
	if n == 0 {
		m.quickEditSel, m.quickEditTop = 0, 0
		return
	}
	if m.quickEditSel < 0 {
		m.quickEditSel = 0
	}
	if m.quickEditSel >= n {
		m.quickEditSel = n - 1
	}
	vis := m.editorListRows()
	if m.quickEditTop < 0 {
		m.quickEditTop = 0
	}
	if m.quickEditTop > m.quickEditSel {
		m.quickEditTop = m.quickEditSel
	}
	if m.quickEditSel >= m.quickEditTop+vis {
		m.quickEditTop = m.quickEditSel - vis + 1
	}
	if m.quickEditTop > n-vis {
		m.quickEditTop = n - vis
	}
	if m.quickEditTop < 0 {
		m.quickEditTop = 0
	}
}

// commitQuickEdit writes both live fields into the selected slot. The editor
// keeps the label and the value as separate inputs, so a single commit point
// has to carry both.
func (m *serialMonitorModel) commitQuickEdit() {
	if m.quickEditSel < 0 || m.quickEditSel >= len(m.quickCmds) {
		return
	}
	if m.quickCmds[m.quickEditSel].Label != m.quickLabelIn.Value() ||
		m.quickCmds[m.quickEditSel].Value != m.quickInput.Value() {
		m.quickDirty = true
	}
	m.quickCmds[m.quickEditSel].Label = m.quickLabelIn.Value()
	m.quickCmds[m.quickEditSel].Value = m.quickInput.Value()
}

// loadQuickEdit fills both fields from the selected slot and focuses the one
// being edited.
func (m *serialMonitorModel) loadQuickEdit() {
	if m.quickEditSel < 0 || m.quickEditSel >= len(m.quickCmds) {
		m.quickLabelIn.SetValue("")
		m.quickInput.SetValue("")
		return
	}
	m.quickLabelIn.SetValue(m.quickCmds[m.quickEditSel].Label)
	m.quickInput.SetValue(m.quickCmds[m.quickEditSel].Value)
	m.focusQuickField()
}

// focusQuickField puts the caret in the field the editor is on and leaves the
// other one showing its text without a cursor.
func (m *serialMonitorModel) focusQuickField() {
	if m.quickEditLabel {
		m.quickInput.Blur()
		m.quickLabelIn.Focus()
		return
	}
	m.quickLabelIn.Blur()
	m.quickInput.Focus()
}

// finishQuickEdit leaves edit mode and clears the scratch inputs. The editor
// keeps the two field rows in its layout, so hiding them does not change the
// 80x24 frame height.
func (m *serialMonitorModel) finishQuickEdit() {
	m.quickEditOn = false
	m.quickLabelIn.SetValue("")
	m.quickInput.SetValue("")
	m.quickLabelIn.Blur()
	m.quickInput.Blur()
}

func (m *serialMonitorModel) beginQuickEdit(label bool) {
	m.quickEditOn = true
	m.quickEditLabel = label
	m.loadQuickEdit()
}

func (m *serialMonitorModel) openEditor() {
	m.quickEditing = true
	m.quickEditSel = 0
	m.quickEditTop = 0
	m.quickEditOn = false
	m.quickMsg = ""
	m.quickDirty = false
	m.quickEscArmed = false
	cmds, err := serialconfig.LoadQuickCmds()
	if err == nil {
		m.quickCmds = cmds
	}
	m.clampQuick()
	m.clampEditor()
	m.quickLoadErr = err
}

func (m *serialMonitorModel) updateEditor(key tea.KeyMsg) (*serialMonitorModel, tea.Cmd) {
	k := key.String()
	if k != "esc" {
		// Any other key cancels a pending "discard?" prompt, so the next Esc
		// has to ask again.
		m.quickEscArmed = false
	}
	if m.quickEditOn {
		switch k {
		case "enter":
			m.commitQuickEdit()
			m.finishQuickEdit()
			return m, nil
		case "esc":
			m.finishQuickEdit()
			return m, nil
		case "tab":
			// Both fields stay live; Tab only moves the caret between them, so
			// nothing typed is lost and both halves remain visible.
			m.quickEditLabel = !m.quickEditLabel
			m.focusQuickField()
			return m, nil
		case "f2":
			// Same key as the monitor's send mode, same meaning: ASCII/HEX.
			// The text is re-expressed so the bytes a slot sends never change
			// just because its encoding was flipped.
			m.commitQuickEdit()
			m.toggleSlotEncoding()
			m.loadQuickEdit()
			return m, nil
		}
		var cmd tea.Cmd
		if m.quickEditLabel {
			m.quickLabelIn, cmd = m.quickLabelIn.Update(key)
		} else {
			m.quickInput, cmd = m.quickInput.Update(key)
		}
		// Live commit: the list row and the byte count describe the draft
		// while it is being typed, exactly like the other TUI forms.
		m.commitQuickEdit()
		return m, cmd
	}

	switch k {
	case "esc":
		// Leaving with edits that Ctrl+S never wrote would silently throw
		// them away, so the first Esc asks and the second one obeys.
		if m.quickDirty && !m.quickEscArmed {
			m.quickEscArmed = true
			m.quickMsg = i18n.T("serial.quick.unsaved")
			return m, nil
		}
		m.finishQuickEdit()
		m.quickEditing = false
		m.quickEscArmed = false
		m.quickMsg = ""
		m.input.Focus()
		return m, nil
	case "ctrl+s":
		// Save the field currently being edited too, then clear the scratch
		// input so a completed add never leaves stale text on screen.
		if m.quickEditOn {
			m.commitQuickEdit()
			m.finishQuickEdit()
		}
		if err := serialconfig.SaveQuickCmds(m.quickCmds); err != nil {
			m.quickMsg = fmt.Sprintf(i18n.T("serial.quick.save_error"), err)
		} else {
			m.quickDirty = false
			m.quickMsg = i18n.T("serial.quick.saved")
		}
		return m, nil
	case "up":
		if len(m.quickCmds) > 0 {
			m.quickEditSel = (m.quickEditSel - 1 + len(m.quickCmds)) % len(m.quickCmds)
			m.clampEditor()
		}
		return m, nil
	case "down":
		if len(m.quickCmds) > 0 {
			m.quickEditSel = (m.quickEditSel + 1) % len(m.quickCmds)
			m.clampEditor()
		}
		return m, nil
	case "n":
		// Append a blank slot and jump straight into editing its label. Tab
		// then moves to the value; Ctrl+S persists the whole list.
		// A new slot inherits the active send mode, so adding one under HEX
		// does not silently produce an ASCII slot that disagrees with TX:HEX.
		m.quickCmds = append(m.quickCmds, serialconfig.QuickCmd{Label: "", Value: "", Hex: m.hexSend})
		m.quickEditSel = len(m.quickCmds) - 1
		m.clampQuick()
		m.clampEditor()
		m.beginQuickEdit(true)
		m.quickDirty = true
		m.quickMsg = i18n.T("serial.quick.added")
		return m, m.quickInput.Focus()
	case "x":
		if len(m.quickCmds) == 0 {
			return m, nil
		}
		m.quickCmds = append(m.quickCmds[:m.quickEditSel], m.quickCmds[m.quickEditSel+1:]...)
		if m.quickEditSel >= len(m.quickCmds) && m.quickEditSel > 0 {
			m.quickEditSel--
		}
		m.clampQuick()
		m.clampEditor()
		m.quickDirty = true
		m.quickMsg = i18n.T("serial.quick.deleted")
		return m, nil
	case "tab":
		// Edit the label: it is a name, never a payload, so it has no mode.
		if m.quickEditSel < len(m.quickCmds) {
			m.beginQuickEdit(true)
		}
		return m, nil
	case "f2":
		m.toggleSlotEncoding()
		return m, nil
	case "enter":
		if m.quickEditSel < len(m.quickCmds) {
			m.beginQuickEdit(false) // editing an existing slot starts at value
		}
		return m, nil
	}
	return m, nil
}

// toggleSlotEncoding flips the selected slot to the mode opposite the global
// one, using the very same conversion as the global F2 switch. That keeps the
// two F2 keys consistent: retargeting a slot to HEX or ASCII follows one rule
// and preserves the same bytes whichever key was pressed.
func (m *serialMonitorModel) toggleSlotEncoding() {
	if m.quickEditSel < 0 || m.quickEditSel >= len(m.quickCmds) {
		return
	}
	c := &m.quickCmds[m.quickEditSel]
	target := !m.hexSend
	if c.Hex == target {
		// It already matches the global mode; flip to the other one instead
		// so the key always changes something.
		target = m.hexSend
	}
	if m.retargetQuickCmd(c, target) {
		m.quickDirty = true
	} else {
		m.quickMsg = i18n.T("serial.quick.keep_hex")
	}
}

// retargetQuickCmd re-expresses a single slot into the target send mode. It
// is the per-slot primitive shared by the global F2 batch and the editor's
// own F2. It returns false when the bytes have no lossless text form (a
// binary frame), in which case the slot is left untouched.
func (m *serialMonitorModel) retargetQuickCmd(c *serialconfig.QuickCmd, hexSend bool) bool {
	if hexSend {
		if c.Hex {
			return true // already hex
		}
		src := c.Value
		if src == "" {
			src = c.Label // an empty value sends its label
		}
		if src == "" {
			return true
		}
		c.Value = hexText(m.withLineEnding([]byte(src)))
		c.Hex = true
		return true
	}
	if !c.Hex {
		return true
	}
	b, err := hex.DecodeString(normalizeHexInput(c.Value))
	if err != nil || len(b) == 0 {
		return false
	}
	if suffix := m.lineEndSuffix(); len(suffix) > 0 {
		if !bytes.HasSuffix(b, suffix) {
			return false // no line ending: ASCII send would add bytes
		}
		b = b[:len(b)-len(suffix)]
	}
	if !printableText(b) {
		return false // binary frame, no text form
	}
	c.Value = string(b)
	c.Hex = false
	return true
}

// openSettings loads the latest prefs into a draft and opens the F10 page.
// The draft is only committed on Enter, so Esc leaves the live session and
// the config file untouched.
func (m *serialMonitorModel) openSettings() {
	m.prefs = m.livePrefs()
	m.settingsOpen = true
	m.settingsSel = 0
	m.settingsMsg = ""
}

// livePrefs is the on-disk preference set with the session's own switches
// layered on top. F1/F2/F3 flip the view modes for the session without
// rewriting the file, so a panel seeded from disk alone would show stale
// values and — on Enter — silently put the old mode back, which reads as
// "switching the mode does not stick".
func (m *serialMonitorModel) livePrefs() serialconfig.SerialPrefs {
	p, err := serialconfig.LoadSerialPrefs()
	if err != nil {
		p = m.prefs
	}
	p.HexView = m.hexView
	p.HexAscii = m.hexAscii
	p.HexSend = m.hexSend
	p.ShowTime = m.showTime
	p.ShowPrefix = m.showPrefix
	p.ShowQuickLabel = m.showQuickLabel
	p.LineEnding = m.lineEnd
	p.IntervalMs = int(m.interval / time.Millisecond)
	p.MaxLines = m.maxLines
	p.PageKey = m.pageKey
	return p.Normalize()
}

func (m *serialMonitorModel) updateSettings(key tea.KeyMsg) (*serialMonitorModel, tea.Cmd) {
	k := key.String()
	switch k {
	case "esc":
		m.settingsOpen = false
		// Discard the draft, but keep the session's own switches: they are
		// the applied state, not something the panel was allowed to change
		// with arrow keys alone.
		m.prefs = m.livePrefs()
		m.input.Focus()
		return m, nil
	case "up":
		m.settingsSel = (m.settingsSel - 1 + settingsCount) % settingsCount
		return m, nil
	case "down":
		m.settingsSel = (m.settingsSel + 1) % settingsCount
		return m, nil
	case "enter":
		p := m.prefs.Normalize()
		if err := serialconfig.SaveSerialPrefs(p); err != nil {
			m.settingsMsg = fmt.Sprintf(i18n.T("serial.settings.save_error"), err)
			return m, nil
		}
		// Apply immediately to the live session.
		m.prefs = p
		m.hexView = p.HexView
		m.hexAscii = p.HexAscii
		m.hexSend = p.HexSend
		m.syncSendPrompt()
		m.showTime = p.ShowTime
		m.showPrefix = p.ShowPrefix
		m.showQuickLabel = p.ShowQuickLabel
		m.setLineEnd(p.LineEnding)
		// A saved send-mode change has to reach the quick slots too, just as
		// F2 does, or the bar contradicts the newly applied TX mode.
		m.retargetQuickCmds(m.hexSend)
		m.interval = p.Interval()
		m.maxLines = p.MaxLines
		m.pageKey = p.PageKey
		// Resolve "auto" against this machine so the saved setting takes
		// effect immediately instead of only after a restart.
		m.quickKey = serialconfig.ResolveQuickKey(p.QuickKey, runtime.GOOS)
		// Display-mode changes apply to the content already on screen.
		if len(m.rxEntries) > m.maxLines {
			m.rxEntries = m.rxEntries[len(m.rxEntries)-m.maxLines:]
		}
		m.rebuildRx()
		m.settingsOpen = false
		m.settingsMsg = ""
		m.status = i18n.T("serial.settings.saved")
		m.input.Focus()
		return m, nil
	case "left", "right":
		delta := -1
		if k == "right" {
			delta = 1
		}
		m.adjustSetting(m.settingsSel, delta)
		return m, nil
	}
	return m, nil
}

// adjustSetting edits one row of the draft. Numeric rows step by a fixed
// amount and clamp; the change is not written to disk until Enter.
func (m *serialMonitorModel) adjustSetting(sel, delta int) {
	switch sel {
	case settingHexView:
		m.prefs.HexView = !m.prefs.HexView
	case settingHexSend:
		m.prefs.HexSend = !m.prefs.HexSend
	case settingShowTime:
		m.prefs.ShowTime = !m.prefs.ShowTime
	case settingLineEnd:
		for i, v := range lineEndChoices {
			if v == m.prefs.LineEnding {
				m.setLineEnd(lineEndChoices[(i+delta+len(lineEndChoices))%len(lineEndChoices)])
				break
			}
		}
	case settingHexAscii:
		m.prefs.HexAscii = !m.prefs.HexAscii
	case settingLogAppend:
		m.prefs.LogAppend = !m.prefs.LogAppend
	case settingLogDir:
		if strings.TrimSpace(m.prefs.LogDir) == "" {
			m.prefs.LogDir = "logs"
		} else {
			m.prefs.LogDir = ""
		}
	case settingPageKey:
		if m.prefs.PageKey == "alt" {
			m.prefs.PageKey = "ctrl"
		} else {
			m.prefs.PageKey = "alt"
		}
	case settingQuickKey:
		// auto -> alt -> option -> auto; auto resolves per OS.
		switch m.prefs.QuickKey {
		case serialconfig.QuickKeyAuto:
			m.prefs.QuickKey = serialconfig.QuickKeyAlt
		case serialconfig.QuickKeyAlt:
			m.prefs.QuickKey = serialconfig.QuickKeyOption
		default:
			m.prefs.QuickKey = serialconfig.QuickKeyAuto
		}
	case settingShowPrefix:
		m.prefs.ShowPrefix = !m.prefs.ShowPrefix
	case settingShowQuickLabel:
		m.prefs.ShowQuickLabel = !m.prefs.ShowQuickLabel
	case settingInterval:
		m.prefs.IntervalMs += delta * 100
	case settingMaxLines:
		m.prefs.MaxLines += delta * 100
	}
	m.prefs = m.prefs.Normalize()
}

// toggleRxLog starts or stops a raw-byte receive log. The displayed pane is
// folded for GBK ConHost, so it must never be used as capture evidence.
func (m *serialMonitorModel) toggleRxLog() {
	if m.logRecording {
		path := m.logPath
		if err := m.flushRxLog(); err != nil {
			m.status = fmt.Sprintf(i18n.T("serial.monitor.log_error"), err)
			return
		}
		m.logRecording = false
		m.status = fmt.Sprintf(i18n.T("serial.monitor.log_stopped"), path)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		m.status = fmt.Sprintf(i18n.T("serial.monitor.log_error"), err)
		return
	}
	dir := m.prefs.LogDir
	if dir == "" {
		dir = home
	} else if err := os.MkdirAll(dir, 0755); err != nil {
		m.status = fmt.Sprintf(i18n.T("serial.monitor.log_error"), err)
		return
	}
	name := fmt.Sprintf("ctty-serial-%s.log", time.Now().Format("20060102-150405"))
	m.logPath = filepath.Join(dir, name)
	m.logRaw = nil
	m.logRecording = true
	m.status = fmt.Sprintf(i18n.T("serial.monitor.log_started"), m.logPath)
}

func (m *serialMonitorModel) flushRxLog() error {
	if m.logPath == "" {
		return nil
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if m.prefs.LogAppend {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(m.logPath, flags, 0644)
	if err != nil {
		return err
	}
	_, err = f.Write(m.logRaw)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	m.logRaw = nil
	return closeErr
}

func (m *serialMonitorModel) jumpSearch(delta int) {
	if len(m.searchMatches) == 0 {
		m.status = i18n.T("serial.monitor.search_none")
		return
	}
	m.searchMatch = (m.searchMatch + delta + len(m.searchMatches)) % len(m.searchMatches)
	idx := m.searchMatches[m.searchMatch]
	offset := idx - m.rx.Height/2
	if offset < 0 {
		offset = 0
	}
	m.rx.SetYOffset(offset)
	m.rxFollow = m.rx.AtBottom()
	if m.rxFollow {
		m.unseen = 0
	}
	m.status = fmt.Sprintf(i18n.T("serial.monitor.search_at"), m.searchMatch+1, len(m.searchMatches))
}

// updateSearch handles the Ctrl+F keyword-highlight input line. Typing filters
// as you go; Enter commits the highlight and Esc clears it.
func (m *serialMonitorModel) updateSearch(key tea.KeyMsg) (*serialMonitorModel, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.searchOn = false
		m.searchQuery = ""
		m.searchInput.SetValue("")
		m.searchMatches = nil
		m.searchMatch = 0
		m.input.Focus()
		m.refreshRxContent()
		return m, nil
	case "enter":
		m.searchOn = false
		m.searchQuery = m.searchInput.Value()
		m.searchMatch = 0
		m.input.Focus()
		m.refreshRxContent()
		m.jumpSearch(0)
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(key)
	m.searchQuery = m.searchInput.Value()
	m.refreshRxContent()
	return m, cmd
}

// View renders an SSCOM-style workspace: parameter/status header, scrolling
// receive pane, send box with live byte count, quick-command bar and a
// two-line help strip. Every row is width-budgeted before RenderCanvas
// applies the GBK glyph fallback.
func (m *serialMonitorModel) View() string {
	if m.settingsOpen {
		return m.settingsView()
	}
	if m.quickEditing {
		return m.editorView()
	}
	inner := m.innerWidth
	if inner < 20 {
		inner = 20
	}
	parity := m.dev.Parity
	if parity == "" {
		parity = "none"
	}
	titleRow := m.monitorTitleRow(inner, parity)

	followWord := i18n.T("serial.monitor.live")
	if !m.rxFollow {
		followWord = i18n.T("serial.monitor.paused")
		if m.unseen > 0 {
			followWord = fmt.Sprintf("%s +%d", followWord, m.unseen)
		}
	}
	focusWord := i18n.T("serial.monitor.focus_input")
	if m.quickFocus {
		focusWord = i18n.T("serial.monitor.focus_quick")
	}
	// Mode tokens lead the row on purpose: at 80 columns the tail fields are
	// dropped when one-shot feedback needs room, and "which mode am I in" is
	// the one thing that must survive that cut.
	status := fmt.Sprintf("[%s] %s  RX:%s TX:%s  RX %d (%d/s)  TX %d  TIME:%s EOL:%s  LOOP:%s %s  Q%d/%d",
		followWord, focusWord,
		m.rxModeLabel(),
		modeWord(m.hexSend, "HEX", "ASCII"),
		m.rxCount, m.rxRate, m.txCount,
		onOff(m.showTime), lineEndLabel(m.lineEnd),
		onOff(m.repeat), m.interval.String(),
		m.quickPage+1, m.quickPageCount())
	statusRow := m.styles.HelpText.Align(lipgloss.Left).Width(inner).
		Render(fitStatusRow(status, m.status, inner))
	if m.searchOn {
		statusRow = m.styles.HelpText.Align(lipgloss.Left).Width(inner).
			Render(fitMonitorRow("  "+i18n.T("serial.monitor.search_prompt")+" "+m.searchInput.View(), inner))
	}

	// The receive pane is always Secondary; the send box takes the Primary
	// (focused) border only while the input owns focus. Tab moves focus to the
	// quick bar, which then becomes the highlighted surface instead.
	rxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.Theme.Secondary).
		Width(inner)
	txBorderFg := m.styles.Theme.Primary
	if m.quickFocus {
		txBorderFg = m.styles.Theme.Secondary
	}
	txStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(txBorderFg).
		Width(inner)

	// A failed open fills the receive pane with the reason instead of
	// leaving an empty box with a hint buried in the status line.
	rxBody := m.rx.View()
	switch {
	case m.port == nil && m.status != "":
		rxBody = renderOpenFailure(m.status, m.rx.Width, m.rx.Height)
	case m.port != nil && len(m.rxEntries) == 0 && len(m.pendingBytes) == 0:
		hint := fmt.Sprintf(i18n.T("serial.monitor.empty"), m.dev.BaudRate, m.rxModeLabel())
		rxBody = renderOpenFailure(hint, m.rx.Width, m.rx.Height)
	}

	txContent := m.sendBoxContent(inner - 2)
	quickRows := m.quickBarRows(inner)

	helpRows := []string{
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).
			Render(fitMonitorRow("  "+fmt.Sprintf(i18n.T("serial.monitor.help_short"), m.quickKeyToken(), m.pageKeyToken()), inner)),
	}
	if m.helpExpanded {
		helpRows = []string{
			m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow("  "+i18n.T("serial.monitor.help1"), inner)),
			m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow("  "+fmt.Sprintf(i18n.T("serial.monitor.help2"), m.pageKeyToken()), inner)),
			m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow("  "+fmt.Sprintf(i18n.T("serial.monitor.help3"), m.quickKeyToken()), inner)),
		}
	}
	contentParts := []string{
		titleRow, statusRow, "",
		rxStyle.Render(rxBody), "",
		txStyle.Render(txContent), "",
		quickRows[0], quickRows[1], "",
	}
	contentParts = append(contentParts, helpRows...)
	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)
	box := alignJetBrainsBox(m.styles.FormContainer.Width(m.boxWidth).Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}

// sendBoxContent builds the send box: the input plus a live byte-count
// preview derived from exactly the payload Enter would send.
func (m *serialMonitorModel) sendBoxContent(width int) string {
	// bubbles textinput slices its placeholder by display width, which is
	// wrong for CJK (2 cells/rune) and leaves uninitialized NULs in the view.
	// Strip them at paint time; the real value and payload are untouched.
	input := strings.ReplaceAll(m.input.View(), "\x00", "")

	// Right-hand hints: history position (while browsing) and the live
	// byte-count / BAD-HEX preview of exactly what Enter would send. When
	// the line is a dropped file path, show the file-send hint instead.
	preview := ""
	badHex := false
	if v := m.input.Value(); v != "" {
		if p, ok := m.detectDroppedFile(v); ok && !m.hexSend {
			var n int64
			if fi, err := os.Stat(p); err == nil {
				n = fi.Size()
			}
			preview = fmt.Sprintf(i18n.T("serial.monitor.file_hint"), n)
		} else if b, err := m.payloadBytes(v, m.hexSend); err != nil {
			preview = i18n.T("serial.monitor.badhex")
			badHex = true
		} else {
			// Preview the real wire size: HEX sends carry no suffix.
			preview = fmt.Sprintf(i18n.T("serial.monitor.bytes"), m.wireLen(len(b), !m.hexSend))
		}
	}
	hint := ""
	if len(m.history) > 0 && m.histIdx < len(m.history) {
		hint = fmt.Sprintf(i18n.T("serial.monitor.hist"), m.histIdx+1, len(m.history))
	}
	// Compose the two hints into one right-aligned suffix. BAD HEX is tinted
	// with the error style; other hints stay plain. Keep ANSI width math on
	// the styled string so padding stays exact on every console.
	suffix := ""
	if hint != "" && preview != "" {
		suffix = hint + "  " + preview
	} else if preview != "" {
		suffix = preview
	} else {
		suffix = hint
	}
	renderedSuffix := suffix
	if badHex && preview != "" {
		// Tint only the BAD HEX token, preserving any preceding HIST hint.
		if hint != "" {
			renderedSuffix = hint + "  " + m.styles.ErrorText.Render(preview)
		} else {
			renderedSuffix = m.styles.ErrorText.Render(preview)
		}
	}
	// The suffix may carry i18n text with DBCS-ambiguous glyphs (e.g. the
	// middle dot in the file-send hint). On GBK ConHost those paint two
	// cells while StringWidth budgets one, overflowing the send row — fold
	// them to deterministic ASCII before any width math, exactly like the
	// input text on the next line.
	renderedSuffix = legacyGlyphFallback(renderedSuffix)
	room := width - ansi.StringWidth(renderedSuffix)
	if room < 4 {
		return ansi.Truncate(legacyGlyphFallback(input), width, "")
	}
	// Keep one blank cell between the text and the hint: when the field is
	// exactly full the truncation used to glue "018 bytes" together.
	limit := room
	if renderedSuffix != "" && limit > 4 {
		limit--
	}
	input = ansi.Truncate(legacyGlyphFallback(input), limit, "")
	pad := room - ansi.StringWidth(input)
	if pad > 0 {
		input += strings.Repeat(" ", pad)
	}
	return input + renderedSuffix
}

// quickBarRows renders the current page's 8 slots as two rows of four. Slot
// numbers are page-local (1..8) so they match the Alt+1..8 keys. Slots past
// the end of the command list render as blank cells instead of fake numbers.
func (m *serialMonitorModel) quickBarRows(inner int) [2]string {
	rows := [2]string{"", ""}
	cellW := inner / quickCmdCols
	if cellW < 8 {
		cellW = 8
	}
	base := m.quickPage * quickCmdCount
	for row := 0; row < 2; row++ {
		var sb strings.Builder
		for col := 0; col < quickCmdCols; col++ {
			idx := base + row*quickCmdCols + col
			sb.WriteString(m.quickSlot(idx, cellW))
		}
		rows[row] = ansi.Truncate(legacyGlyphFallback(sb.String()), inner, "")
	}
	return rows
}

func (m *serialMonitorModel) quickSlot(idx, cellW int) string {
	// Empty slots stay blank: no fake number, no fake label.
	if idx < 0 || idx >= len(m.quickCmds) {
		return strings.Repeat(" ", cellW)
	}
	// Page-local 1-based number: Alt+n fires slot n on the current page, so
	// the badge must read [1]..[8] on every page, never global [9]..[16].
	num := idx%quickCmdCount + 1
	label := m.quickCmds[idx].Label
	// Slots follow the global send mode now, so marking every HEX slot would
	// paint the whole bar with "*" in HEX mode. Only flag the exception: a
	// slot that disagrees with the active mode (a binary frame that had to
	// stay HEX while the mode is ASCII), which is the one that needs a glance.
	hexTag := ""
	if m.quickCmds[idx].Hex != m.hexSend {
		hexTag = "*"
	}
	text := "[" + fmt.Sprintf("%d", num) + "] " + label + hexTag
	if m.quickFocus && idx == m.quickSel {
		// Selected slot uses the project's selection style (inverse video),
		// so Alt+n direct-fires stay fast but the highlighted slot is obvious.
		text = m.styles.Selected.Render(" " + text + " ")
	} else {
		text = " " + text + " "
	}
	w := ansi.StringWidth(text)
	if w < cellW {
		text += strings.Repeat(" ", cellW-w)
	}
	return ansi.Truncate(legacyGlyphFallback(text), cellW, "")
}

// editorView renders the F9 quick-command editor as a modal-style panel.
func (m *serialMonitorModel) editorView() string {
	inner := m.innerWidth
	if inner < 20 {
		inner = 20
	}
	title := m.styles.Header.Align(lipgloss.Left).Width(inner).
		Render(fitMonitorRow(i18n.T("serial.quick.title"), inner))

	// Column budget. The row reads left to right as "which slot, what it is
	// called, what it sends, how it is encoded" — the two halves of a command
	// never sit on top of each other, so "AT" next to "AT" cannot look like a
	// rendering bug.
	const (
		numW  = 2 // slot number, right aligned
		modeW = 7 // "[ASCII]"
		byteW = 4 // "10B"
	)
	avail := inner - 2 // the two-column indent every content row shares
	if avail < 24 {
		avail = 24
	}
	// The name column takes a third of the row, capped so wide terminals do
	// not waste it and narrow ones still leave room for the byte count.
	nameW := avail / 3
	if nameW > 18 {
		nameW = 18
	}
	if nameW < 8 {
		nameW = 8
	}
	rightW := modeW + 1 + byteW
	previewW := avail - numW - 2 - nameW - 1 - rightW - 1
	if previewW < 0 {
		previewW = 0
	}

	var sb strings.Builder
	vis := m.editorListRows()
	end := m.quickEditTop + vis
	if end > len(m.quickCmds) {
		end = len(m.quickCmds)
	}
	for i := m.quickEditTop; i < end; i++ {
		c := m.quickCmds[i]
		sel := i == m.quickEditSel
		mode := "ASCII"
		if c.Hex {
			mode = "HEX"
		}
		modeTag := "[" + mode + "]"
		byteTag := fmt.Sprintf("%dB", c.ByteCount())
		if c.ByteCount() < 0 {
			byteTag = "--"
		}
		marker := "  "
		if sel {
			marker = "> "
		}
		name := ansi.Truncate(legacyGlyphFallback(c.Label), nameW, "")
		nameEmpty := strings.TrimSpace(c.Label) == ""
		if nameEmpty {
			// A freshly added slot is blank by design; "(empty)" tells the
			// user the row exists and what to do, instead of an empty gap
			// that looks like a rendering failure.
			name = i18n.T("serial.quick.unnamed")
			name = ansi.Truncate(name, nameW, "")
		}
		// Only show the payload when it says something the name does not: the
		// stock slots are label==value, and echoing it twice is what made the
		// list look like typo soup. Too narrow for a useful glimpse? Skip it
		// rather than print one stray hex digit.
		preview := ""
		if previewW >= 6 && c.Value != "" && c.Value != c.Label {
			preview = ansi.Truncate(legacyGlyphFallback(c.Value), previewW, "..")
		}
		num := fmt.Sprintf("%d", i+1)
		plain := marker + strings.Repeat(" ", numW-ansi.StringWidth(num)) + num + "  " +
			name + strings.Repeat(" ", nameW-ansi.StringWidth(name)) + " " +
			preview + strings.Repeat(" ", previewW-ansi.StringWidth(preview)) + " "
		modeCell := strings.Repeat(" ", modeW-ansi.StringWidth(modeTag)) + modeTag
		byteCell := strings.Repeat(" ", byteW-ansi.StringWidth(byteTag)) + byteTag
		var row string
		if sel {
			// One solid bar for the selected slot: the "> " marker alone gets
			// lost once the list is longer than a glance.
			row = m.styles.Selected.Render(plain + modeCell + " " + byteCell)
		} else {
			// ASCII is the default, so only HEX shouts. Without this every
			// row shouted and the one that mattered was just more noise.
			modeStyle := m.styles.HelpText
			if c.Hex {
				modeStyle = m.styles.FormField.Bold(true)
			}
			row = plain + modeStyle.Render(modeCell) + " " + byteCell
		}
		sb.WriteString(row + "\n")
	}
	if len(m.quickCmds) == 0 {
		sb.WriteString("  " + i18n.T("serial.quick.empty") + "\n")
	}
	list := sb.String()
	// Scroll indicator when the list is longer than the visible window.
	if total := len(m.quickCmds); total > vis {
		indicator := fmt.Sprintf(i18n.T("serial.quick.window"), m.quickEditTop+1, end, total)
		list += "  " + indicator + "\n"
	}

	// The two halves of the selected command, always on screen while editing.
	// Each row is "label | value" with a fixed label gutter, so the caret can
	// move between them without either line jumping sideways.
	labelRow, valueRow := "", ""
	if m.quickEditSel >= 0 && m.quickEditSel < len(m.quickCmds) {
		c := m.quickCmds[m.quickEditSel]
		labelName := i18n.T("serial.quick.field_label")
		valueName := i18n.T("serial.quick.field_value")
		gutterW := ansi.StringWidth(labelName)
		if w := ansi.StringWidth(valueName); w > gutterW {
			gutterW = w
		}
		// "  " + name + " | " leaves the value at one shared column.
		headW := 2 + gutterW + 3

		mode := "ASCII"
		if c.Hex {
			mode = "HEX"
		}
		draft := serialconfig.QuickCmd{Value: m.quickCmds[m.quickEditSel].Value, Hex: c.Hex}
		hint := fmt.Sprintf("[%s] %dB", mode, draft.ByteCount())
		if draft.ByteCount() < 0 {
			hint = "[" + mode + "] " + i18n.T("serial.monitor.badhex")
		}
		// Value width is shared by the live input and the byte count, so the
		// caret never runs under the hint.
		valW := avail - headW - ansi.StringWidth(hint) - 2
		if valW < 6 {
			valW = 6
		}
		m.quickInput.Width = valW
		m.quickLabelIn.Width = valW

		labelBody, valueBody := c.Label, c.Value
		if m.quickEditOn {
			labelBody = strings.ReplaceAll(m.quickLabelIn.View(), "\x00", "")
			valueBody = strings.ReplaceAll(m.quickInput.View(), "\x00", "")
		}
		labelRow = m.editorFieldRow(labelName, labelBody, gutterW, headW,
			avail, "", m.quickEditOn && m.quickEditLabel)
		valueRow = m.editorFieldRow(valueName, valueBody, gutterW, headW,
			avail, hint, m.quickEditOn && !m.quickEditLabel)
	}

	// A rule separates "the list of slots" from "the one being edited", so the
	// two field rows read as a form rather than as more list rows.
	rule := m.styles.HelpText.Render(strings.Repeat("\u2500", inner))

	// The key hints follow the mode: while typing, Enter commits instead of
	// opening the value field, and saying so is the difference between "I am
	// editing" and "my input was thrown away".
	help1, help2 := "serial.quick.help1", "serial.quick.help2"
	if m.quickEditOn {
		help1, help2 = "serial.quick.edit_help1", "serial.quick.edit_help2"
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, "",
		m.styles.HelpText.Render(strings.TrimSuffix(list, "\n")),
		rule,
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow(labelRow, inner)),
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow(valueRow, inner)),
		"",
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow("  "+i18n.T(help1), inner)),
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow("  "+i18n.T(help2), inner)),
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow("  "+m.quickMsg, inner)),
	)
	box := alignJetBrainsBox(m.styles.FormContainer.Width(m.boxWidth).Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}

// editorFieldRow renders one half of the slot being edited: a fixed-width
// name in the gutter, a rule, then the body. Focusing a field tints its name
// and its text, so the caret is never the only sign of where typing lands —
// the "> " marker is reserved for the selected slot in the list above.
func (m *serialMonitorModel) editorFieldRow(name, text string, gutterW, headW, avail int, hint string, focused bool) string {
	nameStyle := m.styles.Label
	textStyle := lipgloss.NewStyle()
	if focused {
		// A filled label, not just a tint: the form has two rows and the
		// caret alone is too subtle to say which one is taking keys.
		nameStyle = m.styles.Selected
		textStyle = m.styles.FormField
	}
	gutter := "  " + nameStyle.Render(name) +
		strings.Repeat(" ", gutterW-ansi.StringWidth(name)) + " \u2502 "
	bodyW := avail - headW
	if bodyW < 4 {
		bodyW = 4
	}
	body := ansi.Truncate(legacyGlyphFallback(text), bodyW, "")
	row := gutter + textStyle.Render(body)
	pad := avail - ansi.StringWidth(row)
	if pad < 0 {
		pad = 0
	}
	row += strings.Repeat(" ", pad)
	if hint == "" {
		return row
	}
	used := avail - ansi.StringWidth(hint) - 1
	vis := ansi.StringWidth(row)
	if vis > used {
		// A long value would otherwise push the byte count into the border;
		// trim the value instead, the count is what tells the user it happened.
		row = ansi.Truncate(row, used, "")
		vis = ansi.StringWidth(row)
	}
	return row + strings.Repeat(" ", used-vis+1) + m.styles.HelpText.Render(hint)
}

func (m *serialMonitorModel) settingsView() string {
	inner := m.innerWidth
	if inner < 20 {
		inner = 20
	}
	title := m.styles.Header.Align(lipgloss.Left).Width(inner).
		Render(fitMonitorRow(i18n.T("serial.settings.title"), inner))

	rows := m.settingsRows()
	var sb strings.Builder
	for i, row := range rows {
		prefix := "  "
		if i == m.settingsSel {
			prefix = "> "
		}
		line := prefix + row.label
		pad := inner - ansi.StringWidth(line) - ansi.StringWidth(row.value)
		if pad < 1 {
			pad = 1
		}
		sb.WriteString(line + strings.Repeat(" ", pad) + row.value + "\n")
	}
	list := sb.String()

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, "",
		m.styles.HelpText.Render(list), "",
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow("  "+i18n.T("serial.settings.help1"), inner)),
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow("  "+i18n.T("serial.settings.help2"), inner)),
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(fitMonitorRow("  "+m.settingsMsg, inner)),
	)
	box := alignJetBrainsBox(m.styles.FormContainer.Width(m.boxWidth).Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}

type settingsRow struct{ label, value string }

func (m *serialMonitorModel) settingsRows() []settingsRow {
	p := m.prefs
	return []settingsRow{
		{i18n.T("serial.settings.hex_view"), onOff(p.HexView)},
		{i18n.T("serial.settings.hex_send"), onOff(p.HexSend)},
		{i18n.T("serial.settings.show_time"), onOff(p.ShowTime)},
		{i18n.T("serial.settings.line_end"), lineEndLabel(p.LineEnding)},
		{i18n.T("serial.settings.hex_ascii"), onOff(p.HexAscii)},
		{i18n.T("serial.settings.log_append"), onOff(p.LogAppend)},
		{i18n.T("serial.settings.log_dir"), logDirLabel(p.LogDir)},
		{i18n.T("serial.settings.page_key"), pageKeyLabel(p.PageKey, p.QuickKey)},
		{i18n.T("serial.settings.quick_key"), quickKeyLabel(p.QuickKey)},
		{i18n.T("serial.settings.show_prefix"), onOff(p.ShowPrefix)},
		{i18n.T("serial.settings.show_quick_label"), onOff(p.ShowQuickLabel)},
		{i18n.T("serial.settings.interval"), fmt.Sprintf("%d ms", p.IntervalMs)},
		{i18n.T("serial.settings.max_lines"), fmt.Sprintf("%d", p.MaxLines)},
	}
}

func logDirLabel(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "home"
	}
	return dir
}

func (m *serialMonitorModel) rxModeLabel() string {
	if m.hexAscii {
		return "HEX+ASCII"
	}
	return modeWord(m.hexView, "HEX", "ASCII")
}

func modeWord(hex bool, hexWord, plainWord string) string {
	if hex {
		return hexWord
	}
	return plainWord
}

// rxPrefix / txPrefix are the single source of truth for the direction
// marker. With the F10 "Show TX/RX prefix" switch off they return an empty
// string, so both the renderer and the line-budget math reserve zero columns
// instead of silently disagreeing.
func (m *serialMonitorModel) rxPrefix() string {
	if !m.showPrefix {
		return ""
	}
	return rxMark
}

func (m *serialMonitorModel) txPrefix() string {
	if !m.showPrefix {
		return ""
	}
	return txMark
}

func renderOpenFailure(msg string, w, h int) string {
	if w < 8 {
		w = 8
	}
	lines := strings.Split(fitMonitorRow(msg, w), "\n")
	out := make([]string, 0, h)
	out = append(out, lines...)
	for len(out) < h {
		out = append(out, "")
	}
	if len(out) > h {
		out = out[:h]
	}
	return strings.Join(out, "\n")
}

// fitMonitorRow folds ambiguous glyphs before measuring, so a long help or
// status line can never push the right edge of the assistant frame outward.
func fitMonitorRow(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(legacyGlyphFallback(s), width, "")
}

// monitorTitleRow keeps the ctty mark visible on the serial page without
// spending another terminal row: the product title and device summary stay
// left-aligned, while a theme-highlighted ASCII [ctty] badge is right-aligned.
func (m *serialMonitorModel) monitorTitleRow(width int, parity string) string {
	if parity == "" {
		parity = "none"
	}
	left := fmt.Sprintf("%s  %s  %s @ %d 8%s1",
		i18n.T("serial.monitor.title"), m.dev.Name, m.dev.Device, m.dev.BaudRate, strings.ToUpper(parity[:1]))
	// Reuse the theme's selected-chip colors so the brand reads as an accent
	// instead of blending into the header. ASCII only: no ConHost DBCS drift.
	brand := m.styles.Selected.Bold(true).Padding(0, 1).Render(i18n.T("serial.monitor.brand"))
	brandW := ansi.StringWidth(brand)
	if width <= brandW {
		return m.styles.Header.Align(lipgloss.Left).Width(width).Render(fitMonitorRow(brand, width))
	}
	leftW := width - brandW
	left = fitMonitorRow(left, leftW)
	left += strings.Repeat(" ", leftW-ansi.StringWidth(left))
	return m.styles.Header.Align(lipgloss.Left).Width(width).Render(left + brand)
}

// fitStatusRow renders the status line as two panes: the live counters/modes
// on the left and the most recent one-shot feedback (sent bytes, save result,
// errors) on the right. Each pane is folded for the legacy console and
// truncated independently, so a long status can never push the right border
// out or hide the counters.
func fitStatusRow(state, feedback string, width int) string {
	state = legacyGlyphFallback(state)
	feedback = strings.TrimSpace(legacyGlyphFallback(feedback))
	if width <= 0 {
		return ""
	}
	stateW := ansi.StringWidth(state)
	if feedback == "" {
		return ansi.Truncate(state, width, "")
	}
	fbW := ansi.StringWidth(feedback)
	if fbW >= width {
		// Feedback alone dominates: show as much of it as fits.
		return ansi.Truncate(feedback, width, "")
	}
	const gap = 2
	budget := width - fbW - gap
	// Drop whole tail fields (split on the double-space separators) until
	// the left pane fits, so a label is never cut in half ("TIME:" etc).
	fields := strings.Split(state, "  ")
	for len(fields) > 1 && ansi.StringWidth(strings.Join(fields, "  ")) > budget {
		fields = fields[:len(fields)-1]
	}
	state = strings.Join(fields, "  ")
	if ansi.StringWidth(state) > budget {
		state = ansi.Truncate(state, budget, "")
	}
	stateW = ansi.StringWidth(state)
	return state + strings.Repeat(" ", width-stateW-fbW) + feedback
}

func onOff(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

func (m *serialMonitorModel) stepInterval(delta time.Duration) {
	m.interval += delta
	if m.interval < 100*time.Millisecond {
		m.interval = 100 * time.Millisecond
	}
	if m.interval > 10*time.Second {
		m.interval = 10 * time.Second
	}
}

func (m *serialMonitorModel) historyPrev() {
	if len(m.history) == 0 || m.histIdx <= 0 {
		return
	}
	m.histIdx--
	m.input.SetValue(m.history[m.histIdx])
}

func (m *serialMonitorModel) historyNext() {
	if len(m.history) == 0 {
		return
	}
	if m.histIdx >= len(m.history) {
		return
	}
	m.histIdx++
	if m.histIdx == len(m.history) {
		m.input.SetValue("")
		return
	}
	m.input.SetValue(m.history[m.histIdx])
}

func (m *serialMonitorModel) payload() ([]byte, error) {
	return m.payloadBytes(m.input.Value(), m.hexSend)
}

// payloadBytes decodes raw text into the bytes Enter would send. Shared by
// the send path and the live byte-count preview so they can never disagree.
func (m *serialMonitorModel) payloadBytes(raw string, hexMode bool) ([]byte, error) {
	if !hexMode {
		return []byte(raw), nil
	}
	b, err := hex.DecodeString(normalizeHexInput(raw))
	if err != nil {
		// The raw encoding/hex text ("invalid byte: U+0074 't'") tells a
		// serial user nothing; say what the mode expects instead.
		return nil, errors.New(i18n.T("serial.monitor.badhex_hint"))
	}
	return b, nil
}

// normalizeHexInput accepts the spellings people actually paste instead of
// rejecting them: "01 03", "01,03,00", "0x01 0x03", "\x01\x03", "01-03",
// "01:03", tabs and line breaks.
func normalizeHexInput(raw string) string {
	s := strings.ToLower(raw)
	return strings.NewReplacer(
		"0x", "", "\\x", "",
		" ", "", "\t", "", "\r", "", "\n", "", "\v", "", "\f", "",
		",", "", "-", "", ":", "", "_", "",
	).Replace(s)
}

// retargetInput re-expresses whatever is half-typed in the send box in the
// mode that just became active. Without it, switching F2 left "41 42" in the
// box and Enter then sent the six literal characters instead of two bytes.
// It reports whether the text had to be dropped because it has no printable
// form in the new mode.
func (m *serialMonitorModel) retargetInput() (cleared bool) {
	v := m.input.Value()
	if strings.TrimSpace(v) == "" {
		m.input.SetValue("")
		return false
	}
	if m.hexSend {
		// Spell out the bytes Enter would really put on the wire, including
		// the line ending ASCII send appends. Otherwise F2 silently dropped
		// the CRLF: "AT" went out as 04 54 0D 0A, then as 41 54.
		m.input.SetValue(hexText(m.withLineEnding([]byte(v))))
		return false
	}
	b, err := m.payloadBytes(v, true)
	if err != nil {
		// Not hex at all — it was typed as text, so it already belongs in the
		// mode we just switched to.
		return false
	}
	// ASCII send re-appends the configured suffix, so drop the one the hex
	// already carries to keep the round trip byte-identical.
	if suffix := m.lineEndSuffix(); len(suffix) > 0 && bytes.HasSuffix(b, suffix) {
		b = b[:len(b)-len(suffix)]
	}
	if !printableText(b) {
		// Keeping the hex spelling would be worse than dropping it: Enter in
		// ASCII mode puts the literal characters "01 03 ..." on the wire, and
		// the echo then looks exactly like a hex frame, so the switch appears
		// to have done nothing while the device receives the wrong bytes.
		m.input.SetValue("")
		return true
	}
	m.input.SetValue(string(b))
	return false
}

// retargetQuickCmds re-expresses every saved slot in the send mode that just
// became active, mirroring what retargetInput does for the send box. Switching
// F2 changes how a command is *written*, never the bytes it puts on the wire:
//
//   - To HEX: an ASCII slot's payload plus the configured line ending (what
//     ASCII send really emits) becomes its hex text, so firing it still sends
//     the same bytes.
//   - To ASCII: a hex slot is decoded, its trailing line ending stripped and
//     the remainder shown as text. A slot that has no text form (Modbus RTU,
//     any NUL/invalid UTF-8) or carries no line ending (which ASCII send would
//     silently add) is left as HEX, so its bytes stay exact.
//
// It returns the number of slots converted and the number deliberately kept
// as HEX, for the status-line feedback.
func (m *serialMonitorModel) retargetQuickCmds(hexSend bool) (converted, kept int) {
	for i := range m.quickCmds {
		c := &m.quickCmds[i]
		if c.Hex == hexSend {
			continue // already in the target mode
		}
		if m.retargetQuickCmd(c, hexSend) {
			converted++
		} else {
			kept++
		}
	}
	return converted, kept
}

func printableText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r == '\t' {
			continue
		}
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// writePayload pushes all bytes to the port, tolerating short writes.
// withCrlf appends the configured line ending to a payload. It is applied at
// the single write sink so manual sends, quick commands and repeat all honor
// it. HEX mode still sends real \r\n bytes (0D 0A) — the distinction is only
// in how the user typed the payload, not in what goes over the wire.
var lineEndChoices = []string{"crlf", "cr", "lf", "lfcr", "none"}

func lineEndLabel(v string) string {
	switch v {
	case "cr":
		return "CR"
	case "lf":
		return "LF"
	case "lfcr":
		return "LFCR"
	case "none":
		return "NONE"
	default:
		return "CRLF"
	}
}

func (m *serialMonitorModel) setLineEnd(v string) {
	m.lineEnd = v
	m.crlf = v == "crlf"
	m.prefs.Crlf = m.crlf
	m.prefs.LineEnding = v
}

func (m *serialMonitorModel) lineEndSuffix() []byte {
	switch m.lineEnd {
	case "none":
		return nil
	case "cr":
		return []byte{'\r'}
	case "lf":
		return []byte{'\n'}
	case "lfcr":
		return []byte{'\n', '\r'}
	default:
		return []byte{'\r', '\n'}
	}
}

func (m *serialMonitorModel) withLineEnding(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}
	suffix := m.lineEndSuffix()
	if len(suffix) == 0 {
		return payload
	}
	out := make([]byte, 0, len(payload)+len(suffix))
	out = append(out, payload...)
	return append(out, suffix...)
}

// sendSuffix is the trailing byte sequence a send of this kind gets. It is
// passed to the echo so the HEX view can show the real wire bytes while the
// ASCII view stays free of the Enter artifact.
func (m *serialMonitorModel) sendSuffix(addLineEnding bool) []byte {
	if !addLineEnding {
		return nil
	}
	return m.lineEndSuffix()
}

// wireLen is the number of bytes actually sent for a payload of n bytes,
// including the configured line-ending suffix when it is appended.
func (m *serialMonitorModel) wireLen(n int, addLineEnding bool) int {
	if n == 0 || !addLineEnding {
		return n
	}
	return n + len(m.lineEndSuffix())
}

// writePayload sends a payload, optionally appending the configured line
// ending. HEX sends pass false: the user spelled out the exact frame, and an
// invisible 0D 0A made a 4-byte Modbus frame go out as 6 bytes while the echo
// still showed 4.
func (m *serialMonitorModel) writePayload(payload []byte, addLineEnding bool) {
	if m.port == nil {
		return
	}
	wire := payload
	if addLineEnding {
		wire = m.withLineEnding(payload)
	}
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	written := 0
	for written < len(wire) {
		n, err := m.port.Write(wire[written:])
		if n > 0 {
			written += n
		}
		if err != nil || n == 0 {
			break
		}
	}
}

// appendTxLine echoes an outgoing payload into the receive pane with a TX
// direction mark so sent and received data are visually distinct. The raw
// bytes are stored, so the echo re-renders when the view mode changes —
// a dropped file shows as HEX+ASCII there just like received data.
func (m *serialMonitorModel) appendTxLine(label string, payload, suffix []byte) {
	const txEchoCap = 512 // cap the echoed raw copy; counters use the real size
	wire := payload
	if len(suffix) > 0 {
		wire = append(append([]byte(nil), payload...), suffix...)
	}
	full := len(wire)
	raw := wire
	if len(raw) > txEchoCap {
		raw = raw[:txEchoCap]
	}
	trim := 0
	if len(suffix) > 0 && len(raw) == full {
		// The suffix survived the cap, so the ASCII echo may hide it.
		trim = len(suffix)
	}
	e := rxEntry{
		raw:       append([]byte(nil), raw...),
		at:        time.Now(),
		label:     label,
		tx:        true,
		off:       m.rxOffset,
		total:     full,
		asciiTrim: trim,
	}
	m.addEntry(e)
}

// sendCurrent handles a manual Enter. It always sends whatever is in the
// input box, clears it and records history. While a loop is running it does
// NOT overwrite lastPayload, so pressing Enter can insert a one-off command
// without changing what the loop keeps sending.
func (m *serialMonitorModel) sendCurrent() {
	if m.closed || m.port == nil {
		m.status = i18n.T("serial.monitor.not_connected")
		return
	}
	// Drag-and-drop: terminals paste a file's path into the input. When the
	// line is an existing file path, Enter sends the raw bytes instead of
	// the text — the friendliest binary-send flow a console can offer.
	if path, ok := m.detectDroppedFile(m.input.Value()); ok && !m.hexSend {
		m.sendFile(path)
		return
	}
	payload, err := m.payload()
	if err != nil {
		m.status = err.Error()
		return
	}
	if len(payload) == 0 {
		return
	}
	// HEX send is a byte-exact mode: no invisible suffix.
	addLineEnding := !m.hexSend
	if !m.repeat {
		m.lastPayload = payload
		m.lastAddLineEnding = addLineEnding
	}
	raw := m.input.Value()
	if raw != "" && (len(m.history) == 0 || m.history[len(m.history)-1] != raw) {
		m.history = append(m.history, raw)
		if len(m.history) > 100 {
			m.history = m.history[1:]
		}
	}
	m.histIdx = len(m.history)
	m.input.SetValue("")
	m.writePayload(payload, addLineEnding)
	m.txCount += m.wireLen(len(payload), addLineEnding)
	m.appendTxLine("", payload, m.sendSuffix(addLineEnding))
	m.status = fmt.Sprintf(i18n.T("serial.monitor.tx_bytes"), m.wireLen(len(payload), addLineEnding))
}

// sendRepeat handles a loop tick. It sends only the remembered payload and
// never touches the input box, so a manual Enter during a loop stays independent.
func (m *serialMonitorModel) sendRepeat() {
	if m.closed || m.port == nil || len(m.lastPayload) == 0 {
		return
	}
	payload := m.lastPayload
	addLineEnding := m.lastAddLineEnding
	m.writePayload(payload, addLineEnding)
	m.txCount += m.wireLen(len(payload), addLineEnding)
	m.appendTxLine("", payload, m.sendSuffix(addLineEnding))
	m.status = fmt.Sprintf(i18n.T("serial.monitor.tx_bytes"), m.wireLen(len(payload), addLineEnding))
}

// detectDroppedFile recognises a pasted file path in the input. Dropping a
// file onto a Windows terminal pastes its path (quoted when it contains
// spaces), so this treats such a line as "send this file" on Enter. To keep
// ordinary device text safe from false positives the candidate must carry a
// path separator (or quotes) and must exist on disk.
func (m *serialMonitorModel) detectDroppedFile(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	if s == "" || !strings.ContainsAny(s, `\/`) {
		return "", false
	}
	fi, err := os.Stat(s)
	if err != nil || fi.IsDir() {
		return "", false
	}
	return s, true
}

// sendFile transmits a file as raw bytes: no line-ending suffix, no hex
// framing, exact byte count into the TX counter.
func (m *serialMonitorModel) sendFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		m.status = fmt.Sprintf(i18n.T("serial.monitor.file_error"), err)
		return
	}
	if len(data) == 0 {
		m.status = i18n.T("serial.monitor.file_empty")
		return
	}
	m.writeRaw(data)
	m.txCount += len(data)
	m.appendTxLine("FILE", data, nil)
	m.status = fmt.Sprintf(i18n.T("serial.monitor.file_sent"), len(data))
	m.input.SetValue("")
}

func (m *serialMonitorModel) writeRaw(payload []byte) {
	if m.port == nil {
		return
	}
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	written := 0
	for written < len(payload) {
		n, err := m.port.Write(payload[written:])
		if n > 0 {
			written += n
		}
		if err != nil || n == 0 {
			break
		}
	}
}

func (m *serialMonitorModel) lineBudget() int {
	budget := m.rx.Width
	if m.showTime {
		budget -= len("[15:04:05.000] ")
	}
	budget -= len(m.rxPrefix())
	if budget < 8 {
		budget = 8
	}
	return budget
}

func (m *serialMonitorModel) appendRx(data []byte) {
	linesBefore := len(m.rxLines)
	budget := m.lineBudget()
	// Rolling per-second rate for the status line. Recompute when the window
	// elapses; no timers, just sampled as data arrives.
	now := time.Now()
	if now.Sub(m.rxRateSince) >= time.Second {
		secs := now.Sub(m.rxRateSince).Seconds()
		m.rxRate = int(float64(m.rxRateBytes) / secs)
		m.rxRateBytes = 0
		m.rxRateSince = now
	}
	m.rxRateBytes += len(data)
	if m.logRecording {
		m.logRaw = append(m.logRaw, data...)
	}
	for _, b := range data {
		m.rxCount++
		m.rxOffset++
		// A byte after a bare CR that is not LF finishes the line the CR
		// ended (devices that use CR alone as a line break).
		if m.pendingCR {
			m.pendingCR = false
			if b != '\n' {
				m.flushRxLine()
			}
		}
		m.markPending()
		m.pendingBytes = append(m.pendingBytes, b)
		// Line grouping is the same in every view mode: one entry per line,
		// so switching views only re-renders the same entries instead of
		// re-grouping them. Hex views used to group by width, which turned
		// "AT\r\nOK\r\n" received in HEX into a single "AT..OK.." row after
		// F1 went back to ASCII.
		if b == '\n' {
			m.flushRxLine()
			continue
		}
		if b == '\r' {
			m.pendingCR = true
			continue
		}
		switch {
		case m.hexAscii:
			if len(m.pendingBytes) >= 8 {
				m.flushRxLine()
			}
		case m.hexView:
			// Each byte paints as "XX "; flush on the same width rule the
			// old string accumulator used.
			if len(m.pendingBytes)*3 >= budget {
				m.flushRxLine()
			}
		default:
			if len(m.pendingBytes) >= budget {
				m.flushRxLine()
			}
		}
	}
	m.refreshRxContent()
	// Parked in history: tell the user how much scrolled past them instead of
	// yanking the viewport back to the tail.
	if !m.rxFollow {
		if added := len(m.rxLines) - linesBefore; added > 0 {
			m.unseen += added
		}
	}
}

func (m *serialMonitorModel) markPending() {
	if len(m.pendingBytes) == 0 {
		m.pendingAt = time.Now()
	}
}

// renderEntry renders one raw entry into display line(s) under the current
// view mode. RX entries may split into several rows (HEX+ASCII groups by 8
// bytes, HEX/ASCII wrap at the pane budget); TX echoes stay on one row.
func (m *serialMonitorModel) renderEntry(e rxEntry) []string {
	if e.tx {
		return m.renderTxEntry(e)
	}
	var out []string
	budget := m.lineBudget()
	if budget < 3 {
		budget = 3
	}
	if m.hexAscii {
		for i := 0; i < len(e.raw); i += 8 {
			end := i + 8
			if end > len(e.raw) {
				end = len(e.raw)
			}
			line := hexAsciiChunk(e.raw[i:end], e.off+i)
			if m.showTime {
				line = e.at.Format("[15:04:05.000] ") + line
			}
			out = append(out, ansi.Truncate(strings.TrimRight(line, " "), m.rx.Width, ""))
		}
		return out
	}
	if m.hexView {
		// HEX prints every byte, line endings included, so the pane agrees
		// with the RX counter after a mode switch.
		perLine := (budget + 2) / 3 // each byte paints as "XX "
		for i := 0; i < len(e.raw); i += perLine {
			end := i + perLine
			if end > len(e.raw) {
				end = len(e.raw)
			}
			line := m.rxPrefix() + hexText(e.raw[i:end])
			if m.showTime {
				line = e.at.Format("[15:04:05.000] ") + line
			}
			out = append(out, ansi.Truncate(strings.TrimRight(line, " "), m.rx.Width, ""))
		}
		return out
	}
	// ASCII: the trailing CRLF already did its job as the line break, so it
	// is trimmed here; a line that was nothing but a break paints no row.
	// Internal breaks are split as well: an entry collected while a hex view
	// was active was grouped by width, and painting it as one folded row
	// showed the line breaks as "AT..OK..".
	body := bytes.TrimRight(e.raw, "\r\n")
	if len(body) == 0 {
		return nil
	}
	perLine := budget
	for _, seg := range splitRxBreaks(body) {
		if len(seg) == 0 {
			continue
		}
		for i := 0; i < len(seg); i += perLine {
			end := i + perLine
			if end > len(seg) {
				end = len(seg)
			}
			text := asciiFold(seg[i:end])
			line := m.rxPrefix() + text
			if m.showTime {
				line = e.at.Format("[15:04:05.000] ") + line
			}
			out = append(out, ansi.Truncate(strings.TrimRight(line, " "), m.rx.Width, ""))
		}
	}
	return out
}

// splitRxBreaks cuts a body on CRLF, bare CR and bare LF. CRLF is consumed as
// one break so it never leaves an empty segment behind.
func splitRxBreaks(body []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\r':
			out = append(out, body[start:i])
			if i+1 < len(body) && body[i+1] == '\n' {
				i++
			}
			start = i + 1
		case '\n':
			out = append(out, body[start:i])
			start = i + 1
		}
	}
	if start < len(body) {
		out = append(out, body[start:])
	}
	return out
}

// renderTxEntry renders an outgoing echo. The offset|hex|ascii hexdump
// gutter is a received-stream concept — echoing it on TX rows painted a
// meaningless "0000" on every command and split long payloads into orphan
// continuation rows with no TX context, wrecking the 80-col budget. TX rows
// therefore always carry the "TX > [label]" head: text in ASCII mode, a
// compact hex byte stream in both HEX and HEX+ASCII modes, wrapped with
// continuation rows aligned under the first byte.
// renderTxEntry renders an outgoing echo. The offset|hex|ascii hexdump
// gutter is a received-stream concept — echoing it on TX rows painted a
// meaningless "0000" on every command and split long payloads into orphan
// continuation rows with no TX context, wrecking the 80-col budget. TX rows
// therefore always carry the "TX > [label]" head: text in ASCII mode, a
// compact hex byte stream in both HEX and HEX+ASCII modes, wrapped with
// continuation rows aligned under the first byte. Payloads longer than the
// stored echo prefix end with a "+N B" note so nothing disappears silently.
func (m *serialMonitorModel) renderTxEntry(e rxEntry) []string {
	trunc := func(s string) string {
		return ansi.Truncate(strings.TrimRight(s, " "), m.rx.Width, "")
	}
	stamp := ""
	if m.showTime {
		stamp = e.at.Format("[15:04:05.000] ")
	}
	head := m.txPrefix()
	if m.showQuickLabel && e.label != "" {
		head += "[" + e.label + "] "
	}
	raw := e.raw
	hidden := 0
	if e.total > len(raw) {
		hidden = e.total - len(raw)
	}
	// ASCII mode: text, wrapped at the pane edge. Control bytes become \r \n
	// \t or \xNN and UTF-8 stays readable. Falling back to hex for the whole
	// payload — the old rule once a single byte was non-printable — made
	// Chinese commands and hex-typed frames echo as "41 54 ..." and look like
	// the send-mode switch had been ignored.
	if !m.hexView && !m.hexAscii {
		body := raw
		if e.asciiTrim > 0 && e.asciiTrim <= len(body) {
			// The HEX view keeps these bytes (they went out), the text view
			// hides them (Enter added them, the user did not type them).
			body = body[:len(body)-e.asciiTrim]
		}
		if !txNeedsHex(body) {
			avail := m.rx.Width - ansi.StringWidth(stamp) - ansi.StringWidth(head)
			if avail < 3 {
				avail = 3
			}
			indent := strings.Repeat(" ", ansi.StringWidth(head))
			rows := wrapTxText(m.txBody(body), avail)
			out := make([]string, 0, len(rows)+1)
			for i, row := range rows {
				if i == 0 {
					out = append(out, trunc(stamp+head+row))
				} else {
					out = append(out, trunc(stamp+indent+row))
				}
			}
			if hidden > 0 {
				out = append(out, trunc(stamp+indent+fmt.Sprintf("… +%d B", hidden)))
			}
			return out
		}
		// Binary frames fall through to the hex renderer below: a Modbus
		// request echoed as "\x01\x03\x00\x00\x00\x01\x84\n" said nothing,
		// while "01 03 00 00 00 01 84 0A" is the frame the slot fired.
	}
	// HEX / HEX+ASCII: uniform rows of "XX " bytes. The continuation indent
	// mirrors the head width, so every row's byte capacity is the same.
	hexCap := m.rx.Width - ansi.StringWidth(stamp) - ansi.StringWidth(head)
	if hexCap < 3 {
		hexCap = 3
	}
	perLine := hexCap / 3 // "XX " per byte
	if perLine < 1 {
		perLine = 1
	}
	indent := strings.Repeat(" ", ansi.StringWidth(head))
	var out []string
	for i := 0; i < len(raw); i += perLine {
		end := i + perLine
		if end > len(raw) {
			end = len(raw)
		}
		if i == 0 {
			out = append(out, trunc(stamp+head+hexText(raw[:end])))
		} else {
			out = append(out, trunc(stamp+indent+hexText(raw[i:end])))
		}
	}
	if len(out) == 0 {
		out = append(out, trunc(stamp+head))
	}
	if hidden > 0 {
		out = append(out, trunc(stamp+indent+fmt.Sprintf("… +%d B", hidden)))
	}
	return out
}

// txNeedsHex reports whether a payload has no readable text form at all —
// NUL bytes or bytes that are not valid UTF-8, which is what every binary
// frame (Modbus RTU, file contents) looks like. Control bytes alone are not
// enough: a typed CRLF must still echo as \r\n, not as ASCII codes.
func txNeedsHex(raw []byte) bool {
	if !utf8.Valid(raw) {
		return true
	}
	return bytes.IndexByte(raw, 0) >= 0
}

// wrapTxText breaks an echo body into rows of at most width display cells,
// so a long command wraps instead of being cut off at the pane edge.
func wrapTxText(body string, width int) []string {
	var rows []string
	var cur strings.Builder
	w := 0
	for _, r := range body {
		rw := ansi.StringWidth(string(r))
		if w+rw > width && cur.Len() > 0 {
			rows = append(rows, cur.String())
			cur.Reset()
			w = 0
		}
		cur.WriteRune(r)
		w += rw
	}
	rows = append(rows, cur.String())
	return rows
}

// asciiFold maps non-printable bytes to dots so ambiguous glyphs can never
// drift the receive pane off its border on GBK ConHost.
func asciiFold(raw []byte) string {
	var sb strings.Builder
	for _, b := range raw {
		if b >= 0x20 && b <= 0x7e {
			sb.WriteByte(b)
		} else {
			sb.WriteByte('.')
		}
	}
	return sb.String()
}

func hexText(raw []byte) string {
	parts := make([]string, len(raw))
	for i, b := range raw {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, " ")
}

// txBody renders a sent payload for the ASCII view. The echo already holds
// only what was written, never the appended line ending, so nothing is
// trimmed here — a payload that *ends* with 0D 0A because the user typed it
// must still show those bytes.
func (m *serialMonitorModel) txBody(raw []byte) string {
	return txText(raw)
}

// txText keeps readable text readable — including UTF-8 such as Chinese —
// and turns control bytes into \r \n \t or \xNN. Escaping instead of falling
// back to hex keeps one rule for every payload: an ASCII send always echoes
// as ASCII.
func txText(raw []byte) string {
	var b strings.Builder
	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRune(raw[i:])
		if r == utf8.RuneError && size <= 1 {
			fmt.Fprintf(&b, "\\x%02X", raw[i])
			i++
			continue
		}
		switch r {
		case '\r':
			b.WriteString("\\r")
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, "\\x%02X", r)
			} else {
				b.WriteRune(r)
			}
		}
		i += size
	}
	return b.String()
}

// hexAsciiChunk formats one 8-byte group in the offset|hex|ascii gutter.
func hexAsciiChunk(chunk []byte, off int) string {
	var hexPart, asciiPart strings.Builder
	for j, b := range chunk {
		if j > 0 {
			hexPart.WriteString(" ")
		}
		fmt.Fprintf(&hexPart, "%02X", b)
		if b >= 0x20 && b <= 0x7e {
			asciiPart.WriteByte(b)
		} else {
			asciiPart.WriteByte('.')
		}
	}
	return fmt.Sprintf("%04X  %-23s |%s|", off, hexPart.String(), asciiPart.String())
}

func (m *serialMonitorModel) flushRxLine() {
	if len(m.pendingBytes) == 0 {
		return
	}
	if m.pendingAt.IsZero() {
		m.pendingAt = time.Now()
	}
	e := rxEntry{
		raw: append([]byte(nil), m.pendingBytes...),
		at:  m.pendingAt,
		off: m.rxOffset - len(m.pendingBytes),
	}
	m.pendingBytes = nil
	m.pendingCR = false
	m.pendingAt = time.Time{}
	m.addEntry(e)
}

// addEntry stores the raw entry and its rendered lines, caps both at
// maxLines, then refreshes the pane.
func (m *serialMonitorModel) addEntry(e rxEntry) {
	m.rxEntries = append(m.rxEntries, e)
	if len(m.rxEntries) > m.maxLines {
		m.rxEntries = m.rxEntries[len(m.rxEntries)-m.maxLines:]
	}
	m.rxLines = append(m.rxLines, m.renderEntry(e)...)
	if len(m.rxLines) > m.maxLines {
		m.rxLines = m.rxLines[len(m.rxLines)-m.maxLines:]
	}
	m.refreshRxContent()
}

// rebuildRx re-renders every stored entry under the current view mode. This
// is what makes F1/F3 switches apply to the content already on screen.
func (m *serialMonitorModel) rebuildRx() {
	m.rxLines = nil
	for _, e := range m.rxEntries {
		m.rxLines = append(m.rxLines, m.renderEntry(e)...)
	}
	if len(m.rxLines) > m.maxLines {
		m.rxLines = m.rxLines[len(m.rxLines)-m.maxLines:]
	}
	m.refreshRxContent()
}

// highlightLines wraps substring matches of the active search query in the
// selection style. Colour-only ANSI carries zero display width, so borders
// stay aligned; legacyGlyphFallback is applied to the text, not the codes.
func (m *serialMonitorModel) highlightLines(lines []string) []string {
	q := m.searchQuery
	if q == "" {
		return lines
	}
	out := make([]string, len(lines))
	needle := strings.ToLower(q)
	for i, line := range lines {
		low := strings.ToLower(line)
		idx := strings.Index(low, needle)
		if idx < 0 {
			out[i] = line
			continue
		}
		var sb strings.Builder
		pos := 0
		for idx >= 0 {
			sb.WriteString(line[pos:idx]) // text before the match
			match := line[idx : idx+len(q)]
			// Style the match via a zero-width ANSI wrapper.
			sb.WriteString("\x1b[7m" + match + "\x1b[27m")
			pos = idx + len(q)
			next := strings.Index(low[pos:], needle)
			if next < 0 {
				break
			}
			idx = pos + next
		}
		sb.WriteString(line[pos:])
		out[i] = sb.String()
	}
	return out
}

func (m *serialMonitorModel) refreshRxContent() {
	lines := make([]string, 0, len(m.rxLines)+1)
	lines = append(lines, m.rxLines...)
	// Show a partial line immediately without committing it: a chunk may
	// end mid-line, and the next chunk should continue the same row.
	if len(m.pendingBytes) > 0 {
		at := m.pendingAt
		if at.IsZero() {
			at = time.Now()
		}
		e := rxEntry{raw: m.pendingBytes, at: at, off: m.rxOffset - len(m.pendingBytes)}
		lines = append(lines, m.renderEntry(e)...)
	}
	m.searchMatches = nil
	if m.searchQuery != "" {
		needle := strings.ToLower(m.searchQuery)
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), needle) {
				m.searchMatches = append(m.searchMatches, i)
			}
		}
		if m.searchMatch >= len(m.searchMatches) {
			m.searchMatch = 0
		}
	}
	m.rx.SetContent(strings.Join(m.highlightLines(lines), "\n"))
	// Only ride the tail when following. If the user scrolled up to inspect
	// history, SetContent keeps the old YOffset, so refreshing must not yank
	// them back to the newest line.
	if m.rxFollow {
		m.rx.GotoBottom()
	}
}

func (m *serialMonitorModel) closePort() {
	if m.closed {
		return
	}
	m.closed = true
	if m.logRecording {
		_ = m.flushRxLog()
		m.logRecording = false
	}
	if m.port != nil {
		m.ioMu.Lock()
		_ = m.port.Close()
		m.ioMu.Unlock()
	}
}

func (m *serialMonitorModel) doneCmd(err error) tea.Cmd {
	m.closePort()
	return func() tea.Msg { return serialMonitorDoneMsg{err: err} }
}

func (m *serialMonitorModel) switchCmd() tea.Cmd {
	m.closePort()
	return func() tea.Msg { return serialMonitorSwitchMsg{} }
}

func serialReadCmd(port serial.Port, ioMu *sync.Mutex) tea.Cmd {
	if port == nil {
		return nil
	}
	return func() tea.Msg {
		buf := make([]byte, serialMonitorReadSize)
		zeroReads := 0
		for {
			// Never let Close overlap a pending overlapped Read: on Windows
			// that is undefined behaviour in go.bug.st/serial and can kill
			// the process (bugst/go-serial #57, #219). The 100ms read
			// timeout guarantees this lock is released promptly.
			ioMu.Lock()
			n, err := port.Read(buf)
			ioMu.Unlock()
			if n > 0 {
				return serialRxMsg{data: append([]byte(nil), buf[:n]...)}
			}
			if err != nil {
				return serialReadErrMsg{err: err}
			}
			// A timeout returns 0,nil. Drivers without a working timeout
			// would otherwise spin, so back off after a run of empty reads.
			zeroReads++
			if zeroReads >= 50 {
				time.Sleep(10 * time.Millisecond)
				zeroReads = 0
			}
		}
	}
}

func serialTickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return serialTickMsg{} })
}
