package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

var (
	vwcGVR = schema.GroupVersionResource{
		Group:    "admissionregistration.k8s.io",
		Version:  "v1",
		Resource: "validatingwebhookconfigurations",
	}
	mwcGVR = schema.GroupVersionResource{
		Group:    "admissionregistration.k8s.io",
		Version:  "v1",
		Resource: "mutatingwebhookconfigurations",
	}
)

func makeWebhookEntity(kind, name string) entity.Entity {
	gvk := types.NewGVK(
		types.Group("admissionregistration.k8s.io"),
		types.Version("v1"),
		types.Kind(kind),
	)
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name)))
}

func newFakeClient(objects ...runtime.Object) *fake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	return fake.NewSimpleDynamicClient(scheme, objects...)
}

func fakeWebhookResource(apiVersion, kind, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]any{
				"name": name,
			},
		},
	}
}

func TestDeleteWebhookConfigs_ExistingValidating(t *testing.T) {
	ctx := context.Background()

	existing := fakeWebhookResource(
		"admissionregistration.k8s.io/v1",
		"ValidatingWebhookConfiguration",
		"webhook.cert-manager.io",
	)
	client := newFakeClient(existing)

	e := makeWebhookEntity("ValidatingWebhookConfiguration", "webhook.cert-manager.io")
	webhookEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = DeleteWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, false)
	require.NoError(t, err)

	_, getErr := client.Resource(vwcGVR).Get(ctx, "webhook.cert-manager.io", metav1.GetOptions{})
	assert.True(t, errors.IsNotFound(getErr), "resource should have been deleted")
}

func TestDeleteWebhookConfigs_NonExistent(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()

	e := makeWebhookEntity("ValidatingWebhookConfiguration", "webhook.does-not-exist.io")
	webhookEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = DeleteWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, false)
	assert.NoError(t, err, "deleting a non-existent webhook should not return an error")
}

func TestDeleteWebhookConfigs_BothTypes(t *testing.T) {
	ctx := context.Background()

	existingVWC := fakeWebhookResource(
		"admissionregistration.k8s.io/v1",
		"ValidatingWebhookConfiguration",
		"validating.example.io",
	)
	existingMWC := fakeWebhookResource(
		"admissionregistration.k8s.io/v1",
		"MutatingWebhookConfiguration",
		"mutating.example.io",
	)
	client := newFakeClient(existingVWC, existingMWC)

	vEntity := makeWebhookEntity("ValidatingWebhookConfiguration", "validating.example.io")
	mEntity := makeWebhookEntity("MutatingWebhookConfiguration", "mutating.example.io")
	webhookEntities, err := entity.NewEntities([]entity.Entity{vEntity, mEntity})
	require.NoError(t, err)

	err = DeleteWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, false)
	require.NoError(t, err)

	_, vErr := client.Resource(vwcGVR).Get(ctx, "validating.example.io", metav1.GetOptions{})
	assert.True(t, errors.IsNotFound(vErr), "ValidatingWebhookConfiguration should have been deleted")

	_, mErr := client.Resource(mwcGVR).Get(ctx, "mutating.example.io", metav1.GetOptions{})
	assert.True(t, errors.IsNotFound(mErr), "MutatingWebhookConfiguration should have been deleted")
}

func TestDeleteWebhookConfigs_EmptyEntities(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()

	webhookEntities, err := entity.NewEntities([]entity.Entity{})
	require.NoError(t, err)

	err = DeleteWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, false)
	assert.NoError(t, err, "empty entities should be a no-op")
}

func TestDeleteWebhookConfigs_DryRun(t *testing.T) {
	ctx := context.Background()

	existing := fakeWebhookResource(
		"admissionregistration.k8s.io/v1",
		"ValidatingWebhookConfiguration",
		"webhook.dryrun.io",
	)
	client := newFakeClient(existing)

	e := makeWebhookEntity("ValidatingWebhookConfiguration", "webhook.dryrun.io")
	webhookEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = DeleteWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, true)
	require.NoError(t, err)

	result, getErr := client.Resource(vwcGVR).Get(ctx, "webhook.dryrun.io", metav1.GetOptions{})
	require.NoError(t, getErr, "resource should still exist in dry-run mode")
	assert.Equal(t, "webhook.dryrun.io", result.GetName())
}

// --- DisableWebhookConfigs tests ---

func fakeWebhookWithEntries(apiVersion, kind, name string, entries []map[string]any) *unstructured.Unstructured {
	webhooks := make([]any, len(entries))
	for i, e := range entries {
		webhooks[i] = e
	}
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata":   map[string]any{"name": name},
			"webhooks":   webhooks,
		},
	}
}

func TestDisableWebhookConfigs_ValidatingWebhook(t *testing.T) {
	ctx := context.Background()

	existing := fakeWebhookWithEntries(
		"admissionregistration.k8s.io/v1",
		"ValidatingWebhookConfiguration",
		"cert-manager-webhook",
		[]map[string]any{
			{"name": "webhook.cert-manager.io", "failurePolicy": "Fail"},
		},
	)
	client := newFakeClient(existing)

	e := makeWebhookEntity("ValidatingWebhookConfiguration", "cert-manager-webhook")
	webhookEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = DisableWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, false)
	require.NoError(t, err)

	result, getErr := client.Resource(vwcGVR).Get(ctx, "cert-manager-webhook", metav1.GetOptions{})
	require.NoError(t, getErr)

	webhooks := result.Object["webhooks"].([]any)
	wh := webhooks[0].(map[string]any)
	assert.Equal(t, "Ignore", wh["failurePolicy"])
}

func TestDisableWebhookConfigs_MutatingWebhook(t *testing.T) {
	ctx := context.Background()

	existing := fakeWebhookWithEntries(
		"admissionregistration.k8s.io/v1",
		"MutatingWebhookConfiguration",
		"mutating-webhook",
		[]map[string]any{
			{"name": "mutate.example.io", "failurePolicy": "Fail"},
		},
	)
	client := newFakeClient(existing)

	e := makeWebhookEntity("MutatingWebhookConfiguration", "mutating-webhook")
	webhookEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = DisableWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, false)
	require.NoError(t, err)

	result, getErr := client.Resource(mwcGVR).Get(ctx, "mutating-webhook", metav1.GetOptions{})
	require.NoError(t, getErr)

	webhooks := result.Object["webhooks"].([]any)
	wh := webhooks[0].(map[string]any)
	assert.Equal(t, "Ignore", wh["failurePolicy"])
}

func TestDisableWebhookConfigs_AlreadyIgnore(t *testing.T) {
	ctx := context.Background()

	existing := fakeWebhookWithEntries(
		"admissionregistration.k8s.io/v1",
		"ValidatingWebhookConfiguration",
		"already-ignore",
		[]map[string]any{
			{"name": "wh", "failurePolicy": "Ignore"},
		},
	)
	client := newFakeClient(existing)

	e := makeWebhookEntity("ValidatingWebhookConfiguration", "already-ignore")
	webhookEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = DisableWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, false)
	assert.NoError(t, err, "should succeed without error for already-disabled webhooks")
}

func TestDisableWebhookConfigs_NotFound(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()

	e := makeWebhookEntity("ValidatingWebhookConfiguration", "not-in-cluster")
	webhookEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = DisableWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, false)
	assert.NoError(t, err, "should not error for non-existent webhook")
}

func TestDisableWebhookConfigs_DryRun(t *testing.T) {
	ctx := context.Background()

	existing := fakeWebhookWithEntries(
		"admissionregistration.k8s.io/v1",
		"ValidatingWebhookConfiguration",
		"dryrun-webhook",
		[]map[string]any{
			{"name": "wh", "failurePolicy": "Fail"},
		},
	)
	client := newFakeClient(existing)

	e := makeWebhookEntity("ValidatingWebhookConfiguration", "dryrun-webhook")
	webhookEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = DisableWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, true)
	require.NoError(t, err)

	result, getErr := client.Resource(vwcGVR).Get(ctx, "dryrun-webhook", metav1.GetOptions{})
	require.NoError(t, getErr)

	webhooks := result.Object["webhooks"].([]any)
	wh := webhooks[0].(map[string]any)
	assert.Equal(t, "Fail", wh["failurePolicy"], "dry-run should not modify the webhook")
}

func TestDisableWebhookConfigs_MultipleEntries(t *testing.T) {
	ctx := context.Background()

	existing := fakeWebhookWithEntries(
		"admissionregistration.k8s.io/v1",
		"ValidatingWebhookConfiguration",
		"multi-entry",
		[]map[string]any{
			{"name": "wh-a", "failurePolicy": "Fail"},
			{"name": "wh-b", "failurePolicy": "Fail"},
		},
	)
	client := newFakeClient(existing)

	e := makeWebhookEntity("ValidatingWebhookConfiguration", "multi-entry")
	webhookEntities, err := entity.NewEntities([]entity.Entity{e})
	require.NoError(t, err)

	err = DisableWebhookConfigs(ctx, log.Default(), client, webhookEntities, types.KeyTemplateEntity, false)
	require.NoError(t, err)

	result, getErr := client.Resource(vwcGVR).Get(ctx, "multi-entry", metav1.GetOptions{})
	require.NoError(t, getErr)

	webhooks := result.Object["webhooks"].([]any)
	for _, whRaw := range webhooks {
		wh := whRaw.(map[string]any)
		assert.Equal(t, "Ignore", wh["failurePolicy"])
	}
}

// --- IsWorkloadReady tests ---

func fakeDeployment(namespace, name string, readyReplicas int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"replicas": int64(1),
			},
			"status": map[string]any{
				"readyReplicas": readyReplicas,
			},
		},
	}
}

func makeWorkloadEntity(group, version, kind, resource, namespace, name string) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name)).
		WithResource(types.Resource(resource))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	return mustBuild(b)
}

func TestIsWorkloadReady_DeploymentReady(t *testing.T) {
	ctx := context.Background()
	deploy := fakeDeployment("cert-manager", "cert-manager-webhook", 1)
	client := newFakeClient(deploy)

	workload := makeWorkloadEntity("apps", "v1", "Deployment", "deployments", "cert-manager", "cert-manager-webhook")

	ready, err := IsWorkloadReady(ctx, client, workload)
	require.NoError(t, err)
	assert.True(t, ready)
}

func TestIsWorkloadReady_DeploymentNotReady(t *testing.T) {
	ctx := context.Background()
	deploy := fakeDeployment("cert-manager", "cert-manager-webhook", 0)
	client := newFakeClient(deploy)

	workload := makeWorkloadEntity("apps", "v1", "Deployment", "deployments", "cert-manager", "cert-manager-webhook")

	ready, err := IsWorkloadReady(ctx, client, workload)
	require.NoError(t, err)
	assert.False(t, ready)
}

func TestIsWorkloadReady_DeploymentNotFound(t *testing.T) {
	ctx := context.Background()
	client := newFakeClient()

	workload := makeWorkloadEntity("apps", "v1", "Deployment", "deployments", "cert-manager", "missing-deploy")

	ready, err := IsWorkloadReady(ctx, client, workload)
	require.NoError(t, err)
	assert.False(t, ready, "not found workload should be treated as not ready")
}

func TestIsWorkloadReady_StatefulSetReady(t *testing.T) {
	ctx := context.Background()
	sts := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]any{
				"name":      "my-sts",
				"namespace": "default",
			},
			"status": map[string]any{
				"readyReplicas": int64(2),
			},
		},
	}
	client := newFakeClient(sts)

	workload := makeWorkloadEntity("apps", "v1", "StatefulSet", "statefulsets", "default", "my-sts")

	ready, err := IsWorkloadReady(ctx, client, workload)
	require.NoError(t, err)
	assert.True(t, ready)
}
