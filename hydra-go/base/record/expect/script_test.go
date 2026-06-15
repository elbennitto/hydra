package expect

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"hydra-gitops.org/hydra/hydra-go/base/colors"
)

func TestBashScript_RecordingShellShowsColoredPromptAndCommand(t *testing.T) {
	script := NewBashScript()
	script.SetupRecordingShell()
	script.ShowInitialPrompt()
	script.ShowAndRunCommand("hydra local --help", "/usr/bin/hydra local --help")

	body := script.Render()
	assert.True(t, strings.HasPrefix(body, "#!/usr/bin/env bash\n"))
	assert.Contains(t, body, "export TERM='xterm-256color'")
	assert.Contains(t, body, "_HYDRA_PROMPT=")
	assert.Contains(t, body, colors.RecordingShellPS1())
	assert.Contains(t, body, "$ ")
	assert.Contains(t, body, "_HYDRA_CMD=")
	assert.Contains(t, body, colors.BoldWhite())
	assert.Contains(t, body, `printf '%b' "$_HYDRA_PROMPT"`)
	assert.Contains(t, body, "hydra local --help")
	assert.Contains(t, body, "/usr/bin/hydra local --help")
	assert.Contains(t, body, "exit 0\n")
}

func TestHydraDisplayCommand(t *testing.T) {
	assert.Equal(t, "hydra local template --help", HydraDisplayCommand("local template", "--help"))
	assert.Equal(t, "hydra --help", HydraDisplayCommand("", "--help"))
}

func TestBuildHydraExecLine(t *testing.T) {
	assert.Equal(t,
		"HYDRA_RECORD_NO_TERM_ESCAPES=1 CLICOLOR_FORCE=1 '/opt/hydra' 'local' 'template' '--help'",
		BuildHydraExecLine("/opt/hydra", "local template", "--help"),
	)
}

func TestBuildHydraExecLineWithGlobalArgs(t *testing.T) {
	assert.Equal(t,
		"HYDRA_RECORD_NO_TERM_ESCAPES=1 CLICOLOR_FORCE=1 '/opt/hydra' '--no-timestamps' 'local' 'template' '--help'",
		BuildHydraExecLineWithGlobalArgs("/opt/hydra", []string{"--no-timestamps"}, "local template", "--help"),
	)
}

func TestBashScript_WriteSleepDirective(t *testing.T) {
	script := NewBashScript()
	script.WriteSleepDirective(1.2)
	assert.Contains(t, script.Render(), "<<hydra sleep 1.2>>")
}
