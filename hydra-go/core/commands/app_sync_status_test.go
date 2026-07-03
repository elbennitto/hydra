package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestBuildAppSyncStatuses_MarksAppsWithoutChangesAsInSync(t *testing.T) {
	selected := sets.New[types.AppId](
		types.AppId("prod.apps.api"),
		types.AppId("prod.apps.worker"),
	)

	statuses := BuildAppSyncStatuses(selected, sets.New[types.AppId]())

	assert.Equal(t, []AppSyncStatus{
		{AppId: types.AppId("prod.apps.api"), InSync: true},
		{AppId: types.AppId("prod.apps.worker"), InSync: true},
	}, statuses)
}

func TestBuildAppSyncStatuses_MarksChangedAppsAsOutOfSync(t *testing.T) {
	selected := sets.New[types.AppId](
		types.AppId("prod.apps.api"),
		types.AppId("prod.apps.worker"),
		types.AppId("prod.infra.argocd"),
	)
	changed := sets.New[types.AppId](
		types.AppId("prod.apps.worker"),
	)

	statuses := BuildAppSyncStatuses(selected, changed)

	assert.Equal(t, []AppSyncStatus{
		{AppId: types.AppId("prod.apps.api"), InSync: true},
		{AppId: types.AppId("prod.apps.worker"), InSync: false},
		{AppId: types.AppId("prod.infra.argocd"), InSync: true},
	}, statuses)
}
