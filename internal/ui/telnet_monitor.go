package ui

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	"github.com/zsuroy/ctty/internal/telnetclient"
	"github.com/zsuroy/ctty/internal/telnetconfig"
)

const (
	telnetSettingsCount = 10
)

// Settings rows, in display order. Kept as an index enum so the value editor
// and the renderer cannot drift apart.
const (
	telnetSettingViewMode = iota
	telnetSettingShowPrefix
	telnetSettingShowQuickLabel
	telnetSettingAnsiColor
	telnetSettingShowTime
	telnetSettingLogAppend
	telnetSettingLogDir
	telnetSettingPageKey
	telnetSettingQuickKey
	telnetSettingMaxLines
)

// Messages used by the in-app telnet assistant. The read loop emits one
// message per chunk so Bubble Tea keeps owning the event stream; the old
// pass-through bridge (tea.Exec) is untouched for CLI direct connections.
type (
	// telnetRxMsg carries one display-ready chunk from the wire plus any
	// bytes the auto-login watcher answered with (echoed as TX).
	telnetRxMsg struct {
		data    []byte
		loginTx []byte
	}
	telnetReadErrMsg struct{ err error }
	telnetTickMsg    struct{}
	// telnetMonitorDoneMsg closes the monitor and returns to the telnet list.
	telnetMonitorDoneMsg struct{ err error }
	// telnetMonitorSwitchMsg asks the parent to hand the connection to the
	// full-screen passthrough bridge.
	telnetMonitorSwitchMsg struct{}
)

const (
	telnetMonitorReadSize = 1024
	telnetReadTimeout     = 100 * time.Millisecond
)

// telnetRxEntry keeps one logical receive/TX line in raw form. Display lines
// (rxLines) are derived from these, so switching the view mode (F1/F3)
// re-renders the whole pane instead of only affecting future data.
type telnetRxEntry struct {
	// raw is the complete wire sequence, line endings included. Grouping
	// keeps the CRLF that terminated an ASCII line: it is real data, so the
	// HEX views must show it, while the ASCII view trims it when rendering.
	// Dropping it here made a mode switch lose two bytes per line and the
	// byte count on screen disagree with the RX counter.
	raw []byte
	// view is the same line after ANSI control filtering: SGR colour codes
	// stay so the real terminal can paint them, cursor/erase/OSC sequences
	// are dropped. HEX modes keep using raw; only ASCII rendering uses view.
	view  []byte
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

type telnetMonitorModel struct {
	styles     Styles
	width      int
	height     int
	boxWidth   int
	innerWidth int
	dev        telnetconfig.TelnetHost
	conn       net.Conn
	closed     bool
	session    *telnetclient.Session // IAC filter + negotiation; replies go to conn
	// auto answers login prompts from the display stream, mirroring the raw
	// bridge. It is nil for ad-hoc hosts with no configured credentials, so
	// the session stays fully manual.
	auto         *telnetclient.AutoLogin
	rx           viewport.Model
	rxFollow     bool // auto-scroll to newest data; paused when user scrolls up
	unseen       int  // RX lines arrived while the view is parked in history
	input        textinput.Model
	rxLines      []string // rendered display lines (derived from rxEntries)
	rxEntries    []telnetRxEntry
	pendingBytes []byte
	// pendingView mirrors pendingBytes after ANSI control filtering. Both
	// buffers flush together so HEX stays byte-exact while ASCII can render
	// the server's colour codes without leaking control text into the pane.
	pendingView []byte
	ansiFilter  telnetAnsiFilter
	// pendingCR remembers that the last received byte was a bare CR, so the
	// next byte decides whether it was half of a CRLF or a line break on its
	// own. It lives across reads because a chunk can end right between them.
	pendingCR      bool
	rxOffset       int
	pendingAt      time.Time
	rxCount        int
	txCount        int
	hexView        bool // receive pane display mode: false = ASCII (default), true = HEX
	showPrefix     bool // paint "RX < " / "TX > " on monitor rows; off by default
	showQuickLabel bool // paint "[label] " on quick-command TX echoes; on by default
	ansiColor      bool // render peer SGR colours in the ASCII pane; on by default
	hexAscii       bool
	hexSend        bool
	showTime       bool
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
	quickCmds    []telnetconfig.TelnetQuickCmd
	quickFocus   bool
	quickSel     int // global index into quickCmds, across all pages
	quickPage    int // current page (0-based), 8 slots each
	quickLoadErr error
	pageKey      string // "alt" or "ctrl": which modifier pages the quick bar
	quickKey     string // effective modifier for Alt/Option+1..8: "alt" or "option" (resolved from prefs.QuickKey)

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
	prefs        telnetconfig.TelnetPrefs
	maxLines     int
	settingsOpen bool
	settingsSel  int
	settingsMsg  string

	// helpExpanded trades receive rows for the three-line key map. Default
	// stays compact so an 80x24 cmd window keeps six receive rows.
	helpExpanded bool
}

// telnetConnectMode decides which UI a device opens in. The order mirrors
// telnetconfig.ResolveMode — CLI override, device mode, global setting,
// built-in per-entry default — so the TUI and the CLI can never disagree.
func (m Model) telnetConnectMode(dev telnetconfig.TelnetHost) string {
	global := ""
	if m.appConfig != nil {
		global = config.NormalizeTelnetStartMode(m.appConfig.TelnetStartMode)
	}
	return telnetconfig.ResolveMode(m.telnetForceMode, dev.Mode, global, m.telnetOnly)
}

func newTelnetMonitor(styles Styles, width, height int, dev telnetconfig.TelnetHost) *telnetMonitorModel {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = i18n.T("telnet.monitor.placeholder")
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

	cmds, err := telnetconfig.LoadTelnetQuickCmds()
	prefs, prefsErr := telnetconfig.LoadTelnetPrefs()

	m := &telnetMonitorModel{
		styles:         styles,
		dev:            dev,
		rx:             viewport.New(40, 5),
		rxFollow:       true,
		input:          input,
		interval:       prefs.Interval(),
		maxLines:       prefs.MaxLines,
		hexView:        prefs.HexView,
		showPrefix:     prefs.ShowPrefix,
		showQuickLabel: prefs.ShowQuickLabel,
		ansiColor:      prefs.AnsiColor,
		hexAscii:       prefs.HexAscii,
		hexSend:        prefs.HexSend,
		showTime:       prefs.ShowTime,
		crlf:           true,
		// telnet NVT fixes the line ending at CRLF; the legacy LineEnding
		// preference is ignored so an old "none" config cannot silently drop
		// the Enter that every network CLI expects.
		lineEnd:      "crlf",
		pageKey:      prefs.PageKey,
		quickKey:     telnetconfig.ResolveQuickKey(prefs.QuickKey, runtime.GOOS),
		status:       i18n.T("telnet.monitor.connected"),
		quickCmds:    cmds,
		quickLoadErr: err,
		quickInput:   quickInput,
		quickLabelIn: quickLabelIn,
		searchInput:  searchInput,
		rxRateSince:  time.Now(),
		prefs:        prefs,
	}
	m.ansiFilter.keepSGR = m.ansiColor
	if prefsErr != nil {
		// Config file is malformed; defaults are already applied. Surface a
		// short hint but let the assistant open normally.
		m.status = i18n.T("telnet.monitor.prefs_load_error")
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
func (m *telnetMonitorModel) syncSendPrompt() {
	if m.hexSend {
		m.input.Prompt = i18n.T("telnet.monitor.send_prompt_hex")
		m.input.Placeholder = i18n.T("telnet.monitor.placeholder_hex")
	} else {
		m.input.Prompt = "> "
		m.input.Placeholder = i18n.T("telnet.monitor.placeholder")
	}
	m.fitSendInput()
}

// fitSendInput sizes the field for the current prompt: HEX mode spends three
// extra cells on "hex> ", so subtracting a fixed two left the text spilling
// past the box.
func (m *telnetMonitorModel) fitSendInput() {
	if m.rx.Width <= 0 {
		return
	}
	w := m.rx.Width - ansi.StringWidth(m.input.Prompt) - 1 // +1 for the cursor
	if w < 4 {
		w = 4
	}
	m.input.Width = w
}

func (m *telnetMonitorModel) setSize(width, height int) {
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

// Init dials the host. Called explicitly by the parent when the user
// triggers a connect, so a failure can be shown in-frame.
func (m *telnetMonitorModel) Init() tea.Cmd {
	blink := m.input.Focus()
	conn, err := telnetDial(m.dev.Addr())
	if err != nil {
		m.status = fmt.Sprintf(i18n.T("telnet.monitor.open_error"), err)
		return blink
	}
	m.conn = conn
	m.session = telnetclient.NewSession(conn)
	m.session.SetSize(m.width, m.height)
	m.auto = telnetclient.AutoLoginFor(m.dev)
	return tea.Batch(blink, telnetReadCmd(conn, m.session, m.auto))
}

// telnetDial is the seam the monitor uses to open its connection; tests
// swap it for a fake net.Conn.
var telnetDial = telnetclient.Dial

func (m *telnetMonitorModel) Update(msg tea.Msg) (*telnetMonitorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.setSize(msg.Width, msg.Height)
		if m.session != nil {
			// The peer learns the real window via NAWS answers instead of
			// the conventional 80x24 the raw bridge reports.
			m.session.SetSize(m.width, m.height)
		}
		return m, nil

	case telnetRxMsg:
		if m.closed || m.conn == nil {
			return m, nil
		}
		m.appendRx(msg.data)
		if len(msg.loginTx) > 0 {
			// The auto-login watcher answered a prompt: echo it as TX so the
			// exchange is visible in every view, exactly like a manual send.
			m.txCount += len(msg.loginTx)
			m.appendTxLine("LOGIN", msg.loginTx, nil)
		}
		return m, telnetReadCmd(m.conn, m.session, m.auto)

	case telnetReadErrMsg:
		if m.closed || m.conn == nil {
			return m, nil
		}
		err := msg.err
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			// The peer closed the connection (or we just closed it behind a
			// pending read): a normal disconnect, not a fault.
			err = nil
		}
		return m, m.doneCmd(err)

	case telnetTickMsg:
		if m.closed || !m.repeat {
			return m, nil
		}
		m.sendRepeat()
		return m, telnetTickCmd(m.interval)

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

// macOptionArrow normalises the arrow keys macOS terminals emit for
// Option+←/→/↑/↓. With stock text editing those combos send CSI 1;5 C/D/A/B,
// which Bubble Tea decodes as ctrl+*; Universal "Option as Meta" sends CSI
// 1;3 and arrives as alt+* instead. In Option mode both must mean the same
// thing, or paging/scrolling silently does nothing on a Mac.
func (m *telnetMonitorModel) macOptionArrow(k string) string {
	if m.quickKey != telnetconfig.QuickKeyOption || m.pageKey == "ctrl" {
		return k // ctrl+arrow is a real binding in these modes
	}
	switch k {
	case "ctrl+left":
		return "alt+left"
	case "ctrl+right":
		return "alt+right"
	case "ctrl+up":
		return "alt+up"
	case "ctrl+down":
		return "alt+down"
	// A third common Mac mapping sends ESC b / ESC f (word left/right).
	case "alt+b":
		return "alt+left"
	case "alt+f":
		return "alt+right"
	}
	return k
}

func (m *telnetMonitorModel) updateKeys(key tea.KeyMsg) (*telnetMonitorModel, tea.Cmd) {
	k := m.macOptionArrow(key.String())
	// Alt+1..8 fires a quick command on the current page directly, without
	// moving input focus. The digit is the 1-based slot within the page.
	// Read the raw KeyMsg first: some terminals encode Alt+digit in a way
	// whose String() is empty, and the string path alone would miss it.
	if n, ok := telnetQuickDigitFromKey(key); ok {
		m.fireQuick(m.quickPage*quickCmdCount + n)
		return m, nil
	}
	if n, ok := m.quickIndex(k); ok {
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
	case "f3":
		m.showTime = !m.showTime
		m.rebuildRx()
		return m, nil
	case "f4":
		m.rxLines = nil
		m.rxEntries = nil
		m.pendingBytes = nil
		m.pendingView = nil
		m.ansiFilter.reset()
		m.pendingCR = false
		m.pendingAt = time.Time{}
		// The gutter restarts with the counter so offsets and the RX byte
		// count describe the same session.
		m.rxOffset = 0
		m.rxCount = 0
		m.txCount = 0
		m.rxFollow = true
		m.unseen = 0
		m.refreshRxContent()
		return m, nil
	case "f12":
		// Help lives on F12, not "?": in a shell or network-CLI session the
		// question mark is a payload character (context help, "show ver?" and
		// friends) and must reach the send box like any other rune.
		m.helpExpanded = !m.helpExpanded
		m.setSize(m.width, m.height)
		m.status = i18n.T("telnet.monitor.help_toggle")
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

// jumpLive re-arms auto-scroll and clears the unseen counter.
func (m *telnetMonitorModel) jumpLive() tea.Cmd {
	m.rxFollow = true
	m.unseen = 0
	m.rx.GotoBottom()
	return nil
}

// scrollRx moves the receive viewport by delta lines and keeps the follow
// flag honest: reaching the last line re-arms auto-scroll, anything above it
// parks the view so incoming bytes stop competing with what the user is
// reading. SetYOffset clamps, so over-scrolling is harmless.
func (m *telnetMonitorModel) scrollRx(delta int) {
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
func (m *telnetMonitorModel) quickPageCount() int {
	if len(m.quickCmds) == 0 {
		return 1
	}
	return (len(m.quickCmds) + quickCmdCount - 1) / quickCmdCount
}

// clampQuick keeps page/selection inside the current command list. Called
// after load, add and delete so indices never dangle.
func (m *telnetMonitorModel) clampQuick() {
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
func (m *telnetMonitorModel) setQuickSel(idx int) {
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
func (m *telnetMonitorModel) changeQuickPage(delta int) {
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
func (m *telnetMonitorModel) pageKeyToken() string {
	return pageKeyLabel(m.pageKey, m.quickKey)
}

// pageKeyLabel names the effective paging modifier. On a Mac in Option mode
// "Alt" would be misleading — Option is the key users actually press.
func pageKeyLabel(pageKey, quickKey string) string {
	if pageKey == "ctrl" {
		return "Ctrl"
	}
	if quickKey == telnetconfig.QuickKeyOption {
		return "Option"
	}
	return "Alt"
}

// telnetAltQuickIndex maps Bubble Tea's Alt+digit key strings to a 0-based slot on
// the current page. ConHost reports these as "alt+1".."alt+8"; some builds
// use an ESC prefix.
func telnetAltQuickIndex(k string) (int, bool) {
	if len(k) == 5 && strings.HasPrefix(k, "alt+") {
		if n := k[4] - '0'; n >= 1 && n <= quickCmdCount {
			return int(n) - 1, true
		}
	}
	return 0, false
}

// macOptionDigits are the glyphs a stock macOS keyboard layout emits for
// Option+1..Option+8 when the terminal has NOT been told to treat Option as
// Meta (Esc+). In that state Bubble Tea sees just a rune with Alt=false, so
// telnetAltQuickIndex never matches — the shortcut looked simply broken on Mac.
// Mapping them here makes the bar work out of the box there. Keys 9 and 0
// are deliberately absent: only eight slots exist per page.
var macOptionDigits = map[string]int{
	"\u00a1": 0, // ⌥1 ¡ inverted exclamation
	"\u2122": 1, // ⌥2 ™ trademark
	"\u00a3": 2, // ⌥3 £ pound
	"\u00a2": 3, // ⌥4 ¢ cent
	"\u221e": 4, // ⌥5 ∞ infinity
	"\u00a7": 5, // ⌥6 § section
	"\u00b6": 6, // ⌥7 ¶ pilcrow
	"\u2022": 7, // ⌥8 • bullet
}

// telnetQuickDigitFromKey reads an Alt+digit straight off the KeyMsg instead
// of going through String(). ConHost builds can report the digit as a raw
// Type byte with no Runes, which makes Key.String() return an empty string;
// matching only the rendered form silently drops the shortcut there.
func telnetQuickDigitFromKey(key tea.KeyMsg) (int, bool) {
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

// quickIndex maps a key string to a 0-based slot under the configured
// modifier. "alt" keeps the strict historical behaviour (a real Alt+digit,
// which Mac users can get by setting their terminal's Option key to Esc+).
// "option" additionally accepts the glyphs macOS sends by default, so the
// same eight shortcuts work without changing any terminal setting.
//
// Three wire encodings exist for the same physical Option+digit on a Mac:
//   - plain glyph (¡) when Option is a plain text key
//   - alt+glyph  (alt+¡) when the terminal uses "Option as Meta"
//   - alt+digit  (alt+1) when the terminal sends ESC+1 / an ASCII layout
//
// All three must fire the same slot.
func (m *telnetMonitorModel) quickIndex(k string) (int, bool) {
	if idx, ok := telnetAltQuickIndex(k); ok {
		return idx, true
	}
	if m.quickKey == telnetconfig.QuickKeyOption {
		if idx, ok := macOptionDigits[k]; ok {
			return idx, true
		}
		// "Option as Meta" prefixes the Unicode glyph with ESC, which
		// Bubble Tea reports as alt+<glyph> rather than a bare rune.
		if strings.HasPrefix(k, "alt+") {
			if idx, ok := macOptionDigits[strings.TrimPrefix(k, "alt+")]; ok {
				return idx, true
			}
		}
	}
	return 0, false
}

// quickKeyToken is the modifier name shown in the help line. It follows the
// resolved quickKey so a Mac reads "Option+#" while Windows/Linux read
// "Alt+#" — the hint never names a key the current mode ignores.
func (m *telnetMonitorModel) quickKeyToken() string {
	if m.quickKey == telnetconfig.QuickKeyOption {
		return "Option"
	}
	return "Alt"
}

func (m *telnetMonitorModel) fireQuick(idx int) {
	if idx < 0 || idx >= len(m.quickCmds) {
		return
	}
	cmd := m.quickCmds[idx]
	// telnet is a text protocol: a quick slot sends its text plus the NVT
	// line ending, exactly like a typed command. A blank label+value slot is
	// rejected so Enter cannot "send" nothing and echo a bare tag.
	payload := []byte(cmd.Value)
	if len(payload) == 0 {
		payload = []byte(cmd.Label)
	}
	if len(payload) == 0 {
		m.status = i18n.T("telnet.quick.no_payload")
		return
	}
	m.writePayload(payload, true)
	m.txCount += m.wireLen(len(payload), true)
	m.appendTxLine(cmd.Label, payload, m.sendSuffix(true))
	m.status = fmt.Sprintf(i18n.T("telnet.monitor.tx_bytes"), m.wireLen(len(payload), true))
}

// editorListRows is how many command rows fit in the F9 list without pushing
// the panel past the screen. Fixed overhead: title+blank+rule+label+value+
// blank+help1+help2+msg = 9, plus container border(2)+padding(2) = 4. One more
// row is reserved for the trailing newline the list builder emits.
//
// When the list overflows, the scroll indicator is drawn inside the same
// window, so one row is given up rather than appended — otherwise a short
// terminal pushes the bottom border off screen.
func (m *telnetMonitorModel) editorListRows() int {
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
func (m *telnetMonitorModel) clampEditor() {
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
func (m *telnetMonitorModel) commitQuickEdit() {
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
func (m *telnetMonitorModel) loadQuickEdit() {
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
func (m *telnetMonitorModel) focusQuickField() {
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
func (m *telnetMonitorModel) finishQuickEdit() {
	m.quickEditOn = false
	m.quickLabelIn.SetValue("")
	m.quickInput.SetValue("")
	m.quickLabelIn.Blur()
	m.quickInput.Blur()
}

func (m *telnetMonitorModel) beginQuickEdit(label bool) {
	m.quickEditOn = true
	m.quickEditLabel = label
	m.loadQuickEdit()
}

func (m *telnetMonitorModel) openEditor() {
	m.quickEditing = true
	m.quickEditSel = 0
	m.quickEditTop = 0
	m.quickEditOn = false
	m.quickMsg = ""
	m.quickDirty = false
	m.quickEscArmed = false
	cmds, err := telnetconfig.LoadTelnetQuickCmds()
	if err == nil {
		m.quickCmds = cmds
	}
	m.clampQuick()
	m.clampEditor()
	m.quickLoadErr = err
}

func (m *telnetMonitorModel) updateEditor(key tea.KeyMsg) (*telnetMonitorModel, tea.Cmd) {
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
			m.quickMsg = i18n.T("telnet.quick.unsaved")
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
		if err := telnetconfig.SaveTelnetQuickCmds(m.quickCmds); err != nil {
			m.quickMsg = fmt.Sprintf(i18n.T("telnet.quick.save_error"), err)
		} else {
			m.quickDirty = false
			m.quickMsg = i18n.T("telnet.quick.saved")
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
		m.quickCmds = append(m.quickCmds, telnetconfig.TelnetQuickCmd{Label: "", Value: "", Hex: m.hexSend})
		m.quickEditSel = len(m.quickCmds) - 1
		m.clampQuick()
		m.clampEditor()
		m.beginQuickEdit(true)
		m.quickDirty = true
		m.quickMsg = i18n.T("telnet.quick.added")
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
		m.quickMsg = i18n.T("telnet.quick.deleted")
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
func (m *telnetMonitorModel) toggleSlotEncoding() {
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
		m.quickMsg = i18n.T("telnet.quick.keep_hex")
	}
}

// retargetQuickCmd re-expresses a single slot into the target send mode. It
// is the per-slot primitive shared by the global F2 batch and the editor's
// own F2. It returns false when the bytes have no lossless text form (a
// binary frame), in which case the slot is left untouched.
func (m *telnetMonitorModel) retargetQuickCmd(c *telnetconfig.TelnetQuickCmd, hexSend bool) bool {
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
		c.Value = telnetHexText(m.withLineEnding([]byte(src)))
		c.Hex = true
		return true
	}
	if !c.Hex {
		return true
	}
	b, err := hex.DecodeString(telnetNormalizeHexInput(c.Value))
	if err != nil || len(b) == 0 {
		return false
	}
	if suffix := m.lineEndSuffix(); len(suffix) > 0 {
		if !bytes.HasSuffix(b, suffix) {
			return false // no line ending: ASCII send would add bytes
		}
		b = b[:len(b)-len(suffix)]
	}
	if !telnetPrintableText(b) {
		return false // binary frame, no text form
	}
	c.Value = string(b)
	c.Hex = false
	return true
}

// openSettings loads the latest prefs into a draft and opens the F10 page.
// The draft is only committed on Enter, so Esc leaves the live session and
// the config file untouched.
func (m *telnetMonitorModel) openSettings() {
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
func (m *telnetMonitorModel) livePrefs() telnetconfig.TelnetPrefs {
	p, err := telnetconfig.LoadTelnetPrefs()
	if err != nil {
		p = m.prefs
	}
	p.HexView = m.hexView
	p.ShowPrefix = m.showPrefix
	p.ShowQuickLabel = m.showQuickLabel
	p.AnsiColor = m.ansiColor
	p.HexAscii = m.hexAscii
	p.HexSend = m.hexSend
	p.ShowTime = m.showTime
	p.LineEnding = "crlf" // telnet NVT fixed; legacy field kept only for config compatibility
	p.IntervalMs = int(m.interval / time.Millisecond)
	p.MaxLines = m.maxLines
	p.PageKey = m.pageKey
	// Keep the raw preference (auto/alt/option) rather than the resolved
	// value, so Esc-cancel does not silently pin a platform default into the
	// config file.
	p.QuickKey = m.prefs.QuickKey
	return p.Normalize()
}

func (m *telnetMonitorModel) updateSettings(key tea.KeyMsg) (*telnetMonitorModel, tea.Cmd) {
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
		m.settingsSel = (m.settingsSel - 1 + telnetSettingsCount) % telnetSettingsCount
		return m, nil
	case "down":
		m.settingsSel = (m.settingsSel + 1) % telnetSettingsCount
		return m, nil
	case "enter":
		p := m.prefs.Normalize()
		if err := telnetconfig.SaveTelnetPrefs(p); err != nil {
			m.settingsMsg = fmt.Sprintf(i18n.T("telnet.settings.save_error"), err)
			return m, nil
		}
		// Restore the saved display choices immediately: the view mode and
		// the TX/RX prefix are exactly what the F10 panel edits. The line
		// ending stays fixed at CRLF by the telnet NVT, and sending stays
		// plain text.
		m.prefs = p
		m.hexView = p.HexView
		m.showPrefix = p.ShowPrefix
		m.showQuickLabel = p.ShowQuickLabel
		m.ansiColor = p.AnsiColor
		m.ansiFilter.keepSGR = m.ansiColor
		m.refreshAnsiViews()
		m.pendingView = telnetFilterAnsiBytes(m.pendingBytes, m.ansiColor)
		m.hexAscii, m.hexSend = false, false
		m.syncSendPrompt()
		m.showTime = p.ShowTime
		m.setLineEnd("crlf")
		m.interval = p.Interval()
		m.maxLines = p.MaxLines
		m.pageKey = p.PageKey
		// Resolve "auto" against this machine so the saved setting takes
		// effect immediately instead of only after a restart.
		m.quickKey = telnetconfig.ResolveQuickKey(p.QuickKey, runtime.GOOS)
		// Display-mode changes apply to the content already on screen.
		if len(m.rxEntries) > m.maxLines {
			m.rxEntries = m.rxEntries[len(m.rxEntries)-m.maxLines:]
		}
		m.rebuildRx()
		m.settingsOpen = false
		m.settingsMsg = ""
		m.status = i18n.T("telnet.settings.saved")
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
func (m *telnetMonitorModel) adjustSetting(sel, delta int) {
	switch sel {
	case telnetSettingViewMode:
		// Display mode only: sending stays plain text with a CRLF terminator.
		m.prefs.HexView = !m.prefs.HexView
	case telnetSettingShowPrefix:
		m.prefs.ShowPrefix = !m.prefs.ShowPrefix
	case telnetSettingShowQuickLabel:
		m.prefs.ShowQuickLabel = !m.prefs.ShowQuickLabel
	case telnetSettingAnsiColor:
		m.prefs.AnsiColor = !m.prefs.AnsiColor
	case telnetSettingShowTime:
		m.prefs.ShowTime = !m.prefs.ShowTime
	case telnetSettingLogAppend:
		m.prefs.LogAppend = !m.prefs.LogAppend
	case telnetSettingLogDir:
		if strings.TrimSpace(m.prefs.LogDir) == "" {
			m.prefs.LogDir = "logs"
		} else {
			m.prefs.LogDir = ""
		}
	case telnetSettingPageKey:
		if m.prefs.PageKey == "alt" {
			m.prefs.PageKey = "ctrl"
		} else {
			m.prefs.PageKey = "alt"
		}
	case telnetSettingQuickKey:
		// auto -> alt -> option -> auto; auto resolves per OS.
		switch m.prefs.QuickKey {
		case telnetconfig.QuickKeyAuto:
			m.prefs.QuickKey = telnetconfig.QuickKeyAlt
		case telnetconfig.QuickKeyAlt:
			m.prefs.QuickKey = telnetconfig.QuickKeyOption
		default:
			m.prefs.QuickKey = telnetconfig.QuickKeyAuto
		}
	case telnetSettingMaxLines:
		m.prefs.MaxLines += delta * 100
	}
	m.prefs = m.prefs.Normalize()
}

// toggleRxLog starts or stops a raw-byte receive log. The displayed pane is
// folded for GBK ConHost, so it must never be used as capture evidence.
func (m *telnetMonitorModel) toggleRxLog() {
	if m.logRecording {
		path := m.logPath
		if err := m.flushRxLog(); err != nil {
			m.status = fmt.Sprintf(i18n.T("telnet.monitor.log_error"), err)
			return
		}
		m.logRecording = false
		m.status = fmt.Sprintf(i18n.T("telnet.monitor.log_stopped"), path)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		m.status = fmt.Sprintf(i18n.T("telnet.monitor.log_error"), err)
		return
	}
	dir := m.prefs.LogDir
	if dir == "" {
		dir = home
	} else if err := os.MkdirAll(dir, 0755); err != nil {
		m.status = fmt.Sprintf(i18n.T("telnet.monitor.log_error"), err)
		return
	}
	name := fmt.Sprintf("ctty-telnet-%s.log", time.Now().Format("20060102-150405"))
	m.logPath = filepath.Join(dir, name)
	m.logRaw = nil
	m.logRecording = true
	m.status = fmt.Sprintf(i18n.T("telnet.monitor.log_started"), m.logPath)
}

func (m *telnetMonitorModel) flushRxLog() error {
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

func (m *telnetMonitorModel) jumpSearch(delta int) {
	if len(m.searchMatches) == 0 {
		m.status = i18n.T("telnet.monitor.search_none")
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
	m.status = fmt.Sprintf(i18n.T("telnet.monitor.search_at"), m.searchMatch+1, len(m.searchMatches))
}

// updateSearch handles the Ctrl+F keyword-highlight input line. Typing filters
// as you go; Enter commits the highlight and Esc clears it.
func (m *telnetMonitorModel) updateSearch(key tea.KeyMsg) (*telnetMonitorModel, tea.Cmd) {
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
func (m *telnetMonitorModel) View() string {
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
	titleRow := m.monitorTitleRow(inner)

	followWord := i18n.T("telnet.monitor.live")
	if !m.rxFollow {
		followWord = i18n.T("telnet.monitor.paused")
		if m.unseen > 0 {
			followWord = fmt.Sprintf("%s +%d", followWord, m.unseen)
		}
	}
	focusWord := i18n.T("telnet.monitor.focus_input")
	if m.quickFocus {
		focusWord = i18n.T("telnet.monitor.focus_quick")
	}
	status := fmt.Sprintf("[%s] %s  RX %d (%d/s)  TX %d  TIME:%s  Q%d/%d",
		followWord, focusWord,
		m.rxCount, m.rxRate, m.txCount,
		telnetOnOff(m.showTime),
		m.quickPage+1, m.quickPageCount())
	statusRow := m.styles.HelpText.Align(lipgloss.Left).Width(inner).
		Render(telnetFitStatusRow(status, m.status, inner))
	if m.searchOn {
		statusRow = m.styles.HelpText.Align(lipgloss.Left).Width(inner).
			Render(telnetFitMonitorRow("  "+i18n.T("telnet.monitor.search_prompt")+" "+m.searchInput.View(), inner))
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
	case m.conn == nil && m.status != "":
		rxBody = telnetRenderOpenFailure(m.status, m.rx.Width, m.rx.Height)
	case m.conn != nil && len(m.rxEntries) == 0 && len(m.pendingBytes) == 0:
		rxBody = telnetRenderOpenFailure(i18n.T("telnet.monitor.empty"), m.rx.Width, m.rx.Height)
	}

	txContent := m.sendBoxContent(inner - 2)
	quickRows := m.quickBarRows(inner)

	helpRows := []string{
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).
			Render(telnetFitMonitorRow("  "+fmt.Sprintf(i18n.T("telnet.monitor.help_short"), m.quickKeyToken(), m.pageKeyToken()), inner)),
	}
	if m.helpExpanded {
		helpRows = []string{
			m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow("  "+i18n.T("telnet.monitor.help1"), inner)),
			m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow("  "+fmt.Sprintf(i18n.T("telnet.monitor.help2"), m.pageKeyToken()), inner)),
			m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow("  "+i18n.T("telnet.monitor.help3"), inner)),
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
func (m *telnetMonitorModel) sendBoxContent(width int) string {
	// bubbles textinput slices its placeholder by display width, which is
	// wrong for CJK (2 cells/rune) and leaves uninitialized NULs in the view.
	// Strip them at paint time; the real value and payload are untouched.
	input := strings.ReplaceAll(m.input.View(), "\x00", "")

	// Right-hand hints: history position (while browsing) and the live
	// byte count of the text Enter will send (plus the NVT line ending).
	preview := ""
	if v := m.input.Value(); v != "" {
		preview = fmt.Sprintf(i18n.T("telnet.monitor.bytes"), m.wireLen(len(v), true))
	}
	hint := ""
	if len(m.history) > 0 && m.histIdx < len(m.history) {
		hint = fmt.Sprintf(i18n.T("telnet.monitor.hist"), m.histIdx+1, len(m.history))
	}
	// Compose the two hints into one right-aligned suffix.
	suffix := ""
	if hint != "" && preview != "" {
		suffix = hint + "  " + preview
	} else if preview != "" {
		suffix = preview
	} else {
		suffix = hint
	}
	renderedSuffix := suffix
	// The suffix may carry i18n text with DBCS-ambiguous glyphs. On GBK
	// ConHost those paint two cells while StringWidth budgets one,
	// overflowing the send row — fold them to deterministic ASCII before
	// any width math, exactly like the input text on the next line.
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
func (m *telnetMonitorModel) quickBarRows(inner int) [2]string {
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

func (m *telnetMonitorModel) quickSlot(idx, cellW int) string {
	// Empty slots stay blank: no fake number, no fake label.
	if idx < 0 || idx >= len(m.quickCmds) {
		return strings.Repeat(" ", cellW)
	}
	// Page-local 1-based number: Alt+n fires slot n on the current page, so
	// the badge must read [1]..[8] on every page, never global [9]..[16].
	num := idx%quickCmdCount + 1
	label := m.quickCmds[idx].Label
	text := "[" + fmt.Sprintf("%d", num) + "] " + label
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
func (m *telnetMonitorModel) editorView() string {
	inner := m.innerWidth
	if inner < 20 {
		inner = 20
	}
	title := m.styles.Header.Align(lipgloss.Left).Width(inner).
		Render(telnetFitMonitorRow(i18n.T("telnet.quick.title"), inner))

	// Column budget. The row reads left to right as "which slot, what it is
	// called, what it sends, how many bytes" — the two halves of a command
	// never sit on top of each other, so "ls" next to "ls" cannot look like a
	// rendering bug.
	const (
		numW  = 2 // slot number, right aligned
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
	rightW := byteW
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
		val := c.Value
		if val == "" {
			val = c.Label
		}
		byteTag := fmt.Sprintf("%dB", len([]byte(val)))
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
			name = i18n.T("telnet.quick.unnamed")
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
		byteCell := strings.Repeat(" ", byteW-ansi.StringWidth(byteTag)) + byteTag
		var row string
		if sel {
			// One solid bar for the selected slot: the "> " marker alone gets
			// lost once the list is longer than a glance.
			row = m.styles.Selected.Render(plain + byteCell)
		} else {
			row = plain + byteCell
		}
		sb.WriteString(row + "\n")
	}
	if len(m.quickCmds) == 0 {
		sb.WriteString("  " + i18n.T("telnet.quick.empty") + "\n")
	}
	list := sb.String()
	// Scroll indicator when the list is longer than the visible window.
	if total := len(m.quickCmds); total > vis {
		indicator := fmt.Sprintf(i18n.T("telnet.quick.window"), m.quickEditTop+1, end, total)
		list += "  " + indicator + "\n"
	}

	// The two halves of the selected command, always on screen while editing.
	// Each row is "label | value" with a fixed label gutter, so the caret can
	// move between them without either line jumping sideways.
	labelRow, valueRow := "", ""
	if m.quickEditSel >= 0 && m.quickEditSel < len(m.quickCmds) {
		c := m.quickCmds[m.quickEditSel]
		labelName := i18n.T("telnet.quick.field_label")
		valueName := i18n.T("telnet.quick.field_value")
		gutterW := ansi.StringWidth(labelName)
		if w := ansi.StringWidth(valueName); w > gutterW {
			gutterW = w
		}
		// "  " + name + " | " leaves the value at one shared column.
		headW := 2 + gutterW + 3

		val := c.Value
		if val == "" {
			val = c.Label
		}
		hint := fmt.Sprintf("%dB", len([]byte(val)))
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
	help1, help2 := "telnet.quick.help1", "telnet.quick.help2"
	if m.quickEditOn {
		help1, help2 = "telnet.quick.edit_help1", "telnet.quick.edit_help2"
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, "",
		m.styles.HelpText.Render(strings.TrimSuffix(list, "\n")),
		rule,
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow(labelRow, inner)),
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow(valueRow, inner)),
		"",
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow("  "+i18n.T(help1), inner)),
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow("  "+i18n.T(help2), inner)),
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow("  "+m.quickMsg, inner)),
	)
	box := alignJetBrainsBox(m.styles.FormContainer.Width(m.boxWidth).Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}

// editorFieldRow renders one half of the slot being edited: a fixed-width
// name in the gutter, a rule, then the body. Focusing a field tints its name
// and its text, so the caret is never the only sign of where typing lands —
// the "> " marker is reserved for the selected slot in the list above.
func (m *telnetMonitorModel) editorFieldRow(name, text string, gutterW, headW, avail int, hint string, focused bool) string {
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

func (m *telnetMonitorModel) settingsView() string {
	inner := m.innerWidth
	if inner < 20 {
		inner = 20
	}
	title := m.styles.Header.Align(lipgloss.Left).Width(inner).
		Render(telnetFitMonitorRow(i18n.T("telnet.settings.title"), inner))

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
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow("  "+i18n.T("telnet.settings.help1"), inner)),
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow("  "+i18n.T("telnet.settings.help2"), inner)),
		m.styles.HelpText.Align(lipgloss.Left).Width(inner).Render(telnetFitMonitorRow("  "+m.settingsMsg, inner)),
	)
	box := alignJetBrainsBox(m.styles.FormContainer.Width(m.boxWidth).Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}

type telnetSettingsRow struct{ label, value string }

func (m *telnetMonitorModel) settingsRows() []telnetSettingsRow {
	p := m.prefs
	return []telnetSettingsRow{
		{i18n.T("telnet.settings.view_mode"), telnetModeWord(p.HexView, i18n.T("telnet.settings.view_hex"), i18n.T("telnet.settings.view_ascii"))},
		{i18n.T("telnet.settings.show_prefix"), telnetOnOff(p.ShowPrefix)},
		{i18n.T("telnet.settings.show_quick_label"), telnetOnOff(p.ShowQuickLabel)},
		{i18n.T("telnet.settings.ansi_color"), telnetOnOff(p.AnsiColor)},
		{i18n.T("telnet.settings.show_time"), telnetOnOff(p.ShowTime)},
		{i18n.T("telnet.settings.log_append"), telnetOnOff(p.LogAppend)},
		{i18n.T("telnet.settings.log_dir"), telnetLogDirLabel(p.LogDir)},
		{i18n.T("telnet.settings.page_key"), pageKeyLabel(p.PageKey, telnetconfig.ResolveQuickKey(p.QuickKey, runtime.GOOS))},
		{i18n.T("telnet.settings.quick_key"), quickKeyLabel(p.QuickKey)},
		{i18n.T("telnet.settings.max_lines"), fmt.Sprintf("%d", p.MaxLines)},
	}
}

// quickKeyLabel renders the stored modifier with its effective value, so an
// "auto" setting tells the user what it resolved to on this machine.
func quickKeyLabel(v string) string {
	eff := telnetconfig.ResolveQuickKey(v, runtime.GOOS)
	word := "ALT"
	if eff == telnetconfig.QuickKeyOption {
		word = "OPTION"
	}
	if v == telnetconfig.QuickKeyAuto {
		return "AUTO (" + word + ")"
	}
	return strings.ToUpper(v)
}

func telnetLogDirLabel(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "home"
	}
	return dir
}

func (m *telnetMonitorModel) rxModeLabel() string {
	if m.hexAscii {
		return "HEX+ASCII"
	}
	return telnetModeWord(m.hexView, "HEX", "ASCII")
}

func telnetModeWord(hex bool, hexWord, plainWord string) string {
	if hex {
		return hexWord
	}
	return plainWord
}

// rxPrefix / txPrefix are the single source of truth for the direction
// marker. When the F10 "Show TX/RX prefix" switch is off they return an
// empty string, so both the renderer and the line-budget math reserve zero
// columns instead of silently disagreeing.
func (m *telnetMonitorModel) rxPrefix() string {
	if !m.showPrefix {
		return ""
	}
	return rxMark
}

func (m *telnetMonitorModel) txPrefix() string {
	if !m.showPrefix {
		return ""
	}
	return txMark
}

func telnetRenderOpenFailure(msg string, w, h int) string {
	if w < 8 {
		w = 8
	}
	lines := strings.Split(telnetFitMonitorRow(msg, w), "\n")
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

// telnetFitMonitorRow folds ambiguous glyphs before measuring, so a long help or
// status line can never push the right edge of the assistant frame outward.
func telnetFitMonitorRow(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(legacyGlyphFallback(s), width, "")
}

// monitorTitleRow keeps the ctty mark visible on the telnet page without
// spending another terminal row: the product title and host summary stay
// left-aligned, while a theme-highlighted ASCII [ctty] badge is right-aligned.
func (m *telnetMonitorModel) monitorTitleRow(width int) string {
	left := fmt.Sprintf("%s  %s  %s",
		i18n.T("telnet.monitor.title"), m.dev.Name, m.dev.Addr())
	// Reuse the theme's selected-chip colors so the brand reads as an accent
	// instead of blending into the header. ASCII only: no ConHost DBCS drift.
	brand := m.styles.Selected.Bold(true).Padding(0, 1).Render(i18n.T("telnet.monitor.brand"))
	brandW := ansi.StringWidth(brand)
	if width <= brandW {
		return m.styles.Header.Align(lipgloss.Left).Width(width).Render(telnetFitMonitorRow(brand, width))
	}
	leftW := width - brandW
	left = telnetFitMonitorRow(left, leftW)
	left += strings.Repeat(" ", leftW-ansi.StringWidth(left))
	return m.styles.Header.Align(lipgloss.Left).Width(width).Render(left + brand)
}

// telnetFitStatusRow renders the status line as two panes: the live counters/modes
// on the left and the most recent one-shot feedback (sent bytes, save result,
// errors) on the right. Each pane is folded for the legacy console and
// truncated independently, so a long status can never push the right border
// out or hide the counters.
func telnetFitStatusRow(state, feedback string, width int) string {
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

func telnetOnOff(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

func (m *telnetMonitorModel) stepInterval(delta time.Duration) {
	m.interval += delta
	if m.interval < 100*time.Millisecond {
		m.interval = 100 * time.Millisecond
	}
	if m.interval > 10*time.Second {
		m.interval = 10 * time.Second
	}
}

func (m *telnetMonitorModel) historyPrev() {
	if len(m.history) == 0 || m.histIdx <= 0 {
		return
	}
	m.histIdx--
	m.input.SetValue(m.history[m.histIdx])
}

func (m *telnetMonitorModel) historyNext() {
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

func (m *telnetMonitorModel) payload() ([]byte, error) {
	return m.payloadBytes(m.input.Value(), m.hexSend)
}

// payloadBytes decodes raw text into the bytes Enter would send. Shared by
// the send path and the live byte-count preview so they can never disagree.
func (m *telnetMonitorModel) payloadBytes(raw string, hexMode bool) ([]byte, error) {
	if !hexMode {
		return []byte(raw), nil
	}
	b, err := hex.DecodeString(telnetNormalizeHexInput(raw))
	if err != nil {
		// The raw encoding/hex text ("invalid byte: U+0074 't'") tells a
		// telnet user nothing; say what the mode expects instead.
		return nil, errors.New(i18n.T("telnet.monitor.badhex_hint"))
	}
	return b, nil
}

// telnetNormalizeHexInput accepts the spellings people actually paste instead of
// rejecting them: "01 03", "01,03,00", "0x01 0x03", "\x01\x03", "01-03",
// "01:03", tabs and line breaks.
func telnetNormalizeHexInput(raw string) string {
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
func (m *telnetMonitorModel) retargetInput() (cleared bool) {
	v := m.input.Value()
	if strings.TrimSpace(v) == "" {
		m.input.SetValue("")
		return false
	}
	if m.hexSend {
		// Spell out the bytes Enter would really put on the wire, including
		// the line ending ASCII send appends. Otherwise F2 silently dropped
		// the CRLF: "AT" went out as 04 54 0D 0A, then as 41 54.
		m.input.SetValue(telnetHexText(m.withLineEnding([]byte(v))))
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
	if !telnetPrintableText(b) {
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
func (m *telnetMonitorModel) retargetQuickCmds(hexSend bool) (converted, kept int) {
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

func telnetPrintableText(b []byte) bool {
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
var telnetLineEndChoices = []string{"crlf", "cr", "lf", "lfcr", "none"}

func telnetLineEndLabel(v string) string {
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

func (m *telnetMonitorModel) setLineEnd(v string) {
	m.lineEnd = v
	m.crlf = v == "crlf"
	m.prefs.Crlf = m.crlf
	m.prefs.LineEnding = v
}

// lineEndSuffix is the NVT line ending. RFC 854 fixes it at CRLF; telnet has
// no configurable line ending (that was a serial-port concept). Returning a
// constant here means a stale lineEnd field from an old config can never
// drop the Enter a network CLI expects.
func (m *telnetMonitorModel) lineEndSuffix() []byte {
	return []byte{'\r', '\n'}
}

func (m *telnetMonitorModel) withLineEnding(payload []byte) []byte {
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
func (m *telnetMonitorModel) sendSuffix(addLineEnding bool) []byte {
	if !addLineEnding {
		return nil
	}
	return m.lineEndSuffix()
}

// wireLen is the number of bytes actually sent for a payload of n bytes,
// including the configured line-ending suffix when it is appended.
func (m *telnetMonitorModel) wireLen(n int, addLineEnding bool) int {
	if n == 0 || !addLineEnding {
		return n
	}
	return n + len(m.lineEndSuffix())
}

// writePayload sends a payload, optionally appending the configured line
// ending. HEX sends pass false: the user spelled out the exact frame, and an
// invisible 0D 0A made a 4-byte script frame go out as 6 bytes while the echo
// still showed 4.
// ensureSession lazily binds a telnet session to the current connection.
// Init does this eagerly; tests that install a fake conn directly rely on
// this path so outbound bytes still get RFC 854 IAC stuffing.
func (m *telnetMonitorModel) ensureSession() {
	if m.conn != nil && m.session == nil {
		m.session = telnetclient.NewSession(m.conn)
	}
}

func (m *telnetMonitorModel) writePayload(payload []byte, addLineEnding bool) {
	m.ensureSession()
	if m.conn == nil || m.session == nil {
		return
	}
	wire := payload
	if addLineEnding {
		wire = m.withLineEnding(payload)
	}
	// All outbound bytes go through the telnet session so a data 0xFF is
	// stuffed as IAC IAC. A bare conn.Write here would let application data
	// inject an IAC command and desynchronise the peer's parser.
	// Session.Write is a single conn.Write of the stuffed buffer; net.Conn
	// writes are atomic enough for the monitor's single writer.
	_, _ = m.session.Write(wire)
}

// appendTxLine echoes an outgoing payload into the receive pane with a TX
// direction mark so sent and received data are visually distinct. The raw
// bytes are stored, so the echo re-renders when the view mode changes —
// a dropped file shows as HEX+ASCII there just like received data.
func (m *telnetMonitorModel) appendTxLine(label string, payload, suffix []byte) {
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
	e := telnetRxEntry{
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
func (m *telnetMonitorModel) sendCurrent() {
	if m.closed || m.conn == nil {
		m.status = i18n.T("telnet.monitor.not_connected")
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
	// telnet is a text protocol: the NVT line ending is always appended.
	addLineEnding := true
	m.lastPayload = payload
	m.lastAddLineEnding = addLineEnding
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
	m.status = fmt.Sprintf(i18n.T("telnet.monitor.tx_bytes"), m.wireLen(len(payload), addLineEnding))
}

// sendRepeat handles a loop tick. It sends only the remembered payload and
// never touches the input box, so a manual Enter during a loop stays independent.
func (m *telnetMonitorModel) sendRepeat() {
	if m.closed || m.conn == nil || len(m.lastPayload) == 0 {
		return
	}
	payload := m.lastPayload
	addLineEnding := m.lastAddLineEnding
	m.writePayload(payload, addLineEnding)
	m.txCount += m.wireLen(len(payload), addLineEnding)
	m.appendTxLine("", payload, m.sendSuffix(addLineEnding))
	m.status = fmt.Sprintf(i18n.T("telnet.monitor.tx_bytes"), m.wireLen(len(payload), addLineEnding))
}

// detectDroppedFile recognises a pasted file path in the input. Dropping a
// file onto a Windows terminal pastes its path (quoted when it contains
// spaces), so this treats such a line as "send this file" on Enter. To keep
// ordinary device text safe from false positives the candidate must carry a
// path separator (or quotes) and must exist on disk.
func (m *telnetMonitorModel) detectDroppedFile(raw string) (string, bool) {
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
func (m *telnetMonitorModel) sendFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		m.status = fmt.Sprintf(i18n.T("telnet.monitor.file_error"), err)
		return
	}
	if len(data) == 0 {
		m.status = i18n.T("telnet.monitor.file_empty")
		return
	}
	m.writeRaw(data)
	m.txCount += len(data)
	m.appendTxLine("FILE", data, nil)
	m.status = fmt.Sprintf(i18n.T("telnet.monitor.file_sent"), len(data))
	m.input.SetValue("")
}

func (m *telnetMonitorModel) writeRaw(payload []byte) {
	m.ensureSession()
	if m.conn == nil || m.session == nil {
		return
	}
	// Raw file bytes still cross a telnet connection: every data 0xFF must
	// be stuffed or the peer reads it as an IAC command.
	_, _ = m.session.Write(payload)
}

func (m *telnetMonitorModel) lineBudget() int {
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

// telnetAnsiFilter strips control sequences that have no meaning in a
// scrollback log (cursor moves, erase-line such as ESC[0K, screen clears,
// OSC strings) while keeping SGR colour codes so the real terminal can still
// paint the peer's colours. State lives here because a read can end in the
// middle of a sequence. Raw bytes are never touched: HEX views still show the
// exact wire traffic, and Ctrl+L logs it unchanged.
type telnetAnsiFilter struct {
	state   int
	buf     []byte
	keepSGR bool // when false, control sequences are dropped instead of painted
}

const (
	telnetAnsiGround = iota
	telnetAnsiEsc
	telnetAnsiCsi
	telnetAnsiOsc
	telnetAnsiString
	telnetAnsiStringEsc
	telnetAnsiIntermediate
)

// telnetAnsiMaxSeq bounds a malformed sequence so it cannot swallow the rest
// of the session.
const telnetAnsiMaxSeq = 64

func (f *telnetAnsiFilter) reset() {
	f.state = telnetAnsiGround
	f.buf = nil
}

// feed takes one wire byte and returns the bytes safe for the ASCII view.
func (f *telnetAnsiFilter) feed(b byte) []byte {
	if f.state == telnetAnsiGround {
		switch b {
		case 0x1b:
			f.state = telnetAnsiEsc
			f.buf = append(f.buf[:0], b)
			return nil
		case 0x9b: // 8-bit CSI
			f.state = telnetAnsiCsi
			f.buf = append(f.buf[:0], 0x1b, '[')
			return nil
		case 0x9d: // 8-bit OSC
			f.state = telnetAnsiOsc
			f.buf = append(f.buf[:0], 0x1b, ']')
			return nil
		default:
			return []byte{b}
		}
	}
	if len(f.buf) >= telnetAnsiMaxSeq {
		f.reset()
		return f.feed(b)
	}
	f.buf = append(f.buf, b)
	switch f.state {
	case telnetAnsiEsc:
		switch {
		case b == '[':
			f.state = telnetAnsiCsi
		case b == ']':
			f.state = telnetAnsiOsc
		case b == 'P', b == 'X', b == '^', b == '_': // string sequences
			f.state = telnetAnsiString
		case b >= 0x20 && b <= 0x2f:
			f.state = telnetAnsiIntermediate
		case b >= 0x30 && b <= 0x7e:
			f.reset() // two-byte escape (ESC 7, ESC c, ...)
		case b == '\r' || b == '\n':
			// Dangling ESC before a break: drop the ESC, keep the break.
			f.reset()
			return []byte{b}
		default:
			f.reset()
		}
	case telnetAnsiCsi:
		if b == '\r' || b == '\n' {
			f.reset()
			return []byte{b}
		}
		if b >= 0x40 && b <= 0x7e {
			seq := append([]byte(nil), f.buf...)
			f.reset()
			if b == 'm' && f.keepSGR { // SGR: the one control the terminal may see
				return seq
			}
		}
	case telnetAnsiOsc, telnetAnsiString:
		switch {
		case b == 0x07: // BEL terminator
			f.reset()
		case b == 0x1b:
			f.state = telnetAnsiStringEsc
		case b == '\r' || b == '\n':
			// Unterminated string: never eat a line break.
			f.reset()
			return []byte{b}
		}
	case telnetAnsiStringEsc:
		if b == '\\' { // ST terminator
			f.reset()
		} else {
			f.state = telnetAnsiString
		}
	case telnetAnsiIntermediate:
		if b >= 0x30 && b <= 0x7e {
			f.reset()
		}
	}
	return nil
}

// telnetFilterAnsiBytes re-derives the ASCII view of stored raw bytes. A
// settings change has to repaint content already on screen, and each entry
// is one logical line, so a fresh filter per entry is enough.
func telnetFilterAnsiBytes(raw []byte, keepSGR bool) []byte {
	f := telnetAnsiFilter{keepSGR: keepSGR}
	var out []byte
	for _, b := range raw {
		out = append(out, f.feed(b)...)
	}
	return out
}

func (m *telnetMonitorModel) refreshAnsiViews() {
	for i := range m.rxEntries {
		m.rxEntries[i].view = telnetFilterAnsiBytes(m.rxEntries[i].raw, m.ansiColor)
	}
}

func (m *telnetMonitorModel) appendRx(data []byte) {
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
		for _, vb := range m.ansiFilter.feed(b) {
			m.consumeRxByte(vb, budget)
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

// consumeRxByte groups one filtered byte into the current line. The raw and
// view buffers always flush together, so a mode switch re-renders the same
// entries instead of re-grouping them.
func (m *telnetMonitorModel) consumeRxByte(b byte, budget int) {
	// A byte after a bare CR that is not LF finishes the line the CR ended
	// (devices that use CR alone as a line break).
	if m.pendingCR {
		m.pendingCR = false
		if b != '\n' {
			m.flushRxLine()
		}
	}
	m.markPending()
	m.pendingBytes = append(m.pendingBytes, b)
	m.pendingView = append(m.pendingView, b)
	if b == '\n' {
		m.flushRxLine()
		return
	}
	if b == '\r' {
		m.pendingCR = true
		return
	}
	visible := telnetVisibleLen(m.pendingView)
	switch {
	case m.hexAscii:
		if visible >= 8 {
			m.flushRxLine()
		}
	case m.hexView:
		// Each byte paints as "XX "; raw bytes decide the width here so the
		// HEX pane still accounts for every byte on the wire.
		if len(m.pendingBytes)*3 >= budget {
			m.flushRxLine()
		}
	default:
		if visible >= budget {
			m.flushRxLine()
		}
	}
}

func (m *telnetMonitorModel) markPending() {
	if len(m.pendingBytes) == 0 {
		m.pendingAt = time.Now()
	}
}

// renderEntry renders one raw entry into display line(s) under the current
// view mode. RX entries may split into several rows (HEX+ASCII groups by 8
// bytes, HEX/ASCII wrap at the pane budget); TX echoes stay on one row.
func (m *telnetMonitorModel) renderEntry(e telnetRxEntry) []string {
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
			line := telnetHexAsciiChunk(e.raw[i:end], e.off+i)
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
			line := m.rxPrefix() + telnetHexText(e.raw[i:end])
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
	view := e.view
	if view == nil { // entries rendered before ANSI filtering existed
		view = e.raw
	}
	body := bytes.TrimRight(view, "\r\n")
	if telnetVisibleLen(body) == 0 {
		return nil
	}
	for _, seg := range telnetSplitRxBreaks(body) {
		for _, chunk := range telnetSplitVisible(seg, budget) {
			if telnetVisibleLen(chunk) == 0 {
				continue
			}
			text := telnetAsciiFold(chunk)
			line := m.rxPrefix() + text
			if m.showTime {
				line = e.at.Format("[15:04:05.000] ") + line
			}
			line = ansi.Truncate(strings.TrimRight(line, " "), m.rx.Width, "")
			// Each row is drawn by the surrounding TUI, so a colour left
			// open by the peer must be reset before the next row/border.
			if strings.Contains(line, "\x1b[") {
				line += "\x1b[0m"
			}
			out = append(out, line)
		}
	}
	return out
}

// telnetSplitRxBreaks cuts a body on CRLF, bare CR and bare LF. CRLF is consumed as
// one break so it never leaves an empty segment behind.
func telnetSplitRxBreaks(body []byte) [][]byte {
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
func (m *telnetMonitorModel) renderTxEntry(e telnetRxEntry) []string {
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
		if !telnetTxNeedsHex(body) {
			avail := m.rx.Width - ansi.StringWidth(stamp) - ansi.StringWidth(head)
			if avail < 3 {
				avail = 3
			}
			indent := strings.Repeat(" ", ansi.StringWidth(head))
			rows := telnetWrapTxText(m.txBody(body), avail)
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
			out = append(out, trunc(stamp+head+telnetHexText(raw[:end])))
		} else {
			out = append(out, trunc(stamp+indent+telnetHexText(raw[i:end])))
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

// telnetTxNeedsHex reports whether a payload has no readable text form at all —
// NUL bytes or bytes that are not valid UTF-8, which is what every binary
// frame (Modbus RTU, file contents) looks like. Control bytes alone are not
// enough: a typed CRLF must still echo as \r\n, not as ASCII codes.
func telnetTxNeedsHex(raw []byte) bool {
	if !utf8.Valid(raw) {
		return true
	}
	return bytes.IndexByte(raw, 0) >= 0
}

// telnetWrapTxText breaks an echo body into rows of at most width display cells,
// so a long command wraps instead of being cut off at the pane edge.
func telnetWrapTxText(body string, width int) []string {
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

// telnetSGRSeqLen returns the byte length of a complete SGR sequence at the
// start of raw, or 0 when there is none. The ANSI filter only lets SGR
// through, so this is enough to keep colour codes out of width accounting.
func telnetSGRSeqLen(raw []byte) int {
	if len(raw) < 3 || raw[0] != 0x1b || raw[1] != '[' {
		return 0
	}
	for i := 2; i < len(raw); i++ {
		c := raw[i]
		if c >= 0x40 && c <= 0x7e {
			if c == 'm' {
				return i + 1
			}
			return 0
		}
		if c < 0x20 || c > 0x3f {
			return 0
		}
	}
	return 0
}

// telnetSGRSeqLenString is the string form used by search/highlight.
func telnetSGRSeqLenString(s string) int {
	if len(s) < 3 || s[0] != 0x1b || s[1] != '[' {
		return 0
	}
	for i := 2; i < len(s); i++ {
		c := s[i]
		if c >= 0x40 && c <= 0x7e {
			if c == 'm' {
				return i + 1
			}
			return 0
		}
		if c < 0x20 || c > 0x3f {
			return 0
		}
	}
	return 0
}

// telnetVisibleLen counts only the bytes that occupy a display cell; SGR
// codes are zero-width and must not shorten a wrapped row.
func telnetVisibleLen(raw []byte) int {
	n := 0
	for i := 0; i < len(raw); {
		if seq := telnetSGRSeqLen(raw[i:]); seq > 0 {
			i += seq
			continue
		}
		n++
		i++
	}
	return n
}

// telnetSplitVisible cuts filtered bytes into chunks of at most n visible
// bytes without splitting an SGR sequence. Trailing colour codes belong to
// the next chunk, where they will actually paint something.
func telnetSplitVisible(raw []byte, n int) [][]byte {
	if n <= 0 {
		n = 1
	}
	var out [][]byte
	cur := make([]byte, 0, n+16)
	visible := 0
	flush := func() {
		out = append(out, append([]byte(nil), cur...))
		cur = cur[:0]
		visible = 0
	}
	for i := 0; i < len(raw); {
		if seq := telnetSGRSeqLen(raw[i:]); seq > 0 {
			if visible >= n {
				flush()
			}
			cur = append(cur, raw[i:i+seq]...)
			i += seq
			continue
		}
		cur = append(cur, raw[i])
		visible++
		i++
		if visible >= n {
			flush()
		}
	}
	if len(cur) > 0 {
		flush()
	}
	return out
}

// telnetAsciiFold maps non-printable bytes to dots so ambiguous glyphs can never
// drift the receive pane off its border on GBK ConHost. SGR colour codes are
// kept verbatim: they have zero display width and the terminal renders them.
func telnetAsciiFold(raw []byte) string {
	var sb strings.Builder
	for i := 0; i < len(raw); {
		if seq := telnetSGRSeqLen(raw[i:]); seq > 0 {
			sb.Write(raw[i : i+seq])
			i += seq
			continue
		}
		if raw[i] >= 0x20 && raw[i] <= 0x7e {
			sb.WriteByte(raw[i])
		} else {
			sb.WriteByte('.')
		}
		i++
	}
	return sb.String()
}

func telnetHexText(raw []byte) string {
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
func (m *telnetMonitorModel) txBody(raw []byte) string {
	return telnetTxText(raw)
}

// telnetTxText keeps readable text readable — including UTF-8 such as Chinese —
// and turns control bytes into \r \n \t or \xNN. Escaping instead of falling
// back to hex keeps one rule for every payload: an ASCII send always echoes
// as ASCII.
func telnetTxText(raw []byte) string {
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

// telnetHexAsciiChunk formats one 8-byte group in the offset|hex|ascii gutter.
func telnetHexAsciiChunk(chunk []byte, off int) string {
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

func (m *telnetMonitorModel) flushRxLine() {
	if len(m.pendingBytes) == 0 {
		return
	}
	if m.pendingAt.IsZero() {
		m.pendingAt = time.Now()
	}
	e := telnetRxEntry{
		raw:  append([]byte(nil), m.pendingBytes...),
		view: append([]byte(nil), m.pendingView...),
		at:   m.pendingAt,
		off:  m.rxOffset - len(m.pendingBytes),
	}
	m.pendingBytes = nil
	m.pendingView = nil
	m.pendingCR = false
	m.pendingAt = time.Time{}
	m.addEntry(e)
}

// addEntry stores the raw entry and its rendered lines, caps both at
// maxLines, then refreshes the pane.
func (m *telnetMonitorModel) addEntry(e telnetRxEntry) {
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
func (m *telnetMonitorModel) rebuildRx() {
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
// telnetSearchPlain removes SGR codes from a display line and remembers where
// each visible byte came from. Search must still find text that spans a
// colour boundary, while highlighting has to wrap the original (coded) row.
func telnetSearchPlain(line string) (string, []int) {
	var sb strings.Builder
	positions := make([]int, 0, len(line))
	for i := 0; i < len(line); {
		if seq := telnetSGRSeqLenString(line[i:]); seq > 0 {
			i += seq
			continue
		}
		sb.WriteByte(line[i])
		positions = append(positions, i)
		i++
	}
	return sb.String(), positions
}

func (m *telnetMonitorModel) highlightLines(lines []string) []string {
	q := m.searchQuery
	if q == "" {
		return lines
	}
	out := make([]string, len(lines))
	needle := strings.ToLower(q)
	for i, line := range lines {
		plain, positions := telnetSearchPlain(line)
		low := strings.ToLower(plain)
		idx := strings.Index(low, needle)
		if idx < 0 || idx+len(needle) > len(positions) {
			out[i] = line
			continue
		}
		var sb strings.Builder
		wirePos, plainPos := 0, 0
		for idx >= 0 {
			start := positions[idx]
			end := positions[idx+len(needle)-1] + 1
			sb.WriteString(line[wirePos:start]) // text before the match
			// Style the match via a zero-width ANSI wrapper.
			sb.WriteString("\x1b[7m" + line[start:end] + "\x1b[27m")
			wirePos = end
			plainPos = idx + len(needle)
			next := strings.Index(low[plainPos:], needle)
			if next < 0 {
				break
			}
			idx = plainPos + next
		}
		sb.WriteString(line[wirePos:])
		out[i] = sb.String()
	}
	return out
}

func (m *telnetMonitorModel) refreshRxContent() {
	lines := make([]string, 0, len(m.rxLines)+1)
	lines = append(lines, m.rxLines...)
	// Show a partial line immediately without committing it: a chunk may
	// end mid-line, and the next chunk should continue the same row.
	if len(m.pendingBytes) > 0 {
		at := m.pendingAt
		if at.IsZero() {
			at = time.Now()
		}
		e := telnetRxEntry{
			raw:  m.pendingBytes,
			view: m.pendingView,
			at:   at,
			off:  m.rxOffset - len(m.pendingBytes),
		}
		lines = append(lines, m.renderEntry(e)...)
	}
	m.searchMatches = nil
	if m.searchQuery != "" {
		needle := strings.ToLower(m.searchQuery)
		for i, line := range lines {
			plain, _ := telnetSearchPlain(line)
			if strings.Contains(strings.ToLower(plain), needle) {
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

func (m *telnetMonitorModel) closePort() {
	if m.closed {
		return
	}
	m.closed = true
	if m.logRecording {
		_ = m.flushRxLog()
		m.logRecording = false
	}
	if m.conn != nil {
		// Closing a net.Conn unblocks any pending Read with an error, so the
		// read loop winds down on its own; a net.Conn is safe to Close while
		// a Read or Write is in flight (unlike go.bug.st/serial, which is why
		// the serial assistant needs its ioMu and this one does not).
		_ = m.conn.Close()
	}
}

func (m *telnetMonitorModel) doneCmd(err error) tea.Cmd {
	m.closePort()
	return func() tea.Msg { return telnetMonitorDoneMsg{err: err} }
}

func (m *telnetMonitorModel) switchCmd() tea.Cmd {
	m.closePort()
	return func() tea.Msg { return telnetMonitorSwitchMsg{} }
}

// telnetReadCmd returns one self-renewing read: the closure loops on the
// connection with a short deadline, filters protocol bytes through the
// session, lets the auto-login watcher answer prompts, and only returns to
// Update when there is something to show (or the session ended) — so an idle
// link does not spam the event loop the way a per-timeout message would.
// Update renews the command after every telnetRxMsg until disconnect.
func telnetReadCmd(conn net.Conn, session *telnetclient.Session, auto *telnetclient.AutoLogin) tea.Cmd {
	if conn == nil {
		return nil
	}
	return func() tea.Msg {
		buf := make([]byte, telnetMonitorReadSize)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(telnetReadTimeout))
			n, err := conn.Read(buf)
			if n > 0 {
				msg := telnetRxMsg{data: session.Feed(buf[:n])}
				if auto != nil && len(msg.data) > 0 {
					// Observe writes the answer through the telnet session
					// (IAC-stuffed); the capture keeps a copy so Update can
					// echo it as TX.
					capture := &loginTxWriter{w: session}
					auto.Observe(msg.data, capture)
					msg.loginTx = capture.sent
				}
				return msg
			}
			if err != nil {
				// A fired deadline with no data is just the read timeout:
				// wait for more. Anything else (EOF, reset, closed) ends it.
				if errors.Is(err, os.ErrDeadlineExceeded) {
					continue
				}
				return telnetReadErrMsg{err: err}
			}
			// n == 0, err == nil: spurious wake-up; loop again.
		}
	}
}

// loginTxWriter forwards to the telnet session (so credentials are
// IAC-stuffed like any other outbound data) while remembering everything
// the auto-login watcher sent, so the assistant can echo it.
type loginTxWriter struct {
	w    io.Writer
	sent []byte
}

func (w *loginTxWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if n > 0 {
		w.sent = append(w.sent, p[:n]...)
	}
	return n, err
}

func telnetTickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return telnetTickMsg{} })
}
