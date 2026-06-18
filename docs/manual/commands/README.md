# Hydra CLI Reference

Hydra is a GitOps deployment tool that orchestrates Helm-based applications across multiple Kubernetes clusters. It wraps Helm and ArgoCD into a single CLI and adds a context hierarchy for values, secrets, dependencies, and cluster-specific configuration.

This reference is written for operators who already know Kubernetes, Helm, and ArgoCD, but are using Hydra for the first time.

If you want the shortest path to a first safe workflow, start with the [Quickstart](../tutorials/quickstart.md).

## CLI Groups

The top-level CLI groups are (aligned with `hydra --help` and the Cobra tree in `hydra-go/cli/cmd/`):

| Group | Purpose | Typical commands |
| ----- | ------- | ---------------- |
| `hydra ci` | Repository and chart lifecycle automation (see [hydra ci](ci/README.md) for release vs promote workflow) | `config`, `secrets create`, `secrets show`, `run download`, `run auto`, `run test`, `run release`, `run promote`, `run publish`, `run verify`, `run sprint`, `run upgrade`, `run sync`, `run update` |
| `hydra version` | Installation verification | Print the CLI version |
| `hydra local` | Local rendering, inspection, review, and export without cluster access | `apps`, `find`, `config`, `template`, `list`, `source`, `values`, `refs`, `review`, `inspect`, `test refs`, `export` |
| `hydra argocd` | ArgoCD-facing reconciliation control and status | `status`, `sync auto`, `sync manual`, `sync prevent` |
| `hydra gitops` | Live-cluster validation and rendered-vs-live operations | `validate-current-context`, `dump`, `list`, `refs`, `apply`, `diff`, `template`, `values`, `status`, `review app`, `review cluster`, `system`, `show`, `untracked`, `uninstall`, `cert-manager restore`, `backup create`, `backup restore`, `backup list`, `backup diff`, `sync status`, `sync auto`, `sync manual`, `sync prevent`, `scale up`, `scale down`, `scale status`, `inspect` |
| `hydra cluster` | Reserved future cluster-only surface (not implemented) | No subcommands yet — shows help only; use `hydra gitops` today |

See [Delegated tool CLIs](#delegated-tool-clis) for upstream tools exposed through the same binary ([`cosign`](wrapped/cosign.md), [`helm`](wrapped/helm.md), and [`yq`](wrapped/yq.md)).

`hydra completion` is the standard Cobra shell-completion generator and is omitted from the operator-focused table above. `hydra ci run validate` exists as a hidden deprecated alias of `run verify`.

## Delegated tool CLIs

Hydra embeds Helm, Cosign, and [yq](https://github.com/mikefarah/yq) so CI/CD pipelines and local workflows can depend on a **single `hydra` executable** instead of installing and pinning several external tools. Hydra already calls these libraries internally; where an upstream project ships a stable CLI, Hydra can expose it as a top-level subcommand with the same interface as the standalone binary.

See [Wrapped Commands](wrapped/README.md) for the wrapper-focused overview and single-binary container-image context.

| Subcommand | CLI on `hydra` | Role inside Hydra | Pipeline note |
| ---------- | -------------- | ----------------- | ------------- |
| [`hydra cosign`](wrapped/cosign.md) | **Available** — full upstream Cosign passthrough (`hydra-go/cli/cmd/root.go` registers Sigstore's Cobra tree) | OCI chart signing and verification in `hydra ci run publish` / `run verify` | Prefer `hydra cosign …` over a separate `cosign` on `PATH` so the signer matches the Hydra release |
| [`hydra helm`](wrapped/helm.md) | **Available** — full upstream Helm passthrough (`hydra-go/cli/cmd/delegated_cli.go` runs `helm.sh/helm/v4/pkg/cmd` outside Hydra’s root flag merge) | Chart render, dependency resolution, `hydra ci run download` / `test`, and related workflows | Prefer `hydra helm …` over a separate `helm` on `PATH` so the client matches the Hydra release |
| [`hydra yq`](wrapped/yq.md) | **Available** — full upstream yq passthrough (`hydra-go/cli/cmd/delegated_cli.go` registers `github.com/mikefarah/yq/v4/cmd`) | `global.hydra.templatePatches`, Argo tracking patches, and similar YAML transforms | Prefer `hydra yq …` over a separate `yq` on `PATH` so the processor matches the Hydra release |

SOPS is **not** bundled as a delegated CLI: operators still configure the [SOPS](https://github.com/getsops/sops) CLI and keys for editing encrypted secrets in the GitOps tree (see [Prerequisites](#prerequisites)).

For day-to-day GitOps work, use Hydra’s native command families (`hydra local`, `hydra gitops`, `hydra ci`) rather than the delegated CLIs unless you need upstream-compatible flags or behavior.

## Mental Model For Kubernetes Admins

Think about Hydra in three layers:

1. **Context**: a repository or directory with cluster definitions, charts, values, and encrypted secrets.
2. **Render**: Hydra resolves values and chart dependencies, then renders manifests locally.
3. **Operate**: Hydra compares, applies, scales, backs up, or removes the rendered resources against a live cluster.

If you already know the standard tools, Hydra roughly maps like this:

| Familiar tool or task | Hydra command | Use it when |
| --------------------- | ------------- | ----------- |
| Query rendered resources like `helm template \| yq` | [`hydra local find`](local/find.md) | You want to filter rendered resources and project specific values as a YAML array |
| `helm template` | [`hydra local template`](local/template.md) | You want rendered manifests without touching a cluster |
| List rendered resource keys (Hydra ids) | [`hydra local list`](local/list.md) | You want one id per line from the same offline pipeline as `hydra local template` |
| Chart `templates/` on disk (no render) | [`hydra local source`](local/source.md) | You want unrendered Helm template files with optional `--include-path` prefix filters |
| `helm template` (cluster-aware `apiVersion` / shared Hydra ConfigMap patches) | [`hydra gitops template`](cluster/template.md) | You want printed manifests aligned with this cluster’s API preferences and `templatePatches` merged from all apps’ Hydra ConfigMaps |
| Review rendered references offline (sources: selected apps; targets: all enabled apps on the cluster) | [`hydra local review`](local/review.md) | You want cross-app template consistency without a live cluster |
| List transitive reference reachability for one id (templates, YAML) | [`hydra local refs`](local/refs.md) | You want BFS reachability up to 10 hops with signed distance, offline |
| List transitive reference reachability for one id (templates + live, YAML) | [`hydra gitops refs`](cluster/refs.md) | You want the same listing on the merged graph used by cluster inspect |
| Interactively browse the ref graph for a resource (offline) | [`hydra local inspect`](local/inspect.md) | You want to navigate transitive incoming/outgoing refs in a TUI with filtering, sorting, and a **Dist** column |
| Interactively browse the ref graph for a resource (live cluster) | [`hydra gitops inspect`](cluster/inspect.md) | Same as offline inspect plus live merge; transitive lists, filtering, sorting, and **Dist** |
| Review rendered references against the full live cluster inventory | [`hydra gitops review`](cluster/review.md) (`app` or `cluster` subcommand) | You want to validate app-selected or whole-cluster sources against everything the API reports |
| Inspect merged values | [`hydra local values`](local/values.md) | You need Helm-layered values only (no chart ConfigMap merge into `global.hydra`) |
| Inspect merged values including cluster-wide Hydra ConfigMaps | [`hydra gitops values`](cluster/values.md) | You need the full Helm values map with `global.hydra` merged from every app’s rendered ConfigMaps |
| Inspect Hydra config | [`hydra local config`](local/config.md) | You need merged `global.hydra` (Helm plus Hydra ConfigMaps from a single-app render) |
| Read the ArgoCD-reported sync state | [`hydra argocd status`](argocd/README.md) | You want the real application sync status as reported by ArgoCD |
| Check whether selected apps are currently in sync on the cluster | [`hydra gitops status`](cluster/status.md) | You want a cluster-based per-app in-sync check derived from rendered desired state vs live resources |
| `kubectl diff` or compare desired vs live | [`hydra gitops diff`](cluster/diff.md) | You want to review changes before applying |
| `kubectl apply --server-side` | [`hydra gitops apply`](cluster/apply.md) | You want Hydra to deploy the rendered resources |
| `kubectl get ... -o yaml` | [`hydra gitops dump`](cluster/dump.md) | You want to inspect the live cluster state through Hydra's view |
| `kubectl scale` for Hydra-managed workloads | [`hydra gitops scale`](cluster/scale.md) | You want to stop or restore workloads temporarily |
| Freeze ArgoCD reconciliation | [`hydra argocd sync`](argocd/README.md) | You need a maintenance window without ArgoCD undoing your changes |
| Backup generated secrets before uninstall | [`hydra gitops backup`](cluster/backup.md) | You need to preserve runtime-created secrets |

## Review Modes

Hydra has two review families on purpose:

| Command | Target scope | Best for |
| ------- | ------------ | -------- |
| [`hydra local review`](local/review.md) | Templates of all **enabled** apps on the cluster (sources: your selection) | Offline validation across enabled Git-rendered apps |
| [`hydra gitops review app`](cluster/review.md) | **All** live resources on the cluster (sources: selected apps) | Pre-apply checks for chosen apps; omits unassigned cluster-only ref-ownership findings |
| [`hydra gitops review cluster`](cluster/review.md) | **All** live resources (sources: all apps in the named cluster after excludes) | Same API validation plus **`ref ownership: cluster-only resource has no Hydra app assignment`** where applicable |

Use `hydra local review` when Git-rendered state across enabled apps should be internally consistent. Use **`hydra gitops review app`** when references from specific apps must hold against the live API. Use **`hydra gitops review cluster`** when you need the full app set for a cluster directory and the unassigned-resource ownership audit.

For **`hydra local review`** and **`hydra gitops review`**, `--exclude-app` narrows **source** apps only, not the **target** inventory. A referenced `Secret` or `ConfigMap` counts as present if the target set contains the object **or** a ref in the stabilized set declares **`"origin:generated": job`** or **`"origin:generated": controller`** for it (for example a `SopsSecret` that materializes the Secret). References from optional Kubernetes fields (`optional: true` on env, envFrom, volume, or projected volume sources) are tagged `optional:ref` in the reference model and are **not** validated by review refs yet (no missing-target or missing-key findings for those edges). The Hydra UI graph shows that tag on edges.

## Prerequisites

- **kubectl** configured with access to your target clusters
- **ArgoCD** installed on each target cluster for sync-based reconciliation control
- **SOPS** configured for secret encryption and decryption
- A **Hydra context directory** containing cluster definitions, charts, values, and secret material
- Optional: [per-user kubeconfig mapping](../appendix/configuration/user-kubeconfig-mapping.md) under XDG config (`hydra/config.yaml`) when different kubeconfig files per cluster directory are needed

Helm does **not** need to be installed separately for Hydra workflows — Hydra embeds Helm internally. For ad-hoc chart commands, use [`hydra helm`](wrapped/helm.md); see [Delegated tool CLIs](#delegated-tool-clis).

## ArgoCD Topics

If you already work with ArgoCD, these are the main Hydra touchpoints:

- **Application and AppProject model**: Hydra root apps map to ArgoCD `Application` resources, while sync control is handled through `AppProject` resources. See [App IDs](#app-ids) and [`hydra argocd`](argocd/README.md).
- **ArgoCD-reported app state**: Use [`hydra argocd status`](argocd/README.md) when you need the real sync state that ArgoCD currently reports for each application.
- **Sync and reconciliation modes**: Use `auto`, `manual`, or `prevent` via [`hydra argocd sync`](argocd/README.md) when you need to control whether ArgoCD may reconcile drift. The same modes exist under [`hydra gitops sync`](cluster/sync.md); use [`hydra gitops sync status`](cluster/sync.md) for a read-only view of AppProject sync.
- **ArgoCD lifecycle**: ArgoCD itself can be installed with `hydra gitops apply in-cluster.argocd` and removed with `hydra gitops uninstall in-cluster.argocd`. See [`hydra gitops apply`](cluster/apply.md) and [`hydra gitops uninstall`](cluster/uninstall.md).
- **Cluster-based in-sync checks**: Use [`hydra gitops status`](cluster/status.md) when you need a per-app answer to whether the rendered desired state currently matches the live cluster resources.
- **Maintenance workflows**: Before scale-downs, temporary manual changes, or uninstall flows, adjust ArgoCD sync behavior so reconciliation does not immediately undo the operation. See [`hydra argocd`](argocd/README.md), [`hydra gitops scale`](cluster/scale.md), [`hydra gitops apply`](cluster/apply.md), and [`hydra gitops uninstall`](cluster/uninstall.md).

## Safety Checklist

Before running any mutating cluster command:

1. Confirm the Hydra context you intend to use.
2. Confirm your active kubeconfig context matches the target cluster with [`hydra gitops validate-current-context`](cluster/validate-current-context.md).
3. Run [`hydra gitops diff`](cluster/diff.md) before [`hydra gitops apply`](cluster/apply.md).
4. Change ArgoCD sync with [`hydra argocd sync`](argocd/README.md) if you plan to scale, uninstall, or make temporary manual changes.
5. Back up generated secrets before destructive operations such as uninstall.

## Key Concepts

### Hydra Context

The Hydra context is the root directory that Hydra reads from. It contains cluster definitions, Helm charts, values at multiple levels, and encrypted secrets. Almost every command needs a context, either through `--hydra-context <path>` or the `HYDRA_CONTEXT` environment variable. If neither is set, Hydra uses the current working directory.

### App IDs

Most commands target one or more applications through `<appId>` selectors. An App ID is a dot-separated path:

```text
<cluster>.<rootApp>
<cluster>.<rootApp>.<childApp>
```

| Format | Example | Description |
| ------ | ------- | ----------- |
| `cluster.rootApp` | `prod.infra` | Root application on a cluster |
| `cluster.rootApp.childApp` | `prod.infra.cert-manager` | Child application under a root application |

Root apps and child apps have different operational meaning:

- **Root apps** represent ArgoCD Application resources.
- **Child apps** contain the actual workload manifests for the target cluster.

When you want to operate on deployable workloads only, prefer selectors such as `prod.infra.*` instead of `prod.infra`.

Hydra validates every app id against the applications that exist in the current Hydra context: root app directories and child apps that are enabled for the Helm network mode used to resolve apps. Unknown app ids fail early with a clear error instead of producing confusing follow-up failures later.

### Wildcards

| Wildcard | Matches | Example |
| -------- | ------- | ------- |
| `*` | Any characters within a single level, but not `.` | `prod.*` matches `prod.infra` but not `prod.infra.cert-manager` |
| `**` | Any characters across all levels, including `.` | `prod.**` matches `prod.infra` and `prod.infra.cert-manager` |

```text
prod.*             # All root apps on prod
prod.infra.*       # All child apps below infra on prod
prod.**            # Everything on prod, root and child apps
**.cert-manager    # cert-manager on any cluster, if your setup supports it
```

**Important:** Do not select multiple clusters for a live cluster operation. `hydra gitops` commands act against your current kubeconfig context, so validate the cluster first.

### CEL Resource Filters

Many commands support `--include` and `--exclude` with [CEL (Common Expression Language)](https://github.com/google/cel-spec) expressions that filter individual Kubernetes resources after rendering.

```bash
# Only Deployments
--include 'kind == "Deployment"'

# Exclude a namespace
--exclude 'namespace == "kube-system"'

# Combine conditions
--include 'kind == "Deployment" && namespace == "production"'

# Match by name
--include 'name.startsWith("api-")'
```

These filters operate on rendered resources, not on App IDs. To exclude whole applications, use `--exclude-app`.

### Helm Network Mode

Helm network mode controls whether Hydra may fetch chart dependencies while rendering:

| Mode | Description |
| ---- | ----------- |
| `online` | Fetch dependencies from remote registries if needed |
| `local` | Use only locally available chart artifacts |
| `offline` | Fail if a remote fetch would be needed |
| `error` | Treat any network access attempt as an error |

Use `offline` or `local` for air-gapped environments, CI validation, or reproducibility-sensitive workflows.

## Choosing The Right Command

Use this as the fast decision table on a first read:

| If you want to... | Start with | Then usually follow with |
| ----------------- | ---------- | ------------------------ |
| Ask which rendered apps or resources match a condition | [`hydra local find`](local/find.md) | [`hydra local template`](local/template.md) |
| Validate rendered resource references offline (full enabled-app template targets) | [`hydra local review`](local/review.md) | [`hydra local template`](local/template.md) or [`hydra gitops review`](cluster/review.md) |
| Validate rendered resource references against the full live cluster inventory | [`hydra gitops review`](cluster/review.md) | [`hydra gitops diff`](cluster/diff.md) |
| Understand merged config | [`hydra local values`](local/values.md) | [`hydra local template`](local/template.md) |
| Understand merged config with cluster-wide Hydra ConfigMaps in `global.hydra` | [`hydra gitops values`](cluster/values.md) | [`hydra gitops template`](cluster/template.md) |
| Inspect Hydra-specific config | [`hydra local config`](local/config.md) | [`hydra local values`](local/values.md) |
| Read the real sync state from ArgoCD | [`hydra argocd status`](argocd/README.md) | [`hydra gitops status`](cluster/status.md) if you also want a cluster-based in-sync check |
| See whether selected apps are in sync on the cluster | [`hydra gitops status`](cluster/status.md) | [`hydra gitops diff`](cluster/diff.md) |
| See rendered manifests | [`hydra local template`](local/template.md) | [`hydra gitops diff`](cluster/diff.md) |
| Check what changed on a live cluster | [`hydra gitops diff`](cluster/diff.md) | [`hydra gitops apply`](cluster/apply.md) |
| Inspect live state as YAML | [`hydra gitops dump`](cluster/dump.md) | [`hydra gitops diff`](cluster/diff.md) |
| Freeze reconciliation for maintenance | [`hydra argocd sync prevent`](argocd/README.md) | [`hydra gitops scale`](cluster/scale.md) or [`hydra gitops uninstall`](cluster/uninstall.md) |
| Preserve generated secrets | [`hydra gitops backup create`](cluster/backup.md) | [`hydra gitops uninstall`](cluster/uninstall.md) |
| Bootstrap a new cluster | [`hydra gitops apply --bootstrap`](cluster/apply.md) | [`hydra gitops diff`](cluster/diff.md) for later changes |

## First 10 Minutes With A Cluster

This sequence gives a safe first operational pass:

```bash
# 1. Validate that kubeconfig points to the intended cluster
hydra gitops validate-current-context prod

# 2. Inspect the effective values for one app
hydra local values prod.infra.cert-manager

# 3. Render the manifests locally
hydra local template prod.infra.cert-manager

# 4. Compare desired vs live state on the cluster
hydra gitops diff prod.infra.*

# 5. Preview the apply path through server-side apply
hydra gitops apply prod.infra.* --dry-run

# 6. Apply for real
hydra gitops apply prod.infra.*
```

## Common Workflows

### Review And Apply A Change

```bash
hydra gitops validate-current-context prod
hydra gitops diff prod.infra.*
hydra gitops apply prod.infra.*
```

### Bootstrap A Fresh Cluster

```bash
hydra gitops validate-current-context prod
hydra gitops apply prod.** --bootstrap
```

Use `--bootstrap` when encrypted secret material from the Hydra context must be restored as native Kubernetes Secrets before the rest of the apply.

### Safe Uninstall And Reinstall

```bash
hydra gitops backup create prod.infra.cert-manager
hydra gitops uninstall prod.infra.cert-manager
hydra gitops apply prod.infra.cert-manager
hydra gitops backup restore prod.infra.cert-manager --create-namespaces
```

### Maintenance Window With ArgoCD Freeze

```bash
hydra argocd sync prevent prod.infra.*
# ... perform maintenance ...
hydra argocd sync auto prod.infra.*
```

### Temporary Scale Down

```bash
hydra argocd sync prevent prod.apps.*
hydra gitops scale down prod.apps.*
# ... later ...
hydra gitops scale up prod.apps.*
hydra argocd sync auto prod.apps.*
```

### Export For Hydra UI

```bash
hydra local export ./hydra-export --helm-network-mode offline
```

Load `./hydra-export` in Hydra UI to inspect dependency graphs, resources, RBAC, and values interactively.

## Global Flags

Available on all commands:

At the default log level, Hydra prints a short **Welcome to Hydra** line with the CLI version to stderr immediately after logging is initialized. That line is omitted for `hydra version` so the command still prints only one line on stdout (`hydra <version> <sha>` for releases, `hydra dev` for local dev builds). With `--quiet`, informational messages including the welcome line are suppressed.

| Flag | Short | Description |
| ---- | ----- | ----------- |
| `--verbose` | `-v` | Enable debug logging |
| `--quiet` | `-q` | Show only warnings and errors |
| `--color-log` | | Force colored log output |
| `--no-color-log` | | Disable colored log output |
| `--no-timestamps` | | Omit timestamps from log output |
| `--json-log` | | Output logs as JSON for CI or log aggregation |
| `--color` | | Force colored stdout on commands that support it |
| `--no-color` | | Disable colored stdout |
| `--color-mode` | | Color mode for stdout (`auto`, `always`, `never`) |
| `--no-progress` | | Disable terminal progress bars on supported cluster commands (`hydra gitops apply`, `hydra gitops uninstall`, `hydra gitops review`, `hydra gitops system`); logs still go to stderr |
| `--less` | | Run Hydra in a subprocess and pipe combined stdout and stderr through `$PAGER` (default: `less -SR +G` — chop long lines, pass ANSI colors, start at end of output); the child enables `--color` and `--color-log` unless you explicitly disable them with `--no-color` or `--no-color-log` (and skips `--color-log` when `--json-log` is set) |

## Command Reference

### Optional user configuration

| Document | Description |
| -------- | ----------- |
| [User kubeconfig mapping](../appendix/configuration/user-kubeconfig-mapping.md) | Optional XDG file `hydra/config.yaml` to map cluster directories to a kubeconfig file and kubectl context (cluster commands and `validate-current-context` use it the same way) |

### Global Commands

| Command | Type | Description |
| ------- | ---- | ----------- |
| [hydra ci](ci/README.md) | Global, mutating repo/CI state | Run CI/CD chart lifecycle commands |
| [hydra version](version.md) | Global, read-only | Print the Hydra version |

### Delegated tool CLIs

See [Delegated tool CLIs](#delegated-tool-clis) for [`hydra cosign`](wrapped/cosign.md), [`hydra helm`](wrapped/helm.md), and [`hydra yq`](wrapped/yq.md).

### Documentation tooling

| Command | Type | Description |
| ------- | ---- | ----------- |
| `hydra record` | Documentation | Record asciinema casts for CLI help and tutorials (`record cli`, `record file <file>...`); `record file` accepts multiple YAML specs, so shell globs like `docs/manual/tutorials/introduction/*.cast.yaml` work directly |

### ArgoCD Commands

These commands talk to ArgoCD on the selected cluster.

| Command | Type | Description |
| ------- | ---- | ----------- |
| [hydra argocd](argocd/README.md) | ArgoCD, mixed | Show the real sync status reported by ArgoCD and manage AppProject sync |

### Local Commands

These commands do not require cluster connectivity.

| Command | Type | Description |
| ------- | ---- | ----------- |
| [hydra local find](local/find.md) | Local, read-only | Query rendered resources with CEL filters and CEL projection |
| [hydra local template](local/template.md) | Local, read-only | Render Helm templates and output Kubernetes manifests |
| [hydra local list](local/list.md) | Local, read-only | Print sorted Hydra resource ids from the local template render pipeline (one per line) |
| [hydra local source](local/source.md) | Local, read-only | Print unrendered chart template files from disk (`--include-path` prefix filter) |
| [hydra local review](local/review.md) | Local, read-only | Review references: selected sources, all enabled apps' templates as targets |
| [hydra local refs](local/refs.md) | Local, read-only | Transitive reference listing for one id (templates only, YAML) |
| [hydra local values](local/values.md) | Local, read-only | Display computed Helm values for an app |
| [hydra gitops values](cluster/values.md) | Cluster, read-only (Helm only) | Display Helm values with `global.hydra` merged from all apps’ Hydra ConfigMaps |
| [hydra local config](local/config.md) | Local, read-only | Display merged `global.hydra` (Helm deep-merged with `data.hydra` ConfigMaps) |
| [hydra local inspect](local/inspect.md) | Local, read-only | Interactive TUI for browsing the reference graph ([shared](inspect-shared.md)) |
| [hydra local export](local/export.md) | Local, read-only | Export dependency model, manifests, and charts for Hydra UI |
| [hydra local test refs](local/test.md) | Local, read-only | Run reference tests for Hydra apps (`hydra local test` has no other subcommands today) |

### Cluster Commands

These commands connect to a live cluster. All of them validate the kubeconfig context before using it.

| Command | Type | Description |
| ------- | ---- | ----------- |
| [hydra gitops validate-current-context](cluster/validate-current-context.md) | Cluster safety check | Verify the active kubeconfig context matches the expected cluster |
| [hydra gitops review](cluster/review.md) | Cluster, read-only | Review references (`review app` or `review cluster`), full live cluster as targets |
| [hydra gitops refs](cluster/refs.md) | Cluster, read-only | Transitive reference listing for one id (merged templates + live, YAML) |
| [hydra gitops status](cluster/status.md) | Cluster, read-only | Check per app whether rendered desired state is in sync with live cluster resources |
| [hydra gitops sync](cluster/sync.md) | Cluster, read-only / mutating | List or set ArgoCD AppProject sync (`status`, `auto`, `manual`, `prevent`) |
| [hydra gitops diff](cluster/diff.md) | Cluster, read-only | Show differences between rendered templates and cluster state |
| [hydra gitops template](cluster/template.md) | Cluster, read-only | Print rendered manifests with API version normalization and cluster-wide `templatePatches` |
| [hydra gitops values](cluster/values.md) | Cluster, read-only (no kube API) | Print merged Helm values with `global.hydra` including Hydra ConfigMaps from all apps |
| [hydra gitops apply](cluster/apply.md) | Cluster, mutating | Render and apply resources via server-side apply |
| [hydra gitops uninstall](cluster/uninstall.md) | Cluster, destructive | Remove Hydra-managed resources from a cluster |
| [hydra gitops scale](cluster/scale.md) | Cluster, mutating | Scale workloads up or down |
| [hydra gitops inspect](cluster/inspect.md) | Cluster, read-only | Interactive TUI ([shared](inspect-shared.md)) |
| [hydra gitops dump](cluster/dump.md) | Cluster, read-only | Multi-document YAML of live cluster resources |
| [hydra gitops list](cluster/list.md) | Cluster, read-only | List Hydra resource ids in a cluster (one per line) |
| [hydra gitops show](cluster/show.md) | Cluster, read-only | Print central live-resource app assignment (ambiguous/unassigned audit) |
| [hydra gitops system](cluster/system.md) | Cluster, read-only | Show merged `global.hydra.presets` matches against live inventory |
| [hydra gitops untracked](cluster/untracked.md) | Cluster, read-only | List ids for live objects not covered by merged templates, cluster-defaults presets, or uninstall-family ref rules (roots only) |
| [hydra gitops backup](cluster/backup.md) | Cluster and context, mutating | Backup and restore Kubernetes secrets |
| [hydra gitops cert-manager](cluster/cert-manager.md) | Cluster and context, mutating | Backup and restore cert-manager resources |
