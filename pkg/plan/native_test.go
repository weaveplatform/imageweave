package plan_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/weaveplatform/imageweave/pkg/catalog"
	"github.com/weaveplatform/imageweave/pkg/plan"
)

func TestNativePlans(t *testing.T) {
	for _, family := range []string{"macos", "windows-11"} {
		r := request(t)
		r.Family = family
		r.Release = "n-1"
		r.SourceURL = r.FirmwareCode.Path
		r.SourceSHA256 = r.FirmwareCode.SHA256
		r.Target = "apple-vz"
		r.SourceBuild = "25G83"
		if family == "windows-11" {
			r.Target = "hcs"
			r.Edition = "enterprise"
			r.Language = "en-US"
			r.SourceBuild = "26200.1"
		}
		p, err := plan.Resolve(r)
		if err != nil {
			t.Fatal(err)
		}
		if p.Variables["timeout"] != "2h" || p.Qualification != "unverified" ||
			p.Variables["output_directory"] != filepath.Join(r.Workspace, "candidate") {
			t.Fatal(p)
		}
		if _, ok := p.Variables["ssh_private_key_file"]; ok {
			t.Fatal("native plan has SSH credentials")
		}
		for _, change := range []func(*plan.Request){
			func(r *plan.Request) { r.SchemaVersion = 2 },
			func(r *plan.Request) { r.Release = "bad" },
			func(r *plan.Request) { r.Target = "qemu" },
			func(r *plan.Request) { r.Timeout = "bad" },
			func(r *plan.Request) { r.Workspace = "relative" },
			func(r *plan.Request) { r.SourceSHA256 = "bad" },
			func(r *plan.Request) { r.SourceURL += "missing" },
		} {
			bad := r
			change(&bad)
			if _, err := plan.Resolve(bad); err == nil {
				t.Fatal("accepted invalid native request")
			}
		}
	}
	r := request(t)
	r.Family = "macos"
	r.Arch = "amd64"
	r.Target = "apple-vz"
	if _, err := plan.Resolve(r); !errors.Is(err, catalog.ErrBuild) {
		t.Fatal("Intel VZ allowed", err)
	}
}
