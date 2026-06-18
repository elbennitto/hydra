# Pipeline Concept

CI/CD pipelines for Helm chart management in `charts-repository/`. The pipelines handle the full lifecycle from testing through release, promotion, and packaging of Helm charts in a GitOps workflow.

## Key Concepts

- **Eight pipelines** — `test` (validate), `release` (version + tag), `promote` (dev→stage→prod via MR), `publish` (tags at `HEAD` → `helm package` / OCI push in `core/ci/package.go`), `sprint` (major version bump), `upgrade` (service version update), `sync` (cluster config copy), `update` (refresh test data)
- **Configuration** — Single `.hydra-ci.yaml` file defining `rootAppsPath`, `environments`, `appGroups`, and OCI registry
- **Change detection** — Build-tag based: `LastBuildTag(path)` → `HasChanges(tag, HEAD, path)`; if no suitable `build-*` tag exists, compare from `InitialCommitHash` instead (`hydra ci run release`)
- **Versioning scheme** — `major.minor.patch-env` with environment suffixes (`-dev`, `-stage`, none for prod); extra versions for duplicates
- **Root/child app relationship** — Root apps aggregate child apps; version changes propagate to root app `values.yaml` and `Chart.yaml`
- **Promotion flow** — Copy chart directory dev→stage or stage→prod, rewrite version suffix, create branch + MR
- **Target branch mode** — `--target-branch` flag for combining multiple promote commits on a single branch
- **Local/dry-run modes** — `--local` creates commits without push/MR; `--dry-run` simulates without changes

## Source Files

`core/ci/*.go`, `cli/action/ci_*.go`, `cli/cmd/ci.go`

→ **Full details:** [details/pipeline.md](details/pipeline.md)
