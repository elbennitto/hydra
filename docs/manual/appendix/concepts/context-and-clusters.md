# Context and Clusters

How Hydra identifies clusters and connects to the Kubernetes API.

## HYDRA_CONTEXT

The `HYDRA_CONTEXT` environment variable points to a directory containing one subdirectory per cluster:

```tree
$HYDRA_CONTEXT/
    dev/           ← cluster "dev"
    cicd/          ← cluster "cicd"
    team1/         ← cluster "team1"
    team2/         ← cluster "team2"
```

Each subdirectory is a **cluster name** in Hydra. All commands that accept a cluster name resolve it relative to `HYDRA_CONTEXT`.

Validation rules:

- Hydra uses `global.hydra.type` to resolve hierarchy levels (`group`, `context`, `cluster`, `root-app`, `child-app`).
- At least one level must define `global.hydra.type`.
- Levels below a typed parent are inferred automatically.
- `global.hydra.parent: false` stops parent lookup at that level.
- Parent lookup defaults to `true`, except `group` which defaults to `false`.

For the detailed `group` vs `context` resolution model and troubleshooting examples, see [Context Resolution](context-resolution.md).

## What Is a Cluster?

A cluster in Hydra is:

- A subdirectory in `HYDRA_CONTEXT` containing configuration (values, root apps, secrets)
- A target for rendering and deployment
- A mapping to one specific Kubernetes API endpoint

A cluster contains root apps, which in turn contain child apps.

## Kubernetes Context Mapping

Hydra needs to know which kubectl context to use for each cluster. This is resolved in two steps:

### 1. User Config Mapping (`~/.config/hydra/config.yaml`)

```yaml
contexts:
  - path: /path/to/gitops-repository/clusters/development/dev
    config: ~/.kube/development.conf
    name: development-dev-admin
```

Each entry maps:
- `path` — The cluster directory (absolute path)
- `config` — Which kubeconfig file to use
- `name` — Which context name within that kubeconfig

### 2. Fallback: Default Kubeconfig

If no mapping exists in `config.yaml`, Hydra uses the default kubeconfig (`~/.kube/config`) with the currently active context.

### 3. Validation

The cluster's values define which contexts are allowed:

```yaml
global:
  hydra:
    kubectl:
      allowedContexts:
        - name: prod-admin
          cluster: development-dev-api-server
          authInfo: development-dev-user
```

`hydra gitops validate-current-context dev` verifies the current context matches this allowlist. This prevents accidentally running commands against the wrong cluster.

## The Hierarchy

```
HYDRA_CONTEXT
      Cluster (directory)
            Root App (ArgoCD Application)
                  Child App (Helm chart)
                        Kubernetes Resources
```

- **Context** — The workspace directory containing all clusters
- **Cluster** — One Kubernetes cluster with its configuration
- **Root App** — Generates ArgoCD Applications for a group of child apps
- **Child App** — A single Helm chart deployed to the cluster

## See Also

- [Configuration: HYDRA_CONTEXT](../configuration/hydra-context.md)
- [Configuration: config.yaml](../configuration/config-yaml.md)
- [Configuration: Kubernetes Context](../configuration/kubernetes-context.md)
- [App Model](app-model.md)
