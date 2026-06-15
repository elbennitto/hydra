package cmd

import (
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/base/record/expect"
)

func recordingNoTermEscapesEnabled() bool {
	value, ok := os.LookupEnv(expect.RecordingHydraNoTermEscapesEnvName)
	return ok && isTruthyEnvValue(value)
}

func colorForcedByEnv() (bool, bool) {
	if value, ok := os.LookupEnv("CLICOLOR_FORCE"); ok && !isFalsyEnvValue(value) {
		return true, true
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false, true
	}
	if value, ok := os.LookupEnv("CLICOLOR"); ok && isFalsyEnvValue(value) {
		return false, true
	}
	return false, false
}

func stdoutIsTerminalForHydra() bool {
	if recordingNoTermEscapesEnabled() {
		return false
	}
	tty := isatty.IsTerminal(os.Stdout.Fd())
	if !tty && log.StdoutTTYAtCliInit() {
		tty = true
	}
	return tty
}

func stderrIsTerminalForHydra() bool {
	if recordingNoTermEscapesEnabled() {
		return false
	}
	return isatty.IsTerminal(os.Stderr.Fd())
}

func applyColorEnvOverrides() {
	if enabled, ok := colorForcedByEnv(); ok {
		color.NoColor = !enabled
		return
	}
	if recordingNoTermEscapesEnabled() {
		color.NoColor = true
		return
	}
	// Leave fatih/color default auto-detection untouched when no env override applies.
	color.NoColor = strings.TrimSpace(os.Getenv("TERM")) == "dumb"
}
