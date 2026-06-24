package hydra

import (
	"os"
	"path/filepath"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"hydra-gitops.org/hydra/hydra-go/base/cache"
	"hydra-gitops.org/hydra/hydra-go/base/errors"

	"hydra-gitops.org/hydra/hydra-go/core/helm"
	"hydra-gitops.org/hydra/hydra-go/core/types"
	"hydra-gitops.org/hydra/hydra-go/core/values"
)

// Context represents a GitOps context directory containing cluster configurations.
// It provides access to clusters and their associated configurations.
type Context struct {
	l           log.Logger
	ContextPath types.ContextPath
	cfg         types.Config
	caches      *hydraCaches
}

var _ Hydra = (*Context)(nil)

type contextCacheKey struct {
	contextPath                 types.ContextPath
	kubernetesConnectionAllowed types.KubernetesConnectionAllowed
	helmTemplateCacheEnabled    bool
}

func (k contextCacheKey) String() string {
	k8s := "no"
	if k.kubernetesConnectionAllowed {
		k8s = "yes"
	}
	cache := "no"
	if k.helmTemplateCacheEnabled {
		cache = "yes"
	}
	return string(k.contextPath) + ":k8s=" + k8s + ":helmCache=" + cache
}

var contextCache *cache.Cache[contextCacheKey, *Context]

func initContextCache(l log.Logger) {
	if contextCache == nil {
		contextCache = cache.NewCache[contextCacheKey, *Context](l, "context-cache", false, nil)
	}
}

// NewContext creates a new Context with the given path and configuration.
func NewContext(l log.Logger, path types.ContextPath, config types.Config) (*Context, error) {
	initContextCache(l)
	key := contextCacheKey{
		contextPath:                 path,
		kubernetesConnectionAllowed: config.KubernetesConnectionAllowed(),
		helmTemplateCacheEnabled:    config.HelmTemplateCacheEnabled(),
	}
	return contextCache.GetOrLoad(key, func() (*Context, error) {
		return &Context{
			l:           l,
			ContextPath: path,
			cfg:         config,
			caches:      newHydraCaches(l, config.HelmTemplateCacheEnabled()),
		}, nil
	})
}

// GetClusters returns a list of all clusters available in the context.
// Clusters are top-level directories in the context path (e.g. in-cluster, dev, prod).
// Subdirectories of in-cluster that are only root apps (e.g. argocd) without
// a corresponding top-level cluster directory are excluded.
func (c *Context) GetClusters() ([]*Cluster, error) {
	entries, err := os.ReadDir(string(c.ContextPath))
	if err != nil {
		return nil, log.CreateError(
			errors.ErrInvalidHydraStructure,
			"failed to read context directory",
			log.Err(err))
	}

	var clusters []*Cluster
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cluster, err := NewCluster(c, types.ClusterName(entry.Name()), RESTClientLimits{})
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, cluster)
	}

	c.l.DebugLog(logIdContext, "Retrieved clusters from context",
		log.String("contextPath", string(c.ContextPath)),
		log.Int("clusterCount", len(clusters)))

	return clusters, nil
}

// CreateContext creates a new Context by searching for a directory that resolves
// to global.hydra.type=context.
//
// The search starts from the given path and traverses up the directory tree.
// Returns an error if the structure cannot be found.
func CreateContext(l log.Logger, path string, config types.Config) (*Context, error) {
	cleanPath := filepath.Clean(path)
	context, err := createContext(l, cleanPath, config)
	switch {
	case err != nil:
		return nil, err
	case context != nil:
		return context, nil
	default:
		return nil, createMissingContextError(l, cleanPath)
	}
}

func createMissingContextError(l log.Logger, path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return log.CreateError(
			errors.ErrInvalidHydraStructure,
			"could not use Hydra context path '{path}': directory does not exist. Hint: set HYDRA_CONTEXT (or --hydra-context) to an existing Hydra context root directory.",
			log.String("path", path),
			log.String("reason", "directory-missing"),
		)
	}

	valuesPath := filepath.Join(path, "values.yaml")
	groupValuesPath := filepath.Join(filepath.Dir(path), "values.yaml")
	if _, err := os.Stat(valuesPath); os.IsNotExist(err) {
		return log.CreateError(
			errors.ErrInvalidHydraStructure,
			"could not find a valid Hydra context starting from '{path}'.\nHint: assuming '{path}' is the intended context directory, choose one of these fixes.\nRecommended: set 'global.hydra.type: group' in '{groupValuesPath}' so '{path}' resolves as a context beneath that group.\nAlternative: add 'global.hydra.type: context' to '{valuesPath}'.\nMissing file: '{valuesPath}'.",
			log.String("path", path),
			log.String("valuesPath", valuesPath),
			log.String("groupValuesPath", groupValuesPath),
			log.String("reason", "context-values-missing"),
		)
	}

	directive, err := loadHydraTypeDirective(l, path)
	if err != nil {
		return err
	}
	if directive.HasType && directive.Type != hydraTypeContext {
		return log.CreateError(
			errors.ErrInvalidHydraStructure,
			"could not find a valid Hydra context starting from '{path}'.\nHint: assuming '{path}' is the intended context directory, choose one of these fixes.\nRecommended: set 'global.hydra.type: group' in '{groupValuesPath}' so '{path}' resolves as a context beneath that group.\nAlternative: change '{valuesPath}' from 'global.hydra.type: {actualType}' to 'global.hydra.type: {expectedType}'.",
			log.String("path", path),
			log.String("valuesPath", valuesPath),
			log.String("groupValuesPath", groupValuesPath),
			log.String("actualType", directive.Type),
			log.String("expectedType", hydraTypeContext),
			log.String("reason", "context-type-mismatch"),
		)
	}
	if !directive.HasType {
		return log.CreateError(
			errors.ErrInvalidHydraStructure,
			"could not find a valid Hydra context starting from '{path}'.\nHint: assuming '{path}' is the intended context directory, choose one of these fixes.\nRecommended: set 'global.hydra.type: group' in '{groupValuesPath}' so '{path}' resolves as a context beneath that group.\nAlternative: add 'global.hydra.type: context' to '{valuesPath}'.",
			log.String("path", path),
			log.String("valuesPath", valuesPath),
			log.String("groupValuesPath", groupValuesPath),
			log.String("reason", "context-type-missing"),
		)
	}

	return log.CreateError(
		errors.ErrInvalidHydraStructure,
		"could not find a valid Hydra context starting from '{path}'.\nHint: assuming '{path}' is the intended context directory, choose one of these fixes.\nRecommended: set 'global.hydra.type: group' in '{groupValuesPath}' so '{path}' resolves as a context beneath that group.\nAlternative: add 'global.hydra.type: context' to '{valuesPath}'.",
		log.String("path", path),
		log.String("valuesPath", valuesPath),
		log.String("groupValuesPath", groupValuesPath),
		log.String("reason", "context-invalid"),
	)
}

func createContext(l log.Logger, path string, config types.Config) (*Context, error) {
	resolvedType, hasType, err := resolveHydraTypeAtPath(l, path)
	if err != nil {
		return nil, err
	}
	if hasType && resolvedType == hydraTypeContext {
		if err := validateLevelType(l, path, hydraTypeContext, hydraTypeContext); err != nil {
			return nil, err
		}
		l.DebugLog(logIdContext, "Found valid Hydra context directory",
			log.String("contextPath", path),
			log.String("type", resolvedType))
		return NewContext(l, types.ContextPath(path), config)
	}

	allowParentLookup, err := parentLookupEnabledAtPath(l, path)
	if err != nil {
		return nil, err
	}
	if !allowParentLookup {
		return nil, nil
	}

	// If not found, try parent directory
	parentPath := filepath.Dir(path)
	if parentPath == path {
		// We've reached the root directory without finding a valid Hydra context.
		return nil, nil
	}

	l.DebugLog(logIdContext, "valid context not found, checking parent directory",
		log.String("currentPath", path),
		log.String("parentPath", parentPath))

	return createContext(l, parentPath, config)
}

func (*Context) AsApp() HydraApp {
	return nil
}

// ChartCache returns the chart cache used for loading Helm charts.
func (c *Context) ChartCache() *helm.ChartCache {
	return c.caches.chartCache
}

// Config returns the configuration for this Context.
func (c *Context) Config() types.Config {
	return c.cfg
}

// L returns the logger for this Context.
func (c *Context) L() log.Logger {
	return c.l
}

// AsContext returns this Context.
func (c *Context) AsContext() *Context {
	return c
}

// AsCluster returns nil as a Context cannot be converted to a Cluster.
// Use GetClusters() to access the clusters within this context.
func (c *Context) AsCluster() *Cluster {
	return nil
}

// AsRootApp returns nil as a Context cannot be converted to a RootApp.
// Use Cluster.GetRootApps() to access root applications.
func (c *Context) AsRootApp() *RootApp {
	// This method is primarily for type conversion purposes
	// A Context doesn't directly know if it's a RootApp, so we return nil
	// Actual RootApps should be obtained through ResolvePath or Cluster.GetRootApps()
	return nil
}

// AsChildApp returns nil as a Context cannot be converted to a ChildApp.
func (c *Context) AsChildApp() *ChildApp {
	return nil
}

// WithCluster appends the given cluster to the current Context.
func (c *Context) WithCluster(clusterName types.ClusterName, limits RESTClientLimits) (*Cluster, error) {
	return NewCluster(c, clusterName, limits)
}

// WithApp processes the Context with the given app reference.
// It resolves the cluster from the app and delegates to that Cluster.
func (c *Context) WithApp(app types.AppId) (HydraApp, error) {
	clusterName, err := app.ClusterName()
	if err != nil {
		return nil, err
	}
	cluster, err := NewCluster(c, clusterName, RESTClientLimits{})
	if err != nil {
		return nil, err
	}
	return cluster.WithApp(app)
}

// LoadValuesMap loads global values from the context's values.yaml.
// It first loads group-level values, then merges context-specific values.
func (c *Context) LoadValuesMap(mode types.HelmNetworkMode) (types.ValuesMap, error) {
	groupValuesPath := filepath.Join(string(c.ContextPath), "..", "values.yaml")
	groupVals, err := values.LoadValuesFile(c.l, groupValuesPath)
	if err != nil {
		return nil, err
	}
	contextValuesPath := filepath.Join(string(c.ContextPath), "values.yaml")
	vals, err := values.LoadAndMergeValuesFile(c.l, contextValuesPath, groupVals)
	if err != nil {
		return nil, err
	}
	vals, err = ensureHydraTypeInValues(vals, hydraTypeContext, hydraTypeContext)
	if err != nil {
		return nil, err
	}
	return vals, nil
}

func (c *Context) Description() string {
	return "Context at " + string(c.ContextPath)
}
