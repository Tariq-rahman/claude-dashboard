package project

import (
	"context"
	"fmt"
	"os/exec"
)

// ExecRunner is the production Runner: it shells out to the git binary.
type ExecRunner struct{}

// NewExecRunner returns a Runner backed by the system git binary.
func NewExecRunner() *ExecRunner {
	return &ExecRunner{}
}

// ShowTopLevel runs `git -C dir rev-parse --show-toplevel`.
func (ExecRunner) ShowTopLevel(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel in %s: %w", dir, err)
	}

	return string(out), nil
}

// CurrentBranch runs `git -C dir rev-parse --abbrev-ref HEAD`.
func (ExecRunner) CurrentBranch(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD in %s: %w", dir, err)
	}

	return string(out), nil
}
