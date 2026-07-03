package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestShouldServerSideApply_WithAnnotation(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]any{
				"name": "test-crd",
				"annotations": map[string]any{
					"argocd.argoproj.io/sync-options": "ServerSideApply=true",
				},
			},
		},
	}
	e := withUnstructured(makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "test-crd"),
		types.KeyTemplateEntity, u)

	assert.True(t, ShouldServerSideApply(e, types.KeyTemplateEntity))
}

func TestShouldServerSideApply_WithoutAnnotation(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "my-config",
				"namespace": "default",
			},
		},
	}
	e := withUnstructured(makeEntity("", "v1", "ConfigMap", "default", "my-config"),
		types.KeyTemplateEntity, u)

	assert.False(t, ShouldServerSideApply(e, types.KeyTemplateEntity))
}

func TestShouldServerSideApply_WithMultipleOptions(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]any{
				"name": "test-crd-multi",
				"annotations": map[string]any{
					"argocd.argoproj.io/sync-options": "Prune=true,ServerSideApply=true",
				},
			},
		},
	}
	e := withUnstructured(makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "test-crd-multi"),
		types.KeyTemplateEntity, u)

	assert.True(t, ShouldServerSideApply(e, types.KeyTemplateEntity))
}

func TestShouldServerSideApply_WithFalseValue(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]any{
				"name": "test-crd-false",
				"annotations": map[string]any{
					"argocd.argoproj.io/sync-options": "ServerSideApply=false",
				},
			},
		},
	}
	e := withUnstructured(makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "test-crd-false"),
		types.KeyTemplateEntity, u)

	assert.False(t, ShouldServerSideApply(e, types.KeyTemplateEntity))
}

func TestShouldServerSideApply_WithoutUnstructuredData(t *testing.T) {
	e := makeEntity("apiextensions.k8s.io", "v1", "CustomResourceDefinition", "", "test-crd-no-data")

	assert.False(t, ShouldServerSideApply(e, types.KeyTemplateEntity))
}
