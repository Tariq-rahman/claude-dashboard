// Command claude-dash is a glanceable map of every local Claude Code instance.
//
//	claude-dash         launches the TUI (default)
//	claude-dash hook    invoked by Claude Code hooks; reads the payload on stdin
//
// One binary keeps the hook writer and the TUI reader from drifting out of sync.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tariqrahman/claude-dash/internal/hook"
	"github.com/tariqrahman/claude-dash/internal/project"
	"github.com/tariqrahman/claude-dash/internal/store"
	"github.com/tariqrahman/claude-dash/internal/tui"
)

func main() {
	dir := dashboardDir()

	if len(os.Args) > 1 && os.Args[1] == "hook" {
		runHook(dir)
		return
	}

	runTUI(dir)
}

// dashboardDir is ~/.claude/dashboard, falling back to ./.claude/dashboard if
// the home directory cannot be resolved.
func dashboardDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	return filepath.Join(home, ".claude", "dashboard")
}

// runHook applies a single hook event and always exits 0 — fire-and-forget.
func runHook(dir string) {
	h := hook.New(
		store.New(dir),
		project.NewResolver(project.NewExecRunner()),
		func() time.Time { return time.Now().UTC() },
	)

	hook.Execute(context.Background(), os.Stdin, h, filepath.Join(dir, "hook-errors.log"))
	os.Exit(0)
}

// runTUI launches the dashboard. Unlike the hook, it fails loudly.
func runTUI(dir string) {
	model := tui.New(store.New(dir), tui.DefaultConfig())

	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "claude-dash:", err)
		os.Exit(1)
	}
}
