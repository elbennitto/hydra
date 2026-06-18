# hydra cosign

Run the embedded [Cosign](https://github.com/sigstore/cosign) CLI (Sigstore Cosign v2) with the same interface as the standalone `cosign` binary.

## Synopsis

```bash
hydra cosign [cosign arguments and flags]
```

## Description

`hydra cosign` registers upstream Cosign's Cobra command tree under Hydra (`hydra-go/cli/cmd/root.go` via `github.com/sigstore/cosign/v2/cmd/cosign/cli`). All subcommands, flags, and behavior match the embedded Cosign release.

Hydra skips the usual stderr welcome line for `hydra cosign` (and other delegated tool CLIs) so stdout stays suitable for pipes and scripts.

Hydra uses the same Cosign library internally for OCI chart signing in `hydra ci run publish` and signature verification in `hydra ci run verify`. `hydra cosign` is for ad-hoc signing, verification, or key management without installing a separate `cosign` binary.

For subcommands, flags, and environment variables, use the official upstream documentation: [Cosign documentation](https://docs.sigstore.dev/cosign/overview/) (same CLI as `hydra cosign`; only the command prefix differs).

## Examples

```bash
# Same as: cosign version
hydra cosign version

# Verify a container image signature
hydra cosign verify --key cosign.pub ghcr.io/example/image:latest

# Sign a container image
hydra cosign sign --key cosign.key ghcr.io/example/image:latest

# Generate a key pair
hydra cosign generate-key-pair

# Show help for a subcommand
hydra cosign verify --help
```

## See also

- [Cosign documentation](https://docs.sigstore.dev/cosign/overview/) — upstream CLI reference
- [Delegated tool CLIs](../README.md#delegated-tool-clis) in the command reference
- [`hydra ci run publish`](../ci/publish.md) — Hydra-native chart packaging and signing
- [`hydra ci run verify`](../ci/verify.md) — Hydra-native chart signature verification
