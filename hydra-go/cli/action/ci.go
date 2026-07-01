package action

import (
	"fmt"
	"io"
	"os"

	"hydra-gitops.org/hydra/hydra-go/base/log"
	"hydra-gitops.org/hydra/hydra-go/core/ci"
)

// CiFlags holds the common flags for all CI pipeline subcommands.
// --dry-run and --local are mutually exclusive.
type CiFlags struct {
	DryRun             bool
	Local              bool
	ConfigPath         string
	TargetBranch       string
	PromoteTo          string
	Charts             []string
	BuildTag           string
	VersionsFile       string
	SkipMissing        bool
	ForceRun           bool
	ForcePublishUpload bool
	SkipSigning        bool
}

func (f *CiFlags) Mode() ci.Mode {
	if f.DryRun {
		return ci.ModeDryRun
	}
	if f.Local {
		return ci.ModeLocal
	}
	return ci.ModeCI
}

func logCiPromoteEntry(p ci.PromotionEntry) {
	l := log.Default()
	if p.HasError {
		l.Error(logIdAction, "promote error: {group}/{app} {source} → {target}: {reason}",
			log.String("group", p.Group),
			log.String("app", p.App),
			log.String("source", p.SourceEnv),
			log.String("target", p.TargetEnv),
			log.String("reason", p.SkipReason),
		)
	} else if p.Skipped {
		l.DebugLog(logIdAction, "promote skipped: {group}/{app} {source} → {target}: {reason}",
			log.String("group", p.Group),
			log.String("app", p.App),
			log.String("source", p.SourceEnv),
			log.String("target", p.TargetEnv),
			log.String("reason", p.SkipReason),
		)
	} else if p.OldVersion == p.NewVersion {
		l.Info(logIdAction, "promoted {group}/{app} {source} → {target} (content changed)",
			log.String("group", p.Group),
			log.String("app", p.App),
			log.String("source", p.SourceEnv),
			log.String("target", p.TargetEnv),
		)
	} else {
		l.Info(logIdAction, "promoted {group}/{app} {source} → {target} ({oldVersion} → {newVersion})",
			log.String("group", p.Group),
			log.String("app", p.App),
			log.String("source", p.SourceEnv),
			log.String("target", p.TargetEnv),
			log.String("oldVersion", p.OldVersion),
			log.String("newVersion", p.NewVersion),
		)
	}
}

func CiTest(f CiFlags) error {
	l := log.Default()
	l.Info(logIdAction, "Running CI test pipeline in '{mode}' mode", log.String("mode", string(f.Mode())))
	return ci.RunTest(f.ConfigPath, f.Mode())
}

func CiDownload(f CiFlags) error {
	l := log.Default()
	l.Info(logIdAction, "Running CI download pipeline in '{mode}' mode", log.String("mode", string(f.Mode())))
	return ci.RunDownload(f.ConfigPath, f.Mode())
}

func CiRelease(f CiFlags) error {
	l := log.Default()
	l.Info(logIdAction, "Running CI release pipeline in '{mode}' mode", log.String("mode", string(f.Mode())))
	_, err := ci.RunRelease(f.ConfigPath, f.Mode(), f.TargetBranch)
	return err
}

func CiAuto(f CiFlags) error {
	mode := f.Mode()
	l := log.Default()
	l.Info(logIdAction, "Running CI auto pipeline in '{mode}' mode", log.String("mode", string(mode)))
	return ci.RunAuto(f.ConfigPath, mode, f.TargetBranch, f.PromoteTo, logCiPromoteEntry)
}

func CiPromote(f CiFlags) error {
	mode := f.Mode()
	l := log.Default()
	l.Info(logIdAction, "Running CI promote pipeline in '{mode}' mode", log.String("mode", string(mode)))

	actions := ci.NewPromoteActions(mode, f.TargetBranch)
	_, err := ci.RunPromote(f.ConfigPath, mode, actions, f.TargetBranch, f.PromoteTo, logCiPromoteEntry)
	return err
}

func CiPublish(f CiFlags) error {
	l := log.Default()
	l.Info(logIdAction, "Running CI publish pipeline in '{mode}' mode", log.String("mode", string(f.Mode())))
	return ci.RunPublish(f.ConfigPath, f.Mode(), f.Charts, f.ForceRun, f.ForcePublishUpload, f.SkipSigning)
}

func CiValidate(f CiFlags) error {
	l := log.Default()
	l.Info(logIdAction, "Running CI validate pipeline in '{mode}' mode", log.String("mode", string(f.Mode())))
	return ci.RunValidate(f.ConfigPath, f.Mode(), f.Charts, f.BuildTag, f.ForceRun)
}

func CiSprint(f CiFlags) error {
	l := log.Default()
	l.Info(logIdAction, "Running CI sprint pipeline in '{mode}' mode", log.String("mode", string(f.Mode())))
	return ci.RunSprint(f.Mode())
}

func CiUpgrade(f CiFlags) error {
	l := log.Default()
	l.Info(logIdAction, "Running CI upgrade pipeline in '{mode}' mode", log.String("mode", string(f.Mode())))
	return ci.RunUpgrade(f.ConfigPath, f.Mode(), f.VersionsFile, f.SkipMissing)
}

func CiSync(f CiFlags) error {
	l := log.Default()
	l.Info(logIdAction, "Running CI sync pipeline in '{mode}' mode", log.String("mode", string(f.Mode())))
	return ci.RunSync(f.Mode())
}

func CiUpdate(f CiFlags) error {
	l := log.Default()
	l.Info(logIdAction, "Running CI update pipeline in '{mode}' mode", log.String("mode", string(f.Mode())))
	return ci.RunUpdate(f.Mode())
}

func CiSecretCreate(path string, name string, force bool, signers string) error {
	l := log.Default()
	targetPath, sopsConfigPath, err := ci.CreateSecretsFile(path, name, force, ci.SecretCreateSigners(signers))
	if err != nil {
		return err
	}
	l.Info(logIdAction, "Using SOPS config {path}", log.String("path", sopsConfigPath))
	l.Info(logIdAction, "Created encrypted CI secrets file at {path}", log.String("path", targetPath))
	l.Info(logIdAction, "Updated CI config with public signing metadata at {path}", log.String("path", path))
	return nil
}

func CiSecretShow(path string, out io.Writer) error {
	_, data, err := ci.ShowSecretsFile(path)
	if err != nil {
		return err
	}
	if out == nil {
		out = os.Stdout
	}
	_, err = fmt.Fprint(out, string(data))
	return err
}
