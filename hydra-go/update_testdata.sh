#!/bin/bash
# Updates the golden files for all module tests
# Run this script after making changes to the ref parsers or test expectations

set -e

cd "$(dirname "$0")"

echo "=== Updating golden files ==="

echo ""
echo "=== Module: core ==="
echo "Updating references golden files..."
go test -count=1 ./core/references/... -update

echo "Updating view golden files..."
go test -count=1 ./core/view/... -update

echo "Updating cluster preset matches golden files (testdata/cluster_preset_matches/*.expected.yaml)..."
go test -count=1 -run TestClusterPresetMatchesGolden ./core/hydra -update

echo "Updating record file and tutorial cast golden files (core/record/testdata/record_file/*.expected.cast, docs/asciinema/tutorials/*.cast)..."
go test -count=1 -run 'TestRecordFileGolden|TestTutorialRecordingsGolden' ./core/record -update

echo ""
echo "=== Done ==="
echo "Please review the changes to golden / expected files (references, view, cluster_preset_matches, record_file, tutorials)."
