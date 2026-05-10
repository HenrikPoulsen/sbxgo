# install.ps1 - installs sbxgo from GitHub Releases on Windows
#
# Usage from cmd.exe or PowerShell:
#   :: latest
#   powershell -c "irm https://raw.githubusercontent.com/HenrikPoulsen/sbxgo/main/install.ps1 | iex"
#
#   :: pin to a specific version
#   powershell -c "$env:SBXGO_VERSION='v0.3.0'; irm https://raw.githubusercontent.com/HenrikPoulsen/sbxgo/main/install.ps1 | iex"
#
# Inputs (env vars):
#   SBXGO_VERSION       Tag to install (e.g. "v0.3.0" or "0.3.0"). Defaults to the latest release.
#   SBXGO_INSTALL_DIR   Install directory. Defaults to %LOCALAPPDATA%\Programs\sbxgo.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

[Net.ServicePointManager]::SecurityProtocol = `
    [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$Repo   = 'HenrikPoulsen/sbxgo'
$Binary = 'sbxgo'

# detect arch

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    default {
        throw "Unsupported Windows architecture: $($env:PROCESSOR_ARCHITECTURE)"
    }
}

$os    = 'windows'
$asset = "${Binary}_${os}_${arch}.exe"

# resolve install dir

$installDir = if ($env:SBXGO_INSTALL_DIR) {
    $env:SBXGO_INSTALL_DIR
} else {
    Join-Path $env:LOCALAPPDATA "Programs\$Binary"
}
$installPath = Join-Path $installDir "$Binary.exe"

if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
}

# resolve version

$requestedVersion = $env:SBXGO_VERSION
if ($requestedVersion -and $requestedVersion -notmatch '^v') {
    $requestedVersion = "v$requestedVersion"
}

if ($requestedVersion) {
    $version = $requestedVersion
    Write-Host "Using requested version: $version"
} else {
    function Get-FinalUrl([string]$Url) {
        $r = Invoke-WebRequest -Uri $Url -UseBasicParsing
        if ($r.BaseResponse.PSObject.Properties['ResponseUri']) {
            return $r.BaseResponse.ResponseUri.AbsoluteUri
        }
        return $r.BaseResponse.RequestMessage.RequestUri.AbsoluteUri
    }

    $latestUrl = "https://github.com/$Repo/releases/latest"
    $finalUrl  = Get-FinalUrl $latestUrl
    $version   = ($finalUrl -split '/')[-1]

    if (-not $version -or $version -eq 'latest') {
        throw "Could not determine latest release version (resolved URL: $finalUrl)."
    }
}

Write-Host "Installing $Binary $version ($os/$arch)"

# install binary

$assetUrl = "https://github.com/$Repo/releases/download/$version/$asset"

Write-Host "Downloading $Binary..."
try {
    Invoke-WebRequest -Uri $assetUrl -OutFile $installPath -UseBasicParsing
} catch {
    $msg = "Failed to download $Binary from $assetUrl`n  $($_.Exception.Message)"
    if ($requestedVersion) {
        $msg += "`n  Check that $requestedVersion exists at https://github.com/$Repo/releases"
    }
    throw $msg
}

# verify checksum

$checksumsUrl = "https://github.com/$Repo/releases/download/$version/checksums.txt"
$checksums    = $null
try {
    $response = Invoke-WebRequest -Uri $checksumsUrl -UseBasicParsing
    $checksums = if ($response.Content -is [byte[]]) {
        [System.Text.Encoding]::UTF8.GetString($response.Content)
    } else {
        $response.Content
    }
} catch {
    Write-Warning "Could not fetch $checksumsUrl; skipping checksum verification"
}

if ($checksums) {
    $expected = $checksums -split "`n" |
        ForEach-Object {
            $parts = $_ -split '\s+'
            if ($parts.Count -ge 2 -and $parts[1].Trim() -eq $asset) { $parts[0].Trim() }
        } |
        Select-Object -First 1

    if (-not $expected) {
        Write-Warning "$asset not listed in checksums.txt; skipping verification"
    } else {
        $actual = (Get-FileHash -Algorithm SHA256 -Path $installPath).Hash.ToLower()
        if ($actual -ne $expected.ToLower()) {
            Remove-Item -Path $installPath -Force -ErrorAction SilentlyContinue
            throw "Checksum mismatch for ${asset}:`n  expected: $expected`n  actual:   $actual"
        }
        Write-Host "Checksum verified ($expected)."
    }
}

Write-Host "Installed $Binary to $installPath"

# add to PATH

$userPath         = [Environment]::GetEnvironmentVariable('Path', 'User')
$userPathSegments = if ($userPath) { $userPath -split ';' | Where-Object { $_ } } else { @() }

if ($userPathSegments -notcontains $installDir) {
    $newPath = if ($userPath) { "$userPath;$installDir" } else { $installDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    Write-Host ""
    Write-Host "Added $installDir to your user PATH."
    Write-Host "Open a new terminal to pick up the change."
}

Write-Host ""
Write-Host "Next steps:"
Write-Host "  1. cd into your project repository"
Write-Host "  2. Run: $Binary setup       # scaffolds .sbxgo\config.toml on first run"
Write-Host "  3. Edit .sbxgo\config.toml"
Write-Host "  4. Everyday use: $Binary run"
Write-Host ""
Write-Host "Docs: https://github.com/$Repo"
