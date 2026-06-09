// Package state defines the Claude Code instance lifecycle states and the
// pure mapping from hook event names to those states.
package state

// State is the displayed lifecycle state of a Claude Code instance.
//
// There is deliberately no "idle" state: an instance that has gone quiet is
// expressed by age-greying in the TUI, not by a stored state.
type State string

const (
	// Working means the instance is actively processing.
	Working State = "working"
	// WaitingForInput means the instance is waiting for the user to type.
	WaitingForInput State = "waiting-for-input"
	// WaitingForPermission means the instance is blocked on a permission prompt.
	WaitingForPermission State = "waiting-for-permission"
)

// FromEvent maps a Claude Code hook event name to the state it implies.
//
// The boolean reports whether the event is one we act on. Events we do not
// hook (notably "Notification" and "SessionEnd", which deletes rather than
// writes) return ("", false).
func FromEvent(event string) (State, bool) {
	switch event {
	// SessionStart ("freshly opened") and Stop ("turn finished") both land on
	// waiting-for-input.
	case "SessionStart", "Stop":
		return WaitingForInput, true
	case "UserPromptSubmit", "PostToolUse":
		return Working, true
	case "PermissionRequest":
		return WaitingForPermission, true
	default:
		return "", false
	}
}
