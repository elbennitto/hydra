package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func newLocalPackageCommand(packageChart func(flags action.LocalPackageFlags) (string, error)) *cobra.Command {
	f := action.LocalPackageFlags{
		HelmNetworkModeFlag: flags.HelmNetworkModeFlag{
			HelmNetworkMode: types.HelmNetworkModeOnline,
		},
		Destination: ".",
	}

	cmd := &cobra.Command{
		Use:   "package <appId>",
		Short: "Package one app's Helm chart into a .tgz archive",
		Long: `Package the chart directory for the specified app into a Helm .tgz archive.

This uses the same chart packaging path as hydra ci publish, including
global.hydra.templateFiles preprocessing before the archive is written.`,
		Example: `  hydra local package prod.cluster-infra.cert-manager

  hydra local package prod.cluster-infra.cert-manager --destination dist`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.AppId = types.AppId(args[0])
			path, err := packageChart(f)
			if err != nil {
				return err
			}
			return writeCommandResult(path + "\n")
		},
	}

	_ = defineContextFlag(cmd, &f)
	_ = defineNetworkModeFlag(cmd, &f)
	_ = defineNoCacheFlag(cmd, &f)
	cmd.Flags().StringVarP(&f.Destination, "destination", "d", f.Destination, "Directory to write the packaged chart archive to")

	return cmd
}
