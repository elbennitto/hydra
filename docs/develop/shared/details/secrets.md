# Secrets in Hydra YAML

## Overview

Hydra models Kubernetes Secrets and SOPS-encrypted secrets as entities in the dependency graph. This document describes how secret-related data is represented in `*.hydra.yaml` files and how the UI displays it.

No actual secret **values** are stored — only structural metadata (key names, relationships, origin).

## Secret-Related Entity Types

Two kinds of entities represent secrets in the graph:

| Kind         | API Group                     | Example ID                                                       | Description                             |
| ------------ | ----------------------------- | ---------------------------------------------------------------- | --------------------------------------- |
| `Secret`     | `v1` (core)                   | `v1/Secret/argocd/helm-repo-creds`                               | Standard Kubernetes Secret              |
| `SopsSecret` | `isindir.github.com/v1alpha3` | `isindir.github.com/v1alpha3/SopsSecret/argocd/helm-repo-secret` | SOPS-encrypted secret (Custom Resource) |

## Entity Fields

### `secretKeys`

The `secretKeys` field lists the **key names** from the `data` or `stringData` map of a Kubernetes Secret. It contains only the key names, never the values.

```yaml
entities:
  - id: v1/Secret/argocd/helm-repo-creds
    tags:
      - controller:argocd-repo-credentials
    secretKeys:
      - enableOCI
      - name
      - password
      - type
      - url
      - username
```

| Field        | Type     | Required | Description                                                                                         |
| ------------ | -------- | -------- | --------------------------------------------------------------------------------------------------- |
| `secretKeys` | string[] | no       | Key names from the Secret's `data`/`stringData` map. Empty array or omitted when no keys are known. |

Secrets without known keys (e.g. `app:missing` secrets that are referenced but not present in rendered templates) have no `secretKeys` field:

```yaml
- id: v1/Secret/argocd/argocd-dex-server-tls
  tags:
    - app:missing
```

### Tags

Secret entities use these tags to indicate their origin:

| Tag | Meaning |
| --- | --- |
| `controller:<workload>` | Secret is **generated/materialized** by a controller or policy path whose responsible workload is declared in app ref-parser metadata |
| `app:missing` | Secret is referenced by other entities but not defined in any rendered template and not accounted for by the stabilized virtual graph |

## SopsSecret → Secret Relationship

The SOPS Secrets Operator watches `SopsSecret` custom resources and creates corresponding `v1/Secret` objects in the **same namespace**. Hydra models that as a **generic** controller-materialized edge: charts supply ref-parsers (CEL `predicate` / `pick`) that emit the `Secret` id, the relation attribute **`"origin:generated": controller`**, parser-defined provenance metadata (typically repeated relation attributes such as `origin:app` and `origin:workload`), and optional `key(...)` attributes—**not** a bespoke built-in rule that encodes one chart’s field names while other apps stay undocumented.

```yaml
references:
  - from: isindir.github.com/v1alpha3/SopsSecret/argocd/helm-repo-secret
    to: v1/Secret/argocd/helm-repo-creds
    labels:
      - sops
    attributes:
      - "origin:generated": controller
    reverse: true
```

The `reverse: true` flag means `parseHydraYaml()` swaps `from`/`to` at parse time so that the visual direction becomes: `Secret → SopsSecret` (the Secret "points back" to its producer). See [hydra-yaml.md – Reverse Flag](hydra-yaml.md#reverse-flag) for details on the swap semantics.

**Recursive materialization** — When a workload references a `Secret` that only appears after following one or more refs with **`"origin:generated": job`** or **`"origin:generated": controller`**, Hydra’s ref-driven checks resolve those edges **iteratively to a fixpoint** so consumers (UI, review, ordering) see a stable target set.

**Policy engines** — Admission/policy rules that duplicate or mutate data (for example Kyverno **Generate** rules) normally target the **`v1/Secret`**; they do **not** mirror the `SopsSecret` CR. Hydra may still show both CR and `Secret` in the graph for dependency clarity.

### CRD Reference

Each `SopsSecret` entity also has a reference to its CRD:

```yaml
- from: isindir.github.com/v1alpha3/SopsSecret/argocd/helm-repo-secret
  to: apiextensions.k8s.io/v1/CustomResourceDefinition//sopssecrets.isindir.github.com
  labels:
    - crd
```

## Secret Consumer References

Entities that use a Secret reference it via standard labels:

| Label             | Meaning                                    |
| ----------------- | ------------------------------------------ |
| `volume`          | Secret mounted as a volume                 |
| `env`             | Secret key used in an environment variable |
| `envFrom`         | Secret mapped via `envFrom`                |
| `imagePullSecret` | Secret used as image pull credential       |

Example: a Deployment referencing a Secret via volume mount:

```yaml
- from: apps/v1/Deployment/monitoring/grafana
  to: v1/Secret/monitoring/grafana-client-secret
  labels:
    - env
```

## Data Model (TypeScript)

In `src/model.ts`, the `secretKeys` field is part of `HydraEntity`:

```typescript
export type HydraEntity = {
  id: string;
  // ... other fields ...
  secretKeys: string[]; // data/stringData key names, empty array if none
};
```

Parsing in `src/parseHydra.ts`:

```typescript
entity.secretKeys = item.secretKeys ?? [];
```

## UI Representation

### Details Tab

The **Details** tab of the Entity Page shows `secretKeys` as inline badges when present.

### Secrets Tab

The **Secrets** tab (`EntityTab = "secrets"`) is only visible for entities with `kind === "Secret"`. In the unified entity page it is rendered by `SecretsTabContent` inside `src/components/EntityPage.tsx` and shows:

1. **Produced by** — which resource materializes this Secret (found via references whose `attributes` include **`"origin:generated": job`** or **`"origin:generated": controller`**)
2. **Secret Keys** — a table listing each key name and which entities reference this Secret
3. **Consumed by** — all entities that have incoming references to this Secret (e.g. Deployments using it as env or volume)

The tab is accessible via URL hash: `#<cluster>?page=details&node=<entityId>&tab=secrets`. See [navigation.md](../../hydra-ui/details/navigation.md) for the full URL format.

## Complete Example

A SOPS-managed Secret with two consumers:

```yaml
entities:
  # The SopsSecret CR (encrypted source in Git)
  - id: isindir.github.com/v1alpha3/SopsSecret/monitoring/grafana-client-secret
    templatePath: unknown-12.yaml
    templateIndex: 1

  # The generated Kubernetes Secret
  - id: v1/Secret/monitoring/grafana-client-secret
    tags:
      - controller:sops-secrets-operator
    secretKeys:
      - GF_AUTH_GENERIC_OAUTH_CLIENT_ID
      - GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET

  # A consumer (Grafana StatefulSet)
  - id: apps/v1/StatefulSet/monitoring/kube-prometheus-stack-grafana
    templatePath: kube-prometheus-stack/charts/kube-prometheus-stack/charts/grafana/templates/statefulset.yaml
    templateIndex: 1

references:
  # SopsSecret produces the Secret
  - from: isindir.github.com/v1alpha3/SopsSecret/monitoring/grafana-client-secret
    to: v1/Secret/monitoring/grafana-client-secret
    labels:
      - sops
    attributes:
      - "origin:generated": controller
    reverse: true

  # Grafana consumes the Secret via env
  - from: apps/v1/StatefulSet/monitoring/kube-prometheus-stack-grafana
    to: v1/Secret/monitoring/grafana-client-secret
    labels:
      - env

groups:
  - name: kube-prometheus-stack-grafana (StatefulSet)
    ids:
      - apps/v1/StatefulSet/monitoring/kube-prometheus-stack-grafana
      - isindir.github.com/v1alpha3/SopsSecret/monitoring/grafana-client-secret
      - v1/Secret/monitoring/grafana-client-secret
```

### Visual Chain

```text
SopsSecret/grafana-client-secret
       │  (label sops, attribute origin:generated=controller, reversed)
       ▼
Secret/grafana-client-secret
  secretKeys: [GF_AUTH_GENERIC_OAUTH_CLIENT_ID,
               GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET]
       ▲
       │  (env)
StatefulSet/kube-prometheus-stack-grafana
```

## Source Files

| File                              | Purpose                                                                                                                                                              |
| --------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `src/model.ts`                    | `HydraEntity.secretKeys` type definition                                                                                                                             |
| `src/parseHydra.ts`               | YAML parsing, `secretKeys` extraction                                                                                                                                |
| `src/components/EntityPage.tsx`   | Unified Entity Page with `SecretsTabContent` and `secretKeys` display in Details                                                                                     |
| `src/components/SecretsPanel.tsx` | **Dead code / unused.** Legacy standalone Secrets panel component — not imported anywhere. The active implementation is `SecretsTabContent` inside `EntityPage.tsx`. |
