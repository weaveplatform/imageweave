package buildstorage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"howett.net/plist"
)

type (
	command  func(context.Context, string, ...string) ([]byte, error)
	probe    func(string) (uint64, string, error)
	diskInfo struct {
		Mount      string `plist:"MountPoint"`
		Device     string `plist:"DeviceIdentifier"`
		Pool       string `plist:"APFSContainerReference"`
		Filesystem string `plist:"FilesystemType"`
		Internal   bool   `plist:"Internal"`
		Writable   bool   `plist:"Writable"`
	}
)

type mountedImages struct {
	Images []struct {
		Path     string `plist:"image-path"`
		Entities []struct {
			Mount string `plist:"mount-point"`
		} `plist:"system-entities"`
	} `plist:"images"`
}

func execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("storage discovery %s: %w", name, err)
	}
	return b, nil
}

// Discover examines mounted physical and virtual volumes, including volumes
// mounted beneath another mount. It never mounts, reformats or deletes storage.
func Discover(ctx context.Context, host, workspace string) (Inventory, error) {
	local, err := os.UserCacheDir()
	if err != nil {
		return Inventory{}, err
	}
	return discover(
		ctx,
		host,
		workspace,
		filepath.Join(local, "imageweave", "images"),
		execute,
		measure,
	)
}

func discover(
	ctx context.Context,
	host, workspace, local string,
	run command,
	check probe,
) (Inventory, error) {
	in := Inventory{Pools: map[string]uint64{}}
	raw, err := run(ctx, "diskutil", "list", "-plist")
	if err != nil {
		return in, err
	}
	var tree map[string]any
	if _, err = plist.Unmarshal(raw, &tree); err != nil {
		return in, fmt.Errorf("decode mounted storage: %w", err)
	}
	mounts := map[string]bool{}
	collectMounts(tree, mounts)
	raw, err = run(ctx, "hdiutil", "info", "-plist")
	if err != nil {
		return in, err
	}
	var images mountedImages
	if _, err = plist.Unmarshal(raw, &images); err != nil {
		return in, fmt.Errorf("decode backing storage: %w", err)
	}
	backing := map[string]string{}
	for _, img := range images.Images {
		for _, entity := range img.Entities {
			if entity.Mount != "" {
				backing[entity.Mount] = img.Path
				mounts[entity.Mount] = true
			}
		}
	}
	read := func(path, root string) Volume {
		v := Volume{Root: root, Mount: path}
		existing := ancestor(root)
		free, mount, e := check(existing)
		if e != nil {
			v.Reason = e.Error()
			return v
		}
		data, e := run(ctx, "diskutil", "info", "-plist", mount)
		if e != nil {
			v.Reason = e.Error()
			return v
		}
		var d diskInfo
		if _, e = plist.Unmarshal(data, &d); e != nil {
			v.Reason = "invalid volume metadata"
			return v
		}
		v.Mount, v.Filesystem, v.Internal = d.Mount, d.Filesystem, d.Internal
		if d.Pool == "" {
			d.Pool = d.Device
		}
		if d.Pool == "" || d.Mount == "" || !d.Writable {
			v.Reason = "volume is unmounted, read-only or unidentified"
			return v
		}
		v.Free = free
		v.Pools = []string{d.Pool}
		if n, ok := in.Pools[d.Pool]; !ok || v.Free < n {
			in.Pools[d.Pool] = v.Free
		}
		return v
	}
	in.Host = read(host, host)
	paths := make([]string, 0, len(mounts))
	for mount := range mounts {
		paths = append(paths, mount)
	}
	sort.Strings(paths)
	for _, mount := range paths {
		root := filepath.Join(mount, "weave-images", "builds", "macos")
		v := read(mount, root)
		// System-volume roots aren't user workspaces. The host's explicitly selected
		// scratch parent supplies the writable internal candidate instead.
		if v.Internal && (mount == "/" || strings.HasPrefix(mount, "/System/")) {
			continue
		}
		in.Volumes = append(in.Volumes, v)
	}
	hostVolume := read(local, local)
	in.Volumes = append(in.Volumes, hostVolume)
	if workspace != "" && workspace != "auto" {
		in.Volumes = append(in.Volumes, read(workspace, workspace))
	}
	// Resolve sparse disk-image backing paths recursively, rather than treating
	// their advertised virtual capacity as additional physical storage.
	var bind func(*Volume, map[string]bool)
	bind = func(v *Volume, seen map[string]bool) {
		path := backing[v.Mount]
		if path == "" || v.Reason != "" {
			return
		}
		if seen[v.Mount] {
			v.Reason = "cyclic backing storage"
			return
		}
		seen[v.Mount] = true
		parent := read(filepath.Dir(path), filepath.Dir(path))
		bind(&parent, seen)
		if parent.Reason != "" {
			v.Reason = "backing storage: " + parent.Reason
			return
		}
		for _, pool := range parent.Pools {
			found := false
			for _, existing := range v.Pools {
				if pool == existing {
					found = true
				}
			}
			if !found {
				v.Pools = append(v.Pools, pool)
			}
		}
		if parent.Free < v.Free {
			v.Free = parent.Free
		}
	}
	bind(&in.Host, map[string]bool{})
	for i := range in.Volumes {
		bind(&in.Volumes[i], map[string]bool{})
	}
	return in, nil
}

func collectMounts(v any, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			if k == "MountPoint" {
				if path, ok := value.(string); ok && filepath.IsAbs(path) {
					out[path] = true
				}
			}
			collectMounts(value, out)
		}
	case []any:
		for _, value := range x {
			collectMounts(value, out)
		}
	}
}

func ancestor(path string) string {
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}
