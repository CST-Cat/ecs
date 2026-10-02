[CmdletBinding(DefaultParameterSetName = 'PackageContract')]
param(
    [Parameter(Mandatory, ParameterSetName = 'PackageContract')][switch]$CheckPackageContract,
    [Parameter(Mandatory, ParameterSetName = 'PackageContract')][string]$StageRoot,
    [Parameter(Mandatory, ParameterSetName = 'PackageContract')][string]$LockPath,
    [Parameter(Mandatory, ParameterSetName = 'NoAdminOperation')][switch]$CheckOrdinaryUser
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Stop-EcsWindowsGate {
    param([Parameter(Mandatory)][string]$Message)
    throw "windows-tools-gate: $Message"
}

function Get-EcsGateJson {
    param([Parameter(Mandatory)][string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        Stop-EcsWindowsGate "missing JSON file: $Path"
    }
    try { return Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json }
    catch { Stop-EcsWindowsGate "invalid JSON file $Path`: $($_.Exception.Message)" }
}

function Ensure-EcsWindowsTokenType {
    if ('EcsWindowsToken' -as [type]) { return }
    Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;

public static class EcsWindowsToken
{
    private const int TokenElevationTypeInformation = 18;
    private const int TokenElevationInformation = 20;

    [DllImport("advapi32.dll", SetLastError = true)]
    private static extern bool GetTokenInformation(
        IntPtr token,
        int informationClass,
        IntPtr information,
        int informationLength,
        out int returnLength);

    private static int ReadInformation(IntPtr token, int informationClass)
    {
        IntPtr buffer = Marshal.AllocHGlobal(sizeof(int));
        try
        {
            int returnLength;
            if (!GetTokenInformation(token, informationClass, buffer, sizeof(int), out returnLength))
            {
                throw new Win32Exception(Marshal.GetLastWin32Error(), "GetTokenInformation");
            }
            return Marshal.ReadInt32(buffer);
        }
        finally
        {
            Marshal.FreeHGlobal(buffer);
        }
    }

    public static bool IsElevated(IntPtr token)
    {
        return ReadInformation(token, TokenElevationInformation) != 0;
    }

    public static int GetElevationType(IntPtr token)
    {
        return ReadInformation(token, TokenElevationTypeInformation);
    }
}
'@
}

function Get-EcsWindowsTokenEvidence {
    Ensure-EcsWindowsTokenType
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    try {
        $principal = New-Object -TypeName Security.Principal.WindowsPrincipal -ArgumentList $identity
        $elevated = [EcsWindowsToken]::IsElevated($identity.Token)
        $elevationType = [EcsWindowsToken]::GetElevationType($identity.Token)
        $elevationTypeName = switch ($elevationType) {
            1 { 'Default'; break }
            2 { 'Full'; break }
            3 { 'Limited'; break }
            default { "Unknown($elevationType)" }
        }
        return [pscustomobject]@{
            User = [string]$identity.Name
            AdministratorGroupMembership = [bool]($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator))
            TokenElevated = [bool]$elevated
            ElevationType = [int]$elevationType
            ElevationTypeName = $elevationTypeName
        }
    } finally {
        $identity.Dispose()
    }
}

function Test-EcsPathUnderRoot {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Root
    )
    $candidate = [IO.Path]::GetFullPath($Path)
    $rootPath = [IO.Path]::GetFullPath($Root)
    if ($rootPath.Length -gt 3) { $rootPath = $rootPath.TrimEnd([IO.Path]::DirectorySeparatorChar) }
    return $candidate.Equals($rootPath, [StringComparison]::OrdinalIgnoreCase) -or
        $candidate.StartsWith($rootPath + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)
}

function Get-EcsProtectedInstallRoots {
    return @(
        [Environment]::GetFolderPath([Environment+SpecialFolder]::ProgramFiles),
        [Environment]::GetFolderPath([Environment+SpecialFolder]::ProgramFilesX86)
    ) | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) } | Sort-Object -Unique
}

function Invoke-EcsNoAdminOperationCheck {
    param([Parameter(Mandatory)][string]$StageRoot)

    $token = Get-EcsWindowsTokenEvidence
    Write-Host ("no-admin-operation context (observational; does not prove an ordinary-user token): user={0}; administrator_group_membership={1}; token_elevated={2}; token_elevation_type={3}" -f $token.User, $token.AdministratorGroupMembership, $token.TokenElevated, $token.ElevationTypeName)
    if ($token.TokenElevated -or $token.ElevationType -eq 2) {
        Write-Host 'no-admin-operation context: actual runner token is elevated; this check does not claim ordinary-user execution'
    }

    $stagePath = [IO.Path]::GetFullPath($StageRoot)
    foreach ($root in Get-EcsProtectedInstallRoots) {
        if (Test-EcsPathUnderRoot -Path $stagePath -Root $root) {
            Stop-EcsWindowsGate "no-admin-operation check path is under protected Program Files root: $stagePath"
        }
    }

    $machinePathBefore = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Machine)
    $userPathBefore = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
    $probe = Join-Path ([IO.Path]::GetTempPath()) ("ecs-no-admin-operation." + [guid]::NewGuid().ToString('N') + '.tmp')
    foreach ($root in Get-EcsProtectedInstallRoots) {
        if (Test-EcsPathUnderRoot -Path $probe -Root $root) {
            Stop-EcsWindowsGate "no-admin-operation probe is under protected Program Files root: $probe"
        }
    }
    $probeText = 'ecs no-admin-operation check'
    try {
        [IO.File]::WriteAllText($probe, $probeText)
        if ([IO.File]::ReadAllText($probe) -cne $probeText) {
            Stop-EcsWindowsGate 'no-admin-operation temporary write/read check did not round-trip'
        }
    } finally {
        if (Test-Path -LiteralPath $probe -PathType Leaf) { Remove-Item -LiteralPath $probe -Force }
    }
    $machinePathAfter = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Machine)
    $userPathAfter = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
    if ($machinePathBefore -cne $machinePathAfter -or $userPathBefore -cne $userPathAfter) {
        Stop-EcsWindowsGate 'no-admin-operation check detected a Machine or User PATH mutation'
    }

    Write-Host 'no-admin-operation check passed: temp_write=passed; protected_install_path=not_used; machine_path=unchanged; user_path=unchanged; ordinary-user-token-proof=not-claimed'
    return [pscustomobject]@{
        MachinePath = $machinePathBefore
        UserPath = $userPathBefore
    }
}


function Assert-EcsWindowsPackageContract {
    param(
        [Parameter(Mandatory)][string]$PackageStage,
        [Parameter(Mandatory)][object]$Lock
    )

    $manifest = Get-EcsGateJson -Path (Join-Path $PackageStage 'manifest.json')
    $expectedTools = @('zstd', 'npb-ep', 'npb-ft', 'openssl', 'stream', 'fio', 'nexttrace-tiny')
    if ([string]$Lock.schema_version -cne 'ecs.tools.lock/v1' -or (@($Lock.windows_tools) -join '|') -cne ($expectedTools -join '|')) {
        Stop-EcsWindowsGate 'Windows tool set is not the frozen seven-tool contract'
    }
    $targetFacts = @($Lock.architectures | Where-Object { [string]$_.target -ceq 'windows_amd64' })
    if ($targetFacts.Count -ne 1 -or [string]$targetFacts[0].goos -cne 'windows' -or
        [string]$targetFacts[0].goarch -cne 'amd64' -or [string]$targetFacts[0].package -cne 'amd64') {
        Stop-EcsWindowsGate 'tools lock has no unique windows_amd64 target facts'
    }
    if (@($Lock.windows_dll_allowlist).Count -eq 0) {
        Stop-EcsWindowsGate 'Windows DLL import allowlist is empty'
    }

    $nexttraceLocked = @($Lock.tools | Where-Object { [string]$_.name -ceq 'nexttrace-tiny' })
    if ($nexttraceLocked.Count -ne 1) { Stop-EcsWindowsGate 'NextTrace lock identity is missing or duplicated' }
    $nexttrace = $nexttraceLocked[0]
    $nexttraceAssetPattern = [string]$nexttrace.windows_asset_pattern
    if ([string]$nexttrace.upstream -cne 'https://github.com/nxtrace/NTrace-core' -or
        [string]$nexttrace.repository -cne 'nxtrace/NTrace-core' -or
        [string]$nexttrace.version -cne '1.7.1' -or [string]$nexttrace.tag -cne 'v1.7.1' -or
        [string]$nexttrace.commit -cne 'c9919828fcd8c3103827d08bb26d69e9bf538299' -or
        $nexttraceAssetPattern -cne 'nexttrace-tiny_windows_<architecture>.exe') {
        Stop-EcsWindowsGate 'NextTrace lock identity is not the pinned v1.7.1 Windows asset'
    }
    $nexttraceAssetName = $nexttraceAssetPattern.Replace('<architecture>', [string]$targetFacts[0].package)
    $nexttraceAssetUrl = "https://github.com/$($nexttrace.repository)/releases/download/$($nexttrace.tag)/$nexttraceAssetName"

    if ([string]$manifest.schema_version -cne 'ecs-tools.manifest/v1' -or
        [string]$manifest.target -cne 'windows_amd64' -or [string]$manifest.goos -cne 'windows' -or
        [string]$manifest.goarch -cne 'amd64' -or [string]$manifest.architecture -cne 'amd64' -or
        (@($manifest.supported_architectures) -join '|') -cne 'amd64' -or
        (@($manifest.supported_targets) -join '|') -cne 'windows_amd64') {
        Stop-EcsWindowsGate 'manifest target facts are not the frozen windows_amd64 contract'
    }
    if ([string]$manifest.build.toolchain_mode -cne 'native' -or
        [string]$manifest.build.build_triplet -cne 'x86_64-w64-mingw32' -or
        [string]$manifest.build.target_triplet -cne 'x86_64-w64-mingw32' -or
        [string]$manifest.build.smoke_runner -cne 'direct' -or
        [string]$manifest.build.validation.scope -cne 'functional' -or
        [bool]$manifest.build.validation.performance_valid) {
        Stop-EcsWindowsGate 'manifest build facts are not the frozen native functional contract'
    }

    $manifestTools = @($manifest.tools)
    if (($manifestTools.name -join '|') -cne ($expectedTools -join '|')) {
        Stop-EcsWindowsGate 'manifest tool set/order differs from tools lock'
    }
    $binDir = Join-Path $PackageStage 'bin'
    $licenseDir = Join-Path $PackageStage 'LICENSES'
    if (-not (Test-Path -LiteralPath $binDir -PathType Container) -or
        -not (Test-Path -LiteralPath $licenseDir -PathType Container)) {
        Stop-EcsWindowsGate 'package has no bin or LICENSES directory'
    }
    foreach ($license in @('ZSTD-LICENSE', 'ZSTD-COPYING', 'NPB-README.txt', 'NPB-LICENSE.txt', 'OPENSSL-LICENSE.txt', 'FIO-COPYING', 'NEXTTRACE-LICENSE', 'STREAM-LICENSE.txt')) {
        $licensePath = Join-Path $licenseDir $license
        if (-not (Test-Path -LiteralPath $licensePath -PathType Leaf) -or (Get-Item -LiteralPath $licensePath).Length -eq 0) {
            Stop-EcsWindowsGate "missing or empty license file: $license"
        }
    }
    $binEntries = @(Get-ChildItem -LiteralPath $binDir -Force)
    if ($binEntries.Count -ne $expectedTools.Count -or @($binEntries | Where-Object { $_.PSIsContainer }).Count -ne 0) {
        Stop-EcsWindowsGate 'package bin layout is not seven direct files'
    }
    foreach ($name in $expectedTools) {
        $binary = Join-Path $binDir "$name.exe"
        if (-not (Test-Path -LiteralPath $binary -PathType Leaf) -or (Get-Item -LiteralPath $binary).Length -le 0) {
            Stop-EcsWindowsGate "missing or empty binary $name.exe"
        }
    }
    if (@(Get-ChildItem -LiteralPath $PackageStage -Recurse -File -Filter '*.dll').Count -ne 0) {
        Stop-EcsWindowsGate 'bundle contains an external DLL'
    }

    foreach ($manifestTool in $manifestTools) {
        $name = [string]$manifestTool.name
        if ($name -eq 'nexttrace-tiny') {
            $parameters = $manifestTool.parameters
            if ([string]$manifestTool.upstream -cne [string]$nexttrace.upstream -or
                [string]$manifestTool.version -cne [string]$nexttrace.version -or
                [string]$manifestTool.tag_or_commit -cne [string]$nexttrace.tag -or
                [string]$manifestTool.source -cne $nexttraceAssetUrl -or
                [string]$manifestTool.architecture -cne 'amd64' -or
                [string]$manifestTool.license -cne 'GPL-3.0-only' -or
                [string]$parameters.repository -cne [string]$nexttrace.repository -or
                [string]$parameters.tag -cne [string]$nexttrace.tag -or
                [string]$parameters.release_commit -cne [string]$nexttrace.commit -or
                [string]$parameters.provenance -cne 'upstream official release binary' -or
                [string]$parameters.source_mode -cne 'verified-upstream-prebuilt' -or
                @($parameters.dependency_allowlist).Count -eq 0 -or
                (@($parameters.dependency_allowlist) -join '|') -cne (@($Lock.windows_dll_allowlist) -join '|')) {
                Stop-EcsWindowsGate 'NextTrace verified-upstream-prebuilt metadata mismatch'
            }
            foreach ($factName in @('pe_machine', 'pe_imports', 'imports_checked', 'fully_static', 'stripped')) {
                if ($null -eq $parameters.PSObject.Properties[$factName]) {
                    Stop-EcsWindowsGate "NextTrace manifest omits producer fact $factName"
                }
            }
            if ([string]$parameters.pe_machine -cne 'pei-x86-64' -or -not [bool]$parameters.imports_checked -or -not [bool]$parameters.fully_static) {
                Stop-EcsWindowsGate 'NextTrace producer facts are incomplete'
            }
            continue
        }

        $lockedName = if ($name -eq 'npb-ft') { 'npb-ep' } else { $name }
        $lockedTools = @($Lock.tools | Where-Object { [string]$_.name -ceq $lockedName })
        if ($lockedTools.Count -ne 1) { Stop-EcsWindowsGate "manifest identity mismatch for $name" }
        $locked = $lockedTools[0]
        $lockedVersion = if ($locked.PSObject.Properties.Name -contains 'version') { [string]$locked.version } else { '' }
        $lockedTag = if ($locked.PSObject.Properties.Name -contains 'tag') { [string]$locked.tag } else { '' }
        if (($lockedVersion -and [string]$manifestTool.version -cne $lockedVersion) -or
            ($lockedTag -and [string]$manifestTool.tag_or_commit -cne $lockedTag) -or
            (-not $lockedVersion -and [string]$manifestTool.version -eq '') -or
            (-not $lockedTag -and [string]$manifestTool.tag_or_commit -eq '')) {
            Stop-EcsWindowsGate "manifest identity mismatch for $name"
        }
        $expectedSource = if ($locked.PSObject.Properties.Name -contains 'repository') {
            "git+https://github.com/$($locked.repository).git@$($locked.commit)"
        } else {
            [string]$locked.source_url
        }
        if ([string]$manifestTool.source -cne $expectedSource) {
            Stop-EcsWindowsGate "manifest source mismatch for $name"
        }
        $parameters = $manifestTool.parameters
        if ([string]$parameters.pe_machine -cne 'pei-x86-64' -or
            -not [bool]$parameters.imports_checked -or -not [bool]$parameters.fully_static -or
            -not [bool]$parameters.stripped -or
            (@($parameters.dependency_allowlist) -join '|') -cne (@($Lock.windows_dll_allowlist) -join '|')) {
            Stop-EcsWindowsGate "manifest producer facts are incomplete for $name"
        }
    }
}

if ($CheckOrdinaryUser) {
    $null = Invoke-EcsNoAdminOperationCheck -StageRoot (Get-Location).Path
    Write-Output 'windows-tools-gate: -CheckOrdinaryUser validates no-admin-operation only; ordinary-user token execution is not claimed; packaged E2E owns production workload checks'
} else {
    $lock = Get-EcsGateJson -Path $LockPath
    Assert-EcsWindowsPackageContract -PackageStage ([IO.Path]::GetFullPath($StageRoot)) -Lock $lock
    Write-Output 'windows-tools-gate: packaged stage layout, license files, manifest metadata, and builder-produced PE facts passed'
}
