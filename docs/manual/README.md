<div class="hydra-home-logo-wrap">
	<img src="assets/hydra-logo-borderless.svg" alt="Hydra logo" class="hydra-home-logo" />
</div>

# Hydra User Manual

Hydra is a dependency-aware GitOps CLI for managing Kubernetes clusters at scale. It models relationships between resources, orchestrates deployments in the correct order, and provides full visibility into cluster state.

## Who Is This Manual For?

This manual is written for **Kubernetes administrators** who want to learn Hydra. It assumes familiarity with:

- Kubernetes resources (Deployments, Services, ConfigMaps, Secrets, CRDs, …)
- kubectl and kubeconfig
- Helm (charts, values, templating)
- ArgoCD (Applications, sync, reconciliation) if you want to use Hydra together with ArgoCD (this is optional)

If you are already managing clusters with kubectl and Helm, you are ready to start.

## Conventions

- `monospace` denotes commands, flags, file paths, or resource identifiers
- `<placeholder>` denotes values you must replace
- `[optional]` denotes optional arguments
- Examples use a fictional cluster named `prod` unless stated otherwise
