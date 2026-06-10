// Package tui renders the Claude instance dashboard: a Bubble Tea model that
// polls the store once a second, sorts instances by urgency, greys or reaps
// stale rows, and lets the user dismiss zombies. Unlike the hook, the TUI is
// the foreground app and fails loudly.
package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tariq-rahman/claude-dashboard/internal/focus"
	"github.com/Tariq-rahman/claude-dashboard/internal/state"
	"github.com/Tariq-rahman/claude-dashboard/internal/store"
)

// Config holds the reaping thresholds and poll interval.
type Config struct {
	// GreyAfter is the age past which a non-permission row is de-emphasised.
	GreyAfter time.Duration
	// DropAfter is the age past which a non-permission row is deleted.
	DropAfter time.Duration
	// PollEvery is how often the store is re-read.
	PollEvery time.Duration
}

// Production reaping thresholds and poll cadence.
const (
	defaultGreyAfter = 10 * time.Minute
	defaultDropAfter = time.Hour
	defaultPollEvery = time.Second
)

// DefaultConfig returns the production thresholds: grey after 10 minutes, drop
// after 1 hour, poll every second.
func DefaultConfig() Config {
	return Config{
		GreyAfter: defaultGreyAfter,
		DropAfter: defaultDropAfter,
		PollEvery: defaultPollEvery,
	}
}

// tickMsg carries the wall-clock time of a poll tick.
type tickMsg time.Time

// disposition is the reaping decision for a single record.
type disposition int

const (
	dispVisible disposition = iota // shown normally
	dispGrey                       // shown, de-emphasised
	dispDrop                       // deleted and removed from view
)

// rowView is a record paired with its display flag.
type rowView struct {
	rec   store.Record
	stale bool
}

// defaultWidth is the assumed terminal width before the first WindowSizeMsg.
const defaultWidth = 80

// focuser brings a terminal to the foreground. *focus.Focuser satisfies it in
// production; tests inject a fake so no windows actually move.
type focuser interface {
	Focus(ctx context.Context, termType, termID string) error
}

// Model is the Bubble Tea model for the dashboard.
type Model struct {
	store   *store.Store
	cfg     Config
	focuser focuser
	now     time.Time
	rows    []rowView
	cursor  int
	width   int
	err     error  // loud failures, rendered in red
	notice  string // soft transient hints (e.g. "jump not available")
}

// New returns a dashboard Model backed by st, using f to jump to terminals.
func New(st *store.Store, cfg Config, f focuser) Model {
	return Model{store: st, cfg: cfg, focuser: f, width: defaultWidth}
}

// Init kicks off an immediate poll and starts the recurring ticker.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return tickMsg(time.Now()) },
		tickCmd(m.cfg.PollEvery),
	)
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update handles poll ticks and key presses.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m = m.refresh(time.Time(msg))
		return m, tickCmd(m.cfg.PollEvery)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyCtrlC, msg.String() == "q":
		return m, tea.Quit

	case msg.Type == tea.KeyUp, msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case msg.Type == tea.KeyDown, msg.String() == "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}

	case msg.Type == tea.KeyEnter:
		m = m.jumpSelected()

	case msg.String() == "d":
		m = m.dismissSelected()
	}

	return m, nil
}

// jumpSelected brings the selected row's terminal to the foreground. It is
// synchronous: osascript returns well under the 1s poll interval, so a blocking
// call keeps the model logic simple. ErrJumpUnavailable (unknown terminal) and
// ErrTargetNotFound (closed tab) are soft outcomes shown as a footer notice;
// any other error is a loud failure shown in red.
func (m Model) jumpSelected() Model {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return m
	}

	m.err = nil
	m.notice = ""

	rec := m.rows[m.cursor].rec
	err := m.focuser.Focus(context.Background(), rec.TerminalType, rec.TerminalID)
	switch {
	case err == nil:
	case errors.Is(err, focus.ErrJumpUnavailable), errors.Is(err, focus.ErrTargetNotFound):
		m.notice = err.Error()
	default:
		m.err = err
	}

	return m
}

// dismissSelected deletes the selected row's state file (the manual escape
// hatch for zombie rows) and refreshes.
func (m Model) dismissSelected() Model {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return m
	}

	if err := m.store.Delete(m.rows[m.cursor].rec.SessionID); err != nil {
		m.err = err
		return m
	}

	return m.refresh(m.now)
}

// refresh re-reads the store at time t, reaps dropped records, and rebuilds the
// sorted, greyed row list.
func (m Model) refresh(t time.Time) Model {
	m.now = t
	m.notice = "" // soft hints are transient — clear them each poll

	records, err := m.store.ListRecords()
	if err != nil {
		m.err = err
		return m
	}
	m.err = nil

	kept := make([]store.Record, 0, len(records))
	stale := make(map[string]bool, len(records))
	for _, r := range records {
		switch classify(r, t, m.cfg) {
		case dispDrop:
			if delErr := m.store.Delete(r.SessionID); delErr != nil {
				m.err = delErr
			}
		case dispGrey:
			stale[r.SessionID] = true
			kept = append(kept, r)
		case dispVisible:
			kept = append(kept, r)
		}
	}

	sorted := sortRecords(kept)
	rows := make([]rowView, len(sorted))
	for i, r := range sorted {
		rows[i] = rowView{rec: r, stale: stale[r.SessionID]}
	}
	m.rows = rows

	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	return m
}

// classify decides whether a record is shown, greyed, or reaped. Permission
// rows are always visible: a needs-you-now state must never silently vanish.
func classify(rec store.Record, now time.Time, cfg Config) disposition {
	if rec.State == state.WaitingForPermission {
		return dispVisible
	}

	age := now.Sub(rec.UpdatedAt)
	switch {
	case age > cfg.DropAfter:
		return dispDrop
	case age > cfg.GreyAfter:
		return dispGrey
	default:
		return dispVisible
	}
}

// bandRank orders the urgency bands: permission first, then input, then working.
func bandRank(s state.State) int {
	switch s {
	case state.WaitingForPermission:
		return 0
	case state.WaitingForInput:
		return 1
	default:
		return 2
	}
}

// sortRecords sorts by urgency band, then within waiting bands oldest-first
// (longest wait on top) and within the working band newest-first (most active
// on top).
func sortRecords(records []store.Record) []store.Record {
	sorted := make([]store.Record, len(records))
	copy(sorted, records)

	sort.SliceStable(sorted, func(i, j int) bool {
		bi, bj := bandRank(sorted[i].State), bandRank(sorted[j].State)
		if bi != bj {
			return bi < bj
		}

		if sorted[i].State == state.Working {
			return sorted[i].UpdatedAt.After(sorted[j].UpdatedAt)
		}

		return sorted[i].UpdatedAt.Before(sorted[j].UpdatedAt)
	})

	return sorted
}

// formatAge renders a duration compactly: 5s, 2m, 3h, 2d.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}
