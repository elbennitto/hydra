# Context Resolution

How Hydra decides whether a directory is a `group`, `context`, `cluster`, `root-app`, or `child-app`.

## Why This Matters

Most `hydra local` and `hydra gitops` commands start from `HYDRA_CONTEXT` or `--hydra-context`.

Hydra does not identify that directory by name alone. It resolves the hierarchy using `global.hydra.type` values from `values.yaml` files.

## Type Inference

Allowed type values are:

- `group`
- `context`
- `cluster`
- `root-app`
- `child-app`

Hydra infers child levels automatically:

```text
group -> context -> cluster -> root-app -> child-app
```

That means a parent directory can define the type for descendants.

Example:

```tree
gitops-repository/
    clusters/
        test/
            values.yaml
            developer1/
                values.yaml
                in-cluster/
                    values.yaml
```

If `test/values.yaml` contains:

```yaml
global:
  hydra:
    type: group
```

then `developer1/` resolves as a `context` automatically.

If you want `developer1/` to define itself directly as the context instead, set this in `developer1/values.yaml`:

```yaml
global:
  hydra:
    type: context
```

## Recommended Pattern

For layouts like `clusters/<group>/<context>/`, prefer marking the parent as `group`:

```yaml
global:
  hydra:
    type: group
```

Why this is recommended:

- It describes the hierarchy explicitly at the shared parent level.
- Every child directory under that parent can resolve consistently as a `context`.
- It avoids repeating `type: context` in every context directory.

## Common Failure Case

Suppose you set:

```bash
export HYDRA_CONTEXT=/path/to/gitops-repository/clusters/test/developer1
```

and `developer1/` contains `in-cluster/` and `values.yaml`, but Hydra still rejects it.

The most common reasons are:

1. `developer1/values.yaml` exists, but no `global.hydra.type` can be resolved to `context`.
2. `developer1/values.yaml` declares the wrong type, for example `cluster`.
3. `developer1/values.yaml` is missing, so Hydra cannot mark that directory as `context`.

In that situation, keep the chosen `HYDRA_CONTEXT` and fix the hierarchy markers instead.

Recommended fix:

- Set `global.hydra.type: group` in the parent `values.yaml`

Alternative fix:

- Set `global.hydra.type: context` in the selected context directory's `values.yaml`

## Parent Lookup

Hydra looks upward through parent directories unless a level disables that:

```yaml
global:
  hydra:
    parent: false
```

This is `true` by default.

Special case:

- `group` defaults to `parent: false` when `parent` is omitted

That makes `group` a natural boundary for context resolution.

## Related Pages

- [Configuration: HYDRA_CONTEXT](../configuration/hydra-context.md)
- [Context and Clusters](context-and-clusters.md)
- [GitOps Repository Structure](gitops-repository-structure.md)
