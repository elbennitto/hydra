# Pipeline: Promote

This file documents environment promotion, promote MR behavior, and the detailed `RunPromote` implementation design.

Back to [Pipeline detail index](../pipeline.md).

## Pipeline: `promote`

### Purpose

Compares charts between two environments and creates **one MR per chart**
when differences are found.

### Self-Detection

```text
For each chart:
  promote dev→stage:   diff <chart>/dev/ <chart>/stage/  → difference? → MR
  promote stage→prod:  diff <chart>/stage/ <chart>/prod/ → difference? → MR
                                                         → no diff?    → skip
```text

### New Directories

If a `dev/` directory exists but the corresponding `stage/` directory
does not (new app), the promote pipeline creates a branch and MR
(or just a branch in `--local` mode) that copies the content to `stage/`.
The same applies for `stage/` → `prod/`.

### Root App Promotion

Root apps are **not promotable by default**. To enable promotion of a
root app, it must be explicitly listed in `.hydra-ci.yaml` under
`ci.promote.promotableRootApps`. Root apps not in this list are skipped
with a warning.

### Steps

1. **Compute diff:** For each chart, compare contents of `<source>/` with
   `<target>/`
2. **Filter:** Skip charts with no differences
3. **Check:** Does an open MR already exist for this chart + direction?
   - Yes → update MR (update branch)
   - No → create new MR
4. **Create branch:** One branch per chart
5. **Copy:** Copy files from `<source>/` to `<target>/`
6. **Adjust chart version:** `ComputePromoteTargetVersion` switches the
   environment suffix (`x.y.z-dev` → `x.y.z-stage` → `x.y.z`). If the source
   used a Hydra extra counter (`x.y.z-2-dev`), that counter is **not** copied:
   the target becomes `x.y.z-stage` unless the target chart already uses the
   base line `x.y.z-stage`, then `x.y.z-1-stage`. Without an extra counter on
   the source, the target stays the base line even when it already matches
   (skip / content-only promote). The same applies with Helm prerelease
   segments (`x.y.z-pre-2-dev` → `x.y.z-pre-stage` or `x.y.z-pre-1-stage`).
7. **Commit + push**
8. **Create MR** with:
   - Title: `promote: service-ui dev → stage (1.198.3 → 1.200.9)`
   - Description: diff summary, old/new version
9. **MS Teams notification:** Send webhook

### Branch Naming Convention

Promote branches follow this pattern:

```text
hydra/promote/to-<target-env>/<root-app>/<child-app>
```

Examples:

```text
hydra/promote/to-stage/demo/service-ui
hydra/promote/to-prod/demo/service-ui
hydra/promote/to-stage/cluster-infra/ingress-nginx
hydra/promote/to-prod/demo-infra/postgres
```text

### Full Copy Is Intentional

The promote mechanism intentionally copies the directory completely
(`dev` → `stage`, `stage` → `prod`). Environment-specific settings
are not intended in this repository and are instead maintained in the
respective cluster repository.

### MR Creation

A separate MR is created per chart so teams can review and merge their
services independently.

```text
Example: 3 charts have changed → 3 MRs

  MR #1: promote: service-ui dev → stage (1.198.3 → 1.200.9)
  MR #2: promote: service-auth dev → stage (18.30.0 → 18.33.28)
  MR #3: promote: ingress-nginx dev → stage (4.9.0 → 4.11.0)
```

### MS Teams Webhook

On every MR creation, an MS Teams webhook is sent:

- **Channel:** Configurable per app group (demo, cluster-infra, demo-infra)
  or globally via `.hydra-ci.yaml`
- **Content:** Chart name, direction (dev→stage / stage→prod),
  version change, link to MR

---

## Promote Pipeline — Implementation Design

This section describes the implementation design for `RunPromote` in
`core/ci/pipeline.go`. It builds on the high-level promote specification
above and defines types, interfaces, algorithm, and test strategy.

### Function Signature

```go
func RunPromote(configPath string, mode Mode, actions PromoteActions, targetBranch string, promoteTo string, onEntry ...func(PromotionEntry)) (PromoteResult, error)
```text

| Parameter      | Type             | Description                                             |
| -------------- | ---------------- | ------------------------------------------------------- |
| `configPath`   | `string`         | Absolute path to `.hydra-ci.yaml`                       |
| `mode`         | `Mode`           | One of `DryRun`, `Local`, `CI`                          |
| `actions`      | `PromoteActions` | Interface for external side effects (push, MR, webhook) |
| `targetBranch` | `string`         | Branch name from `--target-branch` flag (empty = auto)  |
| `promoteTo`    | `string`         | Only promote to this target environment (empty = all)   |

The repo root is derived from `configPath` (its parent directory).

### PromoteActions Interface

External operations are abstracted behind an interface so that callers
can supply real implementations (GitLab API, Teams webhook) or test
doubles.

```go
type PromoteActions interface {
    Push(branch string) error
    CreateOrUpdateMR(branch, title, description, sourceEnv, targetEnv string) error
    SendWebhook(webhookURL, message string) error
}
```

| Method             | Called when  | Description                                        |
| ------------------ | ------------ | -------------------------------------------------- |
| `Push`             | `mode == CI` | Pushes the promote branch to the remote            |
| `CreateOrUpdateMR` | `mode == CI` | Creates a new MR or updates the existing branch/MR |
| `SendWebhook`      | `mode == CI` | Sends an MS Teams notification with MR details     |

In `DryRun` mode none of these are called.
In `Local` mode none of these are called (branches and commits are
created locally, but nothing leaves the machine).

### PromoteResult Type

The function returns a structured result that captures what was done
(or would be done in dry-run). This is used for CLI output and testing.

```go
type PromoteResult struct {
    Promotions []PromotionEntry
}

type PromotionEntry struct {
    Group      string // app group, e.g. "demo"
    App        string // app name, e.g. "service-ui"
    SourceEnv  string // e.g. "dev"
    TargetEnv  string // e.g. "stage"
    OldVersion string // version in target before promotion (empty if new)
    NewVersion string // version after promotion
    Branch     string // e.g. "hydra/promote/to-stage/demo/service-ui"
    Skipped    bool   // true if promotion was skipped
    SkipReason string // reason for skipping (e.g. "no differences", "root app not promotable")
    HasError   bool   // true if the entry was skipped due to an error (e.g. invalid version)
}
```text

### Algorithm

```text
 1. Load config from configPath
 2. Derive repo root from config path (parent directory)
 3. Open git repo at repo root

 4. For each env pair (source, target) from config.environments:
    // e.g. (dev, stage), (stage, prod)

    5. Glob rootAppsPath/*/*/*  to find all chart directories for source env
       // e.g. apps/*/*/dev/ → list of directories

    6. For each chart directory:
       a. Parse group/app from path
          // apps/demo/service-ui/dev → group="demo", app="service-ui"

       b. Check if app is a root app (app == "root")
          → if yes and app group NOT in config.promote.promotableRootApps:
            record PromotionEntry with Skipped=true,
            SkipReason="root app not promotable", continue

       c. Load source chart (Chart.yaml from source env dir)
          → on error: record entry with HasError=true, continue

       d. Check if target directory exists; if it does, load target chart
          for its current version string (on error: HasError=true, continue)

       e. Compute new version:
          ComputePromoteTargetVersion(sourceVersion, sourceEnv, targetEnv,
            existingTargetVersionOrEmpty)
          → on error: record entry with HasError=true, continue

       f. If target did not exist:
          record PromotionEntry (OldVersion empty), continue to branch step

       g. If target exists:
          - Compare: are there differences? (see Chart Comparison Logic)
          - If no differences → record PromotionEntry with
            Skipped=true, SkipReason="no differences", continue

       h. Determine branch name:
          hydra/promote/to-<target>/<group>/<app>


       i. If mode == DryRun:
          record PromotionEntry (Skipped=false), continue
          // no git operations, no external calls

       j. Create/checkout branch from main

       k. Build FS with full directory copy:
          - FS.AddDir copies all source files to target path
            (Chart.yaml via chart builder with rewritten version,
             all other files byte-for-byte from disk)
          - FS.Remove marks target files absent from source for deletion
          - CommitFS processes removals, then writes + stages all files

       l. Commit changes with message:
          "promote: <app> <source> → <target> (<oldVersion> → <newVersion>)"
          If oldVersion == newVersion (content-only change):
          "promote: <app> <source> → <target> (content changed)"

       m. If mode == CI:
          - actions.Push(branch)
          - actions.CreateOrUpdateMR(branch, title, description, sourceEnv, targetEnv)
          - actions.SendWebhook(webhookURL, message)

 7. Check for entries with HasError=true
    → if any: return PromoteResult AND a combined error listing all failures
    → Valid charts are still processed; only errored charts are skipped.
    → This ensures all problems are visible in a single run.

 8. Return PromoteResult with all collected PromotionEntry items
```

### Chart Comparison Logic

Two environment directories differ when **any** of the following is true:

| Condition                           | Meaning                                                                                         |
| ----------------------------------- | ----------------------------------------------------------------------------------------------- |
| Target env directory is missing     | New app — target must be created                                                                |
| File sets differ                    | Source and target directories contain different files (recursively)                             |
| `Chart.yaml` base versions differ   | Source chart version (stripped of env suffix) differs from target (using `versionContentEqual`) |
| Non-Chart.yaml file content differs | Any file other than `Chart.yaml` has different byte content in source vs target (recursive)     |

`Chart.yaml` is compared using `versionContentEqual` (ignoring the environment suffix) because the env suffix always differs between environments. All other files are compared byte-for-byte.

Implementation: `chartDirsEqual()` in `core/ci/promote.go` uses `filepath.WalkDir` to recursively collect files from both directories, then compares file sets and content.

When the target directory is missing, `OldVersion` in the `PromotionEntry`
is set to `""` (empty string).

### Mode Behavior Summary

| Step                         | `DryRun` | `Local` | `CI` |
| ---------------------------- | :------: | :-----: | :--: |
| Load config + detect diffs   |    ✓     |    ✓    |  ✓   |
| Create branch                |          |    ✓    |  ✓   |
| Copy files + rewrite version |          |    ✓    |  ✓   |
| Commit                       |          |    ✓    |  ✓   |
| `actions.Push`               |          |         |  ✓   |
| `actions.CreateOrUpdateMR`   |          |         |  ✓   |
| `actions.SendWebhook`        |          |         |  ✓   |
| Return `PromoteResult`       |    ✓     |    ✓    |  ✓   |

### CiFlags: Target Branch Field

The `CiFlags` struct in `cli/action/ci.go` gains a new field:

```go
type CiFlags struct {
    DryRun       bool
    Local        bool
    ConfigPath   string
    TargetBranch string
    PromoteTo    string // Only promote to this target environment
}
```text

#### Registration

The `--target-branch` flag is registered as a `PersistentStringVar` on
the `ci` parent command (alongside `--dry-run` and `--local`), so it is
available to all pipeline subcommands but not to `config`.

```go
// cli/cmd/ci.go — inside NewCiCommand
ciCmd.PersistentFlags().StringVar(&ciFlags.TargetBranch, "target-branch", "",
    "Create commits on the specified branch instead of auto-generated branches. Branch must exist.")
```

#### Propagation

`CiFlags.TargetBranch` is passed through to pipeline functions:

1. **`cli/action/ci.go`**: Each action handler (e.g. `CiPromote`) reads
   `flags.TargetBranch` and passes it to the core pipeline function.
2. **`core/ci/pipeline.go`**: `RunPromote` (and future pipeline functions)
   receives the target branch as a parameter or via the `CiFlags` struct.
3. **`core/ci/promote.go`**: `PromoteActions` implementations use the
   target branch to determine branching strategy.

#### Target Branch Applicability per Pipeline

Not every pipeline creates commits. The `--target-branch` flag only
affects pipelines that create commits. Pipelines that do not create
commits silently ignore the flag (no error).

| Pipeline  | Creates Commits? | `--target-branch` applicable? | Notes                                           |
| --------- | :--------------: | :---------------------------: | ----------------------------------------------- |
| `test`    |        No        |              No               | Only validates (lint, template)                 |
| `release` |       Yes        |              Yes              | Version bumps, root app updates, Git tags       |
| `promote` |       Yes        |              Yes              | File copy between environments                  |
| `publish` |        No        |              No               | Only builds and uploads to Harbor               |
| `sprint`  |       Yes        |              Yes              | Major version bump for all root apps            |
| `upgrade` |       Yes        |              Yes              | Chart.yaml dependency update                    |
| `sync`    |       Yes        |              Yes              | Copies cluster configurations                   |
| `update`  |       Yes        |              Yes              | Refreshes unit test data                        |
| `config`  |        No        |              No               | Interactive config creation (no pipeline flags) |

The `config` subcommand does not receive `CiFlags` at all and therefore
has no access to `--target-branch`.

#### PromoteActions Implementation Changes

| Implementation         | Without `--target-branch`                              | With `--target-branch`                                                   |
| ---------------------- | ------------------------------------------------------ | ------------------------------------------------------------------------ |
| `dryRunPromoteActions` | Logs auto-generated branch per chart                   | Logs target branch name in output for each entry                         |
| `localPromoteActions`  | Checkout main → create branch → commit → checkout main | Checkout target branch (once) → commit per chart → stay on target branch |
| `ciPromoteActions`     | Not yet implemented                                    | Will checkout target branch, commit, push once, create single MR         |

#### Validation

When `--target-branch` is set, the branch existence is validated early:

```go
// Validation at start of pipeline function (or in newCiSubcommand)
if ciFlags.TargetBranch != "" {
    if !repo.BranchExists(ciFlags.TargetBranch) {
        return fmt.Errorf("target branch '%s' does not exist", ciFlags.TargetBranch)
    }
}
```text

This check runs before any git operations (branch creation, commits, etc.)
to fail fast and provide a clear error message.

### Structured Logging

The promote pipeline uses the structured logger (`base/log`) instead of
`fmt.Printf` for all output. This ensures consistent, machine-parseable
log output across all modes.

#### PromoteActions Construction

`NewPromoteActions` creates the appropriate implementation for the given mode.
It does **not** accept a logger — logging is centralized in `cli/action/ci.go`
(`CiPromote` function).

```go
func NewPromoteActions(mode Mode, targetBranch string) PromoteActions {
    switch mode {
    case ModeDryRun:
        return &dryRunPromoteActions{targetBranch: targetBranch}
    case ModeLocal:
        return &localPromoteActions{targetBranch: targetBranch}
    case ModeCI:
        return &ciPromoteActions{}
    default:
        return &dryRunPromoteActions{targetBranch: targetBranch}
    }
}

type dryRunPromoteActions struct {
    targetBranch string
}
```

#### dryRunPromoteActions.ExecutePromotion

`ExecutePromotion` is a no-op (returns `nil`). All dry-run output is
handled by the caller in `cli/action/ci.go`, which iterates over the
`PromoteResult` entries and logs them using the structured logger.

```go
func (a *dryRunPromoteActions) ExecutePromotion(_ *git.Repo, _ PromotionEntry, _ *Config) error {
    return nil
}
```text

#### CiPromote Action (cli/action/ci.go)

The result loop in `CiPromote` replaces `fmt.Printf` with the existing
`logIdAction` (`log.Hydra().Child("cli").Child("action")`):

```go
for _, p := range result.Promotions {
    if p.HasError {
        log.L.ErrorLog(logIdAction, "promote error: {group}/{app} {source} → {target}: {reason}",
            log.String("group", p.Group),
            log.String("app", p.App),
            log.String("source", p.SourceEnv),
            log.String("target", p.TargetEnv),
            log.String("reason", p.SkipReason),
        )
    } else if p.Skipped {
        log.L.DebugLog(logIdAction, "promote skipped: {group}/{app} {source} → {target}: {reason}",
            log.String("group", p.Group),
            log.String("app", p.App),
            log.String("source", p.SourceEnv),
            log.String("target", p.TargetEnv),
            log.String("reason", p.SkipReason),
        )
    } else if p.OldVersion == p.NewVersion {
        log.L.InfoLog(logIdAction, "promoted {group}/{app} {source} → {target} (content changed)",
            log.String("group", p.Group),
            log.String("app", p.App),
            log.String("source", p.SourceEnv),
            log.String("target", p.TargetEnv),
        )
    } else {
        log.L.InfoLog(logIdAction, "promoted {group}/{app} {source} → {target} ({oldVersion} → {newVersion})",
            log.String("group", p.Group),
            log.String("app", p.App),
            log.String("source", p.SourceEnv),
            log.String("target", p.TargetEnv),
            log.String("oldVersion", p.OldVersion),
            log.String("newVersion", p.NewVersion),
        )
    }
}
```

#### Unit Test Impact

The existing test `TestPromote_DryRun_NoGitChanges` in
`core/ci/promote_test.go` creates `dryRunPromoteActions` directly.
Since `dryRunPromoteActions` no longer stores a logger (only `targetBranch`),
no special setup is required:

```go
actions := &dryRunPromoteActions{}
```text

No new test files are needed. All other promote tests use
`mockPromoteActions` and are unaffected by this change.

### Target Branch Behavior (Promote)

When `--target-branch` is set, the promote pipeline changes its branching
strategy: instead of creating one branch per chart, all promotion commits
are placed on the single specified target branch.

When `--target-branch` is set, **all** promotions across **all**
environment pairs (e.g. both dev→stage **and** stage→prod) land on the
same target branch as separate commits. The outer loop iterates
environment pairs sequentially; each promotion appends a commit to the
target branch.

#### Changed Behavior

| Aspect                  | Default (no `--target-branch`)                               | With `--target-branch <branch>`                      |
| ----------------------- | ------------------------------------------------------------ | ---------------------------------------------------- |
| Branch creation         | One branch per chart: `hydra/promote/to-<env>/<group>/<app>` | No branch creation; checkout existing `<branch>`     |
| Commits                 | One commit per chart on its own branch                       | One commit per chart, all on the same target branch  |
| `PromotionEntry.Branch` | `hydra/promote/to-<env>/<group>/<app>`                       | `<branch>` (the target branch for all entries)       |
| Push (CI mode)          | Push each per-chart branch                                   | Push the single target branch once after all commits |
| MR (CI mode)            | One MR per chart                                             | One MR for the target branch (all promotions)        |

#### Algorithm Changes

The algorithm in step (g)–(l) of the [main promote algorithm](#algorithm)
changes as follows when `--target-branch` is set:

```text
Before first chart (once):
  g0. Validate: branch must exist → error if not
  g1. Checkout target branch

For each environment pair (source, target):
  For each chart:
    g. Branch name = target branch (same for all entries)
    h. (DryRun: log target branch name instead of auto-generated branch)
    i. Skip branch creation (already on target branch)
    j. AddDir — copy files + format-preserving version rewrite
    k. Commit on current (target) branch
    l. (CI mode: skip per-chart push — push once after env pair)

  After all charts for this env pair (CI mode only):
    m. actions.Push(targetBranch)
    n. actions.CreateOrUpdateMR(targetBranch, combinedTitle, combinedDescription, sourceEnv, targetEnv)
    o. actions.SendWebhook(webhookURL, combinedMessage)
```

#### Combined MR Format (CI Mode with Target Branch)

When promotions span multiple environment pairs, **one MR per environment
pair** is created on the shared target branch.

**MR Title**: `promote: <sourceEnv> → <targetEnv> (<count> charts)`

Examples:

- `promote: dev → stage (3 charts)`
- `promote: stage → prod (2 charts)`

**MR Description** (body):

```markdown

```text
