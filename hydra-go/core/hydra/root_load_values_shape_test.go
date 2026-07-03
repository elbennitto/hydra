package hydra

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
)

// TestRootLoadValuesMap_IncludesGlobalBaseUrlForExampleDevDemo checks that merged umbrella
// values include a top-level global suitable for child extraction (covers regression
// where global only appeared under infra_chart-specific keys).
func TestRootLoadValuesMap_IncludesGlobalBaseUrlForExampleDevDemo(t *testing.T) {
	t.Parallel()
	cfg := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, false)
	ctx, err := NewContext(log.Default(), exampleDevClusterContextPath(t), cfg)
	require.NoError(t, err)
	c, err := ctx.WithCluster(types.InCluster, RESTClientLimits{})
	require.NoError(t, err)
	ha, err := c.WithApp(types.AppId("in-cluster.demo"))
	require.NoError(t, err)
	ra := ha.AsRootApp()
	full, err := ra.LoadValuesMap(types.HelmNetworkModeOnline)
	require.NoError(t, err)
	g1 := values.Lookup(full, "global", "baseUrl")
	t.Logf("top global.baseUrl = %v", g1)
	g2 := values.Lookup(full, "root", "global", "baseUrl")
	t.Logf("root.global.baseUrl = %v", g2)
	require.NotEmpty(t, g1, "expected top-level global.baseUrl from merged Demo umbrella values")
}
