package record

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvalEnvCEL_PwdVariable(t *testing.T) {
	out, err := evalEnvCEL(`pwd + "/tutorial-context"`, map[string]string{"PWD": "/tmp/demo"})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/demo/tutorial-context", out)
}

func TestEvalCEL_PwdVariable(t *testing.T) {
	ok, err := evalCEL(`pwd == "/tmp/demo"`, "", "", "", map[string]string{"PWD": "/tmp/demo"})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestEvalCEL_PlainFunctionStripsEscapes(t *testing.T) {
	ok, err := evalCEL(`plain(cast).contains("warn") && !plain(cast).contains("\\u001b")`, "", "", "\u001b[31mwarn\u001b[0m\n", map[string]string{"PWD": "/tmp/demo"})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestEvalCEL_PlainFunctionStripsC1ColorEscapes(t *testing.T) {
	ok, err := evalCEL(`plain(cast) == "warning\n"`, "", "", "\u009b31mwarning\u009b0m\n", map[string]string{"PWD": "/tmp/demo"})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestEvalCEL_ExposesStdoutStderrAndCast(t *testing.T) {
	ok, err := evalCEL(`stdout == "out" && stderr == "err" && cast == "outerr"`, "", "out", "err", map[string]string{"PWD": "/tmp/demo"})
	require.NoError(t, err)
	assert.True(t, ok)
}
