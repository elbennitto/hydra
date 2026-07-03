package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

type ClusterScaleCommandParams struct {
	ClusterScaleUp     func(flags action.ClusterScaleFlags) error
	ClusterScaleDown   func(flags action.ClusterScaleFlags) error
	ClusterScaleStatus func(flags action.ClusterScaleStatusFlags) error
}

func NewClusterScaleCommand(params ClusterScaleCommandParams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scale",
		Short: "Scale workloads up or down in a Kubernetes cluster",
		Long: `Scale workloads (Deployments, StatefulSets, ReplicaSets, DaemonSets) up or
down in topological dependency order. Scale-up restores git-defined replica
counts. Scale-down sets replicas to 0 (or disables DaemonSets via nodeSelector).

App IDs support glob-style wildcard matching:
  *            matches any characters except '.' (stays within one segment)
  **           matches any characters including '.' (crosses segments)

Use --exclude-app to remove specific apps from the selection.`,
	}

	cmd.AddCommand(newClusterScaleUpCommand(params.ClusterScaleUp))
	cmd.AddCommand(newClusterScaleDownCommand(params.ClusterScaleDown))
	cmd.AddCommand(newClusterScaleStatusCommand(params.ClusterScaleStatus))

	return cmd
}

func newClusterScaleUpCommand(scaleUp func(flags action.ClusterScaleFlags) error) *cobra.Command {
	f := action.ClusterScaleFlags{}

	cmd := &cobra.Command{
		Use:   "up <appId> [appId...]",
		Short: "Scale workloads up to their git-defined replica counts",
		Example: `  hydra gitops scale up prod.infra.monitoring
  hydra gitops scale up prod.*`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			return scaleUp(f)
		},
		SilenceUsage: true,
	}

	DefineFlags(cmd, &f)

	return cmd
}

func newClusterScaleDownCommand(scaleDown func(flags action.ClusterScaleFlags) error) *cobra.Command {
	f := action.ClusterScaleFlags{}

	cmd := &cobra.Command{
		Use:   "down <appId> [appId...]",
		Short: "Scale workloads down to zero replicas",
		Example: `  hydra gitops scale down prod.infra.monitoring
  hydra gitops scale down prod.* --force-scale-down`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			if err := validateClusterScaleDownExclusiveFlags(cmd); err != nil {
				return err
			}
			return scaleDown(f)
		},
		SilenceUsage: true,
	}

	DefineFlags(cmd, &f)
	if err := defineClusterWorkloadTimeoutFlag(cmd, &f); err != nil {
		panic(err)
	}

	return cmd
}

func newClusterScaleStatusCommand(scaleStatus func(flags action.ClusterScaleStatusFlags) error) *cobra.Command {
	f := action.ClusterScaleStatusFlags{}

	cmd := &cobra.Command{
		Use:   "status <appId> [appId...]",
		Short: "Show scale sync state and workload dependencies for selected apps",
		Long: `Read-only report: for each scale target workload, whether the live cluster matches
the scaled-down state (down), the rendered template target (up), missing (missing / missing in YAML)
when there is no live Job object (often after TTL cleanup), ok (ok in YAML) when that missing Job
still has satisfied out-dependencies (see manual), or neither of the above (out of sync in the
default text view; out_of_sync in YAML).

By default, prints colored text to stdout. Use --yaml for machine-readable YAML; with --color
or TTY auto-detection, YAML is syntax-highlighted (same mechanism as other YAML commands).

Direct dependencies list workloads and transitive ref targets; each dependency includes the same
state classification. The command lists the full cluster inventory like other cluster scale
operations. By default, each scale-target row that is fully healthy (up/ok and dependencies) or a
missing Job with satisfied dependencies is omitted; use --all or -A to show those rows.`,
		Example: `  hydra gitops scale status prod.infra.monitoring
  hydra gitops scale status prod.* --yaml
  hydra gitops scale status prod.* --yaml --color`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppIdPatterns = types.ToAppIdPatterns(args)
			if err := mergeAndValidateClusterREST(cmd, &f.ClusterRESTClientFlags); err != nil {
				return err
			}
			return scaleStatus(f)
		},
		SilenceUsage: true,
	}

	DefineFlags(cmd, &f)
	cmd.Flags().BoolVar(&f.YamlOutput, "yaml", false, "Emit YAML instead of the default colored text report")
	cmd.Flags().BoolVarP(&f.ShowAllHealthyApps, "all", "A", false,
		"Show every scale-target row; by default omit rows that are fully healthy or missing Jobs with satisfied deps")

	return cmd
}

func validateClusterScaleDownExclusiveFlags(cmd *cobra.Command) error {
	force := cmd.Flags().Lookup("force-scale-down")
	clusterTmo := cmd.Flags().Lookup("cluster-workload-timeout")
	if force != nil && clusterTmo != nil && force.Changed && clusterTmo.Changed {
		return fmt.Errorf("cannot use --force-scale-down and --cluster-workload-timeout together")
	}
	return nil
}
