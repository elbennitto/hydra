# Wrapped Commands

The commands in this section are delegated upstream CLIs that Hydra wraps into the `hydra` binary.

## Why this exists

Use these commands when you want a single executable for CI/CD and operations workflows.

In particular, this layout is useful for single-binary container images:

- The container only needs `hydra`.
- No additional `helm`, `cosign`, or `yq` binary installation is required.
- Tool versions stay aligned with the Hydra release.

## Commands

- [`hydra cosign`](cosign.md)
- [`hydra helm`](helm.md)
- [`hydra yq`](yq.md)

For details on how delegated CLIs fit into daily workflows, see [Delegated tool CLIs](../README.md#delegated-tool-clis).
