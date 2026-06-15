package cmd

import (
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"hydra-gitops.org/hydra/hydra-go/base/record/expect"
)

func TestColorForcedByEnv_CLICOLORForceEnablesColor(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")

	enabled, ok := colorForcedByEnv()

	assert.True(t, ok)
	assert.True(t, enabled)
}

func TestApplyColorEnvOverrides_RecordingEnvDisablesFatihAutoColorUnlessForced(t *testing.T) {
	old := color.NoColor
	defer func() { color.NoColor = old }()

	t.Setenv(expect.RecordingHydraNoTermEscapesEnvName, "1")
	applyColorEnvOverrides()
	assert.True(t, color.NoColor)

	t.Setenv("CLICOLOR_FORCE", "1")
	applyColorEnvOverrides()
	assert.False(t, color.NoColor)
}
