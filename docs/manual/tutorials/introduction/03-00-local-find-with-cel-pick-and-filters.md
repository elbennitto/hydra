# Extracting Data from Manifests

This chapter continues from the final setup of the previous tutorial and focuses on `hydra local find`.

With `hydra local find`, you can query rendered entities from one app, shape the output with `--pick`, and narrow the result set with `--include` and `--exclude`.

## Goal

By the end of this chapter, you can:

- run `hydra local find <app-id>` on the existing app setup
- use `--pick` to build custom scalar and object output per matched entity
- use `--include` and `--exclude` with CEL expressions
- access Helm-rendered objects via `templateEntity` in filters and picks

## Related CLI Pages

- [`hydra local find`](../../commands/local/find.md) — query rendered entities and shape output
- [`hydra local template`](../../commands/local/template.md) — compare full rendered manifests
- [CEL Appendix](../../appendix/cel/README.md) — CEL syntax, variables, functions, and examples used by `--pick`, `--include`, and `--exclude`

## Prerequisite

Complete [Inspect Manifests and Their Templates](02-00-adding-manifests-to-the-helm-charts.md) first.

At the start of this chapter, your directory structure should still match the final step of the previous chapter:

```tree
~/group/
    values.yaml
    context/
        cluster/
            app/
                Chart.yaml
                values.yaml
                templates/
                    deployment.yaml
```

## Step 1: Start with `hydra local find <app-id>`

Run:

```bash
hydra local find cluster.app
```

In this first run, no `--pick` is set, so Hydra uses the default pick expression `id` and prints the IDs of the matched entities.

Since no `--include` or `--exclude` is used in this call, the output simply contains all defined IDs.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/03-01-find-default-id.cast"></div>

## Step 2: Shape the Output with `--pick`

With `--pick`, you provide a CEL expression that is evaluated per matched entity to build the YAML result list.

The default value for `--pick` is `id`, but you can return custom strings or JSON-like objects.

Example: create strings

```bash
hydra local find cluster.app --pick 'name + "@" + ns'
```

Example: create JSON objects

```bash
hydra local find cluster.app --pick '{"group":group, "namespace":ns, "name":name, "version": version}'
```

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/03-02-find-pick-output.cast"></div>

## Step 3: Use `--include` and `--exclude` with `templateEntity`

`--include` and `--exclude` also work on `hydra local find`.

With `templateEntity`, you can access the object returned by Helm template rendering. CEL expressions for `--include` and `--exclude` must evaluate to boolean values (or `key not found`). Example include filter:

```bash
hydra local find cluster.app --include 'templateEntity.spec.template.spec.containers != null'
```

The `!= null` at the end is required so the expression evaluates to a boolean value.

This keeps only entries where that field exists. If evaluation hits `key not found`, those entries are skipped automatically.

When only this include expression is used, no additional filter on `kind` is active. Because of that, multiple resource kinds can appear in the result.

`--exclude` works the same way and removes entities that match the expression.

For example:

```bash
hydra local find cluster.app --exclude 'kind == "Service"'
```

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/03-03-find-include-exclude.cast"></div>

## Step 4: Use `templateEntity` inside `--pick`

You can also use `templateEntity` in `--pick` to include rendered data directly in the output.

`key not found` cases are skipped automatically, so in this scenario `--pick` can be used instead of `--include`.

```bash
hydra local find cluster.app --pick '{"id":id, "images":templateEntity.spec.template.spec.containers.map(c, c.image)}'
```

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/03-04-find-templateentity-pick.cast"></div>

## Summary

At the end of this chapter, you can query rendered entities with `hydra local find`, shape the output with CEL via `--pick`, and filter against rendered data with `--include` / `--exclude` and `templateEntity`.
