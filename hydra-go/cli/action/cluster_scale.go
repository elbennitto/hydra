package action

import (
	"context"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/k8s"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

type ClusterScaleFlags struct {
	flags.ClusterRESTClientFlags
	flags.HelmNetworkModeFlag
	flags.ContextFlag
	flags.ColorFlag
	flags.DryRunFlag
	flags.NoClusterFlag
	flags.KubernetesVersionFlag
	flags.BootstrapFlag
	flags.ForceScaleDownFlag
	flags.ScaleTimeoutFlag
	flags.ClusterWorkloadTimeoutFlag
	flags.ExcludeAppFlag
	flags.NoCacheFlag
	AppIdPatterns []types.AppIdPattern
}

func (f *ClusterScaleFlags) Flags() flags.Flags {
	return f
}

func (f *ClusterScaleFlags) WithBootstrapFlag() *flags.BootstrapFlag {
	return &f.BootstrapFlag
}

var _ flags.WithBootstrapFlag = (*ClusterScaleFlags)(nil)

func clusterScale(f ClusterScaleFlags, direction commands.ScaleDirection) error {
	l := log.Default()
	config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedYes)

	appIds, err := commands.ResolveAppIdsFromConfig(l, f.HydraContext, config, f.AppIdPatterns, f.ExcludeAppPatterns, f.HelmNetworkMode, false)
	if err != nil {
		return err
	}

	if len(appIds) == 0 {
		return log.CreateError(errors.ErrNoAppsSpecified, "no apps specified for scale")
	}

	cluster, err := commands.ResolveCommandCluster(commands.ResolveCommandClusterOptions{
		Config:       config,
		HydraContext: f.HydraContext,
		Limits:       f.ToRESTClientLimits(),
		AppIds:       appIds,
	})
	if err != nil {
		return err
	}

	skipRootApps := types.SkipRootApps(cluster.ClusterName != types.InCluster)
	renderedEntities, _, _, err := commands.RenderCluster(cluster, appIds, f.KubernetesVersion, types.CrdModeSilent, skipRootApps, nil)
	if err != nil {
		return err
	}

	parsers, err := hydra.HydraAppRefParsers(cluster, appIds, f.HelmNetworkMode, renderedEntities)
	if err != nil {
		return err
	}

	preferredVersions, err := cluster.PreferredVersions(nil)
	if err != nil {
		return err
	}

	if f.NoCluster {
		refs, err := references.Refs(l, renderedEntities, types.KeyTemplateEntity, nil, entity.Entities{}, entity.Entities{}, preferredVersions, parsers)
		if err != nil {
			return err
		}
		refs = references.AnnotateRefsWithSource(refs, types.RefSourceTemplate)
		l.Info(logIdAction, "no-cluster mode: rendered {entities} entities, resolved {refs} refs — skipping cluster operations",
			log.Int("entities", renderedEntities.Len()),
			log.Int("refs", len(refs)))
		return nil
	}

	restConfig, err := commands.RestConfigForHydra(cluster)
	if err != nil {
		return err
	}
	restConfig.WarningHandlerWithContext = k8s.KubernetesAPICtxWarningHandler{
		Logger: l,
		LogID:  logIdAction,
		Source: k8s.KubernetesAPIWarningSourceClusterScale,
		Debug:  true,
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	scaleMap, err := commands.MergedScaleWorkloadMap(cluster, appIds, f.HelmNetworkMode, renderedEntities)
	if err != nil {
		return err
	}

	clusterEntities, err := commands.ListClusterAll(cluster, types.KeyClusterEntity, false, 0)
	if err != nil {
		return err
	}
	invModel, err := commands.BuildResourceModel(commands.ResourceModelInput{
		Cluster:         cluster,
		ClusterEntities: &clusterEntities,
		NetworkMode:     types.HelmNetworkModeOffline,
		Bootstrap:       types.BootstrapNo,
	}, false)
	if err != nil {
		return err
	}
	liveEntities := invModel.ClusterEntities()
	scaleEntities, err := renderedEntities.Merge(liveEntities, types.KeyClusterEntity)
	if err != nil {
		return err
	}

	ownerNamespaces, err := hydra.HydraAppNamespaceOwners(cluster, appIds, f.HelmNetworkMode)
	if err != nil {
		return err
	}
	refEntities, err := commands.AugmentClusterScaleEntitiesForRefs(scaleEntities, ownerNamespaces)
	if err != nil {
		return err
	}
	refs, err := references.Refs(l, refEntities, types.KeyTemplateEntity, nil, entity.Entities{}, entity.Entities{}, preferredVersions, parsers)
	if err != nil {
		return err
	}
	refs = references.AnnotateRefsWithSource(refs, types.RefSourceTemplate)

	// Build the same merged inspect ref graph as `hydra gitops inspect` once and reuse it as
	// fullRefs for both scale up and scale down (symmetry: refsSync drives topological ordering,
	// fullRefs covers transitive reachability / readiness). Pre-rendered templates and pre-listed
	// inventory are passed through so the central helper does not redo RenderClusterSelectedApps
	// or ListClusterAll.
	inspectGraph, err := commands.LoadInspectRefGraph(commands.InspectRefGraphParams{
		Cluster:                     cluster,
		NetworkMode:                 f.HelmNetworkMode,
		Bootstrap:                   f.Bootstrap,
		ClusterInventory:            &liveEntities,
		IncludeTemplateRefs:         true,
		IncludeClusterRefs:          true,
		IncludeCloneMaterialization: true,
		SkipFoundDefinitionsInfoLog: true,
	})
	if err != nil {
		return err
	}
	var fullRefs []types.Ref
	if inspectGraph != nil {
		fullRefs = inspectGraph.Refs
	}

	listPods := func(ctx context.Context) ([]unstructured.Unstructured, error) {
		return commands.ListAllPods(ctx, dynamicClient)
	}

	switch direction {
	case commands.ScaleUp:
		l.Info(logIdAction, "finding workloads to scale up across {count} apps", log.Int("count", len(appIds)))
		clusterMutated := false
		if !f.DryRun {
			clusterMutated, err = commands.ScaleUpWillMutate(scaleEntities, types.KeyTemplateEntity, types.KeyClusterEntity, scaleMap)
			if err != nil {
				return err
			}
		}
		readyEval, err := commands.ReadyEvaluatorFromHydra(cluster, f.HelmNetworkMode, scaleMap, scaleEntities, types.KeyClusterEntity)
		if err != nil {
			return err
		}
		if err := commands.ScaleUpWorkloads(context.Background(), l, dynamicClient,
			scaleEntities, refs, fullRefs, types.KeyTemplateEntity, types.KeyClusterEntity, f.DryRun, f.ScaleTimeout, readyEval, scaleMap); err != nil {
			return err
		}
		_, err = commands.ReconcileScaleUpPods(
			context.Background(),
			l,
			dynamicClient,
			scaleEntities,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			clusterMutated,
			f.DryRun,
			listPods,
		)
		return err
	case commands.ScaleDown:
		l.Info(logIdAction, "finding workloads to scale down across {count} apps", log.Int("count", len(appIds)))

		minor := commands.ParseKubernetesMinorFromVersionString(string(f.KubernetesVersion))
		if minor <= 0 {
			minor = 99
		}
		effectivePresets, err := hydra.EffectiveClusterDefaultsPresetsForCluster(cluster, appIds, f.HelmNetworkMode, renderedEntities, minor)
		if err != nil {
			return err
		}
		exemptClusterOnly := commands.KubernetesBuiltinExpectedIDSet(minor, effectivePresets)

		clusterMutated := false
		if !f.DryRun {
			clusterMutated, err = commands.ScaleDownWillMutate(scaleEntities, types.KeyTemplateEntity, types.KeyClusterEntity, scaleMap)
			if err != nil {
				return err
			}
			coWill, err := commands.ClusterOnlyScaleDownWillMutate(scaleEntities, types.KeyTemplateEntity, types.KeyClusterEntity, fullRefs, exemptClusterOnly)
			if err != nil {
				return err
			}
			clusterMutated = clusterMutated || coWill
		}
		if err := commands.ScaleDownWorkloads(context.Background(), l, dynamicClient,
			scaleEntities, refs, fullRefs, types.KeyTemplateEntity, f.DryRun, f.ForceScaleDown, f.ScaleTimeout, scaleMap); err != nil {
			return err
		}
		clusterOnlyMutated, err := commands.ScaleDownClusterOnlyWorkloads(
			context.Background(),
			l,
			dynamicClient,
			scaleEntities,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			fullRefs,
			exemptClusterOnly,
			f.DryRun,
			f.ForceScaleDown,
			f.ClusterWorkloadTimeout,
			listPods,
		)
		if err != nil {
			return err
		}
		reconcileMutated := clusterMutated || clusterOnlyMutated
		_, err = commands.ReconcileScaleDownPods(
			context.Background(),
			l,
			dynamicClient,
			scaleEntities,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			fullRefs,
			reconcileMutated,
			f.ForceScaleDown,
			f.DryRun,
			listPods,
		)
		return err
	default:
		return log.CreateError(errors.ErrInternalError, "invalid scale direction: {direction}",
			log.String("direction", string(direction)))
	}
}

func ClusterScaleUp(f ClusterScaleFlags) error {
	return clusterScale(f, commands.ScaleUp)
}

func ClusterScaleDown(f ClusterScaleFlags) error {
	return clusterScale(f, commands.ScaleDown)
}
