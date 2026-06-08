package hydra

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/workloadclosure"
	"k8s.io/apimachinery/pkg/util/sets"
)

// clusterDefaultsCelCompileOrigin identifies one CEL row in merged cluster-defaults presets (builtin YAML + Helm overrides).
func clusterDefaultsCelCompileOrigin(eff ClusterDefaultsPresetEffective, predicateGroup string, lineIdx int) string {
	return fmt.Sprintf(
		`cluster-defaults preset %q · predicate group %q · cel[%d] · builtin hydra/hydra-go/core/hydra/embed/presets/%s.yaml · Helm override path global.hydra.presets.%s.predicates.%q.cel`,
		eff.ID, predicateGroup, lineIdx, eff.ID, eff.ID, predicateGroup,
	)
}

func clusterDefaultsExplicitIdOrigin(eff ClusterDefaultsPresetEffective, predicateGroup, hydraID string) string {
	return fmt.Sprintf(
		`cluster-defaults preset %q · predicate group %q · explicit id %q`,
		eff.ID, predicateGroup, hydraID,
	)
}

func clusterDefaultsSelectorRuleLabel(selector types.RefSelector) string {
	parts := make([]string, 0, 5)
	if selector.Group != "" || selector.Version != "" || selector.Kind != "" {
		gvk := strings.Trim(strings.Join([]string{string(selector.Group), string(selector.Version), string(selector.Kind)}, "/"), "/")
		if gvk != "" {
			parts = append(parts, "  gvk: "+gvk)
		}
	}
	if selector.Namespace != "" {
		parts = append(parts, "  namespace: "+string(selector.Namespace))
	}
	if selector.Name != "" {
		parts = append(parts, "  name: "+string(selector.Name))
	}
	return strings.Join(parts, "\n")
}

func clusterDefaultsCelRuleLabel(eff ClusterDefaultsPresetEffective, predicateGroup string, line ClusterDefaultsCelLine) string {
	var b strings.Builder
	b.WriteString("preset: ")
	b.WriteString(eff.ID)
	b.WriteString("\n")
	b.WriteString("predicateGroup: ")
	b.WriteString(predicateGroup)
	if selector := clusterDefaultsSelectorRuleLabel(line.Selector); selector != "" {
		b.WriteString("\nselector:\n")
		b.WriteString(selector)
	}
	if expr := strings.TrimSpace(line.Expr); expr != "" {
		b.WriteString("\ncel: |\n")
		for _, rawLine := range strings.Split(expr, "\n") {
			b.WriteString("  ")
			b.WriteString(strings.TrimRight(rawLine, " \t"))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func clusterDefaultsExplicitIdRuleLabel(eff ClusterDefaultsPresetEffective, predicateGroup, hydraID string) string {
	return fmt.Sprintf("preset: %s\npredicateGroup: %s\nid: %s", eff.ID, predicateGroup, hydraID)
}

// ClusterDefaultsPresetEvalCache holds compiled cluster-defaults preset CEL. Build once with
// [NewClusterDefaultsPresetEvalCache], then call [ClusterDefaultsPresetEvalCache.MatchingPresetIDs]
// for each entity instead of recompiling per call.
type ClusterDefaultsPresetEvalCache struct {
	presets []clusterDefaultsPresetEvalCompiled
}

type ClusterDefaultsPresetMatch struct {
	PresetID string
	Rule     string
	Direct   bool
}

type ClusterDefaultsBatchMatchOptions struct {
	SkipIDs       sets.Set[types.Id]
	OwnerAppsByID map[types.Id]sets.Set[types.AppId]
}

type clusterDefaultsPresetEvalCompiled struct {
	id               string
	enabled          bool
	blocks           []clusterDefaultsPredicateEvalBlock
	excludeBlocks    []clusterDefaultsPredicateEvalBlock
	anchorPredicates []cel.Predicate // preset anchors used by owner/ref closure
	activates        sets.Set[string]
	activatesDirect  sets.Set[string]
	activatedBy      sets.Set[string]
}

type clusterDefaultsPredicateEvalBlock struct {
	ids          []types.Id
	idSet        sets.Set[types.Id]
	idRules      map[types.Id]string
	programs     []cel.Predicate
	programRules []string
}

// NewClusterDefaultsPresetEvalCache compiles all CEL lines for the given effective presets once.
func NewClusterDefaultsPresetEvalCache(
	effective []ClusterDefaultsPresetEffective,
	k8sMinor int,
	env cel.Env,
) (*ClusterDefaultsPresetEvalCache, error) {
	presets := make([]clusterDefaultsPresetEvalCompiled, 0, len(effective))
	for _, eff := range effective {
		pc := clusterDefaultsPresetEvalCompiled{id: eff.ID, enabled: eff.Enabled}
		if !eff.Enabled {
			presets = append(presets, pc)
			continue
		}
		blocks, err := buildEvalBlocksForPreset(eff, k8sMinor, env)
		if err != nil {
			return nil, err
		}
		pc.blocks = blocks
		excludeBlocks, err := buildExcludeEvalBlocksForPreset(eff, env)
		if err != nil {
			return nil, err
		}
		pc.excludeBlocks = excludeBlocks
		anchorPrograms, err := buildAnchorProgramsForPresetClosure(eff, k8sMinor, env)
		if err != nil {
			return nil, err
		}
		pc.anchorPredicates = anchorPrograms
		pc.activates = enabledPositiveActivateClosure(eff.ID, effective, k8sMinor)
		pc.activatesDirect = sets.New[string](clusterDefaultsPositiveActivateTargetsForMinor(eff.Activates, k8sMinor)...)
		presets = append(presets, pc)
	}
	byID := make(map[string]*clusterDefaultsPresetEvalCompiled, len(presets))
	for i := range presets {
		byID[presets[i].id] = &presets[i]
	}
	for i := range presets {
		if !presets[i].enabled || presets[i].activatesDirect == nil {
			continue
		}
		for target := range presets[i].activatesDirect {
			tgt, ok := byID[target]
			if !ok || !tgt.enabled {
				continue
			}
			if tgt.activatedBy == nil {
				tgt.activatedBy = sets.New[string]()
			}
			tgt.activatedBy.Insert(presets[i].id)
		}
	}
	return &ClusterDefaultsPresetEvalCache{presets: presets}, nil
}

// MatchingPresetIDs returns preset ids that match the entity, using precompiled CEL from
// [NewClusterDefaultsPresetEvalCache].
func (c *ClusterDefaultsPresetEvalCache) MatchingPresetIDs(e entity.Entity) ([]string, error) {
	return c.MatchingPresetIDsWithRegarding(e, workloadclosure.EmptyMatchInput(types.KeyClusterEntity), nil)
}

// MatchingPresetIDsWithRegarding matches builtins using direct predicate blocks first; when closure
// carries refs and entities, also matches related entities against every preset anchor via
// ownerReferences and ref/regarding fallbacks.
// When timing is non-nil, each outer preset iteration records wall time via [ClusterDefaultsPresetMatchTiming.Record].
func (c *ClusterDefaultsPresetEvalCache) MatchingPresetIDsWithRegarding(
	e entity.Entity,
	closure workloadclosure.MatchInput,
	timing *ClusterDefaultsPresetMatchTiming,
) ([]string, error) {
	if addonEvent, err := clusterDefaultsEventRegardingAddon(e, closure); err != nil {
		return nil, err
	} else if addonEvent {
		return c.matchingPresetIDsWithClosureAnchors(e, closure, timing)
	}
	id, err := e.Id()
	if err != nil {
		return nil, err
	}
	directMatchesByEntity := make(map[types.Id][]ClusterDefaultsPresetMatch)
	directByEntity := make(map[types.Id]sets.Set[string])
	if err := c.computeDirectMatchesForEntity(e, timing, directMatchesByEntity, directByEntity); err != nil {
		return nil, err
	}
	liveByID := make(map[types.Id]entity.Entity, len(closure.EntityByID)+1)
	for eid, item := range closure.EntityByID {
		liveByID[eid] = item
	}
	liveByID[id] = e
	resolver := clusterDefaultsBatchEntityResolver{
		cache:                 c,
		closure:               closure,
		liveByID:              liveByID,
		directMatchesByEntity: directMatchesByEntity,
		directByEntity:        directByEntity,
	}
	matches, err := resolver.finalMatchesForEntity(id)
	if err != nil {
		return nil, err
	}
	presetIDs := uniquePresetIDs(matches)
	matched := sets.New[string](presetIDs...)
	matched = c.expandMatchedActivationWrappers(matched)
	ids := c.matchedPresetIDsInOrder(matched)
	return c.filterActivatingPresetMatches(ids, matched, directByEntity[id]), nil
}

func (c *ClusterDefaultsPresetEvalCache) matchingPresetIDsWithClosureAnchors(
	e entity.Entity,
	closure workloadclosure.MatchInput,
	timing *ClusterDefaultsPresetMatchTiming,
) ([]string, error) {
	matched := sets.New[string]()
	direct := sets.New[string]()
	for _, p := range c.presets {
		var t0 time.Time
		if timing != nil {
			t0 = time.Now()
		}
		ok, err := p.matches(e)
		if err != nil {
			if timing != nil {
				timing.Record(p.id, time.Since(t0))
			}
			return nil, err
		}
		if ok {
			matched.Insert(p.id)
			direct.Insert(p.id)
			if timing != nil {
				timing.Record(p.id, time.Since(t0))
			}
			continue
		}
		if !p.enabled || len(p.anchorPredicates) == 0 {
			if timing != nil {
				timing.Record(p.id, time.Since(t0))
			}
			continue
		}
		for _, anchor := range p.anchorPredicates {
			viaClosureMatched, err := closure.PredicateMatches(e, anchor)
			if err != nil {
				if timing != nil {
					timing.Record(p.id, time.Since(t0))
				}
				return nil, err
			}
			if viaClosureMatched {
				matched.Insert(p.id)
				break
			}
		}
		if timing != nil {
			timing.Record(p.id, time.Since(t0))
		}
	}
	matched = c.expandMatchedActivationWrappers(matched)
	ids := c.matchedPresetIDsInOrder(matched)
	return c.filterActivatingPresetMatches(ids, matched, direct), nil
}

func (c *ClusterDefaultsPresetEvalCache) expandMatchedActivationWrappers(matched sets.Set[string]) sets.Set[string] {
	if matched.Len() == 0 {
		return matched
	}
	out := sets.New[string](matched.UnsortedList()...)
	changed := true
	for changed {
		changed = false
		for _, preset := range c.presets {
			if !preset.enabled || out.Has(preset.id) || preset.activatesDirect == nil || preset.activatesDirect.Len() == 0 {
				continue
			}
			if preset.activatedBy == nil || preset.activatedBy.Len() == 0 {
				continue
			}
			for target := range preset.activatesDirect {
				if !out.Has(target) {
					continue
				}
				out.Insert(preset.id)
				changed = true
				break
			}
		}
	}
	return out
}

func (c *ClusterDefaultsPresetEvalCache) matchedPresetIDsInOrder(matched sets.Set[string]) []string {
	if matched.Len() == 0 {
		return nil
	}
	ids := make([]string, 0, matched.Len())
	for _, preset := range c.presets {
		if matched.Has(preset.id) {
			ids = append(ids, preset.id)
		}
	}
	return ids
}

// MatchingPresetIDsByEntityWithRegarding evaluates preset matches in batch for all live entities.
// When matchProgress is non-nil, it is invoked once per enabled preset before that preset is evaluated
// (done/total among enabled presets only).
func (c *ClusterDefaultsPresetEvalCache) MatchingPresetIDsByEntityWithRegarding(
	live entity.Entities,
	closure workloadclosure.MatchInput,
	matchProgress func(done int, total int, presetID string),
	profile *ClusterDefaultsBatchPresetProfile,
) (map[types.Id][]ClusterDefaultsPresetMatch, error) {
	return c.MatchingPresetIDsByEntityWithRegardingOptions(live, closure, matchProgress, profile, nil)
}

func (c *ClusterDefaultsPresetEvalCache) MatchingPresetIDsByEntityWithRegardingOptions(
	live entity.Entities,
	closure workloadclosure.MatchInput,
	matchProgress func(done int, total int, presetID string),
	profile *ClusterDefaultsBatchPresetProfile,
	opts *ClusterDefaultsBatchMatchOptions,
) (map[types.Id][]ClusterDefaultsPresetMatch, error) {
	out := make(map[types.Id][]ClusterDefaultsPresetMatch)
	if live.Len() == 0 {
		return out, nil
	}
	entityByID := make(map[types.Id]entity.Entity, live.Len())
	liveItemsByID := make(map[types.Id]entity.Entity, live.Len())
	filteredLiveItems := make([]entity.Entity, 0, live.Len())
	for _, item := range live.Items {
		id, err := item.Id()
		if err != nil {
			return nil, err
		}
		entityByID[id] = item
		liveItemsByID[id] = item
		if opts == nil || opts.SkipIDs == nil || !opts.SkipIDs.Has(id) {
			filteredLiveItems = append(filteredLiveItems, item)
		} else if profile.EnabledForPreset(ClusterDefaultsPresetIDKubernetes) {
			profile.RecordTemplateSkippedEntity()
		}
	}
	filteredLive, err := entity.NewEntities(filteredLiveItems)
	if err != nil {
		return nil, err
	}
	directMatchesByEntity := make(map[types.Id][]ClusterDefaultsPresetMatch)
	directByEntity := make(map[types.Id]sets.Set[string])
	appendDirectMatch := func(id types.Id, match ClusterDefaultsPresetMatch) {
		cur := directMatchesByEntity[id]
		for _, existing := range cur {
			if existing.PresetID == match.PresetID && existing.Rule == match.Rule && existing.Direct == match.Direct {
				return
			}
		}
		directMatchesByEntity[id] = append(cur, match)
	}
	markDirect := func(id types.Id, presetID string) {
		cur := directByEntity[id]
		if cur == nil {
			cur = sets.New[string]()
			directByEntity[id] = cur
		}
		cur.Insert(presetID)
	}
	enabledPresets := 0
	for _, preset := range c.presets {
		if preset.enabled {
			enabledPresets++
		}
	}
	donePreset := 0
	for _, preset := range c.presets {
		if !preset.enabled {
			continue
		}
		presetProfileEnabled := profile.EnabledForPreset(preset.id)
		presetStarted := time.Now()
		donePreset++
		if matchProgress != nil {
			matchProgress(donePreset, enabledPresets, preset.id)
		}
		matchedIDs := sets.New[types.Id]()
		matchesByID := make(map[types.Id][]ClusterDefaultsPresetMatch)
		for _, block := range preset.blocks {
			explicitStarted := time.Now()
			for _, id := range block.ids {
				if _, ok := entityByID[id]; ok && (opts == nil || opts.SkipIDs == nil || !opts.SkipIDs.Has(id)) {
					matchedIDs.Insert(id)
					matchesByID[id] = append(matchesByID[id], ClusterDefaultsPresetMatch{
						PresetID: preset.id,
						Rule:     block.idRules[id],
						Direct:   true,
					})
				}
			}
			if presetProfileEnabled {
				profile.RecordExplicitIDs(time.Since(explicitStarted), len(block.ids))
			}
			for i, prog := range block.programs {
				celStarted := time.Now()
				_, matched, err := prog.Select(filteredLive)
				if presetProfileEnabled {
					profile.RecordCEL(time.Since(celStarted))
				}
				if err != nil {
					return nil, err
				}
				for _, e := range matched.Items {
					id, err := e.Id()
					if err != nil {
						return nil, err
					}
					matchedIDs.Insert(id)
					matchesByID[id] = append(matchesByID[id], ClusterDefaultsPresetMatch{
						PresetID: preset.id,
						Rule:     block.programRules[i],
						Direct:   true,
					})
				}
			}
		}
		excludedIDs, err := matchingIDsForBlocks(preset.excludeBlocks, filteredLive, entityByID)
		if err != nil {
			return nil, err
		}
		for excludedID := range excludedIDs {
			delete(matchesByID, excludedID)
			matchedIDs.Delete(excludedID)
		}
		for id, matches := range matchesByID {
			markDirect(id, preset.id)
			for _, match := range matches {
				appendDirectMatch(id, match)
			}
		}
		if presetProfileEnabled && len(preset.anchorPredicates) > 0 {
			profile.SetAnchorPredicateCount(len(preset.anchorPredicates))
		}
		if presetProfileEnabled {
			profile.RecordTotal(time.Since(presetStarted))
		}
	}
	resolver := clusterDefaultsBatchEntityResolver{
		cache:                 c,
		closure:               closure,
		liveByID:              liveItemsByID,
		directMatchesByEntity: directMatchesByEntity,
		directByEntity:        directByEntity,
		profile:               profile,
	}
	if opts != nil {
		resolver.skipIDs = opts.SkipIDs
		resolver.ownerAppsByID = opts.OwnerAppsByID
	}
	ambiguityByMessage := map[string]clusterDefaultsPresetResolutionAmbiguity{}
	for _, item := range live.Items {
		id, err := item.Id()
		if err != nil {
			return nil, err
		}
		matches, err := resolver.finalMatchesForEntity(id)
		if err != nil {
			var ambiguity clusterDefaultsPresetResolutionAmbiguity
			if errors.As(err, &ambiguity) {
				ambiguityByMessage[ambiguity.Error()] = ambiguity
				continue
			}
			return nil, err
		}
		if len(matches) == 0 {
			continue
		}
		out[id] = matches
	}
	for id, matches := range out {
		presetIDs := make([]string, 0, len(matches))
		for _, match := range matches {
			presetIDs = append(presetIDs, match.PresetID)
		}
		slices.Sort(presetIDs)
		presetIDs = slices.Compact(presetIDs)
		matched := sets.New[string]()
		for _, presetID := range presetIDs {
			matched.Insert(presetID)
		}
		keepIDs := sets.New[string](c.filterActivatingPresetMatches(presetIDs, matched, directByEntity[id])...)
		filtered := make([]ClusterDefaultsPresetMatch, 0, len(matches))
		for _, match := range matches {
			if keepIDs.Has(match.PresetID) {
				filtered = append(filtered, match)
			}
		}
		slices.SortFunc(filtered, func(a, b ClusterDefaultsPresetMatch) int {
			if c := cmp.Compare(a.PresetID, b.PresetID); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Rule, b.Rule); c != 0 {
				return c
			}
			if a.Direct == b.Direct {
				return 0
			}
			if a.Direct {
				return -1
			}
			return 1
		})
		out[id] = filtered
	}
	if len(ambiguityByMessage) > 0 {
		items := make([]clusterDefaultsPresetResolutionAmbiguity, 0, len(ambiguityByMessage))
		for _, item := range ambiguityByMessage {
			items = append(items, item)
		}
		slices.SortFunc(items, func(a, b clusterDefaultsPresetResolutionAmbiguity) int {
			if c := cmp.Compare(string(a.EntityID), string(b.EntityID)); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Distance, b.Distance); c != 0 {
				return c
			}
			if c := cmp.Compare(a.MaxDistance, b.MaxDistance); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
				return c
			}
			return cmp.Compare(formatAmbiguityCandidates(a.Candidates), formatAmbiguityCandidates(b.Candidates))
		})
		return nil, clusterDefaultsPresetResolutionAmbiguities{Items: items}
	}
	return out, nil
}

type clusterDefaultsBatchEntityResolver struct {
	cache                 *ClusterDefaultsPresetEvalCache
	closure               workloadclosure.MatchInput
	liveByID              map[types.Id]entity.Entity
	directMatchesByEntity map[types.Id][]ClusterDefaultsPresetMatch
	directByEntity        map[types.Id]sets.Set[string]
	skipIDs               sets.Set[types.Id]
	ownerAppsByID         map[types.Id]sets.Set[types.AppId]
	inheritanceCache      map[types.Id]clusterDefaultsEntityInheritance
	resolving             sets.Set[types.Id]
	profile               *ClusterDefaultsBatchPresetProfile
}

type clusterDefaultsEntityInheritance struct {
	kind     string
	appID    types.AppId
	presetID string
}

type clusterDefaultsParentCandidate struct {
	id   types.Id
	path []clusterDefaultsPathHop
}

type clusterDefaultsPathHop struct {
	ID  types.Id
	Via workloadclosure.ParentVia
}

type clusterDefaultsAmbiguityCandidate struct {
	Name string
	Path []clusterDefaultsPathHop
}

type clusterDefaultsPresetResolutionAmbiguity struct {
	EntityID    types.Id
	Distance    int
	MaxDistance int
	Kind        string
	Candidates  []clusterDefaultsAmbiguityCandidate
}

func (e clusterDefaultsPresetResolutionAmbiguity) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "preset owner resolution ambiguous for %s at distance %d/%d: %s",
		e.EntityID, e.Distance, e.MaxDistance, e.Kind)
	for _, candidate := range e.Candidates {
		fmt.Fprintf(&b, "\n%s:", candidate.Name)
		for i, hop := range candidate.Path {
			fmt.Fprintf(&b, "\n- id: %s", hop.ID)
			if i > 0 && hop.Via != "" {
				fmt.Fprintf(&b, "\n  via: %s", hop.Via)
			}
		}
	}
	return b.String()
}

type clusterDefaultsPresetResolutionAmbiguities struct {
	Items []clusterDefaultsPresetResolutionAmbiguity
}

func (e clusterDefaultsPresetResolutionAmbiguities) Error() string {
	if len(e.Items) == 0 {
		return "preset owner resolution ambiguous"
	}
	lines := make([]string, 0, len(e.Items)+1)
	lines = append(lines, "preset owner resolution ambiguous:")
	for _, item := range e.Items {
		lines = append(lines, "- "+item.Error())
	}
	return strings.Join(lines, "\n")
}

const (
	clusterDefaultsInheritanceNone     = "none"
	clusterDefaultsInheritanceOwnerApp = "owner-app"
	clusterDefaultsInheritancePreset   = "preset"
)

func (r *clusterDefaultsBatchEntityResolver) finalMatchesForEntity(id types.Id) ([]ClusterDefaultsPresetMatch, error) {
	if r.skipIDs != nil && r.skipIDs.Has(id) {
		return nil, nil
	}
	outcome, rule, err := r.resolveInheritedOutcome(id)
	if err != nil {
		return nil, err
	}
	if outcome.kind == clusterDefaultsInheritanceOwnerApp {
		return nil, nil
	}
	if outcome.kind == clusterDefaultsInheritancePreset {
		return []ClusterDefaultsPresetMatch{{
			PresetID: outcome.presetID,
			Rule:     rule,
			Direct:   false,
		}}, nil
	}
	if r.profile != nil {
		r.profile.RecordDirectMatchedEntity()
	}
	return slices.Clone(r.directMatchesForEntity(id)), nil
}

func (r *clusterDefaultsBatchEntityResolver) resolveInheritedOutcome(id types.Id) (clusterDefaultsEntityInheritance, string, error) {
	layers, err := r.parentLayerCandidates(id)
	if err != nil {
		return clusterDefaultsEntityInheritance{}, "", err
	}
	maxDistance := len(layers)
	for distIdx, layer := range layers {
		if r.profile != nil {
			r.profile.RecordAnchorCandidateEntity()
			r.profile.RecordClosureLayer(len(layer))
		}
		ownerApps := sets.New[types.AppId]()
		presetIDs := sets.New[string]()
		presetSources := map[string][]types.Id{}
		ownerAppPaths := map[types.AppId][]clusterDefaultsPathHop{}
		presetPaths := map[string][]clusterDefaultsPathHop{}
		for _, parent := range layer {
			state, stateErr := r.inheritanceState(parent.id)
			if stateErr != nil {
				return clusterDefaultsEntityInheritance{}, "", stateErr
			}
			switch state.kind {
			case clusterDefaultsInheritanceOwnerApp:
				ownerApps.Insert(state.appID)
				recordAmbiguityPath(ownerAppPaths, state.appID, parent.path)
			case clusterDefaultsInheritancePreset:
				presetIDs.Insert(state.presetID)
				presetSources[state.presetID] = append(presetSources[state.presetID], parent.id)
				recordAmbiguityPath(presetPaths, state.presetID, parent.path)
			}
		}
		if ownerApps.Len() > 1 {
			return clusterDefaultsEntityInheritance{}, "", clusterDefaultsPresetResolutionAmbiguity{
				EntityID:    id,
				Distance:    distIdx + 1,
				MaxDistance: maxDistance,
				Kind:        "multiple owner apps",
				Candidates:  formatAmbiguityCandidatesFromPaths(ownerAppPaths),
			}
		}
		if ownerApps.Len() == 1 {
			if r.profile != nil {
				r.profile.RecordResolvedByOwnerApp()
			}
			return clusterDefaultsEntityInheritance{
				kind:  clusterDefaultsInheritanceOwnerApp,
				appID: ownerApps.UnsortedList()[0],
			}, "", nil
		}
		if presetIDs.Len() > 1 {
			return clusterDefaultsEntityInheritance{}, "", clusterDefaultsPresetResolutionAmbiguity{
				EntityID:    id,
				Distance:    distIdx + 1,
				MaxDistance: maxDistance,
				Kind:        "multiple presets",
				Candidates:  formatAmbiguityCandidatesFromPaths(presetPaths),
			}
		}
		if presetIDs.Len() == 1 {
			presetID := presetIDs.UnsortedList()[0]
			if r.profile != nil {
				r.profile.RecordResolvedByParentPreset()
			}
			return clusterDefaultsEntityInheritance{
				kind:     clusterDefaultsInheritancePreset,
				presetID: presetID,
			}, inheritedPresetRuleLabel(presetID, distIdx+1, presetSources[presetID]), nil
		}
	}
	return clusterDefaultsEntityInheritance{kind: clusterDefaultsInheritanceNone}, "", nil
}

func (r *clusterDefaultsBatchEntityResolver) inheritanceState(id types.Id) (clusterDefaultsEntityInheritance, error) {
	if r.inheritanceCache == nil {
		r.inheritanceCache = make(map[types.Id]clusterDefaultsEntityInheritance)
	}
	if cached, ok := r.inheritanceCache[id]; ok {
		return cached, nil
	}
	if r.resolving == nil {
		r.resolving = sets.New[types.Id]()
	}
	if r.resolving.Has(id) {
		return clusterDefaultsEntityInheritance{kind: clusterDefaultsInheritanceNone}, nil
	}
	r.resolving.Insert(id)
	defer r.resolving.Delete(id)

	if ownerApp, ok, err := r.uniqueOwnerAppForID(id); err != nil {
		return clusterDefaultsEntityInheritance{}, err
	} else if ok {
		state := clusterDefaultsEntityInheritance{kind: clusterDefaultsInheritanceOwnerApp, appID: ownerApp}
		r.inheritanceCache[id] = state
		return state, nil
	}

	if inherited, _, err := r.resolveInheritedOutcome(id); err != nil {
		return clusterDefaultsEntityInheritance{}, err
	} else if inherited.kind != clusterDefaultsInheritanceNone {
		state := inherited
		r.inheritanceCache[id] = state
		return state, nil
	}

	matches := r.directMatchesForEntity(id)
	presetIDs := uniquePresetIDs(matches)
	switch len(presetIDs) {
	case 0:
		state := clusterDefaultsEntityInheritance{kind: clusterDefaultsInheritanceNone}
		r.inheritanceCache[id] = state
		return state, nil
	case 1:
		state := clusterDefaultsEntityInheritance{kind: clusterDefaultsInheritancePreset, presetID: presetIDs[0]}
		r.inheritanceCache[id] = state
		return state, nil
	default:
		candidates := make([]clusterDefaultsAmbiguityCandidate, 0, len(presetIDs))
		for _, presetID := range presetIDs {
			candidates = append(candidates, clusterDefaultsAmbiguityCandidate{
				Name: presetID,
				Path: []clusterDefaultsPathHop{{ID: id}},
			})
		}
		return clusterDefaultsEntityInheritance{}, clusterDefaultsPresetResolutionAmbiguity{
			EntityID:    id,
			Distance:    0,
			MaxDistance: 0,
			Kind:        "multiple direct preset matches",
			Candidates:  candidates,
		}
	}
}

func (r *clusterDefaultsBatchEntityResolver) uniqueOwnerAppForID(id types.Id) (types.AppId, bool, error) {
	if r.ownerAppsByID == nil {
		return "", false, nil
	}
	apps := r.ownerAppsByID[id]
	if apps.Len() == 0 {
		return "", false, nil
	}
	if apps.Len() > 1 {
		appIDs := apps.UnsortedList()
		slices.SortFunc(appIDs, func(a, b types.AppId) int { return cmp.Compare(string(a), string(b)) })
		candidates := make([]clusterDefaultsAmbiguityCandidate, 0, len(appIDs))
		for _, appID := range appIDs {
			candidates = append(candidates, clusterDefaultsAmbiguityCandidate{
				Name: string(appID),
				Path: []clusterDefaultsPathHop{{ID: id}},
			})
		}
		return "", false, clusterDefaultsPresetResolutionAmbiguity{
			EntityID:    id,
			Distance:    0,
			MaxDistance: 0,
			Kind:        "multiple owner apps",
			Candidates:  candidates,
		}
	}
	return apps.UnsortedList()[0], true, nil
}

func (r *clusterDefaultsBatchEntityResolver) directMatchesForEntity(id types.Id) []ClusterDefaultsPresetMatch {
	if _, ok := r.directMatchesByEntity[id]; !ok {
		item, exists := r.liveByID[id]
		if !exists {
			var err error
			item, err = workloadclosure.MinimalClusterEntityFromID(id)
			if err != nil {
				return nil
			}
		}
		if err := r.cache.computeDirectMatchesForEntity(item, nil, r.directMatchesByEntity, r.directByEntity); err != nil {
			return nil
		}
	}
	matches := slices.Clone(r.directMatchesByEntity[id])
	if len(matches) == 0 {
		return nil
	}
	presetIDs := uniquePresetIDs(matches)
	matched := sets.New[string](presetIDs...)
	keepIDs := sets.New[string](r.cache.filterActivatingPresetMatches(presetIDs, matched, r.directByEntity[id])...)
	filtered := make([]ClusterDefaultsPresetMatch, 0, len(matches))
	for _, match := range matches {
		if keepIDs.Has(match.PresetID) {
			filtered = append(filtered, match)
		}
	}
	return filtered
}

func (r *clusterDefaultsBatchEntityResolver) parentLayerCandidates(id types.Id) ([][]clusterDefaultsParentCandidate, error) {
	visited := sets.New[types.Id](id)
	current := []clusterDefaultsParentCandidate{{
		id:   id,
		path: []clusterDefaultsPathHop{{ID: id}},
	}}
	var layers [][]clusterDefaultsParentCandidate
	for len(current) > 0 {
		nextByID := map[types.Id]clusterDefaultsParentCandidate{}
		for _, cur := range current {
			parents, err := r.parentCandidates(cur)
			if err != nil {
				return nil, err
			}
			for _, parent := range parents {
				if visited.Has(parent.id) {
					continue
				}
				existing, ok := nextByID[parent.id]
				if !ok || comparePathHops(parent.path, existing.path) < 0 {
					nextByID[parent.id] = parent
				}
			}
		}
		if len(nextByID) == 0 {
			break
		}
		nextIDs := make([]types.Id, 0, len(nextByID))
		for parentID := range nextByID {
			nextIDs = append(nextIDs, parentID)
		}
		slices.SortFunc(nextIDs, func(a, b types.Id) int { return cmp.Compare(string(a), string(b)) })
		layer := make([]clusterDefaultsParentCandidate, 0, len(nextIDs))
		for _, parentID := range nextIDs {
			layer = append(layer, nextByID[parentID])
			visited.Insert(parentID)
		}
		layers = append(layers, layer)
		current = layer
	}
	return layers, nil
}

func (r *clusterDefaultsBatchEntityResolver) parentCandidates(cur clusterDefaultsParentCandidate) ([]clusterDefaultsParentCandidate, error) {
	byID := map[types.Id]clusterDefaultsParentCandidate{}
	appendCandidate := func(parentID types.Id, via workloadclosure.ParentVia) {
		if parentID == "" || via == "" {
			return
		}
		path := append([]clusterDefaultsPathHop{}, cur.path...)
		path = append(path, clusterDefaultsPathHop{ID: parentID, Via: via})
		candidate := clusterDefaultsParentCandidate{id: parentID, path: path}
		existing, ok := byID[parentID]
		if !ok || comparePathHops(candidate.path, existing.path) < 0 {
			byID[parentID] = candidate
		}
	}

	item, ok := r.liveByID[cur.id]
	if !ok {
		var err error
		item, err = workloadclosure.MinimalClusterEntityFromID(cur.id)
		if err != nil {
			return nil, err
		}
	}
	if u, ok := item.Unstructured(r.closure.Key); ok {
		ownerNs := types.Namespace(u.GetNamespace())
		if ownerNs == "" {
			if _, _, _, ns, _, err := cur.id.Components(); err == nil {
				ownerNs = ns
			}
		}
		for _, ref := range u.GetOwnerReferences() {
			if ref.APIVersion == "" || ref.Kind == "" || ref.Name == "" {
				continue
			}
			if ref.UID != "" {
				if parent, ok := r.closure.UIDMap[types.Uid(ref.UID)]; ok {
					parentID, err := parent.Id()
					if err != nil {
						return nil, err
					}
					appendCandidate(parentID, workloadclosure.ParentViaOwnerRef)
					continue
				}
			}
			av, err := types.ParseApiVersion(ref.APIVersion)
			if err != nil {
				continue
			}
			parentID := types.NewId(av.Group, av.Version, types.Kind(ref.Kind), ownerNs, types.Name(ref.Name))
			if _, ok := r.closure.Index[parentID]; ok {
				appendCandidate(parentID, workloadclosure.ParentViaOwnerRef)
			}
		}
	}
	gvk, err := item.GVKString()
	if err != nil {
		return nil, err
	}
	if gvk == types.KubernetesGvkEventsK8sIoV1Event || gvk == types.KubernetesGvkV1Event {
		for _, ref := range r.closure.RefsByFrom[cur.id] {
			if ref.EndpointType != types.RefEndpointTypeId || ref.RefType != types.RefTypeRegarding {
				continue
			}
			appendCandidate(ref.To, workloadclosure.ParentViaRegardingRef)
		}
		for _, ref := range r.closure.RefsByTo[cur.id] {
			if ref.EndpointType != types.RefEndpointTypeId || !refLabelsContain(ref.Labels, "workloadRegardingEvent") {
				continue
			}
			appendCandidate(ref.From, workloadclosure.ParentViaWorkloadRegardingEventRef)
		}
	}
	for _, ref := range r.closure.RefsByFrom[cur.id] {
		source, ok := workloadclosure.ParentRefSourceFromRef(ref)
		if ok && source.Direction == workloadclosure.ParentRefDirectionOutgoing && ref.EndpointType == types.RefEndpointTypeId {
			appendCandidate(ref.To, source.Via)
		}
	}
	for _, ref := range r.closure.RefsByTo[cur.id] {
		source, ok := workloadclosure.ParentRefSourceFromRef(ref)
		if ok && source.Direction == workloadclosure.ParentRefDirectionIncoming && ref.EndpointType == types.RefEndpointTypeId {
			appendCandidate(ref.From, source.Via)
		}
	}

	parentIDs := make([]types.Id, 0, len(byID))
	for parentID := range byID {
		parentIDs = append(parentIDs, parentID)
	}
	slices.SortFunc(parentIDs, func(a, b types.Id) int { return cmp.Compare(string(a), string(b)) })
	out := make([]clusterDefaultsParentCandidate, 0, len(parentIDs))
	for _, parentID := range parentIDs {
		out = append(out, byID[parentID])
	}
	return out, nil
}

func uniquePresetIDs(matches []ClusterDefaultsPresetMatch) []string {
	if len(matches) == 0 {
		return nil
	}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.PresetID)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func inheritedPresetRuleLabel(presetID string, distance int, parentIDs []types.Id) string {
	ids := make([]string, 0, len(parentIDs))
	for _, id := range parentIDs {
		ids = append(ids, string(id))
	}
	slices.Sort(ids)
	var b strings.Builder
	fmt.Fprintf(&b, "preset: %s\ninheritedOwnerPresetDistance: %d\nownerPresetSourceIds:\n", presetID, distance)
	for _, id := range ids {
		fmt.Fprintf(&b, "  - %s\n", id)
	}
	return strings.TrimRight(b.String(), "\n")
}

func clusterDefaultsEventRegardingAddon(e entity.Entity, closure workloadclosure.MatchInput) (bool, error) {
	eid, err := e.Id()
	if err != nil {
		return false, err
	}
	gvk, err := e.GVKString()
	if err != nil {
		return false, err
	}
	if gvk != types.KubernetesGvkEventsK8sIoV1Event && gvk != types.KubernetesGvkV1Event {
		return false, nil
	}
	for _, ref := range closure.RefsByFrom[eid] {
		if ref.EndpointType != types.RefEndpointTypeId || ref.RefType != types.RefTypeRegarding {
			continue
		}
		subject, ok := closure.EntityByID[ref.To]
		if !ok {
			subject, err = workloadclosure.MinimalClusterEntityFromID(ref.To)
			if err != nil {
				return false, err
			}
		}
		subjectGVK, gvkErr := subject.GVKString()
		if gvkErr != nil {
			return false, gvkErr
		}
		if string(subjectGVK) == "k3s.cattle.io/v1/Addon" {
			return true, nil
		}
	}
	return false, nil
}

func (c *ClusterDefaultsPresetEvalCache) computeDirectMatchesForEntity(
	e entity.Entity,
	timing *ClusterDefaultsPresetMatchTiming,
	directMatchesByEntity map[types.Id][]ClusterDefaultsPresetMatch,
	directByEntity map[types.Id]sets.Set[string],
) error {
	id, err := e.Id()
	if err != nil {
		return err
	}
	appendDirectMatch := func(match ClusterDefaultsPresetMatch) {
		cur := directMatchesByEntity[id]
		for _, existing := range cur {
			if existing.PresetID == match.PresetID && existing.Rule == match.Rule && existing.Direct == match.Direct {
				return
			}
		}
		directMatchesByEntity[id] = append(cur, match)
	}
	markDirect := func(presetID string) {
		cur := directByEntity[id]
		if cur == nil {
			cur = sets.New[string]()
			directByEntity[id] = cur
		}
		cur.Insert(presetID)
	}
	for _, p := range c.presets {
		var t0 time.Time
		if timing != nil {
			t0 = time.Now()
		}
		ok, err := p.matches(e)
		if timing != nil {
			timing.Record(p.id, time.Since(t0))
		}
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		markDirect(p.id)
		appendDirectMatch(ClusterDefaultsPresetMatch{
			PresetID: p.id,
			Direct:   true,
		})
	}
	return nil
}

func buildAnchorProgramsForPresetClosure(
	eff ClusterDefaultsPresetEffective,
	k8sMinor int,
	env cel.Env,
) ([]cel.Predicate, error) {
	var progs []cel.Predicate
	seen := sets.New[string]()
	names := make([]string, 0, len(eff.Predicates))
	for name := range eff.Predicates {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int { return cmp.Compare(a, b) })
	for _, groupName := range names {
		pe := eff.Predicates[groupName]
		if !pe.Enabled || !ClusterDefaultsPredicateMinorApplies(k8sMinor, pe) {
			continue
		}
		for lineIdx, line := range pe.CelLines {
			key := selectorKey(line.Selector) + "::" + line.Expr
			if seen.Has(key) {
				continue
			}
			var (
				prog cel.Predicate
				err  error
			)
			celOrigin := clusterDefaultsCelCompileOrigin(eff, groupName, lineIdx)
			if line.Expr == "" {
				prog, err = env.CompileSelectedPredicateAt(celOrigin, line.Selector)
			} else {
				prog, err = env.CompileSelectedPredicateAt(celOrigin, line.Selector, types.CelPredicate(line.Expr))
			}
			if err != nil {
				return nil, err
			}
			progs = append(progs, prog)
			seen.Insert(key)
		}
		for _, idl := range pe.Ids {
			selector, _, err := types.RefSelectorInput{Id: idl.Id}.Normalized()
			if err != nil {
				return nil, err
			}
			key := selectorKey(selector)
			if seen.Has(key) {
				continue
			}
			prog, err := env.CompileSelectedPredicateAt(clusterDefaultsExplicitIdOrigin(eff, groupName, string(idl.Id)), selector)
			if err != nil {
				return nil, err
			}
			progs = append(progs, prog)
			seen.Insert(key)
		}
	}
	for idx, item := range eff.ManualOverrides {
		if item.Exclude {
			continue
		}
		prog, err := compileClusterRootOverridePredicate(env, eff, idx, item)
		if err != nil {
			return nil, err
		}
		progs = append(progs, prog)
	}
	return progs, nil
}

func (p clusterDefaultsPresetEvalCompiled) matches(e entity.Entity) (bool, error) {
	if !p.enabled {
		return false, nil
	}
	if matched, err := evalBlocksMatch(p.excludeBlocks, e); err != nil {
		return false, err
	} else if matched {
		return false, nil
	}
	return evalBlocksMatch(p.blocks, e)
}

func buildEvalBlocksForPreset(
	eff ClusterDefaultsPresetEffective,
	k8sMinor int,
	env cel.Env,
) ([]clusterDefaultsPredicateEvalBlock, error) {
	names := make([]string, 0, len(eff.Predicates))
	for n := range eff.Predicates {
		names = append(names, n)
	}
	slices.SortFunc(names, func(a, b string) int { return cmp.Compare(a, b) })
	var blocks []clusterDefaultsPredicateEvalBlock
	for _, name := range names {
		pe := eff.Predicates[name]
		if !pe.Enabled {
			continue
		}
		if !ClusterDefaultsPredicateMinorApplies(k8sMinor, pe) {
			continue
		}
		block := clusterDefaultsPredicateEvalBlock{
			ids:     make([]types.Id, len(pe.Ids)),
			idRules: make(map[types.Id]string, len(pe.Ids)),
		}
		block.idSet = sets.New[types.Id]()
		for i, idl := range pe.Ids {
			block.ids[i] = types.Id(idl.Id)
			block.idSet.Insert(types.Id(idl.Id))
			block.idRules[types.Id(idl.Id)] = clusterDefaultsExplicitIdRuleLabel(eff, name, string(idl.Id))
		}
		for lineIdx, line := range pe.CelLines {
			var (
				prog cel.Predicate
				err  error
			)
			celOrigin := clusterDefaultsCelCompileOrigin(eff, name, lineIdx)
			if line.Expr == "" {
				prog, err = env.CompileSelectedPredicateAt(celOrigin, line.Selector)
			} else {
				prog, err = env.CompileSelectedPredicateAt(celOrigin, line.Selector, types.CelPredicate(line.Expr))
			}
			if err != nil {
				return nil, err
			}
			block.programs = append(block.programs, prog)
			block.programRules = append(block.programRules, clusterDefaultsCelRuleLabel(eff, name, line))
		}
		if len(block.ids) > 0 || len(block.programs) > 0 {
			blocks = append(blocks, block)
		}
	}
	for idx, item := range eff.ManualOverrides {
		if item.Exclude {
			continue
		}
		block, err := evalBlockForClusterRootOverride(eff, idx, item, env)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func buildExcludeEvalBlocksForPreset(
	eff ClusterDefaultsPresetEffective,
	env cel.Env,
) ([]clusterDefaultsPredicateEvalBlock, error) {
	var blocks []clusterDefaultsPredicateEvalBlock
	for idx, item := range eff.ManualOverrides {
		if !item.Exclude {
			continue
		}
		block, err := evalBlockForClusterRootOverride(eff, idx, item, env)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func evalBlockForClusterRootOverride(
	eff ClusterDefaultsPresetEffective,
	idx int,
	item types.ClusterRootAppOverrideItem,
	env cel.Env,
) (clusterDefaultsPredicateEvalBlock, error) {
	block := clusterDefaultsPredicateEvalBlock{
		idRules: make(map[types.Id]string, 1),
		idSet:   sets.New[types.Id](),
	}
	if item.Cel == "" {
		if !item.Selector.IsZero() && item.Selector.Name != "" {
			if id, ok := selectorExactID(item.Selector); ok {
				block.ids = append(block.ids, id)
				block.idSet.Insert(id)
				block.idRules[id] = clusterRootHydraOverrideRuleLabel(eff, idx, item, id, "")
				return block, nil
			}
		}
	}
	prog, err := compileClusterRootOverridePredicate(env, eff, idx, item)
	if err != nil {
		return clusterDefaultsPredicateEvalBlock{}, err
	}
	block.programs = append(block.programs, prog)
	block.programRules = append(block.programRules, clusterRootHydraOverrideRuleLabel(eff, idx, item, "", string(item.Cel)))
	return block, nil
}

func compileClusterRootOverridePredicate(
	env cel.Env,
	eff ClusterDefaultsPresetEffective,
	idx int,
	item types.ClusterRootAppOverrideItem,
) (cel.Predicate, error) {
	origin := clusterRootHydraOverrideCompileOrigin(eff, idx, item)
	if item.Cel == "" {
		return env.CompileSelectedPredicateAt(origin, item.Selector)
	}
	return env.CompileSelectedPredicateAt(origin, item.Selector, item.Cel)
}

func matchingIDsForBlocks(
	blocks []clusterDefaultsPredicateEvalBlock,
	filteredLive entity.Entities,
	entityByID map[types.Id]entity.Entity,
) (sets.Set[types.Id], error) {
	out := sets.New[types.Id]()
	if len(blocks) == 0 {
		return out, nil
	}
	for _, block := range blocks {
		for _, id := range block.ids {
			if _, ok := entityByID[id]; ok {
				out.Insert(id)
			}
		}
		for _, prog := range block.programs {
			_, matched, err := prog.Select(filteredLive)
			if err != nil {
				return nil, err
			}
			for _, e := range matched.Items {
				id, err := e.Id()
				if err != nil {
					return nil, err
				}
				out.Insert(id)
			}
		}
	}
	return out, nil
}

func clusterRootHydraOverrideCompileOrigin(
	eff ClusterDefaultsPresetEffective,
	idx int,
	item types.ClusterRootAppOverrideItem,
) string {
	mode := "include"
	if item.Exclude {
		mode = "exclude"
	}
	return fmt.Sprintf(
		`cluster root hydra.yaml · hydra.overrides.%s[%d] · %s`,
		eff.ID, idx, mode,
	)
}

func clusterRootHydraOverrideRuleLabel(
	eff ClusterDefaultsPresetEffective,
	idx int,
	item types.ClusterRootAppOverrideItem,
	id types.Id,
	celExpr string,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "preset: %s\nclusterRootHydraOverrideIndex: %d\nexclude: %t", eff.ID, idx, item.Exclude)
	if id != "" {
		fmt.Fprintf(&b, "\nid: %s", id)
	}
	if selector := clusterDefaultsSelectorRuleLabel(item.Selector); selector != "" {
		b.WriteString("\nselector:\n")
		b.WriteString(selector)
	}
	if expr := strings.TrimSpace(celExpr); expr != "" {
		b.WriteString("\ncel: |\n")
		for _, rawLine := range strings.Split(expr, "\n") {
			b.WriteString("  ")
			b.WriteString(strings.TrimRight(rawLine, " \t"))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func selectorExactID(selector types.RefSelector) (types.Id, bool) {
	if selector.Version == "" || selector.Kind == "" || selector.Name == "" {
		return "", false
	}
	return types.NewId(selector.Group, selector.Version, selector.Kind, selector.Namespace, selector.Name), true
}

func evalBlocksMatch(blocks []clusterDefaultsPredicateEvalBlock, e entity.Entity) (bool, error) {
	eid, err := e.Id()
	if err != nil {
		return false, err
	}
	for _, block := range blocks {
		if block.idSet != nil && block.idSet.Has(eid) {
			return true, nil
		}
		for _, prog := range block.programs {
			matched, err := prog.EvalBool(e, types.MissingKeysAccept)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
	}
	return false, nil
}

// EntityMatchesClusterDefaultsPresetCEL evaluates a single CEL line against an entity.
func EntityMatchesClusterDefaultsPresetCEL(env cel.Env, e entity.Entity, celLine string) (bool, error) {
	prog, err := env.CompilePredicateAt(`EntityMatchesClusterDefaultsPresetCEL`, types.CelPredicate(celLine))
	if err != nil {
		return false, err
	}
	return prog.EvalBool(e, types.MissingKeysAccept)
}

// ClusterDefaultsPredicateMinorApplies reports whether k8sMinor satisfies the predicate group's optional gates.
func ClusterDefaultsPredicateMinorApplies(k8sMinor int, pe ClusterDefaultsPredicateEffective) bool {
	m := k8sMinor
	if m <= 0 {
		m = 99
	}
	if pe.KubernetesMinorMin > 0 && m < pe.KubernetesMinorMin {
		return false
	}
	if pe.KubernetesMinorMax > 0 && m > pe.KubernetesMinorMax {
		return false
	}
	return true
}

// ClusterDefaultsPresetAuditExpectedIDs returns the union of ids from enabled predicates (after minor gating)
// across enabled presets — used for blanket bootstrap audit expected resources.
func ClusterDefaultsPresetAuditExpectedIDs(k8sMinor int, effective []ClusterDefaultsPresetEffective) sets.Set[types.Id] {
	out := sets.New[types.Id]()
	for _, eff := range effective {
		if !eff.Enabled {
			continue
		}
		for _, pe := range eff.Predicates {
			if !pe.Enabled || !ClusterDefaultsPredicateMinorApplies(k8sMinor, pe) {
				continue
			}
			for _, idl := range pe.Ids {
				if idl.Optional {
					continue
				}
				out.Insert(types.Id(idl.Id))
			}
		}
	}
	return out
}

// OmittedExplicitIdsForKubernetesMinor returns explicit ids from predicates that do not apply at
// k8sMinor (see ClusterDefaultsPredicateMinorApplies). Synthesized rbac-cluster-* CEL lines use
// every bootstrap-audit id at load time; at report time we skip evaluating id == "…" lines for
// these ids so older API servers do not show spurious "not found" for newer-only bootstrap RBAC.
func OmittedExplicitIdsForKubernetesMinor(eff ClusterDefaultsPresetEffective, k8sMinor int) sets.Set[types.Id] {
	out := sets.New[types.Id]()
	for _, pe := range eff.Predicates {
		if !ClusterDefaultsPredicateMinorApplies(k8sMinor, pe) {
			for _, idl := range pe.Ids {
				out.Insert(types.Id(idl.Id))
			}
		}
	}
	return out
}

// PresetMatchesEntity returns whether the entity matches any active named predicate in the effective preset.
// k8sMinor gates predicate groups with kubernetesMinorMin/Max; use 99 when the minor is unknown so
// version-gated groups behave like local/bootstrap catalog review.
func PresetMatchesEntity(eff ClusterDefaultsPresetEffective, k8sMinor int, env cel.Env, e entity.Entity) (bool, error) {
	if !eff.Enabled {
		return false, nil
	}
	blocks, err := buildEvalBlocksForPreset(eff, k8sMinor, env)
	if err != nil {
		return false, err
	}
	return evalBlocksMatch(blocks, e)
}

// MatchingClusterDefaultsPresetIDs returns preset ids that match the entity.
func MatchingClusterDefaultsPresetIDs(effective []ClusterDefaultsPresetEffective, k8sMinor int, env cel.Env, e entity.Entity) ([]string, error) {
	return MatchingClusterDefaultsPresetIDsWithRegarding(effective, k8sMinor, env, e, workloadclosure.EmptyMatchInput(types.KeyClusterEntity), nil)
}

// MatchingClusterDefaultsPresetIDsWithRegarding is like [MatchingClusterDefaultsPresetIDs] with optional workload-closure input (deployment anchors).
// timing is optional; when non-nil, per-preset wall time for this entity is recorded (see [ClusterDefaultsPresetMatchTiming]).
func MatchingClusterDefaultsPresetIDsWithRegarding(
	effective []ClusterDefaultsPresetEffective,
	k8sMinor int,
	env cel.Env,
	e entity.Entity,
	closure workloadclosure.MatchInput,
	timing *ClusterDefaultsPresetMatchTiming,
) ([]string, error) {
	cache, err := NewClusterDefaultsPresetEvalCache(effective, k8sMinor, env)
	if err != nil {
		return nil, err
	}
	return cache.MatchingPresetIDsWithRegarding(e, closure, timing)
}

// MatchingEntityIdsForCEL returns sorted Hydra resource ids in clusterEntities for which celLine evaluates to true.
func MatchingEntityIdsForCEL(env cel.Env, clusterEntities entity.Entities, celLine string) ([]types.Id, error) {
	prog, err := env.CompilePredicateAt(`MatchingEntityIdsForCEL`, types.CelPredicate(celLine))
	if err != nil {
		return nil, err
	}
	_, matched, err := prog.Select(clusterEntities)
	if err != nil {
		return nil, err
	}
	ids := make([]types.Id, 0, matched.Len())
	for _, e := range matched.Items {
		id, err := e.Id()
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b types.Id) int { return cmp.Compare(string(a), string(b)) })
	return ids, nil
}

func selectorKey(s types.RefSelector) string {
	return string(s.Group) + "|" + string(s.Version) + "|" + string(s.Kind) + "|" + string(s.Namespace) + "|" + string(s.Name)
}

func enabledPositiveActivateClosure(
	rootID string,
	effective []ClusterDefaultsPresetEffective,
	k8sMinor int,
) sets.Set[string] {
	byID := make(map[string]ClusterDefaultsPresetEffective, len(effective))
	for _, eff := range effective {
		if eff.Enabled {
			byID[eff.ID] = eff
		}
	}
	out := sets.New[string]()
	seen := sets.New[string]()
	var visit func(string)
	visit = func(id string) {
		if seen.Has(id) {
			return
		}
		seen.Insert(id)
		eff, ok := byID[id]
		if !ok {
			return
		}
		for _, target := range clusterDefaultsPositiveActivateTargetsForMinor(eff.Activates, k8sMinor) {
			if target == "" || out.Has(target) {
				continue
			}
			out.Insert(target)
			visit(target)
		}
	}
	visit(rootID)
	out.Delete(rootID)
	return out
}

func (c *ClusterDefaultsPresetEvalCache) filterActivatingPresetMatches(
	ids []string,
	matched sets.Set[string],
	direct sets.Set[string],
) []string {
	if len(ids) < 2 {
		return ids
	}
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		drop := false
		for _, preset := range c.presets {
			if preset.id == id && preset.activates != nil && preset.activates.Len() > 0 {
				for activated := range preset.activates {
					if !matched.Has(activated) {
						continue
					}
					if direct != nil && !direct.Has(activated) {
						continue
					}
					drop = true
					break
				}
				break
			}
			if preset.activates == nil || preset.activates.Len() == 0 || !preset.activates.Has(id) {
				continue
			}
			if !matched.Has(preset.id) {
				continue
			}
			if direct != nil && direct.Has(preset.id) && !direct.Has(id) {
				drop = true
				break
			}
		}
		if !drop {
			filtered = append(filtered, id)
		}
	}
	return filtered
}

func recordAmbiguityPath[K ~string](dest map[K][]clusterDefaultsPathHop, key K, path []clusterDefaultsPathHop) {
	if existing, ok := dest[key]; !ok || comparePathHops(path, existing) < 0 {
		dest[key] = append([]clusterDefaultsPathHop{}, path...)
	}
}

func formatAmbiguityCandidatesFromPaths[K ~string](byValue map[K][]clusterDefaultsPathHop) []clusterDefaultsAmbiguityCandidate {
	values := make([]K, 0, len(byValue))
	for value := range byValue {
		values = append(values, value)
	}
	slices.SortFunc(values, func(a, b K) int { return cmp.Compare(string(a), string(b)) })
	out := make([]clusterDefaultsAmbiguityCandidate, 0, len(values))
	for _, value := range values {
		out = append(out, clusterDefaultsAmbiguityCandidate{
			Name: string(value),
			Path: append([]clusterDefaultsPathHop{}, byValue[value]...),
		})
	}
	return out
}

func formatAmbiguityCandidates(candidates []clusterDefaultsAmbiguityCandidate) string {
	var b strings.Builder
	for _, candidate := range candidates {
		b.WriteString(candidate.Name)
		b.WriteString("|")
		for _, hop := range candidate.Path {
			b.WriteString(string(hop.ID))
			b.WriteString("@")
			b.WriteString(string(hop.Via))
			b.WriteString("|")
		}
	}
	return b.String()
}

func comparePathHops(a, b []clusterDefaultsPathHop) int {
	if len(a) != len(b) {
		return cmp.Compare(len(a), len(b))
	}
	for i := range a {
		if c := cmp.Compare(string(a[i].ID), string(b[i].ID)); c != 0 {
			return c
		}
		if c := cmp.Compare(a[i].Via, b[i].Via); c != 0 {
			return c
		}
	}
	return 0
}

func refLabelsContain(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
