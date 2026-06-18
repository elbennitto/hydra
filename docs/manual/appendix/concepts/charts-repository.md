# Charts Repository

The `charts-repository/` directory contains the shared Helm charts that
Hydra renders from. Think of it as the application catalog for your
infrastructure: it defines what apps look like, while the
`gitops-repository/` defines where and how they run.

Hydra uses this repository for local rendering, review, export, and as
the chart source behind cluster operations.

## What's Inside

```tree
charts-repository/
    .hydra-ci.yaml
    apps/
        argocd/
        cicd/
        cluster-infra/
        demo-infra/
        demo/
        unit-test/
    shared/
        infra_library/
```

The `apps/` directory contains the charts for root apps and child apps.
The `shared/infra_library/` chart provides shared templating logic that
root apps depend on. `.hydra-ci.yaml` defines CI/CD automation used by
`hydra ci`. For a step-by-step **release** and **promote** flow (including
version examples such as extra counters and dependency bumps), see
[`hydra ci` — Workflow: Release and promote](../commands/ci/README.md#workflow-release-and-promote).

## How It Works

Charts in this directory are shared across clusters. Cluster-specific
configuration comes from the `gitops-repository/`, so one chart can be
reused by many clusters with different values.

When you change a chart in `charts-repository/`, that change becomes part
of the desired state for every cluster that references it once it is
rendered and deployed.

## Relationship to the GitOps Repository

```text
charts-repository/                          gitops-repository/
(What apps look like)                       (Where apps run)

apps/cluster-infra/cert-manager/           clusters/test/example-dev/in-cluster/cluster-infra/
  Chart.yaml (dependencies)                  Chart.yaml (points to the chart)
  values.yaml (defaults)                     values.yaml (cluster-specific overrides)
  templates/ (Kubernetes templates)          apps/cert-manager/ (backup and secret files)
```

The `Chart.yaml` in `gitops-repository/` references a chart in
`charts-repository/` with a `file://` dependency:

```yaml
dependencies:
  - name: cluster-infra
    repository: "file://../../../../charts-repository/apps/cluster-infra/root/dev"
```

## Main Concepts

- **Dynamic ref-parsers in cluster** — Charts may ship a `ConfigMap` with
  annotation `hydra-gitops.org/hydra-config: "true"` and a `data.hydra` field containing a
  `refs` map in the same shape as `global.hydra.refs`. For `hydra gitops *`
  commands, Hydra merges those rules from the **full-cluster Helm template
  catalog** in your Git checkout (not by reading ConfigMaps back from the API
  for apply/uninstall). See [Hydra ConfigMaps](../hydra-configmaps.md) for how
  live cluster objects relate to documentation and tooling.
- **`origin:generated` and other ref metadata in YAML** — Prefer declaring **`"origin:generated": job`** / **`"origin:generated": controller`**
  on each `ref-parsers` entry’s `attributes` list (or on the whole ref group), not via CEL
  `refBuilder()...attribute("origin:generated", ...)` inside `pick` `cel` strings. Prefer YAML **`tag`**, **`label`**, and **`reverse`** on the ref-parser row or on each **`pick`** item instead of CEL `.tag()`, `.label()`, and `.reverse()` when the value is the same for every ref from that scope (built-in parsers keep `.tag('optional:ref')` in CEL only on optional branches). See
  [Hydra values / refs](../../develop/hydra-go/details/values.md) and [ref-parser format](../../develop/hydra-go/details/references.md#ref-parser-format) for the full convention.
- A root app chart such as `apps/<root-app>/root/<stage>/` collects the
  app-of-apps style configuration for one logical group.
- A child app chart such as `apps/<root-app>/<child-app>/<stage>/`
  contains the chart inputs for one deployable workload.
- Stage directories such as `dev`, `stage`, and `prod` separate
  stage-specific chart inputs and versions.
- `shared/infra_library/<stage>/` contains shared templates used by root
  apps.

## Application Groups

### `argocd`

ArgoCD is special because it manages itself. This chart installs ArgoCD
and configures its server, RBAC, AppProjects, the root AppSet, and
repository credentials.

### `cluster-infra`

This group contains shared cluster services such as cert-manager,
ingress-nginx, external-dns, Dex, monitoring, logging, policy
enforcement, and secret handling.

### `demo-infra`

This group contains platform services that Demo workloads depend on, such
as Kafka, PostgreSQL, ClickHouse, ActiveMQ, ingress, and shared secrets.

### `demo`

This group contains the Example application platform application services
themselves, such as authentication, configuration, device lifecycle,
monitoring, messaging, and the UI.

### `cicd`

This group contains CI/CD support components such as the GitLab runner
and the NFS CSI driver.

## What Hydra Reads From Here

- Child app defaults from `apps/<root-app>/<child-app>/<stage>/values.yaml`
- Root app defaults from `apps/<root-app>/root/<stage>/values.yaml`
- Shared templating logic from `shared/infra_library/<stage>/`
- Chart metadata and dependencies from each `Chart.yaml`

## Related Reading

- [Directory Structure](directory-structure.md) - detailed chart layout
- [GitOps Repository](../concepts/gitops-repository.md) - cluster-specific
  values and backup manifests
- [CLI](../cli/README.md) - commands that render or inspect these charts
- [CLI Quickstart](../cli/quickstart.md) - first safe workflow using the
  CLI
