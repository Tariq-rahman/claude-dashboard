package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tariqrahman/claude-dash/internal/state"
)

var (
	colourPermission = lipgloss.Color("9")  // red
	colourInput      = lipgloss.Color("11") // amber
	colourWorking    = lipgloss.Color("10") // green
	colourStale      = lipgloss.Color("8")  // grey

	titleStyle    = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Padding(0, 1)
	errStyle      = lipgloss.NewStyle().Foreground(colourPermission).Bold(true).Padding(0, 1)
)

// glyph maps a state to its icon and short label.
func glyph(s state.State) (icon, label string) {
	switch s {
	case state.WaitingForPermission:
		return "⛔", "perm"
	case state.WaitingForInput:
		return "⚠", "input"
	default:
		return "●", "working"
	}
}

func bandColour(s state.State) lipgloss.Color {
	switch s {
	case state.WaitingForPermission:
		return colourPermission
	case state.WaitingForInput:
		return colourInput
	default:
		return colourWorking
	}
}

// View renders the dashboard.
func (m Model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("claude instances"))
	b.WriteByte('\n')

	if m.err != nil {
		b.WriteString(errStyle.Render("error: " + m.err.Error()))
		b.WriteByte('\n')
	}

	if len(m.rows) == 0 {
		b.WriteString(helpStyle.Render("no active instances"))
		b.WriteByte('\n')
	}

	for i, row := range m.rows {
		b.WriteString(m.renderRow(i, row))
		b.WriteByte('\n')
	}

	b.WriteString(helpStyle.Render("↑/↓ select · d dismiss · q quit"))

	return b.String()
}

// statusWidth is the fixed column width for the status_message field.
const statusWidth = 40

func (m Model) renderRow(i int, row rowView) string {
	icon, label := glyph(row.rec.State)
	age := formatAge(m.now.Sub(row.rec.UpdatedAt))

	line := fmt.Sprintf("%s %-8s %-14s %-40s %5s",
		icon, label, row.rec.Project, truncateField(row.rec.StatusMessage, statusWidth), age)

	style := lipgloss.NewStyle().Foreground(bandColour(row.rec.State))
	if row.stale {
		style = lipgloss.NewStyle().Foreground(colourStale)
	}
	if i == m.cursor {
		style = style.Inherit(selectedStyle)
	}

	return style.Render(line)
}

func truncateField(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit-1]) + "…"
}
