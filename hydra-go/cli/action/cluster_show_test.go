package action

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/core/commands"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestBuildClusterShowReport_GroupsAppsAndKeepsAmbiguousAndUnassigned(t *testing.T) {
	t.Parallel()

	assignment := map[types.Id]types.AppId{
		types.Id("v1/Secret/cert-manager/webhook"): types.AppId("in-cluster.cluster-infra.cert-manager"),
	}
	annotations := map[types.Id][]clusterShowAnnotation{
		types.Id("v1/Secret/cert-manager/webhook"): {
			{Reason: commands.AssignmentReason{Kind: commands.AssignmentReasonKindAssignedViaTemplateID}},
		},
		types.Id("metrics.k8s.io/v1beta1/PodMetrics/cert-manager/pod-a"): {
			{Reason: commands.AssignmentReason{Kind: commands.AssignmentReasonKindAmbiguousAppAssignment}},
		},
		types.Id("v1/ConfigMap/default/orphan"): {
			{Reason: commands.AssignmentReason{Kind: commands.AssignmentReasonKindNoAppAssignment}},
		},
	}
	metadata := commands.ClusterEntityAssignmentMetadata{
		DirectRefMatchedBuiltinIDs: sets.New[types.Id](),
		DirectRefMatchedAppIDs:     sets.New[types.Id](),
		AmbiguousAppIDsByClusterEntity: map[types.Id][]types.AppId{
			types.Id("metrics.k8s.io/v1beta1/PodMetrics/cert-manager/pod-a"): {
				types.AppId("in-cluster.cluster-infra.cert-manager-webhook-hetzner"),
				types.AppId("in-cluster.cluster-infra.cert-manager"),
			},
		},
		AmbiguousAppReasonsByClusterEntity: map[types.Id]map[types.AppId][]commands.AssignmentReason{
			types.Id("metrics.k8s.io/v1beta1/PodMetrics/cert-manager/pod-a"): {
				types.AppId("in-cluster.cluster-infra.cert-manager"): {{
					Kind: commands.AssignmentReasonKindAssignedViaRefOwnership,
					RefOwnership: &types.RefOwnershipPredicateLine{
						Cel: `gvk == "metrics.k8s.io/v1beta1/PodMetrics" && ns == "cert-manager"`,
						Source: &types.RefOwnershipRuleSource{
							Kind:      types.RefOwnershipRuleSourceKindHydraRefParser,
							BlockPath: "global.hydra.refs.cert-manager.ref-parsers[0]",
							Sources:   []string{"charts-repository/apps/demo-infra/cert-manager/dev/values.yaml"},
						},
					},
				}},
				types.AppId("in-cluster.cluster-infra.cert-manager-webhook-hetzner"): {{
					Kind: commands.AssignmentReasonKindAssignedViaRefOwnership,
					RefOwnership: &types.RefOwnershipPredicateLine{
						Cel: `gvk == "metrics.k8s.io/v1beta1/PodMetrics" && ns == "cert-manager" && name.startsWith("cert-manager-webhook-hetzner-")`,
						Source: &types.RefOwnershipRuleSource{
							Kind:      types.RefOwnershipRuleSourceKindHydraRefParser,
							BlockPath: "global.hydra.refs.cert-manager-webhook-hetzner.ref-parsers[0]",
							Sources:   []string{"charts-repository/apps/demo-infra/cert-manager-webhook-hetzner/dev/values.yaml"},
						},
					},
				}},
			},
		},
		UnassignedIDs: sets.New[types.Id](types.Id("v1/ConfigMap/default/orphan")),
	}
	appIds := sets.New[types.AppId](
		types.AppId("in-cluster.cluster-infra.cert-manager"),
		types.AppId("in-cluster.cluster-infra.dex"),
	)

	report := buildClusterShowReport("my-cluster", assignment, metadata, appIds, annotations)

	assert.Equal(t, clusterShowReport{
		Cluster: "my-cluster",
		Apps: []clusterShowAppEntry{
			{
				AppId: "in-cluster.cluster-infra.cert-manager",
				Count: 1,
				Resources: []clusterShowResourceEntry{{
					Id: "v1/Secret/cert-manager/webhook",
					Reasons: []clusterShowReasonEntry{{
						Kind: "assigned-via-template-id",
					}},
				}},
			},
			{
				AppId:     "in-cluster.cluster-infra.dex",
				Count:     0,
				Resources: nil,
			},
		},
		Ambiguous: []clusterShowAmbiguousEntry{{
			Id: "metrics.k8s.io/v1beta1/PodMetrics/cert-manager/pod-a",
			Candidates: []clusterShowAmbiguousCandidateEntry{
				{
					AppId: "in-cluster.cluster-infra.cert-manager",
					Reasons: []clusterShowReasonEntry{{
						Kind: "assigned-via-ref-ownership",
						RefOwnership: &clusterShowRefOwnershipEntry{
							Cel: `gvk == "metrics.k8s.io/v1beta1/PodMetrics" && ns == "cert-manager"`,
							Source: &clusterShowRefOwnershipSourceEntry{
								Kind:   "hydra-ref-parser",
								Path:   "ref-parsers[]",
								Source: "charts-repository/apps/demo-infra/cert-manager/dev/values.yaml",
							},
						},
					}},
				},
				{
					AppId: "in-cluster.cluster-infra.cert-manager-webhook-hetzner",
					Reasons: []clusterShowReasonEntry{{
						Kind: "assigned-via-ref-ownership",
						RefOwnership: &clusterShowRefOwnershipEntry{
							Cel: `gvk == "metrics.k8s.io/v1beta1/PodMetrics" && ns == "cert-manager" && name.startsWith("cert-manager-webhook-hetzner-")`,
							Source: &clusterShowRefOwnershipSourceEntry{
								Kind:   "hydra-ref-parser",
								Path:   "ref-parsers[]",
								Source: "charts-repository/apps/demo-infra/cert-manager-webhook-hetzner/dev/values.yaml",
							},
						},
					}},
				},
			},
		}},
		Unassigned: []clusterShowResourceEntry{{
			Id: "v1/ConfigMap/default/orphan",
			Reasons: []clusterShowReasonEntry{{
				Kind: "no-app-assignment",
			}},
		}},
	}, report)
}

func TestClusterShowRecognitionNotes_UsesResourceModelReasonsForPresetApps(t *testing.T) {
	t.Parallel()

	item := commands.ResourceModelRow{
		ID:             types.Id("networking.k8s.io/v1/IPAddress//10.43.59.66"),
		AssignedApp:    types.AppId("in-cluster.preset.kubernetes"),
		HasAssignedApp: true,
		Reasons: []commands.AssignmentReason{
			{Kind: commands.AssignmentReasonKindAssignedViaPresetMatch},
		},
	}

	notes := clusterShowRecognitionNotes(item, commands.ClusterEntityAssignmentMetadata{
		DirectRefMatchedBuiltinIDs:         sets.New[types.Id](),
		DirectRefMatchedAppIDs:             sets.New[types.Id](),
		AmbiguousAppIDsByClusterEntity:     map[types.Id][]types.AppId{},
		AmbiguousAppReasonsByClusterEntity: map[types.Id]map[types.AppId][]commands.AssignmentReason{},
		UnassignedIDs:                      sets.New[types.Id](),
	})

	assert.Equal(t, []clusterShowAnnotation{
		{Reason: commands.AssignmentReason{Kind: commands.AssignmentReasonKindAssignedViaPresetMatch}},
	}, notes)
}

func TestClusterShowReasonEntryFromAssignmentReason_UsesEventRouteAndRawParserFields(t *testing.T) {
	t.Parallel()

	valuesPath := filepath.Join(t.TempDir(), "values.yaml")
	require.NoError(t, os.WriteFile(valuesPath, []byte(`global:
  hydra:
    refs:
      image-pull-secret-mirror:
        tag:
          - uninstall-safe
        ref-parsers:
          - gvk: v1/Secret
            name: image-pull-secret
`), 0o644))

	cache := &clusterShowRefOwnershipSourceCache{decodedByPath: map[string]any{}}
	reason := commands.AssignmentReason{
		Kind:          commands.AssignmentReasonKindAssignedViaRefOwnership,
		EventRef:      "regarding",
		EventSubjects: []types.Id{types.Id("v1/Secret/sops-secrets-operator/image-pull-secret")},
		RefOwnership: &types.RefOwnershipPredicateLine{
			Cel: `version == "v1" && kind == "Secret" && name == "image-pull-secret"`,
			Source: &types.RefOwnershipRuleSource{
				Kind:      types.RefOwnershipRuleSourceKindHydraRefParser,
				BlockPath: "global.hydra.refs.image-pull-secret-mirror.ref-parsers[0]",
				Sources:   []string{valuesPath},
			},
		},
	}

	entry := clusterShowReasonEntryFromAssignmentReason(reason, cache)
	require.NotNil(t, entry.RefOwnership)
	require.NotNil(t, entry.RefOwnership.Via)
	assert.Equal(t, "regarding", entry.RefOwnership.Via.EventRef)
	assert.Equal(t, []string{"v1/Secret/sops-secrets-operator/image-pull-secret"}, entry.RefOwnership.Via.EventSubjects)
	assert.Equal(t, []string{"uninstall-safe"}, entry.RefOwnership.Tags)
	require.NotNil(t, entry.RefOwnership.Parser)
	assert.Equal(t, "v1/Secret", entry.RefOwnership.Parser["gvk"])
	assert.Equal(t, "image-pull-secret", entry.RefOwnership.Parser["name"])
	assert.Empty(t, entry.RefOwnership.Cel)
}

func TestClusterShowReasonEntryFromAssignmentReason_IncludesOwnerRefs(t *testing.T) {
	t.Parallel()

	entry := clusterShowReasonEntryFromAssignmentReason(commands.AssignmentReason{
		Kind: commands.AssignmentReasonKindAssignedViaOwnerRef,
		OwnerRefs: []types.Id{
			types.Id("apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator"),
			types.Id("isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret"),
		},
	}, &clusterShowRefOwnershipSourceCache{decodedByPath: map[string]any{}})

	assert.Equal(t, clusterShowReasonEntry{
		Kind: "assigned-via-owner-ref",
		OwnerRefs: []string{
			"apps/v1/Deployment/sops-secrets-operator/sops-secrets-operator",
			"isindir.github.com/v1alpha3/SopsSecret/sops-secrets-operator/image-pull-secret",
		},
	}, entry)
}

func TestWriteClusterShowTable_PrintsAppRowsWithOneResourcePerLine(t *testing.T) {
	t.Parallel()

	report := clusterShowReport{
		Cluster: "my-cluster",
		Apps: []clusterShowAppEntry{
			{
				AppId: "in-cluster.cluster-infra.cert-manager",
				Count: 2,
				Resources: []clusterShowResourceEntry{
					{Id: "v1/Secret/cert-manager/webhook"},
					{Id: "v1/ConfigMap/cert-manager/cainjector"},
				},
			},
			{AppId: "in-cluster.cluster-infra.dex", Count: 0},
		},
	}

	var buf bytes.Buffer
	err := writeClusterShowTable(&buf, false, report)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "cluster my-cluster\n")
	assert.Contains(t, out, "APP     RESOURCE")
	assert.Contains(t, out, "RESOURCE")
	assert.Contains(t, out, "COUNT")
	assert.Less(t, strings.Index(out, "RESOURCE"), strings.Index(out, "COUNT"))
	assert.Contains(t, out, "in-cluster.cluster-infra.cert-manager")
	assert.Contains(t, out, "  2\n")
	assert.Contains(t, out, "        v1/Secret/cert-manager/webhook\n")
	assert.Contains(t, out, "        v1/ConfigMap/cert-manager/cainjector\n")
	assert.Contains(t, out, "in-cluster.cluster-infra.dex")
	assert.Contains(t, out, "  0\n")
}

func TestWriteClusterShowErrorYaml_PrintsOnlyErrorBlocks(t *testing.T) {
	t.Parallel()

	report := clusterShowReport{
		Cluster: "my-cluster",
		Apps: []clusterShowAppEntry{
			{AppId: "in-cluster.cluster-infra.cert-manager", Count: 1},
		},
		Ambiguous: []clusterShowAmbiguousEntry{{
			Id: "v1/Secret/default/shared",
			Candidates: []clusterShowAmbiguousCandidateEntry{{
				AppId: "in-cluster.cluster-infra.cert-manager",
			}},
		}},
		Unassigned: []clusterShowResourceEntry{{
			Id: "v1/ConfigMap/default/orphan",
		}},
	}

	var buf bytes.Buffer
	err := writeClusterShowErrorYaml(&buf, false, report)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "cluster: my-cluster\n")
	assert.Contains(t, out, "ambiguous:\n")
	assert.Contains(t, out, "unassigned:\n")
	assert.NotContains(t, out, "\napps:\n")
}

func TestWriteClusterShowTable_ColorizesStandardOutputWhenEnabled(t *testing.T) {
	t.Parallel()

	report := clusterShowReport{
		Cluster: "my-cluster",
		Apps: []clusterShowAppEntry{{
			AppId: "in-cluster.cluster-infra.cert-manager",
			Count: 2,
			Resources: []clusterShowResourceEntry{
				{Id: "v1/Secret/cert-manager/webhook"},
				{Id: "v1/ConfigMap/cert-manager/cainjector"},
			},
		}},
	}

	var buf bytes.Buffer
	err := writeClusterShowTable(&buf, true, report)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, colors.LightCyan.String()+"cluster my-cluster"+colors.Reset.String())
	assert.Contains(t, out, colors.BoldLightMagenta()+"APP"+colors.Reset.String()+"     "+colors.BoldLightMagenta()+"RESOURCE"+colors.Reset.String())
	assert.Less(t, strings.Index(out, "RESOURCE"), strings.Index(out, "COUNT"))
	assert.Contains(t, out, colors.LightBlue.String()+"in-cluster.cluster-infra.cert-manager")
	assert.Contains(t, out, colors.LightGreen.String())
	assert.Contains(t, out, "2"+colors.Reset.String())
	assert.Contains(t, out, "        "+colors.LightGray.String()+"v1/Secret/cert-manager/webhook"+colors.Reset.String())
	assert.Contains(t, out, "        "+colors.LightGray.String()+"v1/ConfigMap/cert-manager/cainjector"+colors.Reset.String())
}
