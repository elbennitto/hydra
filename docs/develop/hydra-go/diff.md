# Diff Architecture

Hydra Go has two diff flows that produce unified diff output:

- `hydra gitops diff` compares rendered Helm templates against the live Kubernetes cluster state and supports two diff strategies controlled via `--diff-mode`.
- `hydra gitops backup diff` compares rendered backup secrets against the live cluster secrets after secret-specific normalization.

`hydra gitops apply` does **not** emit a unified diff. It applies desired state to the cluster. Before mutating phases, it classifies resources into **new**, **update**, **replace** (delete-before-apply), **unchanged**, and **delete** (orphans) using a single server-side apply dry-run pass over existing objects, then runs phased applies. Phase log lines use stable phase ids in parentheses, e.g. `phase 2/5: applying namespaces (apply-namespaces)`. Optional phases are omitted from the plan when their flags are unset; when a phase is included but has no work for the current inputs, it is reported as **skipped**.

## Key Concepts

- **Server mode** (default) — Templates go through server-side apply dry-run; API server fills in defaults before comparison, producing clean diffs without false positives
- **Raw mode** — Templates compared directly against cluster state; faster but shows server-default differences
- **Server-Side Apply Dry-Run** — Uses `ApplyPatchType` with `DryRun=server` and `FieldManager="hydra"` to get expected state
- **KeyDryRunEntity** — Stores dry-run results separately for apply flow change detection (`findChangedEntities`)
- **Orphan detection** — Filters by ArgoCD tracking ID and excludes controller-managed resources (ownerReferences)
- **CEL resource filters** — Optional `--include` / `--exclude` (same flag mechanics as `hydra gitops list` and `hydra gitops dump`): applied **after** merge, compare, and orphan filtering, **before** unified diff lines are emitted. Evaluation uses the same entity map as `hydra local find` (`gvk`, `kind`, `namespace`, `name`, `templateEntity`, `clusterEntity`, etc.); missing unstructured keys are null. Predicate evaluation uses `MissingKeysReject`, matching `hydra local find`.
- **YAML comparison** — Both sides stripped of server metadata via `PrintObject(KeepServerFieldsNo)`, compared with `go-difflib`. Optional `-A` / `-B` / `-C` (grep-style) control unified-diff context; see [details/diff.md](details/diff.md).
- **Diff ignore rules (`global.hydra.diff.ignore`)** — Optional CEL `predicate` per named rule plus a list of **`yq:`** expressions applied to each compared document (template vs cluster for `cluster diff`; cluster vs SSA dry-run for `cluster apply` change detection). Optional **`ignoreWhenMissingInCluster`** affects `cluster diff` only (short `diff ignored` line for template-only resources). Built-in rules include ignoring `spec.replicas` for core workload kinds. See [details/diff-ignore-rules-implementation.md](details/diff-ignore-rules-implementation.md).
- **Backup secret normalization** — `cluster backup diff` normalizes secrets before hashing and diffing: `stringData` is folded into `data`, managed annotations with the same prefixes used by backup creation are removed, and an empty `metadata.annotations` block is dropped completely

## Source Files

`cli/action/cluster_diff.go`, `cli/cmd/cluster_diff.go`, `core/commands/server_side_apply.go`, `core/commands/backup.go`

→ **Full details:** [details/diff.md](details/diff.md)
