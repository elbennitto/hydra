package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

func NewClusterSystemCommand(system func(flags action.ClusterSystemFlags) (hydra.Hydra, string, error)) *cobra.Command {
	flags := action.ClusterSystemFlags{}

	cmd := &cobra.Command{
		Use:   "system <cluster>",
		Short: "Show merged global.hydra.presets and CEL line matches against live cluster inventory",
		Long: `Inspect builtin and merged untracked preset configuration (coredns, kubernetes, flannel, canal, kubermatic, syseleven, metakube, syseleven-node-problem-detector, quobyte, cloudinit, cinder, talos)
and list which live cluster resources match each CEL line. This is read-only and does not
change the cluster.

By default only presets that are effectively enabled are listed; use --all to include
disabled presets (same scope as before this flag existed).

Default output is aligned text (per preset, resource id and found / not found / skipped).
Use --yaml to emit the structured YAML document instead (same schema as before).
Use --color, --no-color, or --color-mode for both text and YAML highlighting.

Uses the same Helm/ConfigMap merge as cluster review and uninstall, and the same cluster
entity inventory as other cluster-wide audits (ListClusterAll).

The cluster argument is a single segment (no '.'), matching a directory name under the
Hydra context — not an app id.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			_, _, err := system(flags)
			return err
		},
	}

	DefineFlags(cmd, &flags)

	cmd.Flags().BoolVar(&flags.All, "all", false,
		"Include effectively disabled presets in the report (and evaluate their CEL lines); default is enabled presets only")

	return cmd
}
