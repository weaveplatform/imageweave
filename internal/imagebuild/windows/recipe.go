package windows

import (
	"fmt"
	"strings"

	common "github.com/weaveplatform/imageweave/internal/imagebuild"
)

func NativeAgentRecipe(l common.Lock, platform string) (string, error) {
	entry, err := common.ValidateNativeRecipe(l, platform, "windows")
	if err != nil {
		return "", windowsError(err)
	}
	return windowsAgentRecipe(l.CoreVersion, entry), nil
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func windowsAgentRecipe(version string, e common.PlatformInputs) string {
	lines := make([]string, 0, 26+13*len(e.Modules))
	lines = append(lines,
		`param([Parameter(Mandatory=$true)][string]$Packages)`, `$ErrorActionPreference = 'Stop'`,
		`$work = Join-Path $env:TEMP ('weave-install-' + [guid]::NewGuid().ToString())`,
		`New-Item -ItemType Directory -Path $work | Out-Null`, "try {",
	)
	assets := make([]common.Asset, 1, 1+len(e.Modules))
	assets[0] = e.Core
	for _, m := range e.Modules {
		assets = append(assets, *m.Package)
	}
	for i, a := range assets {
		lines = append(
			lines,
			`$archive = Join-Path $Packages `+psQuote(a.Name),
			`if ((Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant() -cne `+psQuote(
				strings.TrimPrefix(a.Digest, "sha256:"),
			)+`) { throw 'Installer hash differs from lock' }`,
			fmt.Sprintf(`$destination = Join-Path $work 'package-%d'`, i),
			`Expand-Archive -LiteralPath $archive -DestinationPath $destination`,
			`$install = Join-Path $destination 'install.ps1'`,
		)
		argument := "-NoReload"
		if i == 0 {
			argument = "-NoStart"
		}
		lines = append(
			lines,
			`& powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $install `+argument,
			`if ($LASTEXITCODE -ne 0) { throw 'Package installation failed' }`,
		)
	}
	lines = append(
		lines,
		`$agent = Join-Path $env:ProgramFiles 'Weave\weave-agent.exe'`,
		`if ((Get-AuthenticodeSignature -LiteralPath $agent).Status -ne 'Valid') { throw 'Core Authenticode verification failed' }`,
		`$version = & $agent --version`,
		`if ($LASTEXITCODE -ne 0 -or $version -cne `+psQuote(
			version,
		)+`) { throw 'Core version differs from lock' }`,
	)
	for _, m := range e.Modules {
		lines = append(
			lines,
			`$module = Join-Path $env:ProgramFiles `+psQuote(`Weave\modules\`+m.ID+`\`),
			`$binary = Join-Path $module `+psQuote(m.ID+".exe"),
			`if ((Get-AuthenticodeSignature -LiteralPath $binary).Status -ne 'Valid') { throw 'Module Authenticode verification failed' }`,
			`if ((Get-FileHash -Algorithm SHA256 -LiteralPath $binary).Hash.ToLowerInvariant() -cne `+psQuote(
				strings.TrimPrefix(m.Binary.Digest, "sha256:"),
			)+`) { throw 'Module hash differs from lock' }`,
			`$manifest = Get-Content -LiteralPath (Join-Path $module 'module.manifest.json') -Raw | ConvertFrom-Json`,
			`if ($manifest.id -cne `+psQuote(
				m.ID,
			)+` -or $manifest.version -cne `+psQuote(
				m.Version,
			)+`) { throw 'Module manifest differs from lock' }`,
		)
	}
	lines = append(
		lines,
		`Stop-Service -Name WeaveAgent -ErrorAction Stop`,
		`if ((Get-Service -Name WeaveAgent).Status -ne 'Stopped') { throw 'Agent did not stop before sealing' }`,
		`Set-Service -Name WeaveAgent -StartupType Automatic`,
		`foreach ($name in @('channel.pub','store.key','store.db','store.db-wal','store.db-shm')) {`,
		`  $path = Join-Path (Join-Path $env:ProgramData 'weave') $name`,
		`  if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Force }`,
		"}",
		"} finally {",
		`  Remove-Item -LiteralPath $work -Recurse -Force`,
		"}",
	)
	return strings.Join(lines, "\n") + "\n"
}
