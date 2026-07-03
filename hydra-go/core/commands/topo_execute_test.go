package commands

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// callRecord records events from start/waitReady callbacks in a thread-safe manner.
type callRecord struct {
	mu     sync.Mutex
	events []string
}

func (r *callRecord) record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *callRecord) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]string, len(r.events))
	copy(cp, r.events)
	return cp
}

func eventIndexOf(events []string, target string) int {
	for i, e := range events {
		if e == target {
			return i
		}
	}
	return -1
}

func hasEvent(events []string, target string) bool {
	return eventIndexOf(events, target) >= 0
}

// ---------------------------------------------------------------------------
// BuildDependencyGraph
// ---------------------------------------------------------------------------

func TestBuildDependencyGraph_LinearChain(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	c := makeEntity("", "v1", "Secret", "ns", "c")

	entities, err := entity.NewEntities([]entity.Entity{a, b, c})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/ConfigMap/ns/b", To: "apps/v1/Deployment/ns/a"},
		{From: "v1/Secret/ns/c", To: "v1/ConfigMap/ns/b"},
	}

	g, err := BuildDependencyGraph(entities, refs)
	require.NoError(t, err)

	assert.Equal(t, 0, g.InDegree["apps/v1/Deployment/ns/a"])
	assert.Equal(t, 1, g.InDegree["v1/ConfigMap/ns/b"])
	assert.Equal(t, 1, g.InDegree["v1/Secret/ns/c"])

	assert.ElementsMatch(t, []types.Id{"v1/ConfigMap/ns/b"}, g.Adj["apps/v1/Deployment/ns/a"])
	assert.ElementsMatch(t, []types.Id{"v1/Secret/ns/c"}, g.Adj["v1/ConfigMap/ns/b"])
}

func TestBuildDependencyGraph_Diamond(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	c := makeEntity("", "v1", "Secret", "ns", "c")
	d := makeEntity("", "v1", "ServiceAccount", "ns", "d")

	entities, err := entity.NewEntities([]entity.Entity{a, b, c, d})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/ServiceAccount/ns/d", To: "v1/ConfigMap/ns/b"},
		{From: "v1/ServiceAccount/ns/d", To: "v1/Secret/ns/c"},
		{From: "v1/ConfigMap/ns/b", To: "apps/v1/Deployment/ns/a"},
		{From: "v1/Secret/ns/c", To: "apps/v1/Deployment/ns/a"},
	}

	g, err := BuildDependencyGraph(entities, refs)
	require.NoError(t, err)

	assert.Equal(t, 0, g.InDegree["apps/v1/Deployment/ns/a"])
	assert.Equal(t, 1, g.InDegree["v1/ConfigMap/ns/b"])
	assert.Equal(t, 1, g.InDegree["v1/Secret/ns/c"])
	assert.Equal(t, 2, g.InDegree["v1/ServiceAccount/ns/d"])

	assert.ElementsMatch(t, []types.Id{"v1/ConfigMap/ns/b", "v1/Secret/ns/c"}, g.Adj["apps/v1/Deployment/ns/a"])
	assert.ElementsMatch(t, []types.Id{"v1/ServiceAccount/ns/d"}, g.Adj["v1/ConfigMap/ns/b"])
	assert.ElementsMatch(t, []types.Id{"v1/ServiceAccount/ns/d"}, g.Adj["v1/Secret/ns/c"])
}

func TestBuildDependencyGraph_NoRefs(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")

	entities, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)

	g, err := BuildDependencyGraph(entities, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, g.InDegree["apps/v1/Deployment/ns/a"])
	assert.Equal(t, 0, g.InDegree["v1/ConfigMap/ns/b"])
}

func TestBuildDependencyGraph_ExternalRefs(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")

	entities, err := entity.NewEntities([]entity.Entity{a})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/a", To: "v1/Secret/ns/external"},
		{From: "v1/ConfigMap/ns/external2", To: "apps/v1/Deployment/ns/a"},
	}

	g, err := BuildDependencyGraph(entities, refs)
	require.NoError(t, err)

	assert.Equal(t, 0, g.InDegree["apps/v1/Deployment/ns/a"])
	assert.Empty(t, g.Adj["apps/v1/Deployment/ns/a"])
}

func TestBuildDependencyGraph_SelfReference(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")

	entities, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/a", To: "apps/v1/Deployment/ns/a"},
		{From: "v1/ConfigMap/ns/b", To: "apps/v1/Deployment/ns/a"},
	}

	g, err := BuildDependencyGraph(entities, refs)
	require.NoError(t, err)

	assert.Equal(t, 0, g.InDegree["apps/v1/Deployment/ns/a"])
	assert.Equal(t, 1, g.InDegree["v1/ConfigMap/ns/b"])
}

func TestBuildDependencyGraph_Empty(t *testing.T) {
	entities, err := entity.NewEntities(nil)
	require.NoError(t, err)

	g, err := BuildDependencyGraph(entities, nil)
	require.NoError(t, err)

	assert.Empty(t, g.Adj)
	assert.Empty(t, g.InDegree)
	assert.Empty(t, g.Entities)
}

// ---------------------------------------------------------------------------
// TopologicalExecute
// ---------------------------------------------------------------------------

func TestTopologicalExecute_LinearChain(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")

	entities, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/ConfigMap/ns/b", To: "apps/v1/Deployment/ns/a"},
	}

	rec := &callRecord{}
	start := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("start:%s", id))
		return nil
	}
	waitReady := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("ready:%s", id))
		return nil
	}

	err = TopologicalExecute(context.Background(), log.Default(), entities, refs, start, waitReady)
	require.NoError(t, err)

	events := rec.snapshot()
	startA := eventIndexOf(events, "start:apps/v1/Deployment/ns/a")
	readyA := eventIndexOf(events, "ready:apps/v1/Deployment/ns/a")
	startB := eventIndexOf(events, "start:v1/ConfigMap/ns/b")
	readyB := eventIndexOf(events, "ready:v1/ConfigMap/ns/b")

	require.NotEqual(t, -1, startA)
	require.NotEqual(t, -1, readyA)
	require.NotEqual(t, -1, startB)
	require.NotEqual(t, -1, readyB)

	assert.Less(t, startA, startB, "A must be started before B")
	assert.Less(t, readyA, startB, "A must be ready before B starts")
}

func TestTopologicalExecute_Diamond(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	c := makeEntity("", "v1", "Secret", "ns", "c")
	d := makeEntity("", "v1", "ServiceAccount", "ns", "d")

	entities, err := entity.NewEntities([]entity.Entity{a, b, c, d})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/ServiceAccount/ns/d", To: "v1/ConfigMap/ns/b"},
		{From: "v1/ServiceAccount/ns/d", To: "v1/Secret/ns/c"},
		{From: "v1/ConfigMap/ns/b", To: "apps/v1/Deployment/ns/a"},
		{From: "v1/Secret/ns/c", To: "apps/v1/Deployment/ns/a"},
	}

	rec := &callRecord{}
	start := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("start:%s", id))
		return nil
	}
	waitReady := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("ready:%s", id))
		return nil
	}

	err = TopologicalExecute(context.Background(), log.Default(), entities, refs, start, waitReady)
	require.NoError(t, err)

	events := rec.snapshot()
	readyA := eventIndexOf(events, "ready:apps/v1/Deployment/ns/a")
	startB := eventIndexOf(events, "start:v1/ConfigMap/ns/b")
	startC := eventIndexOf(events, "start:v1/Secret/ns/c")
	readyB := eventIndexOf(events, "ready:v1/ConfigMap/ns/b")
	readyC := eventIndexOf(events, "ready:v1/Secret/ns/c")
	startD := eventIndexOf(events, "start:v1/ServiceAccount/ns/d")

	require.NotEqual(t, -1, readyA)
	require.NotEqual(t, -1, startB)
	require.NotEqual(t, -1, startC)
	require.NotEqual(t, -1, readyB)
	require.NotEqual(t, -1, readyC)
	require.NotEqual(t, -1, startD)

	assert.Less(t, readyA, startB, "B starts after A ready")
	assert.Less(t, readyA, startC, "C starts after A ready")
	assert.Less(t, readyB, startD, "D starts after B ready")
	assert.Less(t, readyC, startD, "D starts after C ready")
}

func TestTopologicalExecute_IndependentEntities(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	c := makeEntity("", "v1", "Secret", "ns", "c")

	entities, err := entity.NewEntities([]entity.Entity{a, b, c})
	require.NoError(t, err)

	rec := &callRecord{}
	start := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("start:%s", id))
		return nil
	}
	waitReady := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("ready:%s", id))
		return nil
	}

	err = TopologicalExecute(context.Background(), log.Default(), entities, nil, start, waitReady)
	require.NoError(t, err)

	events := rec.snapshot()
	assert.True(t, hasEvent(events, "start:apps/v1/Deployment/ns/a"))
	assert.True(t, hasEvent(events, "start:v1/ConfigMap/ns/b"))
	assert.True(t, hasEvent(events, "start:v1/Secret/ns/c"))
	assert.True(t, hasEvent(events, "ready:apps/v1/Deployment/ns/a"))
	assert.True(t, hasEvent(events, "ready:v1/ConfigMap/ns/b"))
	assert.True(t, hasEvent(events, "ready:v1/Secret/ns/c"))
}

func TestTopologicalExecute_EagerUnblocking(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	d := makeEntity("", "v1", "Secret", "ns", "d")
	e := makeEntity("", "v1", "ServiceAccount", "ns", "e")

	entities, err := entity.NewEntities([]entity.Entity{a, b, d, e})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/Secret/ns/d", To: "apps/v1/Deployment/ns/a"},
		{From: "v1/ServiceAccount/ns/e", To: "apps/v1/Deployment/ns/a"},
		{From: "v1/ServiceAccount/ns/e", To: "v1/ConfigMap/ns/b"},
	}

	rec := &callRecord{}
	bReadyCh := make(chan struct{})
	dStarted := make(chan struct{})

	start := func(ctx context.Context, ent entity.Entity) error {
		id, _ := ent.Id()
		rec.record(fmt.Sprintf("start:%s", id))
		if id == "v1/Secret/ns/d" {
			close(dStarted)
		}
		return nil
	}
	waitReady := func(ctx context.Context, ent entity.Entity) error {
		id, _ := ent.Id()
		if id == "v1/ConfigMap/ns/b" {
			select {
			case <-bReadyCh:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		rec.record(fmt.Sprintf("ready:%s", id))
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- TopologicalExecute(context.Background(), log.Default(), entities, refs, start, waitReady)
	}()

	select {
	case <-dStarted:
		// D started while B's waitReady is still blocked — proves eager unblocking
	case err := <-done:
		t.Fatalf("TopologicalExecute finished before D was started: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for D to start")
	}

	close(bReadyCh)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for TopologicalExecute to complete")
	}

	events := rec.snapshot()
	readyA := eventIndexOf(events, "ready:apps/v1/Deployment/ns/a")
	readyB := eventIndexOf(events, "ready:v1/ConfigMap/ns/b")
	startE := eventIndexOf(events, "start:v1/ServiceAccount/ns/e")

	require.NotEqual(t, -1, readyA)
	require.NotEqual(t, -1, readyB)
	require.NotEqual(t, -1, startE)
	assert.Greater(t, startE, readyA, "E starts after A ready")
	assert.Greater(t, startE, readyB, "E starts after B ready")
}

func TestTopologicalExecute_Cycle(t *testing.T) {
	a := makeEntity("", "v1", "ConfigMap", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	c := makeEntity("", "v1", "Secret", "ns", "c")

	entities, err := entity.NewEntities([]entity.Entity{a, b, c})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/ConfigMap/ns/a", To: "v1/ConfigMap/ns/b"},
		{From: "v1/ConfigMap/ns/b", To: "v1/ConfigMap/ns/a"},
	}

	rec := &callRecord{}
	start := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("start:%s", id))
		return nil
	}
	waitReady := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("ready:%s", id))
		return nil
	}

	err = TopologicalExecute(context.Background(), log.Default(), entities, refs, start, waitReady)
	require.NoError(t, err)

	events := rec.snapshot()
	assert.True(t, hasEvent(events, "start:v1/Secret/ns/c"), "independent entity C should be started")
	assert.True(t, hasEvent(events, "start:v1/ConfigMap/ns/a"), "cyclic entity A should be started")
	assert.True(t, hasEvent(events, "start:v1/ConfigMap/ns/b"), "cyclic entity B should be started")

	// Cyclic entities are only started after all reachable (non-cyclic) entities complete.
	readyC := eventIndexOf(events, "ready:v1/Secret/ns/c")
	startA := eventIndexOf(events, "start:v1/ConfigMap/ns/a")
	startB := eventIndexOf(events, "start:v1/ConfigMap/ns/b")

	assert.Less(t, readyC, startA, "C completed before cyclic A started")
	assert.Less(t, readyC, startB, "C completed before cyclic B started")
}

func TestTopologicalExecute_Empty(t *testing.T) {
	entities, err := entity.NewEntities(nil)
	require.NoError(t, err)

	startCalled := false
	readyCalled := false
	start := func(ctx context.Context, e entity.Entity) error {
		startCalled = true
		return nil
	}
	waitReady := func(ctx context.Context, e entity.Entity) error {
		readyCalled = true
		return nil
	}

	err = TopologicalExecute(context.Background(), log.Default(), entities, nil, start, waitReady)
	require.NoError(t, err)
	assert.False(t, startCalled)
	assert.False(t, readyCalled)
}

func TestTopologicalExecute_SingleEntity(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")

	entities, err := entity.NewEntities([]entity.Entity{a})
	require.NoError(t, err)

	rec := &callRecord{}
	start := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("start:%s", id))
		return nil
	}
	waitReady := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("ready:%s", id))
		return nil
	}

	err = TopologicalExecute(context.Background(), log.Default(), entities, nil, start, waitReady)
	require.NoError(t, err)

	events := rec.snapshot()
	assert.Equal(t, []string{
		"start:apps/v1/Deployment/ns/a",
		"ready:apps/v1/Deployment/ns/a",
	}, events)
}

func TestTopologicalExecute_StartError(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")

	entities, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/ConfigMap/ns/b", To: "apps/v1/Deployment/ns/a"},
	}

	start := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		if id == "apps/v1/Deployment/ns/a" {
			return fmt.Errorf("start failed for %s", id)
		}
		return nil
	}
	waitReady := func(ctx context.Context, e entity.Entity) error {
		return nil
	}

	err = TopologicalExecute(context.Background(), log.Default(), entities, refs, start, waitReady)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start failed")
}

func TestTopologicalExecute_WaitReadyError(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")

	entities, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/ConfigMap/ns/b", To: "apps/v1/Deployment/ns/a"},
	}

	rec := &callRecord{}
	start := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("start:%s", id))
		return nil
	}
	waitReady := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		if id == "apps/v1/Deployment/ns/a" {
			return fmt.Errorf("readiness check failed for %s", id)
		}
		rec.record(fmt.Sprintf("ready:%s", id))
		return nil
	}

	err = TopologicalExecute(context.Background(), log.Default(), entities, refs, start, waitReady)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "readiness check failed")

	events := rec.snapshot()
	assert.False(t, hasEvent(events, "start:v1/ConfigMap/ns/b"),
		"B should not be started after A's readiness check failed")
}

func TestTopologicalExecute_ConcurrentEntityFail(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	c := makeEntity("", "v1", "Secret", "ns", "c")

	entities, err := entity.NewEntities([]entity.Entity{a, b, c})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/Secret/ns/c", To: "apps/v1/Deployment/ns/a"},
		{From: "v1/Secret/ns/c", To: "v1/ConfigMap/ns/b"},
	}

	rec := &callRecord{}
	start := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("start:%s", id))
		if id == "apps/v1/Deployment/ns/a" {
			return fmt.Errorf("start failed")
		}
		return nil
	}
	waitReady := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		rec.record(fmt.Sprintf("ready:%s", id))
		return nil
	}

	err = TopologicalExecute(context.Background(), log.Default(), entities, refs, start, waitReady)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start failed")

	events := rec.snapshot()
	assert.False(t, hasEvent(events, "start:v1/Secret/ns/c"),
		"C should not be started after A failed")
}

func TestTopologicalExecute_ContextCancelledOnFailure(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	c := makeEntity("", "v1", "Secret", "ns", "c")

	entities, err := entity.NewEntities([]entity.Entity{a, b, c})
	require.NoError(t, err)

	// A and B are independent (no deps), C depends on both.
	refs := []types.Ref{
		{From: "v1/Secret/ns/c", To: "apps/v1/Deployment/ns/a"},
		{From: "v1/Secret/ns/c", To: "v1/ConfigMap/ns/b"},
	}

	bWaitStarted := make(chan struct{})
	bCtxCancelled := make(chan struct{})

	start := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		if id == "apps/v1/Deployment/ns/a" {
			return fmt.Errorf("start failed for A")
		}
		return nil
	}
	waitReady := func(ctx context.Context, e entity.Entity) error {
		id, _ := e.Id()
		if id == "v1/ConfigMap/ns/b" {
			close(bWaitStarted)
			<-ctx.Done()
			close(bCtxCancelled)
			return ctx.Err()
		}
		return nil
	}

	err = TopologicalExecute(context.Background(), log.Default(), entities, refs, start, waitReady)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start failed for A")

	select {
	case <-bWaitStarted:
		// B's waitReady was entered — good
	case <-time.After(2 * time.Second):
		t.Fatal("B's waitReady was never called")
	}

	select {
	case <-bCtxCancelled:
		// B's context was cancelled — this is what we want to verify
	case <-time.After(2 * time.Second):
		t.Fatal("B's context was not cancelled after A failed")
	}
}

// ---------------------------------------------------------------------------
// BuildDependencyGraph with Reverse flag
// ---------------------------------------------------------------------------

func TestBuildDependencyGraph_ReverseFlag(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("apps", "v1", "Deployment", "ns", "b")

	entities, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/a", To: "apps/v1/Deployment/ns/b", Reverse: true},
	}

	g, err := BuildDependencyGraph(entities, refs)
	require.NoError(t, err)

	// Reverse=true swaps direction: B depends on A (instead of A depends on B)
	assert.Equal(t, 0, g.InDegree["apps/v1/Deployment/ns/a"])
	assert.Equal(t, 1, g.InDegree["apps/v1/Deployment/ns/b"])
	assert.ElementsMatch(t, []types.Id{"apps/v1/Deployment/ns/b"}, g.Adj["apps/v1/Deployment/ns/a"])
}

// ---------------------------------------------------------------------------
// ResolveTransitiveWorkloadDeps
// ---------------------------------------------------------------------------

func TestResolveTransitiveWorkloadDeps_ThroughSecret(t *testing.T) {
	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/dep-a": true,
		"apps/v1/Deployment/ns/dep-b": true,
	}

	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/dep-a", To: "v1/Secret/ns/secret-x"},
		{From: "v1/Secret/ns/secret-x", To: "apps/v1/Deployment/ns/dep-b"},
	}

	enriched := ResolveTransitiveWorkloadDeps(refs, workloadIds)

	var synthetic []types.Ref
	for _, r := range enriched {
		if r.RefType == types.RefTypeIndirect {
			synthetic = append(synthetic, r)
		}
	}
	require.Len(t, synthetic, 1)
	assert.Equal(t, types.Id("apps/v1/Deployment/ns/dep-a"), synthetic[0].From)
	assert.Equal(t, types.Id("apps/v1/Deployment/ns/dep-b"), synthetic[0].To)
}

func TestResolveTransitiveWorkloadDeps_LongChain(t *testing.T) {
	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/dex/dex":                                     true,
		"apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator": true,
	}

	// Deployment → Secret (imagePullSecrets) → Secret (clone-source) ← SopsSecret (reverse) → operator-dep
	refs := []types.Ref{
		{From: "apps/v1/Deployment/dex/dex", To: "v1/Secret/dex/image-pull-secret"},
		{From: "v1/Secret/dex/image-pull-secret", To: "v1/Secret/sops-secrets-operator/image-pull-secret"},
		{From: "isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret", To: "v1/Secret/sops-secrets-operator/image-pull-secret", Reverse: true},
		{From: "isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret", To: "apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator"},
	}

	enriched := ResolveTransitiveWorkloadDeps(refs, workloadIds)

	var synthetic []types.Ref
	for _, r := range enriched {
		if r.RefType == types.RefTypeIndirect {
			synthetic = append(synthetic, r)
		}
	}
	require.Len(t, synthetic, 1)
	assert.Equal(t, types.Id("apps/v1/Deployment/dex/dex"), synthetic[0].From)
	assert.Equal(t, types.Id("apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator"), synthetic[0].To)
}

func TestResolveTransitiveWorkloadDeps_ThroughStorageClassAndCSIDriver(t *testing.T) {
	workloadIds := map[types.Id]bool{
		"apps/v1/StatefulSet/demo/activemq":          true,
		"apps/v1/DaemonSet/kube-system/csi-nfs-node": true,
	}

	refs := []types.Ref{
		{
			From:         "apps/v1/StatefulSet/demo/activemq",
			To:           "storage.k8s.io/v1/StorageClass//nfs-csi",
			EndpointType: types.RefEndpointTypeId,
			Labels:       []string{"volumeClaimTemplate storageClass"},
		},
		{
			From:         "storage.k8s.io/v1/StorageClass//nfs-csi",
			To:           "storage.k8s.io/v1/CSIDriver//nfs.csi.k8s.io",
			EndpointType: types.RefEndpointTypeId,
			Labels:       []string{"provisioner"},
		},
		{
			From:         "storage.k8s.io/v1/CSIDriver//nfs.csi.k8s.io",
			To:           "apps/v1/DaemonSet/kube-system/csi-nfs-node",
			EndpointType: types.RefEndpointTypeProvider,
		},
	}

	enriched := ResolveTransitiveWorkloadDeps(refs, workloadIds)

	var synthetic []types.Ref
	for _, r := range enriched {
		if r.RefType == types.RefTypeIndirect {
			synthetic = append(synthetic, r)
		}
	}
	require.Len(t, synthetic, 1)
	assert.Equal(t, types.Id("apps/v1/StatefulSet/demo/activemq"), synthetic[0].From)
	assert.Equal(t, types.Id("apps/v1/DaemonSet/kube-system/csi-nfs-node"), synthetic[0].To)
}

func TestResolveTransitiveWorkloadDeps_PropagatesOptionalTag(t *testing.T) {
	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/monitoring/node-exporter":            true,
		"apps/v1/Deployment/monitoring/kube-prometheus-operator": true,
	}

	refs := []types.Ref{
		{From: "apps/v1/Deployment/monitoring/node-exporter", To: "v1/PodMonitor/monitoring/node-exporter"},
		{
			From: "v1/PodMonitor/monitoring/node-exporter",
			To:   "apps/v1/Deployment/monitoring/kube-prometheus-operator",
			Tags: []string{types.RefTagOptionalStartup},
		},
	}

	enriched := ResolveTransitiveWorkloadDeps(refs, workloadIds)

	var synthetic []types.Ref
	for _, r := range enriched {
		if r.RefType == types.RefTypeIndirect {
			synthetic = append(synthetic, r)
		}
	}
	require.Len(t, synthetic, 1)
	assert.Equal(t, types.Id("apps/v1/Deployment/monitoring/node-exporter"), synthetic[0].From)
	assert.Equal(t, types.Id("apps/v1/Deployment/monitoring/kube-prometheus-operator"), synthetic[0].To)
	assert.Contains(t, synthetic[0].Tags, types.RefTagOptionalStartup)
}

func TestResolveTransitiveWorkloadDeps_NoIntermediaries(t *testing.T) {
	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/a": true,
		"apps/v1/Deployment/ns/b": true,
	}

	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/a", To: "apps/v1/Deployment/ns/b"},
	}

	enriched := ResolveTransitiveWorkloadDeps(refs, workloadIds)

	// No synthetic refs needed; direct workload-to-workload ref already exists
	for _, r := range enriched {
		assert.NotEqual(t, types.RefTypeIndirect, r.RefType)
	}
}

func TestResolveTransitiveWorkloadDeps_NoPath(t *testing.T) {
	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/a": true,
		"apps/v1/Deployment/ns/b": true,
	}

	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/a", To: "v1/Secret/ns/orphan"},
	}

	enriched := ResolveTransitiveWorkloadDeps(refs, workloadIds)

	for _, r := range enriched {
		assert.NotEqual(t, types.RefTypeIndirect, r.RefType)
	}
}

func TestResolveTransitiveWorkloadDeps_Empty(t *testing.T) {
	enriched := ResolveTransitiveWorkloadDeps(nil, nil)
	assert.Empty(t, enriched)
}

func TestResolveTransitiveWorkloadDeps_Diamond(t *testing.T) {
	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/frontend": true,
		"apps/v1/Deployment/ns/backend":  true,
		"apps/v1/Deployment/ns/database": true,
	}

	// frontend → configMap → backend, frontend → secret → database
	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/frontend", To: "v1/ConfigMap/ns/config"},
		{From: "v1/ConfigMap/ns/config", To: "apps/v1/Deployment/ns/backend"},
		{From: "apps/v1/Deployment/ns/frontend", To: "v1/Secret/ns/creds"},
		{From: "v1/Secret/ns/creds", To: "apps/v1/Deployment/ns/database"},
	}

	enriched := ResolveTransitiveWorkloadDeps(refs, workloadIds)

	var synthetic []types.Ref
	for _, r := range enriched {
		if r.RefType == types.RefTypeIndirect {
			synthetic = append(synthetic, r)
		}
	}
	require.Len(t, synthetic, 2)

	edges := map[[2]types.Id]bool{}
	for _, r := range synthetic {
		edges[[2]types.Id{r.From, r.To}] = true
	}
	assert.True(t, edges[[2]types.Id{"apps/v1/Deployment/ns/frontend", "apps/v1/Deployment/ns/backend"}])
	assert.True(t, edges[[2]types.Id{"apps/v1/Deployment/ns/frontend", "apps/v1/Deployment/ns/database"}])
}

func TestResolveTransitiveWorkloadDeps_StopsAtWorkload(t *testing.T) {
	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/a": true,
		"apps/v1/Deployment/ns/b": true,
		"apps/v1/Deployment/ns/c": true,
	}

	// a → secret → b → configMap → c
	// a should depend on b (not directly on c), because b is a workload that stops the BFS
	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/a", To: "v1/Secret/ns/s"},
		{From: "v1/Secret/ns/s", To: "apps/v1/Deployment/ns/b"},
		{From: "apps/v1/Deployment/ns/b", To: "v1/ConfigMap/ns/cm"},
		{From: "v1/ConfigMap/ns/cm", To: "apps/v1/Deployment/ns/c"},
	}

	enriched := ResolveTransitiveWorkloadDeps(refs, workloadIds)

	var synthetic []types.Ref
	for _, r := range enriched {
		if r.RefType == types.RefTypeIndirect {
			synthetic = append(synthetic, r)
		}
	}

	edges := map[[2]types.Id]bool{}
	for _, r := range synthetic {
		edges[[2]types.Id{r.From, r.To}] = true
	}
	assert.True(t, edges[[2]types.Id{"apps/v1/Deployment/ns/a", "apps/v1/Deployment/ns/b"}])
	assert.True(t, edges[[2]types.Id{"apps/v1/Deployment/ns/b", "apps/v1/Deployment/ns/c"}])
	assert.False(t, edges[[2]types.Id{"apps/v1/Deployment/ns/a", "apps/v1/Deployment/ns/c"}],
		"a should not directly depend on c; b stops the BFS")
}

func TestResolveTransitiveWorkloadDeps_CycleInNonWorkloads(t *testing.T) {
	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/a": true,
	}

	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/a", To: "v1/Secret/ns/x"},
		{From: "v1/Secret/ns/x", To: "v1/ConfigMap/ns/y"},
		{From: "v1/ConfigMap/ns/y", To: "v1/Secret/ns/x"},
	}

	enriched := ResolveTransitiveWorkloadDeps(refs, workloadIds)

	for _, r := range enriched {
		assert.NotEqual(t, types.RefTypeIndirect, r.RefType,
			"cycle in non-workloads should not produce synthetic refs")
	}
}

// ---------------------------------------------------------------------------
// ReverseRefs
// ---------------------------------------------------------------------------

func TestReverseRefs_Single(t *testing.T) {
	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/a", To: "v1/ConfigMap/ns/b"},
	}

	reversed := ReverseRefs(refs)
	require.Len(t, reversed, 1)
	assert.Equal(t, types.Id("v1/ConfigMap/ns/b"), reversed[0].From)
	assert.Equal(t, types.Id("apps/v1/Deployment/ns/a"), reversed[0].To)
}

func TestReverseRefs_Empty(t *testing.T) {
	reversed := ReverseRefs(nil)
	assert.Empty(t, reversed)
}

func TestReverseRefs_PreservesFields(t *testing.T) {
	refs := []types.Ref{
		{
			RefType:      types.RefTypeDirect,
			EndpointType: types.RefEndpointTypeId,
			From:         "apps/v1/Deployment/ns/a",
			To:           "v1/ConfigMap/ns/b",
			Labels:       []string{"volume", "config"},
			Tags:         []string{types.RefTagOptionalStartup},
			Desc:         "my-desc",
			Reverse:      true,
		},
	}

	reversed := ReverseRefs(refs)
	require.Len(t, reversed, 1)

	r := reversed[0]
	assert.Equal(t, types.Id("v1/ConfigMap/ns/b"), r.From)
	assert.Equal(t, types.Id("apps/v1/Deployment/ns/a"), r.To)
	assert.Equal(t, types.RefTypeDirect, r.RefType)
	assert.Equal(t, types.RefEndpointTypeId, r.EndpointType)
	assert.Equal(t, []string{"volume", "config"}, r.Labels)
	assert.Equal(t, []string{types.RefTagOptionalStartup}, r.Tags)
	assert.Equal(t, "my-desc", r.Desc)
	assert.Equal(t, true, r.Reverse)
}

// ---------------------------------------------------------------------------
// PlanTopologicalOrder
// ---------------------------------------------------------------------------

func TestPlanTopologicalOrder_LinearChain(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	c := makeEntity("", "v1", "Secret", "ns", "c")

	entities, err := entity.NewEntities([]entity.Entity{a, b, c})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/a", To: "v1/ConfigMap/ns/b"},
		{From: "v1/ConfigMap/ns/b", To: "v1/Secret/ns/c"},
	}

	g, err := BuildDependencyGraph(entities, refs)
	require.NoError(t, err)

	plan := PlanTopologicalOrder(g)

	require.Len(t, plan, 3)
	assert.Equal(t, "v1/Secret/ns/c", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
	assert.Equal(t, "v1/ConfigMap/ns/b", plan[1].Name)
	assert.Equal(t, []string{"v1/Secret/ns/c"}, plan[1].Dependencies)
	assert.Equal(t, "apps/v1/Deployment/ns/a", plan[2].Name)
	assert.Equal(t, []string{"v1/ConfigMap/ns/b"}, plan[2].Dependencies)
}

func TestPlanTopologicalOrder_Diamond(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	c := makeEntity("", "v1", "Secret", "ns", "c")
	d := makeEntity("", "v1", "ServiceAccount", "ns", "d")

	entities, err := entity.NewEntities([]entity.Entity{a, b, c, d})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/ServiceAccount/ns/d", To: "v1/ConfigMap/ns/b"},
		{From: "v1/ServiceAccount/ns/d", To: "v1/Secret/ns/c"},
		{From: "v1/ConfigMap/ns/b", To: "apps/v1/Deployment/ns/a"},
		{From: "v1/Secret/ns/c", To: "apps/v1/Deployment/ns/a"},
	}

	g, err := BuildDependencyGraph(entities, refs)
	require.NoError(t, err)

	plan := PlanTopologicalOrder(g)

	require.Len(t, plan, 4)

	assert.Equal(t, "apps/v1/Deployment/ns/a", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)

	assert.Equal(t, "v1/ConfigMap/ns/b", plan[1].Name)
	assert.Equal(t, []string{"apps/v1/Deployment/ns/a"}, plan[1].Dependencies)
	assert.Equal(t, "v1/Secret/ns/c", plan[2].Name)
	assert.Equal(t, []string{"apps/v1/Deployment/ns/a"}, plan[2].Dependencies)

	assert.Equal(t, "v1/ServiceAccount/ns/d", plan[3].Name)
	assert.Equal(t, []string{"v1/ConfigMap/ns/b", "v1/Secret/ns/c"}, plan[3].Dependencies)
}

func TestPlanTopologicalOrder_NoRefs(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")
	b := makeEntity("", "v1", "ConfigMap", "ns", "b")
	c := makeEntity("", "v1", "Secret", "ns", "c")

	entities, err := entity.NewEntities([]entity.Entity{a, b, c})
	require.NoError(t, err)

	g, err := BuildDependencyGraph(entities, nil)
	require.NoError(t, err)

	plan := PlanTopologicalOrder(g)

	require.Len(t, plan, 3)
	assert.Equal(t, "apps/v1/Deployment/ns/a", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
	assert.Equal(t, "v1/ConfigMap/ns/b", plan[1].Name)
	assert.Empty(t, plan[1].Dependencies)
	assert.Equal(t, "v1/Secret/ns/c", plan[2].Name)
	assert.Empty(t, plan[2].Dependencies)
}

func TestPlanTopologicalOrder_SingleEntity(t *testing.T) {
	a := makeEntity("apps", "v1", "Deployment", "ns", "a")

	entities, err := entity.NewEntities([]entity.Entity{a})
	require.NoError(t, err)

	g, err := BuildDependencyGraph(entities, nil)
	require.NoError(t, err)

	plan := PlanTopologicalOrder(g)

	require.Len(t, plan, 1)
	assert.Equal(t, "apps/v1/Deployment/ns/a", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
}

func TestPlanTopologicalOrder_EmptyGraph(t *testing.T) {
	entities, err := entity.NewEntities(nil)
	require.NoError(t, err)

	g, err := BuildDependencyGraph(entities, nil)
	require.NoError(t, err)

	plan := PlanTopologicalOrder(g)

	assert.Empty(t, plan)
}

func TestPlanTopologicalOrder_IndependentEntities(t *testing.T) {
	x := makeEntity("", "v1", "ConfigMap", "ns", "x")
	y := makeEntity("", "v1", "Secret", "ns", "y")
	z := makeEntity("", "v1", "ServiceAccount", "ns", "z")

	entities, err := entity.NewEntities([]entity.Entity{z, x, y})
	require.NoError(t, err)

	g, err := BuildDependencyGraph(entities, nil)
	require.NoError(t, err)

	plan := PlanTopologicalOrder(g)

	require.Len(t, plan, 3)
	assert.Equal(t, "v1/ConfigMap/ns/x", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
	assert.Equal(t, "v1/Secret/ns/y", plan[1].Name)
	assert.Empty(t, plan[1].Dependencies)
	assert.Equal(t, "v1/ServiceAccount/ns/z", plan[2].Name)
	assert.Empty(t, plan[2].Dependencies)
}

func TestPlanTopologicalOrder_Cycle(t *testing.T) {
	a := makeEntity("", "v1", "ConfigMap", "ns", "a")
	b := makeEntity("", "v1", "Secret", "ns", "b")

	entities, err := entity.NewEntities([]entity.Entity{a, b})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "v1/ConfigMap/ns/a", To: "v1/Secret/ns/b"},
		{From: "v1/Secret/ns/b", To: "v1/ConfigMap/ns/a"},
	}

	g, err := BuildDependencyGraph(entities, refs)
	require.NoError(t, err)

	plan := PlanTopologicalOrder(g)

	require.Len(t, plan, 2)
	assert.Equal(t, "v1/ConfigMap/ns/a", plan[0].Name)
	assert.Equal(t, []string{"v1/Secret/ns/b"}, plan[0].Dependencies)
	assert.Equal(t, "v1/Secret/ns/b", plan[1].Name)
	assert.Equal(t, []string{"v1/ConfigMap/ns/a"}, plan[1].Dependencies)
}

func TestPlanTopologicalOrder_DeterministicOrdering(t *testing.T) {
	e1 := makeEntity("", "v1", "ConfigMap", "ns", "charlie")
	e2 := makeEntity("", "v1", "ConfigMap", "ns", "alpha")
	e3 := makeEntity("", "v1", "ConfigMap", "ns", "bravo")

	entities1, err := entity.NewEntities([]entity.Entity{e1, e2, e3})
	require.NoError(t, err)
	g1, err := BuildDependencyGraph(entities1, nil)
	require.NoError(t, err)

	entities2, err := entity.NewEntities([]entity.Entity{e3, e1, e2})
	require.NoError(t, err)
	g2, err := BuildDependencyGraph(entities2, nil)
	require.NoError(t, err)

	plan1 := PlanTopologicalOrder(g1)
	plan2 := PlanTopologicalOrder(g2)

	require.Len(t, plan1, 3)
	require.Len(t, plan2, 3)

	assert.Equal(t, "v1/ConfigMap/ns/alpha", plan1[0].Name)
	assert.Equal(t, "v1/ConfigMap/ns/bravo", plan1[1].Name)
	assert.Equal(t, "v1/ConfigMap/ns/charlie", plan1[2].Name)

	for i := range plan1 {
		assert.Equal(t, plan1[i].Name, plan2[i].Name,
			"entry %d should have the same name regardless of input order", i)
		assert.Equal(t, plan1[i].Dependencies, plan2[i].Dependencies,
			"entry %d should have the same dependencies regardless of input order", i)
	}
}

// ---------------------------------------------------------------------------
// ResolveAppBasedWorkloadDeps
// ---------------------------------------------------------------------------

func TestResolveAppBasedWorkloadDeps_CrossAppCRDRef(t *testing.T) {
	// App "dex": Deployment + SopsSecret (CR)
	// App "sops": Deployment (operator) + CRD
	// The CR→CRD ref crosses apps, so dex workloads should depend on sops workloads.
	dexDeploy := withAppIds(makeEntity("apps", "v1", "Deployment", "dex", "dex"), []types.AppId{"dex"})
	sopsDeploy := withAppIds(makeEntity("apps", "v1", "Deployment", "sops-operator", "sops-operator"), []types.AppId{"sops"})
	sopsSecret := withAppIds(makeEntity("isindir.github.com", "v1alpha3", "SopsSecret", "dex", "my-secret"), []types.AppId{"dex"})
	sopsCRD := withAppIds(makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "sopssecrets.isindir.github.com"), []types.AppId{"sops"})

	allEntities, err := entity.NewEntities([]entity.Entity{dexDeploy, sopsDeploy, sopsSecret, sopsCRD})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "isindir.github.com/v1alpha3/SopsSecret/dex/my-secret",
			To: "apiextensions.k8s.io/v1/CustomResourceDefinition//sopssecrets.isindir.github.com"},
	}

	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/dex/dex":                     true,
		"apps/v1/Deployment/sops-operator/sops-operator": true,
	}

	enriched := ResolveAppBasedWorkloadDeps(allEntities, refs, workloadIds)

	var synthetic []types.Ref
	for _, r := range enriched[len(refs):] {
		if r.RefType == types.RefTypeIndirect {
			synthetic = append(synthetic, r)
		}
	}
	require.Len(t, synthetic, 1)
	assert.Equal(t, types.Id("apps/v1/Deployment/dex/dex"), synthetic[0].From)
	assert.Equal(t, types.Id("apps/v1/Deployment/sops-operator/sops-operator"), synthetic[0].To)
}

func TestResolveAppBasedWorkloadDeps_SameAppNoSynthetic(t *testing.T) {
	// Both entities in same app → no cross-app dependency
	deploy := withAppIds(makeEntity("apps", "v1", "Deployment", "ns", "web"), []types.AppId{"my-app"})
	secret := withAppIds(makeEntity("", "v1", "Secret", "ns", "my-secret"), []types.AppId{"my-app"})

	allEntities, err := entity.NewEntities([]entity.Entity{deploy, secret})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/web", To: "v1/Secret/ns/my-secret"},
	}

	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/web": true,
	}

	enriched := ResolveAppBasedWorkloadDeps(allEntities, refs, workloadIds)
	assert.Equal(t, len(refs), len(enriched), "no synthetic refs for same-app dependencies")
}

func TestResolveAppBasedWorkloadDeps_NoAppIds(t *testing.T) {
	// Entities without appIds should be ignored
	deploy := makeEntity("apps", "v1", "Deployment", "ns", "web")
	secret := makeEntity("", "v1", "Secret", "ns", "other")

	allEntities, err := entity.NewEntities([]entity.Entity{deploy, secret})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/ns/web", To: "v1/Secret/ns/other"},
	}

	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/web": true,
	}

	enriched := ResolveAppBasedWorkloadDeps(allEntities, refs, workloadIds)
	assert.Equal(t, len(refs), len(enriched), "no synthetic refs when entities lack appIds")
}

func TestResolveAppBasedWorkloadDeps_MultipleWorkloadsPerApp(t *testing.T) {
	// App "frontend" has 2 workloads, app "backend" has 1
	fe1 := withAppIds(makeEntity("apps", "v1", "Deployment", "ns", "fe1"), []types.AppId{"frontend"})
	fe2 := withAppIds(makeEntity("apps", "v1", "Deployment", "ns", "fe2"), []types.AppId{"frontend"})
	be := withAppIds(makeEntity("apps", "v1", "Deployment", "ns", "be"), []types.AppId{"backend"})
	crFE := withAppIds(makeEntity("example.com", "v1", "Widget", "ns", "w1"), []types.AppId{"frontend"})
	crdBE := withAppIds(makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "widgets.example.com"), []types.AppId{"backend"})

	allEntities, err := entity.NewEntities([]entity.Entity{fe1, fe2, be, crFE, crdBE})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "example.com/v1/Widget/ns/w1",
			To: "apiextensions.k8s.io/v1/CustomResourceDefinition//widgets.example.com"},
	}

	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/fe1": true,
		"apps/v1/Deployment/ns/fe2": true,
		"apps/v1/Deployment/ns/be":  true,
	}

	enriched := ResolveAppBasedWorkloadDeps(allEntities, refs, workloadIds)
	synthetic := enriched[len(refs):]
	require.Len(t, synthetic, 2)

	edges := map[[2]types.Id]bool{}
	for _, r := range synthetic {
		edges[[2]types.Id{r.From, r.To}] = true
	}
	assert.True(t, edges[[2]types.Id{"apps/v1/Deployment/ns/fe1", "apps/v1/Deployment/ns/be"}])
	assert.True(t, edges[[2]types.Id{"apps/v1/Deployment/ns/fe2", "apps/v1/Deployment/ns/be"}])
}

func TestResolveAppBasedWorkloadDeps_ReverseRef(t *testing.T) {
	// Reverse ref: logical direction is To→From
	appA := withAppIds(makeEntity("apps", "v1", "Deployment", "ns", "a"), []types.AppId{"app-a"})
	appB := withAppIds(makeEntity("apps", "v1", "Deployment", "ns", "b"), []types.AppId{"app-b"})
	entityA := withAppIds(makeEntity("", "v1", "Secret", "ns", "sa"), []types.AppId{"app-a"})
	entityB := withAppIds(makeEntity("", "v1", "Secret", "ns", "sb"), []types.AppId{"app-b"})

	allEntities, err := entity.NewEntities([]entity.Entity{appA, appB, entityA, entityB})
	require.NoError(t, err)

	// Reverse ref: From=entityA, To=entityB, Reverse=true → logical: entityB depends on entityA
	// So app-b depends on app-a
	refs := []types.Ref{
		{From: "v1/Secret/ns/sa", To: "v1/Secret/ns/sb", Reverse: true},
	}

	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/a": true,
		"apps/v1/Deployment/ns/b": true,
	}

	enriched := ResolveAppBasedWorkloadDeps(allEntities, refs, workloadIds)
	synthetic := enriched[len(refs):]
	require.Len(t, synthetic, 1)
	assert.Equal(t, types.Id("apps/v1/Deployment/ns/b"), synthetic[0].From)
	assert.Equal(t, types.Id("apps/v1/Deployment/ns/a"), synthetic[0].To)
}

func TestResolveAppBasedWorkloadDeps_EmptyRefs(t *testing.T) {
	enriched := ResolveAppBasedWorkloadDeps(entity.Entities{}, nil, nil)
	assert.Empty(t, enriched)
}

func TestResolveAppBasedWorkloadDeps_NoDuplicates(t *testing.T) {
	// Multiple cross-app refs between the same two apps should produce only one synthetic edge per workload pair
	a := withAppIds(makeEntity("apps", "v1", "Deployment", "ns", "a"), []types.AppId{"app-a"})
	b := withAppIds(makeEntity("apps", "v1", "Deployment", "ns", "b"), []types.AppId{"app-b"})
	cr1 := withAppIds(makeEntity("x.io", "v1", "Foo", "ns", "f1"), []types.AppId{"app-a"})
	cr2 := withAppIds(makeEntity("x.io", "v1", "Foo", "ns", "f2"), []types.AppId{"app-a"})
	crd := withAppIds(makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "foos.x.io"), []types.AppId{"app-b"})

	allEntities, err := entity.NewEntities([]entity.Entity{a, b, cr1, cr2, crd})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "x.io/v1/Foo/ns/f1", To: "apiextensions.k8s.io/v1/CustomResourceDefinition//foos.x.io"},
		{From: "x.io/v1/Foo/ns/f2", To: "apiextensions.k8s.io/v1/CustomResourceDefinition//foos.x.io"},
	}

	workloadIds := map[types.Id]bool{
		"apps/v1/Deployment/ns/a": true,
		"apps/v1/Deployment/ns/b": true,
	}

	enriched := ResolveAppBasedWorkloadDeps(allEntities, refs, workloadIds)
	synthetic := enriched[len(refs):]
	require.Len(t, synthetic, 1, "duplicate cross-app edges should be deduplicated")
	assert.Equal(t, types.Id("apps/v1/Deployment/ns/a"), synthetic[0].From)
	assert.Equal(t, types.Id("apps/v1/Deployment/ns/b"), synthetic[0].To)
}
