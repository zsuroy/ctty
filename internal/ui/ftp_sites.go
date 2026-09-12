package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

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
	styles      Styles
	width       int
	height      int
	table       table.Model
	sites       []ftpconfig.FTPSite
	filtered    []ftpconfig.FTPSite
	searchInput textinput.Model
	searchMode  bool
	deleteIdx   int
	confirmDel  bool
	ready       bool
	addMode     bool
	editMode    bool
	editOldName string
	addFields   []textinput.Model
	addFocus    int
	addErr      string
	showInfo    bool
	infoSite    *ftpconfig.FTPSite
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
	var b strings.Builder
	b.WriteString(m.styles.FormTitle.Render(" "+i18n.T("ftp.info_title", s.Name)+" ") + "\n\n")
	rows := [][2]string{
		{i18n.T("info.host_name"), s.Name},
		{i18n.T("info.hostname_ip"), s.Host},
		{i18n.T("info.port"), fmt.Sprintf("%d", s.Port)},
		{i18n.T("info.user"), user},
		{i18n.T("info.tags"), tags},
		{i18n.T("info.password"), pass},
	}
	for _, r := range rows {
		label := "  " + r[0] + ":"
		if w := ansi.StringWidth(label); w < 16 {
			label += strings.Repeat(" ", 16-w)
		}
		b.WriteString(m.styles.FormField.Render(label) + " " + r[1] + "\n")
	}
	b.WriteString("\n" + m.styles.HelpText.Render("  "+i18n.T("ftp.info_help")))
	return renderFormPage(m.styles, m.width, b.String())
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
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" {
		m.filtered = m.sites
		return
	}
	words := strings.Fields(q)
	var out []ftpconfig.FTPSite
	for _, s := range m.sites {
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
	h := m.height - 8
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
		if narrow {
			rows = append(rows, table.Row{
				s.Name,
				fmt.Sprintf("%s@%s", user, s.Host),
				fmt.Sprintf("%d", s.Port),
			})
			continue
		}
		rows = append(rows, table.Row{
			s.Name,
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
		hostW := rem * 3 / 5
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
	nameW := plainColumnWidth(m.filtered, 8, 24, func(s ftpconfig.FTPSite) string { return s.Name })
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
		m.table.SetColumns(cols)
		m.table.SetHeight(h)
	}
	m.table.SetRows(m.siteRows(false))
	m.clampTableCursor()
}

func (m *ftpSitesModel) clampTableCursor() {
	count := len(m.filtered)
	if count == 0 || m.table.Cursor() < count {
		return
	}
	m.table.SetCursor(0)
}

func (m *ftpSitesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
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
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		return m, func() tea.Msg { return ftpDoneMsg{} }
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
		if len(m.filtered) == 0 {
			return m, nil
		}
		m.deleteIdx = m.table.Cursor()
		m.confirmDel = true
		return m, nil
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
	case "y", "Y":
		if m.deleteIdx >= 0 && m.deleteIdx < len(m.filtered) {
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
	m.addMode = true
	m.editMode = false
	m.editOldName = ""
	m.addErr = ""
	m.addFocus = 0
	m.initFields(ftpconfig.DefaultSite(), "")
}

func (m *ftpSitesModel) startEdit(site ftpconfig.FTPSite) {
	m.addMode = false
	m.editMode = true
	m.editOldName = site.Name
	m.addErr = ""
	m.addFocus = 0
	pass, _ := ftpcred.GetPassword(site.Name)
	m.initFields(site, pass)
}

func (m *ftpSitesModel) initFields(site ftpconfig.FTPSite, password string) {
	labels := []string{i18n.T("ftp.field_name"), i18n.T("ftp.field_host"), i18n.T("ftp.field_port"), i18n.T("ftp.field_user"), i18n.T("ftp.field_password"), i18n.T("ftp.field_tags")}
	m.addFields = make([]textinput.Model, len(labels))
	for i, label := range labels {
		ti := textinput.New()
		ti.Placeholder = label
		ti.CharLimit = 128
		ti.Width = 40
		m.addFields[i] = ti
	}
	m.addFields[0].SetValue(site.Name)
	m.addFields[1].SetValue(site.Host)
	port := site.Port
	if port <= 0 {
		port = ftpconfig.DefaultPort
	}
	m.addFields[2].SetValue(fmt.Sprintf("%d", port))
	user := site.User
	if user == "" {
		user = "anonymous"
	}
	m.addFields[3].SetValue(user)
	m.addFields[4].EchoMode = textinput.EchoPassword
	m.addFields[4].EchoCharacter = '*'
	m.addFields[4].SetValue(password)
	m.addFields[5].SetValue(strings.Join(site.Tags, ", "))
	m.addFields[0].Focus()
}

func (m *ftpSitesModel) handleAddKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.addMode = false
		m.editMode = false
		m.editOldName = ""
		m.addErr = ""
		return m, nil
	case "tab", "down":
		m.addFields[m.addFocus].Blur()
		m.addFocus = (m.addFocus + 1) % len(m.addFields)
		m.addFields[m.addFocus].Focus()
		return m, nil
	case "shift+tab", "up":
		m.addFields[m.addFocus].Blur()
		m.addFocus = (m.addFocus - 1 + len(m.addFields)) % len(m.addFields)
		m.addFields[m.addFocus].Focus()
		return m, nil
	case "enter", "ctrl+s":
		name := strings.TrimSpace(m.addFields[0].Value())
		host := strings.TrimSpace(m.addFields[1].Value())
		portStr := strings.TrimSpace(m.addFields[2].Value())
		user := strings.TrimSpace(m.addFields[3].Value())
		pass := m.addFields[4].Value()
		var tags []string
		for _, tg := range strings.Split(m.addFields[5].Value(), ",") {
			if tg = strings.TrimSpace(tg); tg != "" {
				tags = append(tags, tg)
			}
		}
		if name == "" || host == "" {
			m.addErr = i18n.T("ftp.err_name_host_req")
			return m, nil
		}
		port := ftpconfig.DefaultPort
		if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port <= 0 {
			port = ftpconfig.DefaultPort
		}
		site := ftpconfig.FTPSite{Name: name, Host: host, Port: port, User: user, Tags: tags}
		var err error
		if m.editMode {
			err = ftpconfig.Update(m.editOldName, site)
			if err == nil && m.editOldName != name {
				_ = ftpcred.DeletePassword(m.editOldName)
			}
		} else {
			err = ftpconfig.Add(site)
		}
		if err != nil {
			m.addErr = err.Error()
			return m, nil
		}
		if pass != "" {
			_ = ftpcred.SetPassword(name, pass)
		} else if m.editMode {
			_ = ftpcred.DeletePassword(name)
		}
		m.addMode = false
		m.editMode = false
		m.editOldName = ""
		m.reload()
		return m, nil
	}
	var cmd tea.Cmd
	m.addFields[m.addFocus], cmd = m.addFields[m.addFocus].Update(msg)
	return m, cmd
}

func (m *ftpSitesModel) View() string {
	if m.showInfo && m.infoSite != nil {
		return m.renderInfoView()
	}
	if m.addMode || m.editMode {
		var b strings.Builder
		title := i18n.T("ftp.sites_add_title")
		if m.editMode {
			title = i18n.T("ftp.sites_edit_title")
		}
		b.WriteString(m.styles.FormTitle.Render(" "+title+" ") + "\n\n")
		labels := []string{i18n.T("ftp.field_name"), i18n.T("ftp.field_host"), i18n.T("ftp.field_port"), i18n.T("ftp.field_user"), i18n.T("ftp.field_password"), i18n.T("ftp.field_tags")}
		for i, ti := range m.addFields {
			label := "  " + labels[i] + ":"
			if w := ansi.StringWidth(label); w < 18 {
				label += strings.Repeat(" ", 18-w)
			}
			b.WriteString(label + " " + ti.View() + "\n")
		}
		if m.addErr != "" {
			b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.addErr) + "\n")
			b.WriteString("  " + m.styles.HelpText.Render(i18n.T("ftp.sites_form_err_hint")) + "\n")
		}
		b.WriteString("\n" + m.styles.HelpText.Render("  "+i18n.T("ftp.sites_form_help")))
		b.WriteString("\n" + m.styles.HelpText.Render("  "+i18n.T("ftp.cred_hint")))
		return renderFormPage(m.styles, m.width, b.String())
	}

	var components []string
	components = append(components, m.styles.Header.Render(" "+i18n.T("ftp.sites_title")+" "))
	components = append(components, renderSearchBar(m.styles, m.searchMode, i18n.T("search.prompt"), m.searchInput.View(), m.width))
	tableStyle := m.styles.TableFocused
	if m.searchMode {
		tableStyle = m.styles.TableUnfocused
	}
	components = append(components, colorizeSiteTags(tableStyle.Render(m.table.View()), m.filtered))
	if m.confirmDel && m.deleteIdx >= 0 && m.deleteIdx < len(m.filtered) {
		name := m.filtered[m.deleteIdx].Name
		components = append(components, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(
			i18n.T("ftp.sites_delete_confirm", name)))
	}
	components = append(components, renderHelpText(m.styles, i18n.T("ftp.sites_help"), m.width))
	return m.styles.App.Render(lipgloss.JoinVertical(lipgloss.Left, components...))
}
