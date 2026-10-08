package imagebuild

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/oci"

	"github.com/weaveplatform/weaveplatform-oci/pkg/conformance"
	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
)

func DeepCheck(
	ctx context.Context,
	store *oci.Store,
	root ocispec.Descriptor,
) (conformance.Report, error) {
	report, err := conformance.Check(ctx, store, root, conformance.Options{Deep: true})
	if err != nil {
		return report, fmt.Errorf("inspect layout: %w", err)
	}
	if !report.OK() {
		return report, fmt.Errorf("%w: %s", ErrInput, strings.Join(report.Problems(), "; "))
	}
	return report, nil
}

func OpenCandidate(ctx context.Context, dir string) (*oci.Store, conformance.Report, error) {
	// OpenLayout can create a store; a parent input must already be a layout.
	if _, err := os.Stat(filepath.Join(dir, "oci-layout")); err != nil {
		return nil, conformance.Report{}, fmt.Errorf("read base layout: %w", err)
	}
	store, err := pack.OpenLayout(ctx, dir)
	if err != nil {
		return nil, conformance.Report{}, fmt.Errorf("open layout: %w", err)
	}
	var tags []string
	if err := store.Tags(
		ctx,
		"",
		func(page []string) error { tags = append(tags, page...); return nil },
	); err != nil {
		return nil, conformance.Report{}, fmt.Errorf("list layout tags: %w", err)
	}
	if len(tags) != 1 {
		return nil, conformance.Report{}, fmt.Errorf(
			"%w: base layout must have exactly one tag",
			ErrInput,
		)
	}
	root, err := store.Resolve(ctx, tags[0])
	if err != nil {
		return nil, conformance.Report{}, fmt.Errorf("resolve base: %w", err)
	}
	report, err := DeepCheck(ctx, store, root)
	return store, report, err
}

// AgentOptions identifies the verified parent and new Linux agent candidate.
type AgentOptions struct {
	Base, BaseName, Arch, Cache, Out string
	Revision                         int
	Timeout                          time.Duration
	Lock                             Lock
}

// ValidateAgentOptions checks shared derived-candidate inputs before filesystem changes.
func ValidateAgentOptions(o AgentOptions) error {
	if (o.Arch != "amd64" && o.Arch != "arm64") || o.Timeout <= 0 || o.Revision < 1 ||
		o.Out == "" ||
		o.Cache == "" ||
		!ParentName.MatchString(o.BaseName) {
		return fmt.Errorf(
			"%w: platform, parent name, output, cache, revision and timeout required",
			ErrInput,
		)
	}
	return nil
}
