package plan_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/weaveplatform/imageweave/pkg/catalog"
	"github.com/weaveplatform/imageweave/pkg/plan"
)

func request(t *testing.T) plan.Request {
	t.Helper()
	dir := t.TempDir()
	firmware := []byte("test-firmware")
	sum := sha256.Sum256(firmware)
	hash := hex.EncodeToString(sum[:])
	for name, content := range map[string][]byte{"code.fd": firmware, "vars.fd": firmware, "key": []byte("secret-key"), "key.pub": []byte("public-key")} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return plan.Request{
		SchemaVersion: 1,
		Family:        "ubuntu",
		Release:       "N",
		Arch:          "arm64",
		Purpose:       "guest-base",
		Target:        "qemu",
		SourceURL:     "https://vendor.example/build/disk.img",
		SourceSHA256:  strings.Repeat("a", 64),
		SourceBuild:   "20261007.1",
		Workspace:     filepath.Join(dir, "work"),
		SSHPrivateKey: filepath.Join(dir, "key"),
		SSHPublicKey:  filepath.Join(dir, "key.pub"),
		FirmwareCode:  plan.File{Path: filepath.Join(dir, "code.fd"), SHA256: hash},
		FirmwareVars:  plan.File{Path: filepath.Join(dir, "vars.fd"), SHA256: hash},
		Accelerator:   "hvf",
	}
}

func TestResolveConcretePlan(t *testing.T) {
	r := request(t)
	p, err := plan.Resolve(r)
	if err != nil {
		t.Fatal(err)
	}
	if p.Selection.Version != "26.04" || p.Template != "templates/qemu" ||
		p.Qualification != "unverified" {
		t.Fatalf("%+v", p)
	}
	if p.Variables["release"] != "26.04" || p.Variables["source_sha256"] != r.SourceSHA256 {
		t.Fatalf("%+v", p.Variables)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret-key") {
		t.Fatal("private key bytes leaked")
	}
	for _, s := range catalog.Current().Matrix() {
		if s.Family != "ubuntu" && s.Family != "fedora" {
			continue
		}
		r.Family = s.Family
		r.Release = s.Slot
		r.Arch = s.Arch
		r.Accelerator = "tcg"
		p, err = plan.Resolve(r)
		if err != nil || p.Selection.Version != s.Version {
			t.Fatalf("%+v %v", p, err)
		}
	}
}

func TestStrictYAML(t *testing.T) {
	r := request(t)
	b, err := yaml.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := plan.Decode(strings.NewReader(string(b)))
	if err != nil || got != r {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"", "[", string(b) + "unknown: true\n", string(b) + "family: fedora\n", string(b) + "---\n{}\n", string(b) + "---\n", string(b) + "---\n[\n"} {
		if _, err := plan.Decode(strings.NewReader(bad)); !errors.Is(err, plan.ErrInput) {
			t.Fatalf("accepted invalid input %q: %v", bad, err)
		}
	}
}

func TestRejectInvalidInputs(t *testing.T) {
	cases := map[string]func(*plan.Request){
		"schema":    func(r *plan.Request) { r.SchemaVersion = 2 },
		"build":     func(r *plan.Request) { r.SourceBuild = "" },
		"digest":    func(r *plan.Request) { r.SourceSHA256 = "latest" },
		"uppercase": func(r *plan.Request) { r.SourceSHA256 = strings.Repeat("A", 64) },
		"url":       func(r *plan.Request) { r.SourceURL = "://" },
		"scheme":    func(r *plan.Request) { r.SourceURL = "http://vendor.example/disk" },
		"host":      func(r *plan.Request) { r.SourceURL = "https:///disk" },
		"user":      func(r *plan.Request) { r.SourceURL = "https://user:secret@vendor.example/disk" },
		"query":     func(r *plan.Request) { r.SourceURL += "?token=secret" },
		"fragment":  func(r *plan.Request) { r.SourceURL += "#tag" },
		"workspace": func(r *plan.Request) { r.Workspace = "relative" },
		"path": func(r *plan.Request) {
			r.Workspace = filepath.Join(r.Workspace, "x") + string(filepath.Separator) + ".."
		},
		"newline":     func(r *plan.Request) { r.Workspace += "\n" },
		"samekey":     func(r *plan.Request) { r.SSHPublicKey = r.SSHPrivateKey },
		"accelerator": func(r *plan.Request) { r.Accelerator = "auto" },
		"codehash":    func(r *plan.Request) { r.FirmwareCode.SHA256 = "bad" },
		"varshash":    func(r *plan.Request) { r.FirmwareVars.SHA256 = strings.Repeat("0", 64) },
		"missingcode": func(r *plan.Request) { r.FirmwareCode.Path += ".missing" },
		"directory":   func(r *plan.Request) { r.FirmwareCode.Path = filepath.Dir(r.FirmwareCode.Path) },
		"missingkey":  func(r *plan.Request) { r.SSHPrivateKey += ".missing" },
		"emptykey":    func(r *plan.Request) { os.WriteFile(r.SSHPrivateKey, nil, 0o600) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := request(t)
			change(&r)
			if _, err := plan.Resolve(r); !errors.Is(err, plan.ErrInput) {
				t.Fatalf("accepted invalid input: %v", err)
			}
		})
	}
}

func TestRejectUnimplementedScenario(t *testing.T) {
	r := request(t)
	r.Family = "windows-11"
	if _, err := plan.Resolve(r); !errors.Is(err, catalog.ErrBuild) {
		t.Fatal(err)
	}
	r.Family = "macos"
	if _, err := plan.Resolve(r); !errors.Is(err, catalog.ErrBuild) {
		t.Fatal(err)
	}
	r.Family = "missing"
	if _, err := plan.Resolve(r); !errors.Is(err, catalog.ErrSelection) {
		t.Fatal(err)
	}
}

func TestLocalSource(t *testing.T) {
	r := request(t)
	r.SourceURL = r.FirmwareCode.Path
	r.SourceSHA256 = r.FirmwareCode.SHA256
	if _, err := plan.Resolve(r); err != nil {
		t.Fatal(err)
	}
	r.SourceSHA256 = strings.Repeat("0", 64)
	if _, err := plan.Resolve(r); !errors.Is(err, plan.ErrInput) {
		t.Fatal(err)
	}
}
