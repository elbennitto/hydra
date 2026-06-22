package cmd

import (
	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// NewSourceCommand creates the hydra local source subcommand.
func NewSourceCommand() *cobra.Command {
	return newSourceCommand(action.Source)
}

func registerLocalSourceFlags(cmd *cobra.Command, f *action.SourceFlags) error {
	for _, define := range []func(*cobra.Command, any) error{
		defineColorFlag,
		defineContextFlag,
		defineNetworkModeFlag,
		defineExcludeAppFlag,
		defineNoCacheFlag,
		definePredicateFlags,
		defineIncludePathFlag,
	} {
		if err := define(cmd, f); err != nil {
			return err
		}
	}
	return nil
}

func newSourceCommand(source func(flags action.SourceFlags) (hydra.Hydra, string, error)) *cobra.Command {
	f := action.SourceFlags{
		ColorFlag: flags.ColorFlag{
			Color: types.ColorYes,
		},
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{
			HelmNetworkMode: types.HelmNetworkModeOnline,
		},
	}

	cmd := &cobra.Command{
		Use:   "source <appId> [appId...]",
		Short: "Print unrendered Helm chart template files from disk",
		Long: `For each app, print the chart's template files from disk (Helm syntax under templates/
and charts/*/templates/). Output is not valid Kubernetes YAML.

Paths in "# Source:" lines are relative to the chart root and use forward slashes.

Use --exclude-app to remove specific apps from the selection.

Use --include and --exclude (CEL, repeatable) to select rendered manifests using the same logic as
hydra local template, then print only the source template files those manifests came from.

Use --include-path (repeatable) to print only files whose Helm template path matches a prefix at
a path boundary, or contains the same multi-segment path after '/' anywhere (umbrella charts may
prefix template paths with the chart name). Multiple flags are combined with OR semantics.

When color output is enabled, template bodies are syntax-highlighted (Chroma: YAML plus go-text-template).`,
		Example: `  hydra local source prod.infra.monitoring --hydra-context /path/to/context

  hydra local source prod.*.*

  hydra local source prod.apps.my-service --include 'kind == "Deployment"'

  hydra local source prod.infra.prom --include-path charts/kube-prometheus-stack/templates/prometheus`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := log.Default()
			config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo)

			appIds, err := commands.ResolveAppIdsFromConfig(l, f.HydraContext, config, types.ToAppIdPatterns(args), f.ExcludeAppPatterns, f.HelmNetworkMode, false)
			if err != nil {
				return err
			}

			for appId := range appIds {
				f.AppId = appId
				_, result, err := source(f)
				if err != nil {
					return err
				}
				if err := writeCommandResult(result); err != nil {
					return err
				}
				l.Info(logIdCmd, "printed chart template sources for AppId '{appId}'", log.String("appId", string(appId)))
			}

			return nil
		},
	}

	_ = registerLocalSourceFlags(cmd, &f)

	return cmd
}
