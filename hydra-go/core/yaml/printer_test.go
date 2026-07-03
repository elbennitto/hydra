package yaml

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kyaml "sigs.k8s.io/yaml"
)

func TestPrintObject_UnstructuredWithGoIntDoesNotPanic(t *testing.T) {
	t.Parallel()

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "x",
				"namespace": "ns",
			},
			"data": map[string]interface{}{
				// Go int is not accepted by k8s runtime.DeepCopyJSONValue; cluster diff used to panic here.
				"n": int(42),
			},
		},
	}

	out, err := PrintObject(types.KeepServerFieldsNo, nil, u)
	if err != nil {
		t.Fatalf("PrintObject: %v", err)
	}
	if !strings.Contains(string(out), "42") {
		t.Fatalf("expected YAML to contain normalized int value, got:\n%s", out)
	}
}

func TestNormalizeJSONValueForDeepCopy_IntToInt64(t *testing.T) {
	t.Parallel()

	v := normalizeJSONValueForDeepCopy(map[string]interface{}{
		"a": int(7),
		"b": int32(8),
		"c": uint32(9),
	})
	m := v.(map[string]interface{})
	if m["a"].(int64) != 7 || m["b"].(int64) != 8 || m["c"].(int64) != 9 {
		t.Fatalf("unexpected normalized values: %#v", m)
	}
}

func TestNormalizeUnstructuredObjectForDeepCopy_Exported(t *testing.T) {
	t.Parallel()

	out := NormalizeUnstructuredObjectForDeepCopy(map[string]any{
		"spec": map[string]any{"replicas": int(3)},
	})
	if out["spec"].(map[string]any)["replicas"].(int64) != 3 {
		t.Fatalf("expected int64 replicas, got %#v", out["spec"])
	}
}

// TestPrintObject_ConfigMapDataPreservesLeadingNewline documents Helm-style ConfigMap values where
// data.* begins with a newline (e.g. Sprig nindent). kubectl / helm template preserve this; Hydra
// local template must not drop it when re-serializing through PrintObject.
func TestPrintObject_ConfigMapDataPreservesLeadingNewline(t *testing.T) {
	t.Parallel()

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "minimal-newline-cm",
				"namespace": "argocd",
			},
			"data": map[string]interface{}{
				"app.txt": "\nhello\n",
			},
		},
	}

	out, err := PrintObject(types.KeepServerFieldsNo, nil, u)
	require.NoError(t, err)

	// Parse back: leading newline must survive PrintObject (same semantics as helm template).
	var round map[string]interface{}
	require.NoError(t, kyaml.Unmarshal([]byte(out), &round))
	data := round["data"].(map[string]interface{})
	got := data["app.txt"].(string)
	require.True(t, strings.HasPrefix(got, "\n"),
		"PrintObject must preserve leading newline in ConfigMap data; got prefix %q", got[:min(12, len(got))])
	require.Contains(t, got, "hello")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
