package cmd

import (
	"fmt"
	"slices"

	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"github.com/spf13/cobra"
)

func newLocalAppsCommand(resolveApps func(flags action.AppsFlags) ([]types.AppId, error)) *cobra.Command {
	f := action.AppsFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{
			HelmNetworkMode: types.HelmNetworkModeOnline,
		},
	}

	cmd := &cobra.Command{
		Use:   "apps [appId...]",
		Short: "Print app ids that match one or more app-id patterns",
		Long: `Resolve app-id patterns against the Hydra context and print matching app ids,
one per line.

This command only resolves app ids (with --exclude-app support). It does not render
templates and does not connect to a Kubernetes cluster.

When no app-id pattern is provided, Hydra uses '**' (all apps).`,
		Example: `  # List all prod apps
  hydra local apps prod.** --hydra-context /path/to/context

  # List all apps (default pattern '**')
  hydra local apps --hydra-context /path/to/context

  # Exclude one app from the result set
  hydra local apps in-cluster.** --exclude-app in-cluster.apps.argocd`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				f.AppIdPatterns = []types.AppIdPattern{"**"}
			} else {
				f.AppIdPatterns = types.ToAppIdPatterns(args)
			}
			appIds, err := resolveApps(f)
			if err != nil {
				return err
			}

			slices.Sort(appIds)
			for _, appId := range appIds {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(appId))
			}

			return nil
		},
	}

	DefineFlags(cmd, &f)

	return cmd
}
