# Create a Hydra App

This chapter shows what a Hydra context is and how to build the first working Hydra app structure inside it.

## Before You Start

Hydra app IDs follow these patterns: `<cluster>.<root-app>` for root apps and `<cluster>.<root-app>.<child-app>` for child apps. We will come back to the difference between root apps and child apps later. For details, see [Concepts: App Model](../../appendix/concepts/app-model.md).

In this tutorial, we focus only on root apps, so we use the ID pattern `<cluster>.<app>`. The motivation for this ID pattern is that the same app can be installed in different clusters, for example `cluster1.app1` and `cluster2.app1`.

You can pass the context with `--hydra-context`, but for day-to-day usage the `HYDRA_CONTEXT` environment variable is usually more convenient.

## Related CLI Pages

- [`hydra local apps`](../../commands/local/apps.md) — resolve app-id patterns and verify app discovery

## Step 1: Run `hydra local apps` Without a Context

If neither `--hydra-context` nor `HYDRA_CONTEXT` is set, Hydra stops immediately and tells you that a context is required.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/01-01-no-context.cast"></div>

## Step 2: Point `HYDRA_CONTEXT` at the Future GitOps Directory

Now choose where the GitOps directory for your first cluster should live:

```bash
export HYDRA_CONTEXT=~/group/context
echo "$HYDRA_CONTEXT"
hydra local apps
```

Note: In the recorded example terminal sessions for this tutorial, `HYDRA_CONTEXT` is preconfigured automatically. That is why no explicit `export HYDRA_CONTEXT=...` command is shown in those casts.

We use `group` as the directory name in this tutorial, but you can choose any name.

Because the directory does not exist yet, Hydra stops immediately and explains that the context path is missing.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/01-02-missing-dir.cast"></div>

## Step 3: Create the Directory and Try Again

Now create the directory and rerun the same command:

```bash
mkdir -p "$HYDRA_CONTEXT"
hydra local apps
```

This time the path exists, so Hydra moves on to the next validation step. The directory is still not a valid Hydra context yet, and Hydra tells you which `values.yaml` file is missing and which `global.hydra.type` declaration to add next.

Continue with Step 4 to add that required declaration.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/01-03-empty-dir.cast"></div>

## Step 4: Mark the Parent Directory as a Hydra Group

Hydra works with groups of clusters. In most setups, `HYDRA_CONTEXT` points to one cluster directory inside such a group directory.

To let Hydra validate this structure, create a `values.yaml` one level above `HYDRA_CONTEXT`:

```bash
cat > "$HYDRA_CONTEXT/../values.yaml" <<EOF
global:
	hydra:
		type: group
EOF

hydra local apps
```

If Hydra now shows `pattern '**' matched no applications`, the context is valid, but there are no applications defined yet.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/01-04-group-values.cast"></div>

## Step 5: Create the First Cluster and Root App Directories

The recommended next step is to create one directory per managed cluster. In this tutorial we use `cluster` as the cluster name.

The cluster name is the first part of each Hydra app ID. Create a directory with that name:

```bash
mkdir "$HYDRA_CONTEXT/cluster"
```

`cluster` is just an example name. You can use other names such as `dev`, `prod`, or `in-cluster`.

Now create a root application directory below it. Here we use the root app name `app`:

```bash
mkdir "$HYDRA_CONTEXT/cluster/app"
```

`app` is also just an example. Choose a root app name that fits your setup.

Important: the root app directory must be below a cluster directory.

- Correct: `$HYDRA_CONTEXT/cluster/app`
- Not a root app path: `$HYDRA_CONTEXT/app`

Hydra app IDs always follow `<cluster>.<root-app>[.<child-app>]`, so the first path segment below `HYDRA_CONTEXT` is always interpreted as the cluster name.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/01-05-cluster-root-app.cast"></div>

## Step 6: Add a Minimal Root App Chart and Verify App Discovery

To be discovered by `hydra local apps`, a root app directory must contain a valid Helm chart. At minimum, create `Chart.yaml`:

```bash
cat > "$HYDRA_CONTEXT/cluster/app/Chart.yaml" <<EOF
apiVersion: v2
name: app
version: 0.1.0
EOF

hydra local apps
```

Expected result: Hydra now prints `cluster.app`.

<div class="hydra-asciinema" data-cast-path="tutorials/introduction/01-06-root-app-chart.cast"></div>

If `Chart.yaml` is missing, Hydra cannot load the root app chart and app discovery fails.

## Summary

At the end of this chapter, you have:

- set `HYDRA_CONTEXT` to your future GitOps directory
- created the basic Hydra group and context directory structure
- added the required group-level `values.yaml`
- created the first cluster and root app directories
- added a minimal Helm `Chart.yaml` so Hydra can discover the app as `cluster.app`

## Resulting File Tree

The example from this chapter creates the following structure:

```tree
~/group/
    values.yaml
    context/
        cluster/
            app/
                Chart.yaml
```

Explanation:

- `~/group/` is the Hydra group directory. `group` is just an example name; you can use names such as `dev` or `customer1` as well
- `~/group/values.yaml` marks that directory as a Hydra group with `global.hydra.type: group`
- `~/group/context/` is the directory referenced by `HYDRA_CONTEXT`; instead of `context`, any other name can be used here as well, and one context can contain multiple clusters.
- `~/group/context/cluster/` is the cluster directory; its name becomes the first part of the app ID. Instead of `cluster`, any other name can be used here as well.
- `~/group/context/cluster/app/` is the root app directory, and `~/group/context/cluster/app/Chart.yaml` is the minimal Helm chart file that makes the root app discoverable

## Naming Examples

This tutorial uses `group/context/cluster` as a placeholder pattern. Real setups can look like this:

Feel free to use any naming scheme that matches your needs.

| Group | Context | Cluster |
|-------|---------|---------|
| `customer1` | `dev` | `team1` |
| `customer1` | `dev` | `team2` |
| `customer1` | `prod` | `region1` |
| `customer1` | `prod` | `region2` |
| `internal` | `dev` | `env1` |
| `internal` | `dev` | `env2` |
| `internal` | `cicd` | `cluster1` |
| `internal` | `cicd` | `cluster2` |
| `dev` | `team1` | `cluster1` |
| `dev` | `team1` | `cluster2` |

## Small Exercise

Create a second app for the same cluster where you created the first app in this tutorial.

## Next step
[Inspect manifests and their templates](02-00-adding-manifests-to-the-helm-charts.md)
