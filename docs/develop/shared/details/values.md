# Values

How Helm values are composed in ArgoCD child Application CRs.

Reference: [ArgoCD Helm — Value Precedence](https://argo-cd.readthedocs.io/en/stable/user-guide/helm/#helm-value-precedence) — `parameters > valuesObject > values > valueFiles > chart values.yaml`

## Value Categories

Hydra uses 8 distinct value categories. Categories 1–5 enter the child app **directly** (as chart defaults or ArgoCD `valueFiles`). Categories 6–8 enter **indirectly** via the `valuesObject`, which has higher priority than `valueFiles`.

| #   | Category                | Repository             | Path Pattern                                                                      | How it enters child app                                                   |
| --- | ----------------------- | ---------------------- | --------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| 1   | upstream values         | chart tgz archive      | Bundled in Helm chart dependencies (e.g. ingress-nginx, fluent-bit)               | Helm chart defaults (lowest priority)                                     |
| 2   | app values              | charts-repository      | `charts-repository/apps/<root>/<child>/<stage>/values.yaml`                       | `valueFiles[0]` (first entry = `values.yaml` in chart dir)                |
| 3   | group values            | gitops-repository      | `gitops-repository/clusters/<provider>/values.yaml`                               | `valueFiles` (via `../../../../<path>/../values.yaml`)                    |
| 4   | context values          | gitops-repository      | `gitops-repository/clusters/<provider>/<context>/values.yaml`                     | `valueFiles` (via `../../../../<path>/values.yaml`)                       |
| 5   | cluster values          | gitops-repository      | `gitops-repository/clusters/<provider>/<context>/<cluster>/values.yaml`           | `valueFiles` (via `../../../../<path>/<cluster>/values.yaml`)             |
| 6   | root app default values | charts-repository      | `charts-repository/apps/<root>/root/<stage>/values.yaml`                          | Indirectly via `valuesObject` (pre-merged during root app Helm rendering) |
| 7   | root app values         | gitops-repository      | `gitops-repository/clusters/<provider>/<context>/<cluster>/<rootApp>/values.yaml` | Indirectly via `valuesObject` (pre-merged during root app Helm rendering) |
| 8   | hydra values            | infra_library template | Fallback defaults from `infra_library.fn.hydra.fallback`                          | Via `valuesObject` (`global.argocd: true`, etc.)                          |

## valueFiles

Each child Application's `spec.sources[].helm.valueFiles` lists value files in ascending priority (later entries win). This covers categories 2–5:

| Cat | Category       | Path in manifest                           | Resolved repo path                                                      |
| --- | -------------- | ------------------------------------------ | ----------------------------------------------------------------------- |
| 2   | app values     | `values.yaml`                              | `charts-repository/apps/<root>/<child>/<stage>/values.yaml`             |
| 3   | group values   | `../../../../<path>/../values.yaml`        | `gitops-repository/clusters/<provider>/values.yaml`                     |
| 4   | context values | `../../../../<path>/values.yaml`           | `gitops-repository/clusters/<provider>/<context>/values.yaml`           |
| 5   | cluster values | `../../../../<path>/<cluster>/values.yaml` | `gitops-repository/clusters/<provider>/<context>/<cluster>/values.yaml` |

`<path>` = `global.hydra.path` — the context path relative to the repo root (e.g. `clusters/cloud-poc/poc`, without provider prefix).

The `../../../../` navigates from `source.path` (the chart directory) back toward the repository root. `ignoreMissingValueFiles: true` is set, so missing files are skipped.

Category 1 (upstream values) enters as Helm chart defaults bundled in the `.tgz` archive, not via `valueFiles`.
Category 2 (app values) `values.yaml` is the main chart default file in the child app chart directory. It is also bundled in the chart `.tgz`, but since it is explicitly listed as `valueFiles[0]`, ArgoCD loads it from disk.

**Template source:** `charts-repository/shared/infra_library/dev/templates/_apps.tpl`

## valuesObject

The `valuesObject` is built dynamically by the `infra_library.template.app_of_apps` Helm template and has **higher priority than all valueFiles**. It carries categories 6–8.

### Composition

```text
1.  Start with app-specific values:    $.Values.<appName>       (e.g. service-auth.*)
2.  Deep-merge global values:          $.Values.global          (excluding global.hydra)
3.  Deep-merge argocd flag:            { global: { argocd: true } }
```

### Result structure

```yaml
valuesObject:
  global:
    # all global.* values from the Hydra hierarchy (group → context → cluster),
    # minus global.hydra (internal), plus global.argocd: true
    argocd: true
    baseUrl: https://...
    imagePullSecrets: [...]
  <appName>:
    # app-specific values from the Hydra hierarchy
    resources: ...
    configuration: ...
```text

### Sources of valuesObject content

The values inside `valuesObject` originate from the **same GitOps value files** as `valueFiles` (group, context, cluster), but they were already **merged by hydra-go/Helm** during template rendering. The `valuesObject` therefore contains the pre-resolved result of the entire Hydra hierarchy.

Additionally, the `valuesObject` includes:

- **Root app default values** (category 6) — from the root app chart directory
- **Root app values** (category 7) — from the gitops-repository root app directory
- **Hydra values** (category 8) — fallback defaults injected by the infra_library template

Because `valuesObject` has higher ArgoCD priority than `valueFiles`, these values **always win** over anything in the value files. This means: app-specific values and global config from the GitOps hierarchy cannot be overridden by a chart's own `values.yaml`.

**Template source:** `charts-repository/shared/infra_library/dev/templates/_apps.tpl`

## hydra-go Export

How each value category maps to the hydra-go export:

| Category                | hydra-go export type           | Notes                                                                                                            |
| ----------------------- | ------------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| upstream values         | Chart tgz archive              | Not in `valueFiles` export; loaded from `.tgz`                                                                   |
| app values              | `type: "app"` with child appId | Extra valueFiles from child app chart dir. Main `values.yaml` is also in chart tgz.                              |
| group values            | `type: "group"`                | From gitops-repository                                                                                           |
| context values          | `type: "context"`              | From gitops-repository                                                                                           |
| cluster values          | `type: "cluster"`              | From gitops-repository                                                                                           |
| root app default values | Chart tgz archive              | Bundled in root app chart tgz; not separately exported. UI loads from root app chart archive.                    |
| root app values         | `type: "app"` with root appId  | From gitops-repository, root app dir. **Current:** `type: "app"` with root appId. **Planned:** `type: "rootApp"` |
| hydra values            | `fallbackValues`               | Separate export, not in `valueFiles`                                                                             |

## Hydra Globals

The merged values contain `global.hydra.*` fields used by hydra-ui for link building and path resolution:

| Field                     | Usage                                                                                                                                           |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `global.hydra.repository` | Git remote URL — used to build GitHub/GitLab file links (fallback: `gitRemote` from hydra.yaml export)                                          |
| `global.hydra.revision`   | Git branch/revision — used as branch in file links (fallback: `gitBranch` from hydra.yaml export)                                               |
| `global.hydra.stage`      | Stage name (`dev`, `stage`, `prod`) — last directory segment in charts-repository paths (e.g. `charts-repository/apps/<root>/<child>/<stage>/`) |
| `global.hydra.path`       | Context path relative to repo root (e.g. `clusters/cloud-poc/poc`) — used to resolve relative `valueFiles` paths                                  |

## hydra-ui Tree Categories

How each value category should appear in the hydra-ui values tree:

| Category                | Tree location                             | Tag                 | Color  |
| ----------------------- | ----------------------------------------- | ------------------- | ------ |
| upstream values         | Chart tgz archive section                 | `upstream values`   | violet |
| app values              | charts-repository tree / `valueFiles[0]`  | `app values`        | cyan   |
| group values            | gitops-repository tree                    | `group values`      | green  |
| context values          | gitops-repository tree                    | `context values`    | amber  |
| cluster values          | gitops-repository tree                    | `cluster values`    | rose   |
| root app default values | charts-repository tree (derived path)     | `root app defaults` | indigo |
| root app values         | gitops-repository tree / root app section | `root app values`   | blue   |
| hydra values            | Separate top-level node                   | `hydra values`      | slate  |
