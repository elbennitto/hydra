# hydra ci run publish

Package Helm charts for the registry.

## Synopsis

```bash
hydra ci run publish [flags]
```

## Description

Packages Helm charts, signs them with the private key from `hydra ci secrets create` plus the public signing metadata in `ci.sign` inside `.hydra-ci.yaml`, and, in CI mode, pushes both chart and provenance signature to the configured chart registry (for example Harbor OCI).

Hydra always validates that `ci.sign.helm.name` and `ci.sign.helm.key` in `.hydra-ci.yaml` match the values derived from `ci.sign.helm.publicKey`.

`hydra ci secrets create` also adds the generated signing identity to `ci.sign.helm.validKeys` as `{ key, name }` and the generated Cosign public key to `ci.sign.cosign.validKeys`, so the same config can declare which signers are accepted during later validation.

Before signing, Hydra additionally validates that `ci.sign.helm.name`, `ci.sign.helm.key`, and `ci.sign.helm.publicKey` in `.hydra-ci.yaml` exactly match the private key stored in `.hydra-ci-secrets.sops.yaml`. If any value differs, `hydra ci run publish` fails with an error instead of signing with inconsistent metadata.

If `ci.sign.helm.name` or `ci.sign.helm.key` is missing, Hydra also fails while loading the signing configuration and includes the expected value derived from `ci.sign.helm.publicKey` in the error message.

Use `--skip-signing` to package and publish charts without provenance signatures. Hydra logs this as `WARN` because the published chart will be unsigned.

In CI mode, Hydra also requires at least one writable OCI registry token in
`.hydra-ci-secrets.sops.yaml`. Writable tokens are declared with
`secrets.registryTokens[].upload: true`. Tokens without `upload: true` are
treated as read-only and are ignored for `hydra ci run publish`.

Example CI secrets snippet:

```yaml
secrets:
  registryTokens:
    - registry: harbor.example.test
      username: robot$hydra-read
      token: <read-token>
    - registry: harbor.example.test
      username: robot$hydra-write
      token: <write-token>
      upload: true
```

If no writable token is configured, Hydra aborts before the first remote OCI
operation and prints an extended error that shows the expected secret shape.

## Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Preview without pushing |
| `--chart <path>` | Publish only the selected chart. Repeatable. Accepts `<group>/<app>/<env>` or a repo-relative chart path. |
| `--force-run` | Allow publishing explicitly selected charts even when `HEAD` is not at their release commit. |
| `--force-publish-upload` | Ignore `remote chart already exists` and upload the chart version again. |
| `--skip-signing` | Skip provenance signing and publish the chart unsigned. Logged as a warning. |

## Examples

```bash
hydra ci run publish .hydra-ci.yaml
hydra ci run publish --dry-run .hydra-ci.yaml
hydra ci run publish --chart demo/service-ui/dev .hydra-ci.yaml
hydra ci run publish --chart apps/demo/service-ui/dev --force-run .hydra-ci.yaml
hydra ci run publish --force-publish-upload .hydra-ci.yaml
hydra ci run publish --skip-signing .hydra-ci.yaml
```

## See Also

- [hydra ci run release](release.md)
- [Workflow: CI Pipeline](../../appendix/workflows/ci-pipeline.md)
