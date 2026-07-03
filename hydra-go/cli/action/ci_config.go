package action

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"hydra-gitops.org/hydra/hydra-go/base/colors"
	"hydra-gitops.org/hydra/hydra-go/core/ci"
)

type palette struct {
	bold, cyan, green, yellow, dim, reset string
}

func newPalette(useColor bool) palette {
	if !useColor {
		return palette{}
	}
	return palette{
		bold:   "\033[1m",
		cyan:   colors.Cyan.String(),
		green:  colors.Green.String(),
		yellow: colors.Yellow.String(),
		dim:    colors.LightGray.String(),
		reset:  colors.Reset.String(),
	}
}

func CiConfigInit(path string, in io.Reader, out io.Writer, useColor bool) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, ci.ConfigFileName)
	}

	if err := ci.ValidateOutputPath(path); err != nil {
		return err
	}

	absPath, _ := filepath.Abs(path)
	p := newPalette(useColor)

	var cfg *ci.Config
	existing := false
	if data, err := os.ReadFile(path); err == nil {
		cfg = &ci.Config{}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return fmt.Errorf("parsing existing config: %w", err)
		}
		existing = true
	} else {
		cfg = ci.DefaultConfig()
	}

	reader := bufio.NewReader(in)
	configDir := filepath.Dir(absPath)

	fmt.Fprintf(out, "\n%s%shydra ci config%s\n", p.bold, p.cyan, p.reset)
	if existing {
		fmt.Fprintf(out, "%sEditing:%s %s\n\n", p.dim, p.reset, absPath)
	} else {
		fmt.Fprintf(out, "%sCreating:%s %s\n\n", p.dim, p.reset, absPath)
	}

	rap, err := promptString(reader, out, p,
		"ci.rootAppsPath",
		"Directory containing root apps (each with <env>/ subdirs)",
		cfg.CI.RootAppsPath,
		func(w io.Writer, pal palette, val string) bool {
			abs := filepath.Join(configDir, val)
			subdirs := listSubdirs(abs)
			if len(subdirs) == 0 {
				fmt.Fprintf(w, "  %s⚠ No root apps found in %q%s\n", pal.yellow, val, pal.reset)
				return false
			}
			fmt.Fprintf(w, "  %sFound root apps:%s", pal.dim, pal.reset)
			for _, d := range subdirs {
				fmt.Fprintf(w, " %s%s%s", pal.green, filepath.Base(d), pal.reset)
			}
			fmt.Fprintln(w)
			return true
		})
	if err != nil {
		return err
	}
	cfg.CI.RootAppsPath = rap

	absBase := filepath.Join(configDir, cfg.CI.RootAppsPath)

	if detected, err := ci.DetectEnvironments(absBase); err == nil && len(detected) > 0 {
		defaults := ci.DefaultConfig().CI.Environments
		if isSubset(detected, defaults) {
			cfg.CI.Environments = defaults
		} else {
			cfg.CI.Environments = detected
		}
	}

	envs, err := promptSlice(reader, out, p,
		"ci.environments",
		"Environments in promote order, comma-separated (e.g. dev, stage, prod)",
		cfg.CI.Environments,
		func(w io.Writer, pal palette, _ []string) {
			detected, err := ci.DetectEnvironments(absBase)
			if err != nil || len(detected) == 0 {
				fmt.Fprintf(w, "  %s⚠ No environments found in %s/*/*/%s\n", pal.yellow, cfg.CI.RootAppsPath, pal.reset)
				return
			}
			fmt.Fprintf(w, "  %sFound environments:%s", pal.dim, pal.reset)
			for _, e := range detected {
				fmt.Fprintf(w, " %s%s%s", pal.green, e, pal.reset)
			}
			fmt.Fprintln(w)
		})
	if err != nil {
		return err
	}
	cfg.CI.Environments = envs

	reg, err := promptString(reader, out, p,
		"ci.registry",
		"OCI registry URL for helm push",
		cfg.CI.Registry)
	if err != nil {
		return err
	}
	cfg.CI.Registry = reg

	sp, err := promptString(reader, out, p,
		"ci.secretsPath",
		"Directory or file path for the SOPS-encrypted CI secrets file (default: same directory as .hydra-ci.yaml)",
		cfg.CI.SecretsPath)
	if err != nil {
		return err
	}
	cfg.CI.SecretsPath = sp

	wh, err := promptString(reader, out, p,
		"ci.teams.webhookUrl",
		"MS Teams webhook URL for notifications (optional)",
		cfg.CI.Teams.WebhookURL)
	if err != nil {
		return err
	}
	cfg.CI.Teams.WebhookURL = wh

	if err := ci.WriteConfig(path, cfg); err != nil {
		return err
	}

	fmt.Fprintf(out, "\n%s%s✓ Written:%s %s\n", p.bold, p.green, p.reset, absPath)
	return nil
}

type changeAction int

const (
	actionKeep changeAction = iota
	actionChange
	actionRevert
)

func askKeep(reader *bufio.Reader, out io.Writer, p palette, prompt string, canRevert bool) (changeAction, error) {
	for {
		if canRevert {
			fmt.Fprintf(out, "%s %s[Y/n/r]%s: ", prompt, p.dim, p.reset)
		} else {
			fmt.Fprintf(out, "%s %s[Y/n]%s: ", prompt, p.dim, p.reset)
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return actionKeep, fmt.Errorf("reading input: %w", err)
		}
		switch strings.TrimSpace(strings.ToLower(line)) {
		case "", "y":
			return actionKeep, nil
		case "n":
			return actionChange, nil
		case "r":
			if canRevert {
				return actionRevert, nil
			}
		}
	}
}

func promptString(reader *bufio.Reader, out io.Writer, p palette, label, description, current string, previewFn ...func(io.Writer, palette, string) bool) (string, error) {
	original := current
	for {
		display := current
		if display == "" {
			display = p.dim + "(not set)" + p.reset
		} else {
			display = p.green + display + p.reset
		}
		fmt.Fprintf(out, "%s%s%s%s\n", p.bold, p.cyan, label, p.reset)
		fmt.Fprintf(out, "  %s%s%s\n", p.dim, description, p.reset)
		fmt.Fprintf(out, "  Current: %s\n", display)
		valid := true
		if len(previewFn) > 0 && previewFn[0] != nil {
			valid = previewFn[0](out, p, current)
		}
		if valid {
			action, err := askKeep(reader, out, p, "  Keep?", current != original)
			if err != nil {
				return "", err
			}
			switch action {
			case actionKeep:
				fmt.Fprintln(out)
				return current, nil
			case actionRevert:
				current = original
				continue
			}
		}
		fmt.Fprintf(out, "  New value: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("reading input: %w", err)
		}
		current = strings.TrimSpace(line)
	}
}

func promptSlice(reader *bufio.Reader, out io.Writer, p palette, label, description string, current []string, previewFn ...func(io.Writer, palette, []string)) ([]string, error) {
	original := make([]string, len(current))
	copy(original, current)
	for {
		display := strings.Join(current, ", ")
		if display == "" {
			display = p.dim + "(not set)" + p.reset
		} else {
			display = p.green + display + p.reset
		}
		fmt.Fprintf(out, "%s%s%s%s\n", p.bold, p.cyan, label, p.reset)
		fmt.Fprintf(out, "  %s%s%s\n", p.dim, description, p.reset)
		fmt.Fprintf(out, "  Current: %s\n", display)
		if len(previewFn) > 0 && previewFn[0] != nil {
			previewFn[0](out, p, current)
		}
		action, err := askKeep(reader, out, p, "  Keep?", !slicesEqual(current, original))
		if err != nil {
			return nil, err
		}
		switch action {
		case actionKeep:
			fmt.Fprintln(out)
			return current, nil
		case actionRevert:
			current = make([]string, len(original))
			copy(current, original)
			continue
		}
		fmt.Fprintf(out, "  New value: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading input: %w", err)
		}
		parts := strings.Split(strings.TrimSpace(line), ",")
		current = nil
		for _, part := range parts {
			if v := strings.TrimSpace(part); v != "" {
				current = append(current, v)
			}
		}
	}
}

func isSubset(sub, super []string) bool {
	set := make(map[string]struct{}, len(super))
	for _, s := range super {
		set[s] = struct{}{}
	}
	for _, s := range sub {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func listSubdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(dirs)
	return dirs
}
