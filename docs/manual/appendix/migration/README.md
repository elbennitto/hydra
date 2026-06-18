# Migration to Hydra

This section is for operators who already know how to manage Kubernetes clusters with `kubectl`, Helm, and possibly ArgoCD, and want to understand how Hydra changes their workflow.

Hydra does not replace these tools. It wraps and orchestrates them. You still use the same Kubernetes concepts, but Hydra moves the source of truth into Git and makes the operational workflow explicit.

## Start Here

- [From `helm install` to ArgoCD and Hydra](from-kubectl-and-helm.md) - mental model shift, `helm install` vs `helm template`, and the operational FAQ for values, chart versions, and sync control

## What Changes

| Before Hydra | With Hydra |
| --- | --- |
| You run commands against each cluster manually | You define the desired state in files, Hydra applies it |
| You remember which apps are installed where | The directory structure is the source of truth |
| You manage values files yourself | Hydra merges values automatically from a hierarchy |
| You install apps one by one in the right order | Hydra installs everything in dependency order |
| You manually back up secrets before changes | Hydra has built-in backup/restore commands |
| ArgoCD apps are created manually in the UI | Hydra generates ArgoCD apps from the directory structure |

## What Stays the Same

- Kubernetes concepts stay the same: pods, deployments, services, namespaces, and secrets.
- Helm charts stay the same: Hydra still renders normal Helm charts.
- ArgoCD stays the reconciler on the cluster: Hydra helps generate and control it.
- `kubectl` stays useful for inspection and debugging.
- SOPS stays the mechanism for encrypted secret material.

## Reading Guide

| Your background | Start here |
| --- | --- |
| I know `kubectl`, `helm install`, and `helm upgrade` | [From `helm install` to ArgoCD and Hydra](from-kubectl-and-helm.md) |
| I already use ArgoCD | [CLI Reference - ArgoCD Topics](../cli/README.md#argocd-topics) |
| I'm coming from Flux | See below |

## Note on Flux Migration

The long-term goal is to replace Flux with ArgoCD plus Hydra for all clusters. Currently:

- Private Demo installations use ArgoCD plus Hydra.
- Public Demo uses Flux and will be migrated.
- Squad clusters will need to be migrated to ArgoCD.

The migration path is:

1. Install ArgoCD on the cluster.
2. Define the cluster in `gitops-repository/`.
3. Point it to the shared charts in `charts-repository/`.
4. Use `hydra gitops apply` to bootstrap the initial desired state.

The Hydra directory structure keeps the new setup aligned with all other clusters.
