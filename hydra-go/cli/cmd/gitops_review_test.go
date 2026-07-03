package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/utils"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestClusterCommandContainsReviewCommand(t *testing.T) {
	cmd := NewClusterCommand(ClusterCommandParams{})

	reviewParent := childCommandWithUse(cmd, "review")
	require.NotNil(t, reviewParent, "expected hydra gitops review to exist")
	assert.Equal(t, "review", reviewParent.Use)

	appCmd := childCommandWithUse(reviewParent, "app")
	require.NotNil(t, appCmd, "expected hydra gitops review app")
	assert.Equal(t, "app <appId...>", appCmd.Use)

	clusterCmd := childCommandWithUse(reviewParent, "cluster")
	require.NotNil(t, clusterCmd, "expected hydra gitops review cluster")
	assert.Equal(t, "cluster <cluster>", clusterCmd.Use)
}

func TestClusterReviewCommandRegistersExpectedFlags(t *testing.T) {
	cmd := NewClusterCommand(ClusterCommandParams{})

	reviewParent := childCommandWithUse(cmd, "review")
	require.NotNil(t, reviewParent, "expected hydra gitops review to exist")

	appCmd := childCommandWithUse(reviewParent, "app")
	require.NotNil(t, appCmd)

	assert.NotNil(t, appCmd.Flags().Lookup("hydra-context"))
	assert.NotNil(t, appCmd.Flags().Lookup("helm-network-mode"))
	assert.NotNil(t, appCmd.Flags().Lookup("exclude-app"))
	assert.NotNil(t, appCmd.Flags().Lookup("color"))
	assert.NotNil(t, appCmd.Flags().Lookup("no-color"))
	assert.NotNil(t, appCmd.Flags().Lookup("color-mode"))
	assert.NotNil(t, appCmd.Flags().Lookup("yaml"))
	assert.NotNil(t, appCmd.Flags().Lookup("parallel"))
	assert.Nil(t, appCmd.Flags().Lookup("cluster"), "cluster review should derive the cluster from the selected app IDs or cluster subcommand")
}

func TestClusterReviewCommandParsesColorFlags(t *testing.T) {
	var captured *action.ReviewRefsFlags

	cmd := NewClusterReviewCommand(func(flags action.ReviewRefsFlags) error {
		captured = &flags
		return nil
	})

	defer utils.EnvWrapper("HYDRA_CONTEXT", "/tmp/hydra-context")()

	cmd.SetArgs([]string{
		"app", "prod.*.*",
		"--hydra-context", "/tmp/hydra-context",
		"--helm-network-mode", "local",
		"--no-color",
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, []types.AppIdPattern{"prod.*.*"}, captured.AppIdPatterns)
	assert.False(t, bool(captured.Color))
	assert.Empty(t, captured.ClusterReviewClusterName)
}

func TestClusterReviewClusterSubcommandSetsClusterName(t *testing.T) {
	var captured *action.ReviewRefsFlags

	cmd := NewClusterReviewCommand(func(flags action.ReviewRefsFlags) error {
		captured = &flags
		return nil
	})

	defer utils.EnvWrapper("HYDRA_CONTEXT", "/tmp/hydra-context")()

	cmd.SetArgs([]string{
		"cluster", "prod",
		"--hydra-context", "/tmp/hydra-context",
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "prod", captured.ClusterReviewClusterName)
	assert.Empty(t, captured.AppIdPatterns)
}

func childCommandWithUse(cmd *cobra.Command, use string) *cobra.Command {
	for _, child := range cmd.Commands() {
		if child.Use == use || strings.HasPrefix(child.Use, use+" ") {
			return child
		}
	}
	return nil
}

func childCommandWithUsePrefix(cmd *cobra.Command, prefix string) *cobra.Command {
	for _, child := range cmd.Commands() {
		if strings.HasPrefix(child.Use, prefix) {
			return child
		}
	}
	return nil
}
