# Repositories

Hydra uses two Git repositories with distinct responsibilities.

## Charts Repository

The **charts-repository** is the application catalog. It contains:

```
charts-repository/
    apps/
        cluster-infra/
            root/              ← Root app for this group
            cert-manager/      ← Child app
            ingress-nginx/     ← Child app
            ...
        demo/
            root/
            service-auth/
            ...
        cicd/
            root/
            gitlab-runner/
    shared/
        infra_library/         ← Shared Helm library charts
```

Each child app directory contains:
- `Chart.yaml` — Chart metadata and dependencies
- `templates/` — Helm templates
- `values.yaml` — Default values including `global.hydra` configuration (refs, presets, clones, etc.)

**Responsibility**: Define *what* gets deployed (chart logic, default configuration, dependency model).

## GitOps Repository

The **gitops-repository** contains per-cluster configuration:

```tree
gitops-repository/
    clusters/
        prod/
            cluster-infra/
                values.yaml         ← Group-level overrides
                in-cluster/
                    cert-manager/
                        values.yaml ← App-specific overrides
                    ...
            demo/
                ...
        test/
            ...
```

**Responsibility**: Define *where* and *how* things are deployed (cluster-specific overrides, secrets, revision pins).

## Why Two Repositories?

| Concern | Charts Repo | GitOps Repo |
|---------|-------------|-------------|
| Application logic | ✓ | |
| Default values | ✓ | |
| Ref definitions | ✓ | |
| Cluster-specific values | | ✓ |
| Secret references | | ✓ |
| Environment promotion | | ✓ |
| ArgoCD revision pins | | ✓ |

The separation enables:
- **Reuse** — One chart definition, many cluster deployments
- **Promotion** — Promote versions across environments without touching chart code
- **Access control** — Different teams can own charts vs. cluster config

## Value Layering

Values merge bottom-up:

1. `charts-repository/apps/<group>/<app>/values.yaml` (base)
2. `gitops-repository/clusters/<cluster>/<group>/values.yaml` (group override)
3. `gitops-repository/clusters/<cluster>/<group>/in-cluster/<app>/values.yaml` (app override)
4. Hydra ConfigMaps (runtime injection)

See [Values: Overview](../values/overview.md) for the full merge semantics.

## See Also

- [Values](../values/)
- [Context and Clusters](context-and-clusters.md)
