package sops

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	baseerrors "hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

// DecryptSopsFile decrypts a SOPS-encrypted file and returns the plaintext content.
func DecryptSopsFile(path string) (types.YamlString, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// sops reads from absPath and writes decrypted content to stdout.
	cmd := exec.Command("sops", "--decrypt", absPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", formatDecryptError(fmt.Sprintf("file %s", absPath), err, stderr.String())
	}

	return types.YamlString(stdout.String()), nil
}

// DecryptSopsYaml decrypts SOPS-encrypted YAML content in memory via stdin.
func DecryptSopsYaml(data types.YamlString) (types.YamlString, error) {
	cmd := exec.Command("sops", "--decrypt", "--input-type", "yaml", "--output-type", "yaml", "/dev/stdin")
	cmd.Stdin = bytes.NewReader([]byte(data))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", formatDecryptError("YAML via stdin", err, stderr.String())
	}

	return types.YamlString(stdout.String()), nil
}

// EncryptSopsYaml encrypts YAML content in memory and returns the encrypted
// document as YAML. The target path is still provided to sops via
// --filename-override so matching creation_rules apply.
func EncryptSopsYaml(data types.YamlString, path string) (types.YamlString, error) {
	return EncryptSopsYamlWithConfig(data, path, "")
}

// EncryptSopsYamlWithConfig encrypts YAML content in memory and optionally
// passes an explicit SOPS config file via --config.
func EncryptSopsYamlWithConfig(data types.YamlString, path string, configPath string) (types.YamlString, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	filenameOverride := absPath
	args := []string{"--encrypt"}
	if configPath != "" {
		absConfigPath, err := filepath.Abs(configPath)
		if err != nil {
			return "", fmt.Errorf("failed to get absolute config path: %w", err)
		}
		relPath, err := filepath.Rel(filepath.Dir(absConfigPath), absPath)
		if err != nil {
			return "", fmt.Errorf("failed to relativize %s against config %s: %w", absPath, absConfigPath, err)
		}
		filenameOverride = filepath.ToSlash(relPath)
		args = append(args, "--config", absConfigPath)
	}
	args = append(args, "--filename-override", filenameOverride)
	args = append(args, "/dev/stdin")
	cmd := exec.Command("sops", args...)
	cmd.Stdin = bytes.NewReader([]byte(data))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to encrypt YAML for %s: %w\nstderr: %s", absPath, err, stderr.String())
	}

	return types.YamlString(stdout.String()), nil
}

// EncryptSopsFile encrypts the data and writes it to the file at path.
// Data is passed via stdin to sops, sops writes the encrypted content to absPath.
func EncryptSopsFile(data types.YamlString, path string) error {
	encrypted, err := EncryptSopsYaml(data, path)
	if err != nil {
		return err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}
	return os.WriteFile(absPath, []byte(encrypted), 0o644)
}

func formatDecryptError(target string, err error, stderr string) error {
	trimmedStderr := strings.TrimSpace(stderr)
	reason := decryptReason(err, trimmedStderr)
	params := []any{
		log.String("target", target),
		log.String("reason", reason),
		log.String("stderr", trimmedStderr),
	}
	params = append(params, sopsTemplateParams()...)

	message := fmt.Sprintf("failed to decrypt %s: %v", target, err)
	if trimmedStderr != "" {
		message += "\nstderr: " + trimmedStderr
	}

	switch reason {
	case "sops-not-found", "age-key-missing":
		return log.CreateError(
			baseerrors.ErrSopsDecryptFailed,
			message,
			params...,
		)
	default:
		return errors.New(message)
	}
}

func sopsTemplateParams() []any {
	knownNames := map[string]struct{}{
		"SOPS_AGE_KEY":                  {},
		"SOPS_AGE_KEY_CMD":              {},
		"SOPS_AGE_KEY_FILE":             {},
		"SOPS_AGE_SSH_PRIVATE_KEY_CMD":  {},
		"SOPS_AGE_SSH_PRIVATE_KEY_FILE": {},
	}

	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if !found || !strings.HasPrefix(name, "SOPS_") {
			continue
		}
		knownNames[name] = struct{}{}
	}

	names := make([]string, 0, len(knownNames))
	for name := range knownNames {
		names = append(names, name)
	}
	sort.Strings(names)

	statuses := make(map[string]bool, len(names))
	anyDefined := false
	params := make([]any, 0, len(names)+3)
	for _, name := range names {
		_, defined := os.LookupEnv(name)
		if defined {
			anyDefined = true
			params = append(params, log.ExtendedHelp(name, true))
		}
		statuses[name] = defined
	}

	params = append(params, log.ExtendedHelp("sopsEnvStatuses", statuses))
	params = append(params, log.ExtendedHelp("sopsEnvStatusKeys", names))
	params = append(params, log.ExtendedHelp("sopsEnvAnyDefined", anyDefined))
	return params
}

func decryptReason(err error, stderr string) string {
	if errors.Is(err, exec.ErrNotFound) {
		return "sops-not-found"
	}

	lowerStderr := strings.ToLower(stderr)
	if strings.Contains(lowerStderr, "no identity matched any of the recipients") ||
		strings.Contains(lowerStderr, "failed to create reader for decrypting sops data key with age") ||
		strings.Contains(lowerStderr, "0 successful groups required, got 0") ||
		strings.Contains(lowerStderr, "failed to load age identities") ||
		strings.Contains(lowerStderr, "did not find keys in") {
		return "age-key-missing"
	}

	return ""
}
