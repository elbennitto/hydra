# Workflow: Debugging Diffs

How to troubleshoot unexpected diff output.

## Common Causes

### 1. Controller-managed fields

Some fields are set by controllers after apply (e.g., `status`, `metadata.generation`). Use server diff mode:

```bash
hydra gitops diff '<app>' --diff-mode server
```

Server mode uses dry-run apply which strips server-managed fields.

### 2. Default values injected by webhooks

Admission webhooks may add fields not in your templates. Solutions:
- Add the field explicitly to your template
- Configure `global.hydra.diff.ignore` to exclude

### 3. Annotation/label drift

ArgoCD or other tools may add annotations. Check `global.hydra.diff.ignore` patterns.

### 4. Helm hook resources

Resources managed by Helm hooks may not match Hydra's rendered state. Use `--exclude` to filter them.

## Diagnostic Steps

```bash
# 1. Check if diff is real
hydra gitops diff '<app>'

# 2. Try server mode
hydra gitops diff '<app>' --diff-mode server

# 3. Inspect a specific resource
hydra gitops inspect <cluster> "<resource-id>"

# 4. Check diff ignore config
hydra local values '<app>' | grep -A5 "diff:"
```

## Configuration

In values, configure diff ignore rules:

```yaml
global:
  hydra:
    diff:
      ignore:
        - path: /metadata/annotations/argocd.argoproj.io~1.*
        - path: /status
```

## See Also

- [hydra gitops diff](../commands/cluster/diff.md)
- [Values: diff](../values/diff.md)
