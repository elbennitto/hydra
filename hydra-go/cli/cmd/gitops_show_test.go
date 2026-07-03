package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

func TestClusterShowCommandRegistersFlags(t *testing.T) {
	cmd := NewClusterCommand(ClusterCommandParams{})

	show := childCommandWithUse(cmd, "show")
	require.NotNil(t, show, "expected hydra gitops show to exist")
	assert.Equal(t, "show <cluster>", show.Use)

	assert.NotNil(t, show.Flags().Lookup("parallel"))
	assert.NotNil(t, show.Flags().Lookup("exclude-app"))
	assert.NotNil(t, show.Flags().Lookup("color"))
	assert.NotNil(t, show.Flags().Lookup("no-color"))
	assert.NotNil(t, show.Flags().Lookup("color-mode"))
	assert.NotNil(t, show.Flags().Lookup("builtin"))
	assert.NotNil(t, show.Flags().Lookup("yaml"))
}

func TestClusterShowCommand_ParsesBuiltinFlag(t *testing.T) {
	var captured *action.ClusterShowFlags

	cmd := NewClusterShowCommand(func(flags action.ClusterShowFlags) (hydra.Hydra, string, error) {
		captured = &flags
		return nil, "", nil
	})

	cmd.SetArgs([]string{"test-cluster", "--hydra-context", "/tmp/hydra-context"})
	require.NoError(t, cmd.Execute())
	require.NotNil(t, captured)
	assert.False(t, captured.Builtin)

	captured = nil
	cmd = NewClusterShowCommand(func(flags action.ClusterShowFlags) (hydra.Hydra, string, error) {
		captured = &flags
		return nil, "", nil
	})

	cmd.SetArgs([]string{"test-cluster", "--hydra-context", "/tmp/hydra-context", "--builtin"})
	require.NoError(t, cmd.Execute())
	require.NotNil(t, captured)
	assert.True(t, captured.Builtin)
}
