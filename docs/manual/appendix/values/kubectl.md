# kubectl in Values

Kubernetes context validation configuration.

## Structure

```yaml
global:
  hydra:
    kubectl:
      allowedContexts:
        - name: '<context-name>'
          cluster: '<cluster-endpoint>'
          authInfo: '<user-name>'
```

## Example

```yaml
global:
  hydra:
    kubectl:
      allowedContexts:
        - name: prod-admin
          cluster: prod-api.example.com
          authInfo: prod-admin-user
        - name: prod-readonly
          cluster: prod-api.example.com
          authInfo: prod-readonly-user
```

## Purpose

Defines which kubectl contexts are valid for this cluster. When you run:

```bash
hydra gitops validate-current-context prod
```

Hydra checks that your current kubectl context matches one of the `allowedContexts` entries. This prevents accidentally running cluster commands against the wrong cluster.

## Fields

### name

The kubectl context name (as shown by `kubectl config get-contexts`).

### cluster

The cluster endpoint name in the kubeconfig. Used for additional verification.

### authInfo

The user/auth-info entry in the kubeconfig.

## Safety

This is a critical safety feature. Without it, a misconfigured kubeconfig could cause Hydra to apply resources to the wrong production cluster.

Always define `allowedContexts` in the GitOps repository's cluster-level values.

## See Also

- [Configuration: Kubernetes Context](../configuration/kubernetes-context.md)
- [Commands: cluster validate-current-context](../commands/cluster/validate-current-context.md)
