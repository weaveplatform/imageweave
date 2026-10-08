package macosbuild

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/weaveplatform/imageweave/pkg/buildstorage"
)

type storagePlanner func(context.Context, Options, io.Writer) (buildstorage.Plan, error)

// PlanStorage runs without downloading media or creating VM disks. The restore
// media allowance is an explicit upper bound checked again before downloading.
func PlanStorage(ctx context.Context, o Options, log io.Writer) (buildstorage.Plan, error) {
	return planStorage(ctx, o, log, buildstorage.Discover)
}

func planStorage(
	ctx context.Context,
	o Options,
	log io.Writer,
	discover func(context.Context, string, string) (buildstorage.Inventory, error),
) (buildstorage.Plan, error) {
	releases, err := selections(o)
	if err != nil {
		return buildstorage.Plan{}, err
	}
	if o.ScratchRoot == "" {
		cache, e := os.UserCacheDir()
		if e != nil {
			return buildstorage.Plan{}, e
		}
		o.ScratchRoot = filepath.Join(cache, "imageweave", "scratch")
	}
	if !filepath.IsAbs(o.ScratchRoot) {
		return buildstorage.Plan{}, fmt.Errorf("absolute host scratch root required")
	}
	images := len(releases)
	if o.Tier != "base" {
		images *= 2
	}
	budget, err := buildstorage.MacOS(images)
	if err != nil {
		return buildstorage.Plan{}, err
	}
	inventory, err := discover(ctx, o.ScratchRoot, o.Workspace)
	if err != nil {
		return buildstorage.Plan{}, err
	}
	result, err := buildstorage.Select(inventory, budget, o.Workspace)
	if err == nil && o.Workspace == "auto" {
		key := o.Release + "-" + o.Tier
		if o.Revision != "" {
			key += "-r" + o.Revision
		}
		result.Workspace = filepath.Join(result.Workspace, key)
	}
	if log != nil {
		_, _ = fmt.Fprintf(
			log,
			"Storage preflight: %d sequential image(s), %.1f GiB image peak, %.1f GiB host scratch, %.1f GiB reserve per capacity pool\n",
			images,
			float64(budget.Image)/float64(buildstorage.GiB),
			float64(budget.Host)/float64(buildstorage.GiB),
			float64(budget.Reserve)/float64(buildstorage.GiB),
		)
		_ = json.NewEncoder(log).Encode(result)
	}
	return result, err
}

func storedWorkflow(
	ctx context.Context,
	o Options,
	log io.Writer,
	plan storagePlanner,
	build func(context.Context, Options, io.Writer) ([]Result, error),
) ([]Result, error) {
	if _, err := selections(o); err != nil {
		return nil, err
	}
	storage, err := plan(ctx, o, log)
	if err != nil {
		return nil, err
	}
	o.Workspace, o.ScratchRoot = storage.Workspace, storage.Scratch
	if err = os.MkdirAll(o.ScratchRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create host scratch: %w", err)
	}
	release, err := lockWorkspace(filepath.Join(o.ScratchRoot, "imageweave-build"))
	if err != nil {
		return nil, fmt.Errorf("another macOS build owns the scratch root: %w", err)
	}
	defer release()
	return build(ctx, o, log)
}

func checkBuildStorage(ctx context.Context, o Options, log io.Writer) error {
	// Already-existing artifacts consume actual free space. Reserve one more
	// image's peak before starting each new restore/preparation, never all clones
	// concurrently. Explicit workspace remains fixed throughout this invocation.
	o.Release, o.Tier = "n", "base"
	_, err := PlanStorage(ctx, o, log)
	return err
}
