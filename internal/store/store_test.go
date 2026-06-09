package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Tariq-rahman/claude-dashboard/internal/state"
)

func sampleRecord() Record {
	return Record{
		SessionID:     "abc123",
		Project:       "instalment",
		Cwd:           "/Users/tariqrahman/Projects/payments/instalment/internal/storage",
		State:         state.WaitingForPermission,
		StatusMessage: "Bash: git push origin main",
		UpdatedAt:     time.Date(2026, time.June, 9, 13, 50, 0, 0, time.UTC),
	}
}

func TestStore_SaveGetRoundTrip(t *testing.T) {
	t.Parallel()

	s := New(t.TempDir())
	want := sampleRecord()

	require.NoErrorf(t, s.Save(want), "Save")

	got, err := s.Get(want.SessionID)
	require.NoErrorf(t, err, "Get")
	require.Equalf(t, want, got, "round-tripped record mismatch")
}

func TestStore_SaveCreatesDirIfAbsent(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "does", "not", "exist")
	s := New(dir)

	require.NoErrorf(t, s.Save(sampleRecord()), "Save into absent dir")

	_, err := os.Stat(dir)
	require.NoErrorf(t, err, "dashboard dir should have been created")
}

func TestStore_SaveLeavesNoTempFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := New(dir)

	require.NoErrorf(t, s.Save(sampleRecord()), "Save")

	entries, err := os.ReadDir(dir)
	require.NoErrorf(t, err, "ReadDir")
	for _, e := range entries {
		require.NotContainsf(t, e.Name(), ".tmp", "temp file %q left behind after Save", e.Name())
	}
	require.Lenf(t, entries, 1, "expected exactly one state file")
}

func TestStore_GetMissingReturnsNotExist(t *testing.T) {
	t.Parallel()

	s := New(t.TempDir())

	_, err := s.Get("nope")
	require.ErrorIsf(t, err, os.ErrNotExist, "Get on missing session")
}

func TestStore_Delete(t *testing.T) {
	t.Parallel()

	t.Run("removes existing file", func(t *testing.T) {
		t.Parallel()

		s := New(t.TempDir())
		rec := sampleRecord()
		require.NoErrorf(t, s.Save(rec), "Save")

		require.NoErrorf(t, s.Delete(rec.SessionID), "Delete")

		_, err := s.Get(rec.SessionID)
		require.ErrorIsf(t, err, os.ErrNotExist, "record should be gone")
	})

	t.Run("is idempotent for a missing file", func(t *testing.T) {
		t.Parallel()

		s := New(t.TempDir())
		require.NoErrorf(t, s.Delete("never-existed"), "Delete on missing should be a no-op")
	})
}

func TestStore_ListRecords(t *testing.T) {
	t.Parallel()

	t.Run("returns all saved records", func(t *testing.T) {
		t.Parallel()

		s := New(t.TempDir())
		a := sampleRecord()
		a.SessionID = "aaa"
		b := sampleRecord()
		b.SessionID = "bbb"
		b.Project = "payrun"

		require.NoErrorf(t, s.Save(a), "Save a")
		require.NoErrorf(t, s.Save(b), "Save b")

		got, err := s.ListRecords()
		require.NoErrorf(t, err, "ListRecords")
		require.Lenf(t, got, 2, "expected two records")
	})

	t.Run("skips malformed and non-json files", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		s := New(dir)
		require.NoErrorf(t, s.Save(sampleRecord()), "Save valid")

		require.NoErrorf(
			t,
			os.WriteFile(filepath.Join(dir, "garbage.json"), []byte("{not json"), 0o600),
			"write garbage",
		)
		require.NoErrorf(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o600), "write txt")
		require.NoErrorf(t, os.WriteFile(filepath.Join(dir, "pending.json.tmp"), []byte("{}"), 0o600), "write tmp")

		got, err := s.ListRecords()
		require.NoErrorf(t, err, "ListRecords")
		require.Lenf(t, got, 1, "only the valid record should be returned")
		require.Equalf(t, "abc123", got[0].SessionID, "wrong record returned")
	})

	t.Run("returns empty when dir is absent", func(t *testing.T) {
		t.Parallel()

		s := New(filepath.Join(t.TempDir(), "absent"))

		got, err := s.ListRecords()
		require.NoErrorf(t, err, "ListRecords on absent dir")
		require.Emptyf(t, got, "expected no records")
	})
}

func TestStore_SaveOverwritesAtomically(t *testing.T) {
	t.Parallel()

	s := New(t.TempDir())
	rec := sampleRecord()
	require.NoErrorf(t, s.Save(rec), "first Save")

	rec.State = state.Working
	rec.StatusMessage = ""
	require.NoErrorf(t, s.Save(rec), "second Save")

	got, err := s.Get(rec.SessionID)
	require.NoErrorf(t, err, "Get")
	require.Equalf(t, state.Working, got.State, "state should have been overwritten")
	require.Emptyf(t, got.StatusMessage, "status message should have been cleared")
}
