package entity

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestClusterInventoryRootEntities_KeepsRootsDropsChildren(t *testing.T) {
	t.Parallel()
	key := types.KeyClusterEntity

	dep := makeTestUnstructured("apps", "v1", "Deployment", "kube-system", "coredns", "uid-dep", nil)
	rs := makeTestUnstructured("apps", "v1", "ReplicaSet", "kube-system", "coredns-hash", "uid-rs", []map[string]any{
		ownerRef("apps/v1", "Deployment", "coredns", "uid-dep"),
	})
	pod := makeTestUnstructured("", "v1", "Pod", "kube-system", "coredns-hash-abc", "uid-pod", []map[string]any{
		ownerRef("apps/v1", "ReplicaSet", "coredns-hash", "uid-rs"),
	})
	orphanPod := makeTestUnstructured("", "v1", "Pod", "kube-system", "orphan", "uid-orphan", []map[string]any{
		ownerRef("apps/v1", "ReplicaSet", "gone-rs", "uid-missing"),
	})

	ents, err := NewEntities([]Entity{
		mustEntityFromU(t, key, dep),
		mustEntityFromU(t, key, rs),
		mustEntityFromU(t, key, pod),
		mustEntityFromU(t, key, orphanPod),
	})
	require.NoError(t, err)

	roots, err := ents.ClusterInventoryRootEntities(key)
	require.NoError(t, err)
	require.Equal(t, 2, roots.Len())
	ids := make([]types.Id, roots.Len())
	for i := range roots.Items {
		id, err := roots.Items[i].Id()
		require.NoError(t, err)
		ids[i] = id
	}
	require.Contains(t, ids, types.Id("apps/v1/Deployment/kube-system/coredns"))
	require.Contains(t, ids, types.Id("v1/Pod/kube-system/orphan"))
}

func TestClusterInventoryEntitiesExcludingOwnedChildren_UsesExternalUIDMap(t *testing.T) {
	t.Parallel()
	key := types.KeyClusterEntity

	dep := makeTestUnstructured("apps", "v1", "Deployment", "kube-system", "coredns", "uid-dep", nil)
	rs := makeTestUnstructured("apps", "v1", "ReplicaSet", "kube-system", "coredns-hash", "uid-rs", []map[string]any{
		ownerRef("apps/v1", "Deployment", "coredns", "uid-dep"),
	})
	pod := makeTestUnstructured("", "v1", "Pod", "kube-system", "coredns-hash-abc", "uid-pod", []map[string]any{
		ownerRef("apps/v1", "ReplicaSet", "coredns-hash", "uid-rs"),
	})

	full, err := NewEntities([]Entity{
		mustEntityFromU(t, key, dep),
		mustEntityFromU(t, key, rs),
		mustEntityFromU(t, key, pod),
	})
	require.NoError(t, err)
	fullMap := full.UidMap(key)

	// Simulate CEL filter that only selects workloads under the Deployment.
	candidates, err := NewEntities([]Entity{
		mustEntityFromU(t, key, rs),
		mustEntityFromU(t, key, pod),
	})
	require.NoError(t, err)

	filtered, err := candidates.ClusterInventoryEntitiesExcludingOwnedChildren(key, fullMap)
	require.NoError(t, err)
	require.Equal(t, 0, filtered.Len(), "ReplicaSet and Pod are owned within the full inventory and must be skipped")
}

func mustEntityFromU(t *testing.T, key types.EntityKeyUnstructured, u unstructured.Unstructured) Entity {
	t.Helper()
	gvk := u.GroupVersionKind()
	e, err := NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(gvk.Group), types.Version(gvk.Version), types.Kind(gvk.Kind))).
		WithName(types.Name(u.GetName())).
		WithNamespace(types.Namespace(u.GetNamespace())).
		WithUnstructured(key, u).
		Build()
	require.NoError(t, err)
	return e
}

func TestClusterInventoryRootOf_WalksOwnerChainToDeployment(t *testing.T) {
	t.Parallel()
	key := types.KeyClusterEntity

	dep := makeTestUnstructured("apps", "v1", "Deployment", "ns1", "web", "uid-dep", nil)
	rs := makeTestUnstructured("apps", "v1", "ReplicaSet", "ns1", "web-hash", "uid-rs", []map[string]any{
		ownerRef("apps/v1", "Deployment", "web", "uid-dep"),
	})
	pod := makeTestUnstructured("", "v1", "Pod", "ns1", "web-abc", "uid-pod", []map[string]any{
		ownerRef("apps/v1", "ReplicaSet", "web-hash", "uid-rs"),
	})
	podEnt := mustEntityFromU(t, key, pod)

	full, err := NewEntities([]Entity{
		mustEntityFromU(t, key, dep),
		mustEntityFromU(t, key, rs),
		podEnt,
	})
	require.NoError(t, err)
	uidMap := full.UidMap(key)

	root := ClusterInventoryRootOf(podEnt, key, uidMap)
	id, err := root.Id()
	require.NoError(t, err)
	require.Equal(t, types.Id("apps/v1/Deployment/ns1/web"), id)
}
