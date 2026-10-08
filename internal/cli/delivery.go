package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/weaveplatform/imageweave/pkg/delivery"
	"github.com/weaveplatform/imageweave/pkg/plan"
)

func addDeliveryCommand(root *cobra.Command) {
	root.AddCommand(newDeliveryCommand(delivery.Exec), newNativeDeliveryCommand(delivery.Exec))
}

func newDeliveryCommand(run delivery.Runner) *cobra.Command {
	return deliveryCommand(run, false)
}

func newNativeDeliveryCommand(run delivery.Runner) *cobra.Command {
	return deliveryCommand(run, true)
}

func deliveryCommand(run delivery.Runner, native bool) *cobra.Command {
	var options delivery.Options
	var request string
	command := &cobra.Command{
		Use:   "delivery",
		Short: "Build, import and accept an exact Linux OCI candidate",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, err := os.Open(request)
			if err != nil {
				return fmt.Errorf("open delivery request: %w", err)
			}
			defer f.Close()
			options.Request, err = plan.Decode(f)
			if err != nil {
				return fmt.Errorf("decode delivery request: %w", err)
			}
			if native {
				result, err := delivery.BuildNative(cmd.Context(), options, run, cmd.ErrOrStderr())
				if err != nil {
					return fmt.Errorf("construct native delivery: %w", err)
				}
				return writeJSON(cmd.OutOrStdout(), result)
			}
			result, err := delivery.Build(cmd.Context(), options, run, cmd.ErrOrStderr())
			if err != nil {
				return fmt.Errorf("build delivery: %w", err)
			}
			return writeJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().
		StringVar(&request, "request", "", "Reviewed pinned source, firmware and key request YAML")
	command.Flags().
		StringVar(&options.RecipeCommit, "recipe-commit", "", "Full Imageweave recipe commit SHA")
	command.Flags().
		StringVar(&options.SourceURI, "source-uri", "", "Canonical vendor media HTTPS URI")
	command.Flags().
		StringVar(&options.Version, "version", "", "Immutable candidate tag")
	command.Flags().
		StringVar(&options.Out, "out", "", "New absolute delivery output directory")
	command.Flags().
		StringVar(&options.Packer, "packer", "packer", "Packer binary")
	command.Flags().
		StringVar(&options.OCI, "weaveoci", "weaveoci", "OCI binary built from pinned repository commit")
	if native {
		command.Use = "delivery-native"
		command.Short = "Construct and package an unqualified Windows or macOS OCI candidate"
	} else {
		command.Flags().
			StringVar(&options.Imageweave, "imageweave", "imageweave", "Imageweave binary for runtime acceptance")
	}
	return command
}
