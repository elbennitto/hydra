package cmd

import (
	"fmt"
	"os"
	"time"

	"hydra-gitops.org/hydra/hydra-go/base/log"

	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/base/errors"
	"hydra-gitops.org/hydra/hydra-go/cli/flags"
	hc "hydra-gitops.org/hydra/hydra-go/cli/util"
	"hydra-gitops.org/hydra/hydra-go/core/helm"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

func DefineFlags(cmd *cobra.Command, f any) error {
	if err := defineColorFlag(cmd, f); err != nil {
		return err
	}
	if err := defineDryRunFlag(cmd, f); err != nil {
		return err
	}
	if err := defineContextFlag(cmd, f); err != nil {
		return err
	}
	if err := defineAppIdFlag(cmd, f); err != nil {
		return err
	}
	if err := defineClusterFlag(cmd, f); err != nil {
		return err
	}
	if err := defineNetworkModeFlag(cmd, f); err != nil {
		return err
	}
	if err := defineCrdsFlag(cmd, f); err != nil {
		return err
	}
	if err := definePredicateFlags(cmd, f); err != nil {
		return err
	}
	if err := definePickFlag(cmd, f); err != nil {
		return err
	}
	if err := defineUniqFlag(cmd, f); err != nil {
		return err
	}
	if err := defineDiffModeFlag(cmd, f); err != nil {
		return err
	}
	if err := defineDiffUnifiedContextFlags(cmd, f); err != nil {
		return err
	}
	if err := defineForceUninstallFlag(cmd, f); err != nil {
		return err
	}
	if err := defineForceScaleDownFlag(cmd, f); err != nil {
		return err
	}
	if err := defineBootstrapFlag(cmd, f); err != nil {
		return err
	}
	if err := defineSkipBootstrapGuardFlag(cmd, f); err != nil {
		return err
	}
	if err := defineSkipRefChecksFlag(cmd, f); err != nil {
		return err
	}
	if err := defineClusterApplyBehaviorFlags(cmd, f); err != nil {
		return err
	}
	if err := defineClusterApplyBootstrapNoFlags(cmd, f); err != nil {
		return err
	}
	if err := defineScaleTimeoutFlag(cmd, f); err != nil {
		return err
	}
	if err := defineCrdTimeoutFlag(cmd, f); err != nil {
		return err
	}
	if err := defineExcludeAppFlag(cmd, f); err != nil {
		return err
	}
	if err := defineForceBackupRestoreFlag(cmd, f); err != nil {
		return err
	}
	if err := defineSkipBackupRestoreFlag(cmd, f); err != nil {
		return err
	}
	if err := defineBackupRestoreCreateNamespacesFlag(cmd, f); err != nil {
		return err
	}
	if err := defineSkipBackupFlag(cmd, f); err != nil {
		return err
	}
	if err := defineReplaceFlag(cmd, f); err != nil {
		return err
	}
	if err := defineNoCacheFlag(cmd, f); err != nil {
		return err
	}
	if err := defineReviewRefsYamlFlag(cmd, f); err != nil {
		return err
	}
	if err := defineClusterListSkipOwnerRefsFlag(cmd, f); err != nil {
		return err
	}
	if err := defineClusterListParallelFlag(cmd, f); err != nil {
		return err
	}
	return nil
}

func defineColorFlag(cmd *cobra.Command, f any) error {
	var colorFlag *flags.ColorFlag
	if c, ok := f.(flags.WithColorFlag); ok {
		colorFlag = c.WithColorFlag()
	}

	if colorFlag != nil {
		autoDetect := true
		colorFlag.Color = types.ColorNo

		enumTypeValue, err := hc.NewEnumFlagValue(types.ColorEnumType)
		if err != nil {
			return err
		}

		hc.NewFlagBuilder(cmd, enumTypeValue).
			Name("color-mode").
			Usage("Set color mode for output").
			Validate(func(value types.ColorEnum) error {
				switch value {
				case types.ColorEnumAuto:
					// keep default settings
				case types.ColorEnumAlways:
					colorFlag.Color = types.ColorYes
					autoDetect = false
				case types.ColorEnumNever:
					colorFlag.Color = types.ColorNo
					autoDetect = false
				default:
					return fmt.Errorf("invalid value for --color: %s, allowed values are: %v", value, enumTypeValue.StringValues())
				}
				return nil
			}).
			Build()

		hc.NewBoolFlagBuilder(cmd, true).
			Name("color").
			Short("c").
			Usage("Force colored output (default: auto-detect from terminal)").
			Validate(func(color bool) error {
				colorFlag.Color = types.Color(color)
				autoDetect = false
				return nil
			}).
			Build()

		hc.NewBoolFlagBuilder(cmd, true).
			Name("no-color").
			Usage("Disable colored output even when running in a terminal").
			Validate(func(noColor bool) error {
				colorFlag.Color = types.Color(!noColor)
				autoDetect = false
				return nil
			}).
			Build()

		cmd.MarkFlagsMutuallyExclusive("color", "no-color", "color-mode")

		hc.AddPreRun(cmd, func(cmd *cobra.Command, args []string) {
			if autoDetect {
				if forced, ok := colorForcedByEnv(); ok {
					colorFlag.Color = types.Color(forced)
					return
				}
				tty := stdoutIsTerminalForHydra()
				colorFlag.Color = types.Color(tty)
				log.Default().DebugLog(logIdCmd, "auto detected color mode {mode}",
					log.Bool("mode", bool(colorFlag.Color)))
			}
		})
	}

	return nil
}

func defineDryRunFlag(cmd *cobra.Command, f any) error {
	var dryRunFlag *flags.DryRunFlag
	if c, ok := f.(flags.WithDryRunFlag); ok {
		dryRunFlag = c.WithDryRunFlag()
	}

	var noClusterFlag *flags.NoClusterFlag
	if c, ok := f.(flags.WithNoClusterFlag); ok {
		noClusterFlag = c.WithNoClusterFlag()
	}

	if dryRunFlag != nil {
		dryRunFlag.DryRun = false

		hc.NewBoolFlagBuilder(cmd, true).
			Name("dry-run").
			Short("d").
			Usage("Simulate the operation without making any changes").
			Validate(func(dryRun bool) error {
				dryRunFlag.DryRun = types.DryRun(dryRun)
				return nil
			}).
			Build()

		hc.AddPreRun(cmd, func(cmd *cobra.Command, args []string) {
			if dryRunFlag.DryRun == types.DryRunYes {
				l := log.Default()
				l.Warn(logIdCmd, "enabled dry run mode, no changes will be applied")
			}
		})

		if noClusterFlag != nil {
			noClusterFlag.NoCluster = false

			hc.NewBoolFlagBuilder(cmd, true).
				Name("no-cluster").
				Usage("Skip cluster connection and Kubernetes context validation").
				Validate(func(noCluster bool) error {
					noClusterFlag.NoCluster = noCluster
					return nil
				}).
				Build()

			hc.AddPreRun(cmd, func(cmd *cobra.Command, args []string) {
				if noClusterFlag.NoCluster {
					l := log.Default()
					l.Warn(logIdCmd, "no-cluster mode enabled, skipping cluster connection and context validation")
				}
			})

			cmd.MarkFlagsMutuallyExclusive("dry-run", "no-cluster")
		}
	}

	return nil
}

func defineContextFlag(cmd *cobra.Command, f any) error {
	var contextFlag *flags.ContextFlag
	if c, ok := f.(flags.WithContextFlag); ok {
		contextFlag = c.WithContextFlag()
	}

	if contextFlag != nil {
		hc.NewStringFlagBuilder(cmd, "").
			Name("hydra-context").
			Usage("Path to the Hydra context (or set " + types.HydraContextEnvName + " environment variable)").
			PreRun(func(value string) error {
				// If hydra-context flag is not provided, try to get it from environment
				if value == "" {
					value = os.Getenv(types.HydraContextEnvName)
				}

				// If still empty, return error
				if value == "" {
					return log.CreateError(errors.ErrMissingHydraContext, "--hydra-context flag or {ENV} environment variable is required",
						log.String("ENV", types.HydraContextEnvName))
				}

				contextFlag.HydraContext = types.HydraContext(value)

				return nil
			}).
			Build()
	}

	return nil
}

func defineAppIdFlag(cmd *cobra.Command, f any) error {
	var appIdFlag *flags.AppIdFlag
	if c, ok := f.(flags.WithAppIdFlag); ok {
		appIdFlag = c.WithAppIdFlag()
	}

	if appIdFlag != nil {
		hc.AddPersistentPreRunE(cmd, func(cmd *cobra.Command, args []string) error {
			appId, err := types.NewAppId(args[0])
			if err != nil {
				return err
			}
			appIdFlag.AppId = appId
			return nil
		})
	}

	return nil
}

func defineClusterFlag(cmd *cobra.Command, f any) error {
	var clusterFlag *flags.ClusterFlag
	if c, ok := f.(flags.WithClusterFlag); ok {
		clusterFlag = c.WithClusterFlag()
	}

	if clusterFlag != nil {
		hc.AddPersistentPreRunE(cmd, func(cmd *cobra.Command, args []string) error {
			cluster, err := types.NewClusterName(args[0])
			if err != nil {
				return err
			}
			clusterFlag.Cluster = cluster
			return nil
		})
	}

	return nil
}

func defineNetworkModeFlag(cmd *cobra.Command, f any) error {
	var helmNetworkModeFlag *flags.HelmNetworkModeFlag
	if n, ok := f.(flags.WithHelmNetworkModeFlag); ok {
		helmNetworkModeFlag = n.WithHelmNetworkModeFlag()
	}

	if helmNetworkModeFlag != nil {
		helmNetworkModeFlag.HelmNetworkMode = types.HelmNetworkModeOnline

		enumTypeValue, err := hc.NewEnumFlagValue(types.HelmNetworkModeEnumType)
		if err != nil {
			return err
		}

		hc.NewFlagBuilder(cmd, enumTypeValue).
			Name("helm-network-mode").
			Usage("Control how missing Helm charts are resolved: online=download from remote, local=use only local charts, offline=skip missing charts, error=fail if a chart is missing").
			Validate(func(helmNetworkMode types.HelmNetworkMode) error {
				helmNetworkModeFlag.HelmNetworkMode = helmNetworkMode
				return nil
			}).
			Build()
	}

	return nil
}

func defineCrdsFlag(cmd *cobra.Command, f any) error {
	var skipUnknownCrdsFlag *flags.CrdModeFlag
	if n, ok := f.(flags.WithCrdModeFlag); ok {
		skipUnknownCrdsFlag = n.WithCrdModeFlag()
	}

	if skipUnknownCrdsFlag != nil {
		skipUnknownCrdsFlag.CrdMode = types.CrdModeError

		enumTypeValue, err := hc.NewEnumFlagValue(types.CrdModeEnumType)
		if err != nil {
			return err
		}

		hc.NewFlagBuilder(cmd, enumTypeValue).
			Name("crd-mode").
			Usage("Define how missing CRDs are handled: error=abort if CRD is missing, ignore=skip Custom Resources with unknown CRDs").
			Validate(func(crdMode types.CrdMode) error {
				skipUnknownCrdsFlag.CrdMode = crdMode
				return nil
			}).
			Build()
	}

	return nil
}

func defineDiffModeFlag(cmd *cobra.Command, f any) error {
	var diffModeFlag *flags.DiffModeFlag
	if c, ok := f.(flags.WithDiffModeFlag); ok {
		diffModeFlag = c.WithDiffModeFlag()
	}

	if diffModeFlag != nil {
		diffModeFlag.DiffMode = types.DiffModeServer

		enumTypeValue, err := hc.NewEnumFlagValue(types.DiffModeEnumType)
		if err != nil {
			return err
		}

		hc.NewFlagBuilder(cmd, enumTypeValue).
			Name("diff-mode").
			Usage("Diff strategy: 'server' sends templates through server-side dry-run to include defaults, 'raw' compares rendered YAML 1:1 against cluster state").
			Validate(func(diffMode types.DiffMode) error {
				diffModeFlag.DiffMode = diffMode
				return nil
			}).
			Build()
	}

	return nil
}

func defineDiffUnifiedContextFlags(cmd *cobra.Command, f any) error {
	var ctx *flags.DiffUnifiedContextFlag
	if c, ok := f.(flags.WithDiffUnifiedContextFlag); ok {
		ctx = c.WithDiffUnifiedContextFlag()
	}
	if ctx == nil {
		return nil
	}

	ctx.Before = -1
	ctx.After = -1
	ctx.Both = -1

	cmd.Flags().IntVarP(&ctx.Before, "before-context", "B", -1,
		"number of unchanged lines to show before each change in unified diff hunks (grep-style; default: 3)")
	cmd.Flags().IntVarP(&ctx.After, "after-context", "A", -1,
		"number of unchanged lines to show after each change in unified diff hunks (grep-style; default: 3)")
	cmd.Flags().IntVarP(&ctx.Both, "context", "C", -1,
		"number of unchanged lines to show before and after each change (grep -C; default: 3)")

	hc.AddPreRunE(cmd, func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("before-context") && ctx.Before < 0 {
			return fmt.Errorf("--before-context (-B) must be >= 0")
		}
		if cmd.Flags().Changed("after-context") && ctx.After < 0 {
			return fmt.Errorf("--after-context (-A) must be >= 0")
		}
		if cmd.Flags().Changed("context") && ctx.Both < 0 {
			return fmt.Errorf("--context (-C) must be >= 0")
		}
		return nil
	})

	return nil
}

func defineForceUninstallFlag(cmd *cobra.Command, f any) error {
	var forceUninstallFlag *flags.ForceUninstallFlag
	if c, ok := f.(flags.WithForceUninstallFlag); ok {
		forceUninstallFlag = c.WithForceUninstallFlag()
	}

	if forceUninstallFlag != nil {
		forceUninstallFlag.ForceUninstall = types.ForceUninstallNone

		hc.NewBoolFlagBuilder(cmd, true).
			Name("force").
			Usage("Force-delete resources matched by ref-parsers with tag: uninstall-force").
			Validate(func(force bool) error {
				forceUninstallFlag.ForceUninstall = types.ForceUninstallForce
				return nil
			}).
			Build()

		hc.NewBoolFlagBuilder(cmd, true).
			Name("keep").
			Usage("Keep resources matched by ref-parsers with tag: uninstall-force and proceed with uninstallation").
			Validate(func(keep bool) error {
				forceUninstallFlag.ForceUninstall = types.ForceUninstallKeep
				return nil
			}).
			Build()

		hc.NewBoolFlagBuilder(cmd, true).
			Name("force-all").
			Usage("Force-delete all leftover resources including untracked ones not matched by any predicate").
			Validate(func(forceAll bool) error {
				forceUninstallFlag.ForceUninstall = types.ForceUninstallForceAll
				return nil
			}).
			Build()

		cmd.MarkFlagsMutuallyExclusive("force", "keep", "force-all")
	}

	return nil
}

func defineForceScaleDownFlag(cmd *cobra.Command, f any) error {
	var forceScaleDownFlag *flags.ForceScaleDownFlag
	if c, ok := f.(flags.WithForceScaleDownFlag); ok {
		forceScaleDownFlag = c.WithForceScaleDownFlag()
	}

	if forceScaleDownFlag != nil {
		forceScaleDownFlag.ForceScaleDown = types.ForceScaleDownNo

		hc.NewBoolFlagBuilder(cmd, true).
			Name("force-scale-down").
			Usage("Force-delete pods stuck until scale-down timeout; on cluster scale down also force-delete app-associated Pods (including Terminating)").
			Validate(func(force bool) error {
				forceScaleDownFlag.ForceScaleDown = types.ForceScaleDownYes
				return nil
			}).
			Build()
	}

	return nil
}

func definePredicateFlags(cmd *cobra.Command, f any) error {
	var predicatesFlags *flags.PredicatesFlag
	if n, ok := f.(flags.WithPredicatesFlag); ok {
		predicatesFlags = n.WithPredicatesFlag()
	}

	if predicatesFlags != nil {
		predicatesFlags.Predicates = nil

		excludes := []string{}
		cmd.Flags().StringArrayVarP(&excludes, "exclude", "e", nil, "CEL expression to exclude resources, e.g. 'entity.kind==\"Secret\"' (repeatable)")
		hc.AddPreRunE(cmd, func(cmd *cobra.Command, args []string) error {
			for _, exclude := range excludes {
				exclude = fmt.Sprintf("!(%s)", exclude)
				predicatesFlags.Predicates = append(predicatesFlags.Predicates, types.CelPredicate(exclude))
			}
			return nil
		})

		includes := []string{}
		cmd.Flags().StringArrayVarP(&includes, "include", "i", nil, "CEL expression to filter resources, e.g. 'entity.namespace==\"default\"' (repeatable)")
		hc.AddPreRunE(cmd, func(cmd *cobra.Command, args []string) error {
			for _, include := range includes {
				predicatesFlags.Predicates = append(predicatesFlags.Predicates, types.CelPredicate(include))
			}
			return nil
		})
	}

	return nil
}

func defineIncludePathFlag(cmd *cobra.Command, f any) error {
	var includePathFlag *flags.IncludePathFlag
	if n, ok := f.(flags.WithIncludePathFlag); ok {
		includePathFlag = n.WithIncludePathFlag()
	}
	if includePathFlag == nil {
		return nil
	}
	includePathFlag.IncludePathPrefixes = nil

	var paths []string
	cmd.Flags().StringArrayVar(&paths, "include-path", nil,
		"Only print chart template files whose Helm template path matches this prefix at a path boundary, or contains the same slash-separated path segment (repeatable, OR). Example: charts/kube-prometheus-stack/templates/prometheus (also matches .../charts/kube-prometheus-stack/templates/... when Helm adds a parent prefix)")
	hc.AddPreRunE(cmd, func(cmd *cobra.Command, args []string) error {
		for _, p := range paths {
			n := helm.NormalizeTemplateSourcePathPrefix(p)
			if n != "" {
				includePathFlag.IncludePathPrefixes = append(includePathFlag.IncludePathPrefixes, n)
			}
		}
		return nil
	})
	return nil
}

func definePickFlag(cmd *cobra.Command, f any) error {
	var pickFlag *flags.PickFlag
	if p, ok := f.(flags.WithPickFlag); ok {
		pickFlag = p.WithPickFlag()
	}

	if pickFlag != nil {
		pickFlag.Pick = ""

		var pick string
		cmd.Flags().StringVar(&pick, "pick", "", "CEL expression to project matched resources into a YAML array")
		hc.AddPreRunE(cmd, func(cmd *cobra.Command, args []string) error {
			pickFlag.Pick = types.CelExpression(pick)
			return nil
		})
	}

	return nil
}

func defineUniqFlag(cmd *cobra.Command, f any) error {
	var uniqFlag *flags.UniqFlag
	if u, ok := f.(flags.WithUniqFlag); ok {
		uniqFlag = u.WithUniqFlag()
	}

	if uniqFlag != nil {
		uniqFlag.Uniq = false
		cmd.Flags().BoolVar(&uniqFlag.Uniq, "uniq", false, "Deduplicate projected values after --pick evaluation")
	}

	return nil
}

func defineBootstrapFlag(cmd *cobra.Command, f any) error {
	var bootstrapFlag *flags.BootstrapFlag
	if b, ok := f.(flags.WithBootstrapFlag); ok {
		bootstrapFlag = b.WithBootstrapFlag()
	}

	if bootstrapFlag != nil {
		bootstrapFlag.Bootstrap = types.BootstrapNo

		hc.NewBoolFlagBuilder(cmd, true).
			Name("bootstrap").
			Usage("Shorthand that enables all optional apply behaviors (SOPS decode, down-scaled apply, scale-up, orphan cleanup, default sync policy unless overridden, bootstrap guard, bootstrap clones, backup restore, webhook disable) unless opted out with matching --no-* flags; mutually exclusive with --skip-bootstrap-guard").
			Validate(func(bootstrap bool) error {
				bootstrapFlag.Bootstrap = types.Bootstrap(bootstrap)
				return nil
			}).
			Build()

		hc.AddPreRun(cmd, func(cmd *cobra.Command, args []string) {
			if bootstrapFlag.Bootstrap == types.BootstrapYes {
				l := log.Default()
				l.Warn(logIdCmd, "bootstrap mode enabled: optional apply behaviors implied unless opted out with --no-* flags")
			}
		})
	}

	return nil
}

func defineSkipBootstrapGuardFlag(cmd *cobra.Command, f any) error {
	var skipFlag *flags.SkipBootstrapGuardFlag
	if s, ok := f.(flags.WithSkipBootstrapGuardFlag); ok {
		skipFlag = s.WithSkipBootstrapGuardFlag()
	}

	if skipFlag != nil {
		skipFlag.SkipBootstrapGuard = false

		hc.NewBoolFlagBuilder(cmd, true).
			Name("skip-bootstrap-guard").
			Usage("Skip bootstrap-guard validation for resources tagged bootstrap-guard in global.hydra.refs (use when the sops-secrets-operator is already running). Mutually exclusive with --bootstrap").
			Validate(func(skip bool) error {
				skipFlag.SkipBootstrapGuard = skip
				return nil
			}).
			Build()
	}

	return nil
}

func defineSkipRefChecksFlag(cmd *cobra.Command, f any) error {
	var skipFlag *flags.SkipRefChecksFlag
	if s, ok := f.(flags.WithSkipRefChecksFlag); ok {
		skipFlag = s.WithSkipRefChecksFlag()
	}

	if skipFlag != nil {
		skipFlag.SkipRefChecks = false

		hc.NewBoolFlagBuilder(cmd, true).
			Name("skip-ref-checks").
			Usage("Skip post-plan reference validation (references that would remain unresolved after the planned changes). Mutually exclusive with --bootstrap").
			Validate(func(skip bool) error {
				skipFlag.SkipRefChecks = skip
				return nil
			}).
			Build()
	}

	return nil
}

func defineClusterApplyBehaviorFlags(cmd *cobra.Command, f any) error {
	var b *flags.ClusterApplyBehaviorFlags
	if c, ok := f.(flags.WithClusterApplyBehaviorFlags); ok {
		b = c.WithClusterApplyBehaviorFlags()
	}
	if b == nil {
		return nil
	}

	hc.NewBoolFlagBuilder(cmd, true).
		Name("sops-decode").
		Usage("Decrypt SopsSecret CRs and materialize plain Kubernetes Secrets before apply (also enables bootstrap-style backup SopsSecret filtering when combined with --backup-restore)").
		Validate(func(v bool) error {
			b.SopsDecode = v
			return nil
		}).
		Build()

	hc.NewBoolFlagBuilder(cmd, true).
		Name("down-scaled").
		Usage("Apply main workload resources at scale zero (replicas suspended / zero); use with --scale-up for dependency-ordered startup").
		Validate(func(v bool) error {
			b.DownScaled = v
			return nil
		}).
		Build()

	hc.NewBoolFlagBuilder(cmd, true).
		Name("scale-up").
		Usage("After the down-scaled apply phase, scale workloads up in dependency order (requires --down-scaled)").
		Validate(func(v bool) error {
			b.ScaleUp = v
			return nil
		}).
		Build()

	hc.NewBoolFlagBuilder(cmd, true).
		Name("orphan-scale-down").
		Usage("Scale down orphaned workloads and delete resources no longer in templates").
		Validate(func(v bool) error {
			b.OrphanScaleDown = v
			return nil
		}).
		Build()

	hc.NewStringFlagBuilder(cmd, "").
		Name("sync").
		Usage(`ArgoCD AppProject sync policy: default|manual|auto|prevent|keep-or-manual|keep-or-auto|keep-or-prevent|keep-or-default (default "default"; with --bootstrap and without this flag: keep-or-prevent; see manual)`).
		Validate(func(v string) error {
			b.SyncWindow = v
			return nil
		}).
		Build()

	hc.NewBoolFlagBuilder(cmd, true).
		Name("bootstrap-guard").
		Usage("Enforce bootstrap-guard ref rules: fail when guarded resources are present unless using --bootstrap or --skip-bootstrap-guard").
		Validate(func(v bool) error {
			b.BootstrapGuard = v
			return nil
		}).
		Build()

	hc.NewBoolFlagBuilder(cmd, true).
		Name("bootstrap-clones").
		Usage("Materialize global.hydra.clones rules tagged bootstrap (tagless clone rules always apply)").
		Validate(func(v bool) error {
			b.BootstrapClones = v
			return nil
		}).
		Build()

	hc.NewBoolFlagBuilder(cmd, true).
		Name("backup-restore").
		Usage("Run the integrated backup restore phase for secrets discovered from the selected apps").
		Validate(func(v bool) error {
			b.BackupRestore = v
			return nil
		}).
		Build()

	hc.NewBoolFlagBuilder(cmd, true).
		Name("disable-webhooks").
		Usage("Apply admission webhook configurations early with failurePolicy=Ignore, then enable them later in provider dependency order (off by default; implied by --bootstrap)").
		Validate(func(v bool) error {
			b.DisableWebhooks = v
			return nil
		}).
		Build()

	cmd.Flags().IntVar(&b.Parallel, "parallel", 0,
		"Parallel workers for cluster discovery listing and SSA dry-run during apply planning (footer shows one status line per worker when the effective count is >1; 0 = GOMAXPROCS, capped at 64)")

	return nil
}

func defineClusterApplyBootstrapNoFlags(cmd *cobra.Command, f any) error {
	var n *flags.ClusterApplyBootstrapNoFlags
	if c, ok := f.(flags.WithClusterApplyBootstrapNoFlags); ok {
		n = c.WithClusterApplyBootstrapNoFlags()
	}
	if n == nil {
		return nil
	}

	add := func(name, usage string, dst *bool) {
		hc.NewBoolFlagBuilder(cmd, false).
			Name(name).
			Usage(usage).
			Validate(func(v bool) error {
				*dst = v
				return nil
			}).
			Build()
	}

	add("no-sops-decode", "With --bootstrap, skip implied SOPS decode (requires --bootstrap)", &n.NoSopsDecode)
	add("no-down-scaled", "With --bootstrap, skip implied down-scaled apply (requires --bootstrap)", &n.NoDownScaled)
	add("no-scale-up", "With --bootstrap, skip implied scale-up phase (requires --bootstrap)", &n.NoScaleUp)
	add("no-orphan-scale-down", "With --bootstrap, skip implied orphan scale-down (requires --bootstrap)", &n.NoOrphanScaleDown)
	add("no-bootstrap-guard", "With --bootstrap, skip implied bootstrap-guard enforcement (requires --bootstrap)", &n.NoBootstrapGuard)
	add("no-bootstrap-clones", "With --bootstrap, skip implied bootstrap-tagged clone materialization (requires --bootstrap)", &n.NoBootstrapClones)
	add("no-backup-restore", "With --bootstrap, skip implied integrated backup restore (requires --bootstrap; mutually exclusive with --backup-restore and --skip-backup-restore)", &n.NoBackupRestore)
	add("no-disable-webhooks", "With --bootstrap, skip implied non-ready webhook disable phase (requires --bootstrap)", &n.NoDisableWebhooks)

	return nil
}

func defineScaleTimeoutFlag(cmd *cobra.Command, f any) error {
	var scaleTimeoutFlag *flags.ScaleTimeoutFlag
	if c, ok := f.(flags.WithScaleTimeoutFlag); ok {
		scaleTimeoutFlag = c.WithScaleTimeoutFlag()
	}

	if scaleTimeoutFlag != nil {
		scaleTimeoutFlag.ScaleTimeout = 10 * time.Minute

		hc.NewStringFlagBuilder(cmd, "10m").
			Name("scale-timeout").
			Usage("Timeout for workload readiness polling during scale-up and scale-down (Go duration string, e.g. 5m, 10m, 1h)").
			Validate(func(value string) error {
				d, err := time.ParseDuration(value)
				if err != nil {
					return fmt.Errorf("invalid duration for --scale-timeout: %w", err)
				}
				scaleTimeoutFlag.ScaleTimeout = d
				return nil
			}).
			Build()
	}

	return nil
}

func defineCrdTimeoutFlag(cmd *cobra.Command, f any) error {
	var crdTimeoutFlag *flags.CrdTimeoutFlag
	if c, ok := f.(flags.WithCrdTimeoutFlag); ok {
		crdTimeoutFlag = c.WithCrdTimeoutFlag()
	}

	if crdTimeoutFlag != nil {
		crdTimeoutFlag.CrdTimeout = 60 * time.Second

		hc.NewStringFlagBuilder(cmd, "60s").
			Name("crd-timeout").
			Usage("Timeout for CRD establishment polling (Go duration string, e.g. 30s, 60s, 2m)").
			Validate(func(value string) error {
				d, err := time.ParseDuration(value)
				if err != nil {
					return fmt.Errorf("invalid duration for --crd-timeout: %w", err)
				}
				crdTimeoutFlag.CrdTimeout = d
				return nil
			}).
			Build()
	}

	return nil
}

func defineForceBackupRestoreFlag(cmd *cobra.Command, f any) error {
	var forceBackupRestoreFlag *flags.ForceBackupRestoreFlag
	if c, ok := f.(flags.WithForceBackupRestoreFlag); ok {
		forceBackupRestoreFlag = c.WithForceBackupRestoreFlag()
	}

	if forceBackupRestoreFlag != nil {
		forceBackupRestoreFlag.ForceBackupRestore = false

		hc.NewBoolFlagBuilder(cmd, true).
			Name("force-backup-restore").
			Usage("Force-restore backup secrets even when they would overwrite existing cluster secrets with different values").
			Validate(func(force bool) error {
				forceBackupRestoreFlag.ForceBackupRestore = force
				return nil
			}).
			Build()
	}

	return nil
}

func defineSkipBackupRestoreFlag(cmd *cobra.Command, f any) error {
	var skipBackupRestoreFlag *flags.SkipBackupRestoreFlag
	if c, ok := f.(flags.WithSkipBackupRestoreFlag); ok {
		skipBackupRestoreFlag = c.WithSkipBackupRestoreFlag()
	}

	if skipBackupRestoreFlag != nil {
		skipBackupRestoreFlag.SkipBackupRestore = false

		hc.NewBoolFlagBuilder(cmd, true).
			Name("skip-backup-restore").
			Usage("Skip the backup restore phase inside hydra gitops apply").
			Validate(func(skip bool) error {
				skipBackupRestoreFlag.SkipBackupRestore = skip
				return nil
			}).
			Build()
	}

	return nil
}

func defineBackupRestoreCreateNamespacesFlag(cmd *cobra.Command, f any) error {
	var createNamespacesFlag *flags.BackupRestoreCreateNamespacesFlag
	if c, ok := f.(flags.WithBackupRestoreCreateNamespacesFlag); ok {
		createNamespacesFlag = c.WithBackupRestoreCreateNamespacesFlag()
	}

	if createNamespacesFlag != nil {
		createNamespacesFlag.CreateNamespaces = false

		hc.NewBoolFlagBuilder(cmd, true).
			Name("create-namespaces").
			Usage("Create missing target namespaces for the selected backup secrets before restoring them").
			Validate(func(create bool) error {
				createNamespacesFlag.CreateNamespaces = create
				return nil
			}).
			Build()
	}

	return nil
}

func defineSkipBackupFlag(cmd *cobra.Command, f any) error {
	var skipBackupFlag *flags.SkipBackupFlag
	if c, ok := f.(flags.WithSkipBackupFlag); ok {
		skipBackupFlag = c.WithSkipBackupFlag()
	}

	if skipBackupFlag != nil {
		skipBackupFlag.SkipBackup = false

		hc.NewBoolFlagBuilder(cmd, true).
			Name("skip-backup").
			Usage("Skip automatic backup creation before uninstallation").
			Validate(func(skip bool) error {
				skipBackupFlag.SkipBackup = skip
				return nil
			}).
			Build()
	}

	return nil
}

func defineReplaceFlag(cmd *cobra.Command, f any) error {
	var replaceFlag *flags.ReplaceFlag
	if c, ok := f.(flags.WithReplaceFlag); ok {
		replaceFlag = c.WithReplaceFlag()
	}

	if replaceFlag != nil {
		replaceFlag.Replace = false

		hc.NewBoolFlagBuilder(cmd, true).
			Name("replace").
			Usage("Also delete+recreate resources when SSA dry-run fails for reasons other than immutable fields (immutable conflicts are handled automatically when the API reports them)").
			Validate(func(replace bool) error {
				replaceFlag.Replace = replace
				return nil
			}).
			Build()

		hc.AddPreRun(cmd, func(cmd *cobra.Command, args []string) {
			if replaceFlag.Replace {
				l := log.Default()
				l.Warn(logIdCmd, "replace flag enabled: non-immutable SSA dry-run failures will be deleted before apply in addition to API-reported immutable conflicts")
			}
		})
	}

	return nil
}

func defineExcludeAppFlag(cmd *cobra.Command, f any) error {
	var excludeAppFlag *flags.ExcludeAppFlag
	if e, ok := f.(flags.WithExcludeAppFlag); ok {
		excludeAppFlag = e.WithExcludeAppFlag()
	}

	if excludeAppFlag != nil {
		excludeAppFlag.ExcludeAppPatterns = nil

		excludes := []string{}
		cmd.Flags().StringArrayVar(&excludes, "exclude-app", nil,
			"Glob pattern to exclude apps from selection, e.g. 'prod.cluster-infra.ingress-nginx' (repeatable)")
		hc.AddPreRunE(cmd, func(cmd *cobra.Command, args []string) error {
			for _, e := range excludes {
				excludeAppFlag.ExcludeAppPatterns = append(
					excludeAppFlag.ExcludeAppPatterns, types.AppIdPattern(e))
			}
			return nil
		})
	}

	return nil
}

func defineClusterWorkloadTimeoutFlag(cmd *cobra.Command, f any) error {
	var fl *flags.ClusterWorkloadTimeoutFlag
	if c, ok := f.(flags.WithClusterWorkloadTimeoutFlag); ok {
		fl = c.WithClusterWorkloadTimeoutFlag()
	}
	if fl == nil {
		return nil
	}
	fl.ClusterWorkloadTimeout = time.Minute

	hc.NewStringFlagBuilder(cmd, "1m").
		Name("cluster-workload-timeout").
		Usage("After scaling cluster-only workloads to zero, wait this long for their pods to terminate (cannot be combined with --force-scale-down)").
		Validate(func(value string) error {
			d, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("invalid duration for --cluster-workload-timeout: %w", err)
			}
			fl.ClusterWorkloadTimeout = d
			return nil
		}).
		Build()

	return nil
}

func defineReviewRefsYamlFlag(cmd *cobra.Command, f any) error {
	var yamlFlag *flags.ReviewRefsYamlFlag
	if c, ok := f.(flags.WithReviewRefsYamlFlag); ok {
		yamlFlag = c.WithReviewRefsYamlFlag()
	}
	if yamlFlag == nil {
		return nil
	}

	hc.NewBoolFlagBuilder(cmd, false).
		Name("yaml").
		Usage("Emit each finding as YAML (default: human-readable text; color flags apply to both formats)").
		Validate(func(v bool) error {
			yamlFlag.Yaml = v
			return nil
		}).
		Build()

	return nil
}

func defineNoCacheFlag(cmd *cobra.Command, f any) error {
	var noCache *flags.NoCacheFlag
	if c, ok := f.(flags.WithNoCacheFlag); ok {
		noCache = c.WithNoCacheFlag()
	}
	if noCache == nil {
		return nil
	}
	noCache.NoCache = false

	hc.NewBoolFlagBuilder(cmd, true).
		Name("no-cache").
		Usage("Disable persistent Helm template cache under each root app's .hydra/cache/helm and skip in-process Helm-related caches for this run (overrides " + types.HydraNoCacheEnvName + " when set)").
		Validate(func(v bool) error {
			noCache.NoCache = v
			return nil
		}).
		Build()

	return nil
}

func defineClusterListParallelFlag(cmd *cobra.Command, f any) error {
	var fl *flags.ClusterListParallelFlag
	if c, ok := f.(flags.WithClusterListParallelFlag); ok {
		fl = c.WithClusterListParallelFlag()
	}
	if fl == nil {
		return nil
	}
	fl.Parallel = 0
	cmd.Flags().IntVar(&fl.Parallel, "parallel", 0,
		"Parallel workers for live cluster listing, cluster review ref-ownership passes, cluster uninstall filter/merge phases, and apply SSA dry-run / discovery (footer shows one status line per worker when the effective count is >1; 0 = GOMAXPROCS, capped at 64)")
	return nil
}

func defineClusterListSkipOwnerRefsFlag(cmd *cobra.Command, f any) error {
	var fl *flags.ClusterListSkipOwnerRefsFlag
	if c, ok := f.(flags.WithClusterListSkipOwnerRefsFlag); ok {
		fl = c.WithClusterListSkipOwnerRefsFlag()
	}
	if fl == nil {
		return nil
	}
	fl.SkipOwnerRefs = false

	hc.NewBoolFlagBuilder(cmd, true).
		Name("skip-owner-refs").
		Usage("After listing the full cluster inventory, omit resources with a metadata.ownerReference whose UID matches another live object (CEL --include/--exclude apply first; uses ListClusterAll instead of predicate-scoped discovery)").
		Validate(func(v bool) error {
			fl.SkipOwnerRefs = v
			return nil
		}).
		Build()

	return nil
}
