<!-- markdownlint-disable MD013 -->

# Concepts

Core concepts that underpin Hydra. Read this to understand the mental model before diving into commands or configuration.

## Contents

- [Context and Clusters](context-and-clusters.md) — HYDRA_CONTEXT, clusters, kubernetes context mapping
- [Context Resolution](context-resolution.md) — How `group` and `context` are inferred from `values.yaml`
- [App Model](app-model.md) — Root apps, child apps, App IDs
- [Repositories](repositories.md) — Charts repository vs GitOps repository
- [Dependency Graph](dependency-graph.md) — Nodes, edges, topological ordering
- [GitOps Workflow](gitops-workflow.md) — Git → Render → ArgoCD → Cluster
- [Hydra ConfigMaps](hydra-configmaps.md) — Dynamic configuration layering
- [Cluster Command Data Model](cluster-command-data-model.md) — How `hydra gitops show`, `untracked`, `system`, and related commands build and enrich their shared cluster view
- [Clones and TemplatePatches](clones.md) — Runtime resource copying and post-render mutations
- [Bootstrap](bootstrap.md) — Solving the chicken-and-egg problem

## Prerequisites

- [Introduction: What Is Hydra?](../introduction/what-is-hydra.md)
