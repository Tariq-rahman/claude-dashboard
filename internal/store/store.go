// Package store persists one JSON record per Claude Code session under a
// dashboard directory. Writes are atomic (write-temp-then-rename) so the
// polling TUI never observes a half-written file. The package depends only on
// the filesystem — it knows nothing of git or hook payloads.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tariq-rahman/claude-dashboard/internal/state"
)

const (
	fileExt  = ".json"
	tmpExt   = ".json.tmp"
	dirPerm  = 0o755
	filePerm = 0o600
)

// Record is the on-disk state of a single Claude Code session.
type Record struct {
	SessionID     string      `json:"session_id"`
	Project       string      `json:"project"`
	Branch        string      `json:"branch"` // git branch for the session
	Cwd           string      `json:"cwd"`
	State         state.State `json:"state"`
	StatusMessage string      `json:"status_message"` // permission tool detail
	Prompt        string      `json:"prompt"`         // sanitized last-user-prompt snippet
	UpdatedAt     time.Time   `json:"updated_at"`
}

// Store reads and writes session records under a single dashboard directory.
type Store struct {
	dir string
}

// New returns a Store rooted at dir. The directory is created lazily on the
// first Save, so dir need not exist yet.
func New(dir string) *Store {
	return &Store{dir: dir}
}

// Dir returns the dashboard directory this store operates on.
func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) path(sessionID string) string {
	return filepath.Join(s.dir, sessionID+fileExt)
}

// Get reads the record for sessionID. It returns an error wrapping
// os.ErrNotExist when no record exists.
func (s *Store) Get(sessionID string) (Record, error) {
	data, err := os.ReadFile(s.path(sessionID))
	if err != nil {
		return Record{}, fmt.Errorf("reading record %s: %w", sessionID, err)
	}

	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, fmt.Errorf("decoding record %s: %w", sessionID, err)
	}

	return rec, nil
}

// Save atomically writes rec, creating the dashboard directory if absent. It
// writes to a temp file and renames into place; rename is atomic on the same
// filesystem, so a concurrent reader sees either the old file or the new one.
func (s *Store) Save(rec Record) error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("creating dashboard dir %s: %w", s.dir, err)
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding record %s: %w", rec.SessionID, err)
	}

	tmp := filepath.Join(s.dir, rec.SessionID+tmpExt)
	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("writing temp record %s: %w", rec.SessionID, err)
	}

	if err := os.Rename(tmp, s.path(rec.SessionID)); err != nil {
		// Best-effort cleanup so a failed rename doesn't leave a temp file.
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming record %s into place: %w", rec.SessionID, err)
	}

	return nil
}

// Delete removes the record for sessionID. A missing record is not an error.
func (s *Store) Delete(sessionID string) error {
	if err := os.Remove(s.path(sessionID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting record %s: %w", sessionID, err)
	}

	return nil
}

// ListRecords returns every valid record in the dashboard directory. Malformed
// JSON, temp files, and non-.json files are skipped silently — a corrupt file
// must not hide the healthy ones. An absent directory yields no records.
func (s *Store) ListRecords() ([]Record, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("reading dashboard dir %s: %w", s.dir, err)
	}

	var records []Record
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, fileExt) || strings.HasSuffix(name, tmpExt) {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			continue
		}

		var rec Record
		if err := json.Unmarshal(data, &rec); err != nil {
			continue
		}

		records = append(records, rec)
	}

	return records, nil
}
