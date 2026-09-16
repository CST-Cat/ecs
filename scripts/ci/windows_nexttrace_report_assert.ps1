[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$ReportPath,
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

function Stop-EcsNextTraceReportAssertion {
    param([Parameter(Mandatory)][string]$Message)
    throw "windows-nexttrace-report-assert: $Message"
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

if (-not [IO.Path]::IsPathRooted($ReportPath)) {
    Stop-EcsNextTraceReportAssertion 'report path must be absolute'
}
$reportFile = Get-Item -LiteralPath ([IO.Path]::GetFullPath($ReportPath)) -ErrorAction Stop
if ($reportFile.PSIsContainer) {
    Stop-EcsNextTraceReportAssertion "report path is a directory: $($reportFile.FullName)"
}

try {
    $report = Get-Content -Raw -LiteralPath $reportFile.FullName | ConvertFrom-Json
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
if ($respondingHops.Count -lt 1) {
    Stop-EcsNextTraceReportAssertion "$Module canonical trace has no actual responding hop (requires responded=true and non-empty ip)"
}

Write-Output ("bootstrap $Module report assertion passed: schema=ecs.report/v1; status={0}; engine={1}; version={2}; adapter={3}; family={4}; target={5}; hops={6}; responding_hops={7}" -f
    $status, $engine, $version, $adapter, $FamilyName, $Target, $hops.Count, $respondingHops.Count)
