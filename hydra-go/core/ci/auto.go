package ci

import (
	"fmt"
	"path/filepath"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/git"
)

// RunAuto executes CI pipeline stages in order for the charts repository
// described by configPath. onPromoteEntry is invoked for each promote entry
// when the promote step runs; it may be nil.
func RunAuto(configPath string, mode Mode, targetBranch, promoteTo string, onPromoteEntry func(PromotionEntry)) error {
	l := log.Default()
	dir := filepath.Dir(configPath)
	cfg, err := LoadConfig(dir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	steps, err := ResolveAutoSteps(cfg)
	if err != nil {
		return err
	}

	absChartsPath, err := filepath.Abs(filepath.Join(dir, cfg.CI.RootAppsPath))
	if err != nil {
		return fmt.Errorf("resolve charts path: %w", err)
	}

	repo := git.Open(absChartsPath)
	if repo.Err != nil {
		return fmt.Errorf("open charts repo: %w", repo.Err)
	}

	branch, err := repo.CurrentBranch()
	if err != nil {
		return fmt.Errorf("current branch: %w", err)
	}
	l.Info(logIdCI, "hydra ci run auto on branch {branch}", log.String("branch", branch))

	for _, step := range steps {
		l.Info(logIdCI, "hydra ci run auto step {step}", log.String("step", step))
		if err := runAutoStep(step, configPath, mode, targetBranch, promoteTo, onPromoteEntry); err != nil {
			return fmt.Errorf("auto step %q: %w", step, err)
		}
	}
	return nil
}

func runAutoStep(step, configPath string, mode Mode, targetBranch, promoteTo string, onPromoteEntry func(PromotionEntry)) error {
	switch step {
	case "download":
		return RunDownload(configPath, mode)
	case "test":
		return RunTest(configPath, mode)
	case "release":
		_, err := RunRelease(configPath, mode, targetBranch)
		return err
	case "publish":
		return RunPublish(configPath, mode, nil, false, false, false, false)
	case "promote":
		actions := NewPromoteActions(mode, targetBranch)
		var opts []func(PromotionEntry)
		if onPromoteEntry != nil {
			opts = append(opts, onPromoteEntry)
		}
		_, err := RunPromote(configPath, mode, actions, targetBranch, promoteTo, opts...)
		return err
	case "sync":
		return RunSync(mode)
	case "update":
		return RunUpdate(mode)
	case "sprint":
		return RunSprint(mode)
	case "upgrade":
		return RunUpgrade(configPath, mode, "", false)
	default:
		return fmt.Errorf("internal: unknown step %q", step)
	}
}
