package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

	cmd.AddCommand(newRecordCLICommand(root))
	cmd.AddCommand(newRecordFileCommand(root))
	return cmd
}

func newRecordCLICommand(root *cobra.Command) *cobra.Command {
	var opts recordCLIParams

	cmd := &cobra.Command{
		Use:   "cli",
		Short: "Record CLI help output for all Hydra commands",
		Long: `Discover all Hydra CLI commands and record "hydra <command> --help"
for each one as an asciicast next to the corresponding markdown page under
hydra/docs/manual/commands/.

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
the terminal output is rewritten to the record's virtual path ("/home/hydra").`,
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
	cmd.Flags().StringVar(&opts.outputDir, "output-dir", "",
		"Directory for .cast files")
	cmd.Flags().StringVar(&opts.specDir, "spec-dir", defaultRecordSpecDir(),
		"Directory containing record YAML specs")
	cmd.Flags().StringVar(&opts.hydraBin, "hydra-bin", defaultHydraBin(),
		"Hydra executable used inside recordings")
	cmd.Flags().BoolVar(&opts.mirrorOutput, "mirror", false,
		"Write captured recording output to stdout while recording")
}

func defaultRecordHelpOutputDir() string {
	if _, err := os.Stat("hydra/docs/manual"); err == nil {
		return "hydra/docs/manual/commands"
	}
	if _, err := os.Stat("docs/manual"); err == nil {
		return "docs/manual/commands"
	}
	if _, err := os.Stat("hydra/docs"); err == nil {
		return "hydra/docs/manual/commands"
	}
	if _, err := os.Stat("docs"); err == nil {
		return "docs/manual/commands"
	}
	return "hydra/docs/manual/commands"
}

func defaultRecordSpecDir() string {
	helpDir := filepath.ToSlash(defaultRecordHelpOutputDir())
	switch {
	case strings.HasPrefix(helpDir, "hydra/docs/manual"):
		return "hydra/docs/manual"
	case strings.HasPrefix(helpDir, "docs/manual"):
		return "docs/manual"
	default:
		return "docs/manual"
	}
}

func defaultHydraBin() string {
	if len(os.Args) > 0 && os.Args[0] != "" {
		return os.Args[0]
	}
	return "hydra"
}

func runRecordHelp(root *cobra.Command, opts recordCLIParams) error {
	outputDir := resolvedRecordHelpOutputDir(opts.outputDir)
	return record.RecordAllHelp(record.HelpRecordOptions{
		Root:            root,
		HydraBin:        opts.hydraBin,
		HydraGlobalArgs: recordingHydraGlobalArgsFromEnv(os.LookupEnv),
		OutputDir:       outputDir,
		OutputPath:      recordHelpCommandOutputPathResolver(outputDir),
		MirrorOutput:    opts.mirrorOutput,
	})
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

func recordFileOptions(opts recordCLIParams) recordcli.RecordOptions {
	return recordcli.RecordOptions{
		HydraBin:        opts.hydraBin,
		HydraGlobalArgs: recordingHydraGlobalArgsFromEnv(os.LookupEnv),
		SpecDir:         opts.specDir,
		OutputDir:       resolvedRecordFileOutputDir(opts.outputDir, opts.specDir),
		MirrorOutput:    opts.mirrorOutput,
	}
}

func recordOneFileOptions(file string, opts recordCLIParams) recordcli.RecordOptions {
	base := recordFileOptions(opts)
	base.OutputPath = recordFileOutputPath(file, opts.output)
	return base
}

func recordHelpOutputDir(base string) string {
	if strings.HasSuffix(base, "/commands") || strings.HasSuffix(base, `\commands`) {
		return base
	}
	return filepath.Join(base, "commands")
}

func resolvedRecordHelpOutputDir(base string) string {
	if strings.TrimSpace(base) == "" {
		return defaultRecordHelpOutputDir()
	}
	return recordHelpOutputDir(base)
}

var recordHelpCommandHeaderPattern = regexp.MustCompile(`(?m)^#\s+hydra\s+(.+?)\s*$`)

func recordHelpCommandOutputPathResolver(commandsDir string) func(record.HelpCommand, string) string {
	byCommand := discoverRecordHelpCommandCastPaths(commandsDir)
	return func(cmd record.HelpCommand, outputDir string) string {
		if castPath, ok := byCommand[cmd.Path]; ok {
			return castPath
		}
		rel := recordHelpCommandRelativeCastPath(cmd)
		return filepath.Join(outputDir, rel)
	}
}

func recordHelpCommandRelativeCastPath(cmd record.HelpCommand) string {
	parts := strings.Fields(strings.TrimSpace(cmd.Path))
	if len(parts) == 0 {
		return cmd.Slug + ".cast"
	}
	if len(parts) == 1 {
		return parts[0] + ".cast"
	}
	dir := filepath.Join(parts[:len(parts)-1]...)
	return filepath.Join(dir, parts[len(parts)-1]+".cast")
}

func discoverRecordHelpCommandCastPaths(commandsDir string) map[string]string {
	out := map[string]string{}
	root := filepath.Clean(commandsDir)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		markdown, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		match := recordHelpCommandHeaderPattern.FindSubmatch(markdown)
		if len(match) < 2 {
			return nil
		}
		commandPath := strings.Join(strings.Fields(strings.TrimSpace(string(match[1]))), " ")
		if commandPath == "" {
			return nil
		}
		out[commandPath] = strings.TrimSuffix(path, filepath.Ext(path)) + ".cast"
		return nil
	})
	return out
}

func recordFileOutputDir(base string) string {
	return base
}

func resolvedRecordFileOutputDir(base, specDir string) string {
	if strings.TrimSpace(base) == "" {
		return specDir
	}
	return recordFileOutputDir(base)
}

func recordFileOutputPath(file, output string) string {
	trimmedOutput := strings.TrimSpace(output)
	if trimmedOutput != "" {
		return trimmedOutput
	}

	cleanFile := filepath.Clean(file)
	return trimRecordSpecOutputBase(cleanFile) + ".cast"
}

func trimRecordSpecOutputBase(path string) string {
	switch {
	case strings.HasSuffix(path, ".cast.yaml"):
		return strings.TrimSuffix(path, ".cast.yaml")
	case strings.HasSuffix(path, ".cast.yml"):
		return strings.TrimSuffix(path, ".cast.yml")
	default:
		ext := filepath.Ext(path)
		if ext == "" {
			return path
		}
		return strings.TrimSuffix(path, ext)
	}
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
