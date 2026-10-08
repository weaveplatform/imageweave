package linux

import (
	"context"
	"fmt"
	"path/filepath"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
)

// The checker is installed only in the disposable overlay. It runs after
// cloud-final on the next boot, so observing a second marker proves the same
// disk and firmware booted again without interrupting cloud-init shutdown.
func baseAcceptanceScript(b pack.Bundle, marker string) string {
	checks := baseCredentialChecks(b)
	reboot := "#!/bin/sh\n" + bootScript(b, marker+"-REBOOT", checks+
		"test \"$weave_machine_id\" = \"$(cat /var/lib/imageweave-acceptance/machine-id)\"\n") +
		"sync\nsystemctl --no-block poweroff\n"
	unit := "[Unit]\nDescription=Imageweave disposable clone reboot acceptance\nAfter=cloud-final.service\n[Service]\nType=oneshot\nExecStart=/bin/sh /usr/local/sbin/imageweave-acceptance-reboot\n[Install]\nWantedBy=cloud-init.target\n"
	return checks +
		"install -d -m 700 /var/lib/imageweave-acceptance\n" +
		"printf '%s\\n' \"$weave_machine_id\" > /var/lib/imageweave-acceptance/machine-id\n" +
		"printf '%s' " + common.ShellQuote(reboot) + " > /usr/local/sbin/imageweave-acceptance-reboot\n" +
		"printf '%s' " + common.ShellQuote(unit) + " > /etc/systemd/system/imageweave-acceptance.service\n" +
		"systemctl enable imageweave-acceptance.service\n"
}

func baseCredentialChecks(b pack.Bundle) string {
	arch := "x86_64"
	if b.File.Guest.Arch == "arm64" {
		arch = "aarch64"
	}
	return "test \"$ID\" = " + common.ShellQuote(b.File.Guest.Distro) + "\n" +
		"test \"$(uname -m)\" = " + common.ShellQuote(arch) + "\n" +
		"for user in imageweave weave-build; do\n" +
		"  if getent passwd \"$user\" >/dev/null; then exit 1; fi\n" +
		"  test ! -e \"/home/$user\"\n" +
		"done\n" +
		"test ! -e /tmp/imageweave-seal.sh\n" +
		"test ! -d /var/lib/cloud/instances/imageweave-build\n" +
		"weave_credentials=$(find -L /root /home -type f \\( -name authorized_keys -o -name authorized_keys2 -o -name id_rsa -o -name id_ecdsa -o -name id_ed25519 -o -name build-key \\) -size +0c -print -quit)\n" +
		"test -z \"$weave_credentials\"\n" +
		"if grep -R -E 'imageweave|weave-build' /etc/sudoers.d; then exit 1; else test \"$?\" -eq 1; fi\n"
}

func (t Tools) verifyBaseReboot(
	ctx context.Context,
	o BootOptions,
	work, code, arch string,
	result *BootResult,
) error {
	// A first boot pass must never survive a failed reboot or report write.
	result.Passed = false
	second := *result
	second.Marker += "-REBOOT"
	second.MachineID, second.Error = "", ""
	second.ElapsedSeconds = 0
	o.Report = filepath.Join(o.Report, "reboot")
	if err := common.NewDirectory(o.Report); err != nil {
		return fmt.Errorf("linux reboot acceptance: %w", err)
	}
	t.progress(
		"boot linux/%s: restarting the same clone disk and firmware to verify identity and credential cleanup",
		arch,
	)
	err := t.runGuest(ctx, o, work, code, arch, &second)
	result.ElapsedSeconds += second.ElapsedSeconds
	if err == nil && second.MachineID != result.MachineID {
		err = fmt.Errorf("%w: clone machine-id changed after reboot", common.ErrInput)
	}
	if err != nil {
		result.Error = err.Error()
		return err
	}
	result.Passed = true
	result.Profile = "base"
	result.Identities = map[string]string{"machineID": result.MachineID}
	result.Checks = map[string]bool{
		"boot": true, "shutdown": true, "os-version": true,
		"reboot-identity": true, "fresh-firmware": true,
		"no-build-credentials": true, "agent-absent": true,
	}
	return nil
}
