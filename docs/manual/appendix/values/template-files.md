# templateFiles in Values

`templateFiles` mutates Helm chart template files before Helm renders the chart. Use it when a local chart file should replace a dependency template, or when specific template files should be removed from the loaded chart.

This block is declared only in Helm values under `global.hydra.templateFiles`. It is not a post-render patch step and is not merged from Hydra configuration `ConfigMap` `data.hydra`.

## Structure

```yaml
global:
  hydra:
    templateFiles:
      move:
        - from: templates/local-deployment.yaml
          to: charts/upstream/templates/deployment.yaml
      delete:
        - '^templates/obsolete-.*\.yaml$'
        - '^charts/upstream/templates/.*secret.*\.yaml$'
```

## Fields

### move

List of file moves applied to the loaded chart before `helm template` runs.

Each item has:

- `from`: exact source path in the loaded chart
- `to`: exact destination path in the loaded chart

Behavior:

- `from` must exist
- `to` must exist
- the contents of `from` are copied onto `to`
- after that, `from` is removed from the chart

This is the main mechanism for replacing a dependency template with a local template.

### delete

List of regular expressions matched against loaded chart file paths.

Behavior:

- each entry must be a valid regex
- each regex must match at least one file, otherwise Hydra fails the render
- matching files are removed before Helm renders the chart

## Paths

Paths use the names from the loaded Helm chart, for example:

- `templates/deployment.yaml`
- `charts/upstream/templates/configmap.yaml`

For dependency templates, use the `charts/<dependency>/templates/...` path as it appears in the loaded chart.

## Example: Replace a Dependency Template

```yaml
global:
  hydra:
    templateFiles:
      move:
        - from: templates/deployment.yaml
          to: charts/upstream/templates/deployment.yaml
```

This keeps the local chart file as the source of truth, but makes Hydra render it in place of the dependency template.

## Example: Remove Test or Secret Templates

```yaml
global:
  hydra:
    templateFiles:
      delete:
        - '^charts/upstream/templates/tests?/.*\.yaml$'
        - '^charts/upstream/templates/.*secret.*\.yaml$'
```

## Order of Application

1. Hydra loads the chart and its dependencies
2. `templateFiles.move` is applied
3. `templateFiles.delete` is applied
4. Helm renders the modified chart
5. Later steps such as `templatePatches` operate on rendered Kubernetes manifests

Use `templateFiles` for source-level chart file changes. Use [`templatePatches`](template-patches.md) for rendered manifest mutations.

## See Also

- [global.hydra Reference](global-hydra.md)
- [templatePatches](template-patches.md)
- [hydra local template](../../commands/local/template.md)
