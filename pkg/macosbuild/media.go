package macosbuild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/imageweave/pkg/buildstorage"
)

func ipswCache(o Options) string {
	if o.SharedMediaCache {
		return filepath.Join(filepath.Dir(o.Workspace), "media")
	}
	return filepath.Join(o.Workspace, "media")
}

// Include the previous per-attempt media directories so upgrading the builder
// adopts already downloaded IPSWs without duplicating their multi-gigabyte data.
func ipswCandidates(workspace, cache string) ([]string, error) {
	dirs := []string{cache, filepath.Join(workspace, "media")}
	entries, err := mediaEntries(filepath.Dir(workspace))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(filepath.Dir(workspace), entry.Name(), "media"))
		}
	}
	seen := map[string]bool{}
	var candidates []string
	for _, dir := range dirs {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		entries, err = mediaEntries(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".ipsw") {
				candidates = append(candidates, filepath.Join(dir, entry.Name()))
			}
		}
	}
	return candidates, nil
}

// Windows ReadDir can classify a regular-file path as "not found". Check the
// directory itself first so a cache misconfiguration never triggers a transfer.
func mediaEntries(dir string) ([]os.DirEntry, error) {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("IPSW cache path is not a directory: %s", dir)
	}
	return os.ReadDir(dir)
}

func downloadIPSW(ctx context.Context, o Options, media common.Media, log io.Writer,
	admit func(context.Context, Options, uint64, io.Writer) error,
	fetch func(context.Context, common.Media, string) (string, error),
) (string, error) {
	if media.Size <= 0 || media.Size > int64(40*buildstorage.GiB) ||
		!common.SHAPattern.MatchString("sha256:"+media.SHA256) {
		return "", fmt.Errorf("%w: invalid IPSW size or SHA256", ErrBuild)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cache := ipswCache(o)
	destination := filepath.Join(cache, media.SHA256+".ipsw")
	candidates, err := ipswCandidates(o.Workspace, cache)
	if err != nil {
		return "", fmt.Errorf("discover cached IPSWs: %w", err)
	}
	sort.SliceStable(
		candidates,
		func(i, j int) bool { return candidates[i] == destination && candidates[j] != destination },
	)
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil {
			return "", err
		}
		if info.Size() != media.Size {
			if candidate == destination {
				if err = os.Remove(candidate); err != nil {
					return "", err
				}
			}
			continue
		}
		if log != nil {
			_, _ = fmt.Fprintf(log, "Verifying existing IPSW size and SHA256: %s\n", candidate)
		}
		progress := common.NewNativeProgress(ctx, log, "IPSW cache verification")
		finish := progress.Start()
		err = common.VerifiedFile(candidate, media)
		finish(err)
		if err != nil {
			if candidate == destination {
				if err = os.Remove(candidate); err != nil {
					return "", err
				}
			}
			continue
		}
		if err = ctx.Err(); err != nil {
			return "", err
		}
		if err = admit(ctx, o, 0, log); err != nil {
			return "", err
		}
		if candidate != destination {
			if err = os.MkdirAll(cache, 0o750); err != nil {
				return "", err
			}
			// Hard links retain a shared cache reference without allocating another IPSW.
			if err = os.Link(candidate, destination); err != nil {
				return "", fmt.Errorf("adopt verified IPSW: %w", err)
			}
		}
		if log != nil {
			_, _ = fmt.Fprintf(log, "Reusing verified IPSW; download skipped: %s\n", destination)
		}
		return destination, nil
	}
	// A corrupt cache entry must never be handed to Packer or block a fresh fetch.
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%w: IPSW cache entry is not a regular file", ErrBuild)
		}
		if err = os.Remove(destination); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	remaining := uint64(media.Size)
	if info, err := os.Stat(destination + ".part"); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%w: partial IPSW is not a regular file", ErrBuild)
		}
		var previous common.Media
		if err = common.ReadJSON(destination+".part.json", &previous); err != nil {
			return "", err
		}
		if previous != media || info.Size() > media.Size {
			return "", fmt.Errorf("%w: partial IPSW differs from source lock", ErrBuild)
		}
		remaining -= uint64(info.Size())
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err = admit(ctx, o, remaining, log); err != nil {
		return "", err
	}
	if log != nil {
		_, _ = fmt.Fprintf(
			log,
			"No verified IPSW found; downloading %d remaining bytes into %s\n",
			remaining,
			destination,
		)
	}
	return fetch(ctx, media, destination)
}
