package hydra

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

const clusterRootHydraFileName = "hydra.yaml"

func loadClusterRootHydraFile(clusterPath string) (*types.ClusterRootHydraFile, error) {
	path := filepath.Join(clusterPath, clusterRootHydraFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, log.CreateError(
			errors.ErrHydraConfigError,
			"failed to read cluster root hydra.yaml at '{path}': {err}",
			log.String("path", path),
			log.Err(err),
		)
	}
	var file types.ClusterRootHydraFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, log.CreateError(
			errors.ErrHydraConfigError,
			"failed to parse cluster root hydra.yaml at '{path}': {err}",
			log.String("path", path),
			log.Err(err),
		)
	}
	return &file, nil
}

func clusterRootHydraOverrides(cluster *Cluster) (types.ClusterRootHydraOverrides, error) {
	if cluster == nil {
		return nil, nil
	}
	file, err := loadClusterRootHydraFile(cluster.ClusterPath())
	if err != nil || file == nil || file.Hydra == nil || len(file.Hydra.Overrides) == 0 {
		return nil, err
	}
	return file.Hydra.Overrides, nil
}

func ClusterRootAppOverrides(cluster *Cluster) (map[types.AppId]types.ClusterRootAppOverrideList, error) {
	raw, err := clusterRootHydraOverrides(cluster)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	out := make(map[types.AppId]types.ClusterRootAppOverrideList, len(raw))
	for key, items := range raw {
		target, err := types.ParseClusterLocalOverrideTarget(cluster.ClusterName, key)
		if err != nil {
			return nil, log.CreateError(
				errors.ErrHydraConfigError,
				"invalid cluster root hydra.yaml override key {key}: {err}",
				log.String("key", key),
				log.Err(err),
			)
		}
		out[target] = append(out[target], items...)
	}
	return out, nil
}
