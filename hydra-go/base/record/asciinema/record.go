package asciinema

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/base/record/expect"
)

// RecorderOptions configures a single help cast recording.
type RecorderOptions struct {
	// HydraBin is the hydra executable (e.g. "hydra" or an absolute path).
	HydraBin string
	// HydraGlobalArgs are inserted before the subcommand path, for example global flags.
	HydraGlobalArgs []string
	// CommandPath is the hydra subcommand path without "hydra" (e.g. "local template").
	CommandPath string
	// OutputPath is the destination .cast file.
	OutputPath string
	// MirrorOutput streams captured PTY bytes to stdout while recording (see --mirror).
	MirrorOutput bool
}

// RecordHelp captures `hydra <commandPath> --help` in a PTY and writes an asciicast v3 file.
func RecordHelp(opts RecorderOptions) error {
	if opts.HydraBin == "" {
		return fmt.Errorf("hydra binary path is empty")
	}
	if opts.OutputPath == "" {
		return fmt.Errorf("output path is empty")
	}

	l := log.Default()
	display := expect.HydraDisplayCommand(opts.CommandPath, "--help")
	l.DebugLog(logIdAsciinema, "preparing help recording",
		log.String("command", display),
		log.String("hydraBin", opts.HydraBin),
		log.String("output", opts.OutputPath))

	script := expect.NewBashScript()
	script.SetupRecordingShell()
	script.ShowInitialPrompt()
	docCommand := HelpCastDocumentationCommand(display)
	execLine := expect.BuildHydraExecLineWithGlobalArgs(opts.HydraBin, opts.HydraGlobalArgs, opts.CommandPath, "--help")
	script.ShowAndRunCommand(display, execLine)

	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	tmp, err := os.CreateTemp("", "hydra-record-*.sh")
	if err != nil {
		return fmt.Errorf("create temp script: %w", err)
	}
	scriptPath := tmp.Name()
	defer os.Remove(scriptPath)

	if _, err := tmp.WriteString(script.Render()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp script: %w", err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp script: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp script: %w", err)
	}

	var mirror io.Writer
	if opts.MirrorOutput {
		log.FlushProgressForStdout()
		mirror = os.Stdout
	}
	l.DebugLog(logIdAsciinema, "running recording script in PTY",
		log.String("script", scriptPath),
		log.String("windowSize", fmt.Sprintf("%dx%d", defaultCastCols, defaultCastRows)),
		log.Bool("mirrorOutput", opts.MirrorOutput))

	ptyOutput, err := captureScriptOutput(scriptPath, recordingEnv(), mirror)
	if err != nil {
		return err
	}
	l.DebugLog(logIdAsciinema, "captured PTY output",
		log.Int("bytes", len(ptyOutput)),
		log.String("command", display))

	if err := writeRawCast(opts.OutputPath, ptyOutput, docCommand); err != nil {
		return fmt.Errorf("write cast: %w", err)
	}
	l.DebugLog(logIdAsciinema, "wrote asciicast",
		log.String("path", opts.OutputPath),
		log.String("documentationCommand", docCommand))
	return nil
}

func recordingEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "TERM="),
			strings.HasPrefix(e, "NO_COLOR="):
			continue
		}
		out = append(out, e)
	}
	out = append(out, "TERM="+expect.RecordingTerm)
	return out
}
