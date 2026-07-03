package highlight

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestGoTemplateTerminal256_NoColorPassthrough(t *testing.T) {
	in := "apiVersion: v1\n{{ .Values.x }}\n"
	out, err := GoTemplateTerminal256(types.ColorNo, in)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestGoTemplateTerminal256_AddsAnsiWhenColor(t *testing.T) {
	in := "kind: ConfigMap\nmetadata:\n  name: {{ .Release.Name }}\n"
	out, err := GoTemplateTerminal256(types.ColorYes, in)
	require.NoError(t, err)
	assert.NotEqual(t, in, out)
	assert.Contains(t, out, "\x1b[")
}

func TestGoTemplateTerminal256_PreservesNewlines(t *testing.T) {
	in := "a: 1\nb: {{ .x }}\n"
	out, err := GoTemplateTerminal256(types.ColorYes, in)
	require.NoError(t, err)
	assert.Equal(t, strings.Count(in, "\n"), strings.Count(out, "\n"))
}
