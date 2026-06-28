package ci

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/loader"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/provenance"
	"hydra-gitops.org/hydra/hydra-go/base/buildinfo"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/git"
)

func stubPackageSigningSecrets(t *testing.T) {
	t.Helper()

	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)

	oldSecretLoad := loadSecretsConfigHook
	loadSecretsConfigHook = func(configPath string) (*SecretsConfig, error) {
		return &SecretsConfig{Secrets: SecretsValues{
			Sign: generated.Sign,
			RegistryTokens: []RegistryTokenRef{
				{
					Registry: "registry",
					Username: "robot$hydra",
					Token:    "write-token",
					Upload:   true,
				},
			},
		}}, nil
	}
	oldPublicLoad := loadPublicSignConfigHook
	loadPublicSignConfigHook = func(configPath string) (PublicSignConfig, error) {
		return PublicSignConfig{Name: generated.Name, Key: generated.Key, PublicKey: generated.PublicKey}, nil
	}
	t.Cleanup(func() {
		loadSecretsConfigHook = oldSecretLoad
		loadPublicSignConfigHook = oldPublicLoad
	})
}

func stubPublishRegistryAuth(t *testing.T) {
	t.Helper()

	oldAuth := prepareRegistryAuthHook
	prepareRegistryAuthHook = func(configPath string) (string, func(), error) {
		return "/tmp/hydra-test-registry-config.json", func() {}, nil
	}
	t.Cleanup(func() {
		prepareRegistryAuthHook = oldAuth
	})
}

func TestRunPublish_NoBuildTag(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	cfgPath := filepath.Join(dir, ConfigFileName)
	err := RunPublish(cfgPath, ModeDryRun, nil, false, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build-")
	assert.Contains(t, err.Error(), "--force-run")
}

func TestRunPublish_NoBuildTag_ForcePublishOverrides(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	cfgPath := filepath.Join(dir, ConfigFileName)
	logs := capturePackageLogs(t, func() {
		require.NoError(t, RunPublish(cfgPath, ModeDryRun, nil, true, false, false))
	})
	assert.Contains(t, logs, "build tag missing at HEAD")
	assert.Contains(t, logs, "level=WARN")
	assert.NotContains(t, logs, "use --force-run")
}

func TestRunPublish_NoHeadTags_ForcePublishUsesLatestBuildTagInHistory(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).
		CommitFS("release", fs).
		Tag("build-202601011200").
		Tag("demo-service-ui-1.0.0-dev").
		Commit("after release", "README.md", "later\n")
	require.NoError(t, repo.Err)

	cfgPath := filepath.Join(dir, ConfigFileName)
	logs := capturePackageLogs(t, func() {
		require.NoError(t, RunPublish(cfgPath, ModeDryRun, nil, true, false, false))
	})
	assert.Contains(t, logs, "build tag missing at HEAD")
	assert.Contains(t, logs, "latest build-* tag in history")
	assert.Contains(t, logs, "service-ui")
}

func TestRunPublish_DryRun_Succeeds(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldPackage := packageChartArchiveHook
	oldPush := pushChartArchiveHook
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		return packageArtifact{}, fmt.Errorf("publish must not run in dry-run")
	}
	pushChartArchiveHook = func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
		return fmt.Errorf("push must not run in dry-run")
	}
	t.Cleanup(func() {
		packageChartArchiveHook = oldPackage
		pushChartArchiveHook = oldPush
	})

	cfgPath := filepath.Join(dir, ConfigFileName)
	require.NoError(t, RunPublish(cfgPath, ModeDryRun, nil, false, false, false))
}

func TestRunPublish_Local_PackagesWithMockHelm(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldPackage := packageChartArchiveHook
	oldPush := pushChartArchiveHook
	var packageCalls []string
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		packageCalls = append(packageCalls, chartDir)
		require.NotNil(t, signing)
		return packageArtifact{TGZPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz"), ProvPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz.prov")}, nil
	}
	pushChartArchiveHook = func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
		return fmt.Errorf("push must not run in local mode")
	}
	t.Cleanup(func() {
		packageChartArchiveHook = oldPackage
		pushChartArchiveHook = oldPush
	})

	cfgPath := filepath.Join(dir, ConfigFileName)
	require.NoError(t, RunPublish(cfgPath, ModeLocal, nil, false, false, false))
	require.Len(t, packageCalls, 1)
	assert.Equal(t, "dev", filepath.Base(packageCalls[0]))
}

func TestRunPublish_Local_SkipSigningLogsWarningAndPackagesUnsigned(t *testing.T) {
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldPackage := packageChartArchiveHook
	oldPush := pushChartArchiveHook
	var receivedSigning *packageSigningConfig
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		receivedSigning = signing
		return packageArtifact{TGZPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz")}, nil
	}
	pushChartArchiveHook = func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
		return fmt.Errorf("push must not run in local mode")
	}
	t.Cleanup(func() {
		packageChartArchiveHook = oldPackage
		pushChartArchiveHook = oldPush
	})

	cfgPath := filepath.Join(dir, ConfigFileName)
	logs := capturePackageLogs(t, func() {
		require.NoError(t, RunPublish(cfgPath, ModeLocal, nil, false, false, true))
	})
	assert.Nil(t, receivedSigning)
	assert.Contains(t, logs, "chart signing disabled")
	assert.Contains(t, logs, "level=WARN")
}

func TestRunPublish_CI_PushWithMockHelm(t *testing.T) {
	stubPackageSigningSecrets(t)
	stubPublishRegistryAuth(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldPackage := packageChartArchiveHook
	oldPush := pushChartArchiveHook
	oldExists := remoteChartExistsHook
	var packageCalls []string
	var pushCalls []string
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		packageCalls = append(packageCalls, chartDir)
		require.NotNil(t, signing)
		return packageArtifact{TGZPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz"), ProvPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz.prov")}, nil
	}
	pushChartArchiveHook = func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
		pushCalls = append(pushCalls, fmt.Sprintf("%s|%s|%s|%s|%s", artifact.TGZPath, artifact.ProvPath, registryURL, chartName, version))
		return nil
	}
	remoteChartExistsHook = func(registryURL, chartName, version string, registryConfigPath string) (bool, error) {
		return false, nil
	}
	t.Cleanup(func() {
		packageChartArchiveHook = oldPackage
		pushChartArchiveHook = oldPush
		remoteChartExistsHook = oldExists
	})

	cfgPath := filepath.Join(dir, ConfigFileName)
	require.NoError(t, RunPublish(cfgPath, ModeCI, nil, false, false, false))
	require.Len(t, packageCalls, 1)
	require.Len(t, pushCalls, 1)
	assert.Contains(t, pushCalls[0], "oci://registry/helm")
	assert.Contains(t, pushCalls[0], "service-ui")
	assert.Contains(t, pushCalls[0], "1.0.0-dev")
}

func TestRunPublish_CI_UsesPreparedRegistryAuthForRemoteChecksAndPush(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldAuth := prepareRegistryAuthHook
	oldPackage := packageChartArchiveHook
	oldPush := pushChartArchiveHook
	oldExists := remoteChartExistsHook
	var seenExistsConfig string
	var seenPushConfig string
	prepareRegistryAuthHook = func(configPath string) (string, func(), error) {
		assert.Equal(t, filepath.Join(dir, ConfigFileName), configPath)
		return "/tmp/hydra-publish-registry-config.json", func() {}, nil
	}
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		return packageArtifact{TGZPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz"), ProvPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz.prov")}, nil
	}
	remoteChartExistsHook = func(registryURL, chartName, version string, registryConfigPath string) (bool, error) {
		seenExistsConfig = registryConfigPath
		return false, nil
	}
	pushChartArchiveHook = func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
		seenPushConfig = registryConfigPath
		return nil
	}
	t.Cleanup(func() {
		prepareRegistryAuthHook = oldAuth
		packageChartArchiveHook = oldPackage
		pushChartArchiveHook = oldPush
		remoteChartExistsHook = oldExists
	})

	require.NoError(t, RunPublish(filepath.Join(dir, ConfigFileName), ModeCI, nil, false, false, false))
	assert.Equal(t, "/tmp/hydra-publish-registry-config.json", seenExistsConfig)
	assert.Equal(t, "/tmp/hydra-publish-registry-config.json", seenPushConfig)
}

func TestRunPublish_CI_FailsWithoutUploadRegistryToken(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldSecretsLoad := loadSecretsConfigHook
	loadSecretsConfigHook = func(configPath string) (*SecretsConfig, error) {
		return &SecretsConfig{Secrets: SecretsValues{
			RegistryTokens: []RegistryTokenRef{
				{
					Registry: "registry",
					Username: "robot$hydra",
					Token:    "read-token",
					Upload:   false,
				},
			},
			Sign: SignSecrets{SecretKeyring: "c2VjcmV0"},
		}}, nil
	}
	t.Cleanup(func() {
		loadSecretsConfigHook = oldSecretsLoad
	})

	err := RunPublish(filepath.Join(dir, ConfigFileName), ModeCI, nil, false, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prepare OCI registry auth")
	assert.Contains(t, err.Error(), "ci publish requires at least one writable OCI registry token")
	assert.Contains(t, err.Error(), "Expected secret entry: secrets.registryTokens[].upload: true")
	assert.Contains(t, err.Error(), "upload: true")
}

func TestRunPublish_CI_SkipsWhenRemoteChartAlreadyExists(t *testing.T) {
	stubPackageSigningSecrets(t)
	stubPublishRegistryAuth(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldPackage := packageChartArchiveHook
	oldPush := pushChartArchiveHook
	oldExists := remoteChartExistsHook
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		return packageArtifact{}, fmt.Errorf("publish must not run when remote chart already exists")
	}
	pushChartArchiveHook = func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
		return fmt.Errorf("push must not run when remote chart already exists")
	}
	remoteChartExistsHook = func(registryURL, chartName, version string, registryConfigPath string) (bool, error) {
		return true, nil
	}
	t.Cleanup(func() {
		packageChartArchiveHook = oldPackage
		pushChartArchiveHook = oldPush
		remoteChartExistsHook = oldExists
	})

	cfgPath := filepath.Join(dir, ConfigFileName)
	logs := capturePackageLogs(t, func() {
		require.NoError(t, RunPublish(cfgPath, ModeCI, nil, false, false, false))
	})
	assert.Contains(t, logs, "remote chart already exists")
	assert.Contains(t, logs, "skipping publish")
	assert.Contains(t, logs, "oci://registry/helm/service-ui:1.0.0-dev")
	assert.Contains(t, logs, "level=WARN")
}

func TestRunPublish_CI_CosignOnlySignsRemoteArtifact(t *testing.T) {
	stubPublishRegistryAuth(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
  sign:
    cosign:
      publicKey: |
        -----BEGIN PUBLIC KEY-----
        cosign
        -----END PUBLIC KEY-----
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldSecretsLoad := loadSecretsConfigHook
	generatedCosign, err := generateCosignSecrets()
	require.NoError(t, err)
	loadSecretsConfigHook = func(configPath string) (*SecretsConfig, error) {
		return &SecretsConfig{Secrets: SecretsValues{Cosign: generatedCosign.Cosign}}, nil
	}
	oldPublicCosignLoad := loadPublicCosignConfigHook
	loadPublicCosignConfigHook = func(configPath string) (PublicCosignConfig, error) {
		return PublicCosignConfig{PublicKey: generatedCosign.PublicKey}, nil
	}
	oldPackage := packageChartArchiveHook
	oldPush := pushChartArchiveHook
	oldExists := remoteChartExistsHook
	oldResolve := signOCIChartHook
	oldDigestResolve := resolveOCIChartDigestRefHook
	var signedRef string
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		assert.Nil(t, signing)
		return packageArtifact{TGZPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz")}, nil
	}
	pushChartArchiveHook = func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
		return nil
	}
	remoteChartExistsHook = func(registryURL, chartName, version string, registryConfigPath string) (bool, error) {
		return false, nil
	}
	signOCIChartHook = func(ref string, keyPath string) error {
		signedRef = ref
		assert.NotEmpty(t, keyPath)
		return nil
	}
	resolveOCIChartDigestRefHook = func(registryURL, chartName, version string, registryConfigPath string) (string, error) {
		return "registry/helm/service-ui@sha256:deadbeef", nil
	}
	t.Cleanup(func() {
		loadSecretsConfigHook = oldSecretsLoad
		loadPublicCosignConfigHook = oldPublicCosignLoad
		packageChartArchiveHook = oldPackage
		pushChartArchiveHook = oldPush
		remoteChartExistsHook = oldExists
		signOCIChartHook = oldResolve
		resolveOCIChartDigestRefHook = oldDigestResolve
	})

	require.NoError(t, RunPublish(filepath.Join(dir, ConfigFileName), ModeCI, nil, false, false, false))
	assert.Contains(t, signedRef, "registry/helm/service-ui@")
}

func TestRunPublish_CI_RemoteChartExistsCheckError(t *testing.T) {
	stubPackageSigningSecrets(t)
	stubPublishRegistryAuth(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldExists := remoteChartExistsHook
	remoteChartExistsHook = func(registryURL, chartName, version string, registryConfigPath string) (bool, error) {
		return false, fmt.Errorf("registry unavailable")
	}
	t.Cleanup(func() {
		remoteChartExistsHook = oldExists
	})

	cfgPath := filepath.Join(dir, ConfigFileName)
	err := RunPublish(cfgPath, ModeCI, nil, false, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check remote chart")
	assert.Contains(t, err.Error(), "registry unavailable")
}

func TestRunPublish_CI_ForcePublishUploadWhenRemoteChartAlreadyExists(t *testing.T) {
	stubPackageSigningSecrets(t)
	stubPublishRegistryAuth(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldPackage := packageChartArchiveHook
	oldPush := pushChartArchiveHook
	oldExists := remoteChartExistsHook
	var packageCalls []string
	var pushCalls []string
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		packageCalls = append(packageCalls, chartDir)
		require.NotNil(t, signing)
		return packageArtifact{TGZPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz"), ProvPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz.prov")}, nil
	}
	pushChartArchiveHook = func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
		pushCalls = append(pushCalls, fmt.Sprintf("%s|%s|%s|%s|%s", artifact.TGZPath, artifact.ProvPath, registryURL, chartName, version))
		return nil
	}
	remoteChartExistsHook = func(registryURL, chartName, version string, registryConfigPath string) (bool, error) {
		return true, nil
	}
	t.Cleanup(func() {
		packageChartArchiveHook = oldPackage
		pushChartArchiveHook = oldPush
		remoteChartExistsHook = oldExists
	})

	cfgPath := filepath.Join(dir, ConfigFileName)
	logs := capturePackageLogs(t, func() {
		require.NoError(t, RunPublish(cfgPath, ModeCI, nil, false, true, false))
	})
	require.Len(t, packageCalls, 1)
	require.Len(t, pushCalls, 1)
	assert.Contains(t, logs, "remote chart already exists")
	assert.Contains(t, logs, "forcing upload")
}

func TestBuildOCIResolveRef_StripsOCIScheme(t *testing.T) {
	assert.Equal(t,
		"ghcr.io/kubernetes-csi/csi-driver-nfs:1.2.3-dev",
		buildOCIResolveRef("oci://ghcr.io/kubernetes-csi", "csi-driver-nfs", "1.2.3-dev"),
	)
}

func TestPackageChartArchive_AddsHydraVersionAnnotation(t *testing.T) {
	dir := t.TempDir()
	chartDir := filepath.Join(dir, "chart")
	require.NoError(t, os.MkdirAll(chartDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(`apiVersion: v2
name: service-ui
version: 1.0.0-dev
type: application
`), 0o644))

	oldVersion := buildinfo.Version
	buildinfo.Version = "v9.8.7"
	t.Cleanup(func() {
		buildinfo.Version = oldVersion
	})

	artifact, err := packageChartArchive(chartDir, t.TempDir(), nil)
	require.NoError(t, err)

	packaged, err := loader.LoadFile(artifact.TGZPath)
	require.NoError(t, err)
	v2chart, err := convertToV2Chart(packaged)
	require.NoError(t, err)
	require.NotNil(t, v2chart.Metadata)
	assert.Equal(t, "v9.8.7", v2chart.Metadata.Annotations[hydraVersionAnnotation])
}

func TestPackageChartArchive_SignsAndWritesProvenance(t *testing.T) {
	stubPackageSigningSecrets(t)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(`ci:
  rootAppsPath: apps
  environments: [dev]
`), 0o644))
	chartDir := filepath.Join(dir, "chart")
	require.NoError(t, os.MkdirAll(chartDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(`apiVersion: v2
name: service-ui
version: 1.0.0-dev
type: application
`), 0o644))

	signing, err := preparePackageSigningConfig(filepath.Join(dir, ConfigFileName), t.TempDir())
	require.NoError(t, err)

	artifact, err := packageChartArchive(chartDir, t.TempDir(), signing)
	require.NoError(t, err)
	require.FileExists(t, artifact.TGZPath)
	require.FileExists(t, artifact.ProvPath)

	chartData, err := os.ReadFile(artifact.TGZPath)
	require.NoError(t, err)
	provData, err := os.ReadFile(artifact.ProvPath)
	require.NoError(t, err)

	signer, err := provenance.NewFromFiles(signing.KeyPath, signing.KeyringPath)
	require.NoError(t, err)
	verification, err := signer.Verify(chartData, provData, filepath.Base(artifact.TGZPath))
	require.NoError(t, err)
	assert.Equal(t, filepath.Base(artifact.TGZPath), verification.FileName)
}

func TestRunPublish_NonDryRunRequiresSigningSecrets(t *testing.T) {
	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)

	oldLoad := loadSecretsConfigHook
	loadSecretsConfigHook = func(configPath string) (*SecretsConfig, error) {
		return nil, fmt.Errorf("no secrets available")
	}
	oldPublicLoad := loadPublicSignConfigHook
	loadPublicSignConfigHook = func(configPath string) (PublicSignConfig, error) {
		return PublicSignConfig{Name: generated.Name, Key: generated.Key, PublicKey: generated.PublicKey}, nil
	}
	t.Cleanup(func() {
		loadSecretsConfigHook = oldLoad
		loadPublicSignConfigHook = oldPublicLoad
	})

	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	err = RunPublish(filepath.Join(dir, ConfigFileName), ModeLocal, nil, false, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load CI signing configuration")
	assert.Contains(t, err.Error(), "no secrets available")
}

func TestPreparePackageSigningConfig_ErrorsOnMismatchedPublicName(t *testing.T) {
	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)

	oldSecretLoad := loadSecretsConfigHook
	loadSecretsConfigHook = func(configPath string) (*SecretsConfig, error) {
		return DefaultSecretsConfig(generated.Sign, CosignSecrets{}), nil
	}
	oldPublicLoad := loadPublicSignConfigHook
	loadPublicSignConfigHook = func(configPath string) (PublicSignConfig, error) {
		return PublicSignConfig{Name: "Other Name <other@example.invalid>", Key: generated.Key, PublicKey: generated.PublicKey}, nil
	}
	t.Cleanup(func() {
		loadSecretsConfigHook = oldSecretLoad
		loadPublicSignConfigHook = oldPublicLoad
	})

	_, err = preparePackageSigningConfig(filepath.Join(t.TempDir(), ConfigFileName), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ci.sign.helm.name does not match")
}

func TestPreparePackageSigningConfig_ErrorsOnMismatchedPublicKeyFingerprint(t *testing.T) {
	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)

	oldSecretLoad := loadSecretsConfigHook
	loadSecretsConfigHook = func(configPath string) (*SecretsConfig, error) {
		return DefaultSecretsConfig(generated.Sign, CosignSecrets{}), nil
	}
	oldPublicLoad := loadPublicSignConfigHook
	loadPublicSignConfigHook = func(configPath string) (PublicSignConfig, error) {
		return PublicSignConfig{Name: generated.Name, Key: strings.Repeat("0", len(generated.Key)), PublicKey: generated.PublicKey}, nil
	}
	t.Cleanup(func() {
		loadSecretsConfigHook = oldSecretLoad
		loadPublicSignConfigHook = oldPublicLoad
	})

	_, err = preparePackageSigningConfig(filepath.Join(t.TempDir(), ConfigFileName), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ci.sign.helm.key does not match")
}

func TestPreparePackageSigningConfig_ErrorsOnInvalidPublicKeyMaterial(t *testing.T) {
	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)

	oldSecretLoad := loadSecretsConfigHook
	loadSecretsConfigHook = func(configPath string) (*SecretsConfig, error) {
		return DefaultSecretsConfig(generated.Sign, CosignSecrets{}), nil
	}
	oldPublicLoad := loadPublicSignConfigHook
	loadPublicSignConfigHook = func(configPath string) (PublicSignConfig, error) {
		return PublicSignConfig{Name: generated.Name, Key: generated.Key, PublicKey: "not-a-pgp-key"}, nil
	}
	t.Cleanup(func() {
		loadSecretsConfigHook = oldSecretLoad
		loadPublicSignConfigHook = oldPublicLoad
	})

	_, err = preparePackageSigningConfig(filepath.Join(t.TempDir(), ConfigFileName), t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode ci.sign.helm.publicKey")
}

func TestRunPublish_CI_RequiresRegistry(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-1").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	cfgPath := filepath.Join(dir, ConfigFileName)
	err := RunPublish(cfgPath, ModeCI, nil, false, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registry")
}

func TestRunPublish_UsesOnlyHeadReleaseTagsWhenBuildTagOnHead(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev")).
		Add("apps/demo/service-auth/dev", git.NewChart("service-auth").Version("1.1.0-dev"))
	repo := git.Init(dir).
		CommitFS("init", fs).
		Tag("build-202601011100").
		Tag("demo-service-auth-1.1.0-dev").
		Commit("release ui", "apps/demo/service-ui/dev/values.yaml", "x: y\n")
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldPackage := packageChartArchiveHook
	oldPush := pushChartArchiveHook
	var packaged []string
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		packaged = append(packaged, filepath.Base(chartDir))
		require.NotNil(t, signing)
		return packageArtifact{TGZPath: filepath.Join(stageDir, filepath.Base(chartDir)+"-fake.tgz"), ProvPath: filepath.Join(stageDir, filepath.Base(chartDir)+"-fake.tgz.prov")}, nil
	}
	pushChartArchiveHook = func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
		return fmt.Errorf("push must not run in local mode")
	}
	t.Cleanup(func() {
		packageChartArchiveHook = oldPackage
		pushChartArchiveHook = oldPush
	})

	cfgPath := filepath.Join(dir, ConfigFileName)
	require.NoError(t, RunPublish(cfgPath, ModeLocal, nil, false, false, false))
	assert.Equal(t, []string{"dev"}, packaged)
}

func TestRunPublish_SelectedCharts_SkipHeadTagRequirement(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("release", fs)
	require.NoError(t, repo.Err)
	repo.Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldPackage := packageChartArchiveHook
	oldPush := pushChartArchiveHook
	var packaged []string
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		packaged = append(packaged, filepath.Base(chartDir))
		require.NotNil(t, signing)
		return packageArtifact{TGZPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz"), ProvPath: filepath.Join(stageDir, "service-ui-1.0.0-dev.tgz.prov")}, nil
	}
	pushChartArchiveHook = func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
		return fmt.Errorf("push must not run in local mode")
	}
	t.Cleanup(func() {
		packageChartArchiveHook = oldPackage
		pushChartArchiveHook = oldPush
	})

	cfgPath := filepath.Join(dir, ConfigFileName)
	require.NoError(t, RunPublish(cfgPath, ModeLocal, []string{"demo/service-ui/dev"}, false, false, false))
	assert.Equal(t, []string{"dev"}, packaged)
}

func TestRunPublish_SelectedCharts_HeadMismatchRequiresForce(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("release", fs)
	require.NoError(t, repo.Err)
	repo.Tag("demo-service-ui-1.0.0-dev").Commit("after release", "README.md", "later\n")
	require.NoError(t, repo.Err)

	cfgPath := filepath.Join(dir, ConfigFileName)
	logs := capturePackageLogs(t, func() {
		err := RunPublish(cfgPath, ModeDryRun, []string{"demo/service-ui/dev"}, false, false, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match publish commit")
	})
	assert.Contains(t, logs, "publish commit mismatch")
	assert.Contains(t, logs, "use --force-run")
}

func TestRunPublish_SelectedCharts_ForcePublishOverridesHeadMismatch(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("release", fs)
	require.NoError(t, repo.Err)
	repo.Tag("demo-service-ui-1.0.0-dev").Commit("after release", "README.md", "later\n")
	require.NoError(t, repo.Err)

	cfgPath := filepath.Join(dir, ConfigFileName)
	logs := capturePackageLogs(t, func() {
		err := RunPublish(cfgPath, ModeDryRun, []string{"demo/service-ui/dev"}, true, false, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match publish commit")
	})
	assert.Contains(t, logs, "publish commit mismatch")
	assert.NotContains(t, logs, "use --force-run")
	assert.Contains(t, logs, "level=WARN")
}

func TestRunPublish_PackagesTemplateFilesMovesAndDeletes(t *testing.T) {
	stubPackageSigningSecrets(t)
	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, `ci:
  rootAppsPath: apps
  environments: [dev]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
`).
		File("apps/demo/service-ui/dev/Chart.yaml", `apiVersion: v2
name: service-ui
version: 1.0.0-dev
type: application
`).
		File("apps/demo/service-ui/dev/values.yaml", `global:
  hydra:
    templateFiles:
      move:
        - from: templates/extra.yaml
          to: templates/configmap.yaml
      delete:
        - templates/delete-me\.yaml
`).
		File("apps/demo/service-ui/dev/templates/extra.yaml", `apiVersion: v1
kind: ConfigMap
metadata:
  name: moved-template
`).
		File("apps/demo/service-ui/dev/templates/delete-me.yaml", `apiVersion: v1
kind: Secret
metadata:
  name: deleted-template
`).
		File("apps/demo/service-ui/dev/templates/configmap.yaml", `apiVersion: v1
kind: ConfigMap
metadata:
  name: original-template
`).
		File("apps/demo/service-ui/dev/templates/keep.yaml", `apiVersion: v1
kind: Service
metadata:
  name: keep-template
`)
	repo := git.Init(dir).CommitFS("init", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldPackage := packageChartArchiveHook
	var packagedChart *v2chart.Chart
	packageChartArchiveHook = func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
		packageChartArchiveHook = nil
		defer func() { packageChartArchiveHook = oldPackage }()
		packaged, err := packagePreparedChart(chartDir, stageDir, signing)
		if err != nil {
			return packageArtifact{}, err
		}
		loaded, err := loader.LoadFile(packaged.TGZPath)
		if err != nil {
			return packageArtifact{}, err
		}
		packagedChart, err = convertToV2Chart(loaded)
		if err != nil {
			return packageArtifact{}, err
		}
		return packageArtifact{TGZPath: packaged.TGZPath, ProvPath: packaged.ProvPath}, nil
	}
	t.Cleanup(func() {
		packageChartArchiveHook = oldPackage
	})

	err := RunPublish(filepath.Join(dir, ConfigFileName), ModeLocal, nil, false, false, true)
	require.NoError(t, err)
	require.NotNil(t, packagedChart)

	assert.False(t, chartFileExists(packagedChart, "templates/extra.yaml"))
	assert.False(t, chartFileExists(packagedChart, "templates/delete-me.yaml"))
	assert.Equal(t, "moved-template", chartFileConfigMapName(t, packagedChart, "templates/configmap.yaml"))
	assert.Equal(t, "keep-template", chartFileServiceName(t, packagedChart, "templates/keep.yaml"))
}

func TestRunLocalPackage_UsesSameTemplateFilesPackagingPath(t *testing.T) {
	dir := t.TempDir()
	chartDir := filepath.Join(dir, "chart")
	require.NoError(t, os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(`apiVersion: v2
name: service-ui
version: 1.0.0-dev
type: application
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(`global:
  hydra:
    templateFiles:
      move:
        - from: templates/extra.yaml
          to: templates/configmap.yaml
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "templates", "extra.yaml"), []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: moved-local
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(chartDir, "templates", "configmap.yaml"), []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: original-local
`), 0o644))

	packaged, err := RunLocalPackage(chartDir, filepath.Join(dir, "dist"))
	require.NoError(t, err)
	require.FileExists(t, packaged.TGZPath)

	loaded, err := loader.LoadFile(packaged.TGZPath)
	require.NoError(t, err)
	v2chrt, err := convertToV2Chart(loaded)
	require.NoError(t, err)
	assert.False(t, chartFileExists(v2chrt, "templates/extra.yaml"))
	assert.Equal(t, "moved-local", chartFileConfigMapName(t, v2chrt, "templates/configmap.yaml"))
}

func chartFileExists(ch *v2chart.Chart, path string) bool {
	return chartFileData(ch, path) != nil
}

func chartFileData(ch *v2chart.Chart, path string) []byte {
	var result []byte
	var walk func(*v2chart.Chart)
	walk = func(current *v2chart.Chart) {
		if current == nil || result != nil {
			return
		}
		for _, file := range current.Raw {
			if file != nil && file.Name == path {
				result = file.Data
				return
			}
		}
		for _, file := range current.Templates {
			if file != nil && file.Name == path {
				result = file.Data
				return
			}
		}
		for _, dep := range current.Dependencies() {
			walk(dep)
		}
	}
	walk(ch)
	return result
}

func chartFileConfigMapName(t *testing.T, ch *v2chart.Chart, path string) string {
	t.Helper()
	data := chartFileData(ch, path)
	require.NotNil(t, data, "missing chart file %s", path)
	assert.Contains(t, string(data), "kind: ConfigMap")
	name := extractMetadataName(string(data))
	require.NotEmpty(t, name)
	return name
}

func chartFileServiceName(t *testing.T, ch *v2chart.Chart, path string) string {
	t.Helper()
	data := chartFileData(ch, path)
	require.NotNil(t, data, "missing chart file %s", path)
	assert.Contains(t, string(data), "kind: Service")
	name := extractMetadataName(string(data))
	require.NotEmpty(t, name)
	return name
}

func extractMetadataName(yamlText string) string {
	lines := strings.Split(yamlText, "\n")
	inMetadata := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "metadata:" {
			inMetadata = true
			continue
		}
		if !inMetadata {
			continue
		}
		if strings.HasPrefix(line, "  name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "  name:"))
		}
		if trimmed != "" && !strings.HasPrefix(line, "  ") {
			inMetadata = false
		}
	}
	return ""
}

func capturePackageLogs(t *testing.T, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	formattedHandler := log.NewFormatHandler(handler, log.FormatOptions{RemoveUsedAttrs: true})

	oldSlog := slog.Default()
	oldLogger := log.Default()
	slog.SetDefault(slog.New(formattedHandler))
	log.SetDefault(log.NewLoggerWithHandler(formattedHandler))
	t.Cleanup(func() {
		log.SetDefault(oldLogger)
		slog.SetDefault(oldSlog)
	})

	fn()
	return buf.String()
}
