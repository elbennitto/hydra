package commands

import (
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"

	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

// ResolveCommandClusterOptions describes how a hydra CLI command (or a programmatic caller)
// requests a single resolved [hydra.Cluster]. Exactly one of [AppIds] or [ClusterName] must be
// set: when [AppIds] is non-empty, the cluster is derived from the app ids (and all ids must
// belong to the same cluster); otherwise [ClusterName] is used directly (callers that only know
// the current cluster should pass [types.InCluster]).
type ResolveCommandClusterOptions struct {
	Config       types.Config
	HydraContext types.HydraContext
	Limits       hydra.RESTClientLimits

	// AppIds: when set, the cluster name is derived from these app ids and all ids must
	// belong to the same cluster. Mutually exclusive with [ClusterName].
	AppIds sets.Set[types.AppId]

	// ClusterName: explicit cluster name (use [types.InCluster] for the current cluster).
	// Used when [AppIds] is empty.
	ClusterName types.ClusterName
}

// ResolveCommandClusterFunc is the package-level seam used by [ResolveCommandCluster]. Tests
// substitute this variable to inject a fake cluster without going through the live path resolver.
// In production it is the unsubstituted implementation [resolveCommandClusterDefault].
var ResolveCommandClusterFunc = resolveCommandClusterDefault

// ResolveCommandCluster is the single entrypoint every hydra CLI command uses to obtain its
// [hydra.Cluster] handle. It supersedes the previous trio of [hydra.ResolvePathWithCluster]
// and the (now removed) ClusterForAppIds / ClusterForClusterName wrappers. Tests inject fake
// clusters by substituting [ResolveCommandClusterFunc] instead of file-private seams in each
// cli/action file.
func ResolveCommandCluster(opts ResolveCommandClusterOptions) (*hydra.Cluster, error) {
	return ResolveCommandClusterFunc(opts)
}

func resolveCommandClusterDefault(opts ResolveCommandClusterOptions) (*hydra.Cluster, error) {
	clusterName, err := commandClusterNameFromOptions(opts)
	if err != nil {
		return nil, err
	}
	return hydra.ResolvePathWithCluster(log.Default(), opts.HydraContext, clusterName, opts.Config, opts.Limits)
}

func commandClusterNameFromOptions(opts ResolveCommandClusterOptions) (types.ClusterName, error) {
	// A non-nil AppIds (even when empty) selects the app-id-derived path; nil/absent AppIds
	// means the caller chose the explicit ClusterName path.
	if opts.AppIds != nil {
		return effectiveClusterNameFromAppIds(opts.HydraContext, opts.Config, opts.AppIds)
	}
	return opts.ClusterName, nil
}

func clusterNameFromAppIds(appIds sets.Set[types.AppId]) (types.ClusterName, error) {
	oneAppId, ok := appIds.Clone().PopAny()
	if !ok {
		return "", log.CreateError(errors.ErrNoAppsSpecified, "no apps specified")
	}
	clusterName, err := oneAppId.ClusterName()
	if err != nil {
		return "", err
	}
	for appId := range appIds {
		c, err := appId.ClusterName()
		if err != nil {
			return "", err
		}
		if c != clusterName {
			return "", log.CreateError(errors.ErrAppIdsDifferentClusters,
				"all app ids must belong to the same cluster",
				log.String("app1", string(oneAppId)),
				log.String("app2", string(appId)))
		}
	}
	return clusterName, nil
}

func effectiveClusterNameFromAppIds(
	hydraContext types.HydraContext,
	config types.Config,
	appIds sets.Set[types.AppId],
) (types.ClusterName, error) {
	if len(appIds) == 0 {
		return "", log.CreateError(errors.ErrNoAppsSpecified, "no apps specified")
	}
	offlineConfig := types.NewConfig(config.Color(), config.DryRun(), types.KubernetesConnectionAllowedNo, config.HelmTemplateCacheEnabled())
	ctxHydra, err := hydra.ResolvePath(log.Default(), hydraContext, offlineConfig)
	if err != nil {
		return "", err
	}
	ctx := ctxHydra.AsContext()
	if ctx == nil {
		return "", log.CreateError(errors.ErrInvalidHydraStructure,
			"hydra context path does not resolve to a context")
	}

	var effectiveCluster types.ClusterName
	var firstApp types.AppId
	for appId := range appIds {
		clusterName, err := effectiveClusterNameForApp(ctx, appId)
		if err != nil {
			return "", err
		}
		if firstApp == "" {
			firstApp = appId
			effectiveCluster = clusterName
			continue
		}
		if clusterName != effectiveCluster {
			return "", log.CreateError(errors.ErrAppIdsDifferentClusters,
				"all app ids must belong to the same effective cluster",
				log.String("app1", string(firstApp)),
				log.String("app2", string(appId)))
		}
	}
	return effectiveCluster, nil
}

func effectiveClusterNameForApp(ctx *hydra.Context, appId types.AppId) (types.ClusterName, error) {
	clusterName, err := appId.ClusterName()
	if err != nil {
		return "", err
	}
	if !appId.IsRootApp() {
		return clusterName, nil
	}

	cluster, err := ctx.WithCluster(clusterName, hydra.RESTClientLimits{})
	if err != nil {
		return "", err
	}
	rootAppName, err := appId.RootAppName()
	if err != nil {
		return "", err
	}
	rootApp, err := hydra.NewRootApp(cluster, rootAppName)
	if err != nil {
		return "", err
	}
	inClusterArgoCD, err := rootApp.IsManagedByInClusterArgoCD(types.HelmNetworkModeOffline)
	if err != nil {
		return "", err
	}
	if inClusterArgoCD {
		return types.InCluster, nil
	}
	return clusterName, nil
}
