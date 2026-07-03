package flags

import (
	"fmt"

	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

// ClusterRESTClientFlags holds optional Kubernetes client-go REST QPS/burst overrides for
// `hydra gitops` commands (see k8s.io/client-go/rest.Config).
type ClusterRESTClientFlags struct {
	APIClientQPS   float32
	APIClientBurst int
}

// ToRESTClientLimits maps CLI flag fields to [hydra.RESTClientLimits] for cluster resolution and API clients.
func (f ClusterRESTClientFlags) ToRESTClientLimits() hydra.RESTClientLimits {
	return hydra.RESTClientLimits{QPS: f.APIClientQPS, Burst: f.APIClientBurst}
}

// MergeClusterRESTFromCmd copies persistent --qps / --api-burst from the nearest `cluster`
// ancestor into dst. If no such flags exist (tests, non-cluster callers), dst is unchanged.
func MergeClusterRESTFromCmd(cmd *cobra.Command, dst *ClusterRESTClientFlags) error {
	if cmd == nil || dst == nil {
		return nil
	}
	for c := cmd; c != nil; c = c.Parent() {
		pf := c.PersistentFlags()
		if pf.Lookup("qps") == nil {
			continue
		}
		var err error
		dst.APIClientQPS, err = pf.GetFloat32("qps")
		if err != nil {
			return err
		}
		dst.APIClientBurst, err = pf.GetInt("api-burst")
		return err
	}
	return nil
}

// ValidateClusterRESTClientFlags enforces --api-burst rules (requires explicit --qps).
func ValidateClusterRESTClientFlags(f *ClusterRESTClientFlags) error {
	if f == nil {
		return nil
	}
	if f.APIClientBurst != 0 && f.APIClientQPS == 0 {
		return fmt.Errorf("--api-burst requires --qps (set a positive limit or a negative value to disable client-side throttling)")
	}
	if f.APIClientBurst != 0 && f.APIClientQPS < 0 {
		return fmt.Errorf("--api-burst cannot be used with a negative --qps (client-side throttling is already disabled)")
	}
	if f.APIClientBurst < 0 {
		return fmt.Errorf("--api-burst must not be negative")
	}
	return nil
}
