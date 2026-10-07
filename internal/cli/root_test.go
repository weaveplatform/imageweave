package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/weaveplatform/imageweave/internal/cli"
	"github.com/weaveplatform/imageweave/pkg/catalog"
	"github.com/weaveplatform/imageweave/pkg/plan"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := cli.New(&out, &out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestMatrixAndErrors(t *testing.T) {
	out, err := run(t, "matrix")
	if err != nil {
		t.Fatal(err)
	}
	var matrix []catalog.Selection
	if err := json.Unmarshal([]byte(out), &matrix); err != nil || len(matrix) != 24 {
		t.Fatalf("%s %v", out, err)
	}
	for _, args := range [][]string{{"matrix", "extra"}, {"plan"}, {"plan", "--request", filepath.Join(t.TempDir(), "missing")}, {"bogus"}} {
		if _, err := run(t, args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	out, err = run(t, "--version")
	if err != nil || !strings.Contains(out, "devel") {
		t.Fatalf("%s %v", out, err)
	}
	out, err = run(t)
	if err != nil || !strings.Contains(out, "matrix") {
		t.Fatalf("%s %v", out, err)
	}
	out, err = run(t, "--help")
	if err != nil || !strings.Contains(out, "matrix") {
		t.Fatalf("%s %v", out, err)
	}
}

func TestPlan(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	h := sha256.Sum256([]byte("firmware"))
	hash := hex.EncodeToString(h[:])
	r := plan.Request{
		SchemaVersion: 1,
		Family:        "ubuntu",
		Release:       "n-1",
		Arch:          "amd64",
		Purpose:       "guest-base",
		Target:        "qemu",
		SourceURL:     "https://vendor.example/pinned.img",
		SourceSHA256:  strings.Repeat("a", 64),
		SourceBuild:   "build-123",
		Workspace:     filepath.Join(dir, "work"),
		SSHPrivateKey: write("key", "private-value"),
		SSHPublicKey:  write("key.pub", "public-value"),
		Accelerator:   "tcg",
		FirmwareCode:  plan.File{Path: write("code", "firmware"), SHA256: hash},
		FirmwareVars:  plan.File{Path: write("vars", "firmware"), SHA256: hash},
	}
	b, err := yaml.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	path := write("request.yaml", string(b))
	out, err := run(t, "plan", "--request", path)
	if err != nil || !strings.Contains(out, "24.04") || strings.Contains(out, "private-value") {
		t.Fatalf("%s %v", out, err)
	}
	out, err = run(t, "plan", "--request", path, "--vars")
	if err != nil || !strings.Contains(out, "source_sha256") ||
		strings.Contains(out, "qualification") {
		t.Fatalf("%s %v", out, err)
	}
	write("request.yaml", "unknown: true")
	if _, err := run(t, "plan", "--request", path); err == nil {
		t.Fatal("bad YAML accepted")
	}
	write("request.yaml", "schemaVersion: 2")
	if _, err := run(t, "plan", "--request", path); err == nil {
		t.Fatal("invalid plan accepted")
	}
}

var errWrite = errors.New("test output failure")

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestOutputFailure(t *testing.T) {
	cmd := cli.New(brokenWriter{}, brokenWriter{})
	cmd.SetArgs([]string{"matrix"})
	if err := cmd.Execute(); !errors.Is(err, errWrite) {
		t.Fatal(err)
	}
}
