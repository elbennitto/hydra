package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestBuildResourceModel_RequiresAtLeastOneInputSide(t *testing.T) {
	t.Parallel()

	model, err := BuildResourceModel(ResourceModelInput{}, false)

	require.Error(t, err)
	assert.Nil(t, model)
	assert.Contains(t, err.Error(), "requires template entities, cluster entities, or both")
}

func TestBuildResourceModel_LocalOnlyAssignsTemplateApp(t *testing.T) {
	t.Parallel()

	appID := types.AppId("in-cluster.demo.service-api")
	deploy := mustBuild(entity.NewEntityBuilder().
		WithGroup("apps").
		WithVersion("v1").
		WithResource("deployments").
		WithKind("Deployment").
		WithNamespace("demo").
		WithNamespaced(true).
		WithName("service-api").
		WithAppId(appID))
	templates, err := entity.NewEntities([]entity.Entity{deploy})
	require.NoError(t, err)

	model, err := BuildResourceModel(ResourceModelInput{
		TemplateEntities: &templates,
		PerAppTemplateEntities: map[types.AppId]entity.Entities{
			appID: templates,
		},
		NetworkMode: types.HelmNetworkModeOffline,
		Bootstrap:   types.BootstrapNo,
	}, false)

	require.NoError(t, err)
	id, err := deploy.Id()
	require.NoError(t, err)
	row, ok := model.Row(id)
	require.True(t, ok)
	assert.True(t, row.HasTemplate)
	assert.False(t, row.HasCluster)
	assert.True(t, row.HasAssignedApp)
	assert.Equal(t, appID, row.AssignedApp)
	assert.False(t, row.Unassigned)
}

func TestBuildResourceModel_DuplicateTemplateResourceCollectsAllApps(t *testing.T) {
	t.Parallel()

	appA := types.AppId("in-cluster.root.a")
	appB := types.AppId("in-cluster.root.b")
	idEntity := func(app types.AppId) entity.Entity {
		return mustBuild(entity.NewEntityBuilder().
			WithGroup("").
			WithVersion("v1").
			WithResource("configmaps").
			WithKind("ConfigMap").
			WithNamespace("shared").
			WithNamespaced(true).
			WithName("same").
			WithAppId(app))
	}
	entA := idEntity(appA)
	entB := idEntity(appB)
	all, err := entity.NewEntities([]entity.Entity{entA, entB})
	require.NoError(t, err)
	entsA, err := entity.NewEntities([]entity.Entity{entA})
	require.NoError(t, err)
	entsB, err := entity.NewEntities([]entity.Entity{entB})
	require.NoError(t, err)

	_, err = BuildResourceModel(ResourceModelInput{
		TemplateEntities: &all,
		PerAppTemplateEntities: map[types.AppId]entity.Entities{
			appA: entsA,
			appB: entsB,
		},
		NetworkMode: types.HelmNetworkModeOffline,
		Bootstrap:   types.BootstrapNo,
	}, false)

	require.Error(t, err)
	var dup DuplicateTemplateResourceError
	require.ErrorAs(t, err, &dup)
	require.Len(t, dup.Conflicts, 1)
	for _, apps := range dup.Conflicts {
		assert.ElementsMatch(t, []types.AppId{appA, appB}, apps)
	}
}

func TestResourceModel_SetClusterEntitiesInvalidatesClusterTopologyCache(t *testing.T) {
	t.Parallel()

	rootA := makeClusterInventoryEntity("apps", "v1", "Deployment", "ns", "root-a", "uid-root-a", nil)
	childA := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "ns", "child-a", "uid-child-a", []map[string]any{
		{"apiVersion": "apps/v1", "kind": "Deployment", "name": "root-a", "uid": "uid-root-a"},
	})
	first := mustEntities(t, []entity.Entity{childA, rootA})

	rootB := makeClusterInventoryEntity("apps", "v1", "Deployment", "ns", "root-b", "uid-root-b", nil)
	childB := makeClusterInventoryEntity("apps", "v1", "ReplicaSet", "ns", "child-b", "uid-child-b", []map[string]any{
		{"apiVersion": "apps/v1", "kind": "Deployment", "name": "root-b", "uid": "uid-root-b"},
	})
	second := mustEntities(t, []entity.Entity{childB, rootB})

	model := &ResourceModel{}
	model.SetClusterEntities(first)

	firstIndex, err := model.clusterOwnerRefChildIndex()
	require.NoError(t, err)
	firstOrder, err := model.clusterRefOwnershipEntityIndexesRootFirst(nil, nil, nil, true)
	require.NoError(t, err)

	rootAID, err := rootA.Id()
	require.NoError(t, err)
	childAID, err := childA.Id()
	require.NoError(t, err)
	require.Equal(t, []types.Id{childAID}, firstIndex.childrenByOwner[rootAID])
	require.Equal(t, 2, len(firstOrder))

	model.SetClusterEntities(second)

	secondIndex, err := model.clusterOwnerRefChildIndex()
	require.NoError(t, err)
	secondOrder, err := model.clusterRefOwnershipEntityIndexesRootFirst(nil, nil, nil, true)
	require.NoError(t, err)

	rootBID, err := rootB.Id()
	require.NoError(t, err)
	childBID, err := childB.Id()
	require.NoError(t, err)
	assert.Nil(t, secondIndex.childrenByOwner[rootAID])
	assert.Equal(t, []types.Id{childBID}, secondIndex.childrenByOwner[rootBID])
	require.Equal(t, 2, len(secondOrder))
	secondOrderedIDs := make([]types.Id, 0, len(secondOrder))
	for _, idx := range secondOrder {
		id, err := second.Items[idx].Id()
		require.NoError(t, err)
		secondOrderedIDs = append(secondOrderedIDs, id)
	}
	assert.Equal(t, rootBID, secondOrderedIDs[0])
	assert.Equal(t, childBID, secondOrderedIDs[1])
}

func TestResolveResourceModelAssignmentConflictDetails_AddsDetailedReasonsSilently(t *testing.T) {
	t.Parallel()

	previous := assignClusterEntitiesToAtMostOneAppByRefsImpl
	defer func() {
		assignClusterEntitiesToAtMostOneAppByRefsImpl = previous
	}()

	ambiguousID := types.Id("v1/ConfigMap/ns/shared")
	stableID := types.Id("apps/v1/Deployment/ns/stable")
	appA := types.AppId("a.app")
	appB := types.AppId("b.app")
	stableApp := types.AppId("stable.app")

	baseAssignment := map[types.Id]types.AppId{
		stableID: stableApp,
	}
	baseMetadata := ClusterEntityAssignmentMetadata{
		AmbiguousAppIDsByClusterEntity: map[types.Id][]types.AppId{
			ambiguousID: {appA, appB},
		},
		AmbiguousAppReasonsByClusterEntity: map[types.Id]map[types.AppId][]AssignmentReason{},
	}
	detailedAssignment := map[types.Id]types.AppId{}
	detailedMetadata := ClusterEntityAssignmentMetadata{
		AmbiguousAppIDsByClusterEntity: map[types.Id][]types.AppId{
			ambiguousID: {appA, appB},
		},
		AmbiguousAppReasonsByClusterEntity: map[types.Id]map[types.AppId][]AssignmentReason{
			ambiguousID: {
				appA: {{Kind: AssignmentReasonKindAssignedViaTemplateID}},
				appB: {{Kind: AssignmentReasonKindAssignedViaInspectRef}},
			},
		},
	}

	assignClusterEntitiesToAtMostOneAppByRefsImpl = func(
		in AssignClusterEntitiesToAtMostOneAppByRefsInput,
	) (map[types.Id]types.AppId, ClusterEntityAssignmentMetadata, entity.Entities, error) {
		assert.Nil(t, in.Progress)
		assert.Zero(t, in.ProgressPrefixSteps)
		assert.Zero(t, in.ProgressGrandTotal)
		assert.Equal(t, []types.Id{ambiguousID}, in.ConflictDetailIDs)
		assert.Equal(t, []types.Id{ambiguousID}, in.FocusEntityIDs)
		assert.Equal(t, map[types.Id]types.AppId{stableID: stableApp}, in.InitialAssignment)
		return detailedAssignment, detailedMetadata, entity.Entities{}, nil
	}

	assignment, metadata, err := resolveResourceModelAssignmentConflictDetails(
		AssignClusterEntitiesToAtMostOneAppByRefsInput{},
		baseAssignment,
		baseMetadata,
	)
	require.NoError(t, err)
	assert.Equal(t, baseAssignment, assignment)
	assert.Equal(t, detailedMetadata.AmbiguousAppIDsByClusterEntity, metadata.AmbiguousAppIDsByClusterEntity)
	assert.Equal(t, detailedMetadata.AmbiguousAppReasonsByClusterEntity, metadata.AmbiguousAppReasonsByClusterEntity)
}
