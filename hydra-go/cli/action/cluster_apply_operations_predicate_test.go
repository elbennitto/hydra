package action

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestFilterApplyOperationsByResourcePredicate_KeepsMatchingNewAndClearsDeletes(t *testing.T) {
	env, err := cel.NewEnv()
	require.NoError(t, err)
	pred, err := env.CompilePredicate(types.CelPredicate(`kind == "ConfigMap"`))
	require.NoError(t, err)

	cm := makeTestEntity("", "v1", "ConfigMap", "configmaps", "default", "cm1")
	sec := makeTestEntity("", "v1", "Secret", "secrets", "default", "s1")
	orphan := makeTestEntity("", "v1", "Deployment", "deployments", "default", "orph")

	newEnt, err := entity.NewEntities([]entity.Entity{cm, sec})
	require.NoError(t, err)
	delEnt, err := entity.NewEntities([]entity.Entity{orphan})
	require.NoError(t, err)
	empty, err := entity.NewEntities(nil)
	require.NoError(t, err)

	ops := &ApplyOperations{New: newEnt, Update: empty, Replace: empty, Unchanged: empty, Delete: delEnt}
	filtered, err := FilterApplyOperationsByResourcePredicate(ops, pred)
	require.NoError(t, err)
	assert.Equal(t, 1, filtered.New.Len())
	id, err := filtered.New.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("v1/ConfigMap/default/cm1"), id)
	assert.Equal(t, 0, filtered.Delete.Len())
}
