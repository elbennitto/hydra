package ci

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	cosignsigs "github.com/sigstore/cosign/v2/pkg/signature"
	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/sops"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

const SecretsFileName = ".hydra-ci-secrets.sops.yaml"

type SecretsConfig struct {
	Secrets SecretsValues `yaml:"secrets"`
}

type SecretsValues struct {
	Sign           SignSecrets        `yaml:"sign,omitempty"`
	Cosign         CosignSecrets      `yaml:"cosign,omitempty"`
	Publish        PublishSecrets     `yaml:"publish,omitempty"`
	RegistryTokens []RegistryTokenRef `yaml:"registryTokens,omitempty"`
}

type SignSecrets struct {
	SecretKeyring string `yaml:"secretKeyring"`
}

type CosignSecrets struct {
	PrivateKey string `yaml:"privateKey,omitempty"`
}

type PublishSecrets struct {
	GitLabToken string `yaml:"gitlabToken,omitempty"`
}

type RegistryTokenRef struct {
	Registry string `yaml:"registry,omitempty"`
	Username string `yaml:"username,omitempty"`
	Token    string `yaml:"token,omitempty"`
	Upload   bool   `yaml:"upload,omitempty"`
}

type GeneratedSignSecrets struct {
	Name      string
	Key       string
	Sign      SignSecrets
	PublicKey string
}

type GeneratedCosignSecrets struct {
	KeyID     string
	Cosign    CosignSecrets
	PublicKey string
}

type SecretCreateSigners string

const (
	SecretCreateSignersBoth   SecretCreateSigners = "both"
	SecretCreateSignersHelm   SecretCreateSigners = "helm"
	SecretCreateSignersCosign SecretCreateSigners = "cosign"
)

type encryptedSopsDocument struct {
	Sops encryptedSopsMetadata `yaml:"sops"`
}

type encryptedSopsMetadata struct {
	EncryptedRegex string `yaml:"encrypted_regex"`
}

var encryptSecretsDataHook func(data types.YamlString, path string, configPath string) (types.YamlString, error)
var generateSignSecretsHook func(name string) (GeneratedSignSecrets, error)
var generateCosignSecretsHook func() (GeneratedCosignSecrets, error)
var loadSecretsConfigHook func(configPath string) (*SecretsConfig, error)
var loadPublicSignConfigHook func(configPath string) (PublicSignConfig, error)
var loadPublicCosignConfigHook func(configPath string) (PublicCosignConfig, error)
var decryptedSecretsFileLogOnce sync.Once

func DefaultSecretsConfig(signSecrets SignSecrets, cosignSecrets CosignSecrets) *SecretsConfig {
	return &SecretsConfig{
		Secrets: SecretsValues{
			Sign:   signSecrets,
			Cosign: cosignSecrets,
		},
	}
}

func ResolveSecretsFilePath(configPath string) (string, error) {
	if info, err := os.Stat(configPath); err == nil && info.IsDir() {
		configPath = filepath.Join(configPath, ConfigFileName)
	}

	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}

	cfg, err := LoadConfig(filepath.Dir(absConfigPath))
	if err != nil {
		return "", err
	}
	return resolveSecretsFilePath(filepath.Dir(absConfigPath), cfg.CI.SecretsPath)
}

func resolveSecretsFilePath(configDir, configuredPath string) (string, error) {
	base := strings.TrimSpace(configuredPath)
	if base == "" {
		return filepath.Join(configDir, SecretsFileName), nil
	}

	if !filepath.IsAbs(base) {
		base = filepath.Join(configDir, base)
	}

	info, err := os.Stat(base)
	switch {
	case err == nil && info.IsDir():
		return filepath.Join(base, SecretsFileName), nil
	case err == nil:
		return base, nil
	case !os.IsNotExist(err):
		return "", fmt.Errorf("stat secrets path %q: %w", base, err)
	case looksLikeDirectoryPath(configuredPath):
		return filepath.Join(base, SecretsFileName), nil
	default:
		return base, nil
	}
}

func looksLikeDirectoryPath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return true
	}
	if strings.HasSuffix(trimmed, "/") || strings.HasSuffix(trimmed, string(filepath.Separator)) {
		return true
	}
	return filepath.Ext(filepath.Base(filepath.Clean(trimmed))) == ""
}

func CreateSecretsFile(configPath string, name string, force bool, signers SecretCreateSigners) (string, string, error) {
	if info, err := os.Stat(configPath); err == nil && info.IsDir() {
		configPath = filepath.Join(configPath, ConfigFileName)
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve config path: %w", err)
	}

	targetPath, err := ResolveSecretsFilePath(absConfigPath)
	if err != nil {
		return "", "", err
	}

	if _, err := os.Stat(targetPath); err == nil {
		if !force {
			return "", "", fmt.Errorf("secrets file already exists: %s", targetPath)
		}
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("stat secrets file: %w", err)
	}

	sopsConfigPath, err := findNearestSopsConfig(filepath.Dir(targetPath))
	if err != nil {
		return "", "", err
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", "", fmt.Errorf("create secrets directory: %w", err)
	}

	generatedHelm, generatedCosign, err := generateRequestedSecrets(name, signers)
	if err != nil {
		return "", "", err
	}

	data, err := yaml.Marshal(DefaultSecretsConfig(generatedHelm.Sign, generatedCosign.Cosign))
	if err != nil {
		return "", "", fmt.Errorf("marshal secrets template: %w", err)
	}
	encrypted, err := encryptSecretsData(types.YamlString(data), targetPath, sopsConfigPath)
	if err != nil {
		return "", "", err
	}
	if err := validateEncryptedSecretsDocument(encrypted, targetPath); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(targetPath, []byte(encrypted), 0o644); err != nil {
		return "", "", fmt.Errorf("write encrypted secrets file: %w", err)
	}

	cfg, err := LoadConfig(filepath.Dir(absConfigPath))
	if err != nil {
		return "", "", fmt.Errorf("load config for public signing metadata: %w", err)
	}
	if generatedHelm != (GeneratedSignSecrets{}) {
		cfg.CI.Sign.Helm = PublicSignConfig{
			Name:      generatedHelm.Name,
			Key:       generatedHelm.Key,
			PublicKey: generatedHelm.PublicKey,
		}
		cfg.CI.Sign.Helm.ValidKeys = mergeValidSignKeys(cfg.CI.Sign.Helm.ValidKeys, ValidSignKey{
			Key:  generatedHelm.Key,
			Name: generatedHelm.Name,
		})
	}
	if generatedCosign != (GeneratedCosignSecrets{}) {
		cfg.CI.Sign.Cosign = PublicCosignConfig{
			PublicKey: generatedCosign.PublicKey,
		}
		cfg.CI.Sign.Cosign.ValidKeys = mergeValidCosignKeys(cfg.CI.Sign.Cosign.ValidKeys, generatedCosign.PublicKey)
	}
	if err := WriteConfig(absConfigPath, cfg); err != nil {
		return "", "", fmt.Errorf("write config with public signing metadata: %w", err)
	}

	return targetPath, sopsConfigPath, nil
}

func generateRequestedSecrets(name string, signers SecretCreateSigners) (GeneratedSignSecrets, GeneratedCosignSecrets, error) {
	switch signers {
	case "", SecretCreateSignersBoth:
		helmSecrets, err := generateSignSecrets(name)
		if err != nil {
			return GeneratedSignSecrets{}, GeneratedCosignSecrets{}, err
		}
		cosignSecrets, err := generateCosignSecrets()
		if err != nil {
			return GeneratedSignSecrets{}, GeneratedCosignSecrets{}, err
		}
		return helmSecrets, cosignSecrets, nil
	case SecretCreateSignersHelm:
		helmSecrets, err := generateSignSecrets(name)
		if err != nil {
			return GeneratedSignSecrets{}, GeneratedCosignSecrets{}, err
		}
		return helmSecrets, GeneratedCosignSecrets{}, nil
	case SecretCreateSignersCosign:
		cosignSecrets, err := generateCosignSecrets()
		if err != nil {
			return GeneratedSignSecrets{}, GeneratedCosignSecrets{}, err
		}
		return GeneratedSignSecrets{}, cosignSecrets, nil
	default:
		return GeneratedSignSecrets{}, GeneratedCosignSecrets{}, fmt.Errorf("unsupported signer selection %q", signers)
	}
}

func mergeValidCosignKeys(existing []string, key string) []string {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return existing
	}
	for _, entry := range existing {
		if normalizeSignValue(entry) == normalizeSignValue(trimmedKey) {
			return existing
		}
	}
	return append(existing, trimmedKey)
}

func encryptSecretsData(data types.YamlString, path string, configPath string) (types.YamlString, error) {
	if encryptSecretsDataHook != nil {
		return encryptSecretsDataHook(data, path, configPath)
	}
	return sops.EncryptSopsYamlWithConfig(data, path, configPath)
}

func generateSignSecrets(name string) (GeneratedSignSecrets, error) {
	if generateSignSecretsHook != nil {
		return generateSignSecretsHook(name)
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return GeneratedSignSecrets{}, fmt.Errorf("signing key name must not be empty")
	}

	entityName := trimmedName
	entityEmail := ""
	if addr, err := mail.ParseAddress(trimmedName); err == nil {
		entityName = addr.Name
		entityEmail = addr.Address
	}
	cfg := &packet.Config{
		DefaultHash: crypto.SHA512,
	}
	entity, err := openpgp.NewEntity(entityName, "", entityEmail, cfg)
	if err != nil {
		return GeneratedSignSecrets{}, fmt.Errorf("generate signing key: %w", err)
	}
	fingerprint := strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint[:]))

	publicKey, err := serializeEntityArmored(entity, openpgp.PublicKeyType, false, cfg)
	if err != nil {
		return GeneratedSignSecrets{}, fmt.Errorf("export public signing key %s: %w", fingerprint, err)
	}
	privateKey, err := serializeEntityArmored(entity, openpgp.PrivateKeyType, true, cfg)
	if err != nil {
		return GeneratedSignSecrets{}, fmt.Errorf("export private signing key %s: %w", fingerprint, err)
	}

	return GeneratedSignSecrets{
		Name: trimmedName,
		Key:  fingerprint,
		Sign: SignSecrets{
			SecretKeyring: base64.StdEncoding.EncodeToString(privateKey.Bytes()),
		},
		PublicKey: publicKey.String(),
	}, nil
}

func generateCosignSecrets() (GeneratedCosignSecrets, error) {
	if generateCosignSecretsHook != nil {
		return generateCosignSecretsHook()
	}

	keys, err := cosign.GenerateKeyPair(nil)
	if err != nil {
		return GeneratedCosignSecrets{}, fmt.Errorf("generate cosign key pair: %w", err)
	}
	sum := sha256.Sum256(keys.PublicBytes)
	return GeneratedCosignSecrets{
		KeyID: strings.ToUpper(hex.EncodeToString(sum[:])),
		Cosign: CosignSecrets{
			PrivateKey: base64.StdEncoding.EncodeToString(keys.PrivateBytes),
		},
		PublicKey: string(keys.PublicBytes),
	}, nil
}

func serializeEntityArmored(entity *openpgp.Entity, blockType string, includePrivate bool, cfg *packet.Config) (*bytes.Buffer, error) {
	var out bytes.Buffer
	w, err := armor.Encode(&out, blockType, nil)
	if err != nil {
		return nil, err
	}
	if includePrivate {
		err = entity.SerializePrivate(w, cfg)
	} else {
		err = entity.Serialize(w)
	}
	closeErr := w.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return &out, nil
}

func ShowSecretsFile(configPath string) (string, types.YamlString, error) {
	targetPath, err := ResolveSecretsFilePath(configPath)
	if err != nil {
		return "", "", err
	}
	data, err := sops.DecryptSopsFile(targetPath)
	if err != nil {
		return "", "", err
	}
	return targetPath, data, nil
}

func LoadSecretsConfig(configPath string) (*SecretsConfig, error) {
	if loadSecretsConfigHook != nil {
		return loadSecretsConfigHook(configPath)
	}

	l := log.Default()
	targetPath, data, err := ShowSecretsFile(configPath)
	if err != nil {
		l.Error(logIdCI, "ci secrets: could not decrypt secrets file: {err}",
			log.String("err", err.Error()),
		)
		return nil, err
	}
	decryptedSecretsFileLogOnce.Do(func() {
		l.Info(logIdCI, "ci secrets: decrypted secrets file {path}",
			log.String("path", targetPath),
		)
	})

	var cfg SecretsConfig
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		return nil, fmt.Errorf("parse decrypted secrets config %s: %w", targetPath, err)
	}
	if err := validateSecrets(cfg.Secrets, targetPath); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validateSecrets(secrets SecretsValues, targetPath string) error {
	if err := validateSignSecrets(secrets.Sign, targetPath); err != nil {
		return err
	}
	if err := validateCosignSecrets(secrets.Cosign, targetPath); err != nil {
		return err
	}
	if err := validatePublishSecrets(secrets.Publish, targetPath); err != nil {
		return err
	}
	if err := validateRegistryTokens(secrets.RegistryTokens, targetPath); err != nil {
		return err
	}
	if strings.TrimSpace(secrets.Sign.SecretKeyring) == "" &&
		strings.TrimSpace(secrets.Cosign.PrivateKey) == "" &&
		strings.TrimSpace(secrets.Publish.GitLabToken) == "" &&
		len(secrets.RegistryTokens) == 0 {
		return fmt.Errorf("decrypted secrets config %s: at least one of secrets.sign, secrets.cosign, secrets.publish, or secrets.registryTokens must be configured", targetPath)
	}
	return nil
}

func LoadValidatedSignConfig(configPath string) (PublicSignConfig, *SecretsConfig, error) {
	publicCfg, err := LoadPublicSignConfig(configPath)
	if err != nil {
		return PublicSignConfig{}, nil, err
	}
	secretCfg, err := LoadSecretsConfig(configPath)
	if err != nil {
		return PublicSignConfig{}, nil, err
	}
	derived, err := derivePublicSignConfig(secretCfg.Secrets.Sign.SecretKeyring)
	if err != nil {
		return PublicSignConfig{}, nil, err
	}
	if err := validateMatchingSignConfig(publicCfg, derived); err != nil {
		return PublicSignConfig{}, nil, err
	}
	return publicCfg, secretCfg, nil
}

func LoadValidatedCosignConfig(configPath string) (PublicCosignConfig, *SecretsConfig, error) {
	publicCfg, err := LoadPublicCosignConfig(configPath)
	if err != nil {
		return PublicCosignConfig{}, nil, err
	}
	secretCfg, err := LoadSecretsConfig(configPath)
	if err != nil {
		return PublicCosignConfig{}, nil, err
	}
	if strings.TrimSpace(secretCfg.Secrets.Cosign.PrivateKey) == "" {
		return PublicCosignConfig{}, nil, fmt.Errorf("decrypted secrets config %s: secrets.cosign.privateKey must not be empty", SecretsFileName)
	}
	derived, err := derivePublicCosignConfig(secretCfg.Secrets.Cosign.PrivateKey)
	if err != nil {
		return PublicCosignConfig{}, nil, err
	}
	if normalizeSignValue(publicCfg.PublicKey) != normalizeSignValue(derived.PublicKey) {
		return PublicCosignConfig{}, nil, fmt.Errorf("ci.sign.cosign.publicKey does not match the signing key material in secrets.cosign.privateKey")
	}
	return publicCfg, secretCfg, nil
}

func LoadPublicSignConfig(configPath string) (PublicSignConfig, error) {
	if loadPublicSignConfigHook != nil {
		return loadPublicSignConfigHook(configPath)
	}

	if info, err := os.Stat(configPath); err == nil && info.IsDir() {
		configPath = filepath.Join(configPath, ConfigFileName)
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return PublicSignConfig{}, fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := LoadConfig(filepath.Dir(absConfigPath))
	if err != nil {
		return PublicSignConfig{}, err
	}
	if strings.TrimSpace(cfg.CI.Sign.Helm.PublicKey) == "" {
		return PublicSignConfig{}, fmt.Errorf("config %s: ci.sign.helm.publicKey must not be empty", absConfigPath)
	}
	expectedFromPublicKey, err := expectedPublicSignConfigFromPublicKey(cfg.CI.Sign.Helm.PublicKey)
	if err != nil {
		return PublicSignConfig{}, err
	}
	if strings.TrimSpace(cfg.CI.Sign.Helm.Key) == "" {
		return PublicSignConfig{}, fmt.Errorf("config %s: ci.sign.helm.key must not be empty; expected %q from ci.sign.helm.publicKey", absConfigPath, expectedFromPublicKey.Key)
	}
	if strings.TrimSpace(cfg.CI.Sign.Helm.Name) == "" {
		return PublicSignConfig{}, fmt.Errorf("config %s: ci.sign.helm.name must not be empty; expected %q from ci.sign.helm.publicKey", absConfigPath, expectedFromPublicKey.Name)
	}
	if normalizeSignValue(cfg.CI.Sign.Helm.Key) != normalizeSignValue(expectedFromPublicKey.Key) {
		return PublicSignConfig{}, fmt.Errorf("config %s: ci.sign.helm.key=%q does not match ci.sign.helm.publicKey; expected %q", absConfigPath, cfg.CI.Sign.Helm.Key, expectedFromPublicKey.Key)
	}
	if normalizeSignValue(cfg.CI.Sign.Helm.Name) != normalizeSignValue(expectedFromPublicKey.Name) {
		return PublicSignConfig{}, fmt.Errorf("config %s: ci.sign.helm.name=%q does not match ci.sign.helm.publicKey; expected %q", absConfigPath, cfg.CI.Sign.Helm.Name, expectedFromPublicKey.Name)
	}
	return cfg.CI.Sign.Helm, nil
}

func LoadPublicCosignConfig(configPath string) (PublicCosignConfig, error) {
	if loadPublicCosignConfigHook != nil {
		return loadPublicCosignConfigHook(configPath)
	}

	if info, err := os.Stat(configPath); err == nil && info.IsDir() {
		configPath = filepath.Join(configPath, ConfigFileName)
	}
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return PublicCosignConfig{}, fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := LoadConfig(filepath.Dir(absConfigPath))
	if err != nil {
		return PublicCosignConfig{}, err
	}
	if strings.TrimSpace(cfg.CI.Sign.Cosign.PublicKey) == "" {
		return PublicCosignConfig{}, fmt.Errorf("config %s: ci.sign.cosign.publicKey must not be empty", absConfigPath)
	}
	return cfg.CI.Sign.Cosign, nil
}

func validateSignSecrets(sign SignSecrets, targetPath string) error {
	if strings.TrimSpace(sign.SecretKeyring) == "" {
		return nil
	}
	return nil
}

func validateCosignSecrets(sign CosignSecrets, targetPath string) error {
	if strings.TrimSpace(sign.PrivateKey) == "" {
		return nil
	}
	if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sign.PrivateKey)); err != nil {
		return fmt.Errorf("decrypted secrets config %s: decode secrets.cosign.privateKey: %w", targetPath, err)
	}
	return nil
}

func validatePublishSecrets(publish PublishSecrets, targetPath string) error {
	if strings.TrimSpace(publish.GitLabToken) == "" {
		return nil
	}
	return nil
}

func validateRegistryTokens(tokens []RegistryTokenRef, targetPath string) error {
	for i, token := range tokens {
		if strings.TrimSpace(token.Registry) == "" {
			return fmt.Errorf("decrypted secrets config %s: secrets.registryTokens[%d].registry must not be empty", targetPath, i)
		}
		if strings.TrimSpace(token.Username) == "" {
			return fmt.Errorf("decrypted secrets config %s: secrets.registryTokens[%d].username must not be empty", targetPath, i)
		}
		if strings.TrimSpace(token.Token) == "" {
			return fmt.Errorf("decrypted secrets config %s: secrets.registryTokens[%d].token must not be empty", targetPath, i)
		}
	}
	return nil
}

func derivePublicSignConfig(encodedSecretKeyring string) (PublicSignConfig, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedSecretKeyring))
	if err != nil {
		return PublicSignConfig{}, fmt.Errorf("decode base64 key material: %w", err)
	}
	return derivePublicSignConfigFromArmoredKey(raw, "read armored signing key material")
}

func derivePublicSignConfigFromArmoredKey(raw []byte, action string) (PublicSignConfig, error) {
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(raw))
	if err != nil {
		return PublicSignConfig{}, fmt.Errorf("%s: %w", action, err)
	}
	if len(entities) == 0 {
		return PublicSignConfig{}, fmt.Errorf("%s: no keys found", action)
	}
	entity := entities[0]
	name, err := primaryIdentityName(entity)
	if err != nil {
		return PublicSignConfig{}, err
	}
	publicKey, err := serializeEntityArmored(entity, openpgp.PublicKeyType, false, nil)
	if err != nil {
		return PublicSignConfig{}, fmt.Errorf("export public signing key: %w", err)
	}
	return PublicSignConfig{
		Name:      name,
		Key:       strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint[:])),
		PublicKey: publicKey.String(),
	}, nil
}

func expectedPublicSignConfigFromPublicKey(publicKey string) (PublicSignConfig, error) {
	if strings.TrimSpace(publicKey) == "" {
		return PublicSignConfig{}, fmt.Errorf("ci.sign.helm.publicKey must not be empty")
	}
	return derivePublicSignConfigFromArmoredKey([]byte(publicKey), "decode ci.sign.helm.publicKey")
}

func derivePublicCosignConfig(encodedPrivateKey string) (PublicCosignConfig, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedPrivateKey))
	if err != nil {
		return PublicCosignConfig{}, fmt.Errorf("decode base64 cosign key material: %w", err)
	}
	priv, err := cosign.LoadPrivateKey(raw, nil, nil)
	if err != nil {
		return PublicCosignConfig{}, fmt.Errorf("load cosign private key: %w", err)
	}
	pubPEM, err := cosignsigs.PublicKeyPem(priv)
	if err != nil {
		return PublicCosignConfig{}, fmt.Errorf("export cosign public key: %w", err)
	}
	return PublicCosignConfig{PublicKey: string(pubPEM)}, nil
}

func primaryIdentityName(entity *openpgp.Entity) (string, error) {
	if entity == nil {
		return "", fmt.Errorf("signing key material is missing")
	}
	if identity := entity.PrimaryIdentity(); identity != nil {
		return identity.Name, nil
	}
	if len(entity.Identities) == 0 {
		return "", fmt.Errorf("signing key material does not contain a user identity")
	}
	names := make([]string, 0, len(entity.Identities))
	for name := range entity.Identities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names[0], nil
}

func validateMatchingSignConfig(publicCfg, derivedCfg PublicSignConfig) error {
	if normalizeSignValue(publicCfg.Name) != normalizeSignValue(derivedCfg.Name) {
		return fmt.Errorf("ci.sign.helm.name does not match the signing key identity in secrets.sign.secretKeyring")
	}
	if normalizeSignValue(publicCfg.Key) != normalizeSignValue(derivedCfg.Key) {
		return fmt.Errorf("ci.sign.helm.key does not match the signing key fingerprint in secrets.sign.secretKeyring")
	}
	publicKeyCfg, err := derivePublicSignConfigFromArmoredKey([]byte(publicCfg.PublicKey), "decode ci.sign.helm.publicKey")
	if err != nil {
		return err
	}
	if normalizeSignValue(publicKeyCfg.Name) != normalizeSignValue(derivedCfg.Name) || normalizeSignValue(publicKeyCfg.Key) != normalizeSignValue(derivedCfg.Key) {
		return fmt.Errorf("ci.sign.helm.publicKey does not match the signing key material in secrets.sign.secretKeyring")
	}
	return nil
}

func mergeValidSignKeys(existing []ValidSignKey, newEntry ValidSignKey) []ValidSignKey {
	out := make([]ValidSignKey, 0, len(existing)+1)
	seen := false
	for _, entry := range existing {
		if normalizeSignValue(entry.Key) == normalizeSignValue(newEntry.Key) {
			out = append(out, ValidSignKey{
				Key:  strings.TrimSpace(newEntry.Key),
				Name: strings.TrimSpace(newEntry.Name),
			})
			seen = true
			continue
		}
		out = append(out, ValidSignKey{
			Key:  strings.TrimSpace(entry.Key),
			Name: strings.TrimSpace(entry.Name),
		})
	}
	if !seen {
		out = append(out, ValidSignKey{
			Key:  strings.TrimSpace(newEntry.Key),
			Name: strings.TrimSpace(newEntry.Name),
		})
	}
	return out
}

func normalizeSignValue(value string) string {
	return strings.TrimSpace(value)
}

func validateEncryptedSecretsDocument(data types.YamlString, targetPath string) error {
	var doc encryptedSopsDocument
	if err := yaml.Unmarshal([]byte(data), &doc); err != nil {
		return fmt.Errorf("parse encrypted secrets document for %s: %w", targetPath, err)
	}
	if strings.TrimSpace(doc.Sops.EncryptedRegex) != "" {
		return fmt.Errorf("encrypted SOPS document for %s uses encrypted_regex=%q; hydra ci secrets requires encrypting all fields", targetPath, doc.Sops.EncryptedRegex)
	}
	return nil
}

func findNearestSopsConfig(startDir string) (string, error) {
	absStartDir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve secrets directory path: %w", err)
	}

	for dir := absStartDir; ; {
		cfgPath := filepath.Join(dir, ".sops.yaml")
		info, err := os.Stat(cfgPath)
		if err == nil && !info.IsDir() {
			return cfgPath, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat sops config %s: %w", cfgPath, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf(
		"no .sops.yaml found starting from %s and walking up to the filesystem root\n\ncreate %s with at least:\ncreation_rules:\n  - path_regex: ^.*/.*\\.sops\\.yaml\n    age: >\n      <your ssh key here>",
		absStartDir,
		filepath.Join(absStartDir, ".sops.yaml"),
	)
}
