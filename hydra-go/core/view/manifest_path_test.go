package view

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestComputeManifestPath_WithGroup(t *testing.T) {
	id := types.Id("apps/v1/Deployment/default/demo")
	appIds := []types.AppId{"dev.myapp"}

	result := ComputeManifestPath(id, appIds)
	assert.Equal(t, "dev.myapp/apps/v1/Deployment/default/demo.yaml", result)
}

func TestComputeManifestPath_CoreAPI(t *testing.T) {
	id := types.Id("v1/ServiceAccount/default/my-sa")
	appIds := []types.AppId{"dev.myapp"}

	result := ComputeManifestPath(id, appIds)
	assert.Equal(t, "dev.myapp/(core)/v1/ServiceAccount/default/my-sa.yaml", result)
}

func TestComputeManifestPath_ClusterScoped(t *testing.T) {
	id := types.Id("apiextensions.k8s.io/v1/CustomResourceDefinition//certs.cert-manager.io")
	appIds := []types.AppId{"dev.cert-manager"}

	result := ComputeManifestPath(id, appIds)
	assert.Equal(t, "dev.cert-manager/apiextensions.k8s.io/v1/CustomResourceDefinition/(cluster)/certs.cert-manager.io.yaml", result)
}

func TestComputeManifestPath_CoreClusterScoped(t *testing.T) {
	id := types.Id("v1/Namespace//kube-system")
	appIds := []types.AppId{"dev.cluster-infra"}

	result := ComputeManifestPath(id, appIds)
	assert.Equal(t, "dev.cluster-infra/(core)/v1/Namespace/(cluster)/kube-system.yaml", result)
}

func TestComputeManifestPath_NoAppIds(t *testing.T) {
	id := types.Id("v1/Secret/default/external")
	appIds := []types.AppId{}

	result := ComputeManifestPath(id, appIds)
	assert.Equal(t, "", result)
}

func TestComputeManifestPath_MultipleAppIds_UsesFirst(t *testing.T) {
	id := types.Id("apps/v1/Deployment/default/demo")
	appIds := []types.AppId{"dev.first", "dev.second"}

	result := ComputeManifestPath(id, appIds)
	assert.Equal(t, "dev.first/apps/v1/Deployment/default/demo.yaml", result)
}

func TestComputeManifestPath_NestedGroup(t *testing.T) {
	id := types.Id("rbac.authorization.k8s.io/v1/ClusterRole//admin")
	appIds := []types.AppId{"dev.myapp"}

	result := ComputeManifestPath(id, appIds)
	assert.Equal(t, "dev.myapp/rbac.authorization.k8s.io/v1/ClusterRole/(cluster)/admin.yaml", result)
}
