package linux

import (
	"fmt"
	"strings"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
)

// LinuxRecipe installs pinned packages and removes machine-specific identities.
func LinuxRecipe(l Lock, arch string) (string, error) {
	entry, err := l.RequirePackages("linux/" + arch)
	if err != nil {
		return "", fmt.Errorf("linux recipe: %w", err)
	}
	lines := []string{
		"mkdir -p /mnt/weave-packages",
		"mount -o ro /dev/disk/by-label/cidata /mnt/weave-packages",
		"systemctl mask --runtime weave-agent.service",
		"dpkg -i /mnt/weave-packages/packages/*.deb",
		"test \"$(dpkg-query -W -f='${Version}' weave-agent)\" = " + common.ShellQuote(
			l.CoreVersion,
		),
	}
	for _, m := range entry.Modules {
		if !common.NamePattern.MatchString(m.ID) ||
			!common.SHAPattern.MatchString(m.Binary.Digest) {
			return "", fmt.Errorf("%w: invalid module for provisioning", common.ErrInput)
		}
		lines = append(
			lines,
			"test \"$(dpkg-query -W -f='${Version}' "+common.ShellQuote(
				m.ID,
			)+")\" = "+common.ShellQuote(
				m.Version,
			),
			"printf '%s\\n' "+common.ShellQuote(
				strings.TrimPrefix(
					m.Binary.Digest,
					"sha256:",
				)+"  /usr/lib/weave/modules/"+m.ID+"/"+m.ID,
			)+" | sha256sum -c -",
		)
	}
	lines = append(
		lines,
		"systemctl unmask --runtime weave-agent.service",
		"systemctl enable weave-agent.service",
		"systemctl stop weave-agent.service",
		"rm -f /etc/weave/channel.pub /var/lib/weave/store.key /var/lib/weave/store.db /var/lib/weave/store.db-wal /var/lib/weave/store.db-shm",
		"cloud-init clean --logs --seed",
		"rm -f /etc/ssh/ssh_host_* /var/lib/systemd/random-seed /var/lib/dbus/machine-id",
		"truncate -s 0 /etc/machine-id",
		"ln -s /etc/machine-id /var/lib/dbus/machine-id",
		"sync",
	)
	return strings.Join(lines, "\n") + "\n", nil
}
