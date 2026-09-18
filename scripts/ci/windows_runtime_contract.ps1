[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$EcsPath,
    [ValidateNotNullOrEmpty()][string]$Label = 'windows-runtime',
    [string]$TempRoot = [string]$env:RUNNER_TEMP,
    [switch]$RunNativeTests
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not (Test-Path -LiteralPath $EcsPath -PathType Leaf)) {
    throw "Windows runtime contract cannot find ecs.exe: $EcsPath"
}
if ([string]::IsNullOrWhiteSpace($TempRoot) -or -not (Test-Path -LiteralPath $TempRoot -PathType Container)) {
    throw "Windows runtime contract temp root is missing: $TempRoot"
}

function Invoke-Ecs {
  param([Parameter(Mandatory, Position = 0)][string[]]$Arguments)
  $output = @(& $EcsPath @Arguments 2>&1)
  if ($LASTEXITCODE -ne 0) {
    $details = $output -join "`n"
    throw "ecs.exe $($Arguments -join ' ') failed with exit code $LASTEXITCODE`n$details"
  }
  return $output
}

function Get-JsonPropertyValue {
  param(
    [Parameter(Mandatory)][object]$Object,
    [Parameter(Mandatory)][string]$Name,
    [Parameter(Mandatory)][string]$Context
  )
  if ($null -eq $Object) { throw "$Context JSON object is null while reading '$Name'" }
  $objects = @($Object)
  if ($objects.Count -ne 1) { throw "$Context expected one JSON object while reading '$Name', got $($objects.Count)" }
  $properties = @($objects[0].PSObject.Properties | Where-Object { $_.Name -ieq $Name })
  if ($properties.Count -ne 1) {
    $available = @($objects[0].PSObject.Properties | ForEach-Object { $_.Name }) -join ', '
    throw "$Context missing JSON property '$Name'; available properties: $available"
  }
  if ($null -eq $properties[0].Value) { throw "$Context JSON property '$Name' is null" }
  return $properties[0].Value
}

function Get-SystemFieldRaw {
  param(
    [Parameter(Mandatory)][object]$Result,
    [Parameter(Mandatory)][string]$Key
  )
  $fields = @(Get-JsonPropertyValue -Object $Result -Name 'fields' -Context 'system result')
  $fieldMatches = @($fields | Where-Object { [string]$_.key -eq $Key })
  if ($fieldMatches.Count -ne 1) { throw "system JSON must contain one field named $Key" }
  $value = Get-JsonPropertyValue -Object $fieldMatches[0] -Name 'value' -Context "system JSON field $Key"
  $raw = [string](Get-JsonPropertyValue -Object $value -Name 'raw' -Context "system JSON field $Key value")
  if ([string]::IsNullOrWhiteSpace($raw)) { throw "system JSON field $Key is empty" }
  return $raw
}

function Convert-PositiveMachineBytes {
  param(
    [Parameter(Mandatory)][string]$Raw,
    [Parameter(Mandatory)][string]$Name
  )
  $match = [regex]::Match($Raw, '^\s*(?<number>[0-9]+(?:\.[0-9]+)?)\s*(?<unit>B|KiB|MiB|GiB|TiB|PiB)\s*$')
  if (-not $match.Success) { throw "$Name is not a parseable byte value: $Raw" }
  $number = [double]::Parse($match.Groups['number'].Value, [Globalization.CultureInfo]::InvariantCulture)
  $exponent = switch ($match.Groups['unit'].Value) {
    'B' { 0 }
    'KiB' { 1 }
    'MiB' { 2 }
    'GiB' { 3 }
    'TiB' { 4 }
    'PiB' { 5 }
    default { throw "$Name has an unknown byte unit: $Raw" }
  }
  $bytes = $number * [math]::Pow(1024, $exponent)
  if ([double]::IsNaN($bytes) -or [double]::IsInfinity($bytes) -or $bytes -le 0) {
    throw "$Name is not positive: $Raw"
  }
  return $bytes
}

function Convert-PositiveMachineSeconds {
  param(
    [Parameter(Mandatory)][string]$Raw,
    [Parameter(Mandatory)][string]$Name
  )
  $match = [regex]::Match($Raw, '^\s*(?<seconds>[0-9]+(?:\.[0-9]+)?)\s*$')
  if (-not $match.Success) { throw "$Name is not a parseable seconds value: $Raw" }
  $seconds = [double]::Parse($match.Groups['seconds'].Value, [Globalization.CultureInfo]::InvariantCulture)
  if ([double]::IsNaN($seconds) -or [double]::IsInfinity($seconds) -or $seconds -le 0) {
    throw "$Name is not positive: $Raw"
  }
  return $seconds
}

function Assert-WindowsSystemFacts {
  param([Parameter(Mandatory)][object]$Report)
  $schemaVersion = [string](Get-JsonPropertyValue -Object $Report -Name 'schema_version' -Context 'system report')
  if ($schemaVersion -ne 'ecs.report/v1') { throw "system report has unexpected schema_version: $schemaVersion" }
  $results = @(Get-JsonPropertyValue -Object $Report -Name 'results' -Context 'system report')
  $systemMatches = @($results | Where-Object { [string]$_.id -eq 'system' })
  if ($systemMatches.Count -ne 1) { throw 'system JSON has no unique system result' }
  $system = $systemMatches[0]
  $fieldsProperty = @($system.PSObject.Properties | Where-Object { $_.Name -ieq 'fields' })
  if ($fieldsProperty.Count -ne 1) {
    $status = [string](Get-JsonPropertyValue -Object $system -Name 'status' -Context 'system result')
    $evidence = Get-JsonPropertyValue -Object $system -Name 'evidence' -Context 'system result'
    $evidenceValid = Get-JsonPropertyValue -Object $evidence -Name 'valid' -Context 'system evidence'
    $evidenceExpected = Get-JsonPropertyValue -Object $evidence -Name 'expected' -Context 'system evidence'
    $evidenceUnit = [string](Get-JsonPropertyValue -Object $evidence -Name 'unit' -Context 'system evidence')
    $failureProperty = @($system.PSObject.Properties | Where-Object { $_.Name -ieq 'failures' })
    $failureText = '<missing>'
    if ($failureProperty.Count -eq 1) {
      $failureItems = @($failureProperty[0].Value)
      if ($failureItems.Count -eq 0) {
        $failureText = '<empty>'
      } else {
        $failureText = @($failureItems | ForEach-Object {
          [string](Get-JsonPropertyValue -Object $_ -Name 'message' -Context 'system failure')
        }) -join ' | '
      }
    }
    throw "system JSON has no structured facts; status=$status evidence.valid=$evidenceValid evidence.expected=$evidenceExpected evidence.unit=$evidenceUnit failures=$failureText"
  }
  $os = Get-SystemFieldRaw -Result $system -Key 'os'
  if ($os -notmatch '(?i)^Windows(?:\s+Server)?\b') { throw "system OS is not Windows: $os" }
  if ((Get-SystemFieldRaw -Result $system -Key 'arch') -ne 'amd64') { throw 'system arch is not amd64' }
  [void](Convert-PositiveMachineBytes -Raw (Get-SystemFieldRaw -Result $system -Key 'memory_total') -Name 'memory_total')
  [void](Convert-PositiveMachineBytes -Raw (Get-SystemFieldRaw -Result $system -Key 'disk_total') -Name 'disk_total')
  [void](Convert-PositiveMachineSeconds -Raw (Get-SystemFieldRaw -Result $system -Key 'uptime_seconds') -Name 'uptime_seconds')
  $measurements = @(Get-JsonPropertyValue -Object $system -Name 'measurements' -Context 'system result')
  $logicalMatches = @($measurements | Where-Object { [string]$_.key -eq 'logical_cpus' })
  if ($logicalMatches.Count -ne 1) { throw 'system JSON must contain one logical_cpus measurement' }
  $logicalCPUs = [double](Get-JsonPropertyValue -Object $logicalMatches[0] -Name 'value' -Context 'logical_cpus measurement')
  if ([double]::IsNaN($logicalCPUs) -or [double]::IsInfinity($logicalCPUs) -or $logicalCPUs -le 0) {
    throw "logical_cpus is not positive: $logicalCPUs"
  }
}

function Assert-EcsLoopbackLatencyReport {
  param(
    [Parameter(Mandatory)][string]$Root,
    [Parameter(Mandatory)][string]$Language
  )
  $reportItem = Get-ChildItem -LiteralPath $Root -Filter '*.json' -File -Recurse | Select-Object -First 1
  if ($null -eq $reportItem) { throw "loopback $Language JSON report was not written" }
  $report = Get-Content -Raw -LiteralPath $reportItem.FullName | ConvertFrom-Json
  if ([string]$report.schema_version -ne 'ecs.report/v1') {
    throw "loopback $Language JSON has unexpected schema_version: $($report.schema_version)"
  }
  $latencyMatches = @($report.results | Where-Object { [string]$_.id -eq 'latency' })
  if ($latencyMatches.Count -ne 1) { throw "loopback $Language JSON has no unique latency result" }
  $icmpMeasurements = @($latencyMatches[0].measurements | Where-Object {
    [string]$_.key -match '^icmp_.*_loopback_ipv4$' -and [string]$_.method -eq 'icmp-echo-v1'
  })
  if ($icmpMeasurements.Count -eq 0) {
    throw "loopback $Language JSON has no native ICMP measurement"
  }
  $lossMatches = @($latencyMatches[0].measurements | Where-Object { [string]$_.key -eq 'icmp_loss_percent_loopback_ipv4' -and [string]$_.method -eq 'icmp-echo-v1' })
  if ($lossMatches.Count -ne 1 -or [double]$lossMatches[0].value -ne 0) {
    throw "loopback $Language JSON did not report zero native ICMP loss"
  }
  if (@($latencyMatches[0].measurements | Where-Object { [string]$_.key -match '^icmp_' -and [string]$_.method -ne 'icmp-echo-v1' }).Count -ne 0) {
    throw "loopback $Language JSON contains an ICMP measurement with an unexpected method"
  }
}

$version = (Invoke-Ecs @('--version')) -join "`n"
if ($version -notmatch '(?i)ecs\s') { throw "--version output is invalid: $version" }

$list = (Invoke-Ecs @('list')) -join "`n"
foreach ($marker in @('standard', 'full', 'system')) {
  if ($list -notmatch [regex]::Escape($marker)) { throw "list output is missing $marker" }
}

$planText = (Invoke-Ecs @('plan', '--profile', 'standard')) -join "`n"
$plan = $planText | ConvertFrom-Json
if ([string]$plan.schema_version -ne 'ecs.plan/v1' -or [string]$plan.profile -ne 'standard') { throw 'standard plan contract failed' }

$jsonRoot = Join-Path $TempRoot 'ecs-system-json'
$jsonOutput = Invoke-Ecs @('run', '--lang', 'en', '--only', 'system', '--yes', '--format', 'json', '--output', $jsonRoot, '--no-color')
$jsonFile = Get-ChildItem -LiteralPath $jsonRoot -Filter '*.json' -File -Recurse | Select-Object -First 1
if ($null -eq $jsonFile) { throw 'system JSON report was not written' }
$report = Get-Content -Raw $jsonFile.FullName | ConvertFrom-Json
Assert-WindowsSystemFacts -Report $report

$textRoot = Join-Path $TempRoot 'ecs-system-text'
[void](Invoke-Ecs @('run', '--lang', 'en', '--only', 'system', '--yes', '--format', 'md', '--output', $textRoot, '--no-color'))
$textFile = Get-ChildItem -LiteralPath $textRoot -Filter '*.md' -File -Recurse | Select-Object -First 1
if ($null -eq $textFile -or $textFile.Length -eq 0) { throw 'system text report was not written' }

$redirected = Join-Path $TempRoot 'ecs-redirected.txt'
& $EcsPath run --lang en --only system --yes --format md --output (Join-Path $TempRoot 'ecs-redirected-report') *> $redirected
if ($LASTEXITCODE -ne 0) { throw 'redirected ecs.exe run failed' }
if ((Get-Content -Raw $redirected) -match "`e\[") { throw 'redirected output contains ANSI escape sequences' }

$env:NO_COLOR = ''
try {
  $noColorOutput = @(Invoke-Ecs @('run', '--lang', 'en', '--only', 'system', '--yes', '--format', 'md', '--output', (Join-Path $TempRoot 'ecs-no-color-report')))
  if (($noColorOutput -join "`n") -match "`e\[") { throw 'NO_COLOR did not suppress ANSI output' }
} finally { Remove-Item Env:NO_COLOR -ErrorAction SilentlyContinue }

$noColorFlagOutput = @(Invoke-Ecs @('run', '--lang', 'en', '--only', 'system', '--yes', '--format', 'md', '--output', (Join-Path $TempRoot 'ecs-no-color-flag-report'), '--no-color'))
if (($noColorFlagOutput -join "`n") -match "`e\[") { throw '--no-color did not suppress ANSI output' }

$listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
$listener.Start()
try {
  $port = $listener.LocalEndpoint.Port
  foreach ($language in @('en-US', 'zh-CN')) {
    $latencyRoot = Join-Path $TempRoot ("ecs-loopback-$language")
    [void](Invoke-Ecs @('run', '--lang', $language, '--only', 'latency', '--exposure', 'any', '--ip-version', '4', '--latency-attempts', '2', '--latency-targets', "loopback=127.0.0.1:$port", '--yes', '--format', 'json', '--output', $latencyRoot, '--no-color'))
    Assert-EcsLoopbackLatencyReport -Root $latencyRoot -Language $language
  }
} finally { $listener.Stop() }


if ($RunNativeTests) {
  & go test -tags=windows ./internal/probe -run '^TestWindowsNativeICMPLoopback$' -count=1 -v
  if ($LASTEXITCODE -ne 0) { throw "$Label TestWindowsNativeICMPLoopback failed" }
  & go test -tags=windows ./internal/probe -run '^TestWindowsNativeICMPDeadlineDoesNotHang$' -count=1 -v
  if ($LASTEXITCODE -ne 0) { throw "$Label TestWindowsNativeICMPDeadlineDoesNotHang failed" }
  & go test ./internal/probe -run '^TestProbeCommandWindowsProcessTree$' -count=1 -v
  if ($LASTEXITCODE -ne 0) { throw "$Label TestProbeCommandWindowsProcessTree failed" }
  & go test ./internal/ui -count=1 -v
  if ($LASTEXITCODE -ne 0) { throw "$Label internal/ui tests failed" }
  & go test ./cmd/ecs -run '^TestNotifyContextHandlesConsoleInterrupt$' -count=1 -v
  if ($LASTEXITCODE -ne 0) { throw "$Label TestNotifyContextHandlesConsoleInterrupt failed" }
}

Write-Output ("$Label ecs.exe runtime command contract passed: version/list/plan, system JSON/text, redirect, NO_COLOR, --no-color, and native loopback ICMP")
