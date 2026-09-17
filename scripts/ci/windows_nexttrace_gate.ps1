[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$EcsPath,
    [Parameter(Mandatory)][string]$ToolBin,
    [Parameter(Mandatory)][string]$OutputRoot,
    [Parameter(Mandatory)][string]$CapabilityPath,
    [string]$Label = 'windows'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$NextTraceSHA256 = '16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b'
$NextTraceAdapter = 'nexttrace-json-v1'
$NextTraceEngine = 'nexttrace-tiny'
$CapabilitySchemaVersion = 'ecs.windows.nexttrace.capability/v1'
$CapabilityFamilyName = 'ipv4'
$CapabilityTarget = '1.1.1.1'
$CapabilityMaxHops = 12

function Stop-EcsWindowsNextTraceGate {
    param([Parameter(Mandatory)][string]$Message)
    throw "windows-nexttrace-gate: $Message"
}

function Get-EcsRequiredPath {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Description
    )
    if (-not [IO.Path]::IsPathRooted($Path)) {
        Stop-EcsWindowsNextTraceGate "$Description must be an absolute path"
    }
    $full = [IO.Path]::GetFullPath($Path)
    if (-not ((Test-Path -LiteralPath $full -PathType Leaf) -or (Test-Path -LiteralPath $full -PathType Container))) {
        Stop-EcsWindowsNextTraceGate "$Description does not exist: $full"
    }
    return $full
}

function Get-EcsRequiredJsonFile {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Description
    )
    if (-not [IO.Path]::IsPathRooted($Path)) {
        Stop-EcsWindowsNextTraceGate "$Description must be an absolute path"
    }
    try {
        $full = [IO.Path]::GetFullPath($Path)
    } catch {
        Stop-EcsWindowsNextTraceGate "$Description is not a valid path: $($_.Exception.Message)"
    }
    if (-not (Test-Path -LiteralPath $full -PathType Leaf)) {
        Stop-EcsWindowsNextTraceGate "$Description does not name an existing JSON file: $full"
    }
    try {
        $json = Get-Content -Raw -LiteralPath $full -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
    } catch {
        Stop-EcsWindowsNextTraceGate "$Description is not valid JSON: $($_.Exception.Message)"
    }
    return [pscustomobject]@{
        Path = $full
        Json = $json
    }
}

function Get-EcsPropertyValue {
    param(
        [Parameter(Mandatory)][object]$Object,
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$Context
    )
    if ($null -eq $Object) {
        Stop-EcsWindowsNextTraceGate "$Context is null while reading '$Name'"
    }
    $matches = @($Object.PSObject.Properties | Where-Object { $_.Name -ceq $Name })
    if ($matches.Count -ne 1) {
        Stop-EcsWindowsNextTraceGate "$Context has no unique '$Name' property"
    }
    return $matches[0].Value
}

function Get-EcsRawField {
    param(
        [Parameter(Mandatory)][object]$Result,
        [Parameter(Mandatory)][string]$Key
    )
    $fields = @(Get-EcsPropertyValue -Object $Result -Name 'fields' -Context "result $($Result.id)")
    $matches = @($fields | Where-Object { [string]$_.key -ceq $Key })
    if ($matches.Count -ne 1) {
        Stop-EcsWindowsNextTraceGate "result $($Result.id) has no unique field '$Key'"
    }
    $value = Get-EcsPropertyValue -Object $matches[0] -Name 'value' -Context "result $($Result.id) field $Key"
    $raw = Get-EcsPropertyValue -Object $value -Name 'raw' -Context "result $($Result.id) field $Key value"
    if ([string]::IsNullOrWhiteSpace([string]$raw)) {
        Stop-EcsWindowsNextTraceGate "result $($Result.id) field $Key is empty"
    }
    return [string]$raw
}

function Convert-EcsJsonOutput {
    param(
        [Parameter(Mandatory)][object[]]$Lines,
        [Parameter(Mandatory)][string]$Description
    )
    $text = (($Lines | ForEach-Object { [string]$_ }) -join "`n").Trim()
    if ([string]::IsNullOrWhiteSpace($text)) {
        Stop-EcsWindowsNextTraceGate "$Description returned no JSON"
    }
    try {
        return $text | ConvertFrom-Json
    } catch {
        Stop-EcsWindowsNextTraceGate "$Description returned invalid JSON: $($_.Exception.Message)"
    }
}

function Get-EcsSingleReport {
    param([Parameter(Mandatory)][string]$Root)
    $items = @(Get-ChildItem -LiteralPath $Root -Filter '*.json' -File -Recurse)
    if ($items.Count -ne 1) {
        Stop-EcsWindowsNextTraceGate "expected one JSON report below $Root, found $($items.Count)"
    }
    try {
        return Get-Content -Raw -LiteralPath $items[0].FullName | ConvertFrom-Json
    } catch {
        Stop-EcsWindowsNextTraceGate "report $($items[0].FullName) is invalid JSON: $($_.Exception.Message)"
    }
}

function Get-EcsSingleResult {
    param(
        [Parameter(Mandatory)][object]$Report,
        [Parameter(Mandatory)][string]$ID
    )
    if ([string]$Report.schema_version -cne 'ecs.report/v1') {
        Stop-EcsWindowsNextTraceGate "report schema is not ecs.report/v1"
    }
    $results = @(Get-EcsPropertyValue -Object $Report -Name 'results' -Context 'report')
    $matches = @($results | Where-Object { [string]$_.id -ceq $ID })
    if ($matches.Count -ne 1) {
        Stop-EcsWindowsNextTraceGate "report has no unique $ID result"
    }
    return $matches[0]
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

function New-EcsReportRoot {
    param(
        [Parameter(Mandatory)][string]$Prefix,
        [Parameter(Mandatory)][string]$Root
    )
    $path = Join-Path $Root ("$Prefix-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path $path | Out-Null
    return $path
}

function Assert-EcsPlan {
    param(
        [Parameter(Mandatory)][object]$Plan,
        [Parameter(Mandatory)][string]$Module,
        [Parameter(Mandatory)][string]$Family,
        [Parameter(Mandatory)][string]$Target
    )
    if ([string]$Plan.schema_version -cne 'ecs.plan/v1') {
        Stop-EcsWindowsNextTraceGate "$Module plan schema is not ecs.plan/v1"
    }
    $modules = @(Get-EcsPropertyValue -Object $Plan -Name 'modules' -Context "$Module plan")
    if ($modules.Count -ne 1 -or [string]$modules[0].id -cne $Module) {
        Stop-EcsWindowsNextTraceGate "$Module plan selected modules are not exactly [$Module]"
    }
    $requiredTools = @(Get-EcsPropertyValue -Object $Plan -Name 'required_tools' -Context "$Module plan")
    if ($requiredTools.Count -ne 1 -or [string]$requiredTools[0] -cne $NextTraceEngine) {
        Stop-EcsWindowsNextTraceGate "$Module plan did not resolve exactly staged $NextTraceEngine"
    }
    if ([string]$Plan.ip_version -cne $Family) {
        Stop-EcsWindowsNextTraceGate "$Module plan family is $($Plan.ip_version), want $Family"
    }
    if ([string]$Plan.exposure -cne 'public') {
        Stop-EcsWindowsNextTraceGate "$Module plan exposure is not public"
    }
}

function Assert-EcsCanonicalTraceReport {
    param(
        [Parameter(Mandatory)][object]$Report,
        [Parameter(Mandatory)][string]$Module,
        [Parameter(Mandatory)][string]$Family,
        [Parameter(Mandatory)][string]$FamilyName,
        [Parameter(Mandatory)][int]$MaxHops,
        [Parameter(Mandatory)][string]$Target,
        [ValidateSet('none', 'available', 'not-testable')][string]$CapabilityDecision = 'none',
        [bool]$CapabilityLiveNetworkNotProven = $false
    )
    if ($CapabilityDecision -eq 'none' -and $CapabilityLiveNetworkNotProven) {
        Stop-EcsWindowsNextTraceGate "$Module cannot mark live network observation unproven without a capability decision"
    }
    if ($CapabilityDecision -eq 'not-testable' -and -not $CapabilityLiveNetworkNotProven) {
        Stop-EcsWindowsNextTraceGate "$Module cannot use not-testable capability evidence unless live network observation is explicitly unproven"
    }
    if ($CapabilityDecision -eq 'available' -and $CapabilityLiveNetworkNotProven) {
        Stop-EcsWindowsNextTraceGate "$Module available capability evidence cannot claim live network observation is unproven"
    }
    $result = Get-EcsSingleResult -Report $Report -ID $Module
    if ([string]$result.status -notin @('ok', 'warning') -or [string]$result.status -in @('unsupported', 'skipped', 'error')) {
        Stop-EcsWindowsNextTraceGate "$Module result has invalid gate status $($result.status)"
    }
    $evidence = Get-EcsPropertyValue -Object $result -Name 'evidence' -Context "$Module result"
    if ([int]$evidence.valid -lt 1 -or [int]$evidence.expected -ne 1 -or [int]$evidence.valid -ne [int]$evidence.expected) {
        Stop-EcsWindowsNextTraceGate "$Module evidence is not one parsed target: valid=$($evidence.valid) expected=$($evidence.expected)"
    }
    $failureProperty = @($result.PSObject.Properties | Where-Object { $_.Name -ceq 'failures' })
    $failures = if ($failureProperty.Count -eq 1) { @($failureProperty[0].Value) } else { @() }
    $unsupportedFailures = @($failures | Where-Object { [string]$_.category -in @('unsupported', 'tool_missing', 'parse_error') })
    if ($unsupportedFailures.Count -ne 0) {
        Stop-EcsWindowsNextTraceGate "$Module result contains unsupported/tool/parse failures"
    }

    $engine = Get-EcsRawField -Result $result -Key 'engine'
    $version = Get-EcsRawField -Result $result -Key 'version'
    $arguments = Get-EcsRawField -Result $result -Key 'arguments'
    $adapter = [string](Get-EcsPropertyValue -Object $result.methodology.parameters -Name 'adapter' -Context "$Module methodology parameters")
    $parameterFamily = [string](Get-EcsPropertyValue -Object $result.methodology.parameters -Name 'ip_version' -Context "$Module methodology parameters")
    $parameterArguments = [string](Get-EcsPropertyValue -Object $result.methodology.parameters -Name 'arguments' -Context "$Module methodology parameters")
    $parameterTargets = [string](Get-EcsPropertyValue -Object $result.methodology.parameters -Name 'targets' -Context "$Module methodology parameters")
    if ($engine -cne $NextTraceEngine -or $version -notmatch '(^|[^0-9])1[.]7[.]1([^0-9]|$)') {
        Stop-EcsWindowsNextTraceGate "$Module tool provenance is not NextTrace Tiny v1.7.1: engine=$engine version=$version"
    }
    if ($adapter -cne $NextTraceAdapter -or $parameterFamily -cne $Family -or $parameterArguments -cne $arguments) {
        Stop-EcsWindowsNextTraceGate "$Module canonical adapter/family/arguments provenance is inconsistent"
    }
    if ($arguments -notmatch '(?<!\S)--no-color(?!\S)' -or $arguments -notmatch '(?<!\S)--json(?!\S)' -or
        $arguments -notmatch '(?<!\S)-M(?!\S)' -or $arguments -notmatch "(?<!\S)--max-hops\s+$MaxHops(?!\S)" -or
        $arguments -notmatch '(?<!\S)--queries\s+1(?!\S)' -or $arguments -notmatch '(?<!\S)--parallel-requests\s+1(?!\S)' -or
        $arguments -notmatch '(?<!\S)--timeout\s+1000(?!\S)') {
        Stop-EcsWindowsNextTraceGate "$Module arguments do not prove canonical JSON/no-color/family/max-hops/query/timeout mode"
    }
    $familyFlag = "-$Family"
    if ($arguments -notmatch "(?<!\S)$([regex]::Escape($familyFlag))(?!\S)" -or $parameterTargets -notlike "*$Target*") {
        Stop-EcsWindowsNextTraceGate "$Module arguments or target provenance is missing family=$Family target=$Target"
    }
    $sources = @(Get-EcsPropertyValue -Object $result -Name 'sources' -Context "$Module result")
    $nextTraceSources = @($sources | Where-Object {
        [string]$_.name -ceq 'probe.route.source.nexttrace.name' -and
        [string]$_.url -ceq 'https://github.com/nxtrace/NTrace-core'
    })
    if ($nextTraceSources.Count -ne 1) {
        Stop-EcsWindowsNextTraceGate "$Module result has no unique NextTrace source provenance"
    }
    $normalizedTitle = if ($Module -ceq 'route') { 'probe.route.normalized_trace_json' } else { 'probe.backtrace.normalized_trace_json' }
    $blocks = @(Get-EcsPropertyValue -Object $result -Name 'text_blocks' -Context "$Module result")
    $normalizedBlocks = @($blocks | Where-Object { [string]$_.title -ceq $normalizedTitle -and [string]$_.language -ceq 'json' })
    if ($normalizedBlocks.Count -ne 1) {
        Stop-EcsWindowsNextTraceGate "$Module result has no unique canonical normalized trace JSON"
    }
    try {
        $trace = ([string]$normalizedBlocks[0].content) | ConvertFrom-Json
    } catch {
        Stop-EcsWindowsNextTraceGate "$Module canonical normalized trace JSON is invalid: $($_.Exception.Message)"
    }
    $hops = @(Get-EcsPropertyValue -Object $trace -Name 'hops' -Context "$Module canonical trace")
    $respondingHops = @($hops | Where-Object { Test-EcsActualRespondingHop -Hop $_ })
    if ([string]$trace.engine -cne $NextTraceEngine -or
        [string]$trace.adapter -cne $NextTraceAdapter -or
        [string]$trace.family -cne $FamilyName -or
        [string]$trace.target -cne $Target) {
        Stop-EcsWindowsNextTraceGate "$Module canonical trace facts are incomplete: engine=$($trace.engine) adapter=$($trace.adapter) family=$($trace.family) target=$($trace.target) hops=$($hops.Count)"
    }
    $allowZeroRespondingHops = $CapabilityDecision -eq 'not-testable' -and $CapabilityLiveNetworkNotProven
    if ($respondingHops.Count -lt 1 -and -not $allowZeroRespondingHops) {
        Stop-EcsWindowsNextTraceGate "$Module canonical trace has no actual responding hop (requires responded=true and non-empty ip)"
    }
    if ($allowZeroRespondingHops -and $respondingHops.Count -eq 0) {
        Write-Output ("NextTrace $Module responding-hop assertion accepted zero hops only from validated capability evidence: decision=not-testable; live_network_not_proven=true; live network observation not proven; all other canonical assertions remained required")
    }
    $reportedDecision = if ($CapabilityDecision -eq 'none') { 'not-applied' } else { $CapabilityDecision }
    Write-Output ("NextTrace $Module gate passed: engine={0}; version={1}; adapter={2}; family={3}; target={4}; hops={5}; responding_hops={6}; status={7}; capability_decision={8}; live_network_not_proven={9}" -f
        $engine, $version, $adapter, $FamilyName, $Target, $hops.Count, $respondingHops.Count, $result.status, $reportedDecision, $CapabilityLiveNetworkNotProven)
}

function Get-EcsValidatedCapabilityEvidence {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$ExpectedNextTracePath,
        [Parameter(Mandatory)][string]$ExpectedHash
    )
    $file = Get-EcsRequiredJsonFile -Path $Path -Description 'capability evidence JSON'
    $evidence = $file.Json

    Assert-EcsCapabilityEvidence -Evidence $evidence `
        -ExpectedEvidencePath $file.Path `
        -ExpectedRawDirectory ([string](Get-EcsPropertyValue -Object $evidence -Name 'raw_output_directory' -Context 'capability evidence')) `
        -ExpectedNextTracePath $ExpectedNextTracePath

    if ([string](Get-EcsPropertyValue -Object $evidence -Name 'schema_version' -Context 'capability evidence') -cne $CapabilitySchemaVersion) {
        Stop-EcsWindowsNextTraceGate "capability evidence schema must be $CapabilitySchemaVersion"
    }
    if ([string](Get-EcsPropertyValue -Object $evidence -Name 'family' -Context 'capability evidence') -cne $CapabilityFamilyName) {
        Stop-EcsWindowsNextTraceGate "capability evidence family must be $CapabilityFamilyName"
    }
    if ([string](Get-EcsPropertyValue -Object $evidence -Name 'target' -Context 'capability evidence') -cne $CapabilityTarget) {
        Stop-EcsWindowsNextTraceGate "capability evidence target must be $CapabilityTarget"
    }
    if ([int](Get-EcsPropertyValue -Object $evidence -Name 'max_hops' -Context 'capability evidence') -ne $CapabilityMaxHops) {
        Stop-EcsWindowsNextTraceGate "capability evidence max_hops must remain $CapabilityMaxHops"
    }
    if ([string](Get-EcsPropertyValue -Object $evidence -Name 'nexttrace_path' -Context 'capability evidence') -cne $ExpectedNextTracePath) {
        Stop-EcsWindowsNextTraceGate 'capability evidence nexttrace_path does not match this gate staged executable'
    }
    $evidenceExpectedHash = [string](Get-EcsPropertyValue -Object $evidence -Name 'nexttrace_expected_sha256' -Context 'capability evidence')
    $evidenceActualHash = [string](Get-EcsPropertyValue -Object $evidence -Name 'nexttrace_sha256' -Context 'capability evidence')
    if ($evidenceExpectedHash -cne $ExpectedHash -or $evidenceActualHash -cne $ExpectedHash) {
        Stop-EcsWindowsNextTraceGate "capability evidence SHA-256 values must equal the hard-coded staged hash $ExpectedHash"
    }
    $rawDirectory = [string](Get-EcsPropertyValue -Object $evidence -Name 'raw_output_directory' -Context 'capability evidence')
    if (-not [IO.Path]::IsPathRooted($rawDirectory) -or -not (Test-Path -LiteralPath $rawDirectory -PathType Container)) {
        Stop-EcsWindowsNextTraceGate "capability evidence raw output directory does not exist: $rawDirectory"
    }
    $decision = [string](Get-EcsPropertyValue -Object $evidence -Name 'decision' -Context 'capability evidence')
    $liveNetworkNotProven = Get-EcsPropertyValue -Object $evidence -Name 'live_network_not_proven' -Context 'capability evidence'
    return [pscustomobject]@{
        Decision = $decision
        LiveNetworkNotProven = [bool]$liveNetworkNotProven
        Reason = [string](Get-EcsPropertyValue -Object $evidence -Name 'reason' -Context 'capability evidence')
    }
}

function Test-EcsGlobalIPv6Capability {
    try {
        $addresses = @(
            Get-NetIPAddress -AddressFamily IPv6 -AddressState Preferred -ErrorAction Stop |
                Where-Object {
                    $address = [string]$_.IPAddress
                    $address -notmatch '^(?i:fe80:|fc|fd|::1$|::$)'
                }
        )
        $routes = @(
            Get-NetRoute -AddressFamily IPv6 -DestinationPrefix '::/0' -ErrorAction Stop |
                Where-Object { [string]$_.State -notin @('Dead', 'Invalid', 'Unreachable') }
        )
    } catch {
        Write-Host "NextTrace IPv6 gate: not-tested capability=missing; IPv4 capability evidence is not applied to IPv6; reason=$($_.Exception.Message)"
        return $false
    }
    if ($addresses.Count -eq 0 -or $routes.Count -eq 0) {
        Write-Host ("NextTrace IPv6 gate: not-tested capability=missing; IPv4 capability evidence is not applied to IPv6; global_addresses={0} default_routes={1}" -f $addresses.Count, $routes.Count)
        return $false
    }
    Write-Host ("NextTrace IPv6 capability detected: global_addresses={0}; default_routes={1}; running canonical IPv6 gates with strict actual responding-hop requirement; IPv4 capability evidence is not applied to IPv6" -f $addresses.Count, $routes.Count)
    return $true
}

$ecs = Get-EcsRequiredPath -Path $EcsPath -Description 'ecs.exe'
$bin = Get-EcsRequiredPath -Path $ToolBin -Description 'staged tool directory'
$outputItem = New-Item -ItemType Directory -Force -Path $OutputRoot
$output = Get-EcsRequiredPath -Path $outputItem.FullName -Description 'gate output directory'
$capabilityFile = Get-EcsRequiredJsonFile -Path $CapabilityPath -Description 'capability evidence JSON'
$nexttrace = [IO.Path]::GetFullPath((Join-Path $bin 'nexttrace-tiny.exe'))
if (-not (Test-Path -LiteralPath $nexttrace -PathType Leaf)) {
    Stop-EcsWindowsNextTraceGate "staged resolver is missing $nexttrace"
}
$nexttraceHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $nexttrace).Hash.ToLowerInvariant()
if ($nexttraceHash -cne $NextTraceSHA256) {
    Stop-EcsWindowsNextTraceGate "staged NextTrace SHA-256 mismatch: got $nexttraceHash"
}
$capabilityHelper = Get-EcsRequiredPath -Path (Join-Path $PSScriptRoot 'windows_nexttrace_capability.ps1') -Description 'capability validator helper'
. $capabilityHelper -Family IPv4 -Target $CapabilityTarget -NextTracePath $nexttrace -ExpectedSha256 $NextTraceSHA256 -EvidencePath $capabilityFile.Path -MaxHops $CapabilityMaxHops

$hadToolBin = Test-Path Env:ECS_TOOL_BIN
$oldToolBin = $env:ECS_TOOL_BIN
$oldPath = $env:PATH
$hadNoColor = Test-Path Env:NO_COLOR
$oldNoColor = $env:NO_COLOR
try {
    # The production resolver must see this staged directory, while the host
    # PATH is intentionally reduced so a missing staged binary cannot fall back
    # to a same-named executable.
    $env:ECS_TOOL_BIN = $bin
    $env:PATH = "$env:SystemRoot\System32;$env:SystemRoot"
    $env:NO_COLOR = '1'

    $routeTarget4 = '1.1.1.1'
    $backtraceTarget4 = '1.1.1.1'
    $ipv4Capability = Get-EcsValidatedCapabilityEvidence -Path $capabilityFile.Path -ExpectedNextTracePath $nexttrace -ExpectedHash $NextTraceSHA256
    Write-Output ("NextTrace IPv4 capability evidence validated: schema={0}; family={1}; target={2}; max_hops={3}; decision={4}; live_network_not_proven={5}; applies_to=route,backtrace; reason={6}" -f
        $CapabilitySchemaVersion, $CapabilityFamilyName, $CapabilityTarget, $CapabilityMaxHops, $ipv4Capability.Decision, $ipv4Capability.LiveNetworkNotProven, $ipv4Capability.Reason)
    $routePlanLines = @(& $ecs plan --lang en --only route --exposure public --ip-version 4 --route-targets "gate=$routeTarget4" 2>&1)
    if ($LASTEXITCODE -ne 0) { Stop-EcsWindowsNextTraceGate 'ecs.exe plan --only route failed' }
    $routePlan = Convert-EcsJsonOutput -Lines $routePlanLines -Description 'ecs.exe plan --only route'
    Assert-EcsPlan -Plan $routePlan -Module 'route' -Family '4' -Target $routeTarget4

    $backtracePlanLines = @(& $ecs plan --lang en --only backtrace --exposure public --ip-version 4 --backtrace-targets "telecom:gate=$backtraceTarget4" 2>&1)
    if ($LASTEXITCODE -ne 0) { Stop-EcsWindowsNextTraceGate 'ecs.exe plan --only backtrace failed' }
    $backtracePlan = Convert-EcsJsonOutput -Lines $backtracePlanLines -Description 'ecs.exe plan --only backtrace'
    Assert-EcsPlan -Plan $backtracePlan -Module 'backtrace' -Family '4' -Target $backtraceTarget4

    $routeOutput4 = New-EcsReportRoot -Prefix 'route-ipv4' -Root $output
    $routeRunLines = @(& $ecs run --lang en --only route --format json --exposure public --ip-version 4 --route-targets "gate=$routeTarget4" --yes --output $routeOutput4 --no-color 2>&1)
    if ($LASTEXITCODE -ne 0) { Stop-EcsWindowsNextTraceGate 'ecs.exe run --only route --format json failed' }
    $routeReport = Get-EcsSingleReport -Root $routeOutput4
    Assert-EcsCanonicalTraceReport -Report $routeReport -Module 'route' -Family '4' -FamilyName 'ipv4' -MaxHops 12 -Target $routeTarget4 `
        -CapabilityDecision $ipv4Capability.Decision -CapabilityLiveNetworkNotProven $ipv4Capability.LiveNetworkNotProven

    $backtraceOutput4 = New-EcsReportRoot -Prefix 'backtrace-ipv4' -Root $output
    $backtraceRunLines = @(& $ecs run --lang en --only backtrace --format json --exposure public --ip-version 4 --backtrace-targets "telecom:gate=$backtraceTarget4" --yes --output $backtraceOutput4 --no-color 2>&1)
    if ($LASTEXITCODE -ne 0) { Stop-EcsWindowsNextTraceGate 'ecs.exe run --only backtrace --format json failed' }
    $backtraceReport = Get-EcsSingleReport -Root $backtraceOutput4
    Assert-EcsCanonicalTraceReport -Report $backtraceReport -Module 'backtrace' -Family '4' -FamilyName 'ipv4' -MaxHops 20 -Target $backtraceTarget4 `
        -CapabilityDecision $ipv4Capability.Decision -CapabilityLiveNetworkNotProven $ipv4Capability.LiveNetworkNotProven
    Write-Output "NextTrace IPv4 canonical gate passed: label=$Label; staged_sha256=$nexttraceHash"

    if (Test-EcsGlobalIPv6Capability) {
        $routeTarget6 = '2606:4700:4700::1111'
        $backtraceTarget6 = '2606:4700:4700::1111'
        $routeOutput6 = New-EcsReportRoot -Prefix 'route-ipv6' -Root $output
        $routeRun6 = @(& $ecs run --lang en --only route --format json --exposure public --ip-version 6 --route-targets "gate=$routeTarget6" --yes --output $routeOutput6 --no-color 2>&1)
        if ($LASTEXITCODE -ne 0) { Stop-EcsWindowsNextTraceGate 'ecs.exe IPv6 route gate failed' }
        $routeReport6 = Get-EcsSingleReport -Root $routeOutput6
        Assert-EcsCanonicalTraceReport -Report $routeReport6 -Module 'route' -Family '6' -FamilyName 'ipv6' -MaxHops 12 -Target $routeTarget6

        $backtraceOutput6 = New-EcsReportRoot -Prefix 'backtrace-ipv6' -Root $output
        $backtraceRun6 = @(& $ecs run --lang en --only backtrace --format json --exposure public --ip-version 6 --backtrace-targets "telecom:gate=$backtraceTarget6" --yes --output $backtraceOutput6 --no-color 2>&1)
        if ($LASTEXITCODE -ne 0) { Stop-EcsWindowsNextTraceGate 'ecs.exe IPv6 backtrace gate failed' }
        $backtraceReport6 = Get-EcsSingleReport -Root $backtraceOutput6
        Assert-EcsCanonicalTraceReport -Report $backtraceReport6 -Module 'backtrace' -Family '6' -FamilyName 'ipv6' -MaxHops 20 -Target $backtraceTarget6
        Write-Output "NextTrace IPv6 canonical gate passed: label=$Label; staged_sha256=$nexttraceHash"
    }
} catch {
    $failure = $_
    try {
        Write-Host ("NextTrace $Label gate failed; emitting diagnostic evidence from $output")
        $evidenceFiles = @(Get-ChildItem -LiteralPath $output -File -Recurse -ErrorAction Stop | Sort-Object FullName)
        foreach ($evidenceFile in $evidenceFiles) {
            Write-Host ("--- {0} ---" -f $evidenceFile.FullName)
            Get-Content -Raw -LiteralPath $evidenceFile.FullName -ErrorAction Stop | Write-Host
        }
    } catch {
        Write-Host ("NextTrace diagnostic evidence emission failed: {0}" -f $_.Exception.Message)
    }
    throw $failure
} finally {
    $env:PATH = $oldPath
    if ($hadToolBin) {
        $env:ECS_TOOL_BIN = $oldToolBin
    } elseif (Test-Path Env:ECS_TOOL_BIN) {
        Remove-Item Env:ECS_TOOL_BIN -ErrorAction Stop
    }
    if ($hadNoColor) {
        $env:NO_COLOR = $oldNoColor
    } elseif (Test-Path Env:NO_COLOR) {
        Remove-Item Env:NO_COLOR -ErrorAction Stop
    }
}
