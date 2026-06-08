package action

import (
	"slices"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// AppsFlags configures app-id selection for commands that only list matching apps.
type AppsFlags struct {
	flags.HelmNetworkModeFlag
	flags.ContextFlag
	flags.ExcludeAppFlag
	flags.NoCacheFlag
	AppIdPatterns []types.AppIdPattern
}

func (f *AppsFlags) Flags() flags.Flags {
	return f
}

// ResolveApps resolves app-id patterns (plus excludes) against the Hydra context
// and returns matching app ids sorted lexicographically.
func ResolveApps(f AppsFlags) ([]types.AppId, error) {
	l := log.Default()
	config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo)

	resolved, err := commands.ResolveAppIdsFromConfig(
		l,
		f.HydraContext,
		config,
		f.AppIdPatterns,
		f.ExcludeAppPatterns,
		f.HelmNetworkMode,
		false,
	)
	if err != nil {
		return nil, err
	}

	out := make([]types.AppId, 0, len(resolved))
	for appId := range resolved {
		out = append(out, appId)
	}
	slices.Sort(out)

	return out, nil
}
