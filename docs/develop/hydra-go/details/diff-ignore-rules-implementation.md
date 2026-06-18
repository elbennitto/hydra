# `global.hydra.diff.ignore` (CEL + yq)

Hydra applies **diff ignore rules** before string comparison for:

- `hydra gitops diff` — template YAML vs cluster YAML per resource
- `hydra gitops apply` — `findChangedEntities`: live cluster object vs server-side apply dry-run (`KeyDryRunEntity`)

Rules are merged from Helm `global.hydra` and Hydra ConfigMap `data.hydra` documents (same merge semantics as `refs` / `clones`).

## Rule shape

```yaml
global:
  hydra:
    diff:
      ignore:
        myRuleName:
          predicate: <CEL boolean expression on the entity>
          patches:
            - yq: <mikefarah/yq expression on document root>
            - yq: ...
        optionalLeftOnly:
          predicate: <CEL boolean expression on the entity>
          ignoreWhenMissingInCluster: true
```

- **`predicate`** — Selects resources using the same CEL environment as other Hydra rules (`gvk`, `kind`, `namespace`, `name`, `id`, `templateEntity`, `clusterEntity`, `dryRunEntity`, …). Missing keys reject (same as `hydra gitops diff --include`).
- **`patches`** — Each entry must have **`yq:`** with a [yq v4](https://github.com/mikefarah/yq) expression evaluated against the **single-resource YAML document** (root `.`). Expressions run in order for each matching rule. Patches may be omitted when **`ignoreWhenMissingInCluster`** is `true` (see below); otherwise at least one non-empty **`yq:`** is required.
- **`ignoreWhenMissingInCluster`** (optional, `hydra gitops diff` only) — When `true`, if a resource matches the predicate and appears **only in rendered templates** (not yet in the cluster), Hydra prints a single line `diff ignored (resource absent in cluster): <id>` instead of a full unified diff. If the same resource **exists** in the cluster, the usual comparison runs (including any **`patches`** on this rule).

CEL must **not** be used to mutate objects; only **`yq:`** performs transformations.

## Built-in rule

Hydra always prepends a built-in entry `_hydra_builtin_workload_replicas`:

- **Predicate:** `gvk == "apps/v1/Deployment" || gvk == "apps/v1/StatefulSet" || gvk == "apps/v1/ReplicaSet"`
- **Patch:** `yq: del(.spec.replicas)`

This replaces the previous hard-coded `zeroReplicas` logic in `findChangedEntities` and aligns `cluster diff` with the same normalization.

## Scale-to-zero restore (apply only)

If live `spec.replicas` is `0` and the dry-run desired count is `> 0`, the resource is still classified as **changed** so apply can restore template scale. Replica counts are read **before** yq normalization.

## Implementation references

- Types: `core/types/hydra.go` (`HydraDiffSection`, `HydraDiffIgnoreRule`, `HydraDiffYqPatch`)
- Merge: `core/hydra/diff_ignore_merge.go` (`HydraDiffIgnoreRuleEntries`, `BuiltinDiffIgnoreRuleEntries`)
- Pipeline: `core/commands/diff_ignore.go` (`DiffIgnorePipeline`, `ApplyToUnstructured`)
- CLI: `cli/action/cluster_diff.go`, `cli/action/cluster_apply.go` (`findChangedEntities`)
