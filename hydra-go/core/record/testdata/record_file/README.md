# Record File Golden Tests

This directory contains golden fixtures for the simplified `hydra record file` DSL.

Each case consists of:

- `<name>.given.yaml`: tutorial step input.
- `<name>.expected.cast`: expected rendered cast output.

Regenerate expected files with:

```bash
go test ./core/record -run TestRecordFileGolden -update
```
