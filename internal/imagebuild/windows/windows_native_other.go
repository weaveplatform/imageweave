//go:build !windows

package windows

import (
	"context"
	"fmt"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
)

func installWindowsNative(context.Context, WindowsInstallRequest) (WindowsInstallResult, error) {
	return WindowsInstallResult{}, fmt.Errorf(
		"%w: native Windows image installation requires HCS",
		common.ErrInput,
	)
}
