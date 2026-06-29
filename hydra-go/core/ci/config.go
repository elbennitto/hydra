package ci

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const ConfigFileName = ".hydra-ci.yaml"

type Config struct {
	CI CIConfig `yaml:"ci"`
}

type CIConfig struct {
	RootAppsPath   string     `yaml:"rootAppsPath"`
	Environments   []string   `yaml:"environments"`
	AppGroups      []AppGroup `yaml:"appGroups"`
	Registry       string     `yaml:"registry"`
	UpstreamBranch string     `yaml:"upstreamBranch,omitempty"`
	SecretsPath    string     `yaml:"secretsPath,omitempty"`
	Sign           SignConfig `yaml:"sign,omitempty"`
	Promote        Promote    `yaml:"promote"`
	Teams          Teams      `yaml:"teams"`
	// AutoSteps overrides the default stage order for `hydra ci run auto`.
	// Omitted means the default pipeline. If present, it must be non-empty
	// and every entry must be a known pipeline step name.
	AutoSteps []string `yaml:"autoSteps,omitempty"`
}

type AppGroup struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

type Promote struct {
	PromotableRootApps []string `yaml:"promotableRootApps"`
}

type Teams struct {
	WebhookURL string            `yaml:"webhookUrl"`
	Channels   map[string]string `yaml:"channels"`
}

type ValidSignKey struct {
	Key  string `yaml:"key,omitempty"`
	Name string `yaml:"name,omitempty"`
}

type SignConfig struct {
	Helm   PublicSignConfig   `yaml:"helm,omitempty"`
	Cosign PublicCosignConfig `yaml:"cosign,omitempty"`
}

type PublicSignConfig struct {
	Name      string         `yaml:"name,omitempty"`
	Key       string         `yaml:"key,omitempty"`
	PublicKey string         `yaml:"publicKey,omitempty"`
	ValidKeys []ValidSignKey `yaml:"validKeys,omitempty"`
}

type PublicCosignConfig struct {
	PublicKey string   `yaml:"publicKey,omitempty"`
	ValidKeys []string `yaml:"validKeys,omitempty"`
}

func LoadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, ConfigFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", ConfigFileName, err)
	}
	return ParseConfig(data)
}

func ParseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ConfigFileName, err)
	}
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validateConfig(cfg *Config) error {
	if cfg.CI.RootAppsPath == "" {
		return fmt.Errorf("%s: ci.rootAppsPath must not be empty", ConfigFileName)
	}
	if len(cfg.CI.Environments) == 0 {
		return fmt.Errorf("%s: ci.environments must not be empty", ConfigFileName)
	}
	if cfg.CI.UpstreamBranch == "" {
		cfg.CI.UpstreamBranch = "origin/HEAD"
	}
	for i, entry := range cfg.CI.Sign.Helm.ValidKeys {
		if entry.Key == "" {
			return fmt.Errorf("%s: ci.sign.helm.validKeys[%d].key must not be empty", ConfigFileName, i)
		}
		if entry.Name == "" {
			return fmt.Errorf("%s: ci.sign.helm.validKeys[%d].name must not be empty", ConfigFileName, i)
		}
	}
	for i, entry := range cfg.CI.Sign.Cosign.ValidKeys {
		if entry == "" {
			return fmt.Errorf("%s: ci.sign.cosign.validKeys[%d] must not be empty", ConfigFileName, i)
		}
	}
	if err := validateAutoStepsField(cfg); err != nil {
		return err
	}
	return nil
}

var defaultAutoPipeline = []string{
	"download", "test", "release", "publish", "promote", "sync", "update", "sprint", "upgrade",
}

var validAutoPipelineSteps = map[string]struct{}{
	"download": {}, "test": {}, "release": {}, "publish": {}, "promote": {},
	"sync": {}, "update": {}, "sprint": {}, "upgrade": {},
}

// DefaultAutoPipeline returns a copy of the built-in stage order for hydra ci run auto.
func DefaultAutoPipeline() []string {
	out := make([]string, len(defaultAutoPipeline))
	copy(out, defaultAutoPipeline)
	return out
}

func validateAutoStepsField(cfg *Config) error {
	if cfg.CI.AutoSteps == nil {
		return nil
	}
	if len(cfg.CI.AutoSteps) == 0 {
		return fmt.Errorf("%s: ci.autoSteps must contain at least one step when set", ConfigFileName)
	}
	for _, s := range cfg.CI.AutoSteps {
		if _, ok := validAutoPipelineSteps[s]; !ok {
			return fmt.Errorf("%s: unknown ci.autoSteps entry %q", ConfigFileName, s)
		}
	}
	return nil
}

// ResolveAutoSteps returns the pipeline stage names for hydra ci run auto.
// A nil autoSteps in config selects DefaultAutoPipeline.
func ResolveAutoSteps(cfg *Config) ([]string, error) {
	if err := validateAutoStepsField(cfg); err != nil {
		return nil, err
	}
	if cfg.CI.AutoSteps == nil {
		return DefaultAutoPipeline(), nil
	}
	out := make([]string, len(cfg.CI.AutoSteps))
	copy(out, cfg.CI.AutoSteps)
	return out, nil
}

// IsRootAppPromotable returns true if the given root app name is listed
// in the promotableRootApps configuration. An empty list means no root
// apps are promotable.
func (c *Config) IsRootAppPromotable(rootApp string) bool {
	for _, name := range c.CI.Promote.PromotableRootApps {
		if name == rootApp {
			return true
		}
	}
	return false
}

// PromotionPath returns the (source, target) environment pair for the
// given environment. Returns an error if the environment is the last
// in the chain (cannot be promoted further).
func (c *Config) PromotionPath(env string) (source, target string, err error) {
	for i, e := range c.CI.Environments {
		if e == env && i+1 < len(c.CI.Environments) {
			return e, c.CI.Environments[i+1], nil
		}
	}
	return "", "", fmt.Errorf("no promotion target for environment %q", env)
}
