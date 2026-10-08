//go:build !darwin || !arm64

package macos

import (
	"context"
	"fmt"
	"io"

	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

func StartNative(context.Context, string, spec.Config, io.Writer) (Session, error) {
	return nil, fmt.Errorf(
		"%w: native macOS provisioning requires an Apple silicon Mac",
		ErrPrepared,
	)
}
