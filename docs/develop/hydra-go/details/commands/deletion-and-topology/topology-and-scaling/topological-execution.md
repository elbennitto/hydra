# Deletion and Topology: Topological Execution

This page covers the dependency graph helpers, dynamic topological executor, and the related test coverage used by scale and delete operations.

Back to [Topology and Scaling](../topology-and-scaling.md).

## Topological Execution

### TopologicalExecute

```go
func TopologicalExecute(
    ctx context.Context,
    l log.Logger,
    entities entity.Entities,
    refs []types.Ref,
    start func(ctx context.Context, e entity.Entity) error,
    waitReady func(ctx context.Context, e entity.Entity) error,
) error
```text

**Source file:** `core/commands/topo_execute.go`

Dynamic DAG executor that processes entities in dependency order. Instead of producing a sorted list, it actively schedules entities as soon as their dependencies are satisfied.

**Algorithm:** Online Kahn's algorithm — entities are started eagerly as their in-degree reaches 0, without waiting for unrelated entities at the same topological level.

**Edge semantics:** `From` depends on `To`. An entity is started once all entities it depends on have been reported ready.

**Callbacks:**

- `start(ctx, e)`: Called when entity `e` should be started (all dependencies are ready). Multiple entities may be started concurrently if they have no dependency relationship.
- `waitReady(ctx, e)`: Called after `start(ctx, e)`. Blocks until entity `e` is ready. When it returns, all dependents of `e` whose remaining in-degree drops to 0 are immediately started.

**Cancellation:** The executor creates a derived context with `context.WithCancel`. When any callback returns an error, the context is cancelled. All pending and in-flight goroutines observe the cancellation via `ctx.Done()` and abort. The first error is returned.

**Cycle handling:** Entities involved in dependency cycles cannot be reached (their in-degree never reaches 0). After all reachable entities are processed, cyclic entities are started in arbitrary order with a warning log.

**Concurrency model:** The executor runs `start` + `waitReady` pairs concurrently for independent entities using goroutines. A central coordinator tracks in-degrees and dispatches newly unblocked entities.

```text
Example: D→A, E→A+B, F→C, G→D+F

t=0  start(A), start(B), start(C)         ← in-degree 0
t=1  A ready → in-degree(D)=0, in-degree(E)=1 → start(D)
t=2  C ready → in-degree(F)=0 → start(F)
t=3  B ready → in-degree(E)=0 → start(E)
t=4  D ready, F ready → in-degree(G)=0 → start(G)
t=5  E ready, G ready → done
```

**For scale-down / uninstall**, the caller reverses the edge direction:

```go
TopologicalExecute(ctx, l, entities, ReverseRefs(refs), scaleDown, waitScaledDown)
```text

`ReverseRefs` swaps `From` and `To` on each ref, so dependents are processed before their dependencies.

### ReverseRefs

```go
func ReverseRefs(refs []types.Ref) []types.Ref
```

Returns a copy of refs with `From` and `To` swapped on each entry. Used to reverse the dependency direction for scale-down and uninstall operations.

### BuildDependencyGraph

```go
type DependencyGraph struct {
    Adj      map[types.Id][]types.Id  // adjacency list: entity → entities that depend on it
    InDegree map[types.Id]int         // remaining dependency count per entity
    Entities map[types.Id]entity.Entity
}

func BuildDependencyGraph(entities entity.Entities, refs []types.Ref) (DependencyGraph, error)
```text

Pure function that builds the dependency graph from entities and refs. When `ref.Reverse` is true, the from/to IDs are swapped before adding the edge (the dependency direction is inverted). Used by `TopologicalExecute` internally, and exposed for unit testing.

### ResolveTransitiveWorkloadDeps

```go
func ResolveTransitiveWorkloadDeps(refs []types.Ref, workloadIds map[types.Id]bool) []types.Ref
```

**Source file:** `core/commands/topo_execute.go`

Traces dependency chains through non-workload intermediaries (Secrets, ConfigMaps, SopsSecrets, etc.) and creates synthetic `indirect` refs between workloads. Uses BFS starting from each workload, traversing only through non-workload nodes. When another workload is reached, a synthetic ref is added.

**Example:** `Deployment/dex → Secret → Secret ← SopsSecret → Deployment/sops-operator` produces a synthetic `Deployment/dex → (indirect) → Deployment/sops-operator` ref.

Called by `LogStartupOrder`, `ScaleUpWorkloads`, and `ScaleDownWorkloads` before `BuildDependencyGraph` to ensure transitive workload dependencies (via provider-based operator connections and non-workload chains) are visible in the dependency graph.

### PlanTopologicalOrder

```go
type PlanEntry struct {
    Name         string
    Dependencies []string // names of direct dependencies, empty if none
}

func PlanTopologicalOrder(graph DependencyGraph) []PlanEntry
```text

**Source file:** `core/commands/topo_execute.go`

Pure function that computes the topological order from a `DependencyGraph` and returns it as a flat list of `PlanEntry` values. Each entry contains the workload name and the names of its direct dependencies.

**Algorithm:** Iterative Kahn's algorithm — repeatedly collect all nodes with InDegree == 0 (one "level"), append them to the plan, then decrement in-degrees of their dependents. Nodes at the same level (InDegree == 0 simultaneously) are sorted alphabetically for deterministic output.

**Output format:** The returned plan is used by `ScaleUpWorkloads` to produce a human-readable log:

```text
scale-up order:
  1. workload-a (no dependencies)
  2. workload-b (no dependencies)
  3. workload-c (after: workload-a)
  4. workload-d (after: workload-a, workload-b)
```

Entries with no dependencies show `(no dependencies)`. Entries with dependencies show `(after: dep1, dep2, ...)`.

**Cycle handling:** Entities involved in dependency cycles are appended at the end of the plan with their unresolved dependencies listed.

### Unit Tests (PlanTopologicalOrder)

Test scenarios for the `PlanTopologicalOrder` pure function:

1. **Linear chain**: A→B→C — plan order is [C, B, A], each showing its dependency
2. **Diamond**: D→B, D→C, B→A, C→A — plan starts with A (no deps), then B and C (after: A), then D (after: B, C)
3. **No refs** — all entries have no dependencies, sorted alphabetically
4. **Single entity** — one entry with no dependencies
5. **Empty graph** — empty plan returned
6. **Independent entities** — all at same level, sorted alphabetically, all show (no dependencies)
7. **Cycle**: A→B, B→A — both appended at end with unresolved dependencies
8. **Deterministic ordering** — nodes at the same level are always sorted alphabetically regardless of insertion order

### Removed: HydraApplyOrderProvider and HydraUninstallOrderProvider

`HydraApplyOrderProvider` and `HydraUninstallOrderProvider` (previously in `core/commands/order.go`) are removed entirely. All ordering now uses `TopologicalExecute` with `references.Refs()`.

### Unit Tests (BuildDependencyGraph)

Test scenarios for the `BuildDependencyGraph` pure function:

1. Linear chain: A→B→C — adj maps correctly, in-degrees: A=0, B=1, C=1
2. Diamond: D→B, D→C, B→A, C→A — in-degrees: A=0, B=1, C=1, D=2
3. No refs — all in-degrees are 0
4. Refs to external entities (not in entity set) — ignored
5. Self-references — ignored
6. Empty entity set — empty graph

### Unit Tests (TopologicalExecute)

Test scenarios using mock `start`/`waitReady` callbacks that record call order:

1. Linear chain: B depends on A → A started first, B started only after A is ready
2. Diamond: D→B, D→C, B→A, C→A → A started first; B and C started concurrently after A ready; D started after both B and C ready
3. Independent entities: A, B, C with no refs → all three started concurrently
4. Eager unblocking: D→A, E→A+B — when A ready, D starts immediately (does not wait for B)
5. Cycle: A→B, B→A, C independent → C processed normally, A and B logged as cyclic and started afterward
6. Empty → no callbacks called
7. Single entity → started immediately
8. start returns error → execution aborts, error propagated
9. waitReady returns error → execution aborts, error propagated
10. Concurrent entities: one fails → others are cancelled via context, no further starts

### Unit Tests (ReverseRefs)

1. Single ref {From: A, To: B} → {From: B, To: A}
2. Empty refs → empty
3. Preserves other fields (RefType, Labels, etc.)
