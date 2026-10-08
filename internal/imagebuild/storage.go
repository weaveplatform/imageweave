package imagebuild

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
)

func SystemDisk(b pack.Bundle) (string, error) {
	var disk string
	for _, d := range b.File.Disks {
		if d.Role == "system" {
			if disk != "" {
				return "", fmt.Errorf("%w: multiple system disks", ErrInput)
			}
			disk = d.Path
		}
	}
	if disk == "" {
		return "", fmt.Errorf("%w: missing system disk", ErrInput)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(b.Dir, disk))
	if err != nil {
		return "", fmt.Errorf("resolve disk: %w", err)
	}
	root, err := filepath.EvalSymlinks(b.Dir)
	if err != nil {
		return "", fmt.Errorf("resolve bundle: %w", err)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%w: system disk escapes bundle", ErrInput)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat disk: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: disk is not a regular file", ErrInput)
	}
	return resolved, nil
}

func CopyTree(src, dst string) error {
	return CopyTreeContext(context.Background(), src, dst)
}

func CopyTreeContext(ctx context.Context, src, dst string) error {
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("resolve payload path: %w", err)
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%w: payload contains non-regular file", ErrInput)
		}
		return CopyFileContext(ctx, path, filepath.Join(dst, rel))
	})
	if err != nil {
		return fmt.Errorf("copy payload: %w", err)
	}
	return nil
}
