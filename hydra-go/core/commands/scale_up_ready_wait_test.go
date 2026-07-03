package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// Regression: transitive global.hydra.ready checks call dynamic Get using GVR; some entities
// (e.g. Kyverno-cloned Secrets) carry GVK but not the REST resource name.
func TestGvrForTransitiveReady_SecretWithoutResource(t *testing.T) {
	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name("image-pull-secret")).
		WithNamespace(types.Namespace("monitoring")))

	_, gvrErr := e.GVR()
	require.Error(t, gvrErr)

	gvr, err := gvrForTransitiveReady(e)
	require.NoError(t, err)
	require.Equal(t, types.NewGVR("", "v1", "secrets"), gvr)
}

func TestGvrForTransitiveReady_StillUsesStoredResource(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("x")).
		WithNamespace(types.Namespace("y")))

	gvr, err := gvrForTransitiveReady(e)
	require.NoError(t, err)
	require.Equal(t, types.NewGVR("apps", "v1", "deployments"), gvr)
}
