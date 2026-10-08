// Package buildstorage plans native image jobs against independently measured
// capacity domains. Mounted disk images also consume their backing filesystem.
package buildstorage

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const GiB uint64 = 1 << 30

var ErrCapacity = errors.New("insufficient compatible build storage")

type Volume struct {
	Mount      string   `json:"mount"`
	Root       string   `json:"root"`
	Filesystem string   `json:"filesystem"`
	Free       uint64   `json:"freeBytes"`
	Pools      []string `json:"capacityPools"`
	Internal   bool     `json:"internal"`
	Reason     string   `json:"rejection,omitempty"`
}

type Inventory struct {
	Volumes []Volume          `json:"volumes"`
	Pools   map[string]uint64 `json:"capacityBytes"`
	Host    Volume            `json:"host"`
}

type Budget struct {
	Images  int    `json:"images"`
	Media   uint64 `json:"mediaBytesPerRelease"`
	Disk    uint64 `json:"diskBytes"`
	Host    uint64 `json:"hostScratchBytes"`
	Reserve uint64 `json:"reserveBytesPerPool"`
	Image   uint64 `json:"imagePeakBytes"`
}

// MacOS uses logical disk sizes, not optimistic sparse/compression ratios.
// Three copies remain per accepted image (Packer, imported bundle, OCI). One
// additional copy is live during clone acceptance or registry read-back. Clones
// are already unpacked and removed sequentially by the acceptance library.
// Media is an upper bound, added only for bytes still to download at that stage;
// existing media has already reduced measured free space.
func MacOS(images int) (Budget, error) {
	if images < 1 || images > 6 {
		return Budget{}, fmt.Errorf("image count must be between one and six")
	}
	b := Budget{Images: images, Media: 40 * GiB, Disk: 80 * GiB, Host: 4 * GiB, Reserve: 8 * GiB}
	b.Image = uint64(images)*3*b.Disk + b.Disk
	return b, nil
}

type Plan struct {
	Inventory  Inventory `json:"inventory"`
	Budget     Budget    `json:"budget"`
	Workspace  string    `json:"workspace,omitempty"`
	Scratch    string    `json:"scratch,omitempty"`
	MediaCache string    `json:"mediaCache,omitempty"`
	Serial     bool      `json:"sequential"`
}

func native(v Volume) bool { return v.Filesystem == "apfs" || v.Filesystem == "hfs" }

// Select chooses the compatible workspace with the most remaining capacity.
// Explicit workspaces are constraints; automatic selection never relocates an
// existing explicit workspace or counts APFS aliases as independent disks.
func Select(in Inventory, b Budget, workspace string) (Plan, error) {
	p := Plan{Inventory: in, Budget: b, Serial: true, Scratch: in.Host.Root}
	if b.Images < 1 || b.Image == 0 || b.Host == 0 || b.Reserve == 0 ||
		b.Image > ^uint64(0)-b.Host || b.Image+b.Host > ^uint64(0)-b.Reserve {
		return p, fmt.Errorf("invalid storage budget")
	}
	if in.Host.Reason != "" || !native(in.Host) {
		return p, fmt.Errorf(
			"%w: host scratch %s requires writable APFS/HFS+: %s",
			ErrCapacity,
			in.Host.Root,
			in.Host.Reason,
		)
	}
	candidates := append([]Volume(nil), in.Volumes...)
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Free != candidates[j].Free {
			return candidates[i].Free > candidates[j].Free
		}
		return candidates[i].Root < candidates[j].Root
	})
	var reasons []string
	for _, v := range candidates {
		if workspace != "" && workspace != "auto" && v.Root != workspace {
			continue
		}
		reason := v.Reason
		if reason == "" && !native(v) {
			reason = "native VM workspace requires APFS/HFS+"
		}
		if reason == "" {
			reason = capacity(in, b, v)
		}
		if reason != "" {
			reasons = append(reasons, fmt.Sprintf("%s (%s): %s", v.Root, v.Filesystem, reason))
			continue
		}
		p.Workspace = v.Root
		return p, nil
	}
	return p, fmt.Errorf(
		"%w: image peak %.1f GiB, host scratch %.1f GiB, reserve %.1f GiB per shared pool; %s",
		ErrCapacity,
		float64(b.Image)/float64(GiB),
		float64(b.Host)/float64(GiB),
		float64(b.Reserve)/float64(GiB),
		strings.Join(reasons, "; "),
	)
}

func capacity(in Inventory, b Budget, v Volume) string {
	need := map[string]uint64{}
	for _, pool := range v.Pools {
		need[pool] = b.Image
	}
	for _, pool := range in.Host.Pools {
		need[pool] += b.Host
	}
	if len(v.Pools) == 0 || len(in.Host.Pools) == 0 {
		return "capacity domain unavailable"
	}
	for pool, n := range need {
		available, ok := in.Pools[pool]
		if !ok || available < b.Reserve || n > available-b.Reserve {
			return fmt.Sprintf(
				"pool %s has %.1f GiB free; needs %.1f GiB including reserve",
				pool,
				float64(available)/float64(GiB),
				float64(n+b.Reserve)/float64(GiB),
			)
		}
	}
	return ""
}
