package imagebuild

import (
	"testing"
)

func TestNativeRecipeRefusesUnsafeOrIncompleteInputs(t *testing.T) {
	for _, mode := range []string{"platform", "absent platform", "missing installer", "core version", "core name", "module ID", "module version", "module digest", "package name"} {
		t.Run(mode, func(t *testing.T) {
			l, _ := nativePackageFixture(t, "windows")
			e := l.Platforms["windows/arm64"]
			platform := "windows/arm64"
			switch mode {
			case "platform":
				platform = "linux/arm64"
			case "absent platform":
				platform = "windows/amd64"
			case "missing installer":
				e.Modules[0].Package = nil
			case "core version":
				l.CoreVersion = "1'; evil"
			case "core name":
				e.Core.Name = "../installer.zip"
			case "module ID":
				e.Modules[0].ID = "../exec"
			case "module version":
				e.Modules[0].Version = "1'; evil"
			case "module digest":
				e.Modules[0].Binary.Digest = "bad"
			case "package name":
				e.Modules[0].Package.Name = "../module.zip"
			}
			l.Platforms["windows/arm64"] = e
			if _, err := ValidateNativeRecipe(l, platform, "windows"); err == nil {
				t.Fatal("accepted unsafe recipe input")
			}
		})
	}
}
