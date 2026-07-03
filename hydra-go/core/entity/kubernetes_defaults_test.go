package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestIsKubernetesDefaultsStaticFilename(t *testing.T) {
	assert.True(t, IsKubernetesDefaultsStaticFilename("kubernetes-defaults-demo.yaml"))
	assert.False(t, IsKubernetesDefaultsStaticFilename("kubernetes-defaults-demo.yml"))
	assert.False(t, IsKubernetesDefaultsStaticFilename("other.yaml"))
}

func TestFilterKubernetesDefaultsBody_DropsDuplicateIds(t *testing.T) {
	defaultsPart := []byte(`apiVersion: v1
kind: ServiceAccount
metadata:
  namespace: demo
  name: default
---
apiVersion: v1
kind: Namespace
metadata:
  name: other-only
`)
	existingIds := sets.New[types.Id](
		"v1/ServiceAccount/demo/default",
		"v1/Namespace//demo",
	)

	out, err := FilterKubernetesDefaultsBody(log.Default(), "kubernetes-defaults-demo.yaml", defaultsPart, existingIds, types.KeyTemplateEntity)
	require.NoError(t, err)
	require.Contains(t, string(out), "other-only")
	require.NotContains(t, string(out), "name: default")
}

func TestFilterKubernetesDefaultsBody_NoExistingIdsPassesThrough(t *testing.T) {
	raw := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: x
`)
	out, err := FilterKubernetesDefaultsBody(log.Default(), "kubernetes-defaults-demo.yaml", raw, sets.New[types.Id](), types.KeyTemplateEntity)
	require.NoError(t, err)
	assert.Equal(t, string(raw), string(out))
}
