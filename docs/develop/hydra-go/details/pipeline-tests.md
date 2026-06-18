# CI Pipeline Tests

## Overview

This document describes the test cases for all CI pipelines (`hydra ci run <pipeline>`).
Each section defines the tests that are implemented or planned for a specific
pipeline or cross-cutting concern.

**See also:** [pipeline.md](pipeline.md) for pipeline architecture and behavior,
[cli.md](cli.md) for the Cobra command structure,
[overview.md](overview.md) for the module hierarchy.

---

## 1. Change Detection Tests

Test cases for the build-tag based change detection algorithm
(see [pipeline.md — Change Detection](pipeline.md#change-detection)).

| Test Case                            | Given                                                  | Expected               |
| ------------------------------------ | ------------------------------------------------------ | ---------------------- |
| No changes since last build tag      | Last `build-*` tag includes dir, no commits after      | Not changed            |
| Single file changed                  | One file modified after last `build-*` tag             | Changed                |
| Multiple files changed in same chart | Several files modified in same chart dir               | Changed (single entry) |
| No build tag exists (new chart)      | Directory exists but no `build-*` tag ever set         | Changed                |
| Changes in dev/ only                 | Only `dev/` modified since last tag                    | Only `dev/` detected   |
| Changes in multiple environments     | `dev/` and `stage/` modified                           | Both detected          |
| Changes in unrelated directory       | Only an unrelated dir changed                          | Not changed            |
| Nested path matching                 | `rootAppsPath` directory structure matches nested dirs | Correctly matched      |
| New dev directory created            | New app with `dev/` added, no prior tag                | Detected as changed    |

## 2. Version Calculation Tests

Test cases for computing the correct Helm chart version from `Chart.yaml`
(see [pipeline.md — Versioning Scheme](pipeline.md#versioning-scheme)).

| Test Case              | Given                                      | Expected        |
| ---------------------- | ------------------------------------------ | --------------- |
| Standard dev version   | dependency `1.200.9`, env `dev`            | `1.200.9-dev`   |
| Standard stage version | dependency `1.200.9`, env `stage`          | `1.200.9-stage` |
| Standard prod version  | dependency `1.200.9`, env `prod`           | `1.200.9`       |
| Extra version (first)  | `1.200.9-dev` already exists in registry   | `1.200.9-1-dev` |
| Extra version (second) | `1.200.9-1-dev` already exists in registry | `1.200.9-2-dev` |
| Extra version prod     | `1.200.9` already exists in registry       | `1.200.9-1`     |

## 3. Configuration Tests

Test cases for parsing and validating `.hydra-ci.yaml`
(see [pipeline.md — Configuration](pipeline.md#configuration-hydra-ciyaml)).

| Test Case                     | Given                        | Expected                         |
| ----------------------------- | ---------------------------- | -------------------------------- |
| Valid configuration           | Complete `.hydra-ci.yaml`    | Parsed correctly                 |
| Missing rootAppsPath          | No `rootAppsPath` configured | Error with clear message         |
| Custom rootAppsPath           | Non-standard path configured | Used for scanning                |
| Empty promotableRootApps      | Default (empty list)         | All root apps blocked            |
| Specific promotable root apps | List with entries            | Only listed root apps promotable |

---

## 4. Test Pipeline Tests

Test cases for `hydra ci run test`
(see [pipeline.md — Pipeline: test](pipeline.md#pipeline-test)).

| Test Case          | Given                                  | Expected                                        |
| ------------------ | -------------------------------------- | ----------------------------------------------- |
| Lint passes        | Valid chart changed                    | `helm lint` succeeds                            |
| Lint fails         | Invalid chart syntax                   | Pipeline fails with error                       |
| Template passes    | Valid chart changed                    | `helm template` renders correctly               |
| Template fails     | Invalid template reference             | Pipeline fails with error                       |
| No changes         | No charts changed since last build tag | Pipeline exits immediately                      |
| Tag trigger        | `build-*` tag pushed                   | Tests run for charts listed in release metadata |
| MR status reported | Lint or template error                 | Error visible as MR pipeline status             |

## 5. Release Pipeline Tests

Test cases for `hydra ci run release`
(see [pipeline.md — Pipeline: release](pipeline.md#pipeline-release)).
Core logic and `--local` / `--dry-run` behavior are covered in
`hydra-go/core/ci/release_test.go`; full CI (remote) mode is still a stub.

| Test Case                | Given                                  | Expected                                                    |
| ------------------------ | -------------------------------------- | ----------------------------------------------------------- |
| Standard dev release     | Child chart changed in `dev/`          | Version set to `x.y.z-dev`, root app updated, commit + tags |
| Standard stage release   | Child chart changed in `stage/`        | Version set to `x.y.z-stage`, root app updated              |
| Standard prod release    | Child chart changed in `prod/`         | Version set to `x.y.z` (no suffix), root app updated        |
| No changes               | No charts changed since last build tag | Pipeline exits immediately                                  |
| Multiple charts changed  | 3 child charts changed in same group   | All versioned, single `build-*` tag                         |
| Root app version updated | Child app version changes              | Root `values.yaml` + `Chart.yaml` updated                   |
| Git tags created         | Successful release                     | App tags (documentation) + `build-*` tag (trigger)          |
| Cross-group release      | Charts in demo + cluster-infra changed  | Both root apps updated, one build tag                       |
| Extra version needed     | Version already exists in registry     | Extra version number inserted (`x.y.z-1-dev`)               |

## 6. Promote Pipeline Tests

Test cases for `hydra ci run promote`
(see [pipeline.md — Pipeline: promote](pipeline.md#pipeline-promote)).

| Test Case                         | Given                                                        | Expected                                                                                 |
| --------------------------------- | ------------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| Standard promote dev→stage        | `dev/` differs from `stage/`                                 | Branch + MR created                                                                      |
| No changes to promote             | `dev/` equals `stage/`                                       | Skipped                                                                                  |
| New dev directory (no stage)      | `dev/` exists, `stage/` missing                              | Branch + MR creating `stage/`                                                            |
| Promote stage→prod                | `stage/` differs from `prod/`                                | Branch + MR created                                                                      |
| Version suffix rewrite dev→stage  | `1.200.9-dev` in source                                      | `1.200.9-stage` in target                                                                |
| Version suffix rewrite stage→prod | `1.200.9-stage` in source                                    | `1.200.9` in target                                                                      |
| Root app blocked                  | Root app not in `promotableRootApps`                         | Skipped with warning                                                                     |
| Root app exception                | Root app listed in `promotableRootApps`                      | Branch + MR created                                                                      |
| Local mode                        | `--local` flag                                               | Branch created, no MR                                                                    |
| Dry-run mode                      | `--dry-run` flag                                             | Only output, no changes                                                                  |
| Target branch (local)             | Two charts with diffs, `--local --target-branch <branch>`    | All commits on target branch, no auto-generated branches                                 |
| Target branch (dry-run)           | Two charts with diffs, `--dry-run --target-branch <branch>`  | No commits, output mentions target branch for each entry                                 |
| Target branch does not exist      | `--target-branch <branch>`, branch not created               | Error: "target branch does not exist"                                                    |
| Target branch single chart        | One chart with diff, `--target-branch <branch>`              | Single commit on target branch                                                           |
| Target branch (CI mode)           | Two charts with diffs, `--target-branch <branch>` in CI mode | `pushCalls` has 1 entry, `mrCalls` has 1 entry (combined MR), `webhookCalls` has 1 entry |
| Target branch no diffs            | `--target-branch <branch>`, no charts differ                 | No commits, no error, empty PromoteResult                                                |
| Target branch partial failure     | `--target-branch <branch>`, 3 charts, 1 has invalid version  | 2 commits succeed on target branch, error collected for failed chart                     |
| Existing open MR                  | MR already open for same chart                               | Update existing branch                                                                   |
| Branch naming                     | promote `service-ui` dev→stage                               | `hydra/promote/to-stage/demo/service-ui`                                                  |
| Teams notification                | MR created                                                   | Webhook sent to configured channel                                                       |
| Multiple charts differ            | 3 charts changed                                             | 3 separate MRs created                                                                   |

### 6.1 Target Branch Flag Tests (CLI)

Test cases for `--target-branch` flag parsing in `cli/cmd/ci_test.go`
(see [pipeline.md § CiFlags: Target Branch Field](pipeline.md#ciflags-target-branch-field),
[cli.md § CiFlags](cli.md#ciflags)).

| Test Case                          | Given                                           | Expected                                                             |
| ---------------------------------- | ----------------------------------------------- | -------------------------------------------------------------------- |
| `--target-branch` flag parsed      | Pass `--target-branch my-branch`                | `CiFlags.TargetBranch == "my-branch"`                                |
| `--target-branch` with `--dry-run` | Both flags set                                  | `CiFlags.TargetBranch` and `CiFlags.DryRun` both populated correctly |
| `--target-branch` with `--local`   | Both flags set                                  | `CiFlags.TargetBranch` and `CiFlags.Local` both populated correctly  |
| `config` rejects `--target-branch` | Pass `hydra ci config --target-branch X <path>` | Flag not recognized (config does not use CiFlags)                    |

## 7. Publish Pipeline Tests

Test cases for `hydra ci run publish`
(see [pipeline.md — Pipeline: publish](pipeline.md#pipeline-publish)).

| Test Case                   | Given                                | Expected                                       |
| --------------------------- | ------------------------------------ | ---------------------------------------------- |
| Standard package            | `build-*` tag exists, `test` passed  | Charts packaged and uploaded to registry       |
| Parse build tag             | `build-202603051555`                 | Timestamp extracted, release metadata loaded   |
| App tag → directory mapping | `demo-service-ui-1.200.9-dev`         | Resolves to `apps/demo/service-ui/dev/`         |
| Prod app tag mapping        | `demo-service-ui-1.200.9` (no suffix) | Resolves to `apps/demo/service-ui/prod/`        |
| Root app tag mapping        | `demo-root-200.23.0-dev`              | Resolves to `apps/demo/root/dev/`               |
| Registry push               | Chart packaged                       | Uploaded to OCI registry from `.hydra-ci.yaml` |
| Dependency update           | Chart has OCI dependencies           | `helm dependency update` runs before package   |

## 8. Sprint Pipeline Tests

Test cases for `hydra ci run sprint`
(see [pipeline.md — Pipeline: sprint](pipeline.md#pipeline-sprint)).

| Test Case               | Given                           | Expected                                    |
| ----------------------- | ------------------------------- | ------------------------------------------- |
| Sprint boundary crossed | Monday 08:00, new sprint starts | Major version bumped for all root apps      |
| No sprint change        | Monday 08:00, same sprint       | Pipeline exits immediately                  |
| Dev/stage version       | Sprint 42 starts                | `42.0.0-dev` / `42.0.0-stage`               |
| Prod version            | Sprint 42 starts (previous: 41) | `41.0.0`                                    |
| All root apps updated   | Sprint change detected          | All groups (demo, cluster-infra, ...) bumped |
| Commit + push           | Versions bumped                 | Single commit pushed to main                |

## 9. Upload Pipeline Tests

Test cases for `hydra ci run upgrade`
(see [pipeline.md — Pipeline: upgrade](pipeline.md#pipeline-upgrade)).

| Test Case                    | Given                                              | Expected                                               |
| ---------------------------- | -------------------------------------------------- | ------------------------------------------------------ |
| Version-only change          | Service version updated, no test data changes      | Direct push to main                                    |
| Unit test data changes       | Service version updated, rendered manifests differ | FB + MR created, Teams notification                    |
| Lint after update            | `Chart.yaml` updated with new version              | `helm lint` + `helm template` pass                     |
| Invalid update               | Updated version breaks template rendering          | Pipeline fails, no commit                              |
| Chart.yaml updated           | `service-auth` version `22.12.3` deployed to dev   | `dependencies[0].version` set to `22.12.3`             |
| Direct push commit message   | Version-only change                                | `upgrade: service-auth 22.12.3 (dev)`                   |
| MR title on test data change | Rendered manifests changed                         | `upgrade: service-auth 22.12.3 (dev) - Review required` |

## 10. Sync Pipeline Tests

Test cases for `hydra ci run sync`
(see [pipeline.md — Pipeline: sync](pipeline.md#pipeline-sync)).

| Test Case        | Given                                | Expected                   |
| ---------------- | ------------------------------------ | -------------------------- |
| Configs synced   | External cluster configs changed     | Copied to target locations |
| No changes       | External configs match current state | No copy performed          |
| Update triggered | Configs synced successfully          | `update` pipeline started  |

## 11. Update Pipeline Tests

Test cases for `hydra ci run update`
(see [pipeline.md — Pipeline: update](pipeline.md#pipeline-update)).

| Test Case            | Given                                          | Expected                                |
| -------------------- | ---------------------------------------------- | --------------------------------------- |
| Test data changed    | Rendered manifests differ from stored data     | Auto-commit with refreshed test data    |
| No changes           | Rendered manifests match stored data           | Pipeline passes, no commit              |
| Second diff detected | Auto-commit already made, new diff on next run | Pipeline fails, manual review required  |
| Governance limit     | More than one auto-commit per MR attempted     | Pipeline fails                          |
| Commit message       | Auto-commit created                            | `update: refresh unit test data [auto]` |
| MR status            | Test data up to date                           | MR passes `update` check                |

---

## Test Data

Tests follow the **golden file pattern** used throughout the project
(see [overview.md](overview.md#testing-strategy)).

### File Conventions

| Pattern           | Purpose                                                                                                     |
| ----------------- | ----------------------------------------------------------------------------------------------------------- |
| `*.given.yaml`    | Input fixture — describes the test scenario                                                                 |
| `*.expected.yaml` | Expected output — **auto-generated**, do NOT edit manually                                                  |
| `.hydra-ci.yaml`  | Pipeline configuration fixture per test case (in production there is only one global file in the repo root) |
| `Chart.yaml`      | Helm chart metadata fixture                                                                                 |

### Directory Layout

```tree
testdata/
    change_detection/
        no_changes/
            .hydra-ci.yaml
            setup.given.yaml
            result.expected.yaml
        single_file_changed/
            ...
        new_chart/
            ...
    version_calculation/
        standard_dev.given.yaml
        standard_dev.expected.yaml
        extra_version.given.yaml
        extra_version.expected.yaml
    promote/
        dev_to_stage/
            ...
        root_app_blocked/
            ...
    config/
        valid.given.yaml
        missing_chart_paths.given.yaml
        ...
```text

### Rules

- `.expected.yaml` files are **auto-generated** — do NOT add comments
- Add comments explaining test scenarios to `.given.yaml` files
- Use `-update` flag to regenerate golden files: `go test ./... -update`
- Test data is embedded via `//go:embed` directives

---

## Running Tests

### All Tests

```bash
# Run all tests across all modules (build, lint, vet, test)
./test.sh
```

The test script runs for each module (`base`, `core`, `cli`):

1. `go mod tidy`
2. `gofmt -w .`
3. `go vet ./...`
4. `staticcheck ./...`
5. `gopls check`
6. `go build ./...`
7. `gotest -v ./...`

### Pipeline Tests Only

```bash
go test ./... -v -run TestPipeline
```text

### Update Golden Files

```bash
# Regenerate all expected files
./update_testdata.sh

# Or with the -update flag
go test ./... -update
```

### Dry-Run Verification

For promote and upgrade tests that interact with Git or GitLab, use the
`--dry-run` flag in integration tests to validate logic without side effects.

---

## Package `git`

The `git` package provides the shared Git abstraction used by both
pipelines and tests. See [git.md](git.md) for the full API reference.
