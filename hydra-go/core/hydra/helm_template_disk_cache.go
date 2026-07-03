package hydra

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// helmTemplateDiskCacheParams is the on-disk cache key for Helm template renders.
type helmTemplateDiskCacheParams struct {
	CacheFormat                 int    `yaml:"cacheFormat"`
	AppID                       string `yaml:"appId"`
	HelmNetworkMode             string `yaml:"helmNetworkMode"`
	KubernetesVersionOrFallback string `yaml:"kubernetesVersionOrFallback"`
	ReleaseName                 string `yaml:"releaseName"`
	Namespace                   string `yaml:"namespace"`
	SkipCrds                    bool   `yaml:"skipCrds"`
	ValuesJSON                  string `yaml:"valuesJson"`
	StaticManifestsDigest       string `yaml:"staticManifestsDigest,omitempty"`
}

func helmTemplateDiskCacheDir(rootAppDir string) string {
	return filepath.Join(rootAppDir, ".hydra", "cache", "helm")
}

func helmTemplateDiskCachePaths(rootAppDir string, childSuffix string) (paramsPath string, templatesPath string) {
	base := helmTemplateDiskCacheDir(rootAppDir)
	if childSuffix == "" {
		return filepath.Join(base, "cache.yaml"), filepath.Join(base, "templates.yaml")
	}
	safe := sanitizeHelmTemplateDiskChildSuffix(childSuffix)
	return filepath.Join(base, fmt.Sprintf("cache-%s.yaml", safe)), filepath.Join(base, fmt.Sprintf("templates-%s.yaml", safe))
}

var helmDiskChildSuffixSanitize = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeHelmTemplateDiskChildSuffix(s string) string {
	out := helmDiskChildSuffixSanitize.ReplaceAllString(s, "_")
	if out == "" {
		return "app"
	}
	return out
}

func marshalHelmTemplateDiskCacheKey(p helmTemplateDiskCacheParams) ([]byte, error) {
	p.CacheFormat = 1
	return yaml.Marshal(&p)
}

func rootHelmTemplateDiskCacheKeyYAML(
	appID types.AppId,
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
	releaseName string,
	namespace string,
	mergedValues types.ValuesMap,
	staticDigest string,
) ([]byte, error) {
	vj, err := json.Marshal(mergedValues)
	if err != nil {
		return nil, err
	}
	return marshalHelmTemplateDiskCacheKey(helmTemplateDiskCacheParams{
		AppID:                       string(appID),
		HelmNetworkMode:             networkMode.String(),
		KubernetesVersionOrFallback: string(kubernetesVersionOrFallback),
		ReleaseName:                 releaseName,
		Namespace:                   namespace,
		SkipCrds:                    false,
		ValuesJSON:                  string(vj),
		StaticManifestsDigest:       staticDigest,
	})
}

func childHelmTemplateDiskCacheKeyYAML(
	appID types.AppId,
	networkMode types.HelmNetworkMode,
	kubernetesVersionOrFallback types.KubernetesVersionOrFallback,
	releaseName string,
	namespace string,
	skipCrds bool,
	mergedValues types.ValuesMap,
	staticDigest string,
) ([]byte, error) {
	vj, err := json.Marshal(mergedValues)
	if err != nil {
		return nil, err
	}
	return marshalHelmTemplateDiskCacheKey(helmTemplateDiskCacheParams{
		AppID:                       string(appID),
		HelmNetworkMode:             networkMode.String(),
		KubernetesVersionOrFallback: string(kubernetesVersionOrFallback),
		ReleaseName:                 releaseName,
		Namespace:                   namespace,
		SkipCrds:                    skipCrds,
		ValuesJSON:                  string(vj),
		StaticManifestsDigest:       staticDigest,
	})
}

func childAppStaticManifestDigest(rootAppPath string, childName types.ChildAppName) (string, error) {
	dir := filepath.Join(rootAppPath, "apps", string(childName))
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if !info.IsDir() {
		return "", nil
	}
	files, err := recursiveGlob(dir, "*.yaml")
	if err != nil {
		return "", err
	}
	slices.Sort(files)
	h := sha256.New()
	for _, f := range files {
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			return "", rerr
		}
		rel, rerr := filepath.Rel(rootAppPath, f)
		if rerr != nil {
			rel = filepath.Base(f)
		}
		rel = filepath.ToSlash(rel)
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// rootAppStaticBackupManifestDigest hashes root-app backup Sops files (backup-*.sops.yaml)
// in the root app directory so Helm template disk cache invalidates when they change.
func rootAppStaticBackupManifestDigest(rootPath string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(rootPath, "backup-*.sops.yaml"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	slices.Sort(matches)
	h := sha256.New()
	for _, f := range matches {
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			return "", rerr
		}
		rel, rerr := filepath.Rel(rootPath, f)
		if rerr != nil {
			rel = filepath.Base(f)
		}
		rel = filepath.ToSlash(rel)
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func tryReadHelmTemplateDiskCache(rootAppDir string, childSuffix string, wantKeyYAML []byte) (types.YamlString, bool, error) {
	paramsPath, templatesPath := helmTemplateDiskCachePaths(rootAppDir, childSuffix)
	existing, err := os.ReadFile(paramsPath)
	if err != nil {
		return "", false, nil
	}
	if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(wantKeyYAML)) {
		return "", false, nil
	}
	tpl, err := os.ReadFile(templatesPath)
	if err != nil {
		return "", false, nil
	}
	return types.YamlString(tpl), true, nil
}

// writeHelmTemplateDiskCacheFiles is a no-op: disk cache writes are disabled (workaround).
// tryReadHelmTemplateDiskCache still serves existing on-disk entries.
func writeHelmTemplateDiskCacheFiles(_ string, _ string, _ []byte, _ types.YamlString) error {
	return nil
}
