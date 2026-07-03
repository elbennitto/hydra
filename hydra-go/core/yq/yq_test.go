package yq

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	hyaml "hydra-gitops.org/hydra/hydra-go/core/yaml"
	kyaml "sigs.k8s.io/yaml"
)

func TestYqPatchArgo_UsesMetadataNamespaceWhenSet(t *testing.T) {
	l := log.NewLoggerWithHandler(slog.NewTextHandler(io.Discard, nil))
	in := types.YamlString(`apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: webhook-authentication-reader
  namespace: kube-system
`)
	out, err := YqPatchArgo(l, in, "in-cluster.cluster-infra.cert-manager-webhook-hetzner", "cert-manager")
	require.NoError(t, err)
	require.Contains(t, string(out), "argocd.argoproj.io/tracking-id: in-cluster.cluster-infra.cert-manager-webhook-hetzner:rbac.authorization.k8s.io/RoleBinding:kube-system/webhook-authentication-reader")
}

func TestYqPatchArgo_FallsBackToAppNamespaceForClusterScoped(t *testing.T) {
	l := log.NewLoggerWithHandler(slog.NewTextHandler(io.Discard, nil))
	in := types.YamlString(`apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: example-cr
`)
	out, err := YqPatchArgo(l, in, "some.app", "app-ns")
	require.NoError(t, err)
	require.Contains(t, string(out), "argocd.argoproj.io/tracking-id: some.app:rbac.authorization.k8s.io/ClusterRole:app-ns/example-cr")
}

func TestYqPatchArgo_UsesMetadataNamespaceForRole(t *testing.T) {
	l := log.NewLoggerWithHandler(slog.NewTextHandler(io.Discard, nil))
	in := types.YamlString(`apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kyverno-read-secret
  namespace: sops-secrets-operator
`)
	out, err := YqPatchArgo(l, in, "in-cluster.cluster-infra.kyverno", "kyverno")
	require.NoError(t, err)
	require.Contains(t, string(out), "argocd.argoproj.io/tracking-id: in-cluster.cluster-infra.kyverno:rbac.authorization.k8s.io/Role:sops-secrets-operator/kyverno-read-secret")
}

func TestYqPatchArgo_RemovesTrackingIdWhenNone(t *testing.T) {
	l := log.NewLoggerWithHandler(slog.NewTextHandler(io.Discard, nil))
	in := types.YamlString(`apiVersion: v1
kind: ConfigMap
metadata:
  name: x
  namespace: ns
  annotations:
    argocd.argoproj.io/tracking-id: none
`)
	out, err := YqPatchArgo(l, in, "app", "default")
	require.NoError(t, err)
	require.False(t, strings.Contains(string(out), "argocd.argoproj.io/tracking-id"))
}

func TestYqPatchArgo_PreservesConfigMapDataLeadingNewline(t *testing.T) {
	l := log.NewLoggerWithHandler(slog.NewTextHandler(io.Discard, nil))
	// Literal block: empty first line after | encodes a leading newline before "hello".
	in := types.YamlString(`apiVersion: v1
kind: ConfigMap
metadata:
  name: minimal-newline-cm
  namespace: argocd
data:
  app.txt: |

    hello
`)
	out, err := YqPatchArgo(l, in, "in-cluster.test", "argocd")
	require.NoError(t, err)
	require.Contains(t, string(out), "argocd.argoproj.io/tracking-id: in-cluster.test:/ConfigMap:argocd/minimal-newline-cm")
	var doc map[string]interface{}
	require.NoError(t, kyaml.Unmarshal([]byte(out), &doc))
	data := doc["data"].(map[string]interface{})
	s := data["app.txt"].(string)
	require.True(t, strings.HasPrefix(s, "\n"), "data.app.txt must keep leading newline after YqPatchArgo; got %q", s)
	require.Contains(t, s, "hello")
}

// Helm / some encoders emit root keys in sorted order (data before metadata). Inserting
// annotations before the first root "data:" line produced invalid YAML (yaml: did not find expected key).
func TestYqPatchArgo_PassthroughYamlWithoutApiVersionOrKind(t *testing.T) {
	l := log.NewLoggerWithHandler(slog.NewTextHandler(io.Discard, nil))
	// Not a Kubernetes object; must not fail on PrintObject.
	in := types.YamlString(`foo: bar
`)
	out, err := YqPatchArgo(l, in, "some.app", "ns")
	require.NoError(t, err)
	require.Contains(t, string(out), "foo: bar")
}

func TestYqPatchArgo_DataBeforeMetadataParsesAsEntities(t *testing.T) {
	l := log.NewLoggerWithHandler(slog.NewTextHandler(io.Discard, nil))
	in := types.YamlString(`apiVersion: v1
data:
  k: v
kind: ConfigMap
metadata:
  name: reorder-cm
  namespace: argocd
`)
	out, err := YqPatchArgo(l, in, "in-cluster.test", "argocd")
	require.NoError(t, err)
	require.Contains(t, string(out), "argocd.argoproj.io/tracking-id")
	_, err = hyaml.YamlToUnstructured(out)
	require.NoError(t, err, "patched manifest must parse; got:\n%s", string(out))
}
