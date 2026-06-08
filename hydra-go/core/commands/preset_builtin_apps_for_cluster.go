package commands

import (
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

// MergeBuiltinPresetAppsForCluster computes synthetic builtin-app template entities for every
// preset that the cluster's merged Helm/ConfigMap configuration enables (mutual exclusions and
// transitive activations honored), and folds them into the rendered template universe. After the
// merge, builtin apps are first-class apps for every downstream consumer (templateIDToApp,
// AssignClusterEntitiesToAtMostOneAppByRefs, namespace grouping, leftover classification).
//
// CEL-only preset predicates do not produce template entities; only explicit anchor IDs are
// materialized. Real apps win over builtin apps on overlapping IDs (template primacy).
//
// k8sMinor is used to gate per-predicate Kubernetes minor windows; 99 disables gating. networkMode
// controls Helm value rendering when computing the merged preset section.
func MergeBuiltinPresetAppsForCluster(
	cluster *hydra.Cluster,
	allAppIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	perAppRendered map[types.AppId]entity.Entities,
	renderedAllApps entity.Entities,
	k8sMinor int,
) (
	mergedPerApp map[types.AppId]entity.Entities,
	mergedAllApps entity.Entities,
	mergedAppIds sets.Set[types.AppId],
	err error,
) {
	effective, err := hydra.EffectiveClusterDefaultsPresetsForCluster(cluster, allAppIds, networkMode, renderedAllApps, k8sMinor)
	if err != nil {
		return nil, entity.Entities{}, nil, err
	}
	clusterName := types.InCluster
	if cluster != nil {
		clusterName = cluster.ClusterName
	}
	presetEntities, err := PresetTemplateEntities(clusterName, effective, k8sMinor)
	if err != nil {
		return nil, entity.Entities{}, nil, err
	}
	return MergeBuiltinPresetAppsIntoRendered(perAppRendered, renderedAllApps, allAppIds, presetEntities)
}
