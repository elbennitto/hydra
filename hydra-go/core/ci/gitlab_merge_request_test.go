package ci

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hydra-gitops.org/hydra/hydra-go/core/git"
)

func TestGitLabToken_UsesSecretsBeforeCIJobToken(t *testing.T) {
	repo := git.Init(t.TempDir())
	require.NoError(t, repo.Err)

	oldHook := loadSecretsConfigHook
	loadSecretsConfigHook = func(configPath string) (*SecretsConfig, error) {
		return &SecretsConfig{Secrets: SecretsValues{Publish: PublishSecrets{GitLabToken: "token-from-secrets"}}}, nil
	}
	t.Cleanup(func() {
		loadSecretsConfigHook = oldHook
	})

	token, header, err := gitLabToken(repo)
	require.NoError(t, err)
	assert.Equal(t, "token-from-secrets", token)
	assert.Equal(t, "PRIVATE-TOKEN", header)
}

func TestGitLabToken_RejectsEnvFallback(t *testing.T) {
	repo := git.Init(t.TempDir())
	require.NoError(t, repo.Err)

	oldHook := loadSecretsConfigHook
	loadSecretsConfigHook = func(configPath string) (*SecretsConfig, error) {
		return &SecretsConfig{Secrets: SecretsValues{}}, nil
	}
	t.Cleanup(func() {
		loadSecretsConfigHook = oldHook
	})

	t.Setenv("GITLAB_TOKEN", "token-from-env")
	t.Setenv("GITLAB_API_TOKEN", "")
	t.Setenv("CI_JOB_TOKEN", "token-from-ci-job")

	_, _, err := gitLabToken(repo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secrets.publish.gitlabToken")
}

func TestGitLabToken_ReturnsSecretsLoadError(t *testing.T) {
	repo := git.Init(t.TempDir())
	require.NoError(t, repo.Err)

	oldHook := loadSecretsConfigHook
	loadSecretsConfigHook = func(configPath string) (*SecretsConfig, error) {
		return nil, errors.New("decrypt failed")
	}
	t.Cleanup(func() {
		loadSecretsConfigHook = oldHook
	})

	_, _, err := gitLabToken(repo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load CI secrets")
	assert.Contains(t, err.Error(), "decrypt failed")
}
