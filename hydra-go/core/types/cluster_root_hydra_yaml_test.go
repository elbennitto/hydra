package types

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestClusterRootAppOverrideItemUnmarshal_ScalarStringUsesIDNotation(t *testing.T) {
	t.Parallel()

	var item ClusterRootAppOverrideItem
	err := yaml.Unmarshal([]byte(`"v1/ConfigMap/kube-system/example"`), &item)
	require.NoError(t, err)
	require.Equal(t, Group(""), item.Selector.Group)
	require.Equal(t, Version("v1"), item.Selector.Version)
	require.Equal(t, Kind("ConfigMap"), item.Selector.Kind)
	require.Equal(t, Namespace("kube-system"), item.Selector.Namespace)
	require.Equal(t, Name("example"), item.Selector.Name)
	require.Empty(t, item.Cel)
	require.False(t, item.Exclude)
}

func TestClusterRootAppOverrideItemUnmarshal_MapSupportsPredicateAliasAndExclude(t *testing.T) {
	t.Parallel()

	var item ClusterRootAppOverrideItem
	err := yaml.Unmarshal([]byte(`
gvk: metrics.k8s.io/v1beta1/PodMetrics
namespace: kube-system
predicate: 'name.matches("^metrics-server-.*")'
exclude: true
`), &item)
	require.NoError(t, err)
	require.Equal(t, Group("metrics.k8s.io"), item.Selector.Group)
	require.Equal(t, Version("v1beta1"), item.Selector.Version)
	require.Equal(t, Kind("PodMetrics"), item.Selector.Kind)
	require.Equal(t, Namespace("kube-system"), item.Selector.Namespace)
	require.Equal(t, CelPredicate(`name.matches("^metrics-server-.*")`), item.Cel)
	require.True(t, item.Exclude)
}

func TestClusterRootHydraFileUnmarshal_OverridesSupportExistingSelectorShorthand(t *testing.T) {
	t.Parallel()

	var file ClusterRootHydraFile
	err := yaml.Unmarshal([]byte(`
hydra:
  overrides:
    presets.coredns:
      - "v1/ConfigMap/kube-system/manual"
      - gvk: autoscaling/v2/HorizontalPodAutoscaler
        namespace: kube-system
        name: coredns
      - cel: 'ns == "kube-system"'
        exclude: true
`), &file)
	require.NoError(t, err)
	require.NotNil(t, file.Hydra)
	require.Len(t, file.Hydra.Overrides["presets.coredns"], 3)
	require.Equal(t, Name("coredns"), file.Hydra.Overrides["presets.coredns"][1].Selector.Name)
	require.Equal(t, CelPredicate(`ns == "kube-system"`), file.Hydra.Overrides["presets.coredns"][2].Cel)
	require.True(t, file.Hydra.Overrides["presets.coredns"][2].Exclude)
}

func TestParseClusterLocalOverrideTarget(t *testing.T) {
	t.Parallel()

	tests := map[string]AppId{
		"argocd":            "in-cluster.argocd",
		"argocd.root":       "in-cluster.argocd",
		"cluster-infra.dex": "in-cluster.cluster-infra.dex",
		"presets.coredns":   "in-cluster.preset.coredns",
	}
	for raw, want := range tests {
		got, err := ParseClusterLocalOverrideTarget(InCluster, raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, got)
	}
}
