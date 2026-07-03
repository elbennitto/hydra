package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// Regression probe: VisitResources can list the same namespaced CR twice when discovery exposes
// multiple API versions (separate APIResourceList entries). Entity ids differ only by apiVersion,
// so NewEntities does not deduplicate — ref ownership may see an extra "cluster-only" id.
func TestNewEntities_SameNamespacedNameDifferentAPIVersions_NotDeduped(t *testing.T) {
	v1, err := NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1"), types.Kind("KafkaTopic"))).
		WithNamespace(types.Namespace("demo")).
		WithName(types.Name("topic-a")).
		Build()
	require.NoError(t, err)
	v1beta2, err := NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("KafkaTopic"))).
		WithNamespace(types.Namespace("demo")).
		WithName(types.Name("topic-a")).
		Build()
	require.NoError(t, err)

	id1, err := v1.Id()
	require.NoError(t, err)
	id2, err := v1beta2.Id()
	require.NoError(t, err)
	assert.NotEqual(t, id1, id2, "fixture: ids must differ only by version segment")

	out, err := NewEntities([]Entity{v1, v1beta2})
	require.NoError(t, err)
	require.Len(t, out.Items, 2, "distinct ids must both remain in inventory")
}

func TestNewEntities_DeduplicatesDuplicateIds_SameApp_KeepsLast(t *testing.T) {
	gvk := types.NewGVK(types.Group("apiextensions.k8s.io"), types.Version("v1"), types.Kind("CustomResourceDefinition"))
	first, err := NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("dup-in-chart.example.com")).
		WithAppIds([]types.AppId{"cluster.app.one"}).
		WithTemplatePath(types.TemplatePath("charts/one/crd.yaml")).
		Build()
	require.NoError(t, err)

	second, err := NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("dup-in-chart.example.com")).
		WithAppIds([]types.AppId{"cluster.app.one"}).
		WithTemplatePath(types.TemplatePath("charts/two/crd.yaml")).
		Build()
	require.NoError(t, err)

	assert.Equal(t, duplicateScopeSameApp, duplicateAppScope([]Entity{first, second}))

	out, err := NewEntities([]Entity{first, second})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)
	tp, err := out.Items[0].TemplatePath()
	require.NoError(t, err)
	assert.Equal(t, types.TemplatePath("charts/two/crd.yaml"), tp)
}

func TestNewEntities_DeduplicatesDuplicateIds_KeepsLast(t *testing.T) {
	gvk := types.NewGVK(types.Group("apiextensions.k8s.io"), types.Version("v1"), types.Kind("CustomResourceDefinition"))
	first, err := NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("widgets.example.com")).
		WithAppIds([]types.AppId{"cluster.app.one"}).
		WithTemplatePath(types.TemplatePath("charts/one/crd.yaml")).
		Build()
	require.NoError(t, err)

	second, err := NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("widgets.example.com")).
		WithAppIds([]types.AppId{"cluster.app.two"}).
		WithTemplatePath(types.TemplatePath("charts/two/crd.yaml")).
		Build()
	require.NoError(t, err)

	id1, err := first.Id()
	require.NoError(t, err)
	id2, err := second.Id()
	require.NoError(t, err)
	assert.Equal(t, id1, id2, "fixture must use the same entity id")

	out, err := NewEntities([]Entity{first, second})
	require.NoError(t, err)
	require.Len(t, out.Items, 1)

	appIds, err := out.Items[0].AppIds()
	require.NoError(t, err)
	assert.Equal(t, []types.AppId{"cluster.app.two"}, appIds)
	tp, err := out.Items[0].TemplatePath()
	require.NoError(t, err)
	assert.Equal(t, types.TemplatePath("charts/two/crd.yaml"), tp)
}

func TestDuplicateEntitySourcesBreakdown_AggregatesSameAppTemplate(t *testing.T) {
	gvk := types.NewGVK(types.Group("v1"), types.Version("v1"), types.Kind("ConfigMap"))
	a, err := NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("ns")).
		WithName(types.Name("dup")).
		WithAppIds([]types.AppId{"cluster.app"}).
		WithTemplatePath(types.TemplatePath("templates/cm.yaml")).
		Build()
	require.NoError(t, err)
	b, err := NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("ns")).
		WithName(types.Name("dup")).
		WithAppIds([]types.AppId{"cluster.app"}).
		WithTemplatePath(types.TemplatePath("templates/cm.yaml")).
		Build()
	require.NoError(t, err)
	out := duplicateEntitySourcesBreakdown([]Entity{a, b})
	require.Contains(t, out, "cluster.app / templates/cm.yaml (2)")
}

func TestDuplicateAppScope(t *testing.T) {
	gvk := types.NewGVK(types.Group("apiextensions.k8s.io"), types.Version("v1"), types.Kind("CustomResourceDefinition"))
	a, err := NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("widgets.example.com")).
		WithAppIds([]types.AppId{"cluster.app.one"}).
		Build()
	require.NoError(t, err)
	b, err := NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("widgets.example.com")).
		WithAppIds([]types.AppId{"cluster.app.one"}).
		Build()
	require.NoError(t, err)
	assert.Equal(t, duplicateScopeSameApp, duplicateAppScope([]Entity{a, b}))

	c, err := NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("widgets.example.com")).
		WithAppIds([]types.AppId{"cluster.app.other"}).
		Build()
	require.NoError(t, err)
	assert.Equal(t, duplicateScopeCrossApp, duplicateAppScope([]Entity{a, c}))

	noApp, err := NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("orphan.example.com")).
		Build()
	require.NoError(t, err)
	assert.Equal(t, duplicateScopeUnknown, duplicateAppScope([]Entity{noApp, noApp}))
}
