package commands

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/k8s"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
	"k8s.io/apimachinery/pkg/util/sets"
)

// RefOwnershipReviewStepCount is how many ref-ownership milestones advance the unified cluster-review
// footer bar after post-discovery when terminal progress is enabled.
const RefOwnershipReviewStepCount = 8

// refOwnershipLiveProgressEvery controls how often the ref-ownership bar detail refreshes during
// the per-live-object assignment pass.
const refOwnershipLiveProgressEvery = 64

func advanceRefOwnershipReviewProgress(p log.Progress, task log.ProgressTask, step int, detail string) {
	if p == nil {
		return
	}
	if task != nil {
		task.SetDetail(k8s.TruncateFooterDetail(detail))
	}
	p.Advance(clusterReviewPostDiscoveryUnifiedTotal+step, clusterReviewUnifiedTotal)
}

func refOwnershipJumpProgressToEnd(p log.Progress, task log.ProgressTask, detail string) {
	if p == nil {
		return
	}
	if task != nil {
		task.SetDetail(k8s.TruncateFooterDetail(detail))
	}
	p.Advance(clusterReviewUnifiedTotal, clusterReviewUnifiedTotal)
}

// RefOwnershipConflictWithTemplateFinding is reported when a resource id is owned by one app in
// standalone template render but another app's ref-parser ownership predicates also match (same
// rule as uninstall ErrUninstallRefOwnershipConflictsWithTemplate).
const RefOwnershipConflictWithTemplateFinding = "ref ownership conflicts with standalone template render"

// RefOwnershipAmbiguousClusterOnlyFinding is reported when a live cluster resource id appears in
// no standalone template render but ref ownership predicates (including [runtime]-tagged groups)
// match more than one app.
const RefOwnershipAmbiguousClusterOnlyFinding = "ref ownership ambiguous for cluster-only resource"

// RefOwnershipAmbiguousOwnerChainFinding is reported when metadata.ownerReferences resolve to
// multiple distinct Hydra apps for the same object (same rule as cluster uninstall owner expansion).
const RefOwnershipAmbiguousOwnerChainFinding = "ref ownership ambiguous: owner references resolve to multiple apps"

// RefOwnershipUnassignedClusterOnlyFinding is reported for live cluster objects that appear in no
// standalone template render and cannot be assigned to any app via ref predicates or transitive
// ownerReferences, within the same namespace scope used for uninstall leftover scanning.
const RefOwnershipUnassignedClusterOnlyFinding = "ref ownership: cluster-only resource has no Hydra app assignment"

// RefOwnershipOwnerNamespacesConflictFinding is reported when a live v1/Namespace is assigned to one
// app via ref ownership predicates but global.hydra.ownerNamespaces declares a different app.
const RefOwnershipOwnerNamespacesConflictFinding = "ref ownership: v1/Namespace ref assignment conflicts with global.hydra.ownerNamespaces"

// RefOwnershipClusterResourceMatchesClusterDefaultsPresetFinding is reported when a live cluster-only
// resource is assigned to a Hydra app via ref predicates (or owner chain) and also matches an
// active global.hydra.presets cluster-defaults rule (coredns/kubernetes/flannel/canal/kubermatic/syseleven/metakube/syseleven-node-problem-detector/quobyte/cloudinit/cinder).
const RefOwnershipClusterResourceMatchesClusterDefaultsPresetFinding = "ref ownership: cluster-only resource matches cluster defaults preset(s) and a Hydra app assignment"

// RefOwnershipUnassignedClusterOnlyScopeNote is the fixed trailing clause for unassigned cluster-only
// ref ownership review findings (same text as in the emitted message after the resource id).
const RefOwnershipUnassignedClusterOnlyScopeNote = "(would remain unassigned for hydra gitops uninstall in this namespace scope)"

// RefOwnershipUnassignedClusterOnlyMessageGroupTitle is the full human "message type" line for grouped
// stdout (base finding text plus the scope note).
const RefOwnershipUnassignedClusterOnlyMessageGroupTitle = RefOwnershipUnassignedClusterOnlyFinding + " " + RefOwnershipUnassignedClusterOnlyScopeNote

// EntityWithClusterUnstructuredSameAsTemplate returns a copy of e with KeyClusterEntity set to a
// deep copy of the template manifest so ref ownership CEL matches the uninstall/cluster path
// (predicates use clusterEntity).
func EntityWithClusterUnstructuredSameAsTemplate(e entity.Entity) (entity.Entity, error) {
	u, ok := e.Unstructured(types.KeyTemplateEntity)
	if !ok {
		return entity.Entity{}, fmt.Errorf("entity has no template unstructured for ref ownership eval")
	}
	c := *u.DeepCopy()
	return e.Modify(func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(types.KeyClusterEntity, c)
	})
}

// AppendRefOwnershipReviewFindings evaluates uninstall / uninstall-safe / uninstall-force / backup ref-parser + template
// reconciliation for every template resource id across per-app renders, and (when liveCluster is
// non-empty) evaluates cluster-only resources whose ids appear in no template.
//
// Template-mapped resources use predicates from ref groups tagged uninstall / uninstall-safe / uninstall-force / backup,
// excluding groups tagged [runtime] — the same rule as hydra local review, so broad runtime-only
// uninstall rules do not false-positive against template-owned objects.
//
// Live resources are checked per id: if the id appears in templateIDToApp, predicates exclude
// [runtime]-tagged groups (same as local review). If the id is cluster-only, the full set including
// [runtime] applies (aligned with cluster uninstall). Ownership is then propagated along
// ownerReferences like AssignClusterEntitiesToAtMostOneAppByRefs. Unassigned
// findings (no template id, no ref match, no transitive owner, no ownerNamespaces match) are emitted only for namespaces in
// refOwnershipUnassignedNamespaces (uninstall leftover namespaces plus every global.hydra.ownerNamespaces entry); pass nil to skip.
// When reportUnassignedClusterOnlyResources is false, unassigned cluster-only findings are never
// emitted (hydra gitops review app).
//
// kubernetesMinor is the live apiserver minor when known (see KubernetesServerMinorVersion); use 99
// when unavailable so version-gated bootstrap RBAC in global.hydra.presets stays aligned.
//
// skipTemplateVsRefOwnership should be true when hydra gitops review has already compared template
// refs to live-object refs and the edge sets match — template app ids are authoritative and the
// expensive template-vs-ref-predicate reconciliation (step 4) can be skipped.
func AppendRefOwnershipReviewFindings(
	cluster *hydra.Cluster,
	allAppIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	perAppRendered map[types.AppId]entity.Entities,
	renderedAllApps entity.Entities,
	liveCluster entity.Entities,
	reportUnassignedClusterOnlyResources bool,
	kubernetesMinor int,
	skipTemplateVsRefOwnership bool,
	onFinding ReviewFindingCallback,
	reviewParallelism int,
	progress log.Progress,
	task log.ProgressTask,
) (int, error) {
	advanceRefOwnershipReviewProgress(progress, task, 1, "build ref ownership predicates (incl. runtime variants)")
	appOrder := allAppIds.UnsortedList()
	slices.SortFunc(appOrder, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })

	nonNegativePriorityNR, negativePriorityNR, err := hydra.HydraAppRefOwnershipUninstallPredicateLinePriorityBands(
		cluster, allAppIds, networkMode, renderedAllApps, false)
	if err != nil {
		return 0, err
	}
	predLinesNoRuntime := mergeRefOwnershipPredicateLinePriorityBandMaps(nonNegativePriorityNR, negativePriorityNR, appOrder)

	var predLinesWithRuntime map[types.AppId][]types.RefOwnershipPredicateLine
	if liveCluster.Len() > 0 {
		nonNegativePriorityWR, negativePriorityWR, werr := hydra.HydraAppRefOwnershipUninstallPredicateLinePriorityBands(
			cluster, allAppIds, networkMode, renderedAllApps, true)
		if werr != nil {
			return 0, werr
		}
		predLinesWithRuntime = mergeRefOwnershipPredicateLinePriorityBandMaps(nonNegativePriorityWR, negativePriorityWR, appOrder)
	}
	advanceRefOwnershipReviewProgress(progress, task, 2, "namespace scope for unassigned findings")
	var namespaceOwners map[types.Namespace]types.AppId
	if liveCluster.Len() > 0 {
		no, oerr := hydra.HydraAppNamespaceOwners(cluster, allAppIds, networkMode)
		if oerr != nil {
			return 0, oerr
		}
		namespaceOwners = no
	}
	var refOwnershipUnassignedNamespaces sets.Set[types.Namespace]
	if liveCluster.Len() > 0 && reportUnassignedClusterOnlyResources {
		exclusive, exErr := ExclusiveNamespaces(cluster.L(), renderedAllApps, allAppIds)
		if exErr != nil {
			return 0, exErr
		}
		refOwnershipUnassignedNamespaces = UninstallLeftoverNamespaces(exclusive, renderedAllApps)
		for ns := range namespaceOwners {
			refOwnershipUnassignedNamespaces.Insert(ns)
		}
	}
	advanceRefOwnershipReviewProgress(progress, task, 3, "cluster defaults preset environment")
	var effectivePresets []hydra.ClusterDefaultsPresetEffective
	var presetEnv cel.Env
	if liveCluster.Len() > 0 {
		var pErr error
		effectivePresets, pErr = hydra.EffectiveClusterDefaultsPresetsForCluster(cluster, allAppIds, networkMode, renderedAllApps, kubernetesMinor)
		if pErr != nil {
			return 0, pErr
		}
		presetEnv, pErr = cel.NewEnvWithEntityInventory(renderedAllApps)
		if pErr != nil {
			return 0, pErr
		}
	}
	workloadClosure := workloadclosure.EmptyMatchInput(types.KeyClusterEntity)
	if liveCluster.Len() > 0 {
		cluster.ResetPreferredVersionsCache()
		pref, prefErr := cluster.PreferredVersions(func() (types.ScopeInfoMap, error) {
			return ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity, types.CrdModeSilent)
		})
		if prefErr != nil {
			return 0, prefErr
		}
		var rErr error
		workloadClosure, rErr = WorkloadClosureMatchInputFromInventory(cluster.L(), liveCluster, pref)
		if rErr != nil {
			return 0, rErr
		}
	}
	return refOwnershipAppendFindings(
		cluster.L(), allAppIds, perAppRendered, predLinesNoRuntime, predLinesWithRuntime, workloadClosure, liveCluster, refOwnershipUnassignedNamespaces, namespaceOwners, kubernetesMinor,
		effectivePresets, presetEnv, skipTemplateVsRefOwnership, onFinding, reviewParallelism, progress, task)
}

func refOwnershipAppendFindings(
	l log.Logger,
	allAppIds sets.Set[types.AppId],
	perAppRendered map[types.AppId]entity.Entities,
	predLinesNoRuntime map[types.AppId][]types.RefOwnershipPredicateLine,
	predLinesWithRuntime map[types.AppId][]types.RefOwnershipPredicateLine,
	workloadClosure workloadclosure.MatchInput,
	liveCluster entity.Entities,
	refOwnershipUnassignedNamespaces sets.Set[types.Namespace],
	namespaceOwners map[types.Namespace]types.AppId,
	kubernetesMinor int,
	effectivePresets []hydra.ClusterDefaultsPresetEffective,
	presetEnv cel.Env,
	skipTemplateVsRefOwnership bool,
	onFinding ReviewFindingCallback,
	reviewParallelism int,
	progress log.Progress,
	task log.ProgressTask,
) (int, error) {
	workers := EffectiveClusterWorkerParallelism(reviewParallelism)
	templateIDToApp := templateResourceIDToApp(perAppRendered)
	templateEntityByApp := make(map[types.AppId]map[types.Id]entity.Entity, len(perAppRendered))
	for appId, ents := range perAppRendered {
		byID := make(map[types.Id]entity.Entity, len(ents.Items))
		for _, ent := range ents.Items {
			id, err := ent.Id()
			if err != nil {
				continue
			}
			byID[id] = ent
		}
		templateEntityByApp[appId] = byID
	}
	appOrder := allAppIds.UnsortedList()
	slices.SortFunc(appOrder, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })

	envByApp := make(map[types.AppId]cel.Env)
	for _, appId := range appOrder {
		rend, ok := perAppRendered[appId]
		if !ok {
			continue
		}
		env, envErr := cel.NewEnvWithEntityInventory(rend)
		if envErr != nil {
			return 0, envErr
		}
		envByApp[appId] = env
	}

	compiledNoRuntime, err := compileRefOwnershipPredicateLines(appOrder, predLinesNoRuntime, envByApp)
	if err != nil {
		return 0, err
	}
	var compiledWithRuntime map[types.AppId][]compiledRefOwnershipPredicate
	if liveCluster.Len() > 0 && predLinesWithRuntime != nil {
		compiledWithRuntime, err = compileRefOwnershipPredicateLines(appOrder, predLinesWithRuntime, envByApp)
		if err != nil {
			return 0, err
		}
	}

	templateIDCount := len(templateIDToApp)

	var nTemplate int
	var terr error
	if skipTemplateVsRefOwnership && liveCluster.Len() > 0 {
		advanceRefOwnershipReviewProgress(progress, task, 4,
			"template vs ref ownership skipped — template and cluster refs match")
	} else {
		advanceRefOwnershipReviewProgress(progress, task, 4,
			fmt.Sprintf("template vs ref ownership — 0 / %d", templateIDCount))

		nTemplate, terr = refOwnershipRunTemplateVsRefOwnershipScan(
			l, templateIDToApp, templateEntityByApp, appOrder, compiledNoRuntime, workloadClosure, onFinding, workers, templateIDCount, progress, task)
	}
	if terr != nil {
		return nTemplate, terr
	}
	count := nTemplate

	if liveCluster.Len() == 0 || predLinesWithRuntime == nil {
		refOwnershipJumpProgressToEnd(progress, task, "no live cluster inventory for ownership assignment")
		return count, nil
	}

	advanceRefOwnershipReviewProgress(progress, task, 5,
		fmt.Sprintf("assign live resources — 0 / %d", liveCluster.Len()))

	assignment := make(map[types.Id]types.AppId)
	refPredAmbiguous := sets.New[types.Id]()

	nAmbiguous, err := refOwnershipRunLiveResourceAssignment(
		l, liveCluster, kubernetesMinor, templateIDToApp, appOrder, compiledNoRuntime, compiledWithRuntime,
		workloadClosure, onFinding, workers, progress, task, assignment, refPredAmbiguous)
	if err != nil {
		return count, err
	}
	count += nAmbiguous

	if len(namespaceOwners) > 0 {
		if err := ApplyOwnerNamespacesToNamespaceAssignments(
			templateIDToApp, namespaceOwners, liveCluster, assignment, refPredAmbiguous,
			func(eid types.Id, refApp, declaredApp types.AppId) error {
				msg := fmt.Sprintf("%s: resource %s ref-assigned to %s but ownerNamespaces declares %s",
					RefOwnershipOwnerNamespacesConflictFinding, eid, refApp, declaredApp)
				if err := onFinding(ReviewFinding{Target: eid, Message: msg}); err != nil {
					return err
				}
				count++
				return nil
			},
		); err != nil {
			return count, err
		}
	}

	advanceRefOwnershipReviewProgress(progress, task, 6, "expand owner reference chains")

	var ownerAmbiguous []OwnerRefAmbiguousAssignment
	if err := expandAssignmentByOwnerRefsSoft(liveCluster, assignment, types.KeyClusterEntity, templateIDToApp, &ownerAmbiguous); err != nil {
		return count, err
	}
	ownerAmbiguousSet := sets.New[types.Id]()
	for _, o := range ownerAmbiguous {
		ownerAmbiguousSet.Insert(o.EntityID)
		apps := o.AppIDs
		msg := fmt.Sprintf("%s: resource %s has owner reference(s) resolving to more than one app (%v); fix ownership or ref parsers",
			RefOwnershipAmbiguousOwnerChainFinding, o.EntityID, apps)
		if err := onFinding(ReviewFinding{Target: o.EntityID, Message: msg}); err != nil {
			return count, err
		}
		count++
	}

	var presetEvalCache *hydra.ClusterDefaultsPresetEvalCache
	if len(effectivePresets) > 0 {
		var cerr error
		presetEvalCache, cerr = hydra.NewClusterDefaultsPresetEvalCache(effectivePresets, kubernetesMinor, presetEnv)
		if cerr != nil {
			return count, cerr
		}
	}

	advanceRefOwnershipReviewProgress(progress, task, 7, "cluster defaults vs app assignments")

	if len(effectivePresets) > 0 {
		for _, e := range liveCluster.Items {
			eid, err := e.Id()
			if err != nil {
				return count, err
			}
			if _, has := templateIDToApp[eid]; has {
				continue
			}
			if IsKubernetesStandardRefOwnershipExempt(eid, kubernetesMinor) {
				continue
			}
			if refPredAmbiguous.Has(eid) || ownerAmbiguousSet.Has(eid) {
				continue
			}
			owner, ok := assignment[eid]
			if !ok {
				continue
			}
			ids, err := presetEvalCache.MatchingPresetIDsWithRegarding(e, workloadClosure, nil)
			if err != nil {
				return count, err
			}
			if len(ids) == 0 {
				continue
			}
			slices.Sort(ids)
			msg := fmt.Sprintf("%s: resource %s is assigned to app %s and matches cluster defaults preset(s) [%s]",
				RefOwnershipClusterResourceMatchesClusterDefaultsPresetFinding, eid, owner, strings.Join(ids, ", "))
			if err := onFinding(ReviewFinding{Target: eid, Message: msg}); err != nil {
				return count, err
			}
			count++
		}
	}

	advanceRefOwnershipReviewProgress(progress, task, 8, "unassigned cluster-only resources")

	if refOwnershipUnassignedNamespaces != nil {
		liveUidMap := liveCluster.UidMap(types.KeyClusterEntity)
		for _, e := range liveCluster.Items {
			eid, err := e.Id()
			if err != nil {
				return count, err
			}
			if IsKubernetesStandardRefOwnershipExempt(eid, kubernetesMinor) {
				continue
			}
			if _, has := templateIDToApp[eid]; has {
				continue
			}
			if _, has := assignment[eid]; has {
				continue
			}
			if refPredAmbiguous.Has(eid) || ownerAmbiguousSet.Has(eid) {
				continue
			}
			// Only report ownership roots: skip objects that reference another live inventory object
			// as owner, so children (e.g. Pod → ReplicaSet → Deployment) are not duplicated when the
			// chain is unassigned — the transitive root in the snapshot is reported.
			if refOwnershipHasLiveOwnerInInventory(e, liveUidMap, types.KeyClusterEntity) {
				continue
			}
			ns, ok := WorkloadNamespace(e)
			if !ok || !refOwnershipUnassignedNamespaces.Has(ns) {
				continue
			}
			if len(effectivePresets) > 0 {
				ids, err := presetEvalCache.MatchingPresetIDsWithRegarding(e, workloadClosure, nil)
				if err != nil {
					return count, err
				}
				if len(ids) > 0 {
					continue
				}
			}
			msg := fmt.Sprintf("%s: resource %s %s",
				RefOwnershipUnassignedClusterOnlyFinding, eid, RefOwnershipUnassignedClusterOnlyScopeNote)
			if err := onFinding(ReviewFinding{Target: eid, Message: msg}); err != nil {
				return count, err
			}
			count++
		}
	}

	return count, nil
}

type refOwnershipWorkerStatusLines struct {
	tasks []log.ProgressTask
}

func refOwnershipOpenWorkerStatusLines(p log.Progress, lineCount int) *refOwnershipWorkerStatusLines {
	if p == nil || lineCount <= 0 {
		return nil
	}
	w := &refOwnershipWorkerStatusLines{tasks: make([]log.ProgressTask, lineCount)}
	for i := range w.tasks {
		w.tasks[i] = p.NewTask("")
		w.tasks[i].SetDetail(k8s.TruncateFooterDetail("…"))
	}
	return w
}

func refOwnershipCloseWorkerStatusLines(w *refOwnershipWorkerStatusLines) {
	if w == nil {
		return
	}
	for _, t := range w.tasks {
		_ = t.Close()
	}
}

func (w *refOwnershipWorkerStatusLines) set(workerIdx int, detail string) {
	if w == nil || workerIdx < 0 || workerIdx >= len(w.tasks) {
		return
	}
	w.tasks[workerIdx].SetDetail(k8s.TruncateFooterDetail(detail))
}

type refOwnershipTemplateVsRefJob struct {
	eid           types.Id
	templateOwner types.AppId
}

func refOwnershipReviewSingleTemplateVsRef(
	eid types.Id,
	templateOwner types.AppId,
	templateIDToApp map[types.Id]types.AppId,
	templateEntityByApp map[types.AppId]map[types.Id]entity.Entity,
	appOrder []types.AppId,
	compiledNoRuntime map[types.AppId][]compiledRefOwnershipPredicate,
	workloadClosure workloadclosure.MatchInput,
	onFinding ReviewFindingCallback,
) (int, error) {
	byID := templateEntityByApp[templateOwner]
	e, found := byID[eid]
	if !found {
		return 0, nil
	}
	eCluster, convErr := EntityWithClusterUnstructuredSameAsTemplate(e)
	if convErr != nil {
		return 0, convErr
	}
	matching, matchErr := refOwnershipMatchingAppsCompiledWithOptions(
		eCluster, appOrder, compiledNoRuntime, workloadClosure, refOwnershipMatchOptions{
			skipApp: &templateOwner,
			allowFallback: func(entity.Entity) bool {
				return false
			},
		})
	if matchErr != nil {
		return 0, matchErr
	}
	_, _, recErr := reconcileClusterOwnership(templateIDToApp, eid, matching)
	if recErr == nil {
		return 0, nil
	}
	if errors.Id(recErr) != errors.ErrUninstallRefOwnershipConflictsWithTemplate {
		return 0, recErr
	}
	var conflicting []types.AppId
	for a := range matching {
		if a != templateOwner {
			conflicting = append(conflicting, a)
		}
	}
	slices.SortFunc(conflicting, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })
	msg := fmt.Sprintf("%s: template owner %s; ref predicate(s) also match other app(s) %v",
		RefOwnershipConflictWithTemplateFinding, templateOwner, conflicting)
	if err := onFinding(ReviewFinding{Target: eid, Message: msg}); err != nil {
		return 0, err
	}
	return 1, nil
}

func refOwnershipRunTemplateVsRefOwnershipScan(
	l log.Logger,
	templateIDToApp map[types.Id]types.AppId,
	templateEntityByApp map[types.AppId]map[types.Id]entity.Entity,
	appOrder []types.AppId,
	compiledNoRuntime map[types.AppId][]compiledRefOwnershipPredicate,
	workloadClosure workloadclosure.MatchInput,
	onFinding ReviewFindingCallback,
	workers int,
	templateIDCount int,
	progress log.Progress,
	task log.ProgressTask,
) (int, error) {
	if templateIDCount == 0 {
		return 0, nil
	}
	jobs := make([]refOwnershipTemplateVsRefJob, 0, templateIDCount)
	for eid, owner := range templateIDToApp {
		jobs = append(jobs, refOwnershipTemplateVsRefJob{eid: eid, templateOwner: owner})
	}
	slices.SortFunc(jobs, func(a, b refOwnershipTemplateVsRefJob) int {
		return cmp.Compare(a.eid, b.eid)
	})

	if workers <= 1 {
		var c int
		for tidx, j := range jobs {
			if progress != nil && task != nil &&
				(tidx%refOwnershipLiveProgressEvery == 0 || tidx == len(jobs)-1) {
				task.SetDetail(k8s.TruncateFooterDetail(fmt.Sprintf(
					"template vs ref ownership — %d / %d", tidx+1, len(jobs))))
			}
			n, err := refOwnershipReviewSingleTemplateVsRef(
				j.eid, j.templateOwner, templateIDToApp, templateEntityByApp, appOrder, compiledNoRuntime, workloadClosure, onFinding)
			if err != nil {
				return c, err
			}
			c += n
		}
		return c, nil
	}

	var subBar log.Progress
	var subTask log.ProgressTask
	var foot *refOwnershipWorkerStatusLines
	if progress != nil && workers > 1 {
		sb, perr := l.NewProgress("cluster review · template vs ref ownership", len(jobs))
		if perr != nil {
			return 0, perr
		}
		if sb != nil {
			subBar = sb
			subTask = subBar.NewTask("")
			defer func() { _ = subBar.Close() }()
			foot = refOwnershipOpenWorkerStatusLines(subBar, workers)
		}
	}
	if foot == nil && progress != nil && workers > 1 {
		foot = refOwnershipOpenWorkerStatusLines(progress, workers)
	}
	defer refOwnershipCloseWorkerStatusLines(foot)

	jobIdx := make(chan int, workers*2)
	var wg sync.WaitGroup
	var nFind int64
	var done int64
	var firstErr error
	var errMu sync.Mutex
	var onMu sync.Mutex

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			for ji := range jobIdx {
				errMu.Lock()
				fErr := firstErr
				errMu.Unlock()
				if fErr != nil {
					return
				}
				j := jobs[ji]
				if foot != nil {
					foot.set(workerIdx, fmt.Sprintf(
						"worker %d · %s · job %d / %d", workerIdx+1, string(j.eid), ji+1, len(jobs)))
				}
				onMu.Lock()
				n, err := refOwnershipReviewSingleTemplateVsRef(
					j.eid, j.templateOwner, templateIDToApp, templateEntityByApp, appOrder, compiledNoRuntime, workloadClosure, onFinding)
				onMu.Unlock()
				if err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
					return
				}
				atomic.AddInt64(&nFind, int64(n))
				d := atomic.AddInt64(&done, 1)
				if subBar != nil {
					subBar.Advance(int(d), len(jobs))
					if subTask != nil &&
						(d%int64(refOwnershipLiveProgressEvery) == 0 || d == int64(len(jobs))) {
						subTask.SetDetail(k8s.TruncateFooterDetail(fmt.Sprintf(
							"completed %d / %d", d, len(jobs))))
					}
					continue
				}
				if task != nil && progress != nil &&
					(d%int64(refOwnershipLiveProgressEvery) == 0 || d == int64(len(jobs))) {
					task.SetDetail(k8s.TruncateFooterDetail(fmt.Sprintf(
						"template vs ref ownership — %d / %d", d, len(jobs))))
				}
			}
		}(w)
	}
	for i := range jobs {
		jobIdx <- i
	}
	close(jobIdx)
	wg.Wait()
	return int(atomic.LoadInt64(&nFind)), firstErr
}

type refOwnershipLiveAssignOutcome struct {
	err           error
	skip          bool
	ambiguous     bool
	assign        bool
	eid           types.Id
	owner         types.AppId
	ambiguousApps []types.AppId
}

func refOwnershipScanOneLiveItem(
	e entity.Entity,
	kubernetesMinor int,
	templateIDToApp map[types.Id]types.AppId,
	appOrder []types.AppId,
	compiledNoRuntime map[types.AppId][]compiledRefOwnershipPredicate,
	compiledWithRuntime map[types.AppId][]compiledRefOwnershipPredicate,
	workloadClosure workloadclosure.MatchInput,
) refOwnershipLiveAssignOutcome {
	eid, err := e.Id()
	if err != nil {
		return refOwnershipLiveAssignOutcome{err: err}
	}
	if IsKubernetesStandardRefOwnershipExempt(eid, kubernetesMinor) {
		return refOwnershipLiveAssignOutcome{skip: true, eid: eid}
	}
	compiledMap := compiledWithRuntime
	if _, has := templateIDToApp[eid]; has {
		compiledMap = compiledNoRuntime
	}
	matching, matchErr := refOwnershipMatchingAppsCompiledWithOptions(
		e, appOrder, compiledMap, workloadClosure, refOwnershipMatchOptions{
			allowFallback: func(candidate entity.Entity) bool {
				return refOwnershipFallbackAllowed(candidate, nil, templateIDToApp)
			},
		})
	if matchErr != nil {
		return refOwnershipLiveAssignOutcome{err: matchErr}
	}
	owner, ok, recErr := reconcileClusterOwnership(templateIDToApp, eid, matching)
	if recErr != nil {
		if errors.Id(recErr) != errors.ErrUninstallAmbiguousRefOwnership {
			return refOwnershipLiveAssignOutcome{err: recErr}
		}
		ambiguous := matching.UnsortedList()
		slices.SortFunc(ambiguous, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })
		return refOwnershipLiveAssignOutcome{ambiguous: true, eid: eid, ambiguousApps: ambiguous}
	}
	if ok {
		return refOwnershipLiveAssignOutcome{assign: true, eid: eid, owner: owner}
	}
	return refOwnershipLiveAssignOutcome{skip: true, eid: eid}
}

func refOwnershipRunLiveResourceAssignment(
	l log.Logger,
	liveCluster entity.Entities,
	kubernetesMinor int,
	templateIDToApp map[types.Id]types.AppId,
	appOrder []types.AppId,
	compiledNoRuntime map[types.AppId][]compiledRefOwnershipPredicate,
	compiledWithRuntime map[types.AppId][]compiledRefOwnershipPredicate,
	workloadClosure workloadclosure.MatchInput,
	onFinding ReviewFindingCallback,
	workers int,
	progress log.Progress,
	task log.ProgressTask,
	assignment map[types.Id]types.AppId,
	refPredAmbiguous sets.Set[types.Id],
) (int, error) {
	nItems := len(liveCluster.Items)
	if nItems == 0 {
		return 0, nil
	}
	if workers <= 1 {
		var nFindings int
		for i, e := range liveCluster.Items {
			if progress != nil && task != nil && nItems > 0 &&
				(i%refOwnershipLiveProgressEvery == 0 || i == nItems-1) {
				task.SetDetail(k8s.TruncateFooterDetail(fmt.Sprintf(
					"assign live resources — %d / %d", i+1, nItems)))
			}
			o := refOwnershipScanOneLiveItem(e, kubernetesMinor, templateIDToApp, appOrder, compiledNoRuntime, compiledWithRuntime, workloadClosure)
			if o.err != nil {
				return nFindings, o.err
			}
			if o.skip {
				continue
			}
			if o.ambiguous {
				refPredAmbiguous.Insert(o.eid)
				msg := fmt.Sprintf("%s: resource %s matches ref ownership of multiple apps %v; fix ref predicates so exactly one app owns it",
					RefOwnershipAmbiguousClusterOnlyFinding, o.eid, o.ambiguousApps)
				if err := onFinding(ReviewFinding{Target: o.eid, Message: msg}); err != nil {
					return nFindings, err
				}
				nFindings++
				continue
			}
			if o.assign {
				assignment[o.eid] = o.owner
			}
		}
		return nFindings, nil
	}

	outcomes := make([]refOwnershipLiveAssignOutcome, nItems)
	var subBar log.Progress
	var subTask log.ProgressTask
	var foot *refOwnershipWorkerStatusLines
	if progress != nil && workers > 1 {
		sb, perr := l.NewProgress("cluster review · assign live resources", nItems)
		if perr != nil {
			return 0, perr
		}
		if sb != nil {
			subBar = sb
			subTask = subBar.NewTask("")
			defer func() { _ = subBar.Close() }()
			foot = refOwnershipOpenWorkerStatusLines(subBar, workers)
		}
	}
	if foot == nil && progress != nil && workers > 1 {
		foot = refOwnershipOpenWorkerStatusLines(progress, workers)
	}
	defer refOwnershipCloseWorkerStatusLines(foot)
	workCh := make(chan int, workers*2)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	var completed int64

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			for i := range workCh {
				errMu.Lock()
				fErr := firstErr
				errMu.Unlock()
				if fErr != nil {
					return
				}
				e := liveCluster.Items[i]
				eid, idErr := e.Id()
				if foot != nil && idErr == nil {
					foot.set(workerIdx, fmt.Sprintf(
						"worker %d · %s · %d / %d", workerIdx+1, string(eid), i+1, nItems))
				}
				o := refOwnershipScanOneLiveItem(e, kubernetesMinor, templateIDToApp, appOrder, compiledNoRuntime, compiledWithRuntime, workloadClosure)
				outcomes[i] = o
				if o.err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = o.err
					}
					errMu.Unlock()
				}
				d := atomic.AddInt64(&completed, 1)
				if subBar != nil {
					subBar.Advance(int(d), nItems)
					if subTask != nil &&
						(d%int64(refOwnershipLiveProgressEvery) == 0 || d == int64(nItems)) {
						subTask.SetDetail(k8s.TruncateFooterDetail(fmt.Sprintf(
							"completed %d / %d", d, nItems)))
					}
					continue
				}
				if task != nil && progress != nil {
					if d%int64(refOwnershipLiveProgressEvery) == 0 || d == int64(nItems) {
						task.SetDetail(k8s.TruncateFooterDetail(fmt.Sprintf(
							"assign live resources — %d / %d", d, nItems)))
					}
				}
			}
		}(w)
	}
	for i := 0; i < nItems; i++ {
		workCh <- i
	}
	close(workCh)
	wg.Wait()
	if firstErr != nil {
		return 0, firstErr
	}
	var nFindings int
	for i := 0; i < nItems; i++ {
		o := outcomes[i]
		if o.err != nil {
			return nFindings, o.err
		}
		if o.skip {
			continue
		}
		if o.ambiguous {
			refPredAmbiguous.Insert(o.eid)
			msg := fmt.Sprintf("%s: resource %s matches ref ownership of multiple apps %v; fix ref predicates so exactly one app owns it",
				RefOwnershipAmbiguousClusterOnlyFinding, o.eid, o.ambiguousApps)
			if err := onFinding(ReviewFinding{Target: o.eid, Message: msg}); err != nil {
				return nFindings, err
			}
			nFindings++
			continue
		}
		if o.assign {
			assignment[o.eid] = o.owner
		}
	}
	if task != nil && progress != nil {
		task.SetDetail(k8s.TruncateFooterDetail(fmt.Sprintf(
			"assign live resources — %d / %d", nItems, nItems)))
	}
	return nFindings, nil
}

// refOwnershipHasLiveOwnerInInventory reports whether any metadata.ownerReferences UID resolves to
// another entity in the live cluster snapshot. Those objects are not roots of an ownership chain
// for unassigned reporting.
func refOwnershipHasLiveOwnerInInventory(
	e entity.Entity,
	liveUidMap map[types.Uid]entity.Entity,
	key types.EntityKeyUnstructured,
) bool {
	ownerUids := e.OwnerUids(key)
	if ownerUids == nil || ownerUids.Len() == 0 {
		return false
	}
	for uid := range ownerUids {
		if _, ok := liveUidMap[uid]; ok {
			return true
		}
	}
	return false
}
