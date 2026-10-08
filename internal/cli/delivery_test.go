package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weaveplatform/imageweave/pkg/plan"
)

func TestDeliveryCommand(t *testing.T) {
	for _, mode := range []string{"success", "missing", "decode", "build", "write"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			file := func(name string) string {
				p := filepath.Join(dir, name)
				if err := os.WriteFile(p, []byte(name), 0o600); err != nil {
					t.Fatal(err)
				}
				return p
			}
			hash := sha256.Sum256([]byte("firmware"))
			firmware := plan.File{Path: file("firmware"), SHA256: hex.EncodeToString(hash[:])}
			request := plan.Request{SchemaVersion: 1, Family: "ubuntu", Release: "26.04", Arch: "arm64", Purpose: "guest-base", Target: "qemu", SourceURL: "https://vendor.example/image", SourceSHA256: strings.Repeat("a", 64), SourceBuild: "20261008", SSHPrivateKey: file("key"), SSHPublicKey: file("key.pub"), FirmwareCode: firmware, FirmwareVars: firmware, Accelerator: "tcg"}
			data, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			requestFile := filepath.Join(dir, "request.json")
			if mode == "decode" {
				data = []byte("unexpected: value")
			}
			if mode != "missing" {
				if err := os.WriteFile(requestFile, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := newDeliveryCommand(func(context.Context, string, []string, io.Writer) error {
				if mode == "build" {
					return errors.New("failed child")
				}
				return nil
			})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			if mode == "write" {
				cmd.SetOut(deliveryFailWriter{})
			}
			cmd.SetArgs([]string{"--request", requestFile, "--recipe-commit", strings.Repeat("b", 40), "--source-uri", "https://vendor.example/image", "--version", "26.04-test-r1", "--out", filepath.Join(dir, "out")})
			err = cmd.Execute()
			if mode == "success" {
				if err != nil || !strings.Contains(out.String(), "local-acceptance") {
					t.Fatal(err, out.String())
				}
			} else if err == nil {
				t.Fatal("expected failure")
			}
		})
	}
}

type deliveryFailWriter struct{}

func (deliveryFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
