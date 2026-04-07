# actrun - Actionforge graph runner (PowerShell)
#
# This script is the PowerShell equivalent of:
#   https://www.actionforge.dev/actrun.sh
#
# Usage:
#   pwsh -Command "& ([scriptblock]::Create((irm https://www.actionforge.dev/actrun.ps1))) <file.act> [options]"
#
# Examples:
#   pwsh -Command "& ([scriptblock]::Create((irm https://www.actionforge.dev/actrun.ps1))) hello-world.act"
#   pwsh -Command "& ([scriptblock]::Create((irm https://www.actionforge.dev/actrun.ps1))) workflow.act --verbose"
#   pwsh -Command "& ([scriptblock]::Create((irm https://www.actionforge.dev/actrun.ps1))) https://app.actionforge.dev/shared/wispy-paper-a49c664e.act"
#   pwsh -Command "& ([scriptblock]::Create((irm https://www.actionforge.dev/actrun.ps1))) --help"
#
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Arguments
)

$ErrorActionPreference = 'Stop'

$DownloadBase = 'https://www.actionforge.dev/download'
$ApiUrl = 'https://app.actionforge.dev/api/v2/releases/list'
$ShareApiUrl = 'https://app.actionforge.dev/api/v2/share/graph/read'
$CacheDir = if ($env:LOCALAPPDATA) {
    Join-Path $env:LOCALAPPDATA 'actrun'
} else {
    Join-Path $HOME '.cache' 'actrun'
}

# Detect OS
if ($IsWindows -or $env:OS -eq 'Windows_NT') {
    $OS = 'windows'
} elseif ($IsLinux) {
    $OS = 'linux'
} elseif ($IsMacOS) {
    $OS = 'macos'
} else {
    Write-Error "Unsupported OS"
    exit 1
}

# Detect architecture
$archRaw = if ($OS -eq 'windows') {
    $env:PROCESSOR_ARCHITECTURE
} else {
    (uname -m)
}

switch -Regex ($archRaw) {
    'AMD64|x86_64|amd64' { $Arch = 'x64' }
    'ARM64|arm64|aarch64' { $Arch = 'arm64' }
    default { Write-Error "Unsupported architecture: $archRaw"; exit 1 }
}

# Get latest version from API (highest stable version)
$releases = Invoke-RestMethod -Uri $ApiUrl
$Version = $releases |
    ForEach-Object { $_.version } |
    Where-Object { $_ -match '^v\d+\.\d+\.\d+$' } |
    Sort-Object { [version]($_ -replace '^v','') } |
    Select-Object -Last 1

if (-not $Version) {
    Write-Error "Failed to fetch latest version"
    exit 1
}

# Check cache
$CacheVersionDir = Join-Path $CacheDir $Version
$BinaryName = if ($OS -eq 'windows') { 'actrun.exe' } else { 'actrun' }
$CachedBinary = Join-Path $CacheVersionDir $BinaryName

# Helper: resolve shared URL
function Invoke-SharedGraph {
    param([string]$Url, [string]$Binary, [string[]]$Rest)
    if ($Url -match '^https://app\.actionforge\.dev/shared/([a-zA-Z0-9_-]+\.act)$') {
        $shareId = $Matches[1]
        Write-Host "Fetching shared graph: $shareId"
        $graphTmp = [System.IO.Path]::GetTempFileName() + '.act'
        try {
            Invoke-WebRequest -Uri "${ShareApiUrl}?id=${shareId}" -OutFile $graphTmp -UseBasicParsing
            & $Binary $graphTmp @Rest
        } finally {
            Remove-Item -Force $graphTmp -ErrorAction SilentlyContinue
        }
        return $true
    }
    return $false
}

if (Test-Path $CachedBinary) {
    Write-Host "Using cached actrun $Version"
    if ($Arguments.Count -gt 0) {
        $handled = Invoke-SharedGraph -Url $Arguments[0] -Binary $CachedBinary -Rest ($Arguments | Select-Object -Skip 1)
        if (-not $handled) {
            & $CachedBinary @Arguments
        }
    } else {
        & $CachedBinary
    }
    return
}

# Determine file extension
$Ext = switch ($OS) {
    'linux'   { 'tar.gz' }
    'windows' { 'zip' }
    'macos'   { 'pkg' }
}

$Filename = "actrun-${Version}.cli-${Arch}-${OS}.${Ext}"
$Url = "${DownloadBase}/${Filename}"

Write-Host "Downloading $Filename..."

$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "actrun-$([guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

try {
    $DownloadPath = Join-Path $TmpDir $Filename
    Invoke-WebRequest -Uri $Url -OutFile $DownloadPath -UseBasicParsing

    Write-Host "Unpacking..."

    switch ($OS) {
        'linux' {
            tar -xzf $DownloadPath -C $TmpDir
            $Binary = Join-Path $TmpDir 'actrun'
        }
        'windows' {
            Expand-Archive -Path $DownloadPath -DestinationPath $TmpDir -Force
            $Binary = Join-Path $TmpDir 'actrun.exe'
        }
        'macos' {
            $ExpandDir = Join-Path $TmpDir 'pkg_expanded'
            pkgutil --expand-full $DownloadPath $ExpandDir
            $Binary = Get-ChildItem -Path $ExpandDir -Recurse -Filter 'actrun' -File |
                Select-Object -First 1 -ExpandProperty FullName
        }
    }

    if (-not (Test-Path $Binary)) {
        Write-Error "Failed to find actrun binary"
        exit 1
    }

    # Cache the binary
    New-Item -ItemType Directory -Path $CacheVersionDir -Force | Out-Null
    Copy-Item -Path $Binary -Destination $CachedBinary -Force
    if ($OS -ne 'windows') {
        chmod +x $CachedBinary
    }

    Write-Host "Unpacked actrun $Version"

    if ($Arguments.Count -gt 0) {
        $handled = Invoke-SharedGraph -Url $Arguments[0] -Binary $CachedBinary -Rest ($Arguments | Select-Object -Skip 1)
        if (-not $handled) {
            & $CachedBinary @Arguments
        }
    } else {
        & $CachedBinary
    }
} finally {
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}
