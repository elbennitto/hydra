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

// NewTemplateCommand creates and returns the template subcommand
func NewTemplateCommand() *cobra.Command {
	return newTemplateCommand(action.Template)
}

func newTemplateCommand(template func(flags action.TemplateFlags) (hydra.Hydra, string, error)) *cobra.Command {
	f := action.TemplateFlags{
		ColorFlag: flags.ColorFlag{
			Color: types.ColorYes,
		},
	}

	cmd := &cobra.Command{
		Use:   "template <appId> [appId...]",
		Short: "Render and display Helm templates for one or more apps",
		Long: `Render the Helm templates for the specified app(s) and print the resulting
Kubernetes manifests to stdout. This is a local operation that does not
require a connection to the Kubernetes cluster.

The output is valid YAML that can be piped to other tools like kubectl or yq.

App IDs support glob-style wildcard matching:
  *            matches any characters except '.' (stays within one segment)
  **           matches any characters including '.' (crosses segments)

Use --exclude-app to remove specific apps from the selection.

Use --include and --exclude (CEL) to print only matching rendered resources, same as hydra local find.`,
		Example: `  # Render templates for a single app
  hydra local template prod.infra.monitoring --hydra-context /path/to/context

  # Render templates for multiple apps
  hydra local template prod.infra.monitoring prod.infra.logging

  # Render all child apps in a cluster
  hydra local template prod.*.*

  # Render all apps except cert-manager
  hydra local template prod.** --exclude-app prod.cluster-infra.cert-manager

  # Only Deployments (multi-doc YAML, no Helm # Source headers)
  hydra local template prod.apps.my-service --include 'kind == "Deployment"'

  # Pipe rendered templates to kubectl
  hydra local template prod.infra.monitoring | kubectl apply --dry-run=client -f -`,
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
			var cloneYaml types.YamlString
			if cloneEntities.Len() > 0 {
				cloneYaml, err = cloneEntities.ToYaml(types.KeyTemplateEntity)
				if err != nil {
					return err
				}
			}

			for appId := range appIds {
				f.AppId = appId
				_, result, err := template(f)
				if err != nil {
					return err
				}
				if err := writeCommandResult(result); err != nil {
					return err
				}
				l.Info(logIdCmd, "rendered templates for AppId '{appId}'", log.String("appId", string(appId)))
			}

			if len(cloneYaml) > 0 {
				if err := writeCommandResult("---\n" + string(cloneYaml)); err != nil {
					return err
				}
			}

			return nil
		},
	}

	DefineFlags(cmd, &f)

	return cmd
}
