# claude-dash

A glanceable map of every local Claude Code instance. When multiple Claude Code
sessions run in separate terminal tabs, `claude-dash` shows — in one sorted view
— which instance is **waiting for permission**, which is **waiting for input**,
and which is **working**, so you immediately know *which project tab* needs you.

It is a passive map, not an alerter. Active audio alerting stays on your existing
`say` hooks; `claude-dash` does not fire its own notifications.

```
┌─ claude instances ──────────────────────────────────┐
│ ⛔ perm     gatekeeper   Bash: git push origin main  5s │
│ ⚠ input    payrun                                   2m │
│ ● working  instalment                               0s │
└──────────────────────────────────────────────────────┘
```

## How it works

```
Claude Code ──(hooks)──> ~/.claude/dashboard/<session_id>.json ──(poll)──> TUI
```

- On each lifecycle event, Claude Code runs `claude-dashboard hook`, which writes one
  small JSON file per session (atomically, fire-and-forget).
- The TUI polls `~/.claude/dashboard/` once a second, renders one row per
  instance sorted by urgency, greys rows that have gone quiet, and reaps stale
  files.

One binary, two modes:

```
claude-dashboard         # launches the TUI (default)
claude-dashboard hook    # invoked by Claude Code hooks; reads the payload on stdin
```

## Install

```sh
go install github.com/Tariq-rahman/claude-dashboard@latest
```

This installs the binary to `$(go env GOPATH)/bin/claude-dashboard`
(`/Users/tariqrahman/go/bin/claude-dashboard`). Either put that directory on your
`PATH`, or reference the absolute path in the hook config below.

## Run

```sh
claude-dashboard
```

Keys:

| Key            | Action                                            |
|----------------|---------------------------------------------------|
| `↑` / `↓` (or `k`/`j`) | Move the selection                        |
| `d`            | Dismiss (delete the state file of) the selected row — the manual escape hatch for zombie rows |
| `q` / `ctrl-c` | Quit                                              |

Sorting: `waiting-for-permission` → `waiting-for-input` → `working`. Within the
waiting bands the longest wait is on top; within `working` the most recently
active is on top. Rows grey out after 10 minutes of silence and are dropped
after 1 hour — except permission rows, which are never auto-dropped (clear them
with `d`).

## Hook registration (manual step)

Claude cannot edit `~/.claude/settings.json` (it is behind a deny rule), so add
the hook block yourself. Register `claude-dashboard hook` on **six** events —
`SessionStart`, `UserPromptSubmit`, `PostToolUse`, `PermissionRequest`, `Stop`,
and `SessionEnd` (deliberately **not** `Notification`) — alongside any existing
`say` commands on those events.

A ready-to-merge block is in [`docs/settings-hooks.json`](docs/settings-hooks.json).
If `claude-dashboard` is not on the `PATH` your hooks run with, replace the command
with the absolute path `/Users/tariqrahman/go/bin/claude-dashboard hook`.

> **Install check:** if the binary is missing, Claude Code hooks just print
> harmless `command not found` shell noise — they never block a tool call. If
> you see that, the binary isn't installed or isn't on the hook shell's `PATH`.

## Design

See [`docs/specs/2026-06-09-claude-instance-dashboard-design.md`](docs/specs/2026-06-09-claude-instance-dashboard-design.md).
