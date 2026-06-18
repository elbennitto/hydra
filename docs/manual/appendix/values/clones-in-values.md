# clones in Values

Configuring runtime resource copying.

## Structure

```yaml
global:
  hydra:
    clones:
      <clone-name>:
        tag: <string>                # Optional tag (e.g., "bootstrap")
        predicate: '<CEL>'           # Source resource selector
        targets:
          - namespace: '<pattern>'   # Target namespace(s)
        exclude:
          - '<CEL>'                  # Exclude specific targets
```

## Example: Image Pull Secret Distribution

```yaml
global:
  hydra:
    clones:
      image-pull-secret:
        tag: bootstrap
        predicate: 'kind == "Secret" && name == "registry-credentials" && ns == "kyverno"'
        targets:
          - namespace: "*"
        exclude:
          - 'ns == "kube-system"'
          - 'ns == "kube-public"'
```

This clones the `registry-credentials` Secret from the `kyverno` namespace into all other namespaces except `kube-system` and `kube-public`.

## Fields

### predicate

CEL expression selecting which resources to clone. Must match exactly the source resources.

### targets

List of target specifications. Each target defines a `namespace` pattern:
- `"*"` — All namespaces
- `"specific-ns"` — One specific namespace

### exclude

List of CEL expressions. Resources matching any exclude pattern are not cloned to that target.

### tag

Optional tag controlling when clones are evaluated:
- `bootstrap` — Only during bootstrap operations

## See Also

- [Concepts: Clones](../concepts/clones.md)
- [CEL: Predicates](../cel/predicates.md)
