# Workflow: Scaling Workloads

Scale apps up or down safely with dependency awareness.

## Scale Down

```bash
# Scale down all demo services
hydra gitops scale down 'prod.demo.*'

# Verify
hydra gitops scale status 'prod.demo.*'
```

Hydra scales dependents first — services are scaled before their databases, ensuring clean shutdown.

## Scale Up

```bash
# Scale up (dependencies first)
hydra gitops scale up 'prod.demo.*'

# Verify readiness
hydra gitops scale status 'prod.demo.*'
```

Hydra scales dependencies first — databases before services, ensuring services find their backends ready.

## Partial Scale

Use app-id patterns to scale specific apps:

```bash
hydra gitops scale down prod.demo.service-auth
hydra gitops scale up prod.demo.service-auth
```

## Use Cases

| Scenario | Command |
|----------|---------|
| Maintenance window | `scale down 'prod.demo.*'` then `scale up 'prod.demo.*'` |
| Save resources on test | `scale down 'test.demo.*'` |
| Restart a service | `scale down <app>` then `scale up <app>` |

## See Also

- [hydra gitops scale](../commands/cluster/scale.md)
- [Values: scale](../values/scale.md)
