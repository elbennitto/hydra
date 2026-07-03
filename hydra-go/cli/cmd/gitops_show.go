package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

func NewClusterShowCommand(show func(flags action.ClusterShowFlags) (hydra.Hydra, string, error)) *cobra.Command {
	flags := action.ClusterShowFlags{}

	cmd := &cobra.Command{
		Use:   "show <cluster>",
		Short: "Print the central live-cluster app assignment",
		Long: `Print the central Hydra app-assignment view for all live cluster resources. By default,
stdout is a short table with app id and assigned resource count. Use --yaml for the previous full
YAML document grouped by app id. Builtin preset pseudo apps are only included with --builtin.

Resources that cannot be assigned to exactly one app are always emitted as YAML blocks
(ambiguous / unassigned), and the command returns a non-zero exit code.

This is read-only and uses the same central inventory model as other gitops cluster commands.
The cluster argument is a single segment (no '.'), matching a directory name under the
Hydra context — not an app id.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			_, _, err := show(flags)
			return err
		},
	}

	DefineFlags(cmd, &flags)
	cmd.Flags().BoolVar(&flags.Builtin, "builtin", false, "Include builtin preset pseudo apps in the output")
	cmd.Flags().BoolVar(&flags.YamlOutput, "yaml", false, "Emit the full YAML report instead of the default table")
	return cmd
}
