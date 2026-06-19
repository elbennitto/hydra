package record

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/base/record/asciinema"
	"hydra-gitops.org/hydra/hydra-go/base/record/expect"
)

// HelpRecordOptions configures help cast generation.
type HelpRecordOptions struct {
	Root            *cobra.Command
	HydraBin        string
	HydraGlobalArgs []string
	OutputDir       string
	// OutputPath optionally resolves the destination cast path for a command.
	// When nil, casts are written to <OutputDir>/<slug>.cast.
	OutputPath func(cmd HelpCommand, outputDir string) string
	// MirrorOutput writes each cast's captured terminal output to stdout while recording.
	MirrorOutput bool
}

// RecordAllHelp records `hydra <path> --help` for every discovered command path.
func RecordAllHelp(opts HelpRecordOptions) error {
	if opts.Root == nil {
		return fmt.Errorf("root command is nil")
	}
	if opts.HydraBin == "" {
		return fmt.Errorf("hydra binary path is empty")
	}
	if opts.OutputDir == "" {
		return fmt.Errorf("output directory is empty")
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	commands := CollectHelpCommandPaths(opts.Root)
	total := len(commands)
	l := log.Default()
	l.Info(logIdRecord, "recording help casts",
		log.Int("total", total),
		log.String("outputDir", opts.OutputDir),
		log.String("hydraBin", opts.HydraBin),
		log.Bool("mirrorOutput", opts.MirrorOutput))

	bar, err := l.NewProgress("recording help casts", total)
	if err != nil {
		return fmt.Errorf("progress bar: %w", err)
	}
	var task log.ProgressTask
	if bar != nil {
		defer func() { _ = bar.Close() }()
		task = bar.NewTask("cast")
	}

	for i, cmd := range commands {
		outPath := filepath.Join(opts.OutputDir, cmd.Slug+".cast")
		if opts.OutputPath != nil {
			if resolved := strings.TrimSpace(opts.OutputPath(cmd, opts.OutputDir)); resolved != "" {
				outPath = resolved
			}
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fmt.Errorf("create cast output directory: %w", err)
		}
		display := expect.HydraDisplayCommand(cmd.Path, "--help")
		if task != nil {
			task.SetDetail(display)
		}
		l.DebugLog(logIdRecord, "recording help cast",
			log.String("command", cmd.Path),
			log.String("output", outPath),
			log.Int("index", i+1),
			log.Int("total", total))
		if err := asciinema.RecordHelp(asciinema.RecorderOptions{
			HydraBin:        opts.HydraBin,
			HydraGlobalArgs: opts.HydraGlobalArgs,
			CommandPath:     cmd.Path,
			OutputPath:      outPath,
			MirrorOutput:    opts.MirrorOutput,
		}); err != nil {
			return fmt.Errorf("record cli help for %q: %w", cmd.Path, err)
		}
		if bar != nil {
			bar.Advance(i+1, total)
		}
	}
	if task != nil {
		_ = task.Close()
	}

	l.Info(logIdRecord, "finished recording help casts",
		log.Int("count", total),
		log.String("outputDir", opts.OutputDir))
	return nil
}
