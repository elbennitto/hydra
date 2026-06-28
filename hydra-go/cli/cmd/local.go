package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/core/entity"
	"hydra-gitops.org/hydra/hydra-go/core/hydra"
)

// LocalCommandParams holds the action functions for local-only command families.
type LocalCommandParams struct {
	Find          func(flags action.FindFlags) (hydra.Hydra, string, error)
	Config        func(flags action.ConfigFlags) (hydra.Hydra, string, error)
	Template      func(flags action.TemplateFlags) (hydra.Hydra, string, error)
	Package       func(flags action.LocalPackageFlags) (string, error)
	List          func(flags action.TemplateFlags) (entity.Entities, error)
	Source        func(flags action.SourceFlags) (hydra.Hydra, string, error)
	Values        func(flags action.ValuesFlags) (hydra.Hydra, string, error)
	Refs          func(flags action.LocalRefsFlags) error
	LocalTree     func(flags action.LocalTreeFlags) error
	Review        ReviewCommandParams
	Test          TestCommandParams
	ExportContext func(flags action.ClusterViewContextFlags) error
}

// NewLocalCommand creates and returns the local command with subcommands.
func NewLocalCommand(params LocalCommandParams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "local",
		Short: "Local rendering, inspection, review, and export commands",
		Long: `Commands for rendering, inspecting, reviewing, testing, and exporting
Hydra data locally without connecting to a Kubernetes cluster.`,
	}

	cmd.AddCommand(newFindCommand(params.Find))
	cmd.AddCommand(newLocalAppsCommand(action.ResolveApps))
	cmd.AddCommand(newConfigCommand(params.Config))
	cmd.AddCommand(newTemplateCommand(params.Template))
	cmd.AddCommand(newLocalPackageCommand(params.Package))
	cmd.AddCommand(newLocalListCommand(params.List))
	cmd.AddCommand(newSourceCommand(params.Source))
	cmd.AddCommand(newValuesCommand(params.Values))
	cmd.AddCommand(newLocalRefsCommand(params.Refs))
	cmd.AddCommand(newLocalTreeCommand(params.LocalTree))
	cmd.AddCommand(NewReviewCommand(params.Review))
	cmd.AddCommand(NewTestCommand(params.Test))
	cmd.AddCommand(newExportCommand(params.ExportContext))

	return cmd
}
