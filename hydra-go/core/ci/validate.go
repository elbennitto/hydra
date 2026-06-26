package ci

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	cosignverify "github.com/sigstore/cosign/v2/cmd/cosign/cli/verify"
	"helm.sh/helm/v4/pkg/provenance"
	"helm.sh/helm/v4/pkg/registry"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/git"
	oraserrdef "oras.land/oras-go/v2/errdef"
)

var pullOCIChartHook func(registryURL, chartName, version string) (pulledOCIChart, error)
var verifyOCIChartHook func(ref string, keyPath string) error
var validateRetrySleep = time.Sleep

const validatePullRetryAttempts = 3
const validatePullRetryDelay = 3 * time.Second

type pulledOCIChart struct {
	ChartData []byte
	ProvData  []byte
}

type validateVerifierConfig struct {
	KeyringPath string
	Key         string
}

type validateCosignVerifierConfig struct {
	KeyPaths []string
}

type validationOutcome struct {
	ChartPath string
	ChartName string
	Version   string
	Ref       string
	HelmOK    bool
	CosignOK  bool
	HelmErr   string
	CosignErr string
}

func (o validationOutcome) successCount(cfg verifyModes) int {
	count := 0
	if cfg.helm && o.HelmOK {
		count++
	}
	if cfg.cosign && o.CosignOK {
		count++
	}
	return count
}

func (o validationOutcome) requiredCount(cfg verifyModes) int {
	count := 0
	if cfg.helm {
		count++
	}
	if cfg.cosign {
		count++
	}
	return count
}

func (o validationOutcome) status(cfg verifyModes) string {
	successes := o.successCount(cfg)
	required := o.requiredCount(cfg)
	switch {
	case successes == required:
		return "succeeded"
	case successes > 0:
		return "partially ok"
	default:
		return "failed"
	}
}

func (o validationOutcome) reason() string {
	var reasons []string
	if o.HelmErr != "" {
		reasons = append(reasons, o.HelmErr)
	}
	if o.CosignErr != "" {
		reasons = append(reasons, o.CosignErr)
	}
	return strings.Join(reasons, "; ")
}

type verifyModes struct {
	helm   bool
	cosign bool
}

// RunValidate verifies OCI chart signatures using the configured Helm
// provenance and/or Cosign keys. It resolves charts like RunPublish and then
// validates every configured signing mechanism for each remote chart artifact.
func RunValidate(configPath string, mode Mode, selectedCharts []string, buildTag string, forceRun bool) error {
	l := log.Default()
	dir := filepath.Dir(configPath)
	cfg, err := LoadConfig(dir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	registryURL := strings.TrimSpace(strings.TrimSuffix(cfg.CI.Registry, "/"))
	if registryURL == "" {
		return fmt.Errorf("ci verify: ci.registry must be set")
	}

	absChartsPath, err := filepath.Abs(filepath.Join(dir, cfg.CI.RootAppsPath))
	if err != nil {
		return fmt.Errorf("resolve charts path: %w", err)
	}

	repo := git.Open(absChartsPath)
	if repo.Err != nil {
		return fmt.Errorf("open repo: %w", repo.Err)
	}

	relChartsPath, err := filepath.Rel(repo.Path(), absChartsPath)
	if err != nil {
		return fmt.Errorf("relativize charts path: %w", err)
	}
	cfg.CI.RootAppsPath = filepath.ToSlash(relChartsPath)

	groups, err := resolvePackageGroupNames(cfg, repo)
	if err != nil {
		return fmt.Errorf("verify groups: %w", err)
	}

	chartRelPaths, sourceRef, err := resolveValidateChartPaths(repo, cfg, groups, selectedCharts, buildTag, forceRun)
	if err != nil {
		return err
	}

	verifyCfg, err := resolveVerifyModes(configPath)
	if err != nil {
		return err
	}
	if !verifyCfg.helm && !verifyCfg.cosign {
		return fmt.Errorf("ci verify: neither ci.sign nor ci.cosign is configured")
	}

	if mode == ModeDryRun {
		for _, rel := range chartRelPaths {
			ch, err := repo.LoadChart(rel)
			if err != nil {
				return fmt.Errorf("load chart %s: %w", rel, err)
			}
			ref := buildOCIChartRef(registryURL, ch.GetName(), ch.GetVersion())
			l.Info(logIdCI, "verify dry-run: would verify configured signatures for {chart} version {version} at {ref} resolved from {source}",
				log.String("chart", ch.GetName()),
				log.String("version", ch.GetVersion()),
				log.String("ref", ref),
				log.String("source", sourceRef))
		}
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "hydra-verify-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	var helmVerifierCfg *validateVerifierConfig
	var helmVerifier *provenance.Signatory
	if verifyCfg.helm {
		helmVerifierCfg, err = prepareValidateVerifierConfig(configPath, tmpDir)
		if err != nil {
			return err
		}
		helmVerifier, err = provenance.NewFromKeyring(helmVerifierCfg.KeyringPath, helmVerifierCfg.Key)
		if err != nil {
			return fmt.Errorf("load verification keyring: %w", err)
		}
	}

	var cosignVerifierCfg *validateCosignVerifierConfig
	if verifyCfg.cosign {
		cosignVerifierCfg, err = prepareValidateCosignVerifierConfig(configPath, tmpDir)
		if err != nil {
			return err
		}
	}

	var succeeded []validationOutcome
	var partial []validationOutcome
	var failed []validationOutcome

	for _, rel := range chartRelPaths {
		ch, err := repo.LoadChart(rel)
		if err != nil {
			failed = append(failed, validationOutcome{
				ChartPath: rel,
				HelmErr:   fmt.Sprintf("load chart metadata failed: %v", err),
			})
			continue
		}

		name := ch.GetName()
		version := ch.GetVersion()
		ref := buildOCIChartRef(registryURL, name, version)
		outcome := validationOutcome{
			ChartPath: rel,
			ChartName: name,
			Version:   version,
			Ref:       ref,
		}

		var artifact pulledOCIChart
		if verifyCfg.helm {
			artifact, err = pullOCIChartWithRetry(l, registryURL, name, version)
			if err != nil {
				outcome.HelmErr = classifyValidatePullError(err)
			} else if len(artifact.ProvData) == 0 {
				outcome.HelmErr = "helm signature missing: provenance file not found in OCI artifact"
			} else {
				filename := fmt.Sprintf("%s-%s.tgz", name, version)
				verification, err := helmVerifier.Verify(artifact.ChartData, artifact.ProvData, filename)
				if err != nil {
					outcome.HelmErr = fmt.Sprintf("helm signature invalid: %v", err)
				} else if err := verifySignatureMatchesAllowedKeys(verification, cfg.CI.Sign.Helm.ValidKeys, cfg.CI.Sign.Helm); err != nil {
					outcome.HelmErr = err.Error()
				} else {
					outcome.HelmOK = true
					l.Info(logIdCI, "verified Helm   signature for {chart} version {version} at {ref}",
						log.String("chart", name),
						log.String("version", version),
						log.String("ref", ref))
				}
			}
		}

		if verifyCfg.cosign {
			digestRef, err := resolveOCIChartDigestRef(registryURL, name, version, "")
			if err != nil {
				outcome.CosignErr = fmt.Sprintf("cosign resolve failed: %v", err)
			} else if err := verifyOCIChart(digestRef, cosignVerifierCfg); err != nil {
				outcome.CosignErr = fmt.Sprintf("cosign signature invalid: %v", err)
			} else {
				outcome.CosignOK = true
				l.Info(logIdCI, "verified Cosign signature for {chart} version {version} at {ref}",
					log.String("chart", name),
					log.String("version", version),
					log.String("ref", ref))
			}
		}

		switch outcome.status(*verifyCfg) {
		case "succeeded":
			succeeded = append(succeeded, outcome)
		case "partially ok":
			partial = append(partial, outcome)
		default:
			failed = append(failed, outcome)
		}
	}

	sort.Slice(succeeded, func(i, j int) bool { return succeeded[i].ChartPath < succeeded[j].ChartPath })
	sort.Slice(partial, func(i, j int) bool { return partial[i].ChartPath < partial[j].ChartPath })
	sort.Slice(failed, func(i, j int) bool { return failed[i].ChartPath < failed[j].ChartPath })
	for _, item := range partial {
		l.Warn(logIdCI, "chart verification partially ok for {path} at {ref}: {reason}",
			log.String("path", item.ChartPath),
			log.String("ref", item.Ref),
			log.String("reason", item.reason()))
	}
	for _, bad := range failed {
		l.Error(logIdCI, "chart verification failed for {path} at {ref}: {reason}",
			log.String("path", bad.ChartPath),
			log.String("ref", bad.Ref),
			log.String("reason", bad.reason()))
	}

	l.Info(logIdCI, "verification summary from {source}: {ok} succeeded, {partial} partially ok, {failed} failed",
		log.String("source", sourceRef),
		log.Int("ok", len(succeeded)),
		log.Int("partial", len(partial)),
		log.Int("failed", len(failed)))

	if len(partial) > 0 || len(failed) > 0 {
		return fmt.Errorf("ci verify: %d chart verification(s) partially ok, %d failed", len(partial), len(failed))
	}
	return nil
}

func pullOCIChartWithRetry(l log.Logger, registryURL, chartName, version string) (pulledOCIChart, error) {
	var lastErr error
	for attempt := 1; attempt <= validatePullRetryAttempts; attempt++ {
		artifact, err := pullOCIChart(registryURL, chartName, version)
		if err == nil {
			if attempt > 1 {
				l.Info(logIdCI, "chart download retry succeeded for {chart} version {version} on attempt {attempt}",
					log.String("chart", chartName),
					log.String("version", version),
					log.Int("attempt", attempt))
			}
			return artifact, nil
		}

		lastErr = err
		if attempt == validatePullRetryAttempts {
			break
		}

		l.Warn(logIdCI, "chart download attempt {attempt}/{maxAttempts} failed for {chart} version {version}; retrying in {delay}: {error}",
			log.Int("attempt", attempt),
			log.Int("maxAttempts", validatePullRetryAttempts),
			log.String("chart", chartName),
			log.String("version", version),
			log.String("delay", validatePullRetryDelay.String()),
			log.String("error", err.Error()))
		validateRetrySleep(validatePullRetryDelay)
	}

	return pulledOCIChart{}, lastErr
}

func prepareValidateVerifierConfig(configPath, workDir string) (*validateVerifierConfig, error) {
	publicCfg, err := LoadPublicSignConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load CI signing configuration: %w", err)
	}

	signDir := filepath.Join(workDir, "signing")
	if err := os.MkdirAll(signDir, 0o700); err != nil {
		return nil, fmt.Errorf("create signing workspace: %w", err)
	}

	keyringPath, err := decodeArmoredKeyBytesToBinaryFile([]byte(publicCfg.PublicKey), filepath.Join(signDir, "public-keyring.gpg"))
	if err != nil {
		return nil, fmt.Errorf("prepare signing public keyring: %w", err)
	}

	return &validateVerifierConfig{
		KeyringPath: keyringPath,
		Key:         strings.TrimSpace(publicCfg.Key),
	}, nil
}

func prepareValidateCosignVerifierConfig(configPath, workDir string) (*validateCosignVerifierConfig, error) {
	publicCfg, err := LoadPublicCosignConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load CI cosign verification configuration: %w", err)
	}
	if info, err := os.Stat(configPath); err == nil && info.IsDir() {
		configPath = filepath.Join(configPath, ConfigFileName)
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := LoadConfig(filepath.Dir(absConfigPath))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	signDir := filepath.Join(workDir, "cosign")
	if err := os.MkdirAll(signDir, 0o700); err != nil {
		return nil, fmt.Errorf("create cosign verification workspace: %w", err)
	}

	keys := cfg.CI.Sign.Cosign.ValidKeys
	if len(keys) == 0 {
		keys = []string{publicCfg.PublicKey}
	}

	keyPaths := make([]string, 0, len(keys))
	for _, publicKey := range keys {
		keyFile, err := os.CreateTemp(signDir, "cosign-*.pub")
		if err != nil {
			return nil, fmt.Errorf("create cosign public key file: %w", err)
		}
		if _, err := keyFile.WriteString(publicKey); err != nil {
			_ = keyFile.Close()
			return nil, fmt.Errorf("write cosign public key: %w", err)
		}
		if err := keyFile.Close(); err != nil {
			return nil, fmt.Errorf("close cosign public key file: %w", err)
		}
		keyPaths = append(keyPaths, keyFile.Name())
	}

	return &validateCosignVerifierConfig{KeyPaths: keyPaths}, nil
}

func pullOCIChart(registryURL, chartName, version string) (pulledOCIChart, error) {
	if pullOCIChartHook != nil {
		return pullOCIChartHook(registryURL, chartName, version)
	}

	client, err := registry.NewClient(
		registry.ClientOptDebug(false),
		registry.ClientOptEnableCache(true),
		registry.ClientOptWriter(io.Discard),
	)
	if err != nil {
		return pulledOCIChart{}, fmt.Errorf("create registry client: %w", err)
	}

	ref := buildOCIChartRef(registryURL, chartName, version)
	result, err := client.Pull(ref, registry.PullOptWithProv(true))
	if err != nil {
		return pulledOCIChart{}, err
	}

	var provData []byte
	if result.Prov != nil {
		provData = result.Prov.Data
	}
	return pulledOCIChart{
		ChartData: result.Chart.Data,
		ProvData:  provData,
	}, nil
}

func classifyValidatePullError(err error) string {
	if errors.Is(err, oraserrdef.ErrNotFound) {
		return "download failed: remote chart not found"
	}
	return fmt.Sprintf("download failed: %v", err)
}

func verifyOCIChart(ref string, cfg *validateCosignVerifierConfig) error {
	if verifyOCIChartHook != nil {
		var lastErr error
		for _, keyPath := range cfg.KeyPaths {
			if err := verifyOCIChartHook(ref, keyPath); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
		return lastErr
	}
	registryOpts, err := resolveCosignRegistryOptions(ref)
	if err != nil {
		return err
	}
	var lastErr error
	for _, keyPath := range cfg.KeyPaths {
		cmd := &cosignverify.VerifyCommand{
			RegistryOptions: registryOpts,
			KeyRef:          keyPath,
			CheckClaims:     true,
			IgnoreTlog:      true,
			Offline:         true,
		}
		err := withCosignHydraLogger(func() error {
			return cmd.Exec(context.Background(), []string{ref})
		})
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func verifySignatureMatchesAllowedKeys(verification *provenance.Verification, allowedKeys []ValidSignKey, signCfg PublicSignConfig) error {
	if verification == nil || verification.SignedBy == nil || verification.SignedBy.PrimaryKey == nil {
		return fmt.Errorf("helm signature invalid: signer identity missing from provenance verification")
	}
	actualKey := strings.ToUpper(hex.EncodeToString(verification.SignedBy.PrimaryKey.Fingerprint[:]))
	allowed := normalizeValidKeys(allowedKeys)
	if len(allowed) == 0 {
		expectedKey := strings.ToUpper(strings.TrimSpace(signCfg.Key))
		if actualKey != expectedKey {
			return fmt.Errorf("helm signature does not match local signing key: remote=%s local=%s", actualKey, expectedKey)
		}
		return nil
	}
	if _, ok := allowed[actualKey]; !ok {
		return fmt.Errorf("helm signature key %s is not listed in ci.sign.helm.validKeys", actualKey)
	}
	return nil
}

func normalizeValidKeys(allowedKeys []ValidSignKey) map[string]ValidSignKey {
	out := make(map[string]ValidSignKey, len(allowedKeys))
	for _, entry := range allowedKeys {
		key := strings.ToUpper(strings.TrimSpace(entry.Key))
		if key == "" {
			continue
		}
		out[key] = ValidSignKey{
			Key:  key,
			Name: strings.TrimSpace(entry.Name),
		}
	}
	return out
}

func resolveVerifyModes(configPath string) (*verifyModes, error) {
	if info, err := os.Stat(configPath); err == nil && info.IsDir() {
		configPath = filepath.Join(configPath, ConfigFileName)
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := LoadConfig(filepath.Dir(absConfigPath))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return &verifyModes{
		helm:   strings.TrimSpace(cfg.CI.Sign.Helm.PublicKey) != "",
		cosign: strings.TrimSpace(cfg.CI.Sign.Cosign.PublicKey) != "",
	}, nil
}

func resolveValidateChartPaths(repo *git.Repo, cfg *Config, groups []string, selectedCharts []string, buildTag string, forceRun bool) ([]string, string, error) {
	trimmedBuildTag := strings.TrimSpace(buildTag)
	if trimmedBuildTag == "" {
		paths, err := resolvePackageChartPaths(repo, cfg, groups, selectedCharts, forceRun)
		if err != nil {
			return nil, "", err
		}
		if len(selectedCharts) > 0 {
			return paths, "HEAD-selected-charts", nil
		}
		return paths, "HEAD", nil
	}
	if !strings.HasPrefix(trimmedBuildTag, BuildTagPrefix) {
		return nil, "", fmt.Errorf("ci verify: build tag %q must start with %s", trimmedBuildTag, BuildTagPrefix)
	}

	buildCommit, err := repo.CommitHash(trimmedBuildTag)
	if err != nil {
		return nil, "", fmt.Errorf("ci verify: resolve build tag %q: %w", trimmedBuildTag, err)
	}

	if len(selectedCharts) > 0 {
		paths, err := resolveExplicitPackageChartPathsForCommit(repo, cfg, selectedCharts, buildCommit, trimmedBuildTag)
		if err != nil {
			return nil, "", err
		}
		return paths, trimmedBuildTag, nil
	}

	tags, err := repo.TagsPointingTo(trimmedBuildTag)
	if err != nil {
		return nil, "", fmt.Errorf("ci verify: list tags at build tag %q: %w", trimmedBuildTag, err)
	}
	relSet, err := chartPathsFromTags(repo, cfg, groups, tags)
	if err != nil {
		return nil, "", err
	}
	if len(relSet) == 0 {
		return nil, "", fmt.Errorf("ci verify: no app or root tags mapped to charts at %s (tags: %v)", trimmedBuildTag, tags)
	}
	out := make([]string, 0, len(relSet))
	for p := range relSet {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, trimmedBuildTag, nil
}

func resolveExplicitPackageChartPathsForCommit(repo *git.Repo, cfg *Config, selectedCharts []string, expectedCommit string, expectedRef string) ([]string, error) {
	seen := make(map[string]struct{}, len(selectedCharts))
	out := make([]string, 0, len(selectedCharts))

	for _, raw := range selectedCharts {
		rel, err := normalizeSelectedChartPath(cfg.CI.RootAppsPath, raw)
		if err != nil {
			return nil, err
		}
		abs := filepath.Join(repo.Path(), filepath.FromSlash(rel))
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("ci verify: selected chart %q: %w", rel, err)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("ci verify: selected chart path %q is not a directory", rel)
		}

		tag, err := releaseTagForChart(repo, rel)
		if err != nil {
			return nil, err
		}
		tagCommit, err := repo.CommitHash(tag)
		if err != nil {
			return nil, fmt.Errorf("ci verify: resolve publish tag %q for %q: %w", tag, rel, err)
		}
		if tagCommit != expectedCommit {
			return nil, fmt.Errorf("ci verify: chart %s points to release commit %s, but %s points to %s", rel, tagCommit, expectedRef, expectedCommit)
		}

		if _, exists := seen[rel]; exists {
			continue
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}

	sort.Strings(out)
	return out, nil
}
