package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	corehydra "hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestApplyClusterRootOverrides_AssignsGenericAppAndRespectsExclude(t *testing.T) {
	t.Parallel()

	groupDir := t.TempDir()
	contextDir := filepath.Join(groupDir, "ctx")
	clusterDir := filepath.Join(contextDir, "cluster-a")
	require.NoError(t, os.MkdirAll(clusterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(groupDir, "values.yaml"), []byte("global:\n  hydra:\n    type: group\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "values.yaml"), []byte("global:\n  hydra:\n    type: cluster\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "hydra.yaml"), []byte(`
hydra:
  overrides:
    cluster-infra.dex:
      - gvk: apps/v1/Deployment
        namespace: ns
      - id: "apps/v1/Deployment/ns/excluded"
        exclude: true
`), 0o644))

	ctx, err := corehydra.CreateContext(log.Default(), contextDir, types.NewConfig(types.ColorNo, types.DryRunYes, types.KubernetesConnectionAllowedNo, true))
	require.NoError(t, err)
	cluster, err := corehydra.NewCluster(ctx, "cluster-a", corehydra.RESTClientLimits{})
	require.NoError(t, err)

	mustEntity := func(name string) entity.Entity {
		e, err := entity.NewEntityBuilder().
			WithGroup("apps").
			WithVersion("v1").
			WithKind("Deployment").
			WithNamespace("ns").
			WithName(types.Name(name)).
			WithNamespaced(types.NamespacedYes).
			Build()
		require.NoError(t, err)
		return e
	}
	live, err := entity.NewEntities([]entity.Entity{
		mustEntity("owned"),
		mustEntity("excluded"),
	})
	require.NoError(t, err)
	rendered, err := entity.NewEntities(nil)
	require.NoError(t, err)

	target := types.AppId("cluster-a.cluster-infra.dex")
	other := types.AppId("cluster-a.argocd")
	ownedID := types.Id("apps/v1/Deployment/ns/owned")
	excludedID := types.Id("apps/v1/Deployment/ns/excluded")

	assignment := map[types.Id]types.AppId{
		ownedID:    other,
		excludedID: other,
	}
	assignmentReasons := map[types.Id]map[types.AppId][]AssignmentReason{}
	meta := ClusterEntityAssignmentMetadata{
		AmbiguousAppIDsByClusterEntity: map[types.Id][]types.AppId{
			ownedID: {other, target},
		},
		AmbiguousAppReasonsByClusterEntity: map[types.Id]map[types.AppId][]AssignmentReason{},
		UnassignedIDs:                      sets.New[types.Id](ownedID),
	}
	ambiguous := sets.New[types.Id](ownedID)

	err = applyClusterRootOverrides(
		cluster,
		sets.New[types.AppId](target, other),
		rendered,
		live,
		assignment,
		assignmentReasons,
		&meta,
		ambiguous,
	)
	require.NoError(t, err)

	require.Equal(t, target, assignment[ownedID])
	require.Equal(t, other, assignment[excludedID])
	require.Equal(t, AssignmentReasonKindAssignedViaClusterRootOverride, assignmentReasons[ownedID][target][0].Kind)
	require.NotContains(t, meta.AmbiguousAppIDsByClusterEntity, ownedID)
	require.False(t, ambiguous.Has(ownedID))
	require.False(t, meta.UnassignedIDs.Has(ownedID))
}
