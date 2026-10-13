package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/s3client"
	"github.com/zsuroy/ctty/internal/s3config"
	"github.com/zsuroy/ctty/internal/s3cred"
)

type s3SitesDoneMsg struct{}

type s3ShowHelpMsg struct{}

type s3OpenBrowserMsg struct {
	siteName string
}

type s3ProbeMsg struct {
	name     string
	up       bool
	duration time.Duration
}

func probeS3SiteCmd(s s3config.S3Site) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		err := s3client.Probe(s, 3*time.Second)
		dur := time.Since(start)
		return s3ProbeMsg{name: s.Name, up: err == nil, duration: dur}
	}
}

// Site manager for saved S3 sites.
type s3SitesModel struct {
	styles        Styles
	width         int
	height        int
	table         table.Model
	sites         []s3config.S3Site
	filtered      []s3config.S3Site
	searchInput   textinput.Model
	searchMode    bool
	deleteIdx     int
	confirmDel    bool
	ready         bool
	addForm       *s3AddFormModel
	addMode       bool
	editMode      bool
	editOldName   string
	showInfo      bool
	infoSite      *s3config.S3Site
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

func (m *s3SitesModel) setStatus(msg string) {
	m.statusMessage = msg
	m.statusExpiry = time.Now().Add(3 * time.Second)
}

func (m *s3SitesModel) statusActive() bool {
	return m.statusMessage != "" && time.Now().Before(m.statusExpiry)
}

func (m *s3SitesModel) startProbeAllCmd() tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range m.sites {
		cmds = append(cmds, probeS3SiteCmd(s))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *s3SitesModel) startProbeSelectedCmd() tea.Cmd {
	if len(m.selectedSites) == 0 {
		return m.startProbeAllCmd()
	}
	var cmds []tea.Cmd
	for _, s := range m.sites {
		if m.selectedSites[s.Name] {
			cmds = append(cmds, probeS3SiteCmd(s))
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *s3SitesModel) isSiteSelected(name string) bool {
	return m.selectedSites != nil && m.selectedSites[name]
}

func (m *s3SitesModel) toggleSiteSelected(name string) {
	if m.selectedSites == nil {
		m.selectedSites = make(map[string]bool)
	}
	if m.selectedSites[name] {
		delete(m.selectedSites, name)
	} else {
		m.selectedSites[name] = true
	}
}

func (m *s3SitesModel) clearSelection() {
	m.selectedSites = make(map[string]bool)
}

func (m *s3SitesModel) toggleSelectAllVisible() {
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

func (m *s3SitesModel) getSelectedSites() []s3config.S3Site {
	if len(m.selectedSites) == 0 {
		return nil
	}
	var res []s3config.S3Site
	for _, s := range m.sites {
		if m.selectedSites[s.Name] {
			res = append(res, s)
		}
	}
	return res
}

func s3SiteHasTag(site s3config.S3Site, targetTag string) bool {
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

type s3TagCountItem struct {
	tag   string
	count int
}

func (m *s3SitesModel) getTagCounts() []s3TagCountItem {
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
	items := make([]s3TagCountItem, 0, len(counts))
	for tag, count := range counts {
		items = append(items, s3TagCountItem{tag: tag, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].tag < items[j].tag
	})
	return items
}

func (m *s3SitesModel) siteDisplayName(s s3config.S3Site) string {
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

func (m *s3SitesModel) renderTagPicker() string {
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
	label0 := i18n.T("tags.all_hosts")
	if m.selectedTag == "" {
		label0 = "● " + label0
	} else {
		label0 = "  " + label0
	}
	row0 := fmt.Sprintf("%s%-14s %s", cursor0, label0, allCount)
	if m.tagPickerCursor == 0 {
		row0 = m.styles.Selected.Render(row0)
	}
	rows = append(rows, row0)

	for i, item := range tagItems {
		idx := i + 1
		cursor := "  "
		if m.tagPickerCursor == idx {
			cursor = "> "
		}
		activeMark := " "
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

// NewS3SitesForm creates a site manager for S3 sites.
func NewS3SitesForm(styles Styles, width, height int) *s3SitesModel {
	m := &s3SitesModel{
		styles:        styles,
		width:         width,
		height:        height,
		deleteIdx:     -1,
		selectedSites: make(map[string]bool),
		probeStatus:   make(map[string]bool),
		latencies:     make(map[string]time.Duration),
	}
	m.searchInput = textinput.New()
	m.searchInput.Placeholder = i18n.T("search.placeholder")
	m.searchInput.CharLimit = 50
	m.searchInput.Width = searchInputWidth(width, i18n.T("search.prompt"))
	m.reload()
	m.ready = true
	return m
}

func (m *s3SitesModel) Init() tea.Cmd { return nil }

func (m *s3SitesModel) renderInfoView() string {
	s := m.infoSite
	if s == nil {
		return ""
	}

	bucket := s.Bucket
	if bucket == "" {
		bucket = i18n.T("info.not_set")
	}
	region := s.Region
	if region == "" {
		region = i18n.T("info.not_set")
	}
	ak := s.AccessKey
	if ak == "" {
		ak = i18n.T("info.not_set")
	}
	sk := i18n.T("info.not_set")
	if _, ok := s3cred.GetSecretKey(s.Name); ok {
		sk = i18n.T("info.password_saved")
	}
	ssl := i18n.T("common.no")
	if s.UseSSL {
		ssl = i18n.T("common.yes")
	}
	pathStyle := i18n.T("common.no")
	if s.PathStyle {
		pathStyle = i18n.T("common.yes")
	}
	insecure := i18n.T("common.no")
	if s.InsecureTLS {
		insecure = i18n.T("common.yes")
	}
	tags := strings.Join(s.Tags, ", ")
	if tags == "" {
		tags = i18n.T("info.not_set")
	}

	titleText := m.styles.Header.Render(strings.TrimSpace(fmt.Sprintf(i18n.T("s3.info_title"), s.Name)))

	rows := [][2]string{
		{i18n.T("info.host_name") + ":", s.Name},
		{i18n.T("s3.field_endpoint") + ":", s.Endpoint},
		{i18n.T("s3.field_bucket") + ":", bucket},
		{i18n.T("s3.field_region") + ":", region},
		{i18n.T("s3.field_access_key") + ":", ak},
		{i18n.T("s3.field_secret_key") + ":", sk},
		{i18n.T("s3.field_use_ssl") + ":", ssl},
		{i18n.T("s3.field_path_style") + ":", pathStyle},
		{i18n.T("s3.field_insecure_tls") + ":", insecure},
		{i18n.T("info.tags") + ":", tags},
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
	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("s3.info_help"))
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

func (m *s3SitesModel) reload() {
	sites, err := s3config.Load()
	if err != nil {
		sites = nil
	}
	m.sites = sites
	m.applyFilter()
	m.rebuildTable()
}

func (m *s3SitesModel) applyFilter() {
	var base []s3config.S3Site
	if m.selectedTag == "" {
		base = m.sites
	} else {
		for _, s := range m.sites {
			if s3SiteHasTag(s, m.selectedTag) {
				base = append(base, s)
			}
		}
	}

	q := strings.TrimSpace(m.searchInput.Value())
	if q == "" {
		m.filtered = base
		return
	}
	words := strings.Fields(strings.ToLower(q))
	var out []s3config.S3Site
	for _, s := range base {
		hay := strings.ToLower(s.Name + " " + s.Endpoint + " " + s.Bucket + " " + s.Region + " " + s.AccessKey + " " + strings.Join(s.Tags, " "))
		match := true
		for _, w := range words {
			w = strings.TrimPrefix(w, "#")
			if !strings.Contains(hay, w) {
				match = false
				break
			}
		}
		if match {
			out = append(out, s)
		}
	}
	m.filtered = out
}

func plainS3ColumnWidth(sites []s3config.S3Site, minW, maxW int, cell func(s3config.S3Site) string) int {
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

func colorizeS3SiteTags(view string, sites []s3config.S3Site) string {
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
	re := regexp.MustCompile(`#(` + strings.Join(quoted, "|") + `)([\s│]|\z)`)
	var b strings.Builder
	prev := 0
	for _, match := range re.FindAllStringSubmatchIndex(view, -1) {
		b.WriteString(view[prev:match[0]])
		b.WriteString(FormatColoredTag(view[match[2]:match[3]]))
		b.WriteString(view[match[3]:match[1]])
		prev = match[1]
	}
	b.WriteString(view[prev:])
	return b.String()
}

func (m *s3SitesModel) siteRows(narrow bool) []table.Row {
	rows := make([]table.Row, 0, len(m.filtered))
	for _, s := range m.filtered {
		displayName := m.siteDisplayName(s)
		bucket := s.Bucket
		if bucket == "" {
			bucket = "-"
		}
		if narrow {
			rows = append(rows, table.Row{
				displayName,
				s.Endpoint,
			})
			continue
		}
		rows = append(rows, table.Row{
			displayName,
			s.Endpoint,
			bucket,
			FormatPlainTags(s.Tags),
		})
	}
	return rows
}

func (m *s3SitesModel) tableHeight() int {
	overhead := 8
	if m.height >= 18 && m.width >= 64 {
		overhead++
	}
	if m.selectedTag != "" {
		overhead++
	}
	if m.statusActive() {
		overhead++
	}
	h := m.height - overhead
	if h < 5 {
		h = 5
	}
	return h
}

func (m *s3SitesModel) rebuildTable() {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.tableHeight()

	if w < 74 {
		rem := w - 4 - 2*2
		if rem < 6 {
			rem = 6
		}
		nameW := rem * 2 / 5
		if nameW < 3 {
			nameW = 3
		}
		endpointW := rem - nameW
		if endpointW < 3 {
			endpointW = 3
		}
		cols := []table.Column{
			{Title: i18n.T("table.col.name"), Width: nameW},
			{Title: "Endpoint", Width: endpointW},
		}
		if m.table.Columns() == nil || len(m.table.Columns()) == 0 {
			m.table = table.New(table.WithColumns(cols), table.WithHeight(h), table.WithFocused(true))
		} else {
			m.table.SetRows(nil)
			m.table.SetColumns(cols)
			m.table.SetHeight(h)
		}
		m.table.SetRows(m.siteRows(true))
		m.clampTableCursor()
		return
	}

	rem := w - 4 - 4*2
	if rem < 20 {
		rem = 20
	}
	nameW := plainS3ColumnWidth(m.filtered, 15, 28, func(s s3config.S3Site) string { return "[ ] ⚫ " + s.Name })
	endpointW := plainS3ColumnWidth(m.filtered, 18, 36, func(s s3config.S3Site) string { return s.Endpoint })
	bucketW := plainS3ColumnWidth(m.filtered, 10, 20, func(s s3config.S3Site) string { return s.Bucket })
	tagsW := rem - nameW - endpointW - bucketW
	if tagsW < 8 {
		tagsW = 8
		endpointW = rem - nameW - bucketW - tagsW
		if endpointW < 12 {
			endpointW = 12
			nameW = rem - endpointW - bucketW - tagsW
			if nameW < 10 {
				nameW = 10
			}
		}
	}

	cols := []table.Column{
		{Title: i18n.T("table.col.name"), Width: nameW},
		{Title: "Endpoint", Width: endpointW},
		{Title: i18n.T("s3.field_bucket"), Width: bucketW},
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

func (m *s3SitesModel) clampTableCursor() {
	count := len(m.filtered)
	if count == 0 || (m.table.Cursor() >= 0 && m.table.Cursor() < count) {
		return
	}
	m.table.SetCursor(0)
}

func (m *s3SitesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
		m.rebuildTable()
		if m.addForm != nil {
			updated, cmd := m.addForm.Update(msg)
			if fm, ok := updated.(*s3AddFormModel); ok {
				m.addForm = fm
			}
			return m, cmd
		}
		return m, nil

	case s3ProbeMsg:
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
				tagCount := len(m.getTagCounts())
				if m.tagPickerCursor < tagCount {
					m.tagPickerCursor++
				}
				return m, nil
			case "enter", " ":
				tagItems := m.getTagCounts()
				if m.tagPickerCursor == 0 {
					m.selectedTag = ""
					m.setStatus(i18n.T("tags.cleared"))
				} else if m.tagPickerCursor-1 < len(tagItems) {
					chosen := tagItems[m.tagPickerCursor-1].tag
					m.selectedTag = chosen
					m.setStatus(fmt.Sprintf(i18n.T("tags.filtered"), "#"+chosen))
				}
				m.tagPickerOpen = false
				m.applyFilter()
				m.rebuildTable()
				return m, nil
			}
			return m, nil
		}
		if m.searchMode {
			return m.handleSearchKeys(msg)
		}
		return m.handleNormalKeys(msg)
	}
	return m, nil
}

func (m *s3SitesModel) handleAddKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.addForm == nil {
		m.addMode = false
		m.editMode = false
		return m, nil
	}
	updated, cmd := m.addForm.Update(msg)
	if fm, ok := updated.(*s3AddFormModel); ok {
		m.addForm = fm
	}
	if m.addForm.cancelled {
		m.addForm = nil
		m.addMode = false
		m.editMode = false
		return m, nil
	}
	if m.addForm.done {
		m.addForm = nil
		m.addMode = false
		m.editMode = false
		m.reload()
		return m, nil
	}
	return m, cmd
}

func (m *s3SitesModel) handleDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		sel := m.getSelectedSites()
		if len(sel) > 0 {
			deleted := 0
			for _, s := range sel {
				if err := s3config.Delete(s.Name); err == nil {
					_ = s3cred.DeleteSecretKey(s.Name)
					deleted++
				}
			}
			m.clearSelection()
			m.confirmDel = false
			m.deleteIdx = -1
			m.reload()
			m.setStatus(fmt.Sprintf(i18n.T("s3.delete_batch_success"), deleted))
			return m, nil
		}
		if m.deleteIdx >= 0 && m.deleteIdx < len(m.filtered) {
			target := m.filtered[m.deleteIdx]
			if err := s3config.Delete(target.Name); err != nil {
				m.setStatus(fmt.Sprintf(i18n.T("s3.delete_failed"), err.Error()))
			} else {
				_ = s3cred.DeleteSecretKey(target.Name)
				m.setStatus(fmt.Sprintf(i18n.T("s3.delete_success"), target.Name))
			}
			m.confirmDel = false
			m.deleteIdx = -1
			m.reload()
			return m, nil
		}
		m.confirmDel = false
		m.deleteIdx = -1
		return m, nil
	case "n", "N", "esc", "q":
		m.confirmDel = false
		m.deleteIdx = -1
		return m, nil
	}
	return m, nil
}

func (m *s3SitesModel) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchInput.Blur()
		m.searchInput.SetValue("")
		m.applyFilter()
		m.rebuildTable()
		m.table.Focus()
		return m, nil
	case "enter", "tab":
		m.searchMode = false
		m.searchInput.Blur()
		m.table.Focus()
		return m, nil
	default:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		m.applyFilter()
		m.rebuildTable()
		return m, cmd
	}
}

func (m *s3SitesModel) startEdit(site s3config.S3Site) {
	m.editMode = true
	m.editOldName = site.Name
	s := site
	m.addForm = newS3AddForm(m.styles, m.width, m.height, &s)
}

func (m *s3SitesModel) handleNormalKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "q", "ctrl+c":
		return m, func() tea.Msg { return s3SitesDoneMsg{} }
	case "esc":
		if len(m.selectedSites) > 0 {
			m.clearSelection()
			m.rebuildTable()
			return m, nil
		}
		if m.selectedTag != "" {
			m.selectedTag = ""
			m.applyFilter()
			m.rebuildTable()
			m.setStatus(i18n.T("tags.cleared"))
			return m, nil
		}
		return m, func() tea.Msg { return s3SitesDoneMsg{} }
	case "t":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewSerial} }
	case "T":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewTelnet} }
	case "F":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewFTP} }
	case "W":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewWebDAV} }
	case "b", "]":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewLocalBrowser} }
	case "[":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewWebDAV} }
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
		return m, func() tea.Msg { return s3OpenBrowserMsg{siteName: name} }
	case "/", "ctrl+f", "tab":
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
		return m, nil
	case "c":
		if m.selectedTag != "" {
			m.selectedTag = ""
			m.applyFilter()
			m.rebuildTable()
			m.setStatus(i18n.T("tags.cleared"))
		}
		return m, nil
	case "a":
		m.addMode = true
		m.addForm = newS3AddForm(m.styles, m.width, m.height, nil)
		return m, m.addForm.Init()
	case "e":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filtered) {
			m.startEdit(m.filtered[idx])
			return m, m.addForm.Init()
		}
		return m, nil
	case "d":
		sel := m.getSelectedSites()
		if len(sel) > 0 {
			m.confirmDel = true
			m.deleteIdx = -1
			return m, nil
		}
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filtered) {
			m.deleteIdx = idx
			m.confirmDel = true
		}
		return m, nil
	case "i":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filtered) {
			s := m.filtered[idx]
			m.infoSite = &s
			m.showInfo = true
			m.infoScroll = 0
		}
		return m, nil
	case "y":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filtered) {
			u := m.filtered[idx].Endpoint
			if err := clipboard.WriteAll(u); err == nil {
				m.setStatus(i18n.T("main.copied") + ": " + u)
			}
		}
		return m, nil
	case "p":
		m.probing = true
		m.setStatus("Probing S3 sites...")
		return m, m.startProbeSelectedCmd()
	case "r":
		m.reload()
		m.setStatus(i18n.T("s3.refreshed"))
		return m, nil
	case "?", "h":
		return m, func() tea.Msg { return s3ShowHelpMsg{} }
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *s3SitesModel) View() string {
	if m.showInfo && m.infoSite != nil {
		return m.renderInfoView()
	}
	if m.addForm != nil {
		return m.addForm.View()
	}

	m.table.SetHeight(m.tableHeight())

	var components []string
	components = append(components, m.styles.Header.Render(" "+i18n.T("s3.sites_title")+" "))

	if m.height >= 18 {
		if tabs := renderProtocolTabs(m.styles, "s3", len(m.filtered), m.width); tabs != "" {
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
	components = append(components, colorizeS3SiteTags(alignJetBrainsBox(tableStyle.Render(m.table.View())), m.filtered))
	if m.statusActive() {
		components = append(components, renderStatusToast(m.statusMessage))
	}
	helpKey := "s3.sites_help"
	components = append(components, renderHelpText(m.styles, i18n.T(helpKey), m.width))
	base := m.styles.App.Render(lipgloss.JoinVertical(lipgloss.Left, components...))
	if m.tagPickerOpen {
		return renderConfirmModal(m.width, m.height, m.renderTagPicker())
	}
	if m.confirmDel {
		if len(m.selectedSites) > 0 {
			box := renderConfirmBox(m.styles, m.width,
				m.styles.ErrorText.Render(i18n.T("delete.title")),
				fmt.Sprintf(i18n.T("s3.sites_delete_batch_confirm"), len(m.selectedSites)),
				i18n.T("delete.warning"),
				m.styles.HelpText.Render(i18n.T("delete.help")),
			)
			return renderConfirmModal(m.width, m.height, box)
		}
		if m.deleteIdx >= 0 && m.deleteIdx < len(m.filtered) {
			name := m.filtered[m.deleteIdx].Name
			box := renderConfirmBox(m.styles, m.width,
				m.styles.ErrorText.Render(i18n.T("delete.title")),
				fmt.Sprintf(i18n.T("s3.sites_delete_confirm"), name),
				i18n.T("delete.warning"),
				m.styles.HelpText.Render(i18n.T("delete.help")),
			)
			return renderConfirmModal(m.width, m.height, box)
		}
	}
	return base
}
