package cel

import (
	"testing"

	ctypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type countingReader struct {
	name     string
	delegate reader
	counts   map[string]int
}

func (r countingReader) Read(e entity.Entity) ref.Val {
	r.counts[r.name]++
	return r.delegate.Read(e)
}

func (r countingReader) Type() *ctypes.Type {
	return r.delegate.Type()
}

func instrumentEnvReaders(env *Env) map[string]int {
	counts := map[string]int{}
	wrapped := make(map[types.EntityKey]reader, len(env.keyTypes))
	for key, r := range env.keyTypes {
		wrapped[key] = countingReader{
			name:     key.String(),
			delegate: r,
			counts:   counts,
		}
	}
	env.keyTypes = wrapped
	return counts
}

func TestLazyActivation_DoesNotReadUnstructuredForSimplePredicate(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)
	counts := instrumentEnvReaders(&env)

	predicate, err := env.CompilePredicate(`kind == "Deployment"`)
	require.NoError(t, err)

	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("web")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyEntity, unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"spec": map[string]any{
					"replicas": int64(2),
				},
			},
		}))

	ok, err := predicate.EvalBool(e, types.MissingKeysReject)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 0, counts[types.KeyEntity.String()])
}

func TestLazyActivation_ReadsUnstructuredOnlyWhenUsed(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)
	counts := instrumentEnvReaders(&env)

	predicate, err := env.CompilePredicate(`has(entity.spec.replicas) && entity.spec.replicas == 2`)
	require.NoError(t, err)

	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("web")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyEntity, unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"spec": map[string]any{
					"replicas": int64(2),
				},
			},
		}))

	ok, err := predicate.EvalBool(e, types.MissingKeysReject)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, counts[types.KeyEntity.String()])
}
