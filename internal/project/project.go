// Package project derives a human-friendly project name for a Claude Code
// session from its working directory, using the git working-tree root when
// available. Git execution sits behind the Runner interface — the single
// external boundary in claude-dash — so the derivation logic is pure and
// testable.
package project

import (
	"context"
	"path/filepath"
	"strings"
)

// Runner executes the git queries project derivation needs. It is kept behind
// an interface so name derivation can be exercised without a real repository.
type Runner interface {
	// ShowTopLevel returns the absolute path of the git working-tree root for
	// dir, equivalent to `git -C dir rev-parse --show-toplevel`. It returns a
	// non-nil error when dir is not inside a git repository.
	ShowTopLevel(ctx context.Context, dir string) (string, error)
	// CurrentBranch returns the abbreviated branch name for dir, equivalent to
	// `git -C dir rev-parse --abbrev-ref HEAD`. Returns a non-nil error when dir
	// is not inside a git repository.
	CurrentBranch(ctx context.Context, dir string) (string, error)
}

// Resolver derives project names using an injected git Runner.
type Resolver struct {
	git Runner
}

// NewResolver returns a Resolver backed by the given git Runner.
func NewResolver(git Runner) *Resolver {
	return &Resolver{git: git}
}

// GetName derives the project name for cwd: the basename of the git working
// tree root, falling back to the basename of cwd when cwd is not inside a git
// repository (or git yields a blank root).
func (r *Resolver) GetName(ctx context.Context, cwd string) string {
	if root, err := r.git.ShowTopLevel(ctx, cwd); err == nil {
		if root = strings.TrimSpace(root); root != "" {
			return filepath.Base(root)
		}
	}

	return filepath.Base(cwd)
}

// GetBranch returns the git branch for cwd. It returns "" when cwd is not inside
// a git repository or git yields a blank branch. A detached HEAD prints "HEAD",
// which is kept as-is.
func (r *Resolver) GetBranch(ctx context.Context, cwd string) string {
	branch, err := r.git.CurrentBranch(ctx, cwd)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(branch)
}
