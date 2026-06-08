package hydra

import (
	"os"
	"path/filepath"
	"testing"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
