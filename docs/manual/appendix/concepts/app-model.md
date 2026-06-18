# App Model

How Hydra organizes applications into a hierarchy of clusters, groups, and apps.

## App ID Format

Every application has a unique identifier:

```
<cluster>.<group>.<app>
```

Examples:
- `prod.cluster-infra.ingress-nginx`
- `test.demo.service-auth`
- `cicd.cicd.gitlab-runner`

## Root Apps

A **root app** represents an app group (e.g., `cluster-infra`, `demo`, `cicd`). It:

- Lives in the charts-repository under `apps/<group>/root/`
- Generates ArgoCD `Application` resources for each child app in the group
- Is identified by two segments: `<cluster>.<group>` (e.g., `prod.cluster-infra`)

## Child Apps

A **child app** is a single Helm chart within a group. It:

- Lives in the charts-repository under `apps/<group>/<app>/`
- Is deployed as an individual ArgoCD Application
- Contains its own `Chart.yaml`, `templates/`, and `values.yaml`
- Is identified by three segments: `<cluster>.<group>.<app>`

## App Selection with Glob Patterns

Commands that accept App IDs support glob patterns:

| Pattern | Meaning |
|---------|---------|
| `prod.*` | All root apps on prod |
| `prod.cluster-infra.*` | All child apps in cluster-infra on prod |
| `prod.**` | All apps on prod (recursive) |
| `*.cluster-infra.*` | All cluster-infra apps across all clusters |

Use `--exclude-app <pattern>` to exclude specific apps from the selection.

See [Commands: App ID Patterns](../commands/app-id-patterns.md) for the full syntax reference.

## See Also

- [Context and Clusters](context-and-clusters.md)
- [Repositories](repositories.md)
