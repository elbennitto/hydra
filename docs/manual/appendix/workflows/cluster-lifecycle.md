# Cluster Lifecycle

This page tells the complete story of a cluster — from bare metal (or VMs) to a fully running system. It's the end-to-end view that ties all the pieces together.

## The Big Picture

```text
Phase 1              Phase 2              Phase 3              Phase 4
CREATE VMs  ──────>  INSTALL OS  ──────>  BOOTSTRAP  ──────>  GITOPS
                                          CLUSTER              TAKES OVER

Terraform            Talos Linux          Hydra CLI            ArgoCD
(talos-terraform/)   (talos.sh)           (hydra gitops       (automatic
                                           apply --bootstrap)   sync)
```

## Phase 1: Create the Virtual Machines

**Tool:** Terraform + Terragrunt (`talos-terraform/`)

Before you can have a Kubernetes cluster, you need machines. In this project, VMs are created on VMware vSphere.

```bash
# Set up vSphere credentials
cd talos-terraform
source ./setup-env.sh

# Plan: see what VMs will be created
cd environments/prod
terragrunt plan

# Apply: create the VMs
terragrunt apply
```

What this creates:

- **Control plane nodes** (typically 3) — 4 CPUs, 4GB RAM, 80GB disk each
- **Worker nodes** (typically 3-7) — 8 CPUs, 32GB RAM, 300GB disk each
- Each VM gets a specific MAC address for static IP assignment

The VM names follow the pattern: `talos-<cluster>-controlplane-1`, `talos-<cluster>-worker-1`, etc.

At this point, you have empty VMs with no operating system.

## Phase 2: Install Talos Linux

**Tool:** `talos.sh` (wrapper around `talosctl`)

Talos Linux is installed on the VMs. Talos is special because:

- There is **no SSH** — you manage it entirely through an API
- It's **immutable** — the OS can't be modified at runtime
- It runs **only Kubernetes** — nothing else

```bash
# Generate encrypted cluster configs (first time only)
./talos.sh gen <cluster>

# Apply the control plane config to control plane nodes
./talos.sh apply-config <cluster> controlplane

# Apply the worker config to worker nodes
./talos.sh apply-config <cluster> worker
```

The configs are stored encrypted with SOPS in `gitops-repository/talos/<group>/<cluster>/`:

```tree
gitops-repository/talos/<group>/<cluster>/
    talos.yaml                # Cluster settings (Talos version, network)
    talosconfig.sops.yaml     # Encrypted cluster access credentials
    controlplane.sops.yaml    # Encrypted control plane node config
    worker.sops.yaml          # Encrypted worker node config
```

After this step, Talos bootstraps Kubernetes automatically. You now have an **empty Kubernetes cluster** — it's running, but nothing is installed on it.

## Phase 3: Bootstrap the Cluster

**Tool:** Hydra CLI

This is the most important step. An empty cluster needs:

1. ArgoCD (the GitOps engine)
2. Infrastructure services (certificates, ingress, monitoring, policies)
3. Application infrastructure (databases, message brokers)
4. The actual applications

But there's a **chicken-and-egg problem** (see [Bootstrap Explained](bootstrap-explained.md)):

- The SOPS operator needs to run to decrypt secrets
- But the operator needs an image pull secret to download its container image
- That pull secret is itself encrypted with SOPS

Hydra's `--bootstrap` flag solves this by decrypting secrets locally and pushing them directly to the cluster.

```bash
# 1. Set the Hydra context
export HYDRA_CONTEXT=/path/to/gitops-repository/clusters/<group>/<cluster>

# 2. Verify you're connected to the right cluster
hydra gitops validate-current-context <cluster>

# 3. Bootstrap: decrypt all secrets + install everything
hydra gitops apply in-cluster.** --bootstrap
```

What happens during bootstrap:

1. Hydra renders all Helm charts with the merged values
2. It decrypts all SOPS secrets locally (bypassing the not-yet-running operator)
3. It creates the image pull secrets so pods can download container images
4. It applies webhook configurations in a disabled state (`failurePolicy: Ignore`) so bootstrap jobs can already reference them safely
5. It installs everything else in dependency order (CRDs first, then operators, then apps)
6. It waits for pods to be ready before enabling webhooks in provider dependency order
7. It adjusts ArgoCD AppProject sync (default bootstrap policy uses **`keep-or-prevent`**) so ArgoCD does not interfere during setup

After bootstrap:

```bash
# 4. Hand control to ArgoCD for ongoing management
hydra gitops sync auto in-cluster.**
```

ArgoCD now takes over. From this point on, changes go through Git and ArgoCD syncs them automatically.

## Phase 4: GitOps Takes Over

**Tool:** ArgoCD (running on the cluster)

Once the cluster is bootstrapped:

1. Developers and operators make changes by editing files in Git
2. They push commits (ideally through pull requests for review)
3. ArgoCD detects the changes and syncs them to the cluster
4. If something goes wrong, revert the Git commit to roll back

For day-to-day operations, you rarely need to run Hydra commands. The typical flow is:

```bash
# Edit configuration
vim gitops-repository/clusters/test/example-dev/in-cluster/cluster-infra/values.yaml

# Preview
hydra gitops diff in-cluster.cluster-infra.*

# Apply (or just commit + push and let ArgoCD handle it)
hydra gitops apply in-cluster.cluster-infra.*
```

## Maintenance Operations

### Updating an Application

```bash
# 1. Preview what would change
hydra gitops diff in-cluster.cluster-infra.cert-manager

# 2. Apply the change
hydra gitops apply in-cluster.cluster-infra.cert-manager
```

### Maintenance Window

When you need to make temporary manual changes without ArgoCD reverting them:

```bash
# 1. Freeze ArgoCD sync
hydra gitops sync prevent in-cluster.cluster-infra.*

# 2. Do your maintenance work
kubectl ...

# 3. Unfreeze ArgoCD
hydra gitops sync auto in-cluster.cluster-infra.*
```

### Safe Uninstall and Reinstall

```bash
# 1. Backup secrets first
hydra gitops backup create in-cluster.cluster-infra.cert-manager

# 2. Uninstall
hydra gitops uninstall in-cluster.cluster-infra.cert-manager

# 3. Reinstall
hydra gitops apply in-cluster.cluster-infra.cert-manager

# 4. Restore the backed-up secrets
hydra gitops backup restore in-cluster.cluster-infra.cert-manager --create-namespaces
```

### Temporary Scale Down

```bash
# 1. Freeze ArgoCD
hydra gitops sync prevent in-cluster.demo.*

# 2. Scale down
hydra gitops scale down in-cluster.demo.*

# 3. Later, scale back up
hydra gitops scale up in-cluster.demo.*

# 4. Unfreeze ArgoCD
hydra gitops sync auto in-cluster.demo.*
```

## Installation Order

Hydra handles dependency order automatically, but for reference, the logical installation order is:

```text
1. sops-secrets-operator     ← Needs to run first to decrypt secrets
2. cert-manager              ← Issues TLS certificates
3. ingress-nginx             ← Routes external traffic
4. external-dns              ← Creates DNS records
5. kyverno                   ← Enforces policies (e.g., image pull secrets)
6. dex                       ← Authentication (needs cert-manager for TLS)
7. kube-prometheus-stack      ← Monitoring
8. fluent-bit                 ← Logging
9. Demo infrastructure        ← Kafka, PostgreSQL, ClickHouse
10. Demo services             ← The actual application microservices
11. ArgoCD                    ← Installed early but syncs later
```

## Cluster Overview

This project currently manages:

| Cluster | Purpose | Root Apps |
| --- | --- | --- |
| example-dev | Test cluster with full Demo | argocd, cluster-infra, cicd, demo-infra, demo |
| example-user-dev | Developer test cluster | argocd, cluster-infra |
| vsphere-dev | CI/CD cluster | argocd, cluster-infra, cicd |
| mgmt-dev | Management dev cluster | argocd, cluster-infra |
| poc (cloud-poc) | Cloud POC proof of concept | argocd, cluster-infra |

## Next Steps

- [Bootstrap Explained](bootstrap-explained.md) — deep dive into the chicken-and-egg problem
- [CLI Reference](../README.md) — full Hydra command documentation
- [Quickstart](../quickstart.md) — try your first Hydra commands
