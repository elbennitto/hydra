package hydra

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestClusterRootHydraYamlAllowedAtClusterRoot(t *testing.T) {
	t.Parallel()

	group := t.TempDir()
	contextDir := filepath.Join(group, "ctx")
	clusterDir := filepath.Join(contextDir, "cluster-a")
	require.NoError(t, os.MkdirAll(clusterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(group, "values.yaml"), []byte("global:\n  hydra:\n    type: group\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "hydra.yaml"), []byte("hydra:\n  overrides: {}\n"), 0o644))

	ctx, err := CreateContext(log.Default(), contextDir, types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
	require.NoError(t, err)
	_, err = NewCluster(ctx, "cluster-a", RESTClientLimits{})
	require.NoError(t, err)
}

func TestClusterRootHydraYamlRejectedOutsideClusterRoot(t *testing.T) {
	t.Parallel()

	t.Run("group", func(t *testing.T) {
		group := t.TempDir()
		contextDir := filepath.Join(group, "ctx")
		require.NoError(t, os.MkdirAll(contextDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(group, "values.yaml"), []byte("global:\n  hydra:\n    type: group\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(group, "hydra.yaml"), []byte("hydra:\n  overrides: {}\n"), 0o644))

		_, err := CreateContext(log.Default(), contextDir, types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
		require.Error(t, err)
		require.Contains(t, err.Error(), "hydra.yaml is only allowed in cluster root directories")
	})

	t.Run("context", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "values.yaml"), []byte("global:\n  hydra:\n    type: context\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "hydra.yaml"), []byte("hydra:\n  overrides: {}\n"), 0o644))

		_, err := CreateContext(log.Default(), root, types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
		require.Error(t, err)
		require.Contains(t, err.Error(), "hydra.yaml is only allowed in cluster root directories")
	})

	t.Run("root-app", func(t *testing.T) {
		group := t.TempDir()
		contextDir := filepath.Join(group, "ctx")
		clusterDir := filepath.Join(contextDir, "cluster-a")
		rootAppDir := filepath.Join(clusterDir, "platform")
		require.NoError(t, os.MkdirAll(rootAppDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(group, "values.yaml"), []byte("global:\n  hydra:\n    type: group\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(rootAppDir, "hydra.yaml"), []byte("hydra:\n  overrides: {}\n"), 0o644))

		ctx, err := CreateContext(log.Default(), contextDir, types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
		require.NoError(t, err)
		cluster, err := NewCluster(ctx, "cluster-a", RESTClientLimits{})
		require.NoError(t, err)
		_, err = NewRootApp(cluster, "platform")
		require.Error(t, err)
		require.Contains(t, err.Error(), "hydra.yaml is only allowed in cluster root directories")
	})
}

func TestEffectiveClusterDefaultsPresetsWithRootOverrides_ManualIDIncludeAndExclude(t *testing.T) {
	t.Parallel()

	env, err := cel.NewEnv()
	require.NoError(t, err)

	entityID := func(apiVersion, kind, namespace, name string) entity.Entity {
		b := entity.NewEntityBuilder().
			WithKind(types.Kind(kind)).
			WithName(types.Name(name))
		av, err := types.ParseApiVersion(apiVersion)
		require.NoError(t, err)
		b = b.WithGroup(av.Group).WithVersion(av.Version)
		if namespace == "" {
			b = b.WithNamespaced(types.NamespacedNo)
		} else {
			b = b.WithNamespace(types.Namespace(namespace)).WithNamespaced(types.NamespacedYes)
		}
		e, err := b.Build()
		require.NoError(t, err)
		return e
	}

	effs, err := EffectiveClusterDefaultsPresetsForKubernetesMinorWithOverrides(nil, map[types.AppId]types.ClusterRootAppOverrideList{
		"in-cluster.preset.coredns": {
			{Selector: types.RefSelector{Version: "v1", Kind: "ConfigMap", Namespace: "kube-system", Name: "manual-coredns"}},
			{Selector: types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "kube-system", Name: "coredns"}, Exclude: true},
		},
	}, 99)
	require.NoError(t, err)

	customConfigMap := entityID("v1", "ConfigMap", "kube-system", "manual-coredns")
	matches, err := MatchingClusterDefaultsPresetIDs(effs, 99, env, customConfigMap)
	require.NoError(t, err)
	require.Equal(t, []string{"coredns"}, matches)

	deploy := entityID("apps/v1", "Deployment", "kube-system", "coredns")
	matches, err = MatchingClusterDefaultsPresetIDs(effs, 99, env, deploy)
	require.NoError(t, err)
	require.NotContains(t, matches, "coredns")
}

func TestEffectiveClusterDefaultsPresetsWithRootOverrides_ManualCelIncludeSupportsExistingNotation(t *testing.T) {
	t.Parallel()

	enabled := true
	merged := &types.HydraPresetsSection{
		"metrics-server": {Enabled: &enabled},
	}
	effs, err := EffectiveClusterDefaultsPresetsForKubernetesMinorWithOverrides(merged, map[types.AppId]types.ClusterRootAppOverrideList{
		"in-cluster.preset.metrics-server": {
			{
				Selector: types.RefSelector{Group: "apps", Version: "v1", Kind: "Deployment", Namespace: "kube-system"},
				Cel:      `name.startsWith("manual-ms-")`,
			},
		},
	}, 99)
	require.NoError(t, err)

	secret, err := entity.NewEntityBuilder().
		WithGroup("apps").
		WithVersion("v1").
		WithKind("Deployment").
		WithNamespace("kube-system").
		WithName("manual-ms-deploy").
		WithNamespaced(types.NamespacedYes).
		Build()
	require.NoError(t, err)
	env, err := cel.NewEnv()
	require.NoError(t, err)
	matches, err := MatchingClusterDefaultsPresetIDs(effs, 99, env, secret)
	require.NoError(t, err)
	require.Contains(t, matches, "metrics-server")
}
