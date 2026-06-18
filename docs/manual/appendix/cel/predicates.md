# CEL Predicates

Using CEL as filters in CLI commands with `--include` and `--exclude`.

## Syntax

```bash
hydra gitops diff <appId> --include '<CEL expression>'
hydra gitops diff <appId> --exclude '<CEL expression>'
```

Multiple filters can be combined:

```bash
hydra gitops diff prod.** --include 'kind == "Deployment"' --exclude 'ns == "kube-system"'
```

## Semantics

- `--include` — Only show resources matching the expression (whitelist)
- `--exclude` — Hide resources matching the expression (blacklist)
- When both are used: include is applied first, then exclude filters the result

## Common Predicates

### Filter by Kind

```cel
kind == "Deployment"
kind == "Secret"
kind == "ConfigMap" || kind == "Secret"
```

### Filter by Namespace

```cel
ns == "default"
ns == "kube-system"
ns != "kube-system"
ns == ""                    // Cluster-scoped resources
```

### Filter by Name

```cel
name == "my-deployment"
name.startsWith("coredns")
name.matches("^service-.*")
```

### Filter by GVK

```cel
gvk == "apps/v1/Deployment"
gvk == "cert-manager.io/v1/Certificate"
gvk.startsWith("apps/")
```

### Filter by Ownership

```cel
appOwned                    // Only app-owned resources
!appOwned                   // Only non-app resources
builtIn                     // Only cluster builtins
```

### Combine Conditions

```cel
kind == "Deployment" && ns == "default"
(kind == "Secret" || kind == "ConfigMap") && ns != "kube-system"
namespaced && !appOwned && !builtIn
```

### Negation

```cel
!(kind == "Event")
!(ns == "kube-system" || ns == "kube-public")
```

## Commands Supporting CEL Filters

| Command | Flag |
|---------|------|
| `hydra gitops diff` | `--include`, `--exclude` |
| `hydra gitops apply` | `--include`, `--exclude` |
| `hydra local find` | CEL filter argument |
| `hydra gitops untracked` | `--include`, `--exclude` |

## In Ref-Parsers

In ref-parser yaml files, the `predicate` field is a CEL expression:

```yaml
predicate: 'gvk == "apps/v1/Deployment"'
```

## In Values

CEL predicates appear in several `global.hydra` configurations:

```yaml
templatePatches:
  my-patch:
    predicate: 'kind == "Deployment" && ns == "default"'

diff:
  ignore-events:
    predicate: 'kind == "Event"'

clones:
  my-clone:
    predicate: 'kind == "Secret" && name == "pull-secret"'
```

## See Also

- [CEL: Variables](variables.md) — What you can filter on
- [CEL: Examples](examples.md) — More complex examples
