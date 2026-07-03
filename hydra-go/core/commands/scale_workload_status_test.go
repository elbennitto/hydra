package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func mergedDeploymentEntity(namespace, name string, templateReplicas, liveReplicas int64) (entity.Entity, error) {
	tpl := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", namespace, name, templateReplicas, types.KeyTemplateEntity)
	live := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", namespace, name, liveReplicas, types.KeyClusterEntity)
	tplU, err := tpl.UnstructuredOrError(types.KeyTemplateEntity)
	if err != nil {
		return entity.Entity{}, err
	}
	liveU, err := live.UnstructuredOrError(types.KeyClusterEntity)
	if err != nil {
		return entity.Entity{}, err
	}
	b := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))).
		WithResource(types.Resource("deployments")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace))
	return mustBuild(b.WithUnstructured(types.KeyTemplateEntity, tplU).WithUnstructured(types.KeyClusterEntity, liveU)), nil
}

func TestLiveMatchesTemplateScaleState_Deployment(t *testing.T) {
	target := ScaleTarget{
		Id:       types.Id("apps/v1/Deployment/default/web"),
		Name:     types.Name("web"),
		Ns:       types.Namespace("default"),
		GVR:      types.NewGVR("apps", "v1", "deployments"),
		GVK:      types.KubernetesGvkAppsV1Deployment,
		Replicas: 3,
	}
	live := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web", 3, types.KeyClusterEntity)
	u, err := live.UnstructuredOrError(types.KeyClusterEntity)
	require.NoError(t, err)
	assert.True(t, liveMatchesTemplateScaleState(target, &u))
	u2 := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web", 1, types.KeyClusterEntity)
	l2, err := u2.UnstructuredOrError(types.KeyClusterEntity)
	require.NoError(t, err)
	assert.False(t, liveMatchesTemplateScaleState(target, &l2))
}

func TestLiveMatchesScaledDownState_Deployment(t *testing.T) {
	target := ScaleTarget{
		Id:       types.Id("apps/v1/Deployment/default/web"),
		Name:     types.Name("web"),
		Ns:       types.Namespace("default"),
		GVR:      types.NewGVR("apps", "v1", "deployments"),
		GVK:      types.KubernetesGvkAppsV1Deployment,
		Replicas: 3,
	}
	live0 := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web", 0, types.KeyClusterEntity)
	u, err := live0.UnstructuredOrError(types.KeyClusterEntity)
	require.NoError(t, err)
	assert.True(t, liveMatchesScaledDownState(target, &u))
	liveUp := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web", 2, types.KeyClusterEntity)
	u2, err := liveUp.UnstructuredOrError(types.KeyClusterEntity)
	require.NoError(t, err)
	assert.False(t, liveMatchesScaledDownState(target, &u2))
}

func TestClassifyWorkloadScaleSyncState_PriorityTemplateOverDown(t *testing.T) {
	target := ScaleTarget{
		Id:       types.Id("apps/v1/Deployment/default/web"),
		Name:     types.Name("web"),
		Ns:       types.Namespace("default"),
		GVR:      types.NewGVR("apps", "v1", "deployments"),
		GVK:      types.KubernetesGvkAppsV1Deployment,
		Replicas: 0,
	}
	tpl := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web", 0, types.KeyTemplateEntity)
	live := buildWorkloadEntity("apps", "v1", "Deployment", "deployments", "default", "web", 0, types.KeyClusterEntity)
	tplU, err := tpl.UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	liveU, err := live.UnstructuredOrError(types.KeyClusterEntity)
	require.NoError(t, err)
	b := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("apps"), types.Version("v1"), types.Kind("Deployment"))).
		WithResource(types.Resource("deployments")).
		WithName(types.Name("web")).
		WithNamespace(types.Namespace("default"))
	e := mustBuild(b.WithUnstructured(types.KeyTemplateEntity, tplU).WithUnstructured(types.KeyClusterEntity, liveU))
	assert.Equal(t, ClusterScaleWorkloadStateUp, classifyWorkloadScaleSyncState(target, e, types.KeyTemplateEntity, types.KeyClusterEntity))
}

func TestFilterClusterScaleStatusReportOmitFullyHealthyApps(t *testing.T) {
	goodApp := types.AppId("good.app")
	badApp := types.AppId("bad.app")
	report := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{AppId: goodApp, WorkloadId: types.Id("w1"), State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady},
			{AppId: badApp, WorkloadId: types.Id("w2"), State: ClusterScaleWorkloadStateDown},
		},
	}
	filtered := FilterClusterScaleStatusReportOmitFullyHealthyApps(report, false)
	require.Len(t, filtered.Workloads, 1)
	assert.Equal(t, badApp, filtered.Workloads[0].AppId)

	all := FilterClusterScaleStatusReportOmitFullyHealthyApps(report, true)
	require.Len(t, all.Workloads, 2)

	reportDep := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{
				AppId: goodApp, WorkloadId: types.Id("w1"), State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady,
				Dependencies: []ClusterScaleWorkloadDependencyStatus{
					{WorkloadId: types.Id("dep"), State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyNotReady},
				},
			},
		},
	}
	require.Len(t, FilterClusterScaleStatusReportOmitFullyHealthyApps(reportDep, false).Workloads, 1)

	reportMixedRoots := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{AppId: goodApp, WorkloadId: types.Id("a"), State: ClusterScaleWorkloadStateUp, Ready: ""},
			{AppId: goodApp, WorkloadId: types.Id("b"), State: ClusterScaleWorkloadStateOutOfSync},
		},
	}
	// Per-workload filter: healthy "a" omitted, "b" still shown.
	mixedFiltered := FilterClusterScaleStatusReportOmitFullyHealthyApps(reportMixedRoots, false)
	require.Len(t, mixedFiltered.Workloads, 1)
	assert.Equal(t, types.Id("b"), mixedFiltered.Workloads[0].WorkloadId)

	reportAllOK := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{AppId: goodApp, WorkloadId: types.Id("a"), State: ClusterScaleWorkloadStateUp, Ready: ""},
			{AppId: goodApp, WorkloadId: types.Id("b"), State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady},
		},
	}
	require.Len(t, FilterClusterScaleStatusReportOmitFullyHealthyApps(reportAllOK, false).Workloads, 0)

	reportOKJob := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{
				AppId:      goodApp,
				WorkloadId: types.Id("batch/v1/Job/ns/init"),
				State:      ClusterScaleWorkloadStateOK,
				Ready:      "",
				Dependencies: []ClusterScaleWorkloadDependencyStatus{
					{WorkloadId: types.Id("v1/Secret/ns/out"), State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady, RefRole: ScaleDependencyRefRoleProduces},
					{WorkloadId: types.Id("v1/Secret/ns/in"), State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady, RefRole: ScaleDependencyRefRolePrerequisite},
				},
			},
		},
	}
	require.Len(t, FilterClusterScaleStatusReportOmitFullyHealthyApps(reportOKJob, false).Workloads, 0)
	require.Len(t, FilterClusterScaleStatusReportOmitFullyHealthyApps(reportOKJob, true).Workloads, 1)

	reportMissingDepsSatisfied := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{
				AppId:      badApp,
				WorkloadId: types.Id("batch/v1/Job/ns/gone"),
				State:      ClusterScaleWorkloadStateMissing,
				Dependencies: []ClusterScaleWorkloadDependencyStatus{
					{WorkloadId: types.Id("v1/Secret/ns/x"), State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady},
				},
			},
		},
	}
	require.Len(t, FilterClusterScaleStatusReportOmitFullyHealthyApps(reportMissingDepsSatisfied, false).Workloads, 0)
	require.Len(t, FilterClusterScaleStatusReportOmitFullyHealthyApps(reportMissingDepsSatisfied, true).Workloads, 1)

	reportMissingNoDeps := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{AppId: badApp, WorkloadId: types.Id("batch/v1/Job/ns/none"), State: ClusterScaleWorkloadStateMissing},
		},
	}
	require.Len(t, FilterClusterScaleStatusReportOmitFullyHealthyApps(reportMissingNoDeps, false).Workloads, 1)

	reportMissingDepNotReady := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{
				AppId:      badApp,
				WorkloadId: types.Id("batch/v1/Job/ns/bad"),
				State:      ClusterScaleWorkloadStateMissing,
				Dependencies: []ClusterScaleWorkloadDependencyStatus{
					{WorkloadId: types.Id("v1/Secret/ns/y"), State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyNotReady},
				},
			},
		},
	}
	require.Len(t, FilterClusterScaleStatusReportOmitFullyHealthyApps(reportMissingDepNotReady, false).Workloads, 1)
}

func TestClusterScaleStatusAllTargetsOmittedAsHealthy(t *testing.T) {
	goodApp := types.AppId("good.app")
	full := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{AppId: goodApp, WorkloadId: types.Id("w1"), State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady},
		},
	}
	filtered := FilterClusterScaleStatusReportOmitFullyHealthyApps(full, false)
	require.True(t, ClusterScaleStatusAllTargetsOmittedAsHealthy(filtered, full, false))

	assert.False(t, ClusterScaleStatusAllTargetsOmittedAsHealthy(filtered, full, true),
		"--all keeps rows so filtered equals full and condition should be false")
	assert.False(t, ClusterScaleStatusAllTargetsOmittedAsHealthy(ClusterScaleStatusReport{}, ClusterScaleStatusReport{}, false),
		"empty full report: nothing to summarize as all healthy")

	fullTwo := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{AppId: goodApp, WorkloadId: types.Id("w1"), State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady},
			{AppId: goodApp, WorkloadId: types.Id("w2"), State: ClusterScaleWorkloadStateDown},
		},
	}
	partial := FilterClusterScaleStatusReportOmitFullyHealthyApps(fullTwo, false)
	require.Len(t, partial.Workloads, 1)
	assert.False(t, ClusterScaleStatusAllTargetsOmittedAsHealthy(partial, fullTwo, false))
}

func TestComputeClusterScaleWorkloadStatusReport_UpDownOutOfSync(t *testing.T) {
	webMerged, err := mergedDeploymentEntity("default", "web", 3, 3)
	require.NoError(t, err)
	apiMerged, err := mergedDeploymentEntity("default", "api", 2, 0)
	require.NoError(t, err)
	midMerged, err := mergedDeploymentEntity("default", "mid", 3, 1)
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{webMerged, apiMerged, midMerged})
	require.NoError(t, err)

	report, err := ComputeClusterScaleWorkloadStatusReport(ents, nil, nil, types.KeyTemplateEntity, types.KeyClusterEntity, nil)
	require.NoError(t, err)
	require.Len(t, report.Workloads, 3)

	byID := make(map[types.Id]ClusterScaleWorkloadStatus)
	for _, w := range report.Workloads {
		byID[w.WorkloadId] = w
	}
	assert.Equal(t, ClusterScaleWorkloadStateUp, byID[types.Id("apps/v1/Deployment/default/web")].State)
	assert.Equal(t, ClusterScaleWorkloadStateDown, byID[types.Id("apps/v1/Deployment/default/api")].State)
	assert.Equal(t, ClusterScaleWorkloadStateOutOfSync, byID[types.Id("apps/v1/Deployment/default/mid")].State)
}

func TestComputeClusterScaleWorkloadStatusReport_WorkloadDependencies(t *testing.T) {
	backendMerged, err := mergedDeploymentEntity("default", "backend", 2, 2)
	require.NoError(t, err)
	dbMerged, err := mergedDeploymentEntity("default", "database", 1, 1)
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{backendMerged, dbMerged})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/default/backend", To: "apps/v1/Deployment/default/database"},
	}

	report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, nil)
	require.NoError(t, err)
	require.Len(t, report.Workloads, 2)

	var backendStatus *ClusterScaleWorkloadStatus
	for i := range report.Workloads {
		if report.Workloads[i].WorkloadId == types.Id("apps/v1/Deployment/default/backend") {
			backendStatus = &report.Workloads[i]
			break
		}
	}
	require.NotNil(t, backendStatus)
	require.Len(t, backendStatus.Dependencies, 1)
	assert.Equal(t, types.Id("apps/v1/Deployment/default/database"), backendStatus.Dependencies[0].WorkloadId)
	assert.Equal(t, ClusterScaleWorkloadStateUp, backendStatus.Dependencies[0].State)
	assert.False(t, backendStatus.Dependencies[0].Optional)
	assert.Empty(t, backendStatus.Dependencies[0].RefRole)
}

func TestComputeClusterScaleWorkloadStatusReport_DependencyRefRoleFromLabels(t *testing.T) {
	backendMerged, err := mergedDeploymentEntity("default", "backend", 2, 2)
	require.NoError(t, err)
	dbMerged, err := mergedDeploymentEntity("default", "database", 1, 1)
	require.NoError(t, err)
	secPullTpl := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "pull",
				"namespace": "default",
			},
		},
	}
	secRedisTpl := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "redis",
				"namespace": "default",
			},
		},
	}
	secPullEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))).
		WithResource(types.Resource("secrets")).
		WithName(types.Name("pull")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyTemplateEntity, secPullTpl))
	secRedisEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))).
		WithResource(types.Resource("secrets")).
		WithName(types.Name("redis")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyTemplateEntity, secRedisTpl))

	ents, err := entity.NewEntities([]entity.Entity{backendMerged, dbMerged, secPullEnt, secRedisEnt})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/default/backend", To: "apps/v1/Deployment/default/database"},
		{From: "apps/v1/Deployment/default/backend", To: "v1/Secret/default/pull", Labels: []string{"imagePullSecret"}},
		{From: "apps/v1/Deployment/default/backend", To: "v1/Secret/default/redis", Labels: []string{"source"}},
	}
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, re)
	require.NoError(t, err)

	var backendStatus *ClusterScaleWorkloadStatus
	for i := range report.Workloads {
		if report.Workloads[i].WorkloadId == types.Id("apps/v1/Deployment/default/backend") {
			backendStatus = &report.Workloads[i]
			break
		}
	}
	require.NotNil(t, backendStatus)
	require.Len(t, backendStatus.Dependencies, 3)
	byDep := make(map[types.Id]ClusterScaleWorkloadDependencyStatus)
	for _, d := range backendStatus.Dependencies {
		byDep[d.WorkloadId] = d
	}
	assert.Equal(t, ScaleDependencyRefRolePrerequisite, byDep[types.Id("v1/Secret/default/pull")].RefRole)
	assert.Equal(t, ScaleDependencyRefRoleProduces, byDep[types.Id("v1/Secret/default/redis")].RefRole)
	assert.Equal(t, ScaleDependencyRefRoleUnspecified, byDep[types.Id("apps/v1/Deployment/default/database")].RefRole)
	// out (produces) first, then in (prerequisite), then unclassified — by id within each band
	assert.Equal(t, types.Id("v1/Secret/default/redis"), backendStatus.Dependencies[0].WorkloadId)
	assert.Equal(t, types.Id("v1/Secret/default/pull"), backendStatus.Dependencies[1].WorkloadId)
	assert.Equal(t, types.Id("apps/v1/Deployment/default/database"), backendStatus.Dependencies[2].WorkloadId)
}

func TestComputeClusterScaleWorkloadStatusReport_SecretDependencyMissingSyncState(t *testing.T) {
	webMerged, err := mergedDeploymentEntity("default", "web", 1, 1)
	require.NoError(t, err)
	secTpl := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "db-creds",
				"namespace": "default",
			},
		},
	}
	secEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))).
		WithResource(types.Resource("secrets")).
		WithName(types.Name("db-creds")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyTemplateEntity, secTpl))

	ents, err := entity.NewEntities([]entity.Entity{webMerged, secEnt})
	require.NoError(t, err)
	refs := []types.Ref{
		{From: "apps/v1/Deployment/default/web", To: "v1/Secret/default/db-creds"},
	}
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, re)
	require.NoError(t, err)
	var webStatus *ClusterScaleWorkloadStatus
	for i := range report.Workloads {
		if report.Workloads[i].WorkloadId == types.Id("apps/v1/Deployment/default/web") {
			webStatus = &report.Workloads[i]
			break
		}
	}
	require.NotNil(t, webStatus)
	require.Len(t, webStatus.Dependencies, 1)
	assert.Equal(t, types.Id("v1/Secret/default/db-creds"), webStatus.Dependencies[0].WorkloadId)
	assert.Equal(t, ClusterScaleWorkloadStateMissing, webStatus.Dependencies[0].State)
	assert.Empty(t, webStatus.Dependencies[0].Ready)
}

func TestComputeClusterScaleWorkloadStatusReport_PodDependencies(t *testing.T) {
	webMerged, err := mergedDeploymentEntity("default", "web", 1, 1)
	require.NoError(t, err)
	podLive := unstructured.Unstructured{}
	podLive.SetAPIVersion("v1")
	podLive.SetKind("Pod")
	podLive.SetNamespace("default")
	podLive.SetName("sidecar")
	podEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("sidecar")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, podLive))

	ents, err := entity.NewEntities([]entity.Entity{webMerged, podEnt})
	require.NoError(t, err)

	refs := []types.Ref{
		{From: "apps/v1/Deployment/default/web", To: "v1/Pod/default/sidecar"},
	}

	report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, nil)
	require.NoError(t, err)

	var webStatus *ClusterScaleWorkloadStatus
	for i := range report.Workloads {
		if report.Workloads[i].WorkloadId == types.Id("apps/v1/Deployment/default/web") {
			webStatus = &report.Workloads[i]
			break
		}
	}
	require.NotNil(t, webStatus)
	require.Len(t, webStatus.Dependencies, 1)
	assert.Equal(t, types.Id("v1/Pod/default/sidecar"), webStatus.Dependencies[0].WorkloadId)
	assert.Equal(t, ClusterScaleWorkloadStateUp, webStatus.Dependencies[0].State)
}

func TestComputeClusterScaleWorkloadStatusReport_PodDependencyReadyWithEvaluator(t *testing.T) {
	webMerged, err := mergedDeploymentEntity("default", "web", 1, 1)
	require.NoError(t, err)
	podLive := unstructured.Unstructured{}
	podLive.SetAPIVersion("v1")
	podLive.SetKind("Pod")
	podLive.SetNamespace("default")
	podLive.SetName("sidecar")
	require.NoError(t, unstructured.SetNestedField(podLive.Object, "Running", "status", "phase"))
	require.NoError(t, unstructured.SetNestedSlice(podLive.Object, []interface{}{
		map[string]interface{}{"type": "Ready", "status": "True"},
	}, "status", "conditions"))
	podEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("sidecar")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, podLive))

	ents, err := entity.NewEntities([]entity.Entity{webMerged, podEnt})
	require.NoError(t, err)
	refs := []types.Ref{
		{From: "apps/v1/Deployment/default/web", To: "v1/Pod/default/sidecar"},
	}
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, re)
	require.NoError(t, err)

	var webStatus *ClusterScaleWorkloadStatus
	for i := range report.Workloads {
		if report.Workloads[i].WorkloadId == types.Id("apps/v1/Deployment/default/web") {
			webStatus = &report.Workloads[i]
			break
		}
	}
	require.NotNil(t, webStatus)
	require.Len(t, webStatus.Dependencies, 1)
	assert.Equal(t, ClusterScaleReadyReady, webStatus.Dependencies[0].Ready)
}

func TestClassifyDependencySyncState_NonScaleTargetClusterOnly(t *testing.T) {
	podLive := unstructured.Unstructured{}
	podLive.SetAPIVersion("v1")
	podLive.SetKind("Pod")
	podLive.SetNamespace("default")
	podLive.SetName("p1")
	podEnt := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("p1")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyClusterEntity, podLive))

	deployTarget := ScaleTarget{Id: types.Id("apps/v1/Deployment/default/web")}
	assert.Equal(t, ClusterScaleWorkloadStateUp, classifyDependencySyncState([]ScaleTarget{deployTarget}, types.Id("v1/Pod/default/p1"), podEnt, types.KeyTemplateEntity, types.KeyClusterEntity))

	podTplOnly := mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
		WithResource(types.Resource("pods")).
		WithName(types.Name("p2")).
		WithNamespace(types.Namespace("default")).
		WithUnstructured(types.KeyTemplateEntity, podLive))
	assert.Equal(t, ClusterScaleWorkloadStateDown, classifyDependencySyncState([]ScaleTarget{deployTarget}, types.Id("v1/Pod/default/p2"), podTplOnly, types.KeyTemplateEntity, types.KeyClusterEntity))
}

func jobEntityTemplateOnly(namespace, name string, ttlSeconds *int64) entity.Entity {
	spec := map[string]any{
		"template": map[string]any{
			"spec": map[string]any{
				"containers": []any{
					map[string]any{
						"name":    "c",
						"command": []any{"argocd", "admin", "redis-initial-password"},
					},
				},
			},
		},
	}
	if ttlSeconds != nil {
		spec["ttlSecondsAfterFinished"] = *ttlSeconds
	}
	obj := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": spec,
	}
	u := unstructured.Unstructured{Object: obj}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("batch"), types.Version("v1"), types.Kind("Job"))).
		WithResource(types.Resource("jobs")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithUnstructured(types.KeyTemplateEntity, u))
}

func TestClassifyWorkloadScaleSyncState_JobMissingLiveWithTTLIsMissing(t *testing.T) {
	ttl := int64(60)
	e := jobEntityTemplateOnly("argocd", "argocd-redis-secret-init", &ttl)
	target := ScaleTarget{
		Id:    types.Id("batch/v1/Job/argocd/argocd-redis-secret-init"),
		IsJob: true,
	}
	assert.Equal(t, ClusterScaleWorkloadStateMissing, classifyWorkloadScaleSyncState(target, e, types.KeyTemplateEntity, types.KeyClusterEntity))
}

func TestClassifyWorkloadScaleSyncState_JobMissingLiveWithoutTTLIsMissing(t *testing.T) {
	e := jobEntityTemplateOnly("argocd", "some-job", nil)
	target := ScaleTarget{
		Id:    types.Id("batch/v1/Job/argocd/some-job"),
		IsJob: true,
	}
	assert.Equal(t, ClusterScaleWorkloadStateMissing, classifyWorkloadScaleSyncState(target, e, types.KeyTemplateEntity, types.KeyClusterEntity))
}

func mergedSecretEntityForScaleStatus(namespace, name string) (entity.Entity, error) {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
	}
	u := unstructured.Unstructured{Object: obj}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Secret"))).
		WithResource(types.Resource("secrets")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithUnstructured(types.KeyTemplateEntity, u).
		WithUnstructured(types.KeyClusterEntity, u)), nil
}

func TestUpgradeMissingJobToOKIfDepsSatisfied(t *testing.T) {
	jobT := ScaleTarget{IsJob: true}
	assert.Equal(t, ClusterScaleWorkloadStateMissing, upgradeMissingJobToOKIfDepsSatisfied(jobT, ClusterScaleWorkloadStateMissing, nil))
	deps := []ClusterScaleWorkloadDependencyStatus{
		{State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady, RefRole: ScaleDependencyRefRolePrerequisite},
	}
	assert.Equal(t, ClusterScaleWorkloadStateMissing, upgradeMissingJobToOKIfDepsSatisfied(jobT, ClusterScaleWorkloadStateMissing, deps))
	depsOut := []ClusterScaleWorkloadDependencyStatus{
		{State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady, RefRole: ScaleDependencyRefRoleProduces},
		{State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady, RefRole: ScaleDependencyRefRolePrerequisite},
	}
	assert.Equal(t, ClusterScaleWorkloadStateOK, upgradeMissingJobToOKIfDepsSatisfied(jobT, ClusterScaleWorkloadStateMissing, depsOut))
	depsBad := []ClusterScaleWorkloadDependencyStatus{
		{State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyReady, RefRole: ScaleDependencyRefRoleProduces},
		{State: ClusterScaleWorkloadStateUp, Ready: ClusterScaleReadyNotReady, RefRole: ScaleDependencyRefRolePrerequisite},
	}
	assert.Equal(t, ClusterScaleWorkloadStateMissing, upgradeMissingJobToOKIfDepsSatisfied(jobT, ClusterScaleWorkloadStateMissing, depsBad))
}

func TestComputeClusterScaleWorkloadStatusReport_MissingJobBecomesOKWhenOutDepsHealthy(t *testing.T) {
	ttl := int64(60)
	jobEnt := jobEntityTemplateOnly("argocd", "argocd-redis-secret-init", &ttl)
	redisSec, err := mergedSecretEntityForScaleStatus("argocd", "argocd-redis")
	require.NoError(t, err)
	pullSec, err := mergedSecretEntityForScaleStatus("argocd", "image-pull-secret")
	require.NoError(t, err)
	ents, err := entity.NewEntities([]entity.Entity{jobEnt, redisSec, pullSec})
	require.NoError(t, err)
	jobID := types.Id("batch/v1/Job/argocd/argocd-redis-secret-init")
	refs := []types.Ref{
		{From: jobID, To: "v1/Secret/argocd/argocd-redis", Labels: []string{"source"}},
		{From: jobID, To: "v1/Secret/argocd/image-pull-secret", Labels: []string{"imagePullSecret"}},
	}
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), ents, types.KeyClusterEntity)
	require.NoError(t, err)

	report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, re)
	require.NoError(t, err)
	require.Len(t, report.Workloads, 1)
	assert.Equal(t, ClusterScaleWorkloadStateOK, report.Workloads[0].State)
	assert.Empty(t, report.Workloads[0].Ready)
	require.Len(t, report.Workloads[0].Dependencies, 2)
}

func TestComputeClusterScaleWorkloadStatusReport_TTLJobMissingLiveSkipsReady(t *testing.T) {
	ttl := int64(60)
	jobEnt := jobEntityTemplateOnly("argocd", "argocd-redis-secret-init", &ttl)
	ents, err := entity.NewEntities([]entity.Entity{jobEnt})
	require.NoError(t, err)
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	report, err := ComputeClusterScaleWorkloadStatusReport(ents, nil, nil, types.KeyTemplateEntity, types.KeyClusterEntity, re)
	require.NoError(t, err)
	require.Len(t, report.Workloads, 1)
	assert.Equal(t, ClusterScaleWorkloadStateMissing, report.Workloads[0].State)
	assert.Empty(t, report.Workloads[0].Ready)
	assert.Empty(t, report.Workloads[0].ReadyMessages)
}

func TestComputeClusterScaleWorkloadStatusReport_JobMissingLiveWithoutTTLSkipsReady(t *testing.T) {
	jobEnt := jobEntityTemplateOnly("demo", "service-monitor-init-migrations", nil)
	ents, err := entity.NewEntities([]entity.Entity{jobEnt})
	require.NoError(t, err)
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	report, err := ComputeClusterScaleWorkloadStatusReport(ents, nil, nil, types.KeyTemplateEntity, types.KeyClusterEntity, re)
	require.NoError(t, err)
	require.Len(t, report.Workloads, 1)
	assert.Equal(t, ClusterScaleWorkloadStateMissing, report.Workloads[0].State)
	assert.Empty(t, report.Workloads[0].Ready)
	assert.Empty(t, report.Workloads[0].ReadyMessages)
}

func TestWriteClusterScaleStatusText_WithReadyMessages(t *testing.T) {
	report := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{
				WorkloadId:    types.Id("batch/v1/Job/default/migrate"),
				State:         ClusterScaleWorkloadStateUp,
				Ready:         ClusterScaleReadyNotReady,
				ReadyMessages: []string{"job not complete"},
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteClusterScaleStatusText(&buf, report, false))
	out := buf.String()
	assert.Contains(t, out, "not_ready")
	assert.Contains(t, out, "  - job not complete")
}

func TestWriteClusterScaleStatusText_Plain(t *testing.T) {
	report := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{
				WorkloadId: types.Id("apps/v1/Deployment/default/web"),
				State:      ClusterScaleWorkloadStateUp,
				Ready:      ClusterScaleReadyReady,
				Dependencies: []ClusterScaleWorkloadDependencyStatus{
					{WorkloadId: types.Id("apps/v1/Deployment/default/db"), State: ClusterScaleWorkloadStateDown, Optional: true, Ready: ClusterScaleReadyNotReady},
				},
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteClusterScaleStatusText(&buf, report, false))
	out := buf.String()
	assert.Contains(t, out, "apps/v1/Deployment/default/web")
	assert.Contains(t, out, "up")
	assert.Contains(t, out, "ready")
	assert.Contains(t, out, "not_ready")
	assert.Contains(t, out, "optional")
	assert.Contains(t, out, "down")
	assert.NotContains(t, strings.Split(out, "\n")[0], "\033[")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 3, "app header, workload row, dependency row before trailing blank from writer")
	assert.Equal(t, "(no app)", lines[0])
	f0 := strings.Fields(lines[1])
	require.GreaterOrEqual(t, len(f0), 3)
	assert.Equal(t, "ready", f0[len(f0)-1])
	assert.Equal(t, "up", f0[len(f0)-2])
	assert.Equal(t, "apps/v1/Deployment/default/web", f0[0])
	f1 := strings.Fields(lines[2])
	assert.Contains(t, f1, "optional")
	assert.Contains(t, f1, "down")
	assert.Contains(t, f1, "not_ready")
	assert.Contains(t, lines[2], "    apps/v1/Deployment/default/db")
}

func TestWriteClusterScaleStatusText_RefFlowTagPlain(t *testing.T) {
	report := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{
				WorkloadId: types.Id("batch/v1/Job/default/init"),
				State:      ClusterScaleWorkloadStateUp,
				Dependencies: []ClusterScaleWorkloadDependencyStatus{
					{WorkloadId: types.Id("v1/Secret/default/pull"), State: ClusterScaleWorkloadStateUp, RefRole: ScaleDependencyRefRolePrerequisite},
					{WorkloadId: types.Id("v1/Secret/default/redis"), State: ClusterScaleWorkloadStateUp, RefRole: ScaleDependencyRefRoleProduces},
				},
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteClusterScaleStatusText(&buf, report, false))
	out := buf.String()
	assert.Contains(t, out, "in v1/Secret/default/pull")
	assert.Contains(t, out, "out v1/Secret/default/redis")
	assert.NotContains(t, out, "[prerequisite]")
}

func TestWriteClusterScaleStatusText_OKStateGreen(t *testing.T) {
	report := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{WorkloadId: types.Id("batch/v1/Job/ns/init"), State: ClusterScaleWorkloadStateOK},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteClusterScaleStatusText(&buf, report, types.ColorYes))
	out := buf.String()
	assert.Contains(t, out, colors.Green.String()+"ok"+colors.Reset.String())
}

func TestWriteClusterScaleStatusText_RefFlowTagUsesLightBlue(t *testing.T) {
	report := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{
				WorkloadId: types.Id("batch/v1/Job/default/init"),
				State:      ClusterScaleWorkloadStateUp,
				Dependencies: []ClusterScaleWorkloadDependencyStatus{
					{WorkloadId: types.Id("v1/Secret/default/pull"), State: ClusterScaleWorkloadStateUp, RefRole: ScaleDependencyRefRolePrerequisite},
				},
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteClusterScaleStatusText(&buf, report, types.ColorYes))
	out := buf.String()
	assert.Contains(t, out, colors.LightBlue.String()+"in"+colors.Reset.String())
	assert.Contains(t, out, "v1/Secret/default/pull")
}

func TestWriteClusterScaleStatusText_ColorRootWorkloadIdBoldWhite(t *testing.T) {
	report := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{
				WorkloadId: types.Id("apps/v1/Deployment/default/web"),
				State:      ClusterScaleWorkloadStateUp,
				Dependencies: []ClusterScaleWorkloadDependencyStatus{
					{WorkloadId: types.Id("apps/v1/Deployment/default/db"), State: ClusterScaleWorkloadStateDown},
				},
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteClusterScaleStatusText(&buf, report, types.ColorYes))
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 3)
	assert.Contains(t, lines[0], colors.BoldLightMagenta())
	assert.Contains(t, lines[0], "(no app)")
	assert.Contains(t, lines[1], colors.BoldWhite())
	assert.Contains(t, lines[1], "apps/v1/Deployment/default/web")
	assert.NotContains(t, lines[2], colors.BoldWhite())
}

func TestWriteClusterScaleStatusText_GlobalColumnAlignmentAcrossBlocks(t *testing.T) {
	longRoot := types.Id("apps/v1/Deployment/default/loooooong-workload-name")
	report := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{
				WorkloadId: types.Id("a/b"),
				State:      ClusterScaleWorkloadStateUp,
				Ready:      ClusterScaleReadyReady,
			},
			{
				WorkloadId: longRoot,
				State:      ClusterScaleWorkloadStateUp,
				Ready:      ClusterScaleReadyReady,
			},
		},
	}
	blocks := buildClusterScaleStatusTextBlocks(report.Workloads)
	maxCol1, _, _ := measureClusterScaleStatusTextWidths(blocks)
	require.Greater(t, maxCol1, len("a/b"))

	var buf bytes.Buffer
	require.NoError(t, WriteClusterScaleStatusText(&buf, report, false))
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.Len(t, lines, 4, "app header, row1, blank between blocks, row2")
	assert.Equal(t, "(no app)", lines[0])
	assert.Empty(t, lines[2])
	idx0 := strings.Index(lines[1], "  up")
	idx3 := strings.Index(lines[3], "  up")
	assert.Equal(t, maxCol1, idx0, "sync column starts after globally padded first column")
	assert.Equal(t, idx0, idx3, "both blocks align to the same column")
}

func TestWriteClusterScaleStatusText_SortsByAppAndPrintsBoldLightMagentaAppHeaders(t *testing.T) {
	report := ClusterScaleStatusReport{
		Workloads: []ClusterScaleWorkloadStatus{
			{AppId: "zebra.app", WorkloadId: types.Id("apps/v1/Deployment/ns/z"), State: ClusterScaleWorkloadStateUp},
			{AppId: "aaa.app", WorkloadId: types.Id("apps/v1/Deployment/ns/a"), State: ClusterScaleWorkloadStateDown},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteClusterScaleStatusText(&buf, report, types.ColorYes))
	out := buf.String()
	assert.Less(t, strings.Index(out, "aaa.app"), strings.Index(out, "zebra.app"))
	prefix := colors.BoldLightMagenta()
	assert.Contains(t, out, prefix+"aaa.app")
	assert.Contains(t, out, prefix+"zebra.app")
}

func TestComputeClusterScaleWorkloadStatusReport_OptionalDependencyTag(t *testing.T) {
	required, err := mergedDeploymentEntity("default", "z-required", 1, 1)
	require.NoError(t, err)
	provider, err := mergedDeploymentEntity("mon", "provider", 1, 1)
	require.NoError(t, err)
	consumer, err := mergedDeploymentEntity("mon", "consumer", 1, 1)
	require.NoError(t, err)

	ents, err := entity.NewEntities([]entity.Entity{required, provider, consumer})
	require.NoError(t, err)

	refs := []types.Ref{
		{
			From: "apps/v1/Deployment/mon/consumer",
			To:   "apps/v1/Deployment/mon/provider",
			Tags: []string{types.RefTagOptionalStartup},
		},
	}

	report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, nil)
	require.NoError(t, err)

	var consumerRow *ClusterScaleWorkloadStatus
	for i := range report.Workloads {
		if report.Workloads[i].WorkloadId == types.Id("apps/v1/Deployment/mon/consumer") {
			consumerRow = &report.Workloads[i]
			break
		}
	}
	require.NotNil(t, consumerRow)
	require.Len(t, consumerRow.Dependencies, 1)
	assert.True(t, consumerRow.Dependencies[0].Optional)
}

func jobUnstructuredForScaleStatus(namespace, name string, suspend bool, status map[string]any) unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"suspend": suspend,
		},
	}
	if status != nil {
		obj["status"] = status
	}
	return unstructured.Unstructured{Object: obj}
}

func mergedJobEntity(namespace, name string, liveStatus map[string]any) (entity.Entity, error) {
	tplU := jobUnstructuredForScaleStatus(namespace, name, false, nil)
	liveU := jobUnstructuredForScaleStatus(namespace, name, false, liveStatus)
	b := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group("batch"), types.Version("v1"), types.Kind("Job"))).
		WithResource(types.Resource("jobs")).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace))
	return mustBuild(b.WithUnstructured(types.KeyTemplateEntity, tplU).WithUnstructured(types.KeyClusterEntity, liveU)), nil
}

func podUnstructuredForScaleStatus(namespace, name, phase string, conditions []map[string]any) unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
	}
	st := map[string]any{"phase": phase}
	if len(conditions) > 0 {
		sl := make([]interface{}, len(conditions))
		for i, c := range conditions {
			sl[i] = c
		}
		st["conditions"] = sl
	}
	obj["status"] = st
	return unstructured.Unstructured{Object: obj}
}

func TestComputeClusterScaleWorkloadStatusReport_JobWithPodDependencyReadyStates(t *testing.T) {
	re, err := NewReadyEvaluator(mergeReadyRules(nil, nil), entity.Entities{}, types.KeyClusterEntity)
	require.NoError(t, err)

	jobID := types.Id("batch/v1/Job/demo/generator-service-devicetunnel-dragonflydbpw")
	podID := types.Id("v1/Pod/demo/generator-service-devicetunnel-dragonflydbpw-6hf72")

	t.Run("new job no pod status yet", func(t *testing.T) {
		jobEnt, err := mergedJobEntity("demo", "generator-service-devicetunnel-dragonflydbpw", nil)
		require.NoError(t, err)
		podLive := podUnstructuredForScaleStatus("demo", "generator-service-devicetunnel-dragonflydbpw-6hf72", "Pending", nil)
		podEnt := mustBuild(entity.NewEntityBuilder().
			WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
			WithResource(types.Resource("pods")).
			WithName(types.Name("generator-service-devicetunnel-dragonflydbpw-6hf72")).
			WithNamespace(types.Namespace("demo")).
			WithUnstructured(types.KeyClusterEntity, podLive))

		ents, err := entity.NewEntities([]entity.Entity{jobEnt, podEnt})
		require.NoError(t, err)
		refs := []types.Ref{{From: jobID, To: podID}}
		report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, re)
		require.NoError(t, err)
		require.Len(t, report.Workloads, 1)
		assert.Equal(t, jobID, report.Workloads[0].WorkloadId)
		assert.Equal(t, ClusterScaleReadyNotReady, report.Workloads[0].Ready)
		require.Len(t, report.Workloads[0].Dependencies, 1)
		assert.Equal(t, ClusterScaleReadyNotReady, report.Workloads[0].Dependencies[0].Ready)
	})

	t.Run("pod stuck pending image pull job still running", func(t *testing.T) {
		jobEnt, err := mergedJobEntity("demo", "generator-service-devicetunnel-dragonflydbpw", map[string]any{
			"active":    int64(1),
			"succeeded": int64(0),
		})
		require.NoError(t, err)
		podLive := podUnstructuredForScaleStatus("demo", "generator-service-devicetunnel-dragonflydbpw-6hf72", "Pending", nil)
		podEnt := mustBuild(entity.NewEntityBuilder().
			WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
			WithResource(types.Resource("pods")).
			WithName(types.Name("generator-service-devicetunnel-dragonflydbpw-6hf72")).
			WithNamespace(types.Namespace("demo")).
			WithUnstructured(types.KeyClusterEntity, podLive))

		ents, err := entity.NewEntities([]entity.Entity{jobEnt, podEnt})
		require.NoError(t, err)
		refs := []types.Ref{{From: jobID, To: podID}}
		report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, re)
		require.NoError(t, err)
		require.Len(t, report.Workloads, 1)
		assert.Equal(t, ClusterScaleReadyNotReady, report.Workloads[0].Ready)
		assert.Equal(t, ClusterScaleReadyNotReady, report.Workloads[0].Dependencies[0].Ready)
	})

	t.Run("job running pod running and ready job still not ready until complete", func(t *testing.T) {
		jobEnt, err := mergedJobEntity("demo", "generator-service-devicetunnel-dragonflydbpw", map[string]any{
			"active":    int64(1),
			"succeeded": int64(0),
		})
		require.NoError(t, err)
		podLive := podUnstructuredForScaleStatus("demo", "generator-service-devicetunnel-dragonflydbpw-6hf72", "Running", []map[string]any{
			{"type": "Ready", "status": "True"},
		})
		podEnt := mustBuild(entity.NewEntityBuilder().
			WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
			WithResource(types.Resource("pods")).
			WithName(types.Name("generator-service-devicetunnel-dragonflydbpw-6hf72")).
			WithNamespace(types.Namespace("demo")).
			WithUnstructured(types.KeyClusterEntity, podLive))

		ents, err := entity.NewEntities([]entity.Entity{jobEnt, podEnt})
		require.NoError(t, err)
		refs := []types.Ref{{From: jobID, To: podID}}
		report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, re)
		require.NoError(t, err)
		require.Len(t, report.Workloads, 1)
		assert.Equal(t, ClusterScaleReadyNotReady, report.Workloads[0].Ready)
		assert.Equal(t, ClusterScaleReadyReady, report.Workloads[0].Dependencies[0].Ready)
	})

	t.Run("job failed pod failed", func(t *testing.T) {
		jobEnt, err := mergedJobEntity("demo", "generator-service-devicetunnel-dragonflydbpw", map[string]any{
			"conditions": []any{
				map[string]any{
					"type":    "Failed",
					"status":  "True",
					"reason":  "BackoffLimitExceeded",
					"message": "Job has reached the specified backoff limit",
				},
			},
			"failed": int64(1),
		})
		require.NoError(t, err)
		podLive := podUnstructuredForScaleStatus("demo", "generator-service-devicetunnel-dragonflydbpw-6hf72", "Failed", nil)
		podEnt := mustBuild(entity.NewEntityBuilder().
			WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
			WithResource(types.Resource("pods")).
			WithName(types.Name("generator-service-devicetunnel-dragonflydbpw-6hf72")).
			WithNamespace(types.Namespace("demo")).
			WithUnstructured(types.KeyClusterEntity, podLive))

		ents, err := entity.NewEntities([]entity.Entity{jobEnt, podEnt})
		require.NoError(t, err)
		refs := []types.Ref{{From: jobID, To: podID}}
		report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, re)
		require.NoError(t, err)
		require.Len(t, report.Workloads, 1)
		assert.Equal(t, ClusterScaleReadyNotReady, report.Workloads[0].Ready)
		joined := strings.Join(report.Workloads[0].ReadyMessages, " ")
		assert.Contains(t, joined, "job failed [reason=BackoffLimitExceeded]")
		assert.Contains(t, joined, "terminal")
		assert.Equal(t, ClusterScaleReadyNotReady, report.Workloads[0].Dependencies[0].Ready)
	})

	t.Run("job complete pod succeeded", func(t *testing.T) {
		jobEnt, err := mergedJobEntity("demo", "generator-service-devicetunnel-dragonflydbpw", map[string]any{
			"conditions": []any{
				map[string]any{"type": "Complete", "status": "True"},
			},
			"succeeded": int64(1),
		})
		require.NoError(t, err)
		podLive := podUnstructuredForScaleStatus("demo", "generator-service-devicetunnel-dragonflydbpw-6hf72", "Succeeded", nil)
		podEnt := mustBuild(entity.NewEntityBuilder().
			WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Pod"))).
			WithResource(types.Resource("pods")).
			WithName(types.Name("generator-service-devicetunnel-dragonflydbpw-6hf72")).
			WithNamespace(types.Namespace("demo")).
			WithUnstructured(types.KeyClusterEntity, podLive))

		ents, err := entity.NewEntities([]entity.Entity{jobEnt, podEnt})
		require.NoError(t, err)
		refs := []types.Ref{{From: jobID, To: podID}}
		report, err := ComputeClusterScaleWorkloadStatusReport(ents, refs, refs, types.KeyTemplateEntity, types.KeyClusterEntity, re)
		require.NoError(t, err)
		require.Len(t, report.Workloads, 1)
		assert.Equal(t, ClusterScaleReadyReady, report.Workloads[0].Ready)
		assert.Equal(t, ClusterScaleReadyReady, report.Workloads[0].Dependencies[0].Ready)
	})
}
