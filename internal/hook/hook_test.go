package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/Tariq-rahman/claude-dashboard/internal/project"
	"github.com/Tariq-rahman/claude-dashboard/internal/state"
	"github.com/Tariq-rahman/claude-dashboard/internal/store"
)

// initGitRepo creates a throwaway git repository in a temp dir, checked out on
// branch with one empty commit so `git rev-parse --abbrev-ref HEAD` resolves.
func initGitRepo(t *testing.T, branch string) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "checkout", "-q", "-b", branch)
	runGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
}

var (
	fixedNow  = time.Date(2026, time.June, 9, 13, 50, 0, 0, time.UTC)
	errKaboom = errors.New("kaboom")
)

func newTestHook(t *testing.T) (*Hook, *store.Store) {
	t.Helper()

	st := store.New(t.TempDir())
	resolver := project.NewResolver(project.NewExecRunner())
	h := New(st, resolver, func() time.Time { return fixedNow })

	return h, st
}

func TestHook_Handle_EventMapping(t *testing.T) {
	t.Parallel()

	const sessionID = "sess-1"

	tests := []struct {
		name      string
		payload   string
		wantState state.State
	}{
		{
			name:      "UserPromptSubmit -> working",
			payload:   `{"hook_event_name":"UserPromptSubmit","session_id":"sess-1","cwd":"/tmp/proj"}`,
			wantState: state.Working,
		},
		{
			name:      "PostToolUse -> working",
			payload:   `{"hook_event_name":"PostToolUse","session_id":"sess-1","cwd":"/tmp/proj"}`,
			wantState: state.Working,
		},
		{
			name:      "Stop -> waiting-for-input",
			payload:   `{"hook_event_name":"Stop","session_id":"sess-1","cwd":"/tmp/proj"}`,
			wantState: state.WaitingForInput,
		},
		{
			name:      "SessionStart -> waiting-for-input",
			payload:   `{"hook_event_name":"SessionStart","session_id":"sess-1","cwd":"/tmp/proj"}`,
			wantState: state.WaitingForInput,
		},
		{
			name:      "PermissionRequest -> waiting-for-permission",
			payload:   `{"hook_event_name":"PermissionRequest","session_id":"sess-1","cwd":"/tmp/proj","tool_name":"Bash","tool_input":{"command":"ls"}}`,
			wantState: state.WaitingForPermission,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, st := newTestHook(t)

			err := h.Run(t.Context(), strings.NewReader(tt.payload))
			require.NoErrorf(t, err, "Run")

			got, err := st.Get(sessionID)
			require.NoErrorf(t, err, "Get")
			require.Equalf(t, tt.wantState, got.State, "state mismatch")
			require.Equalf(t, fixedNow, got.UpdatedAt, "updated_at should be set from clock")
		})
	}
}

func TestHook_Handle_SessionEndDeletesFile(t *testing.T) {
	t.Parallel()

	h, st := newTestHook(t)
	require.NoErrorf(t, st.Save(store.Record{SessionID: "sess-1", Project: "proj"}), "seed record")

	err := h.Run(t.Context(), strings.NewReader(`{"hook_event_name":"SessionEnd","session_id":"sess-1"}`))
	require.NoErrorf(t, err, "Run SessionEnd")

	_, err = st.Get("sess-1")
	require.ErrorIsf(t, err, os.ErrNotExist, "record should be deleted")
}

func TestHook_Handle_UnhookedEventIsIgnored(t *testing.T) {
	t.Parallel()

	h, st := newTestHook(t)

	err := h.Run(
		t.Context(),
		strings.NewReader(`{"hook_event_name":"Notification","session_id":"sess-1","cwd":"/tmp/proj"}`),
	)
	require.NoErrorf(t, err, "Run Notification")

	_, err = st.Get("sess-1")
	require.ErrorIsf(t, err, os.ErrNotExist, "no record should be created for an unhooked event")
}

func TestHook_Handle_PreservesProjectAcrossEvents(t *testing.T) {
	t.Parallel()

	h, st := newTestHook(t)
	// Seed as if SessionStart already computed the project name once.
	require.NoErrorf(t, st.Save(store.Record{
		SessionID: "sess-1",
		Project:   "instalment",
		Cwd:       "/Users/tariqrahman/Projects/payments/instalment",
		State:     state.WaitingForInput,
		UpdatedAt: fixedNow.Add(-time.Minute),
	}), "seed record")

	// A later PostToolUse event from a deeper cwd must not recompute project.
	err := h.Run(t.Context(), strings.NewReader(
		`{"hook_event_name":"PostToolUse","session_id":"sess-1","cwd":"/Users/tariqrahman/Projects/payments/instalment/internal/storage"}`,
	))
	require.NoErrorf(t, err, "Run PostToolUse")

	got, err := st.Get("sess-1")
	require.NoErrorf(t, err, "Get")
	require.Equalf(t, "instalment", got.Project, "project must be preserved, not recomputed")
	require.Equalf(t, state.Working, got.State, "state should update to working")
}

func TestHook_Handle_SelfHealsWhenFileAbsent(t *testing.T) {
	t.Parallel()

	h, st := newTestHook(t)
	// cwd is a non-git temp dir, so project derivation falls back to its basename.
	cwd := filepath.Join(t.TempDir(), "myproject")
	require.NoErrorf(t, os.MkdirAll(cwd, 0o755), "create cwd")

	payload := `{"hook_event_name":"PostToolUse","session_id":"sess-1","cwd":"` + cwd + `"}`
	err := h.Run(t.Context(), strings.NewReader(payload))
	require.NoErrorf(t, err, "Run")

	got, err := st.Get("sess-1")
	require.NoErrorf(t, err, "Get")
	require.Equalf(t, "myproject", got.Project, "project should be computed once on self-heal")
	require.Equalf(t, state.Working, got.State, "state mismatch")
}

func TestHook_Handle_StatusMessage(t *testing.T) {
	t.Parallel()

	longCmd := strings.Repeat("a", 200)

	tests := []struct {
		name           string
		payload        string
		wantMessage    string
		wantTruncated  bool
		assertContains string
	}{
		{
			name:        "Bash renders tool name and command",
			payload:     `{"hook_event_name":"PermissionRequest","session_id":"s","cwd":"/tmp/p","tool_name":"Bash","tool_input":{"command":"git push origin main"}}`,
			wantMessage: "Bash: git push origin main",
		},
		{
			name:        "Edit renders tool name and path",
			payload:     `{"hook_event_name":"PermissionRequest","session_id":"s","cwd":"/tmp/p","tool_name":"Edit","tool_input":{"file_path":"/Users/x/main.go"}}`,
			wantMessage: "Edit: /Users/x/main.go",
		},
		{
			name:        "Write renders tool name and path",
			payload:     `{"hook_event_name":"PermissionRequest","session_id":"s","cwd":"/tmp/p","tool_name":"Write","tool_input":{"file_path":"/Users/x/out.txt"}}`,
			wantMessage: "Write: /Users/x/out.txt",
		},
		{
			name:        "tool with no key field renders just the tool name",
			payload:     `{"hook_event_name":"PermissionRequest","session_id":"s","cwd":"/tmp/p","tool_name":"WebFetch","tool_input":{"url":"http://x"}}`,
			wantMessage: "WebFetch",
		},
		{
			name:          "long command is capped at 80 chars",
			payload:       `{"hook_event_name":"PermissionRequest","session_id":"s","cwd":"/tmp/p","tool_name":"Bash","tool_input":{"command":"` + longCmd + `"}}`,
			wantTruncated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, st := newTestHook(t)

			err := h.Run(t.Context(), strings.NewReader(tt.payload))
			require.NoErrorf(t, err, "Run")

			got, err := st.Get("s")
			require.NoErrorf(t, err, "Get")

			if tt.wantTruncated {
				require.LessOrEqualf(t, utf8.RuneCountInString(got.StatusMessage), 80,
					"status message must be capped at 80 chars, got %d", utf8.RuneCountInString(got.StatusMessage))
				require.Truef(
					t,
					strings.HasSuffix(got.StatusMessage, "…"),
					"truncated message should end with ellipsis",
				)

				return
			}

			require.Equalf(t, tt.wantMessage, got.StatusMessage, "status message mismatch")
		})
	}
}

func TestHook_Handle_Prompt(t *testing.T) {
	t.Parallel()

	t.Run("sanitizes and caps the prompt on UserPromptSubmit", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			prompt  string
			want    string
			wantCap bool
		}{
			{
				name:   "collapses newlines and runs of whitespace to single spaces",
				prompt: "add the\n\n  cron   job\tfor VAT  ",
				want:   "add the cron job for VAT",
			},
			{
				name:    "caps a long prompt at 200 runes with an ellipsis",
				prompt:  strings.Repeat("a", 300),
				wantCap: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				h, st := newTestHook(t)

				payload, err := json.Marshal(map[string]string{
					"hook_event_name": "UserPromptSubmit",
					"session_id":      "s",
					"cwd":             "/tmp/proj",
					"prompt":          tt.prompt,
				})
				require.NoErrorf(t, err, "marshal payload")

				require.NoErrorf(t, h.Run(t.Context(), bytes.NewReader(payload)), "Run")

				got, err := st.Get("s")
				require.NoErrorf(t, err, "Get")

				if tt.wantCap {
					require.LessOrEqualf(t, utf8.RuneCountInString(got.Prompt), 200,
						"prompt must be capped at 200 runes, got %d", utf8.RuneCountInString(got.Prompt))
					require.Truef(t, strings.HasSuffix(got.Prompt, "…"), "capped prompt should end with ellipsis")

					return
				}

				require.Equalf(t, tt.want, got.Prompt, "sanitized prompt mismatch")
			})
		}
	})

	t.Run("persists across subsequent non-prompt events", func(t *testing.T) {
		t.Parallel()

		followUps := []struct {
			name    string
			payload string
		}{
			{
				name:    "PostToolUse",
				payload: `{"hook_event_name":"PostToolUse","session_id":"s","cwd":"/tmp/proj"}`,
			},
			{
				name:    "Stop",
				payload: `{"hook_event_name":"Stop","session_id":"s","cwd":"/tmp/proj"}`,
			},
			{
				name:    "PermissionRequest",
				payload: `{"hook_event_name":"PermissionRequest","session_id":"s","cwd":"/tmp/proj","tool_name":"Bash","tool_input":{"command":"ls"}}`,
			},
		}

		for _, tt := range followUps {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				h, st := newTestHook(t)

				require.NoErrorf(t, h.Run(t.Context(), strings.NewReader(
					`{"hook_event_name":"UserPromptSubmit","session_id":"s","cwd":"/tmp/proj","prompt":"refactor the store package"}`,
				)), "UserPromptSubmit")

				require.NoErrorf(t, h.Run(t.Context(), strings.NewReader(tt.payload)), "follow-up %s", tt.name)

				got, err := st.Get("s")
				require.NoErrorf(t, err, "Get")
				require.Equalf(t, "refactor the store package", got.Prompt,
					"prompt should persist through %s", tt.name)
			})
		}
	})
}

func TestHook_Handle_Branch(t *testing.T) {
	t.Parallel()

	t.Run("resolves the branch on SessionStart and UserPromptSubmit", func(t *testing.T) {
		t.Parallel()

		events := []string{"SessionStart", "UserPromptSubmit"}

		for _, event := range events {
			t.Run(event, func(t *testing.T) {
				t.Parallel()

				h, st := newTestHook(t)
				repo := initGitRepo(t, "pay-301")

				payload := `{"hook_event_name":"` + event + `","session_id":"s","cwd":"` + repo + `"}`
				require.NoErrorf(t, h.Run(t.Context(), strings.NewReader(payload)), "Run %s", event)

				got, err := st.Get("s")
				require.NoErrorf(t, err, "Get")
				require.Equalf(t, "pay-301", got.Branch, "branch should be resolved on %s", event)
			})
		}
	})

	t.Run("preserves the cached branch on PostToolUse", func(t *testing.T) {
		t.Parallel()

		h, st := newTestHook(t)
		require.NoErrorf(t, st.Save(store.Record{
			SessionID: "s",
			Project:   "instalment",
			Branch:    "pay-258",
			Cwd:       "/tmp/proj",
			State:     state.Working,
			UpdatedAt: fixedNow.Add(-time.Minute),
		}), "seed record")

		require.NoErrorf(t, h.Run(t.Context(), strings.NewReader(
			`{"hook_event_name":"PostToolUse","session_id":"s","cwd":"/tmp/proj"}`,
		)), "PostToolUse")

		got, err := st.Get("s")
		require.NoErrorf(t, err, "Get")
		require.Equalf(t, "pay-258", got.Branch, "branch must be preserved on PostToolUse, not recomputed")
	})
}

func TestHook_Handle_StatusMessageBlankForNonPermission(t *testing.T) {
	t.Parallel()

	h, st := newTestHook(t)
	// First enter permission state with a message.
	require.NoErrorf(t, h.Run(t.Context(), strings.NewReader(
		`{"hook_event_name":"PermissionRequest","session_id":"s","cwd":"/tmp/p","tool_name":"Bash","tool_input":{"command":"ls"}}`,
	)), "permission")
	// Then PostToolUse should clear it.
	require.NoErrorf(t, h.Run(t.Context(), strings.NewReader(
		`{"hook_event_name":"PostToolUse","session_id":"s","cwd":"/tmp/p"}`)), "post tool use")

	got, err := st.Get("s")
	require.NoErrorf(t, err, "Get")
	require.Emptyf(t, got.StatusMessage, "status message should be cleared outside permission state")
}

func TestGuard(t *testing.T) {
	t.Parallel()

	t.Run("recovers from panic and logs, never re-panics", func(t *testing.T) {
		t.Parallel()

		logPath := filepath.Join(t.TempDir(), "hook-errors.log")

		require.NotPanicsf(t, func() {
			guard(logPath, func() error { panic("boom") })
		}, "guard must not propagate panics")

		data, err := os.ReadFile(logPath)
		require.NoErrorf(t, err, "log file should be written")
		require.Containsf(t, string(data), "boom", "panic value should be logged")
	})

	t.Run("logs returned errors", func(t *testing.T) {
		t.Parallel()

		logPath := filepath.Join(t.TempDir(), "hook-errors.log")

		guard(logPath, func() error { return errKaboom })

		data, err := os.ReadFile(logPath)
		require.NoErrorf(t, err, "log file should be written")
		require.Containsf(t, string(data), "kaboom", "error should be logged")
	})

	t.Run("writes nothing on success", func(t *testing.T) {
		t.Parallel()

		logPath := filepath.Join(t.TempDir(), "hook-errors.log")

		guard(logPath, func() error { return nil })

		_, err := os.Stat(logPath)
		require.ErrorIsf(t, err, os.ErrNotExist, "no log file should be created on success")
	})
}
