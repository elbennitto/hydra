package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func makeEntityWithFinalizers(group, version, kind, namespace, name string, finalizers []string) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": version,
			"kind":       kind,
			"metadata": map[string]any{
				"name":       name,
				"namespace":  namespace,
				"finalizers": toAnySlice(finalizers),
			},
		},
	}
	if group != "" {
		u.Object["apiVersion"] = group + "/" + version
	}
	return mustBuild(b.WithUnstructured(types.KeyClusterEntity, u))
}

func makeEntityWithoutFinalizers(group, version, kind, namespace, name string) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": version,
			"kind":       kind,
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
		},
	}
	if group != "" {
		u.Object["apiVersion"] = group + "/" + version
	}
	return mustBuild(b.WithUnstructured(types.KeyClusterEntity, u))
}

func toAnySlice(ss []string) []any {
	result := make([]any, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}

func TestCollectFinalizerPatches_NoFinalizersConfigured(t *testing.T) {
	e := makeEntityWithoutFinalizers("", "v1", "ConfigMap", "default", "app-config")

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	patches, err := collectFinalizerPatches(entities, types.KeyClusterEntity, nil)
	require.NoError(t, err)
	assert.Empty(t, patches)
}

func TestCollectFinalizerPatches_NoEntitiesHaveMatchingFinalizers(t *testing.T) {
	e := makeEntityWithFinalizers("", "v1", "Secret", "default", "tls-cert",
		[]string{"keep.io/important"})

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	patches, err := collectFinalizerPatches(entities, types.KeyClusterEntity,
		[]string{"remove.io/unwanted"})
	require.NoError(t, err)
	assert.Empty(t, patches)
}

func TestCollectFinalizerPatches_EntityHasOnlyMatchingFinalizer(t *testing.T) {
	e := makeEntityWithFinalizers("", "v1", "Secret", "default", "tls-cert",
		[]string{"remove.io/unwanted"})

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	patches, err := collectFinalizerPatches(entities, types.KeyClusterEntity,
		[]string{"remove.io/unwanted"})
	require.NoError(t, err)
	require.Len(t, patches, 1)

	id, err := e.Id()
	require.NoError(t, err)
	assert.Equal(t, id, patches[0].Id)
	assert.Empty(t, patches[0].FinalizersToKeep)
	assert.Equal(t, []string{"remove.io/unwanted"}, patches[0].FinalizersRemoved)
}

func TestCollectFinalizerPatches_EntityHasMatchingAndNonMatchingFinalizers(t *testing.T) {
	e := makeEntityWithFinalizers("", "v1", "Secret", "default", "tls-cert",
		[]string{"remove.io/unwanted", "keep.io/important"})

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	patches, err := collectFinalizerPatches(entities, types.KeyClusterEntity,
		[]string{"remove.io/unwanted"})
	require.NoError(t, err)
	require.Len(t, patches, 1)

	id, err := e.Id()
	require.NoError(t, err)
	assert.Equal(t, id, patches[0].Id)
	assert.Equal(t, []string{"keep.io/important"}, patches[0].FinalizersToKeep)
	assert.Equal(t, []string{"remove.io/unwanted"}, patches[0].FinalizersRemoved)
}

func TestCollectFinalizerPatches_EntityHasMultipleMatchingFinalizers(t *testing.T) {
	e := makeEntityWithFinalizers("apps", "v1", "Deployment", "demo", "app",
		[]string{"remove.io/a", "keep.io/x", "remove.io/b", "keep.io/y"})

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	patches, err := collectFinalizerPatches(entities, types.KeyClusterEntity,
		[]string{"remove.io/a", "remove.io/b"})
	require.NoError(t, err)
	require.Len(t, patches, 1)

	id, err := e.Id()
	require.NoError(t, err)
	assert.Equal(t, id, patches[0].Id)
	assert.Equal(t, []string{"keep.io/x", "keep.io/y"}, patches[0].FinalizersToKeep)
	assert.Equal(t, []string{"remove.io/a", "remove.io/b"}, patches[0].FinalizersRemoved)
}

func TestCollectFinalizerPatches_MultipleEntitiesMatch(t *testing.T) {
	e1 := makeEntityWithFinalizers("", "v1", "Secret", "default", "secret-a",
		[]string{"remove.io/fin", "keep.io/other"})
	e2 := makeEntityWithFinalizers("", "v1", "ConfigMap", "default", "cm-b",
		[]string{"remove.io/fin"})
	e3 := makeEntityWithoutFinalizers("", "v1", "ServiceAccount", "default", "sa-c")

	entities, err := entity.NewEntities([]entity.Entity{e1, e2, e3})
	require.NoError(t, err)

	patches, err := collectFinalizerPatches(entities, types.KeyClusterEntity,
		[]string{"remove.io/fin"})
	require.NoError(t, err)
	require.Len(t, patches, 2)

	id1, err := e1.Id()
	require.NoError(t, err)
	id2, err := e2.Id()
	require.NoError(t, err)

	patchIds := make([]types.Id, len(patches))
	for i, p := range patches {
		patchIds[i] = p.Id
	}
	assert.Contains(t, patchIds, id1)
	assert.Contains(t, patchIds, id2)

	for _, p := range patches {
		if p.Id == id1 {
			assert.Equal(t, []string{"keep.io/other"}, p.FinalizersToKeep)
			assert.Equal(t, []string{"remove.io/fin"}, p.FinalizersRemoved)
		}
		if p.Id == id2 {
			assert.Empty(t, p.FinalizersToKeep)
			assert.Equal(t, []string{"remove.io/fin"}, p.FinalizersRemoved)
		}
	}
}

func TestCollectFinalizerPatches_EntityWithoutUnstructuredData(t *testing.T) {
	withFinalizer := makeEntityWithFinalizers("", "v1", "Secret", "default", "tls-cert",
		[]string{"remove.io/unwanted"})
	templateOnly := makeEntity("", "v1", "ConfigMap", "default", "app-config")

	entities, err := entity.NewEntities([]entity.Entity{withFinalizer, templateOnly})
	require.NoError(t, err)

	patches, err := collectFinalizerPatches(entities, types.KeyClusterEntity,
		[]string{"remove.io/unwanted"})
	require.NoError(t, err)
	require.Len(t, patches, 1)

	id, err := withFinalizer.Id()
	require.NoError(t, err)
	assert.Equal(t, id, patches[0].Id)
	assert.Equal(t, []string{"remove.io/unwanted"}, patches[0].FinalizersRemoved)
}

func TestCollectFinalizerPatches_EmptyEntityList(t *testing.T) {
	entities, err := entity.NewEntities(nil)
	require.NoError(t, err)

	patches, err := collectFinalizerPatches(entities, types.KeyClusterEntity,
		[]string{"remove.io/unwanted"})
	require.NoError(t, err)
	assert.Empty(t, patches)
}
