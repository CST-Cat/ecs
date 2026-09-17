[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$ReportPath,
    [Parameter(Mandatory)][string]$CapabilityPath,
    [Parameter(Mandatory)][string]$NextTracePath,
    [Parameter(Mandatory)][ValidateSet('route', 'backtrace')][string]$Module,
    [Parameter(Mandatory)][ValidateSet('4', '6')][string]$Family,
    [Parameter(Mandatory)][ValidateSet('ipv4', 'ipv6')][string]$FamilyName,
    [Parameter(Mandatory)][int]$MaxHops,
    [Parameter(Mandatory)][string]$Target
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$NextTraceAdapter = 'nexttrace-json-v1'
$NextTraceEngine = 'nexttrace-tiny'
$NextTraceSourceName = 'probe.route.source.nexttrace.name'
$NextTraceSourceURL = 'https://github.com/nxtrace/NTrace-core'
$NextTraceSHA256 = '16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b'
$CapabilitySchemaVersion = 'ecs.windows.nexttrace.capability/v1'
$CapabilityFamilyName = 'ipv4'
$CapabilityTarget = '1.1.1.1'
$CapabilityMaxHops = 12

function Stop-EcsNextTraceReportAssertion {
    param([Parameter(Mandatory)][string]$Message)
    throw "windows-nexttrace-report-assert: $Message"
}

function Get-EcsRequiredAbsoluteFilePath {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Description
    )
    if ([string]::IsNullOrWhiteSpace($Path) -or -not [IO.Path]::IsPathRooted($Path)) {
        Stop-EcsNextTraceReportAssertion "$Description must be an absolute path"
    }
    try {
        $full = [IO.Path]::GetFullPath($Path)
    } catch {
        Stop-EcsNextTraceReportAssertion "$Description is not a valid path: $($_.Exception.Message)"
    }
    try {
        $item = Get-Item -LiteralPath $full -ErrorAction Stop
    } catch {
        Stop-EcsNextTraceReportAssertion "$Description does not name an existing file: $full"
    }
    if ($item.PSIsContainer) {
        Stop-EcsNextTraceReportAssertion "$Description is a directory: $full"
    }
    return $full
}

function Get-EcsReportProperty {
    param(
        [Parameter(Mandatory)][object]$Object,
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$Context
    )
    if ($null -eq $Object) {
        Stop-EcsNextTraceReportAssertion "$Context is null while reading '$Name'"
    }
    $matches = @($Object.PSObject.Properties | Where-Object { $_.Name -ceq $Name })
    if ($matches.Count -ne 1) {
        Stop-EcsNextTraceReportAssertion "$Context has no unique '$Name' property"
    }
    return $matches[0].Value
}

function Get-EcsRawResultField {
    param(
        [Parameter(Mandatory)][object]$Result,
        [Parameter(Mandatory)][string]$Key
    )
    $fields = @(Get-EcsReportProperty -Object $Result -Name 'fields' -Context "result $($Result.id)")
    $matches = @($fields | Where-Object { [string]$_.key -ceq $Key })
    if ($matches.Count -ne 1) {
        Stop-EcsNextTraceReportAssertion "result $($Result.id) has no unique field '$Key'"
    }
    $value = Get-EcsReportProperty -Object $matches[0] -Name 'value' -Context "result $($Result.id) field $Key"
    $raw = Get-EcsReportProperty -Object $value -Name 'raw' -Context "result $($Result.id) field $Key value"
    if ([string]::IsNullOrWhiteSpace([string]$raw)) {
        Stop-EcsNextTraceReportAssertion "result $($Result.id) field $Key is empty"
    }
    return [string]$raw
}

function Test-EcsActualRespondingHop {
    param([Parameter(Mandatory)][object]$Hop)
    $respondedProperties = @($Hop.PSObject.Properties | Where-Object { $_.Name -ceq 'responded' })
    $ipProperties = @($Hop.PSObject.Properties | Where-Object { $_.Name -ceq 'ip' })
    if ($respondedProperties.Count -ne 1 -or $ipProperties.Count -ne 1) {
        return $false
    }
    if (($respondedProperties[0].Value -isnot [bool]) -or (-not $respondedProperties[0].Value)) {
        return $false
    }
    $ip = [string]$ipProperties[0].Value
    if ([string]::IsNullOrWhiteSpace($ip)) {
        return $false
    }
    try {
        [void][System.Net.IPAddress]::Parse($ip)
    } catch {
        return $false
    }
    return $true
}

function Assert-EcsReportCapabilityEvidence {
    param(
        [Parameter(Mandatory)][object]$Evidence,
        [Parameter(Mandatory)][string]$HelperPath,
        [Parameter(Mandatory)][string]$ExpectedEvidencePath,
        [Parameter(Mandatory)][string]$ExpectedRawDirectory,
        [Parameter(Mandatory)][string]$ExpectedNextTracePath,
        [Parameter(Mandatory)][string]$ExpectedTarget,
        [Parameter(Mandatory)][string]$ExpectedHash,
        [Parameter(Mandatory)][int]$ExpectedMaxHops
    )
    . $HelperPath -Family IPv4 -Target $ExpectedTarget -NextTracePath $ExpectedNextTracePath -ExpectedSha256 $ExpectedHash -EvidencePath $ExpectedEvidencePath -MaxHops $ExpectedMaxHops
    Assert-EcsCapabilityEvidence -Evidence $Evidence `
        -ExpectedEvidencePath $ExpectedEvidencePath `
        -ExpectedRawDirectory $ExpectedRawDirectory `
        -ExpectedNextTracePath $ExpectedNextTracePath
}

$reportFullPath = Get-EcsRequiredAbsoluteFilePath -Path $ReportPath -Description 'report path'
$capabilityFullPath = Get-EcsRequiredAbsoluteFilePath -Path $CapabilityPath -Description 'capability evidence path'
$nextTraceFullPath = Get-EcsRequiredAbsoluteFilePath -Path $NextTracePath -Description 'staged NextTrace executable'
if ([IO.Path]::GetFileName($nextTraceFullPath) -ine 'nexttrace-tiny.exe') {
    Stop-EcsNextTraceReportAssertion "staged NextTrace executable must be named nexttrace-tiny.exe: $nextTraceFullPath"
}

try {
    $nextTraceHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $nextTraceFullPath -ErrorAction Stop).Hash.ToLowerInvariant()
} catch {
    Stop-EcsNextTraceReportAssertion "cannot hash staged NextTrace executable: $($_.Exception.Message)"
}
if ($nextTraceHash -cne $NextTraceSHA256) {
    Stop-EcsNextTraceReportAssertion "staged NextTrace SHA-256 mismatch: expected $NextTraceSHA256, got $nextTraceHash"
}

try {
    $capabilityEvidence = Get-Content -Raw -LiteralPath $capabilityFullPath -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
} catch {
    Stop-EcsNextTraceReportAssertion "capability evidence is invalid JSON: $($_.Exception.Message)"
}

$capabilityHelperPath = Get-EcsRequiredAbsoluteFilePath -Path (Join-Path $PSScriptRoot 'windows_nexttrace_capability.ps1') -Description 'capability validator helper'
try {
    $capabilityRawDirectory = [string](Get-EcsReportProperty -Object $capabilityEvidence -Name 'raw_output_directory' -Context 'capability evidence')
    if (-not [IO.Path]::IsPathRooted($capabilityRawDirectory) -or
        -not (Test-Path -LiteralPath $capabilityRawDirectory -PathType Container)) {
        Stop-EcsNextTraceReportAssertion "capability evidence raw output directory does not exist: $capabilityRawDirectory"
    }
    Assert-EcsReportCapabilityEvidence -Evidence $capabilityEvidence `
        -HelperPath $capabilityHelperPath `
        -ExpectedEvidencePath $capabilityFullPath `
        -ExpectedRawDirectory $capabilityRawDirectory `
        -ExpectedNextTracePath $nextTraceFullPath `
        -ExpectedTarget $CapabilityTarget `
        -ExpectedHash $NextTraceSHA256 `
        -ExpectedMaxHops $CapabilityMaxHops
} catch {
    Stop-EcsNextTraceReportAssertion "capability evidence validation failed: $($_.Exception.Message)"
}

$capabilitySchema = [string](Get-EcsReportProperty -Object $capabilityEvidence -Name 'schema_version' -Context 'capability evidence')
$capabilityFamily = [string](Get-EcsReportProperty -Object $capabilityEvidence -Name 'family' -Context 'capability evidence')
$capabilityTarget = [string](Get-EcsReportProperty -Object $capabilityEvidence -Name 'target' -Context 'capability evidence')
$capabilityMaxHops = [int](Get-EcsReportProperty -Object $capabilityEvidence -Name 'max_hops' -Context 'capability evidence')
$capabilityNextTracePath = [string](Get-EcsReportProperty -Object $capabilityEvidence -Name 'nexttrace_path' -Context 'capability evidence')
$capabilityExpectedHash = [string](Get-EcsReportProperty -Object $capabilityEvidence -Name 'nexttrace_expected_sha256' -Context 'capability evidence')
$capabilityActualHash = [string](Get-EcsReportProperty -Object $capabilityEvidence -Name 'nexttrace_sha256' -Context 'capability evidence')
if ($capabilitySchema -cne $CapabilitySchemaVersion -or
    $capabilityFamily -cne $CapabilityFamilyName -or
    $capabilityTarget -cne $CapabilityTarget -or
    $capabilityMaxHops -ne $CapabilityMaxHops) {
    Stop-EcsNextTraceReportAssertion 'capability evidence schema/family/target/max-hops is not the canonical IPv4 contract'
}
if ($capabilityNextTracePath -cne $nextTraceFullPath) {
    Stop-EcsNextTraceReportAssertion 'capability evidence nexttrace_path does not match the staged executable path'
}
if ($capabilityExpectedHash -cne $NextTraceSHA256 -or $capabilityActualHash -cne $NextTraceSHA256) {
    Stop-EcsNextTraceReportAssertion "capability evidence SHA-256 values must equal the pinned NextTrace hash $NextTraceSHA256"
}

$capabilityDecision = [string](Get-EcsReportProperty -Object $capabilityEvidence -Name 'decision' -Context 'capability evidence')
$capabilityLiveNetworkNotProven = Get-EcsReportProperty -Object $capabilityEvidence -Name 'live_network_not_proven' -Context 'capability evidence'
if ($capabilityLiveNetworkNotProven -isnot [bool]) {
    Stop-EcsNextTraceReportAssertion 'capability evidence live_network_not_proven must be boolean'
}
if ($capabilityDecision -notin @('available', 'not-testable')) {
    Stop-EcsNextTraceReportAssertion "capability evidence has an invalid IPv4 decision: $capabilityDecision"
}
$expectedLiveNetworkNotProven = $capabilityDecision -eq 'not-testable'
if ([bool]$capabilityLiveNetworkNotProven -ne $expectedLiveNetworkNotProven) {
    Stop-EcsNextTraceReportAssertion 'capability evidence decision and live_network_not_proven are inconsistent'
}
if ($Family -cne '4' -or $FamilyName -cne $CapabilityFamilyName -or $Target -cne $CapabilityTarget) {
    Stop-EcsNextTraceReportAssertion 'IPv4 capability evidence cannot validate a non-canonical IPv4 report'
}

try {
    $report = Get-Content -Raw -LiteralPath $reportFullPath -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
} catch {
    Stop-EcsNextTraceReportAssertion "report is invalid JSON: $($_.Exception.Message)"
}
if ([string]$report.schema_version -cne 'ecs.report/v1') {
    Stop-EcsNextTraceReportAssertion 'report schema is not ecs.report/v1'
}

$results = @(Get-EcsReportProperty -Object $report -Name 'results' -Context 'report')
$resultIDs = @($results | ForEach-Object { [string]$_.id })
if ($resultIDs.Count -eq 0 -or $resultIDs.Count -ne @($resultIDs | Sort-Object -Unique).Count) {
    Stop-EcsNextTraceReportAssertion 'result IDs are missing or not unique'
}
$matchingResults = @($results | Where-Object { [string]$_.id -ceq $Module })
if ($matchingResults.Count -ne 1) {
    Stop-EcsNextTraceReportAssertion "report has no unique $Module result"
}
$result = $matchingResults[0]

$status = [string](Get-EcsReportProperty -Object $result -Name 'status' -Context "$Module result")
if ([string]::IsNullOrWhiteSpace($status) -or $status -notin @('ok', 'warning') -or $status -in @('unsupported', 'skipped', 'error')) {
    Stop-EcsNextTraceReportAssertion "$Module result has invalid status '$status'"
}

$evidence = Get-EcsReportProperty -Object $result -Name 'evidence' -Context "$Module result"
$valid = [int](Get-EcsReportProperty -Object $evidence -Name 'valid' -Context "$Module evidence")
$expected = [int](Get-EcsReportProperty -Object $evidence -Name 'expected' -Context "$Module evidence")
if ($valid -lt 1 -or $expected -ne 1 -or $valid -ne $expected) {
    Stop-EcsNextTraceReportAssertion "$Module evidence is not one valid expected target: valid=$valid expected=$expected"
}

$failureProperty = @($result.PSObject.Properties | Where-Object { $_.Name -ceq 'failures' })
$failures = if ($failureProperty.Count -eq 1) { @($failureProperty[0].Value) } else { @() }
$parserFailures = @($failures | Where-Object {
    [string]$_.category -in @('unsupported', 'tool_missing', 'parse_error') -or
    [string]$_.category -match '(?i)parse' -or
    [string]$_.stage -match '(?i)parse'
})
if ($parserFailures.Count -ne 0) {
    Stop-EcsNextTraceReportAssertion "$Module result contains a missing-tool, unsupported, or parser failure"
}

$engine = Get-EcsRawResultField -Result $result -Key 'engine'
$version = Get-EcsRawResultField -Result $result -Key 'version'
$arguments = Get-EcsRawResultField -Result $result -Key 'arguments'
$parameters = Get-EcsReportProperty -Object (Get-EcsReportProperty -Object $result -Name 'methodology' -Context "$Module result") -Name 'parameters' -Context "$Module methodology"
$adapter = [string](Get-EcsReportProperty -Object $parameters -Name 'adapter' -Context "$Module methodology parameters")
$parameterFamily = [string](Get-EcsReportProperty -Object $parameters -Name 'ip_version' -Context "$Module methodology parameters")
$parameterArguments = [string](Get-EcsReportProperty -Object $parameters -Name 'arguments' -Context "$Module methodology parameters")
$parameterTargets = [string](Get-EcsReportProperty -Object $parameters -Name 'targets' -Context "$Module methodology parameters")
$parameterMaxHops = [string](Get-EcsReportProperty -Object $parameters -Name 'max_hops' -Context "$Module methodology parameters")

if ($engine -cne $NextTraceEngine -or $version -notmatch '(^|[^0-9])1[.]7[.]1([^0-9]|$)') {
    Stop-EcsNextTraceReportAssertion "$Module tool provenance is not NextTrace Tiny v1.7.1: engine=$engine version=$version"
}
if ($adapter -cne $NextTraceAdapter -or $parameterFamily -cne $Family -or $parameterArguments -cne $arguments -or $parameterMaxHops -cne [string]$MaxHops) {
    Stop-EcsNextTraceReportAssertion "$Module canonical adapter/family/arguments/max-hops provenance is inconsistent"
}
$familyFlag = "-$Family"
$requiredArgumentPatterns = @(
    '(?<!\S)--no-color(?!\S)',
    '(?<!\S)--json(?!\S)',
    "(?<!\S)$([regex]::Escape($familyFlag))(?!\S)",
    '(?<!\S)-M(?!\S)',
    "(?<!\S)--max-hops\s+$MaxHops(?!\S)",
    '(?<!\S)--queries\s+1(?!\S)',
    '(?<!\S)--parallel-requests\s+1(?!\S)',
    '(?<!\S)--timeout\s+1000(?!\S)'
)
foreach ($pattern in $requiredArgumentPatterns) {
    if ($arguments -notmatch $pattern) {
        Stop-EcsNextTraceReportAssertion "$Module canonical arguments are missing required pattern $pattern"
    }
}
if ($parameterTargets -notlike "*$Target*") {
    Stop-EcsNextTraceReportAssertion "$Module target provenance is missing $Target"
}

$sources = @(Get-EcsReportProperty -Object $result -Name 'sources' -Context "$Module result")
$nextTraceSources = @($sources | Where-Object {
    [string]$_.name -ceq $NextTraceSourceName -and [string]$_.url -ceq $NextTraceSourceURL
})
if ($nextTraceSources.Count -ne 1) {
    Stop-EcsNextTraceReportAssertion "$Module result has no unique NextTrace source provenance"
}

$normalizedTitle = if ($Module -ceq 'route') { 'probe.route.normalized_trace_json' } else { 'probe.backtrace.normalized_trace_json' }
$blocks = @(Get-EcsReportProperty -Object $result -Name 'text_blocks' -Context "$Module result")
$normalizedBlocks = @($blocks | Where-Object { [string]$_.title -ceq $normalizedTitle -and [string]$_.language -ceq 'json' })
if ($normalizedBlocks.Count -ne 1) {
    Stop-EcsNextTraceReportAssertion "$Module result has no unique canonical normalized trace JSON"
}
try {
    $trace = ([string]$normalizedBlocks[0].content) | ConvertFrom-Json
} catch {
    Stop-EcsNextTraceReportAssertion "$Module canonical normalized trace JSON is invalid: $($_.Exception.Message)"
}
$hops = @(Get-EcsReportProperty -Object $trace -Name 'hops' -Context "$Module canonical trace")
$respondingHops = @($hops | Where-Object { Test-EcsActualRespondingHop -Hop $_ })
if ([string]$trace.engine -cne $NextTraceEngine -or
    [string]$trace.adapter -cne $NextTraceAdapter -or
    [string]$trace.family -cne $FamilyName -or
    [string]$trace.target -cne $Target) {
    Stop-EcsNextTraceReportAssertion "$Module canonical trace facts are incomplete: engine=$($trace.engine) adapter=$($trace.adapter) family=$($trace.family) target=$($trace.target) hops=$($hops.Count)"
}
$allowZeroRespondingHops = $capabilityDecision -eq 'not-testable' -and [bool]$capabilityLiveNetworkNotProven
if ($respondingHops.Count -lt 1 -and -not $allowZeroRespondingHops) {
    Stop-EcsNextTraceReportAssertion "$Module canonical trace has no actual responding hop (requires responded=true and non-empty ip)"
}
if ($allowZeroRespondingHops -and $respondingHops.Count -eq 0) {
    Write-Output ("NextTrace $Module report assertion accepted zero hops only from validated capability evidence: capability_decision=not-testable; live_network_not_proven=true; live network observation not proven; all other canonical assertions remained required")
}

$capabilityObservation = if ([bool]$capabilityLiveNetworkNotProven) { 'live network observation not proven' } else { 'live network observation proven' }
Write-Output ("bootstrap $Module report assertion passed: schema=ecs.report/v1; status={0}; engine={1}; version={2}; adapter={3}; family={4}; target={5}; hops={6}; responding_hops={7}; capability_decision={8}; live_network_not_proven={9}; {10}" -f
    $status, $engine, $version, $adapter, $FamilyName, $Target, $hops.Count, $respondingHops.Count, $capabilityDecision, $capabilityLiveNetworkNotProven, $capabilityObservation)
