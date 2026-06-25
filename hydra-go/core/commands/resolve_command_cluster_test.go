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

func TestResolveCommandCluster_RejectsEmptyAppIds(t *testing.T) {
	_, err := ResolveCommandCluster(ResolveCommandClusterOptions{
		Config:       types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true),
		HydraContext: types.HydraContext("/does/not/matter"),
		AppIds:       sets.New[types.AppId](),
		Limits:       hydra.RESTClientLimits{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no apps specified")
}

func TestResolveCommandCluster_RejectsAppIdsFromDifferentClusters(t *testing.T) {
	ResolveCommandClusterFunc = resolveCommandClusterDefault
	t.Cleanup(func() {
		ResolveCommandClusterFunc = resolveCommandClusterDefault
	})

	contextDir := t.TempDir()
	writeResolveCommandContext(t, contextDir)
	require.NoError(t, os.MkdirAll(filepath.Join(contextDir, "dev", "apps"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "dev", "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "dev", "apps", "values.yaml"), []byte("apps:\n  apps:\n    api:\n      namespace: dev-ns\nglobal:\n  hydra:\n    type: root-app\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "dev", "apps", "Chart.yaml"), []byte("apiVersion: v2\nname: apps\nversion: 0.1.0\n"), 0o644))

	appIds := sets.New[types.AppId](
		types.AppId("target.platform.api"),
		types.AppId("dev.apps.api"),
	)

	_, err := ResolveCommandCluster(ResolveCommandClusterOptions{
		Config:       types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true),
		HydraContext: types.HydraContext(contextDir),
		AppIds:       appIds,
		Limits:       hydra.RESTClientLimits{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "same effective cluster")
}

func TestResolveCommandCluster_UsesInClusterForArgoCDManagedForeignRootApp(t *testing.T) {
	ResolveCommandClusterFunc = resolveCommandClusterDefault
	t.Cleanup(func() {
		ResolveCommandClusterFunc = resolveCommandClusterDefault
	})

	contextDir := t.TempDir()
	writeResolveCommandContext(t, contextDir)

	cluster, err := ResolveCommandCluster(ResolveCommandClusterOptions{
		Config:       types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true),
		HydraContext: types.HydraContext(contextDir),
		AppIds:       sets.New[types.AppId](types.AppId("target.platform")),
		Limits:       hydra.RESTClientLimits{},
	})
	require.NoError(t, err)
	assert.Equal(t, types.InCluster, cluster.ClusterName)
}

func TestResolveCommandCluster_RejectsMixedEffectiveClusters(t *testing.T) {
	ResolveCommandClusterFunc = resolveCommandClusterDefault
	t.Cleanup(func() {
		ResolveCommandClusterFunc = resolveCommandClusterDefault
	})

	contextDir := t.TempDir()
	writeResolveCommandContext(t, contextDir)

	_, err := ResolveCommandCluster(ResolveCommandClusterOptions{
		Config:       types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true),
		HydraContext: types.HydraContext(contextDir),
		AppIds: sets.New[types.AppId](
			types.AppId("target.platform"),
			types.AppId("target.platform.api"),
		),
		Limits: hydra.RESTClientLimits{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "same effective cluster")
}

func writeResolveCommandContext(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "values.yaml"), []byte("global:\n  hydra:\n    type: context\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "in-cluster", "argocd"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "in-cluster", "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "in-cluster", "argocd", "values.yaml"), []byte("global:\n  hydra:\n    type: root-app\nargocd: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "in-cluster", "argocd", "Chart.yaml"), []byte("apiVersion: v2\nname: argocd\nversion: 0.1.0\n"), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "target", "platform"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target", "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target", "platform", "values.yaml"), []byte("platform:\n  apps:\n    api:\n      namespace: target-ns\nglobal:\n  hydra:\n    type: root-app\n    argocd: true\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target", "platform", "Chart.yaml"), []byte("apiVersion: v2\nname: platform\nversion: 0.1.0\n"), 0o644))
}
