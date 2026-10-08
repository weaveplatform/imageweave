package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/oci"

	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
)

// resolveRoot opens a layout and resolves ref, or the layout's only tag when
// ref is empty.
func resolveRoot(ctx context.Context, dir, ref string) (*oci.Store, ocispec.Descriptor, error) {
	store, err := pack.OpenLayout(ctx, dir)
	if err != nil {
		return nil, ocispec.Descriptor{}, err //nolint:wrapcheck // pack names the directory
	}
	if ref == "" {
		var tags []string
		if err := store.Tags(ctx, "", func(t []string) error {
			tags = append(tags, t...)
			return nil
		}); err != nil {
			return nil, ocispec.Descriptor{}, fmt.Errorf("list tags: %w", err)
		}
		sort.Strings(tags)
		if len(tags) != 1 {
			return nil, ocispec.Descriptor{}, fmt.Errorf(
				"%w: layout %s has %d tags (%s); choose one with --ref",
				errUsage,
				dir,
				len(tags),
				strings.Join(tags, ", "),
			)
		}
		ref = tags[0]
	}
	desc, err := store.Resolve(ctx, ref)
	if err != nil {
		return nil, ocispec.Descriptor{}, fmt.Errorf("resolve %q in %s: %w", ref, dir, err)
	}
	return store, desc, nil
}
