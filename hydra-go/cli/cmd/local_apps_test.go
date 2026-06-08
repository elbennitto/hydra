package cmd

import (
	"bytes"
	"testing"

	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalAppsCommandParsesFlagsAndPrintsSortedApps(t *testing.T) {
	var captured *action.AppsFlags

	cmd := newLocalAppsCommand(func(flags action.AppsFlags) ([]types.AppId, error) {
		captured = &flags
		return []types.AppId{"prod.zeta", "prod.alpha"}, nil
	})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"prod.**",
		"--hydra-context", "/tmp/hydra-context",
		"--helm-network-mode", "local",
		"--exclude-app", "prod.infra.argocd",
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)

	assert.Equal(t, []types.AppIdPattern{"prod.**"}, captured.AppIdPatterns)
	assert.Equal(t, types.HydraContext("/tmp/hydra-context"), captured.HydraContext)
	assert.Equal(t, types.HelmNetworkModeLocal, captured.HelmNetworkMode)
	assert.Equal(t, []types.AppIdPattern{"prod.infra.argocd"}, captured.ExcludeAppPatterns)
	assert.Equal(t, "prod.alpha\nprod.zeta\n", out.String())
}

func TestLocalAppsCommandUsesAllAppsPatternWhenNoArgsProvided(t *testing.T) {
	var captured *action.AppsFlags

	cmd := newLocalAppsCommand(func(flags action.AppsFlags) ([]types.AppId, error) {
		captured = &flags
		return nil, nil
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, []types.AppIdPattern{"**"}, captured.AppIdPatterns)
}
