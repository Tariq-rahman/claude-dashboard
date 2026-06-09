package state

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFromEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		event     string
		wantState State
		wantOK    bool
	}{
		{
			name:      "SessionStart maps to waiting-for-input",
			event:     "SessionStart",
			wantState: WaitingForInput,
			wantOK:    true,
		},
		{
			name:      "UserPromptSubmit maps to working",
			event:     "UserPromptSubmit",
			wantState: Working,
			wantOK:    true,
		},
		{
			name:      "PostToolUse maps to working",
			event:     "PostToolUse",
			wantState: Working,
			wantOK:    true,
		},
		{
			name:      "PermissionRequest maps to waiting-for-permission",
			event:     "PermissionRequest",
			wantState: WaitingForPermission,
			wantOK:    true,
		},
		{
			name:      "Stop maps to waiting-for-input",
			event:     "Stop",
			wantState: WaitingForInput,
			wantOK:    true,
		},
		{
			name:      "unknown event is not mapped",
			event:     "Notification",
			wantState: "",
			wantOK:    false,
		},
		{
			name:      "empty event is not mapped",
			event:     "",
			wantState: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := FromEvent(tt.event)
			require.Equalf(t, tt.wantOK, ok, "ok mismatch for event %q", tt.event)
			require.Equalf(t, tt.wantState, got, "state mismatch for event %q", tt.event)
		})
	}
}

func TestStateString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state State
		want  string
	}{
		{name: "working", state: Working, want: "working"},
		{name: "waiting-for-input", state: WaitingForInput, want: "waiting-for-input"},
		{name: "waiting-for-permission", state: WaitingForPermission, want: "waiting-for-permission"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equalf(t, tt.want, string(tt.state), "string value mismatch")
		})
	}
}
