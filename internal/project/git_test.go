package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecRunner_ShowTopLevel(t *testing.T) {
	t.Parallel()

	t.Run("returns working-tree root inside a repo", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		gitInit(t, root)

		sub := filepath.Join(root, "internal", "storage")
		require.NoErrorf(t, os.MkdirAll(sub, 0o755), "creating subdir")

		runner := NewExecRunner()
		got, err := runner.ShowTopLevel(t.Context(), sub)

		require.NoErrorf(t, err, "ShowTopLevel from subdir")
		// ShowTopLevel returns raw git output (the Resolver trims); strip the
		// trailing newline before resolving the path.
		// macOS /var is a symlink to /private/var, so resolve before comparing.
		gotResolved, err := filepath.EvalSymlinks(strings.TrimSpace(got))
		require.NoErrorf(t, err, "resolving returned root")
		wantResolved, err := filepath.EvalSymlinks(root)
		require.NoErrorf(t, err, "resolving want root")
		require.Equalf(t, wantResolved, gotResolved, "git root mismatch")
	})

	t.Run("returns error outside a repo", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		runner := NewExecRunner()
		_, err := runner.ShowTopLevel(t.Context(), dir)

		require.Errorf(t, err, "expected error outside a git repo")
	})
}

func gitInit(t *testing.T, dir string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", "init")
	cmd.Dir = dir
	require.NoErrorf(t, cmd.Run(), "git init in %s", dir)
}
