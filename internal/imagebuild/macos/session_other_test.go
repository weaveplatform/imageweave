//go:build !darwin || !arm64

package macos

import (
	"errors"
	"io"
	"testing"

	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

func TestNativeRequiresAppleSilicon(t *testing.T) {
	if _, err := StartNative(
		t.Context(),
		"unused",
		spec.Config{},
		io.Discard,
	); !errors.Is(
		err,
		ErrPrepared,
	) {
		t.Fatal(err)
	}
}
