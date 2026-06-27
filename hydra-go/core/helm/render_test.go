package helm

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/chart/loader"
	v2chart "helm.sh/helm/v4/pkg/chart/v2"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestSplitCRDDocuments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple document without separator",
			input:    "apiVersion: v1\nkind: ConfigMap",
			expected: []string{"apiVersion: v1\nkind: ConfigMap"},
		},
		{
			name:     "document with leading separator",
			input:    "---\napiVersion: v1\nkind: ConfigMap",
			expected: []string{"apiVersion: v1\nkind: ConfigMap"},
		},
		{
			name:     "document with comment before separator",
			input:    "# some comment\n---\napiVersion: v1\nkind: ConfigMap",
			expected: []string{"# some comment", "apiVersion: v1\nkind: ConfigMap"},
		},
		{
			name:  "multiple documents",
			input: "---\napiVersion: v1\nkind: ConfigMap\n---\napiVersion: v1\nkind: Secret",
			expected: []string{
				"apiVersion: v1\nkind: ConfigMap",
				"apiVersion: v1\nkind: Secret",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitCRDDocuments([]byte(tt.input))
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d documents, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, doc := range result {
				if doc != tt.expected[i] {
					t.Errorf("doc %d: expected %q, got %q", i, tt.expected[i], doc)
				}
			}
		})
	}
}

func TestSplitManifestMap_AppendsTrailingNewline(t *testing.T) {
	manifest := types.YamlString("---\n# Source: t.yaml\nkind: Foo\nmetadata:\n  name: x")
	result := SplitManifestMap(manifest)
	docs := result["t.yaml"]
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if !strings.HasSuffix(string(docs[0]), "\n") {
		t.Fatalf("expected document to end with newline, got %q", docs[0])
	}
}

func TestSplitManifestMap_CRDWithLeadingSeparator(t *testing.T) {
	manifest := types.YamlString("---\n# Source: my-chart/templates/deployment.yaml\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: test\n---\n# Source: crds/crd-alertmanagers.yaml\napiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: alertmanagers.monitoring.coreos.com\n")

	result := SplitManifestMap(manifest)

	for path := range result {
		if strings.HasPrefix(path, "unknown") {
			t.Errorf("found unexpected unknown path: %q", path)
		}
	}

	if _, ok := result["my-chart/templates/deployment.yaml"]; !ok {
		t.Error("deployment template path not found")
	}
	if _, ok := result["crds/crd-alertmanagers.yaml"]; !ok {
		t.Errorf("CRD template path not found. Available paths: %v", mapKeys(result))
	}
}

func mapKeys(m map[string][]types.YamlString) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

func TestTemplateV2Chart_SkipCrdsOmitsPackagedCrds(t *testing.T) {
	chartPath := filepath.Join("testdata", "skip-crds-chart")
	chrt, err := loader.Load(chartPath)
	if err != nil {
		t.Fatal(err)
	}
	v2chrt, err := convertToV2Chart(chrt)
	if err != nil {
		t.Fatal(err)
	}
	params := func(skip bool) RenderChartParams {
		return RenderChartParams{
			KubernetesVersionOrFallback: "",
			ReleaseName:                 "rel",
			Namespace:                   "default",
			ValuesMap:                   types.ValuesMap{},
			SkipCrds:                    skip,
		}
	}
	outSkip, err := TemplateV2Chart(log.Default(), v2chrt, params(true))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(outSkip), "CustomResourceDefinition") {
		t.Fatalf("expected no CRD when SkipCrds=true, got:\n%s", outSkip)
	}
	outAll, err := TemplateV2Chart(log.Default(), v2chrt, params(false))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(outAll), "CustomResourceDefinition") {
		t.Fatalf("expected CRD when SkipCrds=false, got:\n%s", outAll)
	}
}

func TestApplyChartFileOperations_DeleteRegex(t *testing.T) {
	root := &v2chart.Chart{
		Metadata: &v2chart.Metadata{Name: "root"},
		Templates: []*common.File{
			{Name: "templates/keep.yaml", Data: []byte("keep: true\n")},
			{Name: "templates/delete-secret.yaml", Data: []byte("secret: true\n")},
		},
	}
	dep := &v2chart.Chart{
		Metadata: &v2chart.Metadata{Name: "sub"},
		Templates: []*common.File{
			{Name: "charts/sub/templates/delete-configmap.yaml", Data: []byte("config: true\n")},
			{Name: "charts/sub/templates/keep-job.yaml", Data: []byte("job: true\n")},
		},
	}
	root.AddDependency(dep)

	mutated, err := ApplyChartFileOperations(root, ChartFileOperations{
		Deletes: []string{`(^templates/delete-.*\.yaml$)|(^charts/sub/templates/delete-.*\.yaml$)`},
	})
	require.NoError(t, err)

	out, err := ChartSourceTemplatesMultidoc(mutated, nil)
	require.NoError(t, err)
	require.Contains(t, out, "templates/keep.yaml")
	require.Contains(t, out, "charts/sub/templates/keep-job.yaml")
	require.NotContains(t, out, "templates/delete-secret.yaml")
	require.NotContains(t, out, "charts/sub/templates/delete-configmap.yaml")
}
