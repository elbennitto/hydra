package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

// NewConfigCommand creates the hydra local config subcommand.
func NewConfigCommand() *cobra.Command {
	return newConfigCommand(action.Config)
}

func newConfigCommand(config func(flags action.ConfigFlags) (hydra.Hydra, string, error)) *cobra.Command {
	flags := action.ConfigFlags{}

	cmd := &cobra.Command{
		Use:   "config <appId>",
		Short: "Show global.hydra from Helm values only (no Hydra ConfigMap merge)",
		Long: `Show effective global.hydra for an application from merged Helm chart values only.

Hydra ConfigMap overlays (data.hydra with annotation hydra-gitops.org/hydra-config)
apply to hydra gitops commands (e.g. hydra gitops values), not hydra local config.

ownerNamespaces may still be inferred using an offline full-cluster render (same as before).`,
		Example: `  # Show Hydra config for an app
  hydra local config my-cluster.my-root-app.my-app --hydra-context /path/to/context

  # Colored YAML (TTY auto-detect; force with --color)
  hydra local config my-cluster.my-root-app.my-app --color`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, result, err := config(flags)
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
