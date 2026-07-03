package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
)

type TestCommandParams struct {
	TestRefs func(flags action.TestRefsFlags) error
}

func NewTestCommandParams() TestCommandParams {
	return TestCommandParams{
		TestRefs: action.TestRefs,
	}
}

func NewTestCommand(params TestCommandParams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run tests for Hydra apps",
		Long: `Run various test suites for Hydra apps.

Use subcommands to run specific test types under the hydra local command family.`,
	}

	cmd.AddCommand(newTestRefsCommand(params.TestRefs))

	return cmd
}
