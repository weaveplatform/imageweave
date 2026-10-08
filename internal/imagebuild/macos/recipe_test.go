package macos

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
)

func recipeLock() common.Lock {
	asset := func(name string) common.Asset {
		return common.Asset{
			Name:   name,
			URI:    "https://github.com/weaveplatform/weaveplatform-agent-core/releases/download/v1/" + name,
			Digest: "sha256:" + strings.Repeat("a", 64),
			Size:   1,
		}
	}
	installer := asset("module.pkg")
	return common.Lock{CoreVersion: "1.2.3", Platforms: map[string]common.PlatformInputs{
		"darwin/arm64": {
			Core: asset("core.pkg"),
			Modules: []common.ModuleInput{
				{
					ID:      "presence-macos",
					Version: "2.3.4",
					Binary:  asset("presence"),
					Package: &installer,
					PackageEvidence: []common.Asset{
						{Name: "checksums.txt"},
						{Name: "checksums.txt.sigstore.json"},
					},
				},
			},
		},
	}}
}

func TestRecipeInstallsPinnedInputsAndSealsAfterStopping(t *testing.T) {
	recipe, err := NativeAgentRecipe(recipeLock(), "darwin/arm64")
	must(t, err)
	for _, want := range []string{"core.pkg", "module.pkg", "presence-macos", "1.2.3", "2.3.4", strings.Repeat("a", 64), "pkgutil --check-signature", "spctl --assess", "installer -pkg", "store.db", "store.db-wal", "store.db-shm", "store.key", "channel.pub"} {
		if !strings.Contains(recipe, want) {
			t.Errorf("missing installation/cleanup check %q", want)
		}
	}
	if strings.Contains(recipe, "manifest.sequence") {
		t.Fatal("recipe changes anti-rollback state")
	}
	if strings.Index(
		recipe,
		"launchctl bootout",
	) > strings.Index(
		recipe,
		"rm -f /etc/weave/channel.pub",
	) {
		t.Fatal("sealing occurs while agent can write")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("shell parser unavailable")
	}
	path := filepath.Join(t.TempDir(), "recipe.sh")
	must(t, os.WriteFile(path, []byte(recipe), 0o600))
	output, err := exec.CommandContext(t.Context(), sh, "-n", path).CombinedOutput()
	if err != nil {
		t.Fatalf("shell syntax: %v %s", err, output)
	}
}

func TestRecipeRejectsOtherPlatformAndUnsafeInputs(t *testing.T) {
	for _, platform := range []string{"windows/arm64", "linux/arm64", "darwin/amd64", "bad"} {
		if _, err := NativeAgentRecipe(recipeLock(), platform); err == nil {
			t.Fatal("wrong platform accepted", platform)
		}
	}
	l := recipeLock()
	l.CoreVersion = "1'; evil"
	if _, err := NativeAgentRecipe(l, "darwin/arm64"); err == nil {
		t.Fatal("unsafe version accepted")
	}
}
