package entity

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/yaml"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// helmLikeConfigMapYAML builds a manifest similar to `helm template` output: a # Source line
// plus a ConfigMap whose data key uses a literal block where the first line after `|` is empty,
// which encodes a leading newline in the string value (same effect as Sprig nindent).
// The real chart render is asserted in helm.TestTemplateV2Chart_ServiceMobileRolloutAssistant_nginxConfLeadingNewline.
func helmLikeConfigMapYAML() string {
	var b strings.Builder
	b.WriteString("# Source: templates/nginx-configmap.yaml\n")
	b.WriteString("apiVersion: v1\n")
	b.WriteString("kind: ConfigMap\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: nginx-config\n")
	b.WriteString("  namespace: demo\n")
	b.WriteString("data:\n")
	b.WriteString("  nginx.conf: |\n")
	b.WriteString("\n") // empty first line of block scalar => leading \n in nginx.conf value
	b.WriteString("    events {\n")
	b.WriteString("      worker_connections: 1024\n")
	b.WriteString("    }\n")
	return b.String()
}

func TestNewEntitiesFromYaml_preservesLeadingNewlineInConfigMapData(t *testing.T) {
	t.Parallel()

	manifest := types.YamlString(helmLikeConfigMapYAML())
	entities, err := NewEntitiesFromYaml(log.Default(), manifest, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.NotEmpty(t, entities.Items, "expected at least one entity from manifest")

	u, err := entities.Items[0].UnstructuredOrError(types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Equal(t, "ConfigMap", u.GetKind())

	data, found, err := unstructured.NestedString(u.Object, "data", "nginx.conf")
	require.NoError(t, err)
	require.True(t, found, "expected data.nginx.conf")

	require.True(t, strings.HasPrefix(data, "\n"),
		"nginx.conf must keep the leading newline from the YAML block scalar; got prefix %q", data[:min(20, len(data))])
	require.Contains(t, data, "events {")
}

func TestYamlToUnstructured_preservesLeadingNewlineInConfigMapData(t *testing.T) {
	t.Parallel()

	// Same YAML as produced after SplitManifestMap (single document, no # Source line needed here).
	doc := strings.TrimPrefix(helmLikeConfigMapYAML(), "# Source: templates/nginx-configmap.yaml\n")
	u, err := yaml.YamlToUnstructured(types.YamlString(doc))
	require.NoError(t, err)

	data, found, err := unstructured.NestedString(u.Object, "data", "nginx.conf")
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, strings.HasPrefix(data, "\n"),
		"YamlToUnstructured must preserve leading newline; got prefix %q", data[:min(20, len(data))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
