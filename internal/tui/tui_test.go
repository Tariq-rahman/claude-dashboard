package tui

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/Tariq-rahman/claude-dashboard/internal/focus"
	"github.com/Tariq-rahman/claude-dashboard/internal/state"
	"github.com/Tariq-rahman/claude-dashboard/internal/store"
)

// fakeFocuser records the last Focus call and returns a configured error.
type fakeFocuser struct {
	calls   int
	gotType string
	gotID   string
	err     error
}

func (f *fakeFocuser) Focus(_ context.Context, termType, termID string) error {
	f.calls++
	f.gotType = termType
	f.gotID = termID

	return f.err
}

var now = time.Date(2026, time.June, 9, 14, 0, 0, 0, time.UTC)

func rec(id string, st state.State, age time.Duration) store.Record {
	return store.Record{
		SessionID: id,
		Project:   id + "-proj",
		State:     st,
		UpdatedAt: now.Add(-age),
	}
}

func sessionIDs(rows []rowView) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.rec.SessionID
	}

	return ids
}

func findRow(t *testing.T, rows []rowView, id string) rowView {
	t.Helper()

	for _, r := range rows {
		if r.rec.SessionID == id {
			return r
		}
	}

	require.FailNowf(t, "row not found", "no row with session id %q", id)

	return rowView{}
}

func TestSortRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []store.Record
		want []string
	}{
		{
			name: "bands ordered permission then input then working",
			in: []store.Record{
				rec("work", state.Working, 0),
				rec("input", state.WaitingForInput, 2*time.Minute),
				rec("perm", state.WaitingForPermission, 5*time.Second),
			},
			want: []string{"perm", "input", "work"},
		},
		{
			name: "waiting bands oldest first, working band newest first",
			in: []store.Record{
				rec("workNew", state.Working, 1*time.Second),
				rec("workOld", state.Working, 10*time.Second),
				rec("inputNew", state.WaitingForInput, 2*time.Minute),
				rec("inputOld", state.WaitingForInput, 5*time.Minute),
				rec("permNew", state.WaitingForPermission, 30*time.Second),
				rec("permOld", state.WaitingForPermission, 90*time.Second),
			},
			want: []string{"permOld", "permNew", "inputOld", "inputNew", "workNew", "workOld"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := sortRecords(tt.in)
			ids := make([]string, len(got))
			for i, r := range got {
				ids[i] = r.SessionID
			}
			require.Equalf(t, tt.want, ids, "sort order mismatch")
		})
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	tests := []struct {
		name string
		rec  store.Record
		want disposition
	}{
		{name: "fresh working is visible", rec: rec("a", state.Working, 5*time.Second), want: dispVisible},
		{name: "working past grey threshold is grey", rec: rec("a", state.Working, 11*time.Minute), want: dispGrey},
		{name: "working past drop threshold is dropped", rec: rec("a", state.Working, 2*time.Hour), want: dispDrop},
		{
			name: "input past grey threshold is grey",
			rec:  rec("a", state.WaitingForInput, 11*time.Minute),
			want: dispGrey,
		},
		{
			name: "input past drop threshold is dropped",
			rec:  rec("a", state.WaitingForInput, 2*time.Hour),
			want: dispDrop,
		},
		{
			name: "permission is never grey even when old",
			rec:  rec("a", state.WaitingForPermission, 30*time.Minute),
			want: dispVisible,
		},
		{
			name: "permission is never dropped even when ancient",
			rec:  rec("a", state.WaitingForPermission, 5*time.Hour),
			want: dispVisible,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equalf(t, tt.want, classify(tt.rec, now, cfg), "disposition mismatch")
		})
	}
}

func TestModel_TickReapsAndGreys(t *testing.T) {
	t.Parallel()

	st := store.New(t.TempDir())
	seed := []store.Record{
		rec("permAncient", state.WaitingForPermission, 5*time.Hour), // visible, never dropped
		rec("inputGrey", state.WaitingForInput, 30*time.Minute),     // grey
		rec("inputDrop", state.WaitingForInput, 2*time.Hour),        // dropped
		rec("workGrey", state.Working, 30*time.Minute),              // grey
		rec("workDrop", state.Working, 2*time.Hour),                 // dropped
		rec("workFresh", state.Working, 5*time.Second),              // visible
	}
	for _, r := range seed {
		require.NoErrorf(t, st.Save(r), "seed %s", r.SessionID)
	}

	m := New(st, DefaultConfig(), nil)
	m = updateModel(t, m, tickMsg(now))

	// Dropped records are deleted from the store.
	for _, dropped := range []string{"inputDrop", "workDrop"} {
		_, err := st.Get(dropped)
		require.ErrorIsf(t, err, os.ErrNotExist, "%s should have been reaped", dropped)
	}

	// Surviving rows: the two dropped ones gone, four remain.
	require.ElementsMatchf(t, []string{"permAncient", "inputGrey", "workGrey", "workFresh"},
		sessionIDs(m.rows), "unexpected surviving rows")

	require.Falsef(t, findRow(t, m.rows, "permAncient").stale, "ancient permission must not be greyed")
	require.Truef(t, findRow(t, m.rows, "inputGrey").stale, "old input should be greyed")
	require.Truef(t, findRow(t, m.rows, "workGrey").stale, "hung working should be greyed")
	require.Falsef(t, findRow(t, m.rows, "workFresh").stale, "fresh working should not be greyed")
}

func TestModel_CursorMovement(t *testing.T) {
	t.Parallel()

	st := store.New(t.TempDir())
	for _, r := range []store.Record{
		rec("perm", state.WaitingForPermission, 5*time.Second),
		rec("input", state.WaitingForInput, 1*time.Minute),
		rec("work", state.Working, 1*time.Second),
	} {
		require.NoErrorf(t, st.Save(r), "seed %s", r.SessionID)
	}

	m := New(st, DefaultConfig(), nil)
	m = updateModel(t, m, tickMsg(now))
	require.Equalf(t, 0, m.cursor, "cursor should start at 0")

	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	require.Equalf(t, 1, m.cursor, "down should advance cursor")

	// Cannot move below the last row.
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	require.Equalf(t, 2, m.cursor, "cursor should clamp at last row")

	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	require.Equalf(t, 1, m.cursor, "up should retreat cursor")

	// Cannot move above the first row.
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	require.Equalf(t, 0, m.cursor, "cursor should clamp at first row")
}

func TestModel_DismissDeletesSelectedFile(t *testing.T) {
	t.Parallel()

	st := store.New(t.TempDir())
	for _, r := range []store.Record{
		rec("perm", state.WaitingForPermission, 5*time.Second),
		rec("input", state.WaitingForInput, 1*time.Minute),
	} {
		require.NoErrorf(t, st.Save(r), "seed %s", r.SessionID)
	}

	m := New(st, DefaultConfig(), nil)
	m = updateModel(t, m, tickMsg(now))

	// Cursor 0 is "perm" (top band). Dismiss it.
	require.Equalf(t, "perm", m.rows[0].rec.SessionID, "perm should be selected")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})

	_, err := st.Get("perm")
	require.ErrorIsf(t, err, os.ErrNotExist, "dismissed file should be deleted")
	require.ElementsMatchf(t, []string{"input"}, sessionIDs(m.rows), "only input should remain")
}

func TestModel_QuitKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  tea.KeyMsg
	}{
		{name: "q quits", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}},
		{name: "ctrl+c quits", msg: tea.KeyMsg{Type: tea.KeyCtrlC}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := New(store.New(t.TempDir()), DefaultConfig(), nil)
			_, cmd := m.Update(tt.msg)
			require.NotNilf(t, cmd, "quit key should return a command")
			require.IsTypef(t, tea.QuitMsg{}, cmd(), "command should be tea.Quit")
		})
	}
}

func TestModel_JumpSelected(t *testing.T) {
	t.Parallel()

	// seedOne stores a single record so cursor 0 is deterministic.
	seedOne := func(t *testing.T, r store.Record) *store.Store {
		t.Helper()

		st := store.New(t.TempDir())
		r.SessionID = "s"
		r.State = state.WaitingForPermission // pinned visible, never reaped
		r.UpdatedAt = now
		require.NoErrorf(t, st.Save(r), "seed record")

		return st
	}

	t.Run("enter focuses the selected row's terminal", func(t *testing.T) {
		t.Parallel()

		st := seedOne(t, store.Record{TerminalType: "iterm2", TerminalID: "w0t1p0:UUID"})
		f := &fakeFocuser{}

		m := New(st, DefaultConfig(), f)
		m = updateModel(t, m, tickMsg(now))
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})

		require.Equalf(t, 1, f.calls, "focuser should be invoked once")
		require.Equalf(t, "iterm2", f.gotType, "should pass the row's terminal type")
		require.Equalf(t, "w0t1p0:UUID", f.gotID, "should pass the row's terminal id")
		require.NoErrorf(t, m.err, "a successful jump must not set the loud error")
		require.Emptyf(t, m.notice, "a successful jump must not set a notice")
	})

	t.Run("an unavailable jump shows a footer notice, not the loud error", func(t *testing.T) {
		t.Parallel()

		st := seedOne(t, store.Record{}) // no terminal fields
		f := &fakeFocuser{err: focus.ErrJumpUnavailable}

		m := New(st, DefaultConfig(), f)
		m = updateModel(t, m, tickMsg(now))
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})

		require.NoErrorf(t, m.err, "an unavailable jump must not set the loud error")
		require.NotEmptyf(t, m.notice, "an unavailable jump should set a footer notice")
	})

	t.Run("a closed target shows a footer notice, not the loud error", func(t *testing.T) {
		t.Parallel()

		st := seedOne(t, store.Record{TerminalType: "iterm2", TerminalID: "w0t1p0:GONE"})
		f := &fakeFocuser{err: focus.ErrTargetNotFound}

		m := New(st, DefaultConfig(), f)
		m = updateModel(t, m, tickMsg(now))
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})

		require.NoErrorf(t, m.err, "a closed target must not set the loud error")
		require.NotEmptyf(t, m.notice, "a closed target should set a footer notice")
	})

	t.Run("an unexpected focuser error lands in m.err", func(t *testing.T) {
		t.Parallel()

		st := seedOne(t, store.Record{TerminalType: "iterm2", TerminalID: "w0t1p0:UUID"})
		f := &fakeFocuser{err: errors.New("osascript: command not found")}

		m := New(st, DefaultConfig(), f)
		m = updateModel(t, m, tickMsg(now))
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})

		require.Errorf(t, m.err, "an unexpected error should land in the loud m.err")
		require.Emptyf(t, m.notice, "an unexpected error should not set the soft notice")
	})
}

func TestFormatAge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "seconds", d: 5 * time.Second, want: "5s"},
		{name: "sub-second rounds to 0s", d: 200 * time.Millisecond, want: "0s"},
		{name: "minutes", d: 2 * time.Minute, want: "2m"},
		{name: "hours", d: 3 * time.Hour, want: "3h"},
		{name: "days", d: 50 * time.Hour, want: "2d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equalf(t, tt.want, formatAge(tt.d), "age format mismatch")
		})
	}
}

func updateModel(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()

	next, _ := m.Update(msg)
	nm, ok := next.(Model)
	require.Truef(t, ok, "Update should return a tui.Model")

	return nm
}
