package cmd

import (
	"cmp"
	"fmt"
	"slices"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func newLocalListCommand(sortedRendered func(action.TemplateFlags) (entity.Entities, error)) *cobra.Command {
	f := action.TemplateFlags{
		ColorFlag: flags.ColorFlag{
			Color: types.ColorYes,
		},
	}

	cmd := &cobra.Command{
		Use:   "list <appId> [appId...]",
		Short: "Print Hydra ids of rendered template resources for one or more apps",
		Long: `Runs the same local Helm render pipeline as hydra local template, then prints the
canonical Hydra resource id for each rendered object (one id per line, sorted, deduplicated).

Use --exclude-app to remove specific apps from the selection. Use --include and --exclude (CEL)
to narrow which rendered resources contribute ids, same as hydra local template.`,
		Example: `  # List ids for one app
  hydra local list prod.infra.monitoring --hydra-context /path/to/context

  # Combine glob selection with explicit excludes
  hydra local list prod.** --exclude-app prod.cluster-infra.cert-manager

  # Only Deployment ids
  hydra local list prod.apps.my-service --include 'kind == "Deployment"'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			l := log.Default()
			config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo)

			appIds, err := commands.ResolveAppIdsFromConfig(l, f.HydraContext, config, types.ToAppIdPatterns(args), f.ExcludeAppPatterns, f.HelmNetworkMode, false)
			if err != nil {
				return err
			}

			cluster, err := commands.ResolveCommandCluster(commands.ResolveCommandClusterOptions{
				Config:       config,
				HydraContext: f.HydraContext,
				Limits:       hydra.RESTClientLimits{},
				AppIds:       appIds,
			})
			if err != nil {
				return err
			}

			cloneEntities, err := localTemplatePatchedCloneEntities(l, cluster, appIds, &f)
			if err != nil {
				return err
			}

			seen := sets.New[types.Id]()
			var ids []types.Id

			for _, appId := range sortedAppIdSlice(appIds) {
				f.AppId = appId
				ents, err := sortedRendered(f)
				if err != nil {
					return err
				}
				ids, err = appendDistinctEntityIds(ids, seen, ents)
				if err != nil {
					return err
				}
				l.Info(logIdCmd, "listed template ids for AppId '{appId}'", log.String("appId", string(appId)))
			}

			ids, err = appendDistinctEntityIds(ids, seen, cloneEntities)
			if err != nil {
				return err
			}

			slices.SortFunc(ids, func(a, b types.Id) int {
				return cmp.Compare(string(a), string(b))
			})

			for _, id := range ids {
				fmt.Println(string(id))
			}

			return nil
		},
	}

	DefineFlags(cmd, &f)

	return cmd
}

func sortedAppIdSlice(appIds sets.Set[types.AppId]) []types.AppId {
	out := make([]types.AppId, 0, len(appIds))
	for id := range appIds {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func appendDistinctEntityIds(dst []types.Id, seen sets.Set[types.Id], ents entity.Entities) ([]types.Id, error) {
	for _, item := range ents.Items {
		id, err := item.Id()
		if err != nil {
			return nil, err
		}
		if seen.Has(id) {
			continue
		}
		seen.Insert(id)
		dst = append(dst, id)
	}
	return dst, nil
}
