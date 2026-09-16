$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$EcsArguments = @($args)

function Stop-EcsWindowsRun {
    param([Parameter(Mandatory)][string]$Message)
    throw "ecs run: $Message"
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
                Stop-EcsWindowsRun "release version contains unsafe characters: $ReleaseVersion"
            }
            $base = "https://github.com/$Repository/releases/download/$ReleaseVersion"
        }
    } else {
        $base = $Override.TrimEnd('/')
    }
    if ($base -notmatch '^https://') {
        Stop-EcsWindowsRun "release base must use HTTPS"
    }
    return $base
}

function Get-EcsDownload {
    param(
        [Parameter(Mandatory)][string]$Uri,
        [Parameter(Mandatory)][string]$Destination
    )

    if ($Uri -notmatch '^https://') {
        Stop-EcsWindowsRun "refusing non-HTTPS download: $Uri"
    }
    Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $Destination -ErrorAction Stop
    if (-not (Test-Path -LiteralPath $Destination -PathType Leaf) -or
        (Get-Item -LiteralPath $Destination).Length -eq 0) {
        Stop-EcsWindowsRun "downloaded file is missing or empty: $Uri"
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
        Stop-EcsWindowsRun "checksums.txt must contain exactly one entry for $Asset"
    }
    return $checksumMatches[0]
}

function Assert-EcsSha256 {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Expected,
        [Parameter(Mandatory)][string]$Label
    )

    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
    if ($actual -cne $Expected.ToLowerInvariant()) {
        Stop-EcsWindowsRun "$Label SHA-256 mismatch: expected $Expected, got $actual"
    }
}

function Assert-EcsRegularFile {
    param([Parameter(Mandatory)][string]$Path)
    $item = Get-Item -LiteralPath $Path -ErrorAction Stop
    if ($item.PSIsContainer -or (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        Stop-EcsWindowsRun "expected a regular file: $Path"
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
                Stop-EcsWindowsRun "ZIP contains an invalid member name"
            }
            if ($name -match '^[\\/]' -or $name -match '^[A-Za-z]:') {
                Stop-EcsWindowsRun "ZIP contains an absolute member path: $name"
            }
            if ($name -match '\\') {
                Stop-EcsWindowsRun "ZIP contains a backslash member path: $name"
            }
            $parts = $name.Split('/')
            for ($index = 0; $index -lt $parts.Count; $index++) {
                if ($parts[$index] -eq '..' -or
                    ($parts[$index].Length -eq 0 -and $index -ne ($parts.Count - 1))) {
                    Stop-EcsWindowsRun "ZIP contains a directory-traversal member path: $name"
                }
            }
            if (@($names | Where-Object { $_ -ieq $name }).Count -ne 0) {
                Stop-EcsWindowsRun "ZIP contains a duplicate member: $name"
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
        [Parameter(Mandatory)][string[]]$Expected,
        [Parameter(Mandatory)][string]$Label
    )

    $actualText = (@($Actual | Sort-Object) -join "`n")
    $expectedText = (@($Expected | Sort-Object) -join "`n")
    if ($actualText -cne $expectedText) {
        Stop-EcsWindowsRun "$Label has an unexpected member set"
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
                Stop-EcsWindowsRun "ZIP is missing a regular member: $member"
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

function Expand-EcsCorpus {
    param(
        [Parameter(Mandatory)][string]$ArchivePath,
        [Parameter(Mandatory)][string]$Destination,
        [Parameter(Mandatory)][string]$Member
    )

    $listing = @(& tar.exe -tzf $ArchivePath)
    if ($LASTEXITCODE -ne 0) {
        Stop-EcsWindowsRun 'could not inspect the fixed corpus archive'
    }
    foreach ($name in $listing) {
        if ($name -match '^[\\/]' -or $name -match '\\' -or "/$name/" -match '/\.\./') {
            Stop-EcsWindowsRun "corpus archive contains an unsafe member path: $name"
        }
    }
    if ($listing.Count -ne 1 -or $listing[0] -cne $Member) {
        Stop-EcsWindowsRun 'fixed corpus archive has an unexpected member set'
    }
    New-Item -ItemType Directory -Path $Destination -Force | Out-Null
    & tar.exe -xzf $ArchivePath -C $Destination
    if ($LASTEXITCODE -ne 0) {
        Stop-EcsWindowsRun 'could not extract the fixed corpus archive'
    }
    $path = Join-Path $Destination $Member
    Assert-EcsRegularFile -Path $path
    return $path
}

$repository = if ([string]::IsNullOrWhiteSpace($env:ECS_REPOSITORY)) { 'CST-Cat/ecs' } else { $env:ECS_REPOSITORY }
$version = if ([string]::IsNullOrWhiteSpace($env:ECS_VERSION)) { 'latest' } else { $env:ECS_VERSION }
$releaseOverride = $env:ECS_RELEASE_BASE
$bundleOverride = $env:ECS_BUNDLE_RELEASE_BASE

if ($repository -notmatch '^[^/\s]+/[^/\s]+$') {
    Stop-EcsWindowsRun "repository must be owner/repository"
}
$processor = if (-not [string]::IsNullOrWhiteSpace($env:PROCESSOR_ARCHITEW6432)) {
    $env:PROCESSOR_ARCHITEW6432
} else {
    $env:PROCESSOR_ARCHITECTURE
}
if ($processor -notmatch '^(?i:AMD64|x86_64)$') {
    Stop-EcsWindowsRun "Windows v1 supports amd64 only (detected $processor)"
}

$mainBase = Get-EcsReleaseBase -Repository $repository -ReleaseVersion $version -Override $releaseOverride
$tempRoot = [IO.Path]::GetTempPath()
$work = Join-Path $tempRoot ("ecs-run-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work -Force | Out-Null
$hadToolBin = Test-Path Env:ECS_TOOL_BIN
$oldToolBin = $env:ECS_TOOL_BIN
$hadCorpus = Test-Path Env:ECS_ZSTD_CORPUS
$oldCorpus = $env:ECS_ZSTD_CORPUS

try {
    $mainAsset = 'ecs_windows_amd64.zip'
    $mainArchive = Join-Path $work $mainAsset
    $mainChecksums = Join-Path $work 'checksums.txt'
    Get-EcsDownload -Uri "$mainBase/checksums.txt" -Destination $mainChecksums
    Get-EcsDownload -Uri "$mainBase/$mainAsset" -Destination $mainArchive
    Assert-EcsSha256 -Path $mainArchive -Expected (Get-EcsChecksum -ChecksumFile $mainChecksums -Asset $mainAsset) -Label $mainAsset

    $mainMembers = @(Get-EcsSafeZipEntryNames -ArchivePath $mainArchive)
    $expectedMainMembers = @('ecs.exe', 'LICENSE', 'NOTICE', 'README.md', 'README_EN.md', 'SECURITY.md', 'THIRD_PARTY.md')
    Assert-EcsExactZipEntries -Actual $mainMembers -Expected $expectedMainMembers -Label $mainAsset
    Expand-EcsZipMembers -ArchivePath $mainArchive -Destination $work -Members $expectedMainMembers
    $ecsPath = Join-Path $work 'ecs.exe'
    Assert-EcsRegularFile -Path $ecsPath

    $planOutput = & $ecsPath plan @($EcsArguments)
    if ($LASTEXITCODE -ne 0) {
        Stop-EcsWindowsRun "ecs plan failed with exit code $LASTEXITCODE"
    }
    $planText = (($planOutput | ForEach-Object { [string]$_ }) -join "`n").Trim()
    if ([string]::IsNullOrWhiteSpace($planText)) {
        Stop-EcsWindowsRun 'ecs plan returned no JSON'
    }
    try {
        $plan = $planText | ConvertFrom-Json
    } catch {
        Stop-EcsWindowsRun "ecs plan returned invalid JSON: $($_.Exception.Message)"
    }
    if ([string]$plan.schema_version -cne 'ecs.plan/v1') {
        Stop-EcsWindowsRun 'ecs plan returned an unsupported schema'
    }
    if ($null -eq $plan.PSObject.Properties['required_tools']) {
        Stop-EcsWindowsRun 'ecs plan has no required_tools field'
    }

    $requiredTools = @($plan.required_tools)
    $toolFiles = @()
    foreach ($tool in $requiredTools) {
        if ($tool -isnot [string] -or $tool -notmatch '^[A-Za-z0-9][A-Za-z0-9_-]*$') {
            Stop-EcsWindowsRun "ecs plan contains an unsafe required tool name"
        }
        if ($toolFiles -contains "${tool}.exe") {
            Stop-EcsWindowsRun "ecs plan contains a duplicate required tool: $tool"
        }
        $toolFiles += "${tool}.exe"
    }

    $toolBin = Join-Path $work 'tool-bin'
    New-Item -ItemType Directory -Path $toolBin -Force | Out-Null
    if ($toolFiles.Count -gt 0) {
        $bundleVersionOutput = & $ecsPath version --bundle
        if ($LASTEXITCODE -ne 0) {
            Stop-EcsWindowsRun 'ecs could not report its Bundle version'
        }
        $bundleVersion = (($bundleVersionOutput | ForEach-Object { [string]$_ }) -join "`n").Trim()
        if ($bundleVersion -notmatch '^bundle-v[0-9A-Za-z._+-]+$') {
            Stop-EcsWindowsRun "ecs returned an invalid Bundle version: $bundleVersion"
        }
        $bundleBase = Get-EcsReleaseBase -Repository $repository -ReleaseVersion $bundleVersion -Override $bundleOverride
        $toolsAsset = 'ecs-tools_windows_amd64.zip'
        $toolsArchive = Join-Path $work $toolsAsset
        $toolsChecksums = Join-Path $work 'bundle-checksums.txt'
        Get-EcsDownload -Uri "$bundleBase/checksums.txt" -Destination $toolsChecksums
        Get-EcsDownload -Uri "$bundleBase/$toolsAsset" -Destination $toolsArchive
        Assert-EcsSha256 -Path $toolsArchive -Expected (Get-EcsChecksum -ChecksumFile $toolsChecksums -Asset $toolsAsset) -Label $toolsAsset

        $toolMembers = @(Get-EcsSafeZipEntryNames -ArchivePath $toolsArchive)
        $toolExtract = Join-Path $work 'tool-extract'
        New-Item -ItemType Directory -Path $toolExtract -Force | Out-Null
        foreach ($toolFile in $toolFiles) {
            $toolMember = "bin/$toolFile"
            if (-not (@($toolMembers | Where-Object { $_ -ceq $toolMember }).Count -eq 1)) {
                Stop-EcsWindowsRun "required tool is missing from the Windows Bundle: $toolFile"
            }
            Expand-EcsZipMembers -ArchivePath $toolsArchive -Destination $toolExtract -Members @($toolMember)
            $extractedTool = Join-Path $toolExtract $toolMember
            Assert-EcsRegularFile -Path $extractedTool
            Copy-Item -LiteralPath $extractedTool -Destination (Join-Path $toolBin $toolFile) -Force
            Assert-EcsRegularFile -Path (Join-Path $toolBin $toolFile)
        }
        if ($toolFiles -contains 'zstd.exe') {
            $corpusAsset = 'ecs-corpus_silesia-v1.tar.gz'
            $corpusArchive = Join-Path $work $corpusAsset
            Get-EcsDownload -Uri "$bundleBase/$corpusAsset" -Destination $corpusArchive
            Assert-EcsSha256 -Path $corpusArchive -Expected (Get-EcsChecksum -ChecksumFile $toolsChecksums -Asset $corpusAsset) -Label $corpusAsset
            $corpusDestination = Join-Path $work 'corpus'
            $corpusPath = Expand-EcsCorpus -ArchivePath $corpusArchive -Destination $corpusDestination -Member 'ecs-silesia-v1.corpus'
            $env:ECS_ZSTD_CORPUS = $corpusPath
        }
    }

    $env:ECS_TOOL_BIN = $toolBin
    & $ecsPath @($EcsArguments)
    $runStatus = $LASTEXITCODE
} finally {
    if ($hadToolBin) {
        $env:ECS_TOOL_BIN = $oldToolBin
    } else {
        [Environment]::SetEnvironmentVariable('ECS_TOOL_BIN', $null, 'Process')
    }
    if ($hadCorpus) {
        $env:ECS_ZSTD_CORPUS = $oldCorpus
    } else {
        [Environment]::SetEnvironmentVariable('ECS_ZSTD_CORPUS', $null, 'Process')
    }
    if (Test-Path -LiteralPath $work) {
        Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
    }
}

exit $runStatus
