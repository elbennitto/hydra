package helm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// Umbrella charts may have no templates/ on disk and only charts/*.tgz; sources must come from
// the loaded chart (Helm loader), not a directory-only walk.
func TestChartSourceTemplatesMultidoc_LoadDexUmbrellaWithPackagedDependency(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	dexChart := filepath.Join(repoRoot, "charts-repository", "apps", "cluster-infra", "dex", "dev")
	if _, err := os.Stat(filepath.Join(dexChart, "Chart.yaml")); err != nil {
		t.Skip("dex fixture chart not present in workspace")
	}

	l := log.Default()
	dir := NewChartDirectory(l, dexChart)
	cache := NewChartCache(l)
	charter, err := dir.LoadChart(cache, types.HelmNetworkModeOnline)
	require.NoError(t, err)

	out, err := ChartSourceTemplatesMultidoc(charter, nil)
	require.NoError(t, err)
	assert.Contains(t, out, "# Source:")
	assert.Contains(t, out, "{{")
}
