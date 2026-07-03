package ci

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func fullConfig() *Config {
	return &Config{
		CI: CIConfig{
			RootAppsPath: "apps",
			Environments: []string{"dev", "stage", "prod"},
			AppGroups: []AppGroup{
				{Name: "demo", Path: "apps/demo"},
				{Name: "cicd", Path: "apps/cicd"},
				{Name: "infra", Path: "apps/infra"},
			},
			Registry:    "oci://registry/helm",
			SecretsPath: "secrets",
			Sign: SignConfig{
				Helm: PublicSignConfig{
					Name:      "Hydra CI Test <hydra-ci-test@example.invalid>",
					Key:       "ABCDEF0123456789",
					PublicKey: "-----BEGIN PGP PUBLIC KEY BLOCK-----\n...\n-----END PGP PUBLIC KEY BLOCK-----\n",
					ValidKeys: []ValidSignKey{
						{Key: "ABCDEF0123456789", Name: "Hydra CI Test <hydra-ci-test@example.invalid>"},
					},
				},
			},
			Promote: Promote{
				PromotableRootApps: []string{"demo", "cicd"},
			},
			Teams: Teams{
				WebhookURL: "https://teams.example.com/webhook",
				Channels: map[string]string{
					"alerts":  "channel-alerts",
					"deploys": "channel-deploys",
				},
			},
		},
	}
}

func TestWriteConfig(t *testing.T) {
	cfg := fullConfig()
	path := filepath.Join(t.TempDir(), ".hydra-ci.yaml")

	err := WriteConfig(path, cfg)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got Config
	require.NoError(t, yaml.Unmarshal(data, &got))

	assert.Equal(t, cfg.CI.RootAppsPath, got.CI.RootAppsPath)
	assert.Equal(t, cfg.CI.Environments, got.CI.Environments)
	assert.Equal(t, cfg.CI.AppGroups, got.CI.AppGroups)
	assert.Equal(t, cfg.CI.Registry, got.CI.Registry)
	assert.Equal(t, cfg.CI.SecretsPath, got.CI.SecretsPath)
	assert.Equal(t, cfg.CI.Sign, got.CI.Sign)
	assert.Equal(t, cfg.CI.Promote.PromotableRootApps, got.CI.Promote.PromotableRootApps)
	assert.Equal(t, cfg.CI.Teams.WebhookURL, got.CI.Teams.WebhookURL)
	assert.Equal(t, cfg.CI.Teams.Channels, got.CI.Teams.Channels)
}

func TestWriteConfig_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".hydra-ci.yaml")

	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err))

	cfg := &Config{
		CI: CIConfig{
			RootAppsPath: "apps",
			Environments: []string{"dev"},
		},
	}

	err = WriteConfig(path, cfg)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got Config
	require.NoError(t, yaml.Unmarshal(data, &got))
}

func TestWriteConfig_ErrorParentNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent", "subdir", ".hydra-ci.yaml")

	cfg := &Config{
		CI: CIConfig{
			RootAppsPath: "apps",
			Environments: []string{"dev"},
		},
	}

	err := WriteConfig(path, cfg)
	require.Error(t, err)
}

func TestWriteConfig_Roundtrip(t *testing.T) {
	cfg := fullConfig()
	path := filepath.Join(t.TempDir(), ".hydra-ci.yaml")

	err := WriteConfig(path, cfg)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	parsed, err := ParseConfig(data)
	require.NoError(t, err)

	assert.Equal(t, cfg.CI.RootAppsPath, parsed.CI.RootAppsPath)
	assert.Equal(t, cfg.CI.Environments, parsed.CI.Environments)
	assert.Equal(t, cfg.CI.AppGroups, parsed.CI.AppGroups)
	assert.Equal(t, cfg.CI.Registry, parsed.CI.Registry)
	assert.Equal(t, cfg.CI.SecretsPath, parsed.CI.SecretsPath)
	assert.Equal(t, cfg.CI.Sign, parsed.CI.Sign)
	assert.Equal(t, cfg.CI.Promote, parsed.CI.Promote)
	assert.Equal(t, cfg.CI.Teams, parsed.CI.Teams)
}

func TestDetectAppGroups(t *testing.T) {
	baseDir := t.TempDir()
	for _, name := range []string{"demo", "cicd", "infra"} {
		require.NoError(t, os.MkdirAll(filepath.Join(baseDir, name), 0o755))
	}

	groups, err := DetectAppGroups(baseDir)
	require.NoError(t, err)
	require.Len(t, groups, 3)

	nameToPath := make(map[string]string)
	for _, g := range groups {
		nameToPath[g.Name] = g.Path
	}
	assert.Equal(t, filepath.Join(baseDir, "demo"), nameToPath["demo"])
	assert.Equal(t, filepath.Join(baseDir, "cicd"), nameToPath["cicd"])
	assert.Equal(t, filepath.Join(baseDir, "infra"), nameToPath["infra"])
}

func TestDetectAppGroups_EmptyDir(t *testing.T) {
	baseDir := t.TempDir()

	groups, err := DetectAppGroups(baseDir)
	require.NoError(t, err)
	assert.Empty(t, groups)
}

func TestDetectRootApps(t *testing.T) {
	baseDir := t.TempDir()

	appGroups := []AppGroup{
		{Name: "demo", Path: filepath.Join(baseDir, "apps", "demo")},
		{Name: "cicd", Path: filepath.Join(baseDir, "apps", "cicd")},
		{Name: "infra", Path: filepath.Join(baseDir, "apps", "infra")},
	}

	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "apps", "demo", "root", "dev"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "apps", "cicd", "root", "dev"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "apps", "infra", "service-x", "dev"), 0o755))

	rootApps, err := DetectRootApps(appGroups)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"demo", "cicd"}, rootApps)
}

func TestDetectEnvironments(t *testing.T) {
	baseDir := t.TempDir()

	// Structure: rootAppsPath/<group>/<app>/<env>
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "demo", "service-ui", "dev"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "demo", "service-ui", "stage"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "demo", "service-ui", "prod"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "cicd", "root", "dev"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(baseDir, "cicd", "root", "stage"), 0o755))

	envs, err := DetectEnvironments(baseDir)
	require.NoError(t, err)
	assert.Equal(t, []string{"dev", "prod", "stage"}, envs)
}

func TestDetectEnvironments_EmptyDir(t *testing.T) {
	baseDir := t.TempDir()

	envs, err := DetectEnvironments(baseDir)
	require.NoError(t, err)
	assert.Empty(t, envs)
}

func TestDetectEnvironments_NonExistent(t *testing.T) {
	envs, err := DetectEnvironments(filepath.Join(t.TempDir(), "nonexistent"))
	require.NoError(t, err)
	assert.Nil(t, envs)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	require.NotNil(t, cfg)

	assert.Equal(t, "apps", cfg.CI.RootAppsPath)
	assert.Equal(t, []string{"dev", "stage", "prod"}, cfg.CI.Environments)
	assert.Empty(t, cfg.CI.AppGroups)
	assert.Empty(t, cfg.CI.Registry)
	assert.Empty(t, cfg.CI.Promote.PromotableRootApps)
	assert.Empty(t, cfg.CI.Promote.NewChartVersion)
	assert.Empty(t, cfg.CI.Teams.WebhookURL)
}

func TestValidateOutputPath_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".hydra-ci.yaml")

	err := ValidateOutputPath(path)
	assert.NoError(t, err)
}

func TestValidateOutputPath_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".hydra-ci.yaml")
	require.NoError(t, os.WriteFile(path, []byte("existing"), 0o644))

	err := ValidateOutputPath(path)
	assert.NoError(t, err)
}

func TestValidateOutputPath_IsDirectory(t *testing.T) {
	dir := t.TempDir()

	err := ValidateOutputPath(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory")
}

func TestValidateOutputPath_ParentNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent_abc", "dir", ".hydra-ci.yaml")

	err := ValidateOutputPath(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parent")
}
