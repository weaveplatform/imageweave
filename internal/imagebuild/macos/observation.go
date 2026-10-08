package macos

import (
	"fmt"
	"strings"

	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

// Observation is parsed from authenticated guest output, never expected metadata.
type Observation struct {
	Version, Build, Marker, HardwareUUID, ConsoleUser, Groups, AutoLoginUser string
	SetupComplete, AgentAbsent, BuildAccessAbsent                            bool
	HostKeyDigest                                                            string
}

func (o Observation) Check(cfg spec.Config, marker string) error {
	if o.Version != cfg.Guest.OSVersion || o.Build != cfg.Guest.OSBuild || o.Marker != marker ||
		o.HardwareUUID == "" || o.ConsoleUser != "weave" || o.AutoLoginUser != "weave" ||
		!o.SetupComplete || !o.AgentAbsent || !o.BuildAccessAbsent || o.HostKeyDigest == "" {
		return fmt.Errorf(
			"%w: guest version, identity, account or cleanup observation failed",
			ErrPrepared,
		)
	}
	for _, g := range strings.Fields(o.Groups) {
		if g == "admin" {
			return nil
		}
	}
	return fmt.Errorf("%w: weave is not an administrator", ErrPrepared)
}

func parseObservation(output, hostKey string) (Observation, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 10 {
		return Observation{}, fmt.Errorf("%w: incomplete guest observation", ErrPrepared)
	}
	return Observation{
		Version:           lines[0],
		Build:             lines[1],
		Marker:            lines[2],
		HardwareUUID:      lines[3],
		ConsoleUser:       lines[4],
		Groups:            lines[5],
		AutoLoginUser:     lines[6],
		SetupComplete:     lines[7] == "yes",
		AgentAbsent:       lines[8] == "yes",
		BuildAccessAbsent: lines[9] == "yes",
		HostKeyDigest:     hostKey,
	}, nil
}

// Only a generated ASCII challenge is interpolated by the caller. Probe failure
// is a failure, including permissions errors while inspecting system state.
const observationScript = `set -eu
/usr/bin/sw_vers -productVersion
/usr/bin/sw_vers -buildVersion
printf '%%s\n' '%s'
/usr/sbin/ioreg -rd1 -c IOPlatformExpertDevice | /usr/bin/awk -F '\"' '/IOPlatformUUID/ {print $(NF-1)}'
/usr/bin/stat -f %%Su /dev/console
/usr/bin/id -Gn weave
/usr/bin/defaults read /Library/Preferences/com.apple.loginwindow autoLoginUser
if test -e /var/db/.AppleSetupDone && ! /usr/bin/pgrep -u weave -f '/Setup Assistant.app/' >/dev/null; then echo yes; else echo no; fi
if test ! -e /usr/local/libexec/weave/weave-agent && test ! -d /usr/local/libexec/weave/modules && test ! -e /Library/LaunchDaemons/run.weaveplatform.agent.plist; then echo yes; else echo no; fi
if test ! -s /Users/weave/.ssh/authorized_keys && test ! -e /etc/weave/channel.pub; then echo yes; else echo no; fi
`
