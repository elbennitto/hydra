# Builtin Presets

All presets that ship with Hydra. Enable the presets matching your cluster's distribution.

## Kubernetes Core

| Preset | Default Enabled | Purpose |
|--------|----------------|---------|
| `kubernetes` | Yes | Core Kubernetes components (API server resources) |
| `coredns` | Yes | CoreDNS Deployment, ConfigMap, Service |
| `kube-scheduler` | No | Scheduler-related resources |
| `kube-controller-manager` | No | Controller manager resources |
| `kube-proxy` | No | kube-proxy DaemonSet and related resources |
| `kubernetes-dynamic-resource-allocation` | No | Kubernetes Dynamic Resource Allocation resources, activated by `kubernetes` on supported minors |
| `kubernetes-volume-attributes-class` | No | Kubernetes VolumeAttributesClass resources, activated by `kubernetes` on supported minors |

## Networking

| Preset | Default Enabled | Purpose |
|--------|----------------|---------|
| `flannel` | No | Flannel CNI resources |
| `canal` | No | Canal (Calico + Flannel) CNI resources |
| `calico` | No | Calico CNI resources |

## Distributions

| Preset | Default Enabled | Purpose |
|--------|----------------|---------|
| `talos` | No | Talos Linux-specific resources |
| `k3s` | No | K3s-specific components |
| `k3d` | No | K3d (K3s in Docker) specifics |
| `kubermatic` | No | Kubermatic-managed cluster resources |
| `gardener` | No | Gardener shoot control-plane agents and target-cluster RBAC |
| `syseleven` | No | SysEleven cloud-specific resources |
| `metakube` | No | MetaKube platform resources |
| `cloud-poc` | No | Cloud POC Gardener shoot bundle |

## Storage

| Preset | Default Enabled | Purpose |
|--------|----------------|---------|
| `quobyte` | No | Quobyte storage resources |
| `cinder` | No | OpenStack Cinder CSI resources |
| `cinder-controller` | No | Cinder controller resources |
| `local-path-provisioner` | No | Local path provisioner (K3s default) |

## Monitoring & Utilities

| Preset | Default Enabled | Purpose |
|--------|----------------|---------|
| `metrics-server` | No | Metrics Server resources |
| `syseleven-node-problem-detector` | No | Node problem detector |
| `cloudinit` | No | Cloud-init related resources |
| `monex` | No | Gardener monitoring extension resources |

## K3s Add-ons

| Preset | Default Enabled | Purpose |
|--------|----------------|---------|
| `k3s-addon-coredns` | No | K3s CoreDNS deployment |
| `k3s-addon-traefik` | No | K3s Traefik ingress |
| `k3s-addon-metrics-server-deployment` | No | K3s metrics server Deployment |
| `k3s-addon-metrics-server-service` | No | K3s metrics server Service |
| `k3s-addon-local-storage` | No | K3s local storage class |
| ... | No | Additional K3s add-on variants |

## Which Presets for Which Distribution?

| Distribution | Recommended Presets |
|--------------|-------------------|
| Talos | `kubernetes`, `coredns`, `talos` plus any matching CNI/runtime presets |
| K3s | `kubernetes`, `k3s`, `k3s-addon-*` |
| SysEleven/MetaKube | `kubernetes`, `coredns`, `kube-*`, `syseleven`, `metakube` |
| Kubermatic | `kubernetes`, `coredns`, `kube-*`, `kubermatic` |
| Cloud-POC/Gardener | `kubernetes`, `coredns`, `cloud-poc` |
| Generic | `kubernetes`, `coredns`, `kube-*` + your CNI preset |

## Enabling Presets

In your cluster's values:

```yaml
global:
  hydra:
    presets:
      talos:
        enabled: true
      flannel:
        enabled: false
```

## See Also

- [Preset Overrides](preset-overrides.md) — Customizing preset behavior
- [Preset Activation](preset-activation.md) — How activation works
