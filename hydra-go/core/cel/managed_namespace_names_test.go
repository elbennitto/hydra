package cel_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func mustBuildEntity(b entity.EntityBuilder) entity.Entity {
	e, err := b.Build()
	if err != nil {
		panic(err)
	}
	return e
}

func TestManagedNamespaceNamesFromEntities_FromEntityNamespaces(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	e1 := mustBuildEntity(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("b")).
		WithName(types.Name("x1")))
	e2 := mustBuildEntity(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("a")).
		WithName(types.Name("x2")))
	e3 := mustBuildEntity(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithNamespace(types.Namespace("b")).
		WithName(types.Name("x3")))
	ents, err := entity.NewEntities([]entity.Entity{e1, e2, e3})
	require.NoError(t, err)

	names := cel.ManagedNamespaceNamesFromEntities(ents)
	require.Equal(t, []string{"a", "b"}, names)
}

func TestManagedNamespaceNamesFromEntities_IncludesV1NamespaceResourceName(t *testing.T) {
	nsGVK := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Namespace"))
	deployGVK := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	nsEntity := mustBuildEntity(entity.NewEntityBuilder().
		WithGVK(nsGVK).
		WithName(types.Name("example")))
	deploy := mustBuildEntity(entity.NewEntityBuilder().
		WithGVK(deployGVK).
		WithNamespace(types.Namespace("demo")).
		WithName(types.Name("app")))
	ents, err := entity.NewEntities([]entity.Entity{nsEntity, deploy})
	require.NoError(t, err)

	names := cel.ManagedNamespaceNamesFromEntities(ents)
	require.Equal(t, []string{"demo", "example"}, names)
}
