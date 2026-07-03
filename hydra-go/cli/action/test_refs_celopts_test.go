package action

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestNewEnvWithEntityInventory_ManagedNamespacesCompiles(t *testing.T) {
	data := `apiVersion: v1
kind: Secret
metadata:
  name: image-pull-secret
  namespace: dex
`
	entities, err := entity.NewEntitiesFromYaml(log.Default(), types.YamlString(data), types.KeyClusterEntity)
	require.NoError(t, err)
	env, err := cel.NewEnvWithEntityInventory(entities)
	require.NoError(t, err)
	_, err = env.CompilePredicate(types.CelPredicate(`size(managedNamespaces()) >= 0`))
	require.NoError(t, err)
}
