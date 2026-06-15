package expect

import "strings"

const RecordingHydraNoTermEscapesEnvName = "HYDRA_RECORD_NO_TERM_ESCAPES"
const RecordingHydraForceColorEnvName = "CLICOLOR_FORCE"

// HydraDisplayCommand returns the user-visible command (always prefixed with "hydra").
func HydraDisplayCommand(commandPath string, extraArgs ...string) string {
	parts := []string{"hydra"}
	if commandPath != "" {
		parts = append(parts, commandPath)
	}
	parts = append(parts, extraArgs...)
	return strings.Join(parts, " ")
}

// BuildHydraExecLine builds a safely quoted shell invocation for the real binary.
func BuildHydraExecLine(hydraBin, commandPath string, extraArgs ...string) string {
	return BuildHydraExecLineWithGlobalArgs(hydraBin, nil, commandPath, extraArgs...)
}

// BuildHydraExecLineWithGlobalArgs builds a safely quoted shell invocation for the
// real binary and inserts global Hydra flags before the subcommand path.
func BuildHydraExecLineWithGlobalArgs(hydraBin string, globalArgs []string, commandPath string, extraArgs ...string) string {
	prefix := RecordingHydraEnvPrefix()
	parts := []string{shellQuote(hydraBin)}
	for _, a := range globalArgs {
		parts = append(parts, shellQuote(a))
	}
	for _, p := range strings.Fields(commandPath) {
		parts = append(parts, shellQuote(p))
	}
	for _, a := range extraArgs {
		parts = append(parts, shellQuote(a))
	}
	return prefix + strings.Join(parts, " ")
}

// RecordingHydraEnvPrefix returns shell assignments that disable ANSI-heavy output
// for hydra child processes spawned during cast recording.
func RecordingHydraEnvPrefix() string {
	return RecordingHydraNoTermEscapesEnvName + "=1 " + RecordingHydraForceColorEnvName + "=1 "
}
