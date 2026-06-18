# Git Package

The `git` package provides a unified Git abstraction for the entire Hydra project. Both CI pipelines (change detection, release, promote) and tests operate on the same `git.Repo` type. The package has zero dependencies on `testing`.

## Key Concepts

- **Repo** — Chainable API with accumulated error handling (`repo.Err`); created via `Open()` or `Init()`
- **Commits** — Single-file (`Commit`), multi-file (`CommitFiles`), or FS-based (`CommitFS`) with chaining
- **Tags** — Lightweight tags at HEAD; `Tags(pattern)` for listing, `LastBuildTag(path)` for pipeline change detection
- **Branches** — `Branch(name)` creates and checks out; `Checkout(name)` switches to existing
- **Change detection** — `ChangedPaths(from, to)` and `HasChanges(from, to, path)` for build-tag based detection
- **Chart builder** — `NewChart(name)` / `LoadChart(dir)` for creating and modifying Helm charts with format-preserving writes
- **FS builder** — In-memory file tree for `CommitFS`; supports `File()`, `Add()` (charts), `AddDir()` (promotion), and `Remove()`

## Source Files

`core/git/repo.go`, `core/git/chart.go`, `core/git/fs.go`

→ **Full details:** [details/git.md](details/git.md)
