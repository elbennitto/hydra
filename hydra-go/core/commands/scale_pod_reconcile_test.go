package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func liveDeploymentEntity(namespace, name, uid string, replicas int64) entity.Entity {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"uid":       uid,
			},
			"spec": map[string]any{
				"replicas": replicas,
			},
		},
	}
	tpl := *u.DeepCopy()
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("deployments")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyTemplateEntity, tpl).
		WithUnstructured(types.KeyClusterEntity, u))
}

func liveReplicaSetEntity(namespace, name, uid, deployName, deployUID string) entity.Entity {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "ReplicaSet",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"uid":       uid,
				"ownerReferences": []any{
					map[string]any{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"name":       deployName,
						"uid":        deployUID,
					},
				},
			},
			"spec": map[string]any{
				"replicas": int64(0),
			},
		},
	}
	gvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("ReplicaSet"))
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("replicasets")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyClusterEntity, u))
}

func livePodUnstructured(namespace, name, uid, phase, rsName, rsUID string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"uid":       uid,
				"ownerReferences": []any{
					map[string]any{
						"apiVersion": "apps/v1",
						"kind":       "ReplicaSet",
						"name":       rsName,
						"uid":        rsUID,
					},
				},
			},
			"status": map[string]any{
				"phase": phase,
			},
		},
	}
}

func livePodUnstructuredTerminating(namespace, name, uid, phase, rsName, rsUID string) unstructured.Unstructured {
	pod := livePodUnstructured(namespace, name, uid, phase, rsName, rsUID)
	now := metav1.Now()
	pod.SetDeletionTimestamp(&now)
	return pod
}

func livePodOwnedByStatefulSet(namespace, name, podUID, phase, stsName, stsUID string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"uid":       podUID,
				"ownerReferences": []any{
					map[string]any{
						"apiVersion": "apps/v1",
						"kind":       "StatefulSet",
						"name":       stsName,
						"uid":        stsUID,
					},
				},
			},
			"status": map[string]any{
				"phase": phase,
			},
		},
	}
}

func templateAndLivePodEntity(podU unstructured.Unstructured, templateUID string) entity.Entity {
	tpl := podU.DeepCopy()
	_ = unstructured.SetNestedField(tpl.Object, templateUID, "metadata", "uid")
	gvk := types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithResource(types.Resource("pods")).
		WithName(types.Name(podU.GetName())).
		WithNamespace(types.Namespace(podU.GetNamespace())).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyTemplateEntity, *tpl).
		WithUnstructured(types.KeyClusterEntity, podU))
}

// TestReconcileScaleDownPods_NoClusterMutation_EmptyPodList_NoPodActions lists pods once to detect
// terminating or stale app-associated pods; when none match, no WARN and no further work.
func TestReconcileScaleDownPods_NoClusterMutation_EmptyPodList_NoPodActions(t *testing.T) {
	dep := liveDeploymentEntity("default", "web", "uid-deploy", 0)
	entities, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	var listCalls int
	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		listCalls++
		return nil, nil
	}

	_, err = ReconcileScaleDownPods(
		context.Background(),
		log.Default(),
		fake.NewSimpleDynamicClient(runtime.NewScheme()),
		entities,
		types.KeyTemplateEntity,
		types.KeyClusterEntity,
		nil,
		false,
		types.ForceScaleDownNo,
		types.DryRunNo,
		listPods,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, listCalls)
}

func TestReconcileScaleDownPods_NoClusterMutation_TerminatingPod_WarnsWithoutForce(t *testing.T) {
	const deployUID = "uid-deploy-term"
	const rsUID = "uid-rs-term"
	const podUID = "uid-pod-term"

	dep := liveDeploymentEntity("default", "web", deployUID, 0)
	rs := liveReplicaSetEntity("default", "web-rs", rsUID, "web", deployUID)
	entities, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	podU := livePodUnstructuredTerminating("default", "web-pod-term", podUID, "Running", "web-rs", rsUID)
	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podU)

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podU}, nil
	}

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleDownPods(
			context.Background(),
			logger,
			dyn,
			entities,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			nil,
			false,
			types.ForceScaleDownNo,
			types.DryRunNo,
			listPods,
		)
	})
	require.NoError(t, runErr)

	assert.Contains(t, logs, "level=WARN")
	assert.Contains(t, logs, "still terminating")
	assert.Equal(t, 1, strings.Count(logs, "hint: use --force-scale-down"))

	_, getErr := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("default").
		Get(context.Background(), "web-pod-term", metav1.GetOptions{})
	assert.False(t, kerrors.IsNotFound(getErr))
}

func TestReconcileScaleDownPods_NoClusterMutation_TerminatingPod_ForceDeletesWithGraceZero(t *testing.T) {
	const deployUID = "uid-deploy-fterm"
	const rsUID = "uid-rs-fterm"
	const podUID = "uid-pod-fterm"

	dep := liveDeploymentEntity("default", "web", deployUID, 0)
	rs := liveReplicaSetEntity("default", "web-rs", rsUID, "web", deployUID)
	entities, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	podU := livePodUnstructuredTerminating("default", "web-pod-fterm", podUID, "Running", "web-rs", rsUID)
	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podU)

	var lastGrace *int64
	dyn.PrependReactor("delete", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if da, ok := action.(clienttesting.DeleteActionImpl); ok {
			opts := da.GetDeleteOptions()
			lastGrace = opts.GracePeriodSeconds
		}
		return false, nil, nil
	})

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podU}, nil
	}

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleDownPods(
			context.Background(),
			logger,
			dyn,
			entities,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			nil,
			false,
			types.ForceScaleDownYes,
			types.DryRunNo,
			listPods,
		)
	})
	require.NoError(t, runErr)

	require.NotNil(t, lastGrace, "delete should use explicit grace period when force-deleting")
	assert.Equal(t, int64(0), *lastGrace)
	assert.Contains(t, logs, "pod force-deleted", "force-delete path must log each deleted pod")

	_, getErr := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("default").
		Get(context.Background(), "web-pod-fterm", metav1.GetOptions{})
	assert.True(t, kerrors.IsNotFound(getErr))
}

func TestReconcileScaleDownPods_WithoutForce_WarnsAndHintsNoDeletes(t *testing.T) {
	const deployUID = "uid-deploy-1"
	const rsUID = "uid-rs-1"
	const podUID = "uid-pod-1"

	dep := liveDeploymentEntity("default", "web", deployUID, 0)
	rs := liveReplicaSetEntity("default", "web-rs", rsUID, "web", deployUID)
	entities, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	podU := livePodUnstructured("default", "web-pod", podUID, "Running", "web-rs", rsUID)
	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podU)

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podU}, nil
	}

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleDownPods(
			context.Background(),
			logger,
			dyn,
			entities,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			nil,
			true,
			types.ForceScaleDownNo,
			types.DryRunNo,
			listPods,
		)
	})
	require.NoError(t, runErr)

	assert.Contains(t, logs, "level=WARN", "app-associated stale pod message must be logged at WARN")
	assert.Contains(t, logs, "app-associated pod")
	assert.Equal(t, 1, strings.Count(logs, "hint: use --force-scale-down"))

	_, getErr := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("default").
		Get(context.Background(), "web-pod", metav1.GetOptions{})
	assert.False(t, kerrors.IsNotFound(getErr), "pod must not be deleted without --force-scale-down")
}

func TestReconcileScaleDownPods_WithoutForce_MultipleWarningsSingleHint(t *testing.T) {
	const deployUID = "uid-deploy-m"
	const rsUID = "uid-rs-m"
	podA := livePodUnstructured("default", "pod-a", "uid-a", "Running", "web-rs", rsUID)
	podB := livePodUnstructured("default", "pod-b", "uid-b", "Running", "web-rs", rsUID)

	dep := liveDeploymentEntity("default", "web", deployUID, 0)
	rs := liveReplicaSetEntity("default", "web-rs", rsUID, "web", deployUID)
	entities, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podA, &podB)

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podA, podB}, nil
	}

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleDownPods(
			context.Background(),
			logger,
			dyn,
			entities,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			nil,
			true,
			types.ForceScaleDownNo,
			types.DryRunNo,
			listPods,
		)
	})
	require.NoError(t, runErr)

	assert.GreaterOrEqual(t, strings.Count(logs, "app-associated pod"), 2)
	assert.Equal(t, 1, strings.Count(logs, "hint: use --force-scale-down"))
}

func TestReconcileScaleDownPods_WithForce_DeletesAppAssociatedPod(t *testing.T) {
	const deployUID = "uid-deploy-2"
	const rsUID = "uid-rs-2"
	const podUID = "uid-pod-2"

	dep := liveDeploymentEntity("default", "api", deployUID, 0)
	rs := liveReplicaSetEntity("default", "api-rs", rsUID, "api", deployUID)
	entities, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	podU := livePodUnstructured("default", "api-pod", podUID, "Running", "api-rs", rsUID)
	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podU)

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podU}, nil
	}

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleDownPods(
			context.Background(),
			logger,
			dyn,
			entities,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			nil,
			true,
			types.ForceScaleDownYes,
			types.DryRunNo,
			listPods,
		)
	})
	require.NoError(t, runErr)
	assert.Contains(t, logs, "pod force-deleted", "app-associated force-delete must log each pod")

	_, getErr := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("default").
		Get(context.Background(), "api-pod", metav1.GetOptions{})
	assert.True(t, kerrors.IsNotFound(getErr))
}

func TestReconcileScaleDownPods_TemplateDirectPodDeletedEvenWithoutForce(t *testing.T) {
	const deployUID = "uid-deploy-3"
	podU := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "static-hook",
				"namespace": "default",
				"uid":       "uid-hook",
			},
			"status": map[string]any{
				"phase": "Running",
			},
		},
	}
	// Template + live rows for the same Pod id (static manifest in chart).
	podEnt := templateAndLivePodEntity(podU, "uid-tpl-hook")
	dep := liveDeploymentEntity("default", "web", deployUID, 0)

	entities, err := entity.NewEntities([]entity.Entity{dep, podEnt})
	require.NoError(t, err)

	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podU)

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podU}, nil
	}

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleDownPods(
			context.Background(),
			logger,
			dyn,
			entities,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			nil,
			true,
			types.ForceScaleDownNo,
			types.DryRunNo,
			listPods,
		)
	})
	require.NoError(t, runErr)
	assert.Contains(t, logs, "pod deleted:", "template-direct pod delete must log each removed pod")

	_, getErr := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("default").
		Get(context.Background(), "static-hook", metav1.GetOptions{})
	assert.True(t, kerrors.IsNotFound(getErr))
}

// TestReconcileScaleDownPods_foreignUnlinkedPod_noWarnNoDelete ensures pods that are not linked to an in-graph
// workload (via owner chain) are ignored: no WARN and no delete even with --force-scale-down.
func TestReconcileScaleDownPods_foreignUnlinkedPod_noWarnNoDelete(t *testing.T) {
	const deployUID = "uid-deploy-foreign"
	const rsUID = "uid-rs-foreign"

	dep := liveDeploymentEntity("default", "web", deployUID, 0)
	rs := liveReplicaSetEntity("default", "web-rs", rsUID, "web", deployUID)
	ents, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	// Owner ReplicaSet is not present in entities — pod is not app-associated for this reconcile graph.
	foreign := livePodUnstructured("default", "foreign-pod", "uid-pod-foreign", "Running", "other-rs", "uid-other-rs")
	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &foreign)
	deleteCalls := 0
	dyn.PrependReactor("delete", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		deleteCalls++
		return true, nil, nil
	})

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{foreign}, nil
	}

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleDownPods(
			context.Background(),
			logger,
			dyn,
			ents,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			nil,
			true,
			types.ForceScaleDownYes,
			types.DryRunNo,
			listPods,
		)
	})
	require.NoError(t, runErr)

	assert.Zero(t, deleteCalls)
	assert.NotContains(t, logs, "level=WARN")
	assert.NotContains(t, logs, "app-associated")

	_, getErr := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("default").
		Get(context.Background(), "foreign-pod", metav1.GetOptions{})
	assert.False(t, kerrors.IsNotFound(getErr), "foreign pod must remain when not linked to app workloads")
}

// TestReconcileScaleDownPods_dryRun_forceScaleDown_ownerLinked_noDelete checks dry-run on the app-associated
// delete path: with --force-scale-down and DryRun, log intent but perform no Delete call.
func TestReconcileScaleDownPods_dryRun_forceScaleDown_ownerLinked_noDelete(t *testing.T) {
	const deployUID = "uid-deploy-dr-force"
	const rsUID = "uid-rs-dr-force"
	const podUID = "uid-pod-dr-force"

	dep := liveDeploymentEntity("default", "api", deployUID, 0)
	rs := liveReplicaSetEntity("default", "api-rs", rsUID, "api", deployUID)
	ents, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	podU := livePodUnstructured("default", "api-pod", podUID, "Running", "api-rs", rsUID)
	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podU)
	deleteCalls := 0
	dyn.PrependReactor("delete", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		deleteCalls++
		return true, nil, nil
	})

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podU}, nil
	}

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleDownPods(
			context.Background(),
			logger,
			dyn,
			ents,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			nil,
			true,
			types.ForceScaleDownYes,
			types.DryRunYes,
			listPods,
		)
	})
	require.NoError(t, runErr)

	assert.Zero(t, deleteCalls)
	assert.Contains(t, logs, "[dry-run] would force-delete app-associated pod")

	_, getErr := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("default").
		Get(context.Background(), "api-pod", metav1.GetOptions{})
	assert.False(t, kerrors.IsNotFound(getErr))
}

func TestReconcileScaleDownPods_WithForce_DeletesPodOwnedByRefLinkedStatefulSet(t *testing.T) {
	const pgUID = "uid-pg-ref"
	const stsUID = "uid-sts-ref"
	const podUID = "uid-pod-sts-ref"

	pgTpl := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "acid.zalan.do/v1",
		"kind":       "postgresql",
		"metadata": map[string]any{
			"name": "psql-demo", "namespace": "demo", "uid": pgUID,
		},
		"spec": map[string]any{"numberOfInstances": int64(0)},
	}}
	pgLive := pgTpl.DeepCopy()
	pgGvk := types.NewGVK(types.Group("acid.zalan.do"), types.Version("v1"), types.Kind("postgresql"))
	pg := mustBuild(entity.NewEntityBuilder().
		WithGVK(pgGvk).
		WithResource(types.Resource("postgresqls")).
		WithName(types.Name("psql-demo")).
		WithNamespace(types.Namespace("demo")).
		WithNamespaced(types.NamespacedYes).
		WithUnstructured(types.KeyTemplateEntity, pgTpl).
		WithUnstructured(types.KeyClusterEntity, *pgLive))

	stsLive := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata": map[string]any{
			"name": "psql-demo", "namespace": "demo", "uid": stsUID,
		},
		"spec": map[string]any{"replicas": int64(0)},
	}}
	stsGvk := types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("StatefulSet"))
	stsEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(stsGvk).
		WithResource(types.Resource("statefulsets")).
		WithName(types.Name("psql-demo")).
		WithNamespace(types.Namespace("demo")).
		WithNamespaced(types.NamespacedYes).
		WithUnstructured(types.KeyClusterEntity, stsLive))

	entities, err := entity.NewEntities([]entity.Entity{pg, stsEnt})
	require.NoError(t, err)
	pgID, err := pg.Id()
	require.NoError(t, err)
	stsID, err := stsEnt.Id()
	require.NoError(t, err)
	refs := []types.Ref{{
		RefType:      types.RefTypeDirect,
		EndpointType: types.RefEndpointTypeId,
		From:         pgID,
		To:           stsID,
	}}

	podU := livePodOwnedByStatefulSet("demo", "psql-demo-0", podUID, "Running", "psql-demo", stsUID)
	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podU)

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podU}, nil
	}

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleDownPods(
			context.Background(),
			logger,
			dyn,
			entities,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			refs,
			true,
			types.ForceScaleDownYes,
			types.DryRunNo,
			listPods,
		)
	})
	require.NoError(t, runErr)
	assert.Contains(t, logs, "pod force-deleted")

	_, getErr := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("demo").
		Get(context.Background(), "psql-demo-0", metav1.GetOptions{})
	assert.True(t, kerrors.IsNotFound(getErr))
}

func TestReconcileScaleDownPods_TransitiveOwnerChainCountsAsAppAssociated(t *testing.T) {
	const deployUID = "uid-deploy-chain"
	const rsUID = "uid-rs-chain"
	const podUID = "uid-pod-chain"

	dep := liveDeploymentEntity("apps", "shop", deployUID, 0)
	rs := liveReplicaSetEntity("apps", "shop-rs", rsUID, "shop", deployUID)
	entities, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	podU := livePodUnstructured("apps", "shop-pod", podUID, "Running", "shop-rs", rsUID)
	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podU)

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podU}, nil
	}

	_, err = ReconcileScaleDownPods(
		context.Background(),
		log.Default(),
		dyn,
		entities,
		types.KeyTemplateEntity,
		types.KeyClusterEntity,
		nil,
		true,
		types.ForceScaleDownYes,
		types.DryRunNo,
		listPods,
	)
	require.NoError(t, err)

	_, getErr := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("apps").
		Get(context.Background(), "shop-pod", metav1.GetOptions{})
	assert.True(t, kerrors.IsNotFound(getErr))
}

func TestRefreshAllPods_ListsCoreV1PodsAndMergesResult(t *testing.T) {
	ctx := context.Background()
	celExpr := `gvk == "v1/Pod"`

	podU := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "web",
				"namespace": "default",
				"uid":       "uid-before",
			},
		},
	}
	podEnt := templateAndLivePodEntity(podU, "tpl-uid")
	ents, err := entity.NewEntities([]entity.Entity{podEnt})
	require.NoError(t, err)

	refreshed := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "web",
				"namespace": "default",
				"uid":       "uid-after-list",
			},
			"status": map[string]any{"phase": "Running"},
		},
	}

	var sawResource schema.GroupVersionResource
	var sawNS string
	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			{Group: "", Version: "v1", Resource: "pods"}: "PodList",
		},
	)
	client.PrependReactor("list", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		impl, ok := a.(clienttesting.ListActionImpl)
		if !ok {
			t.Fatalf("expected ListActionImpl, got %T", a)
		}
		sawResource = impl.GetResource()
		sawNS = impl.GetNamespace()
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{refreshed}}, nil
	})

	out, err := RefreshAllPods(ctx, log.Default(), client, ents, celExpr, types.KeyClusterEntity)
	require.NoError(t, err)

	assert.Equal(t, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, sawResource)
	assert.Empty(t, sawNS, "refreshAllPods should load pods across all namespaces")

	id := types.Id("v1/Pod/default/web")
	got := out.EntityMap[id]
	u, err := got.UnstructuredOrError(types.KeyClusterEntity)
	require.NoError(t, err)
	assert.Equal(t, "uid-after-list", string(u.GetUID()))
	_, hasTpl := got.Unstructured(types.KeyTemplateEntity)
	assert.True(t, hasTpl)
}

func TestReconcileScaleDownPods_dryRun_skipsDeleteAndLogsDryRunPrefix(t *testing.T) {
	podU := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "static-hook",
				"namespace": "default",
				"uid":       "uid-hook",
			},
			"status": map[string]any{"phase": "Running"},
		},
	}
	podEnt := templateAndLivePodEntity(podU, "uid-tpl-hook")
	dep := liveDeploymentEntity("default", "web", "uid-deploy-dr", 0)
	ents, err := entity.NewEntities([]entity.Entity{dep, podEnt})
	require.NoError(t, err)

	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podU)
	deleteCalls := 0
	dyn.PrependReactor("delete", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		deleteCalls++
		return true, nil, nil
	})

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podU}, nil
	}

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleDownPods(
			context.Background(),
			logger,
			dyn,
			ents,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			nil,
			true,
			types.ForceScaleDownNo,
			types.DryRunYes,
			listPods,
		)
	})
	require.NoError(t, runErr)
	assert.Zero(t, deleteCalls)
	assert.Contains(t, logs, "[dry-run]")
	assert.Contains(t, logs, "would delete pod")
}

func TestReconcileScaleUpPods_dryRun_skipsCreateAndLogsDryRunPrefix(t *testing.T) {
	tpl := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "hook",
				"namespace": "default",
				"uid":       "tpl-only",
			},
			"spec": map[string]any{
				"containers": []any{map[string]any{"name": "c", "image": "pause:latest"}},
			},
		},
	}
	hookEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("hook")).
		WithNamespace(types.Namespace("default")).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyTemplateEntity, tpl))
	dep := liveDeploymentEntity("default", "web", "uid-dep-dr-up", 1)
	ents, err := entity.NewEntities([]entity.Entity{dep, hookEnt})
	require.NoError(t, err)

	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme())
	createCalls := 0
	dyn.PrependReactor("create", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		createCalls++
		return true, nil, nil
	})

	var runErr error
	logs := captureCommandLogs(t, func(logger log.Logger) {
		_, runErr = ReconcileScaleUpPods(
			context.Background(),
			logger,
			dyn,
			ents,
			types.KeyTemplateEntity,
			types.KeyClusterEntity,
			true,
			types.DryRunYes,
			func(context.Context) ([]unstructured.Unstructured, error) { return nil, nil },
		)
	})
	require.NoError(t, runErr)
	assert.Zero(t, createCalls)
	assert.Contains(t, logs, "[dry-run]")
	assert.Contains(t, logs, "would create pod")
}

func TestReconcileScaleUpPods_CreatesMissingTemplatePod(t *testing.T) {
	tpl := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "hook",
				"namespace": "default",
				"uid":       "tpl-uid",
			},
			"spec": map[string]any{
				"containers": []any{
					map[string]any{"name": "c", "image": "pause:latest"},
				},
			},
		},
	}
	hookEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("hook")).
		WithNamespace(types.Namespace("default")).
		WithNamespaced(types.NamespacedNo).
		WithUnstructured(types.KeyTemplateEntity, tpl))

	dep := liveDeploymentEntity("default", "web", "uid-dep-up", 1)
	entities, err := entity.NewEntities([]entity.Entity{dep, hookEnt})
	require.NoError(t, err)

	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme())
	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return nil, nil
	}

	_, err = ReconcileScaleUpPods(
		context.Background(),
		log.Default(),
		dyn,
		entities,
		types.KeyTemplateEntity,
		types.KeyClusterEntity,
		true,
		types.DryRunNo,
		listPods,
	)
	require.NoError(t, err)

	got, err := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("default").
		Get(context.Background(), "hook", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "hook", got.GetName())
}

func TestReconcileScaleUpPods_contract_templatePodWithLiveClusterNoExtraCreate(t *testing.T) {
	podU := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "hook",
				"namespace": "default",
				"uid":       "live-hook-uid",
			},
			"status": map[string]any{"phase": "Running"},
		},
	}
	hookEnt := templateAndLivePodEntity(podU, "tpl-hook-uid")
	_, hasTpl := hookEnt.Unstructured(types.KeyTemplateEntity)
	_, hasLive := hookEnt.Unstructured(types.KeyClusterEntity)
	require.True(t, hasTpl && hasLive, "fixture: same Pod id carries template and live unstructured")

	// After refresh/merge, ReconcileScaleUpPods must not issue Create for a v1/Pod that already has live data.
	needsTemplatePodCreate := hasTpl && !hasLive
	assert.False(t, needsTemplatePodCreate)

	scheme := runtime.NewScheme()
	client := fake.NewSimpleDynamicClient(scheme, &podU)
	createCount := 0
	client.PrependReactor("create", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		createCount++
		return true, nil, nil
	})

	dep := liveDeploymentEntity("default", "web", "uid-dep-live", 1)
	ents, err := entity.NewEntities([]entity.Entity{dep, hookEnt})
	require.NoError(t, err)

	_, err = ReconcileScaleUpPods(
		context.Background(),
		log.Default(),
		client,
		ents,
		types.KeyTemplateEntity,
		types.KeyClusterEntity,
		true,
		types.DryRunNo,
		func(context.Context) ([]unstructured.Unstructured, error) {
			return []unstructured.Unstructured{podU}, nil
		},
	)
	require.NoError(t, err)
	assert.Zero(t, createCount, "ReconcileScaleUpPods must not create a pod when template and live pod already share the same id")
}

func TestReconcileScaleUpPods_DoesNotDirectlyCreateReplicaSetOwnedPod(t *testing.T) {
	const deployUID = "uid-dep-no-create"
	const rsUID = "uid-rs-nc"
	const podUID = "uid-pod-nc"

	dep := liveDeploymentEntity("default", "app", deployUID, 1)
	rs := liveReplicaSetEntity("default", "app-rs", rsUID, "app", deployUID)
	entities, err := entity.NewEntities([]entity.Entity{dep, rs})
	require.NoError(t, err)

	podU := livePodUnstructured("default", "app-pod", podUID, "Running", "app-rs", rsUID)
	dyn := fake.NewSimpleDynamicClient(runtime.NewScheme(), &podU)

	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		return []unstructured.Unstructured{podU}, nil
	}

	_, err = ReconcileScaleUpPods(
		context.Background(),
		log.Default(),
		dyn,
		entities,
		types.KeyTemplateEntity,
		types.KeyClusterEntity,
		true,
		types.DryRunNo,
		listPods,
	)
	require.NoError(t, err)

	// Only the listed pod should exist — no second pod created from a non-existent template row.
	list, err := dyn.Resource(types.NewGVR("", "v1", "pods").K8s()).
		Namespace("default").
		List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, list.Items, 1)
	assert.Equal(t, "app-pod", list.Items[0].GetName())
}

func TestReconcileScaleUpPods_NoClusterMutation_SkipsListPods(t *testing.T) {
	dep := liveDeploymentEntity("default", "web", "uid-skip", 1)
	entities, err := entity.NewEntities([]entity.Entity{dep})
	require.NoError(t, err)

	var listCalls int
	listPods := func(context.Context) ([]unstructured.Unstructured, error) {
		listCalls++
		return nil, nil
	}

	_, err = ReconcileScaleUpPods(
		context.Background(),
		log.Default(),
		fake.NewSimpleDynamicClient(runtime.NewScheme()),
		entities,
		types.KeyTemplateEntity,
		types.KeyClusterEntity,
		false,
		types.DryRunNo,
		listPods,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, listCalls)
}

// TestReconcileScaleHooks_signatureMatchesCLIContractDoc documents parity with cli/action/cluster_scale_pod_reconcile_contract_test.go hook types.
func TestReconcileScaleHooks_signatureMatchesCLIContractDoc(t *testing.T) {
	type scaleDownContract = func(
		ctx context.Context,
		l log.Logger,
		dyn dynamic.Interface,
		entities entity.Entities,
		templateKey types.EntityKeyUnstructured,
		liveKey types.EntityKeyUnstructured,
		refs []types.Ref,
		clusterMutated bool,
		forceScaleDown types.ForceScaleDown,
		dryRun types.DryRun,
		listPods func(context.Context) ([]unstructured.Unstructured, error),
	) (entity.Entities, error)
	var down scaleDownContract = ReconcileScaleDownPods
	_ = down

	type scaleUpContract = func(
		ctx context.Context,
		l log.Logger,
		dyn dynamic.Interface,
		entities entity.Entities,
		templateKey types.EntityKeyUnstructured,
		liveKey types.EntityKeyUnstructured,
		clusterMutated bool,
		dryRun types.DryRun,
		listPods func(context.Context) ([]unstructured.Unstructured, error),
	) (entity.Entities, error)
	var up scaleUpContract = ReconcileScaleUpPods
	_ = up
}
