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

func TestCluster_GetRootApps_ignoresDotDirectories(t *testing.T) {
	resetCaches()
	tmp := t.TempDir()
	clusterDir := filepath.Join(tmp, string(types.InCluster))
	require.NoError(t, os.MkdirAll(filepath.Join(clusterDir, "myapp"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(clusterDir, ".hydra"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n  info:\n    cluster: in-cluster\n"), 0o644))

	cfg := types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true)
	ctx, err := NewContext(log.Default(), types.ContextPath(tmp), cfg)
	require.NoError(t, err)
	cluster, err := NewCluster(ctx, types.InCluster, RESTClientLimits{})
	require.NoError(t, err)

	apps, err := cluster.GetRootApps()
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.Equal(t, types.RootAppName("myapp"), apps[0].RootAppName)
}

func TestNewRootApp_rejectsReservedBuiltinRootName(t *testing.T) {
	resetCaches()
	tmp := t.TempDir()
	clusterDir := filepath.Join(tmp, string(types.InCluster))
	require.NoError(t, os.MkdirAll(filepath.Join(clusterDir, "myapp"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n  info:\n    cluster: in-cluster\n"), 0o644))

	cfg := types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true)
	ctx, err := NewContext(log.Default(), types.ContextPath(tmp), cfg)
	require.NoError(t, err)
	cluster, err := NewCluster(ctx, types.InCluster, RESTClientLimits{})
	require.NoError(t, err)

	_, err = NewRootApp(cluster, types.ReservedPresetRootAppName)
	require.Error(t, err)
}

func TestCluster_AppIds_MovesArgoCDManagedRootAppsToInCluster(t *testing.T) {
	resetCaches()
	tmp := t.TempDir()
	writeTestContextValues(t, tmp)
	writeTestClusterValues(t, tmp, string(types.InCluster))
	writeTestClusterValues(t, tmp, "target")
	writeTestRootAppValues(t, tmp, string(types.InCluster), "argocd", false)
	writeTestRootAppValues(t, tmp, "target", "platform", true)

	cfg := types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true)
	ctx, err := NewContext(log.Default(), types.ContextPath(tmp), cfg)
	require.NoError(t, err)

	target, err := NewCluster(ctx, "target", RESTClientLimits{})
	require.NoError(t, err)
	targetAppIds, err := target.AppIds(types.HelmNetworkModeOffline)
	require.NoError(t, err)
	assert.False(t, targetAppIds.Has(types.AppId("target.platform")))

	inCluster, err := NewCluster(ctx, types.InCluster, RESTClientLimits{})
	require.NoError(t, err)
	inClusterAppIds, err := inCluster.AppIds(types.HelmNetworkModeOffline)
	require.NoError(t, err)
	assert.True(t, inClusterAppIds.Has(types.AppId("target.platform")))
	assert.True(t, inClusterAppIds.Has(types.AppId("in-cluster.argocd")))
}

func TestCluster_WithApp_AllowsInClusterAccessToForeignArgoCDManagedRootApp(t *testing.T) {
	resetCaches()
	tmp := t.TempDir()
	writeTestContextValues(t, tmp)
	writeTestClusterValues(t, tmp, string(types.InCluster))
	writeTestClusterValues(t, tmp, "target")
	writeTestRootAppValues(t, tmp, "target", "platform", true)

	cfg := types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true)
	ctx, err := NewContext(log.Default(), types.ContextPath(tmp), cfg)
	require.NoError(t, err)

	inCluster, err := NewCluster(ctx, types.InCluster, RESTClientLimits{})
	require.NoError(t, err)

	app, err := inCluster.WithApp(types.AppId("target.platform"))
	require.NoError(t, err)
	require.NotNil(t, app.AsRootApp())
	assert.Equal(t, types.ClusterName("target"), app.AsRootApp().ClusterName)
	assert.Equal(t, types.RootAppName("platform"), app.AsRootApp().RootAppName)
}

func writeTestContextValues(t *testing.T, contextDir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "values.yaml"), []byte("global:\n  hydra:\n    type: context\n"), 0o644))
}

func writeTestClusterValues(t *testing.T, contextDir string, clusterName string) {
	t.Helper()
	clusterDir := filepath.Join(contextDir, clusterName)
	require.NoError(t, os.MkdirAll(clusterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))
}

func writeTestRootAppValues(t *testing.T, contextDir string, clusterName string, rootAppName string, inClusterArgoCD bool) {
	t.Helper()
	rootDir := filepath.Join(contextDir, clusterName, rootAppName)
	require.NoError(t, os.MkdirAll(rootDir, 0o755))
	content := "global:\n  hydra:\n    type: root-app\n"
	if inClusterArgoCD {
		content += "    argocd: true\n"
	}
	content += rootAppName + ": {}\n"
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, "values.yaml"), []byte(content), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(rootDir, "Chart.yaml"), []byte("apiVersion: v2\nname: "+rootAppName+"\nversion: 0.1.0\n"), 0o644))
}
