package cel

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestCompileSelectedPredicate_SelectorOnly(t *testing.T) {
	t.Parallel()

	env, err := NewEnv()
	require.NoError(t, err)

	pred, err := env.CompileSelectedPredicate(types.RefSelector{Kind: "Node"})
	require.NoError(t, err)

	node := mustBuildEntity(t, "", "v1", "Node", "", "n1")
	pod := mustBuildEntity(t, "", "v1", "Pod", "kube-system", "p1")

	okNode, err := pred.EvalBool(node, types.MissingKeysReject)
	require.NoError(t, err)
	okPod, err := pred.EvalBool(pod, types.MissingKeysReject)
	require.NoError(t, err)
	require.True(t, okNode)
	require.False(t, okPod)
}

func TestCompileSelectedPredicate_SelectorAndCel(t *testing.T) {
	t.Parallel()

	env, err := NewEnv()
	require.NoError(t, err)

	pred, err := env.CompileSelectedPredicate(
		types.RefSelector{Kind: "Deployment", Namespace: "kube-system"},
		types.CelPredicate(`name == "match"`),
	)
	require.NoError(t, err)

	wrongNS := mustBuildEntity(t, "apps", "v1", "Deployment", "default", "match")
	right := mustBuildEntity(t, "apps", "v1", "Deployment", "kube-system", "match")

	okWrongNS, err := pred.EvalBool(wrongNS, types.MissingKeysReject)
	require.NoError(t, err)
	okRight, err := pred.EvalBool(right, types.MissingKeysReject)
	require.NoError(t, err)
	require.False(t, okWrongNS)
	require.True(t, okRight)
}

func mustBuildEntity(t *testing.T, group string, version string, kind string, ns string, name string) entity.Entity {
	t.Helper()
	builder := entity.NewEntityBuilder().
		WithGroup(types.Group(group)).
		WithVersion(types.Version(version)).
		WithKind(types.Kind(kind)).
		WithName(types.Name(name))
	if ns != "" {
		builder = builder.WithNamespace(types.Namespace(ns))
	}
	e, err := builder.Build()
	require.NoError(t, err)
	return e
}
