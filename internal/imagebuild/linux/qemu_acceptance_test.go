package linux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

func TestBaseRebootEvidenceAndState(t *testing.T) {
	fakeFirmware(t)
	for _, mode := range []string{"pass", "changed-identity", "reboot-crash", "missing-reboot-marker", "report-directory", "result-write"} {
		t.Run(mode, func(t *testing.T) {
			fake := &fakeQEMU{t: t}
			report := filepath.Join(t.TempDir(), "report")
			var overlay, variables string
			tools := Tools{
				Run: func(ctx context.Context, out, errOut io.Writer, name string, args ...string) error {
					if strings.HasPrefix(name, "qemu-system-") {
						for _, arg := range args {
							if strings.Contains(arg, "overlay.qcow2") {
								if overlay != "" && overlay != arg {
									t.Fatal("reboot replaced clone disk")
								}
								overlay = arg
							}
							if strings.Contains(arg, "vars.fd") {
								if variables != "" && variables != arg {
									t.Fatal("reboot replaced firmware")
								}
								variables = arg
							}
						}
						if fake.boots == 1 {
							switch mode {
							case "changed-identity":
								_, err := fmt.Fprintf(
									out,
									"%s-REBOOT machine-id=%032x\n",
									fake.marker,
									999,
								)
								return err
							case "reboot-crash":
								return errors.New("second boot failed")
							case "missing-reboot-marker":
								_, err := fmt.Fprintf(out, "%s machine-id=%032x\n", fake.marker, 1)
								return err
							case "result-write":
								must(t, os.Mkdir(filepath.Join(report, "result.json"), 0o700))
							}
						}
						if mode == "report-directory" {
							must(t, os.Mkdir(filepath.Join(report, "reboot"), 0o700))
						}
					}
					return fake.run(ctx, out, errOut, name, args...)
				},
			}
			result, err := tools.BootLinux(
				t.Context(),
				BootOptions{
					Bundle:       bootBundle(t, "arm64"),
					Report:       report,
					Timeout:      time.Second,
					VerifyReboot: true,
				},
			)
			if mode != "pass" {
				if err == nil || result.Passed {
					t.Fatalf("accepted %s: %+v %v", mode, result, err)
				}
				return
			}
			must(t, err)
			if !result.Passed || result.Profile != "base" || fake.boots != 2 ||
				result.Identities["machineID"] != result.MachineID ||
				len(result.Checks) != 7 {
				t.Fatalf("incomplete evidence: %+v boots=%d", result, fake.boots)
			}
			for check, passed := range result.Checks {
				if !passed {
					t.Fatal(check)
				}
			}
		})
	}
}

func TestBaseAcceptanceCannotProvision(t *testing.T) {
	for _, mode := range []string{"script", "payload", "output", "agent", "windows"} {
		t.Run(mode, func(t *testing.T) {
			o := BootOptions{
				Bundle:       bootBundle(t, "amd64"),
				Report:       filepath.Join(t.TempDir(), "report"),
				Timeout:      time.Second,
				VerifyReboot: true,
			}
			switch mode {
			case "script":
				o.Script = "echo injected"
			case "payload":
				o.Payload = "payload"
			case "output":
				o.OutputDisk = "child.raw"
			default:
				b, err := pack.LoadBundle(o.Bundle)
				must(t, err)
				if mode == "agent" {
					b.File.Guest.Variant = "agent"
					b.File.Provisioning.Agent = &spec.Agent{Name: "weave-agent", Version: "1.0.0"}
				} else {
					b.File.Guest.OS = "windows"
				}
				must(t, pack.WriteBundleFile(o.Bundle, b.File))
			}
			if _, err := (Tools{}).BootLinux(t.Context(), o); err == nil {
				t.Fatal("accepted " + mode)
			}
		})
	}
}

func TestBaseCredentialChecksExecute(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX guest script execution requires a POSIX host")
	}
	for _, mode := range []string{"clean", "family", "architecture", "build-user", "build-home", "build-seal", "cloud-seed", "authorized-key", "private-key", "sudo", "scan-error", "sudo-error"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range []string{"root", "home", "tmp", "var/lib/cloud/instances", "etc/sudoers.d"} {
				must(t, os.MkdirAll(filepath.Join(root, dir), 0o700))
			}
			b := pack.Bundle{
				File: pack.BundleFile{
					Guest: spec.Guest{
						Arch:      "amd64",
						Distro:    "ubuntu",
						OSVersion: "26.04",
						Variant:   "base",
					},
				},
			}
			script := baseCredentialChecks(b)
			script = strings.NewReplacer("/root", root+"/root", "/home", root+"/home", "/tmp/imageweave", root+"/tmp/imageweave", "/var/lib/cloud", root+"/var/lib/cloud", "/etc/sudoers.d", root+"/etc/sudoers.d").
				Replace(script)
			setup := "set -eu\nID=ubuntu\nuname() { echo x86_64; }\ngetent() { return 1; }\n"
			switch mode {
			case "family":
				setup += "ID=fedora\n"
			case "architecture":
				setup += "uname() { echo aarch64; }\n"
			case "build-user":
				setup += "getent() { return 0; }\n"
			case "build-home":
				must(t, os.Mkdir(filepath.Join(root, "home/imageweave"), 0o700))
			case "build-seal":
				must(
					t,
					os.WriteFile(
						filepath.Join(root, "tmp/imageweave-seal.sh"),
						[]byte("seal"),
						0o600,
					),
				)
			case "cloud-seed":
				must(
					t,
					os.Mkdir(
						filepath.Join(root, "var/lib/cloud/instances/imageweave-build"),
						0o700,
					),
				)
			case "authorized-key":
				must(
					t,
					os.WriteFile(
						filepath.Join(root, "root/authorized_keys"),
						[]byte("ssh-ed25519 leaked"),
						0o600,
					),
				)
			case "private-key":
				must(
					t,
					os.WriteFile(filepath.Join(root, "home/id_ed25519"), []byte("private"), 0o600),
				)
			case "sudo":
				must(
					t,
					os.WriteFile(
						filepath.Join(root, "etc/sudoers.d/90-cloud-init-users"),
						[]byte("imageweave ALL=(ALL) NOPASSWD:ALL"),
						0o600,
					),
				)
			case "scan-error":
				setup += "find() { return 1; }\n"
			case "sudo-error":
				setup += "grep() { return 2; }\n"
			}
			output, err := exec.CommandContext(t.Context(), "sh", "-c", setup+script).
				CombinedOutput()
			if (err == nil) != (mode == "clean") {
				t.Fatalf("%s: %v %s", mode, err, output)
			}
		})
	}
}

func TestRebootScriptSyntaxAndIdentityPersistence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX guest script execution requires a POSIX host")
	}
	b := pack.Bundle{
		File: pack.BundleFile{
			Guest: spec.Guest{Arch: "arm64", Distro: "fedora", OSVersion: "44", Variant: "base"},
		},
	}
	script := baseAcceptanceScript(b, "WEAVE-BOOT-OK-123456789012345678901234")
	cmd := exec.CommandContext(t.Context(), "sh", "-n")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script syntax: %s: %v", out, err)
	}
	for _, want := range []string{"After=cloud-final.service", "WantedBy=cloud-init.target", "test \"$weave_machine_id\" =", "systemctl enable imageweave-acceptance.service", "-REBOOT", "aarch64", "fedora", "agent-absent"} {
		if want == "agent-absent" {
			want = "if command -v weave-agent; then exit 1; fi"
		}
		// Embedded POSIX single quotes are escaped by common.ShellQuote.
		if !strings.Contains(script, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
