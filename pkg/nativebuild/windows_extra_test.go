package nativebuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedValidation(t *testing.T) {
	_, s := windowsFixture()
	if _, err := windowsSeed(t.TempDir(), s, "wrong"); err == nil {
		t.Fatal("invalid recipe")
	}
	if !strings.Contains(windowsSealScript("marker", "custom provision"), "custom provision") {
		t.Fatal("provision lost")
	}
	out := t.TempDir()
	_, err := buildWindows(
		t.Context(),
		Config{SourcePath: "missing", OutputDirectory: out},
		nil,
		operations{},
	)
	if err == nil {
		t.Fatal("copy error lost")
	}
	blocked := filepath.Join(out, "seed")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := windowsSeed(out, s, "WEAVE-IMAGE-READY-0123456789abcdef01234567"); err == nil {
		t.Fatal("reused seed")
	}
}
