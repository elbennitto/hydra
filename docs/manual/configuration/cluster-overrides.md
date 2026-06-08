# Cluster Overrides

`hydra.yaml` is an optional file in a **cluster root directory** that lets you manually assign live cluster resources to a Hydra app.

Cluster overrides apply to both:

- preset apps such as `presets.coredns`
- normal Hydra apps such as `argocd.root` or `cluster-infra.dex`

## Motivation

Typical reasons to use cluster overrides:

- assign cluster resources that are **not managed by Hydra** to a preset app
- manually resolve **wrong double assignments** for one specific cluster

This is especially useful when a resource exists in the live cluster, but Hydra cannot infer the intended owner uniquely from template ids, refs, owner references, or preset matching alone.

Use it when the normal ownership model is not enough:

- a resource should belong to a preset app such as CoreDNS
- a runtime-created resource should belong to a normal Hydra app
- a resource matches more than one app and you need an explicit override

## Placement

`hydra.yaml` is allowed **only** in a cluster root:

```text
<group>/<context>/<cluster>/hydra.yaml
```

It is **not** allowed in:

- group directories
- context directories
- root app directories
- child app/chart directories

## Format

```yaml
hydra:
  overrides:
    presets.coredns:
      - "v1/ConfigMap/kube-system/manual-coredns"
      - id: "apps/v1/Deployment/kube-system/coredns"
        exclude: true

    argocd.root:
      - id: "v1/Secret/argocd/argocd-server-tls"

    cluster-infra.dex:
      - gvk: v1/Secret
        namespace: dex
        cel: 'name.startsWith("dex-")'
```

## Override Targets

Keys under `hydra.overrides` are cluster-local app ids, so they do **not** include the cluster name prefix.

Supported target forms:

- `presets.<presetId>`: preset app, for example `presets.coredns`
- `<rootApp>.<childApp>`: child app, for example `cluster-infra.dex`
- `<rootApp>.root`: root app sugar, for example `argocd.root`
- `<rootApp>`: also resolves to the root app, for example `argocd`

Examples:

- `presets.coredns` -> `in-cluster.preset.coredns`
- `argocd.root` -> `in-cluster.argocd`
- `cluster-infra.dex` -> `in-cluster.cluster-infra.dex`

## Override Entries

Each list item supports the same selector notation already used in Hydra ref and preset configuration.

Supported forms:

- plain string: treated like `id`
- `id`
- `cel`
- `predicate`: alias for `cel`
- selector shorthand:
  `group`, `version`, `kind`, `apiVersion`, `gvk`, `namespace`, `gvkn`, `name`

Examples:

```yaml
hydra:
  overrides:
    presets.coredns:
      - "v1/ConfigMap/kube-system/manual-coredns"
      - id: "v1/ConfigMap/kube-system/manual-coredns-2"
      - gvk: autoscaling/v2/HorizontalPodAutoscaler
        namespace: kube-system
        name: coredns
      - gvk: v1/Secret
        namespace: kube-system
        cel: 'name.startsWith("coredns-")'
```

## Exclude

`exclude: true` removes matching resources from that target app's override set.

This is useful when you want a broad include and then subtract a specific resource:

```yaml
hydra:
  overrides:
    cluster-infra.dex:
      - gvk: v1/Secret
        namespace: dex
      - id: "v1/Secret/dex/dex-shared-ca"
        exclude: true
```

## Behavior

- Manual overrides win over inferred assignment.
- They can assign resources to normal apps or preset apps.
- Preset overrides also affect cluster-default preset matching.
- `exclude: true` only removes matches from that one target app; it does not globally delete the resource from consideration.

## Related

- [Preset Overrides](../presets/preset-overrides.md) for `global.hydra.presets`
- [Builtin Presets](../presets/builtin-presets.md)
- [Preset Activation](../presets/preset-activation.md)
