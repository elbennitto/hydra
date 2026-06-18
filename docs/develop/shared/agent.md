# Shared Architecture Documentation

Concepts shared between hydra-go (CLI backend) and hydra-ui (web frontend).

## Topics

| Topic                                               | Description                                                                                          |
| --------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| [Context](context.md)                               | Hierarchical GitOps structure: clusters, root apps, child apps, path resolution, and values loading  |
| [Hydra YAML](hydra-yaml.md)                         | Serialized dependency graph format with entities, groups, references, and chart metadata             |
| [Grouping](grouping.md)                             | Algorithm that clusters related Kubernetes entities into logical groups for UI visualization         |
| [Values](values.md)                                 | How Helm values from 8 categories are composed and merged in ArgoCD child Application CRs            |
| [Secrets](secrets.md)                               | Secret and SopsSecret entity modeling, key metadata, consumer references, and UI display             |
| [Cluster Dump Structure](cluster-dump-structure.md) | Directory layout of the `hydra ui` export: per-cluster manifests, charts, values, and root app merge |

Each topic file contains a summary. For full implementation details, see the `details/` subdirectory.
