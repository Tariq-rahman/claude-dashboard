package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Tariq-rahman/claude-dashboard/internal/state"
	"github.com/Tariq-rahman/claude-dashboard/internal/store"
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

	if m.notice != "" {
		b.WriteString(helpStyle.Render(m.notice))
		b.WriteByte('\n')
	}

	b.WriteString(helpStyle.Render("↑/↓ select · enter jump · d dismiss · q quit"))

	return b.String()
}

const (
	// detailPrefix marks the second (context) line of a row.
	detailPrefix = "↳ "
	// detailIndent aligns the context line under the project column of line 1.
	detailIndent = "         "
	// placeholder stands in for an absent branch or detail.
	placeholder = "—"
	// minDetailWidth is the smallest detail budget we ever truncate to, so a
	// very narrow terminal never yields a negative width.
	minDetailWidth = 8
)

// renderRow renders one instance as a two-line selectable block: an identifying
// line (icon · label · project · branch · age) and an indented context line
// showing the permission tool detail (when awaiting permission) or the last
// user-prompt snippet otherwise.
func (m Model) renderRow(i int, row rowView) string {
	icon, label := glyph(row.rec.State)
	age := formatAge(m.now.Sub(row.rec.UpdatedAt))

	branch := row.rec.Branch
	if branch == "" {
		branch = placeholder
	}

	line1 := fmt.Sprintf("%s %-8s %-14s %-14s %5s", icon, label, row.rec.Project, branch, age)
	line2 := detailIndent + detailPrefix + truncateField(m.detailText(row.rec), m.detailWidth())

	style := lipgloss.NewStyle().Foreground(bandColour(row.rec.State))
	if row.stale {
		style = lipgloss.NewStyle().Foreground(colourStale)
	}
	if i == m.cursor {
		style = style.Inherit(selectedStyle)
	}

	return style.Render(line1) + "\n" + style.Render(line2)
}

// detailText is the context line's content: the tool detail for a permission
// request (the urgent "what is it asking to run"), the prompt snippet otherwise,
// and a placeholder when neither is set.
func (m Model) detailText(rec store.Record) string {
	detail := rec.Prompt
	if rec.State == state.WaitingForPermission {
		detail = rec.StatusMessage
	}
	if detail == "" {
		return placeholder
	}

	return detail
}

// detailWidth is the rune budget for the context line: the terminal width less
// the indent and the "↳ " marker, floored at minDetailWidth.
func (m Model) detailWidth() int {
	w := m.width - len([]rune(detailIndent)) - len([]rune(detailPrefix))
	if w < minDetailWidth {
		return minDetailWidth
	}

	return w
}

func truncateField(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit-1]) + "…"
}
