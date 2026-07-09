package ci

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	cosignopts "github.com/sigstore/cosign/v2/cmd/cosign/cli/options"
	cosignsign "github.com/sigstore/cosign/v2/cmd/cosign/cli/sign"
	helmchart "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	v2chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"helm.sh/helm/v4/pkg/provenance"
	"helm.sh/helm/v4/pkg/registry"
	"hydra-gitops.org/hydra/hydra-go/base/buildinfo"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/git"
	"hydra-gitops.org/hydra/hydra-go/core/helm"
	coretypes "hydra-gitops.org/hydra/hydra-go/core/types"
	"oras.land/oras-go/v2"
	oraserrdef "oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"
	orasauth "oras.land/oras-go/v2/registry/remote/auth"
	orascredentials "oras.land/oras-go/v2/registry/remote/credentials"
	"sigs.k8s.io/yaml"
)

const hydraVersionAnnotation = "io.hydracd.hydra.version"

var helmRunHook func(ctx context.Context, dir string, args ...string) ([]byte, error)
var packageChartArchiveHook func(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error)
var pushChartArchiveHook func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error
var uploadChartWithoutTagHook func(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) (string, error)
var tagOCIChartHook func(registryURL, chartName, version, digestRef, registryConfigPath string) error
var remoteChartExistsHook func(registryURL, chartName, version string, registryConfigPath string) (bool, error)
var signOCIChartHook func(ref string, keyPath string) error
var resolveOCIChartDigestRefHook func(registryURL, chartName, version string, registryConfigPath string) (string, error)

type packageArtifact struct {
	TGZPath  string
	ProvPath string
}

type PackagedChart struct {
	Name     string
	Version  string
	TGZPath  string
	ProvPath string
}

type packageSigningConfig struct {
	KeyPath     string
	KeyringPath string
	Key         string
}

func helmRun(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if helmRunHook != nil {
		return helmRunHook(ctx, dir, args...)
	}
	cmd := exec.CommandContext(ctx, "helm", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.CombinedOutput()
}

// RunPublish packages charts listed by release tags on HEAD and pushes them to
// ci.registry when mode is CI. Requires a build-* tag on HEAD plus documentation
// tags that map to existing chart directories unless forceRun is set.
func RunPublish(configPath string, mode Mode, selectedCharts []string, forceRun bool, forcePublishUpload bool, skipSigning bool) error {
	l := log.Default()
	dir := filepath.Dir(configPath)
	cfg, err := LoadConfig(dir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
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
		return fmt.Errorf("publish groups: %w", err)
	}

	chartRelPaths, err := resolvePackageChartPaths(repo, cfg, groups, selectedCharts, forceRun)
	if err != nil {
		return err
	}

	registry := strings.TrimSpace(strings.TrimSuffix(cfg.CI.Registry, "/"))
	if mode == ModeCI && registry == "" {
		return fmt.Errorf("ci publish: ci.registry must be set for OCI push (CI mode)")
	}

	var tmpDir string
	if mode != ModeDryRun {
		tmpDir, err = os.MkdirTemp("", "hydra-package-*")
		if err != nil {
			return fmt.Errorf("temp dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)
	}

	registryConfigPath := ""
	registryAuthCleanup := func() {}
	if mode == ModeCI {
		registryConfigPath, registryAuthCleanup, err = prepareUploadRegistryAuth(configPath)
		if err != nil {
			return fmt.Errorf("prepare OCI registry auth: %w", err)
		}
	}
	defer registryAuthCleanup()

	var signing *packageSigningConfig
	var cosignSigning *cosignSigningConfig
	if skipSigning {
		l.Warn(logIdCI, "chart signing disabled; publish will continue without provenance signatures")
	} else if mode != ModeDryRun {
		signing, cosignSigning, err = preparePublishSigning(configPath, tmpDir)
		if err != nil {
			return err
		}
		if signing == nil && cosignSigning == nil {
			return fmt.Errorf("load CI signing configuration: neither ci.sign nor ci.cosign is configured")
		}
	}

	return withHelmRegistryConfig(registryConfigPath, func() error {
		for _, rel := range chartRelPaths {
			absDir := filepath.Join(repo.Path(), filepath.FromSlash(rel))
			ch, err := repo.LoadChart(rel)
			if err != nil {
				return fmt.Errorf("load chart %s: %w", rel, err)
			}
			name := ch.GetName()
			ver := ch.GetVersion()
			if mode == ModeDryRun {
				l.Info(logIdCI, "publish dry-run: would run helm dependency update and helm package for {chart} at {path} version {version}",
					log.String("chart", name), log.String("path", rel), log.String("version", ver))
				if registry != "" {
					if skipSigning {
						l.Warn(logIdCI, "publish dry-run: would push unsigned chart {artifact} to {registry}",
							log.String("artifact", name+"-"+ver+".tgz"), log.String("registry", registry))
					} else {
						switch {
						case signing != nil && cosignSigning != nil:
							l.Info(logIdCI, "publish dry-run: would push {artifact} with Helm provenance and Cosign signature to {registry}",
								log.String("artifact", name+"-"+ver+".tgz"), log.String("registry", registry))
						case signing != nil:
							l.Info(logIdCI, "publish dry-run: would sign {artifact} with Helm provenance and push it to {registry}",
								log.String("artifact", name+"-"+ver+".tgz"), log.String("registry", registry))
						case cosignSigning != nil:
							l.Info(logIdCI, "publish dry-run: would push {artifact} and attach a Cosign signature in {registry}",
								log.String("artifact", name+"-"+ver+".tgz"), log.String("registry", registry))
						}
					}
				} else {
					if skipSigning {
						l.Warn(logIdCI, "publish dry-run: would package chart locally without signing (no ci.registry configured)")
					} else {
						l.Info(logIdCI, "publish dry-run: would package the chart locally using the configured signing settings (no ci.registry push)")
					}
				}
				continue
			}
			if mode == ModeCI {
				remoteRef := buildOCIChartRef(registry, name, ver)
				exists, err := remoteChartExists(registry, name, ver, registryConfigPath)
				if err != nil {
					return fmt.Errorf("chart %s: check remote chart: %w", rel, err)
				}
				if exists {
					if !forcePublishUpload {
						l.Warn(logIdCI, "remote chart already exists; skipping publish for {chart} version {version} at {ref}",
							log.String("chart", name),
							log.String("version", ver),
							log.String("ref", remoteRef))
						continue
					}
					l.Warn(logIdCI, "remote chart already exists; forcing upload for {chart} version {version} at {ref}",
						log.String("chart", name),
						log.String("version", ver),
						log.String("ref", remoteRef))
				}
			}

			if err := downloadChartDependencies(l, absDir, nil); err != nil {
				return fmt.Errorf("chart %s: dependency update: %w", rel, err)
			}

			stageName := strings.ReplaceAll(rel, "/", "_")
			stageDir := filepath.Join(tmpDir, stageName)
			if err := os.MkdirAll(stageDir, 0o755); err != nil {
				return fmt.Errorf("mkdir stage: %w", err)
			}

			packaged, err := packagePreparedChart(absDir, stageDir, signing)
			if err != nil {
				return fmt.Errorf("chart %s: helm package: %w", rel, err)
			}

			if mode == ModeCI {
				artifact := packageArtifact{TGZPath: packaged.TGZPath, ProvPath: packaged.ProvPath}
				if cosignSigning == nil {
					if err := pushChartArchive(artifact, registry, name, ver, registryConfigPath); err != nil {
						return fmt.Errorf("chart %s: helm push: %w", rel, err)
					}
				} else {
					digestRef, err := uploadChartWithoutTag(artifact, registry, name, ver, registryConfigPath)
					if err != nil {
						return fmt.Errorf("chart %s: helm push: %w", rel, err)
					}
					if err := signOCIRef(digestRef, cosignSigning); err != nil {
						return fmt.Errorf("chart %s: cosign sign: %w", rel, err)
					}
					if err := tagOCIChart(registry, name, ver, digestRef, registryConfigPath); err != nil {
						return fmt.Errorf("chart %s: helm push tag: %w", rel, err)
					}
				}
				l.Info(logIdCI, "published {chart} version {version}", log.String("chart", name), log.String("version", ver))
			} else {
				l.Info(logIdCI, "publish local: packaged {chart} to {path}", log.String("chart", name), log.String("path", packaged.TGZPath))
			}
		}
		return nil
	})
}

func RunLocalPackage(chartDir string, destinationDir string) (PackagedChart, error) {
	l := log.Default()
	absChartDir, err := filepath.Abs(chartDir)
	if err != nil {
		return PackagedChart{}, fmt.Errorf("resolve chart dir: %w", err)
	}
	absDestinationDir, err := filepath.Abs(destinationDir)
	if err != nil {
		return PackagedChart{}, fmt.Errorf("resolve destination dir: %w", err)
	}
	if err := os.MkdirAll(absDestinationDir, 0o755); err != nil {
		return PackagedChart{}, fmt.Errorf("create destination dir: %w", err)
	}
	if err := downloadChartDependencies(l, absChartDir, nil); err != nil {
		return PackagedChart{}, fmt.Errorf("dependency update: %w", err)
	}
	return packagePreparedChart(absChartDir, absDestinationDir, nil)
}

type cosignSigningConfig struct {
	KeyPath string
}

func packageChartArchive(chartDir, stageDir string, signing *packageSigningConfig) (packageArtifact, error) {
	packaged, err := packagePreparedChart(chartDir, stageDir, signing)
	if err != nil {
		return packageArtifact{}, err
	}
	return packageArtifact{TGZPath: packaged.TGZPath, ProvPath: packaged.ProvPath}, nil
}

func packagePreparedChart(chartDir, stageDir string, signing *packageSigningConfig) (PackagedChart, error) {
	if packageChartArchiveHook != nil {
		artifact, err := packageChartArchiveHook(chartDir, stageDir, signing)
		if err != nil {
			return PackagedChart{}, err
		}
		return PackagedChart{
			TGZPath:  artifact.TGZPath,
			ProvPath: artifact.ProvPath,
		}, nil
	}

	chrt, err := log.WithoutDebug2(func() (helmchart.Charter, error) {
		return loader.Load(chartDir)
	})
	if err != nil {
		return PackagedChart{}, fmt.Errorf("load chart: %w", err)
	}

	ops, err := chartFileOperationsForPackaging(chartDir)
	if err != nil {
		return PackagedChart{}, err
	}
	if !ops.Empty() {
		chrt, err = helm.ApplyChartFileOperations(chrt, ops)
		if err != nil {
			return PackagedChart{}, fmt.Errorf("apply global.hydra.templateFiles: %w", err)
		}
	}

	v2chrt, err := convertToV2Chart(chrt)
	if err != nil {
		return PackagedChart{}, err
	}

	ensureHydraVersionAnnotation(v2chrt)

	tgzPath, err := v2chartutil.Save(v2chrt, stageDir)
	if err != nil {
		return PackagedChart{}, err
	}

	packaged := PackagedChart{
		Name:    v2chrt.Name(),
		Version: v2chrt.Metadata.Version,
		TGZPath: tgzPath,
	}
	if signing != nil {
		provPath, err := signChartArchive(tgzPath, signing)
		if err != nil {
			return PackagedChart{}, err
		}
		packaged.ProvPath = provPath
	}
	return packaged, nil
}

func chartFileOperationsForPackaging(chartDir string) (helm.ChartFileOperations, error) {
	valuesPath := filepath.Join(chartDir, "values.yaml")
	data, err := os.ReadFile(valuesPath)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		return helm.ChartFileOperations{}, nil
	default:
		return helm.ChartFileOperations{}, fmt.Errorf("read values.yaml: %w", err)
	}

	var hydraGlobal coretypes.HydraGlobal
	if err := yaml.Unmarshal(data, &hydraGlobal); err != nil {
		return helm.ChartFileOperations{}, fmt.Errorf("parse values.yaml for global.hydra.templateFiles: %w", err)
	}
	if hydraGlobal.Global == nil || hydraGlobal.Global.Hydra == nil || hydraGlobal.Global.Hydra.TemplateFiles == nil {
		return helm.ChartFileOperations{}, nil
	}
	if err := coretypes.ValidateHydraTemplateFiles(hydraGlobal.Global.Hydra.TemplateFiles); err != nil {
		return helm.ChartFileOperations{}, err
	}

	templateFiles := hydraGlobal.Global.Hydra.TemplateFiles
	ops := helm.ChartFileOperations{
		Moves:   make([]helm.ChartFileMove, 0, len(templateFiles.Move)),
		Deletes: make([]string, 0, len(templateFiles.Delete)),
	}
	for _, move := range templateFiles.Move {
		ops.Moves = append(ops.Moves, helm.ChartFileMove{
			From: helm.NormalizeChartFileReplacementPath(move.From),
			To:   helm.NormalizeChartFileReplacementPath(move.To),
		})
	}
	for _, path := range templateFiles.Delete {
		ops.Deletes = append(ops.Deletes, helm.NormalizeChartFileReplacementPath(path))
	}
	return ops, nil
}

func pushChartArchive(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) error {
	if pushChartArchiveHook != nil {
		return pushChartArchiveHook(artifact, registryURL, chartName, version, registryConfigPath)
	}

	chartData, err := os.ReadFile(artifact.TGZPath)
	if err != nil {
		return fmt.Errorf("read packaged chart: %w", err)
	}
	var pushOpts []registry.PushOption
	if artifact.ProvPath != "" {
		provData, err := os.ReadFile(artifact.ProvPath)
		if err != nil {
			return fmt.Errorf("read chart provenance: %w", err)
		}
		pushOpts = append(pushOpts, registry.PushOptProvData(provData))
	}

	client, err := registry.NewClient(
		registry.ClientOptCredentialsFile(registryConfigPath),
		registry.ClientOptDebug(false),
		registry.ClientOptEnableCache(true),
		registry.ClientOptWriter(io.Discard),
	)
	if err != nil {
		return fmt.Errorf("create registry client: %w", err)
	}

	ref := buildOCIChartRef(registryURL, chartName, version)
	if _, err := client.Push(chartData, ref, pushOpts...); err != nil {
		return err
	}
	return nil
}

func uploadChartWithoutTag(artifact packageArtifact, registryURL, chartName, version string, registryConfigPath string) (string, error) {
	if uploadChartWithoutTagHook != nil {
		return uploadChartWithoutTagHook(artifact, registryURL, chartName, version, registryConfigPath)
	}
	_ = version
	_ = registryConfigPath

	chartData, err := os.ReadFile(artifact.TGZPath)
	if err != nil {
		return "", fmt.Errorf("read packaged chart: %w", err)
	}

	chrt, err := loader.LoadFile(artifact.TGZPath)
	if err != nil {
		return "", fmt.Errorf("load chart for OCI upload: %w", err)
	}
	v2chrt, err := convertToV2Chart(chrt)
	if err != nil {
		return "", err
	}
	configData, err := json.Marshal(v2chrt.Metadata)
	if err != nil {
		return "", fmt.Errorf("marshal chart metadata for OCI upload: %w", err)
	}

	repo, err := newOCIRepository(registryURL, chartName)
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	chartDesc, err := oras.PushBytes(ctx, repo, registry.ChartLayerMediaType, chartData)
	if err != nil {
		return "", fmt.Errorf("upload chart layer: %w", err)
	}
	configDesc, err := oras.PushBytes(ctx, repo, registry.ConfigMediaType, configData)
	if err != nil {
		return "", fmt.Errorf("upload chart config: %w", err)
	}

	layers := []ocispec.Descriptor{chartDesc}
	if artifact.ProvPath != "" {
		provData, err := os.ReadFile(artifact.ProvPath)
		if err != nil {
			return "", fmt.Errorf("read chart provenance: %w", err)
		}
		provDesc, err := oras.PushBytes(ctx, repo, registry.ProvLayerMediaType, provData)
		if err != nil {
			return "", fmt.Errorf("upload chart provenance: %w", err)
		}
		layers = append(layers, provDesc)
	}

	sort.Slice(layers, func(i, j int) bool {
		return layers[i].Digest < layers[j].Digest
	})

	manifestDesc, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_0, "", oras.PackManifestOptions{
		ConfigDescriptor: &configDesc,
		Layers:           layers,
	})
	if err != nil {
		return "", fmt.Errorf("upload chart manifest: %w", err)
	}

	digestRef := buildOCIRepositoryRef(registryURL, chartName) + "@" + manifestDesc.Digest.String()
	return digestRef, nil
}

func tagOCIChart(registryURL, chartName, version, digestRef, registryConfigPath string) error {
	if tagOCIChartHook != nil {
		return tagOCIChartHook(registryURL, chartName, version, digestRef, registryConfigPath)
	}
	_ = registryConfigPath

	repo, err := newOCIRepository(registryURL, chartName)
	if err != nil {
		return err
	}

	ctx := context.Background()
	desc, err := repo.Resolve(ctx, strings.TrimPrefix(digestRef, buildOCIRepositoryRef(registryURL, chartName)+"@"))
	if err != nil {
		return fmt.Errorf("resolve uploaded digest before tagging: %w", err)
	}
	if err := repo.Tag(ctx, desc, version); err != nil {
		return err
	}
	return nil
}

func newOCIRepository(registryURL, chartName string) (*remote.Repository, error) {
	repoRef := buildOCIRepositoryRef(registryURL, chartName)
	repo, err := remote.NewRepository(repoRef)
	if err != nil {
		return nil, fmt.Errorf("create remote repository: %w", err)
	}

	store, err := openRegistryCredentialStore()
	if err != nil {
		return nil, fmt.Errorf("open registry credentials: %w", err)
	}
	if store != nil {
		client := *orasauth.DefaultClient
		client.Credential = func(ctx context.Context, hostport string) (orasauth.Credential, error) {
			return store.Get(ctx, orascredentials.ServerAddressFromRegistry(hostport))
		}
		repo.Client = &client
	}
	return repo, nil
}

func preparePackageSigningConfig(configPath, workDir string) (*packageSigningConfig, error) {
	publicCfg, secretCfg, err := LoadValidatedSignConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load CI signing configuration: %w", err)
	}

	signDir := filepath.Join(workDir, "signing")
	if err := os.MkdirAll(signDir, 0o700); err != nil {
		return nil, fmt.Errorf("create signing workspace: %w", err)
	}

	keyPath, err := decodeArmoredKeyToBinaryFile(secretCfg.Secrets.Sign.SecretKeyring, filepath.Join(signDir, "secret-keyring.gpg"))
	if err != nil {
		return nil, fmt.Errorf("prepare signing private key: %w", err)
	}
	keyringPath, err := decodeArmoredKeyBytesToBinaryFile([]byte(publicCfg.PublicKey), filepath.Join(signDir, "public-keyring.gpg"))
	if err != nil {
		return nil, fmt.Errorf("prepare signing public keyring: %w", err)
	}

	return &packageSigningConfig{
		KeyPath:     keyPath,
		KeyringPath: keyringPath,
		Key:         strings.TrimSpace(publicCfg.Key),
	}, nil
}

func prepareCosignSigningConfig(configPath, workDir string) (*cosignSigningConfig, error) {
	_, secretCfg, err := LoadValidatedCosignConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("load CI cosign configuration: %w", err)
	}

	signDir := filepath.Join(workDir, "cosign")
	if err := os.MkdirAll(signDir, 0o700); err != nil {
		return nil, fmt.Errorf("create cosign workspace: %w", err)
	}

	keyPath, err := decodeBase64ToFile(secretCfg.Secrets.Cosign.PrivateKey, filepath.Join(signDir, "cosign.key"))
	if err != nil {
		return nil, fmt.Errorf("prepare cosign private key: %w", err)
	}
	return &cosignSigningConfig{KeyPath: keyPath}, nil
}

func preparePublishSigning(configPath, workDir string) (*packageSigningConfig, *cosignSigningConfig, error) {
	var helmSigning *packageSigningConfig
	var cosignSigning *cosignSigningConfig

	if _, err := LoadPublicSignConfig(configPath); err == nil {
		helmSigning, err = preparePackageSigningConfig(configPath, workDir)
		if err != nil {
			return nil, nil, err
		}
	} else if !isMissingHelmSignConfig(err) {
		return nil, nil, err
	}

	if _, err := LoadPublicCosignConfig(configPath); err == nil {
		cosignSigning, err = prepareCosignSigningConfig(configPath, workDir)
		if err != nil {
			return nil, nil, err
		}
	} else if !isMissingCosignSignConfig(err) {
		return nil, nil, err
	}

	return helmSigning, cosignSigning, nil
}

func decodeArmoredKeyToBinaryFile(encoded, targetPath string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("decode base64 key material: %w", err)
	}
	return decodeArmoredKeyBytesToBinaryFile(raw, targetPath)
}

func decodeBase64ToFile(encoded, targetPath string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", fmt.Errorf("decode base64 key material: %w", err)
	}
	if err := os.WriteFile(targetPath, raw, 0o600); err != nil {
		return "", fmt.Errorf("write key material: %w", err)
	}
	return targetPath, nil
}

func signOCIChart(registryURL, chartName, version string, signing *cosignSigningConfig, registryConfigPath string) error {
	ref, err := resolveOCIChartDigestRef(registryURL, chartName, version, registryConfigPath)
	if err != nil {
		return err
	}
	return signOCIRef(ref, signing)
}

func signOCIRef(ref string, signing *cosignSigningConfig) error {
	if signOCIChartHook != nil {
		return signOCIChartHook(ref, signing.KeyPath)
	}

	ro := &cosignopts.RootOptions{Timeout: cosignopts.DefaultTimeout}
	ko := cosignopts.KeyOpts{
		KeyRef:   signing.KeyPath,
		PassFunc: func(bool) ([]byte, error) { return nil, nil },
	}
	registryOpts, err := resolveCosignRegistryOptions(ref)
	if err != nil {
		return err
	}
	signOpts := cosignopts.SignOptions{
		Upload:           true,
		SkipConfirmation: true,
		TlogUpload:       false,
		Registry:         registryOpts,
	}
	return withCosignHydraLogger(func() error {
		return cosignsign.SignCmd(ro, ko, signOpts, []string{ref})
	})
}

func resolveOCIChartDigestRef(registryURL, chartName, version string, registryConfigPath string) (string, error) {
	if resolveOCIChartDigestRefHook != nil {
		return resolveOCIChartDigestRefHook(registryURL, chartName, version, registryConfigPath)
	}
	client, err := registry.NewClient(
		registry.ClientOptCredentialsFile(registryConfigPath),
		registry.ClientOptDebug(false),
		registry.ClientOptEnableCache(true),
		registry.ClientOptWriter(io.Discard),
	)
	if err != nil {
		return "", fmt.Errorf("create registry client: %w", err)
	}
	ref := buildOCIResolveRef(registryURL, chartName, version)
	desc, err := client.Resolve(ref)
	if err != nil {
		return "", fmt.Errorf("resolve chart digest for cosign: %w", err)
	}
	repository := strings.TrimSuffix(ref, ":"+version)
	return repository + "@" + desc.Digest.String(), nil
}

func isMissingHelmSignConfig(err error) bool {
	return err != nil && strings.Contains(err.Error(), "ci.sign.helm.publicKey must not be empty")
}

func isMissingCosignSignConfig(err error) bool {
	return err != nil && strings.Contains(err.Error(), "ci.sign.cosign.publicKey must not be empty")
}

func decodeArmoredKeyBytesToBinaryFile(raw []byte, targetPath string) (string, error) {
	binary, err := decodeArmoredKeyBytes(raw)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(targetPath, binary, 0o600); err != nil {
		return "", fmt.Errorf("write key material: %w", err)
	}
	return targetPath, nil
}

func decodeArmoredKeyBytes(raw []byte) ([]byte, error) {
	block, err := armor.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode armored key material: %w", err)
	}
	binary, err := io.ReadAll(block.Body)
	if err != nil {
		return nil, fmt.Errorf("read armored key material: %w", err)
	}
	return binary, nil
}

func signChartArchive(tgzPath string, signing *packageSigningConfig) (string, error) {
	signer, err := provenance.NewFromFiles(signing.KeyPath, signing.KeyringPath)
	if err != nil {
		return "", fmt.Errorf("load signing key material: %w", err)
	}
	if err := signer.DecryptKey(func(string) ([]byte, error) { return nil, nil }); err != nil {
		return "", fmt.Errorf("unlock signing key: %w", err)
	}

	chrt, err := loader.LoadFile(tgzPath)
	if err != nil {
		return "", fmt.Errorf("load chart for signing: %w", err)
	}
	v2chrt, err := convertToV2Chart(chrt)
	if err != nil {
		return "", err
	}
	metadataBytes, err := yaml.Marshal(v2chrt.Metadata)
	if err != nil {
		return "", fmt.Errorf("marshal chart metadata for signing: %w", err)
	}
	archiveData, err := os.ReadFile(tgzPath)
	if err != nil {
		return "", fmt.Errorf("read chart archive for signing: %w", err)
	}

	sig, err := signer.ClearSign(archiveData, filepath.Base(tgzPath), metadataBytes)
	if err != nil {
		return "", fmt.Errorf("sign chart provenance with key %s: %w", signing.Key, err)
	}
	provPath := tgzPath + ".prov"
	if err := os.WriteFile(provPath, []byte(sig), 0o644); err != nil {
		return "", fmt.Errorf("write chart provenance: %w", err)
	}
	return provPath, nil
}

func remoteChartExists(registryURL, chartName, version string, registryConfigPath string) (bool, error) {
	if remoteChartExistsHook != nil {
		return remoteChartExistsHook(registryURL, chartName, version, registryConfigPath)
	}

	client, err := registry.NewClient(
		registry.ClientOptCredentialsFile(registryConfigPath),
		registry.ClientOptDebug(false),
		registry.ClientOptEnableCache(true),
		registry.ClientOptWriter(io.Discard),
	)
	if err != nil {
		return false, fmt.Errorf("create registry client: %w", err)
	}

	ref := buildOCIResolveRef(registryURL, chartName, version)
	if _, err := client.Resolve(ref); err != nil {
		if errors.Is(err, oraserrdef.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func buildOCIChartRef(registryURL, chartName, version string) string {
	base := strings.TrimSpace(strings.TrimSuffix(registryURL, "/"))
	return fmt.Sprintf("%s/%s:%s", base, chartName, version)
}

func buildOCIResolveRef(registryURL, chartName, version string) string {
	return strings.TrimPrefix(buildOCIChartRef(registryURL, chartName, version), "oci://")
}

func buildOCIRepositoryRef(registryURL, chartName string) string {
	base := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(registryURL, "oci://"), "/"))
	return fmt.Sprintf("%s/%s", base, chartName)
}

func convertToV2Chart(chrt helmchart.Charter) (*v2chart.Chart, error) {
	switch c := chrt.(type) {
	case *v2chart.Chart:
		return c, nil
	case v2chart.Chart:
		return &c, nil
	default:
		return nil, fmt.Errorf("unsupported chart type %T", chrt)
	}
}

func ensureHydraVersionAnnotation(chrt *v2chart.Chart) {
	if chrt == nil || chrt.Metadata == nil {
		return
	}
	if chrt.Metadata.Annotations == nil {
		chrt.Metadata.Annotations = map[string]string{}
	}
	chrt.Metadata.Annotations[hydraVersionAnnotation] = buildinfo.String()
}

func resolvePackageChartPaths(repo *git.Repo, cfg *Config, groups []string, selectedCharts []string, forcePublish bool) ([]string, error) {
	l := log.Default()
	if len(selectedCharts) > 0 {
		return resolveExplicitPackageChartPaths(repo, cfg, selectedCharts, forcePublish)
	}

	tags, err := repo.TagsPointingTo("HEAD")
	if err != nil {
		return nil, fmt.Errorf("list tags at HEAD: %w", err)
	}
	hasBuild := false
	for _, t := range tags {
		if strings.HasPrefix(t, BuildTagPrefix) {
			hasBuild = true
			break
		}
	}
	if !hasBuild {
		if !forcePublish {
			return nil, fmt.Errorf("ci publish: HEAD must have a %s* lightweight tag on the same commit as the release; tags at HEAD: %v; rerun from the release commit or use --force-run", BuildTagPrefix, tags)
		}
		l.Warn(logIdCI, "build tag missing at HEAD: expected a {prefix}* lightweight tag on the release commit",
			log.String("prefix", BuildTagPrefix))
	}

	relSet, err := chartPathsFromTags(repo, cfg, groups, tags)
	if err != nil {
		return nil, err
	}
	if len(relSet) == 0 {
		if forcePublish {
			tags, err = tagsFromLatestBuildCommit(repo)
			if err != nil {
				return nil, fmt.Errorf("ci publish: resolve latest %s* tag for force publish: %w", BuildTagPrefix, err)
			}
			l.Warn(logIdCI, "force publish run resolved tags from latest {prefix}* tag in history",
				log.String("prefix", BuildTagPrefix))
			relSet, err = chartPathsFromTags(repo, cfg, groups, tags)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(relSet) == 0 {
		return nil, fmt.Errorf("ci publish: no app or root tags mapped to charts at HEAD (tags: %v)", tags)
	}
	out := make([]string, 0, len(relSet))
	for p := range relSet {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func tagsFromLatestBuildCommit(repo *git.Repo) ([]string, error) {
	buildTags, err := repo.Tags(BuildTagPrefix + "*")
	if err != nil {
		return nil, err
	}
	if len(buildTags) == 0 {
		return nil, fmt.Errorf("no %s* tags found", BuildTagPrefix)
	}
	return repo.TagsPointingTo(buildTags[len(buildTags)-1])
}

func chartPathsFromTags(repo *git.Repo, cfg *Config, groups []string, tags []string) (map[string]struct{}, error) {
	relSet := make(map[string]struct{})
	for _, tag := range tags {
		if strings.HasPrefix(tag, BuildTagPrefix) {
			continue
		}
		rel, ok, err := chartRelPathFromReleaseTag(tag, cfg.CI.RootAppsPath, groups)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		abs := filepath.Join(repo.Path(), filepath.FromSlash(rel))
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("ci publish: chart %q from tag %q: %w", rel, tag, err)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("ci publish: chart path %q from tag %q is not a directory", rel, tag)
		}
		relSet[rel] = struct{}{}
	}
	return relSet, nil
}

func resolveExplicitPackageChartPaths(repo *git.Repo, cfg *Config, selectedCharts []string, forcePublish bool) ([]string, error) {
	l := log.Default()
	headHash, err := repo.CommitHash("HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve HEAD: %w", err)
	}

	seen := make(map[string]struct{}, len(selectedCharts))
	out := make([]string, 0, len(selectedCharts))
	var publishCommit string

	for _, raw := range selectedCharts {
		rel, err := normalizeSelectedChartPath(cfg.CI.RootAppsPath, raw)
		if err != nil {
			return nil, err
		}
		abs := filepath.Join(repo.Path(), filepath.FromSlash(rel))
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("ci publish: selected chart %q: %w", rel, err)
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("ci publish: selected chart path %q is not a directory", rel)
		}

		tag, err := releaseTagForChart(repo, rel)
		if err != nil {
			return nil, err
		}
		tagCommit, err := repo.CommitHash(tag)
		if err != nil {
			return nil, fmt.Errorf("ci publish: resolve publish tag %q for %q: %w", tag, rel, err)
		}
		if publishCommit == "" {
			publishCommit = tagCommit
		} else if publishCommit != tagCommit {
			return nil, fmt.Errorf("ci publish: selected charts do not point to the same publish commit (%s at %s, %s at %s)", out[0], publishCommit, rel, tagCommit)
		}

		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}

	if publishCommit != "" && headHash != publishCommit {
		if forcePublish {
			l.Warn(logIdCI, "publish commit mismatch: HEAD {head} differs from publish commit {publish}",
				log.String("head", headHash),
				log.String("publish", publishCommit))
		} else {
			l.Error(logIdCI, "publish commit mismatch: HEAD {head} differs from publish commit {publish}; rerun from the release commit or use --force-run",
				log.String("head", headHash),
				log.String("publish", publishCommit))
		}
		return nil, fmt.Errorf("ci publish: HEAD %s does not match publish commit %s", headHash, publishCommit)
	}

	sort.Strings(out)
	return out, nil
}

func normalizeSelectedChartPath(rootAppsPath, chart string) (string, error) {
	trimmed := filepath.ToSlash(strings.TrimSpace(chart))
	if trimmed == "" {
		return "", fmt.Errorf("ci publish: chart argument must not be empty")
	}
	if strings.HasPrefix(trimmed, "/") {
		return "", fmt.Errorf("ci publish: chart %q must be relative", chart)
	}
	parts := strings.Split(trimmed, "/")
	switch len(parts) {
	case 3:
		return filepath.ToSlash(filepath.Join(rootAppsPath, parts[0], parts[1], parts[2])), nil
	default:
		if len(parts) >= 4 {
			return trimmed, nil
		}
		return "", fmt.Errorf("ci publish: chart %q must be <group>/<app>/<env> or a repo-relative chart path", chart)
	}
}

func releaseTagForChart(repo *git.Repo, rel string) (string, error) {
	group, app, _, err := ParseChartPath(rel)
	if err != nil {
		return "", fmt.Errorf("ci publish: parse chart path %q: %w", rel, err)
	}
	ch, err := repo.LoadChart(rel)
	if err != nil {
		return "", fmt.Errorf("ci publish: load chart %q: %w", rel, err)
	}
	version := ch.GetVersion()
	if version == "" {
		return "", fmt.Errorf("ci publish: chart %q has no version", rel)
	}
	if app == "root" {
		return fmt.Sprintf("%s-root-%s", group, version), nil
	}
	return fmt.Sprintf("%s-%s-%s", group, app, version), nil
}
