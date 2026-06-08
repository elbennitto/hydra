package cmd

import (
	"bytes"
	"testing"

	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterCommandContainsAppsSubcommand(t *testing.T) {
	mock := newMockRootCommand()
	rootCmd, _ := newRootCommand(mock.rootCommandParams())

	require.NotNil(t, rootCmd)

	appsCmd := commandByUsePath(rootCmd, "gitops", "apps")
	require.NotNil(t, appsCmd, "expected hydra gitops apps to exist")
	assert.Equal(t, "apps [appId...]", appsCmd.Use)
}

func TestClusterAppsCommandParsesFlagsAndPrintsSortedApps(t *testing.T) {
	var captured *action.AppsFlags

	cmd := NewClusterAppsCommand(func(flags action.AppsFlags) ([]types.AppId, error) {
		captured = &flags
		return []types.AppId{"in-cluster.ops", "in-cluster.apps.argocd"}, nil
	})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"in-cluster.**",
		"--hydra-context", "/tmp/hydra-context",
		"--helm-network-mode", "offline",
		"--exclude-app", "in-cluster.ops",
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)

	assert.Equal(t, []types.AppIdPattern{"in-cluster.**"}, captured.AppIdPatterns)
	assert.Equal(t, types.HydraContext("/tmp/hydra-context"), captured.HydraContext)
	assert.Equal(t, types.HelmNetworkModeOffline, captured.HelmNetworkMode)
	assert.Equal(t, []types.AppIdPattern{"in-cluster.ops"}, captured.ExcludeAppPatterns)
	assert.Equal(t, "in-cluster.apps.argocd\nin-cluster.ops\n", out.String())
}

func TestClusterAppsCommandUsesAllAppsPatternWhenNoArgsProvided(t *testing.T) {
	var captured *action.AppsFlags

	cmd := NewClusterAppsCommand(func(flags action.AppsFlags) ([]types.AppId, error) {
		captured = &flags
		return nil, nil
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, []types.AppIdPattern{"**"}, captured.AppIdPatterns)
}
