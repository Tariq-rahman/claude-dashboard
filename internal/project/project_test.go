package project

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var errNoRepo = errors.New("not a git repository")

func TestResolver_GetName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cwd       string
		mockSetup func(m *MockRunner)
		want      string
	}{
		{
			name: "uses git root basename when in a repo",
			cwd:  "/Users/tariqrahman/Projects/payments/instalment/internal/storage",
			mockSetup: func(m *MockRunner) {
				m.EXPECT().
					ShowTopLevel(t.Context(), "/Users/tariqrahman/Projects/payments/instalment/internal/storage").
					Return("/Users/tariqrahman/Projects/payments/instalment", nil)
			},
			want: "instalment",
		},
		{
			name: "trims trailing newline from git output",
			cwd:  "/Users/tariqrahman/Projects/payments/acc-hmrc",
			mockSetup: func(m *MockRunner) {
				m.EXPECT().
					ShowTopLevel(t.Context(), "/Users/tariqrahman/Projects/payments/acc-hmrc").
					Return("/Users/tariqrahman/Projects/payments/acc-hmrc\n", nil)
			},
			want: "acc-hmrc",
		},
		{
			name: "falls back to cwd basename when not in a repo",
			cwd:  "/Users/tariqrahman/scratch/notes",
			mockSetup: func(m *MockRunner) {
				m.EXPECT().
					ShowTopLevel(t.Context(), "/Users/tariqrahman/scratch/notes").
					Return("", errNoRepo)
			},
			want: "notes",
		},
		{
			name: "falls back to cwd basename when git returns blank",
			cwd:  "/Users/tariqrahman/scratch/notes",
			mockSetup: func(m *MockRunner) {
				m.EXPECT().
					ShowTopLevel(t.Context(), "/Users/tariqrahman/scratch/notes").
					Return("   \n", nil)
			},
			want: "notes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockRunner := NewMockRunner(t)
			tt.mockSetup(mockRunner)

			resolver := NewResolver(mockRunner)
			got := resolver.GetName(t.Context(), tt.cwd)

			require.Equalf(t, tt.want, got, "project name mismatch for cwd %q", tt.cwd)
		})
	}
}
