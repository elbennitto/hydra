# Writing Custom Refs

How to create your own ref-parsers for custom CRDs or application-specific dependencies.

## When You Need Custom Refs

Write custom refs when:
- Your application uses CRDs that reference other resources
- You have dependencies that Hydra's builtin parsers don't detect
- You want specific resources included in backups or uninstall

## Approach 1: Values-Based Refs

For simple, static dependencies, define them in `global.hydra.refs`:

```yaml
global:
  hydra:
    refs:
      my-dependency:
        tag: [uninstall]
        predicate: 'kind == "MyCustomResource" && ns == "my-namespace"'
```

This is the simplest approach and requires no parser coding.

## Approach 2: Custom Parser Files

For dynamic dependencies that need CEL logic, create a ref-parser file.

### Step 1: Create the Parser File

Create a YAML file in the ref-parsers directory:

```yaml
ref-parsers:
  - predicate: 'gvk == "mygroup.io/v1/MyResource"'
    label: my-resource-ref
    pick:
      - cel: |
          has(entity.spec.targetRef) ?
            [refBuilder().outgoing(
              id(entity.spec.targetRef.apiVersion + "/" + entity.spec.targetRef.kind,
                 entity.spec.targetRef.namespace,
                 entity.spec.targetRef.name)
            )] : []
```

### Step 2: Test with Golden Files

Run the ref test command to generate and validate golden files:

```bash
# Generate/update golden files
hydra local test refs 'prod.**' --update

# Validate against existing golden files
hydra local test refs 'prod.**'
```

### Step 3: Verify with Inspect

Use the TUI to visually verify your refs are correct:

```bash
hydra local inspect prod
```

Navigate to your resource and verify the edges appear as expected.

### Step 4: Verify with Review

Run review to check for issues:

```bash
hydra local review 'prod.my-group.my-app'
```

## Tips

- Start with `predicate: "true"` and a simple pick rule to verify the parser runs
- Use `has(field)` for null-safe access to optional fields
- Return empty lists `[]` when there is nothing to extract
- Use `.filter()` and `.map()` for collections (e.g., multiple ownerReferences)

## Debugging

```bash
# See all refs for a specific resource
hydra local refs prod "mygroup.io/v1/MyResource/my-namespace/my-resource"

# See raw rendered output
hydra local template prod.my-group.my-app
```

## See Also

- [Ref Parsers](ref-parsers.md) — Full parser format reference
- [CEL: Functions](../cel/functions.md) — refBuilder() API
- [CEL: Variables](../cel/variables.md) — Available variables
