package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

// NewValuesCommand creates and returns the values subcommand
func NewValuesCommand() *cobra.Command {
	return newValuesCommand(action.Values)
}

func newValuesCommand(values func(flags action.ValuesFlags) (hydra.Hydra, string, error)) *cobra.Command {
	flags := action.ValuesFlags{}

	cmd := &cobra.Command{
		Use:   "values <appId>",
		Short: "Render and display the computed Helm values for an app",
		Long: `Compute and display the merged Helm values for the specified app. This
shows the final values that would be passed to Helm during template
rendering, after all value files and overrides have been merged.

The output is YAML and can be used to inspect or debug value resolution.`,
		Example: `  # Show computed values for an app
  hydra local values my-cluster.my-root-app.my-app --hydra-context /path/to/context

  # Show values with colored YAML output
  hydra local values my-cluster.my-root-app.my-app --color

  # Show values using only local resources
  hydra local values my-cluster.my-root-app.my-app --local`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, result, err := values(flags)
			if err != nil {
				return err
			}

			if err := writeCommandResult(result); err != nil {
				return err
			}

			return nil
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
