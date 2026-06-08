package commands

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	goocel "github.com/google/cel-go/cel"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/k8s"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

type ReviewFinding struct {
	Target  types.Id   `yaml:"target"`
	Message string     `yaml:"message"`
	Sources []types.Id `yaml:"sources"`
}

type ReviewFindingCallback func(ReviewFinding) error

type reviewEdgeKey struct {
	From types.Id
	To   types.Id
}

type reviewFindingKey struct {
	Target  types.Id
	Message string
}

// templateClusterRefEdgeKey identifies a ref edge for template-vs-live-object comparison.
type templateClusterRefEdgeKey struct {
	From         types.Id
	To           types.Id
	RefType      types.RefType
	EndpointType types.RefEndpointType
	Reverse      bool
	Labels       string
}

// ReviewRefsBuiltinOptions configures Kubernetes upstream bootstrap behavior for review refs.
type ReviewRefsBuiltinOptions struct {
	// MergeBuiltinsIntoTemplateTargets adds synthetic bootstrap entities to template targets
	// (local review) before virtual ref resolution.
	MergeBuiltinsIntoTemplateTargets bool
	KubernetesMinor                  int

	// AuditClusterBootstrapMissing emits findings for bootstrap IDs missing from the live cluster
	// (full-catalog scan with empty sources). Used only for hydra gitops review cluster, not app.
	AuditClusterBootstrapMissing bool
	LiveClusterIds               sets.Set[types.Id]

	// AuxiliarySyntheticTargetEntities supplies namespace default bundle entities derived from the
	// full enabled-app template render for cluster review only. They satisfy same-namespace refs to
	// ServiceAccount/default and ConfigMap/kube-root-ca.crt when live objects are absent.
	AuxiliarySyntheticTargetEntities entity.Entities

	// ClusterDefaultsEffectivePresets is merged global.hydra.presets + builtins; used for blanket
	// bootstrap audit expected ids (preset ids union).
	ClusterDefaultsEffectivePresets []hydra.ClusterDefaultsPresetEffective
}

const missingTargetResourceFinding = "missing target resource"

// refDriftMissingOnClusterFinding is reported when a ref edge exists on the rendered template source
// but not when refs are extracted from live cluster resource bodies (same id).
const refDriftMissingOnClusterFinding = "reference in template but not in cluster resource"

// refDriftExtraOnClusterFinding is the inverse: present on the live object graph but not in the template.
const refDriftExtraOnClusterFinding = "reference on cluster but not in template"

// clusterReviewRefsValidationStepCount is the substeps while validating references inside
// reviewRefsEntitiesWithTargetsAndParserSetsCallback (cluster review only), folded into the
// unified post-discovery bar.
const clusterReviewRefsValidationStepCount = 5

// clusterReviewRefsValidationBaseUnified is the unified post-discovery bar index (1-based) of the
// last outer step before reference validation substeps advance the bar.
const clusterReviewRefsValidationBaseUnified = 5

// clusterReviewPostDiscoveryUnifiedTotal counts post-list milestones through the ref-ownership
// handoff: five outer milestones, five reference-validation substeps, then one handoff milestone.
const clusterReviewPostDiscoveryUnifiedTotal = clusterReviewRefsValidationBaseUnified + clusterReviewRefsValidationStepCount + 1

// clusterReviewUnifiedTotal is one footer bar for cluster review after ListClusterAll: post-list
// work ([clusterReviewPostDiscoveryUnifiedTotal] steps) plus ref-ownership milestones
// ([RefOwnershipReviewStepCount] steps).
const clusterReviewUnifiedTotal = clusterReviewPostDiscoveryUnifiedTotal + RefOwnershipReviewStepCount

// refsValidationScanProgressEvery controls how often the nested refs-validation bar detail line
// refreshes during the per-ref scan (large ref lists).
const refsValidationScanProgressEvery = 256

// refsValidationProgress wires reference-validation substeps into the unified post-discovery bar.
type refsValidationProgress struct {
	progress log.Progress
	task     log.ProgressTask
}

func advanceRefsValidationProgress(rv *refsValidationProgress, step int, detail string) {
	if rv == nil || rv.progress == nil {
		return
	}
	if rv.task != nil {
		rv.task.SetDetail(k8s.TruncateFooterDetail(detail))
	}
	unified := clusterReviewRefsValidationBaseUnified + step
	rv.progress.Advance(unified, clusterReviewUnifiedTotal)
}

func ReviewRefs(
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	bootstrap types.Bootstrap,
) ([]ReviewFinding, error) {
	return collectReviewFindings(func(onFinding ReviewFindingCallback) (int, error) {
		return ReviewRefsCallback(cluster, appIds, networkMode, bootstrap, 0, onFinding)
	})
}

func ReviewRefsCallback(
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	bootstrap types.Bootstrap,
	reviewParallelism int,
	onFinding ReviewFindingCallback,
) (int, error) {
	cluster.ResetPreferredVersionsCache()
	preferredVersions, err := cluster.PreferredVersions(func() (types.ScopeInfoMap, error) {
		return ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity, types.CrdModeSilent)
	})
	if err != nil {
		return 0, err
	}

	targetAppIds, err := cluster.AppIds(networkMode)
	if err != nil {
		return 0, err
	}
	targetEntitiesPreClone, err := RenderClusterSelectedApps(cluster, networkMode, "", targetAppIds, types.KeyTemplateEntity)
	if err != nil {
		return 0, err
	}
	perAppRendered, err := PartitionTemplateEntitiesByPrimaryApp(targetEntitiesPreClone)
	if err != nil {
		return 0, err
	}
	_, sourceEntities, err := targetEntitiesPreClone.SelectByPrimaryTemplateAppId(appIds)
	if err != nil {
		return 0, err
	}

	targetEntities, _, err := MaterializeHydraClonesForApply(
		cluster.L(), cluster, targetAppIds, targetEntitiesPreClone, types.KeyTemplateEntity, bootstrap, networkMode, nil)
	if err != nil {
		return 0, err
	}

	parserStart := time.Now()
	sourceParsers, err := hydra.HydraAppRefParsers(cluster, appIds, networkMode, sourceEntities)
	if err != nil {
		return 0, err
	}

	targetParsers, err := hydra.HydraAppRefParsers(cluster, targetAppIds, networkMode, targetEntities)
	if err != nil {
		return 0, err
	}
	logReviewRefsPhase(cluster.L(), "HydraAppRefParsers", parserStart,
		log.Int("sourceParserCount", len(sourceParsers)),
		log.Int("targetParserCount", len(targetParsers)))

	kvo, err := hydra.KubernetesVersionOrFallback(cluster, "", networkMode)
	if err != nil {
		return 0, err
	}
	localMinor := effectiveMinorForLocalBootstrapCatalog(ParseKubernetesMinorFromVersionString(string(kvo)))
	builtinOpts := &ReviewRefsBuiltinOptions{
		MergeBuiltinsIntoTemplateTargets: true,
		KubernetesMinor:                  localMinor,
	}

	nRefs, _, err := reviewRefsEntitiesWithTargetsAndParserSetsCallback(
		cluster.L(),
		sourceEntities,
		types.KeyTemplateEntity,
		targetEntities,
		types.KeyTemplateEntity,
		appIds,
		onFinding,
		nil,
		entity.Entities{},
		entity.Entities{},
		sourceParsers,
		targetParsers,
		preferredVersions,
		builtinOpts,
		nil,
	)
	if err != nil {
		return 0, err
	}
	nOwn, err := AppendRefOwnershipReviewFindings(
		cluster, targetAppIds, networkMode, perAppRendered, targetEntities, entity.Entities{}, false, localMinor, false, onFinding,
		reviewParallelism,
		nil, nil)
	if err != nil {
		return nRefs, err
	}
	return nRefs + nOwn, nil
}

func ReviewClusterRefs(
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	bootstrap types.Bootstrap,
	reportUnassignedClusterOnly bool,
	showProgress bool,
) ([]ReviewFinding, error) {
	return collectReviewFindings(func(onFinding ReviewFindingCallback) (int, error) {
		return ReviewClusterRefsCallback(cluster, appIds, networkMode, bootstrap, reportUnassignedClusterOnly, onFinding, showProgress, 0)
	})
}

func ReviewClusterRefsCallback(
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	bootstrap types.Bootstrap,
	reportUnassignedClusterOnly bool,
	onFinding ReviewFindingCallback,
	showProgress bool,
	reviewParallelism int,
) (int, error) {
	l := cluster.L()
	scopeFromCluster := func() (types.ScopeInfoMap, error) {
		return ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity, types.CrdModeSilent)
	}

	cluster.ResetPreferredVersionsCache()
	preferredVersions, err := cluster.PreferredVersions(scopeFromCluster)
	if err != nil {
		return 0, err
	}

	targetAppIds, err := cluster.AppIds(networkMode)
	if err != nil {
		return 0, err
	}
	inventoryStart := time.Now()
	scopeInfo, err := ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity, types.CrdModeSilent)
	if err != nil {
		return 0, err
	}
	renderedAllApps, err := RenderClusterSelectedApps(cluster, networkMode, "", targetAppIds, types.KeyTemplateEntity)
	if err != nil {
		return 0, err
	}
	renderedAllApps, err = NormalizeApiVersions(cluster.L(), renderedAllApps, types.KeyTemplateEntity, cluster, func() (types.ScopeInfoMap, error) {
		return scopeInfo, nil
	})
	if err != nil {
		return 0, err
	}
	clusterEntities, err := ListClusterAll(cluster, types.KeyClusterEntity, showProgress, reviewParallelism)
	if err != nil {
		return 0, err
	}
	invModel, err := BuildResourceModel(ResourceModelInput{
		Cluster:          cluster,
		NetworkMode:      networkMode,
		Bootstrap:        bootstrap,
		TemplateEntities: &renderedAllApps,
		ClusterEntities:  &clusterEntities,
		AppIds:           targetAppIds,
		ScopeInfo:        scopeInfo,
		Parallel:         reviewParallelism,
	}, showProgress)
	if err != nil {
		return 0, err
	}
	renderedAllApps = invModel.TemplateEntities()
	_, renderedEntities, err := renderedAllApps.SelectByPrimaryTemplateAppId(appIds)
	if err != nil {
		return 0, err
	}
	clusterEntities = invModel.ClusterEntities()
	logReviewRefsPhase(l, "BuildInventory", inventoryStart,
		log.Int("templateEntityCount", len(renderedAllApps.Items)),
		log.Int("clusterEntityCount", len(clusterEntities.Items)))

	var reviewPostDiscovery log.Progress
	var reviewPostTask log.ProgressTask
	if showProgress {
		var perr error
		reviewPostDiscovery, perr = l.NewProgress("cluster review", clusterReviewUnifiedTotal)
		if perr != nil {
			return 0, perr
		}
		if reviewPostDiscovery != nil {
			reviewPostTask = reviewPostDiscovery.NewTask("")
			defer func() { _ = reviewPostDiscovery.Close() }()
		}
	}
	advanceClusterReviewProgress(l, reviewPostDiscovery, reviewPostTask, 1,
		"materialize Hydra clones from templates")

	extraCloneRules, err := hydra.CloneRulesFromHydraConfigMaps(clusterEntities, types.KeyClusterEntity, nil, targetAppIds, appIds)
	if err != nil {
		return 0, err
	}

	withClones, _, err := MaterializeHydraClonesForApply(
		l, cluster, appIds, renderedEntities, types.KeyTemplateEntity, bootstrap, networkMode, extraCloneRules)
	if err != nil {
		return 0, err
	}

	advanceClusterReviewProgress(l, reviewPostDiscovery, reviewPostTask, 2,
		"normalize API versions, apply template patches & filter sources to live cluster")

	withClones, err = NormalizeApiVersions(l, withClones, types.KeyTemplateEntity, cluster, scopeFromCluster)
	if err != nil {
		return 0, err
	}

	var hydraConfigCatalog entity.Entities
	if !appIds.Equal(targetAppIds) {
		hydraConfigCatalog, err = RenderClusterSelectedApps(
			cluster, networkMode, "", targetAppIds, types.KeyTemplateEntity, WithSkipFoundDefinitionsInfoLog())
		if err != nil {
			return 0, err
		}
	}
	templatePatchEntries, err := hydra.HydraTemplatePatchRuleEntries(cluster, appIds, networkMode, withClones, hydraConfigCatalog)
	if err != nil {
		return 0, err
	}
	var templatePatchOwnerByNs map[types.Namespace]types.AppId
	if len(templatePatchEntries) > 0 {
		templatePatchOwnerByNs, err = BuildTemplatePatchOwnerByNamespace(
			cluster, networkMode, withClones, hydraConfigCatalog, types.KeyTemplateEntity)
		if err != nil {
			return 0, err
		}
	}
	templatePatchPipe, err := NewTemplatePatchPipelineWithNamespaceOwners(templatePatchEntries, templatePatchOwnerByNs)
	if err != nil {
		return 0, err
	}
	withClones, err = ApplyTemplatePatchesToEntities(templatePatchPipe, withClones, types.KeyTemplateEntity)
	if err != nil {
		return 0, err
	}
	renderedAllApps, err = ApplyTemplatePatchesToEntities(templatePatchPipe, renderedAllApps, types.KeyTemplateEntity)
	if err != nil {
		return 0, err
	}
	perAppRendered, err := PartitionTemplateEntitiesByPrimaryApp(renderedAllApps)
	if err != nil {
		return 0, err
	}
	_, renderedEntities, err = renderedAllApps.SelectByPrimaryTemplateAppId(appIds)
	if err != nil {
		return 0, err
	}

	clusterIds, err := CollectEntityIds(clusterEntities)
	if err != nil {
		return 0, err
	}
	filteredSources, err := filterEntitiesToExistingIds(withClones, clusterIds)
	if err != nil {
		return 0, err
	}
	logReviewRefsPhase(l, "source filtering against cluster", time.Now(),
		log.Int("candidateCount", len(withClones.Items)),
		log.Int("filteredSourceCount", len(filteredSources.Items)))

	advanceClusterReviewProgress(l, reviewPostDiscovery, reviewPostTask, 3,
		"source ref parsers — Helm charts and live Hydra ConfigMaps")

	parserStart := time.Now()
	sourceParsers, err := hydra.HydraAppRefParsers(cluster, appIds, networkMode, renderedEntities)
	if err != nil {
		return 0, err
	}
	seenSourceCM := sets.New[types.Id]()
	sourceCM2, err := hydra.RefParsersFromHydraConfigMaps(clusterEntities, types.KeyClusterEntity, seenSourceCM, targetAppIds, appIds)
	if err != nil {
		return 0, err
	}
	sourceParsers = append(sourceParsers, sourceCM2...)

	advanceClusterReviewProgress(l, reviewPostDiscovery, reviewPostTask, 4,
		"target ref parsers — charts, live ConfigMaps, namespace default targets")

	targetParsers, err := hydra.HydraAppRefParsers(cluster, targetAppIds, networkMode, renderedAllApps)
	if err != nil {
		return 0, err
	}
	seenTargetCM := sets.New[types.Id]()
	targetCM2, err := hydra.RefParsersFromHydraConfigMaps(clusterEntities, types.KeyClusterEntity, seenTargetCM, targetAppIds, targetAppIds)
	if err != nil {
		return 0, err
	}
	targetParsers = append(targetParsers, targetCM2...)
	logReviewRefsPhase(l, "HydraAppRefParsers", parserStart,
		log.Int("sourceParserCount", len(sourceParsers)),
		log.Int("targetParserCount", len(targetParsers)))

	builtinOpts := &ReviewRefsBuiltinOptions{}
	serverMinor, verr := KubernetesServerMinorVersion(cluster)
	refOwnershipK8sMinor := 99
	if verr == nil {
		refOwnershipK8sMinor = serverMinor
	}
	effectivePresets, err := hydra.EffectiveClusterDefaultsPresetsForCluster(cluster, targetAppIds, networkMode, renderedAllApps, refOwnershipK8sMinor)
	if err != nil {
		return 0, err
	}
	builtinOpts.ClusterDefaultsEffectivePresets = effectivePresets
	auxDefaults, err := BuildSyntheticNamespaceDefaultTargets(l, renderedAllApps, types.KeyClusterEntity)
	if err != nil {
		return 0, err
	}
	builtinOpts.AuxiliarySyntheticTargetEntities = auxDefaults

	if verr != nil {
		l.Warn(logIdCommands, "skipping kubernetes bootstrap default audit: {reason}",
			log.String("reason", verr.Error()))
	} else if reportUnassignedClusterOnly {
		// Full-cluster bootstrap audit (expected upstream IDs absent from the live API) is not
		// app-scoped; run it only for `hydra gitops review cluster`, same gate as unassigned
		// ref-ownership findings.
		builtinOpts.AuditClusterBootstrapMissing = true
		builtinOpts.LiveClusterIds = clusterIds
		builtinOpts.KubernetesMinor = serverMinor
	}

	advanceClusterReviewProgress(l, reviewPostDiscovery, reviewPostTask, 5,
		"cluster defaults — presets, synthetic targets, Kubernetes version")

	var refsVal *refsValidationProgress
	if showProgress && reviewPostDiscovery != nil {
		refsVal = &refsValidationProgress{progress: reviewPostDiscovery, task: reviewPostTask}
	}

	nRefs, refsTemplateClusterMatch, err := reviewRefsEntitiesWithTargetsAndParserSetsCallback(
		l,
		filteredSources,
		types.KeyTemplateEntity,
		clusterEntities,
		types.KeyClusterEntity,
		appIds,
		onFinding,
		nil,
		withClones,
		clusterEntities,
		sourceParsers,
		targetParsers,
		preferredVersions,
		builtinOpts,
		refsVal,
	)
	if err != nil {
		return 0, err
	}

	advanceClusterReviewProgress(l, reviewPostDiscovery, reviewPostTask, clusterReviewPostDiscoveryUnifiedTotal,
		"ref ownership — template app map vs live cluster")

	nOwn, err := AppendRefOwnershipReviewFindings(
		cluster, targetAppIds, networkMode, perAppRendered, renderedAllApps, clusterEntities, reportUnassignedClusterOnly, refOwnershipK8sMinor, refsTemplateClusterMatch, onFinding,
		reviewParallelism,
		reviewPostDiscovery, reviewPostTask)
	if err != nil {
		return nRefs, err
	}
	return nRefs + nOwn, nil
}

func filterEntitiesToExistingIds(entities entity.Entities, existingIds sets.Set[types.Id]) (entity.Entities, error) {
	var filtered []entity.Entity
	for _, item := range entities.Items {
		id, err := item.Id()
		if err != nil {
			return entity.Entities{}, err
		}
		if existingIds.Has(id) {
			filtered = append(filtered, item)
		}
	}
	return entity.NewEntities(filtered)
}

func ReviewRefsEntities(
	l log.Logger,
	entities entity.Entities,
	selectedAppIds sets.Set[types.AppId],
	extraParsers ...[]types.RefParser,
) ([]ReviewFinding, error) {
	return collectReviewFindings(func(onFinding ReviewFindingCallback) (int, error) {
		return ReviewRefsEntitiesCallback(l, entities, selectedAppIds, onFinding, extraParsers...)
	})
}

func ReviewRefsEntitiesCallback(
	l log.Logger,
	entities entity.Entities,
	selectedAppIds sets.Set[types.AppId],
	onFinding ReviewFindingCallback,
	extraParsers ...[]types.RefParser,
) (int, error) {
	mergedParsers := flattenReviewRefParsers(extraParsers...)
	n, _, err := reviewRefsEntitiesWithTargetsAndParserSetsCallback(
		l,
		entities,
		types.KeyTemplateEntity,
		entities,
		types.KeyTemplateEntity,
		selectedAppIds,
		onFinding,
		nil,
		entity.Entities{},
		entity.Entities{},
		mergedParsers,
		mergedParsers,
		nil,
		nil,
		nil,
	)
	return n, err
}

func ReviewRefsEntitiesWithTargets(
	l log.Logger,
	sourceEntities entity.Entities,
	sourceKey types.EntityKeyUnstructured,
	targetEntities entity.Entities,
	targetKey types.EntityKeyUnstructured,
	selectedAppIds sets.Set[types.AppId],
) ([]ReviewFinding, error) {
	return collectReviewFindings(func(onFinding ReviewFindingCallback) (int, error) {
		return ReviewRefsEntitiesWithTargetsCallback(
			l,
			sourceEntities,
			sourceKey,
			targetEntities,
			targetKey,
			selectedAppIds,
			onFinding,
		)
	})
}

func ReviewRefsEntitiesWithTargetsCallback(
	l log.Logger,
	sourceEntities entity.Entities,
	sourceKey types.EntityKeyUnstructured,
	targetEntities entity.Entities,
	targetKey types.EntityKeyUnstructured,
	selectedAppIds sets.Set[types.AppId],
	onFinding ReviewFindingCallback,
) (int, error) {
	n, _, err := reviewRefsEntitiesWithTargetsAndParserSetsCallback(
		l,
		sourceEntities,
		sourceKey,
		targetEntities,
		targetKey,
		selectedAppIds,
		onFinding,
		nil,
		entity.Entities{},
		entity.Entities{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	return n, err
}

func reviewRefsEntitiesWithTargetsCallback(
	l log.Logger,
	sourceEntities entity.Entities,
	sourceKey types.EntityKeyUnstructured,
	targetEntities entity.Entities,
	targetKey types.EntityKeyUnstructured,
	selectedAppIds sets.Set[types.AppId],
	onFinding ReviewFindingCallback,
	extraParsers ...[]types.RefParser,
) (int, error) {
	mergedParsers := flattenReviewRefParsers(extraParsers...)
	n, _, err := reviewRefsEntitiesWithTargetsAndParserSetsCallback(
		l,
		sourceEntities,
		sourceKey,
		targetEntities,
		targetKey,
		selectedAppIds,
		onFinding,
		nil,
		entity.Entities{},
		entity.Entities{},
		mergedParsers,
		mergedParsers,
		nil,
		nil,
		nil,
	)
	return n, err
}

func reviewRefsEntitiesWithTargetsAndParserSetsCallback(
	l log.Logger,
	sourceEntities entity.Entities,
	sourceKey types.EntityKeyUnstructured,
	targetEntities entity.Entities,
	targetKey types.EntityKeyUnstructured,
	selectedAppIds sets.Set[types.AppId],
	onFinding ReviewFindingCallback,
	celEnvOpts []goocel.EnvOption,
	sourceManagedNamespaceEntities entity.Entities,
	sourceClusterInventoryOverlay entity.Entities,
	sourceExtraParsers []types.RefParser,
	targetExtraParsers []types.RefParser,
	preferredVersions map[types.GroupKindKey]types.Version,
	builtinOpts *ReviewRefsBuiltinOptions,
	refsVal *refsValidationProgress,
) (int, bool, error) {
	if builtinOpts != nil && builtinOpts.MergeBuiltinsIntoTemplateTargets && targetKey == types.KeyTemplateEntity {
		merged, err := mergeKubernetesBuiltinTemplateTargets(targetEntities, targetKey, builtinOpts.KubernetesMinor)
		if err != nil {
			return 0, false, err
		}
		targetEntities = merged
	}

	if builtinOpts != nil && builtinOpts.MergeBuiltinsIntoTemplateTargets && targetKey == types.KeyTemplateEntity {
		nsDefaults, err := BuildSyntheticNamespaceDefaultTargets(l, targetEntities, targetKey)
		if err != nil {
			return 0, false, err
		}
		targetEntities, err = targetEntities.Append(nsDefaults)
		if err != nil {
			return 0, false, err
		}
	}

	refsTemplateClusterMatch := false

	selectedSourceIds := sets.New[types.Id]()
	for _, item := range sourceEntities.Items {
		id, err := item.Id()
		if err != nil {
			return 0, false, err
		}

		appIds, err := item.AppIds()
		if err == nil && selectedAppIds.HasAny(appIds...) {
			selectedSourceIds.Insert(id)
		}
	}

	dualTemplateClusterRefCompare := sourceClusterInventoryOverlay.Len() > 0 &&
		targetKey == types.KeyClusterEntity && sourceKey == types.KeyTemplateEntity

	var refsCluster []types.Ref
	clusterRefEdgeKeys := sets.New[templateClusterRefEdgeKey]()

	refsStart := time.Now()
	refs, err := references.Refs(l, sourceEntities, sourceKey, celEnvOpts, sourceManagedNamespaceEntities, sourceClusterInventoryOverlay, preferredVersions, sourceExtraParsers)
	if err != nil {
		return 0, false, err
	}
	refs = references.AnnotateRefsWithSource(refs, types.RefSourceTemplate)
	logReviewRefsPhase(l, "references.Refs", refsStart,
		log.Int("refCount", len(refs)),
		log.Int("sourceEntityCount", len(sourceEntities.Items)))
	advanceRefsValidationProgress(refsVal, 1, "extract refs from rendered sources")

	enrichmentStart := time.Now()
	refs, err = augmentRefsWithExplicitKeyAttributes(sourceEntities, refs)
	if err != nil {
		return 0, false, err
	}
	refs = references.EnsureRefsHaveOriginSource(refs, types.RefSourceTemplate)
	logReviewRefsPhase(l, "explicit key attribute enrichment", enrichmentStart,
		log.Int("refCount", len(refs)))
	advanceRefsValidationProgress(refsVal, 2, "enrich explicit key attributes")

	if dualTemplateClusterRefCompare {
		clusterBodies, err := sourceEntitiesWithClusterTemplateBodies(sourceEntities, sourceClusterInventoryOverlay)
		if err != nil {
			return 0, false, err
		}
		clusterRefsStart := time.Now()
		refsClusterRaw, err := references.Refs(l, clusterBodies, sourceKey, celEnvOpts, sourceManagedNamespaceEntities, sourceClusterInventoryOverlay, preferredVersions, sourceExtraParsers)
		if err != nil {
			return 0, false, err
		}
		refsCluster, err = augmentRefsWithExplicitKeyAttributes(clusterBodies, refsClusterRaw)
		if err != nil {
			return 0, false, err
		}
		refsCluster = references.AnnotateRefsWithSource(refsCluster, types.RefSourceCluster)
		refsCluster = references.EnsureRefsHaveOriginSource(refsCluster, types.RefSourceCluster)
		logReviewRefsPhase(l, "references.Refs cluster resource bodies", clusterRefsStart,
			log.Int("refCount", len(refsCluster)),
			log.Int("sourceEntityCount", len(clusterBodies.Items)))
		for _, r := range refsCluster {
			if !selectedSourceIds.Has(r.From) {
				continue
			}
			if shouldSkipReviewRef(r) {
				continue
			}
			clusterRefEdgeKeys.Insert(refEdgeKeyForTemplateClusterCompare(r))
		}
	}

	targetKeyStart := time.Now()
	targetRefsStart := time.Now()
	expandedTargetEntities, targetRefs, err := references.ResolveVirtualRefs(l, targetEntities, targetKey, celEnvOpts, entity.Entities{}, preferredVersions, targetExtraParsers)
	if err != nil {
		return 0, false, err
	}
	targetRefs = references.AnnotateRefsWithSource(targetRefs, references.RefSourceForEntityKey(targetKey))
	logReviewRefsPhase(l, "target references.Refs", targetRefsStart,
		log.Int("refCount", len(targetRefs)),
		log.Int("targetEntityCount", len(expandedTargetEntities.Items)))

	allTargetIds := sets.New[types.Id]()
	for _, item := range expandedTargetEntities.Items {
		id, err := item.Id()
		if err != nil {
			return 0, false, err
		}
		allTargetIds.Insert(id)
	}

	targetKeys, generatedTargets, err := reviewTargetKeys(expandedTargetEntities, targetKey, targetRefs)
	if err != nil {
		return 0, false, err
	}
	logReviewRefsPhase(l, "target-key normalization", targetKeyStart,
		log.Int("targetKeyCount", len(targetKeys)),
		log.Int("generatedTargetCount", generatedTargets.Len()),
		log.Int("targetEntityCount", len(expandedTargetEntities.Items)))
	advanceRefsValidationProgress(refsVal, 3, "build target key index from live/rendered targets")

	var (
		auxExpanded      entity.Entities
		auxTargetRefs    []types.Ref
		auxAllTargetIds  sets.Set[types.Id]
		auxTargetKeys    map[types.Id]sets.Set[string]
		auxGeneratedTgts sets.Set[types.Id]
	)
	if builtinOpts != nil && len(builtinOpts.AuxiliarySyntheticTargetEntities.Items) > 0 && targetKey == types.KeyClusterEntity {
		var aerr error
		auxExpanded, auxTargetRefs, aerr = references.ResolveVirtualRefs(
			l, builtinOpts.AuxiliarySyntheticTargetEntities, targetKey, celEnvOpts, entity.Entities{}, preferredVersions, targetExtraParsers)
		if aerr != nil {
			return 0, false, aerr
		}
		auxTargetKeys, auxGeneratedTgts, aerr = reviewTargetKeys(auxExpanded, targetKey, auxTargetRefs)
		if aerr != nil {
			return 0, false, aerr
		}
		auxAllTargetIds = sets.New[types.Id]()
		for _, item := range auxExpanded.Items {
			aid, aerr := item.Id()
			if aerr != nil {
				return 0, false, aerr
			}
			auxAllTargetIds.Insert(aid)
		}
	}
	advanceRefsValidationProgress(refsVal, 4, "resolve auxiliary namespace-default targets")

	groupedSources := map[reviewFindingKey]sets.Set[types.Id]{}
	addFinding := func(target types.Id, message string, source types.Id) {
		key := reviewFindingKey{Target: target, Message: message}
		if groupedSources[key] == nil {
			groupedSources[key] = sets.New[types.Id]()
		}
		groupedSources[key].Insert(source)
	}

	var expectedBootstrapIDs sets.Set[types.Id]
	if builtinOpts != nil && builtinOpts.AuditClusterBootstrapMissing && builtinOpts.LiveClusterIds != nil {
		expectedBootstrapIDs = KubernetesBuiltinExpectedIDSet(builtinOpts.KubernetesMinor, builtinOpts.ClusterDefaultsEffectivePresets)
	}

	if dualTemplateClusterRefCompare {
		templateKeys := sets.New[templateClusterRefEdgeKey]()
		for _, r := range refs {
			if !selectedSourceIds.Has(r.From) {
				continue
			}
			if shouldSkipReviewRef(r) {
				continue
			}
			templateKeys.Insert(refEdgeKeyForTemplateClusterCompare(r))
		}
		for _, r := range refs {
			if !selectedSourceIds.Has(r.From) {
				continue
			}
			if shouldSkipReviewRef(r) {
				continue
			}
			if !clusterRefEdgeKeys.Has(refEdgeKeyForTemplateClusterCompare(r)) {
				addFinding(r.To, refDriftMissingOnClusterFinding, r.From)
			}
		}
		for _, r := range refsCluster {
			if !selectedSourceIds.Has(r.From) {
				continue
			}
			if shouldSkipReviewRef(r) {
				continue
			}
			if !templateKeys.Has(refEdgeKeyForTemplateClusterCompare(r)) {
				addFinding(r.To, refDriftExtraOnClusterFinding, r.From)
			}
		}
		refsTemplateClusterMatch = templateClusterRefKeysEqual(templateKeys, clusterRefEdgeKeys)
	}

	advanceRefsValidationProgress(refsVal, 5, fmt.Sprintf("scan references vs cluster — 0 / %d", len(refs)))
	for i, ref := range refs {
		if refsVal != nil && refsVal.task != nil && len(refs) > 0 &&
			(i%refsValidationScanProgressEvery == 0 || i == len(refs)-1) {
			refsVal.task.SetDetail(k8s.TruncateFooterDetail(fmt.Sprintf(
				"scan references vs cluster — %d / %d", i+1, len(refs))))
		}
		if !selectedSourceIds.Has(ref.From) {
			continue
		}
		if shouldSkipReviewRef(ref) {
			continue
		}

		if dualTemplateClusterRefCompare {
			if !clusterRefEdgeKeys.Has(refEdgeKeyForTemplateClusterCompare(ref)) {
				continue
			}
		}

		sourceEnt, sourceEntOk := sourceEntities.EntityMap[ref.From]

		targetExists := allTargetIds.Has(ref.To) || generatedTargets.Has(ref.To)
		if !targetExists && sourceEntOk && qualifiesForSyntheticNamespaceDefaultRef(sourceEnt, ref) {
			targetExists = auxAllTargetIds.Has(ref.To) || auxGeneratedTgts.Has(ref.To)
		}
		if !targetExists {
			if hasIncomingTargetSuppressionRef(refs, targetRefs, ref.To) {
				continue
			}
			if expectedBootstrapIDs != nil && builtinOpts != nil && builtinOpts.LiveClusterIds != nil &&
				expectedBootstrapIDs.Has(ref.To) && !builtinOpts.LiveClusterIds.Has(ref.To) {
				continue
			}
			addFinding(ref.To, missingTargetResourceFinding, ref.From)
			continue
		}

		keys := refAttributesByType(ref.Attributes, "key")
		if len(keys) == 0 {
			continue
		}

		availableKeys, ok := targetKeys[ref.To]
		if !ok && sourceEntOk && qualifiesForSyntheticNamespaceDefaultRef(sourceEnt, ref) {
			availableKeys = auxTargetKeys[ref.To]
			ok = availableKeys != nil
		}
		if !ok {
			continue
		}
		for _, key := range keys {
			if !availableKeys.Has(key) {
				addFinding(ref.To, fmt.Sprintf("missing referenced key %q", key), ref.From)
			}
		}
	}

	if expectedBootstrapIDs != nil && builtinOpts != nil && builtinOpts.AuditClusterBootstrapMissing && builtinOpts.LiveClusterIds != nil {
		missingIDs := expectedBootstrapIDs.UnsortedList()
		slices.Sort(missingIDs)
		for _, bid := range missingIDs {
			if builtinOpts.LiveClusterIds.Has(bid) {
				continue
			}
			key := reviewFindingKey{Target: bid, Message: missingClusterDefaultResourceFinding}
			if groupedSources[key] == nil {
				groupedSources[key] = sets.New[types.Id]()
			}
		}
	}

	if refsVal != nil && refsVal.task != nil {
		refsVal.task.SetDetail(k8s.TruncateFooterDetail("group & sort findings"))
	}

	groupingStart := time.Now()
	findings := make([]ReviewFinding, 0, len(groupedSources))
	for key, sources := range groupedSources {
		sourceList := sources.UnsortedList()
		slices.Sort(sourceList)
		findings = append(findings, ReviewFinding{
			Target:  key.Target,
			Message: key.Message,
			Sources: sourceList,
		})
	}

	slices.SortFunc(findings, func(a, b ReviewFinding) int {
		if c := cmp.Compare(a.Target, b.Target); c != 0 {
			return c
		}
		return cmp.Compare(a.Message, b.Message)
	})
	logReviewRefsPhase(l, "grouping and sorting findings", groupingStart,
		log.Int("findingCount", len(findings)),
		log.Int("selectedSourceCount", selectedSourceIds.Len()))

	if onFinding != nil {
		for _, finding := range findings {
			if err := onFinding(finding); err != nil {
				return 0, false, err
			}
		}
	}

	return len(findings), refsTemplateClusterMatch, nil
}

func flattenReviewRefParsers(parserSets ...[]types.RefParser) []types.RefParser {
	total := 0
	for _, parserSet := range parserSets {
		total += len(parserSet)
	}
	if total == 0 {
		return nil
	}
	flattened := make([]types.RefParser, 0, total)
	for _, parserSet := range parserSets {
		flattened = append(flattened, parserSet...)
	}
	return flattened
}

func collectReviewFindings(
	run func(onFinding ReviewFindingCallback) (int, error),
) ([]ReviewFinding, error) {
	findings := []ReviewFinding{}
	_, err := run(func(finding ReviewFinding) error {
		findings = append(findings, finding)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return findings, nil
}

func advanceClusterReviewProgress(l log.Logger, p log.Progress, task log.ProgressTask, step int, detail string) {
	if p != nil {
		if task != nil {
			task.SetDetail(k8s.TruncateFooterDetail(detail))
		}
		p.Advance(step, clusterReviewUnifiedTotal)
		return
	}
	l.Info(logIdCommands, "cluster review post-discovery {step}/{total}: {detail}",
		log.Int("step", step),
		log.Int("total", clusterReviewUnifiedTotal),
		log.String("detail", detail))
}

func logReviewRefsPhase(l log.Logger, phase string, start time.Time, attrs ...log.Attr) {
	attrs = append(attrs, log.Int64("elapsedMs", time.Since(start).Milliseconds()))
	args := make([]any, 0, len(attrs)+1)
	args = append(args, log.String("phase", phase))
	for _, attr := range attrs {
		args = append(args, attr)
	}
	l.DebugLog(logIdCommands, "review refs phase {phase}", args...)
}

func refEdgeKeyForTemplateClusterCompare(r types.Ref) templateClusterRefEdgeKey {
	labels := append([]string(nil), r.Labels...)
	slices.Sort(labels)
	return templateClusterRefEdgeKey{
		From:         r.From,
		To:           r.To,
		RefType:      r.RefType,
		EndpointType: r.EndpointType,
		Reverse:      r.Reverse,
		Labels:       strings.Join(labels, "\x00"),
	}
}

func templateClusterRefKeysEqual(a, b sets.Set[templateClusterRefEdgeKey]) bool {
	if a.Len() != b.Len() {
		return false
	}
	for k := range a {
		if !b.Has(k) {
			return false
		}
	}
	return true
}

// sourceEntitiesWithClusterTemplateBodies returns the same ids and app metadata as sourceEntities,
// replacing KeyTemplateEntity bodies with live KeyClusterEntity copies when ids match.
func sourceEntitiesWithClusterTemplateBodies(
	sourceEntities entity.Entities,
	clusterEntities entity.Entities,
) (entity.Entities, error) {
	byID := make(map[types.Id]entity.Entity, len(clusterEntities.Items))
	for _, c := range clusterEntities.Items {
		id, err := c.Id()
		if err != nil {
			return entity.Entities{}, err
		}
		byID[id] = c
	}
	out := make([]entity.Entity, 0, len(sourceEntities.Items))
	for _, e := range sourceEntities.Items {
		id, err := e.Id()
		if err != nil {
			return entity.Entities{}, err
		}
		cl, ok := byID[id]
		if !ok {
			out = append(out, e)
			continue
		}
		u, ok := cl.Unstructured(types.KeyClusterEntity)
		if !ok {
			out = append(out, e)
			continue
		}
		mod, err := e.Modify(func(b entity.EntityBuilder) entity.EntityBuilder {
			return b.WithUnstructured(types.KeyTemplateEntity, u)
		})
		if err != nil {
			return entity.Entities{}, err
		}
		out = append(out, mod)
	}
	return entity.NewEntities(out)
}

func shouldSkipReviewRef(ref types.Ref) bool {
	if ref.HasTag(types.RefTagOptionalRef) {
		return true
	}
	kind, err := ref.To.Kind()
	if err != nil {
		return false
	}
	return kind == types.KubernetesKindNamespace
}

// qualifiesForSyntheticNamespaceDefaultRef reports whether ref may be satisfied by synthetic
// namespace-default targets (default ServiceAccount or kube-root-ca.crt in the same namespace
// as the source). Cluster-scoped sources and cross-namespace references do not qualify.
func qualifiesForSyntheticNamespaceDefaultRef(source entity.Entity, ref types.Ref) bool {
	kind, err := ref.To.Kind()
	if err != nil {
		return false
	}
	name, err := ref.To.Name()
	if err != nil {
		return false
	}
	toNs, err := ref.To.Namespace()
	if err != nil || toNs == "" {
		return false
	}
	switch kind {
	case types.KubernetesKindServiceAccount:
		if name != types.Name("default") {
			return false
		}
	case types.KubernetesKindConfigMap:
		if name != types.Name("kube-root-ca.crt") {
			return false
		}
	default:
		return false
	}
	if !source.HasNamespace() {
		return false
	}
	srcNs, err := source.Namespace()
	if err != nil || srcNs == "" {
		return false
	}
	return srcNs == toNs
}

// hasIncomingTargetSuppressionRef reports whether any ref points to toId with origin:generated job or
// origin:generated controller (virtual materialized targets). Source refs and target refs are both
// considered so a generator Job or a chart-declared operator edge in the full target index still
// suppresses a missing-target finding.
func hasIncomingTargetSuppressionRef(refs []types.Ref, targetRefs []types.Ref, toId types.Id) bool {
	for _, r := range refs {
		if r.To == toId && r.RefMaterializesVirtualTarget() {
			return true
		}
	}
	for _, r := range targetRefs {
		if r.To == toId && r.RefMaterializesVirtualTarget() {
			return true
		}
	}
	return false
}

func reviewTargetKeys(
	entities entity.Entities,
	key types.EntityKeyUnstructured,
	refs []types.Ref,
) (map[types.Id]sets.Set[string], sets.Set[types.Id], error) {
	targetKeys := map[types.Id]sets.Set[string]{}
	generatedTargets := sets.New[types.Id]()

	for _, item := range entities.Items {
		id, err := item.Id()
		if err != nil {
			return nil, nil, err
		}

		switch gvk, err := item.GVKString(); {
		case err != nil:
			continue
		case gvk == types.KubernetesGvkV1Secret:
			keys := extractSecretKeysForReview(item, key)
			if keys.Len() > 0 || !isVirtualEntityForReview(item, key) {
				targetKeys[id] = keys
			}
		case gvk == types.KubernetesGvkV1ConfigMap:
			keys := extractConfigMapKeysForReview(item, key)
			if keys.Len() > 0 || !isVirtualEntityForReview(item, key) {
				targetKeys[id] = keys
			}
		}
	}

	for _, ref := range refs {
		if !ref.HasGeneratedValue(types.RefGeneratedController) {
			continue
		}
		sourceEntity, ok := entities.EntityMap[ref.From]
		if !ok {
			continue
		}

		sourceMaterializedKeys := materializedSecretKeysForReview(sourceEntity, key)
		if len(sourceMaterializedKeys) == 0 {
			continue
		}

		_, _, _, refNs, secretName, err := ref.To.Components()
		if err != nil {
			continue
		}

		keys, ok := sourceMaterializedKeys[string(secretName)]
		if !ok {
			continue
		}

		resolvedNs := refNs
		if resolvedNs == "" {
			if n, err := sourceEntity.Namespace(); err == nil && n != "" {
				resolvedNs = n
			} else if appNs, err := sourceEntity.AppNamespace(); err == nil && appNs != "" {
				resolvedNs = types.Namespace(appNs)
			}
		}

		targetIds := []types.Id{ref.To}
		if resolvedNs != "" {
			canonical := secretId(resolvedNs, string(secretName))
			if canonical != ref.To && canonical != "" {
				targetIds = append(targetIds, canonical)
			}
		}

		for _, tid := range targetIds {
			generatedTargets.Insert(tid)
			if targetKeys[tid] == nil {
				targetKeys[tid] = sets.New[string]()
			}
			targetKeys[tid].Insert(keys...)
		}
	}

	return targetKeys, generatedTargets, nil
}

func augmentRefsWithExplicitKeyAttributes(
	entities entity.Entities,
	refs []types.Ref,
) ([]types.Ref, error) {
	edgeAttributes, err := collectExplicitKeyAttributes(entities)
	if err != nil {
		return nil, err
	}

	if len(edgeAttributes) == 0 {
		return refs, nil
	}

	foundEdges := sets.New[reviewEdgeKey]()
	updated := make([]types.Ref, 0, len(refs))
	for _, ref := range refs {
		edge := reviewEdgeKey{From: ref.From, To: ref.To}
		if attrs, ok := edgeAttributes[edge]; ok {
			ref.Attributes = mergeRefAttributes(ref.Attributes, attrs.UnsortedList())
			foundEdges.Insert(edge)
		}
		updated = append(updated, ref)
	}

	for edge, attrs := range edgeAttributes {
		if foundEdges.Has(edge) {
			continue
		}
		updated = append(updated, types.Ref{
			RefType:      types.RefTypeDirect,
			EndpointType: types.RefEndpointTypeId,
			From:         edge.From,
			To:           edge.To,
			Attributes:   mergeRefAttributes(nil, attrs.UnsortedList()),
		})
	}

	slices.SortFunc(updated, func(a, b types.Ref) int {
		if c := cmp.Compare(a.From, b.From); c != 0 {
			return c
		}
		if c := cmp.Compare(a.To, b.To); c != 0 {
			return c
		}
		if c := cmp.Compare(a.RefType, b.RefType); c != 0 {
			return c
		}
		if c := cmp.Compare(a.EndpointType, b.EndpointType); c != 0 {
			return c
		}
		return cmp.Compare(fmt.Sprint(a.Attributes), fmt.Sprint(b.Attributes))
	})

	return updated, nil
}

func collectExplicitKeyAttributes(
	entities entity.Entities,
) (map[reviewEdgeKey]sets.Set[types.RefAttribute], error) {
	result := map[reviewEdgeKey]sets.Set[types.RefAttribute]{}

	for _, item := range entities.Items {
		sourceId, err := item.Id()
		if err != nil {
			return nil, err
		}

		u, ok := item.Unstructured(types.KeyTemplateEntity)
		if !ok {
			continue
		}

		podSpec, ok, err := podSpecForReview(item, u)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		ns, err := item.Namespace()
		if err != nil {
			continue
		}

		collectContainerKeyAttributes(result, sourceId, ns, podSpec["containers"])
		collectContainerKeyAttributes(result, sourceId, ns, podSpec["initContainers"])
		collectVolumeKeyAttributes(result, sourceId, ns, podSpec["volumes"])
	}

	return result, nil
}

func podSpecForReview(
	item entity.Entity,
	u unstructured.Unstructured,
) (map[string]any, bool, error) {
	gvk, err := item.GVKString()
	if err != nil {
		return nil, false, err
	}

	switch gvk {
	case types.KubernetesGvkV1Pod:
		spec, found, err := unstructured.NestedMap(u.Object, "spec")
		return spec, found, err
	case types.KubernetesGvkAppsV1Deployment,
		types.KubernetesGvkAppsV1DaemonSet,
		types.KubernetesGvkAppsV1StatefulSet,
		types.KubernetesGvkAppsV1ReplicaSet,
		types.KubernetesGvkV1ReplicationController,
		types.KubernetesGvkBatchV1Job:
		spec, found, err := unstructured.NestedMap(u.Object, "spec", "template", "spec")
		return spec, found, err
	case types.KubernetesGvkBatchV1CronJob:
		spec, found, err := unstructured.NestedMap(u.Object, "spec", "jobTemplate", "spec", "template", "spec")
		return spec, found, err
	default:
		return nil, false, nil
	}
}

func collectContainerKeyAttributes(
	result map[reviewEdgeKey]sets.Set[types.RefAttribute],
	sourceId types.Id,
	ns types.Namespace,
	value any,
) {
	containers, ok := value.([]any)
	if !ok {
		return
	}

	for _, containerValue := range containers {
		container, ok := containerValue.(map[string]any)
		if !ok {
			continue
		}

		envs, ok := container["env"].([]any)
		if !ok {
			continue
		}
		for _, envValue := range envs {
			env, ok := envValue.(map[string]any)
			if !ok {
				continue
			}
			valueFrom, ok := env["valueFrom"].(map[string]any)
			if !ok {
				continue
			}
			if ref, ok := valueFrom["configMapKeyRef"].(map[string]any); ok {
				addNamedKeyAttribute(result, sourceId, configMapId(ns, mapString(ref, "name")), mapString(ref, "key"))
			}
			if ref, ok := valueFrom["secretKeyRef"].(map[string]any); ok {
				addNamedKeyAttribute(result, sourceId, secretId(ns, mapString(ref, "name")), mapString(ref, "key"))
			}
		}
	}
}

func collectVolumeKeyAttributes(
	result map[reviewEdgeKey]sets.Set[types.RefAttribute],
	sourceId types.Id,
	ns types.Namespace,
	value any,
) {
	volumes, ok := value.([]any)
	if !ok {
		return
	}

	for _, volumeValue := range volumes {
		volume, ok := volumeValue.(map[string]any)
		if !ok {
			continue
		}

		if configMap, ok := volume["configMap"].(map[string]any); ok {
			target := configMapId(ns, mapString(configMap, "name"))
			for _, key := range itemKeysFromConfigSource(configMap) {
				addNamedKeyAttribute(result, sourceId, target, key)
			}
		}

		if secret, ok := volume["secret"].(map[string]any); ok {
			target := secretId(ns, mapString(secret, "secretName"))
			for _, key := range itemKeysFromConfigSource(secret) {
				addNamedKeyAttribute(result, sourceId, target, key)
			}
		}

		projected, ok := volume["projected"].(map[string]any)
		if !ok {
			continue
		}

		sources, ok := projected["sources"].([]any)
		if !ok {
			continue
		}
		for _, sourceValue := range sources {
			source, ok := sourceValue.(map[string]any)
			if !ok {
				continue
			}
			if configMap, ok := source["configMap"].(map[string]any); ok {
				target := configMapId(ns, mapString(configMap, "name"))
				for _, key := range itemKeysFromConfigSource(configMap) {
					addNamedKeyAttribute(result, sourceId, target, key)
				}
			}
			if secret, ok := source["secret"].(map[string]any); ok {
				target := secretId(ns, mapString(secret, "name"))
				for _, key := range itemKeysFromConfigSource(secret) {
					addNamedKeyAttribute(result, sourceId, target, key)
				}
			}
		}
	}
}

func itemKeysFromConfigSource(source map[string]any) []string {
	items, ok := source["items"].([]any)
	if !ok {
		return nil
	}

	result := sets.New[string]()
	for _, itemValue := range items {
		item, ok := itemValue.(map[string]any)
		if !ok {
			continue
		}
		key := mapString(item, "key")
		if key != "" {
			result.Insert(key)
		}
	}

	keys := result.UnsortedList()
	slices.Sort(keys)
	return keys
}

func addNamedKeyAttribute(
	result map[reviewEdgeKey]sets.Set[types.RefAttribute],
	sourceId types.Id,
	targetId types.Id,
	key string,
) {
	if targetId == "" || key == "" {
		return
	}
	edge := reviewEdgeKey{From: sourceId, To: targetId}
	if result[edge] == nil {
		result[edge] = sets.New[types.RefAttribute]()
	}
	result[edge].Insert(types.RefAttribute{Type: "key", Value: key})
}

func mergeRefAttributes(existing []types.RefAttribute, additional []types.RefAttribute) []types.RefAttribute {
	merged := sets.New[types.RefAttribute]()
	merged.Insert(existing...)
	merged.Insert(additional...)
	result := merged.UnsortedList()
	slices.SortFunc(result, func(a, b types.RefAttribute) int {
		if c := cmp.Compare(a.Type, b.Type); c != 0 {
			return c
		}
		return cmp.Compare(a.Value, b.Value)
	})
	return result
}

func refAttributesByType(attributes []types.RefAttribute, attrType string) []string {
	keys := sets.New[string]()
	for _, attr := range attributes {
		if attr.Type == attrType && attr.Value != "" {
			keys.Insert(attr.Value)
		}
	}
	result := keys.UnsortedList()
	slices.Sort(result)
	return result
}

func extractSecretKeysForReview(item entity.Entity, key types.EntityKeyUnstructured) sets.Set[string] {
	result := sets.New[string]()
	u, ok := item.Unstructured(key)
	if !ok {
		return result
	}

	if data, found, err := unstructured.NestedMap(u.Object, "data"); err == nil && found {
		for key := range data {
			result.Insert(key)
		}
	}
	if stringData, found, err := unstructured.NestedMap(u.Object, "stringData"); err == nil && found {
		for key := range stringData {
			result.Insert(key)
		}
	}
	return result
}

func extractConfigMapKeysForReview(item entity.Entity, key types.EntityKeyUnstructured) sets.Set[string] {
	result := sets.New[string]()
	u, ok := item.Unstructured(key)
	if !ok {
		return result
	}

	if data, found, err := unstructured.NestedMap(u.Object, "data"); err == nil && found {
		for key := range data {
			result.Insert(key)
		}
	}
	if binaryData, found, err := unstructured.NestedMap(u.Object, "binaryData"); err == nil && found {
		for key := range binaryData {
			result.Insert(key)
		}
	}
	return result
}

func isVirtualEntityForReview(item entity.Entity, key types.EntityKeyUnstructured) bool {
	u, ok := item.Unstructured(key)
	if !ok {
		return false
	}
	annotations, found, err := unstructured.NestedStringMap(u.Object, "metadata", "annotations")
	if err != nil || !found {
		return false
	}
	return annotations["hydra-gitops.org/hydra/virtual"] == "true"
}

// extractSecretTemplateKeysForReview reads spec.secretTemplates[*] name and key names from any
// entity that defines that shape (e.g. SopsSecret CRs). Used with origin:generated controller refs.
func extractSecretTemplateKeysForReview(item entity.Entity, key types.EntityKeyUnstructured) map[string][]string {
	u, ok := item.Unstructured(key)
	if !ok {
		return nil
	}

	templates, found, err := unstructured.NestedSlice(u.Object, "spec", "secretTemplates")
	if err != nil || !found {
		return nil
	}

	result := map[string][]string{}
	for _, templateValue := range templates {
		template, ok := templateValue.(map[string]any)
		if !ok {
			continue
		}
		name := mapString(template, "name")
		if name == "" {
			continue
		}

		keys := sets.New[string]()
		if data, ok := template["data"].(map[string]any); ok {
			for key := range data {
				keys.Insert(key)
			}
		}
		if stringData, ok := template["stringData"].(map[string]any); ok {
			for key := range stringData {
				keys.Insert(key)
			}
		}
		if len(keys) == 0 {
			continue
		}

		sortedKeys := keys.UnsortedList()
		slices.Sort(sortedKeys)
		result[name] = sortedKeys
	}

	return result
}

func materializedSecretKeysForReview(item entity.Entity, key types.EntityKeyUnstructured) map[string][]string {
	if templateKeys := extractSecretTemplateKeysForReview(item, key); len(templateKeys) > 0 {
		return templateKeys
	}
	gvk, err := item.GVKString()
	if err != nil || gvk != types.KubernetesGvkV1Secret {
		return nil
	}
	name, err := item.Name()
	if err != nil || name == "" {
		return nil
	}
	keys := extractSecretKeysForReview(item, key)
	if keys.Len() == 0 {
		return nil
	}
	result := map[string][]string{
		string(name): keys.UnsortedList(),
	}
	slices.Sort(result[string(name)])
	return result
}

func configMapId(ns types.Namespace, name string) types.Id {
	if name == "" {
		return ""
	}
	return types.Id(fmt.Sprintf("v1/ConfigMap/%s/%s", ns, name))
}

func secretId(ns types.Namespace, name string) types.Id {
	if name == "" {
		return ""
	}
	return types.Id(fmt.Sprintf("v1/Secret/%s/%s", ns, name))
}

func mapString(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok {
		return ""
	}
	text, _ := raw.(string)
	return text
}
