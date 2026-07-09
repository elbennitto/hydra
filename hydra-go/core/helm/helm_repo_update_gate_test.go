package helm

import "testing"

func TestHelmRepoUpdateGate(t *testing.T) {
	t.Parallel()
	var g helmRepoUpdateGate
	repoA := []string{"https://argoproj.github.io/argo-helm"}
	repoB := []string{"https://raw.githubusercontent.com/kubernetes-csi/csi-driver-nfs/master/charts"}

	if g.shouldSkipRepoUpdate(repoA) {
		t.Fatal("first repository should run helm repo update (SkipUpdate=false)")
	}
	g.markRepoUpdated(repoA)
	if !g.shouldSkipRepoUpdate(repoA) {
		t.Fatal("already updated repository should skip update (SkipUpdate=true)")
	}
	if g.shouldSkipRepoUpdate(repoB) {
		t.Fatal("new repository should still run helm repo update (SkipUpdate=false)")
	}
	g.markRepoUpdated(repoB)
	if !g.shouldSkipRepoUpdate(repoB) {
		t.Fatal("after marking repo updated, the same repo should skip update")
	}
}

func TestNormalizedDependencyRepositories(t *testing.T) {
	t.Parallel()
	repos := normalizedDependencyRepositories([]string{"", " file://./charts/lib", " https://example.com/charts ", "https://example.com/charts", "@corp-repo"})
	if len(repos) != 2 {
		t.Fatalf("expected 2 normalized repositories, got %d", len(repos))
	}
	if repos[0] != "https://example.com/charts" {
		t.Fatalf("unexpected first repository: %q", repos[0])
	}
	if repos[1] != "@corp-repo" {
		t.Fatalf("unexpected second repository: %q", repos[1])
	}
}
