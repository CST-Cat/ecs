[CmdletBinding()]
param(
    [string]$Repository = '',
    [string]$Version = '',
    [string]$ReleaseBase = '',
    [string]$InstallDirectory = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Stop-EcsWindowsInstall {
    param([Parameter(Mandatory)][string]$Message)
    throw "ecs install: $Message"
}

function Get-EcsReleaseBase {
    param(
        [Parameter(Mandatory)][string]$Repository,
        [Parameter(Mandatory)][string]$ReleaseVersion,
        [string]$Override
    )

    if ([string]::IsNullOrWhiteSpace($Override)) {
        if ($ReleaseVersion -eq 'latest') {
            $base = "https://github.com/$Repository/releases/latest/download"
        } else {
            if ($ReleaseVersion -notmatch '^[A-Za-z0-9._+-]+$') {
                Stop-EcsWindowsInstall "release version contains unsafe characters: $ReleaseVersion"
            }
            $base = "https://github.com/$Repository/releases/download/$ReleaseVersion"
        }
    } else {
        $base = $Override.TrimEnd('/')
    }
    if ($base -notmatch '^https://') {
        Stop-EcsWindowsInstall 'release base must use HTTPS'
    }
    return $base
}

function Get-EcsDownload {
    param(
        [Parameter(Mandatory)][string]$Uri,
        [Parameter(Mandatory)][string]$Destination
    )

    if ($Uri -notmatch '^https://') {
        Stop-EcsWindowsInstall "refusing non-HTTPS download: $Uri"
    }
    Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $Destination -ErrorAction Stop
    if (-not (Test-Path -LiteralPath $Destination -PathType Leaf) -or
        (Get-Item -LiteralPath $Destination).Length -eq 0) {
        Stop-EcsWindowsInstall "downloaded file is missing or empty: $Uri"
    }
}

function Get-EcsChecksum {
    param(
        [Parameter(Mandatory)][string]$ChecksumFile,
        [Parameter(Mandatory)][string]$Asset
    )

    $checksumMatches = @()
    foreach ($line in Get-Content -LiteralPath $ChecksumFile) {
        $match = [regex]::Match($line, '^\s*([0-9A-Fa-f]{64})\s+\*?(.+?)\s*$')
        if ($match.Success -and $match.Groups[2].Value -ceq $Asset) {
            $checksumMatches += $match.Groups[1].Value.ToLowerInvariant()
        }
    }
    if ($checksumMatches.Count -ne 1) {
        Stop-EcsWindowsInstall "checksums.txt must contain exactly one entry for $Asset"
    }
    return $checksumMatches[0]
}

function Assert-EcsSha256 {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Expected
    )

    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
    if ($actual -cne $Expected.ToLowerInvariant()) {
        Stop-EcsWindowsInstall "SHA-256 mismatch: expected $Expected, got $actual"
    }
}

function Assert-EcsRegularFile {
    param([Parameter(Mandatory)][string]$Path)
    $item = Get-Item -LiteralPath $Path -ErrorAction Stop
    if ($item.PSIsContainer -or (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        Stop-EcsWindowsInstall "expected a regular file: $Path"
    }
}

function Get-EcsSafeZipEntryNames {
    param([Parameter(Mandatory)][string]$ArchivePath)

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [IO.Compression.ZipFile]::OpenRead($ArchivePath)
    $names = @()
    try {
        foreach ($entry in $archive.Entries) {
            $name = [string]$entry.FullName
            if ([string]::IsNullOrEmpty($name) -or $name -match '[\x00-\x1F\x7F]') {
                Stop-EcsWindowsInstall 'ZIP contains an invalid member name'
            }
            if ($name -match '^[\\/]' -or $name -match '^[A-Za-z]:') {
                Stop-EcsWindowsInstall "ZIP contains an absolute member path: $name"
            }
            if ($name -match '\\') {
                Stop-EcsWindowsInstall "ZIP contains a backslash member path: $name"
            }
            $parts = $name.Split('/')
            for ($index = 0; $index -lt $parts.Count; $index++) {
                if ($parts[$index] -eq '..' -or
                    ($parts[$index].Length -eq 0 -and $index -ne ($parts.Count - 1))) {
                    Stop-EcsWindowsInstall "ZIP contains a directory-traversal member path: $name"
                }
            }
            if (@($names | Where-Object { $_ -ieq $name }).Count -ne 0) {
                Stop-EcsWindowsInstall "ZIP contains a duplicate member: $name"
            }
            $names += $name
        }
    } finally {
        $archive.Dispose()
    }
    return $names
}

function Assert-EcsExactZipEntries {
    param(
        [Parameter(Mandatory)][string[]]$Actual,
        [Parameter(Mandatory)][string[]]$Expected
    )

    $actualText = (@($Actual | Sort-Object) -join "`n")
    $expectedText = (@($Expected | Sort-Object) -join "`n")
    if ($actualText -cne $expectedText) {
        Stop-EcsWindowsInstall 'ECS ZIP has an unexpected member set'
    }
}

function Expand-EcsZipMembers {
    param(
        [Parameter(Mandatory)][string]$ArchivePath,
        [Parameter(Mandatory)][string]$Destination,
        [Parameter(Mandatory)][string[]]$Members
    )

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [IO.Compression.ZipFile]::OpenRead($ArchivePath)
    try {
        foreach ($member in $Members) {
            $entry = $archive.GetEntry($member)
            if ($null -eq $entry -or $entry.FullName.EndsWith('/')) {
                Stop-EcsWindowsInstall "ZIP is missing a regular member: $member"
            }
            $destinationPath = Join-Path $Destination $member
            $parent = Split-Path -Parent $destinationPath
            if (-not (Test-Path -LiteralPath $parent -PathType Container)) {
                New-Item -ItemType Directory -Path $parent -Force | Out-Null
            }
            $entryStream = $entry.Open()
            $fileStream = $null
            try {
                $fileStream = [IO.File]::Open($destinationPath, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::None)
                $entryStream.CopyTo($fileStream)
            } finally {
                if ($null -ne $fileStream) { $fileStream.Dispose() }
                $entryStream.Dispose()
            }
        }
    } finally {
        $archive.Dispose()
    }
}

$repository = if ([string]::IsNullOrWhiteSpace($Repository)) { $env:ECS_REPOSITORY } else { $Repository }
if ([string]::IsNullOrWhiteSpace($repository)) { $repository = 'CST-Cat/ecs' }
$version = if ([string]::IsNullOrWhiteSpace($Version)) { $env:ECS_VERSION } else { $Version }
if ([string]::IsNullOrWhiteSpace($version)) { $version = 'latest' }
$releaseOverride = if ([string]::IsNullOrWhiteSpace($ReleaseBase)) { $env:ECS_RELEASE_BASE } else { $ReleaseBase }

if ($repository -notmatch '^[^/\s]+/[^/\s]+$') {
    Stop-EcsWindowsInstall 'repository must be owner/repository'
}
$processor = if (-not [string]::IsNullOrWhiteSpace($env:PROCESSOR_ARCHITEW6432)) {
    $env:PROCESSOR_ARCHITEW6432
} else {
    $env:PROCESSOR_ARCHITECTURE
}
if ($processor -notmatch '^(?i:AMD64|x86_64)$') {
    Stop-EcsWindowsInstall "Windows v1 supports amd64 only (detected $processor)"
}

$mainBase = Get-EcsReleaseBase -Repository $repository -ReleaseVersion $version -Override $releaseOverride
$localAppData = $env:LOCALAPPDATA
if ([string]::IsNullOrWhiteSpace($localAppData)) {
    $localAppData = Join-Path $env:USERPROFILE 'AppData\Local'
}
if ([string]::IsNullOrWhiteSpace($InstallDirectory)) {
    $InstallDirectory = Join-Path $localAppData 'ecs\bin'
}
$localAppDataFull = [IO.Path]::GetFullPath($localAppData).TrimEnd('\')
$installDirectoryFull = [IO.Path]::GetFullPath($InstallDirectory).TrimEnd('\')
$userDirectoryPrefix = $localAppDataFull + '\'
if ($installDirectoryFull -cne $localAppDataFull -and
    -not $installDirectoryFull.StartsWith($userDirectoryPrefix, [StringComparison]::OrdinalIgnoreCase)) {
    Stop-EcsWindowsInstall 'install directory must remain under the current user local-data directory'
}

$work = Join-Path ([IO.Path]::GetTempPath()) ("ecs-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work -Force | Out-Null
$candidate = $null
try {
    $asset = 'ecs_windows_amd64.zip'
    $archive = Join-Path $work $asset
    $checksums = Join-Path $work 'checksums.txt'
    Get-EcsDownload -Uri "$mainBase/checksums.txt" -Destination $checksums
    Get-EcsDownload -Uri "$mainBase/$asset" -Destination $archive
    Assert-EcsSha256 -Path $archive -Expected (Get-EcsChecksum -ChecksumFile $checksums -Asset $asset)

    $members = @(Get-EcsSafeZipEntryNames -ArchivePath $archive)
    $expectedMembers = @('ecs.exe', 'LICENSE', 'NOTICE', 'README.md', 'README_EN.md', 'SECURITY.md', 'THIRD_PARTY.md')
    Assert-EcsExactZipEntries -Actual $members -Expected $expectedMembers
    Expand-EcsZipMembers -ArchivePath $archive -Destination $work -Members @('ecs.exe')
    $source = Join-Path $work 'ecs.exe'
    Assert-EcsRegularFile -Path $source

    New-Item -ItemType Directory -Path $InstallDirectory -Force | Out-Null
    $destination = Join-Path $InstallDirectory 'ecs.exe'
    $candidate = Join-Path $InstallDirectory ('.ecs.exe.' + [guid]::NewGuid().ToString('N') + '.tmp')
    Copy-Item -LiteralPath $source -Destination $candidate -Force
    Assert-EcsRegularFile -Path $candidate
    Move-Item -LiteralPath $candidate -Destination $destination -Force
    $candidate = $null
    Write-Output "installed $destination"
} finally {
    if ($null -ne $candidate -and (Test-Path -LiteralPath $candidate)) {
        Remove-Item -LiteralPath $candidate -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $work) {
        Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
    }
}
