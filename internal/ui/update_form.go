// update_form.go implements the in-app self-update modal: a confirmation
// prompt when a newer release is detected, then live progress while the new
// binary is downloaded, checksum-verified, and swapped in atomically.
package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/selfupdate"
)

// updatePhase tracks where the modal is in the update lifecycle.
type updatePhase int

const (
	updateConfirm updatePhase = iota // waiting for y/n
	updateRunning                    // download/verify/apply in flight
	updateDone                       // success — restart needed
	updateFailed                     // error shown, esc closes
)

// updateProgressMsg carries one progress callback from the apply goroutine.
type updateProgressMsg struct {
	phase   string
	message string
}

// updateAppliedMsg signals successful completion of selfupdate.Apply.
type updateAppliedMsg struct{}

// updateErrorMsg carries a failure from the apply goroutine.
type updateErrorMsg struct{ err error }

// updateTickMsg drives spinner + progress-channel polling while work is in flight.
type updateTickMsg struct{}

// updateVersionInfo is a minimal snapshot of version.UpdateInfo for display.
type updateVersionInfo struct {
	current    string
	latest     string
	releaseURL string
}

// progressChan funnels selfupdate callbacks (arbitrary goroutine) into the tea loop.
var progressChan = make(chan updateProgressMsg, 16)

// pollProgress waits for either a progress event or a short timeout tick.
func pollProgress() tea.Cmd {
	return func() tea.Msg {
		select {
		case m := <-progressChan:
			return m
		case <-time.After(150 * time.Millisecond):
			return updateTickMsg{}
		}
	}
}

var updateSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// updateFormModel is the modal model for the self-update flow.
type updateFormModel struct {
	styles Styles
	width  int
	height int

	info     *updateVersionInfo
	phase    updatePhase
	progress string // last progress message
	percent  int    // -1 when unknown, else 0..100
	frame    int    // spinner frame index
	err      error
}

// NewUpdateForm builds the confirm dialog; latestVer is the remote version.
func NewUpdateForm(styles Styles, width, height int, currentVer, latestVer, releaseURL string) *updateFormModel {
	return &updateFormModel{
		styles: styles,
		width:  width,
		height: height,
		info: &updateVersionInfo{
			current:    currentVer,
			latest:     latestVer,
			releaseURL: releaseURL,
		},
		phase:   updateConfirm,
		percent: -1,
	}
}

// Init satisfies tea.Model; nothing runs until confirmed.
func (m *updateFormModel) Init() tea.Cmd { return nil }

// Update handles keys and async progress events.
func (m *updateFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "n":
			if m.phase != updateRunning {
				return m, func() tea.Msg { return updateCloseMsg{} }
			}
			return m, nil // running updates cannot be cancelled mid-Apply
		case "y", "enter":
			if m.phase == updateConfirm {
				m.phase = updateRunning
				m.progress = ""
				return m, tea.Batch(pollProgress(), runUpdateCmd())
			}
			if m.phase == updateDone || m.phase == updateFailed {
				return m, func() tea.Msg { return updateCloseMsg{} }
			}
		}
		return m, nil

	case updateProgressMsg:
		m.progress = msg.message
		m.percent = percentFromMessage(msg.message)
		return m, pollProgress()

	case updateTickMsg:
		if m.phase == updateRunning {
			m.frame = (m.frame + 1) % len(updateSpinnerFrames)
			return m, pollProgress()
		}
		return m, nil

	case updateAppliedMsg:
		m.phase = updateDone
		m.progress = ""
		return m, nil

	case updateErrorMsg:
		m.phase = updateFailed
		m.err = msg.err
		return m, nil
	}
	return m, nil
}

// runUpdateCmd performs selfupdate.Apply off the UI thread, funneling
// progress through progressChan and finishing with applied/error message.
func runUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err := selfupdate.Apply(ctx, func(phase, message string) {
			progressChan <- updateProgressMsg{phase: phase, message: message}
		})
		if err != nil {
			return updateErrorMsg{err: err}
		}
		return updateAppliedMsg{}
	}
}

// updateCloseMsg tells the root model to dismiss the modal.
type updateCloseMsg struct{}

// percentFromMessage extracts "(NN%)" from a selfupdate progress line,
// returning -1 when the message carries no percentage.
func percentFromMessage(msg string) int {
	parse := func(s string) int {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < 0 || n > 100 {
			return -1
		}
		return n
	}

	// Preferred shape: "... (NN%)" as emitted by selfupdate.
	if i := strings.LastIndex(msg, "("); i >= 0 {
		if j := strings.LastIndex(msg, "%)"); j > i {
			return parse(msg[i+1 : j])
		}
	}

	// Fall back to a trailing bare "NN%" token.
	pct := strings.LastIndex(msg, "%")
	if pct < 0 {
		return -1
	}
	start := pct
	for start > 0 && msg[start-1] >= '0' && msg[start-1] <= '9' {
		start--
	}
	if start == pct {
		return -1 // no digits before '%'
	}
	return parse(msg[start:pct])
}

// progressBar renders an ASCII bar of width cells for pct (-1 = indeterminate).
func progressBar(pct, width int) string {
	if width < 5 {
		width = 5
	}
	filled := 0
	if pct >= 0 {
		filled = width * pct / 100
	}
	bar := "[" + strings.Repeat("=", filled) + strings.Repeat(" ", width-filled) + "]"
	if pct >= 0 {
		bar += fmt.Sprintf(" %3d%%", pct)
	}
	return bar
}

// View renders the modal centered on screen.
func (m *updateFormModel) View() string {
	var body string
	switch m.phase {
	case updateConfirm:
		body = m.renderConfirm()
	case updateRunning:
		spinner := updateSpinnerFrames[m.frame]
		bar := progressBar(m.percent, 32)
		body = lipgloss.JoinVertical(lipgloss.Center,
			m.styles.FormTitle.Render(i18n.T("update.modal_title")),
			"",
			spinner+" "+m.progress,
			bar,
			"",
			m.styles.HelpText.Faint(true).Render(i18n.T("update.running_hint")),
		)
	case updateDone:
		body = lipgloss.JoinVertical(lipgloss.Center,
			m.styles.FormTitle.Render(i18n.T("update.modal_title")),
			"",
			i18n.T("update.success", m.info.latest),
			i18n.T("update.restart_hint"),
			"",
			m.styles.HelpText.Render(i18n.T("update.close_key")),
		)
	case updateFailed:
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		detail := ""
		if m.err != nil {
			detail = ansi.Truncate(m.err.Error(), max(10, m.width-8), "…")
		}
		body = lipgloss.JoinVertical(lipgloss.Center,
			m.styles.FormTitle.Render(i18n.T("update.modal_title")),
			"",
			errStyle.Render("❌ "+i18n.T("update.failed")),
			detail,
			"",
			m.styles.HelpText.Render(i18n.T("update.close_key")),
		)
	}

	box := m.styles.FormContainer.Render(body)
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center, box)
}

// renderConfirm lays out the confirmation dialog with a centered title and
// left-aligned body so the release URL doesn't stretch-center every line.
func (m *updateFormModel) renderConfirm() string {
	title := m.styles.FormTitle.Render(i18n.T("update.modal_title"))

	infoLines := []string{
		i18n.T("update.available_short", m.info.current, m.info.latest),
	}
	if m.info.releaseURL != "" {
		url := ansi.Truncate(m.info.releaseURL, max(24, m.width-12), "…")
		infoLines = append(infoLines, m.styles.HelpText.Faint(true).Render(url))
	}
	info := lipgloss.JoinVertical(lipgloss.Left, infoLines...)

	actions := lipgloss.JoinVertical(lipgloss.Left,
		i18n.T("update.confirm_prompt"),
		m.styles.HelpText.Render(i18n.T("update.confirm_keys")),
	)

	body := lipgloss.JoinVertical(lipgloss.Left, info, "", actions)
	contentW := max(lipgloss.Width(title), lipgloss.Width(body))
	header := lipgloss.NewStyle().Width(contentW).Align(lipgloss.Center).Render(title)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", body)
}
