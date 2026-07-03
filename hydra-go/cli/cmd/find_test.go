package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/utils"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestFindCommandDefaultsPickToId(t *testing.T) {
	var captured *action.FindFlags

	cmd := newFindCommand(func(flags action.FindFlags) (hydra.Hydra, string, error) {
		captured = &flags
		return nil, "[]", nil
	})

	defer utils.EnvWrapper("HYDRA_CONTEXT", "/tmp/hydra-context")()

	cmd.SetArgs([]string{"prod.*.*"})
	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, types.CelExpression("id"), captured.Pick)
}

func TestFindCommandParsesFlags(t *testing.T) {
	var captured *action.FindFlags

	cmd := newFindCommand(func(flags action.FindFlags) (hydra.Hydra, string, error) {
		captured = &flags
		return nil, "[]", nil
	})

	cmd.SetArgs([]string{
		"prod.*.*",
		"--hydra-context", "/tmp/hydra-context",
		"--helm-network-mode", "local",
		"--include", `kind == "KafkaUser"`,
		"--exclude", `ns == "kube-system"`,
		"--exclude-app", "prod.cluster-infra.cert-manager",
		"--pick", "appIds[0]",
		"--uniq",
	})

	err := cmd.Execute()
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, []types.AppIdPattern{"prod.*.*"}, captured.AppIdPatterns)
	assert.Equal(t, types.HydraContext("/tmp/hydra-context"), captured.HydraContext)
	assert.Equal(t, types.HelmNetworkModeLocal, captured.HelmNetworkMode)
	assert.ElementsMatch(t, []types.CelPredicate{
		`kind == "KafkaUser"`,
		`!(ns == "kube-system")`,
	}, captured.Predicates)
	assert.Equal(t, []types.AppIdPattern{"prod.cluster-infra.cert-manager"}, captured.ExcludeAppPatterns)
	assert.Equal(t, types.CelExpression("appIds[0]"), captured.Pick)
	assert.True(t, captured.Uniq)
}
