package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func mustBuild(b EntityBuilder) Entity {
	e, err := b.Build()
	if err != nil {
		panic(err)
	}
	return e
}

func testEntityBuilder(group, version, kind, namespace, name string) EntityBuilder {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	return b
}

func makeTestEntity(group, version, kind, namespace, name string) Entity {
	return mustBuild(testEntityBuilder(group, version, kind, namespace, name))
}

func makeTestUnstructured(group, version, kind, namespace, name, uid string, ownerRefs []map[string]any) unstructured.Unstructured {
	apiVersion := version
	if group != "" {
		apiVersion = group + "/" + version
	}

	metadata := map[string]any{
		"name":      name,
		"namespace": namespace,
		"uid":       uid,
	}
	if ownerRefs != nil {
		refs := make([]any, len(ownerRefs))
		for i, r := range ownerRefs {
			refs[i] = r
		}
		metadata["ownerReferences"] = refs
	}

	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata":   metadata,
		},
	}
}

func ownerRef(apiVersion, kind, name, uid string) map[string]any {
	return map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"name":       name,
		"uid":        uid,
	}
}

func orphanIds(t *testing.T, orphans Entities) []types.Id {
	t.Helper()
	ids := make([]types.Id, 0, orphans.Len())
	for _, e := range orphans.Items {
		id, err := e.Id()
		require.NoError(t, err)
		ids = append(ids, id)
	}
	return ids
}

func TestOrphanedEntities_NoOwnerReferences(t *testing.T) {
	u1 := makeTestUnstructured("apps", "v1", "Deployment", "default", "deploy-a", "uid-a", nil)
	u2 := makeTestUnstructured("", "v1", "Service", "default", "svc-b", "uid-b", nil)

	e1 := mustBuild(testEntityBuilder("apps", "v1", "Deployment", "default", "deploy-a").
		WithUnstructured(types.KeyClusterEntity, u1))
	e2 := mustBuild(testEntityBuilder("", "v1", "Service", "default", "svc-b").
		WithUnstructured(types.KeyClusterEntity, u2))

	entities, err := NewEntities([]Entity{e1, e2})
	require.NoError(t, err)

	orphans := entities.OrphanedEntities(types.KeyClusterEntity)
	assert.Equal(t, 0, orphans.Len())
}

func TestOrphanedEntities_OwnerExists(t *testing.T) {
	uDeploy := makeTestUnstructured("apps", "v1", "Deployment", "default", "deploy-a", "uid-deploy", nil)
	uRS := makeTestUnstructured("apps", "v1", "ReplicaSet", "default", "rs-a", "uid-rs",
		[]map[string]any{ownerRef("apps/v1", "Deployment", "deploy-a", "uid-deploy")})

	eDeploy := mustBuild(testEntityBuilder("apps", "v1", "Deployment", "default", "deploy-a").
		WithUnstructured(types.KeyClusterEntity, uDeploy))
	eRS := mustBuild(testEntityBuilder("apps", "v1", "ReplicaSet", "default", "rs-a").
		WithUnstructured(types.KeyClusterEntity, uRS))

	entities, err := NewEntities([]Entity{eDeploy, eRS})
	require.NoError(t, err)

	orphans := entities.OrphanedEntities(types.KeyClusterEntity)
	assert.Equal(t, 0, orphans.Len())
}

func TestOrphanedEntities_OwnerMissing(t *testing.T) {
	uPod := makeTestUnstructured("", "v1", "Pod", "default", "pod-a", "uid-pod",
		[]map[string]any{ownerRef("apps/v1", "ReplicaSet", "rs-deleted", "uid-deleted-rs")})

	ePod := mustBuild(testEntityBuilder("", "v1", "Pod", "default", "pod-a").
		WithUnstructured(types.KeyClusterEntity, uPod))

	entities, err := NewEntities([]Entity{ePod})
	require.NoError(t, err)

	orphans := entities.OrphanedEntities(types.KeyClusterEntity)
	require.Equal(t, 1, orphans.Len())

	ids := orphanIds(t, orphans)
	assert.Contains(t, ids, types.Id("v1/Pod/default/pod-a"))
}

func TestOrphanedEntities_MultipleOwnersMissing(t *testing.T) {
	uPod := makeTestUnstructured("", "v1", "Pod", "default", "pod-a", "uid-pod",
		[]map[string]any{
			ownerRef("apps/v1", "ReplicaSet", "rs-deleted", "uid-deleted-rs"),
			ownerRef("apps/v1", "StatefulSet", "sts-deleted", "uid-deleted-sts"),
		})

	ePod := mustBuild(testEntityBuilder("", "v1", "Pod", "default", "pod-a").
		WithUnstructured(types.KeyClusterEntity, uPod))

	entities, err := NewEntities([]Entity{ePod})
	require.NoError(t, err)

	orphans := entities.OrphanedEntities(types.KeyClusterEntity)
	require.Equal(t, 1, orphans.Len())

	ids := orphanIds(t, orphans)
	assert.Contains(t, ids, types.Id("v1/Pod/default/pod-a"))
}

func TestOrphanedEntities_MixedOwners(t *testing.T) {
	uDeploy := makeTestUnstructured("apps", "v1", "Deployment", "default", "deploy-a", "uid-deploy", nil)
	uPod := makeTestUnstructured("", "v1", "Pod", "default", "pod-a", "uid-pod",
		[]map[string]any{
			ownerRef("apps/v1", "Deployment", "deploy-a", "uid-deploy"),
			ownerRef("apps/v1", "StatefulSet", "sts-deleted", "uid-deleted-sts"),
		})

	eDeploy := mustBuild(testEntityBuilder("apps", "v1", "Deployment", "default", "deploy-a").
		WithUnstructured(types.KeyClusterEntity, uDeploy))
	ePod := mustBuild(testEntityBuilder("", "v1", "Pod", "default", "pod-a").
		WithUnstructured(types.KeyClusterEntity, uPod))

	entities, err := NewEntities([]Entity{eDeploy, ePod})
	require.NoError(t, err)

	orphans := entities.OrphanedEntities(types.KeyClusterEntity)
	assert.Equal(t, 0, orphans.Len())
}

func TestOrphanedEntities_NoOwnerReferencesIsRoot(t *testing.T) {
	uDeploy := makeTestUnstructured("apps", "v1", "Deployment", "default", "deploy-a", "uid-deploy", nil)
	uPod := makeTestUnstructured("", "v1", "Pod", "default", "pod-a", "uid-pod",
		[]map[string]any{ownerRef("apps/v1", "Deployment", "deploy-a", "uid-deploy")})

	eDeploy := mustBuild(testEntityBuilder("apps", "v1", "Deployment", "default", "deploy-a").
		WithUnstructured(types.KeyClusterEntity, uDeploy))
	ePod := mustBuild(testEntityBuilder("", "v1", "Pod", "default", "pod-a").
		WithUnstructured(types.KeyClusterEntity, uPod))

	entities, err := NewEntities([]Entity{eDeploy, ePod})
	require.NoError(t, err)

	orphans := entities.OrphanedEntities(types.KeyClusterEntity)
	assert.Equal(t, 0, orphans.Len())
}

func TestOrphanedEntities_NoUnstructuredData(t *testing.T) {
	eNoUnstructured := makeTestEntity("", "v1", "ConfigMap", "default", "cm-a")

	uPod := makeTestUnstructured("", "v1", "Pod", "default", "pod-a", "uid-pod",
		[]map[string]any{ownerRef("apps/v1", "ReplicaSet", "rs-deleted", "uid-deleted-rs")})
	ePod := mustBuild(testEntityBuilder("", "v1", "Pod", "default", "pod-a").
		WithUnstructured(types.KeyClusterEntity, uPod))

	entities, err := NewEntities([]Entity{eNoUnstructured, ePod})
	require.NoError(t, err)

	orphans := entities.OrphanedEntities(types.KeyClusterEntity)
	require.Equal(t, 1, orphans.Len())

	ids := orphanIds(t, orphans)
	assert.Contains(t, ids, types.Id("v1/Pod/default/pod-a"))
	assert.NotContains(t, ids, types.Id("v1/ConfigMap/default/cm-a"))
}

func TestOrphanedEntities_MultiLevelChain(t *testing.T) {
	// Deployment exists, but STS and external Deployment are deleted.
	// Pod → ownerRef(STS deleted) → orphaned
	// RS  → ownerRef(external Deployment deleted) → orphaned
	// Deployment has no ownerRefs → root, not orphaned
	uDeploy := makeTestUnstructured("apps", "v1", "Deployment", "default", "deploy-a", "uid-deploy", nil)
	uPod := makeTestUnstructured("", "v1", "Pod", "default", "pod-a", "uid-pod",
		[]map[string]any{ownerRef("apps/v1", "StatefulSet", "sts-deleted", "uid-deleted-sts")})
	uRS := makeTestUnstructured("apps", "v1", "ReplicaSet", "default", "rs-a", "uid-rs",
		[]map[string]any{ownerRef("apps/v1", "Deployment", "deploy-external", "uid-deleted-deploy")})

	eDeploy := mustBuild(testEntityBuilder("apps", "v1", "Deployment", "default", "deploy-a").
		WithUnstructured(types.KeyClusterEntity, uDeploy))
	ePod := mustBuild(testEntityBuilder("", "v1", "Pod", "default", "pod-a").
		WithUnstructured(types.KeyClusterEntity, uPod))
	eRS := mustBuild(testEntityBuilder("apps", "v1", "ReplicaSet", "default", "rs-a").
		WithUnstructured(types.KeyClusterEntity, uRS))

	entities, err := NewEntities([]Entity{eDeploy, ePod, eRS})
	require.NoError(t, err)

	orphans := entities.OrphanedEntities(types.KeyClusterEntity)
	require.Equal(t, 2, orphans.Len())

	ids := orphanIds(t, orphans)
	assert.Contains(t, ids, types.Id("v1/Pod/default/pod-a"))
	assert.Contains(t, ids, types.Id("apps/v1/ReplicaSet/default/rs-a"))
	assert.NotContains(t, ids, types.Id("apps/v1/Deployment/default/deploy-a"))
}
