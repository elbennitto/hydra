package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/exitcode"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func newTestRefsCommand(testRefs func(f action.TestRefsFlags) error) *cobra.Command {
	f := action.TestRefsFlags{}

	cmd := &cobra.Command{
		Use:   "refs <appId...>",
		Short: "Test ref-parsers defined by apps",
		Long: `Test the ref-parsers defined in global.hydra.refs of the given app(s)
against golden files in the chart's test/refs/ directory.

Each test case consists of:
  - A .given.yaml file with input Kubernetes entities
  - A .expected.yaml file with expected refDefinitions and refs

Use --update to regenerate expected files from current parser output.`,
		Example: `  # Run ref-parser tests for an app
  hydra local test refs prod.cluster-infra.sops-secrets-operator --hydra-context ./gitops

  # Update expected files
  hydra local test refs prod.cluster-infra.sops-secrets-operator --hydra-context ./gitops --update

  # Test multiple apps
  hydra local test refs prod.demo-infra.operator-kafka prod.demo-infra.operator-clickhouse`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			err := testRefs(f)
			cmd.SilenceUsage = !exitcode.IsShowUsage(err)
			return err
		},
	}

	cmd.Flags().BoolVar(&f.Update, "update", false, "Update expected files instead of comparing")
	DefineFlags(cmd, &f)

	return cmd
}
