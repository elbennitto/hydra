package action

import (
	"fmt"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/ci"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

type LocalPackageFlags struct {
	flags.ContextFlag
	flags.HelmNetworkModeFlag
	flags.NoCacheFlag
	AppId       types.AppId
	Destination string
}

var _ flags.WithContextFlag = (*LocalPackageFlags)(nil)
var _ flags.WithHelmNetworkModeFlag = (*LocalPackageFlags)(nil)
var _ flags.WithNoCacheFlag = (*LocalPackageFlags)(nil)

func (f *LocalPackageFlags) Flags() flags.Flags {
	return f
}

func (f *LocalPackageFlags) WithNoCacheFlag() *flags.NoCacheFlag {
	return &f.NoCacheFlag
}

func LocalPackage(f LocalPackageFlags) (string, error) {
	l := log.Default()
	config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo)
	app, err := resolveHydraAppForLocalPrint(l, f.HydraContext, f.AppId, config, f.HelmNetworkMode)
	if err != nil {
		return "", err
	}
	hydraApp := app.AsApp()
	if hydraApp == nil {
		return "", fmt.Errorf("local package requires a root or child app")
	}
	chartDir, err := hydra.ChartDirectoryForHydraApp(hydraApp)
	if err != nil {
		return "", err
	}
	packaged, err := ci.RunLocalPackage(chartDir.Path(), f.Destination)
	if err != nil {
		return "", err
	}
	l.Info(logIdAction, "packaged chart for AppId '{appId}' to {path}",
		log.String("appId", string(f.AppId)),
		log.String("path", packaged.TGZPath))
	return packaged.TGZPath, nil
}
