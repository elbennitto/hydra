package cel

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestCompilePredicate_RejectsEmptyExpressionWithHelpfulMessage(t *testing.T) {
	t.Parallel()

	env, err := NewEnv()
	require.NoError(t, err)

	_, err = env.CompilePredicate(types.CelPredicate("   "))
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
	require.Contains(t, err.Error(), "<empty>")
	require.Contains(t, err.Error(), "no source location passed")
}

func TestCompilePredicate_InvalidSyntaxIncludesExpression(t *testing.T) {
	t.Parallel()

	env, err := NewEnv()
	require.NoError(t, err)

	bad := `kind ==`
	_, err = env.CompilePredicate(types.CelPredicate(bad))
	require.Error(t, err)
	require.Contains(t, err.Error(), "CEL compile failed for expression")
	require.Contains(t, err.Error(), bad)
}

func TestCompileSelectedPredicate_IncludesSelectorOriginOnEmptyCel(t *testing.T) {
	t.Parallel()

	env, err := NewEnv()
	require.NoError(t, err)

	_, err = env.CompileSelectedPredicate(
		types.RefSelector{Kind: "Pod", Namespace: "kube-system"},
		types.CelPredicate("   "),
	)
	require.Error(t, err)
	msg := err.Error()
	require.Contains(t, msg, "Pod")
	require.Contains(t, msg, "kube-system")
	require.Contains(t, msg, "must not be empty")
	require.NotContains(t, msg, "no source location passed")
}
