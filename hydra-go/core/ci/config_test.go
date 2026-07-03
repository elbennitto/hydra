package ci

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed all:testdata
var testdataFS embed.FS

func readTestdata(t *testing.T, path string) []byte {
	t.Helper()
	data, err := testdataFS.ReadFile(path)
	require.NoError(t, err)
	return data
}

func TestParseConfig_Valid(t *testing.T) {
	data := readTestdata(t, "testdata/config/valid.given.yaml")
	cfg, err := ParseConfig(data)

	require.NoError(t, err)
	assert.Equal(t, "apps", cfg.CI.RootAppsPath)
	assert.Equal(t, []string{"dev", "stage", "prod"}, cfg.CI.Environments)
	assert.Equal(t, 2, len(cfg.CI.AppGroups))
	assert.Equal(t, "demo", cfg.CI.AppGroups[0].Name)
	assert.Equal(t, "apps/demo", cfg.CI.AppGroups[0].Path)
	assert.Equal(t, "oci://ghcr.io/example-org/helm-charts", cfg.CI.Registry)
	assert.Equal(t, "origin/HEAD", cfg.CI.UpstreamBranch)
	assert.Equal(t, "Hydra CI <ci@example.com>", cfg.CI.Sign.Helm.Name)
	assert.Equal(t, "0123456789ABCDEF0123456789ABCDEF01234567", cfg.CI.Sign.Helm.Key)
	assert.Contains(t, cfg.CI.Sign.Helm.PublicKey, "BEGIN PGP PUBLIC KEY BLOCK")
	assert.Empty(t, cfg.CI.Sign.Helm.ValidKeys)
	assert.Empty(t, cfg.CI.Promote.PromotableRootApps)
}

func TestParseConfig_MissingRootAppsPath(t *testing.T) {
	data := readTestdata(t, "testdata/config/missing_chart_paths.given.yaml")
	_, err := ParseConfig(data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rootAppsPath must not be empty")
}

func TestParseConfig_MissingEnvironments(t *testing.T) {
	data := readTestdata(t, "testdata/config/missing_environments.given.yaml")
	_, err := ParseConfig(data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "environments must not be empty")
}

func TestParseConfig_CustomRootAppsPath(t *testing.T) {
	data := readTestdata(t, "testdata/config/custom_paths.given.yaml")
	cfg, err := ParseConfig(data)

	require.NoError(t, err)
	assert.Equal(t, "custom/apps", cfg.CI.RootAppsPath)
}

func TestParseConfig_PromotableRootApps(t *testing.T) {
	data := readTestdata(t, "testdata/config/promotable_root_apps.given.yaml")
	cfg, err := ParseConfig(data)

	require.NoError(t, err)
	assert.Equal(t, []string{"demo", "cluster-infra"}, cfg.CI.Promote.PromotableRootApps)
}

func TestParseConfig_PromoteNewChartVersion(t *testing.T) {
	cfg, err := ParseConfig([]byte(`
ci:
  rootAppsPath: apps
  environments: [dev, stage]
  promote:
    promotableRootApps: []
    newChartVersion: 0.0.0
`))

	require.NoError(t, err)
	assert.Equal(t, "0.0.0", cfg.CI.Promote.NewChartVersion)
}

func TestParseConfig_PromoteNewChartVersionInvalid(t *testing.T) {
	_, err := ParseConfig([]byte(`
ci:
  rootAppsPath: apps
  environments: [dev, stage]
  promote:
    promotableRootApps: []
    newChartVersion: invalid-version
`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ci.promote.newChartVersion")
}

func TestParseConfig_UpstreamBranchOverride(t *testing.T) {
	cfg, err := ParseConfig([]byte(`
ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  upstreamBranch: origin/stable
`))

	require.NoError(t, err)
	assert.Equal(t, "origin/stable", cfg.CI.UpstreamBranch)
}

func TestIsRootAppPromotable_EmptyList(t *testing.T) {
	cfg := &Config{CI: CIConfig{
		Promote: Promote{PromotableRootApps: []string{}},
	}}

	assert.False(t, cfg.IsRootAppPromotable("demo"))
	assert.False(t, cfg.IsRootAppPromotable("cluster-infra"))
}

func TestIsRootAppPromotable_WithEntries(t *testing.T) {
	cfg := &Config{CI: CIConfig{
		Promote: Promote{PromotableRootApps: []string{"demo", "cluster-infra"}},
	}}

	assert.True(t, cfg.IsRootAppPromotable("demo"))
	assert.True(t, cfg.IsRootAppPromotable("cluster-infra"))
	assert.False(t, cfg.IsRootAppPromotable("cicd"))
}

func TestDefaultAutoPipeline(t *testing.T) {
	d := DefaultAutoPipeline()
	assert.Equal(t, []string{
		"download", "test", "release", "publish", "promote", "sync", "update", "sprint", "upgrade",
	}, d)
	// Returned slice must be a copy (mutating d does not change the next call).
	d[0] = "mutated"
	d2 := DefaultAutoPipeline()
	assert.Equal(t, "download", d2[0])
}

func TestResolveAutoSteps_DefaultWhenOmitted(t *testing.T) {
	data := readTestdata(t, "testdata/config/valid.given.yaml")
	cfg, err := ParseConfig(data)
	require.NoError(t, err)

	steps, err := ResolveAutoSteps(cfg)
	require.NoError(t, err)
	assert.Equal(t, DefaultAutoPipeline(), steps)
}

func TestParseConfig_AutoStepsValid(t *testing.T) {
	data := readTestdata(t, "testdata/config/auto_steps_valid.given.yaml")
	cfg, err := ParseConfig(data)
	require.NoError(t, err)

	steps, err := ResolveAutoSteps(cfg)
	require.NoError(t, err)
	assert.Equal(t, []string{"release", "promote"}, steps)
}

func TestParseConfig_AutoStepsEmpty(t *testing.T) {
	data := readTestdata(t, "testdata/config/auto_steps_empty.given.yaml")
	_, err := ParseConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "autoSteps must contain at least one step")
}

func TestParseConfig_AutoStepsUnknown(t *testing.T) {
	data := readTestdata(t, "testdata/config/auto_steps_unknown.given.yaml")
	_, err := ParseConfig(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown ci.autoSteps entry")
}

func TestPromotionPath(t *testing.T) {
	cfg := &Config{CI: CIConfig{
		Environments: []string{"dev", "stage", "prod"},
	}}

	src, tgt, err := cfg.PromotionPath("dev")
	require.NoError(t, err)
	assert.Equal(t, "dev", src)
	assert.Equal(t, "stage", tgt)

	src, tgt, err = cfg.PromotionPath("stage")
	require.NoError(t, err)
	assert.Equal(t, "stage", src)
	assert.Equal(t, "prod", tgt)

	_, _, err = cfg.PromotionPath("prod")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no promotion target")
}
