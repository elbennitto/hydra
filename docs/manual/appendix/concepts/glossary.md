# Key Concepts

This page explains every technical term used in this project. Read it once before diving into the rest of the documentation. Terms are grouped by topic and build on each other.

## Containers

A **container** is a lightweight, isolated package that contains an application and everything it needs to run (code, libraries, settings). Think of it as a shipping container for software — the same container runs identically on any machine.

**Docker** is the most common tool for building and running containers. When someone says "Docker image," they mean a container package.

## Kubernetes (K8s)

Kubernetes is a system that runs containers across multiple machines. You tell Kubernetes "I want 3 copies of this app running," and it handles starting them, restarting them if they crash, and distributing them across machines.

### Cluster

A **cluster** is a group of machines (physical or virtual) working together under Kubernetes. This project manages multiple clusters for different purposes — development, testing, production, CI/CD.

### Nodes

A **node** is a single machine in the cluster. There are two types:

- **Control plane nodes** — the "brain" of the cluster. They keep track of what should be running and where. They make decisions but don't run your applications.
- **Worker nodes** — the "muscles." They receive instructions from the control plane and actually run your applications.

When you run `kubectl` commands, you're talking to a control plane node, which then distributes work to worker nodes.

#### The Split-Brain Problem

In a distributed system, if the network splits and nodes can't communicate, each group might think it's in charge. This is the **split-brain problem**. To prevent it, Kubernetes requires more than 50% of control plane nodes to be online to make decisions. That's why production clusters typically have 3 or 5 control plane nodes (odd numbers, so there's always a majority).

### Pod

A **pod** is the smallest unit in Kubernetes. It's one or more containers running together on the same node, sharing the same network address. Most pods run a single container.

### Deployment

A **Deployment** tells Kubernetes "run X copies (replicas) of this container image and keep them running." If a pod crashes, the Deployment automatically creates a new one.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  replicas: 3          # Run 3 copies
  template:
    spec:
      containers:
        - name: my-app
          image: my-app:1.0   # The container image to run
```

### Service

A **Service** provides a stable network address that routes traffic to pods. Pods come and go (they restart, scale up/down), but the Service address stays the same.

### Namespace

A **namespace** is a virtual folder for organizing resources. For example, you might have a `cert-manager` namespace for certificate management and an `demo` namespace for the Demo application. Some resources (like nodes) are cluster-wide and don't belong to any namespace.

To see pods in a specific namespace: `kubectl get pods -n cert-manager`

### ConfigMap

A **ConfigMap** holds non-secret configuration data as key-value pairs. It can be mounted into a pod as a file or injected as environment variables.

### Secret

A **Secret** is like a ConfigMap but for sensitive data (passwords, API keys, TLS certificates). The values are base64-encoded (not encrypted — just encoded). You can also use `stringData` for plain text that Kubernetes will encode for you.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-secret
type: Opaque
data:
  password: cGFzc3dvcmQxMjM=    # base64 of "password123"
```

Secrets and ConfigMaps can be mounted into pods in two ways:

- As **files** inside the container
- As **environment variables**

### ServiceAccount

A **ServiceAccount** is an identity for pods. When a pod needs to talk to the Kubernetes API (or other services), it uses a ServiceAccount to authenticate. Different ServiceAccounts can have different permissions.

### RBAC (Role-Based Access Control)

**RBAC** controls who (or what) can do what in the cluster. It works with:

- **Role** — defines a set of permissions (e.g., "can read pods in namespace X")
- **ClusterRole** — like a Role but applies across all namespaces
- **RoleBinding** — assigns a Role to a user or ServiceAccount
- **ClusterRoleBinding** — assigns a ClusterRole cluster-wide

### Labels and Annotations

Every Kubernetes resource can have:

- **Labels** — key-value pairs used for selecting and grouping resources (e.g., `app: cert-manager`)
- **Annotations** — key-value pairs for metadata that isn't used for selection (e.g., build timestamps, documentation links)

Some annotations are managed by Kubernetes itself and should not be edited manually.

### CRD (Custom Resource Definition)

Kubernetes comes with built-in resource types (Pod, Service, Deployment, etc.). A **CRD** lets you add your own resource types. For example, ArgoCD adds a `kind: Application` resource type, and the SOPS operator adds `kind: SopsSecret`.

When you see a YAML file with an unfamiliar `kind:`, it's likely a custom resource defined by a CRD.

### Webhooks

**Webhooks** are HTTP callbacks that Kubernetes triggers when certain events happen (e.g., when a resource is created or modified). Some operators use webhooks to validate or modify resources. During cluster setup, webhooks can cause problems if the operator pod isn't running yet — Hydra handles this by applying webhook configurations early in a disabled state (`failurePolicy: Ignore`) and enabling them later once the backing workloads are ready.

## Helm

**Helm** is the package manager for Kubernetes — like `apt` for Ubuntu or `brew` for macOS, but for Kubernetes applications.

### Chart

A **Helm chart** is a package. It's a directory containing:

```tree
my-chart/
    Chart.yaml       # Name, version, dependencies
    values.yaml      # Default settings
    templates/       # Kubernetes YAML files with placeholders
        deployment.yaml
        service.yaml
        configmap.yaml
```

### Values

**Values** are the settings you plug into a chart's templates. Different environments use different values.

```yaml
# values.yaml
replicas: 3
image:
  repository: my-app
  tag: "1.0"
```

### Templates

**Templates** are Kubernetes YAML files with Go template placeholders:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
spec:
  replicas: {{ .Values.replicas }}
```

When Helm renders the template with the values above, it produces plain Kubernetes YAML with `replicas: 3`.

### Key Helm Commands

| Command | What it does |
| --- | --- |
| `helm install <name> <chart>` | Install a chart on the cluster |
| `helm upgrade <name> <chart>` | Update an installed chart |
| `helm template <chart>` | Render templates locally (no cluster needed) |
| `helm uninstall <name>` | Remove an installed chart |

Hydra embeds Helm internally — you don't need to install Helm separately.

## ArgoCD

**ArgoCD** is a GitOps tool for Kubernetes. It watches a Git repository and automatically keeps the cluster in sync with what's defined in Git.

### How ArgoCD Works

1. You define an **Application** resource that points to a Git repo + path
2. ArgoCD reads the Helm charts (or plain YAML) from that path
3. ArgoCD compares the Git-desired state with the actual cluster state
4. If they differ, ArgoCD either:
   - **Auto-syncs** — automatically applies the changes
   - **Shows the diff** — waits for manual approval

### Application

An ArgoCD **Application** is a Kubernetes custom resource that tells ArgoCD: "Watch this Git path and deploy it to this cluster."

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: cert-manager
spec:
  source:
    repoURL: https://github.com/your-org/your-repo.git
    path: charts-repository/apps/cluster-infra/cert-manager/dev
  destination:
    server: https://kubernetes.default.svc
    namespace: cert-manager
```

### AppProject

An **AppProject** groups Applications and controls their permissions — what they can deploy, which clusters they can target, and which namespaces they can use.

### Sync

**Sync** is the act of applying the Git-desired state to the cluster. ArgoCD can sync automatically or wait for manual approval.

**Sync** settings on AppProjects control *when* and *how* ArgoCD may reconcile. During maintenance, use **manual** or **prevent** (see [`hydra gitops sync`](../commands/cluster/sync.md)) so ArgoCD does not immediately undo temporary changes.

### App of Apps Pattern

Instead of creating each ArgoCD Application manually, you create **one root Application** that contains Helm templates that generate all the other Applications. This is the **App of Apps pattern**, and it's the core pattern that Hydra is built around.

```text
Root Application (ArgoCD Application)
    generates → cert-manager Application
    generates → ingress-nginx Application
    generates → dex Application
    generates → kube-prometheus-stack Application
```

In this repo, root apps are defined in `gitops-repository/` and the child app charts live in `charts-repository/`.

## GitOps

**GitOps** means Git is the single source of truth for your infrastructure. Every change goes through Git:

1. You want to change a setting → you edit a file in Git
2. You push the commit
3. ArgoCD (or Hydra) detects the change and applies it to the cluster

Benefits:

- **Audit trail** — every change is a Git commit with author and timestamp
- **Rollback** — revert a commit to undo a change
- **Review** — use pull requests to review changes before they're applied
- **Reproducibility** — clone the repo and you have the complete cluster definition

## SOPS (Secrets OPerationS)

**SOPS** encrypts files so that sensitive data (passwords, certificates, API keys) can safely live in Git. Only authorized people or systems with the right encryption keys can decrypt them.

Files encrypted with SOPS have the `.sops.yaml` extension in this repo. They look like regular YAML but with encrypted values.

### The Bootstrap Problem

When setting up a new cluster, there's a chicken-and-egg problem:

1. The SOPS secrets operator needs to run to decrypt secrets
2. But the operator needs a container image pull secret to start
3. That pull secret is itself encrypted with SOPS

Hydra's `--bootstrap` flag solves this by decrypting secrets locally and pushing them to the cluster directly, bypassing the operator for the initial setup.

## Talos Linux

**Talos** is a minimal, secure Linux operating system designed specifically for Kubernetes. Unlike regular Linux:

- There is **no SSH access** — you can only manage it through an API (`talosctl`)
- It's **immutable** — the OS files can't be modified at runtime
- It's **minimal** — only contains what's needed to run Kubernetes

This makes it very secure and predictable. In this project, VMs running Talos are created with Terraform and configured with `talos.sh`.

## Terraform and Terragrunt

- **Terraform** — Infrastructure as Code tool. You define virtual machines, networks, etc. in configuration files, and Terraform creates or modifies them on the infrastructure provider (VMware vSphere in this project).
- **Terragrunt** — A wrapper around Terraform that reduces repetition when managing multiple environments (dev, staging, prod) with similar but not identical configurations.

## Hydra-Specific Terms

### Context

The **Hydra context** is the root directory that Hydra reads from. It contains cluster definitions, charts, values, and encrypted secrets. Set it with `--hydra-context <path>` or the `HYDRA_CONTEXT` environment variable.

### App ID

An **App ID** uniquely identifies an application using dot-separated names:

| Format | Example | Meaning |
| --- | --- | --- |
| `cluster.rootApp` | `example-dev.cluster-infra` | Root app "cluster-infra" on cluster "example-dev" |
| `cluster.rootApp.childApp` | `example-dev.cluster-infra.cert-manager` | Child app "cert-manager" under "cluster-infra" on "example-dev" |

Wildcards: `example-dev.*` matches all root apps, `example-dev.**` matches everything on the cluster.

### Values Hierarchy

Hydra merges values from multiple levels, each overriding the previous:

```text
Context values (shared by all clusters)
    ↓ overridden by
Cluster values (specific to one cluster)
    ↓ overridden by
Root app values (specific to one root app on a cluster)
    ↓ overridden by
Child app values (specific to one child app)
```

This means you define defaults once and only override what's different per cluster or per app.

## Next Steps

- [What is the gitops-repository?](../concepts/gitops-repository.md)
- [What is the charts-repository?](../concepts/charts-repository.md)
- [Quickstart](../quickstart.md)
