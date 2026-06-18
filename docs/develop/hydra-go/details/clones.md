# Resource cloning (`global.hydra.clones`)

## Purpose

`global.hydra.clones` defines **manifest copies** of rendered resources into additional namespaces. It is separate from **`global.hydra.refs`**, which models dependency edges and virtual targets for review.

Configuration may come from **Helm values** (`global.hydra.clones`) and from **Hydra ConfigMaps** (`data.hydra`, same shape), merged with the same pattern as ref-parsers.

## Rule fields

| Field | Meaning |
| ----- | ------- |
| `predicate` | CEL predicate on template entities (same env as ref-parsers). |
| `targets.cel` | CEL expression returning `list(string)` of target namespaces. |
| `tag` | Optional. Empty = always active; `bootstrap` = only when `--bootstrap` is active; any other value is treated as “always active” (warning logged). |
| `exclude` | Optional list of namespace patterns: exact match, or `prefix*` suffix for prefix match. |

## Semantics

- **Priority:** rendered entities win over clones (`real > clone`). Duplicate target ids from two clone rules are a **hard error**.
- `SopsSecret` cannot be cloned (namespace change would invalidate the SOPS MAC). Use the derived `v1/Secret` after `ConvertSopsSecretsToSecrets` as the source.
- **Namespace ownership:** each target namespace must resolve to a single owner `appId` (see `BuildNamespaceOwnerMap` in `core/commands/clones.go`). Resolution uses three strategies in priority order: (1) **declared ownership** via `global.hydra.ownerNamespaces` in `HydraValues` — each app may declare a list of namespaces it owns; declarations are collected globally and must be unique (no two apps may claim the same namespace); (2) an explicit `v1/Namespace` object with exactly one owning app is authoritative; (3) if neither (1) nor (2) resolves the namespace, `GroupNamespacesByApp` checks whether exactly one app deploys resources into the namespace. When materializing clones, strategy (3) uses a **full-cluster template render** (`RenderClusterAllApps`) as the namespace index, so “exactly one app” is evaluated against **all** apps on the cluster, not only the apps in the current command selection. Predicate evaluation and source matching still use the **selected** render. When multiple apps share a namespace without a declared owner or explicit `v1/Namespace` object, strategy (3) fails with an **ambiguous app owners** error. The recommended fix is to add `global.hydra.ownerNamespaces` to the primary app (see [Declared namespace ownership](#declared-namespace-ownership-globalhydraownernamespaces) below). Creating an explicit `v1/Namespace` manifest remains an alternative when the app's ArgoCD AppProject permits namespace creation.
- **Implicit sole-namespace ownership (display):** for each app, namespaces where **only that app** deploys resources (same index as strategy (3), excluding Kubernetes system namespaces) can be merged into the printed `ownerNamespaces` list for [`hydra local config`](../../../manual/cli/local/hydra-config.md) — see `InferredOwnerNamespacesForApp` / `MergeInferredOwnerNamespacesIntoHydraMap` in `core/commands/namespace.go`. This does **not** change on-disk Helm values; it surfaces the same sole-app relationship you would otherwise rely on in strategy (3).
- **Kubernetes system namespaces:** `kube-system`, `kube-public`, `kube-node-lease`, and **any namespace whose name has the prefix `kube-`**, are **not** treated as owned by a Hydra application for clone target resolution. They are **excluded from all three strategies** above (including entries in `global.hydra.ownerNamespaces` and `v1/Namespace` objects). That way, several apps deploying into `kube-system` (or other `kube-*` namespaces) does **not** produce **ambiguous app owners**. If a clone rule’s target list includes only such namespaces, no owner can be resolved for them; prefer `exclude` patterns (for example `kube*`) on the rule or avoid targeting system namespaces. Namespaces that are shared by multiple apps but are **not** system namespaces still need a single declared owner or an explicit `v1/Namespace` when strategy (3) would otherwise be ambiguous.
- **Cluster review:** live cluster entities override template and clone predictions for the same id (`cluster-live > local/clone`). Clone targets not yet in the cluster are merged into the target set for review.
- **Uninstall:** `ExpandClonesForUninstall` adds stub entities so clone targets participate in uninstall/orphan logic; use `--bootstrap` on uninstall if bootstrap-tagged clone rules were applied with bootstrap.

## CLI

| Command | Behavior |
| ------- | -------- |
| `hydra gitops apply` | Materializes clones after SOPS conversion when `bootstrap` applies; tag rules accordingly. |
| `hydra local template --bootstrap` | Appends clone YAML after per-app output; errors if `--bootstrap` is unnecessary (no bootstrap-tagged rules or none materialized). |
| `hydra local review --bootstrap` / `hydra gitops review app` or `cluster` with `--bootstrap` | Includes bootstrap-tagged clone materialization in the target model. |

## Example (mirror image pull secret)

```yaml
global:
  hydra:
    clones:
      image-pull-secret-mirror:
        desc: "Mirror image-pull-secret to app namespaces for bootstrap"
        tag: bootstrap
        predicate: 'id == "v1/Secret/sops-secrets-operator/image-pull-secret"'
        targets:
          cel: 'managedNamespaces()'
        exclude:
          - sops-secrets-operator
          - kube*
```

Use **`managedNamespaces()`** (or filter **`templateEntities(...)`** / **`entities(...)`**) in CEL expressions; see [details/cel.md — Entity inventory](cel.md#entity-inventory-and-managed-namespaces-uninstall--backup--refs--clones).

## Declared namespace ownership (`global.hydra.ownerNamespaces`)

Apps can declare which namespaces they own via `global.hydra.ownerNamespaces`, a list of namespace strings in `HydraValues`. Declarations from all apps are collected globally; duplicate namespace claims across different apps produce a hard error.

Entries for [Kubernetes system namespaces](#semantics) (`kube-*` and the fixed set above) are **ignored** for ownership resolution; they cannot be used to assign an app owner to those namespaces for clones.

This is the **highest priority** strategy in `BuildNamespaceOwnerMap` — it takes precedence over explicit `v1/Namespace` objects and over the `GroupNamespacesByApp` fallback. Use it when multiple apps share a namespace and the owning app cannot create an explicit `v1/Namespace` resource (for example because the ArgoCD AppProject restricts namespace creation).

### YAML format

```yaml
global:
  hydra:
    ownerNamespaces:
      - "my-namespace"
      - "my-other-namespace"
```

### Example: operator with multiple apps in one namespace

An operator chart (for example `operator-clickhouse`) may deploy three apps into the same namespace. Only the primary app declares ownership:

```yaml
# primary app values
global:
  hydra:
    ownerNamespaces:
      - "clickhouse"
```

The remaining apps in the namespace do **not** declare `ownerNamespaces`; the primary app's declaration resolves ownership for clone targets.

### Example: cert-manager with shared namespace

When multiple cert-manager-related apps share the `cert-manager` namespace, the main app declares ownership:

```yaml
# cert-manager app values
global:
  hydra:
    ownerNamespaces:
      - "cert-manager"
```

### Uniqueness constraint

`BuildNamespaceOwnerMap` collects `ownerNamespaces` from every app's `HydraValues` and validates that each namespace appears at most once across all apps. If two apps both claim the same namespace, the build fails with an error listing the conflicting apps and namespace.

## Source files

- `core/types/hydra.go` — `HydraCloneRule`, `HydraCloneTargets`
- `core/hydra/hydra_values.go` — `HydraAppCloneRules`
- `core/hydra/configmap_clone_rules.go` — `CloneRulesFromHydraConfigMaps`
- `core/hydra/hydra_values.go` — `HydraAppNamespaceOwners`
- `core/commands/clones.go` — materialization, uninstall expansion, template validation
- `core/commands/namespace.go` — `isKubernetesSystemNamespace`, `InferredOwnerNamespacesForApp`, `MergeInferredOwnerNamespacesIntoHydraMap` (config output), `ExclusiveNamespaces`, `WithoutSystemNamespaces`
- `core/commands/render.go` — `RenderClusterAllApps` (full-cluster namespace index for clone ownership strategy (3))
