package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestValidateClusterApplyBootstrapGuard_BootstrapAndSkipMutuallyExclusive(t *testing.T) {
	err := ValidateClusterApplyBootstrapGuard(
		log.Default(),
		nil,
		nil,
		types.HelmNetworkModeOffline,
		entity.Entities{},
		types.BootstrapYes,
		true,
		true,
	)
	require.Error(t, err)
	assert.True(t, errors.ErrBootstrapGuard.MatchesError(err))
}

func TestValidateClusterApplyBootstrapGuard_EnforceOffSkipsValidation(t *testing.T) {
	err := ValidateClusterApplyBootstrapGuard(
		log.Default(),
		nil,
		nil,
		types.HelmNetworkModeOffline,
		entity.Entities{},
		types.BootstrapNo,
		true,
		false,
	)
	require.NoError(t, err)
}

func TestMatchBootstrapGuardEntities_MatchesImagePullSopsSecret(t *testing.T) {
	e := makeSopsSecretEntity("sops-secrets-operator", "image-pull-secret", []any{
		map[string]any{
			"name":       "image-pull-secret",
			"stringData": map[string]any{".dockerconfigjson": "ENC[x]"},
		},
	})
	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	env, err := cel.NewEnvWithEntityInventory(entities)
	require.NoError(t, err)

	predicates := []string{
		`gvk == "isindir.github.com/v1alpha3/SopsSecret" && ns == "sops-secrets-operator" && name == "image-pull-secret"`,
	}
	ids, err := matchBootstrapGuardEntities(env, predicates, entities)
	require.NoError(t, err)
	assert.Equal(t, 1, ids.Len())
	list := ids.UnsortedList()
	require.Len(t, list, 1)
	assert.Equal(t, types.Id("isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret"), list[0])
}
