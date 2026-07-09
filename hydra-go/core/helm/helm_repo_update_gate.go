package helm

import (
	"strings"
	"sync"
)

// helmRepoUpdateGate ensures helm's dependency downloader runs
// "helm repo update" at most once per dependency repository in a process.
// Without this, each chart with missing dependencies can trigger a full
// repo refresh (and the "Hang tight while we grab the latest..." message).
type helmRepoUpdateGate struct {
	mu      sync.Mutex
	updated map[string]struct{}
}

var defaultHelmRepoUpdateGate helmRepoUpdateGate

func (g *helmRepoUpdateGate) shouldSkipRepoUpdate(repositories []string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, repo := range normalizedDependencyRepositories(repositories) {
		if _, ok := g.updated[repo]; !ok {
			return false
		}
	}
	return true
}

func (g *helmRepoUpdateGate) markRepoUpdated(repositories []string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.updated == nil {
		g.updated = make(map[string]struct{})
	}
	for _, repo := range normalizedDependencyRepositories(repositories) {
		g.updated[repo] = struct{}{}
	}
}

func normalizedDependencyRepositories(repositories []string) []string {
	if len(repositories) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(repositories))
	normalized := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		repo := strings.TrimSpace(repository)
		if repo == "" || strings.HasPrefix(repo, "file://") {
			continue
		}
		if _, ok := seen[repo]; ok {
			continue
		}
		seen[repo] = struct{}{}
		normalized = append(normalized, repo)
	}
	return normalized
}
