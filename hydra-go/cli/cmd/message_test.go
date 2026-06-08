package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
)

func TestMessageCommand_PrintsBoxFromFlag(t *testing.T) {
	cmd := newMessageCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--title", "Hint", "--text", "Hello"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Hint")
	assert.Contains(t, out.String(), "Hello")
	assert.Contains(t, out.String(), "\x1b[38;5;228m")
}

func TestMessageCommand_ReadsTextFromStdin(t *testing.T) {
	cmd := newMessageCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader("Line one\nLine two\n"))

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Line one")
	assert.Contains(t, out.String(), "Line two")
}

func TestMessageCommand_EvaluatesEscapedNewlineInTextFlag(t *testing.T) {
	cmd := newMessageCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--text", `Line one\nLine two`})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Line one")
	assert.Contains(t, out.String(), "Line two")
	assert.NotContains(t, out.String(), `Line one\nLine two`)
}

func TestResolveMessageText_RequiresInput(t *testing.T) {
	_, err := resolveMessageText(strings.NewReader(""), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "message text is required")
}

func TestResolveMessageOutput_UsesGivenWriter(t *testing.T) {
	var out bytes.Buffer
	resolved := resolveMessageOutput(&out)
	assert.Same(t, &out, resolved)
}

func TestResolveMessageOutput_ReplacesSlogWriterWithStdout(t *testing.T) {
	resolved := resolveMessageOutput(log.NewSlogWriter("STDOUT:", log.LevelDebug))
	assert.Same(t, os.Stdout, resolved)
}
