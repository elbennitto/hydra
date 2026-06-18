# Refs

Refs model dependencies between Kubernetes resources as directed edges in Hydra's dependency graph.

## Contents

- [Ref Types](ref-types.md) — direct, indirect, runtime, regarding
- [Ref Parsers](ref-parsers.md) — How dependencies are discovered from manifests
- [Ref Tags](ref-tags.md) — Behavioral markers: backup, uninstall, bootstrap-guard
- [Ref Labels](ref-labels.md) — Semantic categories: namespace, crd, controller, owner
- [Ref Attributes](ref-attributes.md) — Origin tracking and metadata
- [Writing Custom Refs](writing-custom-refs.md) — Creating your own ref-parsers

## Overview

A **ref** is a directed edge from one Kubernetes resource to another, representing a dependency:

```
Source ──ref──▶ Target
(depends on)
```

Refs enable Hydra to:
- Apply resources in dependency order
- Uninstall resources in reverse dependency order
- Include related resources in backups
- Visualize relationships in the TUI

## Quick Example

A Deployment depends on its ServiceAccount:

```
apps/v1/Deployment/default/myapp
  ──[direct, label:serviceaccount]──▶ v1/ServiceAccount/default/myapp
```

## Visualizing Refs

```bash
# List transitive references (local)
hydra local refs prod apps/v1/Deployment/default/myapp

# List transitive references (with cluster state)
hydra gitops refs prod apps/v1/Deployment/default/myapp

# Interactive TUI
hydra local inspect prod
hydra gitops inspect prod
```

## Prerequisites

- [Concepts: Dependency Graph](../concepts/dependency-graph.md)
- [CEL](../cel/) — CEL expressions are used in ref-parsers
