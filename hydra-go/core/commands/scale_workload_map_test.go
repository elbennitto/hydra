package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCollectScaleTargets_SkipsStatefulSetWhenOwnerCRInScaleMap(t *testing.T) {
	prom := buildCustomCREntity(
		"monitoring.coreos.com", "v1", "Prometheus", "prometheuses",
		"monitoring", "kube-prometheus-stack-prometheus", 1, types.KeyClusterEntity,
	)

	stsU := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]any{
				"name":            "prometheus-kube-prometheus-stack-prometheus",
				"namespace":       "monitoring",
				"uid":             "22222222-2222-2222-2222-222222222222",
				"ownerReferences": []any{},
			},
			"spec": map[string]any{
				"replicas": int64(1),
			},
		},
	}
	stsU.Object["metadata"].(map[string]any)["ownerReferences"] = []any{
		map[string]any{
			"apiVersion":         "monitoring.coreos.com/v1",
			"kind":               "Prometheus",
			"name":               "kube-prometheus-stack-prometheus",
			"uid":                "11111111-1111-1111-1111-111111111111",
			"controller":         true,
			"blockOwnerDeletion": true,
		},
	}

	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("StatefulSet"))
	stsEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("statefulsets")).
		WithName(types.Name("prometheus-kube-prometheus-stack-prometheus")).
		WithNamespace(types.Namespace("monitoring")).
		WithUnstructured(types.KeyClusterEntity, stsU))

	entities, err := entity.NewEntities([]entity.Entity{prom, stsEnt})
	require.NoError(t, err)

	scaleMap := map[types.GVKString]types.HydraScaleGroup{
		types.GVKString("monitoring.coreos.com/v1/Prometheus"): {
			GVK:          "monitoring.coreos.com/v1/Prometheus",
			ReplicaPaths: []string{"spec.replicas"},
		},
	}

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity, scaleMap)
	require.NoError(t, err)

	var sawSTS bool
	var sawProm bool
	for _, tg := range targets {
		if tg.GVK == types.KubernetesGvkAppsV1StatefulSet {
			sawSTS = true
		}
		if tg.GVK == types.GVKString("monitoring.coreos.com/v1/Prometheus") {
			sawProm = true
		}
	}
	assert.False(t, sawSTS, "operator-managed StatefulSet should be skipped when owner CR is in scale map")
	assert.True(t, sawProm, "CR from scale map should be a scale target")
}

func TestCollectScaleTargets_SkipsStatefulSetViaLiveOwnerReferences(t *testing.T) {
	prom := buildCustomCREntity(
		"monitoring.coreos.com", "v1", "Prometheus", "prometheuses",
		"monitoring", "kube-prometheus-stack-prometheus", 1, types.KeyTemplateEntity,
	)

	templateU := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]any{
				"name":      "prometheus-kube-prometheus-stack-prometheus",
				"namespace": "monitoring",
			},
			"spec": map[string]any{
				"replicas": int64(1),
			},
		},
	}

	liveU := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]any{
				"name":      "prometheus-kube-prometheus-stack-prometheus",
				"namespace": "monitoring",
				"uid":       "22222222-2222-2222-2222-222222222222",
			},
			"spec": map[string]any{
				"replicas": int64(1),
			},
		},
	}
	liveU.Object["metadata"].(map[string]any)["ownerReferences"] = []any{
		map[string]any{
			"apiVersion":         "monitoring.coreos.com/v1",
			"kind":               "Prometheus",
			"name":               "kube-prometheus-stack-prometheus",
			"uid":                "11111111-1111-1111-1111-111111111111",
			"controller":         true,
			"blockOwnerDeletion": true,
		},
	}

	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("StatefulSet"))
	stsEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("statefulsets")).
		WithName(types.Name("prometheus-kube-prometheus-stack-prometheus")).
		WithNamespace(types.Namespace("monitoring")).
		WithUnstructured(types.KeyTemplateEntity, templateU).
		WithUnstructured(types.KeyClusterEntity, liveU))

	entities, err := entity.NewEntities([]entity.Entity{prom, stsEnt})
	require.NoError(t, err)

	scaleMap := map[types.GVKString]types.HydraScaleGroup{
		types.GVKString("monitoring.coreos.com/v1/Prometheus"): {
			GVK:          "monitoring.coreos.com/v1/Prometheus",
			ReplicaPaths: []string{"spec.replicas"},
		},
	}

	targets, err := CollectScaleTargets(entities, types.KeyTemplateEntity, scaleMap)
	require.NoError(t, err)

	var sawSTS bool
	var sawProm bool
	for _, tg := range targets {
		if tg.GVK == types.KubernetesGvkAppsV1StatefulSet {
			sawSTS = true
		}
		if tg.GVK == types.GVKString("monitoring.coreos.com/v1/Prometheus") {
			sawProm = true
		}
	}
	assert.False(t, sawSTS, "StatefulSet with live ownerReferences should be skipped even when using KeyTemplateEntity")
	assert.True(t, sawProm, "CR from scale map should be a scale target")
}

func TestExtractScaleFromMergedMap(t *testing.T) {
	merged := types.ValuesMap{
		"scale": map[string]any{
			"zalando-postgresql": map[string]any{
				"gvk":          "acid.zalan.do/v1/postgresql",
				"replicaPaths": []any{"spec.numberOfInstances"},
			},
		},
	}
	named := extractScaleFromMergedMap(merged)
	require.NotNil(t, named)
	gvkMap := scaleGVKMapFromNamedGroups(named)
	require.Contains(t, gvkMap, types.GVKString("acid.zalan.do/v1/postgresql"))
	assert.Equal(t, []string{"spec.numberOfInstances"}, gvkMap[types.GVKString("acid.zalan.do/v1/postgresql")].ReplicaPaths)
}
