// build-macos is the stand-alone Go entry point for local image builds.
// Run from the checkout with: go run ./cmd/build-macos --workspace /path/to/images
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/weaveplatform/imageweave/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	command := cli.New(os.Stdout, os.Stderr)
	command.SetArgs(append([]string{"build-macos"}, os.Args[1:]...))
	if err := command.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
