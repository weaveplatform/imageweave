// Package cli exposes the reviewed matrix and concrete Packer build plans.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/weaveplatform/imageweave/internal/buildinfo"
	"github.com/weaveplatform/imageweave/pkg/catalog"
	"github.com/weaveplatform/imageweave/pkg/plan"
)

// New returns an independent command tree for embedding and tests.
func New(out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use: "imageweave", Short: "Resolve reviewed VM image scenarios for Packer",
		SilenceUsage: true, SilenceErrors: true,
		Version: buildinfo.Version(), Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.AddCommand(&cobra.Command{
		Use: "matrix", Short: "List all requested releases and architecture readiness",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeJSON(cmd.OutOrStdout(), catalog.Current().Matrix())
		},
	})
	var request string
	var variablesOnly bool
	p := &cobra.Command{
		Use: "plan", Short: "Resolve a pinned Packer candidate; does not build or publish",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			content, err := os.Open(request)
			if err != nil {
				return fmt.Errorf("open request: %w", err)
			}
			defer content.Close()
			r, err := plan.Decode(content)
			if err != nil {
				return fmt.Errorf("decode build request: %w", err)
			}
			resolved, err := plan.Resolve(r)
			if err != nil {
				return fmt.Errorf("resolve build request: %w", err)
			}
			if variablesOnly {
				return writeJSON(cmd.OutOrStdout(), resolved.Variables)
			}
			return writeJSON(cmd.OutOrStdout(), resolved)
		},
	}
	p.Flags().StringVar(&request, "request", "", "Strict YAML build request")
	p.Flags().BoolVar(&variablesOnly, "vars", false, "Output Packer variable JSON only")
	// This flag is defined directly above; failure would be a programmer error.
	if err := p.MarkFlagRequired("request"); err != nil {
		panic(err)
	}
	root.AddCommand(p, newImage(out, errOut))
	addDeliveryCommand(root)
	return root
}

func writeJSON(w io.Writer, value any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	if err := e.Encode(value); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	return nil
}
