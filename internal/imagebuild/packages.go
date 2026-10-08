package imagebuild

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Packages verifies publisher evidence before creating an offline installation payload.
type Packages struct {
	Recipe     func(Lock, string) (string, error)
	Tools      Tools
	Downloader Downloader
}

func (p Packages) signedChecksums(
	ctx context.Context,
	evidence, artifacts []Asset,
	repo string,
	refs []string,
	cache string,
) error {
	files := map[string]string{}
	var sums string
	for _, a := range evidence {
		path, err := p.Downloader.Asset(ctx, a, cache)
		if err != nil {
			return err
		}
		files[a.Name] = path
		if strings.HasSuffix(a.Name, "checksums.txt") {
			if sums != "" {
				return fmt.Errorf("%w: multiple checksum files", ErrInput)
			}
			sums = a.Name
		}
	}
	if sums == "" || files[sums+".sigstore.json"] == "" {
		return fmt.Errorf("%w: checksum file and Sigstore bundle required", ErrInput)
	}
	if err := p.verifyBlob(ctx, files[sums], files[sums+".sigstore.json"], repo, refs); err != nil {
		return err
	}
	raw, err := os.ReadFile(files[sums])
	if err != nil {
		return fmt.Errorf("read signed checksums: %w", err)
	}
	return checkSums(string(raw), artifacts)
}

func (p Packages) verifyBlob(ctx context.Context, file, bundle, repo string, refs []string) error {
	workflow := "module-release"
	if repo == CoreRepository {
		workflow = "release"
	}
	escaped := make([]string, len(refs))
	for i, r := range refs {
		escaped[i] = regexp.QuoteMeta(r)
	}
	identity := `^https://github\.com/` + regexp.QuoteMeta(
		repo,
	) + `/\.github/workflows/` + workflow + `\.yml@(?:` + strings.Join(
		escaped,
		"|",
	) + `)$`
	cosign := os.Getenv("COSIGN")
	if cosign == "" {
		cosign = "cosign"
	}
	return p.Tools.RunCommand(
		ctx,
		nil,
		cosign,
		"verify-blob",
		"--bundle",
		bundle,
		"--certificate-identity-regexp",
		identity,
		"--certificate-oidc-issuer",
		"https://token.actions.githubusercontent.com",
		file,
	)
}

func checkSums(raw string, artifacts []Asset) error {
	sums := map[string]string{}
	pattern := regexp.MustCompile(`^([0-9a-f]{64}) [ *](.+)$`)
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		match := pattern.FindStringSubmatch(line)
		if match == nil || sums[match[2]] != "" {
			return fmt.Errorf("%w: malformed or duplicate checksum entry", ErrInput)
		}
		sums[match[2]] = "sha256:" + match[1]
	}
	for _, a := range artifacts {
		if sums[a.Name] != a.Digest {
			return fmt.Errorf("%w: %s signed checksum differs from lock", ErrInput, a.Name)
		}
	}
	return nil
}

func (p Packages) module(ctx context.Context, m ModuleInput, arch, cache string) error {
	return p.moduleFor(ctx, m, "linux", arch, cache)
}

func (p Packages) moduleFor(ctx context.Context, m ModuleInput, osName, arch, cache string) error {
	if err := p.signedChecksums(
		ctx,
		m.PackageEvidence,
		[]Asset{*m.Package, m.Manifest, m.Binary},
		ModulesRepository,
		[]string{"refs/tags/modules/" + m.ID + "/v" + m.Version, "refs/heads/main"},
		cache,
	); err != nil {
		return err
	}
	file, err := p.Downloader.Asset(ctx, m.Manifest, cache)
	if err != nil {
		return err
	}
	var manifest struct {
		ID        string `json:"id"`
		Version   string `json:"version"`
		Artifacts []struct {
			OS     string `json:"os"`
			Arch   string `json:"arch"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"artifacts"`
	}
	if err := ReadJSON(file, &manifest); err != nil {
		return err
	}
	if manifest.ID != m.ID || manifest.Version != m.Version {
		return fmt.Errorf("%w: manifest disagrees with module lock", ErrInput)
	}
	matches := 0
	for _, a := range manifest.Artifacts {
		if a.OS == osName && a.Arch == arch {
			matches++
			if a.Digest != m.Binary.Digest || a.Size != m.Binary.Size {
				return fmt.Errorf("%w: manifest binary differs from lock", ErrInput)
			}
		}
	}
	if matches != 1 {
		return fmt.Errorf("%w: manifest requires exactly one matching artifact", ErrInput)
	}
	return nil
}

// PrepareLinux checks all installers and their signed manifests before writing a payload.
func (p Packages) PrepareLinux(
	ctx context.Context,
	l Lock,
	arch, cache, out string,
) ([]Asset, error) {
	return p.Prepare(ctx, l, "linux/"+arch, cache, out)
}

// Prepare authenticates all installers before creating an offline payload.
// Linux and Windows releases authenticate their checksum files; the macOS core
// publishes a Sigstore bundle over the notarized package itself.
func (p Packages) Prepare(
	ctx context.Context,
	l Lock,
	platform, cache, out string,
) ([]Asset, error) {
	osName, arch, ok := strings.Cut(platform, "/")
	if !ok || (osName != "linux" && osName != "darwin" && osName != "windows") ||
		(arch != "amd64" && arch != "arm64") || (osName == "darwin" && arch != "arm64") {
		return nil, fmt.Errorf("%w: unsupported package platform %s", ErrInput, platform)
	}
	entry, err := l.RequirePackages(platform)
	if err != nil {
		return nil, err
	}
	var recipe string
	if osName != "linux" {
		if p.Recipe == nil {
			return nil, fmt.Errorf("%w: native platform requires a recipe provider", ErrInput)
		}
		recipe, err = p.Recipe(l, platform)
		if err != nil {
			return nil, err
		}
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: payload output already exists or is inaccessible", ErrInput)
	}
	if err := p.coreEvidence(ctx, entry, l.CoreVersion, osName, cache); err != nil {
		return nil, err
	}
	sources := []Asset{entry.Core}
	for _, m := range entry.Modules {
		if err := p.moduleFor(ctx, m, osName, arch, cache); err != nil {
			return nil, err
		}
		sources = append(sources, *m.Package)
	}
	paths := make([]string, len(sources))
	for i, a := range sources {
		path, err := p.Downloader.Asset(ctx, a, cache)
		if err != nil {
			return nil, err
		}
		paths[i] = path
	}
	if err := NewDirectory(out); err != nil {
		return nil, err
	}
	for i, a := range sources {
		if err := CopyFileContext(ctx, paths[i], filepath.Join(out, a.Name)); err != nil {
			return nil, err
		}
	}
	if recipe != "" {
		name := "install-agent.sh"
		if osName == "windows" {
			name = "install-agent.ps1"
		}
		if err := os.WriteFile(filepath.Join(out, name), []byte(recipe), 0o600); err != nil {
			return nil, fmt.Errorf("write native agent recipe: %w", err)
		}
	}
	return sources, WriteJSON(filepath.Join(out, "packages.lock.json"), l)
}

func (p Packages) coreEvidence(
	ctx context.Context,
	entry PlatformInputs,
	version, osName, cache string,
) error {
	refs := []string{"refs/tags/v" + version}
	if osName != "darwin" {
		return p.signedChecksums(
			ctx,
			entry.CoreEvidence,
			[]Asset{entry.Core},
			CoreRepository,
			refs,
			cache,
		)
	}
	if len(entry.CoreEvidence) != 1 ||
		entry.CoreEvidence[0].Name != entry.Core.Name+".sigstore.json" {
		return fmt.Errorf("%w: macOS core package requires its Sigstore bundle", ErrInput)
	}
	pkg, err := p.Downloader.Asset(ctx, entry.Core, cache)
	if err != nil {
		return err
	}
	bundle, err := p.Downloader.Asset(ctx, entry.CoreEvidence[0], cache)
	if err != nil {
		return err
	}
	return p.verifyBlob(ctx, pkg, bundle, CoreRepository, refs)
}
