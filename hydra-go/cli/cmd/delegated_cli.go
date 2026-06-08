package cmd

import (
	"errors"
	"os"
	"strings"

	yqcmd "github.com/mikefarah/yq/v4/cmd"
	"github.com/spf13/cobra"
	helmcmd "helm.sh/helm/v4/pkg/cmd"
	"helm.sh/helm/v4/pkg/kube"

	// Initialize Kubernetes client auth plugins (same as helm.sh/helm/v4/cmd/helm).
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"hydra-gitops.org/hydra/hydra-go/cli/exitcode"
)

func wrapDelegatedCLI(cmd *cobra.Command) *cobra.Command {
	prev := cmd.PersistentPreRunE
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		resetDelegatedCLIIO(c)
		if prev != nil {
			return prev(c, args)
		}
		return nil
	}
	return cmd
}

// resetDelegatedCLIIO wires real stdout/stderr through a delegated upstream CLI tree.
// Hydra's root command uses slog-backed writers for its own commands.
func resetDelegatedCLIIO(cmd *cobra.Command) {
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	for _, sub := range cmd.Commands() {
		resetDelegatedCLIIO(sub)
	}
}

// helmCommandIndex returns the index of the top-level "helm" token in argv, or -1.
// Parsing stops at "--" so positional "helm" is not treated as the delegated CLI.
func helmCommandIndex(args []string) int {
	for i, a := range args {
		if a == "--" {
			return -1
		}
		if a == "helm" {
			return i
		}
	}
	return -1
}

func shouldRunDelegatedHelm(args []string) bool {
	return helmCommandIndex(args) >= 0
}

// runDelegatedHelm runs the embedded Helm CLI in a separate Cobra tree so Hydra root
// flags (e.g. -v/--verbose) are not merged into Helm's flag set.
func runDelegatedHelm(args []string) error {
	idx := helmCommandIndex(args)
	if idx < 0 {
		return errors.New("helm: internal error: missing helm command in argv")
	}
	helmArgv := args[idx:]
	helmArgs := helmArgv[1:]

	// Match helm.sh/helm/v4/cmd/helm so managedFields use the upstream manager name.
	kube.ManagedFieldsManager = "helm"

	cmd, err := helmcmd.NewRootCmd(os.Stdout, helmArgs, helmcmd.SetupLogging)
	if err != nil {
		return err
	}
	wrapDelegatedCLI(cmd)
	cmd.SetArgs(helmArgs)
	return mapHelmCommandError(cmd.Execute())
}

func newHelmCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "helm",
		Short: "The Helm package manager for Kubernetes.",
		Long:  "Run the embedded Helm CLI (same interface as the standalone helm binary). See `hydra helm --help` and https://helm.sh/docs/.",
		RunE: func(_ *cobra.Command, args []string) error {
			return runDelegatedHelm(append([]string{"helm"}, args...))
		},
	}
}

func mapHelmCommandError(err error) error {
	if err == nil {
		return nil
	}
	var cerr helmcmd.CommandError
	if errors.As(err, &cerr) {
		return exitcode.New(cerr.ExitCode, cerr.Error())
	}
	return err
}

func newYqCommand() *cobra.Command {
	yqRoot := yqcmd.New()
	applyYqStandaloneDefaultEval(yqRoot)

	yqPreRun := yqRoot.PersistentPreRunE
	yqRoot.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		resetDelegatedCLIIO(c)
		if c == yqRoot {
			applyYqStandaloneDefaultEval(yqRoot)
		}
		if yqPreRun != nil {
			return yqPreRun(c, c.Flags().Args())
		}
		return nil
	}

	return yqRoot
}

// applyYqStandaloneDefaultEval mirrors github.com/mikefarah/yq/v4 yq.go main():
// when no subcommand matches, prepend "eval" so `yq '.foo'` works like the upstream binary.
func applyYqStandaloneDefaultEval(cmd *cobra.Command) {
	args := cmd.Flags().Args()
	if len(args) == 0 {
		return
	}
	_, _, err := cmd.Find(args)
	if err != nil && args[0] != "__complete" && args[0] != "__completeNoDesc" {
		cmd.SetArgs(append([]string{"eval"}, args...))
	}
}

func skipHydraWelcome(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Name() == "version" {
		return true
	}
	path := cmd.CommandPath()
	return strings.HasPrefix(path, "hydra yq") ||
		strings.HasPrefix(path, "hydra cosign") ||
		strings.HasPrefix(path, "hydra helm") ||
		strings.HasPrefix(path, "hydra message")
}
