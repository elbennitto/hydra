# CI Pipeline Tests

Test case specifications for all CI pipelines (`hydra ci run <pipeline>`). Covers change detection, version calculation, configuration parsing, and each pipeline's behavior including edge cases, modes, and error handling.

## Key Concepts

- **Change detection tests** — Build-tag based detection: no changes, single/multiple file changes, new charts, nested paths
- **Version calculation tests** — Standard versions (`x.y.z-dev/stage/prod`), extra versions for duplicates
- **Configuration tests** — Parsing `.hydra-ci.yaml`, validation errors, custom paths
- **Pipeline-specific tests** — Test, release, promote, publish, sprint, upgrade, sync, update with given/expected patterns
- **Target branch tests** — `--target-branch` flag parsing, combined with `--dry-run` and `--local`
- **Golden file pattern** — `.given.yaml` (input), `.expected.yaml` (auto-generated output), `.hydra-ci.yaml` (config fixture)
- **Test data directory layout** — Organized by concern: `change_detection/`, `version_calculation/`, `promote/`, `config/`

## Source Files

Test files in `cli/cmd/ci_test.go`, `core/ci/*_test.go`; test data in `testdata/` subdirectories

→ **Full details:** [details/pipeline-tests.md](details/pipeline-tests.md)
