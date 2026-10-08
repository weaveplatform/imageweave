package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"oras.land/oras-go/v2/content/oci"

	"github.com/weaveplatform/imageweave/internal/imagebuild/macos"
	"github.com/weaveplatform/weaveplatform-oci/pkg/imagecheck"
)

func newImageMacOS(boot imagecheck.BootClone, emit func(any) error, log io.Writer) *cobra.Command {
	var ref, out string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "validate-macos LAYOUT",
		Short: "Boot and reboot two independent copies of an exact macOS OCI image",
		Args:  usageArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := os.Stat(args[0])
			if err != nil || !info.IsDir() {
				return fmt.Errorf("%w: existing OCI layout required", errUsage)
			}
			store, err := oci.New(args[0])
			if err != nil {
				return fmt.Errorf("open candidate: %w", err)
			}
			root, err := store.Resolve(cmd.Context(), ref)
			if err != nil {
				return fmt.Errorf("resolve candidate: %w", err)
			}
			report, err := imagecheck.Validate(
				cmd.Context(),
				imagecheck.Candidate{
					Store:   store,
					Root:    root,
					Tag:     ref,
					Out:     out,
					Timeout: timeout,
					Log:     log,
				},
				boot,
			)
			if err != nil {
				return fmt.Errorf("validate macOS: %w", err)
			}
			return emit(report)
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "Exact candidate index reference")
	cmd.Flags().
		StringVar(&out, "out", "", "New output directory for independent clones and acceptance evidence")
	cmd.Flags().
		DurationVar(&timeout, "timeout", 45*time.Minute, "Timeout per clone including setup and reboot")
	return cmd
}

func macOSBoot(log io.Writer) imagecheck.BootClone {
	return func(ctx context.Context, c imagecheck.Clone) (imagecheck.Boot, error) {
		return macos.AcceptClone(ctx, c, macos.StartNative, log)
	}
}
