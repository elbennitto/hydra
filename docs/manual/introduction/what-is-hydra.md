# What is Hydra?

Hydra is a tool that automates deploying and managing applications on Kubernetes clusters. Think of it as an **autopilot for your servers** — instead of manually installing software on each server one by one, you describe what you want in files, and Hydra makes it happen.

## The Problem Hydra Solves

Imagine you have 10 Kubernetes clusters, each running 20+ applications. Without Hydra, you would need to:

1. SSH into (or connect to) each cluster
2. Run `helm install` for each application, one by one
3. Remember the right configuration values for each cluster
4. Manually track what's installed where
5. Repeat everything when a new cluster is needed

This is a full-day job just for one cluster. For 10 clusters, it's a nightmare.

## How Hydra Solves It

Hydra replaces all of that with:

1. **A directory structure** where you define "cluster X should run applications A, B, C with these settings"
2. **A single command** to install everything: `hydra gitops apply prod.**`
3. **Automatic value merging** — shared settings are defined once, cluster-specific overrides are defined per cluster
4. **A web UI** to visualize what's deployed, what depends on what, and what the configuration looks like

Hydra currently exposes these command modes:

- **`hydra local`** for working only with the local Hydra definitions.
- **`hydra gitops`** for working with those local definitions plus the live Kubernetes cluster.
- **`hydra cluster`** as the reserved future command surface for cluster-only workflows.

## Hydra in One Sentence

> Hydra is a CLI tool and directory structure that lets you define, deploy, and manage all applications across multiple Kubernetes clusters from a single Git repository.

## The Three Parts of Hydra

### 1. The Directory Structure

Two directories in this repository define everything:

- **`gitops-repository/`** — defines which clusters exist and their configuration (values, secrets)
- **`charts-repository/`** — contains the shared Helm charts (application packages) that clusters can install

### 2. The CLI (`hydra-go`)

A command-line tool written in Go. Key commands:

| What you want to do | Command |
| --- | --- |
| See what an app's config looks like | `hydra local values prod.infra.cert-manager` |
| Preview what would be deployed | `hydra local template prod.infra.cert-manager` |
| See what's different on the live cluster | `hydra gitops diff prod.infra.*` |
| Deploy everything | `hydra gitops apply prod.**` |
| Set up a brand-new cluster from scratch | `hydra gitops apply prod.** --bootstrap` |

### 3. The Web UI (`hydra-ui`)

A static web page (no backend server needed) that loads an exported file and shows you:

- Which applications are deployed
- How they depend on each other (e.g., "this app needs that database secret")
- What configuration values each app receives
- Who has permissions to do what (RBAC)

## How It Fits Into the Bigger Picture

```text
┌─────────────────────────────────────────────────────────────────┐
│                        This Repository                          │
│                                                                 │
│  ┌──────────────────┐  ┌────────────────────┐  ┌─────────────┐  │
│  │ gitops-repository│  │ charts-repository  │  │   hydra-go/ │  │
│  │                  │  │                    │  │             │  │
│  │ "Which clusters  │  │ "What apps are     │  │ "The tool   │  │
│  │  exist and what  │  │  available and how │  │  that puts  │  │
│  │  they should run"│  │  they're packaged" │  │  it all     │  │
│  │                  │  │                    │  │  together"  │  │
│  └──────────────────┘  └────────────────────┘  └─────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

## The Goal

The goal of Hydra is to turn cluster setup from a **full-day manual process** into a **single command**. And to make it so that Git is the only source of truth — if you want to know what's running on a cluster, you look at the Git repository, not at the cluster itself. This approach is called **GitOps**.

## Next Steps

- [Key Concepts](concepts.md) — learn the technical terms used throughout this documentation
- [Quickstart](../quickstart.md) — the shortest path to running your first Hydra command
