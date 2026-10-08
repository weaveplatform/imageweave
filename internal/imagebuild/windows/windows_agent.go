package windows

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
	"github.com/weaveplatform/weaveplatform-oci/pkg/chunk"
	"github.com/weaveplatform/weaveplatform-oci/pkg/disk/vhd"
	"github.com/weaveplatform/weaveplatform-oci/pkg/disk/virtualdisk"
	"github.com/weaveplatform/weaveplatform-oci/pkg/pack"
	"github.com/weaveplatform/weaveplatform-oci/pkg/spec"
)

func cloneWindowsDisk(
	ctx context.Context,
	raw, destination string,
	convert func(context.Context, string, string, virtualdisk.Format) error,
) error {
	bridge := destination + ".import.vhd"
	if err := common.CopyFileContext(ctx, raw, bridge); err != nil {
		return windowsError(err)
	}
	defer os.Remove(bridge)
	if err := vhd.Append(bridge, time.Now()); err != nil {
		return fmt.Errorf("create parent import bridge: %w", err)
	}
	return convert(ctx, bridge, destination, virtualdisk.VHDX)
}

// BuildWindowsAgent installs a verified offline payload into a native HCS
// clone, generalizes it and exports raw sectors with exact parent lineage.
// It creates a candidate only; first-boot acceptance is a separate gate.
func (p Packages) BuildWindowsAgent(ctx context.Context, o common.AgentOptions) (string, error) {
	if (runtime.GOOS != "windows" || runtime.GOARCH != o.Arch) && p.WindowsInstall == nil {
		return "", fmt.Errorf(
			"%w: Windows agent build requires a matching HCS host",
			common.ErrInput,
		)
	}
	if err := common.ValidateAgentOptions(o); err != nil {
		return "", windowsError(err)
	}
	entry, err := o.Lock.RequirePackages("windows/" + o.Arch)
	if err != nil {
		return "", windowsError(err)
	}
	store, inspection, err := common.OpenCandidate(ctx, o.Base)
	if err != nil {
		return "", windowsError(err)
	}
	var selected []ocispec.Descriptor
	for _, child := range inspection.Children {
		cfg := child.Description.Config
		if cfg.Guest.OS == "windows" && cfg.Guest.Arch == o.Arch && cfg.Guest.Variant == "base" &&
			cfg.Provisioning.Agent == nil {
			selected = append(selected, child.Descriptor)
		}
	}
	if len(selected) != 1 {
		return "", fmt.Errorf(
			"%w: exactly one matching Windows base platform required",
			common.ErrInput,
		)
	}
	o.Out, err = filepath.Abs(o.Out)
	if err != nil {
		return "", fmt.Errorf("resolve Windows agent output: %w", err)
	}
	if err := common.NewDirectory(o.Out); err != nil {
		return "", windowsError(err)
	}
	parent := filepath.Join(o.Out, "parent")
	if _, err := pack.Unpack(ctx, store, selected[0], parent, chunk.AssembleOptions{}); err != nil {
		return "", fmt.Errorf("unpack Windows parent: %w", err)
	}
	b, err := pack.LoadBundle(parent)
	if err != nil {
		return "", fmt.Errorf("load Windows parent: %w", err)
	}
	if len(b.File.Disks) != 1 {
		return "", fmt.Errorf("%w: Windows agent parent must have one system disk", common.ErrInput)
	}
	disk, err := common.SystemDisk(b)
	if err != nil {
		return "", windowsError(err)
	}
	p.Tools.Progress("Windows %s agent: verifying core and all module installers", o.Arch)
	payload := filepath.Join(o.Out, "packages")
	assets, err := p.Prepare(ctx, o.Lock, "windows/"+o.Arch, o.Cache, payload)
	if err != nil {
		return "", windowsError(err)
	}
	marker := fmt.Sprintf("WEAVE-IMAGE-READY-%x", randomMarker())
	seed, err := windowsAgentSeed(ctx, o.Out, payload, o.Arch, marker)
	if err != nil {
		return "", windowsError(err)
	}
	install := p.WindowsInstall
	if install == nil {
		install = installWindowsNative
	}
	p.Tools.Progress("Windows %s agent: starting isolated HCS parent clone", o.Arch)
	result, err := install(ctx, WindowsInstallRequest{
		Directory: o.Out, BaseDisk: disk, Seed: seed, Marker: marker,
		Timeout: o.Timeout, Arch: o.Arch, Log: p.Tools.Log,
	})
	if err != nil {
		return "", windowsError(err)
	}
	if !result.Generalized || result.OSVersion != b.File.Guest.OSVersion ||
		result.Build != b.File.Guest.OSBuild || result.Edition != b.File.Guest.Edition {
		return "", fmt.Errorf(
			"%w: agent clone changed OS version/edition or was not generalized",
			common.ErrInput,
		)
	}
	if err := common.WriteJSON(filepath.Join(o.Out, "install-result.json"), result); err != nil {
		return "", windowsError(err)
	}
	return Tools(p.Tools).windowsAgentBundle(ctx, o, b, selected[0].Digest.String(), assets, entry)
}

func (t Tools) windowsAgentBundle(
	ctx context.Context,
	o common.AgentOptions,
	b pack.Bundle,
	parentDigest string,
	assets []common.Asset,
	entry common.PlatformInputs,
) (string, error) {
	bundle := filepath.Join(o.Out, "bundle")
	if err := common.NewDirectory(bundle); err != nil {
		return "", windowsError(err)
	}
	if err := moveWindowsRaw(
		filepath.Join(o.Out, "disk.vhd"),
		filepath.Join(bundle, "disk0.img"),
	); err != nil {
		return "", windowsError(err)
	}
	for _, state := range b.File.State {
		if err := common.CopyFileContext(
			ctx,
			filepath.Join(b.Dir, state.Path),
			filepath.Join(bundle, state.Path),
		); err != nil {
			return "", windowsError(err)
		}
	}
	commit, err := common.Tools(t).Output(ctx, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", windowsError(err)
	}
	child := b.File
	child.Guest.Variant = "agent"
	child.Provisioning.Agent = &spec.Agent{Name: "weave-agent", Version: o.Lock.CoreVersion}
	child.Disks = []pack.BundleDisk{{Name: "disk0", Role: "system", Path: "disk0.img"}}
	child.Build.Base = &spec.BaseImage{Name: o.BaseName, Digest: parentDigest}
	child.Build.Template = "windows-agent"
	child.Build.TemplateRef = "weaveplatform/weaveplatform-oci@" + strings.TrimSpace(string(commit))
	child.Build.Created = time.Now().UTC().Format(time.RFC3339)
	for _, asset := range assets {
		child.Build.SourceMedia = append(
			child.Build.SourceMedia,
			spec.SourceMedia{Kind: "package", URI: asset.URI, Digest: asset.Digest},
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
		return "", fmt.Errorf("write Windows agent bundle: %w", err)
	}
	err = common.WriteJSON(filepath.Join(o.Out, "inventory.json"), map[string]any{
		"coreVersion": o.Lock.CoreVersion, "modules": entry.Modules,
		"acceptance": "pending: two-clone native boot, trust and module checks",
	})
	return bundle, windowsError(err)
}

func windowsAgentSeed(ctx context.Context, out, payload, arch, marker string) (string, error) {
	dir := filepath.Join(out, "seed")
	if err := common.NewDirectory(dir); err != nil {
		return "", windowsError(err)
	}
	if err := common.CopyTreeContext(ctx, payload, filepath.Join(dir, "packages")); err != nil {
		return "", windowsError(err)
	}
	// Boot the generalized parent into audit mode. No windowsPE pass or install
	// media is present: a derived build must never repartition/reinstall its base.
	answer := `<?xml version="1.0" encoding="utf-8"?><unattend xmlns="urn:schemas-microsoft-com:unattend">`
	component := `<component name="Microsoft-Windows-Deployment" processorArchitecture="` + arch + `" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">`
	answer += `<settings pass="oobeSystem">` + component + `<Reseal><Mode>Audit</Mode></Reseal></component></settings>`
	answer += `<settings pass="auditUser">` + component + `<RunSynchronous><RunSynchronousCommand wcm:action="add"><Order>1</Order><Path>powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$v=Get-Volume | Where-Object FileSystemLabel -eq 'WEAVE-SEED'; if(!$v){exit 1}; &amp; ($v.DriveLetter+':\seal.ps1')"</Path></RunSynchronousCommand></RunSynchronous></component></settings></unattend>`
	script := windowsSealScript(
		marker,
		`$seed = Get-Volume | Where-Object FileSystemLabel -eq 'WEAVE-SEED'
if (!$seed) { throw 'Verified installation payload absent' }
$packages = $seed.DriveLetter + ':\packages'
Write-BuildProgress 'Installing verified agent and modules from offline payload'
& powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $packages 'install-agent.ps1') -Packages $packages 2>&1 | ForEach-Object { Write-BuildProgress ([string]$_) }
if ($LASTEXITCODE -ne 0) { throw 'Agent payload installation or sealing failed' }`,
	)
	for name, data := range map[string]string{"autounattend.xml": answer, "seal.ps1": script} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			return "", fmt.Errorf("write Windows agent seed: %w", err)
		}
	}
	return windowsSeedISO(out, dir)
}
