# Inspect Manifests and Their Templates

This chapter continues with the root app from the previous tutorial and extends its minimal Helm chart until it renders real Kubernetes manifests. Along the way, you stay in normal Helm territory: you add template files, define chart values, declare a regular Helm dependency in `Chart.yaml`, and inspect the resulting values and manifests with Hydra's local tooling.

## Goal

By the end of this chapter, the app chart renders its own `Deployment` and resources from a dependency chart, and you can inspect the computed Helm values, the rendered manifests, and the underlying Helm template sources with Hydra's local tooling. You will use `hydra local values` for merged chart values, `hydra local template` for rendered output, `hydra local source` for unrendered template files, and `--include` / `--exclude` filters to focus both views on the resources that matter for the current change.

## Related CLI Pages

- [`hydra local apps`](../../commands/local/apps.md) — resolve app-id patterns and verify app discovery before rendering
- [`hydra local template`](../../commands/local/template.md) — render manifests locally
- [`hydra local source`](../../commands/local/source.md) — print chart template sources, including dependencies
- [`hydra local values`](../../commands/local/values.md) — print the computed Helm values for one app
- [`hydra helm`](../../commands/wrapped/helm.md) — delegated Helm CLI used for comparison in this tutorial

## Prerequisite

Complete [Create a Hydra App](01-00-create-a-hydra-app.md) first.

At the start of this chapter, your directory structure should look like this:

```tree
~/group/
    values.yaml
    context/
        cluster/
            app/
                Chart.yaml
```

## Step 1: Create a Simple Deployment

Start by creating `~/group/context/cluster/app/templates/deployment.yaml` with this content:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    metadata:
      labels:
        app: app
    spec:
      containers:
        - name: app
          image: {{ .Values.image }}
          ports:
            - containerPort: 8080
```

Then create `~/group/context/cluster/app/values.yaml`:

```yaml
image: traefik/whoami:v1.11.0
global:
  hydra:
    path: .
```

This is still a very small Helm chart, but now the container image comes from `values.yaml` via `.Values.image`. The added `global.hydra.path: .` also gives Hydra the minimal app metadata it needs for local rendering. Together, that keeps the manifest reusable and makes later value overrides easier, while still giving Hydra one simple Kubernetes resource it can render locally.

So far, there is still no difference in the chart render itself between Hydra and Helm. Before rendering, it is useful to verify that Hydra discovers the app as expected:

```bash
hydra local apps
```

For this example, the output should include `cluster.app`.

You can then render the new chart with [`hydra local template`](../../commands/local/template.md); internally, Hydra uses [`helm template`](../../commands/wrapped/helm.md) for that local render.

That is why it is useful to compare both commands:

```bash
helm template "$HYDRA_CONTEXT/cluster/app"
```

```bash
hydra local template cluster.app
```

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/02-01-simple-deployment.cast"></div>

Example files on GitHub: <https://github.com/hydra-gitops/hydra/tree/main/docs/tutorials/introduction/02-01-simple-deployment>

In this step, [`hydra local template`](../../commands/local/template.md) uses [`helm template`](../../commands/wrapped/helm.md) for the chart render. One small difference is that, when the output is written to a terminal, Hydra automatically uses `yq` for syntax highlighting.

## Step 2: Add a Regular Helm Dependency

The render still looks almost the same as plain Helm so far. To show where Hydra keeps working with normal Helm charts, add a regular Helm dependency to the same example app.

In this tutorial we use [Mosquitto](https://helmforge.dev/docs/charts/mosquitto/), an MQTT server, as the additional chart. Any other Helm chart dependency would work the same way.

Update `~/group/context/cluster/app/Chart.yaml` like this:

```yaml
apiVersion: v2
name: app
version: 0.1.0
dependencies:
  - name: mosquitto
    version: 1.3.2
    repository: oci://ghcr.io/helmforgedev/helm
```

This is a normal Helm `dependencies` entry. There is no Hydra-specific syntax for it.

[`hydra local template`](../../commands/local/template.md) continues to work as before:

```bash
hydra local template cluster.app
```

Hydra still renders the app through Helm, so the dependency is resolved with the same Helm chart machinery that a regular `helm template` run would use.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/02-02-helm-dependency.cast"></div>

Example files on GitHub: <https://github.com/hydra-gitops/hydra/tree/main/docs/tutorials/introduction/02-02-helm-dependency>

## Step 3: Show the Source Templates

One feature that plain Helm does not offer in this form is printing all source templates of a chart, including templates that come from dependencies.

For templates in the local filesystem, you can still open the files directly. Dependency templates are less convenient: first Helm has to download the dependency, and then the templates have to be read from the packaged chart data instead of from your local `templates/` directory.

With [`hydra local source <app-IDs...>`](../../commands/local/source.md), for example `hydra local source cluster.app`, you can print those source templates directly:

```bash
hydra local source cluster.app
```

This prints the unrendered Helm template files, including templates from packaged dependencies such as Mosquitto.

As with [`hydra local template`](../../commands/local/template.md), terminal output is shown with syntax highlighting.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/02-03-source-templates.cast"></div>

Example files on GitHub: <https://github.com/hydra-gitops/hydra/tree/main/docs/tutorials/introduction/02-03-source-templates>

## Step 4: Filter the Rendered Output with `--include` and `--exclude`

Once a chart renders multiple resources, the full output can become noisy quite quickly. In this example, the app now renders its own `Deployment` plus several objects from the Mosquitto dependency.

[`hydra local template`](../../commands/local/template.md) can filter that rendered output directly:

- `--include` keeps only resources that match a CEL expression
- `--exclude` removes resources that match a CEL expression

That makes it easy to focus on one part of the render without changing the chart itself.

For example, to print only the `Deployment` objects:

```bash
hydra local template cluster.app --include 'kind == "Deployment"'
```

And to print everything except core `v1` resources and `apps/v1` resources:

```bash
hydra local template cluster.app --exclude 'apiVersion == "v1"' --exclude 'apiVersion == "apps/v1"'
```

The important difference is that `--include` narrows the output down to the matching resources, while `--exclude` starts from the full render and removes the matching ones.

This is especially useful when a chart has many dependency objects and you want to inspect just one kind of manifest, or temporarily hide a broad category of resources that is not relevant for the current change.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/02-04-template-filters.cast"></div>

Example files on GitHub: <https://github.com/hydra-gitops/hydra/tree/main/docs/tutorials/introduction/02-04-template-filters>

## Step 5: Filter the Source Templates with `--include` and `--exclude`

The same `--include` / `--exclude` logic is also available on [`hydra local source`](../../commands/local/source.md). There the effect is slightly different: Hydra still evaluates the filters against the rendered manifests first, but instead of printing those manifests, it prints only the source template files that produced the matching resources.

For example, to inspect only the source templates behind rendered `Deployment` objects:

```bash
hydra local source cluster.app --include 'kind == "Deployment"'
```

And to print only source templates that produced manifests outside `v1` and `apps/v1`:

```bash
hydra local source cluster.app --exclude 'apiVersion == "v1"' --exclude 'apiVersion == "apps/v1"'
```

That is useful when you already know which rendered resource kind you care about, but want to jump back to the exact Helm template source files responsible for it.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/02-05-source-filters.cast"></div>

Example files on GitHub: <https://github.com/hydra-gitops/hydra/tree/main/docs/tutorials/introduction/02-05-source-filters>

## Step 6: Show the Computed Values

Before looking at rendered manifests or template sources, it is often useful to compare the chart defaults from Helm with the values that actually reach the chart after Hydra has merged the available value layers.

With plain Helm, you can inspect only the chart's own default values like this:

```bash
helm show values "$HYDRA_CONTEXT/cluster/app"
```

That shows the defaults defined by the app chart itself, but it does not include any extra value layers from the surrounding Hydra context.

With [`hydra local values`](../../commands/local/values.md), you can print those computed Helm values directly:

```bash
hydra local values cluster.app
```

This is especially useful once a chart has its own `values.yaml`, inherits defaults from the surrounding Hydra context, and may later receive more overrides from higher levels.

Plain Helm does not offer the same merged view in this form. With Helm, you usually inspect the individual `values.yaml` files, run `helm show values` for chart defaults, or keep track of the `-f` files you pass yourself. [`hydra local values`](../../commands/local/values.md) instead shows the merged result that Hydra computes for one app inside the context hierarchy, so you can verify the effective input before looking at manifests.

You can place `values.yaml` files at group, context, cluster, and root app level. Each file applies to its own level and everything below it. For example, a `values.yaml` on cluster level applies to all root apps in that cluster. [`hydra local values`](../../commands/local/values.md) shows the merged result of those value layers.

As with [`hydra local template`](../../commands/local/template.md) and [`hydra local source`](../../commands/local/source.md), terminal output is shown with syntax highlighting.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/02-06-local-values.cast"></div>

Example files on GitHub: <https://github.com/hydra-gitops/hydra/tree/main/docs/tutorials/introduction/02-06-local-values>

## Summary

At the end of this chapter, you have:

- turned the minimal app chart into a chart that renders actual Kubernetes manifests
- added a first `Deployment` template and a chart-local `values.yaml`
- declared a regular Helm dependency in `Chart.yaml`
- used `hydra local values` to inspect the computed Helm values for the app
- used `hydra local template` to inspect rendered manifests
- used `hydra local source` to inspect the underlying Helm template files
- narrowed both outputs with `--include` and `--exclude` filters
