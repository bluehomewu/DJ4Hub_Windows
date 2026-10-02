<#
.SYNOPSIS
  Build and package DJ 4G Hub for Windows.

.EXAMPLE
  pwsh -File scripts/build-windows.ps1
  pwsh -File scripts/build-windows.ps1 -Arch arm64 -SkipTests
#>
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',
    [switch]$SkipTests
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot

function Invoke-Native {
    param([string]$File, [string[]]$Arguments)
    & $File @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$File $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

$versionSource = Get-Content -Raw (Join-Path $root 'cmd/dj4ghub/version.go')
if ($versionSource -notmatch 'appVersion\s*=\s*"([0-9]+\.[0-9]+\.[0-9]+)"') {
    throw 'Cannot read appVersion from cmd/dj4ghub/version.go'
}
$version = $Matches[1]
$name = "DJ-4G-Hub-$version-windows-$Arch"
$dist = Join-Path $root 'dist'
$stage = Join-Path $dist $name

Push-Location $root
try {
    if (-not $SkipTests) {
        Invoke-Native go @('vet', './cmd/...')
        Invoke-Native go @('test', './...')
    }

    if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
    New-Item -ItemType Directory -Force $stage | Out-Null

    $env:GOOS = 'windows'
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = '0'
    try {
        Invoke-Native go @('build', '-trimpath', '-ldflags', '-s -w', '-o', (Join-Path $stage 'dj4ghub.exe'), './cmd/dj4ghub')
    } finally {
        Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
    }

    Copy-Item (Join-Path $root 'packaging/README.md') (Join-Path $stage 'README.md')
    Copy-Item (Join-Path $root 'packaging/THIRD_PARTY_NOTICES.md') (Join-Path $stage 'THIRD_PARTY_NOTICES.md')
    Copy-Item (Join-Path $root 'LICENSE') (Join-Path $stage 'LICENSE')

    $zip = Join-Path $dist "$name.zip"
    if (Test-Path $zip) { Remove-Item -Force $zip }
    Compress-Archive -Path (Join-Path $stage '*') -DestinationPath $zip
    $hash = (Get-FileHash -Algorithm SHA256 $zip).Hash.ToLowerInvariant()
    Set-Content -NoNewline -Encoding ascii -Path "$zip.sha256" -Value "$hash  $name.zip`n"

    Write-Host "Built $zip"
    Write-Host "SHA256 $hash"
} finally {
    Pop-Location
}
