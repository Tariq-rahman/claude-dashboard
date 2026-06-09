# Claude Instance Dashboard — Design

**Date:** 2026-06-09
**Status:** Approved (pre-implementation, revised after design grilling)

## Problem

Multiple Claude Code instances run simultaneously in IntelliJ terminal tabs on one Mac.
There is no at-a-glance way to know which instance is **waiting for input**, which is
**waiting for permission**, and which is **working**. The user loses track and has to
click through tabs to find the one that needs attention.

## Goal

A **glanceable map**, not an alerter. Surface the live state of every local Claude Code
instance as a single sorted view, so that when the user *chooses* to look, they
immediately know *which project tab* needs them and why.

Active alerting stays on the user's **existing `say` hooks** (`say task complete` on
`Stop`, `say 'waiting for permission'` on `PermissionRequest`). This tool deliberately
does **not** fire its own desktop notifications in v1 (see Non-Goals) — it is the
at-a-glance map; the `say` hooks are the audio alert channel.

Explicitly **out of scope**: programmatically focusing/jumping to an IntelliJ terminal
tab. IntelliJ does not expose individual terminal tabs to external automation (no API /
AppleScript hook), so jumping is not feasible and is not attempted. The user identifies
the tab by project name and clicks it themselves.

## Solution Shape

```
Claude Code instance ──(hooks)──> ~/.claude/dashboard/<session_id>.json ──(poll)──> TUI
```

- **Hooks** write one small JSON file per session on each lifecycle event (best-effort,
  fire-and-forget — see "Hook Robustness").
- A **Go + Bubble Tea TUI** polls that directory once a second and renders one row per
  instance, sorted by urgency. The TUI also reaps (deletes) stale files.

There is **no separate notification path** in v1 — alerting is left to the user's
existing `say` hooks.

## Distribution & Layout

Standalone Go module at `~/Projects/claude-dash`, producing a single binary with two modes:

```
claude-dash          # launches the TUI (default)
claude-dash hook     # invoked by Claude Code hooks; reads hook payload JSON on stdin
```

One binary keeps the hook writer and the TUI reader from drifting out of sync, and makes
install a single `go build`.

## State Files

One file per session: `~/.claude/dashboard/<session_id>.json`

```json
{
  "session_id": "abc123",
  "project": "instalment",
  "cwd": "/Users/tariqrahman/Projects/payments/instalment/internal/storage",
  "state": "waiting-for-permission",
  "status_message": "Bash: git push origin main",
  "updated_at": "2026-06-09T13:50:00Z"
}
```

- `state` enum: `working` | `waiting-for-input` | `waiting-for-permission`. There is no
  `idle` state — "gone quiet" is expressed by age-greying in the TUI, not a stored state.
- `project`: basename of `git rev-parse --show-toplevel`, falling back to the cwd
  basename when not in a git repo. Computed **once per session** and cached (see below) —
  `~/Projects/payments` is a directory of independent repos (not a monorepo), so the git
  root basename (`instalment`, `acc-hmrc`, …) is the distinguishing name even when the
  cwd is a subdirectory like `instalment/internal/storage`.
- `status_message`: populated **only** on `PermissionRequest` — a compact rendering of
  `tool_name` plus a truncated key field from `tool_input` (the command for `Bash`, the
  path for `Edit`/`Write`), capped at 80 chars. Blank in every other state.
- **No `pid` field.** No process-liveness check is performed; reaping is age-based only
  (see "Stale Reaping"). The Claude PID is not available in any hook payload, and
  `getppid()` is not a reliable source (hooks run in their own session without a
  controlling terminal as of v2.1.139).
- The directory `~/.claude/dashboard/` is created by the hook on first write if absent.

### Atomic writes & project caching

The hook performs a **read-modify-write** through the `store` package:

- Read the existing `<session_id>.json` if present. Preserve its `project` field, update
  `state`, `status_message`, and `updated_at`. **No git exec on the hot path**
  (`PostToolUse`, `Stop`, …).
- If the file is absent (`SessionStart`, or the hook was added mid-session so the file
  was never created), compute `project` via git once and create the file. This
  self-heals if `SessionStart` was missed.

Writes are **atomic**: write to `<session_id>.json.tmp`, then `os.Rename` into place
(rename is atomic on the same filesystem) so the polling TUI never reads a half-written
file. Read-modify-write is race-free because Claude serializes a single session's events
and one session owns exactly one file.

## Hook → State Mapping

`claude-dash hook` reads the hook JSON on stdin, switches on `hook_event_name`, and acts:

| Event              | State                     | Notes                                  |
|--------------------|---------------------------|----------------------------------------|
| `SessionStart`     | `waiting-for-input`       | creates file ("freshly opened")        |
| `UserPromptSubmit` | `working`                 |                                        |
| `PostToolUse`      | `working`                 | flips red→green after granted perm     |
| `PermissionRequest`| `waiting-for-permission`  | sets `status_message` from tool detail |
| `Stop`             | `waiting-for-input`       | fires every turn                       |
| `SessionEnd`       | (deletes file)            | clean exit only                        |

`Notification` is deliberately **not** hooked. It fires for overlapping scenarios
(`permission_prompt`, `idle_prompt`, `auth_success`, MCP elicitation), which caused the
displayed state to flap between `waiting-for-input` and `waiting-for-permission` for a
single permission prompt and produced false positives on `auth_success`. With it dropped,
`PermissionRequest` is the sole source of truth for the permission state and `Stop` owns
"waiting for input." If a future version wants an idle-timeout signal, `Notification`
disambiguated by its payload type is the place to add it.

The user's existing hooks (`say task complete` on `Stop`, `say 'waiting for permission'`
on `PermissionRequest`) remain untouched and run alongside the new `claude-dash hook`
command on the same events.

## TUI

Bubble Tea model with a 1-second ticker that re-reads `~/.claude/dashboard/` each tick.
Polling (not fsnotify) for v1: the directory is tiny and polling has no watcher edge
cases.

Rendering:

- One row per instance: `<state icon> <state label>  <project>  <status_message>  <time>`.
- **Urgency sort** — bands: `waiting-for-permission` → `waiting-for-input` → `working`.
  - Within the two waiting bands: **oldest `updated_at` first** (longest wait on top — the
    most neglected instance gets top billing).
  - Within `working`: **most-recent first** (most active on top).
- lipgloss colour coding (red = permission, amber = input, green = working; greyed when
  stale).
- **Time basis:** `now − updated_at`. For `waiting-*` states `updated_at` is not refreshed
  after entry, so this reads as "time spent waiting." For `working`, `PostToolUse`
  refreshes `updated_at`, so it reads as "time since last tool action" (liveness). No
  separate `entered_at` field is needed.
- Keys: `q` / `ctrl-c` quit; `↑`/`↓` move selection; **`d` dismisses (deletes the state
  file of) the selected row** — the manual escape hatch for zombie rows.

Example:

```
┌─ claude instances ──────────────────────────────────┐
│ ⛔ perm     gatekeeper   Bash: git push origin main  5s │
│ ⚠ input    payrun                                   2m │
│ ● working  instalment                               0s │
└──────────────────────────────────────────────────────┘
```

### Stale Reaping

Reaping is purely **age-based** (`now − updated_at`) and **state-aware**. Because `Stop`
and `PostToolUse` refresh `updated_at` constantly while an instance is alive, a row only
ages if the instance is genuinely silent (the user walked away) or crashed.

| State                     | Grey (de-emphasise) | Drop + delete file        |
|---------------------------|---------------------|---------------------------|
| `waiting-for-permission`  | —                   | **never** (manual `d` only)|
| `waiting-for-input`       | > 10 min            | > 1 h                     |
| `working`                 | > 10 min ("hung?")  | > 1 h                     |

- Permission rows are **never auto-dropped**: a blocking, needs-you-now state must not
  silently disappear just because the user was away. The cost is that a crash *during* a
  permission prompt (no `SessionEnd`) leaves a permanent red zombie — cleared manually
  with `d`. Accepted tradeoff.
- Dropping is **non-destructive**: thanks to the hook's self-healing read-modify-write, a
  live instance whose file was deleted recreates it on its next event.

Thresholds (10 min / 1 h) are config constants.

## Hook Robustness

The hook now runs on six events including the high-frequency `PostToolUse`. It is
**fire-and-forget and must never break the user's real session**:

- `main` wraps everything in a recover; all internal errors (dashboard dir unwritable,
  malformed payload, etc.) are logged to a file under `~/.claude/dashboard/`, **never to
  stderr**, and the process **always `os.Exit(0)`**. A dashboard failure must never block
  a tool call or surface as a hook error.
- If the binary is entirely absent (`command not found`), that is harmless shell noise; a
  one-line install-check note in the README avoids chasing phantom errors.
- The **TUI**, by contrast, fails loudly — it is the foreground app. This asymmetry is
  intentional.

## Component Boundaries

| Unit              | Responsibility                                          | Depends on            |
|-------------------|---------------------------------------------------------|-----------------------|
| `state` package   | State enum, event→state mapping (pure)                  | —                     |
| `store` package   | Atomic read-modify-write / read / delete; list states   | filesystem            |
| `project` helper  | Derive project name from cwd / git root (pure logic)    | exec git (injectable) |
| `hook` command    | Parse stdin payload, map event, RMW store               | store, state, project |
| `tui` package     | Bubble Tea model: poll store, sort, render, reap, dismiss| store                 |

The **only** external boundary behind an interface is **git exec** in the `project`
helper. The `notify`/`Notifier` and `proc`/`processChecker` units from the earlier draft
are removed (no desktop notifications, no pid checks).

## Testing

Per project standards: table-driven, `t.Parallel()` on every test and subtest, formatted
assertions (`require.*f`). The only mockery-generated mock is for the **git-exec
boundary** in the `project` helper; everything else is real filesystem (`t.TempDir()`) or
pure logic — no business logic is mocked.

- `state`: event→state mapping table test.
- `project`: cwd/git-root → name derivation table test, git exec mocked.
- `store`: round-trip read-modify-write / read / delete against a `t.TempDir()`,
  including atomic-rename behaviour and project-field preservation.
- `tui`: urgency sort table test (band order + within-band direction); `Update` driven by
  injected tick/key messages with asserted model state; state-aware reaping (grey/drop
  thresholds, permission never dropped) driven by crafted `updated_at` values; `d`-key
  dismiss deletes the selected file.
- `hook`: payload parsing + dispatch per event, asserting store writes; `status_message`
  population + 80-char truncation on `PermissionRequest`; always-exit-0 / panic-recovery
  behaviour.

## Edge Cases

- **Crashed instance** (no `SessionEnd`): aged out by state-aware reaping; permission
  zombies cleared manually with `d`.
- **Hook added mid-session** (no `SessionStart` file): first event recreates the file and
  computes `project` once (self-heal).
- **Duplicate project** (same repo in two terminals): two rows; a short `session_id`
  suffix disambiguates.
- **Missing git repo**: project name falls back to cwd basename.
- **First run**: hook creates `~/.claude/dashboard/` if it does not exist.
- **Half-written file**: prevented by atomic `.tmp` + rename.
- **Hook failure** (disk full, bad payload): swallowed, logged to file, exit 0 — never
  affects the session.

## Manual Step (settings.json)

Claude cannot edit `~/.claude/settings.json` (blocked by the user's own deny rule). Hook
registration is therefore a manual paste or done via the `/update-config` skill. The hook
block adds `claude-dash hook` to **`SessionStart`, `UserPromptSubmit`, `PostToolUse`,
`PermissionRequest`, `Stop`, and `SessionEnd`** (note: **not** `Notification`), alongside
the existing `say` commands and other hooks already on those events. The exact JSON block
will be provided with the implementation.

## Non-Goals (YAGNI)

- No jumping/focusing of IntelliJ tabs.
- **No desktop notifications in v1** — passive map only; alerting stays on existing `say`
  hooks. May be added later via `Notification` disambiguated by payload type.
- No pid/process-liveness checks — age-based reaping only.
- No remote/cloud instances (all instances are local to this Mac).
- No web UI or menu-bar app.
- No fsnotify (polling is sufficient for v1).
- No persistence/history — state is ephemeral and reflects the live moment only.
