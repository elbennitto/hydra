# Defining Hydra Apps

This section describes how **applications** are modeled for Hydra: what a root app and a child app are, how they map to **App IDs**, which repository holds which responsibilities, and which **Hydra-specific values** (such as namespace ownership) belong in Helm values.

For concrete directory trees in your deployment repositories, see the [Charts Repository](concepts/charts-repository.md) and [GitOps Repository](concepts/gitops-repository.md) manuals. This page stays **catalog-agnostic**: examples use placeholders such as `my-platform` and `my-worker`.

## App IDs: root and child

Hydra identifies workloads using **App IDs** derived from the GitOps layout:

- A **root app** corresponds to a directory immediately below the cluster’s `in-cluster/` folder (for example the ArgoCD “app of apps” chart for a whole stack).
- A **child app** is one deployable unit managed under that root (often one Helm release).

The usual string form is:

```text
<cluster-directory>.<root-app-name>.<child-app-name>
```

So for cluster `in-cluster`, root `my-platform`, and child `my-worker`, Hydra refers to `in-cluster.my-platform.my-worker`. The exact spelling of the cluster segment comes from your GitOps context layout; see [GitOps Repository / Directory Structure](gitops-repository/directory-structure.md) for how paths map to IDs.

## Two layers: chart catalog vs cluster GitOps

Hydra renders Helm charts that live in a **shared chart catalog** and merges **cluster-specific values** from the **GitOps repository**:

| Layer | Role |
| --- | --- |
| Chart catalog | Defines reusable charts: root chart (lists child apps, uses shared library templates) and per-child charts (workload + optional subchart dependencies). |
| GitOps repository | Pins each cluster: wrapper `Chart.yaml` pointing at the catalog chart, `values.yaml` layers, optional `apps/<child>/` files for secrets and backups. |

The GitOps wrapper chart for a root app typically contains a small `templates/apps.yaml` that **includes** the shared **app-of-apps** template from the catalog’s library chart. That template emits ArgoCD `Application` and `AppProject` objects for each enabled child. Cluster operators therefore rarely hand-write those manifests.

## Defining a new root app (checklist)

1. **In the chart catalog**, add a **root** chart for the stack (for example `apps/<stack>/root/<stage>/`):
   - `Chart.yaml` declares a dependency on the shared **infra library** chart (the library that ships `infra_library.template.app_of_apps`).
   - `values.yaml` lists child apps under a top-level **`apps`** map. When this chart is used as a Helm dependency whose **name matches the root app**, merged values expose that map as **`<rootAppName>.apps`**, which is what the shared app-of-apps template reads—mirror existing root charts in your catalog.
   - Each child entry typically sets at least **namespace**, **enabled**, and optional **AppProject** overrides (`clusterResourceWhitelist`, `namespaceResourceWhitelist`, **additional `sourceRepos`** when a child chart pulls dependencies from an external Helm repository).

2. **In the GitOps repository**, under `…/<cluster>/in-cluster/`, add a **directory** named like the root app containing:
   - `Chart.yaml` with **exactly one** `dependencies` entry pointing at the catalog root chart (usually via a `file://` URI relative to the monorepo).
   - `charts/<dependency-name>` as a symlink or vendored copy so local `helm dependency build` resolves.
   - `templates/apps.yaml` containing only the library include that renders the app-of-apps resources.
   - `values.yaml` for overrides for that cluster (and optional `apps/<child>/` for SOPS and backup manifests).

Hydra discovers root apps by listing **directories** under the cluster’s `in-cluster/` path (dot-prefixed directories are ignored). Adding a new directory is therefore enough for Hydra to treat a new root app as part of the cluster.

ArgoCD itself is often wired with an **ApplicationSet** that scans the same Git paths and creates one Argo `Application` per root-app directory. After you add a root folder and push, the next sync can materialize that root application without a one-off Application manifest.

## Defining a new child app (checklist)

1. **In the chart catalog**, add a chart directory for the child (for example `apps/<stack>/<child>/<stage>/`):
   - `Chart.yaml` may declare **dependencies** on upstream charts (community or internal).
   - `values.yaml` holds defaults for those dependencies and for **Hydra** (see below).

2. **In the root chart’s values** (catalog), register the child under the app-of-apps list:
   - Set **namespace** to the Kubernetes namespace where the workload should run.
   - If the child chart uses `helm dependency` against an **external Helm index or OCI registry**, ensure the generated **AppProject** allows that repository URL (the shared template can merge `spec.sourceRepos` from the child’s app config). Otherwise ArgoCD may refuse to render the chart.

3. **GitOps**: only if this cluster needs non-default values or secrets, extend the root’s `values.yaml` and/or `apps/<child>/` as you do for existing children.

## Values that Hydra reads (`global.hydra`)

Helm values under **`global.hydra`** configure Hydra behavior (refs, clones, diff, backups, and more). Two areas matter especially when **defining** apps:

### `global.hydra` must exist when templating root charts locally

The app-of-apps template merges **fallback Hydra metadata** with **`global.hydra`** from merged values. If you run `helm template` on a GitOps root chart **without** passing any file that defines `global.hydra`, Helm can fail with a nil-pointer-style error on `.Values.global.hydra`. In real workflows, Hydra (or your value layers) supplies `global.hydra`; a minimal `in-cluster/values.yaml` that sets `global.hydra` is enough for manual templating.

### Namespace ownership (`ownerNamespaces`)

Declare namespaces your app **owns** explicitly when Hydra must resolve **clone targets**, **backups**, or other workflows and a namespace could otherwise look **ambiguous** (multiple apps deploying into the same namespace).

In the **child** app chart’s `values.yaml` (merged into that release), add:

```yaml
global:
  hydra:
    ownerNamespaces:
      - example-namespace
```

Rules:

- Use the **real Kubernetes namespace** name (the same string you set as `namespace` for that child in the root app list).
- Each namespace should be claimed by **at most one** app on a cluster; duplicate declarations are a configuration error.

See also [hydra gitops apply](commands/cluster/apply.md) for how declared owners interact with clone resolution.

### Optional: AppProject `sourceRepos` for external chart dependencies

When a child chart’s `Chart.yaml` references a **third-party Helm repository**, ArgoCD’s **AppProject** for that child must list that repository URL under `spec.sourceRepos` (in addition to your main Git repository). The catalog root chart’s per-child **app-project** values are the usual place to add that allowance. This is independent of Hydra’s own config but required for GitOps sync to succeed.

## Child-specific overrides from the root chart

The shared app-of-apps template can inject **per-child** values into the ArgoCD `Application` (for example everything under a top-level key matching the child name in the root chart’s `values.yaml`). Use that mechanism to pass structured overrides without duplicating entire value files—mirror patterns already used by sibling child apps in your catalog.

## Related reading

- [GitOps Repository](concepts/gitops-repository.md) — value layering and root vs `in-cluster/` layout
- [Charts Repository](concepts/charts-repository.md) — catalog layout and relationship to GitOps
- [App of Apps tutorial](tutorial/04-app-of-apps.md) — pedagogical introduction to generated Applications
- [hydra gitops apply](commands/cluster/apply.md) — clone rules and `ownerNamespaces`
- [hydra local config](commands/local/config.md) — how Hydra merges `ownerNamespaces` with inferred data
- [Hydra values / architecture](../develop/shared/values.md) (developer docs) — deeper detail on `valuesObject` and categories
