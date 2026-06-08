package commands

import (
	"fmt"

	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func clusterRootOverridePredicate(
	env cel.Env,
	target types.AppId,
	index int,
	item types.ClusterRootAppOverrideItem,
) (cel.Predicate, error) {
	origin := fmt.Sprintf("cluster root hydra.yaml · overrides.%s[%d]", target, index)
	if item.Cel == "" {
		return env.CompileSelectedPredicateAt(origin, item.Selector)
	}
	return env.CompileSelectedPredicateAt(origin, item.Selector, item.Cel)
}

func applyClusterRootOverrides(
	cluster *hydra.Cluster,
	allAppIds sets.Set[types.AppId],
	renderedAllApps entity.Entities,
	clusterEntities entity.Entities,
	assignment map[types.Id]types.AppId,
	assignmentReasons map[types.Id]map[types.AppId][]AssignmentReason,
	metadata *ClusterEntityAssignmentMetadata,
	ambiguousIDs sets.Set[types.Id],
) error {
	resolved, err := hydra.ClusterRootAppOverrides(cluster)
	if err != nil || len(resolved) == 0 {
		return err
	}
	env, err := cel.NewEnvWithEntityInventory(renderedAllApps)
	if err != nil {
		return err
	}
	for target, items := range resolved {
		if !target.IsPresetApp() && (allAppIds == nil || !allAppIds.Has(target)) {
			continue
		}
		includes := make([]cel.Predicate, 0, len(items))
		excludes := make([]cel.Predicate, 0, len(items))
		for i, item := range items {
			prog, err := clusterRootOverridePredicate(env, target, i, item)
			if err != nil {
				return err
			}
			if item.Exclude {
				excludes = append(excludes, prog)
			} else {
				includes = append(includes, prog)
			}
		}
		if len(includes) == 0 {
			continue
		}
		for _, e := range clusterEntities.Items {
			id, err := e.Id()
			if err != nil {
				return err
			}
			matched := false
			for _, prog := range includes {
				ok, err := prog.EvalBool(e, types.MissingKeysAccept)
				if err != nil {
					return err
				}
				if ok {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			for _, prog := range excludes {
				ok, err := prog.EvalBool(e, types.MissingKeysAccept)
				if err != nil {
					return err
				}
				if ok {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
			assignment[id] = target
			assignmentReasons[id] = map[types.AppId][]AssignmentReason{
				target: {{
					Kind:           AssignmentReasonKindAssignedViaClusterRootOverride,
					OverrideTarget: string(target),
				}},
			}
			delete(metadata.AmbiguousAppIDsByClusterEntity, id)
			delete(metadata.AmbiguousAppReasonsByClusterEntity, id)
			metadata.UnassignedIDs.Delete(id)
			if ambiguousIDs != nil {
				ambiguousIDs.Delete(id)
			}
		}
	}
	return nil
}
