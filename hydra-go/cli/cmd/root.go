package cmd

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"hydra-gitops.org/hydra/hydra-go/base/buildinfo"
	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/mattn/go-isatty"
	cosigncli "github.com/sigstore/cosign/v2/cmd/cosign/cli"
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/exitcode"
	"hydra-gitops.org/hydra/hydra-go/cli/progress"
	hc "hydra-gitops.org/hydra/hydra-go/cli/util"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"k8s.io/klog/v2"
)

// GlobalFlags holds all global command line flags
type GlobalFlags struct {
	Verbose      bool
	Quiet        bool
	Color        bool
	NoColor      bool
	ColorMode    string
	ColorLog     bool
	NoTimestamps bool
	NoColorLog   bool
	JsonLog      bool
	NoProgress   bool
	Usage        *cobra.Command
	Help         *cobra.Command
}

// RootCommandParams holds the dependencies for creating subcommands
type RootCommandParams struct {
	Find          func(flags action.FindFlags) (hydra.Hydra, string, error)
	Config        func(flags action.ConfigFlags) (hydra.Hydra, string, error)
	Template      func(flags action.TemplateFlags) (hydra.Hydra, string, error)
	Source        func(flags action.SourceFlags) (hydra.Hydra, string, error)
	Values        func(flags action.ValuesFlags) (hydra.Hydra, string, error)
	Review        ReviewCommandParams
	Argocd        ArgocdCommandParams
	Cluster       ClusterCommandParams
	Ci            CiCommandParams
	Test          TestCommandParams
	ExportContext func(flags action.ClusterViewContextFlags) error
}

func NewRootCommandParams() *RootCommandParams {
	return &RootCommandParams{
		Find:          action.Find,
		Config:        action.Config,
		Template:      action.Template,
		Source:        action.Source,
		Values:        action.Values,
		Review:        NewReviewCommandParams(),
		Argocd:        NewArgocdCommandParams(),
		Cluster:       NewClusterCommandParams(),
		Ci:            NewCiCommandParams(),
		Test:          NewTestCommandParams(),
		ExportContext: action.ClusterViewContext,
	}
}

func Execute() error {
	return executeWithArgv(os.Args)
}

func ExecuteArgs(args []string) error {
	return executeWithArgv(append([]string{"hydra"}, args...))
}

func executeWithArgv(argv []string) error {
	if len(argv) == 0 {
		argv = []string{"hydra"}
	}

	args := normalizeInvocationArgs(argv[0], argv[1:])

	if shouldRunLess(args) {
		return runLessPipe(args)
	}
	if shouldRunDelegatedHelm(args) {
		return runDelegatedHelm(args)
	}

	defer log.CloseActiveProgressBars()

	// create the root command
	rootCmd, flags := NewRootCommand()
	ApplyColoredCobraHelp(rootCmd)
	rootCmd.SetArgs(args)

	// Execute the root command
	err := rootCmd.Execute()

	logged := log.LogLazy(err)
	if err != nil && !logged && !exitcode.Is(err) {
		l := log.Default()
		l.Error(logIdCmd, "{err}", log.Err(err))
	}

	// Handle usage and help flags
	if flags.Usage != nil || flags.Help != nil {
		if err == nil || !logged {
			rootCmd.SetOut(nil)
			rootCmd.SetErr(nil)
			if flags.Help != nil {
				flags.Help.HelpFunc()(flags.Help, args)
			}
			if flags.Usage != nil {
				flags.Usage.Usage()
			}
		}
	}

	return err
}

func normalizeInvocationArgs(argv0 string, args []string) []string {
	invoked := normalizeExecutableName(argv0)
	if !isHydraTopLevelCommand(invoked) || invoked == "hydra" {
		return args
	}

	if len(args) > 0 && args[0] == invoked {
		return args
	}

	return append([]string{invoked}, args...)
}

func normalizeExecutableName(argv0 string) string {
	base := filepath.Base(argv0)
	if base == "" {
		return ""
	}

	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}

	return strings.ToLower(base)
}

func isHydraTopLevelCommand(name string) bool {
	switch name {
	case "argocd", "ci", "cluster", "cosign", "gitops", "helm", "local", "message", "record", "version", "yq":
		return true
	default:
		return false
	}
}

// NewRootCommand creates and returns the root cobra command and global flags
func NewRootCommand() (*cobra.Command, *GlobalFlags) {
	return newRootCommand(NewRootCommandParams())
}

func newRootCommand(params *RootCommandParams) (*cobra.Command, *GlobalFlags) {
	flags := &GlobalFlags{}

	rootCmd := &cobra.Command{
		Use:   "hydra",
		Short: "Hydra GitOps CLI for Kubernetes cluster management",
		Long: `Hydra is a GitOps CLI tool for rendering, comparing, and managing
Kubernetes resources across clusters.

It uses a Hydra context directory that defines clusters, root apps, and
child apps. Each app is identified by an appId in the format:

  <cluster>.<rootApp>              (for root apps)
  <cluster>.<rootApp>.<childApp>   (for child apps)

The Hydra context can be specified via the --hydra-context flag or the
HYDRA_CONTEXT environment variable.`,
	}

	rootCmd.SetUsageFunc(func(cmd *cobra.Command) error {
		rootCmd.SetHelpFunc(nil)
		rootCmd.SetUsageFunc(nil)
		flags.Usage = cmd
		return nil
	})
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		rootCmd.SetHelpFunc(nil)
		rootCmd.SetUsageFunc(nil)
		flags.Help = cmd
	})

	// Add persistent flags
	rootCmd.PersistentFlags().BoolVarP(&flags.Verbose, "verbose", "v", false, "Show debug-level log messages")
	rootCmd.PersistentFlags().BoolVarP(&flags.Quiet, "quiet", "q", false, "Only show warnings and errors")
	rootCmd.PersistentFlags().BoolVar(&flags.Color, "color", false, "Force colored output for commands that support colorized stdout")
	rootCmd.PersistentFlags().BoolVar(&flags.NoColor, "no-color", false, "Disable colored output for commands that support colorized stdout")
	rootCmd.PersistentFlags().StringVar(&flags.ColorMode, "color-mode", "", "Set color mode for commands that support colorized stdout (auto|always|never)")
	rootCmd.PersistentFlags().BoolVar(&flags.ColorLog, "color-log", false, "Force colored log output (default: auto-detect from terminal)")
	rootCmd.PersistentFlags().BoolVar(&flags.NoColorLog, "no-color-log", false, "Disable colored log output")
	rootCmd.PersistentFlags().BoolVar(&flags.NoTimestamps, "no-timestamps", false, "Omit timestamps from log messages")
	rootCmd.PersistentFlags().BoolVar(&flags.JsonLog, "json-log", false, "Output logs in JSON format (useful for log aggregation)")
	rootCmd.PersistentFlags().BoolVar(&flags.NoProgress, "no-progress", false, "Disable terminal progress bars (use plain log output)")
	rootCmd.PersistentFlags().Bool("less", false, "Run Hydra in a subprocess and pipe combined stdout+stderr through $PAGER (default: less -SR +G); the child forces --color and --color-log unless explicitly disabled")

	// Mark flags as mutually exclusive
	rootCmd.MarkFlagsMutuallyExclusive("color", "no-color", "color-mode")
	rootCmd.MarkFlagsMutuallyExclusive("verbose", "quiet")
	rootCmd.MarkFlagsMutuallyExclusive("no-color-log", "color-log", "json-log")

	// Set custom output writers for Cobra
	rootCmd.SetOut(log.NewSlogWriter("STDOUT:", log.LevelDebug))
	rootCmd.SetErr(log.NewSlogWriter("STDERR:", log.LevelDebug))

	// Add subcommands
	rootCmd.AddCommand(NewCiCommand(params.Ci))
	rootCmd.AddCommand(newVersionCommand())
	rootCmd.AddCommand(NewLocalCommand(LocalCommandParams{
		Find:          params.Find,
		Config:        params.Config,
		Template:      params.Template,
		List:          action.TemplateSortedRenderedEntities,
		Source:        params.Source,
		Values:        params.Values,
		Refs:          action.LocalRefs,
		LocalTree:     action.LocalTree,
		Review:        params.Review,
		Test:          params.Test,
		ExportContext: params.ExportContext,
	}))
	rootCmd.AddCommand(NewArgocdCommand(params.Argocd))
	rootCmd.AddCommand(NewClusterCommand(params.Cluster))
	rootCmd.AddCommand(newClusterOnlyCommand())
	rootCmd.AddCommand(wrapDelegatedCLI(cosigncli.New()))
	rootCmd.AddCommand(newYqCommand())
	rootCmd.AddCommand(newHelmCommand())
	rootCmd.AddCommand(newMessageCommand())
	rootCmd.AddCommand(newRecordCommand(rootCmd))

	// Default: do not print command usage on RunE failures. Use exitcode.WithShowUsage(err)
	// when a specific failure should still show usage.
	setSilenceUsageRecursive(rootCmd)

	// without this, configureLogging is not called for subcommands
	cobra.EnableTraverseRunHooks = true
	hc.AddPersistentPreRun(rootCmd, func(cmd *cobra.Command, args []string) {
		configureLogging(flags, cmd)
	})

	return rootCmd, flags
}

func setSilenceUsageRecursive(cmd *cobra.Command) {
	cmd.SilenceUsage = true
	for _, c := range cmd.Commands() {
		setSilenceUsageRecursive(c)
	}
}

func configureLogging(flags *GlobalFlags, cmd *cobra.Command) {
	stdoutTTYAtInit := isatty.IsTerminal(os.Stdout.Fd())

	var useColor bool
	switch {
	case flags.ColorLog:
		useColor = true
	case flags.NoColorLog:
		useColor = false
	default:
		useColor = isatty.IsTerminal(os.Stderr.Fd())
	}

	var colors *log.ColorHandlerColors
	if useColor {
		colors = log.DefaultColors()
	}

	level := log.LevelInfo
	if flags.Verbose {
		level = log.LevelDebug
	}
	if flags.Quiet {
		level = log.LevelWarn
	}

	log.SetNoProgress(flags.NoProgress)
	log.SetTerminalProgressUI(false)

	var progressBars log.ProgressBars
	if commandUsesClusterProgressFooter(cmd) {
		tty := isatty.IsTerminal(os.Stderr.Fd())
		if tty && useColor {
			log.SetTerminalProgressUI(true)
			if flags.NoProgress {
				progressBars = log.NewDummyProgressBars()
			} else {
				mpbBars := progress.NewMpbProgressBars()
				_ = mpbBars.InstallStdoutProxyIfNeeded()
				progressBars = mpbBars
			}
		}
	}

	log.Configure(log.Config{
		Level:        level,
		Json:         flags.JsonLog,
		Colors:       colors,
		Timestamps:   !flags.NoTimestamps,
		ProgressBars: progressBars,
	})

	log.SetStdoutTTYAtCliInit(stdoutTTYAtInit)

	klog.SetLogger(logr.FromSlogHandler(slog.Default().Handler()))

	l := log.Default()
	if cmd != nil && !skipHydraWelcome(cmd) {
		l.Info(logIdCmd, "Welcome to Hydra {version}", log.String("version", buildinfo.String()))
	}

	if flags.Verbose {
		l.Info(logIdCmd, "Verbose logging enabled")
	}
}

func commandUsesClusterProgressFooter(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	path := cmd.CommandPath()
	return strings.HasPrefix(path, "hydra record") ||
		strings.HasPrefix(path, "hydra gitops apply") ||
		strings.HasPrefix(path, "hydra gitops uninstall") ||
		strings.HasPrefix(path, "hydra gitops review") ||
		strings.HasPrefix(path, "hydra gitops show") ||
		strings.HasPrefix(path, "hydra gitops untracked") ||
		strings.HasPrefix(path, "hydra gitops system")
}
