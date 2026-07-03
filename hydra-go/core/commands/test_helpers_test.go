package commands

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// clusterInventoryUnstructured builds a minimal live object for KeyClusterEntity (uid, ownerReferences).
func clusterInventoryUnstructured(group, version, kind, namespace, name, uid string, ownerRefs []map[string]any) unstructured.Unstructured {
	apiVersion := version
	if group != "" {
		apiVersion = group + "/" + version
	}
	metadata := map[string]any{
		"name":      name,
		"namespace": namespace,
		"uid":       uid,
	}
	if ownerRefs != nil {
		refs := make([]any, len(ownerRefs))
		for i, r := range ownerRefs {
			refs[i] = r
		}
		metadata["ownerReferences"] = refs
	}
	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata":   metadata,
		},
	}
}

func clusterInventoryOwnerRef(apiVersion, kind, name, uid string) map[string]any {
	return map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"name":       name,
		"uid":        uid,
	}
}

func clusterNamespaceEntity(name, uid string) entity.Entity {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]any{
				"name": name,
				"uid":  uid,
			},
		},
	}
	return mustBuild(entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(""), types.Version("v1"), types.Kind("Namespace"))).
		WithName(types.Name(name)).
		WithUnstructured(types.KeyClusterEntity, u))
}

func makeClusterInventoryEntity(group, version, kind, namespace, name, uid string, ownerRefs []map[string]any) entity.Entity {
	u := clusterInventoryUnstructured(group, version, kind, namespace, name, uid, ownerRefs)
	b := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))).
		WithName(types.Name(name)).
		WithNamespace(types.Namespace(namespace)).
		WithUnstructured(types.KeyClusterEntity, u)
	return mustBuild(b)
}

func mustBuild(b entity.EntityBuilder) entity.Entity {
	e, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("mustBuild: %v", err))
	}
	return e
}

func mustEntities(t *testing.T, items []entity.Entity) entity.Entities {
	t.Helper()
	ents, err := entity.NewEntities(items)
	require.NoError(t, err)
	return ents
}

func mustModify(e entity.Entity, fn func(entity.EntityBuilder) entity.EntityBuilder) entity.Entity {
	result, err := e.Modify(fn)
	if err != nil {
		panic(fmt.Sprintf("mustModify: %v", err))
	}
	return result
}

func withResource(e entity.Entity, r types.Resource) entity.Entity {
	return mustModify(e, func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithResource(r)
	})
}

func withUnstructured(e entity.Entity, key types.EntityKeyUnstructured, u unstructured.Unstructured) entity.Entity {
	return mustModify(e, func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithUnstructured(key, u)
	})
}

func withLiveObject(t *testing.T, e entity.Entity, fields map[string]any) entity.Entity {
	t.Helper()
	u, ok := e.Unstructured(types.KeyClusterEntity)
	require.True(t, ok)
	obj := runtime.DeepCopyJSON(u.Object)
	for k, v := range fields {
		obj[k] = v
	}
	return withUnstructured(e, types.KeyClusterEntity, unstructured.Unstructured{Object: obj})
}

func withAppIds(e entity.Entity, appIds []types.AppId) entity.Entity {
	return mustModify(e, func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithAppIds(appIds)
	})
}

func withNamespace(e entity.Entity, ns types.Namespace) entity.Entity {
	return mustModify(e, func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithNamespace(ns)
	})
}

func withAppNamespace(e entity.Entity, ns types.AppNamespace) entity.Entity {
	return mustModify(e, func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithAppNamespace(ns)
	})
}

func withKind(e entity.Entity, kind types.Kind) entity.Entity {
	return mustModify(e, func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithKind(kind)
	})
}

func makeEntity(group, version, kind, namespace, name string) entity.Entity {
	gvk := types.NewGVK(types.Group(group), types.Version(version), types.Kind(kind))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	return mustBuild(b)
}

func entityIds(t *testing.T, entities entity.Entities) []types.Id {
	t.Helper()
	ids := make([]types.Id, 0, entities.Len())
	for _, e := range entities.Items {
		id, err := e.Id()
		require.NoError(t, err)
		ids = append(ids, id)
	}
	return ids
}
