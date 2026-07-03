package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestClusterEntityAssignmentMetadata_AmbiguousEntityIDs(t *testing.T) {
	meta := ClusterEntityAssignmentMetadata{
		AmbiguousAppIDsByClusterEntity: map[types.Id][]types.AppId{
			"apps/v1/Deployment/ns/b": {"b.app", "a.app"},
			"v1/ConfigMap/ns/a":       {"a.app", "b.app"},
		},
	}

	assert.Equal(t,
		[]types.Id{
			"apps/v1/Deployment/ns/b",
			"v1/ConfigMap/ns/a",
		},
		meta.AmbiguousEntityIDs(),
	)
}

func TestAssignClusterEntitiesToAtMostOneAppByRefs_PublicAPIAddsConflictDetailsOnAmbiguousResult(t *testing.T) {
	previous := assignClusterEntitiesToAtMostOneAppByRefsImpl
	defer func() {
		assignClusterEntitiesToAtMostOneAppByRefsImpl = previous
	}()

	emptyUnassigned, err := entity.NewEntities(nil)
	require.NoError(t, err)

	ambiguousID := types.Id("v1/ConfigMap/ns/shared")
	appA := types.AppId("a.app")
	appB := types.AppId("b.app")
	baseMetadata := ClusterEntityAssignmentMetadata{
		AmbiguousAppIDsByClusterEntity: map[types.Id][]types.AppId{
			ambiguousID: {appA, appB},
		},
		AmbiguousAppReasonsByClusterEntity: map[types.Id]map[types.AppId][]AssignmentReason{},
	}
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

	callCount := 0
	assignClusterEntitiesToAtMostOneAppByRefsImpl = func(
		in AssignClusterEntitiesToAtMostOneAppByRefsInput,
	) (map[types.Id]types.AppId, ClusterEntityAssignmentMetadata, entity.Entities, error) {
		callCount++
		switch callCount {
		case 1:
			assert.Empty(t, in.ConflictDetailIDs)
			return map[types.Id]types.AppId{}, baseMetadata, emptyUnassigned, nil
		case 2:
			assert.Equal(t, []types.Id{ambiguousID}, in.ConflictDetailIDs)
			return map[types.Id]types.AppId{}, detailedMetadata, emptyUnassigned, nil
		default:
			t.Fatalf("unexpected call %d", callCount)
			return nil, ClusterEntityAssignmentMetadata{}, entity.Entities{}, nil
		}
	}

	assignment, metadata, unassigned, err := AssignClusterEntitiesToAtMostOneAppByRefs(
		AssignClusterEntitiesToAtMostOneAppByRefsInput{},
	)
	require.NoError(t, err)
	assert.Empty(t, assignment)
	assert.Equal(t, detailedMetadata, metadata)
	assert.Equal(t, emptyUnassigned, unassigned)
	assert.Equal(t, 2, callCount)
}
