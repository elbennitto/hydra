package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

func makeClusterStatusEntity(appId types.AppId, key types.EntityKeyUnstructured, namespace, name, value string, trackedAppId types.AppId) entity.Entity {
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK("", "v1", "ConfigMap")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)))

	if key == types.KeyTemplateEntity {
		e = withAppIds(e, []types.AppId{appId})
		if trackedAppId == "" {
			trackedAppId = appId
		}
	}

	annotations := map[string]any{}
	if trackedAppId != "" {
		annotations["argocd.argoproj.io/tracking-id"] = string(trackedAppId) + ":/ConfigMap:" + namespace + "/" + name
	}

	e = withUnstructured(e, key, unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":        name,
				"namespace":   namespace,
				"annotations": annotations,
			},
			"data": map[string]any{
				"value": value,
			},
		},
	})

	return e
}

func TestComputeClusterAppStatuses_MarksOnlyDriftingAppsOutOfSync(t *testing.T) {
	rendered, err := entity.NewEntities([]entity.Entity{
		makeClusterStatusEntity("prod.apps.api", types.KeyTemplateEntity, "apps", "api-config", "desired-api", ""),
		makeClusterStatusEntity("prod.apps.worker", types.KeyTemplateEntity, "apps", "worker-config", "desired-worker", ""),
	})
	require.NoError(t, err)

	live, err := entity.NewEntities([]entity.Entity{
		makeClusterStatusEntity("prod.apps.api", types.KeyClusterEntity, "apps", "api-config", "desired-api", "prod.apps.api"),
		makeClusterStatusEntity("prod.apps.worker", types.KeyClusterEntity, "apps", "worker-config", "drifted-worker", "prod.apps.worker"),
	})
	require.NoError(t, err)

	statuses, err := ComputeClusterAppStatuses(rendered, live, sets.New[types.AppId](
		types.AppId("prod.apps.api"),
		types.AppId("prod.apps.worker"),
	))
	require.NoError(t, err)

	assert.Equal(t, []AppSyncStatus{
		{AppId: types.AppId("prod.apps.api"), InSync: true},
		{AppId: types.AppId("prod.apps.worker"), InSync: false},
	}, statuses)
}

func TestComputeClusterAppStatuses_IgnoresUntrackedAndOtherAppTrackedLiveResources(t *testing.T) {
	rendered, err := entity.NewEntities([]entity.Entity{
		makeClusterStatusEntity("prod.apps.api", types.KeyTemplateEntity, "apps", "api-config", "desired-api", ""),
	})
	require.NoError(t, err)

	live, err := entity.NewEntities([]entity.Entity{
		makeClusterStatusEntity("prod.apps.api", types.KeyClusterEntity, "apps", "api-config", "desired-api", "prod.apps.api"),
		makeClusterStatusEntity("prod.apps.api", types.KeyClusterEntity, "apps", "foreign-config", "foreign", "prod.apps.other"),
		makeClusterStatusEntity("prod.apps.api", types.KeyClusterEntity, "apps", "untracked-config", "ignored", ""),
	})
	require.NoError(t, err)

	statuses, err := ComputeClusterAppStatuses(rendered, live, sets.New[types.AppId](
		types.AppId("prod.apps.api"),
	))
	require.NoError(t, err)

	assert.Equal(t, []AppSyncStatus{
		{AppId: types.AppId("prod.apps.api"), InSync: true},
	}, statuses)
}

func TestComputeClusterAppStatuses_TreatsMissingOrExtraTrackedResourcesAsOutOfSync(t *testing.T) {
	rendered, err := entity.NewEntities([]entity.Entity{
		makeClusterStatusEntity("prod.apps.api", types.KeyTemplateEntity, "apps", "api-config", "desired-api", ""),
	})
	require.NoError(t, err)

	live, err := entity.NewEntities([]entity.Entity{
		makeClusterStatusEntity("prod.apps.api", types.KeyClusterEntity, "apps", "orphan-config", "orphan", "prod.apps.api"),
	})
	require.NoError(t, err)

	statuses, err := ComputeClusterAppStatuses(rendered, live, sets.New[types.AppId](
		types.AppId("prod.apps.api"),
	))
	require.NoError(t, err)

	assert.Equal(t, []AppSyncStatus{
		{AppId: types.AppId("prod.apps.api"), InSync: false},
	}, statuses)
}
