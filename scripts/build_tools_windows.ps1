[CmdletBinding()]
param(
    [ValidateSet('windows_amd64')]
    [string]$Target = 'windows_amd64',
    [string]$StageRoot,
    [string]$WorkRoot,
    [switch]$PrintParams
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
. (Join-Path $RepoRoot 'scripts\tools\windows\common.ps1')

function Stop-EcsWindowsBuild {
    param([Parameter(Mandatory)][string]$Message)
    throw "build-tools-windows: $Message"
}

function Get-EcsWindowsMsysGpgAgentProcessIds {
    param([Parameter(Mandatory)][string]$MsysRoot)

    $agentPath = [IO.Path]::GetFullPath((Join-Path $MsysRoot 'usr\bin\gpg-agent.exe'))
    $processIds = @()
    foreach ($process in [Diagnostics.Process]::GetProcessesByName('gpg-agent')) {
        try {
            if ($process.HasExited) { continue }
            $processPath = [IO.Path]::GetFullPath($process.MainModule.FileName)
            if ($processPath.Equals($agentPath, [StringComparison]::OrdinalIgnoreCase)) {
                $processIds += $process.Id
            }
        } catch [InvalidOperationException] {
            # The agent can exit between process enumeration and inspection.
            continue
        } catch [System.ComponentModel.Win32Exception] {
            if ($process.HasExited) { continue }
            throw "could not inspect locked MSYS2 gpg-agent process: $($_.Exception.Message)"
        } catch {
            throw "could not inspect locked MSYS2 gpg-agent process: $($_.Exception.Message)"
        } finally {
            $process.Dispose()
        }
    }
    return @($processIds | Sort-Object -Unique)
}

function Wait-EcsWindowsMsysGpgAgentExit {
    param([Parameter(Mandatory)][string]$MsysRoot)

    $attempts = 40
    $delayMilliseconds = 250
    $lastProcessIds = @()
    for ($attempt = 1; $attempt -le $attempts; $attempt++) {
        $lastProcessIds = @(Get-EcsWindowsMsysGpgAgentProcessIds -MsysRoot $MsysRoot)
        if ($lastProcessIds.Count -eq 0) { return }
        if ($attempt -lt $attempts) {
            Start-Sleep -Milliseconds $delayMilliseconds
        }
    }
    throw "locked MSYS2 gpg-agent did not exit after $($attempts * $delayMilliseconds)ms (process ids: $($lastProcessIds -join ', '))"
}

function Invoke-EcsWindowsMsysGpgCleanup {
    param([Parameter(Mandatory)][string]$MsysRoot)

    $bash = Join-Path $MsysRoot 'usr\bin\bash.exe'
    $gpgconf = Join-Path $MsysRoot 'usr\bin\gpgconf.exe'
    $gpgConnectAgent = Join-Path $MsysRoot 'usr\bin\gpg-connect-agent.exe'
    $gpgHome = Join-Path $MsysRoot 'etc\pacman.d\gnupg'
    if (-not (Test-Path -LiteralPath $bash -PathType Leaf) -or
        -not (Test-Path -LiteralPath $gpgconf -PathType Leaf) -or
        -not (Test-Path -LiteralPath $gpgHome -PathType Container)) {
        return
    }
    if (-not (Test-Path -LiteralPath $gpgConnectAgent -PathType Leaf)) {
        throw "locked MSYS2 base is missing gpg-connect-agent.exe: $gpgConnectAgent"
    }
    $msysUsrBinPosix = ConvertTo-EcsMsysPath (Join-Path $MsysRoot 'usr\bin')
    $cleanupContext = [pscustomobject]@{ Bash = $bash }
    # pacman-key uses /etc/pacman.d/gnupg, while gpgconf otherwise defaults to
    # ~/.gnupg. Keep shutdown in the same locked MSYS2 Bash context as the
    # transaction so POSIX home and socket resolution address that agent.
    $cleanupScript = @'
export PATH=__MSYS_USR_BIN__
set -eu
gpgHome=/etc/pacman.d/gnupg
agentSocket=$(gpgconf --homedir "$gpgHome" --list-dir agent-socket)
if test -n "$agentSocket"; then
    if ! gpg-connect-agent --no-autostart --raw-socket "$agentSocket" KILLAGENT /bye; then
        printf '%s\n' 'MSYS2 gpg-connect-agent raw socket shutdown did not complete; using homedir shutdown' >&2
    fi
fi
gpgconf --homedir "$gpgHome" --kill all
gpg-connect-agent --no-autostart --homedir "$gpgHome" KILLAGENT /bye
'@
    $cleanupScript = $cleanupScript.Replace('__MSYS_USR_BIN__', (ConvertTo-EcsBashLiteral $msysUsrBinPosix))
    Invoke-EcsWindowsBash -Context $cleanupContext -Script $cleanupScript
    Wait-EcsWindowsMsysGpgAgentExit -MsysRoot $MsysRoot
}

function Remove-EcsWindowsWorkRoot {
    param([Parameter(Mandatory)][string]$Path)

    $attempts = 20
    $lastError = $null
    for ($attempt = 1; $attempt -le $attempts; $attempt++) {
        try {
            Remove-Item -LiteralPath $Path -Recurse -Force -ErrorAction Stop
            return
        } catch {
            $lastError = $_
            if ($attempt -lt $attempts) {
                Start-Sleep -Milliseconds 250
            }
        }
    }
    throw "MSYS2 work directory cleanup failed after $attempts attempts: $($lastError.Exception.Message)"
}

function Get-EcsJsonFile {
    param([Parameter(Mandatory)][string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        Stop-EcsWindowsBuild "missing JSON lock: $Path"
    }
    try {
        return Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    } catch {
        Stop-EcsWindowsBuild "invalid JSON lock $Path`: $($_.Exception.Message)"
    }
}

function Copy-EcsLicense {
    param(
        [Parameter(Mandatory)][string]$Destination,
        [Parameter(Mandatory)][string[]]$Candidates
    )
    foreach ($candidate in $Candidates) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            Copy-Item -LiteralPath $candidate -Destination $Destination -Force
            return
        }
    }
    Stop-EcsWindowsBuild "upstream license file is missing: $($Candidates -join ', ')"
}

function Test-EcsWindowsAllowedImport {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string[]]$Allowlist
    )
    foreach ($pattern in $Allowlist) {
        if ($Name -like $pattern) { return $true }
    }
    return $false
}

function Get-EcsWindowsPeFacts {
    param(
        [Parameter(Mandatory)][string]$ObjdumpPath,
        [Parameter(Mandatory)][string]$BinaryPath,
        [Parameter(Mandatory)][string[]]$Allowlist
    )

    $peOutput = (& $ObjdumpPath -p $BinaryPath 2>&1) -join "`n"
    if ($LASTEXITCODE -ne 0) {
        Stop-EcsWindowsBuild "PE inspection failed for $([IO.Path]::GetFileName($BinaryPath))"
    }
    $machineMatch = [regex]::Match($peOutput, '(?im)\bfile format\s+(?<machine>\S+)')
    $machine = if ($machineMatch.Success) { $machineMatch.Groups['machine'].Value } else { '' }
    if ($machine -ne 'pei-x86-64') {
        Stop-EcsWindowsBuild "$([IO.Path]::GetFileName($BinaryPath)) is not a PE x86-64 executable"
    }
    $imports = @([regex]::Matches($peOutput, '(?im)^\s*DLL Name:\s*(?<name>\S+)\s*$') | ForEach-Object { $_.Groups['name'].Value } | Sort-Object -Unique)
    $nonSystemImports = @(
        foreach ($import in $imports) {
            if (-not (Test-EcsWindowsAllowedImport -Name $import -Allowlist $Allowlist) -or
                $import -match '(?i)^(libgcc|libgomp|libwinpthread|libstdc\+\+|libgfortran|libssl|libcrypto|cygwin|msys)') {
                $import
            }
        }
    )
    $fullyStatic = $nonSystemImports.Count -eq 0
    if (-not $fullyStatic) {
        Stop-EcsWindowsBuild "$([IO.Path]::GetFileName($BinaryPath)) imports non-self-contained DLLs: $($nonSystemImports -join ', ')"
    }
    $sectionOutput = (& $ObjdumpPath -h $BinaryPath 2>&1) -join "`n"
    if ($LASTEXITCODE -ne 0) {
        Stop-EcsWindowsBuild "PE section inspection failed for $([IO.Path]::GetFileName($BinaryPath))"
    }
    $symbolOutput = (& $ObjdumpPath -t $BinaryPath 2>&1) -join "`n"
    if ($LASTEXITCODE -ne 0) {
        Stop-EcsWindowsBuild "PE symbol inspection failed for $([IO.Path]::GetFileName($BinaryPath))"
    }
    $sectionNames = @(
        [regex]::Matches($sectionOutput, '(?im)^\s*\d+\s+(?<name>\.[^\s]+)(?:\s|$)') |
            ForEach-Object { $_.Groups['name'].Value }
    )
    $forbiddenSections = @($sectionNames | Where-Object {
        $_ -match '^\.(?:debug|zdebug|stab|gnu_debug|symtab|strtab)'
    })
    $hasDebuggingSectionFlag = $sectionOutput -match '(?im)^\s+.*\bDEBUGGING\b'
    $noSymbols = $symbolOutput -match '(?im)^\s*no symbols\s*$'
    $stripped = ($forbiddenSections.Count -eq 0) -and (-not $hasDebuggingSectionFlag) -and $noSymbols
    return [ordered]@{
        pe_machine = $machine
        imports = @($imports)
        imports_checked = $true
        fully_static = [bool]$fullyStatic
        stripped = [bool]$stripped
    }
}

function Get-EcsToolSource {
    param([Parameter(Mandatory)][pscustomobject]$Tool)
    if ($Tool.PSObject.Properties.Name -contains 'repository') {
        return "git+https://github.com/$($Tool.repository).git@$($Tool.commit)"
    }
    if ($Tool.PSObject.Properties.Name -contains 'source_url') {
        return [string]$Tool.source_url
    }
    Stop-EcsWindowsBuild "tool $($Tool.name) has no canonical source"
}

function Get-EcsToolField {
    param(
        [Parameter(Mandatory)][pscustomobject]$Tool,
        [Parameter(Mandatory)][string]$Name
    )
    if ($Tool.PSObject.Properties.Name -contains $Name) {
        return $Tool.$Name
    }
    return $null
}

function New-EcsManifestTool {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][pscustomobject]$Tool,
        [Parameter(Mandatory)][System.Collections.IDictionary]$Fact,
        [Parameter(Mandatory)][string]$BinaryPath,
        [Parameter(Mandatory)][string[]]$EnabledFeatures,
        [Parameter(Mandatory)][AllowEmptyCollection()][string[]]$DisabledFeatures,
        [Parameter(Mandatory)][string]$License,
        [Parameter(Mandatory)][string[]]$DependencyAllowlist,
        [Parameter(Mandatory)][System.Collections.IDictionary]$PeFacts
    )

    $binaryHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $BinaryPath).Hash.ToLowerInvariant()
    $parameters = [ordered]@{
        binary_sha256 = $binaryHash
        pe_machine = [string]$PeFacts['pe_machine']
        pe_imports = @($PeFacts['imports'])
        dependency_allowlist = @($DependencyAllowlist)
        imports_checked = [bool]$PeFacts['imports_checked']
        fully_static = [bool]$PeFacts['fully_static']
        stripped = [bool]$PeFacts['stripped']
    }
    foreach ($key in $Fact.Keys) {
        if ($key -notin @('build_flags', 'license')) {
            $parameters[$key] = $Fact[$key]
        }
    }
    $version = Get-EcsToolField $Tool 'version'
    $tag = Get-EcsToolField $Tool 'tag'
    if ($null -eq $version) { $version = $Fact['npb_version'] }
    if ($null -eq $tag) { $tag = $Fact['npb_tag'] }
    if ($null -eq $version) { $version = $Fact['stream_version'] }
    if ($null -eq $tag) { $tag = $Fact['stream_revision'] }
    return [ordered]@{
        name = $Name
        upstream = [string]$Tool.upstream
        version = [string]$version
        tag_or_commit = [string]$tag
        source = Get-EcsToolSource $Tool
        build_flags = @($Fact['build_flags'])
        enabled_features = @($EnabledFeatures)
        disabled_features = @($DisabledFeatures)
        architecture = 'amd64'
        license = $License
        parameters = $parameters
    }
}

$lock = Get-EcsJsonFile (Join-Path $RepoRoot 'tools\lock.json')
if ([string]$lock.schema_version -ne 'ecs.tools.lock/v1') {
    Stop-EcsWindowsBuild "unsupported tools lock schema: $($lock.schema_version)"
}
$targetFacts = @($lock.architectures | Where-Object { $_.target -eq $Target })
if ($targetFacts.Count -ne 1 -or [string]$targetFacts[0].goos -ne 'windows' -or [string]$targetFacts[0].goarch -ne 'amd64') {
    Stop-EcsWindowsBuild "lock has no unique windows_amd64 target"
}
$targetFacts = $targetFacts[0]
$windowToolNames = @($lock.windows_tools)
$sourceBuiltToolNames = @('zstd', 'npb-ep', 'npb-ft', 'openssl', 'stream', 'fio')
$prebuiltToolNames = @('nexttrace-tiny')
$expectedToolNames = @($sourceBuiltToolNames + $prebuiltToolNames)
if (($windowToolNames -join '|') -ne ($expectedToolNames -join '|')) {
    Stop-EcsWindowsBuild "Windows tool set is not the frozen seven-tool contract"
}
if ($windowToolNames -contains 'ping' -or @($windowToolNames | Where-Object { $_ -in @('sysbench', 'iperf3') }).Count -ne 0) {
    Stop-EcsWindowsBuild 'unsupported tool entered the Windows bundle'
}

$nexttraceTool = Get-EcsLockedTool -Lock $lock -Name 'nexttrace-tiny'
if ([string]$nexttraceTool.upstream -ne 'https://github.com/nxtrace/NTrace-core' -or
    [string]$nexttraceTool.repository -ne 'nxtrace/NTrace-core' -or
    [string]$nexttraceTool.version -ne '1.7.1' -or
    [string]$nexttraceTool.tag -ne 'v1.7.1' -or
    [string]$nexttraceTool.commit -ne 'c9919828fcd8c3103827d08bb26d69e9bf538299') {
    Stop-EcsWindowsBuild 'NextTrace lock identity is not the pinned v1.7.1 upstream commit'
}
$nexttracePatternProperty = $nexttraceTool.PSObject.Properties['windows_asset_pattern']
$nexttraceDigestProperty = $nexttraceTool.PSObject.Properties['windows_asset_sha256']
if ($null -eq $nexttracePatternProperty -or $null -eq $nexttraceDigestProperty) {
    Stop-EcsWindowsBuild 'NextTrace Windows release asset metadata is missing'
}
$nexttraceAssetPattern = [string]$nexttracePatternProperty.Value
if ($nexttraceAssetPattern -ne 'nexttrace-tiny_windows_<architecture>.exe') {
    Stop-EcsWindowsBuild 'NextTrace Windows asset pattern is not the pinned release pattern'
}
$nexttraceDigests = $nexttraceDigestProperty.Value
$nexttraceDigestEntry = $nexttraceDigests.PSObject.Properties['amd64']
if ($null -eq $nexttraceDigestEntry) {
    Stop-EcsWindowsBuild 'NextTrace Windows AMD64 asset digest is missing'
}
$nexttraceExpectedSha256 = [string]$nexttraceDigestEntry.Value
if ($nexttraceExpectedSha256 -ne '16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b' -or
    $nexttraceExpectedSha256 -notmatch '^[0-9a-f]{64}$') {
    Stop-EcsWindowsBuild 'NextTrace Windows AMD64 asset digest is not the pinned SHA-256'
}
$nexttraceAssetName = ([string]$nexttraceAssetPattern).Replace('<architecture>', [string]$targetFacts.package)
if ($nexttraceAssetName -ne 'nexttrace-tiny_windows_amd64.exe') {
    Stop-EcsWindowsBuild "NextTrace Windows asset name is not pinned for $Target"
}
$nexttraceAssetUrl = "https://github.com/$($nexttraceTool.repository)/releases/download/$($nexttraceTool.tag)/$nexttraceAssetName"
if ($nexttraceAssetUrl -ne 'https://github.com/nxtrace/NTrace-core/releases/download/v1.7.1/nexttrace-tiny_windows_amd64.exe') {
    Stop-EcsWindowsBuild 'NextTrace Windows asset URL is not derived from the pinned release tag and asset'
}

$toolMap = @{}
foreach ($name in $windowToolNames) {
    $toolMap[$name] = Get-EcsLockedTool -Lock $lock -Name $name
}
$npbTool = Get-EcsLockedTool -Lock $lock -Name 'npb-ep'
$toolMap['npb-ft'] = $npbTool
$streamTool = Get-EcsLockedTool -Lock $lock -Name 'stream'
$toolchain = $lock.windows_toolchain
if ($null -eq $toolchain -or @($toolchain.packages).Count -lt 1) {
    Stop-EcsWindowsBuild 'Windows toolchain lock is missing'
}
$buildFlagsProperty = $toolchain.PSObject.Properties['build_flags']
if ($null -eq $buildFlagsProperty -or $null -eq $buildFlagsProperty.Value) {
    Stop-EcsWindowsBuild 'Windows toolchain lock is missing build flag groups'
}
$buildFlags = $buildFlagsProperty.Value
foreach ($flagGroup in @('c', 'fortran', 'linker', 'fortran_linker')) {
    if ($flagGroup -notin @($buildFlags.PSObject.Properties.Name) -or @($buildFlags.PSObject.Properties[$flagGroup].Value).Count -eq 0) {
        Stop-EcsWindowsBuild "Windows toolchain lock is missing build flag group: $flagGroup"
    }
}
$allCompilerFlags = @()
foreach ($flagGroup in @('c', 'fortran', 'linker', 'fortran_linker')) {
    $allCompilerFlags += @($buildFlags.PSObject.Properties[$flagGroup].Value)
}
if (@($allCompilerFlags | Where-Object { $_ -in @('-static-libgomp', '-static-libwinpthread') }).Count -ne 0) {
    Stop-EcsWindowsBuild 'Windows toolchain lock contains unsupported GCC static runtime flags'
}
$distribution = $toolchain.distribution
$allowlist = @($lock.windows_dll_allowlist)
if ($allowlist.Count -eq 0) {
    Stop-EcsWindowsBuild 'Windows DLL import allowlist is empty'
}
$basePackageFacts = @($toolchain.base_packages)
if ($basePackageFacts.Count -eq 0) {
    Stop-EcsWindowsBuild 'Windows toolchain lock omits the fixed MSYS2 base package checks'
}
foreach ($basePackage in $basePackageFacts) {
    if ([string]$basePackage.source_url -ne [string]$distribution.source_url -or
        [string]$basePackage.source_sha256 -ne [string]$distribution.source_sha256) {
        Stop-EcsWindowsBuild "base package $($basePackage.name) is not tied to the locked MSYS2 archive"
    }
}
$gccPackage = @($toolchain.packages | Where-Object { $_.name -eq 'mingw-w64-ucrt-x86_64-gcc' })[0]
$fortranPackage = @($toolchain.packages | Where-Object { $_.name -eq 'mingw-w64-ucrt-x86_64-gcc-fortran' })[0]
$libgompPackage = @($toolchain.packages | Where-Object { $_.name -eq 'mingw-w64-ucrt-x86_64-gcc-libs' })[0]
$nasmPackage = @($toolchain.packages | Where-Object { $_.name -eq 'mingw-w64-ucrt-x86_64-nasm' })[0]
$makePackage = @($toolchain.packages | Where-Object { $_.name -eq 'make' })[0]
$binutilsPackage = @($toolchain.packages | Where-Object { $_.name -eq 'mingw-w64-ucrt-x86_64-binutils' })[0]
if ($null -eq $gccPackage -or $null -eq $fortranPackage -or $null -eq $libgompPackage -or $null -eq $nasmPackage -or $null -eq $makePackage -or $null -eq $binutilsPackage) {
    Stop-EcsWindowsBuild 'Windows toolchain lock omits a required compiler, binutils, OpenMP, NASM, or make package'
}
if (@($libgompPackage.runtime_components) -notcontains 'libgomp') {
    Stop-EcsWindowsBuild 'Windows toolchain lock does not identify the pinned libgomp runtime component'
}

if ($PrintParams) {
    @(
        "target=$Target",
        'toolchain_mode=native',
        'compiler_family=MinGW-w64 GCC',
        "compiler_version=$($gccPackage.version)",
        "fortran_version=$($fortranPackage.version)",
        "libgomp_package_version=$($libgompPackage.version)",
        "nasm_version=$($nasmPackage.version)",
        "make_version=$($makePackage.version)",
        'target_triplet=x86_64-w64-mingw32',
        "base_packages=$($basePackageFacts.Count)",
        "toolchain_packages=$(@($toolchain.packages).Count)",
        "linker_flags=$(@($buildFlags.linker) -join ' ')",
        "fortran_linker_flags=$(@($buildFlags.fortran_linker) -join ' ')",
        'smoke_runner=direct',
        'validation_scope=functional',
        'performance_valid=false',
        "tools=$($windowToolNames -join ',')",
        'nexttrace=verified-upstream-prebuilt',
        "nexttrace_asset=$nexttraceAssetName",
        "nexttrace_source_sha256=$nexttraceExpectedSha256"
    ) | Write-Output
    exit 0
}

if (-not $StageRoot) {
    Stop-EcsWindowsBuild '--stage-root is required unless --print-params is used'
}
if (-not [IO.Path]::IsPathRooted($StageRoot)) {
    Stop-EcsWindowsBuild '--stage-root must be an absolute path'
}
$StageRoot = [IO.Path]::GetFullPath($StageRoot)
$stage = Join-Path $StageRoot $Target
if (Test-Path -LiteralPath $stage) {
    Stop-EcsWindowsBuild "stage already exists: $stage"
}
New-Item -ItemType Directory -Force -Path (Join-Path $stage 'bin'), (Join-Path $stage 'LICENSES') | Out-Null

if (-not $WorkRoot) {
    $WorkRoot = Join-Path ([IO.Path]::GetTempPath()) ("ecs-windows-tools-build." + [guid]::NewGuid().ToString('N'))
} else {
    $WorkRoot = [IO.Path]::GetFullPath($WorkRoot)
}
if (Test-Path -LiteralPath $WorkRoot) {
    Stop-EcsWindowsBuild "work directory already exists: $WorkRoot"
}
New-Item -ItemType Directory -Force -Path $WorkRoot | Out-Null

$msysRoot = $null
$buildError = $null
try {
    $sourceRoot = Join-Path $WorkRoot 'sources'
    $downloadRoot = Join-Path $WorkRoot 'downloads'
    $packageRoot = Join-Path $WorkRoot 'msys-packages'
    $corpusRoot = Join-Path $WorkRoot 'corpus'
    New-Item -ItemType Directory -Force -Path $sourceRoot, $downloadRoot, $packageRoot, $corpusRoot | Out-Null

    $nexttraceDownload = Join-Path $downloadRoot $nexttraceAssetName
    Save-EcsVerifiedDownload -Uri $nexttraceAssetUrl -Sha256 $nexttraceExpectedSha256 -Destination $nexttraceDownload -Description 'official NextTrace Tiny Windows AMD64 release asset'
    $nexttraceDownloadedSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $nexttraceDownload).Hash.ToLowerInvariant()
    if ($nexttraceDownloadedSha256 -ne $nexttraceExpectedSha256) {
        Stop-EcsWindowsBuild "verified NextTrace release asset hash mismatch: expected $nexttraceExpectedSha256, got $nexttraceDownloadedSha256"
    }

    # The pinned checkout is used only for the upstream license text. The
    # packaged executable always comes from the verified official release asset.
    $nexttraceSourceRoot = Join-Path $sourceRoot 'nexttrace'
    Invoke-EcsGitCloneAtCommit -Tool $nexttraceTool -Destination $nexttraceSourceRoot

    foreach ($name in @('zstd', 'openssl', 'fio')) {
        Invoke-EcsGitCloneAtCommit -Tool $toolMap[$name] -Destination (Join-Path $sourceRoot $name)
    }

    $npbArchive = Join-Path $downloadRoot 'NPB3.4.4.tar.gz'
    Save-EcsVerifiedDownload -Uri $npbTool.source_url -Sha256 $npbTool.source_sha256 -Destination $npbArchive -Description 'NPB source archive'
    $tar = (Get-Command tar.exe -ErrorAction SilentlyContinue).Source
    if (-not $tar) { Stop-EcsWindowsBuild 'tar.exe is required to unpack the official NPB archive' }
    Invoke-EcsWindowsNative -FilePath $tar -ArgumentList @('-xzf', $npbArchive, '-C', $sourceRoot)
    $npbSourceRoot = Join-Path $sourceRoot 'NPB3.4.4'
    if (-not (Test-Path -LiteralPath (Join-Path $npbSourceRoot 'NPB3.4-OMP') -PathType Container)) {
        Stop-EcsWindowsBuild 'NPB archive did not contain NPB3.4-OMP'
    }

    $streamSource = Join-Path $sourceRoot 'stream.c'
    Save-EcsVerifiedDownload -Uri $streamTool.source_url -Sha256 $streamTool.source_sha256 -Destination $streamSource -Description 'official STREAM source'

    $corpusZip = Join-Path $downloadRoot 'silesia.zip'
    Save-EcsVerifiedDownload -Uri $lock.corpus.source_url -Sha256 $lock.corpus.source_sha256 -Destination $corpusZip -Description 'Silesia source ZIP'
    $corpusExtract = Join-Path $WorkRoot 'silesia'
    Expand-Archive -LiteralPath $corpusZip -DestinationPath $corpusExtract -Force
    $corpusPath = Join-Path $corpusRoot $lock.corpus.name
    $corpusStream = [IO.File]::Open($corpusPath, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        foreach ($member in @($lock.corpus.order)) {
            $memberPath = Join-Path $corpusExtract $member
            if (-not (Test-Path -LiteralPath $memberPath -PathType Leaf)) {
                Stop-EcsWindowsBuild "Silesia source omitted $member"
            }
            $input = [IO.File]::OpenRead($memberPath)
            try { $input.CopyTo($corpusStream) } finally { $input.Dispose() }
        }
    } finally {
        $corpusStream.Dispose()
    }
    $corpusHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $corpusPath).Hash.ToLowerInvariant()
    $corpusBytes = (Get-Item -LiteralPath $corpusPath).Length
    if ($corpusHash -ne [string]$lock.corpus.sha256 -or $corpusBytes -ne [int64]$lock.corpus.bytes) {
        Stop-EcsWindowsBuild "fixed Silesia corpus mismatch: bytes=$corpusBytes sha256=$corpusHash"
    }

    $msysArchiveRoot = $WorkRoot
    $msysArchive = Join-Path $downloadRoot (Split-Path -Leaf ([Uri]$distribution.source_url).AbsolutePath)
    Save-EcsVerifiedDownload -Uri $distribution.source_url -Sha256 $distribution.source_sha256 -Destination $msysArchive -Description 'pinned MSYS2 base distribution'
    # The official base archive contains one top-level msys64/ directory.
    # Extract beside it so the isolated root is exactly the archived tree.
    Invoke-EcsWindowsNative -FilePath $tar -ArgumentList @('-xf', $msysArchive, '-C', $msysArchiveRoot)
    $msysRoot = Join-Path $WorkRoot 'msys64'
    $bash = Join-Path $msysRoot 'usr\bin\bash.exe'
    if (-not (Test-Path -LiteralPath $bash -PathType Leaf)) {
        Stop-EcsWindowsBuild 'pinned MSYS2 base distribution has no usr/bin/bash.exe'
    }

    foreach ($package in @($toolchain.packages)) {
        $packageFile = Join-Path $packageRoot (Split-Path -Leaf ([Uri]$package.source_url).AbsolutePath)
        Save-EcsVerifiedDownload -Uri $package.source_url -Sha256 $package.source_sha256 -Destination $packageFile -Description "MSYS2 package $($package.name)"
    }

    $msysUsrBin = Join-Path $msysRoot 'usr\bin'
    $ucrtBin = Join-Path $msysRoot 'ucrt64\bin'
    $msysContext = [pscustomobject]@{
        Bash = $bash
        MsysRoot = $msysRoot
        MsysUsrBin = $msysUsrBin
        MsysUsrBinPosix = ConvertTo-EcsMsysPath $msysUsrBin
        UcrtBin = $ucrtBin
        UcrtBinPosix = ConvertTo-EcsMsysPath $ucrtBin
    }
    $packageFiles = @($toolchain.packages | ForEach-Object {
        ConvertTo-EcsMsysPath (Join-Path $packageRoot (Split-Path -Leaf ([Uri]$_.source_url).AbsolutePath))
    })
    $packageInstallArgs = $packageFiles | ForEach-Object { ConvertTo-EcsBashLiteral $_ }
    $basePackageCheckLines = @()
    foreach ($basePackage in @($basePackageFacts)) {
        $packageName = ConvertTo-EcsBashLiteral $basePackage.name
        $packageVersion = ConvertTo-EcsBashLiteral $basePackage.version
        $basePackageCheckLines += "test `$(pacman -Q $packageName | awk '{print `$2}') = $packageVersion"
    }
    $installScript = @"
export PATH=$(ConvertTo-EcsBashLiteral $msysContext.UcrtBinPosix):$(ConvertTo-EcsBashLiteral $msysContext.MsysUsrBinPosix)
set -eu
# The locked base archive carries the official MSYS2 keyring files, but the
# raw archive does not run the msys2-keyring package's post-install hook.
# Initialize and populate that keyring from the archive before verifying the
# locked local package transaction; no keyserver or package database update is
# involved.
pacman-key --init
pacman-key --populate msys2
# pacman-key uses this keyring home; do not let gpgconf default to ~/.gnupg.
gpgconf --homedir /etc/pacman.d/gnupg --kill all
$(($basePackageCheckLines -join "`n"))
pacman -U --noconfirm $($packageInstallArgs -join ' ')
"@
    Invoke-EcsWindowsBash -Context $msysContext -Script $installScript
    $packageCheckLines = @()
    foreach ($package in @($toolchain.packages)) {
        $packageName = ConvertTo-EcsBashLiteral $package.name
        $packageVersion = ConvertTo-EcsBashLiteral $package.version
        $packageCheckLines += "test `$(pacman -Q $packageName | awk '{print `$2}') = $packageVersion"
    }
    $packageCheckScript = @"
export PATH=$(ConvertTo-EcsBashLiteral $msysContext.UcrtBinPosix):$(ConvertTo-EcsBashLiteral $msysContext.MsysUsrBinPosix)
set -eu
$($packageCheckLines -join "`n")
"@
    Invoke-EcsWindowsBash -Context $msysContext -Script $packageCheckScript

    $jobs = 2
    if ($env:JOBS -and $env:JOBS -match '^[1-9][0-9]*$') { $jobs = [int]$env:JOBS }
    elseif ([Environment]::ProcessorCount -gt 0) { $jobs = [Environment]::ProcessorCount }
    $epoch = 946684800
    if ($env:SOURCE_DATE_EPOCH -and $env:SOURCE_DATE_EPOCH -match '^[0-9]+$') { $epoch = [int64]$env:SOURCE_DATE_EPOCH }
    # Keep link-only options out of C/Fortran compile flags. GCC's -static
    # selects static libgomp/libwinpthread archives; only libgfortran has a
    # supported language-specific static-driver option here.
    $cFlagList = @($buildFlags.c)
    $fortranFlagList = @($buildFlags.fortran)
    $linkerFlagList = @($buildFlags.linker)
    $fortranLinkerFlagList = @($linkerFlagList + @($buildFlags.fortran_linker))
    $cFlags = $cFlagList -join ' '
    $fortranFlags = $fortranFlagList -join ' '
    $linkerFlags = $linkerFlagList -join ' '
    $stripPath = Join-Path $msysRoot 'ucrt64\bin\strip.exe'
    $objdumpPath = Join-Path $msysRoot 'ucrt64\bin\objdump.exe'
    if (-not (Test-Path -LiteralPath $stripPath -PathType Leaf) -or
        -not (Test-Path -LiteralPath $objdumpPath -PathType Leaf)) {
        Stop-EcsWindowsBuild 'pinned binutils did not provide ucrt64/bin/strip.exe and objdump.exe'
    }
    $stripPathPosix = ConvertTo-EcsMsysPath $stripPath
    $objdumpPathPosix = ConvertTo-EcsMsysPath $objdumpPath
    $stripCommand = ConvertTo-EcsBashLiteral $stripPathPosix
    $objdumpCommand = ConvertTo-EcsBashLiteral $objdumpPathPosix
    $preamble = @"
export PATH=$(ConvertTo-EcsBashLiteral $msysContext.UcrtBinPosix):$(ConvertTo-EcsBashLiteral $msysContext.MsysUsrBinPosix)
export CC=gcc
export CXX=g++
export FC=gfortran
export AR=ar
export RANLIB=ranlib
export STRIP=$stripCommand
export CFLAGS=$(ConvertTo-EcsBashLiteral $cFlags)
export CXXFLAGS=$(ConvertTo-EcsBashLiteral $cFlags)
export LDFLAGS=$(ConvertTo-EcsBashLiteral $linkerFlags)
    export SOURCE_DATE_EPOCH=$epoch
"@
    $compileDate = [DateTimeOffset]::FromUnixTimeSeconds($epoch).UtcDateTime.ToString('dd MMM yyyy', [Globalization.CultureInfo]::InvariantCulture)
    $toolchainProbeScript = @"
$($preamble)
set -eu
gcc -dumpmachine
gfortran -dumpmachine
gcc --version | sed -n '1p'
gfortran --version | sed -n '1p'
nasm -v | sed -n '1p'
$($stripCommand) --version | sed -n '1p'
$($objdumpCommand) --version | sed -n '1p'
"@
    $toolchainProbe = @(Invoke-EcsWindowsBash -Context $msysContext -Script $toolchainProbeScript -CaptureOutput | ForEach-Object { ([string]$_).Trim() } | Where-Object { $_ })
    if ($toolchainProbe.Count -lt 7 -or $toolchainProbe[0] -ne 'x86_64-w64-mingw32' -or $toolchainProbe[1] -ne 'x86_64-w64-mingw32') {
        Stop-EcsWindowsBuild "fixed UCRT64 toolchain probe failed: $($toolchainProbe -join ' | ')"
    }
    if ($toolchainProbe[2] -notmatch [regex]::Escape(([string]$gccPackage.version).Split('-')[0]) -or
        $toolchainProbe[3] -notmatch [regex]::Escape(([string]$fortranPackage.version).Split('-')[0]) -or
        $toolchainProbe[4] -notmatch [regex]::Escape(([string]$nasmPackage.version).Split('-')[0]) -or
        $toolchainProbe[5] -notmatch [regex]::Escape(([string]$binutilsPackage.version).Split('-')[0]) -or
        $toolchainProbe[6] -notmatch [regex]::Escape(([string]$binutilsPackage.version).Split('-')[0])) {
        Stop-EcsWindowsBuild "fixed compiler/binutils version probe failed: $($toolchainProbe -join ' | ')"
    }
    $toolchainFacts = [ordered]@{
        compiler_family = 'MinGW-w64 GCC'
        compiler_version = $toolchainProbe[2]
        fortran_version = $toolchainProbe[3]
        assembler_family = 'NASM'
        assembler_version = $toolchainProbe[4]
        binutils_version = $toolchainProbe[5]
        objdump_version = $toolchainProbe[6]
        binutils_package_version = [string]$binutilsPackage.version
        compiler_package_version = [string]$gccPackage.version
        fortran_package_version = [string]$fortranPackage.version
        libgomp_package_version = [string]$libgompPackage.version
        nasm_package_version = [string]$nasmPackage.version
        make_package_version = [string]$makePackage.version
        target_triplet = 'x86_64-w64-mingw32'
        build_host = 'windows_amd64'
        openmp_runtime = 'libgomp'
    }

    $context = [pscustomobject]@{
        Target = $Target
        TargetFacts = $targetFacts
        WorkRoot = $WorkRoot
        StageRoot = $stage
        Jobs = $jobs
        Bash = $bash
        MsysRoot = $msysRoot
        Sources = @{
            zstd = Join-Path $sourceRoot 'zstd'
            npb = $npbSourceRoot
            openssl = Join-Path $sourceRoot 'openssl'
            stream = $streamSource
            fio = Join-Path $sourceRoot 'fio'
            nexttrace = $nexttraceSourceRoot
        }
        Binaries = @{
            zstd = Join-Path $stage 'bin\zstd.exe'
            'npb-ep' = Join-Path $stage 'bin\npb-ep.exe'
            'npb-ft' = Join-Path $stage 'bin\npb-ft.exe'
            openssl = Join-Path $stage 'bin\openssl.exe'
            stream = Join-Path $stage 'bin\stream.exe'
            fio = Join-Path $stage 'bin\fio.exe'
            'nexttrace-tiny' = Join-Path $stage 'bin\nexttrace-tiny.exe'
        }
        Tools = $toolMap
        Toolchain = $toolchain
        Stream = $streamTool
        Preamble = $preamble
        CFlags = $cFlags
        FortranFlags = $fortranFlags
        LinkerFlags = @($linkerFlagList)
        FortranLinkerFlags = @($fortranLinkerFlagList)
        CompileDate = $compileDate
        BuildFacts = @{}
    }

    . (Join-Path $RepoRoot 'scripts\tools\windows\zstd.ps1')
    . (Join-Path $RepoRoot 'scripts\tools\windows\npb.ps1')
    . (Join-Path $RepoRoot 'scripts\tools\windows\openssl.ps1')
    . (Join-Path $RepoRoot 'scripts\tools\windows\stream.ps1')
    . (Join-Path $RepoRoot 'scripts\tools\windows\fio.ps1')

    Build-EcsWindowsZstd -Context $context
    Build-EcsWindowsNpb -Context $context
    Build-EcsWindowsOpenSSL -Context $context
    Build-EcsWindowsStream -Context $context
    Build-EcsWindowsFio -Context $context

    foreach ($name in $sourceBuiltToolNames) {
        if ($name -in @('npb-ep', 'npb-ft')) {
            $context.BuildFacts[$name]['linker_flags'] = @($fortranLinkerFlagList)
        } else {
            $context.BuildFacts[$name]['linker_flags'] = @($linkerFlagList)
        }
    }

    foreach ($name in $sourceBuiltToolNames) {
        foreach ($key in $toolchainFacts.Keys) {
            $context.BuildFacts[$name][$key] = $toolchainFacts[$key]
        }
    }

    foreach ($name in $sourceBuiltToolNames) {
        $binary = $context.Binaries[$name]
        if (-not (Test-Path -LiteralPath $binary -PathType Leaf) -or (Get-Item -LiteralPath $binary).Length -eq 0) {
            Stop-EcsWindowsBuild "tool output is missing or empty: $name"
        }
        $stripScript = "$($context.Preamble)`nset -eu`n$stripCommand --strip-all $(ConvertTo-EcsBashLiteral (ConvertTo-EcsMsysPath $binary))`ntest -s $(ConvertTo-EcsBashLiteral (ConvertTo-EcsMsysPath $binary))"
        Invoke-EcsWindowsBash -Context $context -Script $stripScript
    }

    $nexttraceBinary = $context.Binaries['nexttrace-tiny']
    Copy-Item -LiteralPath $nexttraceDownload -Destination $nexttraceBinary -Force
    $nexttracePackagedSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $nexttraceBinary).Hash.ToLowerInvariant()
    $nexttracePackagedBytes = (Get-Item -LiteralPath $nexttraceBinary).Length
    $nexttraceSourceBytes = (Get-Item -LiteralPath $nexttraceDownload).Length
    if ($nexttracePackagedBytes -ne $nexttraceSourceBytes -or $nexttracePackagedSha256 -ne $nexttraceDownloadedSha256 -or
        $nexttracePackagedSha256 -ne $nexttraceExpectedSha256) {
        Stop-EcsWindowsBuild "NextTrace packaged bytes differ from the verified upstream asset: source_sha256=$nexttraceDownloadedSha256 packaged_sha256=$nexttracePackagedSha256"
    }

    $peFactsByTool = @{}
    foreach ($name in $windowToolNames) {
        $peFacts = Get-EcsWindowsPeFacts -ObjdumpPath $objdumpPath -BinaryPath $context.Binaries[$name] -Allowlist $allowlist
        if ($name -in $sourceBuiltToolNames -and -not [bool]$peFacts['stripped']) {
            Stop-EcsWindowsBuild "$name binary still contains debug or symbol sections after locked strip"
        }
        $peFactsByTool[$name] = $peFacts
    }

    $licenseRoot = Join-Path $stage 'LICENSES'
    Copy-EcsLicense (Join-Path $licenseRoot 'ZSTD-LICENSE') @((Join-Path $context.Sources['zstd'] 'LICENSE'))
    Copy-EcsLicense (Join-Path $licenseRoot 'ZSTD-COPYING') @((Join-Path $context.Sources['zstd'] 'COPYING'))
    Copy-EcsLicense (Join-Path $licenseRoot 'NPB-README.txt') @((Join-Path $context.Sources['npb'] 'README'))
    $npbLicense = Join-Path $licenseRoot 'NPB-LICENSE.txt'
    Get-Content -LiteralPath (Join-Path $context.Sources['npb'] 'NPB3.4-OMP\EP\ep.f90') -TotalCount 31 | Set-Content -LiteralPath $npbLicense -Encoding ascii
    Copy-EcsLicense (Join-Path $licenseRoot 'OPENSSL-LICENSE.txt') @((Join-Path $context.Sources['openssl'] 'LICENSE.txt'))
    Copy-EcsLicense (Join-Path $licenseRoot 'FIO-COPYING') @((Join-Path $context.Sources['fio'] 'COPYING'))
    Copy-EcsLicense (Join-Path $licenseRoot 'NEXTTRACE-LICENSE') @((Join-Path $context.Sources['nexttrace'] 'LICENSE'))
    $streamLicense = Join-Path $licenseRoot 'STREAM-LICENSE.txt'
    $streamHeader = Get-Content -LiteralPath $streamSource
    $headerEnd = [Array]::IndexOf([string[]]$streamHeader, ' */')
    if ($headerEnd -lt 0) { Stop-EcsWindowsBuild 'STREAM source has no license header terminator' }
    $streamHeader[0..$headerEnd] | Set-Content -LiteralPath $streamLicense -Encoding ascii

    $manifestTools = @()
    $featureMap = @{
        zstd = @('benchmark', 'compression', 'decompression', 'multithread')
        'npb-ep' = @('NPB3.4-OMP', 'EP', 'Class A', 'OpenMP')
        'npb-ft' = @('NPB3.4-OMP', 'FT', 'Class A', 'OpenMP', '3D FFT')
        openssl = @('speed', 'EVP', 'AES-256-GCM', 'ChaCha20-Poly1305', 'SHA-256')
        stream = @('Copy', 'Scale', 'Add', 'Triad', 'OpenMP')
        fio = @('windowsaio', 'json', 'direct-io')
        'nexttrace-tiny' = @('tiny', 'json')
    }
    $disabledMap = @{
        zstd = @('zlib', 'lzma', 'lz4', 'legacy-formats', 'dictionary-builder', 'trace')
        'npb-ep' = @('MPI', 'other NPB kernels', 'other problem classes')
        'npb-ft' = @('MPI', 'other NPB kernels', 'other problem classes')
        openssl = @('TLS/DTLS/QUIC', 'network/HTTP', 'shared libraries/modules/engines')
        stream = @()
        fio = @('rbd', 'rados', 'gfapi', 'rdma', 'libaio', 'io_uring', 'psync')
        'nexttrace-tiny' = @('full', 'mtr', 'globalping', 'webui')
    }
    foreach ($name in $windowToolNames) {
        if ($name -eq 'nexttrace-tiny') {
            $nexttracePeFacts = $peFactsByTool[$name]
            $nexttraceManifestParameters = [ordered]@{
                repository = [string]$nexttraceTool.repository
                tag = [string]$nexttraceTool.tag
                release_commit = [string]$nexttraceTool.commit
                source_url = $nexttraceAssetUrl
                source_asset = $nexttraceAssetName
                asset_pattern = $nexttraceAssetPattern
                source_sha256 = $nexttraceExpectedSha256
                original_sha256 = $nexttraceDownloadedSha256
                upstream_sha256 = $nexttraceDownloadedSha256
                binary_sha256 = $nexttracePackagedSha256
                packaged_sha256 = $nexttracePackagedSha256
                provenance = 'upstream official release binary'
                source_mode = 'verified-upstream-prebuilt'
                pe_machine = [string]$nexttracePeFacts['pe_machine']
                pe_imports = @($nexttracePeFacts['imports'])
                dependency_allowlist = @($allowlist)
                imports_checked = [bool]$nexttracePeFacts['imports_checked']
                fully_static = [bool]$nexttracePeFacts['fully_static']
                stripped = [bool]$nexttracePeFacts['stripped']
            }
            $manifestTools += [ordered]@{
                name = 'nexttrace-tiny'
                upstream = [string]$nexttraceTool.upstream
                version = [string]$nexttraceTool.version
                tag_or_commit = [string]$nexttraceTool.tag
                source = $nexttraceAssetUrl
                build_flags = @('official-release-asset', 'tiny')
                enabled_features = @($featureMap[$name])
                disabled_features = @($disabledMap[$name])
                architecture = 'amd64'
                license = 'GPL-3.0-only'
                parameters = $nexttraceManifestParameters
            }
            continue
        }
        $manifestTool = New-EcsManifestTool -Name $name -Tool $context.Tools[$name] -Fact $context.BuildFacts[$name] -BinaryPath $context.Binaries[$name] -EnabledFeatures $featureMap[$name] -DisabledFeatures $disabledMap[$name] -License ([string]$context.BuildFacts[$name]['license']) -DependencyAllowlist $allowlist -PeFacts $peFactsByTool[$name]
        $manifestTools += $manifestTool
    }
    $manifest = [ordered]@{
        schema_version = 'ecs-tools.manifest/v1'
        target = $Target
        goos = 'windows'
        goarch = 'amd64'
        architecture = 'amd64'
        supported_architectures = @('amd64')
        supported_targets = @('windows_amd64')
        build = [ordered]@{
            toolchain_mode = 'native'
            build_triplet = 'x86_64-w64-mingw32'
            target_triplet = 'x86_64-w64-mingw32'
            smoke_runner = 'direct'
            validation = [ordered]@{ scope = 'functional'; performance_valid = $false }
        }
        tools = @($manifestTools)
    }
    $manifestPath = Join-Path $stage 'manifest.json'
    $manifest | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $manifestPath -Encoding utf8

    & (Join-Path $RepoRoot 'scripts\ci\windows_tools_gate.ps1') -StageRoot $stage -ManifestPath $manifestPath -LockPath (Join-Path $RepoRoot 'tools\lock.json') -CorpusPath $corpusPath -ObjdumpPath $objdumpPath
    if (-not $?) { Stop-EcsWindowsBuild 'Windows tools gate failed' }
    Write-Output "build-tools-windows: completed real $Target stage at $stage"
}
catch {
    $buildError = $_
    throw
}
finally {
    if (Test-Path -LiteralPath $WorkRoot) {
        $gpgCleanupError = $null
        $removeError = $null
        if ($null -ne $msysRoot) {
            try {
                Invoke-EcsWindowsMsysGpgCleanup -MsysRoot $msysRoot
            } catch {
                $gpgCleanupError = $_
            }
        }
        try {
            Remove-EcsWindowsWorkRoot -Path $WorkRoot
        } catch {
            $removeError = $_
        }
        if ($null -ne $buildError) {
            if ($null -ne $gpgCleanupError) {
                Write-Warning "MSYS2 gpg-agent cleanup failed after the build error: $($gpgCleanupError.Exception.Message)"
            }
            if ($null -ne $removeError) {
                Write-Warning "MSYS2 work directory cleanup failed after the build error: $($removeError.Exception.Message)"
            }
        } elseif ($null -ne $gpgCleanupError) {
            throw $gpgCleanupError
        } elseif ($null -ne $removeError) {
            throw $removeError
        }
    }
}
