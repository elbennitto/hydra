# Pipeline Foundations: Change Detection and Versioning

This page explains how pipelines detect releasable changes and derive chart and root-app versions.

Back to [Pipeline: Foundations](../foundations.md).

## Change Detection

### Build-Tag Based Algorithm

Instead of diffing against `main`, the system uses a **build-tag based** approach.
For each chart directory configured in `.hydra-ci.yaml`:

```text
1. Find the last build-* tag that included this directory in its release
2. Check if any commits between that tag and HEAD modified files in the directory
3. If changes exist → mark the chart as changed
4. If no build-* tag exists for this directory (new chart) → mark as changed
```

**Implementation note (`hydra ci run release`):** Step 1 uses `LastBuildTag` on the
chart path. When that lookup fails (no `build-*` tags at all, or no tag’s
release commit ever touched this path), the baseline ref falls back to the
**initial repository commit** and `HasChanges` is evaluated from there to
step 2.

This approach ensures that only actual unreleased changes trigger version bumps,
regardless of merge order or branch structure.

### Self-Detection Per Pipeline

Each pipeline uses the change detection algorithm to determine its scope:

```text
test:     Detect changed chart directories → run lint/template for each
release:  Detect changed chart directories → version bump + tag for each
promote:  Diff source env vs target env → MR for each difference
publish:  Parse build tag → package + upload listed charts
```

---

## Versioning Scheme

### Environment Suffix

Each chart receives a version with environment suffix. Dependency versions may
include a semver pre-release identifier (e.g. `1.5.1-2b866935`); the
environment suffix is appended after it.

Full format: `x.y.z[-prerelease][-extra][-env]`

- `x.y.z` — semver base version (major.minor.patch)
- `prerelease` (optional) — non-numeric pre-release identifier from the upstream dependency, may contain hyphens but each segment must contain at least one non-digit character
- `extra` (optional) — numeric counter appended when the base version already exists
- `env` (optional) — `dev` or `stage` (prod has no suffix)

| Environment | Version Format                     | Example (plain) | Example (pre-release)  |
| ----------- | ---------------------------------- | --------------- | ---------------------- |
| dev         | `x.y.z[-prerelease][-extra]-dev`   | `1.200.9-dev`   | `1.5.1-2b866935-dev`   |
| stage       | `x.y.z[-prerelease][-extra]-stage` | `1.200.9-stage` | `1.5.1-2b866935-stage` |
| prod        | `x.y.z[-prerelease][-extra]`       | `1.200.9`       | `1.5.1-2b866935`       |

### Root App Versions

The root app version is `<sprint>.<id>.<patch><env>`, e.g. `202.22.0-stage` (the
first number is the sprint, the second is a feature counter, the third is a
release counter). Root apps automatically install all child apps in the
matching version.

On each `release` run, the root app chart version is bumped **once** per
affected `(group, environment)` when at least one child chart in that group
changed:

- If **every** updated child has the same **wrapper base** as before (only the
  extra counter changed, e.g. `1.0.0-dev` → `1.0.0-1-dev`), the **third**
  component of the root version is incremented (`200.22.0-dev` →
  `200.22.1-dev`).
- If **any** child’s base changed (including a new upstream **dependency**
  semver or Helm pre-release segment, e.g. `1.0.0-dev` → `1.0.1-dev`), the
  **second** component is incremented (`200.22.0-dev` → `200.23.0-dev`), and
  the previous behavior is kept for that case.

`SameChildWrapperBaseVersion` in the `hydra` CI package compares
`BaseVersion()` (no environment suffix, no extra counter) of the old and new
child wrapper strings.

### Child App Versions

The base version is derived from the first **dependency version** in
`Chart.yaml`. A leading **`v` / `V`** on that semver (for example `v4.11.0` as
used by some upstream charts) is stripped before computing the wrapper. If the
dependency version contains a pre-release suffix (e.g.
`1.5.1-2b866935`), it is preserved as-is. The `release` pipeline appends the
environment suffix automatically.

Examples: `1.200.9-dev`, `42.0.0-dev`, `1.5.1-2b866935-dev`, `1.200.9-stage`,
`1.5.1-2b866935-stage`, `1.200.9`, `1.5.1-2b866935`.

If the version already exists (because `values.yaml` and/or `Chart.yaml` was
changed without updating the dependency), an extra version number is inserted
before the environment suffix: `x.y.z[-prerelease]-<extra>[-env]`

| Environment | Current Version        | Change Without Dependency | Another Change           |
| ----------- | ---------------------- | ------------------------- | ------------------------ |
| dev         | `1.2.3-dev`            | `1.2.3-1-dev`             | `1.2.3-2-dev`            |
| stage       | `1.2.3-stage`          | `1.2.3-1-stage`           | `1.2.3-2-stage`          |
| prod        | `1.2.3`                | `1.2.3-1`                 | `1.2.3-2`                |
| dev         | `1.5.1-2b866935-dev`   | `1.5.1-2b866935-1-dev`    | `1.5.1-2b866935-2-dev`   |
| stage       | `1.5.1-2b866935-stage` | `1.5.1-2b866935-1-stage`  | `1.5.1-2b866935-2-stage` |
| prod        | `1.5.1-2b866935`       | `1.5.1-2b866935-1`        | `1.5.1-2b866935-2`       |

### Example Child App Chart.yaml

```yaml
# ingress-nginx/dev/Chart.yaml
apiVersion: v2
name: ingress-nginx
type: application
version: 4.11.8-2-dev
dependencies:
  - name: ingress-nginx
    version: "4.11.8"
    repository: "https://kubernetes.github.io/ingress-nginx"
```text

```yaml
# ingress-nginx/prod/Chart.yaml
apiVersion: v2
name: ingress-nginx
type: application
version: 4.11.8
dependencies:
  - name: ingress-nginx
    version: "4.11.8"
    repository: "https://kubernetes.github.io/ingress-nginx"
```

```yaml
# ingress-nginx/prod/Chart.yaml (extra version when values.yaml was changed)
apiVersion: v2
name: ingress-nginx
type: application
version: 4.11.8-1
dependencies:
  - name: ingress-nginx
    version: "4.11.8"
    repository: "https://kubernetes.github.io/ingress-nginx"
```text

```yaml
# fluent-bit/dev/Chart.yaml (dependency with pre-release suffix)
apiVersion: v2
name: fluent-bit
type: application
version: 0.55.0-2b866935-dev
dependencies:
  - name: fluent-bit
    version: "0.55.0-2b866935"
    repository: "oci://ghcr.io/fluent/helm-charts"
```

```yaml
# fluent-bit/prod/Chart.yaml (pre-release, extra version)
apiVersion: v2
name: fluent-bit
type: application
version: 0.55.0-2b866935-1
dependencies:
  - name: fluent-bit
    version: "0.55.0-2b866935"
    repository: "oci://ghcr.io/fluent/helm-charts"
```text

### Version Flow Across Environments

```text
OCI registry (ghcr.io)   dev/                stage/               prod/
                         +-----------+       +-------------+      +---------+
  1.200.9 -- release --> |1.200.9-dev|       |1.198.3-stage|      | 1.195.0 |
  1.200.8                +-----------+       +-------------+      +---------+
  1.198.3                      |                    |                  |
  1.195.0                      |                    | promote     +---------+
                               |                    +---> MR ---> |1.198.3  |
                               |                                  +---------+
                               | promote     +-------------+           |
                               +---> MR ---> |1.200.9-stage|           |
                                             +-------------+           |
                                                    |                  |
                                                    | promote     +---------+
                                                    +---> MR ---> | 1.200.9 |
                                                                  +---------+
```

---
