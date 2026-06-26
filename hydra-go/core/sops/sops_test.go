package sops

import (
	"errors"
	"os/exec"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	baseerrors "hydra-gitops.org/hydra/hydra-go/base/errors"
)

func TestFormatDecryptError_AddsHelpWhenSopsCannotBeStarted(t *testing.T) {
	err := formatDecryptError("file /tmp/test.sops.yaml", exec.ErrNotFound, "")

	assert.Contains(t, err.Error(), "failed to decrypt file /tmp/test.sops.yaml")
	assert.Equal(t, baseerrors.ErrSopsDecryptFailed, baseerrors.Id(err))
	params, ok := baseerrors.TemplateParams(err)
	assert.True(t, ok)
	assert.Equal(t, "sops-not-found", params["reason"])
	assert.Equal(t, "file /tmp/test.sops.yaml", params["target"])
}

func TestFormatDecryptError_AddsHelpWhenAgeKeyIsMissing(t *testing.T) {
	t.Setenv("SOPS_AGE_KEY", "AGE-SECRET-KEY-EXAMPLE")

	err := formatDecryptError(
		"file /tmp/test.sops.yaml",
		errors.New("exit status 1"),
		"Error getting data key: 0 successful groups required, got 0\nfailed to create reader for decrypting sops data key with age: no identity matched any of the recipients",
	)

	assert.Contains(t, err.Error(), "stderr: Error getting data key")
	assert.Equal(t, baseerrors.ErrSopsDecryptFailed, baseerrors.Id(err))
	params, ok := baseerrors.TemplateParams(err)
	assert.True(t, ok)
	assert.Equal(t, "age-key-missing", params["reason"])
	assert.Contains(t, params["stderr"], "no identity matched any of the recipients")
	assert.Equal(t, true, params["SOPS_AGE_KEY"])

	statuses, ok := params["sopsEnvStatuses"].(map[string]bool)
	assert.True(t, ok)
	assert.Equal(t, true, statuses["SOPS_AGE_KEY"])
	assert.Equal(t, false, statuses["SOPS_AGE_KEY_FILE"])

	keys, ok := params["sopsEnvStatusKeys"].([]string)
	assert.True(t, ok)
	assert.Contains(t, keys, "SOPS_AGE_KEY")
	assert.Contains(t, keys, "SOPS_AGE_KEY_FILE")
	assert.True(t, sort.StringsAreSorted(keys))
	assert.Equal(t, true, params["sopsEnvAnyDefined"])
}

func TestFormatDecryptError_AddsHelpWhenAgeSSHIdentitiesAreMissing(t *testing.T) {
	err := formatDecryptError(
		"file /tmp/test.sops.yaml",
		errors.New("exit status 128"),
		`Failed to get the data key required to decrypt the SOPS file.

Group 0: FAILED
  ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMvrL1CJG09aLg6F4A2+TXINIKZRVHr8KUVoHDy0FWXB Dennis Rieks <dennis.rieks@rohde-schwarz.com>: FAILED
    - | failed to load age identities. Did not find keys in
      | locations 'SOPS_AGE_SSH_PRIVATE_KEY_FILE',
      | 'SOPS_AGE_SSH_PRIVATE_KEY_CMD', '/root/.ssh/id_ed25519',
      | '/root/.ssh/id_rsa', 'SOPS_AGE_KEY', 'SOPS_AGE_KEY_FILE',
      | 'SOPS_AGE_KEY_CMD', and '/root/.config/sops/age/keys.txt'.

Recovery failed because no master key was able to decrypt the file. In
order for SOPS to recover the file, at least one key has to be successful,
but none were.`,
	)

	assert.Contains(t, err.Error(), "failed to load age identities")
	assert.Equal(t, baseerrors.ErrSopsDecryptFailed, baseerrors.Id(err))
	params, ok := baseerrors.TemplateParams(err)
	assert.True(t, ok)
	assert.Equal(t, "age-key-missing", params["reason"])
	assert.Contains(t, params["stderr"], "Did not find keys in")
	assert.Equal(t, false, params["sopsEnvAnyDefined"])
}
