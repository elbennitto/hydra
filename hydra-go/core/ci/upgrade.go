package ci

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/git"
)

var errUpgradeMissingDependency = errors.New("upgrade dependency missing")

type UpgradeVersionsFile struct {
	Versions []UpgradeVersionEntry `yaml:"versions"`
}

type UpgradeVersionEntry struct {
	RootApp string `yaml:"rootApp"`
	App     string `yaml:"app"`
	Env     string `yaml:"env"`
	Version string `yaml:"version"`
}

func RunUpgrade(configPath string, mode Mode, versionsFile string, skipMissing bool) error {
	return runUpgrade(configPath, mode, versionsFile, skipMissing, os.Stdin)
}

func runUpgrade(configPath string, mode Mode, versionsFile string, skipMissing bool, stdin io.Reader) error {
	dir := filepath.Dir(configPath)
	cfg, err := LoadConfig(dir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	entries, err := LoadUpgradeVersions(versionsFile, stdin)
	if err != nil {
		return err
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

	for _, entry := range entries {
		if !envAllowed(entry.Env, cfg.CI.Environments) {
			return fmt.Errorf("ci upgrade: env %q for %s/%s is not configured", entry.Env, entry.RootApp, entry.App)
		}

		rel := filepath.ToSlash(filepath.Join(cfg.CI.RootAppsPath, entry.RootApp, entry.App, entry.Env))
		chartPath := filepath.Join(repo.Path(), rel, "Chart.yaml")
		oldVersion, changed, err := updateChartDependencyVersion(chartPath, entry.App, entry.Env, entry.Version, mode == ModeDryRun)
		if err != nil {
			if skipMissing && isUpgradeMissingError(err) {
				log.Default().Warn(logIdCI, "upgrade: skipped missing dependency {path} {app}: {reason}",
					log.String("path", rel), log.String("app", entry.App), log.String("reason", err.Error()))
				continue
			}
			return fmt.Errorf("ci upgrade: %s: %w", rel, err)
		}

		l := log.Default()
		if mode == ModeDryRun {
			l.Info(logIdCI, "upgrade dry-run: {path} dependency {app} {old} → {new}",
				log.String("path", rel), log.String("app", entry.App), log.String("old", oldVersion), log.String("new", entry.Version))
		} else if changed {
			l.Info(logIdCI, "upgrade: updated {path} dependency {app} {old} → {new}",
				log.String("path", rel), log.String("app", entry.App), log.String("old", oldVersion), log.String("new", entry.Version))
		} else {
			l.Info(logIdCI, "upgrade: {path} dependency {app} already at {version}",
				log.String("path", rel), log.String("app", entry.App), log.String("version", entry.Version))
		}
	}

	return nil
}

func isUpgradeMissingError(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, errUpgradeMissingDependency)
}

func LoadUpgradeVersions(path string, stdin io.Reader) ([]UpgradeVersionEntry, error) {
	if path != "" {
		return LoadUpgradeVersionsFile(path)
	}
	if stdin == nil {
		return nil, fmt.Errorf("read versions from stdin: no stdin reader configured")
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("read versions from stdin: %w", err)
	}
	return parseUpgradeVersions(data, "stdin")
}

func LoadUpgradeVersionsFile(path string) ([]UpgradeVersionEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read versions file %s: %w", path, err)
	}
	return parseUpgradeVersions(data, path)
}

func parseUpgradeVersions(data []byte, source string) ([]UpgradeVersionEntry, error) {
	var doc UpgradeVersionsFile
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse versions %s: %w", source, err)
	}
	if len(doc.Versions) == 0 {
		return nil, fmt.Errorf("versions %s: versions must contain at least one entry", source)
	}

	seen := make(map[string]struct{})
	for i, entry := range doc.Versions {
		if entry.RootApp == "" {
			return nil, fmt.Errorf("versions %s: versions[%d].rootApp must not be empty", source, i)
		}
		if entry.App == "" {
			return nil, fmt.Errorf("versions %s: versions[%d].app must not be empty", source, i)
		}
		if entry.Env == "" {
			return nil, fmt.Errorf("versions %s: versions[%d].env must not be empty", source, i)
		}
		if entry.Version == "" {
			return nil, fmt.Errorf("versions %s: versions[%d].version must not be empty", source, i)
		}
		key := entry.RootApp + "/" + entry.App + "/" + entry.Env
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("versions %s: duplicate versions entry for %s", source, key)
		}
		seen[key] = struct{}{}
	}

	return doc.Versions, nil
}

func updateChartDependencyVersion(chartPath string, depName string, env string, newVersion string, dryRun bool) (string, bool, error) {
	raw, err := os.ReadFile(chartPath)
	if err != nil {
		return "", false, fmt.Errorf("read Chart.yaml: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return "", false, fmt.Errorf("parse Chart.yaml: %w", err)
	}
	chartVersionNode, err := findChartVersionNode(&doc)
	if err != nil {
		return "", false, err
	}
	chartVersion, err := NextChildChartWrapperVersion(newVersion, env, chartVersionNode.Value)
	if err != nil {
		return "", false, err
	}
	versionNode, err := findDependencyVersionNode(&doc, depName)
	if err != nil {
		return "", false, err
	}

	oldVersion := versionNode.Value
	if oldVersion == newVersion && chartVersionNode.Value == chartVersion {
		return oldVersion, false, nil
	}
	if dryRun {
		return oldVersion, true, nil
	}

	chartVersionNode.Value = chartVersion
	chartVersionNode.Tag = "!!str"
	versionNode.Value = newVersion
	versionNode.Tag = "!!str"
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", false, fmt.Errorf("marshal Chart.yaml: %w", err)
	}
	if !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	if err := os.WriteFile(chartPath, out, 0o644); err != nil {
		return "", false, fmt.Errorf("write Chart.yaml: %w", err)
	}
	return oldVersion, true, nil
}

func findChartVersionNode(doc *yaml.Node) (*yaml.Node, error) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("unexpected Chart.yaml structure")
	}
	mapping := doc.Content[0]
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == "version" {
			return mapping.Content[i+1], nil
		}
	}
	return nil, fmt.Errorf("chart.yaml version must not be empty")
}

func findDependencyVersionNode(doc *yaml.Node, depName string) (*yaml.Node, error) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("unexpected Chart.yaml structure")
	}
	mapping := doc.Content[0]
	var deps *yaml.Node
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == "dependencies" {
			deps = mapping.Content[i+1]
			break
		}
	}
	if deps == nil {
		return nil, fmt.Errorf("%w: dependencies must contain an entry named %q", errUpgradeMissingDependency, depName)
	}
	if deps.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("dependencies must contain an entry named %q", depName)
	}

	for _, dep := range deps.Content {
		if dep.Kind != yaml.MappingNode {
			continue
		}
		var nameNode, versionNode *yaml.Node
		for i := 0; i < len(dep.Content)-1; i += 2 {
			switch dep.Content[i].Value {
			case "name":
				nameNode = dep.Content[i+1]
			case "version":
				versionNode = dep.Content[i+1]
			}
		}
		if nameNode != nil && nameNode.Value == depName {
			if versionNode == nil {
				return nil, fmt.Errorf("dependency %q has no version", depName)
			}
			return versionNode, nil
		}
	}
	return nil, fmt.Errorf("%w: dependencies must contain an entry named %q", errUpgradeMissingDependency, depName)
}
