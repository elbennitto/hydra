package git

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Chart builds or modifies a Helm chart (Chart.yaml + values.yaml).
type Chart struct {
	name    string
	version string
	typ     string
	deps    []dep
	values  *string

	repoRoot     string
	relDir       string
	rawChartYAML []byte // original Chart.yaml bytes from disk (nil for programmatic charts)
}

type dep struct {
	Name    string
	Version string
	Repo    string
}

// NewChart creates a new Chart builder with the given name.
func NewChart(name string) *Chart {
	return &Chart{name: name, typ: "application"}
}

// Version sets the chart version. Chainable.
func (c *Chart) Version(v string) *Chart {
	c.version = v
	return c
}

// Type sets the chart type. Chainable.
func (c *Chart) Type(t string) *Chart {
	c.typ = t
	return c
}

// Dep adds a dependency. Chainable.
func (c *Chart) Dep(name, version, repo string) *Chart {
	c.deps = append(c.deps, dep{Name: name, Version: version, Repo: repo})
	return c
}

// Values sets the raw values.yaml content (full replacement). Chainable.
func (c *Chart) Values(raw string) *Chart {
	c.values = &raw
	return c
}

// SetValue sets a single value by dot-path (e.g. "apps.service-ui.version").
// Preserves existing values. Chainable.
func (c *Chart) SetValue(path, value string) *Chart {
	var data map[string]any
	if c.values != nil {
		_ = yaml.Unmarshal([]byte(*c.values), &data)
	}
	if data == nil {
		data = make(map[string]any)
	}
	setNestedValue(data, strings.Split(path, "."), value)
	out, err := yaml.Marshal(data)
	if err == nil {
		s := string(out)
		c.values = &s
	}
	return c
}

// GetName returns the chart name.
func (c *Chart) GetName() string {
	return c.name
}

// GetVersion returns the chart version.
func (c *Chart) GetVersion() string {
	return c.version
}

// GetDepVersion returns the version of the first dependency with the given name.
// Returns "" if not found.
func (c *Chart) GetDepVersion(name string) string {
	for _, d := range c.deps {
		if d.Name == name {
			return d.Version
		}
	}
	return ""
}

// PrimaryDependencyVersion returns the dependency version used to derive the
// wrapper chart version: the dependency whose name matches the chart name, or
// the first dependency if there is no name match. Dependencies with version "*"
// (Helm wildcard for file:// and similar) are ignored so the caller can fall
// back to the chart's own version. Returns "" when there are no usable
// dependencies.
func (c *Chart) PrimaryDependencyVersion() string {
	for _, d := range c.deps {
		if d.Version == "*" {
			continue
		}
		if d.Name == c.name {
			return d.Version
		}
	}
	for _, d := range c.deps {
		if d.Version == "*" {
			continue
		}
		return d.Version
	}
	return ""
}

// GetValue reads a value by dot-path from values.yaml content.
func (c *Chart) GetValue(path string) (string, error) {
	if c.values == nil {
		return "", fmt.Errorf("no values set")
	}
	var data map[string]any
	if err := yaml.Unmarshal([]byte(*c.values), &data); err != nil {
		return "", fmt.Errorf("parse values: %w", err)
	}
	v, err := getNestedValue(data, strings.Split(path, "."))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", v), nil
}

// Save writes Chart.yaml and values.yaml back to the directory the chart
// was loaded from. Returns an error if the chart was created with NewChart
// and has no associated directory.
func (c *Chart) Save() error {
	if c.repoRoot == "" || c.relDir == "" {
		return fmt.Errorf("chart has no associated directory; use SaveTo instead")
	}
	return c.writeTo(filepath.Join(c.repoRoot, c.relDir))
}

// SaveTo writes Chart.yaml and values.yaml to the given directory relative
// to the repo root.
func (c *Chart) SaveTo(dir string) error {
	if c.repoRoot == "" {
		return fmt.Errorf("chart has no associated repo; use LoadChart from a Repo")
	}
	return c.writeTo(filepath.Join(c.repoRoot, dir))
}

func (c *Chart) writeTo(absDir string) error {
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", absDir, err)
	}

	chartYAML := c.renderChartYAML()
	if err := os.WriteFile(filepath.Join(absDir, "Chart.yaml"), []byte(chartYAML), 0o644); err != nil {
		return fmt.Errorf("write Chart.yaml: %w", err)
	}

	if c.values != nil {
		if err := os.WriteFile(filepath.Join(absDir, "values.yaml"), []byte(*c.values), 0o644); err != nil {
			return fmt.Errorf("write values.yaml: %w", err)
		}
	}
	return nil
}

func (c *Chart) marshalChartYAML() string {
	typ := c.typ
	if typ == "" {
		typ = "application"
	}

	doc := chartDoc{
		APIVersion: "v2",
		Name:       c.name,
		Type:       typ,
		Version:    c.version,
	}
	for _, d := range c.deps {
		doc.Dependencies = append(doc.Dependencies, chartDepDoc{
			Name:       d.Name,
			Version:    d.Version,
			Repository: d.Repo,
		})
	}

	out, _ := yaml.Marshal(doc)
	return string(out)
}

// RenderWithVersion returns the Chart.yaml content with the given version
// applied, without mutating the chart. Preserves formatting when raw bytes
// are available, otherwise falls back to a full marshal.
func (c *Chart) RenderWithVersion(newVersion string) string {
	if c.rawChartYAML != nil {
		out, err := rewriteChartYAMLVersion(c.rawChartYAML, newVersion)
		if err == nil {
			return out
		}
	}
	old := c.version
	c.version = newVersion
	result := c.marshalChartYAML()
	c.version = old
	return result
}

// renderChartYAML returns the Chart.yaml content. If raw bytes from disk are
// available, it rewrites only the version field (preserving comments, blank
// lines, field order, and unknown fields). Otherwise it falls back to a full
// marshal from struct fields.
func (c *Chart) renderChartYAML() string {
	if c.rawChartYAML != nil {
		out, err := rewriteChartYAMLVersion(c.rawChartYAML, c.version)
		if err == nil {
			return out
		}
	}
	return c.marshalChartYAML()
}

// rewriteChartYAMLVersion uses yaml.Node to locate the top-level "version"
// field, then performs a surgical byte-level replacement of just the version
// value on that line. This preserves comments, blank lines, field order, and
// any fields unknown to the chartDoc struct.
func rewriteChartYAMLVersion(raw []byte, newVersion string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("parse Chart.yaml node: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return "", fmt.Errorf("unexpected Chart.yaml structure")
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return "", fmt.Errorf("chart.yaml root is not a mapping")
	}

	var keyNode, valueNode *yaml.Node
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == "version" {
			keyNode = mapping.Content[i]
			valueNode = mapping.Content[i+1]
			break
		}
	}
	if keyNode == nil {
		return "", fmt.Errorf("no 'version' key found in Chart.yaml")
	}

	oldVersion := valueNode.Value
	lines := bytes.SplitAfter(raw, []byte("\n"))
	lineIdx := keyNode.Line - 1
	if lineIdx >= len(lines) {
		return "", fmt.Errorf("version line %d out of range", keyNode.Line)
	}

	lines[lineIdx] = bytes.Replace(lines[lineIdx], []byte(oldVersion), []byte(newVersion), 1)
	return string(bytes.Join(lines, nil)), nil
}

type chartDoc struct {
	APIVersion   string        `yaml:"apiVersion"`
	Name         string        `yaml:"name"`
	Type         string        `yaml:"type"`
	Version      string        `yaml:"version"`
	Dependencies []chartDepDoc `yaml:"dependencies,omitempty"`
}

type chartDepDoc struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Repository string `yaml:"repository"`
}

func loadChartFromDir(absDir, repoRoot, relDir string) (*Chart, error) {
	chartPath := filepath.Join(absDir, "Chart.yaml")
	data, err := os.ReadFile(chartPath)
	if err != nil {
		return nil, fmt.Errorf("read Chart.yaml: %w", err)
	}

	var doc chartDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse Chart.yaml: %w", err)
	}

	c := &Chart{
		name:         doc.Name,
		version:      doc.Version,
		typ:          doc.Type,
		repoRoot:     repoRoot,
		relDir:       relDir,
		rawChartYAML: data,
	}

	for _, d := range doc.Dependencies {
		c.deps = append(c.deps, dep{
			Name:    d.Name,
			Version: d.Version,
			Repo:    d.Repository,
		})
	}
	valuesPath := filepath.Join(absDir, "values.yaml")
	if valData, err := os.ReadFile(valuesPath); err == nil {
		s := string(valData)
		c.values = &s
	}

	return c, nil
}

// RewriteChartYAMLVersion rewrites only the top-level version field in a raw
// Chart.yaml document while preserving formatting.
func RewriteChartYAMLVersion(raw []byte, newVersion string) (string, error) {
	return rewriteChartYAMLVersion(raw, newVersion)
}

func setNestedValue(m map[string]any, keys []string, value string) {
	if len(keys) == 1 {
		m[keys[0]] = value
		return
	}
	child, ok := m[keys[0]].(map[string]any)
	if !ok {
		child = make(map[string]any)
		m[keys[0]] = child
	}
	setNestedValue(child, keys[1:], value)
}

func getNestedValue(m map[string]any, keys []string) (any, error) {
	if len(keys) == 1 {
		v, ok := m[keys[0]]
		if !ok {
			return nil, fmt.Errorf("key not found: %s", keys[0])
		}
		return v, nil
	}
	child, ok := m[keys[0]].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("key not found: %s", keys[0])
	}
	return getNestedValue(child, keys[1:])
}
