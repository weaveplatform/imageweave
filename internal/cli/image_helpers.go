package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

var errUsage = errors.New("usage")

func usageArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return fmt.Errorf(
				"%w: %s takes %d argument(s), got %d",
				errUsage,
				cmd.Name(),
				n,
				len(args),
			)
		}
		return nil
	}
}
