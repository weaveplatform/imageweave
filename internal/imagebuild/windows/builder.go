// Package windows owns Windows media selection, installation, HCS orchestration
// and Windows guest provisioning. OCI packages only the resulting artifacts.
package windows

import (
	"context"
	"crypto/rand"
	"fmt"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
)

type Tools common.Tools

type Packages struct {
	Tools          common.Tools
	Downloader     common.Downloader
	WindowsInstall WindowsInstallFunc
}

func (p Packages) Prepare(
	ctx context.Context,
	l common.Lock,
	platform, cache, out string,
) ([]common.Asset, error) {
	assets, err := (common.Packages{Tools: p.Tools, Downloader: p.Downloader, Recipe: NativeAgentRecipe}).Prepare(
		ctx,
		l,
		platform,
		cache,
		out,
	)
	return assets, windowsError(err)
}

func randomMarker() []byte {
	marker := make([]byte, 12)
	if _, err := rand.Read(marker); err != nil {
		panic(err)
	}
	return marker
}

func windowsError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("windows image: %w", err)
}
