# Pipeline: Modes

This file explains CI, local, dry-run, and target-branch execution modes for Hydra pipelines.

Back to [Pipeline detail index](../pipeline.md).

## Local Execution and Modes

All pipelines can be run locally by developers.
Three modes are available, optionally combined with `--target-branch`:

| Mode      | Flag        | Commit | Push | MR  | Upload | Teams Webhook |
| --------- | ----------- | ------ | ---- | --- | ------ | ------------- |
| `ci`      | (default)   | yes    | yes  | yes | yes    | yes           |
| `local`   | `--local`   | yes    | no   | no  | no     | no            |
| `dry-run` | `--dry-run` | no     | no   | no  | no     | no            |

### Target Branch Mode

The `--target-branch <branch>` flag redirects all commits to a single,
pre-existing branch instead of creating auto-generated branches per chart.
This is useful for batching multiple pipeline outputs into one branch
(e.g. all promote commits in a single MR).

#### Behavior

- The specified branch **must already exist** in the repository. If it
  does not, the pipeline exits early with an error.
- Instead of creating per-chart branches (e.g.
  `hydra/promote/to-stage/demo/service-ui`), all commits land on the
  target branch.
- The target branch is checked out once, and all commits are appended
  sequentially.

#### Interaction with Modes

| Mode      | `--target-branch` set                                            | `--target-branch` not set                  |
| --------- | ---------------------------------------------------------------- | ------------------------------------------ |
| `ci`      | Commits on target branch, push, MR/webhook for the single branch | Per-chart branches, per-chart MRs          |
| `local`   | Commits on target branch locally, no push                        | Per-chart branches locally, no push        |
| `dry-run` | No commits; output shows target branch name                      | No commits; output shows auto-branch names |

#### Validation

Validation happens early — in `newCiSubcommand` or at the start of each
pipeline function — before any git operations:

```text
1. If --target-branch is set:
   a. Check that the branch exists in the repo (git branch --list <name>)
   b. If not: return error "target branch '<name>' does not exist"
2. Proceed with pipeline
```text

#### Example Usage

```bash
# Promote all charts to a single branch
$ hydra ci run promote --target-branch my-release-branch .

# Combine with --dry-run to preview
$ hydra ci run promote --dry-run --target-branch my-release-branch .
INFO  [hydra.core.ci] would promote demo/service-ui dev → stage (1.198.3-stage → 1.200.9-stage)
INFO  [hydra.core.ci] target branch: my-release-branch
INFO  [hydra.core.ci] would promote demo/service-auth dev → stage (18.30.0-stage → 18.33.28-stage)
INFO  [hydra.core.ci] target branch: my-release-branch

# Combine with --local to commit locally without push
$ hydra ci run promote --local --target-branch my-release-branch .
Checkout: my-release-branch
Diff dev/ vs stage/:
  service-ui: 1.198.3 → 1.200.9
  service-auth: 18.30.0 → 18.33.28
Commit: abc1234 "promote: service-ui dev → stage (1.198.3 → 1.200.9)"
Commit: def5678 "promote: service-auth dev → stage (18.30.0 → 18.33.28)"
[local] All commits on branch: my-release-branch
[local] No push, no MR, no Teams webhook
```

### dry-run

Simulates the entire pipeline flow without making any changes.
Shows what would happen:

```bash
$ hydra ci run test --dry-run
[dry-run] Changed charts: service-ui/dev, service-auth/dev
[dry-run] helm lint apps/demo/service-ui/dev/ ... OK
[dry-run] helm lint apps/demo/service-auth/dev/ ... OK
[dry-run] helm template apps/demo/service-ui/dev/ ... OK
[dry-run] helm template apps/demo/service-auth/dev/ ... OK

$ hydra ci run release --dry-run
[dry-run] Changed charts: service-ui/dev, service-auth/dev
[dry-run] Would build: service-ui 1.200.9-dev
[dry-run] Would build: service-auth 18.33.28-dev
[dry-run] Would update root app: demo/root/dev
[dry-run] No commit, no upload

$ hydra ci run promote --dry-run
INFO  [hydra.core.ci] would promote demo/service-ui dev → stage (1.198.3-stage → 1.200.9-stage)
INFO  [hydra.core.ci] branch: hydra/promote/to-stage/demo/service-ui
INFO  [hydra.core.ci] would promote demo/service-auth dev → stage (18.30.0-stage → 18.33.28-stage)
INFO  [hydra.core.ci] branch: hydra/promote/to-stage/demo/service-auth
```text

### local

Executes all steps including commits, but without push, without MR
creation, without Harbor upload, and without Teams webhook. Useful for
verifying actual changes locally before they go into CI.

```bash
$ hydra ci run release --local
Changed charts: service-ui/dev
Build: service-ui 1.200.9-dev ... OK
Root app updated: demo/root/dev
Commit: abc1234 "release: service-ui 1.200.9-dev"
[local] No push, no upload to Harbor

$ hydra ci run promote --local
Diff dev/ vs stage/:
  service-ui: 1.198.3 → 1.200.9
Branch: hydra/promote/to-stage/demo/service-ui
Files copied: dev/ → stage/
Version suffix adjusted: 1.200.9-dev → 1.200.9-stage
Commit: def5678 "promote: service-ui dev → stage (1.198.3 → 1.200.9)"
[local] No push, no MR, no Teams webhook
```

### ArgoCD App Naming Scheme

ArgoCD app names are composed of cluster, root app, and optionally child app:

- Root app: `<cluster>.<root-app>`
- Child app: `<cluster>.<root-app>.<child-app>`

Examples:

- Root app: `example.demo`
- Child app: `example.demo.service-ui`

A child app chart must use exactly the child app name as its chart name
(e.g. `service-ui`) and match the directory name.

### ci (default)

Full pipeline flow as in CI/CD. Used automatically when no flag is specified.

### Command Overview

```bash
hydra ci run <command> [--dry-run | --local] [--target-branch <branch>]

Commands:
  test      Validate changed charts (lint, template)
  release   Update versions, build, upload
  promote   Create promote MRs (dev→stage, stage→prod)
  publish   Build and upload charts (triggered by build tag)
  sprint    Bump major version at sprint start
  upgrade   Update service version in dev/
  sync      Copy cluster configurations
  update    Refresh unit test data
  config    Create or edit .hydra-ci.yaml interactively (no --dry-run/--local/--target-branch)

Flags (all `run` commands):
  --dry-run                     Only show what would happen (no changes)
  --local                       Create commits, but no push/MR/upload/webhook
  --target-branch <branch>      Create commits on the specified branch instead of
                                auto-generated branches. Branch must already exist.
  (no flag)                     Full CI/CD flow
```text

### Config Path Resolution (Pipeline Subcommands)

All pipeline subcommands (test, release, promote, publish, sprint, upgrade,
sync, update) accept a `<config-path>` positional argument. This argument
supports both a direct file path to `.hydra-ci.yaml` **and** a directory
path containing it.

The resolution is performed centrally in `newCiSubcommand` (`cli/cmd/ci.go`)
before the path is assigned to `CiFlags.ConfigPath`. This ensures every
pipeline action handler receives a resolved **file path**, regardless of
whether the user passed a file or a directory.

#### Resolution Logic

```go
// cli/cmd/ci.go — inside newCiSubcommand RunE
path := args[0]
if info, err := os.Stat(path); err == nil && info.IsDir() {
    path = filepath.Join(path, ci.ConfigFileName)
}
ciFlags.ConfigPath = path
```

This mirrors the existing logic in `CiConfigInit` (`cli/action/ci_config.go`)
for the `config` subcommand.

#### Path Resolution Table

| Input                                   | Resolved `ConfigPath`  | Behavior                                       |
| --------------------------------------- | ---------------------- | ---------------------------------------------- |
| `/repo/.hydra-ci.yaml` (file exists)    | `/repo/.hydra-ci.yaml` | Used as-is                                     |
| `/repo` (directory exists)              | `/repo/.hydra-ci.yaml` | `.hydra-ci.yaml` appended                      |
| `/repo/` (directory, trailing slash)    | `/repo/.hydra-ci.yaml` | `.hydra-ci.yaml` appended                      |
| `.` (current directory)                 | `./.hydra-ci.yaml`     | `.hydra-ci.yaml` appended                      |
| `/repo/.hydra-ci.yaml` (does not exist) | `/repo/.hydra-ci.yaml` | Used as-is; `LoadConfig` will report the error |
| `/nonexistent` (path does not exist)    | `/nonexistent`         | Used as-is; `LoadConfig` will report the error |

#### Why This Matters

Downstream, `LoadConfig(dir)` (`core/ci/config.go`) expects a **directory**
and appends `ConfigFileName` internally. Pipeline action handlers derive the
directory from `ConfigPath` via `filepath.Dir()`. If a user passes a
directory as `ConfigPath` without this resolution, `filepath.Dir(directory)`
returns the **parent** directory, causing `LoadConfig` to look in the wrong
location.

By resolving the path in `newCiSubcommand`, every action handler and core
function consistently receives a file path, and `filepath.Dir(filePath)`
correctly yields the directory containing `.hydra-ci.yaml`.

#### Consistency with `config` Subcommand

The `config` subcommand already performs the same directory-to-file
resolution in `CiConfigInit` (see [Path Validation](config.md#path-validation)
above). The pipeline subcommands now share this behavior via
`newCiSubcommand`, ensuring a uniform user experience across all `hydra ci run`
subcommands.

#### Unit Tests — `cli/cmd/ci_test.go`

The following tests must be created or updated to verify the path resolution
in `newCiSubcommand`:

| #   | Test                                            | Setup                                                      | Assertion                                                       |
| --- | ----------------------------------------------- | ---------------------------------------------------------- | --------------------------------------------------------------- |
| 1   | `TestNewCiSubcommand_FilePathUnchanged`         | Create `.hydra-ci.yaml` in temp dir, pass file path        | `CiFlags.ConfigPath` equals the file path as-is                 |
| 2   | `TestNewCiSubcommand_DirectoryResolvesToFile`   | Create temp dir with `.hydra-ci.yaml`, pass directory path | `CiFlags.ConfigPath` equals `<dir>/.hydra-ci.yaml`              |
| 3   | `TestNewCiSubcommand_TrailingSlashResolved`     | Create temp dir with `.hydra-ci.yaml`, pass `<dir>/`       | `CiFlags.ConfigPath` equals `<dir>/.hydra-ci.yaml`              |
| 4   | `TestNewCiSubcommand_DotResolvesToFile`         | Run from dir containing `.hydra-ci.yaml`, pass `.`         | `CiFlags.ConfigPath` equals `./.hydra-ci.yaml`                  |
| 5   | `TestNewCiSubcommand_NonexistentPathPassedAsIs` | Pass a path that does not exist                            | `CiFlags.ConfigPath` equals the original path (no modification) |

These tests validate that the resolution only triggers when the path
actually exists and is a directory (via `os.Stat`), and that non-existent
or file paths pass through unchanged.

---
