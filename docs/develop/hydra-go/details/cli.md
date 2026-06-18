# CLI Architecture

## Overview

The CLI module (`cli/`) implements the Hydra command-line interface using the Cobra framework. It follows a strict separation: Cobra commands define the user interface, action handlers contain the orchestration logic, and core functions provide the business logic.

**Source files:** `cli/cmd/root.go`, `cli/action/*.go`, `cli/cmd/*.go`, `cli/flags/*.go`, `cli/util/*.go`

## Command Hierarchy

This page documents the current CLI hierarchy.

```text
hydra
├── ci [--dry-run | --local] [--target-branch <branch>]
│   ├── config <path>                       Create or edit .hydra-ci.yaml interactively
│   ├── test <config-path>                  Validate changed charts (lint, template)
│   ├── release <config-path>               Detect changes, update versions, create Git tags
│   ├── promote <config-path>               Create promote MRs (dev→stage, stage→prod)
│   ├── publish <config-path>               Build and upload charts to OCI registry
│   ├── validate <config-path>              Validate OCI chart provenance signatures
│   ├── sprint <config-path>                Bump major version at sprint start
│   ├── upgrade <config-path>               Update service version in dev/
│   ├── sync <config-path>                  Copy cluster configurations
│   └── update <config-path>                Refresh unit test data
├── version                                 Print Hydra CLI version
├── local
│   ├── find <appId> [appId...]             Query rendered resources with CEL projection
│   ├── config <appId>                      Show merged global.hydra (Helm + ConfigMaps)
│   ├── template <appId> [appId...]         Render Helm templates locally
│   ├── list <appId> [appId...]             List rendered resource ids (one per line)
│   ├── source <appId> [appId...]           Show chart source templates without rendering
│   ├── values <appId>                      Show merged values
│   ├── refs <cluster> <id>                 List transitive refs for one rendered entity
│   ├── inspect <cluster> [id]              Browse the rendered ref graph in a TUI
│   ├── review <appId...>                   Review reference integrity inside the local render set
│   ├── test
│   │   └── refs <appId...>                 Validate app-defined ref-parser golden files
│   └── export <output-dir>                 Export Hydra data for Hydra UI
├── argocd
│   ├── status [appId...]                   Show the real ArgoCD-reported sync state
│   └── sync
│       ├── auto <appId> [appId...]         Enable automated ArgoCD sync
│       ├── manual <appId> [appId...]       Disable auto-sync but allow manual sync
│       └── prevent <appId> [appId...]      Prevent ArgoCD sync during maintenance
└── cluster
    ├── validate-current-context <cluster>  Validate kubectl context
    ├── status <appId...>                   Check per app whether cluster state is in sync
    ├── dump <cluster>                      Dump cluster resources (multi-doc YAML)
    ├── list <cluster>                      List resource ids (one per line)
    ├── refs <cluster> <id>                 List transitive refs from the merged cluster graph
    ├── apply <appId...>                    Apply rendered resources
    ├── diff <appId...>                     Compare rendered resources with cluster state
    ├── template <appId> [appId...]         Render templates with cluster-aware API normalization
    ├── values <appId>                      Show merged values including cluster Hydra ConfigMaps
    ├── review
    │   ├── app <appId...>                  Review selected rendered sources against live cluster targets
    │   └── cluster <cluster>               Review all apps in one repository cluster
    ├── system <cluster>                    Report cluster default and preset-managed resources
    ├── untracked <cluster>                 List live resources that Hydra does not track
    ├── uninstall <appId...>                Uninstall selected apps from the cluster
    ├── cert-manager restore <cluster>      Restore cert-manager resources
    ├── backup
    │   ├── create <appId...>               Backup secrets from cluster
    │   ├── restore <appId...>              Restore secrets to cluster
    │   ├── list <appId...>                 List backup SopsSecrets in manifests
    │   └── diff <appId...>                 Compare backups with cluster state
    ├── sync
    │   ├── status [appId...]               Show ArgoCD sync mode for selected or all apps
    │   ├── auto <appId> [appId...]         Enable automated sync on the cluster
    │   ├── manual <appId> [appId...]       Set sync to manual on the cluster
    │   └── prevent <appId> [appId...]      Prevent sync on the cluster
    ├── scale
    │   ├── up <appId> [appId...]           Scale workloads up to git-defined target state
    │   ├── down <appId> [appId...]         Scale workloads down to zero
    │   └── status <appId> [appId...]       Show scale sync state and dependencies
    └── inspect <cluster> [id]              Browse the merged live/template ref graph in a TUI
```

### Review Command Families

`hydra local review` is a flat local, render-only command: `hydra local review <appId...>`. `hydra gitops review` is the cluster-connected review family under `hydra gitops` with two subcommands: `hydra gitops review app <appId...>` and `hydra gitops review cluster <cluster>`.

### Manual Layout

The user manual is split accordingly:

- `hydra/docs/manual/README.md` is the root landing page
- `hydra/docs/manual/cli/README.md` is the CLI start page
- `hydra/docs/manual/cli/global/` holds `hydra ci` and `hydra version`
- `hydra/docs/manual/cli/local/` holds `hydra local ...` pages
- `hydra/docs/manual/cli/argocd/` holds `hydra argocd ...` pages
- `hydra/docs/manual/cli/cluster/` holds `hydra gitops ...` pages
- `hydra/docs/manual/charts-repository/`, `gitops-repository/`, and `ui/` are sibling non-CLI manuals

## Architecture Layers

```text
┌─────────────────────────────────────────────────────────────┐
│  cmd/ — Cobra Commands                                       │
│  Define flags, args, help text. Call action handlers.         │
│  No business logic.                                          │
├─────────────────────────────────────────────────────────────┤
│  action/ — Action Handlers                                   │
│  Resolve paths, create contexts, call core functions.        │
│  Orchestration only.                                         │
├─────────────────────────────────────────────────────────────┤
│  core/ — Business Logic                                      │
│  Entity processing, Helm rendering, reference discovery,     │
│  grouping, values merging. No Cobra dependencies.            │
└─────────────────────────────────────────────────────────────┘
```

## Optional user kubeconfig (XDG)

Hydra may read `hydra/config.yaml` under the XDG configuration base (`$XDG_CONFIG_HOME`, or `$HOME/.config` when unset). The document is parsed in `[core/hydra/userkube](../../../../hydra-go/core/hydra/userkube/userkube.go)`: a top-level `contexts` array maps an absolute **cluster directory path** (the on-disk `<hydra-context>/<clusterName>` directory) to a `config` kubeconfig file path and a kubectl `name` (context name inside that file).

`[HydraClusterAccess](../../../../hydra-go/core/hydra/validation.go)` constructs `genericclioptions.NewConfigFlags(true)` and, when a mapping matches the resolved cluster’s absolute path, sets `KubeConfig` and `Context` on those flags **before** `[k8s.ValidateApiContext](../../../../hydra-go/core/k8s/validate.go)` runs. That single validation step enforces `global.hydra.kubectl.allowedContexts` against the same loader used for `ToRESTConfig()`, so the user file cannot bypass allowed-context rules. The Hydra GitOps context remains exclusively from `--hydra-context` / `HYDRA_CONTEXT` (flag precedence per `defineContextFlag` in `[cli/cmd/define_flags.go](../../../../hydra-go/cli/cmd/define_flags.go)`).

If the YAML file is missing, Hydra skips mapping. If the file is present but invalid YAML, Hydra logs a warning and skips mapping. When a mapping applies, Hydra emits an Info-level log with the user config file path, kube context name, and kubeconfig path. A parent-directory fallback (YAML `path` equals the Hydra context root instead of `<hydra-context>/<clusterName>`) still works but triggers a **Warn**-level log recommending an explicit cluster directory in the file. At load time, Hydra also **Warn**s for each `contexts[].path` that is not an existing directory and each `contexts[].config` that is not an existing non-directory file (missing path, wrong type).

## Command Definitions (cmd/)

### Root Command

```go
func NewRootCommand(params RootCommandParams) *cobra.Command
```

Creates the root `hydra` command with global flags and all subcommands.

**Global Flags:**

- `--verbose` / `-v` — show debug-level log messages
- `--quiet` / `-q` — only show warnings and errors
- `--color-log` — force colored log output
- `--no-color-log` — disable colored log output
- `--no-timestamps` — omit timestamps from log messages
- `--json-log` — output logs in JSON format
- `--no-progress` — disable terminal progress bars
- `--less` — pipe combined stdout and stderr through `$PAGER`

**Logging Configuration:** The root command's `PersistentPreRun` configures the logging system based on global flags using `log.Configure()`. For the **terminal progress bar** architecture (`ProgressBars`, `Progress`, coordination with mpb and slog), see [log-progress.md](../log-progress.md). After that, klog is redirected to slog via `klog.SetLogger(logr.FromSlogHandler(slog.Default().Handler()))`. This is needed because Kubernetes `client-go` uses klog internally (e.g. for API server warnings such as finalizer naming); without the redirect, klog writes directly to stderr and bypasses the Hydra handler chain, producing inconsistent log formatting. The redirect runs after `log.Configure()` so `slog.Default()` already carries the configured Hydra handler chain (ColorHandler or JSONHandler depending on `--json-log`, wrapped by FormatHandler and TransformerHandler).

**Cluster parent flags:** `hydra gitops` adds two persistent Kubernetes REST client throttling flags in addition to the shared root flags: `--qps` (float32, `0` = client-go default, negative disables client-side throttling) and `--api-burst` (int, only valid together with positive `--qps`). These flags are inherited by all cluster subcommands, including read-only commands such as `template`, `values`, `system`, and `untracked`.

**Debug-only fields:** The fields `logId`, `source` (caller file/line), and `stack` (stack trace) are only included in log output when debug mode is active (`--verbose`). At Info, Warn, and Error levels without `--verbose`, these diagnostic fields are omitted to keep output clean. This is enforced in `base/log/` at three levels: `log.Configure()` sets `AddSource` only when the level is Debug; `error.go` and `logger.go` attach `logId` only when `slog.Default()` is enabled for Debug; `stack` was already restricted to Error-level with Debug active.

### Dependency Injection

Commands accept their action functions as parameters, enabling testability:

```go
type RootCommandParams struct {
    Config    func(ConfigFlags) (hydra.Hydra, string, error)
    Template  func(TemplateFlags) (hydra.Hydra, string, error)
    Values    func(ValuesFlags) (hydra.Hydra, string, error)
    Review    ReviewCommandParams
    Cluster   ClusterCommandParams
    Ci        CiCommandParams
    ExportContext func(ClusterViewContextFlags) error
    ExportCluster func(ClusterViewClusterFlags) (hydra.Hydra, error)
}

type ReviewCommandParams struct {
    Refs func(ReviewRefsFlags) error
}

type ClusterCommandParams struct {
    Review                ClusterReviewCommandParams
    ValidateCurrentContext func(flags) (hydra.Hydra, string, error)
    Dump                  func(flags) (hydra.Hydra, string, error)
    UninstallApp          func(flags) error
    UninstallRootApp      func(flags) error
    UninstallCluster      func(flags) (hydra.Hydra, error)
    ClusterBackupCreate   func(ClusterBackupCreateFlags) error
    ClusterBackupRestore  func(ClusterBackupRestoreFlags) error
    ClusterBackupList     func(ClusterBackupListFlags) error
    ClusterBackupDiff     func(ClusterBackupDiffFlags) error
}

type ClusterReviewCommandParams struct {
    Refs func(ClusterReviewRefsFlags) error
}
```

The planned review command families follow the same injection pattern as other Cobra command families: `ReviewCommandParams` hangs off `RootCommandParams` for local review, while `ClusterReviewCommandParams` hangs off `ClusterCommandParams` for cluster-connected review. In both cases, `refs` is the first action and later siblings such as `values` and `all` should be added as separate handlers instead of mode flags on `refs`.

### DefineFlags

```go
func DefineFlags(cmd *cobra.Command, f any)
```

Defines flags on a Cobra command using interface detection. The function checks if the flags struct implements specific interfaces and defines the corresponding flags:

```text
f implements WithColorFlag?     → define --color, --no-color, --color-mode
f implements WithDryRunFlag?    → define --dry-run, -d
f implements WithContextFlag?   → define --hydra-context
f implements WithAppIdFlag?     → extract AppId from args
f implements WithExcludeAppFlag? → define --exclude-app
f implements WithClusterFlag?   → extract ClusterName from args
f implements WithNetworkModeFlag? → define --network-mode, --offline, --local
f implements WithCrdModeFlag?   → define --crd-mode
f implements WithPredicatesFlag? → define --include, --exclude
f implements WithKubernetesVersionFlag? → define --kubernetes-version
f implements WithForceUninstallFlag? → define --force, --keep, --force-all (mutually exclusive)
f implements WithForceScaleDownFlag? → define --force-scale-down
f implements WithScaleTimeoutFlag? → define --scale-timeout
f implements WithCrdTimeoutFlag? → define --crd-timeout
```

## Flag System (flags/)

### Pattern

Each flag type follows a consistent pattern:

1. **Flag struct** — Holds the parsed value
2. **Interface** — `WithXxxFlag` for detection by `DefineFlags`
3. **Composition** — Action flag structs embed multiple flag types

```go
// Flag struct
type ColorFlag struct {
    Color types.Color
}

// Interface
type WithColorFlag interface {
    Flags
    WithColorFlag() *ColorFlag
}

// Usage in action flags (composition via embedding)
type DiffFlags struct {
    flags.ColorFlag
    flags.ContextFlag
    flags.AppIdFlag
    flags.NetworkModeFlag
    flags.KubernetesConnectionAllowedFlag
}
```

### Flag Types

| Flag                              | CLI flags                                | Type                                | Default       |
| --------------------------------- | ---------------------------------------- | ----------------------------------- | ------------- |
| `ColorFlag`                       | `--color`, `--no-color`, `--color-mode`  | `types.Color`                       | auto          |
| `ContextFlag`                     | `--hydra-context`                        | `types.HydraContext`                | (from env)    |
| `AppIdFlag`                       | (positional arg)                         | `types.AppId`                       | (required)    |
| `ClusterFlag`                     | (positional arg)                         | `types.ClusterName`                 | (required)    |
| `DryRunFlag`                      | `--dry-run`, `-d`                        | `types.DryRun`                      | false         |
| `NetworkModeFlag`                 | `--network-mode`, `--offline`, `--local` | `types.NetworkMode`                 | online        |
| `CrdModeFlag`                     | `--crd-mode`                             | `types.CrdMode`                     | error         |
| `PredicatesFlag`                  | `--include`, `--exclude`                 | `[]types.CelPredicate`              | []            |
| `KubernetesVersionFlag`           | `--kubernetes-version`                   | `types.KubernetesVersion`           | (auto)        |
| `KubernetesConnectionAllowedFlag` | (internal)                               | `types.KubernetesConnectionAllowed` | (per command) |
| `KeepServerFieldsFlag`            | `--keep-server-fields`                   | `types.KeepServerFields`            | false         |
| `ForceUninstallFlag`              | `--force`, `--keep`, `--force-all`       | `types.ForceUninstall`              | none          |
| `ForceScaleDownFlag`              | `--force-scale-down`                     | `types.ForceScaleDown`              | false         |
| `ScaleTimeoutFlag`                | `--scale-timeout`                        | `time.Duration`                     | 10m           |
| `CrdTimeoutFlag`                  | `--crd-timeout`                          | `time.Duration`                     | 60s           |

`**ForceUninstall` enum values** (`core/types/`):

| Value                    | Description                                                   |
| ------------------------ | ------------------------------------------------------------- |
| `ForceUninstallNone`     | Default — abort if force-deletable leftovers exist            |
| `ForceUninstallForce`    | Delete force-deletable resources                              |
| `ForceUninstallKeep`     | Keep force-deletable resources, proceed with normal uninstall |
| `ForceUninstallForceAll` | Delete both force-deletable and untracked resources           |

**Source file:** `cli/flags/force_uninstall.go`

`--force`, `--keep`, and `--force-all` are registered as mutually exclusive via Cobra's `MarkFlagsMutuallyExclusive`.

`**ForceScaleDown` type** (`core/types/hydra.go`): `type ForceScaleDown bool`

When `true`, `DeleteResources` force-deletes remaining pods with `GracePeriodSeconds: 0` if the scale-down polling timeout expires (configured via `--scale-timeout`, default `10m`). When `false` (default), the operation aborts with `ErrScaleDownTimeout`.

**Source file:** `cli/flags/force_scale_down.go`

`**ScaleTimeoutFlag` type** (`cli/flags/scale_timeout.go`):

Controls the polling timeout for workload readiness during scale-up and scale-down operations. Accepts Go duration strings (e.g. `5m`, `10m`, `1h`).

```go
type WithScaleTimeoutFlag interface {
    WithScaleTimeoutFlag() *ScaleTimeoutFlag
}
type ScaleTimeoutFlag struct {
    ScaleTimeout time.Duration
}
```

Default: `10m`. Used by `ClusterScaleFlags`, `ClusterApplyFlags`, and `ClusterUninstallFlags`.

**Source file:** `cli/flags/scale_timeout.go`

`**CrdTimeoutFlag` type** (`cli/flags/crd_timeout.go`):

Controls the polling timeout for CRD establishment during apply operations. Accepts Go duration strings (e.g. `30s`, `60s`, `2m`).

```go
type WithCrdTimeoutFlag interface {
    WithCrdTimeoutFlag() *CrdTimeoutFlag
}
type CrdTimeoutFlag struct {
    CrdTimeout time.Duration
}
```

Default: `60s`. Used by `ClusterApplyFlags`.

**Source file:** `cli/flags/crd_timeout.go`

### Config Creation

```go
func NewConfigFromFlags(f Flags, kubernetesConnectionAllowed types.KubernetesConnectionAllowed) types.Config
```

Creates a `types.Config` from flag values, extracting Color, DryRun, and KubernetesConnectionAllowed via interface detection.

## CI Subcommands (cmd/ci.go)

### CiCommandParams

```go
type CiCommandParams struct {
    CiConfig func(path string, in io.Reader, out io.Writer, useColor bool) error
    Test     func(action.CiFlags) error
    Release  func(action.CiFlags) error
    Promote  func(action.CiFlags) error
    Publish  func(action.CiFlags) error
    Validate func(action.CiFlags) error
    Sprint   func(action.CiFlags) error
    Deploy   func(action.CiFlags) error
    Sync     func(action.CiFlags) error
    Update   func(action.CiFlags) error
}
```

### CiFlags

The `CiFlags` struct holds the parsed values for all persistent flags
on the `ci` parent command:

```go
type CiFlags struct {
    DryRun       bool
    Local        bool
    ConfigPath   string
    TargetBranch string
    PromoteTo    string
}
```

| Field          | Flag              | Type     | Default | Description                                                      |
| -------------- | ----------------- | -------- | ------- | ---------------------------------------------------------------- |
| `DryRun`       | `--dry-run`       | `bool`   | `false` | Simulate pipeline without changes                                |
| `Local`        | `--local`         | `bool`   | `false` | Create commits locally, no push/MR/upload/webhook                |
| `ConfigPath`   | (positional arg)  | `string` | —       | Path to `.hydra-ci.yaml` (resolved from file or directory)       |
| `TargetBranch` | `--target-branch` | `string` | `""`    | Create commits on this branch instead of auto-generated branches |
| `PromoteTo`    | `--promote-to`    | `string` | `""`    | Limit `hydra ci run promote` to a specific target environment        |

`--target-branch` is registered as a `PersistentStringVar` on the `ci`
parent command. When set, the branch must already exist in the repository.
It can be combined with `--dry-run` (output shows target branch, no
commits) and `--local` (commits locally on target branch, no push).
See [pipeline.md § Target Branch Mode](pipeline.md#target-branch-mode)
for full behavioral details.

`--promote-to` is registered on the `promote` subcommand and narrows the
promotion run to a specific target environment. Other `ci` subcommands do
not use this field.

### newCiSubcommand Helper

All pipeline subcommands (test, release, promote, publish, validate, sprint, upgrade,
sync, update) are created via a shared helper:

```go
func newCiSubcommand(
    name string,
    short string,
    long string,
    actionFn func(action.CiFlags) error,
    ciFlags *action.CiFlags,
) *cobra.Command
```

This helper creates a `cobra.Command` that:

1. Accepts exactly one positional argument `<config-path>`
2. Resolves the path: if it's a directory, appends `ci.ConfigFileName` (`.hydra-ci.yaml`)
3. Assigns the resolved path to `ciFlags.ConfigPath`
4. Calls `actionFn` with the populated flags

The directory resolution ensures that `filepath.Dir(configPath)` always
returns the correct repository root, regardless of whether the user passes
a file path or a directory path. See
[pipeline.md § Config Path Resolution](pipeline.md#config-path-resolution-pipeline-subcommands)
for the full path resolution table and rationale.

### config Subcommand

The `config` subcommand is registered separately and does **not** use
`newCiSubcommand` or `CiFlags`. It accepts a single positional `<path>`
argument and performs its own directory resolution in `CiConfigInit`.
It does not support `--dry-run`, `--local`, or `--target-branch` flags.

## Action Handlers (action/)

Each action handler follows the same pattern:

```go
func ActionName(f ActionFlags) (hydra.Hydra, string, error)
```

Returns:

- `hydra.Hydra` — The resolved Hydra context (for cleanup/caching)
- `string` — Output to display
- `error` — Error (if any)

### template

```text
1. ResolvePathWithAppId() → Hydra context
2. Template() → rendered manifest
3. Return manifest as output
```

### values

```text
1. ResolvePathWithAppId() → Hydra context
2. LoadValuesMap() → merged values
3. yq.ToYaml() → YAML output
```

### config

```text
1. ResolvePathWithAppId() → Hydra context
2. HydraValues() → Helm global.hydra map
3. RenderClusterSelectedApps() → template entities for the app
4. HydraConfigDocumentsFromEntities() → parsed data.hydra per Hydra ConfigMap (sorted by id)
5. MergeHelmHydraWithConfigMapDocuments() → single merged global.hydra (values.MergeValues)
6. yq.ToYaml() → YAML output (ColorFlag)
```

### review refs

Both commands share the same handler implementation; inject local render-set entities or live cluster entities only for target resolution. Shared review logic should accept a finding callback so tests can observe one finding at a time without replacing global stdout. The CLI action collects findings through that callback, then writes stdout: **without `--yaml`**, human-readable text via `WriteReviewFindingsGroupedText` (grouped by `ReviewFindingMessageGroup`, omitting empty `sources` sections); **with `--yaml`**, the prior YAML sequence emitter (`yq.ToYaml` per finding). Apply `ColorFlag` in both modes (default TTY auto-detect). After templating, emit debug logs around ref-parser collection, `references.Refs`, key-attribute enrichment, target-key normalization, and grouping or sorting. Grouping and sorting may consume the full finding list in memory; stdout then receives that list **after** collection (text as grouped blocks by default, or YAML sequence elements when `--yaml` is set—no whole-array `yaml.Marshal` at the end). Single-finding helpers still use `WriteReviewFindingText` (for example tests).

#### hydra local review

```text
1. Resolve selected app IDs from `appId...` plus `--exclude-app`
2. Render only the selected apps locally, analogous to `hydra local template`, then apply scope maps and propagate the app namespace into namespaced template objects when `metadata.namespace` is absent (same rules as `RenderCluster` / `ApplyScopeInfoMaps`)
3. Build resource-level refs and preserve explicit Secret/ConfigMap key selections as repeated relation attributes such as `type=key`
4. Collect review candidates from source resources in the rendered selection
5. Merge **Kubernetes bootstrap** synthetic targets into the full-cluster template target set (filtered by Kubernetes minor from Hydra values) so bootstrap `ClusterRole` / `ClusterRoleBinding` and core namespace defaults resolve without false `missing target resource`
6. Merge **per-namespace kubernetes-defaults** synthetic targets (`ServiceAccount/default`, `ConfigMap/kube-root-ca.crt` with `ca.crt`, `Namespace`) for every namespace present in that template target set, deduped like `RenderCluster`
7. Resolve referenced entities only inside the rendered selection (template targets include merged bootstrap and namespace-default entities)
8. Group findings when target and message are identical across multiple sources, then order them deterministically for output
9. Emit each finding through the injected callback in order; the action then writes either grouped human-readable text (`WriteReviewFindingsGroupedText`) or, with `--yaml`, the next YAML sequence element (with coloring controlled by `ColorFlag`)—per finding for YAML, not one marshal of the full list
10. Use the finding text `missing target resource` when the target object is absent
11. Fail early if `ValidateNoDuplicateTemplateResourceIds` detects the same template id in more than one app’s standalone render (same as uninstall)
12. Run `AppendRefOwnershipReviewFindings` with an empty live cluster snapshot: for each template id, evaluate **`uninstall`** / **`uninstall-force`** / **`backup`** predicates **excluding** ref groups tagged **`runtime`**
13. Return an error when at least one finding exists (after emission finishes)
```

#### hydra gitops review

```text
1. Resolve selected app IDs from `appId...` plus `--exclude-app`
2. Derive the target cluster from the selected app IDs
3. Render only the selected apps locally for the source-side review set, then apply scope maps and propagate the app namespace into namespaced template objects when `metadata.namespace` is absent (same rules as `RenderCluster` / `ApplyScopeInfoMaps`)
4. Build resource-level refs and preserve explicit Secret/ConfigMap key selections as repeated relation attributes such as `type=key`
5. Collect review candidates only from source resources that belong to the selected apps
6. Build **auxiliary** per-namespace kubernetes-defaults template entities from the full enabled-app render; use them only for **same-namespace** references to `ServiceAccount/default` or `ConfigMap/kube-root-ca.crt` (independent of API server version discovery)
7. Resolve referenced entities against live cluster resources plus that auxiliary set under the qualifier rules (suppress `missing target resource` when the target is a known bootstrap ID missing from the cluster; see bootstrap audit)
8. Append **Kubernetes bootstrap audit** findings: for each expected upstream bootstrap ID (filtered by API server minor from discovery) absent from the live inventory, emit `missing cluster default resource` with empty `sources` (skip the entire audit if the server version cannot be determined)
9. Group findings when target and message are identical across multiple sources, then order them deterministically for output
10. Emit each finding through the injected callback in order; the action then writes either grouped human-readable text (`WriteReviewFindingsGroupedText`) or, with `--yaml`, the next YAML sequence element (with coloring controlled by `ColorFlag`)—per finding for YAML, not one marshal of the full list
11. Use the finding text `missing target resource` when the target object is absent (except suppressed bootstrap cases above)
12. Run `AppendRefOwnershipReviewFindings` with the live cluster snapshot: **template-mapped** resource ids use **`uninstall`** / **`uninstall-force`** / **`backup`** predicates **excluding** **`runtime`** (same as local review); **cluster-only** ids (in no standalone template render) use the full predicate set **including** **`runtime`**, and emit a finding when multiple apps match (`RefOwnershipAmbiguousClusterOnlyFinding`)
13. Return an error when at least one finding exists (after emission finishes)
```

### export output-dir (context export)

```text
1. Resolve HYDRA_CONTEXT → Context
2. GetClusters() → all clusters (top-level directories)
3. Render in-cluster (all apps, skipRootApps=false) → inClusterResult
4. Extract CRD scope info from in-cluster's rendered entities
5. For each other cluster:
   a. Render all apps (skipRootApps=false) with in-cluster's CRD scope info
   b. Split result: root app entities → rootResult, child app entities → childResult
   c. Merge rootResult into inClusterResult
   d. Write childResult to <output-dir>/<cluster>/
6. Write consolidated inClusterResult to <output-dir>/in-cluster/
```

This ensures ArgoCD Application CRs (generated by root apps of all clusters) end up in the in-cluster export, while child app workloads are written to their respective cluster directories.

### export (app | root-app | cluster)

```text
1. Resolve path → Hydra context
2. Template() per app → rendered manifests
3. entity.NewEntitiesFromYaml(l, manifest, key) → entities
4. view.RenderDependencies(l, writer, entities) → .hydra.yaml
```

### cluster dump

```text
1. ResolvePathWithCluster() → Cluster
2. cel.CompilePredicate() → filter predicate
3. commands.ListClusterPredicate() → matching entities
4. yq.PrintObject() per entity → multi-document YAML on stdout
```

### cluster list

```text
1. ResolvePathWithCluster() → Cluster
2. cel.CompilePredicate() → filter predicate
3. If --skip-owner-refs: commands.ListClusterAll() → full inventory; predicate per entity; entity.ClusterInventoryEntitiesExcludingOwnedChildren(KeyClusterEntity, full.UidMap()) → roots among matches. Else: commands.ListClusterPredicate() → matching entities
4. Sort by id
5. fmt.Println(id) per entity → one id per line
```

### cluster uninstall (app | root-app | cluster)

`ClusterUninstallFlags` embeds `ForceUninstallFlag`, `ForceScaleDownFlag`, and `ScaleTimeoutFlag`, so all three uninstall subcommands (`app`, `root-app`, `cluster`) inherit the `--force`, `--keep`, `--force-all`, `--force-scale-down`, and `--scale-timeout` flags.

```text
1. Resolve path → Cluster
2. RenderCluster() → rendered entities
3. ListClusterAll() → live cluster entities
4. selectUninstallStuff() → mark resources for uninstall
5. handleLeftovers() → add owned-by resources + orphaned resources to uninstall set
6. handleForceLeftovers() → separate force vs untracked leftovers
   a. Calculate leftovers (cluster entities in namespaces − uninstall entities)
   b. SeparateUninstallForceLeftovers() → forceLeftovers, untrackedLeftovers
   c. If forceLeftovers > 0: WARN with force-deletable resource list
   d. If untrackedLeftovers > 0:
      - --force-all: add all leftovers (force + untracked) to deletion set, proceed
      - otherwise: abort with hint about --force-all (regardless of --force/--keep)
   e. If only forceLeftovers (no untracked):
      - --force or --force-all: add force entities to deletion set, proceed
      - --keep: ignore them, proceed
      - neither: abort with hint about --force, --keep, and --force-all
   f. If no leftovers: proceed normally
   g. Return updated uninstall entities
7. RemoveUninstallFinalizers() → strip listed finalizers cluster-wide
   a. Load finalizer names from uninstall-finalizer config
   b. Scan ALL cluster entities (not scoped to uninstall namespaces)
   c. Patch matching resources to remove only the listed finalizers
   d. Respects --dry-run
8. ColoredUninstallMessage() → display comparison results
9. DeleteResources(forceScaleDown, scaleTimeout) → two-phase deletion
   Phase 1: Scale down + poll every 2s (configurable timeout, default 10m)
     CollectScaleTargets() → patch replicas=0 → poll until pods gone
     On timeout without --force-scale-down: abort (ErrScaleDownTimeout)
     On timeout with --force-scale-down: force-delete pods (grace-period=0)
   Phase 2: Foreground deletion (topological order, up to 3 passes)
     Remove finalizers → delete with DeletePropagationForeground
```

`handleForceLeftovers` lives in `cli/action/cluster_uninstall.go`:

```go
func handleForceLeftovers(
    cluster *hydra.Cluster,
    l log.Logger,
    color types.Color,
    clusterEntities entity.Entities,
    uninstalls entity.Entities,
    namespaces sets.Set[types.Namespace],
    appIds sets.Set[types.AppId],
    forceUninstall types.ForceUninstall,
) (entity.Entities, error)
```

`ColoredUninstallMessage` no longer contains the leftover check — it only displays the comparison of selected vs live entities.

### cluster apply (ClusterApplyFlags)

`ClusterApplyFlags` embeds `ClusterApplyBehaviorFlags` (optional apply steps), `ScaleTimeoutFlag`, `CrdTimeoutFlag`, and `ForceBackupRestoreFlag`, so the `apply` subcommand inherits `--scale-timeout`, `--crd-timeout`, and `--force-backup-restore`. `--scale-timeout` applies to the scale-up phase when `--scale-up` is enabled. `--crd-timeout` controls the polling timeout for CRD establishment (Phase 1). `--force-backup-restore` is forwarded to the apply-integrated backup restore step when `--backup-restore` is active and keeps the same overwrite semantics as `hydra gitops backup restore`.

On every successful `prepareApplyState`, apply prints a fixed **optional apply behaviors** table (see `renderClusterApplyOptionalBehaviorsTable` in `cli/action`): each optional behavior, its effective state for the run (including resolved `--sync` / `EffectiveSyncWindow`), and the enable/disable CLI flags. Output respects `--color` for header and active cells.

Optional behaviors (`--sops-decode`, `--down-scaled`, `--scale-up`, `--orphan-scale-down`, `--sync`, `--bootstrap-guard`, `--bootstrap-clones`, `--backup-restore`, `--disable-webhooks`) default to off, except `--sync` resolves to `default` unless `--bootstrap` is set without an explicit `--sync`, in which case `keep-or-prevent` applies. `--bootstrap` turns the same bundle on for the run except where the user opts out with a matching `--no-<flag>` such as `--no-scale-up` or `--no-backup-restore`. `--no-*` flags are valid only together with `--bootstrap`. With `--bootstrap`, the positive optional flags above must not appear on the command line except `--sync`; use `--no-*` to disable individual bootstrap-implied steps. For integrated restore, `--skip-backup-restore` remains available without `--bootstrap` and still disables restore when combined with `--bootstrap`; `--no-backup-restore` is the bootstrap-paired opt-out and is mutually exclusive with `--backup-restore` and `--skip-backup-restore` like the other backup flags. `--scale-up` requires `--down-scaled` whether implied by `--bootstrap` or set for a non-bootstrap apply. A short completion hint about adjusting sync modes is emitted when at least one `AppProject` sync was mutated for this run (`ApplyClusterApplySyncWindowToEntities` / `SetAppProjectSyncWindowsWithMutationCount`).

#### Apply-integrated backup restore

Within the automatically numbered apply plan (total phase count depends on flags; backup restore is a phase only when enabled), the backup-restore phase runs only when `--backup-restore` is set and `--skip-backup-restore` is not set. It uses a scoped backup-restore flow for the selected apps:

1. It discovers restore candidates from backup manifests rendered for the selected app IDs. Selecting a single child app therefore does not implicitly pull in every backup from the same namespace.
2. It derives the namespaces used by the selected apps from the rendered entities.
3. An earlier namespace phase may create those namespaces so restore targets exist before the restore phase runs.
4. The restore phase calls `commands.BackupRestore` with the selected app IDs and optional secret filters only.
5. Restore logging uses `Backup overview` as the authoritative summary and must not emit extra per-secret `up-to-date` lines before that overview. Generic apply logs must likewise not repeat generic `applying N resources` messages for the same operation.
6. When `**--sops-decode**` and `**--backup-restore**` are both active, ordinary selected-app `SopsSecret` CRs still belong to the main apply path. Only backup resources that are not part of the selected backup manifest set are excluded from the later main apply path instead of being applied as regular resources.

#### Bootstrap (--bootstrap flag)

`--bootstrap` does not switch to a separate phase plan type: the same `buildApplyPhases` builder is used; optional steps are included when implied or set. It implies the optional preparation and phase behaviors above (SOPS decode, bootstrap-tagged clones, bootstrap-guard enforcement, down-scaled apply, scale-up, orphan cleanup, backup restore, non-ready webhook disable) unless opted out per `--no-*` flag. `--sync` defaults to `keep-or-prevent` for bootstrap unless set explicitly, for example `--sync=default` for template-only sync. Opting out of `--bootstrap-guard` via `--no-bootstrap-guard` leaves `enforceBootstrapGuard` false so guard validation does not run; it is distinct from `--skip-bootstrap-guard`, which remains mutually exclusive with `--bootstrap`.

**SopsSecret decryption (`--sops-decode`)** runs only when enabled: decrypts non-backup `SopsSecret` CRs and creates additional plain `v1/Secret` resources alongside the original `SopsSecret` CRs for the selected apps.

**ArgoCD sync (`--sync`)** adjusts rendered `AppProject` entities before workload apply: `default` leaves template values; `manual`, `auto`, and `prevent` align with `hydra gitops sync` mappings; `keep-or-prevent` copies live `spec.syncWindows` from cluster inventory for existing projects and applies the suffix policy only to new `AppProject` resources (see `commands.ApplyClusterApplySyncWindowToEntities`).

### cluster scale (ClusterScaleFlags)

`ClusterScaleFlags` embeds `ScaleTimeoutFlag`, so the `hydra gitops scale up|down` subcommands inherit the `--scale-timeout` flag. This controls the polling timeout for workload readiness during scale-up and scale-down operations.

`ClusterScaleFlags` also embeds `DryRunFlag` and `NoClusterFlag`: `--dry-run` (`-d`) skips Kubernetes mutations while still resolving apps, rendering, and (when not using `--no-cluster`) connecting to list live state; scale and pod-reconciliation paths honor `types.DryRun` in `core/commands`. `--dry-run` and `--no-cluster` are mutually exclusive.

### cluster backup create / restore / list / diff

```text
cluster backup create <appId...> [--include CEL] [--exclude CEL]
  1. NewConfigFromFlags() → Cluster
  2. commands.BackupCreate(cluster, appIds, networkMode, color, dryRun)
     → collect backup-tagged secret groups
     → listClusterSecrets()
     → apply CLI secret filters (`--include` / `--exclude`) to the candidate secrets
     → backupSingleSecret() → SOPS encrypt

cluster backup restore <appId...> [--include CEL] [--exclude CEL] [--create-namespaces] [--force-backup-restore] [--dry-run]
  1. NewConfigFromFlags() → Cluster
  2. commands.BackupRestore(cluster, appIds, networkMode, k8sVersion, filters, force, color, dryRun)
     → BackupSopsSecrets()
     → decrypt backup targets
     → apply CLI secret filters (`--include` / `--exclude`)
     → classify ownership-invalid backups as `skipped`
     → optional namespace preparation for missing target namespaces of the selected backups
     → restoreSingleBackup() / k8s.Apply()

cluster backup list <appId...>
  1. NewConfigFromFlags() → Cluster (no cluster connection required)
  2. commands.BackupList(cluster, appIds, networkMode, k8sVersion)
     → BackupSopsSecrets() → list found backup SopsSecrets

cluster backup diff <appId...>
  1. NewConfigFromFlags() → Cluster
  2. commands.BackupDiff(cluster, appIds, networkMode, k8sVersion, color)
     → listClusterSecrets() → BackupSopsSecrets() → collectPredicateMatchedSecretIds() → compare
```

Design notes:

- `backup create`, `backup restore`, `backup list`, and `backup diff` all use the selected app IDs as the only backup discovery boundary.
- `backup create` must reject secrets whose target namespace does not belong to the selected app, even if a broad backup predicate matched them.
- `backup restore` must warn and mark ownership-invalid backups as `skipped` rather than applying them.
- `backup restore --create-namespaces` may create missing target namespaces, but only for the already selected and ownership-valid backup secrets; it does not broaden backup discovery beyond the selected app IDs.
- `cluster apply` reuses the same backup discovery, but its phase-2 namespace preparation can make selected app namespaces exist before restore runs.

## FlagBuilder (util/)

Generic builder pattern for type-safe flag definition:

```go
util.NewStringFlagBuilder(cmd, "default").
    Name("context").
    Short("c").
    Usage("Path to hydra context").
    Validate(validatePath).
    Build()
```

**Features:**

- Type-safe with Go generics
- Supports string, bool, and enum flags
- Registers validation hooks as PreRun
- Registers shell completion for enum flags
- Fluent builder API

### FlagValue Interface

```go
type FlagValue[T any] interface {
    Type() FlagType      // string / bool / enum
    Value() T            // Current value
    SetValue(T) error    // Set from typed value
    String() string      // String representation
    SetString(string) error  // Set from string
    Values() []T         // All valid values (for enums)
    StringValues() []string  // String representations of valid values
}
```

### AddPreRun Utilities

Utilities for chaining Cobra PreRun hooks without overwriting:

```go
util.AddPreRun(cmd, func(cmd *cobra.Command, args []string) { ... })
util.AddPreRunE(cmd, func(cmd *cobra.Command, args []string) error { ... })
util.AddPersistentPreRun(cmd, func(cmd *cobra.Command, args []string) { ... })
util.AddPersistentPreRunE(cmd, func(cmd *cobra.Command, args []string) error { ... })
```

These wrap existing PreRun hooks and chain the new one, so multiple hooks can coexist.

## Data Flow (Command Execution)

```text
User input: hydra local template --hydra-context ./gitops production.monitoring.prometheus
  │
  ▼
Cobra (cmd/template.go)
  │  Parse flags and args
  │  DefineFlags() detects interfaces, defines CLI flags
  │  PreRun hooks: read flag values, validate
  │
  ▼
Action handler (action/template.go)
  │  Template(TemplateFlags)
  │  Resolve path + AppId → HydraApp
  │  Call h.Template()
  │
  ▼
Core (hydra/child_app.go or root_app.go)
  │  Load chart, merge values, render via Helm
  │  Apply ArgoCD annotations
  │
  ▼
Output (YAML manifest)
  │
  ▼
Cobra prints to stdout
```
