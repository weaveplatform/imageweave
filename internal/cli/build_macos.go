package cli

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/weaveplatform/imageweave/pkg/macosbuild"
)

func newBuildMacOS(
	run func(context.Context, macosbuild.Options, io.Writer) ([]macosbuild.Result, error),
) *cobra.Command {
	var o macosbuild.Options
	cmd := &cobra.Command{
		Use:   "build-macos",
		Short: "Build, prepare, package and validate macOS images without interactive setup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			results, err := run(cmd.Context(), o, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), results)
		},
	}
	cmd.Flags().StringVar(&o.Workspace, "workspace", "", "Absolute image storage directory")
	cmd.Flags().
		StringVar(&o.Repository, "repository", ".", "Imageweave checkout containing the Go builders and Packer templates")
	cmd.Flags().
		StringVar(&o.Release, "release", "all", "macOS 27, 26, 15, exact version, n/n-1/n-2, or all")
	cmd.Flags().
		StringVar(&o.Tier, "tier", "all", "base, prepared, or all (prepared also builds its base when needed)")
	cmd.Flags().
		StringVar(&o.Timeout, "timeout", "90m", "Native construction and per-clone acceptance timeout")
	cmd.Flags().StringVar(&o.Packer, "packer", "packer", "Packer 1.16.0 executable")
	cmd.Flags().
		StringVar(&o.OCI, "weaveoci", "", "Optional OCI executable; default builds the pinned Go module")
	cmd.Flags().
		StringVar(&o.Revision, "revision", "", "Optional immutable candidate revision (positive number or number-attempt)")
	return cmd
}
