# Pipeline: Migration

This file collects the symlink migration plan, open questions, and next-step items for the pipeline architecture.

Back to [Pipeline detail index](../pipeline.md).

## Migration: Remove Symlinks

### Affected Symlinks

```text
apps/demo/root/dev/charts/infra_library           → shared/infra_library/dev
apps/cluster-infra/root/dev/charts/infra_library  → shared/infra_library/dev
apps/cicd/root/dev/charts/infra_library           → shared/infra_library/dev
apps/demo-infra/root/dev/charts/infra_library      → shared/infra_library/dev
apps/argocd/root/dev/charts/infra_library         → shared/infra_library/dev
apps/unit-test/root/dev/charts/infra_library      → shared/infra_library/dev
apps/unit-test/unit-test-app/dev/charts/*          → various symlinks
```text

### Migration Steps

1. Upload shared library charts to your OCI registry (e.g. `oci://ghcr.io/example-org/helm-charts`)
2. Update all root app `Chart.yaml`:
   `repository: file://...` → `repository: "oci://ghcr.io/example-org/helm-charts"`
3. Delete all symlinks
4. Run `helm dependency update` (pulls `.tgz` from the OCI registry)
5. Create initial `stage/` and `prod/` directories from `dev/`

---

## Open Questions

- Should there be a team mapping (which Teams channel for which chart)?
- Should existing open promote MRs be updated or recreated?
- How should breaking changes in chart dependencies be handled?
- Should there be a rollback mechanism?
- Pipeline technology: GitLab CI, GitHub Actions, or both?

---

## Next Steps

- [ ] Upload `infra_library` to Harbor and remove symlinks
- [ ] Create initial `stage/` and `prod/` directories from `dev/`
- [ ] Detailed concept for `release`: build process, root app update, Harbor upload
- [ ] Detailed concept for `promote`: MR template, version suffix rewriting
- [ ] Define team mapping for MS Teams notifications
- [ ] Decide on pipeline technology and implement
