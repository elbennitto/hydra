package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/utils"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestReviewCommandShape(t *testing.T) {
	cmd := NewReviewCommand(ReviewCommandParams{
		ReviewRefs: func(flags action.ReviewRefsFlags) error {
			return nil
		},
	})

	require.NotNil(t, cmd)
	assert.Equal(t, "review <appId...>", cmd.Use)
	require.Empty(t, cmd.Commands())
}

func TestReviewCommandParsesFlags(t *testing.T) {
	var captured *action.ReviewRefsFlags

	cmd := NewReviewCommand(ReviewCommandParams{
		ReviewRefs: func(flags action.ReviewRefsFlags) error {
			captured = &flags
			return nil
		},
	})

	defer utils.EnvWrapper("HYDRA_CONTEXT", "/tmp/hydra-context")()

	cmd.SetArgs([]string{
		"prod.*.*",
		"--hydra-context", "/tmp/hydra-context",
		"--helm-network-mode", "local",
		"--exclude-app", "prod.platform.skip",
		"--color",
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, []types.AppIdPattern{"prod.*.*"}, captured.AppIdPatterns)
	assert.Equal(t, types.HydraContext("/tmp/hydra-context"), captured.HydraContext)
	assert.Equal(t, types.HelmNetworkModeLocal, captured.HelmNetworkMode)
	assert.Equal(t, []types.AppIdPattern{"prod.platform.skip"}, captured.ExcludeAppPatterns)
	assert.True(t, bool(captured.Color))
}

func TestReviewCommandRegistersLocalReviewFlags(t *testing.T) {
	cmd := NewReviewCommand(ReviewCommandParams{
		ReviewRefs: func(flags action.ReviewRefsFlags) error {
			return nil
		},
	})

	assert.NotNil(t, cmd.Flags().Lookup("hydra-context"))
	assert.NotNil(t, cmd.Flags().Lookup("helm-network-mode"))
	assert.NotNil(t, cmd.Flags().Lookup("exclude-app"))
	assert.NotNil(t, cmd.Flags().Lookup("color"))
	assert.NotNil(t, cmd.Flags().Lookup("no-color"))
	assert.NotNil(t, cmd.Flags().Lookup("color-mode"))
	assert.NotNil(t, cmd.Flags().Lookup("yaml"))
	assert.Nil(t, cmd.Flags().Lookup("cluster"), "hydra local review should stay app-selection based and local")
}
