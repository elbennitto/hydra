# Getting Started

This tutorial series introduces Hydra from the perspective of the CLI modes you use day to day.

Helm knowledge is assumed. Core Helm concepts such as chart `values`, template rendering, and `dependencies` are used throughout the tutorial, but not explained separately here.

## Hydra Modes

Hydra has three operating modes:

- `local` — work only with the local GitOps directory
    - inspect rendered manifests for Hydra apps
    - inspect Helm template sources and computed values
- `gitops` — work with the local GitOps directory and a live Kubernetes connection
    - install, update, and uninstall Hydra apps
- `cluster` — work only against the current `kubectl` context
    - these commands do not use a local GitOps directory

This tutorial covers the `local` commands only.

Several steps include a **recorded terminal demo** — a playable recording of the commands and output in your browser. Installation calls these *Demo Videos*; in the chapters below they appear inline after each step.

## HYDRA_CONTEXT

`HYDRA_CONTEXT` tells Hydra which GitOps directory to read. See [Concepts: Context and Clusters](../../appendix/concepts/context-and-clusters.md) for the full model.

Unlike `KUBECONFIG`, the target of `HYDRA_CONTEXT` is a directory in your GitOps repository. The context contains one subdirectory per cluster.

ArgoCD is optional and is not covered in this tutorial. If you use ArgoCD, one context may also contain an ArgoCD installation that manages the other clusters.

One context can contain multiple clusters, for example all `dev` servers, with `team1`, `team2`, and `team3` as clusters inside that context.

## Chapters

- [Create a Hydra App](01-00-create-a-hydra-app.md) — set `HYDRA_CONTEXT`, walk through the first validation errors, and create the first Helm chart for a Hydra app
- [Inspect Manifests and Their Templates](02-00-adding-manifests-to-the-helm-charts.md) — inspect rendered manifests, template sources, and filters in a Hydra app chart
- [Extracting Data from Manifests](03-00-local-find-with-cel-pick-and-filters.md) — shape result lists with `--pick` and filter entities via `--include` / `--exclude`
