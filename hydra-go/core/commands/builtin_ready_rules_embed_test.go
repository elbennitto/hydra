package commands

import (
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func TestEmbeddedBuiltinReadyRulesPresent(t *testing.T) {
	rules := defaultBuiltinReadyRules(nil)
	names := make([]string, len(rules))
	for i, r := range rules {
		names[i] = r.name
	}
	require.Equal(t, []string{
		"hydra-builtin-apps-v1-DaemonSet",
		"hydra-builtin-apps-v1-Deployment",
		"hydra-builtin-apps-v1-ReplicaSet",
		"hydra-builtin-apps-v1-StatefulSet",
		"hydra-builtin-batch-v1-CronJob",
		"hydra-builtin-batch-v1-Job",
		"hydra-builtin-v1-Pod",
		"hydra-builtin-v1-Secret-and-ConfigMap-cluster",
	}, names)
	deploy := rules[1]
	require.Equal(t, types.CelPredicate(`gvk == "apps/v1/Deployment"`), deploy.predicate)
	require.Len(t, deploy.cel, 7)
	require.Contains(t, string(deploy.cel[0]), "desired replica count is zero")
	require.Contains(t, string(deploy.cel[1]), "ReplicaFailure")
}
