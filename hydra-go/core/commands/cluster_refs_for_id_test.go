package commands

import (
	"cmp"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestEnsureResourceIdKnownWithClusterEntities_acceptsIdOnlyOnLiveCluster(t *testing.T) {
	pod, err := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithNamespace(types.Namespace("monitoring")).
		WithName(types.Name("prometheus-kube-prometheus-stack-prometheus-0")).
		Build()
	require.NoError(t, err)

	clusterEnts, err := entity.NewEntities([]entity.Entity{pod})
	require.NoError(t, err)
	emptyTemplates, err := entity.NewEntities(nil)
	require.NoError(t, err)

	id := types.Id("v1/Pod/monitoring/prometheus-kube-prometheus-stack-prometheus-0")
	err = ensureResourceIdKnownWithClusterEntities(emptyTemplates, clusterEnts, nil, id)
	require.NoError(t, err)
}

func TestEnsureResourceIdKnown_rejectsWhenNotInTemplatesOrRefs(t *testing.T) {
	empty, err := entity.NewEntities(nil)
	require.NoError(t, err)
	err = ensureResourceIdKnown(empty, nil, types.Id("v1/Pod/monitoring/some-pod"))
	require.Error(t, err)
}

// --- Transitive ref distance (hydra local|cluster refs, tree TUI) ---
// Expected behavior matches hydra-ui buildReachabilityMap: separate outgoing/incoming BFS,
// max 10 levels each, logical edge direction like cli/tui EdgesForID (Reverse refs flipped).

func logicalEndpointsForTransitiveTests(refs []types.Ref, id types.Id) (incoming, outgoing []types.Id) {
	for _, r := range refs {
		if r.Reverse {
			if r.From == id {
				incoming = append(incoming, r.To)
			} else if r.To == id {
				outgoing = append(outgoing, r.From)
			}
			continue
		}
		if r.To == id {
			incoming = append(incoming, r.From)
		}
		if r.From == id {
			outgoing = append(outgoing, r.To)
		}
	}
	return incoming, outgoing
}

func sortTransitiveEntries(es []TransitiveRefEntry) []TransitiveRefEntry {
	if es == nil {
		return nil
	}
	out := slices.Clone(es)
	slices.SortFunc(out, func(a, b TransitiveRefEntry) int {
		if c := cmp.Compare(a.Distance, b.Distance); c != 0 {
			return c
		}
		return cmp.Compare(string(a.ID), string(b.ID))
	})
	return out
}

func dedupeUnvisitedNeighbors(cands []types.Id, visited map[types.Id]struct{}) []types.Id {
	var out []types.Id
	seen := map[types.Id]struct{}{}
	for _, v := range cands {
		if _, ok := visited[v]; ok {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func transitiveOutgoingExpected(refs []types.Ref, start types.Id) []TransitiveRefEntry {
	const maxLevel = 10
	visited := map[types.Id]struct{}{start: {}}
	_, seedOut := logicalEndpointsForTransitiveTests(refs, start)
	current := dedupeUnvisitedNeighbors(seedOut, visited)
	var res []TransitiveRefEntry
	for level := 1; level <= maxLevel; level++ {
		if len(current) == 0 {
			break
		}
		for _, v := range current {
			res = append(res, TransitiveRefEntry{ID: v, Distance: level})
		}
		for _, v := range current {
			visited[v] = struct{}{}
		}
		var next []types.Id
		nextSeen := map[types.Id]struct{}{}
		for _, u := range current {
			_, outs := logicalEndpointsForTransitiveTests(refs, u)
			for _, v := range outs {
				if _, ok := visited[v]; ok {
					continue
				}
				if _, ok := nextSeen[v]; ok {
					continue
				}
				nextSeen[v] = struct{}{}
				next = append(next, v)
			}
		}
		current = next
	}
	return res
}

func transitiveIncomingExpected(refs []types.Ref, start types.Id) []TransitiveRefEntry {
	const maxLevel = 10
	visited := map[types.Id]struct{}{start: {}}
	seedIn, _ := logicalEndpointsForTransitiveTests(refs, start)
	current := dedupeUnvisitedNeighbors(seedIn, visited)
	var res []TransitiveRefEntry
	for level := 1; level <= maxLevel; level++ {
		if len(current) == 0 {
			break
		}
		for _, v := range current {
			res = append(res, TransitiveRefEntry{ID: v, Distance: -level})
		}
		for _, v := range current {
			visited[v] = struct{}{}
		}
		var next []types.Id
		nextSeen := map[types.Id]struct{}{}
		for _, u := range current {
			inc, _ := logicalEndpointsForTransitiveTests(refs, u)
			for _, v := range inc {
				if _, ok := visited[v]; ok {
					continue
				}
				if _, ok := nextSeen[v]; ok {
					continue
				}
				nextSeen[v] = struct{}{}
				next = append(next, v)
			}
		}
		current = next
	}
	return res
}

func transitiveRefsGoldenExpected(refs []types.Ref, start types.Id) []TransitiveRefEntry {
	if start == "" {
		return nil
	}
	out := []TransitiveRefEntry{{ID: start, Distance: 0}}
	out = append(out, transitiveOutgoingExpected(refs, start)...)
	out = append(out, transitiveIncomingExpected(refs, start)...)
	return out
}

func transitiveEdgesGoldenExpected(refs []types.Ref, id types.Id) (incoming, outgoing []TransitiveRefEntry) {
	for _, e := range transitiveRefsGoldenExpected(refs, id) {
		if e.ID == id {
			continue
		}
		if e.Distance < 0 {
			incoming = append(incoming, e)
		} else if e.Distance > 0 {
			outgoing = append(outgoing, e)
		}
	}
	return sortTransitiveEntries(incoming), sortTransitiveEntries(outgoing)
}

func TestTransitiveRefsFromId_directEdgesSignedPlusOneMinusOne(t *testing.T) {
	idA := types.Id("a")
	idB := types.Id("b")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"out"}},
		{From: idB, To: idA, Labels: []string{"back"}},
	}
	want := transitiveRefsGoldenExpected(refs, idA)
	got := TransitiveRefsFromId(refs, idA)
	assert.Equal(t, sortTransitiveEntries(want), sortTransitiveEntries(got),
		"from A: B at +1 outgoing; from A incoming walk: B at -1")
}

func TestTransitiveRefsFromId_transitiveChainTwoHops(t *testing.T) {
	idA := types.Id("chain/a")
	idB := types.Id("chain/b")
	idC := types.Id("chain/c")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"ab"}},
		{From: idB, To: idC, Labels: []string{"bc"}},
	}
	want := transitiveRefsGoldenExpected(refs, idA)
	got := TransitiveRefsFromId(refs, idA)
	assert.Equal(t, sortTransitiveEntries(want), sortTransitiveEntries(got))
	assert.Contains(t, entriesByID(want), idC)
	assert.Equal(t, 2, entriesByID(want)[idC], "C must be two hops outgoing from A")
}

func TestTransitiveRefsFromId_reverseOwnerLogicalDirection(t *testing.T) {
	child := types.Id("apps/v1/ReplicaSet/ns/rs")
	parent := types.Id("apps/v1/Deployment/ns/deploy")
	refs := []types.Ref{
		{From: child, To: parent, Labels: []string{"controller"}, Reverse: true},
	}
	want := transitiveRefsGoldenExpected(refs, child)
	got := TransitiveRefsFromId(refs, child)
	assert.Equal(t, sortTransitiveEntries(want), sortTransitiveEntries(got))
	assert.Equal(t, -1, entriesByID(want)[parent], "owner should appear as logical incoming (-1) from child")

	wantP := transitiveRefsGoldenExpected(refs, parent)
	gotP := TransitiveRefsFromId(refs, parent)
	assert.Equal(t, sortTransitiveEntries(wantP), sortTransitiveEntries(gotP))
	assert.Equal(t, 1, entriesByID(wantP)[child], "child should appear as logical outgoing (+1) from parent")
}

func TestTransitiveRefsFromId_cycleTerminates(t *testing.T) {
	idA := types.Id("cyc/a")
	idB := types.Id("cyc/b")
	idC := types.Id("cyc/c")
	refs := []types.Ref{
		{From: idA, To: idB},
		{From: idB, To: idC},
		{From: idC, To: idA},
	}
	want := transitiveRefsGoldenExpected(refs, idA)
	got := TransitiveRefsFromId(refs, idA)
	assert.Equal(t, sortTransitiveEntries(want), sortTransitiveEntries(got))
	// BFS must terminate (no infinite expansion on the cycle).
	assert.Less(t, len(got), 32, "reachable set should stay small for a 3-cycle")
}

func TestTransitiveRefsFromId_respectsMaxDepthTen(t *testing.T) {
	var refs []types.Ref
	ids := make([]types.Id, 12)
	for i := range ids {
		ids[i] = types.Id(fmt.Sprintf("depth/ns/res-%d", i))
	}
	for i := 0; i < len(ids)-1; i++ {
		refs = append(refs, types.Ref{From: ids[i], To: ids[i+1]})
	}
	start := ids[0]
	want := transitiveRefsGoldenExpected(refs, start)
	got := TransitiveRefsFromId(refs, start)
	assert.Equal(t, sortTransitiveEntries(want), sortTransitiveEntries(got))
	byID := entriesByID(want)
	_, has11 := byID[ids[11]]
	assert.False(t, has11, "golden must not include node at distance 11")
	for _, e := range got {
		if e.Distance == 0 {
			continue
		}
		mag := e.Distance
		if mag < 0 {
			mag = -mag
		}
		assert.LessOrEqual(t, mag, 10, "distance magnitude must not exceed 10")
	}
}

func TestTransitiveRefsFromId_startIdDistanceZero(t *testing.T) {
	idA := types.Id("root")
	idB := types.Id("leaf")
	refs := []types.Ref{{From: idA, To: idB}}
	want := transitiveRefsGoldenExpected(refs, idA)
	got := TransitiveRefsFromId(refs, idA)
	var zero *TransitiveRefEntry
	for _, e := range got {
		if e.ID == idA {
			zero = &e
			break
		}
	}
	require.NotNil(t, zero)
	assert.Equal(t, 0, zero.Distance)
	assert.Equal(t, sortTransitiveEntries(want), sortTransitiveEntries(got))
}

func TestTransitiveRefsFromId_bidirectionalTwoEntriesForSameTarget(t *testing.T) {
	idA := types.Id("bi/a")
	idB := types.Id("bi/b")
	refs := []types.Ref{
		{From: idA, To: idB, Labels: []string{"a-to-b"}},
		{From: idB, To: idA, Labels: []string{"b-to-a"}},
	}
	want := transitiveRefsGoldenExpected(refs, idA)
	got := TransitiveRefsFromId(refs, idA)
	assert.Equal(t, sortTransitiveEntries(want), sortTransitiveEntries(got))
	distances := map[int]struct{}{}
	for _, e := range got {
		if e.ID != idB {
			continue
		}
		distances[e.Distance] = struct{}{}
	}
	assert.Contains(t, distances, 1, "B reachable at +1 along outgoing")
	assert.Contains(t, distances, -1, "B reachable at -1 along incoming")
}

func TestTransitiveRefsFromId_isolatedNodeReturnsAnchorOnly(t *testing.T) {
	start := types.Id("isolated/node")
	assert.Equal(t, []TransitiveRefEntry{{ID: start, Distance: 0}}, TransitiveRefsFromId(nil, start))
	assert.Equal(t, []TransitiveRefEntry{{ID: start, Distance: 0}}, TransitiveRefsFromId([]types.Ref{}, start))
}

func TestTransitiveRefsFromId_emptyStartReturnsNil(t *testing.T) {
	assert.Nil(t, TransitiveRefsFromId(nil, ""))
}

func TestTransitiveRefsFromId_reverseChainAcrossMultipleHops(t *testing.T) {
	child := types.Id("apps/v1/ReplicaSet/ns/child")
	parent := types.Id("apps/v1/Deployment/ns/parent")
	root := types.Id("apps/v1/StatefulSet/ns/root")
	refs := []types.Ref{
		{From: child, To: parent, Reverse: true, Labels: []string{"controller"}},
		{From: parent, To: root, Reverse: true, Labels: []string{"owner"}},
	}

	got := TransitiveRefsFromId(refs, child)
	assert.Equal(t, sortTransitiveEntries(transitiveRefsGoldenExpected(refs, child)), sortTransitiveEntries(got))
	assert.Equal(t, -2, entriesByID(got)[root])
}

func TestTransitiveEdgesForID_partitionsSignedSlices(t *testing.T) {
	idA := types.Id("p/a")
	idB := types.Id("p/b")
	idC := types.Id("p/c")
	refs := []types.Ref{
		{From: idA, To: idB},
		{From: idC, To: idA},
	}
	wIn, wOut := transitiveEdgesGoldenExpected(refs, idA)
	gIn, gOut := TransitiveEdgesForID(refs, idA)
	assert.Equal(t, wIn, gIn)
	assert.Equal(t, wOut, gOut)
}

func entriesByID(es []TransitiveRefEntry) map[types.Id]int {
	m := make(map[types.Id]int)
	for _, e := range es {
		m[e.ID] = e.Distance
	}
	return m
}
