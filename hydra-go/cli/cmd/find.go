package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func NewFindCommand() *cobra.Command {
	return newFindCommand(action.Find)
}

func newFindCommand(find func(flags action.FindFlags) (hydra.Hydra, string, error)) *cobra.Command {
	flags := action.FindFlags{}

	cmd := &cobra.Command{
		Use:   "find <appId> [appId...]",
		Short: "Find rendered resources and project values as a YAML array",
		Long: `Render one or more Hydra apps locally, filter the resulting resources with
CEL predicates, and project values from each match into a YAML array.

This is a local read-only query command. It does not connect to a live
cluster.

App IDs support glob-style wildcard matching:
  *            matches any characters except '.' (stays within one segment)
  **           matches any characters including '.' (crosses segments)

The --pick flag is only available on this command. It evaluates a CEL
expression for every matched resource and serializes the result as a YAML
array. If not specified, --pick defaults to 'id'. Use --uniq to deduplicate
projected values after evaluation.`,
		Example: `  # Which child apps render KafkaUser resources?
  hydra local find prod.*.* --include 'kind == "KafkaUser"' --pick 'appIds[0]' --uniq

  # Query all apps across all clusters
  hydra local find ** --include 'kind == "Deployment"' --pick '{"appId": appIds[0], "name": name}'

  # Exclude selected apps from the search
  hydra local find prod.** --exclude-app prod.cluster-infra.cert-manager --include 'kind == "Secret"' --pick 'name'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.AppIdPatterns = types.ToAppIdPatterns(args)

			_, result, err := find(flags)
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
