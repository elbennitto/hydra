# charts-repository Directory Structure

This page walks through the structure of a real chart to explain how application packages are built.

## Overall Layout

Each application group follows the same pattern:

```tree
charts-repository/apps/
    <group>/                  # e.g., cluster-infra, demo-infra, demo
        root/                 # The root app chart (generates ArgoCD child Applications)
            dev/
                Chart.yaml
                values.yaml
                charts/
                    infra_library     # Shared Helm library (symlink)
                templates/
    │
        <child-app>/          # Each child app has its own chart
            dev/
                Chart.yaml
                values.yaml
                templates/    # (optional, some charts are pure wrapper charts)
    │
        <another-child-app>/
            dev/
```

The `dev/` subdirectory exists because charts can have different versions. Currently all charts use `dev/`.

## Anatomy of a Root App Chart

Using `cluster-infra/root` as the example:

```tree
charts-repository/apps/cluster-infra/root/dev/
    Chart.yaml                            # Chart metadata
    values.yaml                           # Default values: which child apps are enabled
    charts/
        infra_library                     # Shared Helm library (symlink)
    templates/                            # (no templates shown - they're in infra_library)
```

### Chart.yaml

```yaml
apiVersion: v2
name: cluster-infra
version: 0.1.0
dependencies:
  - name: infra_library
    version: "0.1.0"
    repository: "file://charts/infra_library"
```

The root chart depends on `infra_library`, which provides shared templates for generating ArgoCD Application resources.

### values.yaml (root app)

The root app's values control which child apps are enabled:

```yaml
cert-manager:
  enabled: true
ingress-nginx:
  enabled: true
kube-prometheus-stack:
  enabled: true
dex:
  enabled: true
kyverno:
  enabled: true
sops-secrets-operator:
  enabled: true
fluent-bit:
  enabled: false        # Disabled by default, can be enabled per cluster
```

Per-cluster overrides in `gitops-repository/` can enable or disable individual apps.

## Anatomy of a Child App Chart

Using `cluster-infra/cert-manager` as the example:

```tree
charts-repository/apps/cluster-infra/cert-manager/dev/
    Chart.yaml                # Chart metadata + upstream dependency
    values.yaml               # Default configuration for cert-manager
```

### Chart.yaml (child app)

```yaml
apiVersion: v2
name: cert-manager
version: 0.1.0
dependencies:
  - name: cert-manager
    version: "1.17.1"
    repository: "https://charts.jetstack.io"
```

This chart wraps the **upstream** cert-manager chart (version 1.17.1 from the official Jetstack repository). The wrapper pattern lets you add defaults and customizations while still using the official chart.

Some charts are pure wrappers (only `Chart.yaml` + `values.yaml`), while others add their own templates for additional Kubernetes resources.

### values.yaml (child app)

Default configuration values. These get merged with cluster-specific values from `gitops-repository/`:

```yaml
cert-manager:
  installCRDs: true
  prometheus:
    enabled: true
  webhook:
    timeoutSeconds: 30
```

## Charts With Custom Templates

Some child apps add their own Kubernetes resources via `templates/`:

```tree
charts-repository/apps/cluster-infra/kyverno/dev/
    Chart.yaml
    values.yaml
    templates/
        clusterpolicy-attach-imagePullSecrets.yaml      # Auto-attach registry credentials
        clusterpolicy-generate-clone-image-pull-secret-policy.yaml
        kyverno-rbac-harbor-secret.yaml                 # RBAC for secret access
```

These templates create custom Kubernetes resources alongside the upstream chart.

## Charts With Test Data

Some charts include test data for Hydra's unit testing:

```tree
charts-repository/apps/cluster-infra/kube-prometheus-stack/dev/
    Chart.yaml
    values.yaml
    templates/
        grafana-dashboards.yaml
    dashboards/                 # Grafana dashboard JSON files
        ArgoCD/
        Cluster-Infra/
        Demo/
        Demo-Infra/
        Logs/
    test/
        refs/                   # Golden file tests for reference parsing
            optional-monitoring.given.yaml
            optional-monitoring.expected.yaml
```

## The infra_library

The `infra_library` is a shared Helm library chart used by all root apps. It provides:

- Templates for generating ArgoCD Application resources (the App of Apps pattern)
- Value merging logic
- Common helpers

It appears as a symlink `charts/infra_library` in each root app's directory.

## How Values Flow Through a Chart

When Hydra renders `in-cluster.cluster-infra.cert-manager` for the example-dev cluster:

```text
1. charts-repository/apps/cluster-infra/cert-manager/dev/values.yaml
   (chart defaults: installCRDs=true, prometheus=true)
      │
      ▼  merged with
2. gitops-repository/clusters/test/values.yaml
   (test group values)
      │
      ▼  merged with
3. gitops-repository/clusters/test/example-dev/values.yaml
   (example-dev cluster values)
      │
      ▼  merged with
4. gitops-repository/clusters/test/example-dev/in-cluster/values.yaml
   (shared values for all root apps)
      │
      ▼  merged with
5. gitops-repository/clusters/test/example-dev/in-cluster/cluster-infra/values.yaml
   (cluster-infra root app values)
      │
      ▼  final merged values used to render
6. The cert-manager Helm templates → Kubernetes YAML
```

Each level can override values from the previous level. Maps are deep-merged (nested keys are combined), while scalar values (strings, numbers) are replaced.

## Next Steps

- [Migration from kubectl and Helm](../migration/from-kubectl-and-helm.md) — how Hydra compares to manual workflows
- [Cluster Lifecycle](../operations/cluster-lifecycle.md) — the full journey from empty cluster to running apps
