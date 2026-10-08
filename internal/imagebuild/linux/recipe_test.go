package linux

import (
	"strings"
	"testing"
)

func TestLinuxRecipe(t *testing.T) {
	l, _ := packageFixture(t)
	recipe, err := LinuxRecipe(l, "arm64")
	must(t, err)
	for _, want := range []string{"store.db-wal", "store.db-shm", "store.key", "channel.pub", "cloud-init clean", "truncate -s 0 /etc/machine-id", "sha256sum -c -", "systemctl mask --runtime"} {
		if !strings.Contains(recipe, want) {
			t.Errorf("recipe missing %s", want)
		}
	}
	if strings.Contains(recipe, "manifest.sequence") {
		t.Fatal("recipe deletes anti-rollback state")
	}
	l.Platforms["linux/arm64"].Modules[0].Package = nil
	if _, err := LinuxRecipe(l, "arm64"); err == nil {
		t.Fatal("missing installer")
	}
}

func TestLinuxRecipeRejectsUnsafeModule(t *testing.T) {
	l, _ := packageFixture(t)
	l.Platforms["linux/arm64"].Modules[0].ID = "unsafe/id"
	if _, err := LinuxRecipe(l, "arm64"); err == nil {
		t.Fatal("unsafe recipe accepted")
	}
}
