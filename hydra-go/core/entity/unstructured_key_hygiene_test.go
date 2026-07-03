package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func podEntityClusterOnly(namespace, name, clusterUID string) Entity {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"uid":       clusterUID,
			},
		},
	}
	b := NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithNamespaced(types.NamespacedNo)
	return mustBuild(b.WithUnstructured(types.KeyClusterEntity, u))
}

func podEntityTemplateAndCluster(namespace, name, tplUID, clusterUID string) Entity {
	tpl := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"uid":       tplUID,
			},
		},
	}
	cl := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"uid":       clusterUID,
			},
			"status": map[string]any{
				"phase": "Running",
			},
		},
	}
	b := NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithNamespaced(types.NamespacedNo)
	b = b.WithUnstructured(types.KeyTemplateEntity, tpl).WithUnstructured(types.KeyClusterEntity, cl)
	return mustBuild(b)
}

func TestStripUnstructuredKey_DropsClusterOnlyEntity(t *testing.T) {
	pod := podEntityClusterOnly("default", "orphan", "uid-live")
	entities, err := NewEntities([]Entity{pod})
	require.NoError(t, err)

	out, err := StripUnstructuredKey(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Equal(t, 0, out.Len())
}

func TestStripUnstructuredKey_RetainsTemplateOnlyEntity(t *testing.T) {
	tpl := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "hook",
				"namespace": "default",
				"uid":       "uid-tpl",
			},
		},
	}
	b := NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("hook")).
		WithNamespace(types.Namespace("default")).
		WithNamespaced(types.NamespacedNo)
	pod := mustBuild(b.WithUnstructured(types.KeyTemplateEntity, tpl))

	entities, err := NewEntities([]Entity{pod})
	require.NoError(t, err)

	out, err := StripUnstructuredKey(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
	_, ok := out.Items[0].Unstructured(types.KeyTemplateEntity)
	assert.True(t, ok)
	_, hasCluster := out.Items[0].Unstructured(types.KeyClusterEntity)
	assert.False(t, hasCluster)
}

func TestStripUnstructuredKey_KeepsTemplateAfterRemovingCluster(t *testing.T) {
	pod := podEntityTemplateAndCluster("default", "web", "uid-tpl", "uid-old")
	entities, err := NewEntities([]Entity{pod})
	require.NoError(t, err)

	out, err := StripUnstructuredKey(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
	_, ok := out.Items[0].Unstructured(types.KeyTemplateEntity)
	assert.True(t, ok)
	_, hasCluster := out.Items[0].Unstructured(types.KeyClusterEntity)
	assert.False(t, hasCluster)
}

func TestStripUnstructuredKey_RebuildsEntityMapAndIdList(t *testing.T) {
	a := podEntityClusterOnly("default", "a", "ua")
	b := podEntityTemplateAndCluster("default", "b", "tb", "cb")
	entities, err := NewEntities([]Entity{a, b})
	require.NoError(t, err)

	out, err := StripUnstructuredKey(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
	id, err := out.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("v1/Pod/default/b"), id)
	_, inMap := out.EntityMap[id]
	assert.True(t, inMap)
	assert.True(t, out.IdSet.Has(id))
}

func TestRefreshUnstructuredFromListed_StripMergeAndUpdatedLiveUID(t *testing.T) {
	pod := podEntityTemplateAndCluster("default", "web", "uid-tpl", "uid-before")
	entities, err := NewEntities([]Entity{pod})
	require.NoError(t, err)

	refreshed := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "web",
				"namespace": "default",
				"uid":       "uid-after-list",
			},
			"status": map[string]any{"phase": "Running"},
		},
	}

	out, err := RefreshUnstructuredFromListed(entities, types.KeyClusterEntity, types.KeyClusterEntity, []unstructured.Unstructured{refreshed})
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
	u, err := out.Items[0].UnstructuredOrError(types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Equal(t, "uid-after-list", string(u.GetUID()))
	_, stillTpl := out.Items[0].Unstructured(types.KeyTemplateEntity)
	assert.True(t, stillTpl)
}

func TestStripAndMergeClusterPods_IgnoresCELParameterForListedSlice(t *testing.T) {
	pod := podEntityClusterOnly("kube-system", "node-agent", "u1")
	entities, err := NewEntities([]Entity{pod})
	require.NoError(t, err)

	listed := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "node-agent",
				"namespace": "kube-system",
				"uid":       "u2",
			},
		},
	}

	out, err := StripAndMergeClusterPods(entities, `kind == "Pod"`, []unstructured.Unstructured{listed})
	require.NoError(t, err)
	require.Equal(t, 1, out.Len())
	u, err := out.Items[0].UnstructuredOrError(types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Equal(t, "u2", string(u.GetUID()))
}
