package commands

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"

	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/k8s"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
	"k8s.io/apimachinery/pkg/util/sets"
)

// WorkloadNamespace returns the Kubernetes namespace string for a template or cluster entity:
// namespaced resources use metadata.namespace; Namespace objects use metadata.name.
func WorkloadNamespace(e entity.Entity) (types.Namespace, bool) {
	ns, err := e.Namespace()
	if err == nil && ns != "" {
		return ns, true
	}
	gvk, err := e.GVKString()
	if err != nil {
		return "", false
	}
	if gvk == types.KubernetesGvkV1Namespace {
		name, err := e.Name()
		if err != nil {
			return "", false
		}
		return types.Namespace(name), true
	}
	return "", false
}

// TemplateAppsByNamespace maps each workload namespace to the set of apps that render at least
// one template entity into that namespace (full-cluster per-app render).
func TemplateAppsByNamespace(perAppRendered map[types.AppId]entity.Entities) map[types.Namespace]sets.Set[types.AppId] {
	result := make(map[types.Namespace]sets.Set[types.AppId])
	for appId, ents := range perAppRendered {
		for _, e := range ents.Items {
			ns, ok := WorkloadNamespace(e)
			if !ok {
				continue
			}
			if result[ns] == nil {
				result[ns] = sets.New[types.AppId]()
			}
			result[ns].Insert(appId)
		}
	}
	return result
}

// templateResourceIDToApp maps each template entity id to the app whose standalone render
// contains that resource. Duplicate ids across apps must not occur (validated before uninstall).
func templateResourceIDToApp(perAppRendered map[types.AppId]entity.Entities) map[types.Id]types.AppId {
	out := make(map[types.Id]types.AppId)
	for appId, ents := range perAppRendered {
		for _, e := range ents.Items {
			id, err := e.Id()
			if err != nil {
				continue
			}
			out[id] = appId
		}
	}
	return out
}

type compiledRefOwnershipPredicate struct {
	pred   cel.Predicate
	source types.RefOwnershipPredicateLine
}

type refOwnershipMatchOptions struct {
	skipApp       *types.AppId
	allowFallback func(entity.Entity) bool
}

func refOwnershipEffectivePriority(line types.RefOwnershipPredicateLine) int {
	return line.Priority
}

type assignmentObserver func(id types.Id, app types.AppId)

type ownerRefChildIndex struct {
	childrenByOwner  map[types.Id][]types.Id
	indexByID        map[types.Id]int
	parentIDsByChild map[types.Id]sets.Set[types.Id]
}

// AssignClusterEntitiesToAtMostOneAppByRefsInput configures the live-cluster ownership assignment
// pipeline used by the resource model and related ref-ownership callers.
//
// Internally, the assignment walk is intentionally fast in conflict-free inventories: it records
// only which entity IDs are ambiguous and which apps were involved, and it stops evaluating
// further ref predicates for an entity as soon as a second non-builtin app proves that the entity
// is ambiguous. Detailed per-app reasons are collected only for ConflictDetailIDs.
//
// The exported AssignClusterEntitiesToAtMostOneAppByRefs wrapper automatically feeds detected
// ambiguous IDs back into this mechanism so public callers receive detailed reasons on conflicts.
type AssignClusterEntitiesToAtMostOneAppByRefsInput struct {
	Cluster         *hydra.Cluster
	ResourceModel   *ResourceModel
	ClusterEntities entity.Entities

	AllAppIDs  sets.Set[types.AppId]
	WeakAppIDs sets.Set[types.AppId]

	PerAppRendered    map[types.AppId]entity.Entities
	RenderedAllApps   entity.Entities
	MergedInspectRefs []types.Ref

	// ConflictDetailIDs requests detailed ambiguous-app reasons only for the listed cluster entity
	// IDs during the internal fast-path assignment pass.
	ConflictDetailIDs []types.Id
	// FocusEntityIDs restricts reassignment work to the listed cluster entity IDs while preserving
	// all other InitialAssignment entries as fixed context. Empty means evaluate the full inventory.
	FocusEntityIDs []types.Id

	InitialAssignment   map[types.Id]types.AppId
	Progress            log.Progress
	ProgressPrefixSteps int
	ProgressGrandTotal  int
	Parallel            int
	NetworkMode         types.HelmNetworkMode

	aggregateProgress *resourceModelProgressTracker
	splitProgress     bool
}

type splitAssignProgress struct {
	l         log.Logger
	aggregate *resourceModelProgressTracker
	step      resourceModelProgressStep
	bar       log.Progress
	task      log.ProgressTask
	label     string
	total     int
	done      int
	finished  bool
}

func newSplitAssignProgress(l log.Logger, aggregate *resourceModelProgressTracker) *splitAssignProgress {
	return &splitAssignProgress{l: l, aggregate: aggregate}
}

func (p *splitAssignProgress) Start(step resourceModelProgressStep, total int) error {
	p.Close()
	p.step = step
	p.label = resourceModelProgressStepLabel(step)
	p.total = total
	p.done = 0
	p.finished = false
	if total <= 0 {
		return nil
	}
	bar, err := p.l.NewProgress(p.label, total)
	if err != nil {
		return err
	}
	p.bar = bar
	if bar != nil {
		p.task = bar.NewTask("")
	}
	return nil
}

func (p *splitAssignProgress) SetDetail(detail string) {
	if p == nil || p.task == nil {
		return
	}
	p.task.SetDetail(k8s.TruncateFooterDetail(detail))
}

func (p *splitAssignProgress) Advance(detail string) {
	if p == nil {
		return
	}
	if p.total > 0 {
		p.done++
		if p.bar != nil {
			p.bar.Advance(p.done, p.total)
		}
		if p.done >= p.total {
			p.finished = true
		}
	}
	p.SetDetail(detail)
}

func (p *splitAssignProgress) Close() {
	if p == nil {
		return
	}
	if p.finished && p.aggregate != nil {
		p.aggregate.AdvanceStep(p.step, p.label)
	}
	if p.task != nil {
		_ = p.task.Close()
		p.task = nil
	}
	if p.bar != nil {
		_ = p.bar.Close()
		p.bar = nil
	}
	p.label = ""
	p.step = 0
	p.total = 0
	p.done = 0
	p.finished = false
}

func pendingRefOwnershipEntityIndexes(
	clusterInNamespaces entity.Entities,
	assignment map[types.Id]types.AppId,
	ambiguousIDs sets.Set[types.Id],
	focusIDs sets.Set[types.Id],
) ([]int, error) {
	indexes := make([]int, 0, clusterInNamespaces.Len())
	for i, e := range clusterInNamespaces.Items {
		eid, err := e.Id()
		if err != nil {
			return nil, err
		}
		if len(focusIDs) > 0 && !focusIDs.Has(eid) {
			continue
		}
		if _, has := assignment[eid]; has {
			continue
		}
		if ambiguousIDs.Has(eid) {
			continue
		}
		indexes = append(indexes, i)
	}
	return indexes, nil
}

func pendingRefOwnershipEntityIndexesRootFirst(
	clusterInNamespaces entity.Entities,
	assignment map[types.Id]types.AppId,
	ambiguousIDs sets.Set[types.Id],
	focusIDs sets.Set[types.Id],
	childIndex ownerRefChildIndex,
) ([]int, error) {
	indexes, err := pendingRefOwnershipEntityIndexes(clusterInNamespaces, assignment, ambiguousIDs, focusIDs)
	if err != nil {
		return nil, err
	}
	return sortRefOwnershipEntityIndexesRootFirst(indexes, clusterInNamespaces, assignment, ambiguousIDs, childIndex)
}

func allRefOwnershipEntityIndexesRootFirst(
	clusterInNamespaces entity.Entities,
	assignment map[types.Id]types.AppId,
	ambiguousIDs sets.Set[types.Id],
	focusIDs sets.Set[types.Id],
	childIndex ownerRefChildIndex,
) ([]int, error) {
	indexes := make([]int, 0, clusterInNamespaces.Len())
	for i, e := range clusterInNamespaces.Items {
		eid, err := e.Id()
		if err != nil {
			return nil, err
		}
		if len(focusIDs) > 0 && !focusIDs.Has(eid) {
			continue
		}
		if ambiguousIDs.Has(eid) {
			continue
		}
		indexes = append(indexes, i)
	}
	return sortRefOwnershipEntityIndexesRootFirst(indexes, clusterInNamespaces, assignment, ambiguousIDs, childIndex)
}

func sortRefOwnershipEntityIndexesRootFirst(
	indexes []int,
	clusterInNamespaces entity.Entities,
	assignment map[types.Id]types.AppId,
	ambiguousIDs sets.Set[types.Id],
	childIndex ownerRefChildIndex,
) ([]int, error) {
	if len(indexes) <= 1 {
		return indexes, nil
	}
	depthMemo := make(map[types.Id]int, len(indexes))
	visiting := sets.New[types.Id]()
	var depth func(id types.Id) int
	depth = func(id types.Id) int {
		if d, ok := depthMemo[id]; ok {
			return d
		}
		if visiting.Has(id) {
			return 0
		}
		visiting.Insert(id)
		maxChildDepth := -1
		for _, child := range childIndex.childrenByOwner[id] {
			if _, assigned := assignment[child]; assigned {
				continue
			}
			if ambiguousIDs.Has(child) {
				continue
			}
			cd := depth(child)
			if cd > maxChildDepth {
				maxChildDepth = cd
			}
		}
		visiting.Delete(id)
		d := maxChildDepth + 1
		depthMemo[id] = d
		return d
	}
	idByIndex := make(map[int]types.Id, len(indexes))
	for _, i := range indexes {
		id, idErr := clusterInNamespaces.Items[i].Id()
		if idErr != nil {
			return nil, idErr
		}
		idByIndex[i] = id
	}
	slices.SortFunc(indexes, func(a, b int) int {
		da := depth(idByIndex[a])
		db := depth(idByIndex[b])
		if da != db {
			return cmp.Compare(db, da)
		}
		return cmp.Compare(string(idByIndex[a]), string(idByIndex[b]))
	})
	return indexes, nil
}

func buildOwnerRefChildIndex(clusterInNamespaces entity.Entities, key types.EntityKeyUnstructured) (ownerRefChildIndex, error) {
	idx := ownerRefChildIndex{
		childrenByOwner:  make(map[types.Id][]types.Id),
		indexByID:        make(map[types.Id]int, clusterInNamespaces.Len()),
		parentIDsByChild: make(map[types.Id]sets.Set[types.Id]),
	}
	uidToID := make(map[types.Uid]types.Id, clusterInNamespaces.Len())
	for i, e := range clusterInNamespaces.Items {
		id, err := e.Id()
		if err != nil {
			return idx, err
		}
		idx.indexByID[id] = i
		if u, ok := e.Unstructured(key); ok && u.GetUID() != "" {
			uidToID[types.Uid(u.GetUID())] = id
		}
	}
	for _, e := range clusterInNamespaces.Items {
		childID, err := e.Id()
		if err != nil {
			return idx, err
		}
		u, ok := e.Unstructured(key)
		if !ok {
			continue
		}
		refs := u.GetOwnerReferences()
		if len(refs) == 0 {
			continue
		}
		ownerNs, nsOK := WorkloadNamespace(e)
		if !nsOK {
			ownerNs = ""
		}
		parentIDs := sets.New[types.Id]()
		for _, ref := range refs {
			if ref.UID != "" {
				if parentID, ok := uidToID[types.Uid(ref.UID)]; ok {
					parentIDs.Insert(parentID)
				}
			}
			if ref.APIVersion == "" || ref.Kind == "" || ref.Name == "" {
				continue
			}
			parentID, idErr := clusterResourceIDFromOwnerReference(string(ref.APIVersion), string(ref.Kind), string(ref.Name), ownerNs)
			if idErr != nil {
				continue
			}
			if _, ok := idx.indexByID[parentID]; ok {
				parentIDs.Insert(parentID)
			}
		}
		for parentID := range parentIDs {
			idx.childrenByOwner[parentID] = append(idx.childrenByOwner[parentID], childID)
		}
		if parentIDs.Len() > 0 {
			idx.parentIDsByChild[childID] = parentIDs
		}
	}
	for parentID := range idx.childrenByOwner {
		slices.SortFunc(idx.childrenByOwner[parentID], func(a, b types.Id) int {
			return cmp.Compare(string(a), string(b))
		})
	}
	return idx, nil
}

func assignOwnerRefDescendants(
	root types.Id,
	app types.AppId,
	assignment map[types.Id]types.AppId,
	ambiguousIDs sets.Set[types.Id],
	childIndex ownerRefChildIndex,
	observer assignmentObserver,
) {
	queue := append([]types.Id(nil), childIndex.childrenByOwner[root]...)
	seen := sets.New[types.Id](root)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen.Has(id) {
			continue
		}
		seen.Insert(id)
		if ambiguousIDs.Has(id) {
			continue
		}
		if parents := childIndex.parentIDsByChild[id]; parents.Len() != 1 {
			continue
		}
		existing, assigned := assignment[id]
		if assigned && existing != app {
			continue
		}
		if !assigned {
			assignment[id] = app
			if observer != nil {
				observer(id, app)
			}
		}
		queue = append(queue, childIndex.childrenByOwner[id]...)
	}
}

func mergeRefOwnershipPredicateLinePriorityBandMaps(
	nonNegativePriority map[types.AppId][]types.RefOwnershipPredicateLine,
	negativePriority map[types.AppId][]types.RefOwnershipPredicateLine,
	appOrder []types.AppId,
) map[types.AppId][]types.RefOwnershipPredicateLine {
	out := make(map[types.AppId][]types.RefOwnershipPredicateLine)
	for _, appId := range appOrder {
		var lines []types.RefOwnershipPredicateLine
		lines = append(lines, nonNegativePriority[appId]...)
		lines = append(lines, negativePriority[appId]...)
		if len(lines) > 0 {
			out[appId] = lines
		}
	}
	return out
}

func compileRefOwnershipPredicateLines(
	appOrder []types.AppId,
	predicateLines map[types.AppId][]types.RefOwnershipPredicateLine,
	envByApp map[types.AppId]cel.Env,
) (map[types.AppId][]compiledRefOwnershipPredicate, error) {
	out := make(map[types.AppId][]compiledRefOwnershipPredicate)
	for _, appId := range appOrder {
		lines := predicateLines[appId]
		if len(lines) == 0 {
			continue
		}
		env, ok := envByApp[appId]
		if !ok {
			continue
		}
		compiled := make([]compiledRefOwnershipPredicate, 0, len(lines))
		for _, ln := range lines {
			celStr := strings.TrimSpace(ln.Cel)
			if celStr == "" {
				continue
			}
			prog, err := env.CompilePredicate(types.CelPredicate(celStr))
			if err != nil {
				return nil, fmt.Errorf("app %q ref-ownership predicate: %w", appId, err)
			}
			compiled = append(compiled, compiledRefOwnershipPredicate{pred: prog, source: ln})
		}
		if len(compiled) > 0 {
			out[appId] = compiled
		}
	}
	return out, nil
}

func assignmentReasonEqual(a, b AssignmentReason) bool {
	if a.Kind != b.Kind ||
		a.Preset != b.Preset ||
		a.EventRef != b.EventRef ||
		!slices.Equal(a.PresetIDs, b.PresetIDs) ||
		!slices.Equal(a.PresetRules, b.PresetRules) ||
		!slices.Equal(a.OwnerRefs, b.OwnerRefs) ||
		!slices.Equal(a.EventSubjects, b.EventSubjects) {
		return false
	}
	switch {
	case a.RefOwnership == nil && b.RefOwnership == nil:
		return true
	case a.RefOwnership == nil || b.RefOwnership == nil:
		return false
	default:
		if a.RefOwnership.Cel != b.RefOwnership.Cel {
			return false
		}
		if a.RefOwnership.Priority != b.RefOwnership.Priority {
			return false
		}
		switch {
		case a.RefOwnership.Source == nil && b.RefOwnership.Source == nil:
			return true
		case a.RefOwnership.Source == nil || b.RefOwnership.Source == nil:
			return false
		default:
			return a.RefOwnership.Source.Kind == b.RefOwnership.Source.Kind &&
				a.RefOwnership.Source.GroupName == b.RefOwnership.Source.GroupName &&
				a.RefOwnership.Source.BlockPath == b.RefOwnership.Source.BlockPath &&
				slices.Equal(a.RefOwnership.Source.Sources, b.RefOwnership.Source.Sources)
		}
	}
}

func ownerRefReasonForEntity(
	e entity.Entity,
	key types.EntityKeyUnstructured,
	uidMap map[types.Uid]entity.Entity,
) AssignmentReason {
	reason := AssignmentReason{Kind: AssignmentReasonKindAssignedViaOwnerRef}
	u, ok := e.Unstructured(key)
	if !ok {
		return reason
	}
	refs := u.GetOwnerReferences()
	if len(refs) == 0 {
		return reason
	}
	ownerNs, nsOK := WorkloadNamespace(e)
	if !nsOK {
		ownerNs = ""
	}
	seen := sets.New[types.Id]()
	for _, ref := range refs {
		if ref.UID != "" {
			if parent, ok := uidMap[types.Uid(ref.UID)]; ok {
				pid, err := parent.Id()
				if err == nil && !seen.Has(pid) {
					seen.Insert(pid)
					reason.OwnerRefs = append(reason.OwnerRefs, pid)
				}
			}
		}
		ownerID, err := clusterResourceIDFromOwnerReference(string(ref.APIVersion), string(ref.Kind), string(ref.Name), ownerNs)
		if err != nil || seen.Has(ownerID) {
			continue
		}
		seen.Insert(ownerID)
		reason.OwnerRefs = append(reason.OwnerRefs, ownerID)
	}
	slices.SortFunc(reason.OwnerRefs, func(a, b types.Id) int { return cmp.Compare(string(a), string(b)) })
	return reason
}

func refOwnershipReasonForLine(line types.RefOwnershipPredicateLine) AssignmentReason {
	cp := line
	if cp.Source != nil {
		src := *cp.Source
		src.Sources = append([]string{}, src.Sources...)
		cp.Source = &src
	}
	return AssignmentReason{
		Kind:         AssignmentReasonKindAssignedViaRefOwnership,
		RefOwnership: &cp,
	}
}

func filterAppsByHighestPriority(matching sets.Set[types.AppId], bestPriorityByApp map[types.AppId]int) sets.Set[types.AppId] {
	if matching.Len() <= 1 {
		return matching
	}
	best := 0
	haveBest := false
	for app := range matching {
		p := bestPriorityByApp[app]
		if !haveBest || p > best {
			best = p
			haveBest = true
		}
	}
	if !haveBest {
		return matching
	}
	filtered := sets.New[types.AppId]()
	for app := range matching {
		if bestPriorityByApp[app] == best {
			filtered.Insert(app)
		}
	}
	return filtered
}

func filterAssignmentReasonsByPriority(
	reasonsByApp map[types.AppId][]AssignmentReason,
	bestPriorityByApp map[types.AppId]int,
) map[types.AppId][]AssignmentReason {
	if len(reasonsByApp) == 0 {
		return reasonsByApp
	}
	filtered := make(map[types.AppId][]AssignmentReason, len(reasonsByApp))
	for app, reasons := range reasonsByApp {
		best := bestPriorityByApp[app]
		for _, reason := range reasons {
			if reason.RefOwnership == nil || refOwnershipEffectivePriority(*reason.RefOwnership) == best {
				filtered[app] = append(filtered[app], reason)
			}
		}
	}
	return filtered
}

func appendAssignmentReasonUnique(reasons []AssignmentReason, reason AssignmentReason) []AssignmentReason {
	for _, existing := range reasons {
		if assignmentReasonEqual(existing, reason) {
			return reasons
		}
	}
	return append(reasons, reason)
}

func assignWithDetailedReasons(
	assignment map[types.Id]types.AppId,
	assignmentReasons map[types.Id]map[types.AppId][]AssignmentReason,
	id types.Id,
	app types.AppId,
	reasons []AssignmentReason,
) {
	assignment[id] = app
	if len(reasons) == 0 {
		return
	}
	reasonsByApp := assignmentReasons[id]
	if reasonsByApp == nil {
		reasonsByApp = map[types.AppId][]AssignmentReason{}
	}
	for _, reason := range reasons {
		reasonsByApp[app] = appendAssignmentReasonUnique(reasonsByApp[app], reason)
	}
	assignmentReasons[id] = reasonsByApp
}

func refOwnershipEventReason(label string, subjectIDs []types.Id) AssignmentReason {
	ids := append([]types.Id{}, subjectIDs...)
	slices.SortFunc(ids, func(a, b types.Id) int { return cmp.Compare(string(a), string(b)) })
	return AssignmentReason{
		Kind:          AssignmentReasonKindAssignedViaRefOwnership,
		EventRef:      label,
		EventSubjects: ids,
	}
}

func refOwnershipSubjectIDs(subjects []entity.Entity) ([]types.Id, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	out := make([]types.Id, 0, len(subjects))
	for _, subject := range subjects {
		sid, err := subject.Id()
		if err != nil {
			return nil, err
		}
		out = append(out, sid)
	}
	return out, nil
}

func refOwnershipDetailedEventReasonsForSubjects(
	label string,
	subjects []entity.Entity,
	apps sets.Set[types.AppId],
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	closure workloadclosure.MatchInput,
	opts refOwnershipMatchOptions,
) (map[types.AppId][]AssignmentReason, error) {
	reasonsByApp := make(map[types.AppId][]AssignmentReason, apps.Len())
	subjectIDs, err := refOwnershipSubjectIDs(subjects)
	if err != nil {
		return nil, err
	}
	routeReason := refOwnershipEventReason(label, subjectIDs)
	for app := range apps {
		reasonsByApp[app] = append(reasonsByApp[app], routeReason)
	}
	if len(subjects) == 0 {
		return reasonsByApp, nil
	}
	subjectMatches, subjectReasons, err := refOwnershipMatchingAppsForSubjects(subjects, appOrder, compiledByApp, closure, opts)
	if err != nil {
		return nil, err
	}
	for app := range subjectMatches {
		reasonsByApp[app] = append(reasonsByApp[app], subjectReasons[app]...)
	}
	return reasonsByApp, nil
}

func refOwnershipDetailedEventAppsFromAssignedSubjects(
	e entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	closure workloadclosure.MatchInput,
	assignment map[types.Id]types.AppId,
	templateIDToApp map[types.Id]types.AppId,
	allowTemplateOrphanFallback bool,
	opts refOwnershipMatchOptions,
) (sets.Set[types.AppId], map[types.AppId][]AssignmentReason, bool, error) {
	relatedSubjects, err := refOwnershipEventSubjectsForLabel(e, closure, "related", "")
	if err != nil {
		return nil, nil, false, err
	}
	relatedApps, err := refOwnershipAppsForSubjects(relatedSubjects, assignment, templateIDToApp)
	if err != nil {
		return nil, nil, false, err
	}
	relatedOrphanApps := sets.New[types.AppId]()
	if allowTemplateOrphanFallback {
		relatedOrphanApps, err = refOwnershipOrphanEventAppsForSubjects(relatedSubjects, templateIDToApp)
		if err != nil {
			return nil, nil, false, err
		}
	}
	if relatedApps.Len() > 0 || relatedOrphanApps.Len() > 0 {
		combined := sets.New[types.AppId]()
		combined.Insert(relatedApps.UnsortedList()...)
		combined.Insert(relatedOrphanApps.UnsortedList()...)
		if hasNonBuiltinApp(combined) {
			combined = preferSpecificAppsOverBuiltins(combined)
		} else if relatedOrphanApps.Len() == 0 {
			return nil, nil, true, nil
		}
		reasonsByApp, err := refOwnershipDetailedEventReasonsForSubjects(
			"related", relatedSubjects, combined, appOrder, compiledByApp, closure, opts)
		if err != nil {
			return nil, nil, false, err
		}
		return combined, reasonsByApp, true, nil
	}

	regardingSubjects, err := refOwnershipEventSubjectsForLabel(e, closure, "", types.RefTypeRegarding)
	if err != nil {
		return nil, nil, false, err
	}
	regardingApps, err := refOwnershipAppsForSubjects(regardingSubjects, assignment, templateIDToApp)
	if err != nil {
		return nil, nil, false, err
	}
	regardingOrphanApps := sets.New[types.AppId]()
	if allowTemplateOrphanFallback {
		regardingOrphanApps, err = refOwnershipOrphanEventAppsForSubjects(regardingSubjects, templateIDToApp)
		if err != nil {
			return nil, nil, false, err
		}
	}
	if regardingApps.Len() > 0 || regardingOrphanApps.Len() > 0 {
		combined := sets.New[types.AppId]()
		combined.Insert(regardingApps.UnsortedList()...)
		combined.Insert(regardingOrphanApps.UnsortedList()...)
		if hasNonBuiltinApp(combined) {
			combined = preferSpecificAppsOverBuiltins(combined)
		} else if regardingOrphanApps.Len() == 0 {
			return nil, nil, true, nil
		}
		reasonsByApp, err := refOwnershipDetailedEventReasonsForSubjects(
			"regarding", regardingSubjects, combined, appOrder, compiledByApp, closure, opts)
		if err != nil {
			return nil, nil, false, err
		}
		return combined, reasonsByApp, true, nil
	}

	if allowTemplateOrphanFallback {
		workloadApps, foundWorkloadRefs, err := refOwnershipEventAppsFromWorkloadRegardingAnchors(
			e, closure, assignment, templateIDToApp)
		if err != nil {
			return nil, nil, false, err
		}
		if workloadApps.Len() > 0 {
			if hasNonBuiltinApp(workloadApps) {
				workloadApps = preferSpecificAppsOverBuiltins(workloadApps)
			}
			eid, err := e.Id()
			if err != nil {
				return nil, nil, false, err
			}
			reasonsByApp := make(map[types.AppId][]AssignmentReason, workloadApps.Len())
			reason := refOwnershipEventReason("workloadRegardingEvent", []types.Id{eid})
			for app := range workloadApps {
				reasonsByApp[app] = append(reasonsByApp[app], reason)
			}
			return workloadApps, reasonsByApp, true, nil
		}
		if foundWorkloadRefs {
			return nil, nil, true, nil
		}
	}
	return nil, nil, len(relatedSubjects) > 0 || len(regardingSubjects) > 0, nil
}

func assignmentReasonSubset(existing []AssignmentReason, candidates []AssignmentReason) bool {
	if len(candidates) == 0 {
		return true
	}
	for _, candidate := range candidates {
		found := false
		for _, reason := range existing {
			if assignmentReasonEqual(reason, candidate) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func assignmentReasonMapSubset(existing []AssignmentReason, candidates map[types.AppId][]AssignmentReason) map[types.AppId][]AssignmentReason {
	if len(candidates) == 0 {
		return nil
	}
	filtered := make(map[types.AppId][]AssignmentReason)
	for app, reasons := range candidates {
		if assignmentReasonSubset(existing, reasons) {
			continue
		}
		filtered[app] = reasons
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func buildPresetClosureForAssignment(
	cluster *hydra.Cluster,
	clusterEntities entity.Entities,
	renderedAllApps entity.Entities,
	mergedInspectRefs []types.Ref,
) (workloadclosure.MatchInput, error) {
	closure := workloadclosure.EmptyMatchInput(types.KeyClusterEntity)
	if clusterEntities.Len() == 0 {
		return closure, nil
	}
	buildMergedClosure := func(
		l log.Logger,
		preferredVersions map[types.GroupKindKey]types.Version,
	) (workloadclosure.MatchInput, error) {
		refsForClosure := filterMergedInspectRefsForOwnershipClosure(mergedInspectRefs)
		if len(refsForClosure) == 0 {
			clusterRefs, rerr := references.Refs(
				l,
				clusterEntities,
				types.KeyClusterEntity,
				nil,
				entity.Entities{},
				entity.Entities{},
				preferredVersions,
				nil,
			)
			if rerr != nil {
				return workloadclosure.MatchInput{}, rerr
			}
			if renderedAllApps.Len() == 0 {
				refsForClosure = clusterRefs
			} else {
				templateRefs, terr := references.Refs(
					l,
					renderedAllApps,
					types.KeyTemplateEntity,
					nil,
					entity.Entities{},
					clusterEntities,
					preferredVersions,
					nil,
				)
				if terr != nil {
					return workloadclosure.MatchInput{}, terr
				}
				refsForClosure = references.MergeRefLists(templateRefs, clusterRefs)
				refsForClosure = references.CanonicalizeOwnerRefTargetsToClusterIDs(refsForClosure, clusterEntities)
				refsForClosure = references.MergeRefLists(refsForClosure)
			}
		}
		return WorkloadClosureMatchInputFromMergedInventory(refsForClosure, clusterEntities, renderedAllApps)
	}
	if cluster != nil {
		cluster.ResetPreferredVersionsCache()
		pref, err := cluster.PreferredVersions(func() (types.ScopeInfoMap, error) {
			return ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity, types.CrdModeSilent)
		})
		if err != nil {
			return workloadclosure.MatchInput{}, err
		}
		return buildMergedClosure(cluster.L(), pref)
	}
	return buildMergedClosure(log.Default(), nil)
}

func preferSpecificAppsOverBuiltins(apps sets.Set[types.AppId]) sets.Set[types.AppId] {
	if apps == nil || apps.Len() == 0 {
		return sets.New[types.AppId]()
	}
	specific := sets.New[types.AppId]()
	for app := range apps {
		if !app.IsPresetApp() {
			specific.Insert(app)
		}
	}
	if specific.Len() > 0 {
		return specific
	}
	return apps
}

func hasNonBuiltinApp(apps sets.Set[types.AppId]) bool {
	if apps == nil {
		return false
	}
	for app := range apps {
		if !app.IsPresetApp() {
			return true
		}
	}
	return false
}

func filterAssignmentReasonsByApps(
	reasonsByApp map[types.AppId][]AssignmentReason,
	apps sets.Set[types.AppId],
) map[types.AppId][]AssignmentReason {
	if len(reasonsByApp) == 0 || apps == nil || apps.Len() == 0 {
		return reasonsByApp
	}
	filtered := make(map[types.AppId][]AssignmentReason, apps.Len())
	for app := range apps {
		if reasons, ok := reasonsByApp[app]; ok {
			filtered[app] = reasons
		}
	}
	return filtered
}

// refOwnershipMatchingAppsCompiled returns the set of apps whose compiled ref-ownership predicates
// match the given entity. Used by uninstall, cluster review, and leftover classification.
// When skipApp is non-nil, that app is omitted from matching (cluster review template phase only needs
// other apps to detect ref-vs-template conflicts).
func refOwnershipMatchingAppsCompiled(
	e entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	skipApp *types.AppId,
	closure workloadclosure.MatchInput,
) (sets.Set[types.AppId], error) {
	return refOwnershipMatchingAppsCompiledWithOptions(
		e, appOrder, compiledByApp, closure, refOwnershipMatchOptions{skipApp: skipApp})
}

func refOwnershipMatchingAppsCompiledWithOptions(
	e entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	closure workloadclosure.MatchInput,
	opts refOwnershipMatchOptions,
) (sets.Set[types.AppId], error) {
	matching, _, err := refOwnershipMatchingAppsCompiledWithReasonsWithOptions(
		e, appOrder, compiledByApp, closure, opts)
	return matching, err
}

func refOwnershipFirstMatchingAppCompiledWithOptions(
	e entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	closure workloadclosure.MatchInput,
	opts refOwnershipMatchOptions,
) (types.AppId, bool, error) {
	var (
		bestApp      types.AppId
		bestPriority int
		found        bool
	)
	if relatedSubjects, err := refOwnershipEventSubjectsForLabel(e, closure, "related", ""); err != nil {
		return "", false, err
	} else if len(relatedSubjects) > 0 {
		app, found, err := refOwnershipFirstMatchingAppForSubjects(relatedSubjects, appOrder, compiledByApp, closure, opts)
		if err != nil || found {
			return app, found, err
		}
	}
	if regardingSubjects, err := refOwnershipEventSubjectsForLabel(e, closure, "", types.RefTypeRegarding); err != nil {
		return "", false, err
	} else if len(regardingSubjects) > 0 {
		app, found, err := refOwnershipFirstMatchingAppForSubjects(regardingSubjects, appOrder, compiledByApp, closure, opts)
		if err != nil || found {
			return app, found, err
		}
	}
	for _, appId := range appOrder {
		if opts.skipApp != nil && appId == *opts.skipApp {
			continue
		}
		progs := compiledByApp[appId]
		if len(progs) == 0 {
			continue
		}
		for _, cp := range progs {
			if refOwnershipEffectivePriority(cp.source) < 0 && opts.allowFallback != nil && !opts.allowFallback(e) {
				continue
			}
			ok, evalErr := closure.PredicateMatches(e, cp.pred)
			if evalErr != nil {
				return "", false, evalErr
			}
			if ok {
				priority := refOwnershipEffectivePriority(cp.source)
				if !found || priority > bestPriority {
					bestApp = appId
					bestPriority = priority
					found = true
				}
				break
			}
		}
	}
	return bestApp, found, nil
}

func refOwnershipMatchingAppsCompiledWithReasons(
	e entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	skipApp *types.AppId,
	closure workloadclosure.MatchInput,
) (sets.Set[types.AppId], map[types.AppId][]AssignmentReason, error) {
	return refOwnershipMatchingAppsCompiledWithReasonsWithOptions(
		e, appOrder, compiledByApp, closure, refOwnershipMatchOptions{skipApp: skipApp})
}

func refOwnershipMatchingAppsCompiledWithReasonsWithOptions(
	e entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	closure workloadclosure.MatchInput,
	opts refOwnershipMatchOptions,
) (sets.Set[types.AppId], map[types.AppId][]AssignmentReason, error) {
	if relatedSubjects, err := refOwnershipEventSubjectsForLabel(e, closure, "related", ""); err != nil {
		return nil, nil, err
	} else if len(relatedSubjects) > 0 {
		matching, reasonsByApp, err := refOwnershipMatchingAppsForSubjects(
			relatedSubjects, appOrder, compiledByApp, closure, opts)
		if err != nil {
			return nil, nil, err
		}
		matching = preferSpecificAppsOverBuiltins(matching)
		reasonsByApp = filterAssignmentReasonsByApps(reasonsByApp, matching)
		if matching.Len() > 0 {
			return matching, reasonsByApp, nil
		}
	}
	if regardingSubjects, err := refOwnershipEventSubjectsForLabel(e, closure, "", types.RefTypeRegarding); err != nil {
		return nil, nil, err
	} else if len(regardingSubjects) > 0 {
		matching, reasonsByApp, err := refOwnershipMatchingAppsForSubjects(
			regardingSubjects, appOrder, compiledByApp, closure, opts)
		if err != nil {
			return nil, nil, err
		}
		matching = preferSpecificAppsOverBuiltins(matching)
		reasonsByApp = filterAssignmentReasonsByApps(reasonsByApp, matching)
		if matching.Len() > 0 {
			return matching, reasonsByApp, nil
		}
	}

	matching := sets.New[types.AppId]()
	reasonsByApp := make(map[types.AppId][]AssignmentReason)
	bestPriorityByApp := make(map[types.AppId]int)
	for _, appId := range appOrder {
		if opts.skipApp != nil && appId == *opts.skipApp {
			continue
		}
		progs := compiledByApp[appId]
		if len(progs) == 0 {
			continue
		}
		for _, cp := range progs {
			if refOwnershipEffectivePriority(cp.source) < 0 && opts.allowFallback != nil && !opts.allowFallback(e) {
				continue
			}
			ok, evalErr := closure.PredicateMatches(e, cp.pred)
			if evalErr != nil {
				return nil, nil, evalErr
			}
			if ok {
				matching.Insert(appId)
				priority := refOwnershipEffectivePriority(cp.source)
				prev, seen := bestPriorityByApp[appId]
				if !seen || priority > prev {
					bestPriorityByApp[appId] = priority
				}
				reasonsByApp[appId] = append(reasonsByApp[appId], refOwnershipReasonForLine(cp.source))
			}
		}
	}
	matching = filterAppsByHighestPriority(matching, bestPriorityByApp)
	reasonsByApp = filterAssignmentReasonsByPriority(reasonsByApp, bestPriorityByApp)
	matching = preferSpecificAppsOverBuiltins(matching)
	reasonsByApp = filterAssignmentReasonsByApps(reasonsByApp, matching)
	return matching, reasonsByApp, nil
}

func refOwnershipFirstMatchingAppForSubjects(
	subjects []entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	closure workloadclosure.MatchInput,
	opts refOwnershipMatchOptions,
) (types.AppId, bool, error) {
	for _, appId := range appOrder {
		if opts.skipApp != nil && appId == *opts.skipApp {
			continue
		}
		progs := compiledByApp[appId]
		if len(progs) == 0 {
			continue
		}
		for _, cp := range progs {
			for _, subject := range subjects {
				if refOwnershipEffectivePriority(cp.source) < 0 && opts.allowFallback != nil && !opts.allowFallback(subject) {
					continue
				}
				ok, evalErr := closure.PredicateMatches(subject, cp.pred)
				if evalErr != nil {
					return "", false, evalErr
				}
				if ok {
					return appId, true, nil
				}
			}
		}
	}
	return "", false, nil
}

func refOwnershipMatchingAppsCompiledFast(
	e entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	skipApp *types.AppId,
	closure workloadclosure.MatchInput,
) (sets.Set[types.AppId], error) {
	return refOwnershipMatchingAppsCompiledFastWithOptions(
		e, appOrder, compiledByApp, closure, refOwnershipMatchOptions{skipApp: skipApp})
}

func refOwnershipMatchingAppsCompiledFastWithOptions(
	e entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	closure workloadclosure.MatchInput,
	opts refOwnershipMatchOptions,
) (sets.Set[types.AppId], error) {
	if relatedSubjects, err := refOwnershipEventSubjectsForLabel(e, closure, "related", ""); err != nil {
		return nil, err
	} else if len(relatedSubjects) > 0 {
		matching, err := refOwnershipMatchingAppsForSubjectsFast(
			relatedSubjects, appOrder, compiledByApp, closure, opts)
		if err != nil {
			return nil, err
		}
		matching = preferSpecificAppsOverBuiltins(matching)
		if matching.Len() > 0 {
			return matching, nil
		}
	}
	if regardingSubjects, err := refOwnershipEventSubjectsForLabel(e, closure, "", types.RefTypeRegarding); err != nil {
		return nil, err
	} else if len(regardingSubjects) > 0 {
		matching, err := refOwnershipMatchingAppsForSubjectsFast(
			regardingSubjects, appOrder, compiledByApp, closure, opts)
		if err != nil {
			return nil, err
		}
		matching = preferSpecificAppsOverBuiltins(matching)
		if matching.Len() > 0 {
			return matching, nil
		}
	}

	specific := sets.New[types.AppId]()
	builtins := sets.New[types.AppId]()
	bestPriorityByApp := make(map[types.AppId]int)
	for _, appId := range appOrder {
		if opts.skipApp != nil && appId == *opts.skipApp {
			continue
		}
		progs := compiledByApp[appId]
		if len(progs) == 0 {
			continue
		}
		appMatched := false
		for _, cp := range progs {
			if refOwnershipEffectivePriority(cp.source) < 0 && opts.allowFallback != nil && !opts.allowFallback(e) {
				continue
			}
			ok, evalErr := closure.PredicateMatches(e, cp.pred)
			if evalErr != nil {
				return nil, evalErr
			}
			if ok {
				appMatched = true
				priority := refOwnershipEffectivePriority(cp.source)
				prev, seen := bestPriorityByApp[appId]
				if !seen || priority > prev {
					bestPriorityByApp[appId] = priority
				}
				break
			}
		}
		if !appMatched {
			continue
		}
		if appId.IsPresetApp() {
			if specific.Len() == 0 {
				builtins.Insert(appId)
			}
			continue
		}
		specific.Insert(appId)
	}
	if specific.Len() > 0 {
		return filterAppsByHighestPriority(specific, bestPriorityByApp), nil
	}
	return filterAppsByHighestPriority(builtins, bestPriorityByApp), nil
}

func refOwnershipMatchingAppsForSubjects(
	subjects []entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	closure workloadclosure.MatchInput,
	opts refOwnershipMatchOptions,
) (sets.Set[types.AppId], map[types.AppId][]AssignmentReason, error) {
	matching := sets.New[types.AppId]()
	reasonsByApp := make(map[types.AppId][]AssignmentReason)
	bestPriorityByApp := make(map[types.AppId]int)
	for _, appId := range appOrder {
		if opts.skipApp != nil && appId == *opts.skipApp {
			continue
		}
		progs := compiledByApp[appId]
		if len(progs) == 0 {
			continue
		}
		for _, cp := range progs {
			for _, subject := range subjects {
				if refOwnershipEffectivePriority(cp.source) < 0 && opts.allowFallback != nil && !opts.allowFallback(subject) {
					continue
				}
				ok, evalErr := closure.PredicateMatches(subject, cp.pred)
				if evalErr != nil {
					return nil, nil, evalErr
				}
				if !ok {
					continue
				}
				matching.Insert(appId)
				priority := refOwnershipEffectivePriority(cp.source)
				prev, seen := bestPriorityByApp[appId]
				if !seen || priority > prev {
					bestPriorityByApp[appId] = priority
				}
				reasonsByApp[appId] = append(reasonsByApp[appId], refOwnershipReasonForLine(cp.source))
				break
			}
		}
	}
	matching = filterAppsByHighestPriority(matching, bestPriorityByApp)
	reasonsByApp = filterAssignmentReasonsByPriority(reasonsByApp, bestPriorityByApp)
	return matching, reasonsByApp, nil
}

func refOwnershipMatchingAppsForSubjectsFast(
	subjects []entity.Entity,
	appOrder []types.AppId,
	compiledByApp map[types.AppId][]compiledRefOwnershipPredicate,
	closure workloadclosure.MatchInput,
	opts refOwnershipMatchOptions,
) (sets.Set[types.AppId], error) {
	specific := sets.New[types.AppId]()
	builtins := sets.New[types.AppId]()
	bestPriorityByApp := make(map[types.AppId]int)
	for _, appId := range appOrder {
		if opts.skipApp != nil && appId == *opts.skipApp {
			continue
		}
		progs := compiledByApp[appId]
		if len(progs) == 0 {
			continue
		}
		appMatched := false
		for _, cp := range progs {
			for _, subject := range subjects {
				if refOwnershipEffectivePriority(cp.source) < 0 && opts.allowFallback != nil && !opts.allowFallback(subject) {
					continue
				}
				ok, evalErr := closure.PredicateMatches(subject, cp.pred)
				if evalErr != nil {
					return nil, evalErr
				}
				if ok {
					appMatched = true
					priority := refOwnershipEffectivePriority(cp.source)
					prev, seen := bestPriorityByApp[appId]
					if !seen || priority > prev {
						bestPriorityByApp[appId] = priority
					}
					break
				}
			}
			if appMatched {
				break
			}
		}
		if !appMatched {
			continue
		}
		if appId.IsPresetApp() {
			if specific.Len() == 0 {
				builtins.Insert(appId)
			}
			continue
		}
		specific.Insert(appId)
	}
	if specific.Len() > 0 {
		return filterAppsByHighestPriority(specific, bestPriorityByApp), nil
	}
	return filterAppsByHighestPriority(builtins, bestPriorityByApp), nil
}

func refOwnershipEventSubjectsForLabel(
	e entity.Entity,
	closure workloadclosure.MatchInput,
	label string,
	refType types.RefType,
) ([]entity.Entity, error) {
	eid, err := e.Id()
	if err != nil {
		return nil, err
	}
	gvk, err := e.GVKString()
	if err != nil {
		return nil, err
	}
	if gvk != types.KubernetesGvkEventsK8sIoV1Event && gvk != types.KubernetesGvkV1Event {
		return nil, nil
	}
	refs := closure.RefsByFrom[eid]
	if closure.RefsByFrom == nil {
		for _, ref := range closure.Refs {
			if ref.From == eid {
				refs = append(refs, ref)
			}
		}
	}
	if len(refs) == 0 {
		return nil, nil
	}
	subjects := make([]entity.Entity, 0, len(refs))
	for _, ref := range refs {
		if ref.EndpointType != types.RefEndpointTypeId {
			continue
		}
		if label != "" && !slices.Contains(ref.Labels, label) {
			continue
		}
		if refType != "" && ref.RefType != refType {
			continue
		}
		subject, ok := closure.EntityByID[ref.To]
		if !ok {
			subject, err = workloadclosure.MinimalClusterEntityFromID(ref.To)
			if err != nil {
				return nil, err
			}
		}
		subjects = append(subjects, subject)
	}
	return subjects, nil
}

func refOwnershipFallbackAllowed(
	e entity.Entity,
	assignment map[types.Id]types.AppId,
	templateIDToApp map[types.Id]types.AppId,
) bool {
	eid, err := e.Id()
	if err != nil {
		return false
	}
	if _, has := assignment[eid]; has {
		return false
	}
	if _, has := templateIDToApp[eid]; has {
		return false
	}
	return true
}

func refOwnershipEventAppsFromAssignedSubjects(
	e entity.Entity,
	closure workloadclosure.MatchInput,
	assignment map[types.Id]types.AppId,
	templateIDToApp map[types.Id]types.AppId,
	allowTemplateOrphanFallback bool,
) (sets.Set[types.AppId], bool, error) {
	relatedSubjects, err := refOwnershipEventSubjectsForLabel(e, closure, "related", "")
	if err != nil {
		return nil, false, err
	}
	relatedApps, err := refOwnershipAppsForSubjects(relatedSubjects, assignment, templateIDToApp)
	if err != nil {
		return nil, false, err
	}
	relatedOrphanApps := sets.New[types.AppId]()
	if allowTemplateOrphanFallback {
		relatedOrphanApps, err = refOwnershipOrphanEventAppsForSubjects(relatedSubjects, templateIDToApp)
		if err != nil {
			return nil, false, err
		}
	}
	if relatedApps.Len() > 0 || relatedOrphanApps.Len() > 0 {
		combined := sets.New[types.AppId]()
		combined.Insert(relatedApps.UnsortedList()...)
		combined.Insert(relatedOrphanApps.UnsortedList()...)
		if hasNonBuiltinApp(combined) {
			return preferSpecificAppsOverBuiltins(combined), true, nil
		}
		if relatedOrphanApps.Len() > 0 {
			return combined, true, nil
		}
		return nil, true, nil
	}
	regardingSubjects, err := refOwnershipEventSubjectsForLabel(e, closure, "", types.RefTypeRegarding)
	if err != nil {
		return nil, false, err
	}
	regardingApps, err := refOwnershipAppsForSubjects(regardingSubjects, assignment, templateIDToApp)
	if err != nil {
		return nil, false, err
	}
	regardingOrphanApps := sets.New[types.AppId]()
	if allowTemplateOrphanFallback {
		regardingOrphanApps, err = refOwnershipOrphanEventAppsForSubjects(regardingSubjects, templateIDToApp)
		if err != nil {
			return nil, false, err
		}
	}
	if regardingApps.Len() > 0 || regardingOrphanApps.Len() > 0 {
		combined := sets.New[types.AppId]()
		combined.Insert(regardingApps.UnsortedList()...)
		combined.Insert(regardingOrphanApps.UnsortedList()...)
		if hasNonBuiltinApp(combined) {
			return preferSpecificAppsOverBuiltins(combined), true, nil
		}
		if regardingOrphanApps.Len() > 0 {
			return combined, true, nil
		}
		return nil, true, nil
	}
	if allowTemplateOrphanFallback {
		workloadApps, foundWorkloadRefs, err := refOwnershipEventAppsFromWorkloadRegardingAnchors(
			e, closure, assignment, templateIDToApp)
		if err != nil {
			return nil, false, err
		}
		if workloadApps.Len() > 0 {
			if hasNonBuiltinApp(workloadApps) {
				return preferSpecificAppsOverBuiltins(workloadApps), true, nil
			}
			return workloadApps, true, nil
		}
		if foundWorkloadRefs {
			return nil, true, nil
		}
	}
	return nil, len(relatedSubjects) > 0 || len(regardingSubjects) > 0, nil
}

func refOwnershipEventAppsFromWorkloadRegardingAnchors(
	e entity.Entity,
	closure workloadclosure.MatchInput,
	assignment map[types.Id]types.AppId,
	templateIDToApp map[types.Id]types.AppId,
) (sets.Set[types.AppId], bool, error) {
	eid, err := e.Id()
	if err != nil {
		return nil, false, err
	}
	refs := closure.RefsByTo[eid]
	if closure.RefsByTo == nil {
		for _, ref := range closure.Refs {
			if ref.To == eid {
				refs = append(refs, ref)
			}
		}
	}
	if len(refs) == 0 {
		return nil, false, nil
	}
	out := sets.New[types.AppId]()
	found := false
	for _, ref := range refs {
		if ref.EndpointType != types.RefEndpointTypeId {
			continue
		}
		if !slices.Contains(ref.Labels, "workloadRegardingEvent") {
			continue
		}
		found = true
		appendAppFromClusterOrTemplate(ref.From, assignment, templateIDToApp, out)
	}
	return out, found, nil
}

func refOwnershipOrphanEventAppsForSubjects(
	subjects []entity.Entity,
	templateIDToApp map[types.Id]types.AppId,
) (sets.Set[types.AppId], error) {
	out := sets.New[types.AppId]()
	for _, subject := range subjects {
		if _, ok := subject.Unstructured(types.KeyClusterEntity); ok {
			continue
		}
		sid, err := subject.Id()
		if err != nil {
			return nil, err
		}
		appendAppFromClusterOrTemplate(sid, nil, templateIDToApp, out)
		serviceLBApps, matched := refOwnershipAppsForOrphanK3sServiceLBSubject(sid, templateIDToApp)
		if matched {
			out.Insert(serviceLBApps.UnsortedList()...)
		}
		localPathApps, matched := refOwnershipAppsForOrphanLocalPathProvisionerHelperSubject(sid, templateIDToApp)
		if matched {
			out.Insert(localPathApps.UnsortedList()...)
		}
	}
	return out, nil
}

func refOwnershipAppsForOrphanK3sServiceLBSubject(
	subjectID types.Id,
	templateIDToApp map[types.Id]types.AppId,
) (sets.Set[types.AppId], bool) {
	serviceName, ok := orphanK3sServiceLBServiceName(subjectID)
	if !ok {
		return nil, false
	}
	out := sets.New[types.AppId]()
	for templateID, appID := range templateIDToApp {
		_, _, kind, _, name, err := templateID.Components()
		if err != nil {
			continue
		}
		if kind != types.KubernetesKindService || name != serviceName {
			continue
		}
		out.Insert(appID)
	}
	return out, true
}

func refOwnershipAppsForOrphanLocalPathProvisionerHelperSubject(
	subjectID types.Id,
	templateIDToApp map[types.Id]types.AppId,
) (sets.Set[types.AppId], bool) {
	_, _, kind, ns, name, err := subjectID.Components()
	if err != nil {
		return nil, false
	}
	if kind != types.KubernetesKindPod || ns != types.Namespace("kube-system") {
		return nil, false
	}
	subjectName := string(name)
	if !strings.HasPrefix(subjectName, "helper-pod-create-pvc-") {
		return nil, false
	}
	anchorID := types.Id("apps/v1/Deployment/kube-system/" + hydra.ClusterDefaultsPresetIDLocalPathProvisioner)
	appID, ok := templateIDToApp[anchorID]
	if !ok {
		return sets.New[types.AppId](), true
	}
	return sets.New[types.AppId](appID), true
}

func orphanK3sServiceLBServiceName(subjectID types.Id) (types.Name, bool) {
	_, _, kind, _, name, err := subjectID.Components()
	if err != nil {
		return "", false
	}
	subjectName := string(name)
	if !strings.HasPrefix(subjectName, "svclb-") {
		return "", false
	}
	trimmed := strings.TrimPrefix(subjectName, "svclb-")
	if kind == types.KubernetesKindPod {
		lastDash := strings.LastIndex(trimmed, "-")
		if lastDash <= 0 {
			return "", false
		}
		trimmed = trimmed[:lastDash]
	}
	lastDash := strings.LastIndex(trimmed, "-")
	if lastDash <= 0 {
		return "", false
	}
	hash := trimmed[lastDash+1:]
	if !looksLikeK3sServiceLBHash(hash) {
		return "", false
	}
	serviceName := trimmed[:lastDash]
	if serviceName == "" {
		return "", false
	}
	return types.Name(serviceName), true
}

func looksLikeK3sServiceLBHash(s string) bool {
	if len(s) < 8 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func refOwnershipAppsForSubjects(
	subjects []entity.Entity,
	assignment map[types.Id]types.AppId,
	templateIDToApp map[types.Id]types.AppId,
) (sets.Set[types.AppId], error) {
	if len(subjects) == 0 {
		return sets.New[types.AppId](), nil
	}
	apps := sets.New[types.AppId]()
	for _, subject := range subjects {
		sid, err := subject.Id()
		if err != nil {
			return nil, err
		}
		appendAppFromClusterOrTemplate(sid, assignment, templateIDToApp, apps)
	}
	return apps, nil
}

// reconcileClusterOwnership applies template-primary ownership: if eid is present in templateIDToApp,
// that app always wins. Any ref-parser match for a different app is an error. Without a template id,
// ref-only rules apply (zero → unassigned, one → owner, more than one → ambiguous error).
func reconcileClusterOwnership(
	templateIDToApp map[types.Id]types.AppId,
	eid types.Id,
	refMatching sets.Set[types.AppId],
) (owner types.AppId, assigned bool, err error) {
	refMatching = preferSpecificAppsOverBuiltins(refMatching)
	templateOwner, hasTemplate := templateIDToApp[eid]
	if hasTemplate {
		for refApp := range refMatching {
			if refApp != templateOwner {
				refs := refMatching.UnsortedList()
				slices.SortFunc(refs, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })
				return "", false, log.CreateError(errors.ErrUninstallRefOwnershipConflictsWithTemplate,
					"cluster resource {id} is owned by app {template} per standalone template render but ref predicates also match other app(s) ({refs}); fix ref parsers so only the template owner matches",
					log.String("id", string(eid)),
					log.String("template", string(templateOwner)),
					log.String("refs", fmt.Sprintf("%v", refs)))
			}
		}
		return templateOwner, true, nil
	}

	switch refMatching.Len() {
	case 0:
		return "", false, nil
	case 1:
		return refMatching.UnsortedList()[0], true, nil
	default:
		ids := refMatching.UnsortedList()
		slices.SortFunc(ids, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })
		return "", false, log.CreateError(errors.ErrUninstallAmbiguousRefOwnership,
			"cluster resource {id} matches ref predicates of more than one app ({apps}); fix ref predicates so exactly one app owns it",
			log.String("id", string(eid)),
			log.String("apps", fmt.Sprintf("%v", ids)))
	}
}

// ApplyOwnerNamespacesToNamespaceAssignments assigns live v1/Namespace objects to apps declared in
// global.hydra.ownerNamespaces when the Namespace id is absent from templateIDToApp. Entries in
// refPredAmbiguous are skipped. When ref ownership already assigned a different app, handleConflict
// is invoked when non-nil; otherwise return ErrUninstallAmbiguousRefOwnership.
func ApplyOwnerNamespacesToNamespaceAssignments(
	templateIDToApp map[types.Id]types.AppId,
	namespaceOwners map[types.Namespace]types.AppId,
	liveCluster entity.Entities,
	assignment map[types.Id]types.AppId,
	refPredAmbiguous sets.Set[types.Id],
	handleConflict func(eid types.Id, refApp, declaredApp types.AppId) error,
) error {
	if len(namespaceOwners) == 0 {
		return nil
	}
	for _, e := range liveCluster.Items {
		gvk, err := e.GVKString()
		if err != nil || gvk != types.KubernetesGvkV1Namespace {
			continue
		}
		eid, err := e.Id()
		if err != nil {
			return err
		}
		if _, has := templateIDToApp[eid]; has {
			continue
		}
		if refPredAmbiguous != nil && refPredAmbiguous.Has(eid) {
			continue
		}
		name, err := e.Name()
		if err != nil {
			continue
		}
		declaredApp, ok := namespaceOwners[types.Namespace(name)]
		if !ok {
			continue
		}
		if existing, has := assignment[eid]; has {
			if existing == declaredApp {
				continue
			}
			if handleConflict != nil {
				return handleConflict(eid, existing, declaredApp)
			}
			return log.CreateError(errors.ErrUninstallAmbiguousRefOwnership,
				"v1/Namespace {id} matches ref ownership for app {ref} but global.hydra.ownerNamespaces declares app {declared}",
				log.String("id", string(eid)), log.String("ref", string(existing)), log.String("declared", string(declaredApp)))
		}
		assignment[eid] = declaredApp
	}
	return nil
}

// namespaceNameFromV1NamespaceClusterID returns the Namespace object's metadata.name when id is a
// core v1/Namespace cluster entity id (version/kind//name). Other ids return false.
func namespaceNameFromV1NamespaceClusterID(id types.Id) (types.Namespace, bool) {
	_, ver, kind, _, name, err := id.Components()
	if err != nil {
		return "", false
	}
	if ver != types.KubernetesVersionV1 || kind != types.KubernetesKindNamespace || name == "" {
		return "", false
	}
	return types.Namespace(name), true
}

// workloadNamespaceFromNamespacedClusterEntityID returns metadata.namespace for a namespaced
// cluster entity id (5-part form: group/version/kind/namespace/name). Returns false for the
// Namespace kind (cluster-scoped id form), cluster-scoped kinds, or an empty namespace segment.
func workloadNamespaceFromNamespacedClusterEntityID(id types.Id) (types.Namespace, bool) {
	_, _, kind, ns, _, err := id.Components()
	if err != nil {
		return "", false
	}
	if kind == types.KubernetesKindNamespace {
		return "", false
	}
	if ns == "" {
		return "", false
	}
	return types.Namespace(ns), true
}

// Shared Kubernetes bootstrap ownership (kube-system / kube-public Namespace objects, the
// extension-apiserver-authentication-reader Role + RoleBinding, kube-root-ca.crt ConfigMap,
// k3s.cattle.io Addons, the addons.k3s.cattle.io CRD, CoreDNS workloads, ...) is materialized as
// template entities of the corresponding builtin apps via [PresetTemplateEntities] /
// [MergeBuiltinPresetAppsForCluster]. The standard template-primat rules inside
// [propagateOwnershipFromMergedInspectRefs] (`templateIDToApp[r.To] == existing` in the conflict
// path; `_, inTemplate := templateIDToApp[r.To]` in the unassigned path; `tplOwner != sole` in
// the sole-workload-consumer prepass) keep cross-app references from overriding that ownership
// without any kind/name special casing here.

func mergedInspectRefClusterEntityKind(id types.Id) (types.Kind, bool) {
	_, _, kind, _, _, err := id.Components()
	if err != nil || kind == "" {
		return "", false
	}
	return kind, true
}

func mergedInspectRefIsWorkloadPropagationSource(id types.Id) bool {
	k, ok := mergedInspectRefClusterEntityKind(id)
	if !ok {
		return false
	}
	switch string(k) {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob":
		return true
	default:
		return false
	}
}

func mergedInspectRefSkipsOwnershipPropagation(ref types.Ref) bool {
	return slices.Contains(ref.Labels, "workloadRegardingEvent")
}

func filterMergedInspectRefsForOwnershipClosure(refs []types.Ref) []types.Ref {
	if len(refs) == 0 {
		return nil
	}
	filtered := make([]types.Ref, 0, len(refs))
	for _, ref := range refs {
		if mergedInspectRefSkipsOwnershipPropagation(ref) {
			continue
		}
		filtered = append(filtered, ref)
	}
	return filtered
}

// mergedInspectRefSoleConsumerSkipsTarget skips Namespace targets: namespace ownership follows
// ownerNamespaces / propagation, never a single workload→namespace ref. Shared bootstrap ids
// (kube-system Namespace, RBAC, kube-root-ca.crt, ...) are now protected by template-primat via
// the builtin preset apps materialized in [PresetTemplateEntities]; no name-based special cases.
func mergedInspectRefSoleConsumerSkipsTarget(to types.Id) bool {
	k, ok := mergedInspectRefClusterEntityKind(to)
	return ok && k == "Namespace"
}

// mergedInspectRefTryAssignNamespaceOwner sets assignment[to] to the namespace owner app from
// global.hydra.ownerNamespaces when that namespace is declared. Resolves shared resources in a
// namespace when multiple apps reference them or merged inspect would otherwise conflict. Does not
// override template ownership when templateIDToApp[to] names a different app. Skips negative-priority
// targets and sole-consumer-skipped targets (e.g. Namespace kind).
func mergedInspectRefTryAssignNamespaceOwner(
	to types.Id,
	templateIDToApp map[types.Id]types.AppId,
	namespaceOwners map[types.Namespace]types.AppId,
	assignment map[types.Id]types.AppId,
	negativePriorityClusterEntityIDs *sets.Set[types.Id],
) bool {
	if len(namespaceOwners) == 0 {
		return false
	}
	if mergedInspectRefSoleConsumerSkipsTarget(to) {
		return false
	}
	wn, ok := workloadNamespaceFromNamespacedClusterEntityID(to)
	if !ok {
		return false
	}
	declared, dOK := namespaceOwners[wn]
	if !dOK {
		return false
	}
	if negativePriorityClusterEntityIDs != nil && negativePriorityClusterEntityIDs.Has(to) {
		return false
	}
	if tplOwner, inTpl := templateIDToApp[to]; inTpl && tplOwner != declared {
		return false
	}
	assignment[to] = declared
	return true
}

// ambiguousRefOwnershipJoinError combines several merged-inspect ownership conflicts into one
// error value. [errors.Id] still reports [errors.ErrUninstallAmbiguousRefOwnership].
type ambiguousRefOwnershipJoinError struct {
	errs []error
}

func (e ambiguousRefOwnershipJoinError) Error() string {
	msgs := make([]string, len(e.errs))
	for i, err := range e.errs {
		msgs[i] = err.Error()
	}
	return strings.Join(msgs, "\n")
}

func (ambiguousRefOwnershipJoinError) ErrorId() errors.ErrorId {
	return errors.ErrUninstallAmbiguousRefOwnership
}

func joinAmbiguousRefOwnershipErrors(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return ambiguousRefOwnershipJoinError{errs: append([]error(nil), errs...)}
	}
}

// propagateOwnershipFromMergedInspectRefs walks the merged inspect ref graph and propagates the
// already-assigned owner of a ref source to its target when the target is unassigned. Iterates
// to a fixpoint so chained edges (A→B→C) propagate transitively. A target that already has a
// different owner triggers [errors.ErrUninstallAmbiguousRefOwnership] (all such edges in one run are
// collected into one error; each conflict is also logged immediately as its own message) unless the assignment matches
// templateIDToApp for that target (template-primat over merged inspect cross-edges), or unless negativePriorityClusterEntityIDs
// is non-nil and contains the target id: then the existing assignment is treated as negative-priority
// (uninstall-safe-only / bootstrap-clone ownership) and is replaced by the propagating owner,
// or unless the existing assignment is a preset app ([types.AppId.IsPresetApp]) and the
// propagating owner is not: then the real app wins over cluster-defaults preset ownership,
// or unless the propagating owner is a preset app and the existing assignment is not:
// then merged inspect ignores builtins as claimants (shared kube-system ServiceAccount→Secret edges
// must not override ref/chart ownership),
// or unless both the propagating owner and the existing assignment are preset apps:
// then merged inspect ignores cross-builtin conflicts (shared bootstrap secrets such as kube-system
// image-pull-secret referenced from several cluster-defaults presets).
// Before propagation, ref targets with Deployment-style workload consumer apps inherit ownership:
// a single consumer app, or the namespace owner app when several consumers disagree, except ids in
// negativePriorityClusterEntityIDs so
// negative-priority replacement in the fixpoint still runs. Edges from SopsSecret CRs,
// sops materialization, Kyverno clone-source/clone-target, and cert-manager TLS materialization
// conflict cases are handled by skips (never use controller operator apps as propagation sources).
// For v1/Namespace targets and for namespaced entity ids, if namespaceOwners maps the workload
// namespace to the same app as existing assignment, cross-app inspect edges do not conflict.
// When propagation would otherwise error, the namespace owner app is applied if declared (even when
// it is neither the prior assignment nor the propagating source app).
// Edges whose target is the cluster object v1/Namespace for kube-system or kube-public are skipped
// (bootstrap namespace objects: cross-app references must not override ref ownership of that object).
// References whose `From` resolves to no app are skipped.
func propagateOwnershipFromMergedInspectRefs(
	refs []types.Ref,
	templateIDToApp map[types.Id]types.AppId,
	assignment map[types.Id]types.AppId,
	namespaceOwners map[types.Namespace]types.AppId,
	negativePriorityClusterEntityIDs *sets.Set[types.Id],
) error {
	if len(refs) == 0 {
		return nil
	}
	var conflictErrs []error
	seenConflict := sets.New[string]()
	ownerOf := func(id types.Id) (types.AppId, bool) {
		if app, ok := assignment[id]; ok {
			return app, true
		}
		if app, ok := templateIDToApp[id]; ok {
			return app, true
		}
		return "", false
	}
	consumerAppsByTarget := make(map[types.Id]sets.Set[types.AppId])
	for _, r := range refs {
		if r.From == "" || r.To == "" || r.From == r.To {
			continue
		}
		if mergedInspectRefSkipsOwnershipPropagation(r) {
			continue
		}
		if mergedInspectRefSoleConsumerSkipsTarget(r.To) {
			continue
		}
		if !mergedInspectRefIsWorkloadPropagationSource(r.From) {
			continue
		}
		fromApp, ok := ownerOf(r.From)
		if !ok {
			continue
		}
		if consumerAppsByTarget[r.To] == nil {
			consumerAppsByTarget[r.To] = sets.New[types.AppId]()
		}
		consumerAppsByTarget[r.To].Insert(fromApp)
	}
	for to, apps := range consumerAppsByTarget {
		if negativePriorityClusterEntityIDs != nil && negativePriorityClusterEntityIDs.Has(to) {
			continue
		}
		if apps.Len() == 1 {
			sole := apps.UnsortedList()[0]
			if tplOwner, inTpl := templateIDToApp[to]; inTpl && tplOwner != sole {
				continue
			}
			assignment[to] = sole
			continue
		}
		if apps.Len() > 1 {
			mergedInspectRefTryAssignNamespaceOwner(to, templateIDToApp, namespaceOwners, assignment, negativePriorityClusterEntityIDs)
		}
	}
	for {
		changed := false
		for _, r := range refs {
			if r.From == "" || r.To == "" || r.From == r.To {
				continue
			}
			if mergedInspectRefSkipsOwnershipPropagation(r) {
				continue
			}
			if fromKind, ok := mergedInspectRefClusterEntityKind(r.From); ok && fromKind == "SopsSecret" {
				continue
			}
			if slices.Contains(r.Labels, "sops") && r.RefMaterializesVirtualTarget() {
				continue
			}
			if slices.Contains(r.Labels, "clone-source") {
				continue
			}
			if slices.Contains(r.Labels, "clone-target") && r.RefMaterializesVirtualTarget() {
				continue
			}
			fromApp, ok := ownerOf(r.From)
			if !ok {
				continue
			}
			if existing, has := assignment[r.To]; has {
				if existing == fromApp {
					continue
				}
				if tplOwner, inTpl := templateIDToApp[r.To]; inTpl && existing == tplOwner {
					// Template-primat: merged inspect edges from another app must not conflict with
					// standalone template ownership already assigned to this id (same rule as below
					// for unassigned template ids).
					continue
				}
				if len(namespaceOwners) > 0 {
					if nsName, ok := namespaceNameFromV1NamespaceClusterID(r.To); ok {
						if declared, dOK := namespaceOwners[nsName]; dOK && declared == existing {
							continue
						}
					} else if wn, ok := workloadNamespaceFromNamespacedClusterEntityID(r.To); ok {
						if declared, dOK := namespaceOwners[wn]; dOK && declared == existing {
							continue
						}
					}
				}
				if negativePriorityClusterEntityIDs != nil && negativePriorityClusterEntityIDs.Has(r.To) {
					assignment[r.To] = fromApp
					negativePriorityClusterEntityIDs.Delete(r.To)
					changed = true
					continue
				}
				if r.RefMaterializesVirtualTarget() && slices.Contains(r.Labels, "cert-manager-certificate-tls-secret") {
					continue
				}
				if mergedInspectRefTryAssignNamespaceOwner(r.To, templateIDToApp, namespaceOwners, assignment, negativePriorityClusterEntityIDs) {
					changed = true
					continue
				}
				if existing.IsPresetApp() && !fromApp.IsPresetApp() {
					assignment[r.To] = fromApp
					changed = true
					continue
				}
				if fromApp.IsPresetApp() && !existing.IsPresetApp() {
					continue
				}
				if fromApp.IsPresetApp() && existing.IsPresetApp() {
					continue
				}
				key := string(r.From) + "\x00" + string(r.To) + "\x00" + string(fromApp) + "\x00" + string(existing)
				if seenConflict.Has(key) {
					continue
				}
				seenConflict.Insert(key)
				conflictErr := log.CreateError(errors.ErrUninstallAmbiguousRefOwnership,
					"cluster object {to} is referenced by source {from} owned by app {fromApp} but already assigned to app {existing} (merged inspect ref-graph propagation)",
					log.String("to", string(r.To)),
					log.String("from", string(r.From)),
					log.String("fromApp", string(fromApp)),
					log.String("existing", string(existing)))
				log.LogLazy(conflictErr)
				conflictErrs = append(conflictErrs, conflictErr)
				continue
			}
			if _, inTemplate := templateIDToApp[r.To]; inTemplate {
				// Template-primat: never override an entity that templates declare.
				continue
			}
			assignment[r.To] = fromApp
			changed = true
		}
		if !changed {
			break
		}
	}
	return log.ReturnedErrorAlreadyEmitted(joinAmbiguousRefOwnershipErrors(conflictErrs))
}

func propagateOwnershipFromMergedInspectRefsCollect(
	refs []types.Ref,
	templateIDToApp map[types.Id]types.AppId,
	assignment map[types.Id]types.AppId,
	namespaceOwners map[types.Namespace]types.AppId,
	negativePriorityClusterEntityIDs *sets.Set[types.Id],
	recordAmbiguous func(id types.Id, apps ...types.AppId),
) error {
	if len(refs) == 0 {
		return nil
	}
	ownerOf := func(id types.Id) (types.AppId, bool) {
		if app, ok := assignment[id]; ok {
			return app, true
		}
		if app, ok := templateIDToApp[id]; ok {
			return app, true
		}
		return "", false
	}
	consumerAppsByTarget := make(map[types.Id]sets.Set[types.AppId])
	for _, r := range refs {
		if r.From == "" || r.To == "" || r.From == r.To {
			continue
		}
		if mergedInspectRefSkipsOwnershipPropagation(r) {
			continue
		}
		if mergedInspectRefSoleConsumerSkipsTarget(r.To) || !mergedInspectRefIsWorkloadPropagationSource(r.From) {
			continue
		}
		fromApp, ok := ownerOf(r.From)
		if !ok {
			continue
		}
		if consumerAppsByTarget[r.To] == nil {
			consumerAppsByTarget[r.To] = sets.New[types.AppId]()
		}
		consumerAppsByTarget[r.To].Insert(fromApp)
	}
	for to, apps := range consumerAppsByTarget {
		if negativePriorityClusterEntityIDs != nil && negativePriorityClusterEntityIDs.Has(to) {
			continue
		}
		if apps.Len() == 1 {
			sole := apps.UnsortedList()[0]
			if tplOwner, inTpl := templateIDToApp[to]; inTpl && tplOwner != sole {
				recordAmbiguous(to, tplOwner, sole)
				continue
			}
			assignment[to] = sole
			continue
		}
		if apps.Len() > 1 && !mergedInspectRefTryAssignNamespaceOwner(to, templateIDToApp, namespaceOwners, assignment, negativePriorityClusterEntityIDs) {
			recordAmbiguous(to, apps.UnsortedList()...)
		}
	}
	for {
		changed := false
		for _, r := range refs {
			if r.From == "" || r.To == "" || r.From == r.To {
				continue
			}
			if mergedInspectRefSkipsOwnershipPropagation(r) {
				continue
			}
			if fromKind, ok := mergedInspectRefClusterEntityKind(r.From); ok && fromKind == "SopsSecret" {
				continue
			}
			if slices.Contains(r.Labels, "sops") && r.RefMaterializesVirtualTarget() {
				continue
			}
			if slices.Contains(r.Labels, "clone-source") {
				continue
			}
			if slices.Contains(r.Labels, "clone-target") && r.RefMaterializesVirtualTarget() {
				continue
			}
			fromApp, ok := ownerOf(r.From)
			if !ok {
				continue
			}
			if existing, has := assignment[r.To]; has {
				if existing == fromApp {
					continue
				}
				if tplOwner, inTpl := templateIDToApp[r.To]; inTpl && existing == tplOwner {
					recordAmbiguous(r.To, existing, fromApp)
					continue
				}
				if len(namespaceOwners) > 0 {
					if nsName, ok := namespaceNameFromV1NamespaceClusterID(r.To); ok {
						if declared, dOK := namespaceOwners[nsName]; dOK && declared == existing {
							continue
						}
					} else if wn, ok := workloadNamespaceFromNamespacedClusterEntityID(r.To); ok {
						if declared, dOK := namespaceOwners[wn]; dOK && declared == existing {
							continue
						}
					}
				}
				if negativePriorityClusterEntityIDs != nil && negativePriorityClusterEntityIDs.Has(r.To) {
					assignment[r.To] = fromApp
					negativePriorityClusterEntityIDs.Delete(r.To)
					changed = true
					continue
				}
				if r.RefMaterializesVirtualTarget() && slices.Contains(r.Labels, "cert-manager-certificate-tls-secret") {
					continue
				}
				if mergedInspectRefTryAssignNamespaceOwner(r.To, templateIDToApp, namespaceOwners, assignment, negativePriorityClusterEntityIDs) {
					changed = true
					continue
				}
				if existing.IsPresetApp() && !fromApp.IsPresetApp() {
					assignment[r.To] = fromApp
					changed = true
					continue
				}
				if fromApp.IsPresetApp() && !existing.IsPresetApp() {
					continue
				}
				if fromApp.IsPresetApp() && existing.IsPresetApp() {
					continue
				}
				recordAmbiguous(r.To, existing, fromApp)
				continue
			}
			if _, inTemplate := templateIDToApp[r.To]; inTemplate {
				continue
			}
			assignment[r.To] = fromApp
			changed = true
		}
		if !changed {
			break
		}
	}
	return nil
}

func assignPersistentVolumesFromAssignedClaims(
	refs []types.Ref,
	templateIDToApp map[types.Id]types.AppId,
	assignment map[types.Id]types.AppId,
	observe assignmentObserver,
) {
	for _, ref := range refs {
		if ref.From == "" || ref.To == "" || ref.From == ref.To {
			continue
		}
		fromKind, err := ref.From.Kind()
		if err != nil || fromKind != types.KubernetesKindPersistentVolumeClaim {
			continue
		}
		toKind, err := ref.To.Kind()
		if err != nil || toKind != types.KubernetesKindPersistentVolume {
			continue
		}
		if _, has := assignment[ref.To]; has {
			continue
		}
		if _, inTemplate := templateIDToApp[ref.To]; inTemplate {
			continue
		}
		app, ok := assignment[ref.From]
		if !ok {
			continue
		}
		assignment[ref.To] = app
		if observe != nil {
			observe(ref.To, app)
		}
	}
}

func assignRefsProgressAdvance(p log.Progress, step int, total int) {
	if p != nil {
		p.Advance(step, total)
	}
}

// AssignClusterEntitiesToAppRefsProgressTotal returns the footer progress denominator for
// [AssignClusterEntitiesToAtMostOneAppByRefs]: per-app CEL prep steps (nApps), four coarse prep
// checkpoints (predicate lines, workload closure, compile strong, compile weak), optional
// namespace-ownership passes, then four passes over entities plus tails.
func AssignClusterEntitiesToAppRefsProgressTotal(nEntities, nApps int) int {
	prep := nApps + 4
	const postTemplateStrongWeakTail = 9 // expand1, expand2, weak-propagation, sole-template, ownerNamespaces ns, ownerNamespaces wl, validate, presets, assignment-ready
	if nEntities <= 0 {
		return prep + postTemplateStrongWeakTail
	}
	return prep + 4*nEntities + postTemplateStrongWeakTail
}

// AssignClusterEntitiesToAtMostOneAppByRefs assigns live cluster entities to apps in a strict,
// generic order:
//  1. standalone template ids are authoritative;
//  2. Kubernetes metadata.ownerReferences propagate that ownership transitively;
//  3. runtime objects are checked against priority >= 0 teardown refs
//     (uninstall, uninstall-force, backup);
//  4. runtime objects are checked against weak-yield teardown refs
//     (currently uninstall-safe-only builtin/app ref parsers);
//  5. sole-template-namespace assignment for implicit namespaces when exactly one enabled app renders
//     into that workload namespace (covers live Namespace objects without a manifest id);
//  6. global.hydra.ownerNamespaces declares ownership for shared namespaces (explicit Namespace
//     assignment, then remaining workloads in those namespaces).
//  7. cluster-defaults presets are evaluated only on leftovers that stayed unassigned after the
//     app ownership passes above.
//
// The public API keeps the common conflict-free path cheap while still guaranteeing detailed
// ambiguous-app diagnostics to callers: it first runs the fast assignment pass, then automatically
// reruns only the ambiguous IDs with ConflictDetailIDs populated when a conflict was detected.
//
// Internal package callers that intentionally want the raw fast-path result can call the unexported
// assignClusterEntitiesToAtMostOneAppByRefs helper directly.
var assignClusterEntitiesToAtMostOneAppByRefsImpl = assignClusterEntitiesToAtMostOneAppByRefs

func AssignClusterEntitiesToAtMostOneAppByRefs(
	in AssignClusterEntitiesToAtMostOneAppByRefsInput,
) (assignment map[types.Id]types.AppId, metadata ClusterEntityAssignmentMetadata, unassigned entity.Entities, err error) {
	assignment, metadata, unassigned, err = assignClusterEntitiesToAtMostOneAppByRefsImpl(in)
	if err != nil {
		return nil, ClusterEntityAssignmentMetadata{}, entity.Entities{}, err
	}
	ambiguousIDs := metadata.AmbiguousEntityIDs()
	if len(ambiguousIDs) == 0 {
		return assignment, metadata, unassigned, nil
	}
	if clusterEntityAssignmentHasDetailedReasonsForAll(metadata, ambiguousIDs) {
		return assignment, metadata, unassigned, nil
	}
	detailInput := in
	detailInput.ConflictDetailIDs = ambiguousIDs
	return assignClusterEntitiesToAtMostOneAppByRefsImpl(detailInput)
}

func clusterEntityAssignmentHasDetailedReasonsForAll(
	metadata ClusterEntityAssignmentMetadata,
	ambiguousIDs []types.Id,
) bool {
	for _, id := range ambiguousIDs {
		apps := metadata.AmbiguousAppIDsByClusterEntity[id]
		if len(apps) == 0 {
			return false
		}
		reasonsByApp := metadata.AmbiguousAppReasonsByClusterEntity[id]
		if len(reasonsByApp) == 0 {
			return false
		}
		for _, app := range apps {
			if len(reasonsByApp[app]) == 0 {
				return false
			}
		}
	}
	return true
}

func assignClusterEntitiesToAtMostOneAppByRefs(
	in AssignClusterEntitiesToAtMostOneAppByRefsInput,
) (assignment map[types.Id]types.AppId, metadata ClusterEntityAssignmentMetadata, unassigned entity.Entities, err error) {
	cluster := in.Cluster
	resourceModel := in.ResourceModel
	clusterInNamespaces := in.ClusterEntities
	allAppIds := in.AllAppIDs
	weakAppIds := in.WeakAppIDs
	perAppRendered := in.PerAppRendered
	renderedAllApps := in.RenderedAllApps
	mergedInspectRefs := in.MergedInspectRefs
	initialAssignment := in.InitialAssignment
	progress := in.Progress
	progressPrefixSteps := in.ProgressPrefixSteps
	progressGrandTotal := in.ProgressGrandTotal
	parallel := in.Parallel
	networkMode := in.NetworkMode
	aggregateProgress := in.aggregateProgress
	splitProgress := in.splitProgress

	conflictDetailIDs := sets.New[types.Id](in.ConflictDetailIDs...)
	shouldCollectConflictDetails := func(id types.Id) bool {
		return conflictDetailIDs.Has(id)
	}
	focusIDs := sets.New[types.Id](in.FocusEntityIDs...)
	isFocused := func(id types.Id) bool {
		return len(focusIDs) == 0 || focusIDs.Has(id)
	}

	nEntities := clusterInNamespaces.Len()
	appOrder := allAppIds.UnsortedList()
	slices.SortFunc(appOrder, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })
	totalSteps := AssignClusterEntitiesToAppRefsProgressTotal(nEntities, len(appOrder))
	metadata.DirectRefMatchedBuiltinIDs = sets.New[types.Id]()
	metadata.DirectRefMatchedAppIDs = sets.New[types.Id]()
	metadata.AmbiguousAppIDsByClusterEntity = map[types.Id][]types.AppId{}
	metadata.AmbiguousAppReasonsByClusterEntity = map[types.Id]map[types.AppId][]AssignmentReason{}
	metadata.UnassignedIDs = sets.New[types.Id]()

	_ = parallel

	var detailTask log.ProgressTask
	var splitPhase *splitAssignProgress
	if splitProgress {
		if cluster != nil {
			splitPhase = newSplitAssignProgress(cluster.L(), aggregateProgress)
		} else {
			splitPhase = newSplitAssignProgress(log.Default(), aggregateProgress)
		}
		defer splitPhase.Close()
	} else if progress != nil {
		detailTask = progress.NewTask("")
	}
	defer func() {
		if detailTask != nil {
			_ = detailTask.Close()
		}
	}()
	setDetail := func(msg string) {
		if splitPhase != nil {
			splitPhase.SetDetail(msg)
		} else if detailTask != nil {
			detailTask.SetDetail(msg)
		}
	}
	startPhase := func(step resourceModelProgressStep, total int) error {
		if splitPhase == nil {
			return nil
		}
		return splitPhase.Start(step, total)
	}

	step := 0
	advance := func(detail string) {
		if splitPhase != nil {
			splitPhase.Advance(detail)
			return
		}
		step++
		idx := progressPrefixSteps + step
		total := progressGrandTotal
		if progress != nil && progressGrandTotal <= 0 {
			total = totalSteps
			idx = step
		}
		assignRefsProgressAdvance(progress, idx, total)
	}

	templateIDToApp := templateResourceIDToApp(perAppRendered)
	clusterUIDMap := clusterInNamespaces.UidMap(types.KeyClusterEntity)
	clusterEntityByID := make(map[types.Id]entity.Entity, clusterInNamespaces.Len())
	for _, e := range clusterInNamespaces.Items {
		eid, idErr := e.Id()
		if idErr != nil {
			return nil, metadata, entity.Entities{}, idErr
		}
		clusterEntityByID[eid] = e
	}
	ownerRefReasonByID := make(map[types.Id]AssignmentReason, len(clusterEntityByID))
	for id, e := range clusterEntityByID {
		ownerRefReasonByID[id] = ownerRefReasonForEntity(e, types.KeyClusterEntity, clusterUIDMap)
	}
	var ownerRefChildren ownerRefChildIndex
	if resourceModel != nil {
		resourceModel.SetClusterEntities(clusterInNamespaces)
		ownerRefChildren, err = resourceModel.clusterOwnerRefChildIndex()
		if err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	} else {
		ownerRefChildren, err = buildOwnerRefChildIndex(clusterInNamespaces, types.KeyClusterEntity)
		if err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	}
	if weakAppIds == nil || weakAppIds.Len() == 0 {
		weakAppIds = allAppIds
	}
	assignmentReasons := make(map[types.Id]map[types.AppId][]AssignmentReason)
	appendReason := appendAssignmentReasonUnique
	setAssignmentReason := func(id types.Id, app types.AppId, reason AssignmentReason) {
		reasonsByApp, ok := assignmentReasons[id]
		if !ok || assignment[id] != app {
			reasonsByApp = map[types.AppId][]AssignmentReason{}
		}
		reasonsByApp[app] = appendReason(reasonsByApp[app], reason)
		assignmentReasons[id] = reasonsByApp
	}
	assignWithReason := func(id types.Id, app types.AppId, reason AssignmentReason) {
		assignment[id] = app
		setAssignmentReason(id, app, reason)
	}
	copyReasons := func(id types.Id) map[types.AppId][]AssignmentReason {
		src := assignmentReasons[id]
		if len(src) == 0 {
			return nil
		}
		out := make(map[types.AppId][]AssignmentReason, len(src))
		for app, reasons := range src {
			out[app] = append([]AssignmentReason{}, reasons...)
		}
		return out
	}
	ambiguousIDs := sets.New[types.Id]()
	recordAmbiguous := func(id types.Id, appReasons map[types.AppId][]AssignmentReason, apps ...types.AppId) {
		merged := sets.New[types.AppId](apps...)
		for _, existing := range metadata.AmbiguousAppIDsByClusterEntity[id] {
			merged.Insert(existing)
		}
		if current, ok := assignment[id]; ok {
			merged.Insert(current)
		}
		reasonsByApp := metadata.AmbiguousAppReasonsByClusterEntity[id]
		if reasonsByApp == nil {
			reasonsByApp = map[types.AppId][]AssignmentReason{}
		}
		for app, reasons := range copyReasons(id) {
			for _, reason := range reasons {
				reasonsByApp[app] = appendReason(reasonsByApp[app], reason)
			}
		}
		for app, reasons := range appReasons {
			for _, reason := range reasons {
				reasonsByApp[app] = appendReason(reasonsByApp[app], reason)
			}
		}
		delete(assignment, id)
		delete(assignmentReasons, id)
		list := merged.UnsortedList()
		slices.SortFunc(list, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })
		metadata.AmbiguousAppIDsByClusterEntity[id] = list
		metadata.AmbiguousAppReasonsByClusterEntity[id] = reasonsByApp
		ambiguousIDs.Insert(id)
	}

	if err := startPhase(resourceModelProgressStepRefOwnershipUninstallPredicateLines, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("ref ownership uninstall predicate lines …")
	nonNegativePriorityWR, _, err := hydra.HydraAppRefOwnershipUninstallPredicateLinePriorityBands(
		cluster, allAppIds, networkMode, renderedAllApps, true)
	if err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	advance("ref ownership uninstall predicate lines")
	_, negativePriorityWR, err := hydra.HydraAppRefOwnershipUninstallPredicateLinePriorityBands(
		cluster, weakAppIds, networkMode, renderedAllApps, true)
	if err != nil {
		return nil, metadata, entity.Entities{}, err
	}

	envByApp := make(map[types.AppId]cel.Env)
	if err := startPhase(resourceModelProgressStepCELInventoryEnvironments, len(appOrder)); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("CEL inventory environments …")
	celTmpEnv, err := cel.NewEnv()
	if err != nil {
		l := log.Default()
		l.Error(logIdCommands, "failed to build CEL inventory environment: {err}", log.Err(err))
		return nil, metadata, entity.Entities{}, err
	}
	clusterOverlaySnaps, err := cel.ClusterInventoryOverlaySnapshots(celTmpEnv, clusterInNamespaces)
	if err != nil {
		l := log.Default()
		l.Error(logIdCommands, "failed to build CEL inventory environment: {err}", log.Err(err))
		return nil, metadata, entity.Entities{}, err
	}
	for idx, appId := range appOrder {
		setDetail(fmt.Sprintf("CEL inventory environments · %d / %d", idx+1, len(appOrder)))
		if len(nonNegativePriorityWR[appId]) == 0 && len(negativePriorityWR[appId]) == 0 {
			advance(fmt.Sprintf("CEL inventory environments · %d / %d", idx+1, len(appOrder)))
			continue
		}
		rend, ok := perAppRendered[appId]
		if ok {
			invOpt, envErr := cel.ClusterInventorySupportWithOverlaySnapshots(celTmpEnv, rend, entity.Entities{}, clusterOverlaySnaps)
			if envErr != nil {
				l := log.Default()
				l.Error(logIdCommands, "failed to build CEL inventory environment for app {app}: {err}",
					log.String("app", string(appId)), log.Err(envErr))
				return nil, metadata, entity.Entities{}, envErr
			}

			env, envErr := cel.NewEnv(invOpt)
			if envErr != nil {
				l := log.Default()
				l.Error(logIdCommands, "failed to build CEL inventory environment for app {app}: {err}",
					log.String("app", string(appId)), log.Err(envErr))
				return nil, metadata, entity.Entities{}, envErr
			}
			envByApp[appId] = env
		}
		advance(fmt.Sprintf("CEL inventory environments · %d / %d", idx+1, len(appOrder)))
	}

	if err := startPhase(resourceModelProgressStepWorkloadClosureMatchInput, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("workload closure match input …")
	closure := workloadclosure.EmptyMatchInput(types.KeyClusterEntity)
	buildMergedClosure := func(
		l log.Logger,
		preferredVersions map[types.GroupKindKey]types.Version,
	) (workloadclosure.MatchInput, error) {
		refsForClosure := filterMergedInspectRefsForOwnershipClosure(mergedInspectRefs)
		if len(refsForClosure) == 0 {
			clusterRefs, rerr := references.Refs(
				l,
				clusterInNamespaces,
				types.KeyClusterEntity,
				nil,
				entity.Entities{},
				entity.Entities{},
				preferredVersions,
				nil,
			)
			if rerr != nil {
				return workloadclosure.MatchInput{}, rerr
			}
			if renderedAllApps.Len() == 0 {
				refsForClosure = clusterRefs
			} else {
				templateRefs, terr := references.Refs(
					l,
					renderedAllApps,
					types.KeyTemplateEntity,
					nil,
					entity.Entities{},
					clusterInNamespaces,
					preferredVersions,
					nil,
				)
				if terr != nil {
					return workloadclosure.MatchInput{}, terr
				}
				refsForClosure = references.MergeRefLists(templateRefs, clusterRefs)
				refsForClosure = references.CanonicalizeOwnerRefTargetsToClusterIDs(refsForClosure, clusterInNamespaces)
				refsForClosure = references.MergeRefLists(refsForClosure)
			}
		}
		return WorkloadClosureMatchInputFromMergedInventory(refsForClosure, clusterInNamespaces, renderedAllApps)
	}
	if clusterInNamespaces.Len() > 0 {
		if cluster != nil {
			cluster.ResetPreferredVersionsCache()
			pref, perr := cluster.PreferredVersions(func() (types.ScopeInfoMap, error) {
				return ScopeInfoMapFromCluster(cluster, types.KeyClusterEntity, types.CrdModeSilent)
			})
			if perr != nil {
				return nil, metadata, entity.Entities{}, perr
			}
			var cerr error
			closure, cerr = buildMergedClosure(cluster.L(), pref)
			if cerr != nil {
				return nil, metadata, entity.Entities{}, cerr
			}
		} else {
			var cerr error
			closure, cerr = buildMergedClosure(log.Default(), nil)
			if cerr != nil {
				return nil, metadata, entity.Entities{}, cerr
			}
		}
	}
	advance("workload closure match input")

	if err := startPhase(resourceModelProgressStepCompileRefOwnershipPredicates, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("compile ref ownership predicates …")
	compiledNonNegativePriorityWithRuntime, err := compileRefOwnershipPredicateLines(appOrder, nonNegativePriorityWR, envByApp)
	if err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	advance("compile ref ownership predicates")

	if err := startPhase(resourceModelProgressStepCompileWeakRefOwnershipPredicates, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("compile negative-priority ref ownership predicates …")
	compiledNegativePriorityWithRuntime, err := compileRefOwnershipPredicateLines(appOrder, negativePriorityWR, envByApp)
	if err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	advance("compile negative-priority ref ownership predicates")

	if err := startPhase(resourceModelProgressStepTemplateIDAssignment, nEntities); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("template id assignment …")

	assignment = cloneAssignmentMap(initialAssignment)
	if assignment == nil {
		assignment = make(map[types.Id]types.AppId)
	}
	for i, e := range clusterInNamespaces.Items {
		setDetail(fmt.Sprintf("template id assignment · %d / %d", i+1, nEntities))
		eid, idErr := e.Id()
		if idErr != nil {
			return nil, metadata, entity.Entities{}, idErr
		}
		if !isFocused(eid) {
			advance(fmt.Sprintf("template id assignment · %d / %d", i+1, nEntities))
			continue
		}
		if _, has := assignment[eid]; has {
			advance(fmt.Sprintf("template id assignment · %d / %d", i+1, nEntities))
			continue
		}
		if owner, ok := templateIDToApp[eid]; ok {
			assignWithReason(eid, owner, AssignmentReason{Kind: AssignmentReasonKindAssignedViaTemplateID})
		}
		advance(fmt.Sprintf("template id assignment · %d / %d", i+1, nEntities))
	}

	if err := startPhase(resourceModelProgressStepExpandOwnerReferencesPass1, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("expand owner references (pass 1) …")
	if err := expandAssignmentByOwnerRefsObserved(clusterInNamespaces, resourceModel, assignment, types.KeyClusterEntity, templateIDToApp,
		func(id types.Id, app types.AppId) {
			setAssignmentReason(id, app, ownerRefReasonByID[id])
		}, focusIDs); err != nil {
		return nil, metadata, entity.Entities{}, err
	}

	advance("expand owner references (pass 1)")

	assignByPriorityRef := func(i int, compiledByApp map[types.AppId][]compiledRefOwnershipPredicate, evaluateAssigned bool, ignoreSharedRules bool) (types.Id, types.AppId, []AssignmentReason, bool, bool, bool, error) {
		e := clusterInNamespaces.Items[i]
		eid, idErr := e.Id()
		if idErr != nil {
			return "", "", nil, false, false, false, idErr
		}
		if ambiguousIDs.Has(eid) {
			return eid, "", nil, false, false, false, nil
		}
		existingOwner, hasExistingOwner := assignment[eid]
		if hasExistingOwner && !evaluateAssigned {
			return eid, "", nil, false, false, false, nil
		}
		recordExistingAssignmentConflict := func(owner types.AppId, reasonsByApp map[types.AppId][]AssignmentReason) error {
			recordAmbiguous(eid, reasonsByApp, owner)
			return nil
		}
		matchOpts := refOwnershipMatchOptions{
			allowFallback: func(candidate entity.Entity) bool {
				return refOwnershipFallbackAllowed(candidate, assignment, templateIDToApp)
			},
		}
		if hasExistingOwner && evaluateAssigned {
			allowTemplateOrphanFallback := len(mergedInspectRefs) == 0
			var (
				eventApps         sets.Set[types.AppId]
				eventReasonsByApp map[types.AppId][]AssignmentReason
				foundEventRefs    bool
				eventErr          error
			)
			if shouldCollectConflictDetails(eid) {
				eventApps, eventReasonsByApp, foundEventRefs, eventErr = refOwnershipDetailedEventAppsFromAssignedSubjects(
					e, appOrder, compiledByApp, closure, assignment, templateIDToApp, allowTemplateOrphanFallback, matchOpts)
			} else {
				eventApps, foundEventRefs, eventErr = refOwnershipEventAppsFromAssignedSubjects(
					e, closure, assignment, templateIDToApp, allowTemplateOrphanFallback)
			}
			if eventErr != nil {
				return "", "", nil, false, false, false, eventErr
			} else if foundEventRefs {
				for app := range eventApps {
					if app == existingOwner {
						continue
					}
					if shouldCollectConflictDetails(eid) {
						reasonsByApp := map[types.AppId][]AssignmentReason{}
						reasonsByApp[app] = append(reasonsByApp[app], eventReasonsByApp[app]...)
						if err := recordExistingAssignmentConflict(app, reasonsByApp); err != nil {
							return "", "", nil, false, false, false, err
						}
					} else if err := recordExistingAssignmentConflict(app, nil); err != nil {
						return "", "", nil, false, false, false, err
					}
					return eid, "", nil, false, false, false, nil
				}
			}
			skipApp := existingOwner
			if ignoreSharedRules {
				_, reasonsByApp, reasonsErr := refOwnershipMatchingAppsCompiledWithReasonsWithOptions(
					e, appOrder, compiledByApp, closure, refOwnershipMatchOptions{
						skipApp:       &skipApp,
						allowFallback: matchOpts.allowFallback,
					})
				if reasonsErr != nil {
					return "", "", nil, false, false, false, reasonsErr
				}
				reasonsByApp = assignmentReasonMapSubset(assignmentReasons[eid][existingOwner], reasonsByApp)
				if len(reasonsByApp) > 0 {
					var conflictApp types.AppId
					foundConflict := false
					for app := range reasonsByApp {
						conflictApp = app
						foundConflict = true
						break
					}
					if foundConflict {
						if shouldCollectConflictDetails(eid) {
							if err := recordExistingAssignmentConflict(conflictApp, reasonsByApp); err != nil {
								return "", "", nil, false, false, false, err
							}
						} else if err := recordExistingAssignmentConflict(conflictApp, nil); err != nil {
							return "", "", nil, false, false, false, err
						}
					}
				}
			} else {
				conflictApp, foundConflict, conflictErr := refOwnershipFirstMatchingAppCompiledWithOptions(
					e, appOrder, compiledByApp, closure, refOwnershipMatchOptions{
						skipApp:       &skipApp,
						allowFallback: matchOpts.allowFallback,
					})
				if conflictErr != nil {
					return "", "", nil, false, false, false, conflictErr
				}
				if foundConflict {
					if shouldCollectConflictDetails(eid) {
						_, reasonsByApp, reasonsErr := refOwnershipMatchingAppsCompiledWithReasonsWithOptions(
							e, appOrder, compiledByApp, closure, refOwnershipMatchOptions{
								skipApp:       &skipApp,
								allowFallback: matchOpts.allowFallback,
							})
						if reasonsErr != nil {
							return "", "", nil, false, false, false, reasonsErr
						}
						if err := recordExistingAssignmentConflict(conflictApp, reasonsByApp); err != nil {
							return "", "", nil, false, false, false, err
						}
					} else if err := recordExistingAssignmentConflict(conflictApp, nil); err != nil {
						return "", "", nil, false, false, false, err
					}
				}
			}
			return eid, "", nil, false, false, false, nil
		}
		allowTemplateOrphanFallback := len(mergedInspectRefs) == 0
		var (
			eventApps         sets.Set[types.AppId]
			eventReasonsByApp map[types.AppId][]AssignmentReason
			foundEventRefs    bool
			eventErr          error
		)
		if shouldCollectConflictDetails(eid) {
			eventApps, eventReasonsByApp, foundEventRefs, eventErr = refOwnershipDetailedEventAppsFromAssignedSubjects(
				e, appOrder, compiledByApp, closure, assignment, templateIDToApp, allowTemplateOrphanFallback, matchOpts)
		} else {
			eventApps, foundEventRefs, eventErr = refOwnershipEventAppsFromAssignedSubjects(
				e, closure, assignment, templateIDToApp, allowTemplateOrphanFallback)
		}
		if eventErr != nil {
			return "", "", nil, false, false, false, eventErr
		} else if foundEventRefs {
			eventApps = preferSpecificAppsOverBuiltins(eventApps)
			switch eventApps.Len() {
			case 0:
			case 1:
				owner := eventApps.UnsortedList()[0]
				if hasExistingOwner {
					if owner != existingOwner {
						reasonsByApp := map[types.AppId][]AssignmentReason{}
						if shouldCollectConflictDetails(eid) {
							reasonsByApp[owner] = append(reasonsByApp[owner], eventReasonsByApp[owner]...)
						}
						if err := recordExistingAssignmentConflict(owner, reasonsByApp); err != nil {
							return "", "", nil, false, false, false, err
						}
					}
					return eid, "", nil, false, false, false, nil
				}
				return eid, owner, append([]AssignmentReason{}, eventReasonsByApp[owner]...), true, owner.IsPresetApp(), !owner.IsPresetApp(), nil
			default:
				ambiguousReasonsByApp := map[types.AppId][]AssignmentReason{}
				if shouldCollectConflictDetails(eid) {
					for app := range eventApps {
						ambiguousReasonsByApp[app] = append(ambiguousReasonsByApp[app], eventReasonsByApp[app]...)
					}
				}
				recordAmbiguous(eid, ambiguousReasonsByApp, eventApps.UnsortedList()...)
				return eid, "", nil, false, false, false, nil
			}
		}
		var (
			matching     sets.Set[types.AppId]
			reasonsByApp map[types.AppId][]AssignmentReason
			matchErr     error
		)
		if shouldCollectConflictDetails(eid) {
			matching, reasonsByApp, matchErr = refOwnershipMatchingAppsCompiledWithReasonsWithOptions(
				e, appOrder, compiledByApp, closure, matchOpts)
		} else {
			matching, matchErr = refOwnershipMatchingAppsCompiledFastWithOptions(
				e, appOrder, compiledByApp, closure, matchOpts)
		}
		if matchErr != nil {
			return "", "", nil, false, false, false, matchErr
		}
		if matching.Len() > 1 {
			if shouldCollectConflictDetails(eid) {
				ambiguousReasonsByApp := make(map[types.AppId][]AssignmentReason, matching.Len())
				for app := range matching {
					if len(reasonsByApp[app]) == 0 {
						ambiguousReasonsByApp[app] = appendReason(nil, AssignmentReason{Kind: AssignmentReasonKindAssignedViaRefOwnership})
						continue
					}
					for _, reason := range reasonsByApp[app] {
						ambiguousReasonsByApp[app] = appendReason(ambiguousReasonsByApp[app], reason)
					}
				}
				recordAmbiguous(eid, ambiguousReasonsByApp, matching.UnsortedList()...)
				return eid, "", nil, false, false, false, nil
			}
			recordAmbiguous(eid, nil, matching.UnsortedList()...)
			return eid, "", nil, false, false, false, nil
		}
		owner, ok, reconcileErr := reconcileClusterOwnership(nil, eid, matching)
		if reconcileErr != nil {
			return "", "", nil, false, false, false, reconcileErr
		}
		matchedByBuiltinRefParser := false
		matchedByAppRefParser := false
		if ok && matching.Has(owner) {
			if owner.IsPresetApp() {
				matchedByBuiltinRefParser = true
			} else {
				matchedByAppRefParser = true
			}
		}
		if ok && hasExistingOwner {
			if owner != existingOwner {
				if err := recordExistingAssignmentConflict(owner, reasonsByApp); err != nil {
					return "", "", nil, false, false, false, err
				}
			}
			return eid, "", nil, false, false, false, nil
		}
		return eid, owner, append([]AssignmentReason{}, reasonsByApp[owner]...), ok, matchedByBuiltinRefParser, matchedByAppRefParser, nil
	}

	var nonNegativePriorityCandidateIndexes []int
	if resourceModel != nil {
		nonNegativePriorityCandidateIndexes, err = resourceModel.clusterRefOwnershipEntityIndexesRootFirst(assignment, ambiguousIDs, focusIDs, true)
		if err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	} else {
		nonNegativePriorityCandidateIndexes, err = allRefOwnershipEntityIndexesRootFirst(clusterInNamespaces, assignment, ambiguousIDs, focusIDs, ownerRefChildren)
		if err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	}
	if err := startPhase(resourceModelProgressStepStrongTeardownRefs, len(nonNegativePriorityCandidateIndexes)); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	for pos, i := range nonNegativePriorityCandidateIndexes {
		setDetail(fmt.Sprintf("priority >= 0 teardown refs · %d / %d", pos+1, len(nonNegativePriorityCandidateIndexes)))
		eid, owner, ownerReasons, ok, matchedBuiltinRef, matchedAppRef, perr := assignByPriorityRef(i, compiledNonNegativePriorityWithRuntime, true, false)
		if perr != nil {
			return nil, metadata, entity.Entities{}, perr
		}
		if ok {
			if len(ownerReasons) == 0 {
				assignWithReason(eid, owner, AssignmentReason{Kind: AssignmentReasonKindAssignedViaRefOwnership})
			} else {
				assignWithDetailedReasons(assignment, assignmentReasons, eid, owner, ownerReasons)
			}
			assignOwnerRefDescendants(eid, owner, assignment, ambiguousIDs, ownerRefChildren, func(id types.Id, app types.AppId) {
				setAssignmentReason(id, app, ownerRefReasonByID[id])
			})
			if matchedBuiltinRef {
				metadata.DirectRefMatchedBuiltinIDs.Insert(eid)
			}
			if matchedAppRef {
				metadata.DirectRefMatchedAppIDs.Insert(eid)
			}
		}
		advance(fmt.Sprintf("priority >= 0 teardown refs · %d / %d", pos+1, len(nonNegativePriorityCandidateIndexes)))
	}

	if err := startPhase(resourceModelProgressStepExpandOwnerReferencesPass2, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("expand owner references (pass 2) …")
	if err := expandAssignmentByOwnerRefsObserved(clusterInNamespaces, resourceModel, assignment, types.KeyClusterEntity, templateIDToApp,
		func(id types.Id, app types.AppId) {
			setAssignmentReason(id, app, ownerRefReasonByID[id])
		}, focusIDs); err != nil {
		return nil, metadata, entity.Entities{}, err
	}

	advance("expand owner references (pass 2)")

	negativePriorityClusterEntityIDs := sets.New[types.Id]()
	var negativePriorityCandidateIndexes []int
	if resourceModel != nil {
		negativePriorityCandidateIndexes, err = resourceModel.clusterRefOwnershipEntityIndexesRootFirst(assignment, ambiguousIDs, focusIDs, true)
		if err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	} else {
		negativePriorityCandidateIndexes, err = allRefOwnershipEntityIndexesRootFirst(clusterInNamespaces, assignment, ambiguousIDs, focusIDs, ownerRefChildren)
		if err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	}
	if err := startPhase(resourceModelProgressStepWeakTeardownRefs, len(negativePriorityCandidateIndexes)); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	for pos, i := range negativePriorityCandidateIndexes {
		setDetail(fmt.Sprintf("priority < 0 teardown refs · %d / %d", pos+1, len(negativePriorityCandidateIndexes)))
		eid, owner, ownerReasons, ok, matchedBuiltinRef, matchedAppRef, perr := assignByPriorityRef(i, compiledNegativePriorityWithRuntime, true, true)
		if perr != nil {
			return nil, metadata, entity.Entities{}, perr
		}
		if ok {
			if len(ownerReasons) == 0 {
				assignWithReason(eid, owner, AssignmentReason{Kind: AssignmentReasonKindAssignedViaRefOwnership})
			} else {
				assignWithDetailedReasons(assignment, assignmentReasons, eid, owner, ownerReasons)
			}
			negativePriorityClusterEntityIDs.Insert(eid)
			assignOwnerRefDescendants(eid, owner, assignment, ambiguousIDs, ownerRefChildren, func(id types.Id, app types.AppId) {
				setAssignmentReason(id, app, ownerRefReasonByID[id])
			})
			if matchedBuiltinRef {
				metadata.DirectRefMatchedBuiltinIDs.Insert(eid)
			}
			if matchedAppRef {
				metadata.DirectRefMatchedAppIDs.Insert(eid)
			}
		}
		advance(fmt.Sprintf("priority < 0 teardown refs · %d / %d", pos+1, len(negativePriorityCandidateIndexes)))
	}

	var namespaceOwners map[types.Namespace]types.AppId
	if cluster != nil {
		namespaceOwners, err = hydra.HydraAppNamespaceOwners(cluster, allAppIds, networkMode)
		if err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	} else {
		namespaceOwners = map[types.Namespace]types.AppId{}
	}

	if err := startPhase(resourceModelProgressStepWeakOwnershipPropagation, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("negative-priority ownership propagation …")
	if negativePriorityClusterEntityIDs.Len() > 0 && len(mergedInspectRefs) > 0 {
		weakRefs := make([]types.Ref, 0, len(mergedInspectRefs))
		for _, ref := range mergedInspectRefs {
			if negativePriorityClusterEntityIDs.Has(ref.To) && isFocused(ref.To) {
				weakRefs = append(weakRefs, ref)
			}
		}
		wrapRecordAmbiguous := func(id types.Id, apps ...types.AppId) {
			reasonsByApp := map[types.AppId][]AssignmentReason{}
			if shouldCollectConflictDetails(id) {
				reasonsByApp = make(map[types.AppId][]AssignmentReason, len(apps))
				for _, app := range apps {
					reasonsByApp[app] = appendReason(reasonsByApp[app], AssignmentReason{Kind: AssignmentReasonKindAssignedViaInspectRef})
				}
			}
			recordAmbiguous(id, reasonsByApp, apps...)
		}
		if err := propagateOwnershipFromMergedInspectRefsCollect(weakRefs, templateIDToApp, assignment, namespaceOwners, &negativePriorityClusterEntityIDs, wrapRecordAmbiguous); err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	}
	advance("negative-priority ownership propagation")

	pvRefs := mergedInspectRefs
	if len(focusIDs) > 0 {
		pvRefs = nil
		for _, ref := range mergedInspectRefs {
			if isFocused(ref.To) {
				pvRefs = append(pvRefs, ref)
			}
		}
	}
	assignPersistentVolumesFromAssignedClaims(pvRefs, templateIDToApp, assignment,
		func(id types.Id, app types.AppId) {
			setAssignmentReason(id, app, AssignmentReason{Kind: AssignmentReasonKindAssignedViaInspectRef})
		})

	if err := startPhase(resourceModelProgressStepSoleTemplateNamespaceOwnership, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("sole-template namespace ownership …")
	unassignedPass, err := clusterInventoryUnassignedForAbort(clusterInNamespaces, assignment, types.KeyClusterEntity)
	if err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	if len(focusIDs) > 0 {
		unassignedPass, _, err = unassignedPass.SelectByIdSet(focusIDs)
		if err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	}
	unassignedPass, err = assignUnassignedClusterEntitiesInSoleTemplateNamespaces(unassignedPass, assignment, perAppRendered)
	if err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	advance("sole-template namespace ownership")

	if err := startPhase(resourceModelProgressStepOwnerNamespacesNamespaceObjects, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("ownerNamespaces Namespace objects …")
	namespaceAmbiguous := sets.New[types.Id]()
	if len(focusIDs) > 0 {
		namespaceAmbiguous = focusIDs
	}
	if err := ApplyOwnerNamespacesToNamespaceAssignments(templateIDToApp, namespaceOwners, clusterInNamespaces, assignment, namespaceAmbiguous, nil); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	advance("ownerNamespaces Namespace objects")

	if err := startPhase(resourceModelProgressStepOwnerNamespacesRemainingWorkloads, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("ownerNamespaces remaining workloads …")
	_, err = assignUnassignedClusterEntitiesByOwnerNamespaces(unassignedPass, assignment, namespaceOwners)
	if err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	advance("ownerNamespaces remaining workloads")

	if err := startPhase(resourceModelProgressStepValidateNamespaceOwnership, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("validate namespace ownership …")
	if len(focusIDs) == 0 {
		if err := validateLiveNamespacesFullyOwned(clusterInNamespaces, assignment, perAppRendered, namespaceOwners); err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	}
	advance("validate namespace ownership")

	if err := startPhase(resourceModelProgressStepClusterDefaultsPresets, nEntities); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	if cluster != nil {
		k8sMinor := 99
		if sm, smErr := KubernetesServerMinorVersion(cluster); smErr == nil && sm > 0 {
			k8sMinor = sm
		}
		_, presetRenderedAll, _, presetMergeErr := MergeBuiltinPresetAppsForCluster(
			cluster, allAppIds, networkMode, perAppRendered, renderedAllApps, k8sMinor)
		if presetMergeErr != nil {
			return nil, metadata, entity.Entities{}, presetMergeErr
		}
		effectivePresets, effectiveErr := hydra.EffectiveClusterDefaultsPresetsForCluster(cluster, allAppIds, networkMode, renderedAllApps, k8sMinor)
		if effectiveErr != nil {
			return nil, metadata, entity.Entities{}, effectiveErr
		}
		if len(effectivePresets) > 0 {
			presetEnv, presetEnvErr := cel.NewEnvWithEntityInventory(presetRenderedAll)
			if presetEnvErr != nil {
				return nil, metadata, entity.Entities{}, presetEnvErr
			}
			presetClosure, presetClosureErr := buildPresetClosureForAssignment(cluster, clusterInNamespaces, presetRenderedAll, nil)
			if presetClosureErr != nil {
				return nil, metadata, entity.Entities{}, presetClosureErr
			}
			presetCache, presetCacheErr := hydra.NewClusterDefaultsPresetEvalCache(effectivePresets, k8sMinor, presetEnv)
			if presetCacheErr != nil {
				return nil, metadata, entity.Entities{}, presetCacheErr
			}
			for i, e := range clusterInNamespaces.Items {
				setDetail(fmt.Sprintf("cluster-defaults presets · %d / %d", i+1, nEntities))
				eid, idErr := e.Id()
				if idErr != nil {
					return nil, metadata, entity.Entities{}, idErr
				}
				if !isFocused(eid) {
					advance(fmt.Sprintf("cluster-defaults presets · %d / %d", i+1, nEntities))
					continue
				}
				if _, has := assignment[eid]; has || ambiguousIDs.Has(eid) {
					advance(fmt.Sprintf("cluster-defaults presets · %d / %d", i+1, nEntities))
					continue
				}
				presetIDs, presetMatchErr := presetCache.MatchingPresetIDsWithRegarding(e, presetClosure, nil)
				if presetMatchErr != nil {
					return nil, metadata, entity.Entities{}, presetMatchErr
				}
				switch len(presetIDs) {
				case 0:
				case 1:
					presetApp, presetAppErr := types.NewPresetAppId(cluster.ClusterName, presetIDs[0])
					if presetAppErr != nil {
						return nil, metadata, entity.Entities{}, presetAppErr
					}
					assignWithReason(eid, presetApp, AssignmentReason{
						Kind:   AssignmentReasonKindAssignedViaPresetMatch,
						Preset: presetIDs[0],
					})
				default:
					apps := make([]types.AppId, 0, len(presetIDs))
					reasonsByApp := map[types.AppId][]AssignmentReason{}
					for _, presetID := range presetIDs {
						presetApp, presetAppErr := types.NewPresetAppId(cluster.ClusterName, presetID)
						if presetAppErr != nil {
							return nil, metadata, entity.Entities{}, presetAppErr
						}
						apps = append(apps, presetApp)
						if shouldCollectConflictDetails(eid) {
							reasonsByApp[presetApp] = append(reasonsByApp[presetApp], AssignmentReason{
								Kind:   AssignmentReasonKindAssignedViaPresetMatch,
								Preset: presetID,
							})
						}
					}
					if shouldCollectConflictDetails(eid) {
						recordAmbiguous(eid, reasonsByApp, apps...)
					} else {
						recordAmbiguous(eid, nil, apps...)
					}
				}
				advance(fmt.Sprintf("cluster-defaults presets · %d / %d", i+1, nEntities))
			}
		} else {
			for i := 0; i < nEntities; i++ {
				setDetail(fmt.Sprintf("cluster-defaults presets · %d / %d", i+1, nEntities))
				advance(fmt.Sprintf("cluster-defaults presets · %d / %d", i+1, nEntities))
			}
		}
	} else {
		for i := 0; i < nEntities; i++ {
			setDetail(fmt.Sprintf("cluster-defaults presets · %d / %d", i+1, nEntities))
			advance(fmt.Sprintf("cluster-defaults presets · %d / %d", i+1, nEntities))
		}
	}

	if err := applyClusterRootOverrides(
		cluster,
		allAppIds,
		renderedAllApps,
		clusterInNamespaces,
		assignment,
		assignmentReasons,
		&metadata,
		ambiguousIDs,
	); err != nil {
		return nil, metadata, entity.Entities{}, err
	}

	unassigned, err = clusterInventoryUnassignedForAbort(clusterInNamespaces, assignment, types.KeyClusterEntity)
	if err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	if len(focusIDs) > 0 {
		unassigned, _, err = unassigned.SelectByIdSet(focusIDs)
		if err != nil {
			return nil, metadata, entity.Entities{}, err
		}
	}
	for _, item := range unassigned.Items {
		id, idErr := item.Id()
		if idErr != nil {
			return nil, metadata, entity.Entities{}, idErr
		}
		metadata.UnassignedIDs.Insert(id)
	}
	if err := startPhase(resourceModelProgressStepAssignmentReady, 1); err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	setDetail("assignment ready …")
	advance("assignment ready")
	emptyUnassigned, err := entity.NewEntities(nil)
	if err != nil {
		return nil, metadata, entity.Entities{}, err
	}
	return assignment, metadata, emptyUnassigned, nil
}

// validateLiveNamespacesFullyOwned returns an error when a root v1/Namespace object is still
// unassigned after sole-template and ownerNamespaces passes (excluding well-known kube-* namespaces
// and objects with metadata.ownerReferences).
func validateLiveNamespacesFullyOwned(
	liveCluster entity.Entities,
	assignment map[types.Id]types.AppId,
	perAppRendered map[types.AppId]entity.Entities,
	namespaceOwners map[types.Namespace]types.AppId,
) error {
	byNS := TemplateAppsByNamespace(perAppRendered)
	for _, e := range liveCluster.Items {
		gvk, err := e.GVKString()
		if err != nil || gvk != types.KubernetesGvkV1Namespace {
			continue
		}
		eid, err := e.Id()
		if err != nil {
			return err
		}
		if _, has := assignment[eid]; has {
			continue
		}
		if owners := e.OwnerUids(types.KeyClusterEntity); owners != nil && owners.Len() > 0 {
			continue
		}
		ns, ok := namespaceNameFromV1NamespaceClusterID(eid)
		if !ok {
			continue
		}
		if ns == types.Namespace("default") {
			continue
		}
		if namespaceExemptFromSoleTemplateClusterOwnership(ns) {
			continue
		}
		apps := byNS[ns]
		nApps := 0
		if apps != nil {
			nApps = apps.Len()
		}
		_, declared := namespaceOwners[ns]
		if nApps > 1 && !declared {
			return log.CreateError(errors.ErrHydraConfigError,
				"namespace {ns} is rendered by multiple enabled apps; declare it under global.hydra.ownerNamespaces in exactly one owning app",
				log.String("ns", string(ns)))
		}
		if nApps == 0 {
			return log.CreateError(errors.ErrHydraConfigError,
				"live Namespace {ns} could not be assigned to an app (no enabled app renders workloads into this namespace)",
				log.String("ns", string(ns)))
		}
		return log.CreateError(errors.ErrHydraConfigError,
			"namespace {ns} could not be assigned after sole-template and ownerNamespaces passes (check ownerNamespaces and enabled apps)",
			log.String("ns", string(ns)))
	}
	return nil
}

// assignUnassignedClusterEntitiesInSoleTemplateNamespaces assigns unassigned namespaced resources to
// the only app that renders a template into that namespace when ref parsers and template ids did not
// already assign an owner. This covers runtime-created secrets and similar objects in single-app
// workload namespaces. Well-known multi-tenant Kubernetes namespaces are skipped.
func assignUnassignedClusterEntitiesInSoleTemplateNamespaces(
	unassigned entity.Entities,
	assignment map[types.Id]types.AppId,
	perAppRendered map[types.AppId]entity.Entities,
) (entity.Entities, error) {
	byNS := TemplateAppsByNamespace(perAppRendered)
	var kept []entity.Entity
	for _, e := range unassigned.Items {
		ns, ok := WorkloadNamespace(e)
		if !ok {
			kept = append(kept, e)
			continue
		}
		if namespaceExemptFromSoleTemplateClusterOwnership(ns) {
			kept = append(kept, e)
			continue
		}
		apps := byNS[ns]
		if apps == nil || apps.Len() != 1 {
			kept = append(kept, e)
			continue
		}
		eid, err := e.Id()
		if err != nil {
			kept = append(kept, e)
			continue
		}
		assignment[eid] = apps.UnsortedList()[0]
	}
	return entity.NewEntities(kept)
}

func namespaceExemptFromSoleTemplateClusterOwnership(ns types.Namespace) bool {
	switch string(ns) {
	case "kube-system", "kube-public", "kube-node-lease":
		return true
	default:
		return false
	}
}

// assignUnassignedClusterEntitiesByOwnerNamespaces assigns unassigned resources to the app
// declared in global.hydra.ownerNamespaces for their workload namespace. This resolves namespaces
// where several charts render (multiple template apps) but Hydra already declares a single owner.
func assignUnassignedClusterEntitiesByOwnerNamespaces(
	unassigned entity.Entities,
	assignment map[types.Id]types.AppId,
	namespaceOwners map[types.Namespace]types.AppId,
) (entity.Entities, error) {
	if len(namespaceOwners) == 0 {
		return unassigned, nil
	}
	var kept []entity.Entity
	for _, e := range unassigned.Items {
		ns, ok := WorkloadNamespace(e)
		if !ok {
			kept = append(kept, e)
			continue
		}
		if namespaceExemptFromSoleTemplateClusterOwnership(ns) {
			kept = append(kept, e)
			continue
		}
		app, has := namespaceOwners[ns]
		if !has {
			kept = append(kept, e)
			continue
		}
		eid, err := e.Id()
		if err != nil {
			kept = append(kept, e)
			continue
		}
		assignment[eid] = app
	}
	return entity.NewEntities(kept)
}

// clusterInventoryUnassignedForAbort lists cluster entities that still have no app assignment after
// ref matching and expandAssignmentByOwnerRefs. Objects with any metadata.ownerReferences are omitted:
// they are treated as Kubernetes-owned children even when the owner UID is not in the scanned
// inventory (so transitive expansion could not copy an app).
func clusterInventoryUnassignedForAbort(
	clusterInNamespaces entity.Entities,
	assignment map[types.Id]types.AppId,
	key types.EntityKeyUnstructured,
) (entity.Entities, error) {
	var items []entity.Entity
	for _, e := range clusterInNamespaces.Items {
		eid, err := e.Id()
		if err != nil {
			return entity.Entities{}, err
		}
		if _, exists := assignment[eid]; exists {
			continue
		}
		owners := e.OwnerUids(key)
		if owners != nil && owners.Len() > 0 {
			continue
		}
		items = append(items, e)
	}
	return entity.NewEntities(items)
}

func clusterResourceIDFromOwnerReference(apiVersion, kind, name string, ownerNamespace types.Namespace) (types.Id, error) {
	av, err := types.ParseApiVersion(apiVersion)
	if err != nil {
		return "", err
	}
	return types.NewId(av.Group, av.Version, types.Kind(kind), ownerNamespace, types.Name(name)), nil
}

func appendAppFromClusterOrTemplate(
	id types.Id,
	assignment map[types.Id]types.AppId,
	templateIDToApp map[types.Id]types.AppId,
	apps sets.Set[types.AppId],
) {
	if app, ok := assignment[id]; ok {
		apps.Insert(app)
	}
	if templateIDToApp != nil {
		if app, ok := templateIDToApp[id]; ok {
			apps.Insert(app)
		}
	}
}

// collectAppsFromKubernetesOwnerRefs resolves metadata.ownerReferences to app ids using, in order:
// UID lookup in live inventory, id built from apiVersion/kind/name (same workload namespace as the
// child for namespaced children), then standalone template ownership for those ids. This lets
// runtime children (Pods, etc.) inherit an app when a parent is only present in the template render
// or when UID-based resolution misses a parent that still exists under the canonical cluster id.
func collectAppsFromKubernetesOwnerRefs(
	e entity.Entity,
	key types.EntityKeyUnstructured,
	uidMap map[types.Uid]entity.Entity,
	clusterIndex map[types.Id]entity.Entity,
	assignment map[types.Id]types.AppId,
	templateIDToApp map[types.Id]types.AppId,
) (apps sets.Set[types.AppId], hasOwnerRefs bool, err error) {
	u, ok := e.Unstructured(key)
	if !ok {
		return nil, false, nil
	}
	refs := u.GetOwnerReferences()
	if len(refs) == 0 {
		return nil, false, nil
	}
	ownerNs, nsOK := WorkloadNamespace(e)
	if !nsOK {
		ownerNs = ""
	}
	out := sets.New[types.AppId]()
	for _, ref := range refs {
		if ref.APIVersion == "" || ref.Kind == "" || ref.Name == "" {
			continue
		}
		if ref.UID != "" {
			if parent, ok := uidMap[types.Uid(ref.UID)]; ok {
				pid, idErr := parent.Id()
				if idErr != nil {
					return nil, true, idErr
				}
				appendAppFromClusterOrTemplate(pid, assignment, templateIDToApp, out)
			}
		}
		ownerID, idErr := clusterResourceIDFromOwnerReference(string(ref.APIVersion), string(ref.Kind), string(ref.Name), ownerNs)
		if idErr != nil {
			continue
		}
		if parent, ok := clusterIndex[ownerID]; ok {
			pid, perr := parent.Id()
			if perr != nil {
				return nil, true, perr
			}
			appendAppFromClusterOrTemplate(pid, assignment, templateIDToApp, out)
		}
		appendAppFromClusterOrTemplate(ownerID, assignment, templateIDToApp, out)
	}
	return out, true, nil
}

// expandAssignmentByOwnerRefs copies app ownership along Kubernetes ownerReference edges: any cluster
// object whose direct owner is already mapped to exactly one app (via live assignment or
// standalone template ownership for the parent's cluster id) is assigned the same app.
// Runs to a fixpoint so chains like Pod → ReplicaSet → Deployment inherit the Deployment's app once
// the Deployment is owned via template id or ref parsers. If direct owners resolve to more than one
// distinct app, returns ErrUninstallAmbiguousRefOwnership.
func expandAssignmentByOwnerRefs(
	clusterInNamespaces entity.Entities,
	assignment map[types.Id]types.AppId,
	key types.EntityKeyUnstructured,
	templateIDToApp map[types.Id]types.AppId,
) error {
	return expandAssignmentByOwnerRefsObserved(clusterInNamespaces, nil, assignment, key, templateIDToApp, nil, nil)
}

func expandAssignmentByOwnerRefsObserved(
	clusterInNamespaces entity.Entities,
	resourceModel *ResourceModel,
	assignment map[types.Id]types.AppId,
	key types.EntityKeyUnstructured,
	templateIDToApp map[types.Id]types.AppId,
	observer assignmentObserver,
	focusIDs sets.Set[types.Id],
) error {
	uidMap := clusterInNamespaces.UidMap(key)
	var (
		childIndex ownerRefChildIndex
		err        error
	)
	if resourceModel != nil && key == types.KeyClusterEntity {
		childIndex, err = resourceModel.clusterOwnerRefChildIndex()
		if err != nil {
			return err
		}
	} else {
		childIndex, err = buildOwnerRefChildIndex(clusterInNamespaces, key)
		if err != nil {
			return err
		}
	}
	clusterIndex := make(map[types.Id]entity.Entity, clusterInNamespaces.Len())
	for _, ent := range clusterInNamespaces.Items {
		id, err := ent.Id()
		if err != nil {
			return err
		}
		clusterIndex[id] = ent
	}
	maxSteps := clusterInNamespaces.Len() + 1
	ambiguousIDs := sets.New[types.Id]()
	for step := 0; step < maxSteps; step++ {
		changed := false
		var indexes []int
		if resourceModel != nil && key == types.KeyClusterEntity {
			indexes, err = resourceModel.clusterRefOwnershipEntityIndexesRootFirst(assignment, ambiguousIDs, focusIDs, false)
			if err != nil {
				return err
			}
		} else {
			indexes, err = pendingRefOwnershipEntityIndexesRootFirst(clusterInNamespaces, assignment, ambiguousIDs, focusIDs, childIndex)
			if err != nil {
				return err
			}
		}
		for _, i := range indexes {
			e := clusterInNamespaces.Items[i]
			eid, err := e.Id()
			if err != nil {
				return err
			}
			if _, exists := assignment[eid]; exists {
				continue
			}
			apps, hasOwners, err := collectAppsFromKubernetesOwnerRefs(e, key, uidMap, clusterIndex, assignment, templateIDToApp)
			if err != nil {
				return err
			}
			if !hasOwners {
				continue
			}
			switch apps.Len() {
			case 0:
				continue
			case 1:
				app := apps.UnsortedList()[0]
				assignment[eid] = app
				if observer != nil {
					observer(eid, app)
				}
				assignOwnerRefDescendants(eid, app, assignment, ambiguousIDs, childIndex, observer)
				changed = true
			default:
				ids := apps.UnsortedList()
				slices.SortFunc(ids, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })
				return log.CreateError(errors.ErrUninstallAmbiguousRefOwnership,
					"cluster resource {id} has owner reference(s) resolving to more than one app ({apps}); fix ownership or ref parsers",
					log.String("id", string(eid)),
					log.String("apps", fmt.Sprintf("%v", ids)))
			}
		}
		if !changed {
			return nil
		}
	}
	return log.CreateError(errors.ErrInternalError,
		"internal: owner-reference expansion did not converge within {steps} steps",
		log.Int("steps", maxSteps))
}

// OwnerRefAmbiguousAssignment records a resource whose ownerReferences point at multiple distinct
// assigned apps (cluster review soft path).
type OwnerRefAmbiguousAssignment struct {
	EntityID types.Id
	AppIDs   []types.AppId
}

// expandAssignmentByOwnerRefsSoft is like expandAssignmentByOwnerRefs but records ambiguous
// owner-reference cases instead of returning an error. Used by hydra gitops review ref ownership.
func expandAssignmentByOwnerRefsSoft(
	clusterInNamespaces entity.Entities,
	assignment map[types.Id]types.AppId,
	key types.EntityKeyUnstructured,
	templateIDToApp map[types.Id]types.AppId,
	ambiguous *[]OwnerRefAmbiguousAssignment,
) error {
	uidMap := clusterInNamespaces.UidMap(key)
	childIndex, err := buildOwnerRefChildIndex(clusterInNamespaces, key)
	if err != nil {
		return err
	}
	clusterIndex := make(map[types.Id]entity.Entity, clusterInNamespaces.Len())
	for _, ent := range clusterInNamespaces.Items {
		id, err := ent.Id()
		if err != nil {
			return err
		}
		clusterIndex[id] = ent
	}
	recordedAmbiguous := sets.New[types.Id]()
	maxSteps := clusterInNamespaces.Len() + 1
	for step := 0; step < maxSteps; step++ {
		changed := false
		indexes, err := pendingRefOwnershipEntityIndexesRootFirst(clusterInNamespaces, assignment, recordedAmbiguous, nil, childIndex)
		if err != nil {
			return err
		}
		for _, i := range indexes {
			e := clusterInNamespaces.Items[i]
			eid, err := e.Id()
			if err != nil {
				return err
			}
			if _, exists := assignment[eid]; exists {
				continue
			}
			apps, hasOwners, err := collectAppsFromKubernetesOwnerRefs(e, key, uidMap, clusterIndex, assignment, templateIDToApp)
			if err != nil {
				return err
			}
			if !hasOwners {
				continue
			}
			switch apps.Len() {
			case 0:
				continue
			case 1:
				app := apps.UnsortedList()[0]
				assignment[eid] = app
				assignOwnerRefDescendants(eid, app, assignment, recordedAmbiguous, childIndex, nil)
				changed = true
			default:
				if ambiguous != nil && !recordedAmbiguous.Has(eid) {
					recordedAmbiguous.Insert(eid)
					ids := apps.UnsortedList()
					slices.SortFunc(ids, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })
					*ambiguous = append(*ambiguous, OwnerRefAmbiguousAssignment{EntityID: eid, AppIDs: ids})
				}
			}
		}
		if !changed {
			return nil
		}
	}
	return log.CreateError(errors.ErrInternalError,
		"internal: owner-reference expansion (soft) did not converge within {steps} steps",
		log.Int("steps", maxSteps))
}

// StakeholderAppsByNamespace merges template stakeholders with apps owning cluster inventory in each namespace.
func StakeholderAppsByNamespace(
	templateByNs map[types.Namespace]sets.Set[types.AppId],
	assignment map[types.Id]types.AppId,
	clusterInNamespaces entity.Entities,
) (map[types.Namespace]sets.Set[types.AppId], error) {
	result := make(map[types.Namespace]sets.Set[types.AppId])
	for ns, apps := range templateByNs {
		cp := sets.New[types.AppId]()
		cp.Insert(apps.UnsortedList()...)
		result[ns] = cp
	}

	for _, e := range clusterInNamespaces.Items {
		eid, err := e.Id()
		if err != nil {
			return nil, err
		}
		app, ok := assignment[eid]
		if !ok {
			continue
		}
		if app.IsPresetApp() {
			continue
		}
		ns, ok := WorkloadNamespace(e)
		if !ok {
			continue
		}
		if result[ns] == nil {
			result[ns] = sets.New[types.AppId]()
		}
		result[ns].Insert(app)
	}
	return result, nil
}

func StakeholderAppsByNamespaceFromUnifiedEntities(
	templateByNs map[types.Namespace]sets.Set[types.AppId],
	items []InventoryEntity,
) (map[types.Namespace]sets.Set[types.AppId], error) {
	result := make(map[types.Namespace]sets.Set[types.AppId])
	for ns, apps := range templateByNs {
		cp := sets.New[types.AppId]()
		cp.Insert(apps.UnsortedList()...)
		result[ns] = cp
	}
	for _, item := range items {
		if !item.HasLive || !item.HasAssignedApp {
			continue
		}
		if item.AssignedApp.IsPresetApp() {
			continue
		}
		ns, ok := WorkloadNamespace(item.Live)
		if !ok {
			continue
		}
		if result[ns] == nil {
			result[ns] = sets.New[types.AppId]()
		}
		result[ns].Insert(item.AssignedApp)
	}
	return result, nil
}

func StakeholderAppsByNamespaceFromResourceRows(
	templateByNs map[types.Namespace]sets.Set[types.AppId],
	items []ResourceModelRow,
) (map[types.Namespace]sets.Set[types.AppId], error) {
	result := make(map[types.Namespace]sets.Set[types.AppId])
	for ns, apps := range templateByNs {
		cp := sets.New[types.AppId]()
		cp.Insert(apps.UnsortedList()...)
		result[ns] = cp
	}
	for _, item := range items {
		if !item.HasCluster || !item.HasAssignedApp {
			continue
		}
		if item.AssignedApp.IsPresetApp() {
			continue
		}
		ns, ok := WorkloadNamespace(item.Cluster)
		if !ok {
			continue
		}
		if result[ns] == nil {
			result[ns] = sets.New[types.AppId]()
		}
		result[ns].Insert(item.AssignedApp)
	}
	return result, nil
}

// NamespacesAllowingUninstallSafe returns namespaces where every stakeholder app is in selectedAppIds
// (or there are no stakeholders in that namespace).
func NamespacesAllowingUninstallSafe(
	stakeholders map[types.Namespace]sets.Set[types.AppId],
	selectedAppIds sets.Set[types.AppId],
) sets.Set[types.Namespace] {
	allowed := sets.New[types.Namespace]()
	for ns, apps := range stakeholders {
		if stakeholdersAllowUninstallSafe(apps, selectedAppIds) {
			allowed.Insert(ns)
		}
	}
	return allowed
}

func stakeholdersAllowUninstallSafe(apps sets.Set[types.AppId], selectedAppIds sets.Set[types.AppId]) bool {
	if apps.Len() == 0 {
		return true
	}
	for a := range apps {
		if !selectedAppIds.Has(a) {
			return false
		}
	}
	return true
}

// IntersectNamespaces returns a ∩ b.
func IntersectNamespaces(a, b sets.Set[types.Namespace]) sets.Set[types.Namespace] {
	out := sets.New[types.Namespace]()
	for ns := range a {
		if b.Has(ns) {
			out.Insert(ns)
		}
	}
	return out
}
