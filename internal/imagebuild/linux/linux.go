package linux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/oci"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/weaveplatform-oci/pkg/chunk"
	"github.com/weaveplatform/weaveplatform-oci/pkg/imagecheck"
	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

// Acceptance records validation of the actual packed and unpacked bytes.
type Acceptance = imagecheck.Report

// ValidateOptions selects the bundles and required architectures for a candidate.
type ValidateOptions struct {
	Bundles []string
	Out     string
	Arches  []string
	Timeout time.Duration
}

func validationBundles(o ValidateOptions) ([]pack.Bundle, string, error) {
	if len(o.Bundles) == 0 || len(o.Arches) == 0 {
		return nil, "", fmt.Errorf("%w: bundles and architectures required", common.ErrInput)
	}
	seen := map[string]bool{}
	var bundles []pack.Bundle
	tag, version, build, distro := "", "", "", ""
	for _, dir := range o.Bundles {
		b, err := pack.LoadBundle(dir)
		if err != nil {
			return nil, "", fmt.Errorf("load bundle: %w", err)
		}
		g := b.File.Guest
		t := b.File.Annotations[spec.AnnotationVersion]
		if len(bundles) == 0 {
			tag, version, build, distro = t, g.OSVersion, g.OSBuild, g.Distro
		}
		if g.OS != "linux" || g.Variant != "base" || b.File.Provisioning.Agent != nil ||
			seen[g.Arch] ||
			!slices.Contains(o.Arches, g.Arch) ||
			t == "" ||
			t != tag ||
			g.OSVersion != version ||
			g.OSBuild != build || g.Distro != distro {
			return nil, "", fmt.Errorf(
				"%w: bundles must cover requested platforms once with matching base tier, tag and OS version/build",
				common.ErrInput,
			)
		}
		seen[g.Arch] = true
		bundles = append(bundles, b)
	}
	if len(seen) != len(o.Arches) {
		return nil, "", fmt.Errorf("%w: missing or duplicate architectures", common.ErrInput)
	}
	return bundles, tag, nil
}

// ValidateLinux packs, deeply verifies and boots two fresh clones per architecture.
func (t Tools) ValidateLinux(
	ctx context.Context,
	o ValidateOptions,
) (result Acceptance, err error) {
	bundles, tag, err := validationBundles(o)
	if err != nil {
		return result, err
	}
	if err := common.NewDirectory(o.Out); err != nil {
		return result, fmt.Errorf("linux candidate: %w", err)
	}
	store, err := pack.OpenLayout(ctx, filepath.Join(o.Out, "layout"))
	if err != nil {
		return result, fmt.Errorf("create layout: %w", err)
	}
	var children []ocispec.Descriptor
	var configs []pack.BundleFile
	for _, b := range bundles {
		t.progress("validate linux/%s: packing bundle %s", b.File.Guest.Arch, b.Dir)
		desc, err := pack.Manifest(ctx, b, store, chunk.Options{TempDir: o.Out})
		if err != nil {
			return result, fmt.Errorf("pack candidate: %w", err)
		}
		children = append(children, desc)
		configs = append(configs, b.File)
	}
	root, err := pack.Index(ctx, store, children, nil)
	if err != nil {
		return result, fmt.Errorf("pack index: %w", err)
	}
	if err := store.Tag(ctx, root, tag); err != nil {
		return result, fmt.Errorf("tag index: %w", err)
	}
	t.progress("validate: packed index %s; starting strict deep integrity check", root.Digest)
	report, err := common.DeepCheck(ctx, store, root)
	if err != nil {
		return result, fmt.Errorf("linux candidate: %w", err)
	}
	if err := common.WriteJSON(filepath.Join(o.Out, "inspection.json"), report); err != nil {
		return result, fmt.Errorf("linux candidate: %w", err)
	}
	if err := common.WriteJSON(filepath.Join(o.Out, "bundles.json"), configs); err != nil {
		return result, fmt.Errorf("linux candidate: %w", err)
	}
	result = Acceptance{
		SchemaVersion:   3,
		PlatformDigests: map[string]string{},
		IndexDigest:     root.Digest.String(),
		Tag:             tag,
		Platforms:       map[string][]BootResult{},
	}
	defer func() {
		if err != nil {
			result.Passed = false
		}
		err = errors.Join(err, common.WriteJSON(filepath.Join(o.Out, "acceptance.json"), result))
	}()
	for i, b := range bundles {
		result.PlatformDigests["linux/"+b.File.Guest.Arch] = children[i].Digest.String()
		clones, err := t.validateClones(ctx, store, children[i], b.File.Guest.Arch, o)
		result.Platforms["linux/"+b.File.Guest.Arch] = clones
		if err != nil {
			return result, err
		}
	}
	result.Passed = true
	encoded, err := json.Marshal(result)
	if err != nil {
		return result, fmt.Errorf("encode evidence: %w", err)
	}
	if err := imagecheck.Check(encoded, report, tag); err != nil {
		return result, fmt.Errorf("check acceptance: %w", err)
	}
	t.progress(
		"validate: all requested Linux architectures passed two fresh clone boots and reboots; index=%s",
		root.Digest,
	)
	return result, nil
}

func (t Tools) validateClones(
	ctx context.Context,
	store *oci.Store,
	desc ocispec.Descriptor,
	arch string,
	o ValidateOptions,
) ([]BootResult, error) {
	tmp, err := os.MkdirTemp(o.Out, "unpack-"+arch+"-")
	if err != nil {
		return nil, fmt.Errorf("create unpack directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	bundle := filepath.Join(tmp, "bundle")
	t.progress("validate linux/%s: unpacking candidate %s", arch, desc.Digest)
	if _, err := pack.Unpack(ctx, store, desc, bundle, chunk.AssembleOptions{}); err != nil {
		return nil, fmt.Errorf("unpack candidate: %w", err)
	}
	var clones []BootResult
	for n := 1; n <= 2; n++ {
		t.progress("validate linux/%s: starting clone %d/2", arch, n)
		result, err := t.BootLinux(
			ctx,
			BootOptions{
				Bundle:       bundle,
				VerifyReboot: true,
				Timeout:      o.Timeout,
				Report:       filepath.Join(o.Out, "reports", fmt.Sprintf("%s-%d", arch, n)),
			},
		)
		clones = append(clones, result)
		if err != nil {
			return clones, err
		}
	}
	if clones[0].MachineID == clones[1].MachineID {
		return clones, fmt.Errorf("%w: fresh %s clones reused a machine-id", common.ErrInput, arch)
	}
	t.progress(
		"validate linux/%s: both clones passed with distinct machine IDs (%s, %s)",
		arch,
		clones[0].MachineID,
		clones[1].MachineID,
	)
	return clones, nil
}

// BuildLinuxAgent creates a sealed candidate; module/channel acceptance remains a separate gate.
func (p Packages) BuildLinuxAgent(ctx context.Context, o AgentOptions) (string, error) {
	entry, err := o.Lock.RequirePackages("linux/" + o.Arch)
	if err != nil {
		return "", fmt.Errorf("linux candidate: %w", err)
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9./_-]*$`).MatchString(o.BaseName) || o.Revision < 1 {
		return "", fmt.Errorf(
			"%w: valid base repository and positive revision required",
			common.ErrInput,
		)
	}
	store, report, err := common.OpenCandidate(ctx, o.Base)
	if err != nil {
		return "", fmt.Errorf("linux candidate: %w", err)
	}
	var selected []ocispec.Descriptor
	for _, child := range report.Children {
		g := child.Description.Config.Guest
		if g.OS == "linux" && g.Arch == o.Arch {
			selected = append(selected, child.Descriptor)
		}
	}
	if len(selected) != 1 {
		return "", fmt.Errorf("%w: base needs exactly one matching platform", common.ErrInput)
	}
	if err := common.NewDirectory(o.Out); err != nil {
		return "", fmt.Errorf("linux candidate: %w", err)
	}
	parent := filepath.Join(o.Out, "parent")
	if _, err := pack.Unpack(ctx, store, selected[0], parent, chunk.AssembleOptions{}); err != nil {
		return "", fmt.Errorf("unpack base: %w", err)
	}
	b, err := pack.LoadBundle(parent)
	if err != nil {
		return "", fmt.Errorf("load parent: %w", err)
	}
	if b.File.Guest.Variant != "base" || b.File.Provisioning.Agent != nil ||
		len(b.File.State) > 0 ||
		len(b.File.Disks) != 1 {
		return "", fmt.Errorf(
			"%w: parent must be a base with one disk and fresh UEFI state",
			common.ErrInput,
		)
	}
	sources, err := p.PrepareLinux(ctx, o.Lock, o.Arch, o.Cache, filepath.Join(o.Out, "packages"))
	if err != nil {
		return "", err
	}
	recipe, err := LinuxRecipe(o.Lock, o.Arch)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(
		filepath.Join(o.Out, "provision.sh"),
		[]byte(recipe),
		0o600,
	); err != nil {
		return "", fmt.Errorf("write recipe: %w", err)
	}
	bundle := filepath.Join(o.Out, "bundle")
	if err := common.NewDirectory(bundle); err != nil {
		return "", fmt.Errorf("linux candidate: %w", err)
	}
	if _, err := p.Tools.BootLinux(
		ctx,
		BootOptions{
			Bundle:     parent,
			Timeout:    o.Timeout,
			Report:     filepath.Join(o.Out, "provision-report"),
			Script:     recipe,
			OutputDisk: filepath.Join(bundle, "disk0.img"),
			Payload:    filepath.Join(o.Out, "packages"),
		},
	); err != nil {
		return "", err
	}
	commit, err := p.Tools.output(ctx, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	child := b.File
	child.Guest.Variant = "agent"
	child.Provisioning = spec.Provisioning{
		CredentialHint: "cloud-init",
		Agent:          &spec.Agent{Name: "weave-agent", Version: o.Lock.CoreVersion},
	}
	child.Disks = []pack.BundleDisk{{Name: "disk0", Role: "system", Path: "disk0.img"}}
	child.Build.Base = &spec.BaseImage{Name: o.BaseName, Digest: selected[0].Digest.String()}
	child.Build.Template = "linux-agent"
	child.Build.TemplateRef = "weaveplatform/imageweave@" + strings.TrimSpace(string(commit))
	child.Build.Created = time.Now().UTC().Format(time.RFC3339)
	for _, s := range sources {
		child.Build.SourceMedia = append(
			child.Build.SourceMedia,
			spec.SourceMedia{Kind: "package", URI: s.URI, Digest: s.Digest},
		)
	}
	child.Annotations[spec.AnnotationVersion] = fmt.Sprintf(
		"%s-%s-agent%s-r%d",
		child.Guest.OSVersion,
		child.Guest.OSBuild,
		o.Lock.CoreVersion,
		o.Revision,
	)
	child.Annotations[spec.AnnotationRevision] = strings.TrimSpace(string(commit))
	if err := pack.WriteBundleFile(bundle, child); err != nil {
		return "", fmt.Errorf("write agent bundle: %w", err)
	}
	err = common.WriteJSON(
		filepath.Join(o.Out, "inventory.json"),
		map[string]any{
			"coreVersion": o.Lock.CoreVersion,
			"modules":     entry.Modules,
			"acceptance":  "pending: module readiness and host channel tests",
		},
	)
	if err != nil {
		return bundle, fmt.Errorf("write Linux inventory: %w", err)
	}
	return bundle, nil
}
