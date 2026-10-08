package cli

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/weaveplatform/imageweave/pkg/buildstorage"
	"github.com/weaveplatform/imageweave/pkg/macosbuild"
)

func newMacOSStoragePlan(
	plan func(context.Context, macosbuild.Options, io.Writer) (buildstorage.Plan, error),
) *cobra.Command {
	var o macosbuild.Options
	c := &cobra.Command{
		Use:   "plan-macos-storage",
		Short: "Discover storage and size a sequential macOS image job before downloading",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			p, err := plan(c.Context(), o, c.ErrOrStderr())
			if writeErr := writeJSON(c.OutOrStdout(), p); writeErr != nil {
				return writeErr
			}
			return err
		},
	}
	macOSBuildFlags(c, &o)
	return c
}
