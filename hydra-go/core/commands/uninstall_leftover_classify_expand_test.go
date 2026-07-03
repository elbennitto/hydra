package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Regression: uninstall-force "warn" leftovers that are owned by a workload whose UID is seeded
// (e.g. cluster-defaults builtin match) must move to ignored — same closure as ignored-classified parents.
func TestExpandUninstallForceWarnLeftoversOwnedBySeededUIDs_ChildOfSeededOwner(t *testing.T) {
	const (
		parentUID = "uid-daemonset"
		childUID  = "uid-pod"
	)
	parent := makeClusterInventoryEntity("apps", "v1", "DaemonSet", "kube-system", "kube-flannel", parentUID, nil)
	child := makeClusterInventoryEntity("", "v1", "Pod", "kube-system", "kube-flannel-abc", childUID,
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "DaemonSet", "kube-flannel", parentUID)})

	allLeftovers, err := entity.NewEntities([]entity.Entity{parent, child})
	require.NoError(t, err)

	warn, err := entity.NewEntities([]entity.Entity{child})
	require.NoError(t, err)

	ignored, err := entity.NewEntities(nil)
	require.NoError(t, err)

	seeds := sets.New[types.Uid]()
	seeds.Insert(types.Uid(parentUID))

	warnOut, ignoredOut, err := ExpandUninstallForceWarnLeftoversOwnedBySeededUIDs(allLeftovers, ignored, warn, seeds, entity.Entities{})
	require.NoError(t, err)
	require.Equal(t, 0, warnOut.Len(), "pod owned by seeded parent UID should leave warn")
	require.Equal(t, 1, ignoredOut.Len(), "pod should merge into ignored")
}

func TestExpandUninstallForceWarnLeftoversOwnedBySeededUIDs_NodeOwnerOutsideLeftovers(t *testing.T) {
	const (
		nodeUID = "uid-node-talos"
		podUID  = "uid-mirror-pod"
	)
	node := makeClusterInventoryEntity("", "v1", "Node", "", "talos-192-168-0-50", nodeUID, nil)
	pod := makeClusterInventoryEntity("", "v1", "Pod", "kube-system", "kube-apiserver-talos-192-168-0-50", podUID,
		[]map[string]any{clusterInventoryOwnerRef("v1", "Node", "talos-192-168-0-50", nodeUID)})

	leftovers, err := entity.NewEntities([]entity.Entity{pod})
	require.NoError(t, err)
	fullCluster, err := entity.NewEntities([]entity.Entity{node, pod})
	require.NoError(t, err)

	ignored, err := entity.NewEntities(nil)
	require.NoError(t, err)
	warn, err := entity.NewEntities([]entity.Entity{pod})
	require.NoError(t, err)

	seeds := sets.New[types.Uid](types.Uid(nodeUID))

	warnOut, _, err := ExpandUninstallForceWarnLeftoversOwnedBySeededUIDs(leftovers, ignored, warn, seeds, fullCluster)
	require.NoError(t, err)
	require.Equal(t, 0, warnOut.Len(), "Node-owned pod should leave warn when Node exists only in ownerUIDInventory")

	warnOutNoInv, _, err := ExpandUninstallForceWarnLeftoversOwnedBySeededUIDs(leftovers, ignored, warn, seeds, entity.Entities{})
	require.NoError(t, err)
	require.Equal(t, 1, warnOutNoInv.Len(), "without extended inventory, owner outside leftovers must not close")
}

func TestClusterUIDClosureFromOwnerSeeds_NodeMirrorPod(t *testing.T) {
	const (
		nodeUID = "uid-node-talos"
		podUID  = "uid-mirror-pod"
	)
	node := makeClusterInventoryEntity("", "v1", "Node", "", "talos-192-168-0-50", nodeUID, nil)
	pod := makeClusterInventoryEntity("", "v1", "Pod", "kube-system", "kube-apiserver-talos-192-168-0-50", podUID,
		[]map[string]any{clusterInventoryOwnerRef("v1", "Node", "talos-192-168-0-50", nodeUID)})

	inv, err := entity.NewEntities([]entity.Entity{node, pod})
	require.NoError(t, err)

	seeds := sets.New[types.Uid](types.Uid(nodeUID))
	protected := ClusterUIDClosureFromOwnerSeeds(inv, seeds, nil)

	assert.True(t, protected.Has(types.Uid(nodeUID)))
	assert.True(t, protected.Has(types.Uid(podUID)), "mirror pod owned by seeded Node should be in closure")
}

func TestClusterUIDClosureFromOwnerSeeds_MultiHop(t *testing.T) {
	const (
		rootUID = "uid-root"
		midUID  = "uid-mid"
		leafUID = "uid-leaf"
	)
	root := makeClusterInventoryEntity("apps", "v1", "Deployment", "ns", "web", rootUID, nil)
	mid := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "ns", "web-1", midUID,
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "Deployment", "web", rootUID)})
	leaf := makeClusterInventoryEntity("", "v1", "Pod", "ns", "web-1-abc", leafUID,
		[]map[string]any{clusterInventoryOwnerRef("apps/v1", "ReplicaSet", "web-1", midUID)})

	inv, err := entity.NewEntities([]entity.Entity{root, mid, leaf})
	require.NoError(t, err)

	protected := ClusterUIDClosureFromOwnerSeeds(inv, sets.New[types.Uid](types.Uid(rootUID)), nil)
	assert.True(t, protected.Has(types.Uid(leafUID)))
}
