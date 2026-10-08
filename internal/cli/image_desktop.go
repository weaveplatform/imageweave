package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/weaveplatform/imageweave/internal/imagebuild"
	linuximage "github.com/weaveplatform/imageweave/internal/imagebuild/linux"
)

func newImageDesktop(
	p linuximage.Packages,
	load func() (imagebuild.Lock, error),
	emit func(any) error,
) *cobra.Command {
	var file string
	build := func(ctx context.Context, o imagebuild.AgentOptions) (string, error) {
		lock, err := linuximage.LoadDesktopLock(file)
		if err != nil {
			return "", fmt.Errorf("desktop lock: %w", err)
		}
		return p.BuildLinuxDesktop(ctx, linuximage.DesktopOptions{AgentOptions: o, Desktop: lock})
	}
	cmd := newImageAgentBuilder("linux-desktop", build, load, emit)
	cmd.Use = "build-linux-desktop"
	cmd.Short = "Build Xfce/X11 from an exact agent parent and pinned Ubuntu package closure"
	cmd.Flags().
		StringVar(&file, "desktop-lock", "", "Desktop snapshot, parent and complete package lock")
	return cmd
}
