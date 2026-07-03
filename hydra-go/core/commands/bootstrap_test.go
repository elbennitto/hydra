package commands

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func mockDecryptor(decryptedYaml types.YamlString) SopsDecryptor {
	return func(yaml types.YamlString) (types.YamlString, error) {
		return decryptedYaml, nil
	}
}

func failingDecryptor(msg string) SopsDecryptor {
	return func(yaml types.YamlString) (types.YamlString, error) {
		return "", fmt.Errorf("%s", msg)
	}
}

func makeSopsSecretEntity(namespace, name string, secretTemplates []any) entity.Entity {
	gvk := types.NewGVK(types.Group("isindir.github.com"), types.Version("v1alpha3"), types.Kind("SopsSecret"))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	e := mustBuild(b)
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "isindir.github.com/v1alpha3",
			"kind":       "SopsSecret",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"secretTemplates": secretTemplates,
			},
		},
	}
	return withUnstructured(e, types.KeyTemplateEntity, u)
}

func decryptedSopsSecretYaml(namespace, name string, templates []map[string]any) types.YamlString {
	result := fmt.Sprintf("apiVersion: isindir.github.com/v1alpha3\nkind: SopsSecret\nmetadata:\n  name: %s\n  namespace: %s\nspec:\n  secretTemplates:\n", name, namespace)
	for _, tmpl := range templates {
		result += fmt.Sprintf("  - name: %s\n", tmpl["name"])
		if typ, ok := tmpl["type"]; ok {
			result += fmt.Sprintf("    type: %s\n", typ)
		}
		if sd, ok := tmpl["stringData"].(map[string]string); ok {
			result += "    stringData:\n"
			for k, v := range sd {
				result += fmt.Sprintf("      %s: %s\n", k, v)
			}
		}
		if d, ok := tmpl["data"].(map[string]string); ok {
			result += "    data:\n"
			for k, v := range d {
				result += fmt.Sprintf("      %s: %s\n", k, v)
			}
		}
		if ann, ok := tmpl["annotations"].(map[string]string); ok && len(ann) > 0 {
			result += "    annotations:\n"
			for k, v := range ann {
				// Quote values so YAML keeps them as strings (e.g. "false" not a boolean).
				result += fmt.Sprintf("      %s: %q\n", k, v)
			}
		}
	}
	return types.YamlString(result)
}

func TestExpandSopsSecretsForUninstall_DerivesSingleSecret(t *testing.T) {
	sopsEntity := makeSopsSecretEntity("dex", "argocd-secret", []any{
		map[string]any{
			"name":       "argocd-secret",
			"stringData": map[string]any{"client-secret": "ENC[AES256_GCM,data:abc]"},
		},
	})

	entities, err := entity.NewEntities([]entity.Entity{sopsEntity})
	require.NoError(t, err)

	result, err := ExpandSopsSecretsForUninstall(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)

	assert.Equal(t, 2, result.Len())

	ids := entityIds(t, result)
	assert.Contains(t, ids, types.Id("isindir.github.com/v1alpha3/SopsSecret/dex/argocd-secret"))
	assert.Contains(t, ids, types.Id("v1/Secret/dex/argocd-secret"))
}

func TestExpandSopsSecretsForUninstall_DerivesMultipleSecrets(t *testing.T) {
	sopsEntity := makeSopsSecretEntity("demo", "multi", []any{
		map[string]any{"name": "device-api", "stringData": map[string]any{"hash": "ENC[a]"}},
		map[string]any{"name": "dbsecret", "stringData": map[string]any{"password": "ENC[b]"}},
	})

	entities, err := entity.NewEntities([]entity.Entity{sopsEntity})
	require.NoError(t, err)

	result, err := ExpandSopsSecretsForUninstall(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)

	assert.Equal(t, 3, result.Len())

	ids := entityIds(t, result)
	assert.Contains(t, ids, types.Id("v1/Secret/demo/device-api"))
	assert.Contains(t, ids, types.Id("v1/Secret/demo/dbsecret"))
}

func TestExpandSopsSecretsForUninstall_MixedEntities(t *testing.T) {
	sopsEntity := makeSopsSecretEntity("cert-manager", "hetzner-credentials", []any{
		map[string]any{"name": "hetzner-credentials", "stringData": map[string]any{"token": "ENC[v]"}},
	})
	configMap := makeEntity("", "v1", "ConfigMap", "cert-manager", "my-config")

	entities, err := entity.NewEntities([]entity.Entity{sopsEntity, configMap})
	require.NoError(t, err)

	result, err := ExpandSopsSecretsForUninstall(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)

	assert.Equal(t, 3, result.Len())

	ids := entityIds(t, result)
	assert.Contains(t, ids, types.Id("isindir.github.com/v1alpha3/SopsSecret/cert-manager/hetzner-credentials"))
	assert.Contains(t, ids, types.Id("v1/ConfigMap/cert-manager/my-config"))
	assert.Contains(t, ids, types.Id("v1/Secret/cert-manager/hetzner-credentials"))
}

func TestExpandSopsSecretsForUninstall_NoSopsSecrets(t *testing.T) {
	configMap := makeEntity("", "v1", "ConfigMap", "default", "app-config")

	entities, err := entity.NewEntities([]entity.Entity{configMap})
	require.NoError(t, err)

	result, err := ExpandSopsSecretsForUninstall(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Len())
}

func TestExpandSopsSecretsForUninstall_EmptyEntities(t *testing.T) {
	entities, err := entity.NewEntities(nil)
	require.NoError(t, err)

	result, err := ExpandSopsSecretsForUninstall(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)

	assert.Equal(t, 0, result.Len())
}

func TestConvertSopsSecrets_SingleTemplateCreatesOneSecret(t *testing.T) {
	sopsEntity := makeSopsSecretEntity("sops-operator", "image-pull-secret", []any{
		map[string]any{
			"name":       "image-pull-secret",
			"type":       "kubernetes.io/dockerconfigjson",
			"stringData": map[string]any{".dockerconfigjson": "ENC[encrypted]"},
		},
	})

	entities, err := entity.NewEntities([]entity.Entity{sopsEntity})
	require.NoError(t, err)

	decrypted := decryptedSopsSecretYaml("sops-operator", "image-pull-secret", []map[string]any{
		{
			"name": "image-pull-secret",
			"type": "kubernetes.io/dockerconfigjson",
			"stringData": map[string]string{
				".dockerconfigjson": `{"auths":{"reg.example.com":{"auth":"abc"}}}`,
			},
		},
	})

	result, err := ConvertSopsSecretsToSecrets(log.Default(), entities, types.KeyTemplateEntity, mockDecryptor(decrypted))
	require.NoError(t, err)

	// Original SopsSecret + new Secret
	assert.Equal(t, 2, result.Len())

	ids := entityIds(t, result)
	assert.Contains(t, ids, types.Id("isindir.github.com/v1alpha3/SopsSecret/sops-operator/image-pull-secret"))
	assert.Contains(t, ids, types.Id("v1/Secret/sops-operator/image-pull-secret"))

	// Verify the new Secret has correct data
	for _, e := range result.Items {
		kind, err := e.Kind()
		require.NoError(t, err)
		if string(kind) == "Secret" {
			u, ok := e.Unstructured(types.KeyTemplateEntity)
			require.True(t, ok)
			assert.Equal(t, "v1", u.GetAPIVersion())
			assert.Equal(t, "Secret", u.GetKind())
			assert.Equal(t, "image-pull-secret", u.GetName())
			assert.Equal(t, "sops-operator", u.GetNamespace())

			secretType, _, _ := unstructured.NestedString(u.Object, "type")
			assert.Equal(t, "kubernetes.io/dockerconfigjson", secretType)

			sd, _, _ := unstructured.NestedMap(u.Object, "stringData")
			assert.Contains(t, sd, ".dockerconfigjson")

			ann, _, _ := unstructured.NestedStringMap(u.Object, "metadata", "annotations")
			assert.Equal(t, "true", ann[sopsSecretManagedAnnotationKey])
		}
	}
}

func TestConvertSopsSecrets_PreservesExplicitManagedAnnotation(t *testing.T) {
	sopsEntity := makeSopsSecretEntity("ns", "my-sops", []any{
		map[string]any{"name": "child", "stringData": map[string]any{"k": "ENC[v]"}},
	})

	entities, err := entity.NewEntities([]entity.Entity{sopsEntity})
	require.NoError(t, err)

	decrypted := decryptedSopsSecretYaml("ns", "my-sops", []map[string]any{
		{
			"name":        "child",
			"stringData":  map[string]string{"k": "v"},
			"annotations": map[string]string{sopsSecretManagedAnnotationKey: "false"},
		},
	})

	result, err := ConvertSopsSecretsToSecrets(log.Default(), entities, types.KeyTemplateEntity, mockDecryptor(decrypted))
	require.NoError(t, err)

	for _, e := range result.Items {
		kind, err := e.Kind()
		require.NoError(t, err)
		if string(kind) != "Secret" {
			continue
		}
		u, ok := e.Unstructured(types.KeyTemplateEntity)
		require.True(t, ok)
		ann, _, _ := unstructured.NestedStringMap(u.Object, "metadata", "annotations")
		assert.Equal(t, "false", ann[sopsSecretManagedAnnotationKey])
	}
}

func TestConvertSopsSecrets_MultipleTemplatesCreateMultipleSecrets(t *testing.T) {
	sopsEntity := makeSopsSecretEntity("demo", "multi-secret", []any{
		map[string]any{"name": "secret-a", "stringData": map[string]any{"key": "ENC[a]"}},
		map[string]any{"name": "secret-b", "type": "Opaque", "stringData": map[string]any{"user": "ENC[b]"}},
	})

	entities, err := entity.NewEntities([]entity.Entity{sopsEntity})
	require.NoError(t, err)

	decrypted := decryptedSopsSecretYaml("demo", "multi-secret", []map[string]any{
		{"name": "secret-a", "stringData": map[string]string{"key": "value-a"}},
		{"name": "secret-b", "type": "Opaque", "stringData": map[string]string{"user": "admin"}},
	})

	result, err := ConvertSopsSecretsToSecrets(log.Default(), entities, types.KeyTemplateEntity, mockDecryptor(decrypted))
	require.NoError(t, err)

	// Original SopsSecret + 2 new Secrets
	assert.Equal(t, 3, result.Len())

	ids := entityIds(t, result)
	assert.Contains(t, ids, types.Id("isindir.github.com/v1alpha3/SopsSecret/demo/multi-secret"))
	assert.Contains(t, ids, types.Id("v1/Secret/demo/secret-a"))
	assert.Contains(t, ids, types.Id("v1/Secret/demo/secret-b"))
}

func TestConvertSopsSecrets_MixedEntitiesOnlySopsConverted(t *testing.T) {
	sopsEntity := makeSopsSecretEntity("ns", "my-sops", []any{
		map[string]any{"name": "decrypted", "stringData": map[string]any{"k": "ENC[v]"}},
	})
	configMap := makeEntity("", "v1", "ConfigMap", "ns", "my-config")
	deployment := makeEntity("apps", "v1", "Deployment", "ns", "my-deploy")

	entities, err := entity.NewEntities([]entity.Entity{sopsEntity, configMap, deployment})
	require.NoError(t, err)

	decrypted := decryptedSopsSecretYaml("ns", "my-sops", []map[string]any{
		{"name": "decrypted", "stringData": map[string]string{"k": "plain-value"}},
	})

	result, err := ConvertSopsSecretsToSecrets(log.Default(), entities, types.KeyTemplateEntity, mockDecryptor(decrypted))
	require.NoError(t, err)

	// SopsSecret + ConfigMap + Deployment + new Secret
	assert.Equal(t, 4, result.Len())

	ids := entityIds(t, result)
	assert.Contains(t, ids, types.Id("isindir.github.com/v1alpha3/SopsSecret/ns/my-sops"))
	assert.Contains(t, ids, types.Id("v1/ConfigMap/ns/my-config"))
	assert.Contains(t, ids, types.Id("apps/v1/Deployment/ns/my-deploy"))
	assert.Contains(t, ids, types.Id("v1/Secret/ns/decrypted"))
}

func TestConvertSopsSecrets_NoSopsSecretEntitiesUnchanged(t *testing.T) {
	configMap := makeEntity("", "v1", "ConfigMap", "default", "app-config")
	secret := makeEntity("", "v1", "Secret", "default", "existing-secret")

	entities, err := entity.NewEntities([]entity.Entity{configMap, secret})
	require.NoError(t, err)

	neverCalled := func(yaml types.YamlString) (types.YamlString, error) {
		t.Fatal("decryptor should not be called when there are no SopsSecrets")
		return "", nil
	}

	result, err := ConvertSopsSecretsToSecrets(log.Default(), entities, types.KeyTemplateEntity, neverCalled)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Len())
}

func TestConvertSopsSecrets_WithDataFieldInsteadOfStringData(t *testing.T) {
	sopsEntity := makeSopsSecretEntity("ns", "b64-secret", []any{
		map[string]any{"name": "b64-secret", "data": map[string]any{"cert": "ENC[b64data]"}},
	})

	entities, err := entity.NewEntities([]entity.Entity{sopsEntity})
	require.NoError(t, err)

	decrypted := decryptedSopsSecretYaml("ns", "b64-secret", []map[string]any{
		{"name": "b64-secret", "data": map[string]string{"cert": "LS0tLS1CRUdJTi..."}},
	})

	result, err := ConvertSopsSecretsToSecrets(log.Default(), entities, types.KeyTemplateEntity, mockDecryptor(decrypted))
	require.NoError(t, err)
	assert.Equal(t, 2, result.Len())

	for _, e := range result.Items {
		kind, err := e.Kind()
		require.NoError(t, err)
		if string(kind) == "Secret" {
			u, ok := e.Unstructured(types.KeyTemplateEntity)
			require.True(t, ok)
			data, _, _ := unstructured.NestedMap(u.Object, "data")
			assert.Contains(t, data, "cert")
		}
	}
}

func TestConvertSopsSecrets_DecryptorError(t *testing.T) {
	sopsEntity := makeSopsSecretEntity("ns", "bad-secret", []any{
		map[string]any{"name": "bad", "stringData": map[string]any{"k": "ENC[v]"}},
	})

	entities, err := entity.NewEntities([]entity.Entity{sopsEntity})
	require.NoError(t, err)

	_, err = ConvertSopsSecretsToSecrets(log.Default(), entities, types.KeyTemplateEntity, failingDecryptor("sops key not available"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sops key not available")
}

func TestConvertSopsSecrets_EmptyEntities(t *testing.T) {
	entities, err := entity.NewEntities(nil)
	require.NoError(t, err)

	result, err := ConvertSopsSecretsToSecrets(log.Default(), entities, types.KeyTemplateEntity, failingDecryptor("should not be called"))
	require.NoError(t, err)
	assert.Equal(t, 0, result.Len())
}

func makeBackupSopsSecretEntity(namespace, name string, secretTemplates []any) entity.Entity {
	gvk := types.NewGVK(types.Group("isindir.github.com"), types.Version("v1alpha3"), types.Kind("SopsSecret"))
	b := entity.NewEntityBuilder().
		WithGVK(gvk).
		WithName(types.Name(name))
	if namespace != "" {
		b = b.WithNamespace(types.Namespace(namespace))
	}
	e := mustBuild(b)
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "isindir.github.com/v1alpha3",
			"kind":       "SopsSecret",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"annotations": map[string]any{
					hydra.AnnotationHydraBackup: "true",
				},
			},
			"spec": map[string]any{
				"suspend":         true,
				"secretTemplates": secretTemplates,
			},
		},
	}
	return withUnstructured(e, types.KeyTemplateEntity, u)
}

func TestConvertSopsSecrets_BackupSopsSecretSkipped(t *testing.T) {
	backupEntity := makeBackupSopsSecretEntity("cert-manager", "letsencrypt-prod-backup", []any{
		map[string]any{"name": "letsencrypt-prod", "data": map[string]any{"tls.key": "ENC[secret]"}},
	})

	entities, err := entity.NewEntities([]entity.Entity{backupEntity})
	require.NoError(t, err)

	neverCalled := func(yaml types.YamlString) (types.YamlString, error) {
		t.Fatal("decryptor should not be called for backup SopsSecrets")
		return "", nil
	}

	result, err := ConvertSopsSecretsToSecrets(log.Default(), entities, types.KeyTemplateEntity, neverCalled)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Len(), "only the original backup SopsSecret should remain, no derived Secret")
	ids := entityIds(t, result)
	assert.Contains(t, ids, types.Id("isindir.github.com/v1alpha3/SopsSecret/cert-manager/letsencrypt-prod-backup"))
	assert.NotContains(t, ids, types.Id("v1/Secret/cert-manager/letsencrypt-prod"))
}

func TestConvertSopsSecrets_BackupSkippedNormalConverted(t *testing.T) {
	backupEntity := makeBackupSopsSecretEntity("cert-manager", "letsencrypt-prod-backup", []any{
		map[string]any{"name": "letsencrypt-prod", "data": map[string]any{"tls.key": "ENC[key]"}},
	})
	normalEntity := makeSopsSecretEntity("dex", "dex-secret", []any{
		map[string]any{"name": "dex-secret", "stringData": map[string]any{"client-secret": "ENC[v]"}},
	})

	entities, err := entity.NewEntities([]entity.Entity{backupEntity, normalEntity})
	require.NoError(t, err)

	decrypted := decryptedSopsSecretYaml("dex", "dex-secret", []map[string]any{
		{"name": "dex-secret", "stringData": map[string]string{"client-secret": "plain"}},
	})

	result, err := ConvertSopsSecretsToSecrets(log.Default(), entities, types.KeyTemplateEntity, mockDecryptor(decrypted))
	require.NoError(t, err)

	assert.Equal(t, 3, result.Len(), "backup SopsSecret + normal SopsSecret + derived Secret from normal")
	ids := entityIds(t, result)
	assert.Contains(t, ids, types.Id("isindir.github.com/v1alpha3/SopsSecret/cert-manager/letsencrypt-prod-backup"))
	assert.Contains(t, ids, types.Id("isindir.github.com/v1alpha3/SopsSecret/dex/dex-secret"))
	assert.Contains(t, ids, types.Id("v1/Secret/dex/dex-secret"))
	assert.NotContains(t, ids, types.Id("v1/Secret/cert-manager/letsencrypt-prod"))
}

func TestExpandSopsSecretsForUninstall_BackupSopsSecretSkipped(t *testing.T) {
	backupEntity := makeBackupSopsSecretEntity("cert-manager", "letsencrypt-prod-backup", []any{
		map[string]any{"name": "letsencrypt-prod", "data": map[string]any{"tls.key": "ENC[key]"}},
	})

	entities, err := entity.NewEntities([]entity.Entity{backupEntity})
	require.NoError(t, err)

	result, err := ExpandSopsSecretsForUninstall(log.Default(), entities, types.KeyTemplateEntity)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Len(), "only the original backup SopsSecret should remain")
	ids := entityIds(t, result)
	assert.Contains(t, ids, types.Id("isindir.github.com/v1alpha3/SopsSecret/cert-manager/letsencrypt-prod-backup"))
	assert.NotContains(t, ids, types.Id("v1/Secret/cert-manager/letsencrypt-prod"))
}

func TestAppendDerivedSopsSecretsForUninstall_AddsStubsForSelectedSopsSecrets(t *testing.T) {
	sopsEntity := makeSopsSecretEntity("dex", "argocd-secret", []any{
		map[string]any{
			"name":       "argocd-secret",
			"stringData": map[string]any{"client-secret": "ENC[AES256_GCM,data:abc]"},
		},
	})

	pre, err := entity.NewEntities([]entity.Entity{sopsEntity})
	require.NoError(t, err)

	expanded, err := ExpandSopsSecretsForUninstall(log.Default(), pre, types.KeyTemplateEntity)
	require.NoError(t, err)

	selected, err := entity.NewEntities([]entity.Entity{sopsEntity})
	require.NoError(t, err)

	out, err := AppendDerivedSopsSecretsForUninstall(selected, pre, expanded, types.KeyTemplateEntity)
	require.NoError(t, err)

	assert.Equal(t, 2, out.Len())
	ids := entityIds(t, out)
	assert.Contains(t, ids, types.Id("isindir.github.com/v1alpha3/SopsSecret/dex/argocd-secret"))
	assert.Contains(t, ids, types.Id("v1/Secret/dex/argocd-secret"))
}
