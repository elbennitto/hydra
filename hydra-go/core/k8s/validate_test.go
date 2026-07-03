package k8s

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestValidateApiContext_UsesExplicitContextOverride(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config")
	require.NoError(t, clientcmd.WriteToFile(clientcmdapi.Config{
		Contexts: map[string]*clientcmdapi.Context{
			"example-prod": {
				Cluster:  "prod-cluster",
				AuthInfo: "prod-user",
			},
		},
		Clusters: map[string]*clientcmdapi.Cluster{
			"prod-cluster": {
				Server: "https://example.invalid",
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"prod-user": {},
		},
	}, configPath))

	ctxName := "example-prod"
	flags := genericclioptions.NewConfigFlags(true)
	flags.KubeConfig = &configPath
	flags.Context = &ctxName

	err := ValidateApiContext(log.Default(), types.ColorNo, &types.HydraValues{
		KubeCtl: types.HydraKubectl{
			AllowedContexts: []types.HydraKubectlContext{{Name: "example-prod"}},
		},
	}, "test cluster", flags)

	require.NoError(t, err)
}
