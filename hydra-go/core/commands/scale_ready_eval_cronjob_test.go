package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func testCronJobEntityForReady(t *testing.T, spec map[string]any) entity.Entity {
	t.Helper()
	obj := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "CronJob",
		"metadata": map[string]any{
			"name":      "tick",
			"namespace": "default",
		},
	}
	if spec != nil {
		obj["spec"] = spec
	}
	u := unstructured.Unstructured{Object: obj}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("batch"), types.Version("v1"), types.Kind("CronJob"))).
		WithResource(types.Resource("cronjobs")).
		WithName(types.Name("tick")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, u))
}

func TestReadyEvaluator_BuiltinCronJobReadyStates(t *testing.T) {
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	t.Run("not suspended ready", func(t *testing.T) {
		e := testCronJobEntityForReady(t, map[string]any{
			"schedule": "*/5 * * * *",
			"suspend":  false,
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyReady, state)
		assert.Empty(t, msgs)
	})

	t.Run("suspend field absent defaults to scheduling allowed", func(t *testing.T) {
		e := testCronJobEntityForReady(t, map[string]any{
			"schedule": "*/5 * * * *",
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyReady, state)
		assert.Empty(t, msgs)
	})

	t.Run("suspended not ready", func(t *testing.T) {
		e := testCronJobEntityForReady(t, map[string]any{
			"schedule": "*/5 * * * *",
			"suspend":  true,
		})
		matched, state, msgs, err := re.ReadyState(e, types.KeyClusterEntity)
		require.NoError(t, err)
		assert.True(t, matched)
		assert.Equal(t, ClusterScaleReadyNotReady, state)
		j := strings.Join(msgs, " ")
		assert.Contains(t, j, "CronJob is suspended")
		assert.Contains(t, j, "spec.suspend")
	})
}
