package hydra

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
)

// exampleDevClusterContextPath resolves gitops-repository/clusters/test/example-dev for integration tests.
func exampleDevClusterContextPath(t *testing.T) types.ContextPath {
	t.Helper()
	candidates := []string{}
	if root := os.Getenv("HYDRA_GITOPS_FIXTURE_ROOT"); root != "" {
		candidates = append(candidates, filepath.Join(root, "gitops-repository", "clusters", "test", "example-dev"))
	}
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; ; dir = filepath.Dir(dir) {
			candidates = append(candidates, filepath.Join(dir, "gitops-repository", "clusters", "test", "example-dev"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return types.ContextPath(p)
		}
	}
	t.Skip("example-dev gitops fixture not available (set HYDRA_GITOPS_FIXTURE_ROOT)")
	return ""
}

// Validates LoadValuesFromRootApp carries umbrella global.baseUrl into the child merge (example-dev demo app).
func TestLoadValuesFromRootApp_ExampleDevServiceAuth_HasGlobalBaseUrl(t *testing.T) {
	t.Parallel()
	cfg := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, false)
	ctx, err := NewContext(log.Default(), exampleDevClusterContextPath(t), cfg)
	require.NoError(t, err)
	c, err := ctx.WithCluster(types.InCluster, RESTClientLimits{})
	require.NoError(t, err)
	h, err := c.WithApp(types.AppId("in-cluster.demo.service-auth"))
	require.NoError(t, err)
	ca := h.AsChildApp()
	require.NotNil(t, ca)
	raw, _, err := ca.LoadValuesFromRootApp(types.HelmNetworkModeOnline)
	require.NoError(t, err)
	u := values.Lookup(raw, "global", "baseUrl")
	require.NotEmpty(t, u, "LoadValuesFromRootApp must propagate umbrella global.baseUrl; keys: %#v", values.Lookup(raw, "global"))
}
