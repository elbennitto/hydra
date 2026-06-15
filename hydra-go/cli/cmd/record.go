package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/base/record"
	recordcli "hydra-gitops.org/hydra/hydra-go/cli/record"
)

const recordNoTimestampsEnvName = "HYDRA_RECORD_NO_TIMESTAMPS"

var logIdRecordCmd = log.Hydra().Child("record")

func newRecordCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record terminal casts for documentation",
		Long: `Record asciicast v3 files for Hydra CLI documentation.

Subcommands discover Hydra commands automatically and capture their
help output in a pseudo-terminal.`,
	}

	cmd.AddCommand(newRecordHelpCommand(root))
	cmd.AddCommand(newRecordFileCommand(root))
	cmd.AddCommand(newRecordAllCommand(root))
	return cmd
}

func newRecordAllCommand(root *cobra.Command) *cobra.Command {
	var opts recordCLIParams

	cmd := &cobra.Command{
		Use:   "all",
		Short: "Record all documentation casts",
		Long: `Record all documentation casts.

This records both Hydra CLI help casts and record-file casts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecordAll(root, opts)
		},
	}

	addRecordCommonFlags(cmd, &opts)
	return cmd
}

func newRecordHelpCommand(root *cobra.Command) *cobra.Command {
	var opts recordCLIParams

	cmd := &cobra.Command{
		Use:   "help",
		Short: "Record help output for all Hydra CLI commands",
		Long: `Discover all Hydra CLI commands and record "hydra <command> --help"
for each one as an asciicast under hydra/docs/asciinema/help/.

	Each recording starts with a generic "$ " shell prompt. Captured terminal output is
	not written to stdout by default; use --mirror to enable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecordHelp(root, opts)
		},
	}

	addRecordCommonFlags(cmd, &opts)
	return cmd
}

func newRecordFileCommand(root *cobra.Command) *cobra.Command {
	var opts recordCLIParams

	cmd := &cobra.Command{
		Use:   "file <file>...",
		Short: "Record one or more casts from YAML files",
		Long: `Load one or more record YAML files and record them as asciicasts.

The record file is executed in a fresh temporary directory. Any real temp path in
the terminal output is rewritten to the record's virtual path ("/<record-slug>").`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecordFiles(args, opts)
		},
	}

	addRecordCommonFlags(cmd, &opts)
	cmd.Flags().StringVar(&opts.output, "output", "",
		"Optional output .cast file path (default: same directory as <file> with .cast extension)")
	return cmd
}

type recordCLIParams struct {
	outputDir    string
	output       string
	specDir      string
	hydraBin     string
	mirrorOutput bool
}

func addRecordCommonFlags(cmd *cobra.Command, opts *recordCLIParams) {
	cmd.Flags().StringVar(&opts.outputDir, "output-dir", defaultRecordHelpOutputDir(),
		"Directory for .cast files")
	cmd.Flags().StringVar(&opts.specDir, "spec-dir", defaultRecordSpecDir(),
		"Directory containing record YAML specs")
	cmd.Flags().StringVar(&opts.hydraBin, "hydra-bin", defaultHydraBin(),
		"Hydra executable used inside recordings")
	cmd.Flags().BoolVar(&opts.mirrorOutput, "mirror", false,
		"Write captured recording output to stdout while recording")
}

func defaultRecordHelpOutputDir() string {
	if _, err := os.Stat("hydra/docs/asciinema"); err == nil {
		return "hydra/docs/asciinema"
	}
	if _, err := os.Stat("docs/asciinema"); err == nil {
		return "docs/asciinema"
	}
	if _, err := os.Stat("hydra/docs"); err == nil {
		return "hydra/docs/asciinema/help"
	}
	if _, err := os.Stat("docs"); err == nil {
		return "docs/asciinema/help"
	}
	return "hydra/docs/asciinema/help"
}

func defaultRecordSpecDir() string {
	hydraDir := filepath.ToSlash(filepath.Join("hydra", "docs", "asciinema", recordSpecDirName()))
	if _, err := os.Stat(hydraDir); err == nil {
		return hydraDir
	}
	docsDir := filepath.ToSlash(filepath.Join("docs", "asciinema", recordSpecDirName()))
	if _, err := os.Stat(docsDir); err == nil {
		return docsDir
	}
	return hydraDir
}

func defaultHydraBin() string {
	if len(os.Args) > 0 && os.Args[0] != "" {
		return os.Args[0]
	}
	return "hydra"
}

func runRecordHelp(root *cobra.Command, opts recordCLIParams) error {
	return record.RecordAllHelp(record.HelpRecordOptions{
		Root:            root,
		HydraBin:        opts.hydraBin,
		HydraGlobalArgs: recordingHydraGlobalArgsFromEnv(os.LookupEnv),
		OutputDir:       recordHelpOutputDir(opts.outputDir),
		MirrorOutput:    opts.mirrorOutput,
	})
}

func runRecordFile(file string, opts recordCLIParams) error {
	return runRecordFiles([]string{file}, opts)
}

func runRecordFiles(files []string, opts recordCLIParams) error {
	if len(files) == 0 {
		return nil
	}
	if len(files) > 1 && strings.TrimSpace(opts.output) != "" {
		return fmt.Errorf("--output can only be used with a single record file")
	}
	for _, file := range files {
		if err := runSingleRecordFile(file, opts); err != nil {
			return err
		}
	}
	return nil
}

func runSingleRecordFile(file string, opts recordCLIParams) error {
	l := log.Default()
	outPath := recordFileOutputPath(file, opts.output)
	l.Info(logIdRecordCmd, "recording started",
		log.String("mode", "file"),
		log.String("file", file),
		log.String("output", outPath),
		log.String("hydraBin", opts.hydraBin))
	if err := recordcli.RecordOne(file, recordOneFileOptions(file, opts)); err != nil {
		return err
	}
	l.Info(logIdRecordCmd, "recording finished",
		log.String("mode", "file"),
		log.String("file", file),
		log.String("output", outPath))
	return nil
}

func runRecordAll(root *cobra.Command, opts recordCLIParams) error {
	l := log.Default()
	l.Info(logIdRecordCmd, "recording started",
		log.String("mode", "all"),
		log.String("outputDir", opts.outputDir),
		log.String("hydraBin", opts.hydraBin))
	if err := runRecordHelp(root, opts); err != nil {
		return err
	}
	if err := recordcli.RecordAll(recordFileOptions(opts)); err != nil {
		return err
	}
	l.Info(logIdRecordCmd, "recording finished",
		log.String("mode", "all"),
		log.String("outputDir", opts.outputDir))
	return nil
}

func recordFileOptions(opts recordCLIParams) recordcli.RecordOptions {
	return recordcli.RecordOptions{
		HydraBin:        opts.hydraBin,
		HydraGlobalArgs: recordingHydraGlobalArgsFromEnv(os.LookupEnv),
		SpecDir:         opts.specDir,
		OutputDir:       recordFileOutputDir(opts.outputDir),
		MirrorOutput:    opts.mirrorOutput,
	}
}

func recordOneFileOptions(file string, opts recordCLIParams) recordcli.RecordOptions {
	base := recordFileOptions(opts)
	base.OutputPath = recordFileOutputPath(file, opts.output)
	return base
}

func recordHelpOutputDir(base string) string {
	if strings.HasSuffix(base, "/help") || strings.HasSuffix(base, `\help`) {
		return base
	}
	return filepath.Join(base, "help")
}

func recordFileOutputDir(base string) string {
	slashSuffix := "/" + recordSpecDirName()
	backslashSuffix := `\` + recordSpecDirName()
	if strings.HasSuffix(base, slashSuffix) || strings.HasSuffix(base, backslashSuffix) {
		return base
	}
	return filepath.Join(base, recordSpecDirName())
}

func recordFileOutputPath(file, output string) string {
	trimmedOutput := strings.TrimSpace(output)
	if trimmedOutput != "" {
		return trimmedOutput
	}

	cleanFile := filepath.Clean(file)
	ext := filepath.Ext(cleanFile)
	if ext == "" {
		return cleanFile + ".cast"
	}
	return strings.TrimSuffix(cleanFile, ext) + ".cast"
}

func recordSpecDirName() string {
	return "tuto" + "rials"
}

func recordingHydraGlobalArgsFromEnv(lookupEnv func(string) (string, bool)) []string {
	value, ok := lookupEnv(recordNoTimestampsEnvName)
	if !ok {
		return []string{"--no-timestamps"}
	}
	if isFalsyEnvValue(value) {
		return nil
	}
	return []string{"--no-timestamps"}
}

func isTruthyEnvValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func isFalsyEnvValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "f", "no", "n", "off":
		return true
	default:
		return false
	}
}
