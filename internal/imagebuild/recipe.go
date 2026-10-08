package imagebuild

import (
	"fmt"
	"strings"
)

// ValidateNativeRecipe validates pinned native installer inputs before rendering.
func ValidateNativeRecipe(l Lock, platform, expectedOS string) (PlatformInputs, error) {
	osName, _, _ := strings.Cut(platform, "/")
	if osName != expectedOS || (osName != "darwin" && osName != "windows") {
		return PlatformInputs{}, fmt.Errorf("%w: native recipe requires %s", ErrInput, expectedOS)
	}
	entry, err := l.RequirePackages(platform)
	if err != nil {
		return PlatformInputs{}, err
	}
	if !VersionPattern.MatchString(l.CoreVersion) {
		return PlatformInputs{}, fmt.Errorf("%w: invalid core version", ErrInput)
	}
	if err := entry.Core.Validate(); err != nil {
		return PlatformInputs{}, err
	}
	for _, m := range entry.Modules {
		if !NamePattern.MatchString(m.ID) || !VersionPattern.MatchString(m.Version) ||
			!SHAPattern.MatchString(m.Binary.Digest) {
			return PlatformInputs{}, fmt.Errorf("%w: invalid native module", ErrInput)
		}
		if err := m.Package.Validate(); err != nil {
			return PlatformInputs{}, err
		}
	}
	return entry, nil
}
