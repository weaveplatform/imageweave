package nativebuild

import (
	"fmt"
	"regexp"
	"strings"
)

// WindowsSelection matches guestweave's edition-release selector.
type WindowsSelection struct{ FromWindows, Arch, Language string }

type WindowsSource struct{ Release, Edition, Arch, Language string }

var (
	windowsReleasePattern = regexp.MustCompile(`^[0-9]{2}h[12]$`)
	windowsBuildPattern   = regexp.MustCompile(`^[2-9][0-9]{4}$`)
	languagePattern       = regexp.MustCompile(`^[a-zA-Z]{2,3}-[a-zA-Z]{2,4}$`)
)

// ParseWindowsSelection validates explicit edition, release, architecture and language.
func ParseWindowsSelection(s WindowsSelection) (string, string, error) {
	edition, release, ok := strings.Cut(strings.ToLower(s.FromWindows), "-")
	_, supported := windowsEditions[edition]
	if !ok || !supported ||
		(release != "latest" && !windowsReleasePattern.MatchString(release) && !windowsBuildPattern.MatchString(release)) {
		return "", "", fmt.Errorf(
			"%w: --from-windows must select pro, home, enterprise or education plus a release, base build, or latest",
			ErrInput,
		)
	}
	if (s.Arch != "amd64" && s.Arch != "arm64") || !languagePattern.MatchString(s.Language) {
		return "", "", fmt.Errorf(
			"%w: --arch amd64/arm64 and a language such as en-US required",
			ErrInput,
		)
	}
	return edition, release, nil
}

type windowsEdition struct{ ID, Image, Key string }

// These public setup keys select an edition; they do not activate Windows.
var windowsEditions = map[string]windowsEdition{
	"pro":        {"Professional", "Windows 11 Pro", "VK7JG-NPHTM-C97JM-9MPGT-3V66T"},
	"home":       {"Core", "Windows 11 Home", "TX9XD-98N7V-6WMQ6-BX7FG-H8Q99"},
	"enterprise": {"Enterprise", "Windows 11 Enterprise", "NPPR9-FWDCX-D2C8J-H872K-2YT43"},
	"education":  {"Education", "Windows 11 Education", "NW6C2-QMPVW-D7KKK-3GKT6-VCFB2"},
}
