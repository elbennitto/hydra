package commands

import (
	"cmp"
	"slices"

	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

type AssignmentReasonKind string

const (
	AssignmentReasonKindMatchedByPresetID              AssignmentReasonKind = "matched-by-preset-id"
	AssignmentReasonKindMatchedByPresetCEL             AssignmentReasonKind = "matched-by-preset-cel"
	AssignmentReasonKindAssignedPreset                 AssignmentReasonKind = "assigned-preset"
	AssignmentReasonKindAssignedViaBuiltinRef          AssignmentReasonKind = "assigned-via-builtin-ref"
	AssignmentReasonKindAssignedViaAppRef              AssignmentReasonKind = "assigned-via-app-ref"
	AssignmentReasonKindAssignedViaTemplateID          AssignmentReasonKind = "assigned-via-template-id"
	AssignmentReasonKindAssignedViaPresetTemplate      AssignmentReasonKind = "assigned-via-preset-template"
	AssignmentReasonKindAssignedViaPresetMatch         AssignmentReasonKind = "assigned-via-preset-match"
	AssignmentReasonKindAssignedViaClusterRootOverride AssignmentReasonKind = "assigned-via-cluster-root-override"
	AssignmentReasonKindAssignedViaOwnerRef            AssignmentReasonKind = "assigned-via-owner-ref"
	AssignmentReasonKindAssignedViaRefOwnership        AssignmentReasonKind = "assigned-via-ref-ownership"
	AssignmentReasonKindAssignedViaInspectRef          AssignmentReasonKind = "assigned-via-inspect-ref"
	AssignmentReasonKindAmbiguousAppAssignment         AssignmentReasonKind = "ambiguous-app-assignment"
	AssignmentReasonKindNoAppAssignment                AssignmentReasonKind = "no-app-assignment"
)

type AssignmentReason struct {
	Kind           AssignmentReasonKind
	PresetIDs      []string
	PresetRules    []string
	Preset         string
	OverrideTarget string
	OwnerRefs      []types.Id
	RefOwnership   *types.RefOwnershipPredicateLine
	EventRef       string
	EventSubjects  []types.Id
}

// ClusterEntityAssignmentMetadata carries secondary facts from
// AssignClusterEntitiesToAtMostOneAppByRefs.
//
// AmbiguousAppIDsByClusterEntity is always populated for detected ownership conflicts. Detailed
// AmbiguousAppReasonsByClusterEntity entries are populated only for IDs explicitly requested via
// AssignClusterEntitiesToAtMostOneAppByRefsInput.ConflictDetailIDs.
type ClusterEntityAssignmentMetadata struct {
	DirectRefMatchedBuiltinIDs         sets.Set[types.Id]
	DirectRefMatchedAppIDs             sets.Set[types.Id]
	AmbiguousAppIDsByClusterEntity     map[types.Id][]types.AppId
	AmbiguousAppReasonsByClusterEntity map[types.Id]map[types.AppId][]AssignmentReason
	UnassignedIDs                      sets.Set[types.Id]
}

// AmbiguousEntityIDs returns the sorted list of cluster entity IDs whose ownership matched more
// than one app during assignment. This is the cheap conflict summary produced by the internal fast
// path. The exported AssignClusterEntitiesToAtMostOneAppByRefs wrapper already feeds these IDs back
// into ConflictDetailIDs automatically when detailed per-app conflict reasons are needed. Internal
// callers using the unexported fast path may still use this list to request details explicitly.
func (m ClusterEntityAssignmentMetadata) AmbiguousEntityIDs() []types.Id {
	ids := make([]types.Id, 0, len(m.AmbiguousAppIDsByClusterEntity))
	for id := range m.AmbiguousAppIDsByClusterEntity {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b types.Id) int { return cmp.Compare(string(a), string(b)) })
	return ids
}

func cloneClusterEntityAssignmentMetadata(src ClusterEntityAssignmentMetadata) ClusterEntityAssignmentMetadata {
	out := ClusterEntityAssignmentMetadata{
		DirectRefMatchedBuiltinIDs:         sets.New[types.Id](),
		DirectRefMatchedAppIDs:             sets.New[types.Id](),
		AmbiguousAppIDsByClusterEntity:     map[types.Id][]types.AppId{},
		AmbiguousAppReasonsByClusterEntity: map[types.Id]map[types.AppId][]AssignmentReason{},
		UnassignedIDs:                      sets.New[types.Id](),
	}
	for id := range src.DirectRefMatchedBuiltinIDs {
		out.DirectRefMatchedBuiltinIDs.Insert(id)
	}
	for id := range src.DirectRefMatchedAppIDs {
		out.DirectRefMatchedAppIDs.Insert(id)
	}
	for id, apps := range src.AmbiguousAppIDsByClusterEntity {
		out.AmbiguousAppIDsByClusterEntity[id] = append([]types.AppId{}, apps...)
	}
	for id, byApp := range src.AmbiguousAppReasonsByClusterEntity {
		copiedByApp := make(map[types.AppId][]AssignmentReason, len(byApp))
		for app, reasons := range byApp {
			copiedReasons := append([]AssignmentReason{}, reasons...)
			for i := range copiedReasons {
				copiedReasons[i].PresetIDs = append([]string{}, copiedReasons[i].PresetIDs...)
				copiedReasons[i].PresetRules = append([]string{}, copiedReasons[i].PresetRules...)
				copiedReasons[i].OwnerRefs = append([]types.Id{}, copiedReasons[i].OwnerRefs...)
				copiedReasons[i].EventSubjects = append([]types.Id{}, copiedReasons[i].EventSubjects...)
			}
			copiedByApp[app] = copiedReasons
		}
		out.AmbiguousAppReasonsByClusterEntity[id] = copiedByApp
	}
	for id := range src.UnassignedIDs {
		out.UnassignedIDs.Insert(id)
	}
	return out
}
