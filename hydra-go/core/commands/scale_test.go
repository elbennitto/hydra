package commands

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func makeDaemonSetEntity(namespace, name string, nodeSelector map[string]string) entity.Entity {
	return buildDaemonSetEntity(namespace, name, nodeSelector, types.KeyClusterEntity)
}

func buildDaemonSetEntity(namespace, name string, nodeSelector map[string]string, key types.EntityKeyUnstructured) entity.Entity {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("DaemonSet"))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("daemonsets")).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}

	templateSpec := map[string]any{}
	if nodeSelector != nil {
		nsMap := map[string]any{}
		for k, v := range nodeSelector {
			nsMap[k] = v
		}
		templateSpec["nodeSelector"] = nsMap
	}

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "DaemonSet",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"template": map[string]any{
					"spec": templateSpec,
				},
			},
		},
	}
	return mustBuild(b.WithUnstructured(key, u))
}

func buildJobEntity(namespace, name string, suspend bool, ownerCronJob bool, key types.EntityKeyUnstructured) entity.Entity {
	gvk := types.NewGVK(types.Group("batch"), types.Version("v1"), types.Kind("Job"))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("jobs")).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	meta := map[string]any{
		"name":      name,
		"namespace": namespace,
	}
	if ownerCronJob {
		meta["ownerReferences"] = []any{
			map[string]any{
				"apiVersion": "batch/v1",
				"kind":       "CronJob",
				"name":       "parent-cron",
				"uid":        "abc-uid",
			},
		}
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "batch/v1",
			"kind":       "Job",
			"metadata":   meta,
			"spec": map[string]any{
				"suspend": suspend,
			},
		},
	}
	return mustBuild(b.WithUnstructured(key, u))
}

func buildWorkloadEntity(group, version, kind, resource, namespace, name string, replicas int64, key types.EntityKeyUnstructured) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource(resource)).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	apiVersion := version
	if group != "" {
		apiVersion = group + "/" + version
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"replicas": replicas,
			},
		},
	}
	return mustBuild(b.WithUnstructured(key, u))
}

func makeConfigMapWithData(namespace, name string, key types.EntityKeyUnstructured) entity.Entity {
	b := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("ConfigMap"))).
		WithResource(types.Resource("configmaps")).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"data": map[string]any{
				"config.yaml": "key: value",
			},
		},
	}
	return mustBuild(b.WithUnstructured(key, u))
}

// --- CollectScaleTargets tests ---

func TestCollectScaleTargets_DeploymentWithReplicas(t *testing.T) {
	dep := makeScaleDownEntity("apps", "v1", "Deployment", "deployments", "default", "web-app", 3)
	entities, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, types.Id("apps/v1/Deployment/default/web-app"), targets[0].Id)
	assert.Equal(t, types.Name("web-app"), targets[0].Name)
	assert.Equal(t, types.Namespace("default"), targets[0].Ns)
	assert.Equal(t, types.GVKString("apps/v1/Deployment"), targets[0].GVK)
	assert.Equal(t, types.NewGVR("apps", "v1", "deployments"), targets[0].GVR)
	assert.Equal(t, int64(3), targets[0].Replicas)
	assert.False(t, targets[0].IsDaemonSet)
}

func TestCollectScaleTargets_StatefulSetWithZeroReplicas(t *testing.T) {
	sts := makeScaleDownEntity("apps", "v1", "StatefulSet", "statefulsets", "default", "db", 0)
	entities, err := entity.NewEntities([]entity.Entity{sts})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, types.Id("apps/v1/StatefulSet/default/db"), targets[0].Id)
	assert.Equal(t, int64(0), targets[0].Replicas)
	assert.False(t, targets[0].IsDaemonSet)
}

func TestCollectScaleTargets_DaemonSetWithNodeSelector(t *testing.T) {
	ds := makeDaemonSetEntity("default", "log-agent", map[string]string{"app": "web"})
	entities, err := entity.NewEntities([]entity.Entity{ds})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, types.Id("apps/v1/DaemonSet/default/log-agent"), targets[0].Id)
	assert.Equal(t, types.Name("log-agent"), targets[0].Name)
	assert.Equal(t, types.Namespace("default"), targets[0].Ns)
	assert.Equal(t, types.GVKString("apps/v1/DaemonSet"), targets[0].GVK)
	assert.Equal(t, types.NewGVR("apps", "v1", "daemonsets"), targets[0].GVR)
	assert.True(t, targets[0].IsDaemonSet)
	assert.Equal(t, map[string]string{"app": "web"}, targets[0].NodeSelector)
}

func TestCollectScaleTargets_DaemonSetWithoutNodeSelector(t *testing.T) {
	ds := makeDaemonSetEntity("default", "monitor", nil)
	entities, err := entity.NewEntities([]entity.Entity{ds})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, types.Id("apps/v1/DaemonSet/default/monitor"), targets[0].Id)
	assert.True(t, targets[0].IsDaemonSet)
	assert.Nil(t, targets[0].NodeSelector)
}

func TestCollectScaleTargets_ConfigMapNotIncluded(t *testing.T) {
	cm := makeEntity("", "v1", "ConfigMap", "default", "app-config")
	entities, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Empty(t, targets)
}

func TestCollectScaleTargets_IncludesStandaloneJob(t *testing.T) {
	j := buildJobEntity("demo", "migrations", false, false, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{j})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, types.Id("batch/v1/Job/demo/migrations"), targets[0].Id)
	assert.True(t, targets[0].IsJob)
	assert.Equal(t, types.GVKString("batch/v1/Job"), targets[0].GVK)
}

func TestCollectScaleTargets_JobOwnedByCronJobExcluded(t *testing.T) {
	j := buildJobEntity("demo", "from-cron", false, true, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{j})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Empty(t, targets)
}

func TestCollectScaleTargets_MultipleWorkloads(t *testing.T) {
	dep := makeScaleDownEntity("apps", "v1", "Deployment", "deployments", "default", "frontend", 2)
	sts := makeScaleDownEntity("apps", "v1", "StatefulSet", "statefulsets", "default", "db", 1)
	ds := makeDaemonSetEntity("default", "log-agent", map[string]string{"app": "web"})

	entities, err := entity.NewEntities([]entity.Entity{dep, sts, ds})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 3)

	targetIds := make([]types.Id, len(targets))
	for i, tgt := range targets {
		targetIds[i] = tgt.Id
	}
	assert.Contains(t, targetIds, types.Id("apps/v1/Deployment/default/frontend"))
	assert.Contains(t, targetIds, types.Id("apps/v1/StatefulSet/default/db"))
	assert.Contains(t, targetIds, types.Id("apps/v1/DaemonSet/default/log-agent"))
}

func TestCollectScaleTargets_DeploymentWithNilReplicas(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("cert-manager-webhook")).
		WithNamespace(types.Namespace("cert-manager")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      "cert-manager-webhook",
				"namespace": "cert-manager",
			},
			"spec": map[string]any{},
		},
	}
	e = withUnstructured(e, types.KeyClusterEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, int64(1), targets[0].Replicas)
	assert.False(t, targets[0].IsDaemonSet)
}

func TestCollectScaleTargets_DeploymentWithFloat64Replicas(t *testing.T) {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("argocd-repo-server")).
		WithNamespace(types.Namespace("argocd")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      "argocd-repo-server",
				"namespace": "argocd",
			},
			"spec": map[string]any{
				"replicas": float64(1),
			},
		},
	}
	e = withUnstructured(e, types.KeyClusterEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, int64(1), targets[0].Replicas)
	assert.False(t, targets[0].IsDaemonSet)
}

func TestCollectScaleTargets_EntityWithoutUnstructuredData(t *testing.T) {
	dep := withResource(makeEntity("apps", "v1", "Deployment", "default", "no-data"), types.Resource("deployments"))
	entities, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Empty(t, targets)
}

// --- ZeroWorkloads tests ---

func TestZeroWorkloads_DeploymentReplicasSetToZero(t *testing.T) {
	dep := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web-app", 3, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	replicas := values.Lookup(u.Object, "spec", "replicas")
	assert.EqualValues(t, 0, replicas)
}

func TestZeroWorkloads_StatefulSetAlreadyZero(t *testing.T) {
	sts := buildWorkloadEntity("apps", "v1", "StatefulSet", "statefulsets", "default", "db", 0, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{sts})
	require.NoError(t, err)

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	replicas := values.Lookup(u.Object, "spec", "replicas")
	assert.EqualValues(t, 0, replicas)
}

func TestZeroWorkloads_JobSetsSuspendTrue(t *testing.T) {
	j := buildJobEntity("default", "migrate", false, false, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{j})
	require.NoError(t, err)

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	assert.Equal(t, true, values.Lookup(u.Object, "spec", "suspend"))
}

func TestZeroWorkloads_DaemonSetNodeSelectorReplaced(t *testing.T) {
	ds := buildDaemonSetEntity("default", "log-agent", map[string]string{"app": "web"}, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{ds})
	require.NoError(t, err)

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	ns := values.Lookup(u.Object, "spec", "template", "spec", "nodeSelector")
	expected := map[string]any{hydra.AnnotationHydraScaleDisabled: "true"}
	assert.Equal(t, expected, ns)
}

func TestZeroWorkloads_DaemonSetWithoutNodeSelectorGetsDisabled(t *testing.T) {
	ds := buildDaemonSetEntity("default", "monitor", nil, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{ds})
	require.NoError(t, err)

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	ns := values.Lookup(u.Object, "spec", "template", "spec", "nodeSelector")
	expected := map[string]any{hydra.AnnotationHydraScaleDisabled: "true"}
	assert.Equal(t, expected, ns)
}

func TestZeroWorkloads_ConfigMapUnchanged(t *testing.T) {
	cm := makeConfigMapWithData("default", "app-config", types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	assert.Equal(t, "ConfigMap", u.Object["kind"])
	data := values.Lookup(u.Object, "data", "config.yaml")
	assert.Equal(t, "key: value", data)
}

func TestZeroWorkloads_EntityWithoutUnstructuredData(t *testing.T) {
	dep := withResource(makeEntity("apps", "v1", "Deployment", "default", "no-data"), types.Resource("deployments"))
	entities, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	name, err := result.Items[0].Name()
	require.NoError(t, err)
	assert.Equal(t, types.Name("no-data"), name)
}

// --- ZeroWorkloads custom workload tests ---

func TestZeroWorkloads_CustomWorkloadReplicasSetToZero(t *testing.T) {
	kafka := buildKafkaCREntity("kafka-ns", "my-kafka", 3, 3, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{kafka})
	require.NoError(t, err)

	customWorkloads := map[types.GVKString]types.HydraScaleGroup{
		"kafka.strimzi.io/v1beta2/Kafka": {
			GVK:          "kafka.strimzi.io/v1beta2/Kafka",
			ReplicaPaths: []string{"spec.kafka.replicas", "spec.zookeeper.replicas"},
		},
	}

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity, customWorkloads)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	kafkaReplicas := values.Lookup(u.Object, "spec", "kafka", "replicas")
	assert.EqualValues(t, 0, kafkaReplicas)

	zkReplicas := values.Lookup(u.Object, "spec", "zookeeper", "replicas")
	assert.EqualValues(t, 0, zkReplicas)
}

func TestZeroWorkloads_CustomWorkloadSingleReplicaPath(t *testing.T) {
	kc := buildCustomCREntity("kafka.strimzi.io", "v1beta2", "KafkaConnect", "kafkaconnects", "kafka-ns", "my-connect", 5, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{kc})
	require.NoError(t, err)

	customWorkloads := map[types.GVKString]types.HydraScaleGroup{
		"kafka.strimzi.io/v1beta2/KafkaConnect": {
			GVK:          "kafka.strimzi.io/v1beta2/KafkaConnect",
			ReplicaPaths: []string{"spec.replicas"},
		},
	}

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity, customWorkloads)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	replicas := values.Lookup(u.Object, "spec", "replicas")
	assert.EqualValues(t, 0, replicas)
}

func TestZeroWorkloads_CustomWorkloadUnchangedOtherFields(t *testing.T) {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("kafkas")).
		WithName(types.Name("my-kafka")).
		WithNamespace(types.Namespace("kafka-ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "my-kafka",
				"namespace": "kafka-ns",
			},
			"spec": map[string]any{
				"kafka": map[string]any{
					"replicas": int64(3),
					"listeners": []any{
						map[string]any{
							"name": "plain",
							"port": int64(9092),
							"type": "internal",
						},
					},
				},
				"zookeeper": map[string]any{
					"replicas": int64(3),
				},
			},
		},
	}
	e = withUnstructured(e, types.KeyTemplateEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	customWorkloads := map[types.GVKString]types.HydraScaleGroup{
		"kafka.strimzi.io/v1beta2/Kafka": {
			GVK:          "kafka.strimzi.io/v1beta2/Kafka",
			ReplicaPaths: []string{"spec.kafka.replicas", "spec.zookeeper.replicas"},
		},
	}

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity, customWorkloads)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	uResult, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	kafkaReplicas := values.Lookup(uResult.Object, "spec", "kafka", "replicas")
	assert.EqualValues(t, 0, kafkaReplicas)

	zkReplicas := values.Lookup(uResult.Object, "spec", "zookeeper", "replicas")
	assert.EqualValues(t, 0, zkReplicas)

	listeners := values.Lookup(uResult.Object, "spec", "kafka", "listeners")
	require.NotNil(t, listeners)
	listenerSlice, ok := listeners.([]any)
	require.True(t, ok)
	require.Len(t, listenerSlice, 1)
	listenerMap, ok := listenerSlice[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "plain", listenerMap["name"])
}

func TestZeroWorkloads_CustomWorkloadMultipleReplicaPaths(t *testing.T) {
	kafka := buildKafkaCREntity("kafka-ns", "multi-kafka", 5, 3, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{kafka})
	require.NoError(t, err)

	customWorkloads := map[types.GVKString]types.HydraScaleGroup{
		"kafka.strimzi.io/v1beta2/Kafka": {
			GVK:          "kafka.strimzi.io/v1beta2/Kafka",
			ReplicaPaths: []string{"spec.kafka.replicas", "spec.zookeeper.replicas"},
		},
	}

	result, err := ZeroWorkloads(entities, types.KeyTemplateEntity, customWorkloads)
	require.NoError(t, err)
	require.Equal(t, 1, result.Len())

	u, err := result.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)

	kafkaReplicas := values.Lookup(u.Object, "spec", "kafka", "replicas")
	assert.EqualValues(t, 0, kafkaReplicas)

	zkReplicas := values.Lookup(u.Object, "spec", "zookeeper", "replicas")
	assert.EqualValues(t, 0, zkReplicas)
}

// --- LogStartupOrder tests ---

func TestLogStartupOrder_EmptyEntities(t *testing.T) {
	entities, err := entity.NewEntities(nil)
	require.NoError(t, err)

	plan, err := LogStartupOrder(log.Default(), entities, nil, types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Nil(t, plan)
}

func TestLogStartupOrder_NoWorkloads(t *testing.T) {
	cm := makeConfigMapWithData("default", "app-config", types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{cm})
	require.NoError(t, err)

	plan, err := LogStartupOrder(log.Default(), entities, nil, types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Nil(t, plan)
}

func TestLogStartupOrder_SingleWorkload(t *testing.T) {
	dep := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web-app", 3, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	plan, err := LogStartupOrder(log.Default(), entities, nil, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, plan, 1)
	assert.Equal(t, "apps/v1/Deployment/default/web-app", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
}

func TestLogStartupOrder_IndependentWorkloads(t *testing.T) {
	dep1 := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "backend", 2, types.KeyTemplateEntity)
	dep2 := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "api", 1, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{dep1, dep2})
	require.NoError(t, err)

	plan, err := LogStartupOrder(log.Default(), entities, nil, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, plan, 2)
	assert.Equal(t, "apps/v1/Deployment/default/api", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
	assert.Equal(t, "apps/v1/Deployment/default/backend", plan[1].Name)
	assert.Empty(t, plan[1].Dependencies)
}

func TestLogStartupOrder_WithDependencies(t *testing.T) {
	backend := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "backend", 2, types.KeyTemplateEntity)
	database := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "database", 1, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{backend, database})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/default/backend", To: "apps/v1/Deployment/default/database"},
	}

	plan, err := LogStartupOrder(log.Default(), entities, refs, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, plan, 2)
	assert.Equal(t, "apps/v1/Deployment/default/database", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
	assert.Equal(t, "apps/v1/Deployment/default/backend", plan[1].Name)
	assert.Equal(t, []string{"apps/v1/Deployment/default/database"}, plan[1].Dependencies)
}

func TestLogStartupOrder_TransitiveDependencyThroughSecret(t *testing.T) {
	dex := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "dex", "dex", 1, types.KeyTemplateEntity)
	sopsOp := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "sops-secrets-operator", "sops-secrets-operator", 1, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{dex, sopsOp})
	require.NoError(t, err)

	// dex → Secret (imagePullSecrets) → Secret (clone-source) ← SopsSecret (reverse) → sops-operator
	refs := []types.Ref{
		{From: "apps/v1/Deployment/dex/dex", To: "v1/Secret/dex/image-pull-secret"},
		{From: "v1/Secret/dex/image-pull-secret", To: "v1/Secret/sops-secrets-operator/image-pull-secret"},
		{From: "isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret",
			To: "v1/Secret/sops-secrets-operator/image-pull-secret", Reverse: true},
		{From: "isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret",
			To: "apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator"},
	}

	plan, err := LogStartupOrder(log.Default(), entities, refs, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, plan, 2)
	assert.Equal(t, "apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
	assert.Equal(t, "apps/v1/Deployment/dex/dex", plan[1].Name)
	assert.Equal(t, []string{"apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator"}, plan[1].Dependencies)
}

func TestLogStartupOrder_MixedEntities(t *testing.T) {
	dep1 := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "worker", 1, types.KeyTemplateEntity)
	dep2 := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "api", 2, types.KeyTemplateEntity)
	cm := makeConfigMapWithData("default", "config", types.KeyTemplateEntity)
	svc := makeEntity("", "v1", "Service", "default", "api-svc")
	entities, err := entity.NewEntities([]entity.Entity{dep1, cm, dep2, svc})
	require.NoError(t, err)

	plan, err := LogStartupOrder(log.Default(), entities, nil, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, plan, 2)
	assert.Equal(t, "apps/v1/Deployment/default/api", plan[0].Name)
	assert.Equal(t, "apps/v1/Deployment/default/worker", plan[1].Name)
}

// --- Owned ReplicaSet filtering tests for CollectScaleTargets ---

func makeOwnedReplicaSetScaleEntity(namespace, name, ownerKind, ownerName, ownerUID string, replicas int64) entity.Entity {
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("ReplicaSet"))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("replicasets")).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "ReplicaSet",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"ownerReferences": []any{
					map[string]any{
						"apiVersion": "apps/v1",
						"kind":       ownerKind,
						"name":       ownerName,
						"uid":        ownerUID,
					},
				},
			},
			"spec": map[string]any{
				"replicas": replicas,
			},
		},
	}
	return mustBuild(b.WithUnstructured(types.KeyClusterEntity, u))
}

func TestCollectScaleTargets_ReplicaSetOwnedByDeployment(t *testing.T) {
	rs := makeOwnedReplicaSetScaleEntity("default", "web-app-abc123", "Deployment", "web-app", "deploy-uid-123", 2)
	dep := makeScaleDownEntity("apps", "v1", "Deployment", "deployments", "default", "web-app", 3)

	entities, err := entity.NewEntities([]entity.Entity{rs, dep})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	targetIds := make([]types.Id, len(targets))
	for i, tgt := range targets {
		targetIds[i] = tgt.Id
	}
	assert.Contains(t, targetIds, types.Id("apps/v1/Deployment/default/web-app"))
	assert.NotContains(t, targetIds, types.Id("apps/v1/ReplicaSet/default/web-app-abc123"))
}

func TestCollectScaleTargets_ReplicaSetOwnedByStatefulSet(t *testing.T) {
	rs := makeOwnedReplicaSetScaleEntity("default", "db-rs-xyz789", "StatefulSet", "db", "sts-uid-456", 2)
	sts := makeScaleDownEntity("apps", "v1", "StatefulSet", "statefulsets", "default", "db", 3)

	entities, err := entity.NewEntities([]entity.Entity{rs, sts})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, types.Id("apps/v1/StatefulSet/default/db"), targets[0].Id)
	assert.NotContains(t, []types.Id{targets[0].Id}, types.Id("apps/v1/ReplicaSet/default/db-rs-xyz789"))
}

func TestCollectScaleTargets_BareReplicaSet(t *testing.T) {
	rs := makeScaleDownEntity("apps", "v1", "ReplicaSet", "replicasets", "default", "standalone-rs", 2)

	entities, err := entity.NewEntities([]entity.Entity{rs})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, types.Id("apps/v1/ReplicaSet/default/standalone-rs"), targets[0].Id)
	assert.Equal(t, int64(2), targets[0].Replicas)
}

// --- Custom workload helpers ---

func buildKafkaCREntity(namespace, name string, kafkaReplicas, zookeeperReplicas int64, key types.EntityKeyUnstructured) entity.Entity {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("kafkas")).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"kafka": map[string]any{
					"replicas": kafkaReplicas,
				},
				"zookeeper": map[string]any{
					"replicas": zookeeperReplicas,
				},
			},
		},
	}
	return mustBuild(b.WithUnstructured(key, u))
}

func buildCustomCREntity(group, version, kind, resource, namespace, name string, replicas int64, key types.EntityKeyUnstructured) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource(resource)).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	apiVersion := version
	if group != "" {
		apiVersion = group + "/" + version
	}
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"replicas": replicas,
			},
		},
	}
	return mustBuild(b.WithUnstructured(key, u))
}

func captureCommandLogs(t *testing.T, fn func(logger log.Logger)) string {
	return captureCommandLogsAtLevel(t, slog.LevelInfo, fn)
}

func captureCommandLogsAtLevel(t *testing.T, level slog.Level, fn func(logger log.Logger)) string {
	t.Helper()

	var buf bytes.Buffer
	oldDefault := slog.Default()
	oldLogger := log.Default()

	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})
	formattedHandler := log.NewFormatHandler(handler, log.FormatOptions{RemoveUsedAttrs: true})
	slog.SetDefault(slog.New(formattedHandler))
	logger := log.NewLogger()
	log.SetDefault(logger)

	defer func() {
		log.SetDefault(oldLogger)
		slog.SetDefault(oldDefault)
	}()

	fn(logger)
	return buf.String()
}

func makeReadyDeploymentObject(namespace, name string, replicas, readyReplicas int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"replicas": replicas,
			},
			"status": map[string]any{
				"readyReplicas": readyReplicas,
			},
		},
	}
}

func makeLiveDaemonSetObject(namespace, name string, nodeSelector map[string]string) *unstructured.Unstructured {
	templateSpec := map[string]any{}
	if nodeSelector != nil {
		nsMap := map[string]any{}
		for k, v := range nodeSelector {
			nsMap[k] = v
		}
		templateSpec["nodeSelector"] = nsMap
	}

	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "DaemonSet",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"template": map[string]any{
					"spec": templateSpec,
				},
			},
			"status": map[string]any{
				"desiredNumberScheduled": int64(0),
				"numberReady":            int64(0),
			},
		},
	}
}

// --- CollectScaleTargets custom workload tests ---

func TestCollectScaleTargets_CustomWorkloadKafkaCR(t *testing.T) {
	kafka := buildKafkaCREntity("kafka-ns", "my-kafka", 3, 3, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{kafka})
	require.NoError(t, err)

	customWorkloads := map[types.GVKString]types.HydraScaleGroup{
		"kafka.strimzi.io/v1beta2/Kafka": {
			GVK:          "kafka.strimzi.io/v1beta2/Kafka",
			ReplicaPaths: []string{"spec.kafka.replicas", "spec.zookeeper.replicas"},
		},
	}

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity, customWorkloads)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.True(t, targets[0].IsCustomWorkload)
	assert.Equal(t, []string{"spec.kafka.replicas", "spec.zookeeper.replicas"}, targets[0].ReplicaPaths)
	assert.Equal(t, map[string]int64{
		"spec.kafka.replicas":     3,
		"spec.zookeeper.replicas": 3,
	}, targets[0].OriginalReplicas)
}

func TestCollectScaleTargets_CustomWorkloadSingleReplicaPath(t *testing.T) {
	kc := buildCustomCREntity("kafka.strimzi.io", "v1beta2", "KafkaConnect", "kafkaconnects", "kafka-ns", "my-connect", 5, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{kc})
	require.NoError(t, err)

	customWorkloads := map[types.GVKString]types.HydraScaleGroup{
		"kafka.strimzi.io/v1beta2/KafkaConnect": {
			GVK:          "kafka.strimzi.io/v1beta2/KafkaConnect",
			ReplicaPaths: []string{"spec.replicas"},
		},
	}

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity, customWorkloads)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.True(t, targets[0].IsCustomWorkload)
	assert.Equal(t, []string{"spec.replicas"}, targets[0].ReplicaPaths)
	assert.Equal(t, map[string]int64{
		"spec.replicas": 5,
	}, targets[0].OriginalReplicas)
}

func TestCollectScaleTargets_CustomWorkloadMissingReplicaPath(t *testing.T) {
	gvk := types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("kafkas")).
		WithName(types.Name("partial-kafka")).
		WithNamespace(types.Namespace("kafka-ns")))

	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "kafka.strimzi.io/v1beta2",
			"kind":       "Kafka",
			"metadata": map[string]any{
				"name":      "partial-kafka",
				"namespace": "kafka-ns",
			},
			"spec": map[string]any{
				"kafka": map[string]any{
					"replicas": int64(3),
				},
			},
		},
	}
	e = withUnstructured(e, types.KeyClusterEntity, u)

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	customWorkloads := map[types.GVKString]types.HydraScaleGroup{
		"kafka.strimzi.io/v1beta2/Kafka": {
			GVK:          "kafka.strimzi.io/v1beta2/Kafka",
			ReplicaPaths: []string{"spec.kafka.replicas", "spec.zookeeper.replicas"},
		},
	}

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity, customWorkloads)
	require.NoError(t, err)
	require.Len(t, targets, 1)

	assert.Equal(t, int64(3), targets[0].OriginalReplicas["spec.kafka.replicas"])
	assert.Equal(t, int64(0), targets[0].OriginalReplicas["spec.zookeeper.replicas"])
}

func TestCollectScaleTargets_MixBuiltInAndCustom(t *testing.T) {
	dep := makeScaleDownEntity("apps", "v1", "Deployment", "deployments", "default", "web-app", 3)
	kafka := buildKafkaCREntity("kafka-ns", "my-kafka", 3, 3, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{dep, kafka})
	require.NoError(t, err)

	customWorkloads := map[types.GVKString]types.HydraScaleGroup{
		"kafka.strimzi.io/v1beta2/Kafka": {
			GVK:          "kafka.strimzi.io/v1beta2/Kafka",
			ReplicaPaths: []string{"spec.kafka.replicas", "spec.zookeeper.replicas"},
		},
	}

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity, customWorkloads)
	require.NoError(t, err)
	require.Len(t, targets, 2)

	targetIds := make([]types.Id, len(targets))
	for i, tgt := range targets {
		targetIds[i] = tgt.Id
	}
	assert.Contains(t, targetIds, types.Id("apps/v1/Deployment/default/web-app"))
	assert.Contains(t, targetIds, types.Id("kafka.strimzi.io/v1beta2/Kafka/kafka-ns/my-kafka"))

	for _, tgt := range targets {
		if tgt.GVK == "apps/v1/Deployment" {
			assert.False(t, tgt.IsCustomWorkload)
		}
		if tgt.GVK == "kafka.strimzi.io/v1beta2/Kafka" {
			assert.True(t, tgt.IsCustomWorkload)
		}
	}
}

func TestCollectScaleTargets_CustomWorkloadWithoutUnstructured(t *testing.T) {
	e := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("kafka.strimzi.io"), types.Version("v1beta2"), types.Kind("Kafka"))).
		WithResource(types.Resource("kafkas")).
		WithName(types.Name("no-data-kafka")).
		WithNamespace(types.Namespace("kafka-ns")))

	entities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	customWorkloads := map[types.GVKString]types.HydraScaleGroup{
		"kafka.strimzi.io/v1beta2/Kafka": {
			GVK:          "kafka.strimzi.io/v1beta2/Kafka",
			ReplicaPaths: []string{"spec.kafka.replicas"},
		},
	}

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity, customWorkloads)
	require.NoError(t, err)
	assert.Empty(t, targets)
}

func TestCollectScaleTargets_NoCustomWorkloadsBackwardCompatible(t *testing.T) {
	kafka := buildKafkaCREntity("kafka-ns", "my-kafka", 3, 3, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{kafka})
	require.NoError(t, err)

	targets, err := CollectScaleTargets(entities, types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Empty(t, targets)
}

func TestLogStartupOrder_CrossAppCRDDependencyDoesNotFanOutToAllWorkloads(t *testing.T) {
	dexDeploy := withAppIds(buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "dex", "dex", 1, types.KeyTemplateEntity),
		[]types.AppId{"dex"})
	sopsDeploy := withAppIds(buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "sops-operator", "sops-operator", 1, types.KeyTemplateEntity),
		[]types.AppId{"sops"})
	sopsSecret := withAppIds(makeEntity("isindir.github.com", "v1alpha3", "SopsSecret", "dex", "my-secret"),
		[]types.AppId{"dex"})
	sopsCRD := withAppIds(makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "sopssecrets.isindir.github.com"),
		[]types.AppId{"sops"})

	entities, err := entity.NewEntities([]entity.Entity{dexDeploy, sopsDeploy, sopsSecret, sopsCRD})
	require.NoError(t, err)

	// A CR → CRD ref across apps must not fan out to unrelated workloads.
	refs := []types.Ref{
		{From: "isindir.github.com/v1alpha3/SopsSecret/dex/my-secret",
			To: "apiextensions.k8s.io/v1/CustomResourceDefinition//sopssecrets.isindir.github.com"},
	}

	plan, err := LogStartupOrder(log.Default(), entities, refs, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, plan, 2)

	assert.Equal(t, "apps/v1/Deployment/dex/dex", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
	assert.Equal(t, "apps/v1/Deployment/sops-operator/sops-operator", plan[1].Name)
	assert.Empty(t, plan[1].Dependencies)
}

func TestLogStartupOrder_TransitiveDependencyThroughKyvernoClonedImagePullSecret(t *testing.T) {
	dex := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "dex", "dex", 1, types.KeyTemplateEntity)
	kyverno := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "kyverno", "kyverno-admission-controller", 1, types.KeyTemplateEntity)
	sopsOp := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "sops-secrets-operator", "sops-secrets-operator", 1, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{dex, kyverno, sopsOp})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/dex/dex", To: "v1/Secret/dex/image-pull-secret"},
		{From: "v1/Secret/dex/image-pull-secret", To: "v1/Secret/sops-secrets-operator/image-pull-secret", Labels: []string{"clone-source"}},
		{From: "v1/Secret/dex/image-pull-secret", To: "apps/v1/Deployment/kyverno/kyverno-admission-controller"},
		{
			From:    "isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret",
			To:      "v1/Secret/sops-secrets-operator/image-pull-secret",
			Reverse: true,
		},
		{From: "isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret",
			To: "apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator"},
	}

	plan, err := LogStartupOrder(log.Default(), entities, refs, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, plan, 3)
	assert.Equal(t, "apps/v1/Deployment/kyverno/kyverno-admission-controller", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
	assert.Equal(t, "apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator", plan[1].Name)
	assert.Empty(t, plan[1].Dependencies)
	assert.Equal(t, "apps/v1/Deployment/dex/dex", plan[2].Name)
	assert.ElementsMatch(t,
		[]string{
			"apps/v1/Deployment/kyverno/kyverno-admission-controller",
			"apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator",
		},
		plan[2].Dependencies,
	)
}

func TestLogStartupOrder_OptionalOnlyWorkloadsRunAfterRequired(t *testing.T) {
	required := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "z-required", 1, types.KeyTemplateEntity)
	optionalProvider := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "monitoring", "m-optional-provider", 1, types.KeyTemplateEntity)
	optionalConsumer := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "monitoring", "a-optional-consumer", 1, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{optionalConsumer, required, optionalProvider})
	require.NoError(t, err)

	refs := []types.Ref{
		{
			From: "apps/v1/Deployment/monitoring/a-optional-consumer",
			To:   "apps/v1/Deployment/monitoring/m-optional-provider",
			Tags: []string{"optional:startup"},
		},
	}

	plan, err := LogStartupOrder(log.Default(), entities, refs, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, plan, 3)
	assert.Equal(t, "apps/v1/Deployment/default/z-required", plan[0].Name)
	assert.Equal(t, "apps/v1/Deployment/monitoring/m-optional-provider", plan[1].Name)
	assert.Equal(t, "apps/v1/Deployment/monitoring/a-optional-consumer", plan[2].Name)
	assert.Equal(t, []string{"apps/v1/Deployment/monitoring/m-optional-provider"}, plan[2].Dependencies)
}

func TestLogStartupOrder_MissingOptionalDependenciesDoNotBlockRequiredWorkloads(t *testing.T) {
	required := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "z-required", 1, types.KeyTemplateEntity)
	optionalConsumer := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "monitoring", "a-optional-consumer", 1, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{optionalConsumer, required})
	require.NoError(t, err)

	refs := []types.Ref{
		{
			From: "apps/v1/Deployment/monitoring/a-optional-consumer",
			To:   "apps/v1/Deployment/monitoring/missing-provider",
			Tags: []string{"optional:startup"},
		},
	}

	plan, err := LogStartupOrder(log.Default(), entities, refs, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, plan, 2)
	assert.Equal(t, "apps/v1/Deployment/default/z-required", plan[0].Name)
	assert.Empty(t, plan[0].Dependencies)
	assert.Equal(t, "apps/v1/Deployment/monitoring/a-optional-consumer", plan[1].Name)
	assert.Empty(t, plan[1].Dependencies)
}

func TestScaleUpWorkloads_AlreadyReadyDeploymentDoesNotLogWaitingOrWait(t *testing.T) {
	deployment := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web-app", 3, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{deployment})
	require.NoError(t, err)

	liveDeployment := makeReadyDeploymentObject("default", "web-app", 3, 3)
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveDeployment)

	var scaleErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		// A zero timeout makes any unnecessary wait-loop entry fail immediately.
		scaleErr = ScaleUpWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			nil,
			nil,
			types.KeyClusterEntity,
			types.KeyClusterEntity,
			false,
			0*time.Second,
			nil,
		)
	})

	assert.NoError(t, scaleErr)
	assert.Contains(t, logs, "nothing to scale up")
	assert.NotContains(t, logs, "found 1 workloads:")
	assert.NotContains(t, logs, "scaling up web-app to 3 replicas (current: 3, skipped)")
	assert.NotContains(t, logs, "waiting for web-app to become ready")
}

func TestScaleDownWorkloads_LogsScalingLifecycleAndScaledResources(t *testing.T) {
	deployment := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "service-monitoring", 2, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{deployment})
	require.NoError(t, err)

	liveDeployment := makeReadyDeploymentObject("default", "service-monitoring", 2, 2)
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveDeployment)

	var scaleErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		scaleErr = ScaleDownWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			nil,
			nil,
			types.KeyClusterEntity,
			true,
			false,
			0*time.Second,
		)
	})

	require.NoError(t, scaleErr)
	assert.NotContains(t, logs, "found 1 workloads:")
	assert.Contains(t, logs, "* apps/v1/Deployment/default/service-monitoring")
	assert.Contains(t, logs, "starting scale down")
	assert.Contains(t, logs, "scaling 1 resources:")
	assert.Contains(t, logs, "scale down finished")
	assert.NotContains(t, logs, "scaling down service-monitoring to 0 replicas")
}

func TestScaleDownWorkloads_NotFoundLogsDebug(t *testing.T) {
	deployment := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "missing", 1, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{deployment})
	require.NoError(t, err)

	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())

	var scaleErr error
	logs := captureCommandLogsAtLevel(t, slog.LevelDebug, func(logger log.Logger) {
		scaleErr = ScaleDownWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			nil,
			nil,
			types.KeyClusterEntity,
			false,
			false,
			3*time.Second,
		)
	})

	require.NoError(t, scaleErr)
	assert.Contains(t, logs, "level=DEBUG")
	assert.Contains(t, logs, "missing not found, skipping")
	assert.NotContains(t, logs, "level=WARN")
}

func TestScaleDownWorkloads_DaemonSetAlreadyDisabledLogsDebugAndSkipsPatch(t *testing.T) {
	daemonSet := buildDaemonSetEntity("default", "csi-nfs-node", map[string]string{"app": "nfs"}, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{daemonSet})
	require.NoError(t, err)

	liveDaemonSet := makeLiveDaemonSetObject("default", "csi-nfs-node", map[string]string{
		hydra.AnnotationHydraScaleDisabled: "true",
	})
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveDaemonSet)
	patchCalls := 0
	dynamicClient.PrependReactor("patch", "daemonsets", func(action clienttesting.Action) (bool, runtime.Object, error) {
		patchCalls++
		return false, nil, nil
	})

	var scaleErr error
	logs := captureCommandLogsAtLevel(t, slog.LevelDebug, func(logger log.Logger) {
		scaleErr = ScaleDownWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			nil,
			nil,
			types.KeyClusterEntity,
			false,
			false,
			3*time.Second,
		)
	})

	require.NoError(t, scaleErr)
	assert.Zero(t, patchCalls)
	assert.Contains(t, logs, "level=DEBUG")
	assert.Contains(t, logs, "daemonset csi-nfs-node already disabled, skipping")
	assert.NotContains(t, logs, "disabling daemonset csi-nfs-node")
}

func TestScaleDownWorkloads_AlreadyZeroReplicasLogsDebugAndSkipsPatch(t *testing.T) {
	deployment := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "sops-secrets-operator", 0, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{deployment})
	require.NoError(t, err)

	liveDeployment := makeReadyDeploymentObject("default", "sops-secrets-operator", 0, 0)
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveDeployment)
	patchCalls := 0
	dynamicClient.PrependReactor("patch", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		patchCalls++
		return false, nil, nil
	})

	var scaleErr error
	logs := captureCommandLogsAtLevel(t, slog.LevelDebug, func(logger log.Logger) {
		scaleErr = ScaleDownWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			nil,
			nil,
			types.KeyClusterEntity,
			false,
			false,
			3*time.Second,
		)
	})

	require.NoError(t, scaleErr)
	assert.Zero(t, patchCalls)
	assert.Contains(t, logs, "level=DEBUG")
	assert.Contains(t, logs, "sops-secrets-operator is already scaled down with 0 replicas, skipping")
	assert.NotContains(t, logs, "scaling down sops-secrets-operator to 0 replicas")
}

func TestScaleUpWorkloads_DaemonSetRestoreReplacesDisabledNodeSelector(t *testing.T) {
	daemonSet := buildDaemonSetEntity("default", "log-agent", map[string]string{
		"kubernetes.io/os": "linux",
		"node-role":        "infra",
	}, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{daemonSet})
	require.NoError(t, err)

	liveDaemonSet := makeLiveDaemonSetObject("default", "log-agent", map[string]string{
		hydra.AnnotationHydraScaleDisabled: "true",
		"legacy":                           "keep-me",
	})
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveDaemonSet)

	err = ScaleUpWorkloads(
		context.Background(),
		log.Default(),
		dynamicClient,
		entities,
		nil,
		nil,
		types.KeyClusterEntity,
		types.KeyClusterEntity,
		false,
		0*time.Second,
		nil,
	)
	require.NoError(t, err)

	updated, err := dynamicClient.Resource(types.NewGVR("apps", "v1", "daemonsets").K8s()).
		Namespace("default").
		Get(context.Background(), "log-agent", metav1.GetOptions{})
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"kubernetes.io/os": "linux",
		"node-role":        "infra",
	}, values.Lookup(updated.Object, "spec", "template", "spec", "nodeSelector"))
}

func TestScaleUpWorkloads_DaemonSetRestoreRemovesNodeSelectorWhenTemplateHasNone(t *testing.T) {
	daemonSet := buildDaemonSetEntity("default", "monitor", nil, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{daemonSet})
	require.NoError(t, err)

	liveDaemonSet := makeLiveDaemonSetObject("default", "monitor", map[string]string{
		hydra.AnnotationHydraScaleDisabled: "true",
	})
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveDaemonSet)

	err = ScaleUpWorkloads(
		context.Background(),
		log.Default(),
		dynamicClient,
		entities,
		nil,
		nil,
		types.KeyClusterEntity,
		types.KeyClusterEntity,
		false,
		0*time.Second,
		nil,
	)
	require.NoError(t, err)

	updated, err := dynamicClient.Resource(types.NewGVR("apps", "v1", "daemonsets").K8s()).
		Namespace("default").
		Get(context.Background(), "monitor", metav1.GetOptions{})
	require.NoError(t, err)

	templateSpec, ok := values.Lookup(updated.Object, "spec", "template", "spec").(map[string]any)
	require.True(t, ok)
	assert.Nil(t, values.Lookup(updated.Object, "spec", "template", "spec", "nodeSelector"))
	_, exists := templateSpec["nodeSelector"]
	assert.False(t, exists)
}

func TestLogStartupOrder_JobRunsAfterDeploymentWhenRef(t *testing.T) {
	dep := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web", 1, types.KeyTemplateEntity)
	job := buildJobEntity("default", "migrate", true, false, types.KeyTemplateEntity)
	entities, err := entity.NewEntities([]entity.Entity{job, dep})
	require.NoError(t, err)

	refs := []types.Ref{
		{
			From:         "batch/v1/Job/default/migrate",
			To:           "apps/v1/Deployment/default/web",
			RefType:      types.RefTypeIndirect,
			EndpointType: types.RefEndpointTypeId,
		},
	}

	plan, err := LogStartupOrder(log.Default(), entities, refs, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Len(t, plan, 2)
	assert.Equal(t, "apps/v1/Deployment/default/web", plan[0].Name)
	assert.Equal(t, "batch/v1/Job/default/migrate", plan[1].Name)
	assert.Equal(t, []string{"apps/v1/Deployment/default/web"}, plan[1].Dependencies)
}

func TestScaleUpWorkloads_JobAlreadyCompleteSkipsWait(t *testing.T) {
	jobEnt := buildJobEntity("default", "hook", false, false, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{jobEnt})
	require.NoError(t, err)

	liveJob := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "batch/v1",
			"kind":       "Job",
			"metadata": map[string]any{
				"name":      "hook",
				"namespace": "default",
			},
			"spec": map[string]any{
				"suspend": false,
			},
			"status": map[string]any{
				"succeeded": int64(1),
				"conditions": []any{
					map[string]any{"type": "Complete", "status": "True"},
				},
			},
		},
	}
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveJob)

	var scaleErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		scaleErr = ScaleUpWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			nil,
			nil,
			types.KeyClusterEntity,
			types.KeyClusterEntity,
			false,
			0*time.Second,
			nil,
		)
	})

	assert.NoError(t, scaleErr)
	assert.NotContains(t, logs, "waiting for job hook to complete")
}

func TestScaleUpWorkloads_CompletedJobDoesNotWaitOnMissingDownstreamPod(t *testing.T) {
	jobEnt := buildJobEntity("default", "hook", false, false, types.KeyTemplateEntity)
	podEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("hook-pod")).
		WithNamespace(types.Namespace("default")))
	entities, err := entity.NewEntities([]entity.Entity{jobEnt, podEnt})
	require.NoError(t, err)

	liveJob := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "batch/v1",
			"kind":       "Job",
			"metadata": map[string]any{
				"name":      "hook",
				"namespace": "default",
			},
			"spec": map[string]any{
				"suspend": false,
			},
			"status": map[string]any{
				"succeeded": int64(1),
				"conditions": []any{
					map[string]any{"type": "Complete", "status": "True"},
				},
			},
		},
	}
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveJob)
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entities, types.KeyClusterEntity)
	require.NoError(t, err)
	refs := []types.Ref{{From: "batch/v1/Job/default/hook", To: "v1/Pod/default/hook-pod"}}

	var scaleErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		scaleErr = ScaleUpWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			refs,
			refs,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			false,
			2*time.Second,
			re,
			nil,
		)
	})

	assert.NoError(t, scaleErr)
	assert.NotContains(t, logs, "waiting for job hook to complete")
	assert.NotContains(t, logs, "aborted: workload hook did not become ready within")
}

// When the local workload is ready but global.hydra.ready checks on transitive refs still fail,
// waitReady keeps polling; it must not re-emit the same "ready (n/n)" line every tick.
func TestScaleUpWorkloads_ReadyInfoNotRepeatedWhileTransitiveReadyBlocked(t *testing.T) {
	blocker := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "transitive-blocker", 1, types.KeyClusterEntity)
	consumer := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "transitive-consumer", 1, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{blocker, consumer})
	require.NoError(t, err)

	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entities, types.KeyClusterEntity)
	require.NoError(t, err)

	liveBlocker := makeReadyDeploymentObject("default", "transitive-blocker", 1, 0)
	liveConsumer := makeReadyDeploymentObject("default", "transitive-consumer", 0, 0)
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveBlocker, liveConsumer)

	// The fake client does not simulate the controller updating status after scale-up; once a patch
	// is applied, serve a fully ready Deployment so waitReady reaches the transitive gate.
	consumerPatchSeen := false
	dynamicClient.PrependReactor("patch", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		pa, ok := action.(clienttesting.PatchActionImpl)
		if !ok || pa.Namespace != "default" || pa.Name != "transitive-consumer" {
			return false, nil, nil
		}
		consumerPatchSeen = true
		return false, nil, nil
	})
	dynamicClient.PrependReactor("get", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		ga, ok := action.(clienttesting.GetActionImpl)
		if !ok || ga.Namespace != "default" || ga.GetName() != "transitive-consumer" {
			return false, nil, nil
		}
		if consumerPatchSeen {
			return true, makeReadyDeploymentObject("default", "transitive-consumer", 1, 1), nil
		}
		return true, makeReadyDeploymentObject("default", "transitive-consumer", 0, 0), nil
	})

	refs := []types.Ref{
		{
			From:         "apps/v1/Deployment/default/transitive-consumer",
			To:           "apps/v1/Deployment/default/transitive-blocker",
			RefType:      types.RefTypeIndirect,
			EndpointType: types.RefEndpointTypeId,
		},
	}

	var scaleErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		scaleErr = ScaleUpWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			refs,
			nil,
			types.KeyClusterEntity,
			types.KeyClusterEntity,
			false,
			6*time.Second,
			re,
			nil,
		)
	})

	require.Error(t, scaleErr)
	assert.Contains(t, scaleErr.Error(), "transitive-consumer")
	assert.Contains(t, scaleErr.Error(), "ready within")
	assert.Equal(t, 1, strings.Count(logs, "transitive-consumer: ready (1/1)"), logs)
	assert.Contains(t, logs, "transitive-consumer: local readiness reached, waiting for transitive ready gates")
	assert.Contains(t, logs, "Deployment default/transitive-blocker")
}

func TestScaleUpWorkloads_NotReadyProgressLogsAreRateLimited(t *testing.T) {
	oldPollInterval := scaleUpWaitPollInterval
	oldProgressInterval := scaleUpProgressLogInterval
	scaleUpWaitPollInterval = 20 * time.Millisecond
	scaleUpProgressLogInterval = 55 * time.Millisecond
	defer func() {
		scaleUpWaitPollInterval = oldPollInterval
		scaleUpProgressLogInterval = oldProgressInterval
	}()

	externalDNS := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "external-dns", 1, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{externalDNS})
	require.NoError(t, err)

	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), makeReadyDeploymentObject("default", "external-dns", 0, 0))

	var scaleErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		scaleErr = ScaleUpWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			nil,
			nil,
			types.KeyClusterEntity,
			types.KeyClusterEntity,
			false,
			145*time.Millisecond,
			nil,
			nil,
		)
	})

	require.Error(t, scaleErr)
	assert.Contains(t, scaleErr.Error(), "external-dns")
	assert.Equal(t, 3, strings.Count(logs, "external-dns: 0/1 ready"), logs)
	assert.Contains(t, logs, "waiting for external-dns to become ready")
}

func TestScaleUpWorkloads_TransitiveWaitLogsAreRateLimited(t *testing.T) {
	oldPollInterval := scaleUpWaitPollInterval
	oldProgressInterval := scaleUpProgressLogInterval
	scaleUpWaitPollInterval = 20 * time.Millisecond
	scaleUpProgressLogInterval = 55 * time.Millisecond
	defer func() {
		scaleUpWaitPollInterval = oldPollInterval
		scaleUpProgressLogInterval = oldProgressInterval
	}()

	blocker := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "transitive-blocker", 1, types.KeyClusterEntity)
	consumer := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "transitive-consumer", 1, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{blocker, consumer})
	require.NoError(t, err)

	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entities, types.KeyClusterEntity)
	require.NoError(t, err)

	liveBlocker := makeReadyDeploymentObject("default", "transitive-blocker", 1, 0)
	liveConsumer := makeReadyDeploymentObject("default", "transitive-consumer", 0, 0)
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveBlocker, liveConsumer)

	consumerPatchSeen := false
	dynamicClient.PrependReactor("patch", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		pa, ok := action.(clienttesting.PatchActionImpl)
		if !ok || pa.Namespace != "default" || pa.Name != "transitive-consumer" {
			return false, nil, nil
		}
		consumerPatchSeen = true
		return false, nil, nil
	})
	dynamicClient.PrependReactor("get", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		ga, ok := action.(clienttesting.GetActionImpl)
		if !ok || ga.Namespace != "default" || ga.GetName() != "transitive-consumer" {
			return false, nil, nil
		}
		if consumerPatchSeen {
			return true, makeReadyDeploymentObject("default", "transitive-consumer", 1, 1), nil
		}
		return true, makeReadyDeploymentObject("default", "transitive-consumer", 0, 0), nil
	})

	refs := []types.Ref{
		{
			From:         "apps/v1/Deployment/default/transitive-consumer",
			To:           "apps/v1/Deployment/default/transitive-blocker",
			RefType:      types.RefTypeIndirect,
			EndpointType: types.RefEndpointTypeId,
		},
	}

	var scaleErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		scaleErr = ScaleUpWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			refs,
			nil,
			types.KeyClusterEntity,
			types.KeyClusterEntity,
			false,
			145*time.Millisecond,
			re,
			nil,
		)
	})

	require.Error(t, scaleErr)
	assert.Contains(t, scaleErr.Error(), "transitive-consumer")
	assert.Equal(t, 3, strings.Count(logs, "transitive-consumer: local readiness reached, waiting for transitive ready gates"), logs)
	assert.Equal(t, 1, strings.Count(logs, "transitive-consumer: ready (1/1)"), logs)
	assert.Contains(t, logs, "Deployment default/transitive-blocker")
}

func TestScaleUpWorkloads_LogsTransitiveWaitCompletion(t *testing.T) {
	blocker := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "transitive-blocker", 1, types.KeyClusterEntity)
	consumer := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "transitive-consumer", 1, types.KeyClusterEntity)
	entities, err := entity.NewEntities([]entity.Entity{blocker, consumer})
	require.NoError(t, err)

	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entities, types.KeyClusterEntity)
	require.NoError(t, err)

	liveBlocker := makeReadyDeploymentObject("default", "transitive-blocker", 1, 0)
	liveConsumer := makeReadyDeploymentObject("default", "transitive-consumer", 0, 0)
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme(), liveBlocker, liveConsumer)

	consumerPatchSeen := false
	blockerGets := 0
	dynamicClient.PrependReactor("patch", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		pa, ok := action.(clienttesting.PatchActionImpl)
		if !ok || pa.Namespace != "default" || pa.Name != "transitive-consumer" {
			return false, nil, nil
		}
		consumerPatchSeen = true
		return false, nil, nil
	})
	dynamicClient.PrependReactor("get", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		ga, ok := action.(clienttesting.GetActionImpl)
		if !ok || ga.Namespace != "default" {
			return false, nil, nil
		}
		switch ga.GetName() {
		case "transitive-consumer":
			if consumerPatchSeen {
				return true, makeReadyDeploymentObject("default", "transitive-consumer", 1, 1), nil
			}
			return true, makeReadyDeploymentObject("default", "transitive-consumer", 0, 0), nil
		case "transitive-blocker":
			blockerGets++
			if blockerGets >= 3 {
				return true, makeReadyDeploymentObject("default", "transitive-blocker", 1, 1), nil
			}
			return true, makeReadyDeploymentObject("default", "transitive-blocker", 1, 0), nil
		default:
			return false, nil, nil
		}
	})

	refs := []types.Ref{
		{
			From:         "apps/v1/Deployment/default/transitive-consumer",
			To:           "apps/v1/Deployment/default/transitive-blocker",
			RefType:      types.RefTypeIndirect,
			EndpointType: types.RefEndpointTypeId,
		},
	}

	var scaleErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		scaleErr = ScaleUpWorkloads(
			context.Background(),
			logger,
			dynamicClient,
			entities,
			refs,
			nil,
			types.KeyClusterEntity,
			types.KeyClusterEntity,
			false,
			8*time.Second,
			re,
			nil,
		)
	})

	require.NoError(t, scaleErr)
	assert.Contains(t, logs, "transitive-consumer: local readiness reached, waiting for transitive ready gates")
	assert.Contains(t, logs, "transitive-consumer: transitive ready gates satisfied")
	assert.Contains(t, logs, "scale up finished")
}

func TestWorkloadReadyAtTarget_DaemonSetWithoutDesiredNumberScheduledIsNotReady(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "DaemonSet",
			"metadata": map[string]any{
				"name":      "log-agent",
				"namespace": "default",
			},
			"status": map[string]any{
				"numberReady": int64(2),
			},
		},
	}

	assert.False(t, workloadReadyAtTarget(obj, ScaleTarget{IsDaemonSet: true}))
}
