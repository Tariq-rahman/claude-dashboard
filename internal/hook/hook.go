// Package hook implements the `claude-dash hook` subcommand: it reads a Claude
// Code hook payload from stdin, maps the event to a state, and performs an
// atomic read-modify-write against the store. It is fire-and-forget — the
// guard wrapper recovers panics and logs failures so a dashboard problem can
// never break the user's real session.
package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"unicode/utf8"

	"github.com/tariqrahman/claude-dash/internal/project"
	"github.com/tariqrahman/claude-dash/internal/state"
	"github.com/tariqrahman/claude-dash/internal/store"
)

// maxStatusMessageLen caps the rendered permission status_message.
const maxStatusMessageLen = 80

// logFilePerm is the mode for the hook error log.
const logFilePerm = 0o600

// errPanic wraps a recovered panic value for logging.
var errPanic = errors.New("panic")

// payload is the subset of the Claude Code hook JSON that claude-dash reads.
type payload struct {
	HookEventName string          `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

// toolInput holds the key fields we surface from a permission request.
type toolInput struct {
	Command  string `json:"command"`
	FilePath string `json:"file_path"`
}

// Hook applies hook events to the store. The project Resolver is consulted only
// when a session's file is absent (SessionStart, or a hook added mid-session),
// keeping git off the hot path.
type Hook struct {
	store    *store.Store
	resolver *project.Resolver
	now      func() time.Time
}

// New returns a Hook backed by the given store, project resolver, and clock.
func New(st *store.Store, resolver *project.Resolver, now func() time.Time) *Hook {
	return &Hook{store: st, resolver: resolver, now: now}
}

// Run reads a hook payload from r and applies it to the store.
func (h *Hook) Run(ctx context.Context, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("reading hook payload: %w", err)
	}

	var p payload
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("decoding hook payload: %w", err)
	}

	if err := h.handle(ctx, p); err != nil {
		return fmt.Errorf("handling %s: %w", p.HookEventName, err)
	}

	return nil
}

func (h *Hook) handle(ctx context.Context, evt payload) error {
	// SessionEnd is the one clean-exit event: drop the file entirely.
	if evt.HookEventName == "SessionEnd" {
		if err := h.store.Delete(evt.SessionID); err != nil {
			return fmt.Errorf("deleting on session end: %w", err)
		}

		return nil
	}

	newState, ok := state.FromEvent(evt.HookEventName)
	if !ok {
		// Unhooked event (e.g. Notification) — nothing to do.
		return nil
	}

	rec, err := h.store.Get(evt.SessionID)
	switch {
	case err == nil:
		// Existing file: preserve the cached project name, never recompute.
	case errors.Is(err, os.ErrNotExist):
		// Absent file: compute project once (self-heal) and create it.
		rec = store.Record{
			SessionID: evt.SessionID,
			Project:   h.resolver.GetName(ctx, evt.Cwd),
		}
	default:
		return fmt.Errorf("reading existing record: %w", err)
	}

	rec.SessionID = evt.SessionID
	rec.Cwd = evt.Cwd
	rec.State = newState
	rec.UpdatedAt = h.now()

	if newState == state.WaitingForPermission {
		rec.StatusMessage = statusMessage(evt.ToolName, evt.ToolInput)
	} else {
		rec.StatusMessage = ""
	}

	if err := h.store.Save(rec); err != nil {
		return fmt.Errorf("saving record: %w", err)
	}

	return nil
}

// statusMessage renders a compact "ToolName: detail" string for a permission
// request: the command for Bash, the path for Edit/Write, capped at 80 chars.
func statusMessage(toolName string, rawInput json.RawMessage) string {
	var in toolInput
	if len(rawInput) > 0 {
		// A malformed tool_input simply leaves the detail blank.
		if err := json.Unmarshal(rawInput, &in); err != nil {
			return toolName
		}
	}

	detail := in.Command
	if detail == "" {
		detail = in.FilePath
	}

	msg := toolName
	if detail != "" {
		msg = toolName + ": " + detail
	}

	return truncate(msg, maxStatusMessageLen)
}

// truncate caps s at limit runes, replacing the final rune with an ellipsis
// when it overflows.
func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}

	runes := []rune(s)

	return string(runes[:limit-1]) + "…"
}

// Execute is the entire body of the `claude-dash hook` subcommand: it parses
// the payload from r and applies it via h, recovering panics and logging any
// failure to logPath. It never panics and never returns — the caller then
// always exits 0, so a dashboard failure can never break the real session.
func Execute(ctx context.Context, r io.Reader, h *Hook, logPath string) {
	guard(logPath, func() error { return h.Run(ctx, r) })
}

// guard runs fn, recovering any panic and logging both panics and returned
// errors to logPath. It never panics and never surfaces an error: the hook
// subcommand wraps its whole body in guard and then always exits 0.
func guard(logPath string, fn func() error) {
	defer func() {
		if r := recover(); r != nil {
			logErr(logPath, fmt.Errorf("%w: %v", errPanic, r))
		}
	}()

	if err := fn(); err != nil {
		logErr(logPath, err)
	}
}

// logErr appends a timestamped line to logPath, swallowing any logging error —
// there is nowhere safe left to report to.
func logErr(logPath string, err error) {
	f, openErr := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, logFilePerm)
	if openErr != nil {
		return
	}
	defer func() { _ = f.Close() }()

	_, _ = fmt.Fprintf(f, "%s %v\n", time.Now().Format(time.RFC3339), err)
}
