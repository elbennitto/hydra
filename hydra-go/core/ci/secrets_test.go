package ci

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func writeSecretsTestConfig(t *testing.T, dir string, secretsPath string) string {
	t.Helper()
	cfg := `ci:
  rootAppsPath: apps
  environments: [dev]
`
	if secretsPath != "" {
		cfg += "  secretsPath: " + secretsPath + "\n"
	}
	path := filepath.Join(dir, ConfigFileName)
	require.NoError(t, os.WriteFile(path, []byte(cfg), 0o644))
	return path
}

func writePublicSignConfig(t *testing.T, dir string, sign PublicSignConfig) string {
	t.Helper()
	cfg := `ci:
  rootAppsPath: apps
  environments: [dev]
  sign:
    helm:
`
	if sign.Name != "" {
		cfg += "      name: " + sign.Name + "\n"
	}
	if sign.Key != "" {
		cfg += "      key: " + sign.Key + "\n"
	}
	if sign.PublicKey != "" {
		cfg += "      publicKey: |\n"
		for _, line := range strings.Split(strings.TrimSuffix(sign.PublicKey, "\n"), "\n") {
			cfg += "        " + line + "\n"
		}
	}
	path := filepath.Join(dir, ConfigFileName)
	require.NoError(t, os.WriteFile(path, []byte(cfg), 0o644))
	return path
}

func TestResolveSecretsFilePath_DefaultsToSiblingFile(t *testing.T) {
	dir := t.TempDir()

	got, err := resolveSecretsFilePath(dir, "")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, SecretsFileName), got)
}

func TestResolveSecretsFilePath_DirectoryPathGetsDefaultFilename(t *testing.T) {
	dir := t.TempDir()

	got, err := resolveSecretsFilePath(dir, "secrets")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "secrets", SecretsFileName), got)
}

func TestResolveSecretsFilePath_FilePathIsUsedAsIs(t *testing.T) {
	dir := t.TempDir()

	got, err := resolveSecretsFilePath(dir, "ci/custom-secrets.sops.yaml")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "ci", "custom-secrets.sops.yaml"), got)
}

func TestValidateSecrets_AllowsRegistryTokensWithoutSigningKeys(t *testing.T) {
	err := validateSecrets(SecretsValues{
		RegistryTokens: []RegistryTokenRef{
			{
				Registry: "harbor.example.test",
				Username: "robot$hydra",
				Token:    "token-value",
				Upload:   true,
			},
		},
	}, filepath.Join(t.TempDir(), SecretsFileName))
	require.NoError(t, err)
}

func TestValidateSecrets_RejectsIncompleteRegistryToken(t *testing.T) {
	err := validateSecrets(SecretsValues{
		RegistryTokens: []RegistryTokenRef{
			{
				Registry: "harbor.example.test",
				Username: "robot$hydra",
			},
		},
	}, filepath.Join(t.TempDir(), SecretsFileName))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secrets.registryTokens[0].token must not be empty")
}

func TestValidateSecrets_AllowsPublishGitLabTokenWithoutSigningKeys(t *testing.T) {
	err := validateSecrets(SecretsValues{
		Publish: PublishSecrets{
			GitLabToken: "glpat-example",
		},
	}, filepath.Join(t.TempDir(), SecretsFileName))
	require.NoError(t, err)
}

func TestFindNearestSopsConfig_FindsParentConfig(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "nested")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".sops.yaml"), []byte("creation_rules: []\n"), 0o644))

	got, err := findNearestSopsConfig(targetDir)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(root, ".sops.yaml"), got)
}

func TestFindNearestSopsConfig_ErrorsWhenMissing(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "nested")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))

	_, err := findNearestSopsConfig(targetDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no .sops.yaml found starting from")
	assert.Contains(t, err.Error(), targetDir)
	assert.Contains(t, err.Error(), filepath.Join(targetDir, ".sops.yaml"))
}

func TestCreateSecretsFile_RejectsExistingFileWithoutForce(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".sops.yaml"), []byte("creation_rules: []\n"), 0o644))
	cfgPath := writeSecretsTestConfig(t, root, "")
	targetPath := filepath.Join(root, SecretsFileName)
	require.NoError(t, os.WriteFile(targetPath, []byte("existing"), 0o644))

	_, _, err := CreateSecretsFile(cfgPath, "build", false, SecretCreateSignersBoth)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestCreateSecretsFile_OverwritesExistingFileWithForce(t *testing.T) {
	root := t.TempDir()
	sopsConfigPath := filepath.Join(root, ".sops.yaml")
	require.NoError(t, os.WriteFile(sopsConfigPath, []byte("creation_rules: []\n"), 0o644))
	cfgPath := writeSecretsTestConfig(t, root, "")
	targetPath := filepath.Join(root, SecretsFileName)
	require.NoError(t, os.WriteFile(targetPath, []byte("existing"), 0o644))

	oldHook := encryptSecretsDataHook
	var usedConfigPath string
	var plainData string
	encryptSecretsDataHook = func(data types.YamlString, path string, configPath string) (types.YamlString, error) {
		usedConfigPath = configPath
		plainData = string(data)
		return types.YamlString(`secrets:
  sign:
    secretKeyring: ENC[AES256_GCM,data:def,type:str]
sops:
  kms: []
`), nil
	}
	t.Cleanup(func() {
		encryptSecretsDataHook = oldHook
	})

	gotPath, gotConfigPath, err := CreateSecretsFile(cfgPath, "build", true, SecretCreateSignersHelm)
	require.NoError(t, err)
	assert.Equal(t, targetPath, gotPath)
	assert.Equal(t, sopsConfigPath, gotConfigPath)
	assert.Equal(t, sopsConfigPath, usedConfigPath)

	var plainCfg SecretsConfig
	require.NoError(t, yaml.Unmarshal([]byte(plainData), &plainCfg))
	assert.NotEmpty(t, plainCfg.Secrets.Sign.SecretKeyring)
	_, err = base64.StdEncoding.DecodeString(plainCfg.Secrets.Sign.SecretKeyring)
	require.NoError(t, err)

	data, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "ENC[AES256_GCM")

	cfgData, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	var cfg Config
	require.NoError(t, yaml.Unmarshal(cfgData, &cfg))
	assert.Equal(t, "build", cfg.CI.Sign.Helm.Name)
	assert.NotEmpty(t, cfg.CI.Sign.Helm.Key)
	assert.Contains(t, cfg.CI.Sign.Helm.PublicKey, "BEGIN PGP PUBLIC KEY BLOCK")
	require.Len(t, cfg.CI.Sign.Helm.ValidKeys, 1)
	assert.Equal(t, cfg.CI.Sign.Helm.Key, cfg.CI.Sign.Helm.ValidKeys[0].Key)
	assert.Equal(t, cfg.CI.Sign.Helm.Name, cfg.CI.Sign.Helm.ValidKeys[0].Name)
}

func TestCreateSecretsFile_RejectsMatchingEncryptedRegexRule(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".sops.yaml"), []byte("creation_rules: []\n"), 0o644))
	cfgPath := writeSecretsTestConfig(t, root, "")

	oldHook := encryptSecretsDataHook
	encryptSecretsDataHook = func(data types.YamlString, path string, configPath string) (types.YamlString, error) {
		return types.YamlString(`secrets:
  sign:
    secretKeyring: build@company.example
sops:
  encrypted_regex: "^(data|stringData)$"
`), nil
	}
	t.Cleanup(func() {
		encryptSecretsDataHook = oldHook
	})

	_, _, err := CreateSecretsFile(cfgPath, "build", false, SecretCreateSignersBoth)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypted_regex")
	assert.Contains(t, err.Error(), "encrypting all fields")
}

func TestCreateSecretsFile_RejectsEmptySigningKeyName(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".sops.yaml"), []byte("creation_rules: []\n"), 0o644))
	cfgPath := writeSecretsTestConfig(t, root, "")

	_, _, err := CreateSecretsFile(cfgPath, "   ", false, SecretCreateSignersHelm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestCreateSecretsFile_CosignOnlyUpdatesCosignConfig(t *testing.T) {
	root := t.TempDir()
	sopsConfigPath := filepath.Join(root, ".sops.yaml")
	require.NoError(t, os.WriteFile(sopsConfigPath, []byte("creation_rules: []\n"), 0o644))
	cfgPath := writeSecretsTestConfig(t, root, "")

	oldEncryptHook := encryptSecretsDataHook
	encryptSecretsDataHook = func(data types.YamlString, path string, configPath string) (types.YamlString, error) {
		return types.YamlString(`secrets:
  cosign:
    privateKey: ENC[AES256_GCM,data:def,type:str]
sops:
  kms: []
`), nil
	}
	oldCosignHook := generateCosignSecretsHook
	generateCosignSecretsHook = func() (GeneratedCosignSecrets, error) {
		return GeneratedCosignSecrets{
			KeyID:     "COSIGN-KEY",
			Cosign:    CosignSecrets{PrivateKey: base64.StdEncoding.EncodeToString([]byte("private-key"))},
			PublicKey: "-----BEGIN PUBLIC KEY-----\ncosign\n-----END PUBLIC KEY-----\n",
		}, nil
	}
	t.Cleanup(func() {
		encryptSecretsDataHook = oldEncryptHook
		generateCosignSecretsHook = oldCosignHook
	})

	_, _, err := CreateSecretsFile(cfgPath, "build", false, SecretCreateSignersCosign)
	require.NoError(t, err)

	cfgData, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	var cfg Config
	require.NoError(t, yaml.Unmarshal(cfgData, &cfg))
	assert.Empty(t, cfg.CI.Sign.Helm.PublicKey)
	assert.Equal(t, "-----BEGIN PUBLIC KEY-----\ncosign\n-----END PUBLIC KEY-----\n", cfg.CI.Sign.Cosign.PublicKey)
	require.Len(t, cfg.CI.Sign.Cosign.ValidKeys, 1)
	assert.Equal(t, "-----BEGIN PUBLIC KEY-----\ncosign\n-----END PUBLIC KEY-----", cfg.CI.Sign.Cosign.ValidKeys[0])
	assert.Equal(t, sopsConfigPath, filepath.Join(root, ".sops.yaml"))
}

func TestLoadPublicSignConfig_MissingNameShowsExpectedValue(t *testing.T) {
	root := t.TempDir()
	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)

	cfgPath := writePublicSignConfig(t, root, PublicSignConfig{
		Key:       generated.Key,
		PublicKey: generated.PublicKey,
	})

	_, err = LoadPublicSignConfig(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ci.sign.helm.name must not be empty")
	assert.Contains(t, err.Error(), generated.Name)
}

func TestLoadPublicSignConfig_MissingKeyShowsExpectedValue(t *testing.T) {
	root := t.TempDir()
	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)

	cfgPath := writePublicSignConfig(t, root, PublicSignConfig{
		Name:      generated.Name,
		PublicKey: generated.PublicKey,
	})

	_, err = LoadPublicSignConfig(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ci.sign.helm.key must not be empty")
	assert.Contains(t, err.Error(), generated.Key)
}

func TestLoadPublicSignConfig_MismatchedNameShowsExpectedValue(t *testing.T) {
	root := t.TempDir()
	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)

	cfgPath := writePublicSignConfig(t, root, PublicSignConfig{
		Name:      "Other Name <other@example.invalid>",
		Key:       generated.Key,
		PublicKey: generated.PublicKey,
	})

	_, err = LoadPublicSignConfig(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ci.sign.helm.name=")
	assert.Contains(t, err.Error(), "does not match ci.sign.helm.publicKey")
	assert.Contains(t, err.Error(), generated.Name)
}

func TestLoadPublicSignConfig_MismatchedKeyShowsExpectedValue(t *testing.T) {
	root := t.TempDir()
	generated, err := generateSignSecrets("Hydra CI Test <hydra-ci-test@example.invalid>")
	require.NoError(t, err)

	cfgPath := writePublicSignConfig(t, root, PublicSignConfig{
		Name:      generated.Name,
		Key:       strings.Repeat("0", len(generated.Key)),
		PublicKey: generated.PublicKey,
	})

	_, err = LoadPublicSignConfig(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ci.sign.helm.key=")
	assert.Contains(t, err.Error(), "does not match ci.sign.helm.publicKey")
	assert.Contains(t, err.Error(), generated.Key)
}
