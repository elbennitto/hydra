# The Bootstrap Problem

When setting up a brand-new Kubernetes cluster, there's a fundamental chicken-and-egg problem. This page explains what it is and how Hydra solves it.

## The Problem

In a GitOps setup, secrets (passwords, certificates, API keys) are stored encrypted in Git using SOPS. On the cluster, a component called the **sops-secrets-operator** decrypts these secrets and creates regular Kubernetes Secrets from them.

But here's the catch on an empty cluster:

```text
You want to install:     But it needs:              Which requires:
─────────────────────    ──────────────────────     ─────────────────────
sops-secrets-operator    A container image          An image pull secret
                         from Harbor registry       (to authenticate with
                                                     the registry)

                         But the image pull         The sops-secrets-operator
                         secret is encrypted        to decrypt it
                         with SOPS...

                         ...which isn't running
                         yet because we can't
                         pull its image.
```

In short:

1. The operator needs an image pull secret to start
2. The image pull secret is encrypted with SOPS
3. Only the operator can decrypt SOPS secrets
4. The operator can't start without the secret

This is the **bootstrap problem**.

## The Solution: `--bootstrap`

Hydra's `--bootstrap` flag on [`hydra gitops apply`](../commands/cluster/apply.md) is shorthand that turns on the full set of optional apply behaviors for that run (see the apply command documentation for individual flags). It breaks the cycle by decrypting secrets **locally on your machine** (using your SOPS keys) and pushing them directly to the cluster as plain Kubernetes Secrets:

```text
Normal flow (after bootstrap):
  SopsSecret (encrypted, in Git)
    → sops-secrets-operator (on cluster)
    → Kubernetes Secret (decrypted, on cluster)

Bootstrap flow:
  SopsSecret (encrypted, in Git)
    → Hydra CLI (on YOUR machine, decrypts locally)
    → Kubernetes Secret (pushed directly to cluster)
```

## What `--bootstrap` Does Step by Step

When you run `hydra gitops apply in-cluster.** --bootstrap`:

```text
Step 1: RENDER
  Hydra renders all Helm charts with merged values
  → produces Kubernetes manifests including SopsSecret resources

Step 2: DECRYPT SECRETS
  For each SopsSecret (except backup secrets):
    - Read the encrypted file
    - Decrypt it locally using YOUR SOPS keys
    - Create a plain v1/Secret alongside the SopsSecret
  Result: both the SopsSecret CRs and plain Secrets exist in the manifest set

Step 3: PREVENT ARGOCD SYNC
  Adjust AppProject sync (for example **prevent** or **keep-or-prevent** via `--sync`)
  → keeps ArgoCD from interfering during installation

Step 4: DOWNSCALE WEBHOOKS
  Existing webhook configurations that would block this apply are set to
  failurePolicy=Ignore, and newly applied webhook configs are created in that
  disabled state
  → they would fail because the webhook pods aren't running yet

Step 5: APPLY IN PHASES
  Phase 1: Apply CRDs (Custom Resource Definitions)
  Phase 2: Create namespaces
  Phase 3: Apply the decrypted secrets (image pull secrets, etc.)
  Phase 4: Apply everything else, including webhook configurations in disabled form

Step 6: WAIT FOR PODS
  Wait for workloads to become ready in dependency order

Step 7: ENABLE WEBHOOKS
  Re-apply webhook configurations in provider dependency order
  → restores normal failurePolicy once the backing workloads are ready
```

After bootstrap completes, the sops-secrets-operator is running. From this point on, it takes ownership of the secrets — it decrypts SopsSecrets and keeps the plain Secrets in sync. The bootstrap-created secrets become operator-managed.

## After Bootstrap

You need to hand control to ArgoCD:

```bash
hydra gitops sync auto in-cluster.**
```

This switches sync back toward **auto** (for example from **prevent**), letting ArgoCD manage ongoing synchronization.

## When to Use `--bootstrap`

| Scenario | Use `--bootstrap`? |
| --- | --- |
| Setting up a brand-new empty cluster | Yes |
| Recovering a cluster after total failure | Yes |
| Day-to-day deployments and updates | No, use `hydra gitops apply` without `--bootstrap` |
| Adding a new app to an existing cluster | No |
| Updating configuration on an existing cluster | No |

## Bootstrap-guard resources

Charts can mark bootstrap-critical manifests in `global.hydra.refs` with the **`bootstrap-guard`** tag. Hydra evaluates those declarations when you run `hydra gitops apply`: if your selection includes a guarded resource and you are not using `--bootstrap`, the command fails unless you pass **`--skip-bootstrap-guard`**. Use that override only when the sops-secrets-operator (or other dependency) is already running and you intentionally apply the manifest without local decryption. You cannot combine `--bootstrap` and `--skip-bootstrap-guard`. If you pass `--skip-bootstrap-guard` but your selection does not include any guarded resource, Hydra warns you that the flag had no effect.

## Backup Secrets Are Different

Secrets that were created by `hydra gitops backup create` (marked with `hydra-gitops.org/hydra-backup: "true"`) are **not decrypted** during bootstrap. They follow a separate restore path:

- Backup secrets are created with `suspend: true` so the operator ignores them
- They are restored explicitly with `hydra gitops backup restore`
- This prevents conflicts between the operator and the backup system

## The Webhook Problem

Kubernetes webhooks are HTTP callbacks that validate or modify resources when they're created or updated. Many operators install webhooks (cert-manager, Kyverno, etc.).

During bootstrap, this creates another chicken-and-egg problem:

- Webhooks need their operator pods to be running to handle the callbacks
- But the operator pods are being installed during bootstrap
- If a webhook is registered but its pod isn't running, all related API calls fail

Hydra solves this by:

1. Detecting which webhook configurations are relevant to the current apply
2. Downscaling them to a disabled state (`failurePolicy: Ignore`)
3. Applying them early so dependent bootstrap Jobs can already see the webhook objects
4. Waiting for the backing workloads to become ready
5. Enabling the webhook configurations again in provider dependency order

## Next Steps

- [Cluster Lifecycle](cluster-lifecycle.md) — the full story from VMs to running applications
- [`hydra gitops apply`](../commands/cluster/apply.md) — detailed command reference
- [`hydra gitops backup`](../commands/cluster/backup.md) — backup and restore secrets
