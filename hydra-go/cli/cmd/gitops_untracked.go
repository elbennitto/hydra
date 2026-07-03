package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

func NewClusterUntrackedCommand(untracked func(flags action.ClusterUntrackedFlags) (hydra.Hydra, string, error)) *cobra.Command {
	flags := action.ClusterUntrackedFlags{}

	cmd := &cobra.Command{
		Use:   "untracked <cluster>",
		Short: "List live cluster resources not owned by Hydra templates, presets, or priority >= 0 uninstall refs",
		Long: `Print one Hydra resource id per line for objects present in the cluster that are not
explained by the merged render of all enabled apps on that cluster and are not matched by
enabled global.hydra.presets cluster-defaults rules (builtin coredns / kubernetes / flannel / canal / kubermatic / syseleven / metakube / syseleven-node-problem-detector / quobyte / cloudinit / cinder / talos
plus Helm/ConfigMap merges), are not Kubernetes standard ref-ownership exemptions (for
example kube-root-ca.crt or default ServiceAccounts), and are not assigned by Hydra's central
inventory app-ownership model. That model uses template ids first, metadata.ownerReferences second,
then unambiguous priority >= 0 predicates from ref groups tagged uninstall, uninstall-force, or backup.
Normal inspect refs and negative-priority ref ownership do not assign app ownership here.

Before matching templates and presets, the live inventory is reduced to ownership roots: any
object with an ownerReference whose UID matches another object in the same snapshot is omitted
(so ReplicaSets and Pods under a Deployment are not listed). If every owner UID is absent from
the snapshot or empty, the object is still treated as a root (dangling or invalid owner links).

This is read-only. Uses the same ListClusterAll inventory, Helm merge, API-version normalization,
and preset CEL environment as hydra gitops system.

Full-cluster discovery uses the same footer progress bar as other list-all operations (one step
per listable API resource type when stderr is a TTY). Tune Kubernetes client throughput with
the inherited hydra gitops flags --qps and --api-burst (see hydra gitops --help).

Optional --include / --exclude CEL filters narrow the printed untracked set (same as hydra gitops list).

The cluster argument is a single segment (no '.'), matching a directory name under the
Hydra context — not an app id.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := mergeAndValidateClusterREST(cmd, &flags.ClusterRESTClientFlags); err != nil {
				return err
			}
			_, _, err := untracked(flags)
			return err
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
