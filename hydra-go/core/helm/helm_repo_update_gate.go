package helm

import "sync"

// helmRepoUpdateGate ensures helm's dependency downloader runs
// "helm repo update" at most once per process. Without this, each chart
// with missing dependencies triggers a full repo refresh (and the
// "Hang tight while we grab the latest..." message).
type helmRepoUpdateGate struct {
	mu   sync.Mutex
	done bool
}

func (g *helmRepoUpdateGate) shouldSkipRepoUpdate() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.done
}

func (g *helmRepoUpdateGate) markRepoUpdated() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.done = true
}
