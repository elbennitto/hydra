package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

func NewClusterListCommand(list func(flags action.ClusterListFlags) (hydra.Hydra, string, error)) *cobra.Command {
	flags := action.ClusterListFlags{}

	cmd := &cobra.Command{
		Use:   "list <cluster>",
		Short: "Print Hydra resource ids visible in a cluster",
		Long: `List the Hydra id of each Kubernetes resource managed through Hydra's cluster view.

Output is one id per line (plain text, not YAML). Use --include and --exclude with the same
CEL resource filters as hydra gitops dump to narrow the set.

With --skip-owner-refs, the command loads the full live inventory first, applies the CEL filters,
then drops objects that have a metadata.ownerReference with a non-empty UID pointing at another
live object (for example ReplicaSets and Pods under a Deployment). Dangling or unknown owner UIDs
are kept.`,
		Example: `  # All resource ids in a cluster
  hydra gitops list my-cluster --hydra-context /path/to/context

  # Only Secrets
  hydra gitops list my-cluster --include 'entity.kind=="Secret"'

  # Exclude kube-system
  hydra gitops list my-cluster --exclude 'namespace == "kube-system"'

  # Only top-level objects (omit controller-owned children)
  hydra gitops list my-cluster --skip-owner-refs`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			_, _, err := list(flags)
			return err
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
