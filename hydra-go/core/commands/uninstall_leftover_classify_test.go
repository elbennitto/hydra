package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestSoleOriginAppFromGeneratedControllerRefs(t *testing.T) {
	kyverno := types.AppId("in-cluster.cluster-infra.kyverno")
	secretID := types.Id("v1/Secret/default/image-pull-secret")
	from := types.Id("kyverno.io/v1/ClusterPolicy//sync-pull-secret")
	refs := []types.Ref{
		{
			From: from,
			To:   secretID,
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: string(kyverno)},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
	}
	app, ok := soleOriginAppFromGeneratedControllerRefs(refs, secretID)
	require.True(t, ok)
	assert.Equal(t, kyverno, app)

	_, ok = soleOriginAppFromGeneratedControllerRefs(refs, types.Id("v1/ConfigMap/default/other"))
	assert.False(t, ok)

	// Two different origin:app values for the same target id → not sole.
	refsAmb := []types.Ref{
		{
			To: secretID,
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: "a.app"},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
		{
			To: secretID,
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: "b.app"},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
	}
	_, ok = soleOriginAppFromGeneratedControllerRefs(refsAmb, secretID)
	assert.False(t, ok)

	// origin:app without origin:generated controller is ignored.
	refsNoGen := []types.Ref{
		{
			To: secretID,
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: string(kyverno)},
			},
		},
	}
	_, ok = soleOriginAppFromGeneratedControllerRefs(refsNoGen, secretID)
	assert.False(t, ok)
}

func TestMergeForceLeftoversOwnedByCloneRulesIntoUninstalls_NoForceLeftovers(t *testing.T) {
	u, err := entity.NewEntities(nil)
	require.NoError(t, err)
	f, err := entity.NewEntities(nil)
	require.NoError(t, err)
	outU, outF, err := MergeForceLeftoversOwnedByCloneRulesIntoUninstalls(
		nil, u, f, sets.New[types.AppId](), nil, entity.Entities{}, entity.Entities{}, nil)
	require.NoError(t, err)
	require.Equal(t, 0, outU.Len())
	require.Equal(t, 0, outF.Len())
}

func TestMergeForceLeftoversOwnedByCloneRulesIntoUninstalls_NoSelectionLeavesForce(t *testing.T) {
	sec := makeClusterInventoryEntity("", "v1", "Secret", "default", "image-pull-secret", "uid-1", nil)
	force, err := entity.NewEntities([]entity.Entity{sec})
	require.NoError(t, err)
	u, err := entity.NewEntities(nil)
	require.NoError(t, err)
	outU, outF, err := MergeForceLeftoversOwnedByCloneRulesIntoUninstalls(
		nil, u, force, sets.New[types.AppId](), nil, entity.Entities{}, entity.Entities{}, nil)
	require.NoError(t, err)
	require.Equal(t, 0, outU.Len())
	require.Equal(t, 1, outF.Len())
}

func TestMergeForceLeftoversOwnedByCloneRulesIntoUninstalls_GeneratedControllerRefsBecomeUninstalls(t *testing.T) {
	kyverno := types.AppId("in-cluster.cluster-infra.kyverno")
	other := types.AppId("in-cluster.cluster-infra.other")
	secretDefault := makeEntity("", "v1", "Secret", "default", "image-pull-secret")
	secretKubeSystem := makeEntity("", "v1", "Secret", "kube-system", "image-pull-secret")
	force, err := entity.NewEntities([]entity.Entity{secretDefault, secretKubeSystem})
	require.NoError(t, err)
	uninstalls, err := entity.NewEntities(nil)
	require.NoError(t, err)
	refs := []types.Ref{
		{
			From: types.Id("kyverno.io/v1/ClusterPolicy//clone-image-pull-secret"),
			To:   types.Id("v1/Secret/default/image-pull-secret"),
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: string(kyverno)},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
		{
			From: types.Id("kyverno.io/v1/ClusterPolicy//clone-image-pull-secret-to-kube-system"),
			To:   types.Id("v1/Secret/kube-system/image-pull-secret"),
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: string(kyverno)},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
	}

	outU, outF, err := MergeForceLeftoversOwnedByCloneRulesIntoUninstalls(
		nil,
		uninstalls,
		force,
		sets.New[types.AppId](kyverno),
		nil,
		entity.Entities{},
		entity.Entities{},
		refs,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, outU.Len())
	assert.Equal(t, 0, outF.Len())

	outU, outF, err = MergeForceLeftoversOwnedByCloneRulesIntoUninstalls(
		nil,
		uninstalls,
		force,
		sets.New[types.AppId](other),
		nil,
		entity.Entities{},
		entity.Entities{},
		refs,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, outU.Len())
	assert.Equal(t, 2, outF.Len())
}

func TestClassifyLeftoversUninstallForce_FallsBackToGeneratedControllerOriginApp(t *testing.T) {
	cluster, kyverno, other := buildBackupSelectionTestCluster(t)
	secretDefault := makeEntity("", "v1", "Secret", "default", "image-pull-secret")
	secretKubeSystem := makeEntity("", "v1", "Secret", "kube-system", "image-pull-secret")
	leftovers := mustEntities(t, []entity.Entity{secretDefault, secretKubeSystem})
	refs := []types.Ref{
		{
			From: types.Id("kyverno.io/v1/ClusterPolicy//clone-image-pull-secret"),
			To:   types.Id("v1/Secret/default/image-pull-secret"),
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: string(kyverno)},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
		{
			From: types.Id("kyverno.io/v1/ClusterPolicy//clone-image-pull-secret-to-kube-system"),
			To:   types.Id("v1/Secret/kube-system/image-pull-secret"),
			Attributes: []types.RefAttribute{
				{Type: types.RefAttributeOriginApp, Value: string(kyverno)},
				{Type: types.RefAttributeOriginGenerated, Value: types.RefGeneratedController},
			},
		},
	}
	perAppRendered := map[types.AppId]entity.Entities{
		kyverno: mustEntities(t, []entity.Entity{makeEntity("", "v1", "ConfigMap", "kyverno", "values")}),
		other:   mustEntities(t, []entity.Entity{makeEntity("", "v1", "ConfigMap", "other", "cfg")}),
	}

	t.Run("selected app becomes force leftover", func(t *testing.T) {
		force, warn, ignored, err := ClassifyLeftoversUninstallForce(
			cluster,
			leftovers,
			sets.New[types.AppId](kyverno),
			sets.New[types.AppId](kyverno, other),
			perAppRendered,
			refs,
			entity.Entities{},
		)
		require.NoError(t, err)
		assert.Len(t, force.Items, 2)
		assert.Len(t, warn.Items, 0)
		assert.Len(t, ignored.Items, 0)
	})

	t.Run("non-selected app becomes ignored leftover", func(t *testing.T) {
		force, warn, ignored, err := ClassifyLeftoversUninstallForce(
			cluster,
			leftovers,
			sets.New[types.AppId](other),
			sets.New[types.AppId](kyverno, other),
			perAppRendered,
			refs,
			entity.Entities{},
		)
		require.NoError(t, err)
		assert.Len(t, force.Items, 0)
		assert.Len(t, warn.Items, 0)
		assert.Len(t, ignored.Items, 2)
	})
}
