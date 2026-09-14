package ui

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/ftpcred"
	"github.com/zsuroy/ctty/internal/i18n"
)

// Site manager for saved FTP sites (mirrors telnet/serial list pattern).
type ftpSitesModel struct {
	styles        Styles
	width         int
	height        int
	table         table.Model
	sites         []ftpconfig.FTPSite
	filtered      []ftpconfig.FTPSite
	searchInput   textinput.Model
	searchMode    bool
	deleteIdx     int
	confirmDel    bool
	ready         bool
	addForm       *ftpAddFormModel
	addMode       bool
	editMode      bool
	editOldName   string
	showInfo      bool
	infoSite      *ftpconfig.FTPSite
	infoScroll    int
	statusMessage string
	statusExpiry  time.Time

	tagPickerOpen   bool
	tagPickerCursor int
	selectedTag     string
	selectedSites   map[string]bool
	probing         bool
	probeStatus     map[string]bool
	latencies       map[string]time.Duration
}

func (m *ftpSitesModel) setStatus(msg string) {
	m.statusMessage = msg
	m.statusExpiry = time.Now().Add(3 * time.Second)
}

func (m *ftpSitesModel) statusActive() bool {
	return m.statusMessage != "" && time.Now().Before(m.statusExpiry)
}

type ftpProbeMsg struct {
	name     string
	up       bool
	duration time.Duration
}

func probeFTPSiteCmd(s ftpconfig.FTPSite) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		dur := time.Since(start)
		if err == nil {
			_ = conn.Close()
		}
		return ftpProbeMsg{name: s.Name, up: err == nil, duration: dur}
	}
}

func (m *ftpSitesModel) startProbeAllCmd() tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range m.sites {
		cmds = append(cmds, probeFTPSiteCmd(s))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *ftpSitesModel) startProbeSelectedCmd() tea.Cmd {
	if len(m.selectedSites) == 0 {
		return m.startProbeAllCmd()
	}
	var cmds []tea.Cmd
	for _, s := range m.sites {
		if m.selectedSites[s.Name] {
			cmds = append(cmds, probeFTPSiteCmd(s))
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *ftpSitesModel) isSiteSelected(name string) bool {
	return m.selectedSites != nil && m.selectedSites[name]
}

func (m *ftpSitesModel) toggleSiteSelected(name string) {
	if m.selectedSites == nil {
		m.selectedSites = make(map[string]bool)
	}
	if m.selectedSites[name] {
		delete(m.selectedSites, name)
	} else {
		m.selectedSites[name] = true
	}
}

func (m *ftpSitesModel) clearSelection() {
	m.selectedSites = make(map[string]bool)
}

func (m *ftpSitesModel) toggleSelectAllVisible() {
	if m.selectedSites == nil {
		m.selectedSites = make(map[string]bool)
	}
	if len(m.filtered) == 0 {
		return
	}
	allSelected := true
	for _, s := range m.filtered {
		if !m.selectedSites[s.Name] {
			allSelected = false
			break
		}
	}
	if allSelected {
		for _, s := range m.filtered {
			delete(m.selectedSites, s.Name)
		}
	} else {
		for _, s := range m.filtered {
			m.selectedSites[s.Name] = true
		}
	}
}

func (m *ftpSitesModel) getSelectedSites() []ftpconfig.FTPSite {
	if len(m.selectedSites) == 0 {
		return nil
	}
	var res []ftpconfig.FTPSite
	for _, s := range m.sites {
		if m.selectedSites[s.Name] {
			res = append(res, s)
		}
	}
	return res
}

func siteHasTag(site ftpconfig.FTPSite, targetTag string) bool {
	cleanTarget := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(targetTag), "#"))
	if cleanTarget == "" {
		return false
	}
	for _, t := range site.Tags {
		clean := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if clean == cleanTarget {
			return true
		}
	}
	return false
}

type ftpTagCountItem struct {
	tag   string
	count int
}

func (m *ftpSitesModel) getTagCounts() []ftpTagCountItem {
	counts := make(map[string]int)
	for _, s := range m.sites {
		for _, t := range s.Tags {
			clean := strings.TrimSpace(strings.TrimPrefix(t, "#"))
			if clean == "" {
				continue
			}
			counts[clean]++
		}
	}
	items := make([]ftpTagCountItem, 0, len(counts))
	for tag, count := range counts {
		items = append(items, ftpTagCountItem{tag: tag, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].tag < items[j].tag
	})
	return items
}

func (m *ftpSitesModel) siteDisplayName(s ftpconfig.FTPSite) string {
	prefix := "[ ] "
	if m.isSiteSelected(s.Name) {
		prefix = "[✓] "
	}
	name := s.Name
	if st, ok := m.probeStatus[s.Name]; ok {
		if st {
			if dur, exists := m.latencies[s.Name]; exists {
				ms := dur.Milliseconds()
				if ms < 50 {
					name = "🟢 " + name
				} else if ms < 150 {
					name = "🟡 " + name
				} else {
					name = "🟠 " + name
				}
			} else {
				name = "🟢 " + name
			}
		} else {
			name = "🔴 " + name
		}
	} else if m.probing {
		name = "🔵 " + name
	} else {
		name = "⚫ " + name
	}
	return prefix + name
}

func (m *ftpSitesModel) renderTagPicker() string {
	tagItems := m.getTagCounts()
	title := m.styles.FocusedLabel.Bold(true).Render("🏷️  " + i18n.T("tags.title"))
	var rows []string
	rows = append(rows, title, "")
	totalCount := len(m.sites)
	allCount := fmt.Sprintf("(%d)", totalCount)
	cursor0 := "  "
	if m.tagPickerCursor == 0 {
		cursor0 = "> "
	}
	activeMark0 := "○"
	if m.selectedTag == "" {
		activeMark0 = "●"
	}
	line0 := fmt.Sprintf("%s%s [%s]  %s", cursor0, activeMark0, i18n.T("tags.all"), allCount)
	if m.tagPickerCursor == 0 {
		line0 = m.styles.Selected.Render(line0)
	}
	rows = append(rows, line0)
	for i, item := range tagItems {
		idx := i + 1
		cursor := "  "
		if m.tagPickerCursor == idx {
			cursor = "> "
		}
		activeMark := "○"
		if m.selectedTag == item.tag {
			activeMark = "●"
		}
		shortcut := ""
		if idx <= 9 {
			shortcut = fmt.Sprintf("%d. ", idx)
		}
		tagFormatted := FormatColoredTags([]string{item.tag})
		line := fmt.Sprintf("%s%s %s%s (%d)", cursor, activeMark, shortcut, tagFormatted, item.count)
		if m.tagPickerCursor == idx {
			line = m.styles.Selected.Render(line)
		}
		rows = append(rows, line)
	}
	rows = append(rows, "", m.styles.HelpText.Render(i18n.T("tags.help")))
	return renderCardBox(m.styles.FormContainer, m.width, rows...)
}

type ftpOpenBrowserMsg struct {
	siteName string
}

// NewFTPSitesForm creates the FTP site list manager.
func NewFTPSitesForm(styles Styles, width, height int) *ftpSitesModel {
	m := &ftpSitesModel{
		styles: styles,
		width:  width,
		height: height,
	}
	m.searchInput = textinput.New()
	m.searchInput.Placeholder = i18n.T("search.placeholder")
	m.searchInput.CharLimit = 50
	m.searchInput.Width = searchInputWidth(width, i18n.T("search.prompt"))
	m.reload()
	m.ready = true
	return m
}

func (m *ftpSitesModel) Init() tea.Cmd { return nil }

func (m *ftpSitesModel) renderInfoView() string {
	s := m.infoSite
	user := s.User
	if user == "" {
		user = "anonymous"
	}
	pass := i18n.T("info.not_set")
	if _, ok := ftpcred.GetPassword(s.Name); ok {
		pass = i18n.T("info.password_saved")
	}
	tags := strings.Join(s.Tags, ", ")
	if tags == "" {
		tags = i18n.T("info.not_set")
	}

	titleText := m.styles.Header.Render(strings.TrimSpace(i18n.T("ftp.info_title", s.Name)))

	rows := [][2]string{
		{i18n.T("info.host_name") + ":", s.Name},
		{i18n.T("info.hostname_ip") + ":", s.Host},
		{i18n.T("info.port") + ":", fmt.Sprintf("%d", s.Port)},
		{i18n.T("info.user") + ":", user},
		{i18n.T("info.tags") + ":", tags},
		{i18n.T("info.password") + ":", pass},
	}

	maxLabelW := 0
	for _, r := range rows {
		if w := ansi.StringWidth(r[0]); w > maxLabelW {
			maxLabelW = w
		}
	}

	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.styles.Theme.Primary)

	var bodyLines []string
	for _, r := range rows {
		line := lipgloss.JoinHorizontal(
			lipgloss.Top,
			labelStyle.Render("  "+padDisplay(r[0], maxLabelW)),
			" ",
			r[1],
		)
		bodyLines = append(bodyLines, line)
	}

	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 24
	}

	boxWidth := m.width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	container := m.styles.FormContainer
	if totalHeight < 24 {
		container = container.Padding(0, 1)
	}

	innerW := boxWidth - container.GetHorizontalFrameSize()
	if innerW < 10 {
		innerW = 10
	}

	targetBoxH := totalHeight
	if totalHeight >= 14 {
		targetBoxH = totalHeight - 1
	}

	frameH := container.GetVerticalFrameSize()
	headerH := lipgloss.Height(titleText)
	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("ftp.info_help"))
	helpH := lipgloss.Height(helpText)

	overhead := frameH + headerH + helpH + 2
	viewportHeight := targetBoxH - overhead
	if viewportHeight < 3 {
		viewportHeight = 3
	}
	bodyLines = wrapInfoLines(bodyLines, innerW)
	visibleBody := scrollInfoWindow(bodyLines, viewportHeight, &m.infoScroll)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		titleText,
		"",
		visibleBody,
		"",
		helpText,
	)

	box := container.Width(boxWidth).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}

func (m *ftpSitesModel) reload() {
	sites, err := ftpconfig.Load()
	if err != nil {
		sites = nil
	}
	m.sites = sites
	m.applyFilter()
	m.rebuildTable()
}

func (m *ftpSitesModel) applyFilter() {
	var base []ftpconfig.FTPSite
	if m.selectedTag != "" {
		for _, s := range m.sites {
			if siteHasTag(s, m.selectedTag) {
				base = append(base, s)
			}
		}
	} else {
		base = m.sites
	}
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" {
		m.filtered = base
		return
	}
	words := strings.Fields(q)
	var out []ftpconfig.FTPSite
	for _, s := range base {
		hay := strings.ToLower(s.Name + " " + s.Host + " " + s.User + " " + strings.Join(s.Tags, " "))
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, s)
		}
	}
	m.filtered = out
}

func (m *ftpSitesModel) tableHeight() int {
	overhead := 8
	if m.height >= 18 && m.width >= 64 {
		overhead++
	}
	h := m.height - overhead
	if h < 5 {
		h = 5
	}
	return h
}

// plainColumnWidth sizes a column by its longest plain-text value,
// clamped to [minW, maxW]. Measured without ANSI so bubbles table
// truncation and padding stay exact.
func plainColumnWidth(sites []ftpconfig.FTPSite, minW, maxW int, cell func(ftpconfig.FTPSite) string) int {
	w := minW
	for _, s := range sites {
		if lw := ansi.StringWidth(cell(s)); lw > w {
			w = lw
		}
	}
	if w > maxW {
		w = maxW
	}
	return w
}

// colorizeSiteTags applies per-tag colors to an already-rendered table.
// Coloring after layout keeps ANSI bytes out of bubbles table width math
// (runewidth counts escapes as visible cells, which chops text early and
// can split sequences, breaking borders). A single alternation pass,
// longest-first with a boundary lookahead, so "#Min" never corrupts
// "#Minio" and truncated cells ("#Mi…") stay plain.
func colorizeSiteTags(view string, sites []ftpconfig.FTPSite) string {
	seen := map[string]bool{}
	var tags []string
	for _, s := range sites {
		for _, t := range s.Tags {
			clean := strings.TrimSpace(strings.TrimPrefix(t, "#"))
			if clean == "" || seen[clean] {
				continue
			}
			seen[clean] = true
			tags = append(tags, clean)
		}
	}
	if len(tags) == 0 {
		return view
	}
	sort.Slice(tags, func(i, j int) bool { return len(tags[i]) > len(tags[j]) })
	quoted := make([]string, len(tags))
	for i, t := range tags {
		quoted[i] = regexp.QuoteMeta(t)
	}
	// Boundary (space, box border, or end) is consumed and re-emitted so
	// adjacent tokens still match; truncated cells ("#Mi…") never match.
	re := regexp.MustCompile(`#(` + strings.Join(quoted, "|") + `)([\s│]|\z)`)
	var b strings.Builder
	prev := 0
	for _, m := range re.FindAllStringSubmatchIndex(view, -1) {
		b.WriteString(view[prev:m[0]])
		b.WriteString(FormatColoredTag(view[m[2]:m[3]]))
		b.WriteString(view[m[3]:m[1]])
		prev = m[1]
	}
	b.WriteString(view[prev:])
	return b.String()
}

// siteRows renders the filtered sites for the current column set.
func (m *ftpSitesModel) siteRows(narrow bool) []table.Row {
	rows := make([]table.Row, 0, len(m.filtered))
	for _, s := range m.filtered {
		user := s.User
		if user == "" {
			user = "anonymous"
		}
		displayName := m.siteDisplayName(s)
		if narrow {
			rows = append(rows, table.Row{
				displayName,
				fmt.Sprintf("%s@%s", user, s.Host),
				fmt.Sprintf("%d", s.Port),
			})
			continue
		}
		rows = append(rows, table.Row{
			displayName,
			fmt.Sprintf("%s@%s", user, s.Host),
			fmt.Sprintf("%d", s.Port),
			FormatPlainTags(s.Tags),
		})
	}
	return rows
}

func (m *ftpSitesModel) rebuildTable() {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.tableHeight()
	// Bubbles table renders each cell with Padding(0,1) = 2 extra cols per
	// cell; TableFocused adds border(2); App adds padding(2). So
	// rendered = colWidths + numCols*2 + 4, same convention as the serial and
	// telnet getColumns. Budget the column widths to fill the terminal exactly.
	portW := 6
	if w < 40 {
		portW = 4
	}
	if w < 74 {
		// Narrow terminals: drop the Tags column, keep Name/User@Host/Port.
		rem := w - 4 - 3*2 - portW
		if rem < 6 {
			rem = 6
		}
		hostW := rem * 2 / 5
		if hostW < 3 {
			hostW = 3
		}
		nameW := rem - hostW
		if nameW < 3 {
			nameW = 3
		}
		cols := []table.Column{
			{Title: i18n.T("table.col.name"), Width: nameW},
			{Title: i18n.T("table.col.user") + "@" + i18n.T("table.col.hostname"), Width: hostW},
			{Title: i18n.T("table.col.port"), Width: portW},
		}
		if m.table.Columns() == nil || len(m.table.Columns()) == 0 {
			m.table = table.New(table.WithColumns(cols), table.WithHeight(h), table.WithFocused(true))
		} else {
			// Drain rows first: bubbles renderRow indexes columns by row
			// length, so swapping 3/4-column layouts on a populated
			// table panics. SetRows(nil) renders nothing, making the
			// column swap safe in both directions.
			m.table.SetRows(nil)
			m.table.SetColumns(cols)
			m.table.SetHeight(h)
		}
		m.table.SetRows(m.siteRows(true))
		m.clampTableCursor()
		return
	}
	rem := w - 4 - 4*2 - portW
	if rem < 12 {
		rem = 12
	}
	// Content-based widths (SSH-style): short values don't starve Tags.
	nameW := plainColumnWidth(m.filtered, 15, 31, func(s ftpconfig.FTPSite) string { return "[ ] ⚫ " + s.Name })
	hostW := plainColumnWidth(m.filtered, 12, 32, func(s ftpconfig.FTPSite) string {
		user := s.User
		if user == "" {
			user = "anonymous"
		}
		return user + "@" + s.Host
	})
	tagsW := rem - nameW - hostW
	if tagsW < 8 {
		tagsW = 8
		hostW = rem - nameW - tagsW
		if hostW < 12 {
			hostW = 12
			nameW = rem - hostW - tagsW
		}
	}
	cols := []table.Column{
		{Title: i18n.T("table.col.name"), Width: nameW},
		{Title: i18n.T("table.col.user") + "@" + i18n.T("table.col.hostname"), Width: hostW},
		{Title: i18n.T("table.col.port"), Width: portW},
		{Title: i18n.T("table.col.tags"), Width: tagsW},
	}
	if m.table.Columns() == nil || len(m.table.Columns()) == 0 {
		m.table = table.New(table.WithColumns(cols), table.WithHeight(h), table.WithFocused(true))
	} else {
		m.table.SetRows(nil)
		m.table.SetColumns(cols)
		m.table.SetHeight(h)
	}
	m.table.SetRows(m.siteRows(false))
	m.clampTableCursor()
}

func (m *ftpSitesModel) clampTableCursor() {
	count := len(m.filtered)
	if count == 0 || (m.table.Cursor() >= 0 && m.table.Cursor() < count) {
		return
	}
	m.table.SetCursor(0)
}

func (m *ftpSitesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
		m.rebuildTable()
		if m.addForm != nil {
			updated, cmd := m.addForm.Update(msg)
			if fm, ok := updated.(*ftpAddFormModel); ok {
				m.addForm = fm
			}
			return m, cmd
		}
		return m, nil
	case ftpProbeMsg:
		if m.probeStatus == nil {
			m.probeStatus = make(map[string]bool)
		}
		if m.latencies == nil {
			m.latencies = make(map[string]time.Duration)
		}
		m.probeStatus[msg.name] = msg.up
		m.latencies[msg.name] = msg.duration
		if len(m.probeStatus) >= len(m.sites) {
			m.probing = false
		} else if len(m.selectedSites) > 0 {
			done := true
			for name := range m.selectedSites {
				if _, ok := m.probeStatus[name]; !ok {
					done = false
					break
				}
			}
			if done {
				m.probing = false
			}
		}
		m.rebuildTable()
		return m, nil
	case tea.KeyMsg:
		if m.addMode || m.editMode {
			return m.handleAddKeys(msg)
		}
		if m.showInfo {
			switch msg.String() {
			case "esc", "i", "q":
				m.showInfo = false
				m.infoSite = nil
				return m, nil
			case "up", "k", "down", "j":
				scrollInfoKey(msg.String(), &m.infoScroll)
				return m, nil
			case "e", "enter":
				if m.infoSite != nil {
					site := *m.infoSite
					m.showInfo = false
					m.infoSite = nil
					m.startEdit(site)
				}
				return m, nil
			}
			return m, nil
		}
		if m.confirmDel {
			return m.handleDeleteConfirm(msg)
		}
		if m.tagPickerOpen {
			switch msg.String() {
			case "esc", "q":
				m.tagPickerOpen = false
				return m, nil
			case "up", "k":
				if m.tagPickerCursor > 0 {
					m.tagPickerCursor--
				}
				return m, nil
			case "down", "j":
				tagItems := m.getTagCounts()
				if m.tagPickerCursor < len(tagItems) {
					m.tagPickerCursor++
				}
				return m, nil
			case "c":
				m.selectedTag = ""
				m.tagPickerOpen = false
				m.applyFilter()
				m.rebuildTable()
				m.setStatus(i18n.T("tags.cleared"))
				return m, nil
			case "enter":
				tagItems := m.getTagCounts()
				if m.tagPickerCursor == 0 {
					m.selectedTag = ""
				} else if m.tagPickerCursor-1 < len(tagItems) {
					m.selectedTag = tagItems[m.tagPickerCursor-1].tag
				}
				m.tagPickerOpen = false
				m.applyFilter()
				m.rebuildTable()
				return m, nil
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				tagItems := m.getTagCounts()
				idx := int(msg.String()[0] - '1')
				if idx < len(tagItems) {
					m.selectedTag = tagItems[idx].tag
					m.tagPickerOpen = false
					m.applyFilter()
					m.rebuildTable()
					return m, nil
				}
			}
			return m, nil
		}
		if m.searchMode {
			return m.handleSearch(msg)
		}
		return m.handleListKeys(msg)
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *ftpSitesModel) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "q", "ctrl+c":
		if len(m.selectedSites) > 0 {
			m.clearSelection()
			m.rebuildTable()
			m.setStatus(i18n.T("main.selection_cleared"))
			return m, nil
		}
		if m.selectedTag != "" {
			m.selectedTag = ""
			m.applyFilter()
			m.rebuildTable()
			m.setStatus(i18n.T("tags.cleared"))
			return m, nil
		}
		return m, func() tea.Msg { return ftpDoneMsg{} }
	case "t":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewSerial} }
	case "T":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewTelnet} }
	case "b", "]":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewLocalBrowser} }
	case "[":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewTelnet} }
	case "g", "home":
		m.table.SetCursor(0)
		return m, nil
	case "G", "end":
		if len(m.filtered) > 0 {
			m.table.SetCursor(len(m.filtered) - 1)
		}
		return m, nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key[0] - '1')
		if idx < len(m.filtered) {
			m.table.SetCursor(idx)
		}
		return m, nil
	case " ":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx < 0 || idx >= len(m.filtered) {
			return m, nil
		}
		m.toggleSiteSelected(m.filtered[idx].Name)
		if idx < len(m.filtered)-1 {
			m.table.SetCursor(idx + 1)
		}
		m.rebuildTable()
		return m, nil
	case "ctrl+a":
		m.toggleSelectAllVisible()
		m.rebuildTable()
		return m, nil
	case "enter":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx < 0 || idx >= len(m.filtered) {
			return m, nil
		}
		name := m.filtered[idx].Name
		return m, func() tea.Msg { return ftpOpenBrowserMsg{siteName: name} }
	case "/", "ctrl+f":
		m.searchMode = true
		m.table.Blur()
		m.searchInput.Focus()
		return m, textinput.Blink
	case "tab":
		m.searchMode = true
		m.table.Blur()
		m.searchInput.Focus()
		return m, textinput.Blink
	case "w":
		tagItems := m.getTagCounts()
		if len(tagItems) == 0 {
			m.setStatus(i18n.T("tags.none"))
			return m, nil
		}
		m.tagPickerOpen = true
		m.tagPickerCursor = 0
		for i, it := range tagItems {
			if it.tag == m.selectedTag {
				m.tagPickerCursor = i + 1
				break
			}
		}
		return m, nil
	case "c":
		if m.selectedTag != "" {
			m.selectedTag = ""
			m.applyFilter()
			m.rebuildTable()
			m.setStatus(i18n.T("tags.cleared"))
			return m, nil
		}
		return m, nil
	case "a":
		m.startAdd()
		return m, nil
	case "e":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx < 0 || idx >= len(m.filtered) {
			return m, nil
		}
		m.startEdit(m.filtered[idx])
		return m, nil
	case "d", "x":
		if len(m.selectedSites) > 0 {
			m.deleteIdx = m.table.Cursor()
			m.confirmDel = true
			return m, nil
		}
		if len(m.filtered) == 0 {
			return m, nil
		}
		m.deleteIdx = m.table.Cursor()
		m.confirmDel = true
		return m, nil
	case "y":
		if len(m.selectedSites) > 0 {
			var cmds []string
			for _, s := range m.getSelectedSites() {
				cmds = append(cmds, FormatFTPCommand(s))
			}
			joined := strings.Join(cmds, "\n")
			copyToClipboard(joined)
			m.setStatus(fmt.Sprintf(i18n.T("main.copied"), fmt.Sprintf("%d sites", len(cmds))))
			return m, nil
		}
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx < 0 || idx >= len(m.filtered) {
			return m, nil
		}
		cmdStr := FormatFTPCommand(m.filtered[idx])
		copyToClipboard(cmdStr)
		m.setStatus(fmt.Sprintf(i18n.T("main.copied"), cmdStr))
		return m, nil
	case "p":
		if m.probing {
			return m, nil
		}
		if len(m.selectedSites) > 0 {
			m.probing = true
			m.probeStatus = make(map[string]bool)
			m.latencies = make(map[string]time.Duration)
			m.rebuildTable()
			m.setStatus(fmt.Sprintf(i18n.T("main.ping_selected"), len(m.selectedSites)))
			return m, m.startProbeSelectedCmd()
		}
		if len(m.sites) == 0 {
			return m, nil
		}
		m.probing = true
		m.probeStatus = make(map[string]bool)
		m.latencies = make(map[string]time.Duration)
		m.rebuildTable()
		m.setStatus("Probing all sites...")
		return m, m.startProbeAllCmd()
	case "i":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx < 0 || idx >= len(m.filtered) {
			return m, nil
		}
		site := m.filtered[idx]
		m.infoSite = &site
		m.showInfo = true
		m.infoScroll = 0
		return m, nil
	case "r":
		m.reload()
		return m, nil
	default:
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
}

func (m *ftpSitesModel) handleSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchInput.Blur()
		m.table.Focus()
		return m, nil
	case "enter", "tab":
		m.searchMode = false
		m.searchInput.Blur()
		m.table.Focus()
		return m, nil
	}
	var cmd tea.Cmd
	oldValue := m.searchInput.Value()
	m.searchInput, cmd = m.searchInput.Update(msg)
	if m.searchInput.Value() != oldValue {
		m.applyFilter()
		m.rebuildTable()
	}
	return m, cmd
}

func (m *ftpSitesModel) handleDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		if len(m.selectedSites) > 0 {
			for name := range m.selectedSites {
				_ = ftpconfig.Delete(name)
				_ = ftpcred.DeletePassword(name)
			}
			m.clearSelection()
			m.reload()
		} else if m.deleteIdx >= 0 && m.deleteIdx < len(m.filtered) {
			name := m.filtered[m.deleteIdx].Name
			_ = ftpconfig.Delete(name)
			_ = ftpcred.DeletePassword(name)
			m.reload()
		}
		m.confirmDel = false
		return m, nil
	case "n", "N", "esc":
		m.confirmDel = false
		return m, nil
	}
	return m, nil
}

func (m *ftpSitesModel) startAdd() {
	m.addForm = newFTPAddForm(m.styles, m.width, m.height, nil)
	m.addMode = true
	m.editMode = false
	m.editOldName = ""
}

func (m *ftpSitesModel) startEdit(site ftpconfig.FTPSite) {
	m.addForm = newFTPAddForm(m.styles, m.width, m.height, &site)
	m.addMode = false
	m.editMode = true
	m.editOldName = site.Name
}

func (m *ftpSitesModel) handleAddKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.addForm == nil {
		m.addMode = false
		m.editMode = false
		return m, nil
	}
	sub, cmd := m.addForm.Update(msg)
	if fm, ok := sub.(*ftpAddFormModel); ok {
		m.addForm = fm
	}
	if m.addForm.done {
		m.addMode = false
		m.editMode = false
		m.editOldName = ""
		m.addForm = nil
		m.reload()
		return m, nil
	}
	if m.addForm.cancelled {
		m.addMode = false
		m.editMode = false
		m.editOldName = ""
		m.addForm = nil
		return m, nil
	}
	return m, cmd
}

func (m *ftpSitesModel) View() string {
	if m.showInfo && m.infoSite != nil {
		return m.renderInfoView()
	}
	if m.addForm != nil {
		return m.addForm.View()
	}

	var components []string
	components = append(components, m.styles.Header.Render(" "+i18n.T("ftp.sites_title")+" "))

	if m.height >= 18 {
		if tabs := renderProtocolTabs(m.styles, "ftp", len(m.filtered), m.width); tabs != "" {
			components = append(components, tabs)
		}
	}

	if m.selectedTag != "" {
		tagBannerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
		bannerText := fmt.Sprintf(i18n.T("tags.active_banner"), m.selectedTag, len(m.filtered))
		components = append(components, tagBannerStyle.Render(bannerText))
	}

	searchPrompt := i18n.T("search.prompt")
	searchContent := searchPrompt + m.searchInput.View()
	searchMaxW := searchMaxWidth(m.width)
	if m.width >= 50 && len(m.sites) > 0 {
		var badge string
		if len(m.selectedSites) > 0 {
			badge = fmt.Sprintf("[%s]", fmt.Sprintf(i18n.T("main.selected_count"), len(m.selectedSites)))
		} else if m.searchInput.Value() != "" {
			badge = fmt.Sprintf("[%d/%d %s]", len(m.filtered), len(m.sites), i18n.T("search.matched"))
		} else {
			cursor := m.table.Cursor() + 1
			if cursor > len(m.sites) {
				cursor = len(m.sites)
			}
			if len(m.filtered) != len(m.sites) {
				cursor = m.table.Cursor() + 1
				if cursor > len(m.filtered) {
					cursor = len(m.filtered)
				}
				badge = fmt.Sprintf("[%d/%d]", cursor, len(m.filtered))
			} else {
				badge = fmt.Sprintf("[%d/%d]", cursor, len(m.sites))
			}
		}
		gap := searchMaxW - ansi.StringWidth(searchContent) - ansi.StringWidth(badge)
		if gap >= 2 {
			searchContent = searchContent + strings.Repeat(" ", gap) + badge
		}
	}
	searchContent = ansi.Truncate(searchContent, searchMaxW, "")
	if m.searchMode {
		components = append(components, m.styles.SearchFocused.Render(searchContent))
	} else {
		components = append(components, m.styles.SearchUnfocused.Render(searchContent))
	}
	tableStyle := m.styles.TableFocused
	if m.searchMode {
		tableStyle = m.styles.TableUnfocused
	}
	components = append(components, colorizeSiteTags(tableStyle.Render(m.table.View()), m.filtered))
	if m.statusActive() {
		components = append(components, renderStatusToast(m.statusMessage))
	}
	helpKey := "ftp.sites_help"
	if len(m.selectedSites) > 0 {
		helpKey = "ftp.sites_help"
	}
	components = append(components, renderHelpText(m.styles, i18n.T(helpKey)+" • Space: select • Ctrl+A: all • w: tags • p: probe", m.width))
	base := m.styles.App.Render(lipgloss.JoinVertical(lipgloss.Left, components...))
	if m.tagPickerOpen {
		return renderConfirmModal(m.width, m.height, m.renderTagPicker())
	}
	if m.confirmDel {
		if len(m.selectedSites) > 0 {
			box := renderConfirmBox(m.styles, m.width,
				m.styles.ErrorText.Render(i18n.T("delete.title")),
				i18n.T("ftp.sites_delete_batch_confirm", len(m.selectedSites)),
				i18n.T("delete.warning"),
				m.styles.HelpText.Render(i18n.T("delete.help")),
			)
			return renderConfirmModal(m.width, m.height, box)
		}
		if m.deleteIdx >= 0 && m.deleteIdx < len(m.filtered) {
			name := m.filtered[m.deleteIdx].Name
			box := renderConfirmBox(m.styles, m.width,
				m.styles.ErrorText.Render(i18n.T("delete.title")),
				i18n.T("ftp.sites_delete_confirm", name),
				i18n.T("delete.warning"),
				m.styles.HelpText.Render(i18n.T("delete.help")),
			)
			return renderConfirmModal(m.width, m.height, box)
		}
	}
	return base
}
