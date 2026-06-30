# hydra local review

Review rendered Hydra configurations locally without talking to a live cluster.

## Synopsis

```bash
hydra local review <appId> [appId...] [flags]
```

## Description

The `local review` command is the local, read-only review command under `hydra local`. It never talks to a live cluster.

Hydra renders the **apps you select** and uses those manifests only as **reference sources**. **Targets** for the review are resolved against the rendered templates of **all** effectively enabled applications on the **same** cluster as your selected `appId` arguments (the same single-cluster rule as other review commands). That lets offline review accept cross-app references when the peer app is enabled in the Hydra context, not only when you listed it on the command line.

After reference validation, Hydra runs a **ref ownership** check: ref groups that include the **`runtime`** tag together with **`uninstall`** / **`uninstall-safe`** / **`uninstall-force`** / **`backup`** are **skipped** here. For each remaining case, if a resource id appears in a standalone per-app template render, every other app’s matching ref-parser predicates must **not** claim that object unless they match only the template owner. If another app’s predicates also claim the resource, the review emits a finding (`ref ownership conflicts with standalone template render`). Fix ref parsers so only the app that renders the manifest owns it, or add **`runtime`** to broad uninstall rules that should apply only to **cluster-only** resources (see [`hydra gitops review`](../cluster/review.md)). **`hydra gitops uninstall`** and **`hydra gitops review`** (live pass) include **`runtime`** predicates **only** for resources whose id is **not** in any standalone template render.

The check covers:

- **Kubernetes bootstrap targets** — Hydra treats a fixed set of **upstream** default objects (initial namespaces, `default` ServiceAccounts, `kube-root-ca.crt` ConfigMaps, the `kubernetes` Service in `default`, and RBAC bootstrap `ClusterRole` / `ClusterRoleBinding` names derived from Kubernetes `bootstrappolicy` test data) as **present in the local target set** even though enabled-app templates usually do not render them. That avoids spurious `missing target resource` findings when a manifest references a bootstrap `ClusterRole` such as `view` or `cluster-admin`. The set is **not** distribution-specific (no k3s/k3d extensions). Expected objects are filtered by the **Kubernetes minor version** from Hydra values (the same Kubernetes version source Helm uses); when that version cannot be parsed, Hydra assumes a recent minor so the full bootstrap list applies.
- **Per-namespace Kubernetes defaults** — For every namespace that appears in the **template target** set, Hydra also treats the cluster-style **kubernetes-defaults** bundle as present when it is not already in the templates: `ServiceAccount/default`, `ConfigMap/kube-root-ca.crt` (including a synthetic `ca.crt` data key for key-aware checks), and the `Namespace` object. Only **same-namespace** references from namespaced workloads to `default` or `kube-root-ca.crt` benefit; cross-namespace references still require an explicit manifest peer or another valid target.
- missing target resources such as `Secret` or `ConfigMap` reported as `missing target resource` when neither a matching object exists in the target set nor a **stabilized** chain of refs whose attributes include **`"origin:generated": job`** or **`"origin:generated": controller`** (declared in app ref-parsers) accounts for that resource—**including** when materialization requires more than one hop (evaluated to a fixpoint). **StatefulSet** `volumeClaimTemplates` contribute such edges to expected per-pod **`PersistentVolumeClaim`** names (`<claimTemplateName>-<statefulSetName>-<ordinal>`), honoring **`spec.ordinals.start`** and **`spec.replicas`** (with Kubernetes defaults). Live PVC confirmation via the API inventory applies only to [`hydra gitops review`](../cluster/review.md).
- missing explicitly referenced keys inside `Secret` and `ConfigMap`
- repeated findings grouped by identical target and message

References that Kubernetes marks as optional (`optional: true` on `secretKeyRef`, `configMapKeyRef`, `envFrom` sources, volume `configMap`/`secret`, or projected `configMap`/`secret`) are recorded with Hydra’s `optional:ref` tag but are **skipped** by this review (no findings for those edges). The Hydra UI graph shows that tag on edges.

Namespaced objects in the rendered manifests may omit `metadata.namespace` (common for Helm charts; the release namespace is implicit). For reference checking, Hydra treats those resources as belonging to the application's target namespace, so source and target identities in findings match that namespace and spurious `missing target resource` results are avoided when the peer object exists under the expected namespace in the target set.

Applications with `enabled: false` are not rendered by Hydra. They do not contribute **sources** or **template targets** in this command, so references involving only disabled apps are not modeled in local review.

When you need the same source manifests checked against **live** cluster objects instead of the full local template inventory, use [`hydra gitops review`](../cluster/review.md).

Hydra may still **collect and group** all findings in memory before anything is printed; what is progressive is only the **stdout** phase: once the final ordered list exists, each finding is written in order. **By default** that is a short **human-readable text** block (with optional color): findings are **grouped by message type** (for example unassigned cluster-only ref ownership uses one heading that includes the scope note, e.g. `ref ownership: cluster-only resource has no Hydra app assignment (would remain unassigned for hydra gitops uninstall in this namespace scope)`, with one **Target** line per resource and no redundant **Detail** line for that class), the **Sources** section is printed **only** when it is non-empty, and a per-target **Message** or **Detail** line appears when the full text adds detail beyond the shared type. Use **`--yaml`** to get the previous behavior: each finding as the next element of one YAML sequence, without marshaling the entire list as a single YAML value at the end (so machine parsers still see one array). You do not get partial findings while grouping is still running.

When findings exist, `hydra local review` exits with a non-zero status so it can be used in CI.

If the same template resource id would be produced by **more than one** app’s standalone render, the command fails with **`ErrUninstallDuplicateTemplateResource`** (same as uninstall) before emitting reference findings.

## hydra local review vs hydra gitops review

Use the local command when you want offline validation against **Hydra-rendered** templates:

- [`hydra local review`](../local/review.md) checks **sources** only from the apps you select; **targets** are every enabled app's templates on that cluster in the Hydra context. It also runs the **template vs ref-parser ownership** check described above.
- [`hydra gitops review`](../cluster/review.md) uses the same **sources**, but **targets** are **all** resources Hydra can see on the live cluster.

`hydra local review` is the right first step when you care about Git-rendered consistency across enabled apps. `hydra gitops review` is the follow-up when live cluster state (including objects Hydra does not template) must match the references.

## Arguments

| Argument | Description |
| -------- | ----------- |
| `appId` | One or more [App IDs](../README.md#app-ids) (supports [wildcards](../README.md#wildcards)) |

## Flags

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--exclude-app` | | Glob pattern to exclude applications from the **source** set (repeatable); does **not** remove other enabled apps from the **target** template inventory |
| `--helm-network-mode` | | [Helm network mode](../README.md#helm-network-mode): `online`, `local`, `offline`, or `error` |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--yaml` | | Emit each finding as YAML (default: human-readable text) |
| `--color` | `-c` | Force colored output (text or YAML) |
| `--no-color` | | Disable colored output even in a terminal |
| `--color-mode` | | Color mode: `auto` (default, TTY-detected), `always`, or `never` |
| `--bootstrap` | | Include `global.hydra.clones` rules tagged `bootstrap` in the target template set |
| `--parallel` | | Parallel workers for the ref-ownership **template vs ref-parser** pass (and for **live assignment** on [`hydra gitops review`](../cluster/review.md)). With an effective value greater than `1` and terminal progress, each such phase uses its own footer **sub-bar** plus one worker status line per worker. **`0`** (default) means [GOMAXPROCS](https://pkg.go.dev/runtime#GOMAXPROCS), capped at **64**. |

`--color`, `--no-color`, and `--color-mode` are mutually exclusive. With no color flag, Hydra uses the same automatic behavior as other commands (color only when standard output is a TTY).

## What Gets Checked

- **Sources:** template entities from the resolved `appId` set after `--exclude-app` (only these may appear in finding `sources`).
- **Targets:** template entities from **every** effectively enabled app on the same cluster as your selection; `--exclude-app` does **not** shrink this target inventory.
- **Ref ownership:** each template resource id from per-app standalone renders must not be matched by another app’s **`uninstall`** / **`uninstall-force`** / **`backup`** ref-parser predicates, **excluding** groups tagged **`runtime`** (see [Description](#description)).
- Namespaced rendered resources without `metadata.namespace` are aligned to the app's namespace before references are evaluated (same idea as full cluster render), so lookups use the namespace where the app actually deploys.
- A referenced `Secret` or `ConfigMap` is treated as present if it appears in the target set **or** ref-parsers describe a path to it using the **`generated`** attribute (`job` or `controller`), **including nested paths** resolved to completion; otherwise Hydra reports `missing target resource`. **Ref labels** are not used for this check—materialization uses ref **attributes** (for example `generated`, `origin:app`). Optional Kubernetes references are skipped using the **`optional:ref`** tag on refs.
- Explicit key references such as `secretKeyRef`, `configMapKeyRef`, and keyed projected-volume items are validated against the resolved target (direct object or generated key set).
- `envFrom` checks only that the referenced `Secret` or `ConfigMap` exists, because it does not point to a single named key.

## Output and logs

- **Stdout** carries only finding output when there are findings: **default** human-readable text per finding, or with **`--yaml`** the YAML sequence of findings (same shape as before). With zero findings, stdout has no finding payload. Keep logs off this stream so parsers stay stable when using `--yaml`.
- When there are findings, Hydra also logs a one-line **ERROR** summary with the total count (so you see how many issues were reported without relying on stderr from the CLI layer). With zero findings, an **INFO** line confirms that no reference issues were found.
- **Stderr / log level** carries operational messages. For timing-oriented **debug** messages after Helm templating (reference parsing, ref graph build, key enrichment, target-key normalization, grouping or sorting), enable debug logging with the global `--verbose` (`-v`) flag described under [Global Flags](../README.md#global-flags).

## Examples

```bash
# Review one app
hydra local review prod.infra.cert-manager

# Review all apps on one cluster except one child app
hydra local review prod.** --exclude-app prod.infra.cert-manager

# Review a subset with offline rendering
hydra local review prod.apps.* --helm-network-mode offline

# Machine-readable YAML findings (legacy format)
hydra local review prod.apps.* --yaml
```

## See Also

- [`hydra local template`](template.md) - inspect rendered manifests directly
- [`hydra gitops review`](../cluster/review.md) - validate rendered references against live cluster targets
- [`hydra local find`](find.md) - query rendered resources with CEL
- [`hydra gitops diff`](../cluster/diff.md) - compare rendered state against the live cluster
