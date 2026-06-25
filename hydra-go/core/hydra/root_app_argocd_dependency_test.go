package hydra

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestRootApp_IsManagedByInClusterArgoCDFallsBackToDependencyHydraValues(t *testing.T) {
	resetCaches()
	rootApp := newTestRootAppForArgoCDFlag(t, "global:\n  hydra:\n    type: root-app\nplatform:\n  global:\n    hydra:\n      argocd: true\n")

	managed, err := rootApp.IsManagedByInClusterArgoCD(types.HelmNetworkModeOffline)
	require.NoError(t, err)
	assert.True(t, managed)
}

func TestRootApp_IsManagedByInClusterArgoCDPrefersTopLevelHydraValues(t *testing.T) {
	resetCaches()
	rootApp := newTestRootAppForArgoCDFlag(t, "global:\n  hydra:\n    type: root-app\n    argocd: false\nplatform:\n  global:\n    hydra:\n      argocd: true\n")

	managed, err := rootApp.IsManagedByInClusterArgoCD(types.HelmNetworkModeOffline)
	require.NoError(t, err)
	assert.False(t, managed)
}

func newTestRootAppForArgoCDFlag(t *testing.T, rootValues string) *RootApp {
	t.Helper()

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "values.yaml"), []byte("global:\n  hydra:\n    type: context\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "target", "platform"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "target", "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "target", "platform", "values.yaml"), []byte(rootValues), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "target", "platform", "Chart.yaml"), []byte("apiVersion: v2\nname: platform\nversion: 0.1.0\n"), 0o644))

	cfg := types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true)
	ctx, err := NewContext(log.Default(), types.ContextPath(tmp), cfg)
	require.NoError(t, err)
	cluster, err := NewCluster(ctx, "target", RESTClientLimits{})
	require.NoError(t, err)
	rootApp, err := NewRootApp(cluster, "platform")
	require.NoError(t, err)
	return rootApp
}
