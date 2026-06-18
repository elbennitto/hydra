# Configuration: HYDRA_CONTEXT

The environment variable and flag that select which Hydra context directory Hydra reads from.

## Setting the context

```bash
export HYDRA_CONTEXT=/path/to/hydra/context
```

Or per command:

```bash
hydra --hydra-context /path/to/hydra/context local template 'prod.**'
```

When neither `HYDRA_CONTEXT` nor `--hydra-context` is set, Hydra uses the current working directory.

## What is a Hydra context?

The Hydra context is the root directory that contains cluster definitions, charts, values, and encrypted secrets. Almost every `hydra local` and `hydra gitops` command needs it.

In a typical layout, cluster definitions live under something like `gitops-repository/clusters/<cluster>/`, while charts live in a separate charts repository that Hydra resolves from the context configuration.

## Validation Rules

Hydra resolves hierarchy levels using `global.hydra.type` and optional `global.hydra.parent`.

- Allowed `global.hydra.type` values are `group`, `context`, `cluster`, `root-app`, `child-app`.
- At least one level in the hierarchy must define `global.hydra.type`.
- Child levels are inferred automatically from parents (`group -> context -> cluster -> root-app -> child-app`).
- If a level defines a conflicting explicit type, Hydra returns an error.
- `global.hydra.parent` controls parent lookup (`true` by default, `false` disables lookup for that level).
- Default for `group` is `parent: false` when `parent` is omitted.

Hydra context detection no longer depends on an `in-cluster/argocd` directory.

## Troubleshooting

If Hydra reports that it "could not find a valid Hydra context", assume first that your chosen directory is intended to be the context and check which hierarchy marker is missing.

Example layout:

```tree
gitops-repository/
    clusters/
        test/
            values.yaml
            developer1/
                values.yaml
                in-cluster/
```

For `HYDRA_CONTEXT=.../clusters/test/developer1`, Hydra expects `developer1/` to resolve as a `context`.

Recommended fix:

- Set `global.hydra.type: group` in `.../clusters/test/values.yaml`

Alternative fix:

- Set `global.hydra.type: context` in `.../clusters/test/developer1/values.yaml`

This lets Hydra infer the hierarchy as:

```text
group -> context -> cluster -> root-app -> child-app
```

If `developer1/values.yaml` is missing entirely, create it before setting `global.hydra.type: context`.

For a deeper explanation of how Hydra resolves `group` and `context`, see [Concepts: Context Resolution](../concepts/context-resolution.md).

## Relationship to Kubernetes context

`HYDRA_CONTEXT` is independent of your active `kubectl` context. You can point Hydra at the `prod` GitOps tree while your kubeconfig still targets another cluster.

Use [`hydra gitops validate-current-context`](../commands/cluster/validate-current-context.md) to verify that the live cluster matches the cluster name you intend to operate on.

Warning: this validation applies to `hydra gitops` only. The reserved `hydra cluster` command surface will not use local Hydra files, so `HYDRA_CONTEXT` cannot be part of that validation.

## See Also

- [config.yaml](config-yaml.md) — optional per-user kubeconfig mapping
- [Concepts: Context and Clusters](../concepts/context-and-clusters.md)
- [Kubernetes Context](kubernetes-context.md) — kubectl requirements
