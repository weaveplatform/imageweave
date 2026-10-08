package macos

import (
	"fmt"
	"strings"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
)

// NativeAgentRecipe validates pinned Darwin inputs before rendering installation
// and sealing commands. The caller removes the disposable build login separately.
func NativeAgentRecipe(l common.Lock, platform string) (string, error) {
	entry, err := common.ValidateNativeRecipe(l, platform, "darwin")
	if err != nil {
		return "", fmt.Errorf("validate macOS recipe: %w", err)
	}
	return macAgentRecipe(l.CoreVersion, entry), nil
}

func macAgentRecipe(version string, e common.PlatformInputs) string {
	lines := make([]string, 0, 14+6*len(e.Modules))
	lines = append(lines, "#!/bin/sh", "set -eu", `packages=$1`, `test "$(id -u)" = 0`)
	assets := make([]common.Asset, 1, 1+len(e.Modules))
	assets[0] = e.Core
	for _, m := range e.Modules {
		assets = append(assets, *m.Package)
	}
	for _, a := range assets {
		path := `"$packages/"` + common.ShellQuote(a.Name)
		lines = append(
			lines,
			`test "$(shasum -a 256 `+path+` | awk '{print $1}')" = `+common.ShellQuote(
				strings.TrimPrefix(a.Digest, "sha256:"),
			),
			"pkgutil --check-signature "+path,
			"spctl --assess --type install "+path,
			"installer -pkg "+path+" -target /",
		)
	}
	lines = append(
		lines,
		`test "$(/usr/local/libexec/weave/weave-agent --version)" = `+common.ShellQuote(version),
	)
	for _, m := range e.Modules {
		lines = append(
			lines,
			`test "$(pkgutil --pkg-info `+common.ShellQuote(
				"run.weaveplatform.module."+m.ID,
			)+` | awk '/^version: / {print $2}')" = `+common.ShellQuote(
				m.Version,
			),
			`test "$(shasum -a 256 `+common.ShellQuote(
				"/usr/local/libexec/weave/modules/"+m.ID+"/"+m.ID,
			)+` | awk '{print $1}')" = `+common.ShellQuote(
				strings.TrimPrefix(m.Binary.Digest, "sha256:"),
			),
		)
	}
	lines = append(
		lines,
		"launchctl bootout system/run.weaveplatform.agent || { if launchctl print system/run.weaveplatform.agent >/dev/null 2>&1; then exit 1; fi; }",
		"launchctl enable system/run.weaveplatform.agent",
		"rm -f /etc/weave/channel.pub",
		`for name in store.key store.db store.db-wal store.db-shm; do rm -f "/Library/Application Support/Weave/$name"; done`,
		"sync",
	)
	return strings.Join(lines, "\n") + "\n"
}
