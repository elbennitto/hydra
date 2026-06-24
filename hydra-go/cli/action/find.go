package action

import (
	"encoding/json"
	"reflect"
	"slices"

	celref "github.com/google/cel-go/common/types/ref"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/yq"
	"k8s.io/apimachinery/pkg/util/sets"
)

type FindFlags struct {
	flags.HelmNetworkModeFlag
	flags.ContextFlag
	flags.ColorFlag
	flags.PredicatesFlag
	flags.ExcludeAppFlag
	flags.PickFlag
	flags.UniqFlag
	flags.NoCacheFlag
	AppIdPatterns []types.AppIdPattern
}

func (f *FindFlags) Flags() flags.Flags {
	return f
}

func Find(f FindFlags) (hydra.Hydra, string, error) {
	l := log.Default()
	if f.Pick == "" {
		f.Pick = "id"
	}

	config := flags.NewConfigFromFlags(&f, types.KubernetesConnectionAllowedNo)

	appIds, err := commands.ResolveAppIdsFromConfig(l, f.HydraContext, config, f.AppIdPatterns, f.ExcludeAppPatterns, f.HelmNetworkMode, false)
	if err != nil {
		return nil, "", err
	}
	if len(appIds) == 0 {
		return nil, "", log.CreateError(errors.ErrNoAppsSpecified, "no apps specified for find")
	}

	entities, err := renderFindEntities(l, f.HydraContext, config, f.HelmNetworkMode, appIds)
	if err != nil {
		return nil, "", err
	}

	env, err := cel.NewEnv()
	if err != nil {
		return nil, "", err
	}

	predicate, err := env.CompilePredicateAt(`hydra find --predicate`, f.Predicates...)
	if err != nil {
		return nil, "", err
	}

	_, matched, err := entities.Select(func(e entity.Entity) (bool, error) {
		return predicate.EvalBool(e, types.MissingKeysReject)
	})
	if err != nil {
		return nil, "", err
	}

	matched, err = matched.Sort(entity.NewIdFieldOrder(types.DirectionAscending))
	if err != nil {
		return nil, "", err
	}

	expression, err := env.CompileExpressionAt(`hydra find --pick`, f.Pick)
	if err != nil {
		return nil, "", err
	}

	projected := make([]any, 0, matched.Len())
	seen := map[string]struct{}{}
	for _, item := range matched.Items {
		value, err := expression.Eval(item)
		if err != nil {
			return nil, "", err
		}

		nativeValue, err := celValueToNative(value, f.Pick)
		if err != nil {
			return nil, "", err
		}

		if f.Uniq {
			key, err := projectedValueKey(nativeValue, f.Pick)
			if err != nil {
				return nil, "", err
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}

		projected = append(projected, nativeValue)
	}

	result, err := yq.ToYaml(f.Color, projected)
	if err != nil {
		return nil, "", err
	}

	return nil, result, nil
}

func renderFindEntities(
	l log.Logger,
	hydraContext types.HydraContext,
	config types.Config,
	networkMode types.HelmNetworkMode,
	appIds sets.Set[types.AppId],
) (entity.Entities, error) {
	clusterAppIds, clusterNames, err := groupAppIdsByCluster(appIds)
	if err != nil {
		return entity.Entities{}, err
	}

	var allItems []entity.Entity
	for _, clusterName := range clusterNames {
		cluster, err := commands.ResolveCommandCluster(commands.ResolveCommandClusterOptions{
			Config:       config,
			HydraContext: hydraContext,
			Limits:       hydra.RESTClientLimits{},
			ClusterName:  clusterName,
		})
		if err != nil {
			return entity.Entities{}, err
		}

		for _, appId := range sortedAppIds(clusterAppIds[clusterName]) {
			rendered, err := commands.RenderClusterSelectedApps(cluster, networkMode, "", sets.New(appId), types.KeyTemplateEntity)
			if err != nil {
				return entity.Entities{}, err
			}

			allItems = append(allItems, rendered.Items...)
		}
	}

	return mergeFindEntities(allItems)
}

func groupAppIdsByCluster(appIds sets.Set[types.AppId]) (map[types.ClusterName]sets.Set[types.AppId], []types.ClusterName, error) {
	grouped := map[types.ClusterName]sets.Set[types.AppId]{}

	for appId := range appIds {
		clusterName, err := appId.ClusterName()
		if err != nil {
			return nil, nil, err
		}

		if _, ok := grouped[clusterName]; !ok {
			grouped[clusterName] = sets.New[types.AppId]()
		}
		grouped[clusterName].Insert(appId)
	}

	clusterNames := make([]types.ClusterName, 0, len(grouped))
	for clusterName := range grouped {
		clusterNames = append(clusterNames, clusterName)
	}
	slices.Sort(clusterNames)

	return grouped, clusterNames, nil
}

func sortedAppIds(appIds sets.Set[types.AppId]) []types.AppId {
	result := make([]types.AppId, 0, len(appIds))
	for appId := range appIds {
		result = append(result, appId)
	}
	slices.Sort(result)
	return result
}

func mergeFindEntities(items []entity.Entity) (entity.Entities, error) {
	merged := map[types.Id]entity.Entity{}
	order := []types.Id{}

	for _, item := range items {
		id, err := item.Id()
		if err != nil {
			return entity.Entities{}, err
		}

		existing, ok := merged[id]
		if !ok {
			merged[id] = item
			order = append(order, id)
			continue
		}

		mergedItem, err := mergeFindEntity(existing, item)
		if err != nil {
			return entity.Entities{}, err
		}
		merged[id] = mergedItem
	}

	mergedItems := make([]entity.Entity, 0, len(order))
	for _, id := range order {
		mergedItems = append(mergedItems, merged[id])
	}

	return entity.NewEntities(mergedItems)
}

func mergeFindEntity(existing entity.Entity, additional entity.Entity) (entity.Entity, error) {
	existingAppIds, err := existing.AppIds()
	if err != nil {
		return entity.Entity{}, err
	}
	additionalAppIds, err := additional.AppIds()
	if err != nil {
		return entity.Entity{}, err
	}

	appIdSet := sets.New(existingAppIds...)
	appIdSet.Insert(additionalAppIds...)

	return existing.Modify(func(b entity.EntityBuilder) entity.EntityBuilder {
		return b.WithAppIds(sortedAppIds(appIdSet))
	})
}

func celValueToNative(value celref.Val, expression types.CelExpression) (any, error) {
	if value == nil {
		return nil, nil
	}

	if native, err := value.ConvertToNative(reflect.TypeOf(map[string]any{})); err == nil {
		return normalizeProjectedValue(native, expression)
	}
	if native, err := value.ConvertToNative(reflect.TypeOf([]any{})); err == nil {
		return normalizeProjectedValue(native, expression)
	}
	if native, err := value.ConvertToNative(reflect.TypeOf((*any)(nil)).Elem()); err == nil {
		return normalizeProjectedValue(native, expression)
	}

	return normalizeProjectedValue(value.Value(), expression)
}

func normalizeProjectedValue(value any, expression types.CelExpression) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case string, bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return v, nil
	case []any:
		result := make([]any, 0, len(v))
		for _, item := range v {
			normalized, err := normalizeProjectedValue(item, expression)
			if err != nil {
				return nil, err
			}
			result = append(result, normalized)
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			normalized, err := normalizeProjectedValue(item, expression)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case celref.Val:
		return celValueToNative(v, expression)
	}

	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil, nil
	}

	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return nil, nil
		}
		return normalizeProjectedValue(rv.Elem().Interface(), expression)
	case reflect.Slice, reflect.Array:
		result := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			normalized, err := normalizeProjectedValue(rv.Index(i).Interface(), expression)
			if err != nil {
				return nil, err
			}
			result = append(result, normalized)
		}
		return result, nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, log.CreateError(errors.ErrEvaluationFailed,
				"pick expression '{expression}' returned a map with unsupported key type {type}",
				log.String("expression", string(expression)),
				log.String("type", rv.Type().String()))
		}

		result := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			normalized, err := normalizeProjectedValue(iter.Value().Interface(), expression)
			if err != nil {
				return nil, err
			}
			result[iter.Key().String()] = normalized
		}
		return result, nil
	default:
		return nil, log.CreateError(errors.ErrEvaluationFailed,
			"pick expression '{expression}' returned unsupported type {type}",
			log.String("expression", string(expression)),
			log.String("type", rv.Type().String()))
	}
}

func projectedValueKey(value any, expression types.CelExpression) (string, error) {
	key, err := json.Marshal(value)
	if err != nil {
		return "", log.CreateError(errors.ErrEvaluationFailed,
			"failed to serialize projected value of '{expression}' for deduplication: {err}",
			log.String("expression", string(expression)),
			log.Err(err))
	}

	return string(key), nil
}
