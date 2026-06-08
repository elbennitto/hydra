# Presets

Presets recognize cluster infrastructure components that are not managed by Hydra apps but exist as part of the Kubernetes distribution.

## Contents

- [Builtin Presets](builtin-presets.md) — All ~30 builtin presets and their purpose
- [Preset Structure](preset-structure.md) — YAML format, predicates, optional markers
- [Preset Overrides](preset-overrides.md) — Customizing presets via global.hydra.presets
- [Cluster Overrides](../configuration/cluster-overrides.md) — Manually assign live resources to preset apps or normal apps via `hydra.yaml`
- [Preset Activation](preset-activation.md) — Activation logic, chains, debugging

## Overview

A Kubernetes cluster contains many resources that are **not** deployed by your apps — CoreDNS, kube-proxy, flannel, metrics-server, etc. These are part of the cluster infrastructure.

Without presets, these resources would appear as **untracked** in `hydra gitops untracked`. Presets tell Hydra: "these resources belong to the cluster infrastructure."

## How Presets Work

1. Each preset defines **predicates** (IDs or CEL expressions) that match infrastructure resources.
2. Hydra represents each enabled preset as a preset app, for example `in-cluster.preset.coredns`.
3. Preset anchors and matches assign resources to that preset app in the shared resource model.
4. Child resources are then attached through the same owner-reference and generic-ref logic used for normal apps.

## Inspecting Presets

```bash
# Show which presets match against live cluster state
hydra gitops system prod

# Include disabled presets
hydra gitops system prod --all
```

## Prerequisites

- [Concepts: Dependency Graph](../concepts/dependency-graph.md)
- [CEL](../cel/) — CEL expressions are used in preset predicates
