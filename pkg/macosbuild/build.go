// Package macosbuild runs the complete local macOS image workflow without an
// interactive operator: pinned media, native Packer build, OCI and acceptance.
package macosbuild

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"oras.land/oras-go/v2/content/oci"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/imageweave/internal/imagebuild/macos"
	"github.com/weaveplatform/imageweave/pkg/catalog"
	"github.com/weaveplatform/imageweave/pkg/delivery"
	"github.com/weaveplatform/imageweave/pkg/plan"
	"github.com/weaveplatform/weaveplatform-oci/pkg/conformance"
	"github.com/weaveplatform/weaveplatform-oci/pkg/imagecheck"
)

var ErrBuild = errors.New("macOS build workflow")

// Options select ad-hoc builds. The workspace contains media locks, cached
// downloads, artifacts and reports; repository files contain no lab paths.
type Options struct {
	Workspace, Repository, Release, Tier, Packer, OCI string
	Timeout                                           string
}

type Result struct {
	Version    string `json:"version"`
	Tier       string `json:"tier"`
	Layout     string `json:"layout"`
	Acceptance string `json:"acceptance"`
	Digest     string `json:"digest"`
	Reused     bool   `json:"reused"`
}

type services struct {
	sources  func(context.Context, string) ([]macos.AppleSource, error)
	download func(context.Context, common.Media, string) (string, error)
	build    func(context.Context, delivery.Options) (delivery.Construction, error)
	inspect  func(context.Context, string, string) (conformance.Report, error)
	validate func(context.Context, string, string, string) error
}

type tools struct {
	commit, imageweave, oci string
	runner                  delivery.Runner
}

// Run is the stand-alone workflow used by cmd/build-macos and the regular CLI.
// A successful existing artifact is deeply checked and its evidence rechecked
// before reuse. An incomplete directory is preserved and reported as an error.
func Run(ctx context.Context, o Options, log io.Writer) ([]Result, error) {
	return workflow(ctx, o, log, prepareTools, productionServices)
}

func workflow(
	ctx context.Context,
	o Options,
	log io.Writer,
	setup func(context.Context, Options, io.Writer) (tools, error),
	factory func(Options, tools, io.Writer) services,
) ([]Result, error) {
	releases, err := selections(o)
	if err != nil {
		return nil, err
	}
	releaseLock, err := lockWorkspace(o.Workspace)
	if err != nil {
		return nil, err
	}
	defer releaseLock()
	installed, err := setup(ctx, o, log)
	if err != nil {
		return nil, err
	}
	o.OCI = installed.oci
	return run(ctx, o, releases, installed, factory(o, installed, log), log)
}

func productionServices(o Options, installed tools, log io.Writer) services {
	return services{
		sources:  func(ctx context.Context, r string) ([]macos.AppleSource, error) { return macos.MacSources(ctx, nil, r) },
		download: (common.Downloader{Log: log}).Download,
		build: func(ctx context.Context, opts delivery.Options) (delivery.Construction, error) {
			return delivery.BuildNative(ctx, opts, installed.runner, log)
		},
		inspect: func(ctx context.Context, path, ref string) (report conformance.Report, err error) {
			progress := common.NewNativeProgress(ctx, log, "macOS artifact verification")
			finish := progress.Start()
			defer func() { finish(err) }()
			progress.Step("checking complete OCI content for " + ref)
			return inspect(ctx, path, ref)
		},
		validate: func(ctx context.Context, layout, tag, out string) error {
			return installed.runner(
				ctx,
				installed.imageweave,
				[]string{
					"image",
					"validate-macos",
					layout,
					"--ref",
					tag,
					"--out",
					out,
					"--timeout",
					o.Timeout,
				},
				log,
			)
		},
	}
}

func selections(o Options) ([]string, error) {
	if !filepath.IsAbs(o.Workspace) || filepath.Clean(o.Workspace) != o.Workspace ||
		o.Repository == "" ||
		o.Packer == "" ||
		(o.Tier != "base" && o.Tier != "prepared" && o.Tier != "all") {
		return nil, fmt.Errorf(
			"%w: absolute workspace, repository, tools and base/prepared/all tier required",
			ErrBuild,
		)
	}
	if timeout, err := time.ParseDuration(o.Timeout); err != nil || timeout <= 0 {
		return nil, fmt.Errorf("%w: positive timeout required", ErrBuild)
	}
	if o.Release == "all" {
		return []string{"27", "26", "15"}, nil
	}
	major := strings.Split(o.Release, ".")[0]
	s, err := catalog.Current().Resolve("macos", major, "arm64")
	if err != nil {
		return nil, fmt.Errorf("select macOS release: %w", err)
	}
	if strings.Contains(o.Release, ".") {
		if err := macos.ValidateMacVersion(o.Release); err != nil {
			return nil, err
		}
		return []string{o.Release}, nil
	}
	return []string{s.Version}, nil
}

func run(
	ctx context.Context,
	o Options,
	releases []string,
	installed tools,
	api services,
	log io.Writer,
) ([]Result, error) {
	var results []Result
	for _, release := range releases {
		source, err := sourceLock(ctx, o.Workspace, release, api.sources)
		if err != nil {
			return results, err
		}
		base, report, err := buildTier(ctx, o, source, "base", nil, installed, api, log)
		if err != nil {
			return results, err
		}
		if o.Tier != "prepared" {
			results = append(results, base)
		}
		if o.Tier == "base" {
			continue
		}
		if len(report.Children) != 1 {
			return results, fmt.Errorf("%w: macOS base must contain one platform", ErrBuild)
		}
		parent := &plan.Parent{
			Layout: base.Layout,
			Ref:    base.Version,
			Name:   "ghcr.io/weaveplatform/weave-images/macos-" + strings.Split(source.Version, ".")[0] + "-base",
			Digest: report.Children[0].Descriptor.Digest.String(),
		}
		prepared, _, err := buildTier(ctx, o, source, "prepared", parent, installed, api, log)
		if err != nil {
			return results, err
		}
		results = append(results, prepared)
	}
	return results, nil
}

// Lock before compiling workers as well as before writing image outputs.
func lockWorkspace(workspace string) (func(), error) {
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return nil, fmt.Errorf("create build workspace: %w", err)
	}
	lock := filepath.Join(workspace, "build.lock")
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: workspace is locked; inspect an interrupted run before removing build.lock: %w",
			ErrBuild,
			err,
		)
	}
	// The lock's existence owns the workspace; its contents are diagnostic only.
	_, _ = fmt.Fprintf(f, "pid=%d\n", os.Getpid())
	_ = f.Close()
	return func() { _ = os.Remove(lock) }, nil
}

func sourceLock(
	ctx context.Context,
	workspace, release string,
	lookup func(context.Context, string) ([]macos.AppleSource, error),
) (macos.AppleSource, error) {
	path := filepath.Join(workspace, "macos-"+release+"-source.json")
	var source macos.AppleSource
	raw, err := os.ReadFile(path)
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&source); err != nil {
			return source, fmt.Errorf("read source lock: %w", err)
		}
		if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return source, fmt.Errorf("%w: trailing source lock data", ErrBuild)
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			return source, fmt.Errorf("open source lock: %w", err)
		}
		found, err := lookup(ctx, release)
		if err != nil {
			return source, err
		}
		if len(found) == 0 {
			return source, fmt.Errorf("%w: no Apple media for %s", ErrBuild, release)
		}
		source = found[0]
	}
	if err := source.Validate(release); err != nil {
		return source, err
	}
	if raw == nil {
		data, err := json.MarshalIndent(source, "", "  ")
		if err != nil {
			return source, fmt.Errorf("encode source lock: %w", err)
		}
		temporary := path + ".tmp"
		defer os.Remove(temporary)
		if err = os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
			return source, fmt.Errorf("save source lock: %w", err)
		}
		if err = os.Rename(temporary, path); err != nil {
			return source, fmt.Errorf("commit source lock: %w", err)
		}
	}
	return source, nil
}

func buildTier(
	ctx context.Context,
	o Options,
	source macos.AppleSource,
	tier string,
	parent *plan.Parent,
	installed tools,
	api services,
	log io.Writer,
) (Result, conformance.Report, error) {
	tag := source.Version + "-" + source.Build + "-" + tier + "-" + installed.commit[:12]
	out := filepath.Join(o.Workspace, tag)
	result := Result{
		Version:    tag,
		Tier:       tier,
		Layout:     filepath.Join(out, "layout"),
		Acceptance: filepath.Join(out, "acceptance", "acceptance.json"),
	}
	var report conformance.Report
	if _, err := os.Stat(out); err == nil {
		report, err = api.inspect(ctx, result.Layout, tag)
		if err != nil {
			return result, report, fmt.Errorf(
				"existing candidate is incomplete or corrupt: %w",
				err,
			)
		}
		data, err := os.ReadFile(result.Acceptance)
		if err != nil {
			return result, report, fmt.Errorf(
				"existing candidate has no completed acceptance: %w",
				err,
			)
		}
		if err = imagecheck.CheckPromotion(data, report, tag); err != nil {
			return result, report, fmt.Errorf("existing candidate acceptance: %w", err)
		}
		result.Reused = true
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			return result, report, fmt.Errorf("inspect output: %w", err)
		}
		req := plan.Request{
			SchemaVersion: 1,
			Family:        "macos",
			Release:       strings.Split(source.Version, ".")[0],
			Arch:          "arm64",
			Target:        "apple-vz",
			Purpose:       "guest-base",
			SourceBuild:   source.Build,
			Timeout:       o.Timeout,
		}
		sourceURI := source.URL
		if parent == nil {
			media, err := api.download(
				ctx,
				common.Media{URI: source.URL, Size: source.Size, SHA256: source.SHA256},
				filepath.Join(o.Workspace, "media", source.SHA256+".ipsw"),
			)
			if err != nil {
				return result, report, err
			}
			req.SourceURL, req.SourceSHA256 = media, source.SHA256
		} else {
			req.Purpose, req.Parent = "guest-prepared", parent
			sourceURI = ""
		}
		_, err = api.build(
			ctx,
			delivery.Options{
				Request:      req,
				RecipeCommit: installed.commit,
				SourceURI:    sourceURI,
				Version:      tag,
				Out:          out,
				Packer:       o.Packer,
				OCI:          o.OCI,
			},
		)
		if err != nil {
			return result, report, err
		}
		if err = api.validate(
			ctx,
			result.Layout,
			tag,
			filepath.Join(out, "acceptance"),
		); err != nil {
			return result, report, err
		}
		report, err = api.inspect(ctx, result.Layout, tag)
		if err != nil {
			return result, report, err
		}
		data, err := os.ReadFile(result.Acceptance)
		if err != nil {
			return result, report, fmt.Errorf("read completed acceptance: %w", err)
		}
		if err = imagecheck.CheckPromotion(data, report, tag); err != nil {
			return result, report, err
		}
	}
	if err := matchArtifact(report, source, tier, parent, installed.commit); err != nil {
		return result, report, err
	}
	result.Digest = report.Root.Digest.String()
	if log != nil {
		_, _ = fmt.Fprintf(
			log,
			"macOS %s %s: verified %s (reused=%t)\n",
			source.Version,
			tier,
			result.Digest,
			result.Reused,
		)
	}
	return result, report, nil
}

func inspect(ctx context.Context, path, ref string) (conformance.Report, error) {
	var empty conformance.Report
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return empty, fmt.Errorf("%w: missing OCI output", ErrBuild)
	}
	store, err := oci.New(path)
	if err != nil {
		return empty, fmt.Errorf("open OCI output: %w", err)
	}
	root, err := store.Resolve(ctx, ref)
	if err != nil {
		return empty, fmt.Errorf("resolve OCI output: %w", err)
	}
	report, err := conformance.Check(ctx, store, root, conformance.Options{Deep: true})
	if err != nil || !report.OK() {
		return report, fmt.Errorf(
			"%w: OCI output failed verification: %w; %v",
			ErrBuild,
			err,
			report.Problems(),
		)
	}
	return report, nil
}

func matchArtifact(
	report conformance.Report,
	source macos.AppleSource,
	tier string,
	parent *plan.Parent,
	commit string,
) error {
	if len(report.Children) != 1 {
		return fmt.Errorf("%w: expected one macOS platform", ErrBuild)
	}
	c := report.Children[0].Description.Config
	if c.Guest.OS != "darwin" || c.Guest.Arch != "arm64" || c.Guest.OSVersion != source.Version ||
		c.Guest.OSBuild != source.Build ||
		c.Guest.Variant != tier ||
		c.Build.TemplateRef != "weaveplatform/imageweave@"+commit {
		return fmt.Errorf(
			"%w: artifact differs from selected platform, source, tier or recipe",
			ErrBuild,
		)
	}
	if parent != nil {
		if c.Build.Base == nil || c.Build.Base.Name != parent.Name ||
			c.Build.Base.Digest != parent.Digest {
			return fmt.Errorf("%w: artifact differs from pinned parent", ErrBuild)
		}
	} else if c.Build.Base != nil || len(c.Build.SourceMedia) != 1 || c.Build.SourceMedia[0].Kind != "ipsw" || c.Build.SourceMedia[0].URI != source.URL || c.Build.SourceMedia[0].Digest != "sha256:"+source.SHA256 {
		return fmt.Errorf("%w: artifact differs from pinned Apple media", ErrBuild)
	}
	return nil
}
