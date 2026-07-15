package ci

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/provenance"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/git"
	oraserrdef "oras.land/oras-go/v2/errdef"
)

func TestRunValidate_DryRun_UsesHeadByDefault(t *testing.T) {
	cfgPath, _ := writeValidateConfigRepo(t, "1.0.0-dev")

	logs := captureValidateLogs(t, func() {
		require.NoError(t, RunValidate(cfgPath, ModeDryRun, nil, "", false))
	})

	assert.Contains(t, logs, "verify dry-run")
	assert.Contains(t, logs, "resolved from HEAD")
	assert.Contains(t, logs, "service-ui")
}

func TestRunValidate_UsesExplicitBuildTag(t *testing.T) {
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
    helm:
      name: Hydra CI Test <hydra-ci-test@example.invalid>
      key: ABCDEF
      publicKey: |
        -----BEGIN PGP PUBLIC KEY BLOCK-----
        fake
        -----END PGP PUBLIC KEY BLOCK-----
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).
		CommitFS("release", fs).
		Tag("build-202601011200").
		Tag("demo-service-ui-1.0.0-dev").
		Commit("after release", "README.md", "later\n")
	require.NoError(t, repo.Err)

	logs := captureValidateLogs(t, func() {
		err := RunValidate(filepath.Join(dir, ConfigFileName), ModeDryRun, nil, "build-202601011200", false)
		require.NoError(t, err)
	})

	assert.Contains(t, logs, "resolved from build-202601011200")
}

func TestRunValidate_SucceedsWhenSignatureMatches(t *testing.T) {
	cfgPath, generated := writeValidateConfigRepo(t, "1.0.0-dev")

	publicCfg, err := LoadPublicSignConfig(cfgPath)
	require.NoError(t, err)
	artifact := signedValidateArtifact(t, publicCfg, generated, filepath.Join(filepath.Dir(cfgPath), "apps", "demo", "service-ui", "dev"))

	oldPull := pullOCIChartHook
	pullOCIChartHook = func(registryURL, chartName, version string) (pulledOCIChart, error) {
		return artifact, nil
	}
	t.Cleanup(func() { pullOCIChartHook = oldPull })

	logs := captureValidateLogs(t, func() {
		require.NoError(t, RunValidate(cfgPath, ModeLocal, nil, "", false))
	})

	assert.Contains(t, logs, "verified Helm   signature")
	assert.Contains(t, logs, "1 succeeded, 0 partially ok, 0 failed")
}

func TestRunValidate_SucceedsWhenCosignSignatureMatches(t *testing.T) {
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
	repo := git.Init(dir).CommitFS("release", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldVerify := verifyOCIChartHook
	oldDigestResolve := resolveOCIChartDigestRefHook
	var verifiedRef string
	verifyOCIChartHook = func(ref string, keyPath string) error {
		verifiedRef = ref
		assert.NotEmpty(t, keyPath)
		return nil
	}
	resolveOCIChartDigestRefHook = func(registryURL, chartName, version string, registryConfigPath string) (string, error) {
		return "registry/helm/demo.service-ui@sha256:deadbeef", nil
	}
	t.Cleanup(func() {
		verifyOCIChartHook = oldVerify
		resolveOCIChartDigestRefHook = oldDigestResolve
	})

	logs := captureValidateLogs(t, func() {
		require.NoError(t, RunValidate(filepath.Join(dir, ConfigFileName), ModeLocal, nil, "", false))
	})

	assert.Contains(t, verifiedRef, "registry/helm/demo.service-ui@")
	assert.Contains(t, logs, "verified Cosign signature")
	assert.Contains(t, logs, "1 succeeded, 0 partially ok, 0 failed")
}

func TestRunValidate_FailsWhenSignatureMissing(t *testing.T) {
	cfgPath, _ := writeValidateConfigRepo(t, "1.0.0-dev")

	oldPull := pullOCIChartHook
	pullOCIChartHook = func(registryURL, chartName, version string) (pulledOCIChart, error) {
		return pulledOCIChart{ChartData: []byte("chart-data")}, nil
	}
	t.Cleanup(func() { pullOCIChartHook = oldPull })

	logs := captureValidateLogs(t, func() {
		err := RunValidate(cfgPath, ModeLocal, nil, "", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "0 chart verification(s) partially ok, 1 failed")
	})

	assert.Contains(t, logs, "helm signature missing")
	assert.Contains(t, logs, "0 succeeded, 0 partially ok, 1 failed")
}

func TestRunValidate_FailsWhenDownloadFails(t *testing.T) {
	cfgPath, _ := writeValidateConfigRepo(t, "1.0.0-dev")

	oldPull := pullOCIChartHook
	pullOCIChartHook = func(registryURL, chartName, version string) (pulledOCIChart, error) {
		return pulledOCIChart{}, oraserrdef.ErrNotFound
	}
	t.Cleanup(func() { pullOCIChartHook = oldPull })

	logs := captureValidateLogs(t, func() {
		err := RunValidate(cfgPath, ModeLocal, nil, "", false)
		require.Error(t, err)
	})

	assert.Contains(t, logs, "download failed: remote chart not found")
}

func TestRunValidate_RetriesDownloadThreeTimes(t *testing.T) {
	cfgPath, _ := writeValidateConfigRepo(t, "1.0.0-dev")

	oldPull := pullOCIChartHook
	oldSleep := validateRetrySleep
	attempts := 0
	var delays []string
	pullOCIChartHook = func(registryURL, chartName, version string) (pulledOCIChart, error) {
		attempts++
		return pulledOCIChart{}, fmt.Errorf("temporary harbor 401")
	}
	validateRetrySleep = func(d time.Duration) {
		delays = append(delays, d.String())
	}
	t.Cleanup(func() {
		pullOCIChartHook = oldPull
		validateRetrySleep = oldSleep
	})

	logs := captureValidateLogs(t, func() {
		err := RunValidate(cfgPath, ModeLocal, nil, "", false)
		require.Error(t, err)
	})

	assert.Equal(t, 3, attempts)
	assert.Equal(t, []string{"3s", "3s"}, delays)
	assert.Contains(t, logs, "chart download attempt 1/3 failed")
	assert.Contains(t, logs, "chart download attempt 2/3 failed")
	assert.Contains(t, logs, "retrying in 3s")
}

func TestRunValidate_DownloadRetryEventuallySucceeds(t *testing.T) {
	cfgPath, generated := writeValidateConfigRepo(t, "1.0.0-dev")

	publicCfg, err := LoadPublicSignConfig(cfgPath)
	require.NoError(t, err)
	artifact := signedValidateArtifact(t, publicCfg, generated, filepath.Join(filepath.Dir(cfgPath), "apps", "demo", "service-ui", "dev"))

	oldPull := pullOCIChartHook
	oldSleep := validateRetrySleep
	attempts := 0
	pullOCIChartHook = func(registryURL, chartName, version string) (pulledOCIChart, error) {
		attempts++
		if attempts < 3 {
			return pulledOCIChart{}, fmt.Errorf("temporary harbor 401")
		}
		return artifact, nil
	}
	validateRetrySleep = func(time.Duration) {}
	t.Cleanup(func() {
		pullOCIChartHook = oldPull
		validateRetrySleep = oldSleep
	})

	logs := captureValidateLogs(t, func() {
		require.NoError(t, RunValidate(cfgPath, ModeLocal, nil, "", false))
	})

	assert.Equal(t, 3, attempts)
	assert.Contains(t, logs, "chart download retry succeeded")
	assert.Contains(t, logs, "on attempt 3")
	assert.Contains(t, logs, "1 succeeded, 0 partially ok, 0 failed")
}

func TestRunValidate_FailsWhenSignatureIsInvalid(t *testing.T) {
	cfgPath, generated := writeValidateConfigRepo(t, "1.0.0-dev")

	publicCfg, err := LoadPublicSignConfig(cfgPath)
	require.NoError(t, err)
	artifact := signedValidateArtifact(t, publicCfg, generated, filepath.Join(filepath.Dir(cfgPath), "apps", "demo", "service-ui", "dev"))
	artifact.ChartData = []byte("tampered")

	oldPull := pullOCIChartHook
	pullOCIChartHook = func(registryURL, chartName, version string) (pulledOCIChart, error) {
		return artifact, nil
	}
	t.Cleanup(func() { pullOCIChartHook = oldPull })

	logs := captureValidateLogs(t, func() {
		err := RunValidate(cfgPath, ModeLocal, nil, "", false)
		require.Error(t, err)
	})

	assert.Contains(t, logs, "helm signature invalid")
}

func TestRunValidate_PartiallyOkWhenHelmFailsButCosignSucceeds(t *testing.T) {
	dir := t.TempDir()
	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)
	fs := git.NewFS().
		File(ConfigFileName, fmt.Sprintf(`ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
  sign:
    helm:
      name: %q
      key: %q
      publicKey: |
%s
    cosign:
      publicKey: |
        -----BEGIN PUBLIC KEY-----
        cosign-active
        -----END PUBLIC KEY-----
      validKeys:
        - |
          -----BEGIN PUBLIC KEY-----
          cosign-allowed
          -----END PUBLIC KEY-----
`, generated.Name, generated.Key, indentYAMLBlock(generated.PublicKey, 8))).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("release", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-1.0.0-dev")
	require.NoError(t, repo.Err)

	oldPull := pullOCIChartHook
	oldVerify := verifyOCIChartHook
	oldDigestResolve := resolveOCIChartDigestRefHook
	pullOCIChartHook = func(registryURL, chartName, version string) (pulledOCIChart, error) {
		return pulledOCIChart{ChartData: []byte("chart-data")}, nil
	}
	verifyOCIChartHook = func(ref string, keyPath string) error {
		assert.NotEmpty(t, keyPath)
		return nil
	}
	resolveOCIChartDigestRefHook = func(registryURL, chartName, version string, registryConfigPath string) (string, error) {
		return "registry/helm/demo.service-ui@sha256:deadbeef", nil
	}
	t.Cleanup(func() {
		pullOCIChartHook = oldPull
		verifyOCIChartHook = oldVerify
		resolveOCIChartDigestRefHook = oldDigestResolve
	})

	logs := captureValidateLogs(t, func() {
		err := RunValidate(filepath.Join(dir, ConfigFileName), ModeLocal, nil, "", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "1 chart verification(s) partially ok, 0 failed")
	})

	assert.Contains(t, logs, "verified Cosign signature")
	assert.Contains(t, logs, "chart verification partially ok")
	assert.Contains(t, logs, "helm signature missing")
	assert.Contains(t, logs, "0 succeeded, 1 partially ok, 0 failed")
}

func TestVerifySignatureMatchesAllowedKeys_FailsWhenKeyNotAllowlisted(t *testing.T) {
	cfgPath, generated := writeValidateConfigRepo(t, "1.0.0-dev")

	publicCfg, err := LoadPublicSignConfig(cfgPath)
	require.NoError(t, err)
	artifact := signedValidateArtifact(t, publicCfg, generated, filepath.Join(filepath.Dir(cfgPath), "apps", "demo", "service-ui", "dev"))

	verifierCfg, err := prepareValidateVerifierConfig(cfgPath, t.TempDir())
	require.NoError(t, err)
	verifier, err := provenance.NewFromKeyring(verifierCfg.KeyringPath, verifierCfg.Key)
	require.NoError(t, err)

	verification, err := verifier.Verify(artifact.ChartData, artifact.ProvData, "service-ui-1.0.0-dev.tgz")
	require.NoError(t, err)

	err = verifySignatureMatchesAllowedKeys(verification, []ValidSignKey{
		{Key: "DEADBEEF", Name: "Other User <other@example.invalid>"},
	}, PublicSignConfig{
		Key: generated.Key,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not listed in ci.sign.helm.validKeys")
}

func TestVerifySignatureMatchesAllowedKeys_AcceptsAllowlistedKey(t *testing.T) {
	cfgPath, generated := writeValidateConfigRepo(t, "1.0.0-dev")

	publicCfg, err := LoadPublicSignConfig(cfgPath)
	require.NoError(t, err)
	artifact := signedValidateArtifact(t, publicCfg, generated, filepath.Join(filepath.Dir(cfgPath), "apps", "demo", "service-ui", "dev"))

	verifierCfg, err := prepareValidateVerifierConfig(cfgPath, t.TempDir())
	require.NoError(t, err)
	verifier, err := provenance.NewFromKeyring(verifierCfg.KeyringPath, verifierCfg.Key)
	require.NoError(t, err)

	verification, err := verifier.Verify(artifact.ChartData, artifact.ProvData, "service-ui-1.0.0-dev.tgz")
	require.NoError(t, err)

	err = verifySignatureMatchesAllowedKeys(verification, []ValidSignKey{
		{Key: generated.Key, Name: generated.Name},
	}, PublicSignConfig{
		Key: generated.Key,
	})
	require.NoError(t, err)
}

func TestRunValidate_SelectedChartsMustMatchBuildTagCommit(t *testing.T) {
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
    helm:
      name: Hydra CI Test <hydra-ci-test@example.invalid>
      key: ABCDEF
      publicKey: |
        -----BEGIN PGP PUBLIC KEY BLOCK-----
        fake
        -----END PGP PUBLIC KEY BLOCK-----
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).
		CommitFS("release", fs).
		Tag("demo-service-ui-1.0.0-dev").
		Commit("next", "README.md", "later\n").
		Tag("build-202601011200")
	require.NoError(t, repo.Err)

	err := RunValidate(filepath.Join(dir, ConfigFileName), ModeDryRun, []string{"demo/service-ui/dev"}, "build-202601011200", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "points to release commit")
}

func TestRunValidate_NoBuildTag_ForceRunUsesLatestBuildTagInHistory(t *testing.T) {
	cfgPath, _ := writeValidateConfigRepoWithPostReleaseCommit(t, "1.0.0-dev")

	logs := captureValidateLogs(t, func() {
		require.NoError(t, RunValidate(cfgPath, ModeDryRun, nil, "", true))
	})

	assert.Contains(t, logs, "build tag missing at HEAD")
	assert.Contains(t, logs, "latest build-* tag in history")
}

func TestRunValidate_SelectedCharts_HeadMismatchRequiresForceRun(t *testing.T) {
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
    helm:
      name: Hydra CI Test <hydra-ci-test@example.invalid>
      key: ABCDEF
      publicKey: |
        -----BEGIN PGP PUBLIC KEY BLOCK-----
        fake
        -----END PGP PUBLIC KEY BLOCK-----
`).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version("1.0.0-dev"))
	repo := git.Init(dir).CommitFS("release", fs)
	require.NoError(t, repo.Err)
	repo.Tag("demo-service-ui-1.0.0-dev").Commit("after release", "README.md", "later\n")
	require.NoError(t, repo.Err)

	logs := captureValidateLogs(t, func() {
		err := RunValidate(filepath.Join(dir, ConfigFileName), ModeDryRun, []string{"demo/service-ui/dev"}, "", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match publish commit")
	})

	assert.Contains(t, logs, "publish commit mismatch")
	assert.Contains(t, logs, "use --force-run")
}

func TestRunValidate_SelectedCharts_ForceRunLogsWarningOnHeadMismatch(t *testing.T) {
	cfgPath, _ := writeValidateConfigRepoWithPostReleaseCommit(t, "1.0.0-dev")

	logs := captureValidateLogs(t, func() {
		err := RunValidate(cfgPath, ModeDryRun, []string{"demo/service-ui/dev"}, "", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match publish commit")
	})

	assert.Contains(t, logs, "publish commit mismatch")
	assert.Contains(t, logs, "level=WARN")
	assert.NotContains(t, logs, "use --force-run")
}

func writeValidateConfigRepo(t *testing.T, version string) (string, GeneratedSignSecrets) {
	t.Helper()
	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)

	dir := t.TempDir()
	fs := git.NewFS().
		File(ConfigFileName, fmt.Sprintf(`ci:
  rootAppsPath: apps
  environments: [dev, stage, prod]
  registry: oci://registry/helm
  appGroups:
    - name: demo
      path: apps/demo
  sign:
    helm:
      name: %q
      key: %q
      publicKey: |
%s
      validKeys:
        - key: %q
          name: %q
`, generated.Name, generated.Key, indentYAMLBlock(generated.PublicKey, 8), generated.Key, generated.Name)).
		Add("apps/demo/service-ui/dev", git.NewChart("service-ui").Version(version))
	repo := git.Init(dir).CommitFS("release", fs)
	require.NoError(t, repo.Err)
	repo.Tag("build-202601011200").Tag("demo-service-ui-" + version)
	require.NoError(t, repo.Err)
	return filepath.Join(dir, ConfigFileName), generated
}

func writeValidateConfigRepoWithPostReleaseCommit(t *testing.T, version string) (string, GeneratedSignSecrets) {
	t.Helper()
	cfgPath, generated := writeValidateConfigRepo(t, version)
	repo := git.Open(filepath.Dir(cfgPath))
	require.NoError(t, repo.Err)
	repo.Commit("after release", "README.md", "later\n")
	require.NoError(t, repo.Err)
	return cfgPath, generated
}

func signedValidateArtifact(t *testing.T, publicCfg PublicSignConfig, generated GeneratedSignSecrets, chartDir string) pulledOCIChart {
	t.Helper()
	require.Equal(t, publicCfg.Key, generated.Key)

	dir := t.TempDir()
	keyPath, err := decodeArmoredKeyToBinaryFile(generated.Sign.SecretKeyring, filepath.Join(dir, "secret-keyring.gpg"))
	require.NoError(t, err)
	keyringPath, err := decodeArmoredKeyBytesToBinaryFile([]byte(publicCfg.PublicKey), filepath.Join(dir, "public-keyring.gpg"))
	require.NoError(t, err)

	artifact, err := packageChartArchive(chartDir, t.TempDir(), &packageSigningConfig{
		KeyPath:     keyPath,
		KeyringPath: keyringPath,
		Key:         publicCfg.Key,
	})
	require.NoError(t, err)

	chartData, err := os.ReadFile(artifact.TGZPath)
	require.NoError(t, err)
	provData, err := os.ReadFile(artifact.ProvPath)
	require.NoError(t, err)

	return pulledOCIChart{
		ChartData: chartData,
		ProvData:  provData,
	}
}

func indentYAMLBlock(input string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(strings.TrimRight(input, "\n"), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func captureValidateLogs(t *testing.T, fn func()) string {
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
