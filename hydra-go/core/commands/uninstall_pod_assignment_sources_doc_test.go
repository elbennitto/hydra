package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

// TestPodClusterAppAssignmentSources_Doc ties the three v1/Pod cluster assignment mechanisms
// documented on [AssignClusterEntitiesToAtMostOneAppByRefs] to concrete helpers / phases in this package.
func TestPodClusterAppAssignmentSources_Doc(t *testing.T) {
	t.Run("template_id_wins_via_reconcileClusterOwnership", func(t *testing.T) {
		id := types.Id("v1/Pod/ns/my-pod")
		app := types.AppId("chart.app")
		tpl := map[types.Id]types.AppId{id: app}
		owner, ok, err := reconcileClusterOwnership(tpl, id, sets.New[types.AppId]())
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, app, owner)
	})

	t.Run("ref_predicate_single_app_assigns", func(t *testing.T) {
		id := types.Id("v1/Pod/ns/my-pod")
		a := types.AppId("a.app")
		owner, ok, err := reconcileClusterOwnership(map[types.Id]types.AppId{}, id, sets.New[types.AppId](a))
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, a, owner)
	})

	t.Run("owner_ref_chain_via_expandAssignmentByOwnerRefs", func(t *testing.T) {
		deploy := makeClusterInventoryEntity("apps", "v1", "Deployment", "default", "web", "uid-deploy", nil)
		rs := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "default", "web-abc", "uid-rs",
			[]map[string]any{clusterInventoryOwnerRef("apps/v1", "Deployment", "web", "uid-deploy")})
		pod := makeClusterInventoryEntity("", "v1", "Pod", "default", "web-abc-xyz", "uid-pod",
			[]map[string]any{clusterInventoryOwnerRef("apps/v1", "ReplicaSet", "web-abc", "uid-rs")})
		ents, err := entity.NewEntities([]entity.Entity{deploy, rs, pod})
		require.NoError(t, err)
		deployID, err := deploy.Id()
		require.NoError(t, err)
		podID, err := pod.Id()
		require.NoError(t, err)
		app := types.AppId("my.app")
		assignment := map[types.Id]types.AppId{deployID: app}
		require.NoError(t, expandAssignmentByOwnerRefs(ents, assignment, types.KeyClusterEntity, nil))
		assert.Equal(t, app, assignment[podID])
	})
}
