package view

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/references"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

// WorkloadGvkSet returns a set of GVKStrings for workload resources.
// Workloads are always group seeds: they create their own group and absorb
// connected non-seed entities. They are never absorbed into other groups.
func WorkloadGvkSet() sets.Set[types.GVKString] {
	return sets.New[types.GVKString](
		"apps/v1/Deployment",
		"apps/v1/StatefulSet",
		"apps/v1/DaemonSet",
		"apps/v1/ReplicaSet",
		"v1/Pod",
		"batch/v1/Job",
		"batch/v1/CronJob",
	)
}

// StandaloneSeedGvkSet returns a set of GVKStrings for entities that become
// group seeds when they have no non-reverse incoming edges. A ServiceAccount
// that is only referenced by RoleBindings/ClusterRoleBindings (reverse edges)
// and Secrets (imagePullSecret) — but NOT by any workload — acts as a group
// seed and absorbs its RBAC chain, just like a workload would.
func StandaloneSeedGvkSet() sets.Set[types.GVKString] {
	return sets.New[types.GVKString](
		"v1/ServiceAccount",
	)
}

// GroupId represents a unique identifier for a group of merged entities
type GroupId string

// DependenciesModel represents the data used for dependencies rendering
type DependenciesModel struct {
	Entities       []IdModel        `yaml:"entities"`
	Groups         []GroupModel     `yaml:"groups"`
	References     []RefModel       `yaml:"references"`
	Charts         []ChartModel     `yaml:"charts,omitempty"`
	ValueFiles     []ValueFileModel `yaml:"valueFiles,omitempty"`
	AppValues      []AppValuesModel `yaml:"appValues,omitempty"`
	FallbackValues []AppValuesModel `yaml:"fallbackValues,omitempty"`
	GitRemote      string           `yaml:"gitRemote,omitempty"`
	GitRepoPrefix  string           `yaml:"gitRepoPrefix,omitempty"`
	GitBranch      string           `yaml:"gitBranch,omitempty"`
}

type GroupModel struct {
	Name string     `yaml:"name,omitempty"`
	Ids  []types.Id `yaml:"ids"`
}

// RbacRuleModel represents a single RBAC policy rule from a Role or ClusterRole.
type RbacRuleModel struct {
	ApiGroups     []string `yaml:"apiGroups"`
	Resources     []string `yaml:"resources"`
	Verbs         []string `yaml:"verbs"`
	ResourceNames []string `yaml:"resourceNames,omitempty"`
}

type IdModel struct {
	Id            types.Id        `yaml:"id"`
	AppIds        []types.AppId   `yaml:"appIds,omitempty"`
	Tags          []string        `yaml:"tags,omitempty"`
	TemplatePath  string          `yaml:"templatePath,omitempty"`
	TemplateIndex int             `yaml:"templateIndex,omitempty"`
	ManifestPath  string          `yaml:"manifestPath,omitempty"`
	RbacRules     []RbacRuleModel `yaml:"rbacRules,omitempty"`
	SecretKeys    []string        `yaml:"secretKeys,omitempty"`
}

func compareIdModel(a, b IdModel) int {
	if a.Id < b.Id {
		return -1
	}
	if a.Id > b.Id {
		return 1
	}
	return 0
}

func NewIdModel(id types.Id, tags ...string) (IdModel, error) {
	return IdModel{
		Id:   id,
		Tags: tags,
	}, nil
}

type RefModel struct {
	From       types.Id             `yaml:"from"`
	To         types.Id             `yaml:"to"`
	Labels     []string             `yaml:"labels,omitempty"`
	Tags       []string             `yaml:"tags,omitempty"`
	Attributes []types.RefAttribute `yaml:"attributes,omitempty"`
	Desc       string               `yaml:"desc,omitempty"`
	Reverse    bool                 `yaml:"reverse,omitempty"`
}

type ChartDependencyModel struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Repository string `yaml:"repository,omitempty"`
}

type ChartModel struct {
	AppId        types.AppId            `yaml:"appId,omitempty"`
	Name         string                 `yaml:"name"`
	Version      string                 `yaml:"version"`
	AppVersion   string                 `yaml:"appVersion,omitempty"`
	Dependencies []ChartDependencyModel `yaml:"dependencies,omitempty"`
	ChartPath    string                 `yaml:"chartPath,omitempty"`
}

// ValueFileModel describes a values file from the GitOps repository that is
// not bundled inside a chart .tgz archive.
type ValueFileModel struct {
	Path  string      `yaml:"path"`            // relative path from the context parent directory
	Type  string      `yaml:"type"`            // "group", "context", "cluster", or "app"
	AppId types.AppId `yaml:"appId,omitempty"` // set only for type "app"
}

// AppValuesModel references the merged values for a single app.
type AppValuesModel struct {
	AppId types.AppId `yaml:"appId"`
}

func NewRefModel(ref types.Ref) RefModel {
	return RefModel{
		From:       ref.From,
		To:         ref.To,
		Labels:     ref.Labels,
		Tags:       ref.Tags,
		Attributes: ref.Attributes,
		Desc:       ref.Desc,
		Reverse:    ref.Reverse,
	}
}

// unionFind implements a simple union-find data structure for grouping entities
type unionFind struct {
	parent map[types.Id]types.Id
	size   map[types.Id]int
}

func newUnionFind(ids []types.Id) *unionFind {
	uf := &unionFind{
		parent: make(map[types.Id]types.Id),
		size:   make(map[types.Id]int),
	}
	for _, id := range ids {
		uf.parent[id] = id
		uf.size[id] = 1
	}
	return uf
}

func (uf *unionFind) find(id types.Id) types.Id {
	if uf.parent[id] != id {
		uf.parent[id] = uf.find(uf.parent[id]) // path compression
	}
	return uf.parent[id]
}

func (uf *unionFind) union(a, b types.Id) {
	rootA := uf.find(a)
	rootB := uf.find(b)
	if rootA != rootB {
		// Union by size (attach smaller tree to larger)
		if uf.size[rootA] < uf.size[rootB] {
			uf.parent[rootA] = rootB
			uf.size[rootB] += uf.size[rootA]
		} else {
			uf.parent[rootB] = rootA
			uf.size[rootA] += uf.size[rootB]
		}
	}
}

func (uf *unionFind) getSize(id types.Id) int {
	return uf.size[uf.find(id)]
}

// computeGroups calculates entity groups using seed-based and union-find grouping:
//
// Phase 1a: Start from group seeds (workloads + standalone SAs) and absorb connected
// non-seed entities that have no external edges facing the group.
// Phase 1b: Merge singleton seeds with identical edge fingerprints, then re-absorb.
// Phase 2: Union-find based merging of degree=1 groups and shared leaves.
// Phase 3: Group remaining ungrouped shared-leaf entities.
//
// Group seeds are: workloads (Deployment, Job, etc.) which are always seeds, and
// ServiceAccounts with no non-reverse incoming edges (standalone SAs not used by
// any workload). Standalone SAs absorb their RBAC chain just like workloads do.
//
// Returns: map of entity ID to group ID, and map of group ID to group name.
func computeGroups(ids []types.Id, refs []types.Ref) (map[types.Id]GroupId, map[GroupId]string) {
	idSet := sets.New(ids...)

	// Get GVKString from Id
	idToGVK := func(id types.Id) types.GVKString {
		group, version, kind, _, _, err := id.Components()
		if err != nil {
			return ""
		}
		if group == "" {
			return types.GVKString(fmt.Sprintf("%s/%s", version, kind))
		}
		return types.GVKString(fmt.Sprintf("%s/%s/%s", group, version, kind))
	}

	// Check if an ID is a workload (always a group seed)
	workloadGVKs := WorkloadGvkSet()
	isWorkload := func(id types.Id) bool {
		return workloadGVKs.Has(idToGVK(id))
	}

	// Check if an ID is a candidate for standalone seed (SA etc.)
	standaloneSeedGVKs := StandaloneSeedGvkSet()
	isStandaloneSeedCandidate := func(id types.Id) bool {
		return standaloneSeedGVKs.Has(idToGVK(id))
	}

	// ============================================================
	// Phase 1: Seed-based grouping (following edge directions)
	// ============================================================
	// Seeds are entities that create their own groups and absorb neighbors.
	// Two types of seeds:
	//   1. Workloads (Deployment, Job, etc.) — always seeds
	//   2. Standalone SAs — seeds only when they have no non-reverse incoming
	//      edges (i.e. no workload references them via serviceAccountName)

	// Build adjacency lists considering reverse flag
	// "Logical outgoing" from A: normal edges where A is From, plus reversed edges where A is To
	// "Logical incoming" to A: normal edges where A is To, plus reversed edges where A is From
	type edgeInfo struct {
		neighbor types.Id
		reverse  bool
	}
	logicalOutgoing := make(map[types.Id][]edgeInfo)
	logicalIncoming := make(map[types.Id][]edgeInfo)

	// Also track non-reverse incoming edges separately (for standalone seed detection)
	nonReverseIncoming := make(map[types.Id]int)

	for _, ref := range refs {
		if !idSet.Has(ref.From) || !idSet.Has(ref.To) {
			continue
		}
		if ref.Reverse {
			logicalIncoming[ref.From] = append(logicalIncoming[ref.From], edgeInfo{neighbor: ref.To, reverse: true})
			logicalOutgoing[ref.To] = append(logicalOutgoing[ref.To], edgeInfo{neighbor: ref.From, reverse: true})
		} else {
			logicalOutgoing[ref.From] = append(logicalOutgoing[ref.From], edgeInfo{neighbor: ref.To, reverse: false})
			logicalIncoming[ref.To] = append(logicalIncoming[ref.To], edgeInfo{neighbor: ref.From, reverse: false})
			nonReverseIncoming[ref.To]++
		}
	}

	// Determine standalone seeds: SA-type entities with no non-reverse incoming edges.
	// These SAs are not used by any workload (no serviceAccountName reference) and only
	// have reverse edges from RoleBindings/ClusterRoleBindings (subjects).
	isStandaloneSeed := func(id types.Id) bool {
		return isStandaloneSeedCandidate(id) && nonReverseIncoming[id] == 0
	}

	// Combined check: is this entity a group seed (workload OR standalone SA)?
	isGroupSeed := func(id types.Id) bool {
		return isWorkload(id) || isStandaloneSeed(id)
	}

	// Initialize: each entity is its own group
	entityToGroup := make(map[types.Id]int)
	groupMembers := make(map[int]sets.Set[types.Id])
	nextGroupId := 0

	for _, id := range ids {
		entityToGroup[id] = nextGroupId
		groupMembers[nextGroupId] = sets.New(id)
		nextGroupId++
	}

	// Helper to merge entity into a group
	mergeIntoGroup := func(entityId types.Id, targetGroupId int) {
		oldGroupId := entityToGroup[entityId]
		if oldGroupId == targetGroupId {
			return
		}
		groupMembers[oldGroupId].Delete(entityId)
		if groupMembers[oldGroupId].Len() == 0 {
			delete(groupMembers, oldGroupId)
		}
		groupMembers[targetGroupId].Insert(entityId)
		entityToGroup[entityId] = targetGroupId
	}

	// Helper to count logical incoming edges from outside a group
	countExternalLogicalIncoming := func(entityId types.Id, groupId int) int {
		count := 0
		for _, edge := range logicalIncoming[entityId] {
			if entityToGroup[edge.neighbor] != groupId {
				count++
			}
		}
		return count
	}

	// Helper to count logical outgoing edges to outside a group
	countExternalLogicalOutgoing := func(entityId types.Id, groupId int) int {
		count := 0
		for _, edge := range logicalOutgoing[entityId] {
			if entityToGroup[edge.neighbor] != groupId {
				count++
			}
		}
		return count
	}

	// Collect all group seeds: workloads + standalone SAs
	seeds := []types.Id{}
	for _, id := range ids {
		if isGroupSeed(id) {
			seeds = append(seeds, id)
		}
	}

	// Helper: iteratively absorb non-seed neighbors into a group when they
	// have no external edges on the side that faces the group.
	absorbNeighbors := func(seedGroupId int) {
		for {
			absorbed := false
			currentMembers := groupMembers[seedGroupId].UnsortedList()

			candidates := sets.New[types.Id]()
			for _, member := range currentMembers {
				for _, edge := range logicalOutgoing[member] {
					if entityToGroup[edge.neighbor] != seedGroupId {
						candidates.Insert(edge.neighbor)
					}
				}
				for _, edge := range logicalIncoming[member] {
					if entityToGroup[edge.neighbor] != seedGroupId {
						candidates.Insert(edge.neighbor)
					}
				}
			}

			for candidate := range candidates {
				if isGroupSeed(candidate) {
					continue
				}

				externalIncoming := countExternalLogicalIncoming(candidate, seedGroupId)
				externalOutgoing := countExternalLogicalOutgoing(candidate, seedGroupId)
				incomingFromGroup := len(logicalIncoming[candidate]) - externalIncoming
				outgoingToGroup := len(logicalOutgoing[candidate]) - externalOutgoing

				shouldMerge := false
				if externalIncoming == 0 && incomingFromGroup > 0 {
					shouldMerge = true
				}
				if externalOutgoing == 0 && outgoingToGroup > 0 {
					shouldMerge = true
				}

				if shouldMerge {
					mergeIntoGroup(candidate, seedGroupId)
					absorbed = true
					break
				}
			}

			if !absorbed {
				break
			}
		}
	}

	// Phase 1a: Each seed absorbs its exclusive neighbors
	for _, seed := range seeds {
		absorbNeighbors(entityToGroup[seed])
	}

	// ============================================================
	// Phase 1b: Merge singleton seeds with identical edges
	// ============================================================
	// Two seeds that have the exact same set of logical neighbors
	// (outgoing + incoming) form a natural group. For example, two Jobs
	// referencing the same ServiceAccount. Merging them first lets the
	// combined group absorb shared dependencies that were previously
	// blocked by cross-references between the separate seed groups.
	{
		type seedFP struct {
			id          types.Id
			groupId     int
			fingerprint string
		}

		var fps []seedFP
		for _, s := range seeds {
			sGroupId := entityToGroup[s]
			// Only consider singleton seed groups (the seed is the only
			// member, meaning Phase 1a couldn't absorb anything)
			if groupMembers[sGroupId].Len() != 1 {
				continue
			}
			neighbors := sets.New[types.Id]()
			for _, edge := range logicalOutgoing[s] {
				neighbors.Insert(edge.neighbor)
			}
			for _, edge := range logicalIncoming[s] {
				neighbors.Insert(edge.neighbor)
			}
			if neighbors.Len() == 0 {
				continue
			}
			sorted := neighbors.UnsortedList()
			slices.Sort(sorted)
			fingerprint := fmt.Sprintf("%v", sorted)
			fps = append(fps, seedFP{id: s, groupId: sGroupId, fingerprint: fingerprint})
		}

		// Group by fingerprint
		fpGroups := make(map[string][]seedFP)
		for _, fp := range fps {
			fpGroups[fp.fingerprint] = append(fpGroups[fp.fingerprint], fp)
		}

		// Merge seeds with the same fingerprint, then re-run absorption
		for _, group := range fpGroups {
			if len(group) <= 1 {
				continue
			}
			targetGroupId := group[0].groupId
			for _, fp := range group[1:] {
				mergeIntoGroup(fp.id, targetGroupId)
			}
			// Re-run absorption for the merged group — the combined group
			// may now absorb entities that were previously blocked.
			absorbNeighbors(targetGroupId)
		}
	}

	// ============================================================
	// Phase 2: Union-find based grouping (degree=1 merging)
	// ============================================================

	// Convert current groups to union-find structure
	uf := newUnionFind(ids)

	// Union all entities that are already in the same group
	for _, members := range groupMembers {
		memberList := members.UnsortedList()
		if len(memberList) > 1 {
			first := memberList[0]
			for _, m := range memberList[1:] {
				uf.union(first, m)
			}
		}
	}

	// Track which groups contain seeds (workloads or standalone SAs)
	groupHasSeed := make(map[types.Id]bool)
	for _, id := range ids {
		if isGroupSeed(id) {
			groupHasSeed[id] = true
		}
	}

	// Iteratively merge groups with degree=1
	for {
		groupDegree := make(map[types.Id]int)
		neighbor := make(map[types.Id]types.Id)

		currentGroupHasSeed := make(map[types.Id]bool)
		for _, id := range ids {
			root := uf.find(id)
			if groupHasSeed[id] {
				currentGroupHasSeed[root] = true
			}
			groupDegree[root] = 0
		}

		for _, ref := range refs {
			fromRoot := uf.find(ref.From)
			toRoot := uf.find(ref.To)
			if fromRoot != toRoot {
				groupDegree[fromRoot]++
				groupDegree[toRoot]++
				neighbor[fromRoot] = toRoot
				neighbor[toRoot] = fromRoot
			}
		}

		merged := false

		for _, id := range ids {
			root := uf.find(id)
			if groupDegree[root] == 1 && !currentGroupHasSeed[root] {
				partner := neighbor[root]
				if partner != "" {
					uf.union(root, partner)
					merged = true
					break
				}
			}
		}

		if !merged {
			break
		}
	}

	// Merge all groups with inDegree > 1 and outDegree = 0 (shared leaves/hubs)
	{
		groupInDegree := make(map[types.Id]int)
		groupOutDegree := make(map[types.Id]int)
		for _, id := range ids {
			root := uf.find(id)
			groupInDegree[root] = 0
			groupOutDegree[root] = 0
		}
		for _, ref := range refs {
			fromRoot := uf.find(ref.From)
			toRoot := uf.find(ref.To)
			if fromRoot != toRoot {
				groupOutDegree[fromRoot]++
				groupInDegree[toRoot]++
			}
		}

		var sharedLeaves []types.Id
		seen := make(map[types.Id]bool)
		for _, id := range ids {
			root := uf.find(id)
			if seen[root] {
				continue
			}
			seen[root] = true
			if groupInDegree[root] > 1 && groupOutDegree[root] == 0 {
				sharedLeaves = append(sharedLeaves, root)
			}
		}

		if len(sharedLeaves) > 1 {
			first := sharedLeaves[0]
			for _, root := range sharedLeaves[1:] {
				uf.union(first, root)
			}
		}
	}

	// ============================================================
	// Phase 3: Group ungrouped entities with inDegree > 1 and outDegree = 0
	// ============================================================
	{
		// Calculate in/out degree for individual entities
		entityInDegree := make(map[types.Id]int)
		entityOutDegree := make(map[types.Id]int)
		for _, id := range ids {
			entityInDegree[id] = 0
			entityOutDegree[id] = 0
		}
		for _, ref := range refs {
			if ref.Reverse {
				entityInDegree[ref.To]++
				entityOutDegree[ref.From]++
			} else {
				entityOutDegree[ref.From]++
				entityInDegree[ref.To]++
			}
		}

		// Find ungrouped entities (group size = 1) with inDegree > 1 and outDegree = 0
		var ungroupedSharedLeaves []types.Id
		for _, id := range ids {
			root := uf.find(id)
			if uf.getSize(root) == 1 && entityInDegree[id] > 1 && entityOutDegree[id] == 0 {
				ungroupedSharedLeaves = append(ungroupedSharedLeaves, id)
			}
		}

		// Merge all ungrouped shared leaves into one group
		if len(ungroupedSharedLeaves) > 1 {
			first := ungroupedSharedLeaves[0]
			for _, id := range ungroupedSharedLeaves[1:] {
				uf.union(first, id)
			}
		}
	}

	// ============================================================
	// Build final result
	// ============================================================

	finalGroupMembers := make(map[types.Id][]types.Id)
	for _, id := range ids {
		root := uf.find(id)
		finalGroupMembers[root] = append(finalGroupMembers[root], id)
	}

	finalOutDegree := make(map[types.Id]int)
	for _, id := range ids {
		root := uf.find(id)
		finalOutDegree[root] = 0
	}
	for _, ref := range refs {
		fromRoot := uf.find(ref.From)
		toRoot := uf.find(ref.To)
		if fromRoot != toRoot {
			finalOutDegree[fromRoot]++
		}
	}

	var roots []types.Id
	seenRoots := make(map[types.Id]bool)
	for _, id := range ids {
		root := uf.find(id)
		if !seenRoots[root] {
			seenRoots[root] = true
			roots = append(roots, root)
		}
	}

	slices.SortFunc(roots, func(a, b types.Id) int {
		aHasOut := finalOutDegree[a] > 0
		bHasOut := finalOutDegree[b] > 0
		if aHasOut && !bHasOut {
			return -1
		}
		if !aHasOut && bHasOut {
			return 1
		}
		return len(finalGroupMembers[b]) - len(finalGroupMembers[a])
	})

	result := make(map[types.Id]GroupId)
	groupIdMap := make(map[types.Id]GroupId)
	groupNames := make(map[GroupId]string)

	// Calculate outDegree for each entity (for finding leaf nodes)
	entityOutDegree := make(map[types.Id]int)
	for _, id := range ids {
		entityOutDegree[id] = 0
	}
	for _, ref := range refs {
		if ref.Reverse {
			entityOutDegree[ref.To]++
		} else {
			entityOutDegree[ref.From]++
		}
	}

	for i, root := range roots {
		gid := GroupId(fmt.Sprintf("g%d", i))
		groupIdMap[root] = gid

		// Find seed (workload or standalone SA) in this group to use as name.
		// Prefer workloads over standalone seeds for naming.
		foundName := false
		for _, id := range finalGroupMembers[root] {
			if isWorkload(id) {
				_, _, kind, _, name, err := id.Components()
				if err == nil && name != "" {
					groupNames[gid] = fmt.Sprintf("%s (%s)", name, kind)
					foundName = true
					break
				}
			}
		}
		if !foundName {
			for _, id := range finalGroupMembers[root] {
				if isGroupSeed(id) {
					_, _, kind, _, name, err := id.Components()
					if err == nil && name != "" {
						groupNames[gid] = fmt.Sprintf("%s (%s)", name, kind)
						foundName = true
						break
					}
				}
			}
		}

		// If no workload, check if there's exactly one leaf node (outDegree = 0)
		if !foundName && len(finalGroupMembers[root]) > 1 {
			var leafNodes []types.Id
			for _, id := range finalGroupMembers[root] {
				if entityOutDegree[id] == 0 {
					leafNodes = append(leafNodes, id)
				}
			}
			if len(leafNodes) == 1 {
				_, _, kind, _, name, err := leafNodes[0].Components()
				if err == nil && name != "" {
					groupNames[gid] = fmt.Sprintf("%s (%s)", name, kind)
					foundName = true
				}
			}
		}

		// Groups without workloads or single leaf are named "Shared"
		if !foundName && len(finalGroupMembers[root]) > 1 {
			groupNames[gid] = "Shared"
		}
	}
	for _, id := range ids {
		root := uf.find(id)
		result[id] = groupIdMap[root]
	}

	// Debug: log groups with more than one entity
	// TODO: Add logger parameter to computeGroups to enable logging
	_ = roots // suppress unused warning when logging is disabled

	return result, groupNames
}

func ToModel(l log.Logger, entities entity.Entities, charts ...[]ChartModel) (DependenciesModel, error) {
	return ToModelWithParsers(l, entities, nil, charts...)
}

func ToModelWithParsers(l log.Logger, entities entity.Entities, extraParsers []types.RefParser, charts ...[]ChartModel) (DependenciesModel, error) {
	var model DependenciesModel

	originalIds := sets.New[types.Id]()
	for _, e := range entities.Items {
		id, err := e.Id()
		if err != nil {
			return DependenciesModel{}, err
		}
		originalIds.Insert(id)
	}

	// Expand virtual/provisioned entities before building the dependency model so grouping, tags,
	// and downstream ref consumers all see the same stabilized graph.
	expandedEntities, refs, err := references.ResolveVirtualRefs(l, entities, types.KeyTemplateEntity, nil, entity.Entities{}, nil, extraParsers)
	if err != nil {
		return DependenciesModel{}, err
	}
	refs = references.AnnotateRefsWithSource(refs, types.RefSourceTemplate)

	// First pass: collect all existing IDs
	allIds := sets.New[types.Id]()
	for _, e := range expandedEntities.Items {
		id, err := e.Id()
		if err != nil {
			return DependenciesModel{}, err
		}
		allIds.Insert(id)
	}

	// Find missing entities (outgoing refs to non-existing entities)
	missingIds := sets.New[types.Id]()
	for _, ref := range refs {
		if !allIds.Has(ref.To) {
			missingIds.Insert(ref.To)
		}
	}
	allIds = allIds.Union(missingIds)

	// Compute groups based on merge rules
	allIdsList := allIds.UnsortedList()
	slices.Sort(allIdsList)
	idToGroupId, groupNames := computeGroups(allIdsList, refs)

	// Build entity models and group sets
	groupSets := make(map[GroupId]sets.Set[types.Id])
	for _, id := range allIdsList {
		var wrapper IdModel
		var err error
		if missingIds.Has(id) {
			wrapper, err = NewIdModel(id, "app:missing")
		} else {
			var tags []string
			if !originalIds.Has(id) {
				tags = virtualEntityTags(id, refs)
			}
			wrapper, err = NewIdModel(id, tags...)
		}
		if err != nil {
			return DependenciesModel{}, err
		}

		// Add template source info, RBAC rules, and secret keys if available
		if e, ok := expandedEntities.EntityMap[id]; ok {
			if appIds, appIdsErr := e.AppIds(); appIdsErr == nil && len(appIds) > 0 {
				wrapper.AppIds = appIds
			}
			if tp, tpErr := e.TemplatePath(); tpErr == nil {
				wrapper.TemplatePath = string(tp)
			}
			if ti, tiErr := e.TemplateIndex(); tiErr == nil {
				wrapper.TemplateIndex = int(ti)
			}
			wrapper.ManifestPath = ComputeManifestPath(id, wrapper.AppIds)
			wrapper.RbacRules = extractRbacRules(e)
			wrapper.SecretKeys = extractSecretKeys(e)
		}

		model.Entities = append(model.Entities, wrapper)

		groupId := idToGroupId[id]
		if groupSets[groupId] == nil {
			groupSets[groupId] = sets.New[types.Id]()
		}
		groupSets[groupId].Insert(id)
	}

	// Sort entities by ID for deterministic output
	slices.SortFunc(model.Entities, compareIdModel)

	// Convert group map to ordered slice
	groupIds := slices.Collect(maps.Keys(groupSets))
	slices.SortFunc(groupIds, func(a, b GroupId) int {
		return cmp.Compare(a, b)
	})
	for _, gid := range groupIds {
		group := groupSets[gid]
		if len(group) > 1 {
			ids := group.UnsortedList()
			slices.Sort(ids)
			model.Groups = append(model.Groups, GroupModel{Name: groupNames[gid], Ids: ids})
		}
	}
	slices.SortFunc(model.Groups, func(a, b GroupModel) int {
		return compareIdSlices(a.Ids, b.Ids)
	})

	// Build RefModels
	for _, ref := range refs {
		model.References = append(model.References, NewRefModel(ref))
	}
	slices.SortFunc(model.References, func(a, b RefModel) int {
		if c := cmp.Compare(a.From, b.From); c != 0 {
			return c
		}
		if c := cmp.Compare(a.To, b.To); c != 0 {
			return c
		}
		return cmp.Compare(strings.Join(a.Labels, ","), strings.Join(b.Labels, ","))
	})
	if model.Entities == nil {
		model.Entities = []IdModel{}
	}
	if model.Groups == nil {
		model.Groups = []GroupModel{}
	}
	if model.References == nil {
		model.References = []RefModel{}
	}
	if len(charts) > 0 && len(charts[0]) > 0 {
		model.Charts = charts[0]
	}

	return model, nil
}

// RenderDependencies extracts nodes and references from entities and renders them
func RenderDependencies(l log.Logger, w io.Writer, entities entity.Entities, charts ...[]ChartModel) error {
	model, err := ToModel(l, entities, charts...)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(model)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// secretGVKSet returns the set of GVKStrings for Secret entities whose data keys should be exported.
var secretGVKSet = sets.New[types.GVKString](
	"v1/Secret",
)

// extractSecretKeys extracts the data/stringData key names from a Secret entity.
func extractSecretKeys(e entity.Entity) []string {
	gvkStr, err := e.GVKString()
	if err != nil || !secretGVKSet.Has(gvkStr) {
		return nil
	}
	u, ok := e.Unstructured(types.KeyTemplateEntity)
	if !ok {
		return nil
	}
	keySet := sets.New[string]()
	if data, found, err := unstructured.NestedMap(u.Object, "data"); err == nil && found {
		for k := range data {
			keySet.Insert(k)
		}
	}
	if stringData, found, err := unstructured.NestedMap(u.Object, "stringData"); err == nil && found {
		for k := range stringData {
			keySet.Insert(k)
		}
	}
	if keySet.Len() == 0 {
		return nil
	}
	result := keySet.UnsortedList()
	slices.Sort(result)
	return result
}

func virtualEntityTags(id types.Id, refs []types.Ref) []string {
	appTags := sets.New[string]()
	controllerTags := sets.New[string]()
	for _, ref := range refs {
		if ref.To != id || !ref.RefMaterializesVirtualTarget() {
			continue
		}
		for _, attr := range ref.Attributes {
			switch attr.Type {
			case types.RefAttributeOriginApp:
				if attr.Value != "" {
					appTags.Insert("app:" + attr.Value)
				}
			case types.RefAttributeOriginWorkload:
				if attr.Value != "" {
					controllerTags.Insert("controller:" + attr.Value)
				}
			}
		}
	}
	tags := append(appTags.UnsortedList(), controllerTags.UnsortedList()...)
	slices.Sort(tags)
	return tags
}

// rbacGVKSet returns the set of GVKStrings for RBAC entities that carry policy rules.
var rbacRulesGVKSet = sets.New[types.GVKString](
	"rbac.authorization.k8s.io/v1/Role",
	"rbac.authorization.k8s.io/v1/ClusterRole",
)

// extractRbacRules extracts RBAC policy rules from a Role or ClusterRole entity.
// Returns nil for non-RBAC entities or entities without rules.
func extractRbacRules(e entity.Entity) []RbacRuleModel {
	gvkStr, err := e.GVKString()
	if err != nil || !rbacRulesGVKSet.Has(gvkStr) {
		return nil
	}
	u, ok := e.Unstructured(types.KeyTemplateEntity)
	if !ok {
		return nil
	}
	rulesSlice, found, err := unstructured.NestedSlice(u.Object, "rules")
	if err != nil || !found {
		return nil
	}
	var result []RbacRuleModel
	for _, ruleInterface := range rulesSlice {
		rule, ok := ruleInterface.(map[string]any)
		if !ok {
			continue
		}
		apiGroups := toStringSlice(rule["apiGroups"])
		resources := toStringSlice(rule["resources"])
		verbs := toStringSlice(rule["verbs"])
		resourceNames := toStringSlice(rule["resourceNames"])
		model := RbacRuleModel{
			ApiGroups: apiGroups,
			Resources: resources,
			Verbs:     verbs,
		}
		if len(resourceNames) > 0 {
			model.ResourceNames = resourceNames
		}
		result = append(result, model)
	}
	return result
}

// toStringSlice converts an interface{} (expected []interface{}) to []string.
func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func compareIdSlices(a, b []types.Id) int {
	minLen := min(len(b), len(a))
	for i := 0; i < minLen; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

// ComputeManifestPath builds the relative manifest file path for an entity.
// Format: <first-appId>/<group-or-(core)>/<version>/<kind>/<namespace-or-(cluster)>/<name>.yaml
// Returns "" if the entity has no appIds (e.g. app:missing entities).
func ComputeManifestPath(id types.Id, appIds []types.AppId) string {
	if len(appIds) == 0 {
		return ""
	}

	group, version, kind, namespace, name, err := id.Components()
	if err != nil {
		return ""
	}

	groupDir := string(group)
	if groupDir == "" {
		groupDir = "(core)"
	}

	nsDir := string(namespace)
	if nsDir == "" {
		nsDir = "(cluster)"
	}

	return path.Join(string(appIds[0]), groupDir, string(version), string(kind), nsDir, string(name)+".yaml")
}
