package catalog_test

import (
	"errors"
	"testing"

	"github.com/weaveplatform/imageweave/pkg/catalog"
)

func TestReleaseContract(t *testing.T) {
	c := catalog.Current()
	if c.SchemaVersion != 1 || c.ReviewedAt != "2026-10-07" || len(c.Matrix()) != 24 {
		t.Fatalf("unexpected catalog: %+v", c)
	}
	want := map[string][]string{
		"windows-11": {"26H2", "25H2", "24H2"},
		"ubuntu":     {"26.04", "24.04", "22.04"},
		"macos":      {"27", "26", "15"},
		"fedora":     {"44", "43", "42"},
	}
	slots := []string{"n", "n-1", "n-2"}
	for family, releases := range want {
		for i, version := range releases {
			for _, arch := range []string{"amd64", "arm64"} {
				s, err := c.Resolve(family, slots[i], arch)
				if err != nil || s.Version != version || s.Arch != arch {
					t.Fatalf("%s/%s/%s: %+v %v", family, slots[i], arch, s, err)
				}
				exact, err := c.Resolve(family, version, arch)
				if err != nil || exact != s {
					t.Fatalf("exact resolution changed: %+v %v", exact, err)
				}
				_, err = s.Template("guest-base", "qemu")
				if family == "ubuntu" || family == "fedora" {
					if err != nil {
						t.Fatal(err)
					}
				} else if !errors.Is(err, catalog.ErrBuild) {
					t.Fatalf("unimplemented build accepted: %v", err)
				}
			}
		}
	}
}

func TestExceptionsAndInvalidSelections(t *testing.T) {
	c := catalog.Current()
	s, err := c.Resolve("macos", "N", "amd64")
	if err != nil || s.Status != "unsupported" {
		t.Fatalf("%+v %v", s, err)
	}
	s, err = c.Resolve("fedora", "n-2", "arm64")
	if err != nil || s.Maintenance != "archived" {
		t.Fatalf("%+v %v", s, err)
	}
	s, err = c.Resolve("ubuntu", "n", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"agent", "qemu"}, {"guest-base", "aws"}} {
		if _, err := s.Template(pair[0], pair[1]); !errors.Is(err, catalog.ErrBuild) {
			t.Fatal(err)
		}
	}
	for _, args := range [][3]string{{"debian", "n", "amd64"}, {"ubuntu", "20.04", "amd64"}, {"ubuntu", "n", "x86"}, {"windows-11", "26H1", "arm64"}, {"fedora", "45", "amd64"}} {
		if _, err := c.Resolve(args[0], args[1], args[2]); !errors.Is(err, catalog.ErrSelection) {
			t.Fatal(err)
		}
	}
	c.Families[0].Releases[0].Version = "changed"
	s, err = catalog.Current().Resolve("windows-11", "n", "arm64")
	if err != nil || s.Version != "26H2" {
		t.Fatalf("global catalog mutated: %+v %v", s, err)
	}
}
