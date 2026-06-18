# GitOps Repository

The `gitops-repository/` directory defines which clusters exist and what
they should run. It is the cluster-side blueprint of your infrastructure:
it selects clusters, contexts, and root apps, and provides the values and
backups that Hydra layers on top of chart defaults.

## What's Inside

Hydra primarily reads from `clusters/`:

```tree
gitops-repository/
    clusters/
        <provider>/
            values.yaml
            <context>/
                values.yaml
                <cluster>/
                    values.yaml
                    <root-app>/
                        values.yaml
                        apps/
                            <child-app>/
                                backup-<namespace>-<name>.sops.yaml
```

This documentation focuses on `clusters/`, because that is the repository
structure Hydra uses for rendering and cluster operations.

## How It Works

Each cluster directory defines three main things:

1. Which applications should be installed
2. Which configuration values should be used
3. Which secrets and backup manifests belong to that cluster

Hydra reads this structure and translates it into Kubernetes resources
for review, export, backup handling, and deployment workflows.

## Value Layers

Hydra reads values from this repository in ascending scope:

1. `clusters/<provider>/values.yaml` for group-wide defaults
2. `clusters/<provider>/<context>/values.yaml` for context-wide defaults
3. `clusters/<provider>/<context>/<cluster>/values.yaml` for
   cluster-specific values
4. `clusters/<provider>/<context>/<cluster>/<root-app>/values.yaml` for
   root app values

These layers are combined with the chart defaults from the
[Charts Repository](../concepts/charts-repository.md).

A concrete example looks like this:

```tree
gitops-repository/clusters/
    test/
        values.yaml
        example-dev/
            values.yaml
            in-cluster/
                values.yaml
                argocd/
                    values.yaml
                cluster-infra/
                    values.yaml
```

The effective values for `argocd` on that cluster are built from:
`test/values.yaml` + `example-dev/values.yaml` + `in-cluster/values.yaml` +
`argocd/values.yaml`.

## Cluster Groups

Clusters are often organized into provider or environment groups such as
test, CI/CD, management, or cloud-specific deployments. The
`clusters/<provider>/` layer is where shared defaults for each group live.

## Root Applications

Inside each cluster, the `in-cluster/` directory typically contains root
applications. Each root app is an ArgoCD Application that manages a
group of related child apps such as:

- `argocd` for ArgoCD itself
- `cluster-infra` for shared platform services
- `cicd` for CI/CD components
- `demo-infra` for data stores and platform dependencies
- `demo` for application services

## Secrets and Backups

Files ending in `.sops.yaml` are encrypted secrets or backup manifests.
Common examples include image pull credentials, OAuth client secrets,
database secrets, and backup files restored during cluster workflows.

Per-app backup manifests typically live below the root app directory:

```text
gitops-repository/clusters/<provider>/<context>/<cluster>/<root-app>/apps/<child-app>/backup-<namespace>-<name>.sops.yaml
```

Hydra uses those manifests for `hydra gitops backup`,
`hydra gitops uninstall`, and the backup restore phase of
`hydra gitops apply`.

## Related Reading

- [Defining Hydra Apps](../defining-hydra-apps.md) - App IDs, root vs child
  apps, and Hydra Helm conventions (`global.hydra`, `ownerNamespaces`)
- [Directory Structure](directory-structure.md) - detailed annotated tree
  of the real layout
- [Charts Repository](../concepts/charts-repository.md) - chart defaults and
  shared templates
- [CLI](../cli/README.md) - commands that render and apply this
  configuration
- [CLI Quickstart](../cli/quickstart.md) - first safe workflow using the
  CLI
