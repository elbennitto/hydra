package cmd

import (
	"github.com/spf13/cobra"
	"hydra-gitops.org/hydra/hydra-go/cli/action"
	"hydra-gitops.org/hydra/hydra-go/cli/exitcode"
	"hydra-gitops.org/hydra/hydra-go/core/types"
)

type ReviewCommandParams struct {
	ReviewRefs func(flags action.ReviewRefsFlags) error
}

func NewReviewCommandParams() ReviewCommandParams {
	return ReviewCommandParams{
		ReviewRefs: action.ReviewRefs,
	}
}

func NewReviewCommand(params ReviewCommandParams) *cobra.Command {
	flags := action.ReviewRefsFlags{}

	cmd := &cobra.Command{
		Use:   "review <appId...>",
		Short: "Review rendered references and ref ownership",
		Long: `Review rendered resource references for the selected app(s).

The review validates that referenced entities exist, that explicit Secret and
ConfigMap key selections are present in the target object, and that each
template resource id is not also claimed by another app's ref-parser ownership
predicates (the same check cluster uninstall uses).`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.AppIdPatterns = types.ToAppIdPatterns(args)
			err := params.ReviewRefs(flags)
			cmd.SilenceUsage = !exitcode.IsShowUsage(err)
			return err
		},
	}

	DefineFlags(cmd, &flags)

	return cmd
}
