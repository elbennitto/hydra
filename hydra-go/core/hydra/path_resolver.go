package hydra

import (
	"path/filepath"
	"strings"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"hydra-gitops.org/hydra/hydra-go/base/errors"

	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// ResolvePath analyzes a given path and returns the appropriate query type implementing the Hydra interface
// A RootApp is also a Cluster and a Context
// A Cluster is also a Context
// Returns a Hydra interface that can be one of: *Context, *Cluster, or *RootApp
func ResolvePath(l log.Logger, hydraContext types.HydraContext, config types.Config) (Hydra, error) {
	path := filepath.Clean(strings.TrimRight(string(hydraContext), "/"))

	// Try to create a context from the path
	context, err := CreateContext(l, path, config)
	if err != nil {
		return nil, log.CreateError(
			errors.ErrInvalidHydraStructure,
			"failed to use Hydra context path '{path}': {err}",
			log.String("path", path),
			log.Err(err))
	}

	// Analyze the structure to determine if it's a RootApp or Cluster
	// Structure: <context>/<cluster>/<rootapp>

	// Get relative path from context to the given path
	relPath, err := filepath.Rel(string(context.ContextPath), path)
	if err != nil {
		return nil, log.CreateError(
			errors.ErrInvalidHydraStructure,
			"failed to calculate relative path",
			log.Err(err))
	}

	parts := strings.Split(filepath.ToSlash(relPath), "/")

	l.DebugLog(logIdHydra, "Analyzing path structure",
		log.String("path", path),
		log.String("contextPath", string(context.ContextPath)),
		log.String("relPath", relPath),
		log.Int("pathDepth", len(parts)))

	// If path equals context path, it's just a context
	if relPath == "." {
		l.DebugLog(logIdHydra, "Resolved as Context '{contextPath}'",
			log.String("contextPath", string(context.ContextPath)))
		return context, nil
	}

	// Check if path is within <cluster>/<rootapp> structure
	if len(parts) >= 1 && parts[0] != "" {
		clusterName := types.ClusterName(parts[0])

		// Create a Cluster with ClusterName set
		cluster, err := NewCluster(context, clusterName, RESTClientLimits{})
		if err != nil {
			return nil, err
		}

		// If there are more parts, it's a RootApp
		if len(parts) >= 2 {
			rootAppName := types.RootAppName(parts[1])

			// Create a RootApp with the Cluster information
			rootApp, err := NewRootApp(cluster, rootAppName)
			if err != nil {
				return nil, err
			}
			l.DebugLog(logIdHydra, "Resolved as RootApp '{rootAppName}' in Cluster '{clusterName}'",
				log.String("clusterName", string(clusterName)),
				log.String("rootAppName", string(rootAppName)))
			return rootApp, nil
		}

		// It's just a cluster
		l.DebugLog(logIdHydra, "Resolved as Cluster '{clusterName}'",
			log.String("clusterName", string(clusterName)))
		return cluster, nil
	}

	// Default: treat as context
	l.DebugLog(logIdHydra, "Resolved as Context '{path}'", log.String("path", path))
	return context, nil
}

func ResolvePathWithCluster(
	l log.Logger,
	hydraContext types.HydraContext,
	clusterName types.ClusterName,
	config types.Config,
	limits RESTClientLimits,
) (*Cluster, error) {
	hydra, err := ResolvePath(l, hydraContext, config)
	if err != nil {
		return nil, log.CreateError(
			errors.ErrInvalidHydraStructure,
			"Error resolving path '{path}': {err}",
			log.String("path", string(hydraContext)),
			log.Err(err))
	}

	return hydra.WithCluster(clusterName, limits)
}

// ResolvePathWithAppId resolves a path and validates it with the given appId
// Returns the resolved Hydra object after validating it matches the expected appId
func ResolvePathWithAppId(
	l log.Logger,
	hydraContext types.HydraContext,
	appId types.AppId,
	config types.Config,
) (HydraApp, error) {
	clusterName, err := appId.ClusterName()
	if err != nil {
		return nil, err
	}

	cluster, err := ResolvePathWithCluster(l, hydraContext, clusterName, config, RESTClientLimits{})
	if err != nil {
		return nil, err
	}

	hydraApp, err := cluster.WithApp(appId)
	if err != nil {
		return nil, log.CreateError(
			errors.ErrInvalidHydraStructure,
			"Error resolving path '{path}' with app id '{appId}': {err}",
			log.String("path", string(hydraContext)),
			log.String("appId", string(appId)),
			log.Err(err))
	}

	return hydraApp, nil
}
