package commands

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestEnrichEntityPaths_joinsRootAppDirectory(t *testing.T) {
	clusterPath := filepath.Join(t.TempDir(), "clusters", "test", "demo", "in-cluster")

	rootApp := types.NewRootAppId(types.InCluster, types.RootAppName("argocd"))
	rootE := mustBuild(entity.NewEntityBuilder().
		WithApiVersion(types.NewApiVersion(types.Group(""), types.Version("v1"))).
		WithKind(types.Kind("SopsSecret")).
		WithResource(types.Resource("sopssecrets")).
		WithNamespaced(true).
		WithNamespace(types.Namespace("argocd")).
		WithName(types.Name("argocd-server-tls-backup")).
		WithAppIds([]types.AppId{rootApp}).
		WithTemplatePath(types.TemplatePath("backup-argocd-argocd-server-tls.sops.yaml")))

	out, err := enrichEntityPaths(rootE, clusterPath, "")
	require.NoError(t, err)
	abs, err := out.AbsPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(clusterPath, "argocd", "backup-argocd-argocd-server-tls.sops.yaml"), string(abs))

	childApp := types.NewChildAppId(types.InCluster, types.RootAppName("cluster-infra"), types.ChildAppName("dex"))
	childE := mustBuild(entity.NewEntityBuilder().
		WithApiVersion(types.NewApiVersion(types.Group(""), types.Version("v1"))).
		WithKind(types.Kind("SopsSecret")).
		WithResource(types.Resource("sopssecrets")).
		WithNamespaced(true).
		WithNamespace(types.Namespace("dex")).
		WithName(types.Name("dex-tls-backup")).
		WithAppIds([]types.AppId{childApp}).
		WithTemplatePath(types.TemplatePath("apps/dex/backup-dex-dex-tls.sops.yaml")))

	out2, err := enrichEntityPaths(childE, clusterPath, "")
	require.NoError(t, err)
	abs2, err := out2.AbsPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(clusterPath, "cluster-infra", "apps", "dex", "backup-dex-dex-tls.sops.yaml"), string(abs2))
}

func TestEnrichEntityPaths_stripsRedundantRootAppPrefixFromTemplatePath(t *testing.T) {
	clusterPath := filepath.Join(t.TempDir(), "clusters", "test", "demo", "in-cluster")

	rootApp := types.NewRootAppId(types.InCluster, types.RootAppName("argocd"))
	e := mustBuild(entity.NewEntityBuilder().
		WithApiVersion(types.NewApiVersion(types.Group(""), types.Version("v1"))).
		WithKind(types.Kind("SopsSecret")).
		WithResource(types.Resource("sopssecrets")).
		WithNamespaced(true).
		WithNamespace(types.Namespace("argocd")).
		WithName(types.Name("github-gitops-private-key")).
		WithAppIds([]types.AppId{rootApp}).
		WithTemplatePath(types.TemplatePath("argocd/templates/github-gitops-private-key.sops.yaml")))

	out, err := enrichEntityPaths(e, clusterPath, "")
	require.NoError(t, err)
	abs, err := out.AbsPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(clusterPath, "argocd", "templates", "github-gitops-private-key.sops.yaml"), string(abs))
}
