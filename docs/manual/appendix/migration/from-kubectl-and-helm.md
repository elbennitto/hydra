# From `helm install` to ArgoCD and Hydra

This page is for operators who are used to `helm install`, `helm upgrade`, and manual ArgoCD UI workflows and want to understand what changes in a Hydra-managed cluster.

The short version is:

- `helm install` is an imperative, one-time cluster mutation.
- `helm template` is a local render only.
- ArgoCD is the continuous reconciler that keeps the cluster aligned with Git.
- Hydra is the operator-facing layer that computes values, renders charts, diffs, applies, and controls ArgoCD reconciliation.

## The Mental Model Shift

With plain Helm, the release object in the cluster is the operational anchor. With ArgoCD plus Hydra, the Git repository and the generated ArgoCD `Application` resources are the anchor instead.

| Tool | Writes to cluster | Stores Helm release metadata | Continuously reconciles | Primary purpose |
| --- | --- | --- | --- | --- |
| `helm install` | Yes | Yes | No | Install a chart once and create a Helm release |
| `helm template` | No | No | No | Render manifests locally for review |
| ArgoCD | Yes | No Helm release | Yes | Compare Git desired state with live state and sync drift |
| `hydra local ...` | No | No | No | Show computed values and rendered manifests |
| `hydra gitops ...` | Yes | No Helm release | No | Diff, apply, dump, scale, or uninstall through Hydra |
| `hydra argocd ...` | Indirectly | No Helm release | Controls reconciliation | Read ArgoCD status and change sync behavior |

The most important consequence is that ArgoCD plus Hydra behaves much closer to `helm template` plus `kubectl apply` than to `helm install`.

## `helm install` vs `helm template`

If you only remember one distinction, remember this one:

- `helm install` renders and immediately writes resources to the cluster.
- `helm template` only renders YAML locally and does not create a Helm release.

That difference matters because ArgoCD and Hydra mostly operate on rendered desired state, not on Helm release history.

| Question | `helm install` | `helm template` | ArgoCD and Hydra |
| --- | --- | --- | --- |
| Does it need cluster access? | Yes | No | ArgoCD: yes, Hydra local: no, Hydra gitops: yes, Hydra cluster: planned |
| Does it create a Helm release? | Yes | No | No |
| Can I ask it for merged values later? | Yes, from the Helm release | No | Yes, through Hydra value rendering or the ArgoCD `Application` source |
| Is it a good GitOps primitive? | Not by itself | Yes, as a render step | Yes, this is the normal model |
| What is the closest Hydra equivalent? | `hydra gitops apply <appId>` | `hydra local template <appId>` | `hydra argocd status` and `hydra argocd sync ...` for reconciliation control |

## The New Daily Workflow

What used to be a single imperative Helm command now becomes an explicit GitOps workflow:

1. Change values or chart inputs in Git.
2. Inspect the merged result with `hydra local values <appId>`.
3. Inspect the rendered manifests with `hydra local template <appId>`.
4. Compare desired and live state with `hydra gitops diff <appId>`.
5. Apply directly with Hydra or commit and let ArgoCD reconcile.

Quick mapping:

| Old habit | New Hydra habit |
| --- | --- |
| `helm get values <release>` | `hydra local values <appId>` |
| `helm template <chart>` | `hydra local template <appId>` |
| `kubectl diff -f ...` | `hydra gitops diff <appId>` |
| `helm install` or `helm upgrade` | `hydra gitops apply <appId>` |
| Disable ArgoCD auto-sync in the UI | `hydra argocd sync manual <appId>` |
| Freeze reconciliation completely for maintenance | `hydra argocd sync prevent <appId>` |

## Common Migration Questions

### How do I find out which values an app uses?

In a Hydra setup, there are two slightly different answers:

- "What does Hydra compute after the full values hierarchy is merged?"
- "What did ArgoCD receive on the generated `Application` object?"

For operators, the Hydra answer is usually the most useful one.

| Path | How | What it tells you |
| --- | --- | --- |
| `kubectl` | `kubectl -n argocd get application <app> -o yaml \| yq '.spec.sources[]?.helm \| {valueFiles, valuesObject, parameters}'` | The value files and inline Helm input currently stored on the ArgoCD `Application` |
| ArgoCD UI | Open `Applications` -> select the app -> inspect the Helm source, `Parameters`, and source details | The same ArgoCD-side view, but in the UI |
| Hydra | `hydra local values <appId>` | The fully merged values after Hydra's hierarchy has been resolved |

Use `hydra local template <appId>` immediately afterward if you want to see how those values affect the rendered manifests.

### How do I find out which chart version is running in the cluster?

This question changes in GitOps.

With `helm install`, you could ask Helm about the release. With ArgoCD plus Hydra, there is usually no Helm release object to query. Instead, you typically answer the question in one of two ways:

1. Check the live workload labels such as `helm.sh/chart` and `app.kubernetes.io/version`.
2. Check the desired chart version in Git or in the rendered manifests and confirm the app is in sync.

| Path | How | What it tells you |
| --- | --- | --- |
| `kubectl` | `kubectl get deployment <deployment> -n <namespace> -o jsonpath='{.metadata.labels.helm\.sh/chart}{"\n"}{.metadata.labels.app\.kubernetes\.io/version}{"\n"}'` | The chart label and app version label on a representative live workload |
| ArgoCD UI | Open the application, pick a workload from the resource tree, and inspect its labels or manifest | The same live labels through the UI |
| Hydra | `hydra gitops dump <cluster> --include 'kind == "Deployment" && namespace == "<namespace>"' \| yq 'select(.metadata.name == "<deployment>") \| .metadata.labels."helm.sh/chart", .metadata.labels."app.kubernetes.io/version"'` | The live labels through Hydra's cluster view |

If those labels are missing, fall back to the desired state:

- `hydra local template <appId>` to inspect rendered labels and manifests
- the relevant `Chart.yaml` in `gitops-repository/` or `charts-repository/`
- `hydra argocd status <appId>` to confirm the app is currently in sync

### How do I turn ArgoCD off in an emergency so I can change the cluster manually?

The preferred answer is: do not shut down the whole ArgoCD control plane unless you really have to. In most cases, you only want to stop reconciliation for the affected apps.

For that normal maintenance case, use:

- `hydra argocd sync manual <appId>` if manual sync should still be possible
- `hydra argocd sync prevent <appId>` if no sync should happen at all

If you truly need to stop ArgoCD reconciliation globally, use Kubernetes to stop the controllers:

```bash
kubectl -n argocd scale statefulset argocd-application-controller --replicas=0
kubectl -n argocd scale deploy argocd-applicationset-controller --replicas=0
```

Bring them back afterward:

```bash
kubectl -n argocd scale statefulset argocd-application-controller --replicas=1
kubectl -n argocd scale deploy argocd-applicationset-controller --replicas=1
```

Comparison:

| Path | How | Notes |
| --- | --- | --- |
| `kubectl` | Scale the ArgoCD controller workloads to zero as shown above | Emergency-only, because it affects the whole ArgoCD control plane |
| ArgoCD UI | No reliable global "power off" switch for the full control plane | Use the UI for per-app or per-project sync control instead |
| Hydra | `hydra argocd sync prevent <scope>` | Supported Hydra approach for maintenance windows; this pauses reconciliation without shutting ArgoCD down |

### How do I disable auto-sync for a single app?

If you only want to disable automatic reconciliation but still allow a human-triggered sync, the closest Hydra mode is `manual`.

| Path | How | Notes |
| --- | --- | --- |
| `kubectl` | `kubectl -n argocd edit application <app>` | Remove or adjust `spec.syncPolicy.automated` if you manage the app directly through ArgoCD |
| ArgoCD UI | Open the app and disable `Auto-Sync` in the app details | ArgoCD-native per-app workflow |
| Hydra | `hydra argocd sync manual <appId>` | Recommended Hydra workflow; operational goal is "auto-sync off, manual sync still allowed" |

If the app is Hydra-managed, prefer the Hydra command over manually editing the generated ArgoCD `Application`.

### How do I disable auto-sync for all apps?

At cluster or project scope, Hydra uses selectors instead of editing every app one by one.

| Path | How | Notes |
| --- | --- | --- |
| `kubectl` | `kubectl -n argocd edit appproject <project>` | Add or adjust AppProject sync (for example **manual** or **prevent**) for the matching applications |
| ArgoCD UI | Open the project and change its sync settings | Best UI-level option for many apps at once |
| Hydra | `hydra argocd sync manual <cluster>.**` | Disables automatic reconciliation for all selected Hydra apps while keeping manual sync available |

Use `hydra argocd sync prevent <cluster>.**` instead when you want to block both automatic and manual sync during a maintenance window.

## Recommended Safety Pattern For Manual Cluster Changes

If you need to touch the cluster manually, this is the safe sequence:

```bash
# 1. Freeze reconciliation for the affected apps
hydra argocd sync prevent <appId>

# 2. Make your emergency change with kubectl
kubectl edit deployment <name> -n <namespace>

# 3. Verify or diff afterward
hydra gitops diff <appId>

# 4. Re-enable reconciliation
hydra argocd sync auto <appId>
```

That pattern avoids the common problem where ArgoCD immediately reverts the manual emergency change while you are still investigating.

## Next Steps

- [Migration Overview](README.md) - overview and reading guide for migration topics
- [CLI Reference](../cli/README.md) - full command documentation
- [CLI Quickstart](../cli/quickstart.md) - shortest safe path into daily Hydra usage
- [Cluster Lifecycle](../operations/cluster-lifecycle.md) - full operational journey from empty cluster to running apps
