# Values

How Helm values are composed in ArgoCD child Application CRs. Hydra uses 8 distinct value categories that enter child apps either directly (via `valueFiles`) or indirectly (via `valuesObject`).

## Key Concepts

- **8 value categories** — upstream values, app values, group/context/cluster values (via `valueFiles`), root app defaults, root app values, and hydra values (via `valuesObject`)
- **valueFiles** — ArgoCD `spec.sources[].helm.valueFiles` lists value files in ascending priority; covers categories 2–5
- **valuesObject** — built dynamically by `infra_library.template.app_of_apps`; has higher priority than all `valueFiles`; carries categories 6–8
- **valuesObject composition** — app-specific values (`$.Values.<appName>`) deep-merged with `$.Values.global` (excluding `global.hydra`) plus `global.argocd: true`
- **Hydra globals** — `global.hydra.repository`, `global.hydra.revision`, `global.hydra.stage`, `global.hydra.path` used for link building and path resolution
- **hydra-go export mapping** — each category maps to export types: `group`, `context`, `cluster`, `app`, chart tgz, or `fallbackValues`
- **UI tree categories** — each value category has a dedicated tag and color in the hydra-ui values tree

**Reference:** [ArgoCD Helm — Value Precedence](https://argo-cd.readthedocs.io/en/stable/user-guide/helm/#helm-value-precedence)

→ **Full details:** [details/values.md](details/values.md)
