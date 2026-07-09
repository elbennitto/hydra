package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/registry"
	"hydra-gitops.org/hydra/hydra-go/base/log"
)

var prepareRegistryAuthHook func(configPath string) (string, func(), error)
var registryLoginSleep = time.Sleep

const registryLoginRetryAttempts = 3
const registryLoginRetryDelay = 3 * time.Second

func prepareRegistryAuth(configPath string) (string, func(), error) {
	return prepareRegistryAuthWithOptions(configPath, false)
}

func prepareUploadRegistryAuth(configPath string) (string, func(), error) {
	return prepareRegistryAuthWithOptions(configPath, true)
}

func prepareRegistryAuthWithOptions(configPath string, requireUpload bool) (string, func(), error) {
	if prepareRegistryAuthHook != nil {
		return prepareRegistryAuthHook(configPath)
	}

	tokens, err := loadRegistryTokens(configPath, requireUpload)
	if err != nil {
		return "", nil, err
	}
	if len(tokens) == 0 {
		if requireUpload {
			return "", nil, missingUploadTokenError(configPath)
		}
		return "", func() {}, nil
	}

	tmpDir, err := os.MkdirTemp("", "hydra-registry-auth-*")
	if err != nil {
		return "", nil, fmt.Errorf("create registry auth temp dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	registryConfig := filepath.Join(tmpDir, "registry", "config.json")
	if err := os.MkdirAll(filepath.Dir(registryConfig), 0o755); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("create registry auth directory: %w", err)
	}

	client, err := registry.NewClient(
		registry.ClientOptCredentialsFile(registryConfig),
		registry.ClientOptEnableCache(true),
	)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("create OCI registry client: %w", err)
	}

	l := log.Default()
	for _, token := range tokens {
		host := normalizeRegistryHost(token.Registry)
		if err := loginWithRetry(l, client, host, token.Username, token.Token); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("login to OCI registry %q: %w", host, err)
		}
		l.Info(logIdCI, "authenticated OCI registry credentials for host {host}",
			log.String("host", host))
	}

	return registryConfig, cleanup, nil
}

func loginWithRetry(l log.Logger, client *registry.Client, host, username, token string) error {
	var lastErr error
	for attempt := 1; attempt <= registryLoginRetryAttempts; attempt++ {
		err := client.Login(host, registry.LoginOptBasicAuth(username, token))
		if err == nil {
			if attempt > 1 {
				l.Info(logIdCI, "registry login retry succeeded for host {host} on attempt {attempt}",
					log.String("host", host),
					log.Int("attempt", attempt))
			}
			return nil
		}

		lastErr = err
		if attempt == registryLoginRetryAttempts || !shouldRetryRegistryLogin(err) {
			break
		}

		l.Warn(logIdCI, "registry login attempt {attempt}/{maxAttempts} failed for host {host}; retrying in {delay}: {error}",
			log.Int("attempt", attempt),
			log.Int("maxAttempts", registryLoginRetryAttempts),
			log.String("host", host),
			log.String("delay", registryLoginRetryDelay.String()),
			log.String("error", err.Error()))
		registryLoginSleep(registryLoginRetryDelay)
	}
	return lastErr
}

func shouldRetryRegistryLogin(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized")
}

func loadRegistryTokens(configPath string, requireUpload bool) ([]RegistryTokenRef, error) {
	targetPath, err := ResolveSecretsFilePath(configPath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(targetPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat secrets file %s: %w", targetPath, err)
	}

	cfg, err := LoadSecretsConfig(configPath)
	if err != nil {
		return nil, err
	}
	if !requireUpload {
		return cfg.Secrets.RegistryTokens, nil
	}

	filtered := make([]RegistryTokenRef, 0, len(cfg.Secrets.RegistryTokens))
	for _, token := range cfg.Secrets.RegistryTokens {
		if token.Upload {
			filtered = append(filtered, token)
		}
	}
	return filtered, nil
}

func normalizeRegistryHost(registryValue string) string {
	host := strings.TrimSpace(registryValue)
	host = strings.TrimPrefix(host, "oci://")
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}
	return host
}

func missingUploadTokenError(configPath string) error {
	return fmt.Errorf(
		"ci publish requires at least one writable OCI registry token.\n"+
			"Config: %s\n"+
			"Expected secret entry: secrets.registryTokens[].upload: true\n"+
			"Example:\n"+
			"  secrets:\n"+
			"    registryTokens:\n"+
			"      - registry: harbor.example.test\n"+
			"        username: robot$hydra-write\n"+
			"        token: <write-token>\n"+
			"        upload: true",
		configPath,
	)
}

func missingDownloadTokenError(configPath, registryHost string) error {
	return fmt.Errorf(
		"missing OCI registry credentials for dependency download.\n"+
			"Config: %s\n"+
			"Registry host: %s\n"+
			"Expected secret entry: secrets.registryTokens[]\n"+
			"Example:\n"+
			"  secrets:\n"+
			"    registryTokens:\n"+
			"      - registry: %s\n"+
			"        username: robot$hydra-read\n"+
			"        token: <read-token>",
		configPath,
		registryHost,
		registryHost,
	)
}

func withHelmRegistryConfig(registryConfigPath string, fn func() error) error {
	if strings.TrimSpace(registryConfigPath) == "" {
		return fn()
	}

	oldValue, hadOldValue := os.LookupEnv("HELM_REGISTRY_CONFIG")
	if err := os.Setenv("HELM_REGISTRY_CONFIG", registryConfigPath); err != nil {
		return fmt.Errorf("set HELM_REGISTRY_CONFIG: %w", err)
	}
	defer func() {
		if hadOldValue {
			_ = os.Setenv("HELM_REGISTRY_CONFIG", oldValue)
			return
		}
		_ = os.Unsetenv("HELM_REGISTRY_CONFIG")
	}()

	return fn()
}
