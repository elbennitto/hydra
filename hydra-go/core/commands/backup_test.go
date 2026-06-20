package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	kyaml "sigs.k8s.io/yaml"

	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	hydrasops "hydra-gitops.org/hydra/hydra-go/core/sops"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = writer
	defer func() {
		os.Stdout = oldStdout
	}()

	outputCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		outputCh <- buf.String()
	}()

	fn()

	require.NoError(t, writer.Close())
	output := <-outputCh
	require.NoError(t, reader.Close())

	return output
}

func withDiscardLogger(t *testing.T, fn func()) {
	t.Helper()

	oldDefault := slog.Default()
	oldLogger := log.Default()

	discardLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	slog.SetDefault(discardLogger)
	log.SetDefault(log.NewLogger())

	defer func() {
		log.SetDefault(oldLogger)
		slog.SetDefault(oldDefault)
	}()

	fn()
}

type captureLogHandler struct {
	level    slog.Level
	messages []string
}

func (h *captureLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *captureLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.messages = append(h.messages, r.Message)
	return nil
}

func (h *captureLogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureLogHandler) WithGroup(_ string) slog.Handler      { return h }

func withCaptureLogger(t *testing.T, level slog.Level, fn func(h *captureLogHandler)) {
	t.Helper()

	oldDefault := slog.Default()
	oldLogger := log.Default()

	handler := &captureLogHandler{level: level}
	slog.SetDefault(slog.New(handler))
	log.SetDefault(log.NewLoggerWithHandler(handler))

	defer func() {
		log.SetDefault(oldLogger)
		slog.SetDefault(oldDefault)
	}()

	fn(handler)
}

// --- normalizeSecretData tests ---

func TestNormalize_StringDataConvertedToBase64Data(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "test"},
		"stringData": map[string]any{
			"password": "supersecret",
		},
	}

	normalized := normalizeSecretData(obj)

	data, ok := normalized["data"].(map[string]any)
	require.True(t, ok, "data field must exist")
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("supersecret")), data["password"])
	_, hasStringData := normalized["stringData"]
	assert.False(t, hasStringData, "stringData must be removed")
}

func TestNormalize_DataPassedThrough(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("value"))
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "test"},
		"data": map[string]any{
			"key": encoded,
		},
	}

	normalized := normalizeSecretData(obj)

	data, ok := normalized["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, encoded, data["key"])
}

func TestNormalize_StringDataOverridesData(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"data": map[string]any{
			"key": "b2xkdmFsdWU=",
		},
		"stringData": map[string]any{
			"key": "newvalue",
		},
	}

	normalized := normalizeSecretData(obj)

	data := normalized["data"].(map[string]any)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("newvalue")), data["key"])
}

func TestNormalize_EmptyStringDataRemoved(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"stringData": map[string]any{},
	}

	normalized := normalizeSecretData(obj)

	_, hasData := normalized["data"]
	assert.False(t, hasData, "no data field when both data and stringData are empty")
	_, hasStringData := normalized["stringData"]
	assert.False(t, hasStringData)
}

func TestNormalize_MixedDataAndStringData(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"data": map[string]any{
			"existing": base64.StdEncoding.EncodeToString([]byte("fromdata")),
		},
		"stringData": map[string]any{
			"new": "fromstringdata",
		},
	}

	normalized := normalizeSecretData(obj)

	data := normalized["data"].(map[string]any)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("fromdata")), data["existing"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("fromstringdata")), data["new"])
}

func TestNormalize_ManagedAnnotationsFilteredFromMetadata(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      "test",
			"namespace": "default",
			"annotations": map[string]any{
				"custom.io/note": "keep-me",
				"kubectl.kubernetes.io/last-applied-configuration": `{"kind":"Secret"}`,
				"argocd.argoproj.io/tracking-id":                   "app:default/test",
				"helm.sh/resource-policy":                          "keep",
			},
		},
	}

	normalized := normalizeSecretData(obj)

	metadata, ok := normalized["metadata"].(map[string]any)
	require.True(t, ok, "metadata field must exist")
	annotations, ok := metadata["annotations"].(map[string]any)
	require.True(t, ok, "annotations field must exist when custom annotations remain")
	assert.Equal(t, map[string]any{
		"custom.io/note": "keep-me",
	}, annotations)
}

func TestNormalize_EmptyManagedAnnotationsRemovedFromMetadata(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      "test",
			"namespace": "default",
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": `{"kind":"Secret"}`,
				"argocd.argoproj.io/tracking-id":                   "app:default/test",
				"helm.sh/resource-policy":                          "keep",
			},
		},
	}

	normalized := normalizeSecretData(obj)

	metadata, ok := normalized["metadata"].(map[string]any)
	require.True(t, ok, "metadata field must exist")
	_, hasAnnotations := metadata["annotations"]
	assert.False(t, hasAnnotations, "metadata.annotations must be removed when only managed annotations were present")
}

// --- secretHashedYaml tests ---

func TestHashedYaml_ValuesReplacedWithHash(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "test"},
		"data": map[string]any{
			"tls.crt": "Y2VydGRhdGE=",
			"tls.key": "a2V5ZGF0YQ==",
		},
	}

	result := secretHashedYaml(obj)

	assert.Contains(t, result, "sha256:")
	assert.NotContains(t, result, "Y2VydGRhdGE=")
	assert.NotContains(t, result, "a2V5ZGF0YQ==")
	assert.Contains(t, result, "tls.crt")
	assert.Contains(t, result, "tls.key")
}

func TestHashedYaml_DeterministicHash(t *testing.T) {
	obj := map[string]any{
		"data": map[string]any{
			"key": "dmFsdWU=",
		},
	}

	result1 := secretHashedYaml(obj)
	result2 := secretHashedYaml(obj)

	assert.Equal(t, result1, result2)
}

func TestHashedYaml_DifferentValuesProduceDifferentHashes(t *testing.T) {
	obj1 := map[string]any{
		"data": map[string]any{"key": "dmFsdWUx"},
	}
	obj2 := map[string]any{
		"data": map[string]any{"key": "dmFsdWUy"},
	}

	result1 := secretHashedYaml(obj1)
	result2 := secretHashedYaml(obj2)

	assert.NotEqual(t, result1, result2)
}

func TestHashedYaml_MetadataPreserved(t *testing.T) {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "my-secret", "namespace": "default"},
		"type":       "kubernetes.io/tls",
		"data": map[string]any{
			"tls.crt": "Y2VydA==",
		},
	}

	result := secretHashedYaml(obj)

	assert.Contains(t, result, "apiVersion: v1")
	assert.Contains(t, result, "kind: Secret")
	assert.Contains(t, result, "name: my-secret")
	assert.Contains(t, result, "namespace: default")
	assert.Contains(t, result, "type: kubernetes.io/tls")
}

// --- backupDiff tests ---

func makeSecretObj(name, namespace string, data map[string]any, labels, annotations map[string]any) map[string]any {
	metadata := map[string]any{
		"name":      name,
		"namespace": namespace,
	}
	if labels != nil {
		metadata["labels"] = labels
	}
	if annotations != nil {
		metadata["annotations"] = annotations
	}
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   metadata,
	}
	if data != nil {
		obj["data"] = data
	}
	return obj
}

func TestDiff_IdenticalSecrets_NoDiff(t *testing.T) {
	obj := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil, nil)

	diff := backupDiff(obj, obj, "v1/Secret/default/test", types.ColorNo)
	assert.Empty(t, diff)
}

func TestDiff_DataValueChanged(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"key": "b2xk"},
		nil, nil)
	cluster := makeSecretObj("test", "default",
		map[string]any{"key": "bmV3"},
		nil, nil)

	diff := backupDiff(backup, cluster, "v1/Secret/default/test", types.ColorNo)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "sha256:")
	assert.Contains(t, diff, "--- backup/")
	assert.Contains(t, diff, "+++ cluster/")
}

func TestDiff_MultipleDataValuesChanged(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"key1": "b2xkMQ==", "key2": "b2xkMg=="},
		nil, nil)
	cluster := makeSecretObj("test", "default",
		map[string]any{"key1": "bmV3MQ==", "key2": "bmV3Mg=="},
		nil, nil)

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff)
	lines := strings.Split(diff, "\n")
	changedLines := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			changedLines++
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			changedLines++
		}
	}
	assert.GreaterOrEqual(t, changedLines, 4, "should show at least 4 changed lines (2 old + 2 new)")
}

func TestDiff_LabelsChanged(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		map[string]any{"app": "old"},
		nil)
	cluster := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		map[string]any{"app": "new"},
		nil)

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "old")
	assert.Contains(t, diff, "new")
}

func TestDiff_AnnotationsChanged(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil,
		map[string]any{"note": "before"})
	cluster := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil,
		map[string]any{"note": "after"})

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "before")
	assert.Contains(t, diff, "after")
}

func TestDiff_ManagedAnnotationDifferencesIgnored(t *testing.T) {
	testCases := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "kubectl last applied configuration",
			key:   "kubectl.kubernetes.io/last-applied-configuration",
			value: `{"kind":"Secret"}`,
		},
		{
			name:  "argocd tracking annotation",
			key:   "argocd.argoproj.io/tracking-id",
			value: "app:default/test",
		},
		{
			name:  "helm managed annotation",
			key:   "helm.sh/resource-policy",
			value: "keep",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backupRaw := makeSecretObj("test", "default",
				map[string]any{"key": "dmFsdWU="},
				nil,
				map[string]any{tc.key: tc.value + "-backup"})
			clusterRaw := makeSecretObj("test", "default",
				map[string]any{"key": "dmFsdWU="},
				nil,
				map[string]any{tc.key: tc.value + "-cluster"})

			backup := normalizeSecretData(backupRaw)
			cluster := normalizeSecretData(clusterRaw)
			diff := backupDiff(backup, cluster, "v1/Secret/default/test", types.ColorNo)
			assert.Empty(t, diff, "managed annotations should not produce a diff")
		})
	}
}

func TestDiff_ManagedAnnotationsRemovedWhenNoAnnotationsRemain(t *testing.T) {
	backupRaw := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil, nil)
	clusterRaw := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil,
		map[string]any{
			"kubectl.kubernetes.io/last-applied-configuration": `{"kind":"Secret"}`,
		})

	backup := normalizeSecretData(backupRaw)
	cluster := normalizeSecretData(clusterRaw)
	diff := backupDiff(backup, cluster, "v1/Secret/default/test", types.ColorNo)
	assert.Empty(t, diff, "managed annotations should be filtered and empty annotations blocks should be removed")

	clusterMetadata, ok := cluster["metadata"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, clusterMetadata, "annotations", "empty metadata.annotations blocks must be removed after filtering")
}

func TestDiff_CustomAnnotationDifferenceRemainsWhenManagedAnnotationsAreIgnored(t *testing.T) {
	backupRaw := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil,
		map[string]any{
			"custom.io/note": "before",
			"kubectl.kubernetes.io/last-applied-configuration": `{"kind":"Secret","version":"backup"}`,
			"argocd.argoproj.io/tracking-id":                   "app:default/test-backup",
			"helm.sh/resource-policy":                          "keep-backup",
		})
	clusterRaw := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil,
		map[string]any{
			"custom.io/note": "after",
			"kubectl.kubernetes.io/last-applied-configuration": `{"kind":"Secret","version":"cluster"}`,
			"argocd.argoproj.io/tracking-id":                   "app:default/test-cluster",
			"helm.sh/resource-policy":                          "keep-cluster",
		})

	backup := normalizeSecretData(backupRaw)
	cluster := normalizeSecretData(clusterRaw)
	diff := backupDiff(backup, cluster, "v1/Secret/default/test", types.ColorNo)
	assert.NotEmpty(t, diff, "custom annotation differences must still produce a diff")
	assert.Contains(t, diff, "before")
	assert.Contains(t, diff, "after")
	assert.NotContains(t, diff, "kubectl.kubernetes.io/last-applied-configuration")
	assert.NotContains(t, diff, "argocd.argoproj.io/tracking-id")
	assert.NotContains(t, diff, "helm.sh/resource-policy")
}

func TestDiff_LabelsAdded(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil, nil)
	cluster := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		map[string]any{"env": "prod"},
		nil)

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "env")
	assert.Contains(t, diff, "prod")
}

func TestDiff_LabelsRemoved(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		map[string]any{"env": "prod"},
		nil)
	cluster := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil, nil)

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "env")
}

func TestDiff_MoreDataKeysInCluster(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"key1": "dmFsdWU="},
		nil, nil)
	cluster := makeSecretObj("test", "default",
		map[string]any{"key1": "dmFsdWU=", "key2": "bmV3"},
		nil, nil)

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "key2")
}

func TestDiff_FewerDataKeysInCluster(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"key1": "dmFsdWU=", "key2": "ZXh0cmE="},
		nil, nil)
	cluster := makeSecretObj("test", "default",
		map[string]any{"key1": "dmFsdWU="},
		nil, nil)

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "key2")
}

func TestDiff_DataKeyRenamed(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"old-key": "dmFsdWU="},
		nil, nil)
	cluster := makeSecretObj("test", "default",
		map[string]any{"new-key": "dmFsdWU="},
		nil, nil)

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "old-key")
	assert.Contains(t, diff, "new-key")
}

func TestDiff_SecretTypeChanged(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil, nil)
	backup["type"] = "Opaque"

	cluster := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil, nil)
	cluster["type"] = "kubernetes.io/tls"

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "Opaque")
	assert.Contains(t, diff, "kubernetes.io/tls")
}

func TestDiff_OnlyMetadataChanged_DataIdentical(t *testing.T) {
	backup := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		map[string]any{"version": "1"},
		nil)
	cluster := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		map[string]any{"version": "2"},
		nil)

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff, "diff should exist for metadata change")
	// data hashes should be the same, so the diff should only show metadata
	assert.Contains(t, diff, "version")
}

func TestDiff_EmptyDataVsPopulatedData(t *testing.T) {
	backup := makeSecretObj("test", "default", nil, nil, nil)
	cluster := makeSecretObj("test", "default",
		map[string]any{"key": "dmFsdWU="},
		nil, nil)

	diff := backupDiff(backup, cluster, "test", types.ColorNo)
	assert.NotEmpty(t, diff)
	assert.Contains(t, diff, "key")
}

func TestDiff_StringDataNormalization(t *testing.T) {
	// Backup from SopsSecret has stringData
	backupRaw := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "test", "namespace": "default"},
		"stringData": map[string]any{
			"password": "supersecret",
		},
	}

	// Cluster has equivalent data in base64
	clusterRaw := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]any{"name": "test", "namespace": "default"},
		"data": map[string]any{
			"password": base64.StdEncoding.EncodeToString([]byte("supersecret")),
		},
	}

	backupNormalized := normalizeSecretData(backupRaw)
	clusterNormalized := normalizeSecretData(clusterRaw)

	diff := backupDiff(backupNormalized, clusterNormalized, "test", types.ColorNo)
	assert.Empty(t, diff, "after normalization, stringData and equivalent base64 data should match")
}

func TestRestoreSingleBackup_IdenticalAfterNormalizationReturnsUpToDate(t *testing.T) {
	withDiscardLogger(t, func() {
		backupFile := filepath.Join(t.TempDir(), "my-secret-backup.sops.yaml")
		writeEncryptedBackupSelectionFile(t, backupFile, "default", "my-secret")

		clusterSecret := unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]any{
					"name":      "my-secret",
					"namespace": "default",
					"annotations": map[string]any{
						"kubectl.kubernetes.io/last-applied-configuration": `{"kind":"Secret"}`,
					},
				},
				"data": map[string]any{
					"password": base64.StdEncoding.EncodeToString([]byte("test")),
				},
			},
		}
		secrets := &clusterSecrets{
			byId: map[string]unstructured.Unstructured{
				"v1/Secret/default/my-secret": clusterSecret,
			},
		}

		result, err := restoreSingleBackup(log.Default(), nil, secrets, backupFile, false, types.ColorNo, types.DryRunNo)
		require.NoError(t, err)
		assert.Equal(t, "v1/Secret/default/my-secret", result.SecretId)
		assert.Equal(t, BackupStatusUpToDate, result.Status)
		assert.NotEqual(t, BackupStatusAlreadyExists, result.Status)
		assert.Empty(t, result.Diff)
	})
}

func TestRestoreSingleBackup_DifferentAfterNormalizationReturnsWouldOverwrite(t *testing.T) {
	withDiscardLogger(t, func() {
		backupFile := filepath.Join(t.TempDir(), "my-secret-backup.sops.yaml")
		writeEncryptedBackupSelectionFile(t, backupFile, "default", "my-secret")

		clusterSecret := unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]any{
					"name":      "my-secret",
					"namespace": "default",
					"annotations": map[string]any{
						"kubectl.kubernetes.io/last-applied-configuration": `{"kind":"Secret"}`,
					},
				},
				"data": map[string]any{
					"password": base64.StdEncoding.EncodeToString([]byte("different")),
				},
			},
		}
		secrets := &clusterSecrets{
			byId: map[string]unstructured.Unstructured{
				"v1/Secret/default/my-secret": clusterSecret,
			},
		}

		result, err := restoreSingleBackup(log.Default(), nil, secrets, backupFile, false, types.ColorNo, types.DryRunNo)
		require.NoError(t, err)
		assert.Equal(t, "v1/Secret/default/my-secret", result.SecretId)
		assert.Equal(t, BackupStatusWouldOverwrite, result.Status)
		assert.NotEqual(t, BackupStatusUpToDate, result.Status)
		assert.NotEmpty(t, result.Diff)
	})
}

// --- convertSecretToSopsSecretYaml tests ---

func TestConvert_BasicSecret(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "my-secret",
				"namespace": "default",
			},
			"data": map[string]any{
				"key": "dmFsdWU=",
			},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	yaml := string(yamlStr)
	assert.Contains(t, yaml, "apiVersion: isindir.github.com/v1alpha3")
	assert.Contains(t, yaml, "kind: SopsSecret")
	assert.Contains(t, yaml, "name: my-secret-backup")
	assert.Contains(t, yaml, "secretTemplates")
}

func TestConvert_BackupAnnotationSet(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "my-secret",
				"namespace": "default",
			},
			"data": map[string]any{"key": "dmFsdWU="},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	yaml := string(yamlStr)
	assert.Contains(t, yaml, hydra.AnnotationHydraBackup+`: "true"`)
}

func TestConvert_SecretTypePreserved(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "tls-secret",
				"namespace": "default",
			},
			"type": "kubernetes.io/tls",
			"data": map[string]any{
				"tls.crt": "Y2VydA==",
				"tls.key": "a2V5",
			},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	yaml := string(yamlStr)
	assert.Contains(t, yaml, "type: kubernetes.io/tls")
}

func TestConvert_NamespacePreserved(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "test",
				"namespace": "cert-manager",
			},
			"data": map[string]any{"key": "dmFsdWU="},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	yaml := string(yamlStr)
	assert.Contains(t, yaml, "namespace: cert-manager")
}

func TestRoundtrip_RestoreTemplateNeedsNamespaceContext(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "wildcard-tls",
				"namespace": "missing-ns",
			},
			"data": map[string]any{
				"tls.crt": "Y2VydA==",
			},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	sopsObj, err := parseYamlToMap(string(yamlStr))
	require.NoError(t, err)

	templates, err := extractSecretTemplates(sopsObj)
	require.NoError(t, err)
	require.Len(t, templates, 1)

	restoredWithoutNamespace := buildSecretUnstructured(templates[0], "")
	restoredWithNamespace := buildSecretUnstructured(templates[0], "missing-ns")

	assert.Empty(t, restoredWithoutNamespace.GetNamespace(),
		"without restore context, the template itself does not inject a namespace")
	assert.Equal(t, "missing-ns", restoredWithNamespace.GetNamespace(),
		"once restore context is allowed to target the namespace, the same template becomes namespaced")
	assert.Equal(t, restoredWithoutNamespace.GetName(), restoredWithNamespace.GetName())
	assert.Equal(t, restoredWithoutNamespace.Object["data"], restoredWithNamespace.Object["data"])
}

func TestConvert_LabelsAndAnnotationsPreserved(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "test",
				"namespace": "default",
				"labels": map[string]any{
					"app":                         "myapp",
					"cert-manager.io/issuer-name": "letsencrypt",
				},
				"annotations": map[string]any{
					"cert-manager.io/issuer-name": "letsencrypt-prod",
				},
			},
			"data": map[string]any{"key": "dmFsdWU="},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	yaml := string(yamlStr)
	assert.Contains(t, yaml, "app: myapp")
	assert.Contains(t, yaml, "cert-manager.io/issuer-name")
}

func TestConvert_SuspendSetToTrue(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "test",
				"namespace": "default",
			},
			"data": map[string]any{"key": "dmFsdWU="},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	yaml := string(yamlStr)
	assert.Contains(t, yaml, "suspend: true")
}

// --- backup/restore roundtrip tests for labels and annotations ---

func TestRoundtrip_LabelsPreservedThroughBackupRestore(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "my-tls",
				"namespace": "cert-manager",
				"labels": map[string]any{
					"app":                         "nginx",
					"cert-manager.io/issuer-name": "letsencrypt-prod",
					"environment":                 "production",
				},
			},
			"type": "kubernetes.io/tls",
			"data": map[string]any{
				"tls.crt": "Y2VydGRhdGE=",
				"tls.key": "a2V5ZGF0YQ==",
			},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	sopsObj, err := parseYamlToMap(string(yamlStr))
	require.NoError(t, err)

	templates, err := extractSecretTemplates(sopsObj)
	require.NoError(t, err)
	require.Len(t, templates, 1)

	restored := buildSecretUnstructured(templates[0], "cert-manager")

	restoredLabels := restored.GetLabels()
	assert.Equal(t, "nginx", restoredLabels["app"])
	assert.Equal(t, "letsencrypt-prod", restoredLabels["cert-manager.io/issuer-name"])
	assert.Equal(t, "production", restoredLabels["environment"])
}

func TestRoundtrip_AnnotationsPreservedThroughBackupRestore(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "my-tls",
				"namespace": "cert-manager",
				"annotations": map[string]any{
					"cert-manager.io/issuer-name": "letsencrypt-prod",
					"cert-manager.io/issuer-kind": "ClusterIssuer",
					"cert-manager.io/common-name": "*.example.com",
					"cert-manager.io/alt-names":   "*.example.com,example.com",
				},
			},
			"data": map[string]any{"tls.crt": "Y2VydA=="},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	sopsObj, err := parseYamlToMap(string(yamlStr))
	require.NoError(t, err)

	templates, err := extractSecretTemplates(sopsObj)
	require.NoError(t, err)
	require.Len(t, templates, 1)

	restored := buildSecretUnstructured(templates[0], "cert-manager")

	restoredAnnotations := restored.GetAnnotations()
	assert.Equal(t, "letsencrypt-prod", restoredAnnotations["cert-manager.io/issuer-name"])
	assert.Equal(t, "ClusterIssuer", restoredAnnotations["cert-manager.io/issuer-kind"])
	assert.Equal(t, "*.example.com", restoredAnnotations["cert-manager.io/common-name"])
	assert.Equal(t, "*.example.com,example.com", restoredAnnotations["cert-manager.io/alt-names"])
}

func TestRoundtrip_LabelsAndAnnotationsTogether(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "combined-secret",
				"namespace": "default",
				"labels": map[string]any{
					"app":     "myapp",
					"release": "v1.2.3",
				},
				"annotations": map[string]any{
					"custom.io/managed-by": "hydra",
					"custom.io/version":    "3",
				},
			},
			"data": map[string]any{"password": "c2VjcmV0"},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	sopsObj, err := parseYamlToMap(string(yamlStr))
	require.NoError(t, err)

	templates, err := extractSecretTemplates(sopsObj)
	require.NoError(t, err)
	require.Len(t, templates, 1)

	restored := buildSecretUnstructured(templates[0], "default")

	restoredLabels := restored.GetLabels()
	assert.Equal(t, "myapp", restoredLabels["app"])
	assert.Equal(t, "v1.2.3", restoredLabels["release"])

	restoredAnnotations := restored.GetAnnotations()
	assert.Equal(t, "hydra", restoredAnnotations["custom.io/managed-by"])
	assert.Equal(t, "3", restoredAnnotations["custom.io/version"])
}

func TestRoundtrip_KubernetesLabelsFilteredOut(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "test",
				"namespace": "default",
				"labels": map[string]any{
					"app":                              "myapp",
					"app.kubernetes.io/managed-by":     "Helm",
					"helm.sh/chart":                    "mychart-1.0",
					"app.kubernetes.io/instance":       "release",
					"node.kubernetes.io/instance-type": "m5.large",
					"topology.kubernetes.io/zone":      "eu-west-1a",
				},
			},
			"data": map[string]any{"key": "dmFsdWU="},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	sopsObj, err := parseYamlToMap(string(yamlStr))
	require.NoError(t, err)

	templates, err := extractSecretTemplates(sopsObj)
	require.NoError(t, err)
	require.Len(t, templates, 1)

	restored := buildSecretUnstructured(templates[0], "default")

	restoredLabels := restored.GetLabels()
	assert.Equal(t, "myapp", restoredLabels["app"], "custom labels should be preserved")
	assert.Equal(t, "Helm", restoredLabels["app.kubernetes.io/managed-by"], "app.kubernetes.io/ labels should be preserved")
	assert.Equal(t, "release", restoredLabels["app.kubernetes.io/instance"], "app.kubernetes.io/ labels should be preserved")
	assert.Empty(t, restoredLabels["helm.sh/chart"], "helm.sh labels should be filtered")
	assert.Empty(t, restoredLabels["node.kubernetes.io/instance-type"], "node.kubernetes.io labels should be filtered")
	assert.Empty(t, restoredLabels["topology.kubernetes.io/zone"], "topology.kubernetes.io labels should be filtered")
}

func TestRoundtrip_CertManagerLabelsPreserved(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "letsencrypt-prod",
				"namespace": "cert-manager",
				"labels": map[string]any{
					"app.kubernetes.io/managed-by": "cert-manager",
				},
			},
			"type": "kubernetes.io/tls",
			"data": map[string]any{
				"tls.crt": "Y2VydGRhdGE=",
				"tls.key": "a2V5ZGF0YQ==",
			},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	sopsObj, err := parseYamlToMap(string(yamlStr))
	require.NoError(t, err)

	templates, err := extractSecretTemplates(sopsObj)
	require.NoError(t, err)
	require.Len(t, templates, 1)

	restored := buildSecretUnstructured(templates[0], "cert-manager")

	restoredLabels := restored.GetLabels()
	assert.Equal(t, "cert-manager", restoredLabels["app.kubernetes.io/managed-by"],
		"app.kubernetes.io/managed-by label from cert-manager must be preserved in backup")
}

func TestFilterBackupLabels_AppKubernetesIoPreserved(t *testing.T) {
	labels := map[string]string{
		"app":                              "nginx",
		"app.kubernetes.io/managed-by":     "cert-manager",
		"app.kubernetes.io/name":           "my-app",
		"app.kubernetes.io/instance":       "release-1",
		"app.kubernetes.io/version":        "1.0.0",
		"app.kubernetes.io/component":      "server",
		"app.kubernetes.io/part-of":        "platform",
		"kubernetes.io/os":                 "linux",
		"node.kubernetes.io/instance-type": "m5.large",
		"helm.sh/chart":                    "mychart-1.0",
	}

	filtered := filterBackupLabels(labels)

	assert.Equal(t, "nginx", filtered["app"])
	assert.Equal(t, "cert-manager", filtered["app.kubernetes.io/managed-by"])
	assert.Equal(t, "my-app", filtered["app.kubernetes.io/name"])
	assert.Equal(t, "release-1", filtered["app.kubernetes.io/instance"])
	assert.Equal(t, "1.0.0", filtered["app.kubernetes.io/version"])
	assert.Equal(t, "server", filtered["app.kubernetes.io/component"])
	assert.Equal(t, "platform", filtered["app.kubernetes.io/part-of"])
	assert.Empty(t, filtered["kubernetes.io/os"], "bare kubernetes.io/ labels should be filtered")
	assert.Empty(t, filtered["node.kubernetes.io/instance-type"], "node.kubernetes.io/ labels should be filtered")
	assert.Empty(t, filtered["helm.sh/chart"], "helm.sh/ labels should be filtered")
}

func TestFilterBackupAnnotations_PreservesCustomAnnotations(t *testing.T) {
	annotations := map[string]string{
		"cert-manager.io/issuer-name":                      "letsencrypt-prod",
		"cert-manager.io/issuer-kind":                      "ClusterIssuer",
		"custom.io/managed-by":                             "hydra",
		"kubectl.kubernetes.io/last-applied-configuration": `{"big":"json"}`,
		"argocd.argoproj.io/tracking-id":                   "some-id",
		"helm.sh/resource-policy":                          "keep",
	}

	filtered := filterBackupAnnotations(annotations)

	assert.Equal(t, "letsencrypt-prod", filtered["cert-manager.io/issuer-name"])
	assert.Equal(t, "ClusterIssuer", filtered["cert-manager.io/issuer-kind"])
	assert.Equal(t, "hydra", filtered["custom.io/managed-by"])
	assert.Empty(t, filtered["kubectl.kubernetes.io/last-applied-configuration"], "kubectl annotations should be filtered")
	assert.Empty(t, filtered["argocd.argoproj.io/tracking-id"], "argocd annotations should be filtered")
	assert.Empty(t, filtered["helm.sh/resource-policy"], "helm annotations should be filtered")
}

func TestFilterBackupAnnotations_StripsSopsSecretManaged(t *testing.T) {
	filtered := filterBackupAnnotations(map[string]string{
		sopsSecretManagedAnnotationKey: sopsSecretManagedAnnotationValue,
	})
	assert.Empty(t, filtered)
}

func TestFilterBackupAnnotations_EmptyInput(t *testing.T) {
	filtered := filterBackupAnnotations(map[string]string{})
	assert.Empty(t, filtered)
}

func TestFilterBackupLabels_EmptyInput(t *testing.T) {
	filtered := filterBackupLabels(map[string]string{})
	assert.Empty(t, filtered)
}

func TestPrintBackupResults_SkippedRestoreOmitsLegacyScopeWarning(t *testing.T) {
	output := captureStdout(t, func() {
		PrintBackupResults(log.Default(), []BackupResult{
			{
				SecretId: "v1/Secret/missing-ns/wildcard-tls",
				Status:   BackupStatus("skipped"),
			},
		}, types.ColorNo)
	})

	assert.Contains(t, output, "Backup overview:")
	assert.Contains(t, output, "v1/Secret/missing-ns/wildcard-tls")
	assert.Contains(t, strings.ToLower(output), "skipped")
	assert.NotContains(t, strings.ToLower(output), "warn")
	assert.NotContains(t, output, "--all")
	assert.NotContains(t, output, "--include")
	assert.NotContains(t, output, "--exclude")
}

func TestPrintBackupResults_RestoredMissingNamespaceCaseOmitsScopeWarning(t *testing.T) {
	output := captureStdout(t, func() {
		PrintBackupResults(log.Default(), []BackupResult{
			{
				SecretId: "v1/Secret/missing-ns/wildcard-tls",
				Status:   BackupStatusRestored,
			},
		}, types.ColorNo)
	})

	assert.Contains(t, output, "v1/Secret/missing-ns/wildcard-tls")
	assert.Contains(t, output, "restored")
	assert.NotContains(t, strings.ToLower(output), "skipped")
	assert.NotContains(t, output, "--all")
	assert.NotContains(t, output, "--include")
	assert.NotContains(t, output, "--exclude")
}

func TestCollectMissingBackupRestoreNamespaces_FindsOnlyAbsentTargets(t *testing.T) {
	candidates := []backupRestoreCandidate{
		{
			secretId:  "v1/Secret/existing-ns/existing-secret",
			secretObj: map[string]any{"metadata": map[string]any{"namespace": "existing-ns", "name": "existing-secret"}},
		},
		{
			secretId:  "v1/Secret/missing-ns/new-secret",
			secretObj: map[string]any{"metadata": map[string]any{"namespace": "missing-ns", "name": "new-secret"}},
		},
		{
			secretId:  "v1/Secret/missing-ns/other-secret",
			secretObj: map[string]any{"metadata": map[string]any{"namespace": "missing-ns", "name": "other-secret"}},
		},
		{
			secretId:  "v1/Secret//cluster-secret",
			secretObj: map[string]any{"metadata": map[string]any{"name": "cluster-secret"}},
		},
	}

	missing := collectMissingBackupRestoreNamespaces(
		candidates,
		sets.New(types.Namespace("existing-ns"), types.Namespace("kube-system")),
	)

	assert.True(t, missing.Has(types.Namespace("missing-ns")))
	assert.False(t, missing.Has(types.Namespace("existing-ns")))
	assert.False(t, missing.Has(types.Namespace("")))
	assert.Equal(t, 1, missing.Len(), "missing namespace collection should deduplicate target namespaces")
}

func TestRoundtrip_ManagedAnnotationsFilteredOut(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "test",
				"namespace": "default",
				"annotations": map[string]any{
					"cert-manager.io/issuer-name":                      "letsencrypt-prod",
					"kubectl.kubernetes.io/last-applied-configuration": `{"big":"json"}`,
					"argocd.argoproj.io/tracking-id":                   "some-tracking-id",
					"helm.sh/resource-policy":                          "keep",
				},
			},
			"data": map[string]any{"key": "dmFsdWU="},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	sopsObj, err := parseYamlToMap(string(yamlStr))
	require.NoError(t, err)

	templates, err := extractSecretTemplates(sopsObj)
	require.NoError(t, err)
	require.Len(t, templates, 1)

	restored := buildSecretUnstructured(templates[0], "default")

	restoredAnnotations := restored.GetAnnotations()
	assert.Equal(t, "letsencrypt-prod", restoredAnnotations["cert-manager.io/issuer-name"], "custom annotations should be preserved")
	assert.Empty(t, restoredAnnotations["kubectl.kubernetes.io/last-applied-configuration"], "kubectl annotations should be filtered")
	assert.Empty(t, restoredAnnotations["argocd.argoproj.io/tracking-id"], "argocd annotations should be filtered")
	assert.Empty(t, restoredAnnotations["helm.sh/resource-policy"], "helm annotations should be filtered")
}

func TestRoundtrip_DataAndTypePreserved(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "my-tls",
				"namespace": "cert-manager",
				"labels": map[string]any{
					"app": "nginx",
				},
				"annotations": map[string]any{
					"cert-manager.io/issuer-name": "letsencrypt-prod",
				},
			},
			"type": "kubernetes.io/tls",
			"data": map[string]any{
				"tls.crt": "Y2VydGRhdGE=",
				"tls.key": "a2V5ZGF0YQ==",
				"ca.crt":  "Y2FkYXRh",
			},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	sopsObj, err := parseYamlToMap(string(yamlStr))
	require.NoError(t, err)

	templates, err := extractSecretTemplates(sopsObj)
	require.NoError(t, err)
	require.Len(t, templates, 1)

	restored := buildSecretUnstructured(templates[0], "cert-manager")

	assert.Equal(t, "v1", restored.GetAPIVersion())
	assert.Equal(t, "Secret", restored.GetKind())
	assert.Equal(t, "my-tls", restored.GetName())
	assert.Equal(t, "cert-manager", restored.GetNamespace())

	restoredType, _, _ := unstructured.NestedString(restored.Object, "type")
	assert.Equal(t, "kubernetes.io/tls", restoredType)

	data := restored.Object["data"].(map[string]any)
	assert.Equal(t, "Y2VydGRhdGE=", data["tls.crt"])
	assert.Equal(t, "a2V5ZGF0YQ==", data["tls.key"])
	assert.Equal(t, "Y2FkYXRh", data["ca.crt"])

	assert.Equal(t, "nginx", restored.GetLabels()["app"])
	assert.Equal(t, "letsencrypt-prod", restored.GetAnnotations()["cert-manager.io/issuer-name"])
}

func TestRoundtrip_EmptyLabelsAndAnnotations(t *testing.T) {
	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "bare-secret",
				"namespace": "default",
			},
			"data": map[string]any{"key": "dmFsdWU="},
		},
	}

	yamlStr, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)

	sopsObj, err := parseYamlToMap(string(yamlStr))
	require.NoError(t, err)

	templates, err := extractSecretTemplates(sopsObj)
	require.NoError(t, err)
	require.Len(t, templates, 1)

	restored := buildSecretUnstructured(templates[0], "default")

	assert.Empty(t, restored.GetLabels())
	// buildSecretUnstructured injects sopssecret/managed so the sops-secrets operator can adopt the Secret.
	assert.Equal(t, map[string]string{
		sopsSecretManagedAnnotationKey: sopsSecretManagedAnnotationValue,
	}, restored.GetAnnotations())
}

func writeBackupSelectionTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func writeEncryptedBackupSelectionFile(t *testing.T, path, namespace, name string) {
	t.Helper()

	secret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"stringData": map[string]any{
				"password": "test",
			},
		},
	}

	sopsSecretYaml, err := convertSecretToSopsSecretYaml(secret)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	configPath := writeBackupTestSopsConfig(t)
	overridePath := filepath.Join(t.TempDir(), "test-backup.sops.yaml")
	encrypted, err := hydrasops.EncryptSopsYamlWithConfig(sopsSecretYaml, overridePath, configPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(encrypted), 0644))
}

func writeBackupTestSopsConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".sops.yaml")
	writeBackupSelectionTestFile(t, path, `creation_rules:
  - path_regex: .*\.sops\.yaml$
    age: >-
      ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMvrL1CJG09aLg6F4A2+TXINIKZRVHr8KUVoHDy0FWXB Dennis Rieks <dennis.rieks@rohde-schwarz.com>,
      age1xqq5gh0k7gcv5cgdu85m4nc0yft4dp9wu62dd4rcfzmwknrc0u9sc3s3cw,
      age1gytlmwd26fgj3ddrgl3z0lrufk549f7sc7lzajalxksdqtceqvdqevhf83
`)
	return path
}

func writeBackupSelectionChart(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0755))
	writeBackupSelectionTestFile(t, filepath.Join(dir, "Chart.yaml"), "apiVersion: v2\nname: "+name+"\nversion: 0.1.0\n")
	writeBackupSelectionTestFile(t, filepath.Join(dir, "values.yaml"), "{}\n")
}

func writeBackupSelectionWrapperChart(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0755))
	writeBackupSelectionTestFile(t, filepath.Join(dir, "Chart.yaml"), `apiVersion: v2
name: `+name+`
version: 0.1.0
dependencies:
  - name: `+name+`-dev
    version: 0.1.0
    repository: file://./dev
`)
	writeBackupSelectionTestFile(t, filepath.Join(dir, "values.yaml"), "{}\n")
}

func backupSelectionManifest(name, namespace, templateName string) string {
	return `apiVersion: isindir.github.com/v1alpha3
kind: SopsSecret
metadata:
  name: ` + name + `
  namespace: ` + namespace + `
  annotations:
    hydra-gitops.org/hydra-backup: "true"
spec:
  suspend: true
  secretTemplates:
    - name: ` + templateName + `
      stringData:
        password: ENC[AES256_GCM,data:test]
`
}

func writeBackupSelectionRootApp(t *testing.T, dir string) {
	t.Helper()

	writeBackupSelectionTestFile(t, filepath.Join(dir, "Chart.yaml"), `apiVersion: v2
name: argocd
version: 0.1.0
dependencies:
  - name: root-stage
    version: 0.1.0
    repository: file://../root/dev
`)
	writeBackupSelectionTestFile(t, filepath.Join(dir, "values.yaml"), `argocd:
  apps:
    app-a:
      namespace: shared-ns
    app-b:
      namespace: shared-ns
app-a:
  global:
    hydra:
      path: apps/app-a
      refs:
        backup:
          tag:
            - backup
          ref-parsers:
            - predicate: 'id == "v1/Secret/shared-ns/app-a-secret"'
app-b:
  global:
    hydra:
      path: apps/app-b
      refs:
        backup:
          tag:
            - backup
          ref-parsers:
            - predicate: 'id == "v1/Secret/shared-ns/app-b-secret"'
global:
  hydra:
    type: root-app
`)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0755))
}

func buildBackupSelectionTestCluster(t *testing.T) (*hydra.Cluster, types.AppId, types.AppId) {
	t.Helper()

	contextDir := filepath.Join(t.TempDir(), "gitops")
	writeBackupSelectionTestFile(t, filepath.Join(contextDir, "values.yaml"), "global:\n  hydra:\n    type: context\n")
	writeBackupSelectionTestFile(t, filepath.Join(contextDir, "in-cluster", "values.yaml"), "global:\n  hydra:\n    type: cluster\n")
	writeBackupSelectionRootApp(t, filepath.Join(contextDir, "in-cluster", "argocd"))
	writeBackupSelectionWrapperChart(t, filepath.Join(contextDir, "in-cluster", "root"), "root")
	writeBackupSelectionWrapperChart(t, filepath.Join(contextDir, "in-cluster", "app-a"), "app-a")
	writeBackupSelectionWrapperChart(t, filepath.Join(contextDir, "in-cluster", "app-b"), "app-b")
	writeBackupSelectionChart(t, filepath.Join(contextDir, "in-cluster", "root", "dev"), "root-stage")
	writeBackupSelectionChart(t, filepath.Join(contextDir, "in-cluster", "app-a", "dev"), "app-a-dev")
	writeBackupSelectionChart(t, filepath.Join(contextDir, "in-cluster", "app-b", "dev"), "app-b-dev")

	writeBackupSelectionTestFile(
		t,
		filepath.Join(contextDir, "in-cluster", "argocd", "apps", "app-a", "backup-shared-ns-app-a-secret.sops.yaml"),
		backupSelectionManifest("app-a-secret-backup", "shared-ns", "app-a-secret"),
	)
	writeBackupSelectionTestFile(
		t,
		filepath.Join(contextDir, "in-cluster", "argocd", "apps", "app-b", "backup-shared-ns-app-b-secret.sops.yaml"),
		backupSelectionManifest("app-b-secret-backup", "shared-ns", "app-b-secret"),
	)

	config := types.NewConfig(types.ColorNo, types.DryRunNo, types.KubernetesConnectionAllowedNo, true)
	context, err := hydra.CreateContext(log.Default(), contextDir, config)
	require.NoError(t, err)

	cluster, err := context.WithCluster(types.InCluster, hydra.RESTClientLimits{})
	require.NoError(t, err)

	appA := types.NewChildAppId(types.InCluster, types.RootAppName("argocd"), types.ChildAppName("app-a"))
	appB := types.NewChildAppId(types.InCluster, types.RootAppName("argocd"), types.ChildAppName("app-b"))

	return cluster, appA, appB
}

func TestCollectBackupSopsSecrets_SelectedChildAppIgnoresSiblingBackupsInSharedNamespace(t *testing.T) {
	withDiscardLogger(t, func() {
		cluster, appA, appB := buildBackupSelectionTestCluster(t)

		backups, err := BackupSopsSecrets(cluster, []types.AppId{appA}, types.HelmNetworkModeLocal, "")
		require.NoError(t, err)
		require.Len(t, backups, 1, "apply-scoped restore must only consider backup manifests rendered for the selected child app")

		assert.Equal(t, "v1/Secret/shared-ns/app-a-secret", backups[0].SecretId)
		assert.Contains(t, backups[0].AbsPath, filepath.Join("argocd", "apps", "app-a"))
		assert.NotContains(t, backups[0].AbsPath, filepath.Join("argocd", "apps", "app-b"))

		siblingBackups, err := BackupSopsSecrets(cluster, []types.AppId{appB}, types.HelmNetworkModeLocal, "")
		require.NoError(t, err)
		require.Len(t, siblingBackups, 1)
		assert.Equal(t, "v1/Secret/shared-ns/app-b-secret", siblingBackups[0].SecretId)
	})
}

func TestBackupCreateWithOptions_SkipFoundDefinitionsInfoLog(t *testing.T) {
	t.Run("default logs rendered definitions", func(t *testing.T) {
		withCaptureLogger(t, slog.LevelInfo, func(h *captureLogHandler) {
			cluster, appA, _ := buildBackupSelectionTestCluster(t)

			_, err := BackupCreateWithOptions(cluster, []types.AppId{appA}, types.HelmNetworkModeLocal, types.ColorNo, types.DryRunNo, BackupCreateOptions{})
			require.Error(t, err)

			assert.Contains(t, h.messages, "found definitions of {resources} resources stored in {apps} apps")
		})
	})

	t.Run("option suppresses rendered definitions log", func(t *testing.T) {
		withCaptureLogger(t, slog.LevelInfo, func(h *captureLogHandler) {
			cluster, appA, _ := buildBackupSelectionTestCluster(t)

			_, err := BackupCreateWithOptions(cluster, []types.AppId{appA}, types.HelmNetworkModeLocal, types.ColorNo, types.DryRunNo, BackupCreateOptions{
				SkipFoundDefinitionsInfoLog: true,
			})
			require.Error(t, err)

			assert.NotContains(t, h.messages, "found definitions of {resources} resources stored in {apps} apps")
		})
	})
}

func TestValidateBackupCreateOwnership_FailsForForeignNamespace(t *testing.T) {
	appA := types.NewChildAppId(types.InCluster, types.RootAppName("argocd"), types.ChildAppName("app-a"))
	group := backupGroup{
		AppId:        appA,
		ChildAppName: "app-a",
		AppNamespace: types.Namespace("app-a-ns"),
	}

	foreignSecret := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "app-b-secret",
				"namespace": "app-b-ns",
			},
		},
	}

	err := validateBackupCreateOwnership(group, foreignSecret)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "app-a")
	assert.Contains(t, err.Error(), "app-b-ns")
}

func TestCollectBackupRestoreCandidates_SkipsForeignNamespaceBackups(t *testing.T) {
	appA := types.NewChildAppId(types.InCluster, types.RootAppName("argocd"), types.ChildAppName("app-a"))
	backupFile := filepath.Join(t.TempDir(), "backup-app-b-secret.sops.yaml")
	writeEncryptedBackupSelectionFile(t, backupFile, "app-b-ns", "app-b-secret")

	candidates, skipped, err := collectBackupRestoreCandidates([]BackupSopsSecretInfo{
		{
			SecretId:     "v1/Secret/app-b-ns/app-b-secret",
			AbsPath:      backupFile,
			AppId:        appA,
			AppNamespace: types.Namespace("app-a-ns"),
		},
	}, nil)
	require.NoError(t, err)
	assert.Empty(t, candidates, "ownership-invalid backups must not continue into restore apply candidates")
	require.Len(t, skipped, 1)
	assert.Equal(t, "v1/Secret/app-b-ns/app-b-secret", skipped[0].SecretId)
	assert.Equal(t, BackupStatusSkipped, skipped[0].Status)
}

// parseYamlToMap is a test helper that parses YAML into a map.
func parseYamlToMap(yamlStr string) (map[string]any, error) {
	var result map[string]any
	if err := kyaml.Unmarshal([]byte(yamlStr), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// --- helper function tests ---

func TestFilterSecretsByPredicate_IdMatch(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "letsencrypt-prod",
				"namespace": "cert-manager",
			},
			"data": map[string]any{"tls.key": "c2VjcmV0"},
		},
	}

	v1Api := types.NewApiVersion(types.Group(""), types.Version("v1"))
	e := mustBuild(entity.NewEntityBuilder().
		WithApiVersion(v1Api).
		WithKind(types.Kind("Secret")).
		WithResource(types.Resource("secrets")).
		WithNamespaced(true).
		WithNamespace(types.Namespace("cert-manager")).
		WithName(types.Name("letsencrypt-prod")).
		WithUnstructured(types.KeyClusterEntity, u))

	cs := &clusterSecrets{
		byId:       map[string]unstructured.Unstructured{"v1/Secret/cert-manager/letsencrypt-prod": u},
		entityList: []entity.Entity{e},
	}

	env, err := cel.NewEnv()
	require.NoError(t, err)

	programs, err := env.CompilePredicate(
		"clusterEntity != null",
		types.KubernetesGvkV1Secret.CelPredicate(),
		types.CelPredicate(`id == "v1/Secret/cert-manager/letsencrypt-prod"`),
	)
	require.NoError(t, err)

	matched, err := filterSecretsByPredicate(cs, programs)
	require.NoError(t, err)
	assert.Len(t, matched, 1)
}

func TestFilterSecretsByPredicate_AnnotationMatch(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "wildcard-tls",
				"namespace": "cert-manager",
				"annotations": map[string]any{
					"cert-manager.io/issuer-name": "letsencrypt-prod",
				},
			},
			"data": map[string]any{"tls.crt": "Y2VydA=="},
		},
	}

	v1Api := types.NewApiVersion(types.Group(""), types.Version("v1"))
	e := mustBuild(entity.NewEntityBuilder().
		WithApiVersion(v1Api).
		WithKind(types.Kind("Secret")).
		WithResource(types.Resource("secrets")).
		WithNamespaced(true).
		WithNamespace(types.Namespace("cert-manager")).
		WithName(types.Name("wildcard-tls")).
		WithUnstructured(types.KeyClusterEntity, u))

	cs := &clusterSecrets{
		byId:       map[string]unstructured.Unstructured{"v1/Secret/cert-manager/wildcard-tls": u},
		entityList: []entity.Entity{e},
	}

	env, err := cel.NewEnv()
	require.NoError(t, err)

	programs, err := env.CompilePredicate(
		"clusterEntity != null",
		types.KubernetesGvkV1Secret.CelPredicate(),
		types.CelPredicate(`clusterEntity.annotations().getOrEmpty("cert-manager.io/issuer-name") == "letsencrypt-prod"`),
	)
	require.NoError(t, err)

	matched, err := filterSecretsByPredicate(cs, programs)
	require.NoError(t, err)
	assert.Len(t, matched, 1)
}

func TestFilterSecretsByPredicate_NoMatch(t *testing.T) {
	u := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      "other-secret",
				"namespace": "default",
			},
			"data": map[string]any{"key": "dmFsdWU="},
		},
	}

	v1Api := types.NewApiVersion(types.Group(""), types.Version("v1"))
	e := mustBuild(entity.NewEntityBuilder().
		WithApiVersion(v1Api).
		WithKind(types.Kind("Secret")).
		WithResource(types.Resource("secrets")).
		WithNamespaced(true).
		WithNamespace(types.Namespace("default")).
		WithName(types.Name("other-secret")).
		WithUnstructured(types.KeyClusterEntity, u))

	cs := &clusterSecrets{
		byId:       map[string]unstructured.Unstructured{"v1/Secret/default/other-secret": u},
		entityList: []entity.Entity{e},
	}

	env, err := cel.NewEnv()
	require.NoError(t, err)

	programs, err := env.CompilePredicate(
		"clusterEntity != null",
		types.KubernetesGvkV1Secret.CelPredicate(),
		types.CelPredicate(`id == "v1/Secret/cert-manager/letsencrypt-prod"`),
	)
	require.NoError(t, err)

	matched, err := filterSecretsByPredicate(cs, programs)
	require.NoError(t, err)
	assert.Len(t, matched, 0)
}

func TestDeepCopyMap_DoesNotMutateOriginal(t *testing.T) {
	original := map[string]any{
		"data": map[string]any{
			"key": "value",
		},
	}

	copied := deepCopyMap(original)
	copied["data"].(map[string]any)["key"] = "modified"

	assert.Equal(t, "value", original["data"].(map[string]any)["key"])
}

func TestNewSecretEntity_RestoreEntityKeepsSecretGVK(t *testing.T) {
	secretObj := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      "wildcard-tls",
			"namespace": "cert-manager",
		},
		"data": map[string]any{
			"tls.crt": "Y2VydA==",
		},
	}

	secretEntity, err := newSecretEntity(secretObj, types.KeyTemplateEntity)
	require.NoError(t, err)
	entities, err := entity.NewEntities([]entity.Entity{secretEntity})
	require.NoError(t, err, "restore entities must keep enough GVK metadata for entity.NewEntities")
	require.Len(t, entities.Items, 1)

	id, err := entities.Items[0].Id()
	require.NoError(t, err)
	assert.Equal(t, types.Id("v1/Secret/cert-manager/wildcard-tls"), id)
}
