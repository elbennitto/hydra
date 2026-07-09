package cmd

import (
	"io"
	"os"
	"path/filepath"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/ci"
)

// CiCommandParams holds the action functions for all CI pipeline subcommands.
type CiCommandParams struct {
	CiDownload     func(flags action.CiFlags) error
	CiTest         func(flags action.CiFlags) error
	CiRelease      func(flags action.CiFlags) error
	CiPromote      func(flags action.CiFlags) error
	CiAuto         func(flags action.CiFlags) error
	CiPublish      func(flags action.CiFlags) error
	CiValidate     func(flags action.CiFlags) error
	CiSprint       func(flags action.CiFlags) error
	CiUpgrade      func(flags action.CiFlags) error
	CiSync         func(flags action.CiFlags) error
	CiUpdate       func(flags action.CiFlags) error
	CiConfig       func(path string, in io.Reader, out io.Writer, useColor bool) error
	CiSecretCreate func(path string, name string, force bool, signers string) error
	CiSecretShow   func(path string, out io.Writer) error
}

func NewCiCommandParams() CiCommandParams {
	return CiCommandParams{
		CiDownload:     action.CiDownload,
		CiTest:         action.CiTest,
		CiRelease:      action.CiRelease,
		CiPromote:      action.CiPromote,
		CiAuto:         action.CiAuto,
		CiPublish:      action.CiPublish,
		CiValidate:     action.CiValidate,
		CiSprint:       action.CiSprint,
		CiUpgrade:      action.CiUpgrade,
		CiSync:         action.CiSync,
		CiUpdate:       action.CiUpdate,
		CiConfig:       action.CiConfigInit,
		CiSecretCreate: action.CiSecretCreate,
		CiSecretShow:   action.CiSecretShow,
	}
}

// NewCiCommand creates the "hydra ci" parent command with config, secrets, and pipeline runners.
func NewCiCommand(params CiCommandParams) *cobra.Command {
	ciFlags := &action.CiFlags{}

	cmd := &cobra.Command{
		Use:   "ci",
		Short: "CI/CD configuration, secrets, and pipeline commands",
		Long: `Manage Hydra CI configuration and encrypted secrets, and run CI/CD
pipeline stages for the Helm chart repository.

Use "hydra ci run <stage>" for pipeline execution. Pipeline configuration is
read from .hydra-ci.yaml in the repository root.`,
		Example: `  # Create or edit the pipeline config
  hydra ci config .hydra-ci.yaml

  # Create encrypted CI secrets
  hydra ci secrets create --name "Hydra CI <ci@example.com>" .hydra-ci.yaml

  # Run the test pipeline in CI mode
  hydra ci run test .hydra-ci.yaml

  # Simulate the release pipeline
  hydra ci run release --dry-run .hydra-ci.yaml`,
	}

	cmd.PersistentFlags().BoolVar(&ciFlags.DryRun, "dry-run", false,
		"Simulate the pipeline without making any changes")
	cmd.PersistentFlags().BoolVar(&ciFlags.Local, "local", false,
		"Create commits locally but skip push, MR creation, registry upload, and webhook notifications")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "local")

	cmd.PersistentFlags().StringVar(&ciFlags.TargetBranch, "target-branch", "",
		"Create commits on the specified branch instead of auto-generated branches. Branch must exist.")
	cmd.PersistentFlags().StringVar(&ciFlags.PromoteTo, "promote-to", "",
		"For promote and auto: only promote to the specified target environment (e.g. stage, prod). Skips all other environment pairs.")

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run CI/CD pipeline stages",
		Long: `Run CI/CD pipeline steps for the Helm chart repository.

Each subcommand corresponds to a pipeline stage: download, test, release,
promote, publish, verify, sprint, upgrade, sync, update, plus auto to run
multiple stages in order.
By default, commands run in full CI mode (commit, push, MR, upgrade,
webhook).

Use --local to commit changes without pushing, creating MRs, uploading to the
registry, or sending notifications. Use --dry-run to simulate the entire flow
without making any changes.`,
		Example: `  # Download dependencies for all charts
  hydra ci run download .hydra-ci.yaml

  # Run the test pipeline in CI mode
  hydra ci run test .hydra-ci.yaml

  # Simulate the release pipeline
  hydra ci run release --dry-run .hydra-ci.yaml

  # Run promote locally (commit only, no push/MR)
  hydra ci run promote --local .hydra-ci.yaml

  # Run all configured pipeline stages in order (fail-fast)
  hydra ci run auto --dry-run .hydra-ci.yaml`,
	}

	runCmd.AddCommand(newCiSubcommand("download",
		"Download dependencies for all charts",
		"List all charts in configured environments and run\nhelm dependency update for each chart, even if dependency\nartifacts already exist locally.",
		params.CiDownload, ciFlags))

	runCmd.AddCommand(newCiSubcommand("test",
		"Validate all charts (lint, template)",
		"List all charts in configured environments, verify\nthat dependencies are already present locally, and run helm lint\nand helm template on each chart. Fails if a dependency is missing.",
		params.CiTest, ciFlags))

	runCmd.AddCommand(newCiSubcommand("release",
		"Detect changes, update versions, create Git tags",
		"Detect changed chart directories, bump wrapper and root-app versions,\ncommit the changes, and create documentation and build tags.",
		params.CiRelease, ciFlags))

	runCmd.AddCommand(newCiSubcommand("promote",
		"Create promote MRs (dev→stage, stage→prod)",
		"Compare chart directories between environments, create branches\nfollowing the hydra/promote/to-<env>/<group>/<app> convention,\ncopy files, rewrite version suffixes, and open merge requests.",
		params.CiPromote, ciFlags))

	runCmd.AddCommand(newCiSubcommand("auto",
		"Run pipeline stages in order (from .hydra-ci.yaml or ci.autoSteps)",
		"Runs download, test, release, publish, promote, sync, update, sprint,\nand upgrade in the default dependency order unless ci.autoSteps overrides it.\nLogs the current git branch of the charts repository. Stops at the first\nerror (fail-fast).",
		params.CiAuto, ciFlags))

	publishCmd := newCiSubcommand("publish",
		"Build and upload charts to the OCI registry",
		"Parse the build tag from the release pipeline, package each chart\nwith helm package, and push to the registry configured in .hydra-ci.yaml.",
		params.CiPublish, ciFlags)
	publishCmd.Flags().StringArrayVar(&ciFlags.Charts, "chart", nil,
		"Chart to publish. Repeatable. Accepts either <group>/<app>/<env> or a repo-relative chart path such as apps/<group>/<app>/<env>.")
	publishCmd.Flags().BoolVar(&ciFlags.ForceRun, "force-run", false,
		"Publish explicitly selected charts even when HEAD is not at their release commit")
	publishCmd.Flags().BoolVar(&ciFlags.ForcePublishUpload, "force-publish-upload", false,
		"Upload chart versions even when the same version already exists in the OCI registry")
	publishCmd.Flags().BoolVar(&ciFlags.SkipSigning, "skip-signing", false,
		"Package and publish charts without provenance signing")
	runCmd.AddCommand(publishCmd)

	verifyCmd := newCiSubcommand("verify",
		"Validate published chart signatures in the OCI registry",
		"Resolve charts like the publish pipeline, download the remote chart and\nsignature artifacts from the OCI registry, and verify every configured\nsigning mechanism (Helm provenance and/or Cosign).",
		params.CiValidate, ciFlags)
	verifyCmd.Flags().StringArrayVar(&ciFlags.Charts, "chart", nil,
		"Chart to validate. Repeatable. Accepts either <group>/<app>/<env> or a repo-relative chart path such as apps/<group>/<app>/<env>.")
	verifyCmd.Flags().StringVar(&ciFlags.BuildTag, "build-tag", "",
		"Resolve charts from the specified build-* tag instead of HEAD")
	verifyCmd.Flags().BoolVar(&ciFlags.ForceRun, "force-run", false,
		"Resolve charts even when HEAD is not at the expected release or build commit")
	runCmd.AddCommand(verifyCmd)
	validateCmd := newCiSubcommand("validate",
		"Deprecated alias for verify",
		"Deprecated alias for `hydra ci run verify`.",
		params.CiValidate, ciFlags)
	validateCmd.Hidden = true
	validateCmd.Flags().StringArrayVar(&ciFlags.Charts, "chart", nil,
		"Chart to validate. Repeatable. Accepts either <group>/<app>/<env> or a repo-relative chart path such as apps/<group>/<app>/<env>.")
	validateCmd.Flags().StringVar(&ciFlags.BuildTag, "build-tag", "",
		"Resolve charts from the specified build-* tag instead of HEAD")
	validateCmd.Flags().BoolVar(&ciFlags.ForceRun, "force-run", false,
		"Resolve charts even when HEAD is not at the expected release or build commit")
	runCmd.AddCommand(validateCmd)

	runCmd.AddCommand(newCiSubcommand("sprint",
		"Bump major version of all root apps at sprint start",
		"Check if a sprint boundary has been crossed. If so, bump the major\nversion of all root apps for dev, stage, and prod environments.",
		params.CiSprint, ciFlags))

	upgradeCmd := newCiSubcommand("upgrade",
		"Update service dependency versions from a versions file",
		"Read desired service versions from a YAML file and update the matching\nChart.yaml dependency in each apps/<rootApp>/<app>/<env> chart.\nUse --dry-run to validate and log planned dependency changes without writing files.",
		params.CiUpgrade, ciFlags)
	upgradeCmd.Flags().StringVar(&ciFlags.VersionsFile, "versions-file", "",
		"YAML file containing versions entries to apply during upgrade; reads stdin when omitted")
	upgradeCmd.Flags().BoolVar(&ciFlags.SkipMissing, "skip-missing", false,
		"Warn and continue when an upgrade entry has no matching chart dependency")
	runCmd.AddCommand(upgradeCmd)

	runCmd.AddCommand(newCiSubcommand("sync",
		"Copy cluster configurations into the repository",
		"Synchronize cluster configurations from external cluster repositories\ninto the current repository and trigger the update pipeline.",
		params.CiSync, ciFlags))

	runCmd.AddCommand(newCiSubcommand("update",
		"Refresh unit test data by rendering all clusters",
		"Render all charts against all cluster definitions, commit any\nresulting changes as unit test data, and enforce the one-auto-commit\ngovernance rule.",
		params.CiUpdate, ciFlags))

	cmd.AddCommand(runCmd)

	noColor := false
	configCmd := &cobra.Command{
		Use:   "config <path>",
		Short: "Create or edit .hydra-ci.yaml interactively",
		Long:  "Interactively create or edit a .hydra-ci.yaml configuration file.\nGuides through every field with sensible defaults and filesystem-based\nauto-detection of app groups and root apps.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			useColor := !noColor && isatty.IsTerminal(os.Stdout.Fd())
			return params.CiConfig(args[0], os.Stdin, os.Stdout, useColor)
		},
	}
	configCmd.Flags().BoolVar(&noColor, "no-color", false,
		"Disable colored output")
	cmd.AddCommand(configCmd)

	secretCmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage encrypted CI secret files",
		Long:  "Create and display the SOPS-encrypted CI secret file configured by ci.secretsPath or the default sibling .hydra-ci-secrets.sops.yaml. Public signing metadata is written into ci.sign in .hydra-ci.yaml and must stay identical to the private signing key in the secrets file.",
	}
	secretCreateForce := false
	secretCreateName := ""
	secretCreateSigners := string(ci.SecretCreateSignersBoth)
	secretCreateCmd := &cobra.Command{
		Use:   "create <config-path>",
		Short: "Create the encrypted CI secrets file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				path = filepath.Join(path, ci.ConfigFileName)
			}
			return params.CiSecretCreate(path, secretCreateName, secretCreateForce, secretCreateSigners)
		},
	}
	secretCreateCmd.Flags().BoolVar(&secretCreateForce, "force", false,
		"Overwrite the existing encrypted CI secrets file if it already exists")
	secretCreateCmd.Flags().StringVar(&secretCreateName, "name", "",
		"User ID to embed into the generated signing key")
	secretCreateCmd.Flags().StringVar(&secretCreateSigners, "signers", string(ci.SecretCreateSignersBoth),
		"Which signing material to generate: both, helm, or cosign")
	_ = secretCreateCmd.MarkFlagRequired("name")
	secretCmd.AddCommand(secretCreateCmd)
	secretCmd.AddCommand(&cobra.Command{
		Use:   "show <config-path>",
		Short: "Decrypt and print the CI secrets file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				path = filepath.Join(path, ci.ConfigFileName)
			}
			return params.CiSecretShow(path, os.Stdout)
		},
	})
	cmd.AddCommand(secretCmd)

	return cmd
}

func newCiSubcommand(
	name string,
	short string,
	long string,
	actionFn func(action.CiFlags) error,
	ciFlags *action.CiFlags,
) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <config-path>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				path = filepath.Join(path, ci.ConfigFileName)
			}
			ciFlags.ConfigPath = path
			return actionFn(*ciFlags)
		},
	}
}
