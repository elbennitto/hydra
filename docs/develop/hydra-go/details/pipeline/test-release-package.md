# Pipeline: Test, Release, and Publish

This file describes the validation, release, and packaging pipelines and how they connect through tags and chart metadata.

Back to [Pipeline detail index](../pipeline.md).

## Pipeline: `test`

### Purpose

Validates changed Helm charts before they are merged to `main` or
before `publish` uploads them to Harbor. Runs automatically on MR
creation, on every push to the feature branch, and on Git tags.

### Trigger

- MR is created (against `main`)
- New commit on a branch with an open MR
- Git tag `build-<date><time>`

### Self-Detection

```text
Build-tag based change detection:
  Changed files under */dev/    → test for these charts
  Changed files under */stage/  → test for these charts
  Changed files under */prod/   → test for these charts
  No chart changes              → pipeline exits immediately

Tag trigger build-<date><time>:
  Load app tags from release metadata → test for these charts
```

### Test Steps

1. **Detect changed charts:** Run the build-tag based change detection
   algorithm against paths configured in `.hydra-ci.yaml`
2. **For each changed chart:**
   - `helm lint` — syntax check
   - `helm template` — test rendering to catch template errors
3. **Result:** Pipeline status (OK/Error) is shown on the MR

### MR Integration

- `test` is configured as a **required check**
- The MR can only be merged once `test` passes
- Errors are directly visible as pipeline status on the MR

## Pipeline: `release`

### Purpose

Detects which environment directories (`dev/`, `stage/`, `prod/`) have
changed, updates versions, maintains the root app, and creates Git tags.
App tags serve as traceability/documentation.
Only `build-<date><time>` triggers the `publish` pipeline.

### Self-Detection

```text
Build-tag based change detection:
  Changed files under */dev/    → release for dev
  Changed files under */stage/  → release for stage
  Changed files under */prod/   → release for prod
  No changes                    → pipeline exits immediately
```

### Release Steps

1. **Detect changes:** For each child chart directory
   `apps/<group>/<app>/<env>/` (excluding `app == root`), determine whether
   files under that directory changed since the **last `build-*` tag whose
   release commit touched that directory** (`git.LastBuildTag`). If no such
   tag exists, compare against the **repository initial commit** instead
   (`git.InitialCommitHash` + `HasChanges`).
2. **For each changed child chart + environment:**
   - Read the **wrapper base semver** from `Chart.yaml`: the dependency whose
     `name` matches the chart `name`, otherwise the **first** dependency entry.
     If there are **no** dependencies, use the chart's own `version` parsed as
     a Hydra wrapper version and take **`BaseVersion()`** (major.minor.patch
     plus any Helm pre-release segment, without env suffix or extra counter) as
     the upstream semver input—so standalone charts still get correct `-dev` /
     `-stage` / prod suffixes from the same algorithm.
   - Compute the next wrapper `version` with `ComputeWrapperVersion` /
     `NextChildChartWrapperVersion` (same semver base as the dependency, plus
     optional extra counter `-1`, `-2`, … before the environment suffix when
     the canonical wrapper for that dependency+env already matches the current
     chart and files still changed). **Phase 1 (implemented):** collision
     detection uses the **current chart only** (no OCI registry lookup yet).
3. **Update root app:** For each affected `(group, env)` with at least one
   changed child:
   - Load `apps/<group>/root/<env>/`, set `apps.<child>.version` to each new
     wrapper version, then bump the root chart `version` **once** via
     `NextRootAppChartVersionAfterChildChanges`: if every child is an
     extra-counter-only re-release (`1.0.0-dev` → `1.0.0-1-dev`), increment
     the **third** component (`200.22.0-dev` → `200.22.1-dev`); if any child’s
     base semver or Helm pre-release segment changed (e.g. `1.0.0-dev` →
     `1.0.1-dev`), increment the **middle** component (`200.22.0-dev` →
     `200.23.0-dev`) as before. `BumpRootAppChartVersion` only performs the
     middle-component bump and remains available for other callers.
4. **Commit (local mode):** Single commit containing all updated
   `Chart.yaml` / `values.yaml` files.
5. **Create Git tags (local mode):** Lightweight tags at that commit:
   - One `AppTag` per changed child (`<group>-<child>-<full-wrapper-version>`)
   - One `RootAppTag` per affected root (`<group>-root-<full-root-version>`)
   - Exactly one `build-<UTCYYYYMMDDHHmm>` tag (UTC wall clock in the Hydra
     process; trigger for future `publish`)

**CLI modes:** `hydra ci run release --dry-run` logs planned versions and does not
modify the working tree. `hydra ci run release --local` performs the commit and
tags on `main` or on `--target-branch` when set. Full **CI** mode
(`hydra ci run release` without `--dry-run` / `--local`) is not yet wired to
remote push and remains a stub in code.

### Git Tag Formats

```text
Documentation tags:
  <root-app>-<child-app>-<version><suffix>
  <root-app>-root-<version><suffix>

Publish trigger tag:
  build-<date><time>

Suffix:
  -dev      for dev
  -stage    for stage
  (empty)   for prod

Examples:
  demo-service-ui-1.200.9-dev        (documentation)
  demo-service-ui-1.200.9-stage      (documentation)
  demo-service-ui-1.200.9            (documentation)
  demo-service-backend-1.5.1-2b866935-dev    (documentation, pre-release)
  demo-service-backend-1.5.1-2b866935-stage  (documentation, pre-release)
  demo-service-backend-1.5.1-2b866935        (documentation, pre-release)
  demo-root-200.23.0-dev             (documentation)
  build-202603051555                (trigger)
```

Documentation tags identify root app + child app + version + environment.
`build-<date><time>` identifies the concrete release run.
Note: A child chart is still named just `<child-app>` (e.g. `service-ui`);
the form `demo-service-ui` is only used in tags as a combination of root app
and child app.

### Scope

The pipeline applies uniformly to **all charts** in `charts-repository/`,
regardless of app group:

| App Group     | Path                    |
| ------------- | ----------------------- |
| demo           | `apps/demo/service-*/`   |
| demo-infra     | `apps/demo-infra/*/`     |
| cluster-infra | `apps/cluster-infra/*/` |
| cicd          | `apps/cicd/*/`          |
| argocd        | `apps/argocd/*/`        |

---

## Pipeline: `publish`

### Purpose

Builds Helm charts and uploads them to Harbor. Triggered by the build tag
created by the `release` pipeline.
Runs only when `test` on the same build tag was successful.

### Trigger

```text
Git tag matching: build-<date><time>
Example: build-202603051555
```

### Package Steps

1. **Parse build tag:** Extract timestamp from the tag
2. **Load release metadata:** List lightweight tag names that point to the same commit as `HEAD`, then keep every tag except `build-*` (the Hydra CLI uses the go-git repository opened from `ci.rootAppsPath`).
3. **Determine chart directories:** Map each documentation tag to `apps/<group>/<app>/<env>/` or `apps/<group>/root/<env>/` using `ci.appGroups` names (longest match first) and `ParseChartVersion` on the version suffix (including prod tags without `-dev`/`-stage`).
4. **Build charts (per app tag):**
   - Run `helm dependency update`
   - Run `helm package`
5. **Upload to Harbor:** Push charts to the registry configured in `.hydra-ci.yaml`
6. **Report status:** Log success/error

### App Tag → Directory Mapping

| Tag                                        | Directory                                |
| ------------------------------------------ | ---------------------------------------- |
| `demo-service-ui-1.200.9-dev`               | `apps/demo/service-ui/dev/`               |
| `demo-service-ui-1.200.9-stage`             | `apps/demo/service-ui/stage/`             |
| `demo-service-ui-1.200.9`                   | `apps/demo/service-ui/prod/`              |
| `demo-service-backend-1.5.1-2b866935-dev`   | `apps/demo/service-backend/dev/`          |
| `demo-service-backend-1.5.1-2b866935-stage` | `apps/demo/service-backend/stage/`        |
| `demo-service-backend-1.5.1-2b866935`       | `apps/demo/service-backend/prod/`         |
| `demo-root-1.200.9-dev`                     | `apps/demo/root/dev/`                     |
| `cluster-infra-ingress-nginx-4.11.0-dev`   | `apps/cluster-infra/ingress-nginx/dev/`  |
| `cluster-infra-ingress-nginx-4.11.0`       | `apps/cluster-infra/ingress-nginx/prod/` |

Note: App tags serve as documentation and as input for `publish`.
The `publish` trigger is exclusively `build-<date><time>`.

---
