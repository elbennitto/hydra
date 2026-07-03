package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
)

func TestPreferredVersionMap_SingleEntry(t *testing.T) {
	scopeInfoMap := ScopeInfoMap{
		"kafka.strimzi.io/v1/Kafka": ScopeInfo{},
	}

	result, err := PreferredVersionMap(scopeInfoMap)
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Equal(t, Version("v1"), result[GroupKindKey("kafka.strimzi.io/Kafka")])
}

func TestPreferredVersionMap_MultipleEntries(t *testing.T) {
	scopeInfoMap := ScopeInfoMap{
		"kafka.strimzi.io/v1/Kafka":      ScopeInfo{},
		"kafka.strimzi.io/v1/KafkaTopic": ScopeInfo{},
	}

	result, err := PreferredVersionMap(scopeInfoMap)
	require.NoError(t, err)

	assert.Len(t, result, 2)
	assert.Equal(t, Version("v1"), result[GroupKindKey("kafka.strimzi.io/Kafka")])
	assert.Equal(t, Version("v1"), result[GroupKindKey("kafka.strimzi.io/KafkaTopic")])
}

func TestPreferredVersionMap_EmptyGroup(t *testing.T) {
	scopeInfoMap := ScopeInfoMap{
		"v1/ConfigMap": ScopeInfo{},
	}

	result, err := PreferredVersionMap(scopeInfoMap)
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Equal(t, Version("v1"), result[GroupKindKey("ConfigMap")])
}

func TestPreferredVersionMap_EmptyMap(t *testing.T) {
	scopeInfoMap := ScopeInfoMap{}

	result, err := PreferredVersionMap(scopeInfoMap)
	require.NoError(t, err)

	assert.Empty(t, result)
}

func TestPreferredVersionMap_EmptyGroupKey(t *testing.T) {
	scopeInfoMap := ScopeInfoMap{
		"v1/Pod": ScopeInfo{},
	}

	result, err := PreferredVersionMap(scopeInfoMap)
	require.NoError(t, err)

	_, hasSlashPrefix := result[GroupKindKey("/Pod")]
	assert.False(t, hasSlashPrefix, "GroupKindKey for core resources must not have a leading slash")

	assert.Equal(t, Version("v1"), result[GroupKindKey("Pod")])
}

func TestPreferredVersionMap_ConflictingVersionsSameGroupKind_ReturnsError(t *testing.T) {
	m := ScopeInfoMap{
		"kafka.strimzi.io/v1beta2/KafkaTopic": ScopeInfo{},
		"kafka.strimzi.io/v1/KafkaTopic":      ScopeInfo{},
	}
	_, err := PreferredVersionMap(m)
	require.Error(t, err)
	assert.True(t, errors.ErrConflictingPreferredApiVersions.MatchesError(err))
}

func TestNewGroupKindKey_WithGroup(t *testing.T) {
	key := NewGroupKindKey(Group("kafka.strimzi.io"), Kind("Kafka"))

	assert.Equal(t, GroupKindKey("kafka.strimzi.io/Kafka"), key)
}

func TestNewGroupKindKey_EmptyGroup(t *testing.T) {
	key := NewGroupKindKey(Group(""), Kind("ConfigMap"))

	assert.Equal(t, GroupKindKey("ConfigMap"), key)
}
