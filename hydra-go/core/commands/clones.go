package commands

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	goocel "github.com/google/cel-go/cel"
	celTypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/cel"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

// BuildNamespaceOwnerMap resolves namespace -> owning app id from rendered entities.
// Priority order: (1) declaredOwners from global.hydra.ownerNamespaces, (2) v1/Namespace objects,
// (3) GroupNamespacesByApp fallback when exactly one app uses the namespace.
// Kubernetes system namespaces (kube-system, kube-public, kube-node-lease, and any name with prefix "kube-")
// are excluded from all strategies — they never appear in this map. Clone materialization uses
// cloneTargetOwner so a rule's DeclaringApp can still own clones into those namespaces.
// When namespaceOwnershipIndex is non-empty, strategy (3) uses GroupNamespacesByApp(namespaceOwnershipIndex)
// (typically a full-cluster render) so sole-app inference matches the whole cluster; v1/Namespace objects
// are still taken from entities. When namespaceOwnershipIndex is empty, strategy (3) uses entities.
func BuildNamespaceOwnerMap(entities entity.Entities, key types.EntityKeyUnstructured, declaredOwners map[types.Namespace]types.AppId, namespaceOwnershipIndex entity.Entities) (map[types.Namespace]types.AppId, error) {
	owner := map[types.Namespace]types.AppId{}

	for ns, appId := range declaredOwners {
		if isKubernetesSystemNamespace(ns) {
			continue
		}
		owner[ns] = appId
	}

	for _, e := range entities.Items {
		gvk, err := e.GVKString()
		if err != nil || gvk != types.KubernetesGvkV1Namespace {
			continue
		}
		name, err := e.Name()
		if err != nil || name == "" {
			continue
		}
		ns := types.Namespace(name)
		if isKubernetesSystemNamespace(ns) {
			continue
		}
		if _, done := owner[ns]; done {
			continue
		}
		appIds, err := e.AppIds()
		if err != nil || len(appIds) != 1 {
			return nil, log.CreateError(errors.ErrHydraConfigError,
				"v1/Namespace {ns}: expected exactly one owning app id, got {count}",
				log.String("ns", string(ns)), log.Int("count", len(appIds)))
		}
		if existing, ok := owner[ns]; ok && existing != appIds[0] {
			return nil, log.CreateError(errors.ErrHydraConfigError,
				"conflicting owners for namespace {ns}: {a} vs {b}",
				log.String("ns", string(ns)), log.String("a", string(existing)), log.String("b", string(appIds[0])))
		}
		owner[ns] = appIds[0]
	}

	byNsSource := namespaceOwnershipIndex
	if byNsSource.Len() == 0 {
		byNsSource = entities
	}
	byNs := GroupNamespacesByApp(byNsSource)
	type ambiguousNS struct {
		ns   types.Namespace
		apps []types.AppId
	}
	var ambiguous []ambiguousNS
	for ns, apps := range byNs {
		if isKubernetesSystemNamespace(ns) {
			continue
		}
		if _, done := owner[ns]; done {
			continue
		}
		list := apps.UnsortedList()
		slices.Sort(list)
		switch len(list) {
		case 0:
			continue
		case 1:
			owner[ns] = list[0]
		default:
			ambiguous = append(ambiguous, ambiguousNS{ns: ns, apps: list})
		}
	}
	if len(ambiguous) > 0 {
		slices.SortFunc(ambiguous, func(a, b ambiguousNS) int {
			return strings.Compare(string(a.ns), string(b.ns))
		})
		var parts []string
		var ambiguousHelp []map[string]any
		for _, a := range ambiguous {
			appStrs := make([]string, len(a.apps))
			for i, id := range a.apps {
				appStrs[i] = string(id)
			}
			parts = append(parts, string(a.ns)+": ["+strings.Join(appStrs, ", ")+"]")
			ambiguousHelp = append(ambiguousHelp, map[string]any{
				"namespace": string(a.ns),
				"apps":      appStrs,
			})
		}
		summary := strings.Join(parts, "; ")
		return nil, log.CreateError(errors.ErrCloneTargetOwnerAmbiguous,
			"ambiguous app owners for clone target resolution in {count} namespace(s): {summary} — to fix this, add each namespace to global.hydra.ownerNamespaces in the owning app's values so ownership is declared explicitly",
			log.Int("count", len(ambiguous)),
			log.String("summary", summary),
			log.String("ambiguous", summary),
			log.Any("namespaces", parts),
			log.ExtendedHelp("ambiguous", ambiguousHelp),
		)
	}
	return owner, nil
}

// cloneTargetOwner resolves which Hydra app owns materialized clone entities in ns.
// BuildNamespaceOwnerMap omits Kubernetes system namespaces; when a clone rule's
// targets include such a namespace, the rule's DeclaringApp supplies the owner for
// Helm app context (for example bootstrap mirrors into kube-system).
func cloneTargetOwner(ownerMap map[types.Namespace]types.AppId, ns types.Namespace, entry types.HydraCloneRuleEntry) (types.AppId, bool) {
	if o, ok := ownerMap[ns]; ok {
		return o, true
	}
	if isKubernetesSystemNamespace(ns) {
		da := strings.TrimSpace(string(entry.DeclaringApp))
		if da != "" {
			return types.AppId(da), true
		}
	}
	return "", false
}

// CloneTagActive applies global.hydra.clones tag semantics for the given bootstrap context.
func CloneTagActive(tag string, bootstrap types.Bootstrap) bool {
	switch strings.TrimSpace(tag) {
	case "":
		return true
	case "bootstrap":
		return bootstrap == types.BootstrapYes
	default:
		return true
	}
}

// BuildClonedResources evaluates clone rules and returns clone entities (stubsOnly uses minimal objects for uninstall).
// realIds: entity IDs that already exist — clones with the same ID are discarded (real > clone).
// Returns bootstrap-tagged resources actually materialized (count of clone entities whose rule had tag bootstrap).
func BuildClonedResources(
	l log.Logger,
	cluster *hydra.Cluster,
	networkMode types.HelmNetworkMode,
	entities entity.Entities,
	key types.EntityKeyUnstructured,
	rules []types.HydraCloneRuleEntry,
	bootstrap types.Bootstrap,
	stubsOnly bool,
	realIds sets.Set[types.Id],
	declaredOwners map[types.Namespace]types.AppId,
) (entity.Entities, int, error) {
	if len(rules) == 0 {
		return entity.Entities{}, 0, nil
	}
	env, err := buildCloneCelEnv(entities, key)
	if err != nil {
		return entity.Entities{}, 0, err
	}
	var namespaceOwnershipIndex entity.Entities
	if cluster != nil {
		skipRoot := types.SkipRootApps(cluster.ClusterName != types.InCluster)
		namespaceOwnershipIndex, _, err = RenderClusterAllApps(cluster, networkMode, "", key, skipRoot,
			WithSkipFoundDefinitionsInfoLog())
		if err != nil {
			return entity.Entities{}, 0, err
		}
	}
	ownerMap, err := BuildNamespaceOwnerMap(entities, key, declaredOwners, namespaceOwnershipIndex)
	if err != nil {
		return entity.Entities{}, 0, err
	}

	cloneSeen := map[types.Id]struct{}{}
	var built []entity.Entity
	bootstrapMaterialized := 0

	for _, entry := range rules {
		rule := entry.Rule
		if !CloneTagActive(rule.Tag, bootstrap) {
			continue
		}
		pred, err := env.CompilePredicateAt(fmt.Sprintf(`hydra clone rule %q · predicate`, entry.Name), types.CelPredicate(rule.Predicate))
		if err != nil {
			return entity.Entities{}, 0, fmt.Errorf("clone rule %q: predicate: %w", entry.Name, err)
		}

		targetProg, err := env.CompileExpressionAt(fmt.Sprintf(`hydra clone rule %q · targets.cel`, entry.Name), types.CelExpression(rule.Targets.CEL))
		if err != nil {
			return entity.Entities{}, 0, fmt.Errorf("clone rule %q: targets: %w", entry.Name, err)
		}

		for _, src := range entities.Items {
			ok, err := pred.EvalBool(src, types.MissingKeysReject)
			if err != nil {
				return entity.Entities{}, 0, fmt.Errorf("clone rule %q: %w", entry.Name, err)
			}
			if !ok {
				continue
			}
			kind, err := src.Kind()
			if err != nil {
				return entity.Entities{}, 0, err
			}
			if string(kind) == sopsSecretKind {
				id, _ := src.Id()
				return entity.Entities{}, 0, log.CreateError(errors.ErrHydraConfigError,
					"clone rule {rule} matched SopsSecret {id} — SopsSecrets cannot be cloned because changing metadata.namespace would invalidate the SOPS MAC. Use the derived v1/Secret (after ConvertSopsSecretsToSecrets) as clone source instead.",
					log.String("rule", entry.Name), log.String("id", string(id)))
			}

			targetNS, err := resolveTargetNamespaces(l, targetProg, rule, src)
			if err != nil {
				return entity.Entities{}, 0, fmt.Errorf("clone rule %q: %w", entry.Name, err)
			}
			for _, tn := range targetNS {
				ns := types.Namespace(tn)
				owner, ok := cloneTargetOwner(ownerMap, ns, entry)
				if !ok {
					return entity.Entities{}, 0, log.CreateError(errors.ErrHydraConfigError,
						"clone rule {rule}: no unique owner for target namespace {ns}",
						log.String("rule", entry.Name), log.String("ns", string(ns)))
				}
				cloneEnt, err := buildClonedEntity(
					cluster, networkMode, src, key, entry, ns, owner, stubsOnly,
				)
				if err != nil {
					return entity.Entities{}, 0, err
				}
				cid, err := cloneEnt.Id()
				if err != nil {
					return entity.Entities{}, 0, err
				}
				if realIds != nil && realIds.Has(cid) {
					continue
				}
				if _, dup := cloneSeen[cid]; dup {
					return entity.Entities{}, 0, log.CreateError(errors.ErrHydraConfigError,
						"clone rules would create duplicate entity {id}",
						log.String("id", string(cid)))
				}
				cloneSeen[cid] = struct{}{}
				built = append(built, cloneEnt)
				if strings.TrimSpace(rule.Tag) == "bootstrap" {
					bootstrapMaterialized++
				}
			}
		}
	}

	out, err := entity.NewEntities(built)
	if err != nil {
		return entity.Entities{}, 0, err
	}
	return out, bootstrapMaterialized, nil
}

func resolveTargetNamespaces(
	l log.Logger,
	targetProg cel.Expression,
	rule types.HydraCloneRule,
	src entity.Entity,
) ([]string, error) {
	v, err := targetProg.Eval(src)
	if err != nil {
		return nil, err
	}
	raw, err := refValToStringList(targetProg.Expression(), v)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw))
	for _, ns := range raw {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			continue
		}
		if namespaceExcluded(ns, rule.Exclude) {
			continue
		}
		out = append(out, ns)
	}
	if len(out) == 0 {
		l.DebugLog(logIdCommands, "clone targets resolved to no namespaces after filtering")
	}
	return out, nil
}

func namespaceExcluded(ns string, exclude []string) bool {
	for _, p := range exclude {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(ns, prefix) {
				return true
			}
			continue
		}
		if ns == p {
			return true
		}
	}
	return false
}

func refValToStringList(expr types.CelExpression, v ref.Val) ([]string, error) {
	if native, err := cel.RefValToNative(v, expr, reflect.TypeOf([]string{}), "[]string"); err == nil {
		return native.([]string), nil
	}
	lister, ok := v.(traits.Lister)
	if !ok {
		return nil, cel.NewExpressionResultTypeError(expr, "[]string", v)
	}
	var out []string
	it := lister.Iterator()
	for it.HasNext() == celTypes.True {
		item := it.Next()
		s, ok := item.(celTypes.String)
		if !ok {
			return nil, cel.NewExpressionResultTypeError(expr, "[]string", item)
		}
		out = append(out, string(s))
	}
	return out, nil
}

func buildClonedEntity(
	cluster *hydra.Cluster,
	networkMode types.HelmNetworkMode,
	src entity.Entity,
	key types.EntityKeyUnstructured,
	entry types.HydraCloneRuleEntry,
	targetNS types.Namespace,
	ownerApp types.AppId,
	stubsOnly bool,
) (entity.Entity, error) {
	u, ok := src.Unstructured(key)
	if !ok {
		return entity.Entity{}, fmt.Errorf("clone rule %q: source has no unstructured body", entry.Name)
	}
	gvkStr, err := src.GVKString()
	if err != nil {
		return entity.Entity{}, err
	}
	group, version, kind, err := types.GVKString(gvkStr).Components()
	if err != nil {
		return entity.Entity{}, err
	}

	var out *unstructured.Unstructured
	if stubsOnly {
		name, err := src.Name()
		if err != nil {
			return entity.Entity{}, err
		}
		out = &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": u.GetAPIVersion(),
			"kind":       u.GetKind(),
			"metadata": map[string]any{
				"name":      string(name),
				"namespace": string(targetNS),
			},
		}}
	} else {
		cp := u.DeepCopy()
		obj := cp.Object
		unstructured.SetNestedField(obj, string(targetNS), "metadata", "namespace")
		annKey := hydra.AnnotationHydraCloneSource
		ann := map[string]any{}
		if existing, ok, _ := unstructured.NestedMap(obj, "metadata", "annotations"); ok && existing != nil {
			for k, v := range existing {
				ann[k] = v
			}
		}
		srcTag := string(entry.DeclaringApp) + "/" + entry.Name
		if strings.TrimSpace(string(entry.DeclaringApp)) == "" {
			srcTag = entry.Name
		}
		ann[annKey] = srcTag
		if err := unstructured.SetNestedMap(obj, ann, "metadata", "annotations"); err != nil {
			return entity.Entity{}, err
		}
		out = cp
	}

	name := out.GetName()
	if name == "" {
		return entity.Entity{}, fmt.Errorf("clone rule %q: missing metadata.name", entry.Name)
	}

	h, err := cluster.WithApp(ownerApp)
	if err != nil {
		return entity.Entity{}, err
	}
	appNs, err := h.Namespace(networkMode)
	if err != nil {
		return entity.Entity{}, err
	}

	b := entity.NewEntityBuilder().
		WithGVK(types.NewGVK(group, version, kind)).
		WithName(types.Name(name)).
		WithNamespace(targetNS).
		WithUnstructured(key, *out).
		WithAppNamespace(types.AppNamespace(appNs)).
		WithAppIds([]types.AppId{ownerApp})

	return b.Build()
}

func buildCloneCelEnv(entities entity.Entities, key types.EntityKeyUnstructured) (cel.Env, error) {
	entities, err := entities.CopyItems(key, types.KeyEntity)
	if err != nil {
		return cel.Env{}, err
	}
	_, entities, err = entities.SelectByContainsEntityKey(key)
	if err != nil {
		return cel.Env{}, err
	}

	scopeInfoMap, err := entities.ScopeInfoMapFromCrds(key)
	if err != nil {
		return cel.Env{}, err
	}
	crdGVKs := make([]string, 0, len(scopeInfoMap))
	for gvk := range scopeInfoMap {
		crdGVKs = append(crdGVKs, string(gvk))
	}
	slices.Sort(crdGVKs)

	servicesByNamespace := make(map[string][]cel.ServiceInfo)
	for _, e := range entities.Items {
		gvkStr, err := e.GVKString()
		if err != nil {
			continue
		}
		if gvkStr != "v1/Service" {
			continue
		}
		ns, err := e.Namespace()
		if err != nil {
			continue
		}
		nm, err := e.Name()
		if err != nil {
			continue
		}
		u, ok := e.Unstructured(key)
		if !ok {
			continue
		}
		selector, _, _ := unstructured.NestedStringMap(u.Object, "spec", "selector")
		servicesByNamespace[string(ns)] = append(servicesByNamespace[string(ns)], cel.ServiceInfo{
			Id:       string(ns) + "/" + string(nm),
			Selector: selector,
		})
	}

	preOpts := []goocel.EnvOption{cel.ListSupport("CRDs", crdGVKs), cel.ServiceSupport(servicesByNamespace)}
	tmpEnv, err := cel.NewEnv(preOpts...)
	if err != nil {
		return cel.Env{}, err
	}
	invOpt, err := cel.ClusterInventorySupport(tmpEnv, entities, entity.Entities{}, entity.Entities{})
	if err != nil {
		return cel.Env{}, err
	}
	finalOpts := append([]goocel.EnvOption{invOpt}, preOpts...)
	return cel.NewEnv(finalOpts...)
}

// MaterializeHydraClonesForApply merges clone materialization into rendered entities (real > clone).
func MaterializeHydraClonesForApply(
	l log.Logger,
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	rendered entity.Entities,
	key types.EntityKeyUnstructured,
	bootstrap types.Bootstrap,
	networkMode types.HelmNetworkMode,
	extraCloneRules []types.HydraCloneRuleEntry,
) (entity.Entities, int, error) {
	rules, err := hydra.HydraAppCloneRules(cluster, appIds, networkMode, rendered)
	if err != nil {
		return entity.Entities{}, 0, err
	}
	rules = append(rules, extraCloneRules...)
	declaredOwners, err := hydra.HydraAppNamespaceOwners(cluster, appIds, networkMode)
	if err != nil {
		return entity.Entities{}, 0, err
	}
	realIds, err := CollectEntityIds(rendered)
	if err != nil {
		return entity.Entities{}, 0, err
	}
	clones, bootCount, err := BuildClonedResources(l, cluster, networkMode, rendered, key, rules, bootstrap, false, realIds, declaredOwners)
	if err != nil {
		return entity.Entities{}, 0, err
	}
	merged, err := MergeRenderedWithClones(rendered, clones)
	if err != nil {
		return entity.Entities{}, 0, err
	}
	return merged, bootCount, nil
}

// DiffEntities returns entities present in merged but not in base (by id).
func DiffEntities(base, merged entity.Entities) (entity.Entities, error) {
	baseIds, err := CollectEntityIds(base)
	if err != nil {
		return entity.Entities{}, err
	}
	var extras []entity.Entity
	for _, e := range merged.Items {
		id, err := e.Id()
		if err != nil {
			return entity.Entities{}, err
		}
		if !baseIds.Has(id) {
			extras = append(extras, e)
		}
	}
	return entity.NewEntities(extras)
}

// RekeyEntitiesUnstructured copies unstructured from fromKey to toKey for each entity.
func RekeyEntitiesUnstructured(
	entities entity.Entities,
	fromKey, toKey types.EntityKeyUnstructured,
) (entity.Entities, error) {
	var out []entity.Entity
	for _, e := range entities.Items {
		u, ok := e.Unstructured(fromKey)
		if !ok {
			return entity.Entities{}, fmt.Errorf("rekey: missing unstructured for key %s", fromKey)
		}
		ne, err := e.Modify(func(b entity.EntityBuilder) entity.EntityBuilder {
			b = b.WithoutUnstructured(fromKey)
			b = b.WithUnstructured(toKey, u)
			return b
		})
		if err != nil {
			return entity.Entities{}, err
		}
		out = append(out, ne)
	}
	return entity.NewEntities(out)
}

// MergeRenderedWithClones appends clone entities; IDs that already exist in rendered are skipped (real > clone).
func MergeRenderedWithClones(
	rendered entity.Entities,
	clones entity.Entities,
) (entity.Entities, error) {
	ids := sets.New[types.Id]()
	for _, e := range rendered.Items {
		id, err := e.Id()
		if err != nil {
			return entity.Entities{}, err
		}
		ids.Insert(id)
	}
	var extra []entity.Entity
	for _, e := range clones.Items {
		id, err := e.Id()
		if err != nil {
			return entity.Entities{}, err
		}
		if ids.Has(id) {
			continue
		}
		extra = append(extra, e)
	}
	if len(extra) == 0 {
		return rendered, nil
	}
	return rendered.Append(entity.Entities{Items: extra})
}

// CollectEntityIds returns the set of entity ids.
func CollectEntityIds(ents entity.Entities) (sets.Set[types.Id], error) {
	out := sets.New[types.Id]()
	for _, e := range ents.Items {
		id, err := e.Id()
		if err != nil {
			return nil, err
		}
		out.Insert(id)
	}
	return out, nil
}

// ValidateBootstrapTemplateClones returns an error when --bootstrap was unnecessary for local template.
func ValidateBootstrapTemplateClones(
	bootstrap types.Bootstrap,
	rules []types.HydraCloneRuleEntry,
	bootstrapMaterialized int,
) error {
	if bootstrap != types.BootstrapYes {
		return nil
	}
	hasBootstrapRules := false
	for _, e := range rules {
		if strings.TrimSpace(e.Rule.Tag) == "bootstrap" {
			hasBootstrapRules = true
			break
		}
	}
	if !hasBootstrapRules {
		return log.CreateError(errors.ErrHydraConfigError,
			"--bootstrap was specified but no clone rules with tag \"bootstrap\" are defined under global.hydra.clones")
	}
	if bootstrapMaterialized == 0 {
		return log.CreateError(errors.ErrHydraConfigError,
			"--bootstrap was specified but no resources were materialized from clone rules tagged \"bootstrap\" (predicate matched nothing or all targets were excluded)")
	}
	return nil
}

// ExpandClonesForUninstall adds minimal clone target entities for uninstall / orphan handling (no decryption).
func ExpandClonesForUninstall(
	l log.Logger,
	cluster *hydra.Cluster,
	appIds sets.Set[types.AppId],
	networkMode types.HelmNetworkMode,
	entities entity.Entities,
	key types.EntityKeyUnstructured,
	bootstrap types.Bootstrap,
) (entity.Entities, error) {
	rules, err := hydra.HydraAppCloneRules(cluster, appIds, networkMode, entities)
	if err != nil {
		return entities, err
	}
	declaredOwners, err := hydra.HydraAppNamespaceOwners(cluster, appIds, networkMode)
	if err != nil {
		return entities, err
	}
	realIds, err := CollectEntityIds(entities)
	if err != nil {
		return entities, err
	}
	additional, _, err := BuildClonedResources(l, cluster, networkMode, entities, key, rules, bootstrap, true, realIds, declaredOwners)
	if err != nil {
		return entities, err
	}
	if additional.Len() == 0 {
		return entities, nil
	}
	l.Info(logIdCommands, "uninstall: expanded {count} clone stub entities",
		log.Int("count", additional.Len()))
	return entities.Append(additional)
}
