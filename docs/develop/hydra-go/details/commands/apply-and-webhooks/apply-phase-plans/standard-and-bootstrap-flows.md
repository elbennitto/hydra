# Apply and Webhooks: Standard and Bootstrap Flows

This page covers the automatically numbered `hydra gitops apply` flow and how optional steps are gated. **`--bootstrap`** is a shorthand that enables the optional behaviors for the run unless the user overrides individual flags; it does **not** select a different phase list.

Back to [Apply Phase Plans](../apply-phase-plans.md).

## Data Flow (Automatically Numbered Apply)

The `ClusterApply` function always builds the same apply phase plan. Phase numbers are assigned automatically by the phase builder in the order the phases are added. **Integrated backup restore** runs only when `--backup-restore` is set and `--skip-backup-restore` is not; otherwise that phase reports `skipped`. **Down-scaled apply** uses `ZeroWorkloads` when `--down-scaled` is set; when it is not set, main resources are still applied at **template scale** in the same phase. **Scale-up**, **orphan scale-down**, and **orphan deletion** phases report `skipped` when their flags are off. **SOPS decoding**, **bootstrap-tagged clones**, and **bootstrap-guard** enforcement run during preparation only when the corresponding flags are set (or implied by `--bootstrap`). When `--no-cluster` is set, the flow renders entities, resolves refs, splits entities, then calls `LogStartupOrder` to display the startup order (scale-up order) and returns immediately — no cluster connection, restore phase, or apply operations are executed. All phase log messages use the format `"phase {current}/{total}: {description}"`. Phases with no work log `"(skipped)"` instead of being silently omitted. See [Phase Logging](phase-logging-and-diffing.md#phase-logging) for details.

```text
hydra gitops apply prod.* [--bootstrap] [--no-cluster]
  │
  ▼
1. RenderCluster(cluster, appIds, ...)
   → renderedEntities
   Scope for custom resources may incorporate CRD manifests from the full cluster
   render (all apps in the cluster), while the entity set above remains limited to
   the selected appIds. Each CRD required to admit resources in renderedEntities
   must still be live on the target cluster or included in this run’s selected CRD
   apply set — see [Cluster apply: CRD sources for scope vs apply eligibility](../../rendering-and-listing.md#cluster-apply-crd-sources-for-scope-vs-apply-eligibility).
  │
  ▼
2. [if --sops-decode] ConvertSopsSecretsToSecrets(renderedEntities, ...)
   → entities (with plain Secrets added)
  │
  ▼
3. references.Refs(l, entities, KeyTemplateEntity)
   → refs (dependency graph)
  │
  ▼
4. splitCRDs(entities) → crdEntities, nonCrdEntities
   splitNamespaces(nonCrdEntities) → namespaceEntities, nonCrdEntities
  │
  ├── [if --no-cluster]
  │     a. LogStartupOrder(l, nonCrdEntities, refs, KeyTemplateEntity, customWorkloads)
  │        Computes and displays the startup order (scale-up order) without
  │        any cluster connection. Uses CollectScaleTargets → filterWorkloadEntities
  │        → BuildDependencyGraph → PlanTopologicalOrder → logScaleUpPlan.
  │     b. Log summary (entities, CRDs, namespaces, resources, refs counts)
  │     c. Return — no cluster operations executed
  │
  ▼
Phase 1/N: applying CRDs
  │  a. Filter entities where GVK == "apiextensions.k8s.io/v1/CustomResourceDefinition"
  │  b. If no CRDs: log "phase 1/{total}: applying CRDs (skipped)"
  │  c. Apply CRDs to cluster (always server-side apply)
  │  d. WaitForCRDsEstablished(): poll every 2s (configurable timeout, default 60s via --crd-timeout)
  │     - GET CRD, check .status.conditions[] for type=Established, status=True
  │     - If timeout: abort with ErrCrdEstablishTimeout
  │  e. If CRD apply fails, the operation aborts. Already-applied CRDs
  │     remain in the cluster. Re-running the command is safe (idempotent).
  │
  ▼
Phase 2/N: applying namespaces
  │  a. Filter entities where GVK == "v1/Namespace" from non-CRD entities
  │  b. If no namespaces: log "phase 2/{total}: applying namespaces (skipped)"
  │  c. Apply namespaces to cluster using regular kubectl apply (not server-side apply)
   │  d. This remains the authoritative namespace reconciliation step for the
   │     rendered manifests, even though selected namespaces may already have
   │     been created earlier for backup restore preparation
  │
  ▼
Phase 3/N: restoring backup secrets
  │  a. Discover backup inputs only from manifests rendered for the selected apps
  │  b. Derive the namespaces used by the selected apps from the rendered entities
  │  c. Create only those namespaces early when they are needed as restore targets
  │  d. Call BackupRestore() with:
  │     - selected app IDs
  │     - optional secret filters only
  │  e. If --skip-backup-restore: log "(skipped)" and continue
  │  f. If any result is `would-overwrite`, abort unless --force-backup-restore
  │
  ▼
Phase 4/N: disabling non-ready webhook configurations
  │  a. splitWebhooks(nonCrdEntities) → webhookEntities, nonWebhookEntities
  │  b. FilterWebhooksToDisable(webhookEntities, nonWebhookEntities, allEntities, key, isProviderReady)
  │     → toDisable, toKeep
  │     Uses ResolveWebhookProviders to find backing workloads, then checks
  │     cluster readiness (readyReplicas != 0) via IsWebhookProviderReady.
  │  c. If toDisable is non-empty:
  │     DisableWebhookConfigs(ctx, l, dynamicClient, toDisable, key, dryRun)
  │     Patches failurePolicy to "Ignore" for non-ready webhooks
  │  d. If no webhooks to disable: log "phase 4/{total}: disabling non-ready webhook configurations (skipped)"
  │
  ▼
Phase 5/N: applying resources at scale zero (excluding webhooks)
  │  a. Exclude CRDs (already applied in Phase 1), Namespaces (already applied in Phase 2),
  │     and webhook configurations (handled in Phase 3 and Phase 6)
  │  b. If no remaining entities: log "phase 5/{total}: applying resources at scale zero (excluding webhooks) (skipped)"
  │  c. ListClusterAll(cluster, KeyClusterEntity) → clusterEntities
  │     Fetches the current cluster state to identify new vs existing entities.
  │     This cluster state is also reused for orphan detection later.
  │  d. Merge(nonWebhookEntities, clusterEntities) + Compare(KeyTemplateEntity, KeyClusterEntity)
  │     → LeftOnly (new), Both (existing)
  │  e. Apply new entities (LeftOnly):
  │     - ZeroWorkloads(): set spec.replicas=0 / nodeSelector for DaemonSets
  │     - applyEntitiesBySSA() to apply new entities
  │  f. Diff existing entities (Both):
  │     - Log "diffing {count} cluster entities"
  │     - ServerSideDryRunApplyEntities(cluster, existingEntities, KeyTemplateEntity, KeyDryRunEntity)
  │       Stores dry-run result under KeyDryRunEntity (entity now has 3 keys: template, cluster, dryrun)
  │       NOTE: If the dry-run fails for an entity (e.g. unknown CRD, permission error),
  │       that entity falls back to its original template data — KeyDryRunEntity is NOT set.
  │     - findChangedEntities(): For each entity:
  │       * Entities without KeyDryRunEntity (SSA dry-run failure) are conservatively
  │         treated as changed — when in doubt, apply
  │       * For workloads (Deployment/ReplicaSet/StatefulSet): zero spec.replicas in both
  │         cluster and dryrun copies before comparison (exception: live replicas == 0 and
  │         dry-run desired > 0 → treat as changed)
  │       * Convert both to YAML via PrintObject(KeepServerFieldsNo)
  │       * If YAML differs → entity needs re-apply
  │     - ZeroWorkloads() on changed entities
  │     - applyEntitiesBySSA() to apply only changed entities
  │
  ▼
Phase 6/N: scaling up workloads
  │  ScaleUpWorkloads displays a plan before executing:
  │
  │  a. Resolve transitive workload refs and preserve tags such as `optional`
  │  b. Build and log the required startup plan
  │  c. Execute the required workload graph
  │  d. Build and log the optional startup plan for workloads that are only linked
  │     through optional refs
  │  e. Execute the optional workload graph after the required workloads
  │  f. Log plan via l.InfoLog:
  │       scale-up order:
  │         1. workload-a (no dependencies)
  │         2. workload-b (no dependencies)
  │         3. workload-c (after: workload-a)
  │         4. workload-d (after: workload-a, workload-b)
  │  g. TopologicalExecute with dynamic eager scheduling:
  │
  │  TopologicalExecute(ctx, l, workloadEntities, refs,
  │    start = func(ctx, e) {
  │      Deployment/StatefulSet/ReplicaSet:
  │        patch spec.replicas to rendered template value
  │      DaemonSet:
  │        replace spec.template.spec.nodeSelector with the rendered template value
  │        remove `hydra-gitops.org/hydra-disabled` when it is not present in the template
  │        if the template has no nodeSelector, remove the field entirely
  │    },
  │    waitReady = func(ctx, e) {
  │      Poll every 2s (configurable timeout, default 10m via --scale-timeout):
  │        Deployment/StatefulSet/ReplicaSet:
  │          status.readyReplicas == spec.replicas
  │        DaemonSet:
  │          status.desiredNumberScheduled == status.numberReady
  │      If timeout: return ErrScaleUpTimeout
  │    },
  │  )
  │
  │  Entities with no dependencies are started concurrently.
  │  When a workload becomes ready, dependents whose last remaining
  │  dependency was that workload are started immediately — no waiting
  │  for unrelated workloads at the same topological level.
  │
  ▼
Phase 7/N: applying webhook configurations
  │  a. If no webhook entities: log "phase 7/{total}: applying webhook configurations (skipped)"
  │  b. applyEntitiesBySSA(l, webhookEntities, key, dryRun)
  │     Restores the correct failurePolicy for webhooks that were disabled in Phase 3
  │     and applies any new or changed webhook configurations.
  │     Safe because all workloads (including webhook-backing services) are running
  │     after Phase 5.
  │
  ▼
Orphan Detection
  │  a. Uses clusterEntities already fetched before the main apply phase
  │  b. findOrphans(cluster, clusterEntities, renderedEntities, appIds) → orphans
  │  c. Orphans are deleted in the final phase(s) below; if there are no orphans,
  │     the delete phase logs `(skipped)`.
  │
  ▼
Phase 8/N: scaling down orphaned workloads (only when `--orphan-scale-down`)
  │  collectOrphanScaleDownTargets(orphans) → scale down replica workloads to 0
  │  If no targets: log "phase 8/{total}: scaling down orphaned workloads (skipped)"
  │  When this phase is omitted (flag off), phase numbers shift so the delete phase is 8/N.
  │
  ▼
Phase 9/N: deleting orphaned resources (always; or 8/N when orphan scale-down is off)
  │  phaseDeleteOrphans: deletes **all** entities in `orphans` (webhooks, workloads,
  │  and other tracked types) in dependency-aware order; foreground delete with
  │  finalizer removal where needed.
  │  If no orphans: log "phase {n}/{total}: deleting orphaned resources (skipped)"
  │
  ▼
Complete
```text

## Data Flow (Automatically Numbered Bootstrap Apply)

When `--bootstrap` is active, the apply flow uses the same automatically numbered phase structure as the standard flow, with additional bootstrap-specific preparation steps (SOPS secret decryption, AppProject sync adjustment). **CRD scope and apply eligibility** follow the same rules as standard apply: scope for custom resources may use CRD manifests from the full cluster render, while each CRD required to admit the selected manifests must be live on the cluster or included in this run’s selected CRD apply set — see [Cluster apply: CRD sources for scope vs apply eligibility](../../rendering-and-listing.md#cluster-apply-crd-sources-for-scope-vs-apply-eligibility). Bootstrap does not relax or branch that behavior. Like standard apply, bootstrap also includes the scoped backup-restore phase in the phase plan: backup inputs are discovered from the selected app manifests, selected app namespaces may be created early, and then only that selected-app backup set is restored within the selected-app namespace scope unless `--skip-backup-restore` skips the phase. Bootstrap adds one more rule on top of that restore phase: ordinary selected-app `SopsSecret` resources still follow the normal bootstrap conversion/apply path, but backup resources classified as out-of-scope by the restore phase are removed from the later bootstrap apply set. The flow still addresses a chicken-and-egg problem: webhook admission controllers may block resource creation when their backing services are not yet running. The webhook-disable phase patches `failurePolicy` to `"Ignore"`, the scale-up phase starts required workloads before optional ones, and the webhook-apply phase restores webhook configurations via SSA after their backing services are ready.

All phase log messages use the format `"phase {current}/{total}: {description}"`. Phases with no work log `"(skipped)"`. See [Phase Logging](phase-logging-and-diffing.md#phase-logging) for details.

```text
hydra gitops apply my-cluster.* --bootstrap
  │
  ▼
1. RenderCluster(cluster, appIds, ...)
   → renderedEntities
   Same CRD scope / eligibility rules as the standard flow: scope may incorporate
   CRD manifests from the full cluster render (all apps in the cluster), while
   renderedEntities remain limited to the selected appIds; each CRD required to
   admit those entities must be live on the target cluster or included in this
   run’s selected CRD apply set — see
   [Cluster apply: CRD sources for scope vs apply eligibility](../../rendering-and-listing.md#cluster-apply-crd-sources-for-scope-vs-apply-eligibility).
  │
  ▼
2. ConvertSopsSecretsToSecrets(renderedEntities, ...)
   → entities (with plain Secrets added)
  │
  ▼
3. references.Refs(l, entities, KeyTemplateEntity)
   → refs (dependency graph)
  │
  ▼
Phase 1/N: applying CRDs
  │  a. Filter CRDs, apply server-side, WaitForCRDsEstablished (configurable timeout via --crd-timeout, default 60s)
  │  b. If no CRDs: log "phase 1/{total}: applying CRDs (skipped)"
  │
  ▼
Phase 2/N: applying namespaces
  │  a. Filter namespaces, apply
  │  b. If no namespaces: log "phase 2/{total}: applying namespaces (skipped)"
  │  c. This is still the authoritative namespace reconciliation step after the
  │     early restore-focused namespace preparation
  │
  ▼
Phase 3/N: restoring backup secrets
  │  a. Discover backup inputs only from manifests rendered for the selected apps
  │  b. Derive selected app namespaces from the rendered entities
  │  c. Create only those namespaces early when they are needed as restore targets
  │  d. Call BackupRestore() with the selected app IDs and optional secret filters
  │  e. If --skip-backup-restore: log "(skipped)" and continue
  │  f. Bootstrap-only follow-up: keep ordinary selected-app `SopsSecret`
  │     resources in the later normal apply set, but remove backup resources
  │     that are not part of the selected backup manifest set so they are not applied
  │     later as regular resources.
  │
  ▼
Phase 4/N: disabling non-ready webhook configurations
  │  a. splitWebhooks(nonCrdEntities) → webhookEntities, nonWebhookEntities
  │     (nonCrdEntities already excludes CRDs and Namespaces from prior phases)
  │  b. FilterWebhooksToDisable(webhookEntities, nonWebhookEntities, allEntities, key, isProviderReady)
  │     → toDisable, toKeep
  │  c. If toDisable is non-empty:
  │     DisableWebhookConfigs(ctx, l, dynamicClient, toDisable, key, dryRun)
  │     Patches failurePolicy to "Ignore" for non-ready webhooks
  │  d. If no webhooks to disable: log "phase 4/{total}: disabling non-ready webhook configurations (skipped)"
  │
  ▼
Phase 5/N: applying resources at scale zero (excluding webhooks) [MODIFIED FOR BOOTSTRAP]
  │  a. Use nonWebhookEntities (webhooks excluded) instead of all nonCrdEntities.
  │     During bootstrap this set still contains ordinary selected-app
  │     `SopsSecret` resources, but excludes backup resources that were already
  │     classified as out-of-scope by the scoped restore phase.
  │  b. If no remaining entities: log "phase 5/{total}: applying resources at scale zero (excluding webhooks) (skipped)"
  │  c. ListClusterAll(cluster, KeyClusterEntity) → clusterEntities
  │     Fetches the current cluster state to identify new vs existing entities.
  │     This cluster state is also reused for orphan detection later.
  │  d. Merge(nonWebhookEntities, clusterEntities) + Compare(KeyTemplateEntity, KeyClusterEntity)
  │     → LeftOnly (new), Both (existing)
  │  e. Apply new entities (LeftOnly):
  │     - ZeroWorkloads(): set spec.replicas=0 / nodeSelector for DaemonSets
  │     - ApplyClusterApplySyncWindowToEntities() (per `--sync`) → adjusted AppProject entities
  │     - applyEntitiesBySSA() to apply new entities
  │  f. Diff existing entities (Both):
  │     - Log "diffing {count} cluster entities"
  │     - ServerSideDryRunApplyEntities(cluster, existingEntities, KeyTemplateEntity, KeyDryRunEntity)
  │       Stores dry-run result under KeyDryRunEntity (entity now has 3 keys: template, cluster, dryrun)
  │       NOTE: If the dry-run fails for an entity (e.g. unknown CRD, permission error),
  │       that entity falls back to its original template data — KeyDryRunEntity is NOT set.
  │     - findChangedEntities(): For each entity:
  │       * Entities without KeyDryRunEntity (SSA dry-run failure) are conservatively
  │         treated as changed — when in doubt, apply
  │       * For workloads (Deployment/ReplicaSet/StatefulSet): zero spec.replicas in both
  │         cluster and dryrun copies before comparison (exception: live replicas == 0 and
  │         dry-run desired > 0 → treat as changed)
  │       * Convert both to YAML via PrintObject(KeepServerFieldsNo)
  │       * If YAML differs → entity needs re-apply
  │     - ZeroWorkloads() on changed entities
  │     - ApplyClusterApplySyncWindowToEntities() on changed entities (existing AppProjects: policy may copy live sync configuration)
  │     - applyEntitiesBySSA() to apply only changed entities
  │
  ▼
Phase 6/N: scaling up workloads [MODIFIED FOR BOOTSTRAP]
  │  ScaleUpWorkloads starts required workloads first and delays optional-only
  │  workloads until the end of the scale-up phase:
  │
  │  a. Resolve transitive workload refs and preserve tags such as `optional`
  │  b. Build + execute the required workload plan
  │  c. Build + execute the optional-only workload plan
  │  d. Log plan via l.InfoLog:
  │       scale-up order:
  │         1. workload-a (no dependencies)
  │         2. workload-b (no dependencies)
  │         3. workload-c (after: workload-a)
  │         4. workload-d (after: workload-a, workload-b)
  │  e. TopologicalExecute(ctx, l, workloadEntities, refs,
  │       start = func(ctx, e) {
  │         Deployment/StatefulSet/ReplicaSet:
  │           patch spec.replicas to rendered template value
  │         DaemonSet:
  │           replace spec.template.spec.nodeSelector with the rendered template value
  │           remove `hydra-gitops.org/hydra-disabled` when it is not present in the template
  │           if the template has no nodeSelector, remove the field entirely
  │       },
  │       waitReady = func(ctx, e) { poll readiness },
  │     )
  │
  │  No special webhook provider handling — all workloads (including
  │  webhook providers like cert-manager-webhook) are scaled up together
  │  in dependency order. Webhook providers will be ready before their
  │  dependents because of the topological ordering.
  │
  ▼
Phase 7/N: applying webhook configurations [BOOTSTRAP ONLY]
  │  a. If no webhook entities: log "phase 7/{total}: applying webhook configurations (skipped)"
  │  b. applyEntitiesBySSA(l, webhookEntities, key, dryRun)
  │     Now safe because ALL workloads (including webhook-backing services)
  │     are running after Phase 5
  │
  ▼
Orphan Detection
  │  a. Uses clusterEntities already fetched before the main apply phase
  │  b. findOrphans(cluster, clusterEntities, renderedEntities, appIds) → orphans
  │
  ▼
Phase 8/N: scaling down orphaned workloads (only when `--orphan-scale-down`)
  │  If no targets: log "phase 8/{total}: scaling down orphaned workloads (skipped)"
  │  When omitted, the delete phase is numbered 8/N instead of 9/N.
  │
  ▼
Phase 9/N: deleting orphaned resources (always; or 8/N without orphan scale-down)
  │  Deletes all `orphans` in one phase (including webhook configurations).
  │  Foreground deletion with finalizer removal where needed.
  │  If no orphans: log "phase {n}/{total}: deleting orphaned resources (skipped)"
  │
  ▼
Log bootstrap success message:
  "Bootstrap complete. To hand over control to ArgoCD, run:
    hydra argocd sync auto <appIds...>"
```

### Key Design Decisions (Apply Phase Plan)

1. **Unified scale-up with plan display**: All workloads (including webhook providers) are scaled up together in dependency order via `ScaleUpWorkloads`, which displays a topological plan before execution. This replaces the previous approach of separately resolving and sequentially scaling webhook providers. The topological ordering naturally ensures that webhook provider workloads are ready before any workloads that depend on them.

2. **Webhooks applied after scale-up**: Webhook configurations are applied only after workloads have been scaled up. This is safe because the backing services (e.g., cert-manager-webhook) are ready before the webhook apply phase runs.

3. **Disabling non-ready webhooks**: Instead of deleting webhook configurations, the webhook-disable phase patches `failurePolicy` to `"Ignore"` on webhooks whose backing workloads are not yet ready. This is less invasive — the configuration remains in the cluster and the later webhook apply phase restores the correct `failurePolicy` via SSA. Only webhooks whose rules match the applied resources AND whose providers are not ready are disabled. Webhooks that don't intercept applied resources or whose providers are already running are left untouched.

4. **Backup resource ownership during bootstrap**: In bootstrap mode only, ordinary selected-app `SopsSecret` CRs still belong to the later normal apply phases. The stricter rule applies only to backup resources that the scoped restore phase classified as outside the selected restore scope: those remain `skipped` and are not applied later as regular resources. This bootstrap-only rule documents the current fix scope and does not yet change standard apply semantics.

5. **Standard vs bootstrap mode**: Both standard and bootstrap apply build a phase plan from the same shared phase types. The concrete phase numbers come from build order, not from hard-coded literals in `ClusterApply`. Bootstrap swaps in bootstrap-specific phase implementations where needed, but uses the same runner and status model.

6. **ResolveWebhookProviders used by FilterWebhooksToDisable**: The `ResolveWebhookProviders` function in `webhook.go` is called by `FilterWebhooksToDisable` in the webhook-disable phase to resolve backing workloads and check their readiness. It is no longer called directly for individual scale-up — the unified topological scale-up handles all workloads.

7. **Orphan cleanup phases**: The three orphan cleanup phases are still delegated to `DeleteResources`, but their phase offsets are derived from the apply phase plan instead of being hard-coded to fixed apply-phase numbers.
