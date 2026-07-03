package flags

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestNewConfigFromFlags_defaultHelmTemplateCacheEnabledWhenEnvUnset(t *testing.T) {
	t.Setenv(types.HydraNoCacheEnvName, "")
	c := NewConfigFromFlags(&NoCacheFlag{}, types.KubernetesConnectionAllowedNo)
	assert.True(t, c.HelmTemplateCacheEnabled())
}

func TestNewConfigFromFlags_noCacheDisablesHelmTemplateCache(t *testing.T) {
	t.Setenv(types.HydraNoCacheEnvName, "")
	c := NewConfigFromFlags(&NoCacheFlag{NoCache: true}, types.KubernetesConnectionAllowedNo)
	assert.False(t, c.HelmTemplateCacheEnabled())
}

func TestNewConfigFromFlags_envNoCacheDisablesHelmTemplateCache(t *testing.T) {
	t.Setenv(types.HydraNoCacheEnvName, "1")
	c := NewConfigFromFlags(&NoCacheFlag{}, types.KubernetesConnectionAllowedNo)
	assert.False(t, c.HelmTemplateCacheEnabled())
}
