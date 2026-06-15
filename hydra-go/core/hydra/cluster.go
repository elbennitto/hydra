package hydra

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"hydra-gitops.org/hydra/hydra-go/base/cache"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// Cluster represents a specific cluster within a GitOps context.
// It combines a Context with a cluster name to provide access to cluster-specific
// configurations and root applications.
type Cluster struct {
	*Context
	ClusterName types.ClusterName

	// RESTClientLimits tune client-go REST QPS/burst for this cluster instance (see [RESTClientLimits]).
	RESTClientLimits

	// preferredVersionsCache caches types.PreferredVersionMap(scopeInfo) for this cluster instance.
	// ResetPreferredVersionsCache clears it so the next PreferredVersions call recomputes (e.g. at RenderCluster start).
	pvMu    sync.Mutex
	pvMap   map[types.GroupKindKey]types.Version
	pvErr   error
	pvReady bool

	// Logged API version normalization warnings (in-memory only; see api_version_normalization_warnings.go).
	apiNormWarnMu   sync.Mutex
	apiNormWarnKeys sets.Set[string]

	// helmInputValuesForApp, when non-nil, supplies per-app Helm chart ValuesMap for Template rendering
	// (cluster CLI). Protected by helmInputMu; must be cleared after the caller finishes rendering.
	helmInputMu           sync.Mutex
	helmInputValuesForApp func(types.AppId) (types.ValuesMap, error)

	// kubeRESTFlagsOnce ensures BuildValidatedKubernetesConfigFlagsForCluster runs at most once per
	// cached Cluster instance so user-config WARN/INFO and allowedContexts checks are not repeated.
	kubeRESTFlagsOnce sync.Once
	kubeRESTFlags     *genericclioptions.ConfigFlags
	kubeRESTFlagsErr  error
}

var _ Hydra = (*Cluster)(nil)

var clusterCache *cache.Cache[clusterCacheKey, *Cluster]

func initClusterCache(l log.Logger) {
	if clusterCache == nil {
		clusterCache = cache.NewCache[clusterCacheKey, *Cluster](l, "cluster-cache", false, nil)
	}
}

type clusterCacheKey struct {
	contextPath                 types.ContextPath
	clusterName                 types.ClusterName
	kubernetesConnectionAllowed types.KubernetesConnectionAllowed
	helmTemplateCacheEnabled    bool
	restQPS                     float32
	restBurst                   int
}

func (k clusterCacheKey) String() string {
	k8s := "no"
	if k.kubernetesConnectionAllowed {
		k8s = "yes"
	}
	cache := "no"
	if k.helmTemplateCacheEnabled {
		cache = "yes"
	}
	return fmt.Sprintf("%s:%s:k8s=%s:helmCache=%s:restQPS=%g:restBurst=%d",
		k.contextPath, k.clusterName, k8s, cache, k.restQPS, k.restBurst)
}

func (c *Cluster) cacheKey() clusterCacheKey {
	return clusterCacheKey{
		contextPath:                 c.ContextPath,
		clusterName:                 c.ClusterName,
		kubernetesConnectionAllowed: c.Config().KubernetesConnectionAllowed(),
		helmTemplateCacheEnabled:    c.Config().HelmTemplateCacheEnabled(),
		restQPS:                     c.RESTClientLimits.QPS,
		restBurst:                   c.RESTClientLimits.Burst,
	}
}

// ClusterPath returns the path to this cluster's directory.
func (c *Cluster) ClusterPath() string {
	return filepath.Join(string(c.ContextPath), string(c.ClusterName))
}

// WithCluster validates the cluster name and returns a Cluster instance for the given REST limits.
func (c *Cluster) WithCluster(cluster types.ClusterName, limits RESTClientLimits) (*Cluster, error) {
	err := validateClusterName(c.ClusterName, cluster)
	if err != nil {
		return nil, err
	}
	return NewCluster(c.Context, cluster, limits)
}

// NewCluster creates a new Cluster instance for the given context and cluster name.
// The cluster represents a deployment target within the GitOps repository structure.
// limits are stored on the cluster and used when configuring Kubernetes API clients.
func NewCluster(context *Context, clusterName types.ClusterName, limits RESTClientLimits) (*Cluster, error) {
	initClusterCache(context.l)
	key := clusterCacheKey{
		contextPath:                 context.ContextPath,
		clusterName:                 clusterName,
		kubernetesConnectionAllowed: context.Config().KubernetesConnectionAllowed(),
		helmTemplateCacheEnabled:    context.Config().HelmTemplateCacheEnabled(),
		restQPS:                     limits.QPS,
		restBurst:                   limits.Burst,
	}

	cluster, err := clusterCache.GetOrLoad(key, func() (*Cluster, error) {
		return &Cluster{
			Context:          context,
			ClusterName:      clusterName,
			RESTClientLimits: limits,
		}, nil
	})
	if err != nil {
		return nil, err
	}

	if err := validateLevelType(cluster.l, cluster.ClusterPath(), hydraTypeCluster, hydraTypeCluster); err != nil {
		return nil, err
	}

	return cluster, nil
}

func (c *Cluster) AppIds(networkMode types.HelmNetworkMode) (sets.Set[types.AppId], error) {
	rootApps, err := c.GetRootApps()
	if err != nil {
		return nil, err
	}

	appIds := sets.New[types.AppId]()
	for _, rootApp := range rootApps {
		appIds = appIds.Insert(rootApp.AppId())
		childAppIds, err := rootApp.GetChildAppIds(networkMode)
		if err != nil {
			return nil, err
		}
		appIds = appIds.Union(childAppIds)
	}

	return appIds, nil
}

// GetRootApps returns a list of all RootApps available in this cluster.
// RootApps are directories within the cluster's in-cluster directory.
// Dot-prefixed directories (e.g. .hydra used for Hydra state under the cluster path) are ignored.
func (c *Cluster) GetRootApps() ([]*RootApp, error) {
	clusterPath := filepath.Join(
		string(c.ContextPath),
		string(c.ClusterName))

	entries, err := os.ReadDir(clusterPath)
	if err != nil {
		return nil, log.CreateError(
			errors.ErrInvalidHydraStructure,
			"failed to read cluster directory '{path}'",
			log.String("path", clusterPath),
			log.Err(err))
	}

	var rootApps []*RootApp
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			rootApp, err := NewRootApp(c, types.RootAppName(entry.Name()))
			if err != nil {
				return nil, err
			}
			rootApps = append(rootApps, rootApp)
		}
	}

	return rootApps, nil
}

// AsCluster returns this Cluster.
func (c *Cluster) AsCluster() *Cluster {
	return c
}

// WithApp processes the Cluster with the given app reference.
// It validates that the app's cluster matches this cluster and then
// delegates to the corresponding RootApp.
func (c *Cluster) WithApp(app types.AppId) (HydraApp, error) {
	if err := validateCluster(app, c.ClusterName); err != nil {
		return nil, err
	}

	rootAppName, err := app.RootAppName()
	if err != nil {
		return nil, err
	}

	rootApp, err := NewRootApp(c, rootAppName)
	if err != nil {
		return nil, err
	}

	return rootApp.WithApp(app)
}

// LoadValuesMap loads and merges global values from context and cluster levels.
// Values from the cluster level override values from the context level.
func (c *Cluster) LoadValuesMap(mode types.HelmNetworkMode) (types.ValuesMap, error) {
	// First load context-level values
	contextVals, err := c.Context.LoadValuesMap(mode)
	if err != nil {
		return nil, err
	}

	// Load and merge cluster-level values
	vals, err := values.LoadAndMergeValuesFile(c.l, c.ClusterValuesFile(), contextVals)
	if err != nil {
		return nil, err
	}
	vals, err = ensureHydraTypeInValues(vals, hydraTypeCluster, hydraTypeCluster)
	if err != nil {
		return nil, err
	}

	return vals, nil
}

func (c *Cluster) ClusterValuesFile() string {
	return filepath.Join(string(c.ContextPath), string(c.ClusterName), "values.yaml")
}

func (c *Cluster) Description() string {
	return "Cluster " + string(c.ClusterName) + " at " + string(c.Context.ContextPath)
}

// ResetPreferredVersionsCache clears the cached preferred-version map so the next PreferredVersions
// call will run compute again. Call at the start of a render/refs operation that shares a Cluster
// instance across multiple steps.
func (c *Cluster) ResetPreferredVersionsCache() {
	if c == nil {
		return
	}
	c.pvMu.Lock()
	defer c.pvMu.Unlock()
	c.pvMap = nil
	c.pvErr = nil
	c.pvReady = false
}

// PreferredVersions returns the map of group/kind to preferred API version derived from cluster
// API discovery (ScopeInfoMap). The first call runs compute and caches the result; later calls
// return the cache until ResetPreferredVersionsCache is used.
//
// If cluster is nil, compute is invoked once without caching (for tests or callers without a cluster).
func (c *Cluster) PreferredVersions(
	compute func() (types.ScopeInfoMap, error),
) (map[types.GroupKindKey]types.Version, error) {
	if c == nil {
		if compute == nil {
			return nil, nil
		}
		scope, err := compute()
		if err != nil {
			return nil, err
		}
		return types.PreferredVersionMap(scope)
	}
	c.pvMu.Lock()
	defer c.pvMu.Unlock()
	if c.pvReady {
		return c.pvMap, c.pvErr
	}
	if compute == nil {
		// No cached value yet; do not mark ready so a later call with compute can populate.
		return nil, nil
	}
	scope, err := compute()
	if err != nil {
		c.pvErr = err
		c.pvReady = true
		return nil, err
	}
	pv, pvErr := types.PreferredVersionMap(scope)
	if pvErr != nil {
		c.pvErr = pvErr
		c.pvReady = true
		return nil, pvErr
	}
	c.pvMap = pv
	c.pvReady = true
	return c.pvMap, nil
}
