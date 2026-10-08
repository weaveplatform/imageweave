package linux

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

func cloneLock(t *testing.T, l Lock) Lock {
	t.Helper()
	raw, e := json.Marshal(l)
	must(t, e)
	var v Lock
	must(t, json.Unmarshal(raw, &v))
	return v
}

func fakeAsset(repo, name string) Asset {
	return Asset{
		Name:   name,
		URI:    "https://github.com/" + repo + "/releases/download/v1.2.3/" + name,
		Digest: "sha256:" + strings.Repeat("a", 64),
		Size:   1,
	}
}

func cachedAsset(t *testing.T, cache, repo, name string, data []byte) Asset {
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

func evidence(t *testing.T, cache, repo, prefix string, assets []Asset) []Asset {
	t.Helper()
	var sums strings.Builder
	for _, a := range assets {
		sums.WriteString(strings.TrimPrefix(a.Digest, "sha256:") + "  " + a.Name + "\n")
	}
	return []Asset{
		cachedAsset(t, cache, repo, prefix+"checksums.txt", []byte(sums.String())),
		cachedAsset(t, cache, repo, prefix+"checksums.txt.sigstore.json", []byte("signature")),
	}
}

func packageFixture(t *testing.T) (Lock, string) {
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
	m := ModuleInput{
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
			[]Asset{pkg, manifest, binary},
		),
	}
	return Lock{
		SchemaVersion: 1,
		CoreVersion:   "1.2.3",
		Platforms: map[string]PlatformInputs{
			"linux/arm64": {
				Core: core,
				CoreEvidence: evidence(
					t,
					cache,
					common.CoreRepository,
					"weaveplatform-agent_1.2.3_",
					[]Asset{core},
				),
				Modules: []ModuleInput{m},
			},
		},
	}, cache
}

func mediaFor(uri string, data []byte) Media {
	sum := sha256.Sum256(data)
	return Media{URI: uri, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}
