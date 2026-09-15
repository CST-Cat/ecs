[CmdletBinding(DefaultParameterSetName = 'FullGate')]
param(
    [Parameter(Mandatory, ParameterSetName = 'FullGate')][string]$StageRoot,
    [Parameter(Mandatory, ParameterSetName = 'FullGate')][string]$ManifestPath,
    [Parameter(Mandatory, ParameterSetName = 'FullGate')][string]$LockPath,
    [Parameter(Mandatory, ParameterSetName = 'FullGate')][string]$CorpusPath,
    [Parameter(Mandatory, ParameterSetName = 'FullGate')][string]$ObjdumpPath,
    [Parameter(ParameterSetName = 'FullGate')][int]$TimeoutSeconds = 180,
    [Parameter(Mandatory, ParameterSetName = 'OrdinaryUser')][switch]$CheckOrdinaryUser
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

function ConvertTo-EcsProcessArgument {
    param([Parameter(Mandatory)][AllowEmptyString()][string]$Value)
    if ($Value.Length -gt 0 -and $Value -notmatch '[\s"]') { return $Value }
    $builder = New-Object Text.StringBuilder
    [void]$builder.Append('"')
    $backslashes = 0
    foreach ($character in $Value.ToCharArray()) {
        if ($character -eq '\') {
            $backslashes++
            continue
        }
        if ($character -eq '"') {
            for ($index = 0; $index -lt (2 * $backslashes + 1); $index++) { [void]$builder.Append('\') }
            [void]$builder.Append('"')
        } else {
            for ($index = 0; $index -lt $backslashes; $index++) { [void]$builder.Append('\') }
            [void]$builder.Append($character)
        }
        $backslashes = 0
    }
    for ($index = 0; $index -lt (2 * $backslashes); $index++) { [void]$builder.Append('\') }
    [void]$builder.Append('"')
    return $builder.ToString()
}

function Ensure-EcsWindowsJobObjectType {
    if ('EcsWindowsJobObject' -as [type]) { return }
    Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;

public static class EcsWindowsJobObject
{
    private const uint JobObjectExtendedLimitInformationClass = 9;
    private const uint JobObjectLimitKillOnJobClose = 0x2000;

    [StructLayout(LayoutKind.Sequential)]
    private struct JobObjectBasicLimitInformation
    {
        public long PerProcessUserTimeLimit;
        public long PerJobUserTimeLimit;
        public uint LimitFlags;
        public UIntPtr MinimumWorkingSetSize;
        public UIntPtr MaximumWorkingSetSize;
        public uint ActiveProcessLimit;
        public UIntPtr Affinity;
        public uint PriorityClass;
        public uint SchedulingClass;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct IoCounters
    {
        public ulong ReadOperationCount;
        public ulong WriteOperationCount;
        public ulong OtherOperationCount;
        public ulong ReadTransferCount;
        public ulong WriteTransferCount;
        public ulong OtherTransferCount;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct JobObjectExtendedLimitInformation
    {
        public JobObjectBasicLimitInformation BasicLimitInformation;
        public IoCounters IoInfo;
        public UIntPtr ProcessMemoryLimit;
        public UIntPtr JobMemoryLimit;
        public UIntPtr PeakProcessMemoryUsed;
        public UIntPtr PeakJobMemoryUsed;
    }

    [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
    private static extern IntPtr CreateJobObjectW(IntPtr jobAttributes, string name);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool SetInformationJobObject(
        IntPtr job,
        uint informationClass,
        ref JobObjectExtendedLimitInformation information,
        uint informationLength);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool AssignProcessToJobObject(IntPtr job, IntPtr process);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool CloseHandle(IntPtr handle);

    private static void ThrowLastWin32Error(string operation)
    {
        throw new Win32Exception(Marshal.GetLastWin32Error(), operation);
    }

    public static IntPtr CreateKillOnCloseJob()
    {
        IntPtr job = CreateJobObjectW(IntPtr.Zero, null);
        if (job == IntPtr.Zero) ThrowLastWin32Error("CreateJobObjectW");
        JobObjectExtendedLimitInformation information = new JobObjectExtendedLimitInformation();
        information.BasicLimitInformation.LimitFlags = JobObjectLimitKillOnJobClose;
        uint length = (uint)Marshal.SizeOf(typeof(JobObjectExtendedLimitInformation));
        if (!SetInformationJobObject(job, JobObjectExtendedLimitInformationClass, ref information, length))
        {
            CloseHandle(job);
            ThrowLastWin32Error("SetInformationJobObject");
        }
        return job;
    }

    public static void Assign(IntPtr job, IntPtr process)
    {
        if (!AssignProcessToJobObject(job, process)) ThrowLastWin32Error("AssignProcessToJobObject");
    }

    public static void Close(IntPtr job)
    {
        if (job != IntPtr.Zero && !CloseHandle(job)) ThrowLastWin32Error("CloseHandle(job)");
    }
}
'@
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

function Invoke-EcsOrdinaryUserCheck {
    param([Parameter(Mandatory)][string]$StageRoot)

    $token = Get-EcsWindowsTokenEvidence
    Write-Host ("ordinary-user token evidence: user={0}; administrator_group_membership={1}; token_elevated={2}; token_elevation_type={3}" -f $token.User, $token.AdministratorGroupMembership, $token.TokenElevated, $token.ElevationTypeName)
    if ($token.TokenElevated -or $token.ElevationType -eq 2) {
        Write-Host 'ordinary-user token evidence: actual token is elevated; continuing only with the no-admin-operation checks below'
    }

    $stagePath = [IO.Path]::GetFullPath($StageRoot)
    foreach ($root in Get-EcsProtectedInstallRoots) {
        if (Test-EcsPathUnderRoot -Path $stagePath -Root $root) {
            Stop-EcsWindowsGate "ordinary-user evidence path is under protected Program Files root: $stagePath"
        }
    }

    $machinePathBefore = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Machine)
    $userPathBefore = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
    $probe = Join-Path ([IO.Path]::GetTempPath()) ("ecs-ordinary-user." + [guid]::NewGuid().ToString('N') + '.tmp')
    foreach ($root in Get-EcsProtectedInstallRoots) {
        if (Test-EcsPathUnderRoot -Path $probe -Root $root) {
            Stop-EcsWindowsGate "ordinary-user probe is under protected Program Files root: $probe"
        }
    }
    $probeText = 'ecs ordinary-user execution evidence'
    try {
        [IO.File]::WriteAllText($probe, $probeText)
        if ([IO.File]::ReadAllText($probe) -cne $probeText) {
            Stop-EcsWindowsGate 'ordinary-user temporary write/read evidence did not round-trip'
        }
    } finally {
        if (Test-Path -LiteralPath $probe -PathType Leaf) { Remove-Item -LiteralPath $probe -Force }
    }
    $machinePathAfter = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Machine)
    $userPathAfter = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
    if ($machinePathBefore -cne $machinePathAfter -or $userPathBefore -cne $userPathAfter) {
        Stop-EcsWindowsGate 'ordinary-user evidence detected a Machine or User PATH mutation'
    }

    Write-Host 'ordinary-user execution evidence: temp_write=passed; protected_install_path=not_used; machine_path=unchanged; user_path=unchanged'
    return [pscustomobject]@{
        MachinePath = $machinePathBefore
        UserPath = $userPathBefore
    }
}

function Assert-EcsGlobalPathUnchanged {
    param([Parameter(Mandatory)][pscustomobject]$Evidence)
    $machinePathAfter = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Machine)
    $userPathAfter = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
    if ($Evidence.MachinePath -cne $machinePathAfter -or $Evidence.UserPath -cne $userPathAfter) {
        Stop-EcsWindowsGate 'real workload changed the Machine or User PATH'
    }
    Write-Host 'ordinary-user execution evidence: real workload Machine/User PATH remained unchanged'
}

function Invoke-EcsWindowsProcess {
    param(
        [Parameter(Mandatory)][string]$FilePath,
        [Parameter(Mandatory)][AllowEmptyCollection()][string[]]$ArgumentList,
        [Parameter(Mandatory)][string]$WorkingDirectory,
        [hashtable]$Environment = @{},
        [int]$Timeout = 180
    )

    Ensure-EcsWindowsJobObjectType
    $start = New-Object Diagnostics.ProcessStartInfo
    $start.FileName = $FilePath
    $start.Arguments = (($ArgumentList | ForEach-Object { ConvertTo-EcsProcessArgument $_ }) -join ' ')
    $start.WorkingDirectory = $WorkingDirectory
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    foreach ($key in $Environment.Keys) {
        $start.EnvironmentVariables[$key] = [string]$Environment[$key]
    }
    $process = New-Object Diagnostics.Process
    $process.StartInfo = $start
    $job = [IntPtr]::Zero
    $stdoutTask = $null
    $stderrTask = $null
    $started = $false
    $assigned = $false
    try {
        $job = [EcsWindowsJobObject]::CreateKillOnCloseJob()
        if (-not $process.Start()) { Stop-EcsWindowsGate "could not start $FilePath" }
        $started = $true
        [EcsWindowsJobObject]::Assign($job, $process.Handle)
        $assigned = $true
        $stdoutTask = $process.StandardOutput.ReadToEndAsync()
        $stderrTask = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit($Timeout * 1000)) {
            [EcsWindowsJobObject]::Close($job)
            $job = [IntPtr]::Zero
            $process.WaitForExit()
            [void]$stdoutTask.GetAwaiter().GetResult()
            [void]$stderrTask.GetAwaiter().GetResult()
            Stop-EcsWindowsGate "$([IO.Path]::GetFileName($FilePath)) timed out after ${Timeout}s; Job Object terminated the process tree"
        }
        $process.WaitForExit()
        [EcsWindowsJobObject]::Close($job)
        $job = [IntPtr]::Zero
        $stdout = $stdoutTask.GetAwaiter().GetResult()
        $stderr = $stderrTask.GetAwaiter().GetResult()
        return [pscustomobject]@{ ExitCode = $process.ExitCode; Stdout = $stdout; Stderr = $stderr }
    } finally {
        if ($job -ne [IntPtr]::Zero) {
            [EcsWindowsJobObject]::Close($job)
            $job = [IntPtr]::Zero
            if ($started -and $assigned) {
                $process.WaitForExit()
            }
        }
        $process.Dispose()
    }
}

function Assert-EcsProcessSucceeded {
    param(
        [Parameter(Mandatory)][pscustomobject]$Result,
        [Parameter(Mandatory)][string]$Description
    )
    if ($Result.ExitCode -ne 0) {
        Stop-EcsWindowsGate "$Description failed with exit code $($Result.ExitCode): $($Result.Stderr.Trim())"
    }
}

function Assert-EcsText {
    param(
        [Parameter(Mandatory)][string]$Text,
        [Parameter(Mandatory)][string]$Pattern,
        [Parameter(Mandatory)][string]$Description
    )
    if ($Text -notmatch $Pattern) {
        Stop-EcsWindowsGate "$Description was not recognized"
    }
}

function Get-EcsPeFacts {
    param(
        [Parameter(Mandatory)][string]$Objdump,
        [Parameter(Mandatory)][string]$Binary,
        [Parameter(Mandatory)][string[]]$Allowlist
    )
    $result = Invoke-EcsWindowsProcess -FilePath $Objdump -ArgumentList @('-p', $Binary) -WorkingDirectory (Split-Path -Parent $Binary) -Timeout 30
    Assert-EcsProcessSucceeded -Result $result -Description "PE inspection of $([IO.Path]::GetFileName($Binary))"
    $peText = $result.Stdout + $result.Stderr
    $machineMatch = [regex]::Match($peText, '(?im)\bfile format\s+(?<machine>\S+)')
    $machine = if ($machineMatch.Success) { $machineMatch.Groups['machine'].Value } else { '' }
    if ($machine -ne 'pei-x86-64') {
        Stop-EcsWindowsGate "$(Split-Path -Leaf $Binary) is not a PE x86-64 executable"
    }
    $imports = @([regex]::Matches($peText, '(?im)^\s*DLL Name:\s*(?<name>\S+)\s*$') | ForEach-Object { $_.Groups['name'].Value } | Sort-Object -Unique)
    $nonSystemImports = @(
        foreach ($import in $imports) {
            if (-not (Test-EcsAllowedImport -Name $import -Allowlist $Allowlist) -or
                $import -match '(?i)^(libgcc|libgomp|libwinpthread|libstdc\+\+|libgfortran|libssl|libcrypto|cygwin|msys)') {
                $import
            }
        }
    )
    $fullyStatic = $nonSystemImports.Count -eq 0
    if (-not $fullyStatic) {
        Stop-EcsWindowsGate "$(Split-Path -Leaf $Binary) imports non-self-contained DLLs: $($nonSystemImports -join ', ')"
    }
    $sections = Invoke-EcsWindowsProcess -FilePath $Objdump -ArgumentList @('-h', $Binary) -WorkingDirectory (Split-Path -Parent $Binary) -Timeout 30
    Assert-EcsProcessSucceeded -Result $sections -Description "PE section inspection of $([IO.Path]::GetFileName($Binary))"
    $symbols = Invoke-EcsWindowsProcess -FilePath $Objdump -ArgumentList @('-t', $Binary) -WorkingDirectory (Split-Path -Parent $Binary) -Timeout 30
    Assert-EcsProcessSucceeded -Result $symbols -Description "PE symbol inspection of $([IO.Path]::GetFileName($Binary))"
    $sectionText = $sections.Stdout + $sections.Stderr
    $symbolText = $symbols.Stdout + $symbols.Stderr
    $sectionNames = @(
        [regex]::Matches($sectionText, '(?im)^\s*\d+\s+(?<name>\.[^\s]+)(?:\s|$)') |
            ForEach-Object { $_.Groups['name'].Value }
    )
    $forbiddenSections = @($sectionNames | Where-Object {
        $_ -match '^\.(?:debug|zdebug|stab|gnu_debug|symtab|strtab)'
    })
    $hasDebuggingSectionFlag = $sectionText -match '(?im)^\s+.*\bDEBUGGING\b'
    $noSymbols = $symbolText -match '(?im)^\s*no symbols\s*$'
    $stripped = ($forbiddenSections.Count -eq 0) -and (-not $hasDebuggingSectionFlag) -and $noSymbols
    return [pscustomobject]@{
        Machine = $machine
        Imports = @($imports)
        ImportsChecked = $true
        FullyStatic = [bool]$fullyStatic
        Stripped = [bool]$stripped
    }
}

function Test-EcsAllowedImport {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string[]]$Allowlist
    )
    foreach ($pattern in $Allowlist) {
        if ($Name -like $pattern) { return $true }
    }
    return $false
}

function New-EcsGateWorkDirectory {
    $path = Join-Path ([IO.Path]::GetTempPath()) ("ecs-windows-tools-gate." + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path $path | Out-Null
    return $path
}

function New-EcsFixedSample {
    param(
        [Parameter(Mandatory)][string]$Source,
        [Parameter(Mandatory)][string]$Destination,
        [int]$Bytes = 1048576
    )
    $input = [IO.File]::OpenRead($Source)
    $output = [IO.File]::Open($Destination, [IO.FileMode]::Create, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $buffer = New-Object byte[] 65536
        $remaining = $Bytes
        while ($remaining -gt 0) {
            $read = $input.Read($buffer, 0, [Math]::Min($buffer.Length, $remaining))
            if ($read -le 0) { Stop-EcsWindowsGate "corpus is shorter than $Bytes bytes" }
            $output.Write($buffer, 0, $read)
            $remaining -= $read
        }
    } finally {
        $input.Dispose()
        $output.Dispose()
    }
}

if ($CheckOrdinaryUser) {
    $null = Invoke-EcsOrdinaryUserCheck -StageRoot (Get-Location).Path
    Write-Output 'windows-tools-gate: ordinary-user/no-admin-operation check passed; real workload gate remains required'
    exit 0
}

$lock = Get-EcsGateJson $LockPath
$manifest = Get-EcsGateJson $ManifestPath
$expectedTools = @($lock.windows_tools)
$frozenTools = @('zstd', 'npb-ep', 'npb-ft', 'openssl', 'stream', 'fio')
if ([string]$lock.schema_version -ne 'ecs.tools.lock/v1') { Stop-EcsWindowsGate 'unexpected tools lock schema' }
if (($expectedTools -join '|') -ne ($frozenTools -join '|')) { Stop-EcsWindowsGate 'Windows tool set is not the frozen six-tool contract' }
$targetFacts = @($lock.architectures | Where-Object { $_.target -eq 'windows_amd64' })
if ($targetFacts.Count -ne 1 -or [string]$targetFacts[0].goos -ne 'windows' -or [string]$targetFacts[0].goarch -ne 'amd64' -or [string]$targetFacts[0].package -ne 'amd64') {
    Stop-EcsWindowsGate 'tools lock has no unique windows_amd64 target facts'
}
if (@($lock.windows_dll_allowlist).Count -eq 0) { Stop-EcsWindowsGate 'Windows DLL import allowlist is empty' }
if ([string]$manifest.schema_version -ne 'ecs-tools.manifest/v1') { Stop-EcsWindowsGate 'manifest schema changed' }
if ([string]$manifest.target -ne 'windows_amd64' -or [string]$manifest.goos -ne 'windows' -or [string]$manifest.goarch -ne 'amd64' -or [string]$manifest.architecture -ne 'amd64') {
    Stop-EcsWindowsGate 'manifest target facts are not windows_amd64'
}
if ((@($manifest.supported_architectures) -join '|') -ne 'amd64' -or (@($manifest.supported_targets) -join '|') -ne 'windows_amd64') {
    Stop-EcsWindowsGate 'manifest supported target lists are not the frozen Windows contract'
}
if ([string]$manifest.build.toolchain_mode -ne 'native' -or
    [string]$manifest.build.build_triplet -ne 'x86_64-w64-mingw32' -or
    [string]$manifest.build.target_triplet -ne 'x86_64-w64-mingw32' -or
    [string]$manifest.build.smoke_runner -ne 'direct' -or
    [string]$manifest.build.validation.scope -ne 'functional') {
    Stop-EcsWindowsGate 'manifest build facts are not the frozen native functional contract'
}
if ([bool]$manifest.build.validation.performance_valid) { Stop-EcsWindowsGate 'performance_valid must remain false' }
$manifestTools = @($manifest.tools)
if (($manifestTools.name -join '|') -ne ($expectedTools -join '|')) { Stop-EcsWindowsGate 'manifest tool set/order differs from tools lock' }
if (@($manifestTools | Where-Object { [string]$_.name -match '\.exe$' -or [string]$_.name -eq 'nexttrace-tiny' }).Count -ne 0) { Stop-EcsWindowsGate 'manifest contains a physical .exe id or NextTrace' }
if (-not (Test-Path -LiteralPath $CorpusPath -PathType Leaf)) { Stop-EcsWindowsGate "missing fixed corpus: $CorpusPath" }
if (-not (Test-Path -LiteralPath $ObjdumpPath -PathType Leaf)) { Stop-EcsWindowsGate "missing pinned PE inspector: $ObjdumpPath" }
$corpusFile = Get-Item -LiteralPath $CorpusPath
$corpusHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $CorpusPath).Hash.ToLowerInvariant()
if ($corpusFile.Length -ne [int64]$lock.corpus.bytes -or $corpusHash -ne [string]$lock.corpus.sha256) {
    Stop-EcsWindowsGate "fixed Silesia corpus mismatch: bytes=$($corpusFile.Length) sha256=$corpusHash"
}

$binDir = Join-Path $StageRoot 'bin'
$licenseDir = Join-Path $StageRoot 'LICENSES'
$manifestFile = Join-Path $StageRoot 'manifest.json'
if (-not (Test-Path -LiteralPath $binDir -PathType Container)) { Stop-EcsWindowsGate 'bundle has no bin directory' }
if (-not (Test-Path -LiteralPath $licenseDir -PathType Container)) { Stop-EcsWindowsGate 'bundle has no LICENSES directory' }
if (-not (Test-Path -LiteralPath $manifestFile -PathType Leaf)) { Stop-EcsWindowsGate 'bundle has no manifest.json' }
$requiredLicenses = @('ZSTD-LICENSE', 'ZSTD-COPYING', 'NPB-README.txt', 'NPB-LICENSE.txt', 'OPENSSL-LICENSE.txt', 'FIO-COPYING', 'STREAM-LICENSE.txt')
foreach ($license in $requiredLicenses) {
    $licensePath = Join-Path $licenseDir $license
    if (-not (Test-Path -LiteralPath $licensePath -PathType Leaf) -or (Get-Item -LiteralPath $licensePath).Length -eq 0) {
        Stop-EcsWindowsGate "missing or empty license file: $license"
    }
}
$binaries = @(Get-ChildItem -LiteralPath $binDir -File | Sort-Object Name)
if ($binaries.Count -ne $expectedTools.Count) { Stop-EcsWindowsGate "bundle has $($binaries.Count) binaries, want $($expectedTools.Count)" }
foreach ($name in $expectedTools) {
    $binary = Join-Path $binDir "$name.exe"
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { Stop-EcsWindowsGate "missing binary $name.exe" }
    if ((Get-Item -LiteralPath $binary).Length -le 0) { Stop-EcsWindowsGate "$name.exe is empty" }
}
foreach ($manifestTool in $manifestTools) {
    $manifestBinary = Join-Path $binDir "$($manifestTool.name).exe"
    $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $manifestBinary).Hash.ToLowerInvariant()
    if ([string]$manifestTool.parameters.binary_sha256 -ne $actualHash) { Stop-EcsWindowsGate "manifest hash mismatch for $($manifestTool.name)" }
    if ([string]$manifestTool.parameters.pe_machine -ne 'pei-x86-64' -or
        -not [bool]$manifestTool.parameters.imports_checked -or
        -not [bool]$manifestTool.parameters.fully_static -or
        -not [bool]$manifestTool.parameters.stripped -or
        (@($manifestTool.parameters.dependency_allowlist) -join '|') -ne (@($lock.windows_dll_allowlist) -join '|')) {
        Stop-EcsWindowsGate "manifest build facts are incomplete for $($manifestTool.name)"
    }
    $locked = @($lock.tools | Where-Object { $_.name -eq $manifestTool.name })
    if ($manifestTool.name -eq 'npb-ft') { $locked = @($lock.tools | Where-Object { $_.name -eq 'npb-ep' }) }
    if ($locked.Count -ne 1) {
        Stop-EcsWindowsGate "manifest identity mismatch for $($manifestTool.name)"
    }
    $lockedVersion = if ($locked[0].PSObject.Properties.Name -contains 'version') { [string]$locked[0].version } else { '' }
    $lockedTag = if ($locked[0].PSObject.Properties.Name -contains 'tag') { [string]$locked[0].tag } else { '' }
    if (($lockedVersion -and [string]$manifestTool.version -ne $lockedVersion) -or
        ($lockedTag -and [string]$manifestTool.tag_or_commit -ne $lockedTag) -or
        (-not $lockedVersion -and [string]$manifestTool.version -eq '') -or
        (-not $lockedTag -and [string]$manifestTool.tag_or_commit -eq '')) {
        Stop-EcsWindowsGate "manifest identity mismatch for $($manifestTool.name)"
    }
    $expectedSource = if ($locked[0].PSObject.Properties.Name -contains 'repository') {
        "git+https://github.com/$($locked[0].repository).git@$($locked[0].commit)"
    } else {
        [string]$locked[0].source_url
    }
    if ([string]$manifestTool.source -ne $expectedSource) { Stop-EcsWindowsGate "manifest source mismatch for $($manifestTool.name)" }
    if ($locked[0].PSObject.Properties.Name -contains 'source_sha256' -and
        [string]$manifestTool.parameters.source_sha256 -ne [string]$locked[0].source_sha256) {
        Stop-EcsWindowsGate "manifest source hash mismatch for $($manifestTool.name)"
    }
}
if (@(Get-ChildItem -LiteralPath $StageRoot -Recurse -File -Filter '*.dll').Count -ne 0) {
    Stop-EcsWindowsGate 'bundle contains a non-system DLL'
}

$ordinaryEvidence = Invoke-EcsOrdinaryUserCheck -StageRoot $StageRoot

foreach ($binaryFile in $binaries) {
    $peFacts = Get-EcsPeFacts -Objdump $ObjdumpPath -Binary $binaryFile.FullName -Allowlist @($lock.windows_dll_allowlist)
    $logicalName = [IO.Path]::GetFileNameWithoutExtension($binaryFile.Name)
    $manifestTool = @($manifestTools | Where-Object { $_.name -eq $logicalName })
    if ($manifestTool.Count -ne 1) {
        Stop-EcsWindowsGate "manifest has no unique entry for $($binaryFile.Name)"
    }
    $manifestParameters = $manifestTool[0].parameters
    if ([string]$manifestParameters.pe_machine -ne [string]$peFacts.Machine -or
        (@($manifestParameters.pe_imports) -join '|') -ne (@($peFacts.Imports) -join '|') -or
        [bool]$manifestParameters.imports_checked -ne [bool]$peFacts.ImportsChecked -or
        [bool]$manifestParameters.fully_static -ne [bool]$peFacts.FullyStatic -or
        [bool]$manifestParameters.stripped -ne [bool]$peFacts.Stripped) {
        Stop-EcsWindowsGate "manifest PE facts do not match the inspected $($binaryFile.Name)"
    }
}

$gateWork = New-EcsGateWorkDirectory
$originalPath = $env:PATH
$systemPath = "$env:SystemRoot\System32;$env:SystemRoot"
try {
    foreach ($root in Get-EcsProtectedInstallRoots) {
        if (Test-EcsPathUnderRoot -Path $gateWork -Root $root) {
            Stop-EcsWindowsGate "real workload directory is under protected Program Files root: $gateWork"
        }
    }
    $env:PATH = $systemPath
    $zstd = Join-Path $binDir 'zstd.exe'
    $npbEP = Join-Path $binDir 'npb-ep.exe'
    $npbFT = Join-Path $binDir 'npb-ft.exe'
    $openssl = Join-Path $binDir 'openssl.exe'
    $stream = Join-Path $binDir 'stream.exe'
    $fio = Join-Path $binDir 'fio.exe'

    $zstdVersion = [string](@($lock.tools | Where-Object { $_.name -eq 'zstd' })[0].version)
    $zstdRun = Invoke-EcsWindowsProcess -FilePath $zstd -ArgumentList @('--version') -WorkingDirectory $gateWork -Environment @{ PATH = $systemPath } -Timeout $TimeoutSeconds
    Assert-EcsProcessSucceeded -Result $zstdRun -Description 'zstd --version'
    Assert-EcsText -Text ($zstdRun.Stdout + $zstdRun.Stderr) -Pattern "v$([regex]::Escape($zstdVersion))([^0-9]|$)" -Description 'zstd version'
    $sample = Join-Path $gateWork 'silesia-shortest.corpus'
    New-EcsFixedSample -Source $CorpusPath -Destination $sample
    $zstdBench = Invoke-EcsWindowsProcess -FilePath $zstd -ArgumentList @('-q', '-b3', '-i1', '-T1', $sample) -WorkingDirectory $gateWork -Environment @{ PATH = $systemPath } -Timeout $TimeoutSeconds
    Assert-EcsProcessSucceeded -Result $zstdBench -Description 'zstd shortest fixed Silesia benchmark'
    Assert-EcsText -Text ($zstdBench.Stdout + $zstdBench.Stderr) -Pattern "bench $([regex]::Escape($zstdVersion)).*input 1048576 bytes, 1 seconds" -Description 'zstd shortest fixed benchmark output'
    Assert-EcsText -Text ($zstdBench.Stdout + $zstdBench.Stderr) -Pattern '(?m)^-3\s+.*MB/s\s+.*MB/s' -Description 'zstd compression and decompression throughput output'

    $npbVersion = [string](@($lock.tools | Where-Object { $_.name -eq 'npb-ep' })[0].version)
    $ompEnvironment = @{ PATH = $systemPath; OMP_NUM_THREADS = '1'; OMP_DYNAMIC = 'FALSE'; OMP_PROC_BIND = 'close'; OMP_PLACES = 'cores'; OMP_SCHEDULE = 'static'; NPB_TIMER_FLAG = '0' }
    foreach ($case in @([pscustomobject]@{ Name = 'EP'; Path = $npbEP }, [pscustomobject]@{ Name = 'FT'; Path = $npbFT })) {
        $npbRunDir = Join-Path $gateWork ("npb-" + $case.Name.ToLowerInvariant())
        New-Item -ItemType Directory -Force -Path $npbRunDir | Out-Null
        $result = Invoke-EcsWindowsProcess -FilePath $case.Path -ArgumentList @() -WorkingDirectory $npbRunDir -Environment $ompEnvironment -Timeout $TimeoutSeconds
        Assert-EcsProcessSucceeded -Result $result -Description "NPB $($case.Name) Class A smoke"
        $text = $result.Stdout + $result.Stderr
        Assert-EcsText -Text $text -Pattern "NAS Parallel Benchmarks \(NPB3\.4-OMP\) - $($case.Name) Benchmark" -Description "NPB $($case.Name) header"
        Assert-EcsText -Text $text -Pattern '(?m)^\s*Class\s*=\s*A\s*$' -Description "NPB $($case.Name) Class A"
        Assert-EcsText -Text $text -Pattern '(?m)^\s*Total threads\s*=\s*1\s*$' -Description "NPB $($case.Name) one-thread smoke"
        Assert-EcsText -Text $text -Pattern '(?m)^\s*Verification\s*=\s*SUCCESSFUL\s*$' -Description "NPB $($case.Name) verification"
        Assert-EcsText -Text $text -Pattern "(?m)^\s*Version\s*=\s*$([regex]::Escape($npbVersion))\s*$" -Description "NPB $($case.Name) version"
    }

    $streamRun = Invoke-EcsWindowsProcess -FilePath $stream -ArgumentList @() -WorkingDirectory $gateWork -Environment (@{ PATH = $systemPath; OMP_NUM_THREADS = '1' }) -Timeout $TimeoutSeconds
    Assert-EcsProcessSucceeded -Result $streamRun -Description 'STREAM Copy/Scale/Add/Triad smoke'
    foreach ($kernel in @('Copy', 'Scale', 'Add', 'Triad')) { Assert-EcsText -Text ($streamRun.Stdout + $streamRun.Stderr) -Pattern "(?m)^\s*${kernel}:" -Description "STREAM $kernel output" }
    Assert-EcsText -Text ($streamRun.Stdout + $streamRun.Stderr) -Pattern 'Solution Validates' -Description 'STREAM validation'

    $opensslVersion = [string](@($lock.tools | Where-Object { $_.name -eq 'openssl' })[0].version)
    $opensslEnvironment = @{ PATH = $systemPath; OPENSSL_CONF = 'NUL' }
    $opensslVersionRun = Invoke-EcsWindowsProcess -FilePath $openssl -ArgumentList @('version') -WorkingDirectory $gateWork -Environment $opensslEnvironment -Timeout $TimeoutSeconds
    Assert-EcsProcessSucceeded -Result $opensslVersionRun -Description 'OpenSSL version'
    Assert-EcsText -Text ($opensslVersionRun.Stdout + $opensslVersionRun.Stderr) -Pattern "^OpenSSL $([regex]::Escape($opensslVersion))([\s]|$)" -Description 'OpenSSL locked version'
    foreach ($algorithm in @('aes-256-gcm', 'chacha20-poly1305', 'sha256')) {
        $arguments = @('speed', '-elapsed', '-seconds', '1', '-bytes', '16384', '-mr', '-multi', '1', '-evp', $algorithm)
        if ($algorithm -ne 'sha256') { $arguments += '-aead' }
        $speed = Invoke-EcsWindowsProcess -FilePath $openssl -ArgumentList $arguments -WorkingDirectory $gateWork -Environment $opensslEnvironment -Timeout $TimeoutSeconds
        Assert-EcsProcessSucceeded -Result $speed -Description "OpenSSL speed $algorithm"
        $text = $speed.Stdout + $speed.Stderr
        $label = if ($algorithm -eq 'aes-256-gcm') { 'AES-256-GCM' } elseif ($algorithm -eq 'chacha20-poly1305') { 'ChaCha20-Poly1305' } else { 'sha256' }
        Assert-EcsText -Text $text -Pattern ("\+DT:{0}:1:16384" -f $label) -Description "OpenSSL speed $algorithm parameters"
        Assert-EcsText -Text $text -Pattern ("(?m)^\+F:[0-9]+:{0}:[0-9]+(\.[0-9]+)?\s*$" -f [regex]::Escape($label)) -Description "OpenSSL speed $algorithm machine-readable output"
    }

    $fioData = Join-Path $gateWork 'fio-windowsaio.data'
    [IO.File]::WriteAllBytes($fioData, (New-Object byte[] 4096))
    $fioVersion = [string](@($lock.tools | Where-Object { $_.name -eq 'fio' })[0].version)
    $fioVersionRun = Invoke-EcsWindowsProcess -FilePath $fio -ArgumentList @('--version') -WorkingDirectory $gateWork -Environment @{ PATH = $systemPath } -Timeout $TimeoutSeconds
    Assert-EcsProcessSucceeded -Result $fioVersionRun -Description 'fio version'
    Assert-EcsText -Text ($fioVersionRun.Stdout + $fioVersionRun.Stderr) -Pattern "fio-$([regex]::Escape($fioVersion))([\s]|$)" -Description 'fio locked version'
    $fioJson = Join-Path $gateWork 'fio-windowsaio.json'
    $fioRun = Invoke-EcsWindowsProcess -FilePath $fio -ArgumentList @('--name=ecs-windowsaio-smoke', "--filename=$fioData", '--rw=read', '--bs=4k', '--size=4k', '--ioengine=windowsaio', '--iodepth=1', '--numjobs=1', '--direct=1', '--output-format=json', "--output=$fioJson") -WorkingDirectory $gateWork -Environment @{ PATH = $systemPath } -Timeout $TimeoutSeconds
    Assert-EcsProcessSucceeded -Result $fioRun -Description 'fio windowsaio real I/O smoke'
    if (-not (Test-Path -LiteralPath $fioJson -PathType Leaf)) { Stop-EcsWindowsGate 'fio did not produce JSON output' }
    $fioResult = Get-EcsGateJson $fioJson
    if (@($fioResult.jobs).Count -ne 1 -or [string]$fioResult.jobs[0].jobname -ne 'ecs-windowsaio-smoke') { Stop-EcsWindowsGate 'fio JSON job identity is invalid' }
    if ([string]$fioResult.jobs[0].'job options'.ioengine -ne 'windowsaio' -or [int64]$fioResult.jobs[0].read.io_bytes -lt 4096) { Stop-EcsWindowsGate 'fio JSON did not prove a windowsaio read' }
    $enghelp = Invoke-EcsWindowsProcess -FilePath $fio -ArgumentList @('--enghelp') -WorkingDirectory $gateWork -Environment @{ PATH = $systemPath } -Timeout $TimeoutSeconds
    Assert-EcsProcessSucceeded -Result $enghelp -Description 'fio engine list'
    Assert-EcsText -Text ($enghelp.Stdout + $enghelp.Stderr) -Pattern '(?im)^\s*windowsaio\b' -Description 'fio windowsaio engine'

    Assert-EcsGlobalPathUnchanged -Evidence $ordinaryEvidence
    Write-Output "windows-tools-gate: $($expectedTools.Count)/$($expectedTools.Count) real functional checks passed; performance_valid=false; route/backtrace=unsupported; NextTrace=not bundled"
} finally {
    $env:PATH = $originalPath
    if (Test-Path -LiteralPath $gateWork) { Remove-Item -LiteralPath $gateWork -Recurse -Force }
}
