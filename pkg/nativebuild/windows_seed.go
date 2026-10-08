package nativebuild

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deploymenttheory/go-sdk-winmediafoundry/pkg/udf"

	"github.com/weaveplatform/weaveplatform-oci/pkg/disk/vhd"
)

func moveWindowsRaw(disk, destination string) error {
	_, f, size, err := vhd.Raw(disk)
	if err != nil {
		return fmt.Errorf("validate native fixed VHD: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close VHD: %w", err)
	}
	// The VM is stopped and its handles are closed before stripping the footer.
	if err := os.Truncate(disk, size); err != nil {
		return fmt.Errorf("remove VHD footer: %w", err)
	}
	if err := os.Rename(disk, destination); err != nil {
		return fmt.Errorf("move raw disk: %w", err)
	}
	return nil
}

func windowsSeed(out string, s WindowsSource, marker string) (string, error) {
	dir := filepath.Join(out, "seed")
	if err := newDirectory(dir); err != nil {
		return "", err
	}
	answer, script, err := windowsRecipe(s, marker)
	if err != nil {
		return "", err
	}
	for name, data := range map[string]string{"autounattend.xml": answer, "seal.ps1": script} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			return "", fmt.Errorf("write Windows seed: %w", err)
		}
	}
	return windowsSeedISO(out, dir)
}

func windowsSeedISO(out, dir string) (string, error) {
	iso := filepath.Join(out, "seed.iso")
	f, err := os.OpenFile(
		iso,
		os.O_CREATE|os.O_EXCL|os.O_RDWR,
		0o600,
	) //nolint:gosec // Build-owned seed output.
	if err != nil {
		return "", fmt.Errorf("create Windows seed ISO: %w", err)
	}
	// Use Media Foundry's UDF writer directly, as for Windows installation
	// media. Its ISO9660 writer retains open staging handles, which leaves
	// directory-entry sizes stale on Windows when finalizing the image.
	if err := errors.Join(udf.Write(f, dir, "WEAVE-SEED"), f.Close()); err != nil {
		return "", fmt.Errorf("build Windows seed ISO: %w", err)
	}
	return iso, nil
}

func windowsReceipt(line, marker string) (WindowsInstallResult, error) {
	var result WindowsInstallResult
	prefix := marker + " "
	if !strings.HasPrefix(line, prefix) {
		return result, fmt.Errorf("%w: Windows completion marker absent", ErrInput)
	}
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(strings.TrimPrefix(line, prefix))),
		&result,
	); err != nil {
		return result, fmt.Errorf("decode Windows receipt: %w", err)
	}
	if !result.Generalized {
		return result, fmt.Errorf("%w: Windows guest was not generalized", ErrInput)
	}
	return result, nil
}
