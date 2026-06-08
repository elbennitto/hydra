package action

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/k8s"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Footer labels (English, concise for narrow terminals).
const (
	uninstallFooterInventoryOwnership = "uninstall · inventory scope & ownership"
	uninstallFooterPlanPrepare        = "uninstall · plan (prepare)"
	uninstallFooterMergeManifest      = "uninstall · merge manifest vs cluster"
	uninstallFooterPlanFinalize       = "uninstall · plan (finalize)"
	uninstallFooterLeftoverPrep       = "uninstall · leftover prep (diff & owners)"
	uninstallFooterLeftoverScan       = "uninstall · leftover dependency scan"
	uninstallFooterLeftoverMergeOwned = "uninstall · merge owned leftovers into plan"
	uninstallFooterLeftoverMergeOrph  = "uninstall · merge orphan rows into plan"
)

// uninstallLeftoverPrepPhases is footer steps before scanning each leftover row (set difference,
// UID maps, transitive owner walk — often the slow part).
const uninstallLeftoverPrepPhases = 4

// uninstallPlanPhaseSteps is the number of footer steps from opening the prepare-plan bar through
// workload leftover handling (through the colored diff summary). Must match advancePlan calls.
const uninstallPlanPhaseSteps = 5

// uninstallPlanTailSteps is the plan bar after the colored diff summary: force leftovers,
// persistent volumes, and remove finalizers.
const uninstallPlanTailSteps = 3

// mergeProgressMirror forwards merge footer progress to mpb and mirrors the same activity onto the
// plan (prepare) subtitle when mirror is non-nil — including when terminal bars are disabled but the
// subtitle should still show fraction + current id.
type mergeProgressMirror struct {
	inner   log.Progress
	mirror  func(string)
	phase   string
	total   int
	lastIdx int
	lastTot int
}

func newMergeProgressMirror(inner log.Progress, total int, phase string, mirror func(string)) log.Progress {
	if inner == nil && mirror == nil {
		return nil
	}
	return &mergeProgressMirror{
		inner:   inner,
		mirror:  mirror,
		phase:   phase,
		total:   total,
		lastTot: total,
	}
}

func (m *mergeProgressMirror) Advance(index, total int) {
	m.lastIdx = index
	if total > 0 {
		m.lastTot = total
	}
	if m.inner != nil {
		m.inner.Advance(index, total)
	}
	m.syncMirror("")
}

func (m *mergeProgressMirror) syncMirror(detail string) {
	if m.mirror == nil {
		return
	}
	tot := m.lastTot
	if tot < 1 {
		tot = m.total
	}
	if detail != "" {
		detail = k8s.TruncateFooterDetail(detail)
		cur := m.lastIdx + 1
		if tot > 0 && cur > tot {
			cur = tot
		}
		m.mirror(fmt.Sprintf("leftovers %s · %d / %d · %s", m.phase, cur, tot, detail))
		return
	}
	m.mirror(fmt.Sprintf("leftovers %s · %d / %d", m.phase, m.lastIdx, m.lastTot))
}

func (m *mergeProgressMirror) Close() error {
	if m.inner != nil {
		return m.inner.Close()
	}
	return nil
}

func (m *mergeProgressMirror) NewTask(name string) log.ProgressTask {
	var inner log.ProgressTask
	if m.inner != nil {
		inner = m.inner.NewTask(name)
	}
	return &mergeProgressMirrorTask{parent: m, inner: inner}
}

type mergeProgressMirrorTask struct {
	parent *mergeProgressMirror
	inner  log.ProgressTask
}

func (t *mergeProgressMirrorTask) SetDetail(detail string) {
	if t.inner != nil {
		t.inner.SetDetail(detail)
	}
	t.parent.syncMirror(detail)
}

func (t *mergeProgressMirrorTask) Close() error {
	if t.inner != nil {
		return t.inner.Close()
	}
	return nil
}

type ClusterUninstallFlags struct {
	flags.ClusterRESTClientFlags
	flags.HelmNetworkModeFlag
	flags.ContextFlag
	flags.ColorFlag
	flags.DryRunFlag
	flags.NoClusterFlag
	flags.KubernetesVersionFlag
	flags.ForceUninstallFlag
	flags.ForceScaleDownFlag
	flags.ScaleTimeoutFlag
	flags.SkipBackupFlag
	flags.ExcludeAppFlag
	flags.BootstrapFlag
	flags.NoCacheFlag
	flags.ClusterListParallelFlag
	AppIdPatterns []types.AppIdPattern
}

var _ flags.WithBootstrapFlag = (*ClusterUninstallFlags)(nil)
var _ flags.WithClusterListParallelFlag = (*ClusterUninstallFlags)(nil)

func (f *ClusterUninstallFlags) WithBootstrapFlag() *flags.BootstrapFlag {
	return &f.BootstrapFlag
}

func (f *ClusterUninstallFlags) WithClusterListParallelFlag() *flags.ClusterListParallelFlag {
	return &f.ClusterListParallelFlag
}

func (f *ClusterUninstallFlags) Flags() flags.Flags {
	return f
}

// ClusterUninstall uninstalls selected app(s)
func ClusterUninstall(f ClusterUninstallFlags) error {
	l := log.Default()
	config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedYes)

	appIds, err := commands.ResolveAppIdsFromConfig(l, f.HydraContext, config, f.AppIdPatterns, f.ExcludeAppPatterns, f.HelmNetworkMode, false)
	if err != nil {
		return err
	}

	if len(appIds) == 0 {
		return log.CreateError(errors.ErrNoAppsSpecified, "no apps specified for uninstallation")
	}

	if f.Parallel < 0 {
		return fmt.Errorf("--parallel must not be negative")
	}
	if f.Parallel > 64 {
		return fmt.Errorf("--parallel must be at most 64")
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

	if !f.SkipBackup && !f.NoCluster {
		l.Info(logIdAction, "creating automatic backup before uninstallation (use --skip-backup to skip)")
		appIdSlice := make([]types.AppId, 0, len(appIds))
		for appId := range appIds {
			appIdSlice = append(appIdSlice, appId)
		}
		results, backupErr := commands.BackupCreateWithOptions(cluster, appIdSlice, f.HelmNetworkMode, f.Color, f.DryRun, commands.BackupCreateOptions{
			SkipFoundDefinitionsInfoLog: true,
		})
		if err := errAutomaticPreUninstallBackup(backupErr); err != nil {
			return err
		}
		commands.PrintBackupResults(l, results, f.Color)
	}

	// all resources rendered for the selected apps
	renderedEntities, namespaces, allAppIds, err := commands.RenderCluster(cluster, appIds, f.KubernetesVersion, types.CrdModeSilent, types.SkipRootAppsNo, nil)
	if err != nil {
		return err
	}

	renderedAllApps, err := commands.RenderClusterSelectedApps(cluster, types.HelmNetworkModeOffline, "", allAppIds, types.KeyTemplateEntity,
		commands.WithSkipFoundDefinitionsInfoLog())
	if err != nil {
		return err
	}
	scopeFromCluster := func() (types.ScopeInfoMap, error) {
		return commands.ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity, types.CrdModeSilent)
	}
	renderedAllApps, err = commands.NormalizeApiVersions(l, renderedAllApps, types.KeyTemplateEntity, cluster, scopeFromCluster)
	if err != nil {
		return err
	}
	preExpandAllApps := renderedAllApps
	renderedAllApps, err = commands.ExpandSopsSecretsForUninstall(cluster.L(), renderedAllApps, types.KeyTemplateEntity)
	if err != nil {
		return err
	}
	renderedEntities, err = commands.AppendDerivedSopsSecretsForUninstall(
		renderedEntities, preExpandAllApps, renderedAllApps, types.KeyTemplateEntity)
	if err != nil {
		return err
	}
	beforeClones := renderedAllApps
	renderedAllApps, err = commands.ExpandClonesForUninstall(
		cluster.L(), cluster, allAppIds, f.HelmNetworkMode, renderedAllApps, types.KeyTemplateEntity, f.Bootstrap)
	if err != nil {
		return err
	}
	cloneOnly, err := commands.DiffEntities(beforeClones, renderedAllApps)
	if err != nil {
		return err
	}
	renderedEntities, err = commands.MergeRenderedWithClones(renderedEntities, cloneOnly)
	if err != nil {
		return err
	}

	if f.NoCluster {
		l.Info(logIdAction, "no-cluster mode: rendered {entities} entities for uninstallation — skipping cluster operations",
			log.Int("entities", renderedEntities.Len()))
		return nil
	}

	perAppRendered, err := commands.PartitionTemplateEntitiesByPrimaryApp(preExpandAllApps)
	if err != nil {
		return err
	}

	// all resources currently present in the cluster
	clusterEntities, err := commands.ListClusterAll(cluster, types.KeyClusterEntity, log.TerminalProgressUI(), f.Parallel)
	if err != nil {
		return err
	}

	var planBar log.Progress
	var planDetailTask log.ProgressTask
	planStep := 0
	planBarTotal := uninstallPlanPhaseSteps
	advancePlan := func(detail string) {
		if planBar == nil {
			return
		}
		planStep++
		if planDetailTask != nil {
			planDetailTask.SetDetail(detail)
		}
		planBar.Advance(planStep, planBarTotal)
	}
	defer func() {
		if planBar != nil {
			_ = planBar.Close()
		}
	}()

	// Namespaces for leftover / uninstall-safe / uninstall-force scans: exclusive namespaces plus
	// every namespace the selected apps render into (includes shared namespaces such as demo).
	leftoverNamespaces := commands.UninstallLeftoverNamespaces(namespaces, renderedEntities)
	logUninstallNamespaces(l, leftoverNamespaces)

	var filterNamespacesBar log.Progress
	if log.TerminalProgressUI() {
		filterNamespacesBar, err = l.NewProgress(uninstallFooterInventoryOwnership, 1)
		if err != nil {
			return err
		}
	}
	if filterNamespacesBar != nil {
		filterNamespacesBar.Advance(1, 1)
	}
	model, err := commands.BuildResourceModel(commands.ResourceModelInput{
		Cluster:                cluster,
		NetworkMode:            types.HelmNetworkModeOffline,
		Bootstrap:              f.Bootstrap,
		TemplateEntities:       &renderedAllApps,
		ClusterEntities:        &clusterEntities,
		PerAppTemplateEntities: perAppRendered,
		AppIds:                 allAppIds,
		PredicateAppIds:        appIds,
		Parallel:               f.Parallel,
	}, log.TerminalProgressUI())
	if filterNamespacesBar != nil {
		_ = filterNamespacesBar.Close()
	}
	if err != nil {
		return err
	}
	selectedAssignedIds := selectedAssignedClusterIds(model, appIds)
	workloadScopedLive, _, err := commands.LiveEntitiesInHydraWorkloadScope(
		clusterEntities,
		leftoverNamespaces,
		renderedAllApps,
		cluster,
		appIds,
		renderedAllApps,
		types.HelmNetworkModeOffline,
	)
	if err != nil {
		return err
	}
	selectedWorkloadScopedLive, err := filterSelectedWorkloadScopedLive(workloadScopedLive, selectedAssignedIds)
	if err != nil {
		return err
	}
	if log.TerminalProgressUI() {
		planBar, err = l.NewProgress(uninstallFooterPlanPrepare, uninstallPlanPhaseSteps)
		if err != nil {
			return err
		}
		planDetailTask = planBar.NewTask("")
	}
	advancePlan("resolve app ownership")

	templateByNs := commands.TemplateAppsByNamespace(perAppRendered)
	stakeholders, err := commands.StakeholderAppsByNamespaceFromResourceRows(templateByNs, model.RowsForIDs(workloadScopedLive.IdSet))
	if err != nil {
		return err
	}
	safeNamespaces := commands.IntersectNamespaces(
		commands.NamespacesAllowingUninstallSafe(stakeholders, appIds),
		leftoverNamespaces)
	advancePlan("derive uninstall-safe namespaces")

	// Hide the plan bar's detail line while merging so the dedicated merge footer bar is visibly the second
	// progress row (otherwise "merge entities" reads like a subtitle of the plan bar only).
	if planDetailTask != nil {
		_ = planDetailTask.Close()
		planDetailTask = nil
	}
	nMerge := entity.EntityMapIds(renderedEntities.EntityMap, clusterEntities.EntityMap).Len()
	mergeTotal := nMerge
	if mergeTotal < 1 {
		mergeTotal = 1
	}
	var mergeEntitiesBar log.Progress
	if log.TerminalProgressUI() {
		// Open the merge bar before advancing the plan to step 3 so two footer rows are present for the whole phase.
		mergeEntitiesBar, err = l.NewProgress(uninstallFooterMergeManifest, mergeTotal)
		if err != nil {
			if planBar != nil {
				planDetailTask = planBar.NewTask("")
			}
			return err
		}
	}
	advancePlan("merge manifest vs cluster rows")
	entities, err := renderedEntities.MergeWithProgress(
		clusterEntities,
		mergeEntitiesBar,
		f.Parallel,
		types.KeyTemplateEntity,
		types.KeyClusterEntity)
	if mergeEntitiesBar != nil {
		_ = mergeEntitiesBar.Close()
	}
	if planBar != nil && planDetailTask == nil {
		planDetailTask = planBar.NewTask("")
	}
	if err != nil {
		return err
	}

	entities, err = selectUninstallStuff(cluster, entities, appIds, allAppIds, safeNamespaces, leftoverNamespaces, selectedAssignedIds, renderedEntities, renderedAllApps)
	if err != nil {
		return err
	}
	advancePlan("apply uninstall / backup / Argo selections")

	advancePlan("scan inherited workload leftovers")
	entities, err = handleLeftovers(cluster.L(), selectedWorkloadScopedLive, entities, func(phase string) {
		if planDetailTask != nil {
			planDetailTask.SetDetail(phase)
		}
	}, f.Parallel)
	if err != nil {
		return err
	}

	// Drop the footer while printing the large colored diff: mixing stdout printf, slog, and mpb
	// redraws garbles terminals; reopen a short tail bar for the remaining plan steps.
	log.FlushProgressForStdout()
	if planBar != nil {
		_ = planBar.Close()
		planBar = nil
	}
	planDetailTask = nil

	// Show the regular uninstall plan (Hydra-selected resources) before any force-deletable /
	// untracked leftover messaging from [handleForceLeftovers].
	var pendingNothingTodo error
	if err := ColoredUninstallMessage(cluster.L(), f.Color, entities, clusterEntities,
		leftoverNamespaces, cluster.ClusterName, types.KeyTemplateEntity, types.KeyClusterEntity); err != nil {
		if !errors.ErrNothingTodo.MatchesError(err) {
			return err
		}
		pendingNothingTodo = err
	}

	planBarTotal = uninstallPlanTailSteps
	planStep = 0
	if log.TerminalProgressUI() {
		planBar, err = l.NewProgress(uninstallFooterPlanFinalize, uninstallPlanTailSteps)
		if err != nil {
			return err
		}
		planDetailTask = planBar.NewTask("")
	}

	entities, err = handleForceLeftovers(cluster, cluster.L(), clusterEntities, entities,
		selectedWorkloadScopedLive, leftoverNamespaces, appIds, allAppIds, perAppRendered, renderedAllApps, f)
	if err != nil {
		return err
	}
	advancePlan("merge force-delete leftovers")

	entities, err = commands.MergePersistentVolumesBoundToUninstallClaims(
		cluster, entities, clusterEntities, renderedAllApps, types.HelmNetworkModeOffline)
	if err != nil {
		return err
	}
	advancePlan("attach bound persistent volumes")

	webhookEntities, entitiesSansWebhooks, err := commands.SplitWebhooks(entities)
	if err != nil {
		return err
	}
	webhookPhaseAlreadyDone := false
	webhookDeleted := 0
	finalizerPatches := 0
	if webhookEntities.Len() > 0 {
		var wdel int
		var werr error
		wdel, werr = commands.DeleteAdmissionWebhookConfigurations(cluster, webhookEntities, 1, uninstallPlanTailSteps, log.TerminalProgressUI())
		if werr != nil {
			return werr
		}
		webhookPhaseAlreadyDone = true
		webhookDeleted = wdel
	}
	entities = entitiesSansWebhooks

	finalizerPatches, err = commands.RemoveUninstallFinalizers(cluster, clusterEntities, appIds)
	if err != nil {
		return err
	}
	advancePlan("strip uninstall finalizers")

	if planBar != nil {
		_ = planBar.Close()
		l.Info(logIdAction, "{plan}: completed {count} step(s)",
			log.String("plan", uninstallFooterPlanFinalize),
			log.Int("count", uninstallPlanTailSteps))
		planBar = nil
	}

	// ignore entities only on the left side (remove template only entities that are missing in the cluster)
	_, entities, err = entities.SelectByContainsEntityKey(types.KeyClusterEntity)
	if err != nil {
		return err
	}
	entities, err = skipProtectedNamespaceDeletes(cluster.L(), entities, clusterEntities)
	if err != nil {
		return err
	}
	if entities.Len() == 0 {
		return finishClusterUninstallAfterEarlyCleanup(cluster.L(), pendingNothingTodo, webhookDeleted, finalizerPatches)
	}

	return commands.DeleteResources(cluster, entities, types.KeyClusterEntity, f.ForceScaleDown, f.ScaleTimeout, 1, 3, log.TerminalProgressUI(), webhookPhaseAlreadyDone)
}

func finishClusterUninstallAfterEarlyCleanup(l log.Logger, pendingNothingTodo error, webhookDeleted, finalizerPatches int) error {
	if webhookDeleted > 0 || finalizerPatches > 0 {
		switch {
		case webhookDeleted > 0 && finalizerPatches > 0:
			l.Info(logIdAction, "Uninstall completed; deleted {webhooks} admission webhook object(s) and patched {patches} cluster object(s) to remove uninstall finalizer(s).",
				log.Int("webhooks", webhookDeleted),
				log.Int("patches", finalizerPatches))
		case webhookDeleted > 0:
			l.Info(logIdAction, "Uninstall completed; deleted {count} admission webhook object(s).",
				log.Int("count", webhookDeleted))
		default:
			l.Info(logIdAction, "Uninstall completed; patched {count} cluster object(s) to remove uninstall finalizer(s).",
				log.Int("count", finalizerPatches))
		}
		return nil
	}
	if pendingNothingTodo != nil {
		return pendingNothingTodo
	}
	return log.CreateInfo(errors.ErrNothingTodo, "Found no resources to uninstall.")
}

// errAutomaticPreUninstallBackup turns a failed pre-uninstall backup into ErrAborted so the
// uninstall does not continue without a successful backup (unless the user passed --skip-backup).
func errAutomaticPreUninstallBackup(backupErr error) error {
	if backupErr == nil {
		return nil
	}
	return log.CreateError(errors.ErrAborted, "aborted: automatic backup before uninstallation failed: {err}", log.String("err", backupErr.Error()))
}

func ColoredUninstallMessage(
	l log.Logger,
	color types.Color,
	entities entity.Entities,
	clusterEntities entity.Entities,
	namespaces sets.Set[types.Namespace],
	clusterName types.ClusterName,
	leftKey types.EntityKeyUnstructured,
	rightKey types.EntityKeyUnstructured,
) error {
	l.DebugLog(logIdAction, "calculating diff between rendered and found resources")
	compareResult, err := entities.Compare(leftKey, rightKey)
	if err != nil {
		return err
	}

	l.Info(logIdAction, "Diff: rendered app manifests vs live cluster (Hydra-managed uninstall preview; not every later cleanup object appears here).")

	var colorMessage = ""
	var colorKey = ""
	var colorReset = ""
	var colorAlreadyUninstalled = ""
	var colorToBeUninstalled = ""

	if color {
		colorMessage = colors.LightWhite.String()
		colorKey = colors.LightMagenta.String()
		colorReset = colors.Reset.String()
		colorAlreadyUninstalled = colors.Green.String()
		colorToBeUninstalled = colors.LightRed.String()
	}

	if compareResult.LeftOnly.Len() == 0 {
		l.Info(logIdAction, "Found no resources in rendered manifests that are absent from the cluster (nothing rendered-only missing live).")
	} else {
		l.Info(logIdAction, "Found {missing} resource(s) in rendered manifests but not in the cluster (already removed or never applied):",
			log.Int("missing", compareResult.LeftOnly.Len()))
		for _, e := range compareResult.LeftOnly.Items {
			id, err := e.Id()
			if err != nil {
				return err
			}
			line := fmt.Sprintf("%s  * %s%s%s:%s already uninstalled %s✓%s",
				colorMessage, colorKey, string(id), colorMessage, colorAlreadyUninstalled, colorMessage, colorReset)
			l.Info(logIdAction, "{line}", log.String("line", line))
		}
	}

	if compareResult.RightOnly.Len() == 0 {
		l.Info(logIdAction, "Found no live cluster resources that are absent from rendered manifests (no extra cluster-only rows in this diff).")
	} else {
		l.Info(logIdAction, "Found {missing} live cluster resource(s) not represented in rendered app manifests (managed by apps at runtime in the cluster):",
			log.Int("missing", compareResult.RightOnly.Len()))
		for _, e := range compareResult.RightOnly.Items {
			id, err := e.Id()
			if err != nil {
				return err
			}
			line := fmt.Sprintf("%s  * %s%s%s:%s will be uninstalled %s☑%s",
				colorMessage, colorKey, string(id), colorMessage, colorToBeUninstalled, colorMessage, colorReset)
			l.Info(logIdAction, "{line}", log.String("line", line))
		}
	}

	if compareResult.Both.Len() == 0 {
		l.Info(logIdAction, "Found no resources that appear in both rendered manifests and the cluster for this uninstall scope.")
	} else {
		l.Info(logIdAction, "Found {both} resource(s) present in both rendered manifests and the cluster (will be reconciled / removed as part of uninstall):",
			log.Int("both", compareResult.Both.Len()))
		for _, e := range compareResult.Both.Items {
			id, err := e.Id()
			if err != nil {
				return err
			}
			line := fmt.Sprintf("%s  * %s%s%s:%s will be uninstalled %s☑%s",
				colorMessage, colorKey, string(id), colorMessage, colorToBeUninstalled, colorMessage, colorReset)
			l.Info(logIdAction, "{line}", log.String("line", line))
		}
	}

	if compareResult.RightOnly.Len() == 0 && compareResult.Both.Len() == 0 {
		return log.CreateInfo(errors.ErrNothingTodo, "Found no resources to uninstall.")
	} else {
		l.Info(logIdAction, "Result of manifest vs cluster diff: {resources} resource(s) in cluster {cluster} are in uninstall scope.",
			log.Int("resources", compareResult.RightOnly.Len()+compareResult.Both.Len()),
			log.String("cluster", string(clusterName)))
	}

	return nil
}

func resolveForceLeftovers(
	l log.Logger,
	forceLeftovers entity.Entities,
	untrackedLeftovers entity.Entities,
	forceUninstall types.ForceUninstall,
	uninstalls entity.Entities,
) (entity.Entities, error) {
	if untrackedLeftovers.Len() > 0 {
		if forceUninstall == types.ForceUninstallForceAll {
			if forceLeftovers.Len() > 0 {
				l.Warn(logIdAction,
					"With --force-all: merging {count} workload leftover(s) into the delete plan. They match uninstall-force references to the selected app(s) but were not in the primary uninstall manifest list:",
					log.Int("count", forceLeftovers.Len()))
				commands.LogEntityIds(l, forceLeftovers)
			}
			merged, err := uninstalls.Merge(forceLeftovers, types.KeyClusterEntity)
			if err != nil {
				return entity.Entities{}, err
			}
			return merged.Merge(untrackedLeftovers, types.KeyClusterEntity)
		}

		planned, err := mergePlannedUninstallForAbort(uninstalls, forceLeftovers, untrackedLeftovers)
		if err != nil {
			return entity.Entities{}, err
		}
		totalPreview := plannedClusterKeyedCount(planned)
		untrackedN := untrackedLeftovers.Len()
		mappedN := totalPreview - untrackedN
		if mappedN < 0 {
			mappedN = 0
		}
		l.Info(logIdAction,
			"Workload leftovers: objects still exist in uninstall namespaces. Without --force-all this run stops here. If you pass --force-all, Hydra would delete {total} cluster object(s) in one merged scope: {mapped} from normal uninstall / force-delete classification plus {untracked} that are not linked to any app (listed under the error below).",
			log.Int("total", totalPreview),
			log.Int("mapped", mappedN),
			log.Int("untracked", untrackedN))
		logPlannedDeletionSummaryForAbort(l, "Total cluster objects in merged --force-all scope", planned)

		if forceLeftovers.Len() > 0 {
			l.Warn(logIdAction,
				"Force-deletable leftovers: {count} object(s) are linked to an app via uninstall-force rules (not the same as the unlinked objects in the error below):",
				log.Int("count", forceLeftovers.Len()))
			commands.LogEntityIds(l, forceLeftovers)
		}

		logPlannedDeletionBreakdownForAbort(l, planned)

		l.Error(logIdAction,
			"{count} cluster object(s) in uninstall namespaces cannot be linked to any selected Hydra app using priority >= 0 uninstall ownership rules (uninstall / uninstall-force / backup refs plus clone rules; often API noise such as Events or metrics reads). They are skipped on this run unless you use --force-all:",
			log.Int("count", untrackedLeftovers.Len()))
		commands.LogEntityErrorIds(l, untrackedLeftovers)

		return entity.Entities{}, log.CreateError(errors.ErrAborted,
			"uninstall aborted: {untracked} cluster object(s) cannot be assigned to a selected app via priority >= 0 uninstall ownership rules (uninstall / uninstall-force / backup refs plus clone rules). Re-run with --force-all to delete them together with the selected uninstall, or adjust force / namespace options.",
			log.Int("untracked", untrackedLeftovers.Len()))
	}

	if forceLeftovers.Len() > 0 {
		l.Warn(logIdAction,
			"Workload leftovers in uninstall namespaces: {count} cluster object(s) match priority >= 0 uninstall ownership rules for the selected uninstall but are outside the main uninstall list. How they are handled depends on --force / --keep / --force-all:",
			log.Int("count", forceLeftovers.Len()))
		commands.LogEntityIds(l, forceLeftovers)

		switch forceUninstall {
		case types.ForceUninstallForce, types.ForceUninstallForceAll:
			return uninstalls.Merge(forceLeftovers, types.KeyClusterEntity)
		case types.ForceUninstallKeep:
			return removeNamespacesOfKeptResources(l, uninstalls, forceLeftovers)
		default:
			planned, err := mergePlannedUninstallForAbort(uninstalls, forceLeftovers, entity.Entities{})
			if err != nil {
				return entity.Entities{}, err
			}
			if err := logPlannedForceDeletionSummaryForAbort(l, uninstalls, forceLeftovers, planned); err != nil {
				return entity.Entities{}, err
			}
			logPlannedDeletionBreakdownForAbort(l, planned)

			return entity.Entities{}, log.CreateError(errors.ErrAborted,
				"aborted: force-deletable resources found, use --force to delete, --keep to keep them, or --force-all to delete all leftovers")
		}
	}

	return uninstalls, nil
}

func removeNamespacesOfKeptResources(l log.Logger, uninstalls entity.Entities, keptResources entity.Entities) (entity.Entities, error) {
	keptNamespaces := sets.New[types.Namespace]()
	for _, e := range keptResources.Items {
		ns, err := e.Namespace()
		if err != nil || ns == "" {
			continue
		}
		keptNamespaces.Insert(ns)
	}

	if keptNamespaces.Len() == 0 {
		return uninstalls, nil
	}

	filtered := []entity.Entity{}
	for _, e := range uninstalls.Items {
		gvk, err := e.GVKString()
		if err != nil {
			return entity.Entities{}, err
		}
		if gvk == types.KubernetesGvkV1Namespace {
			name, err := e.Name()
			if err != nil {
				return entity.Entities{}, err
			}
			if keptNamespaces.Has(types.Namespace(name)) {
				l.Info(logIdAction, "keeping namespace {namespace} because it contains kept resources",
					log.String("namespace", string(name)))
				continue
			}
		}
		filtered = append(filtered, e)
	}

	return entity.NewEntities(filtered)
}

// mergePlannedUninstallForAbort combines Hydra uninstall targets with force-deletable and (when present)
// untracked leftovers so the user can see the full deletion plan before an abort.
func mergePlannedUninstallForAbort(
	uninstalls, forceLeftovers, untrackedLeftovers entity.Entities,
) (entity.Entities, error) {
	merged, err := uninstalls.Merge(forceLeftovers, types.KeyClusterEntity)
	if err != nil {
		return entity.Entities{}, err
	}
	if untrackedLeftovers.Len() == 0 {
		return merged, nil
	}
	return merged.Merge(untrackedLeftovers, types.KeyClusterEntity)
}

func logPlannedDeletionSummaryForAbort(l log.Logger, label string, planned entity.Entities) {
	n := plannedClusterKeyedCount(planned)
	if n == 0 {
		return
	}
	l.Info(logIdAction, "{label}: {count} resources",
		log.String("label", label),
		log.Int("count", n))
}

// plannedClusterKeyedCount is how many planned uninstall rows represent a live cluster object.
// Merged plan slices can include template-only rows without KeyClusterEntity; those are omitted
// from user-facing uninstall counts and breakdowns.
func plannedClusterKeyedCount(planned entity.Entities) int {
	var n int
	for _, e := range planned.Items {
		if e.HasKey(types.KeyClusterEntity) {
			n++
		}
	}
	return n
}

// plannedUninstallForceSplit classifies each cluster-keyed planned row after merge(uninstalls, forceLeftovers).
// Ids present in both maps count toward uninstall (same as merge treating the Hydra uninstall side as primary).
func plannedUninstallForceSplit(
	planned, uninstalls, forceLeftovers entity.Entities,
) (uninstallN, forceN, totalN int, err error) {
	for _, e := range planned.Items {
		if !e.HasKey(types.KeyClusterEntity) {
			continue
		}
		id, idErr := e.Id()
		if idErr != nil {
			return 0, 0, 0, idErr
		}
		_, inU := uninstalls.EntityMap[id]
		_, inF := forceLeftovers.EntityMap[id]
		if !inU && !inF {
			return 0, 0, 0, fmt.Errorf("planned uninstall id %s is neither in uninstall nor force-leftover maps", id)
		}
		totalN++
		if inF && !inU {
			forceN++
		} else {
			uninstallN++
		}
	}
	return uninstallN, forceN, totalN, nil
}

func logPlannedForceDeletionSummaryForAbort(
	l log.Logger,
	uninstalls, forceLeftovers, planned entity.Entities,
) error {
	u, f, t, err := plannedUninstallForceSplit(planned, uninstalls, forceLeftovers)
	if err != nil {
		return err
	}
	if t == 0 {
		return nil
	}
	l.Info(logIdAction,
		"Planned deletion if you run with --force: {uninstall} uninstall + {force_uninstall} force uninstall = {total} resources",
		log.Int("uninstall", u),
		log.Int("force_uninstall", f),
		log.Int("total", t))
	return nil
}

// gvkAppClusterSplit counts planned uninstall entities for one GVK: merged template+live ("app+cluster")
// versus live-only leftovers ("cluster-only").
type gvkAppClusterSplit struct {
	appAndCluster int
	clusterOnly   int
}

func (s gvkAppClusterSplit) total() int { return s.appAndCluster + s.clusterOnly }

// logPlannedDeletionBreakdownForAbort logs per GVK how many resources are both rendered for an app and
// present in the cluster ("app+cluster") versus cluster-only, plus totals — instead of listing every id.
func logPlannedDeletionBreakdownForAbort(l log.Logger, planned entity.Entities) {
	if plannedClusterKeyedCount(planned) == 0 {
		return
	}
	byGVK := make(map[string]gvkAppClusterSplit, 32)
	for _, e := range planned.Items {
		if !e.HasKey(types.KeyClusterEntity) {
			continue
		}
		hasTpl := e.HasKey(types.KeyTemplateEntity)
		gvk, err := e.GVKString()
		gvkKey := string(gvk)
		if err != nil {
			gvkKey = "(unparsable)"
		}
		row := byGVK[gvkKey]
		if hasTpl {
			row.appAndCluster++
		} else {
			row.clusterOnly++
		}
		byGVK[gvkKey] = row
	}
	gvks := slices.Collect(maps.Keys(byGVK))
	slices.SortFunc(gvks, func(a, b string) int { return cmp.Compare(a, b) })

	var sumApp, sumCluster int
	for _, gvk := range gvks {
		row := byGVK[gvk]
		sumApp += row.appAndCluster
		sumCluster += row.clusterOnly
	}
	total := sumApp + sumCluster
	l.Info(logIdAction,
		"--force-all preview by origin: rendered-with-live {app_cluster} + live-only {cluster_only} = {total} cluster object(s)",
		log.Int("app_cluster", sumApp),
		log.Int("cluster_only", sumCluster),
		log.Int("total", total))

	l.Info(logIdAction, "Same scope by API type (group/version/kind):")
	for _, gvk := range gvks {
		row := byGVK[gvk]
		l.Info(logIdAction, " * {gvk}: app+cluster {app_cluster} + cluster-only {cluster_only} = {total}",
			log.String("gvk", gvk),
			log.Int("app_cluster", row.appAndCluster),
			log.Int("cluster_only", row.clusterOnly),
			log.Int("total", row.total()))
	}
}

func handleForceLeftovers(
	cluster *hydra.Cluster,
	l log.Logger,
	clusterEntities entity.Entities,
	uninstalls entity.Entities,
	workloadScopedLive entity.Entities,
	namespaces sets.Set[types.Namespace],
	selectedAppIds sets.Set[types.AppId],
	allAppIds sets.Set[types.AppId],
	perAppRendered map[types.AppId]entity.Entities,
	renderedAllApps entity.Entities,
	f ClusterUninstallFlags,
) (entity.Entities, error) {
	var err error
	leftovers, err := workloadScopedLive.Without(uninstalls)
	if err != nil {
		return entity.Entities{}, err
	}

	if leftovers.Len() == 0 {
		return uninstalls, nil
	}

	mergedRefs := loadUninstallMergedInspectRefs(cluster, f, renderedAllApps, clusterEntities)

	forceLeftovers, warnLeftovers, ignoredLeftovers, err := commands.ClassifyLeftoversUninstallForce(
		cluster, leftovers, selectedAppIds, allAppIds, perAppRendered, mergedRefs, clusterEntities)
	if err != nil {
		return entity.Entities{}, err
	}

	k8sMinor := 99
	if sm, verr := commands.KubernetesServerMinorVersion(cluster); verr == nil {
		k8sMinor = sm
	}

	warnLeftovers, _, err = commands.ExpandUninstallForceWarnLeftoversOwnedBySeededUIDs(
		leftovers, ignoredLeftovers, warnLeftovers, nil, clusterEntities)
	if err != nil {
		return entity.Entities{}, err
	}

	effectivePresets, err := hydra.EffectiveClusterDefaultsPresetsForCluster(cluster, allAppIds, types.HelmNetworkModeOffline, renderedAllApps, k8sMinor)
	if err != nil {
		return entity.Entities{}, err
	}
	_, presetRenderedAllApps, _, err := commands.MergeBuiltinPresetAppsForCluster(
		cluster, allAppIds, types.HelmNetworkModeOffline, perAppRendered, renderedAllApps, k8sMinor)
	if err != nil {
		return entity.Entities{}, err
	}
	presetEnv, err := cel.NewEnvWithEntityInventory(presetRenderedAllApps)
	if err != nil {
		return entity.Entities{}, err
	}

	presetClosure, err := presetWorkloadClosureForClusterDefaultsFilters(cluster, clusterEntities, presetRenderedAllApps, mergedRefs)
	if err != nil {
		return entity.Entities{}, err
	}

	warnLeftovers, err = filterLeftoversByClusterDefaultsPresets(warnLeftovers, effectivePresets, presetEnv, k8sMinor, presetClosure)
	if err != nil {
		return entity.Entities{}, err
	}

	forceLeftovers, err = filterLeftoversByClusterDefaultsPresets(forceLeftovers, effectivePresets, presetEnv, k8sMinor, presetClosure)
	if err != nil {
		return entity.Entities{}, err
	}

	uninstalls, forceLeftovers, err = commands.MergeForceLeftoversOwnedByCloneRulesIntoUninstalls(
		cluster, uninstalls, forceLeftovers, selectedAppIds, perAppRendered, renderedAllApps, clusterEntities, mergedRefs)
	if err != nil {
		return entity.Entities{}, err
	}

	uninstallsForResolve := uninstalls
	if f.ForceUninstall == types.ForceUninstallForceAll && warnLeftovers.Len() > 0 {
		uninstallsForResolve, err = uninstalls.Merge(warnLeftovers, types.KeyClusterEntity)
		if err != nil {
			return entity.Entities{}, err
		}
	}

	return resolveForceLeftovers(l, forceLeftovers, warnLeftovers, f.ForceUninstall, uninstallsForResolve)
}

func selectUninstallStuff(
	cluster *hydra.Cluster,
	entities entity.Entities,
	appIds sets.Set[types.AppId],
	allAppIds sets.Set[types.AppId],
	safeNamespaces sets.Set[types.Namespace],
	uninstallNamespaces sets.Set[types.Namespace],
	selectedAssignedIds sets.Set[types.Id],
	renderedSelected entity.Entities,
	renderedAllApps entity.Entities,
) (entity.Entities, error) {
	l := cluster.L()
	var err error
	var runtimeAutoSelectedIds sets.Set[types.Id]
	var safeSelectedIds sets.Set[types.Id]

	// mark resources selected for uninstallation
	entities, err = commands.MarkAsSelectedByUninstallPredicates(cluster, entities, appIds, renderedSelected)
	if err != nil {
		return entity.Entities{}, err
	}

	// mark resources managed by other apps that are safe for uninstallation by selected apps
	entities, err = commands.MarkAsSelectedBySafeForUninstallationPredicates(cluster, entities, allAppIds, safeNamespaces, renderedAllApps)
	if err != nil {
		return entity.Entities{}, err
	}

	// mark argocd managed resources of selected apps for uninstallation
	entities, err = commands.MarkAsSelectedArgoCdManagedResources(l, entities, types.KeyClusterEntity, appIds)
	if err != nil {
		return entity.Entities{}, err
	}

	// mark everything from renderedEntities for uninstallation
	entities, _, err = entities.SelectByContainsEntityKey(types.KeyTemplateEntity)
	if err != nil {
		return entity.Entities{}, err
	}

	// mark all resources for uninstallation related to defined resources using partOf relationship
	entities, err = commands.MarkAsSelectedPartOf(l, entities, types.KeyTemplateEntity,
		[]types.EntityKeyUnstructured{types.KeyTemplateEntity, types.KeyClusterEntity})
	if err != nil {
		return entity.Entities{}, err
	}

	entities, runtimeAutoSelectedIds, err = markUninstallNamespaceRuntimeObjects(entities, uninstallNamespaces)
	if err != nil {
		return entity.Entities{}, err
	}

	safeSelectedIds, err = commands.EntityIdsMatchingSafeForUninstallationPredicates(
		cluster,
		entities,
		allAppIds,
		safeNamespaces,
		renderedAllApps,
	)
	if err != nil {
		return entity.Entities{}, err
	}
	entities, err = markSelectedByIdSets(entities, selectedAssignedIds, safeSelectedIds)
	if err != nil {
		return entity.Entities{}, err
	}

	// continue only with selected entities
	entities, err = entities.Selected()
	if err != nil {
		return entity.Entities{}, err
	}
	entities, err = filterPrimaryUninstallSelection(entities, uninstallNamespaces, selectedAssignedIds, safeSelectedIds, runtimeAutoSelectedIds)
	if err != nil {
		return entity.Entities{}, err
	}

	entities, err = entities.UnselectAll()
	if err != nil {
		return entity.Entities{}, err
	}

	return entities, nil
}

func markSelectedByIdSets(entities entity.Entities, idSets ...sets.Set[types.Id]) (entity.Entities, error) {
	ids := sets.New[types.Id]()
	for _, idSet := range idSets {
		for id := range idSet {
			ids.Insert(id)
		}
	}
	if ids.Len() == 0 {
		return entities, nil
	}
	selected, _, err := entities.SelectByIdSet(ids)
	if err != nil {
		return entity.Entities{}, err
	}
	return selected, nil
}

func logUninstallNamespaces(l log.Logger, namespaces sets.Set[types.Namespace]) {
	nsList := make([]string, 0, len(namespaces))
	for ns := range namespaces {
		nsList = append(nsList, string(ns))
	}
	slices.Sort(nsList)
	if len(nsList) == 0 {
		l.Info(logIdAction, "uninstall namespaces: <none>")
		return
	}
	l.Info(logIdAction, "uninstall namespaces: {namespaces}",
		log.String("namespaces", strings.Join(nsList, ", ")))
}

func selectedAssignedClusterIds(model *commands.ResourceModel, appIds sets.Set[types.AppId]) sets.Set[types.Id] {
	out := sets.New[types.Id]()
	if model == nil || !model.HasAppAssignment() {
		return out
	}
	selectedApps := appIds.UnsortedList()
	slices.Sort(selectedApps)
	for _, appId := range selectedApps {
		for id := range model.IdsForApp(appId) {
			out.Insert(id)
		}
	}
	return out
}

func filterSelectedWorkloadScopedLive(
	workloadScopedLive entity.Entities,
	selectedAssignedIds sets.Set[types.Id],
) (entity.Entities, error) {
	if workloadScopedLive.Len() == 0 || selectedAssignedIds.Len() == 0 {
		return entity.NewEntities(nil)
	}
	kept := make([]entity.Entity, 0, workloadScopedLive.Len())
	for _, e := range workloadScopedLive.Items {
		id, err := e.Id()
		if err != nil {
			return entity.Entities{}, err
		}
		if selectedAssignedIds.Has(id) {
			kept = append(kept, e)
		}
	}
	return entity.NewEntities(kept)
}

func filterPrimaryUninstallSelection(
	selected entity.Entities,
	uninstallNamespaces sets.Set[types.Namespace],
	selectedAssignedIds sets.Set[types.Id],
	safeSelectedIds sets.Set[types.Id],
	runtimeAutoSelectedIds sets.Set[types.Id],
) (entity.Entities, error) {
	kept := make([]entity.Entity, 0, selected.Len())
	for _, e := range selected.Items {
		if e.HasKey(types.KeyTemplateEntity) {
			kept = append(kept, e)
			continue
		}
		workloadNamespace, ok := commands.WorkloadNamespace(e)
		if ok && workloadNamespace != "" && uninstallNamespaces != nil && !uninstallNamespaces.Has(workloadNamespace) {
			continue
		}
		id, err := e.Id()
		if err != nil {
			return entity.Entities{}, err
		}
		if selectedAssignedIds.Has(id) || safeSelectedIds.Has(id) || runtimeAutoSelectedIds.Has(id) {
			kept = append(kept, e)
		}
	}
	return entity.NewEntities(kept)
}

// find leftovers in the workload scope (namespaces plus cluster-scoped Hydra targets)
// mirrorFooterDetail mirrors phase strings onto the plan (prepare) subtitle; parallel controls
// concurrent merge workers for leftover merges (same semantics as --parallel elsewhere).
func handleLeftovers(
	l log.Logger,
	workloadScopedLive entity.Entities,
	uninstalls entity.Entities,
	mirrorFooterDetail func(string),
	parallel int,
) (entity.Entities, error) {
	var prepBar log.Progress
	var prepTask log.ProgressTask
	if log.TerminalProgressUI() {
		var err error
		prepBar, err = l.NewProgress(uninstallFooterLeftoverPrep, uninstallLeftoverPrepPhases)
		if err != nil {
			return entity.Entities{}, err
		}
		prepTask = prepBar.NewTask("")
	}

	prepStep := 0
	setPrepDetail := func(msg string) {
		if prepTask != nil {
			prepTask.SetDetail(msg)
		}
		if mirrorFooterDetail != nil {
			mirrorFooterDetail(msg)
		}
	}
	finishPrepStep := func() {
		prepStep++
		if prepBar != nil {
			prepBar.Advance(prepStep, uninstallLeftoverPrepPhases)
		}
	}
	closePrep := func() {
		if prepBar != nil {
			_ = prepBar.Close()
			prepBar = nil
		}
		prepTask = nil
	}

	setPrepDetail("subtract planned uninstall from workload inventory …")
	leftovers, err := workloadScopedLive.Without(uninstalls)
	if err != nil {
		closePrep()
		return entity.Entities{}, err
	}
	finishPrepStep()

	if leftovers.Len() == 0 {
		for prepStep < uninstallLeftoverPrepPhases {
			finishPrepStep()
		}
		closePrep()
		return uninstalls, nil
	}

	setPrepDetail("index UIDs on objects selected for uninstall …")
	uninstallUids := uninstalls.UidMap(types.KeyClusterEntity)
	finishPrepStep()

	allOwnerUids := workloadScopedLive.AllOwnerUidsWithProgress(types.KeyClusterEntity, func(done, total int) {
		setPrepDetail(fmt.Sprintf("resolve transitive ownerReferences on workload scope … %d / %d inventory rows", done, total))
	})
	finishPrepStep()

	setPrepDetail("index UIDs on leftover objects …")
	leftoverUidMap := leftovers.UidMap(types.KeyClusterEntity)
	finishPrepStep()

	closePrep()

	var scanBar log.Progress
	var scanTask log.ProgressTask
	if log.TerminalProgressUI() {
		var barErr error
		scanBar, barErr = l.NewProgress(uninstallFooterLeftoverScan, leftovers.Len())
		if barErr != nil {
			return entity.Entities{}, barErr
		}
		scanTask = scanBar.NewTask("")
	}
	defer func() {
		if scanBar != nil {
			_ = scanBar.Close()
		}
	}()

	// filter leftovers to only those owned by entities selected for uninstallation
	leftoversOwnedBySelected := []entity.Entity{}
	orphanedItems := []entity.Entity{}
	for i, leftover := range leftovers.Items {
		if scanBar != nil && scanTask != nil {
			if id, idErr := leftover.Id(); idErr == nil {
				d := k8s.TruncateFooterDetail(string(id))
				scanTask.SetDetail(d)
				if mirrorFooterDetail != nil {
					mirrorFooterDetail(fmt.Sprintf("leftovers scan · %d / %d · %s", i+1, leftovers.Len(), d))
				}
			}
		}

		// take the uid of the leftover
		uid, ok := leftover.Uid(types.KeyClusterEntity)
		if ok {
			ownerUids, ok := allOwnerUids[uid]
			if ok {
				// check all owner uids of the leftover
				for ownerUid := range ownerUids {
					// check if this owner is selected for uninstallation
					if _, ok := uninstallUids[ownerUid]; ok {
						// this leftover is owned by a selected entity and should be uninstalled
						leftoversOwnedBySelected = append(leftoversOwnedBySelected, leftover)
						break
					}
				}
			}
		}

		if _, ok := leftover.Unstructured(types.KeyClusterEntity); ok {
			directOwnerUids := leftover.OwnerUids(types.KeyClusterEntity)
			if directOwnerUids != nil && directOwnerUids.Len() > 0 {
				allMissing := true
				for ownerUid := range directOwnerUids {
					if _, exists := leftoverUidMap[ownerUid]; exists {
						allMissing = false
						break
					}
				}
				if allMissing {
					orphanedItems = append(orphanedItems, leftover)
				}
			}
		}

		if scanBar != nil {
			scanBar.Advance(i+1, leftovers.Len())
		}
	}

	if scanBar != nil {
		_ = scanBar.Close()
		scanBar = nil
	}

	ownedBySelected, err := entity.NewEntities(leftoversOwnedBySelected)
	if err != nil {
		return entity.Entities{}, err
	}

	l.DebugLog(logIdAction, "ownedBy matched {count} entities", log.Int("count", ownedBySelected.Len()))
	commands.DebugLogEntityIds(l, ownedBySelected)

	orphanedLeftovers, err := entity.NewEntities(orphanedItems)
	if err != nil {
		return entity.Entities{}, err
	}
	l.DebugLog(logIdAction, "orphan matched {count} entities", log.Int("count", orphanedLeftovers.Len()))
	commands.DebugLogEntityIds(l, orphanedLeftovers)

	nMergeOwned := entity.EntityMapIds(uninstalls.EntityMap, ownedBySelected.EntityMap).Len()
	if nMergeOwned < 1 {
		nMergeOwned = 1
	}
	var mergeOwnedBar log.Progress
	if log.TerminalProgressUI() {
		var mErr error
		mergeOwnedBar, mErr = l.NewProgress(uninstallFooterLeftoverMergeOwned, nMergeOwned)
		if mErr != nil {
			return entity.Entities{}, mErr
		}
	}
	mergeOwnedProg := newMergeProgressMirror(mergeOwnedBar, nMergeOwned, "merge owned", mirrorFooterDetail)
	uninstalls, err = uninstalls.MergeWithProgress(ownedBySelected, mergeOwnedProg, parallel, types.KeyClusterEntity)
	if mergeOwnedProg != nil {
		_ = mergeOwnedProg.Close()
	}
	if err != nil {
		return entity.Entities{}, err
	}

	nMergeOrph := entity.EntityMapIds(uninstalls.EntityMap, orphanedLeftovers.EntityMap).Len()
	if nMergeOrph < 1 {
		nMergeOrph = 1
	}
	var mergeOrphBar log.Progress
	if log.TerminalProgressUI() {
		var mErr error
		mergeOrphBar, mErr = l.NewProgress(uninstallFooterLeftoverMergeOrph, nMergeOrph)
		if mErr != nil {
			return entity.Entities{}, mErr
		}
	}
	mergeOrphProg := newMergeProgressMirror(mergeOrphBar, nMergeOrph, "orphan merge", mirrorFooterDetail)
	out, err := uninstalls.MergeWithProgress(orphanedLeftovers, mergeOrphProg, parallel, types.KeyClusterEntity)
	if mergeOrphProg != nil {
		_ = mergeOrphProg.Close()
	}
	return out, err
}

func markUninstallNamespaceRuntimeObjects(
	entities entity.Entities,
	uninstallNamespaces sets.Set[types.Namespace],
) (entity.Entities, sets.Set[types.Id], error) {
	if uninstallNamespaces.Len() == 0 || entities.Len() == 0 {
		return entities, sets.New[types.Id](), nil
	}
	selected, _, err := entities.Select(func(e entity.Entity) (bool, error) {
		return shouldAutoSelectUninstallNamespaceRuntimeObject(e, uninstallNamespaces)
	})
	if err != nil {
		return entity.Entities{}, nil, err
	}
	autoSelectedIds := sets.New[types.Id]()
	for _, e := range selected.Items {
		if !e.Selected() {
			continue
		}
		id, idErr := e.Id()
		if idErr != nil {
			return entity.Entities{}, nil, idErr
		}
		autoSelectedIds.Insert(id)
	}
	return selected, autoSelectedIds, nil
}

func shouldAutoSelectUninstallNamespaceRuntimeObject(
	e entity.Entity,
	uninstallNamespaces sets.Set[types.Namespace],
) (bool, error) {
	gvk, err := e.GVKString()
	if err != nil {
		return false, err
	}
	if gvk == types.KubernetesGvkV1Namespace {
		name, err := e.Name()
		if err != nil {
			return false, err
		}
		ns := types.Namespace(name)
		if commands.WithoutSystemNamespaces(sets.New(ns)).Len() == 0 {
			return false, nil
		}
		return uninstallNamespaces.Has(ns), nil
	}
	if gvk == types.KubernetesGvkV1ServiceAccount {
		name, err := e.Name()
		if err != nil {
			return false, err
		}
		if name != types.Name("default") {
			return false, nil
		}
		ns, err := e.Namespace()
		if err != nil {
			return false, err
		}
		if commands.WithoutSystemNamespaces(sets.New(ns)).Len() == 0 {
			return false, nil
		}
		return uninstallNamespaces.Has(ns), nil
	}
	return false, nil
}

func skipProtectedNamespaceDeletes(
	l log.Logger,
	uninstalls entity.Entities,
	clusterEntities entity.Entities,
) (entity.Entities, error) {
	if uninstalls.Len() == 0 {
		return uninstalls, nil
	}

	plannedIds := uninstalls.IdSet
	kept := make([]entity.Entity, 0, uninstalls.Len())

	for _, e := range uninstalls.Items {
		gvk, err := e.GVKString()
		if err != nil {
			return entity.Entities{}, err
		}
		if gvk != types.KubernetesGvkV1Namespace {
			kept = append(kept, e)
			continue
		}

		name, err := e.Name()
		if err != nil {
			return entity.Entities{}, err
		}
		ns := types.Namespace(name)
		if commands.WithoutSystemNamespaces(sets.New(ns)).Len() == 0 {
			l.Warn(logIdAction, "Skipping namespace delete for protected Kubernetes system namespace {namespace}.",
				log.String("namespace", string(ns)))
			continue
		}

		leftovers, err := namespaceUninstallLeftovers(ns, plannedIds, clusterEntities)
		if err != nil {
			return entity.Entities{}, err
		}
		if leftovers.Len() > 0 {
			l.Warn(logIdAction,
				"Skipping namespace delete for {namespace}: found {count} live resource(s) in that namespace that are not part of this uninstall plan.",
				log.String("namespace", string(ns)),
				log.Int("count", leftovers.Len()))
			commands.LogEntityIds(l, leftovers)
			continue
		}

		kept = append(kept, e)
	}

	return entity.NewEntities(kept)
}

func namespaceUninstallLeftovers(
	namespace types.Namespace,
	plannedIds sets.Set[types.Id],
	clusterEntities entity.Entities,
) (entity.Entities, error) {
	var leftovers []entity.Entity
	for _, e := range clusterEntities.Items {
		workloadNamespace, ok := commands.WorkloadNamespace(e)
		if !ok || workloadNamespace != namespace {
			continue
		}
		id, err := e.Id()
		if err != nil {
			return entity.Entities{}, err
		}
		if plannedIds.Has(id) {
			continue
		}
		if isIgnorableNamespaceDeleteLeftover(e, id, namespace) {
			continue
		}
		leftovers = append(leftovers, e)
	}
	return entity.NewEntities(leftovers)
}

func isIgnorableNamespaceDeleteLeftover(e entity.Entity, id types.Id, namespace types.Namespace) bool {
	gvk, err := e.GVKString()
	if err != nil {
		return false
	}
	switch gvk {
	case types.KubernetesGvkV1Namespace:
		name, err := e.Name()
		return err == nil && types.Namespace(name) == namespace
	case types.KubernetesGvkV1ServiceAccount:
		name, err := e.Name()
		if err != nil {
			return false
		}
		ns, err := e.Namespace()
		if err != nil {
			return false
		}
		return ns == namespace && name == types.Name("default")
	default:
		return commands.IsKubernetesAPIServerManagedKubeRootCAConfigMap(id)
	}
}

func presetWorkloadClosureForClusterDefaultsFilters(
	cluster *hydra.Cluster,
	refInventory entity.Entities,
	renderedTemplates entity.Entities,
	mergedInspectRefs []types.Ref,
) (workloadclosure.MatchInput, error) {
	if len(mergedInspectRefs) > 0 {
		return commands.WorkloadClosureMatchInputFromMergedInventory(mergedInspectRefs, refInventory, renderedTemplates)
	}
	if cluster == nil || refInventory.Len() == 0 {
		return workloadclosure.EmptyMatchInput(types.KeyClusterEntity), nil
	}
	cluster.ResetPreferredVersionsCache()
	pref, err := cluster.PreferredVersions(func() (types.ScopeInfoMap, error) {
		return commands.ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity, types.CrdModeSilent)
	})
	if err != nil {
		return workloadclosure.MatchInput{}, err
	}
	return commands.WorkloadClosureMatchInputFromInventory(cluster.L(), refInventory, pref)
}

// filterLeftoversByClusterDefaultsPresets drops leftover entities that match an effective
// cluster-defaults preset (CEL or anchor id, evaluated via [hydra.NewClusterDefaultsPresetEvalCache]).
// Presets describe optional cluster plumbing (for example **flannel**); matching uninstall-warn or
// uninstall-force ref rules alone must not surface those entities as leftovers during uninstall
// planning.
func filterLeftoversByClusterDefaultsPresets(
	leftovers entity.Entities,
	effective []hydra.ClusterDefaultsPresetEffective,
	env cel.Env,
	k8sMinor int,
	presetClosure workloadclosure.MatchInput,
) (entity.Entities, error) {
	if leftovers.Len() == 0 || len(effective) == 0 {
		return leftovers, nil
	}
	cache, err := hydra.NewClusterDefaultsPresetEvalCache(effective, k8sMinor, env)
	if err != nil {
		return entity.Entities{}, err
	}
	var kept []entity.Entity
	for _, e := range leftovers.Items {
		ids, err := cache.MatchingPresetIDsWithRegarding(e, presetClosure, nil)
		if err != nil {
			return entity.Entities{}, err
		}
		if len(ids) == 0 {
			kept = append(kept, e)
		}
	}
	return entity.NewEntities(kept)
}

// loadUninstallMergedInspectRefs builds the same merged inspect ref graph as `hydra gitops
// inspect` (template + cluster, canonicalized + augmented, with clone materialization). Uninstall
// uses this graph for preset/closure filtering, not for app ownership assignment. Returns nil on
// load failure so uninstall never fails purely because of this enhancement.
func loadUninstallMergedInspectRefs(
	cluster *hydra.Cluster,
	f ClusterUninstallFlags,
	renderedAllApps entity.Entities,
	clusterEntities entity.Entities,
) []types.Ref {
	g, err := commands.LoadInspectRefGraph(commands.InspectRefGraphParams{
		Cluster:                     cluster,
		NetworkMode:                 types.HelmNetworkModeOffline,
		Bootstrap:                   f.Bootstrap,
		RenderedTemplateEntities:    &renderedAllApps,
		ClusterInventory:            &clusterEntities,
		IncludeTemplateRefs:         true,
		IncludeClusterRefs:          true,
		IncludeCloneMaterialization: true,
		SkipFoundDefinitionsInfoLog: true,
	})
	if err != nil || g == nil {
		return nil
	}
	return g.Refs
}
