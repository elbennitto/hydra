# Configuration: Kubernetes Context

How `kubectl` context relates to Hydra operations.

## Requirement

All `hydra gitops` commands require a valid kubectl context pointing to the correct cluster.

## Warning

`hydra gitops validate-current-context` exists only for the `hydra gitops` command surface.

The reason is that `hydra gitops` uses the local Hydra context as input, so `HYDRA_CONTEXT` and `--hydra-context` can be considered during validation. The future `hydra cluster` command surface does **not** use local Hydra files, so `HYDRA_CONTEXT` cannot be taken into account there.

## Safety Guard

Hydra validates the active kubectl context against `allowedContexts` before any cluster operation:

```yaml
# In config.yaml or values
kubectl:
  allowedContexts:
    - prod-admin
    - prod-admin@prod
```

Run the validation explicitly:

```bash
hydra gitops validate-current-context prod
```

## Managing Contexts

```bash
# List available contexts
kubectl config get-contexts

# Switch context
kubectl config use-context prod-admin

# Verify
kubectl cluster-info
hydra gitops validate-current-context prod
```

## Common Mistakes

| Problem | Solution |
|---------|----------|
| Wrong context active | `kubectl config use-context <correct>` |
| Context name doesn't match allowedContexts | Update `allowedContexts` in config |
| Context points to wrong cluster | Check kubeconfig server URLs |

## HYDRA_CONTEXT vs kubectl context

| | HYDRA_CONTEXT | kubectl context |
|---|---|---|
| Purpose | Selects Hydra configuration (apps, values) | Selects K8s API endpoint |
| Set via | `export HYDRA_CONTEXT=prod` | `kubectl config use-context` |
| Scope | Hydra CLI only | All kubectl/K8s tools |

For `hydra gitops`, both must align to work correctly.

For the planned `hydra cluster` command surface, only the live cluster side can be validated because no local Hydra context is involved.

## See Also

- [HYDRA_CONTEXT](hydra-context.md)
- [hydra gitops validate-current-context](../commands/cluster/validate-current-context.md)
