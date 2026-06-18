# Workflow: Cluster Bootstrap

How to bring up a new cluster from zero to fully managed.

## Prerequisites

- Talos/K8s cluster provisioned (nodes joined, API reachable)
- `kubectl` context configured and validated
- SOPS age key available
- Hydra CLI installed

## Steps

### 1. Configure kubectl context

```bash
kubectl config use-context <new-cluster-context>
hydra gitops validate-current-context <cluster>
```

### 2. Initial apply (bootstrap mode)

Bootstrap mode skips guards that expect existing resources:

```bash
hydra gitops apply '<cluster>.**' --bootstrap
```

This applies everything in topological order. Apps that depend on CRDs or namespaces created by other apps will be applied after their dependencies.

### 3. Wait for readiness

```bash
hydra gitops status '<cluster>.**'
```

Some apps (cert-manager, ingress-nginx) may take a few minutes to become ready.

### 4. Validate

```bash
hydra gitops review cluster <cluster>
hydra gitops system <cluster>
```

Ensure no untracked resources and all presets match.

### 5. Enable ArgoCD sync

```bash
hydra gitops sync auto '<cluster>.**'
```

## Troubleshooting

| Symptom | Diagnosis |
|---------|-----------|
| CRD not found | Missing dependency; check app ordering |
| Timeout on cert-manager | DNS not propagated yet; wait and retry |
| Bootstrap guard failure | Use `--bootstrap` flag |

## See Also

- [Concepts: Bootstrap](../concepts/bootstrap.md)
- [hydra gitops apply](../commands/cluster/apply.md)
- [Tutorial: First Cluster](../tutorials/first-cluster.md)
