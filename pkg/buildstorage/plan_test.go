package buildstorage

import (
	"errors"
	"testing"
)

func inventory() Inventory {
	return Inventory{
		Host: Volume{
			Root:       "/host",
			Mount:      "/",
			Filesystem: "apfs",
			Free:       100 * GiB,
			Pools:      []string{"host"},
		},
		Volumes: []Volume{
			{Root: "/small", Filesystem: "apfs", Free: 100 * GiB, Pools: []string{"host"}},
			{Root: "/large", Filesystem: "apfs", Free: 600 * GiB, Pools: []string{"large"}},
			{Root: "/other", Filesystem: "apfs", Free: 500 * GiB, Pools: []string{"other"}},
		},
		Pools: map[string]uint64{"host": 100 * GiB, "large": 600 * GiB, "other": 500 * GiB},
	}
}

func TestBudget(t *testing.T) {
	for _, n := range []int{0, 7} {
		if _, err := MacOS(n); err == nil {
			t.Fatal("invalid count")
		}
	}
	one, _ := MacOS(1)
	all, _ := MacOS(6)
	if one.Image != 360*GiB || all.Image != 1760*GiB || one.Host != 4*GiB || one.Reserve != 8*GiB {
		t.Fatal(one, all)
	}
}

func TestSelection(t *testing.T) {
	b, _ := MacOS(1)
	for _, mode := range []string{"largest", "explicit", "full", "host full", "host rejected", "host filesystem", "image rejected", "image filesystem", "shared", "no pools", "unknown pool", "tie", "bad budget", "overflow", "missing explicit"} {
		t.Run(mode, func(t *testing.T) {
			in := inventory()
			request := "auto"
			want := "/large"
			budget := b
			switch mode {
			case "explicit":
				request = "/other"
				want = "/other"
			case "full":
				in.Pools["large"] = 1
				in.Pools["other"] = 1
				want = ""
			case "host full":
				in.Pools["host"] = 1
				want = ""
			case "host rejected":
				in.Host.Reason = "read only"
				want = ""
			case "host filesystem":
				in.Host.Filesystem = "exfat"
				want = ""
			case "image rejected":
				in.Volumes[1].Reason = "read only"
				want = "/other"
			case "image filesystem":
				in.Volumes[1].Filesystem = "exfat"
				want = "/other"
			case "shared":
				in.Host.Pools = []string{"large"}
				in.Pools["large"] = b.Image + b.Reserve
				in.Volumes = in.Volumes[1:2]
				want = ""
			case "no pools":
				in.Volumes[1].Pools = nil
				want = "/other"
			case "unknown pool":
				delete(in.Pools, "large")
				want = "/other"
			case "tie":
				in.Volumes[2].Free = in.Volumes[1].Free
			case "bad budget":
				budget.Image = 0
				want = ""
			case "overflow":
				budget.Image = ^uint64(0)
				want = ""
			case "missing explicit":
				request = "/missing"
				want = ""
			}
			p, err := Select(in, budget, request)
			if (err != nil) != (want == "") || p.Workspace != want {
				t.Fatalf("%+v %v want %s", p, err, want)
			}
			if !p.Serial {
				t.Fatal("parallel plan")
			}
			if want == "" && mode != "bad budget" && mode != "overflow" &&
				!errors.Is(err, ErrCapacity) {
				t.Fatal(err)
			}
		})
	}
}
