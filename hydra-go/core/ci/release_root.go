package ci

import (
	"fmt"
	"os"
	"path/filepath"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/git"
)

type groupEnvKey struct {
	group string
	env   string
}

type rootChartUpdate struct {
	relPath    string
	chart      *git.Chart
	oldVersion string
}

// buildRootChartUpdates loads each affected root chart, applies child version
// pins from the plan, and bumps the root chart version once per group/env.
func buildRootChartUpdates(repo *git.Repo, cfg *Config, plan []ReleaseChildEntry) (map[groupEnvKey]rootChartUpdate, error) {
	l := log.Default()
	byKey := make(map[groupEnvKey][]ReleaseChildEntry)
	for _, e := range plan {
		k := groupEnvKey{group: e.Group, env: e.Env}
		byKey[k] = append(byKey[k], e)
	}

	out := make(map[groupEnvKey]rootChartUpdate)
	for k, ents := range byKey {
		rel := filepath.ToSlash(filepath.Join(cfg.CI.RootAppsPath, k.group, "root", k.env))
		chartYAML := filepath.Join(repo.Path(), rel, "Chart.yaml")
		if _, err := os.Stat(chartYAML); err != nil {
			if os.IsNotExist(err) {
				l.Info(logIdCI, "release: skip root app chart (no Chart.yaml) {path}",
					log.String("path", rel))
				continue
			}
			return nil, fmt.Errorf("stat root chart %s: %w", rel, err)
		}
		rc, err := repo.LoadChart(rel)
		if err != nil {
			return nil, fmt.Errorf("load root chart %s: %w", rel, err)
		}
		oldRoot := rc.GetVersion()
		for _, e := range ents {
			rc.SetValue("apps."+e.App+".version", e.NewVersion)
		}
		newRoot, err := NextRootAppChartVersionAfterChildChanges(oldRoot, ents)
		if err != nil {
			return nil, fmt.Errorf("bump root version %s: %w", rel, err)
		}
		newRoot, err = NormalizeChartVersionEnv(newRoot, k.env)
		if err != nil {
			return nil, fmt.Errorf("normalize root version %s for env %s: %w", rel, k.env, err)
		}
		rc.Version(newRoot)
		out[k] = rootChartUpdate{relPath: rel, chart: rc, oldVersion: oldRoot}
	}
	return out, nil
}
