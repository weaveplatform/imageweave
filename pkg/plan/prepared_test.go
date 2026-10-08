package plan

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPreparedPlans(t *testing.T) {
	r := Request{
		SchemaVersion: 1,
		Family:        "macos",
		Release:       "26",
		Arch:          "arm64",
		Purpose:       "guest-prepared",
		Target:        "apple-vz",
		SourceBuild:   "25G83",
		Workspace:     filepath.Join(t.TempDir(), "build"),
		Parent: &Parent{
			Layout: filepath.Join(t.TempDir(), "parent"),
			Ref:    "base",
			Name:   "ghcr.io/example/macos-26-base",
			Digest: "sha256:" + strings.Repeat("a", 64),
		},
	}
	for _, release := range []string{"15", "26", "27", "n", "n-1", "n-2"} {
		r.Release = release
		p, err := Resolve(r)
		if err != nil {
			t.Fatal(err)
		}
		if p.Template != "templates/macos/prepared" || p.Qualification != "unverified" ||
			p.Variables["parent_digest"] != r.Parent.Digest {
			t.Fatal(p)
		}
	}
	for name, change := range map[string]func(*Request){
		"family": func(r *Request) { r.Family = "windows-11" }, "arch": func(r *Request) { r.Arch = "amd64" }, "target": func(r *Request) { r.Target = "qemu" }, "schema": func(r *Request) { r.SchemaVersion = 2 }, "parent": func(r *Request) { r.Parent = nil }, "digest": func(r *Request) { r.Parent.Digest = "sha256:bad" }, "name": func(r *Request) { r.Parent.Name = "" }, "ref": func(r *Request) { r.Parent.Ref = "" }, "source build": func(r *Request) { r.SourceBuild = "" }, "media": func(r *Request) { r.SourceURL = "/media.ipsw" }, "source hash": func(r *Request) { r.SourceSHA256 = strings.Repeat("a", 64) }, "ssh private": func(r *Request) { r.SSHPrivateKey = "/key" }, "ssh public": func(r *Request) { r.SSHPublicKey = "/key.pub" }, "layout": func(r *Request) { r.Parent.Layout = "relative" }, "workspace": func(r *Request) { r.Workspace = "relative" }, "timeout": func(r *Request) { r.Timeout = "bad" }, "negative timeout": func(r *Request) { r.Timeout = "-1s" }, "base with parent": func(r *Request) { r.Purpose = "guest-base" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := r
			p := *r.Parent
			copy.Parent = &p
			change(&copy)
			if _, err := Resolve(copy); err == nil {
				t.Fatal("accepted invalid prepared request")
			}
		})
	}
}
