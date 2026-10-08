package macos

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/packer"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/weaveplatform/weaveplatform-oci/pkg/chunk"
	"github.com/weaveplatform/weaveplatform-oci/pkg/imagecheck"
	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

func parentFixture(t *testing.T) (Parent, spec.Config) {
	t.Helper()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "disk0.img"), make([]byte, 4096), 0o600))
	must(
		t,
		os.WriteFile(
			filepath.Join(dir, "auxstorage.bin"),
			[]byte("private writable firmware"),
			0o600,
		),
	)
	f := pack.BundleFile{
		SchemaVersion: 1,
		Annotations: map[string]string{
			spec.AnnotationVersion:  "test-r1",
			spec.AnnotationRevision: strings.Repeat("a", 40),
			spec.AnnotationSource:   "https://github.com/weaveplatform/imageweave",
		},
		Guest: spec.Guest{
			OS:        "darwin",
			Arch:      "arm64",
			OSVersion: "26.6.2",
			OSBuild:   "25G83",
			Variant:   "base",
		},
		Firmware: spec.Firmware{Type: "apple", TPM: "none", HardwareModel: "YnBsaXN0MDA="},
		Resources: spec.Resources{
			CPU:    spec.MinDefault{Min: 2, Default: 4},
			Memory: spec.MinDefault{Min: 4 << 30, Default: 4 << 30},
		},
		Provisioning: spec.Provisioning{CredentialHint: "set-at-first-boot"},
		Build: spec.Build{
			Template:    "templates/macos/image.pkr.hcl",
			TemplateRef: "repo@commit",
			Created:     "2026-10-08T00:00:00Z",
			SourceMedia: []spec.SourceMedia{
				{
					URI:    "https://vendor.example/base",
					Digest: "sha256:" + strings.Repeat("a", 64),
					Kind:   "ipsw",
				},
			},
		},
		Disks: []pack.BundleDisk{{Name: "disk0", Role: "system", Path: "disk0.img"}},
		State: []pack.BundleState{
			{
				Name:      spec.StateAuxStorage,
				Path:      "auxstorage.bin",
				Semantics: spec.SemanticsCarry,
				Required:  true,
			},
		},
	}
	must(t, pack.WriteBundleFile(dir, f))
	b, err := pack.LoadBundle(dir)
	must(t, err)
	layout := filepath.Join(t.TempDir(), "layout")
	store, err := pack.OpenLayout(t.Context(), layout)
	must(t, err)
	child, err := pack.Manifest(t.Context(), b, store, chunk.Options{})
	must(t, err)
	root, err := pack.Index(t.Context(), store, []ocispec.Descriptor{child}, nil)
	must(t, err)
	must(t, store.Tag(t.Context(), root, "base"))
	description, err := pack.Describe(t.Context(), store, child)
	must(t, err)
	return Parent{
		Layout: layout,
		Ref:    "base",
		Name:   "ghcr.io/example/macos-26-base",
		Digest: child.Digest.String(),
	}, description.Config
}

type fakeCommunicator struct {
	calls   int
	fail    int
	scripts []string
	output  string
}

func (c *fakeCommunicator) Start(_ context.Context, cmd *packer.RemoteCmd) error {
	c.calls++
	c.scripts = append(c.scripts, cmd.Command)
	if c.calls == c.fail {
		return ErrPrepared
	}
	if cmd.Stdout != nil {
		_, _ = io.WriteString(cmd.Stdout, c.output)
	}
	cmd.SetExited(0)
	return nil
}
func (*fakeCommunicator) Upload(string, io.Reader, *os.FileInfo) error { return nil }
func (*fakeCommunicator) UploadDir(string, string, []string) error     { return nil }
func (*fakeCommunicator) Download(string, io.Writer) error             { return nil }
func (*fakeCommunicator) DownloadDir(string, string, []string) error   { return nil }

type fakeSession struct {
	comm        fakeCommunicator
	calls       []string
	fail        string
	observation Observation
	second      *Observation
	observed    int
	setup       []bool
}

func (v *fakeSession) step(name string) error {
	v.calls = append(v.calls, name)
	if v.fail == name {
		return ErrPrepared
	}
	return nil
}

func (v *fakeSession) Connect(_ context.Context, setup bool) (packer.Communicator, error) {
	v.setup = append(v.setup, setup)
	return &v.comm, v.step(fmt.Sprintf("connect%d", len(v.setup)))
}

func (v *fakeSession) Observe(_ context.Context, marker string) (Observation, error) {
	v.observed++
	o := v.observation
	if v.observed == 2 && v.second != nil {
		o = *v.second
	}
	o.Marker = marker
	return o, v.step(fmt.Sprintf("observe%d", v.observed))
}
func (v *fakeSession) Restart(context.Context) error { return v.step("restart") }
func (v *fakeSession) Stop(context.Context) error    { return v.step("stop") }
func (v *fakeSession) Close() error                  { return v.step("close") }
func (v *fakeSession) Identities() map[string]string {
	return map[string]string{"machineIdentifier": "native-id", "macAddress": "02:01:02:03:04:05"}
}

func goodObservation() Observation {
	return Observation{
		Version:           "26.6.2",
		Build:             "25G83",
		HardwareUUID:      "hardware-uuid",
		ConsoleUser:       "weave",
		Groups:            "staff admin",
		AutoLoginUser:     "weave",
		SetupComplete:     true,
		AgentAbsent:       true,
		BuildAccessAbsent: true,
		HostKeyDigest:     "sha256:" + strings.Repeat("b", 64),
	}
}

func factory(v *fakeSession) StartSession {
	return func(context.Context, string, spec.Config, io.Writer) (Session, error) { return v, nil }
}

func TestPreparedLifecycle(t *testing.T) {
	p, _ := parentFixture(t)
	v := &fakeSession{observation: goodObservation()}
	out := filepath.Join(t.TempDir(), "prepared")
	result, err := Prepare(
		t.Context(),
		p,
		out,
		factory(v),
		func(_ context.Context, c packer.Communicator) error {
			if c != &v.comm {
				t.Fatal("wrong Packer communicator")
			}
			return nil
		},
		io.Discard,
	)
	must(t, err)
	if result.Parent.Digest != p.Digest || result.Observation.ConsoleUser != "weave" ||
		strings.Join(v.calls, ",") != "connect1,restart,connect2,observe1,stop,close" ||
		!v.setup[0] ||
		v.setup[1] {
		t.Fatal(result, v.calls, v.setup)
	}
	if len(v.comm.scripts) != 2 || !strings.Contains(v.comm.scripts[1], "ssh_host_") ||
		strings.Contains(v.comm.scripts[1], "dslocal") {
		t.Fatal(v.comm.scripts)
	}
	if _, err := os.Stat(filepath.Join(out, "disk0.img")); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedFailuresCloseRuntime(t *testing.T) {
	p, _ := parentFixture(t)
	for _, step := range []string{"start", "connect1", "provision", "configure", "restart", "connect2", "observe1", "invalid observation", "seal", "stop", "close"} {
		t.Run(step, func(t *testing.T) {
			v := &fakeSession{observation: goodObservation(), fail: step}
			if step == "invalid observation" {
				v.observation.AgentAbsent = false
			}
			if step == "configure" {
				v.comm.fail = 1
			}
			if step == "seal" {
				v.comm.fail = 2
			}
			start := factory(v)
			if step == "start" {
				start = func(context.Context, string, spec.Config, io.Writer) (Session, error) { return nil, ErrPrepared }
			}
			_, err := Prepare(
				t.Context(),
				p,
				filepath.Join(t.TempDir(), "output"),
				start,
				func(context.Context, packer.Communicator) error {
					if step == "provision" {
						return ErrPrepared
					}
					return nil
				},
				nil,
			)
			if err == nil {
				t.Fatal("accepted failure", step)
			}
			if step != "start" && v.calls[len(v.calls)-1] != "close" {
				t.Fatal("leaked runtime", v.calls)
			}
		})
	}
	if _, err := Prepare(t.Context(), p, "", nil, nil, nil); err == nil {
		t.Fatal("nil adapters accepted")
	}
}

func TestParentRejectsMissingOrDifferentInputs(t *testing.T) {
	p, _ := parentFixture(t)
	for _, change := range []func(*Parent){func(p *Parent) { p.Digest = "bad" }, func(p *Parent) { p.Layout = "absent" }, func(p *Parent) { p.Ref = "absent" }, func(p *Parent) { p.Digest = "sha256:" + strings.Repeat("a", 64) }} {
		copy := p
		change(&copy)
		if _, err := unpackParent(
			t.Context(),
			copy,
			filepath.Join(t.TempDir(), "copy"),
		); err == nil {
			t.Fatal(copy)
		}
	}
	if _, err := unpackParent(t.Context(), p, t.TempDir()); err == nil {
		t.Fatal("overwrote existing directory")
	}
	must(t, os.WriteFile(filepath.Join(p.Layout, "index.json"), []byte("corrupt"), 0o600))
	if _, err := unpackParent(t.Context(), p, filepath.Join(t.TempDir(), "copy")); err == nil {
		t.Fatal("accepted corrupt index")
	}
}

func TestMacOSAcceptanceNeverRepairsPreparedClone(t *testing.T) {
	_, cfg := parentFixture(t)
	for _, variant := range []string{"base", "prepared"} {
		cfg.Guest.Variant = variant
		v := &fakeSession{observation: goodObservation()}
		b, err := AcceptClone(
			t.Context(),
			imagecheck.Clone{Config: cfg, Marker: "challenge"},
			factory(v),
			io.Discard,
		)
		must(t, err)
		// Fake lifecycle operations may finish within one Windows clock tick.
		if !b.Passed || b.MachineID != "hardware-uuid" || b.ElapsedSeconds < 0 ||
			v.setup[0] != (variant == "base") ||
			v.setup[1] {
			t.Fatal(b, v.setup)
		}
		if variant == "prepared" &&
			(len(v.comm.scripts) != 0 || len(b.Operations) != 7 || len(b.RebootOperations) != 7) {
			t.Fatal("acceptance repaired prepared guest", v.comm.scripts, b)
		}
	}
	for _, step := range []string{"connect1", "observe1", "restart", "connect2", "observe2", "stop", "close"} {
		v := &fakeSession{observation: goodObservation(), fail: step}
		b, err := AcceptClone(
			t.Context(),
			imagecheck.Clone{Config: cfg, Marker: "challenge"},
			factory(v),
			nil,
		)
		if err == nil || b.Passed {
			t.Fatal(step, b, err)
		}
	}
	for _, field := range []string{"uuid", "key", "account"} {
		second := goodObservation()
		if field == "uuid" {
			second.HardwareUUID = "different"
		}
		if field == "key" {
			second.HostKeyDigest = "different"
		}
		if field == "account" {
			second.ConsoleUser = "other"
		}
		v := &fakeSession{observation: goodObservation(), second: &second}
		b, err := AcceptClone(
			t.Context(),
			imagecheck.Clone{Config: cfg, Marker: "challenge"},
			factory(v),
			nil,
		)
		if err == nil || b.Passed {
			t.Fatal(field, b, err)
		}
	}
}

func TestObservationAndLeases(t *testing.T) {
	_, cfg := parentFixture(t)
	o := goodObservation()
	o.Marker = "challenge"
	must(t, o.Check(cfg, "challenge"))
	for _, change := range []func(*Observation){func(o *Observation) { o.Groups = "staff" }, func(o *Observation) { o.Build = "other" }, func(o *Observation) { o.SetupComplete = false }, func(o *Observation) { o.BuildAccessAbsent = false }, func(o *Observation) { o.Marker = "wrong" }} {
		copy := o
		change(&copy)
		if copy.Check(cfg, "challenge") == nil {
			t.Fatal(copy)
		}
	}
	o, err := parseObservation(
		"26.6.2\n25G83\nchallenge\nhardware-uuid\nweave\nstaff admin\nweave\nyes\nyes\nyes\n",
		"sha256:key",
	)
	must(t, err)
	must(t, o.Check(cfg, "challenge"))
	if _, err := parseObservation("truncated", ""); err == nil {
		t.Fatal("incomplete probe accepted")
	}
	lease := "{\n hw_address=1,2:a:0:1:2:3\n ip_address=192.168.64.2\n}\n"
	ip, err := parseLease(lease, "02:0a:00:01:02:03")
	must(t, err)
	if ip != "192.168.64.2" {
		t.Fatal(ip)
	}
	for _, mac := range []string{"invalid", "02:00:00:00:00:00"} {
		if _, err := parseLease(lease, mac); err == nil {
			t.Fatal(mac)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if pause(ctx, time.Hour) == nil {
		t.Fatal("cancelled wait succeeded")
	}
	must(t, pause(t.Context(), 0))
}
