package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestWithoutDuplicateSyntheticKubernetesDefaults_DropsWhenRenderedHasSameId(t *testing.T) {
	nsYAML := types.YamlString(`# Source: shared/templates/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: demo
`)
	rendered, err := entity.NewEntitiesFromYaml(log.Default(), nsYAML, types.KeyTemplateEntity)
	require.NoError(t, err)

	synthetic, err := CreateNamespaceEntities(sets.New(types.Namespace("demo")), types.KeyTemplateEntity)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(synthetic.Items), 1)

	out, err := WithoutDuplicateSyntheticKubernetesDefaults(log.Default(), rendered, synthetic)
	require.NoError(t, err)
	for _, e := range out.Items {
		id, err := e.Id()
		require.NoError(t, err)
		assert.NotEqual(t, types.Id("v1/Namespace//demo"), id)
	}
}

func TestInferredOwnerNamespacesForApp_SoleAppNamespaces(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	a := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("a"))), types.Namespace("only-me")), []types.AppId{"c.app"})
	b := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("b"))), types.Namespace("shared")), []types.AppId{"c.other"})
	ents, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)
	require.Equal(t, []string{"only-me"}, InferredOwnerNamespacesForApp("c.app", ents))
	require.Equal(t, []string{"shared"}, InferredOwnerNamespacesForApp("c.other", ents))
}

func TestUninstallLeftoverNamespaces_IncludesTemplateNamespacesWhenNotExclusive(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	ent := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("chi"))), types.Namespace("demo")), []types.AppId{"in-cluster.demo-infra.demo-clickhouse"})
	ents, err := entity.NewEntities([]entity.Entity{ent})
	require.NoError(t, err)

	exclusive := sets.New[types.Namespace]()
	out := UninstallLeftoverNamespaces(exclusive, ents)
	assert.True(t, out.Has(types.Namespace("demo")), "shared namespace from selected app render must be scanned for leftovers")
}

func TestMergeInferredOwnerNamespacesIntoHydraMap(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	a := withAppIds(withNamespace(mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("a"))), types.Namespace("app-ns")), []types.AppId{"c.app"})
	ents, err := entity.NewEntities([]entity.Entity{a})
	require.NoError(t, err)
	base := types.ValuesMap{"ownerNamespaces": []string{"manual-ns"}}
	out := MergeInferredOwnerNamespacesIntoHydraMap(base, "c.app", ents)
	require.Equal(t, []string{"app-ns", "manual-ns"}, out["ownerNamespaces"])
}

func TestClassifyNamespaceNamePresence(t *testing.T) {
	tmpl := sets.New(types.Namespace("a"), types.Namespace("b"))
	live := sets.New(types.Namespace("b"), types.Namespace("c"))
	assert.Equal(t, NamespaceNameTemplateAndCluster, ClassifyNamespaceNamePresence(types.Namespace("b"), tmpl, live))
	assert.Equal(t, NamespaceNameTemplateOnly, ClassifyNamespaceNamePresence(types.Namespace("a"), tmpl, live))
	assert.Equal(t, NamespaceNameClusterOnly, ClassifyNamespaceNamePresence(types.Namespace("c"), tmpl, live))
	assert.Equal(t, NamespaceNameNeither, ClassifyNamespaceNamePresence(types.Namespace("z"), tmpl, live))
}

func TestNamespaceNamePresenceLabel(t *testing.T) {
	assert.Equal(t, "neither", NamespaceNamePresenceLabel(NamespaceNameNeither))
	assert.Equal(t, "template and cluster", NamespaceNamePresenceLabel(NamespaceNameTemplateAndCluster))
}

func TestCollectLiveClusterNamespaceNames(t *testing.T) {
	e := clusterNamespaceEntity("ns-a", "u1")
	ents, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)
	out := CollectLiveClusterNamespaceNames(ents)
	assert.True(t, out.Has(types.Namespace("ns-a")))
}
