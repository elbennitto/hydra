package cel

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func intListFromCELValue(t *testing.T, v any) []int64 {
	t.Helper()
	switch x := v.(type) {
	case []int64:
		return x
	case []interface{}:
		out := make([]int64, 0, len(x))
		for _, e := range x {
			switch n := e.(type) {
			case int64:
				out = append(out, n)
			case int:
				out = append(out, int64(n))
			case int32:
				out = append(out, int64(n))
			default:
				require.Fail(t, fmt.Sprintf("unexpected list element type %T", e))
			}
		}
		return out
	default:
		require.Fail(t, fmt.Sprintf("unexpected value type %T", v))
		return nil
	}
}

func TestOrdinalRangeAndVctPvcOrdinalNameCEL(t *testing.T) {
	env, err := NewEnv()
	require.NoError(t, err)

	cases := []struct {
		expr string
		want any
	}{
		{`ordinalRange(0, 3)`, []int64{0, 1, 2}},
		{`ordinalRange(5, 3)`, []int64{5, 6, 7}},
		{`ordinalRange(10, 0)`, []int64{}},
		{`ordinalRange(2, -1)`, []int64{}},
		{`vctPvcOrdinalName('data-web-0', 'data', 'web')`, true},
		{`vctPvcOrdinalName('data-web-12', 'data', 'web')`, true},
		{`vctPvcOrdinalName('data-web-', 'data', 'web')`, false},
		{`vctPvcOrdinalName('data-web-0extra', 'data', 'web')`, false},
		{`vctPvcOrdinalName('data-other-0', 'data', 'web')`, false},
		{`vctPvcOrdinalName('data-web-0', '', 'web')`, false},
	}

	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			expr, err := env.CompileExpression(types.CelExpression(tc.expr))
			require.NoError(t, err)
			v, err := expr.EvalFromMap(map[string]any{})
			require.NoError(t, err)
			if want, ok := tc.want.([]int64); ok {
				require.Equal(t, want, intListFromCELValue(t, v.Value()))
				return
			}
			require.Equal(t, tc.want, v.Value())
		})
	}
}
