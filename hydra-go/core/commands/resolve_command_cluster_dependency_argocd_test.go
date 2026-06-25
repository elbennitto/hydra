package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestResolveCommandCluster_UsesInClusterForDependencyDefinedArgoCDManagedRootApp(t *testing.T) {
	ResolveCommandClusterFunc = resolveCommandClusterDefault
	t.Cleanup(func() {
		ResolveCommandClusterFunc = resolveCommandClusterDefault
	})

	contextDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "values.yaml"), []byte("global:\n  hydra:\n    type: context\n"), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(contextDir, "in-cluster", "argocd"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "in-cluster", "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "in-cluster", "argocd", "values.yaml"), []byte("global:\n  hydra:\n    type: root-app\nargocd: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "in-cluster", "argocd", "Chart.yaml"), []byte("apiVersion: v2\nname: argocd\nversion: 0.1.0\n"), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(contextDir, "target", "platform"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "target", "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "target", "platform", "values.yaml"), []byte("global:\n  hydra:\n    type: root-app\nplatform:\n  global:\n    hydra:\n      argocd: true\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "target", "platform", "Chart.yaml"), []byte("apiVersion: v2\nname: platform\nversion: 0.1.0\n"), 0o644))

	cluster, err := ResolveCommandCluster(ResolveCommandClusterOptions{
		Config:       types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true),
		HydraContext: types.HydraContext(contextDir),
		AppIds:       sets.New[types.AppId](types.AppId("target.platform")),
		Limits:       hydra.RESTClientLimits{},
	})
	require.NoError(t, err)
	assert.Equal(t, types.InCluster, cluster.ClusterName)
}
