# Package `git`

## Overview

The `git` package provides a unified Git abstraction for the entire
hydra project. Both CI pipelines (change detection, release, promote, …)
and tests operate on the same `git.Repo` type.

The package has **zero dependencies on `testing`**. Test convenience
comes from combining `git.Init` with `t.TempDir()`.

**See also:** [pipeline.md](pipeline.md) for pipeline architecture,
[pipeline-tests.md](pipeline-tests.md) for test cases.

---

## Repo

### Open / Create

```go
// Open an existing repository
repo := git.Open("/path/to/charts-repository")

// Initialize a new repository at the given path
repo := git.Init("/path/to/new/repo")

// In tests: combine with t.TempDir() for auto-cleanup
repo := git.Init(t.TempDir())
```text

### Error Handling

All chainable methods accumulate errors in `repo.Err`.
Once an error occurs, subsequent calls are no-ops.

```go
repo := git.Init(t.TempDir()).
    Commit("init", "a.txt", "hello").
    Tag("v1.0.0")

if repo.Err != nil {
    t.Fatal(repo.Err)
}
```

### Path

```go
repo.Path() // returns the repository root directory
```text

---

## Commits

```go
// Single-file commit (chainable)
repo.Commit("init", "a.txt", "hello").
    Commit("change", "a.txt", "hello2")

// Multi-file commit (chainable)
repo.CommitFiles("init", map[string]string{
    "a.txt": "hello",
    "b.txt": "world",
})

// FS-based commit (chainable, see FS Builder below)
repo.CommitFS("init charts", fs)
```

---

## Tags

```go
// Create a lightweight tag at HEAD (chainable)
repo.Tag("build-202603051555")

// Chain with commits
repo.Commit("init", "a.txt", "hello").
    Tag("build-202603051555").
    Commit("change", "a.txt", "hello2").
    Tag("demo-ingress-nginx-1.200.9-dev")

// List tags matching a glob pattern
tags, err := repo.Tags("build-*")

// Find the last build-* tag that included a specific path in its release
tag, err := repo.LastBuildTag("apps/demo/ingress-nginx/dev")
```text

---

## Branches

```go
// Create and checkout a new branch (chainable)
repo.Branch("hydra/promote/to-stage/demo/nginx")

// Checkout an existing branch (chainable)
repo.Checkout("main")
```

---

## Change Detection

These methods implement the build-tag based change detection algorithm
described in [pipeline.md — Change Detection](pipeline.md#change-detection).

```go
// List paths changed between two refs
paths, err := repo.ChangedPaths("build-202603051555", "HEAD")

// Check if a specific path has changes between two refs
changed, err := repo.HasChanges("build-202603051555", "HEAD", "apps/demo/ingress-nginx/dev")
```text

### Typical Pipeline Usage

```go
repo := git.Open(repoPath)

tag, err := repo.LastBuildTag("apps/demo/ingress-nginx/dev")
if err != nil {
    // no build tag → chart is new → treat as changed
}

changed, err := repo.HasChanges(tag, "HEAD", "apps/demo/ingress-nginx/dev")
if changed {
    // chart needs version bump / test / promote
}
```

---

## Chart Builder

Builds or modifies Helm charts. Used by both pipelines (load → update
→ save) and tests (create from scratch → add to FS → commit).

### New Chart

```go
chart := git.NewChart("ingress-nginx").
    Version("1.200.9-dev").
    Dep("ingress-nginx", "4.11.8", "https://kubernetes.github.io/ingress-nginx").
    Values(`
replicaCount: 2
image:
  tag: "1.200.9"
`)
```text

### Load Existing Chart

Reads `Chart.yaml` and `values.yaml` from a directory relative to the
repository root. The loaded chart can be modified and saved back.

```go
repo := git.Open("/path/to/charts-repository")

chart, err := repo.LoadChart("apps/demo/ingress-nginx/dev")

chart.Version("1.200.10-dev").
    Dep("ingress-nginx", "4.11.9", "https://kubernetes.github.io/ingress-nginx")

err = chart.Save()
```

### Values

```go
// Set raw values.yaml content (replaces entirely)
chart.Values(`
replicaCount: 2
image:
  tag: "1.200.9"
`)

// Update a single value by dot-path (preserves existing values)
chart.SetValue("image.tag", "1.200.10")

// Read a value by dot-path
tag, err := chart.GetValue("image.tag")
```text

### Read Chart Fields

```go
chart.GetName() string
chart.GetVersion() string
chart.GetDepVersion("ingress-nginx") string
```

### Persistence

```go
// Save back to the directory it was loaded from
err := chart.Save()

// Save to a different directory relative to the repo
err := chart.SaveTo("apps/demo/ingress-nginx/stage")
```text

### Format-Preserving Writes

When a chart is loaded from disk (`LoadChart`), the original raw bytes
of `Chart.yaml` are stored. On save, only the `version` field is
surgically replaced (using `yaml.Node` for locating the field and
byte-level replacement for the actual change). This preserves:

- Comments
- Blank lines
- Field order
- Unknown fields (e.g. `description`)

For programmatically created charts (`NewChart`), `Chart.yaml` is
generated from struct fields as before.

### Generated Files

`Save` / `SaveTo` writes two files into the target directory:

**`Chart.yaml`:**

```yaml
apiVersion: v2
name: ingress-nginx
type: application
version: 1.200.9-dev
dependencies:
  - name: ingress-nginx
    version: "1.200.9"
    repository: "https://kubernetes.github.io/ingress-nginx"
```

**`values.yaml`** (only when values were set):

```yaml
replicaCount: 2
image:
  tag: "1.200.9"
```text

`Type` defaults to `"application"` when empty.

### Pipeline Usage: Release

```go
repo := git.Open(repoPath)
chart, err := repo.LoadChart("apps/demo/ingress-nginx/dev")

newVersion := calcVersion(chart.GetDepVersion("ingress-nginx"), "dev")
chart.Version(newVersion)

err = chart.Save()
```

### Pipeline Usage: Promote dev → stage

```go
repo := git.Open(repoPath)
chart, err := repo.LoadChart("apps/demo/ingress-nginx/dev")

err = chart.PromoteTo("stage")
// loads from apps/demo/ingress-nginx/dev
// deletes   apps/demo/ingress-nginx/stage (if exists)
// rewrites  4.11.8-dev → 4.11.8-stage
// writes to apps/demo/ingress-nginx/stage
```text

### Pipeline Usage: Root App Update

```go
repo := git.Open(repoPath)
root, err := repo.LoadChart("apps/demo/root/dev")

root.SetValue("apps.ingress-nginx.version", "4.11.8-dev").
    Version(bumpRootVersion(root.GetVersion()))

err = root.Save()
```

---

## FS Builder

Creates an in-memory file tree. Used by `CommitFS` to write multiple
files (including generated charts) in a single commit.

```go
fs := git.NewFS().
    File(".hydra-ci.yaml", configYAML).
    Add("apps/demo/ingress-nginx/dev",
        git.NewChart("ingress-nginx").
            Version("4.11.8-dev").
            Dep("ingress-nginx", "4.11.8", "https://kubernetes.github.io/ingress-nginx").
            Values("replicaCount: 2"),
    ).
    Add("apps/demo/fluent-bit/dev",
        git.NewChart("fluent-bit").
            Version("0.55.0-dev").
            Dep("fluent-bit", "0.55.0", "oci://ghcr.io/fluent/helm-charts"),
    )

repo.CommitFS("init charts", fs)
```text

`Add(path, chart)` writes the chart's `Chart.yaml` (and `values.yaml`
if values were set) into the FS at the given path. Arbitrary files can
still be added with `File`.

### Full Directory Copy

For promotion, `AddDir` copies an entire source directory into the FS.
`Chart.yaml` is taken from the chart builder (with the version already
rewritten); all other files are copied byte-for-byte from disk.

```go
fs := git.NewFS()
err := fs.AddDir("apps/demo/ingress-nginx/stage", "/abs/path/to/dev", sourceChart)
```

Files in the target that no longer exist in the source can be marked
for removal:

```go
fs.Remove("apps/demo/ingress-nginx/stage/obsolete.yaml")
```text

`CommitFS` processes removals before additions.

---

## Types

```go
type Repo struct {
    Err error // accumulated error from chained calls
}

// --- Repo construction ---

func Open(path string) *Repo
func Init(path string) *Repo

// --- Repo methods ---

func (r *Repo) Path() string

// Commits (chainable)
func (r *Repo) Commit(msg, file, content string) *Repo
func (r *Repo) CommitFiles(msg string, files map[string]string) *Repo
func (r *Repo) CommitFS(msg string, fs *FS) *Repo

// Tags (chainable)
func (r *Repo) Tag(name string) *Repo

// Tags (query)
func (r *Repo) Tags(pattern string) ([]string, error)
func (r *Repo) LastBuildTag(path string) (string, error)

// Branches (chainable)
func (r *Repo) Branch(name string) *Repo
func (r *Repo) Checkout(name string) *Repo

// Change detection
func (r *Repo) ChangedPaths(from, to string) ([]string, error)
func (r *Repo) HasChanges(from, to, path string) (bool, error)

// Charts
func (r *Repo) LoadChart(dir string) (*Chart, error)

// --- Chart builder ---

type Chart struct{}

func NewChart(name string) *Chart

// Builder (chainable)
func (c *Chart) Version(v string) *Chart
func (c *Chart) Type(t string) *Chart
func (c *Chart) Dep(name, version, repo string) *Chart
func (c *Chart) Values(raw string) *Chart
func (c *Chart) SetValue(path, value string) *Chart

// Read
func (c *Chart) GetName() string
func (c *Chart) GetVersion() string
func (c *Chart) GetDepVersion(name string) string
func (c *Chart) GetValue(path string) (string, error)

// Persistence
func (c *Chart) Save() error
func (c *Chart) SaveTo(dir string) error

// --- FS builder ---

type FS struct{}

func NewFS() *FS
func (fs *FS) File(path, content string) *FS
func (fs *FS) Add(path string, chart *Chart) *FS
func (fs *FS) AddDir(targetRelPath, sourceAbsDir string, chart *Chart) error
func (fs *FS) Remove(paths ...string) *FS
```

---

## Full Examples

### Test: Change Detection

```go
func TestChangeDetection_SingleFileChanged(t *testing.T) {
    repo := git.Init(t.TempDir()).
        CommitFS("init", git.NewFS().
            File(".hydra-ci.yaml", `
ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  appGroups:
    - name: demo
      path: apps/demo
  registry: "oci://ghcr.io/example-org/helm-charts"
`).
            Add("apps/demo/ingress-nginx/dev",
                git.NewChart("ingress-nginx").
                    Version("4.11.8-dev").
                    Dep("ingress-nginx", "4.11.8", "https://kubernetes.github.io/ingress-nginx"),
            ),
        ).
        Tag("build-202603051555").
        Commit("update values", "apps/demo/ingress-nginx/dev/values.yaml", "newKey: newValue")

    if repo.Err != nil {
        t.Fatal(repo.Err)
    }

    changed, err := repo.HasChanges("build-202603051555", "HEAD", "apps/demo/ingress-nginx/dev")
    require.NoError(t, err)
    require.True(t, changed)
}
```text

### Test: No Changes Since Build Tag

```go
func TestChangeDetection_NoChanges(t *testing.T) {
    repo := git.Init(t.TempDir()).
        CommitFS("init", git.NewFS().
            Add("apps/demo/ingress-nginx/dev",
                git.NewChart("ingress-nginx").
                    Version("4.11.8-dev").
                    Dep("ingress-nginx", "4.11.8", "https://kubernetes.github.io/ingress-nginx"),
            ),
        ).
        Tag("build-202603051555")

    if repo.Err != nil {
        t.Fatal(repo.Err)
    }

    changed, err := repo.HasChanges("build-202603051555", "HEAD", "apps/demo/ingress-nginx/dev")
    require.NoError(t, err)
    require.False(t, changed)
}
```

### Test: Chart with Values

```go
func TestReleaseUpdatesRootApp(t *testing.T) {
    repo := git.Init(t.TempDir()).
        CommitFS("init", git.NewFS().
            Add("apps/demo/ingress-nginx/dev",
                git.NewChart("ingress-nginx").
                    Version("4.11.8-dev").
                    Dep("ingress-nginx", "4.11.8", "https://kubernetes.github.io/ingress-nginx").
                    Values("replicaCount: 2"),
            ).
            Add("apps/demo/root/dev",
                git.NewChart("demo").
                    Version("1.0.0-dev").
                    Dep("libchart", "1.0.0", "file://../../../../shared/libchart/dev").
                    Values(`
apps:
  nginx:
    namespace: demo
    enabled: true
    version: "4.11.8-dev"
`),
            ),
        )

    if repo.Err != nil {
        t.Fatal(repo.Err)
    }

    root, err := repo.LoadChart("apps/demo/root/dev")
    require.NoError(t, err)

    root.SetValue("apps.ingress-nginx.version", "4.11.9-dev").
        Version("1.0.1-dev")

    require.NoError(t, root.Save())
}
```text

### Test: Promote Branch Creation

```go
func TestPromote_BranchNaming(t *testing.T) {
    repo := git.Init(t.TempDir()).
        CommitFS("init", git.NewFS().
            Add("apps/demo/ingress-nginx/dev",
                git.NewChart("ingress-nginx").
                    Version("4.11.8-dev").
                    Dep("ingress-nginx", "4.11.8", "https://kubernetes.github.io/ingress-nginx"),
            ).
            Add("apps/demo/ingress-nginx/stage",
                git.NewChart("ingress-nginx").
                    Version("4.11.7-stage").
                    Dep("ingress-nginx", "4.11.7", "https://kubernetes.github.io/ingress-nginx"),
            ),
        ).
        Branch("hydra/promote/to-stage/demo/nginx")

    if repo.Err != nil {
        t.Fatal(repo.Err)
    }
}
```

### Pipeline: Change Detection

```go
func detectChangedCharts(repoPath string, config HydraCI) ([]ChangedChart, error) {
    repo := git.Open(repoPath)

    var changed []ChangedChart
    for _, chartPath := range expandGlobs(config.RootAppsPath) {
        tag, err := repo.LastBuildTag(chartPath)
        if err != nil {
            changed = append(changed, ChangedChart{Path: chartPath, IsNew: true})
            continue
        }

        hasChanges, err := repo.HasChanges(tag, "HEAD", chartPath)
        if err != nil {
            return nil, err
        }
        if hasChanges {
            changed = append(changed, ChangedChart{Path: chartPath})
        }
    }

    return changed, nil
}
```text

### Pipeline: Release Version Bump

```go
func releaseChart(repo *git.Repo, chartDir, env string) error {
    chart, err := repo.LoadChart(chartDir)
    if err != nil {
        return err
    }

    depVersion := chart.GetDepVersion(chart.GetName())
    chart.Version(calcVersion(depVersion, env))

    return chart.Save()
}
```

### Pipeline: Promote dev → stage

Promotion uses `AddDir` to copy the entire source directory. `Chart.yaml`
is format-preserved with only the version rewritten; all other files are
copied byte-for-byte.

```go
sourceChart, _ := repo.LoadChart("apps/demo/ingress-nginx/dev")
sourceChart.Version("4.11.8-stage")

fs := git.NewFS()
fs.AddDir("apps/demo/ingress-nginx/stage", "/abs/path/to/dev", sourceChart)
repo.CommitFS("promote: nginx dev → stage", fs)
```text
