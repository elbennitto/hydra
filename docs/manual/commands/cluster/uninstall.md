# hydra gitops uninstall

Remove Hydra-managed resources from a Kubernetes cluster.

## Synopsis

```text
hydra gitops uninstall <appId> [appId...] [flags]
```

## Description

Removes Kubernetes resources managed by Hydra from the target cluster. By default, creates an automatic backup before removal (disable with `--skip-backup`). If that backup fails—for example the Kubernetes API is unreachable or lists time out—the command aborts with `ErrAborted` and does not proceed to uninstallation.

With **color output** and a **TTY stderr**, Hydra shows **per-resource footer progress** while **listing the cluster** and during **uninstall** phases (webhook deletion, scale-down, resource deletion), including when **`--dry-run`** is set (`dry-run uninstall · …`).

When a bound **PersistentVolumeClaim** is part of the uninstall batch, Hydra also adds the corresponding **PersistentVolume** (cluster-scoped) using the built-in PVC→PV ref edge (`spec.volumeName`), so the PV appears in the uninstall preview and is deleted with the PVC.

Before the colored uninstall preview and again after PV merge, Hydra **removes** planned deletions whose live **`metadata.uid`** lies in the **transitive owner closure** of objects that match **embedded builtin cluster-defaults presets** when evaluated against the **full cluster list** (policy aligned with seeding for uninstall-force warn expansion: for example **`v1/Node`** under the **`kubernetes`** preset, then every inventory object whose owners chain to that UID). This avoids deleting **Node-owned mirror/static Pods** (and other children of those seeds) that were only selected because they are absent from GitOps manifests.

Leftover scanning and `uninstall-force` / `uninstall-safe` namespace checks use **exclusive namespaces plus every namespace your selected apps render into**.

**App ownership:** Hydra builds one central resource model before uninstall planning. First, every live object whose normalized resource id appears in a standalone app render is assigned to that app; cluster-default presets are materialized as preset apps such as `in-cluster.preset.coredns`, so their rendered ids participate in the same step. Second, ownership is copied transitively through Kubernetes **`metadata.ownerReferences`** and generic workload/Event refs when the owner object is already assigned. Third, only still-unassigned runtime objects may be assigned by ref-parser predicates with **`priority >= 0`** from groups tagged **`uninstall`**, **`uninstall-force`**, or **`backup`** (including groups also tagged **`runtime`**). Fourth, objects still unassigned may be assigned by predicates with **`priority < 0`**, including groups tagged only **`uninstall-safe`**. Each priority band must resolve to **exactly one** app; more than one match aborts with `ErrUninstallAmbiguousRefOwnership`. Normal inspect/dependency refs do **not** create app ownership. During both priority bands, Hydra checks owner-reference roots before children; when a root matches one app, single-owner children inherit that app immediately and are skipped by later predicate checks.

**`uninstall-safe`:** predicates still come from all cluster apps’ ref groups, but selection runs only in namespaces where **every** app that has template, ownerReference-inherited, or `priority >= 0` ref-assigned cluster resources there is part of the **current uninstall app list**—so shared namespaces keep cross-app resources until all stakeholder apps are uninstalled. As an ownership signal, `uninstall-safe` is evaluated through the `priority < 0` band, after direct template ownership, owner-reference propagation, and `priority >= 0` teardown refs.

Hydra computes those stakeholders from the same unified resource model used by `cluster untracked`: template presence, live cluster state, app ownership, preset-app ownership, and workload scope live together on one per-ID row. The uninstall action reads namespace stakeholders and workload leftovers from that shared model instead of rebuilding separate ownership-only cluster slices.

**`uninstall-force`** (leftover pass only): a leftover must match `uninstall-force` rules of **at most one** app; **more than one** → abort. **Zero** matches are warned and treated like **untracked** leftovers → **`ErrAborted`** unless **`--force-all`**, **except** objects that match an active **`global.hydra.presets`** untracked CEL rule (builtin **`coredns`** / **`kubernetes`** / **`flannel`** / **`canal`** / **`kubermatic`** / **`syseleven`** / **`metakube`** / **`syseleven-node-problem-detector`** / **`quobyte`** / **`cloudinit`** / **`cinder`** plus Helm/ConfigMap merges, including presets enabled via **`activates`**): those are **omitted** from the warn list (infrastructure noise). Builtin preset seed UIDs for that pass are taken from the **full cluster list** (so cluster-scoped **`v1/Node`** matches under **`kubernetes`**), and warn-list expansion resolves **`metadata.ownerReferences`** against that same inventory so **Node-owned mirror Pods** are not treated as untracked. If a leftover **does** match **`uninstall-force`** for some app **and** matches an active untracked preset, Hydra logs an extra **warning** so you can reconcile preset coverage with real app ownership. Use [`hydra gitops system`](system.md) to inspect merged presets and per-CEL-line matches against the live inventory.

The command also aborts if the **same template resource id** would be produced by **more than one** app’s standalone render.

Objects that match **only** another app’s `uninstall-force` rules are **ignored** for the current uninstall.

**Warning:** This is a destructive operation. Always back up runtime-created secrets first with [`hydra gitops backup create`](backup.md), and use `--dry-run` before the real uninstall.

## When To Use It

Use `hydra gitops uninstall` when the desired outcome is removal, not a temporary stop:

- Remove an app entirely from a cluster
- Tear down and rebuild an app cleanly
- Clear out Hydra-managed resources before a reinstall

Use [`hydra gitops scale`](scale.md) if you only want workloads stopped temporarily. The planned Pod refresh and Pod reconciliation described for `hydra gitops scale` are wired in the `gitops scale` command path; uninstall keeps its own documented flags and phases.

### When the command stops before deleting

If the **automatic pre-uninstall backup** fails, the command aborts before resource planning. If uninstall stops because **force-deletable** leftovers need `--force`, `--keep`, or **`--force-all`** (including **warn-listed** leftovers that matched no `uninstall-force` rules), Hydra prints a **planned deletion** summary first: resources that **would** be removed if you re-run with the appropriate flags. If it stops because of **ambiguous ref ownership**, **ref ownership conflicting with template ownership**, **ambiguous uninstall-force ownership**, or **duplicate template ids across apps**, the command aborts with an error instead of the main preview. The main uninstall preview (`Found … resources … will be uninstalled`) runs only after those checks succeed, so this block is how you see the full plan when the command aborts early.

The last delete phase logs `deleting {N} resources` (total objects in the uninstall batch), not only objects detected as owner-orphans.

Resource listings in the uninstall preview, planned-deletion summaries when the command aborts early, and the automatic pre-uninstall backup overview are sorted lexicographically by resource ID so repeated runs are easy to compare.

## Recommended Safety Sequence

For a controlled uninstall:

1. Validate the kubeconfig target with [`hydra gitops validate-current-context`](validate-current-context.md).
2. Freeze or relax ArgoCD sync behavior as needed with [`hydra argocd`](../argocd/README.md).
3. Back up generated secrets with [`hydra gitops backup create`](backup.md).
4. Preview the uninstall with `--dry-run`.
5. Run the uninstall.
6. Re-apply and restore backups if this is part of a reinstall workflow.

## Arguments

| Argument | Description                                                                                |
| -------- | ------------------------------------------------------------------------------------------ |
| `appId`  | One or more [App IDs](../README.md#app-ids) (supports [wildcards](../README.md#wildcards)) |

## Flags

| Flag | Short | Description |
| --- | --- | --- |
| `--hydra-context` | | Path to the [Hydra context directory](../README.md#hydra-context) (or set `HYDRA_CONTEXT` env var) |
| `--no-cache` | | Disable persistent Helm template cache and in-process Helm-related caches for this run |
| `--color` | `-c` | Force colored output |
| `--dry-run` | `-d` | Show what would be removed without deleting anything |
| `--no-cluster` | | Skip cluster connection (use with `--dry-run`) |
| `--force` | | Force-delete resources marked with `uninstall-force` in the ref-parser |
| `--keep` | | Keep resources marked with `uninstall-force` instead of deleting them |
| `--force-all` | | Delete all untracked resources (use with caution) |
| `--force-scale-down` | | Delete pods that do not terminate within the timeout |
| `--scale-timeout` | | Timeout for scale-down operations before pod deletion (e.g. `10m`) |
| `--skip-backup` | | Skip the automatic cert-manager backup before uninstall |
| `--exclude-app` | | Glob pattern to exclude applications (repeatable) |
| `--include` | `-i` | [CEL expression](../README.md#cel-resource-filters) to filter resources |
| `--exclude` | `-e` | [CEL expression](../README.md#cel-resource-filters) to exclude resources |

## Examples

```bash
# Preview what would be removed
hydra gitops uninstall prod.infra.cert-manager --dry-run

# Uninstall a single app
hydra gitops uninstall prod.infra.cert-manager

# Force uninstall of resources marked for force deletion
hydra gitops uninstall prod.apps.my-service --force

# Uninstall without cert-manager backup (already backed up manually)
hydra gitops uninstall prod.infra.cert-manager --skip-backup

# Preview a full removal including force-deletable resources
hydra gitops uninstall prod.apps.my-service --dry-run --force

# Safe uninstall/reinstall cycle
hydra gitops backup create prod.infra.cert-manager
hydra gitops uninstall prod.infra.cert-manager
hydra gitops apply prod.infra.cert-manager
hydra gitops backup restore prod.infra.cert-manager --create-namespaces
```

## See Also

- [`hydra gitops backup`](backup.md) — backup secrets before uninstalling
- [`hydra gitops apply`](apply.md) — reinstall after uninstall
- [`hydra gitops scale`](scale.md) — scale down instead of uninstalling (less destructive)
