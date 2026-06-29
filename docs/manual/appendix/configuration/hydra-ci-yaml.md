# Configuration: .hydra-ci.yaml

Configuration file for `hydra ci`.

## Location

```text
charts-repository/.hydra-ci.yaml
```

## Structure

```yaml
ci:
  rootAppsPath: apps
  upstreamBranch: origin/HEAD
  environments:
    - dev
    - stage
    - prod
  appGroups:
    - name: demo
      path: apps/demo
    - name: cluster-infra
      path: apps/cluster-infra
  registry: oci://harbor.example.com/charts
  promote:
    promotableRootApps:
      - demo
  teams:
    webhookUrl: ""
    channels: {}
```

## Key Fields

| Field | Description |
|-------|-------------|
| `ci.rootAppsPath` | Directory that contains the chart app groups |
| `ci.upstreamBranch` | Default upstream ref for `release` and `promote` when `--target-branch` is not set; defaults to `origin/HEAD` |
| `ci.environments` | Ordered environment chain used by promote, for example `dev -> stage -> prod` |
| `ci.appGroups` | Explicit app-group name/path mapping used for chart discovery |
| `ci.registry` | OCI registry target for `hydra ci run publish` |
| `ci.promote.promotableRootApps` | Root apps that may be promoted |
| `ci.teams.webhookUrl` | Default Teams webhook for CI notifications |
| `ci.teams.channels` | Per-app-group Teams webhook overrides |
| `ci.autoSteps` | Optional override for the default `hydra ci run auto` stage order |

## `ci.upstreamBranch`

`ci.upstreamBranch` controls which upstream ref Hydra uses as the default checkout base for commit-producing CI steps when `--target-branch` is not provided.

- Default: `origin/HEAD`
- Typical explicit value: `origin/main`
- Accepted forms: `origin/main`, `main`, or `refs/remotes/origin/main`

Behavior:

- Hydra resolves the configured upstream ref and logs which branch/ref it resolved to.
- If the matching local branch already exists, Hydra checks it out.
- If the local branch does not exist yet, Hydra creates it from the configured upstream ref and logs that decision.
- With the default `origin/HEAD`, Hydra follows the remote default branch automatically, which is useful in CI clones where the default branch name is not hardcoded.

Example:

```yaml
ci:
  rootAppsPath: apps
  upstreamBranch: origin/main
  environments:
    - dev
    - stage
    - prod
```

## See Also

- [Workflow: CI Pipeline](../workflows/ci-pipeline.md)
- [Commands: CI](../../commands/ci/README.md)
- [hydra ci config](../../commands/ci/config.md)
