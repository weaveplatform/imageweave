package windows

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fakeAsset(repo, name string) common.Asset {
	return common.Asset{
		Name:   name,
		URI:    "https://github.com/" + repo + "/releases/download/v1.2.3/" + name,
		Digest: "sha256:" + strings.Repeat("a", 64),
		Size:   1,
	}
}

func mediaFor(uri string, data []byte) common.Media {
	sum := sha256.Sum256(data)
	return common.Media{URI: uri, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func cachedAsset(t *testing.T, cache, repo, name string, data []byte) common.Asset {
	t.Helper()
	m := mediaFor("", data)
	a := fakeAsset(repo, name)
	a.Size = m.Size
	a.Digest = "sha256:" + m.SHA256
	dir := filepath.Join(cache, m.SHA256)
	must(t, os.MkdirAll(dir, 0o750))
	must(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	return a
}

func evidence(t *testing.T, cache, repo, prefix string, assets []common.Asset) []common.Asset {
	t.Helper()
	var sums strings.Builder
	for _, a := range assets {
		sums.WriteString(strings.TrimPrefix(a.Digest, "sha256:") + "  " + a.Name + "\n")
	}
	return []common.Asset{
		cachedAsset(t, cache, repo, prefix+"checksums.txt", []byte(sums.String())),
		cachedAsset(t, cache, repo, prefix+"checksums.txt.sigstore.json", []byte("signature")),
	}
}

func packageFixture(t *testing.T) (common.Lock, string) {
	t.Helper()
	cache := t.TempDir()
	core := cachedAsset(
		t,
		cache,
		common.CoreRepository,
		"weave-agent_1.2.3_arm64.deb",
		[]byte("core"),
	)
	binary := cachedAsset(
		t,
		cache,
		common.ModulesRepository,
		"weave-linux-exec-linux-arm64",
		[]byte("binary"),
	)
	raw, err := json.Marshal(
		map[string]any{
			"id":      "weave-linux-exec",
			"version": "2.3.4",
			"artifacts": []map[string]any{
				{"os": "linux", "arch": "arm64", "digest": binary.Digest, "size": binary.Size},
			},
		},
	)
	must(t, err)
	manifest := cachedAsset(t, cache, common.ModulesRepository, "module.manifest.json", raw)
	pkg := cachedAsset(
		t,
		cache,
		common.ModulesRepository,
		"weave-linux-exec_2.3.4_arm64.deb",
		[]byte("module package"),
	)
	m := common.ModuleInput{
		ID:       "weave-linux-exec",
		Version:  "2.3.4",
		Binary:   binary,
		Manifest: manifest,
		Package:  &pkg,
		PackageEvidence: evidence(
			t,
			cache,
			common.ModulesRepository,
			"",
			[]common.Asset{pkg, manifest, binary},
		),
	}
	return common.Lock{
		SchemaVersion: 1,
		CoreVersion:   "1.2.3",
		Platforms: map[string]common.PlatformInputs{
			"linux/arm64": {
				Core: core,
				CoreEvidence: evidence(
					t,
					cache,
					common.CoreRepository,
					"weaveplatform-agent_1.2.3_",
					[]common.Asset{core},
				),
				Modules: []common.ModuleInput{m},
			},
		},
	}, cache
}

func nativePackageFixture(t *testing.T, osName string) (common.Lock, string) {
	t.Helper()
	l, cache := packageFixture(t)
	entry := l.Platforms["linux/arm64"]
	delete(l.Platforms, "linux/arm64")
	if osName == "darwin" {
		entry.Core = cachedAsset(
			t,
			cache,
			common.CoreRepository,
			"weave-agent_1.2.3_darwin_arm64.pkg",
			[]byte("core pkg"),
		)
		entry.CoreEvidence = []common.Asset{
			cachedAsset(
				t,
				cache,
				common.CoreRepository,
				entry.Core.Name+".sigstore.json",
				[]byte("signature"),
			),
		}
	} else {
		entry.Core = cachedAsset(
			t,
			cache,
			common.CoreRepository,
			"weaveplatform-agent_1.2.3_windows_arm64.zip",
			[]byte("core zip"),
		)
		entry.CoreEvidence = evidence(
			t,
			cache,
			common.CoreRepository,
			"weaveplatform-agent_1.2.3_",
			[]common.Asset{entry.Core},
		)
	}
	m := entry.Modules[0]
	m.ID = strings.Replace(
		m.ID,
		"linux",
		map[string]string{"darwin": "macos", "windows": "windows"}[osName],
		1,
	)
	m.Binary = cachedAsset(
		t,
		cache,
		common.ModulesRepository,
		m.ID+"-"+osName+"-arm64",
		[]byte("native binary"),
	)
	raw, err := json.Marshal(
		map[string]any{
			"id":      m.ID,
			"version": m.Version,
			"artifacts": []map[string]any{
				{"os": osName, "arch": "arm64", "digest": m.Binary.Digest, "size": m.Binary.Size},
			},
		},
	)
	must(t, err)
	m.Manifest = cachedAsset(t, cache, common.ModulesRepository, "module.manifest.json", raw)
	pkg := cachedAsset(
		t,
		cache,
		common.ModulesRepository,
		m.ID+"_2.3.4_"+osName+"_arm64."+map[string]string{"darwin": "pkg", "windows": "zip"}[osName],
		[]byte("native module package"),
	)
	m.Package = &pkg
	m.PackageEvidence = evidence(
		t,
		cache,
		common.ModulesRepository,
		"",
		[]common.Asset{pkg, m.Manifest, m.Binary},
	)
	entry.Modules = []common.ModuleInput{m}
	l.Platforms[osName+"/arm64"] = entry
	return l, cache
}
