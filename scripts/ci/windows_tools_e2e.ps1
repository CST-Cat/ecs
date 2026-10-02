[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('2022', '2025')][string]$Label,
    [string]$ArtifactRoot = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ([string]::IsNullOrWhiteSpace($ArtifactRoot)) {
    $ArtifactRoot = Join-Path $PWD '.ci/windows-tools-dist'
}
$ArtifactRoot = [IO.Path]::GetFullPath($ArtifactRoot)

$lock = Get-Content -Raw 'tools/lock.json' | ConvertFrom-Json
$nexttraceLockEntries = @($lock.tools | Where-Object { [string]$_.name -ceq 'nexttrace-tiny' })
if ($nexttraceLockEntries.Count -ne 1) { throw 'tools lock has no unique NextTrace entry' }
$expectedNextTraceSha256 = [string]$nexttraceLockEntries[0].windows_asset_sha256.amd64
if ([string]::IsNullOrWhiteSpace($expectedNextTraceSha256)) { throw 'tools lock has no NextTrace Windows AMD64 asset digest' }
$label = $Label
$artifactRoot = [IO.Path]::GetFullPath($ArtifactRoot)
$runnerTemp = [IO.Path]::GetFullPath($env:RUNNER_TEMP)
$bundle = Join-Path $runnerTemp ("ecs-tools-packaged-E2E-$label-" + [guid]::NewGuid().ToString('N'))
$mainExtract = $null
$stage = $bundle
$serverJob = $null
$certificate = $null
$installDirectory = $null
$fixtureRoot = Join-Path $env:RUNNER_TEMP ("ecs-bootstrap-fixture-$label")
$runWorkBefore = $null
$certificateCheckDefaultWasPresent = $false
$certificateCheckDefaultOriginalValue = $null
$noProxyDefaultWasPresent = $false
$noProxyDefaultOriginalValue = $null
$icmpPrerequisite = Join-Path $PWD 'scripts/ci/windows_icmp_prerequisite.ps1'
$icmpOwnerToken = [guid]::NewGuid().ToString('N')

function Get-ArtifactFile {
  param(
    [Parameter(Mandatory)][string]$Root,
    [Parameter(Mandatory)][string]$Filter
  )
  $items = @(Get-ChildItem -LiteralPath $Root -File -Filter $Filter -Recurse)
  if ($items.Count -ne 1) { throw "artifact file $Filter is missing or ambiguous below $Root" }
  return $items[0]
}

function Assert-EcsBootstrapPlan {
  param(
    [Parameter(Mandatory)][object]$Plan,
    [Parameter(Mandatory)][ValidateSet('route', 'backtrace')][string]$Module,
    [Parameter(Mandatory)][string]$Family
  )
  if ([string]$Plan.schema_version -cne 'ecs.plan/v1') { throw "$Module plan schema is not ecs.plan/v1" }
  $modules = @($Plan.modules)
  if ($modules.Count -ne 1 -or [string]$modules[0].id -cne $Module) { throw "$Module plan selected modules are not exactly [$Module]" }
  $requiredTools = @($Plan.required_tools)
  if ($requiredTools.Count -ne 1 -or [string]$requiredTools[0] -cne 'nexttrace-tiny') { throw "$Module plan did not resolve exactly staged nexttrace-tiny" }
  if ([string]$Plan.ip_version -cne $Family -or [string]$Plan.exposure -cne 'public') {
    throw "$Module plan family/exposure is not the canonical public $Family contract"
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

try {
  & $icmpPrerequisite -Action Setup -Label "E2E-$label" -OwnerToken $icmpOwnerToken -CapabilityAwareIPv6
  $runnerTemp = [IO.Path]::GetFullPath($env:RUNNER_TEMP)
  if (-not (Test-Path -LiteralPath $runnerTemp -PathType Container)) { throw "E2E-$label RUNNER_TEMP is missing: $runnerTemp" }
  $capabilityID = [guid]::NewGuid().ToString('N')
  $capabilityPath = [IO.Path]::GetFullPath((Join-Path $runnerTemp ("ecs-nexttrace-capability-E2E-$label-$capabilityID.json")))
  if ([IO.Path]::GetDirectoryName($capabilityPath) -ine $runnerTemp) { throw "E2E-$label capability evidence escaped RUNNER_TEMP: $capabilityPath" }
  if (Test-Path -LiteralPath $capabilityPath) { throw "E2E-$label capability evidence path was not fresh: $capabilityPath" }
  $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
  $principal = [Security.Principal.WindowsPrincipal]::new($identity)
  $isAdministrator = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
  Write-Output ("bootstrap identity name={0}; administrator={1}" -f $identity.Name, $isAdministrator)

  $commitFile = Get-ArtifactFile -Root $artifactRoot -Filter 'CURRENT_COMMIT'
  if ((Get-Content -Raw -LiteralPath $commitFile.FullName).Trim() -cne [string]$env:GITHUB_SHA) {
    throw 'bootstrap artifact does not identify the current workflow commit'
  }
  $mainRoot = Join-Path $artifactRoot 'main'
  $bundleRoot = Join-Path $artifactRoot 'bundle'
  $mainArchive = Get-ArtifactFile -Root $mainRoot -Filter 'ecs_windows_amd64.zip'
  $mainChecksums = Get-ArtifactFile -Root $mainRoot -Filter 'checksums.txt'
  $bundleArchive = Get-ArtifactFile -Root $bundleRoot -Filter 'ecs-tools_windows_amd64.zip'
  $bundleChecksums = Get-ArtifactFile -Root $bundleRoot -Filter 'checksums.txt'
  $corpusArchive = Get-ArtifactFile -Root $bundleRoot -Filter 'ecs-corpus_silesia-v1.tar.gz'
  $runScript = Get-ArtifactFile -Root (Join-Path $artifactRoot 'bootstrap') -Filter 'run.ps1'
  $installScript = Get-ArtifactFile -Root (Join-Path $artifactRoot 'bootstrap') -Filter 'install.ps1'
  Expand-Archive -LiteralPath $bundleArchive.FullName -DestinationPath $bundle -Force
  & ./scripts/ci/windows_tools_gate.ps1 -CheckPackageContract -StageRoot $stage -LockPath (Join-Path $PWD 'tools/lock.json')
  Write-Output "E2E-$label accepted the packaged seven-tool manifest, licenses, layout, and BUILD producer facts"
  $nextTracePath = [IO.Path]::GetFullPath((Join-Path $stage 'bin\nexttrace-tiny.exe'))
  if (-not (Test-Path -LiteralPath $nextTracePath -PathType Leaf)) { throw "E2E-$label packaged NextTrace is missing: $nextTracePath" }
  if ([IO.Path]::GetFileName($nextTracePath) -cne 'nexttrace-tiny.exe') { throw "E2E-$label packaged NextTrace has an unexpected file name: $nextTracePath" }
  & ./scripts/ci/windows_nexttrace_capability.ps1 -Family IPv4 -Target '1.1.1.1' -MaxHops 12 -NextTracePath $nextTracePath -ExpectedSha256 $expectedNextTraceSha256 -EvidencePath $capabilityPath
  if (-not (Test-Path -LiteralPath $capabilityPath -PathType Leaf)) { throw "E2E-$label NextTrace capability evidence is missing: $capabilityPath" }
  . ./scripts/ci/windows_nexttrace_capability.ps1 -Family IPv4 -Target '1.1.1.1' -MaxHops 12 -NextTracePath $nextTracePath -ExpectedSha256 $expectedNextTraceSha256 -EvidencePath $capabilityPath
  try {
    $capabilityEvidence = Get-Content -Raw -LiteralPath $capabilityPath -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
  } catch {
    throw "E2E-$label NextTrace capability evidence is invalid JSON: $($_.Exception.Message)"
  }
  $capability = Assert-EcsCapabilityEvidence -Evidence $capabilityEvidence `
    -ExpectedEvidencePath $capabilityPath `
    -ExpectedNextTracePath $nextTracePath `
    -ExpectedFamilyName ipv4 `
    -ExpectedSha256 $expectedNextTraceSha256 `
    -ActualSha256 $expectedNextTraceSha256
  . ./scripts/ci/windows_nexttrace_report_assert.ps1

  & ./scripts/ci/windows_tools_integration.ps1 -Server $label -StageRoot $stage -CorpusArchivePath $corpusArchive.FullName

  Add-Type -AssemblyName System.IO.Compression.FileSystem
  $zip = [IO.Compression.ZipFile]::OpenRead($mainArchive.FullName)
  try { $mainMembers = @($zip.Entries | ForEach-Object { [string]$_.FullName }) } finally { $zip.Dispose() }
  $expectedMainMembers = @('ecs.exe', 'LICENSE', 'NOTICE', 'README.md', 'README_EN.md', 'SECURITY.md', 'THIRD_PARTY.md')
  if ((@($mainMembers | Sort-Object) -join "`n") -cne (@($expectedMainMembers | Sort-Object) -join "`n")) { throw 'main Windows ZIP member set changed' }
  $mainExtract = Join-Path $runnerTemp ("ecs-bootstrap-main-$label-" + [guid]::NewGuid().ToString('N'))
  Expand-Archive -LiteralPath $mainArchive.FullName -DestinationPath $mainExtract -Force
  $ecsPath = Join-Path $mainExtract 'ecs.exe'
  if (-not (Test-Path -LiteralPath $ecsPath -PathType Leaf)) { throw 'main Windows ZIP did not extract ecs.exe' }
  & ./scripts/ci/windows_runtime_contract.ps1 -EcsPath $ecsPath -Label "E2E-$label" -RunNativeTests

  $planOutput = @(& $ecsPath plan --lang en --profile standard --only zstd --exposure any 2>&1)
  if ($LASTEXITCODE -ne 0) { throw 'current main ZIP plan failed' }
  $plan = ($planOutput -join "`n") | ConvertFrom-Json
  $requiredTools = @($plan.required_tools)
  if ($requiredTools.Count -ne 1 -or [string]$requiredTools[0] -cne 'zstd') { throw "only-required-tools plan drifted: $($requiredTools -join ',')" }

  $hadPlanToolBin = Test-Path Env:ECS_TOOL_BIN
  $oldPlanToolBin = [Environment]::GetEnvironmentVariable('ECS_TOOL_BIN', [EnvironmentVariableTarget]::Process)
  $hadPlanPath = Test-Path Env:PATH
  $oldPlanPath = [Environment]::GetEnvironmentVariable('PATH', [EnvironmentVariableTarget]::Process)
  $hadPlanNoColor = Test-Path Env:NO_COLOR
  $oldPlanNoColor = [Environment]::GetEnvironmentVariable('NO_COLOR', [EnvironmentVariableTarget]::Process)
  try {
    $env:ECS_TOOL_BIN = Join-Path $stage 'bin'
    $env:PATH = "$env:SystemRoot\System32;$env:SystemRoot"
    $env:NO_COLOR = '1'
    foreach ($planCase in @(
      [pscustomobject]@{ Module = 'route'; Family = '4'; Arguments = @('--route-targets', 'gate=1.1.1.1') },
      [pscustomobject]@{ Module = 'backtrace'; Family = '4'; Arguments = @('--backtrace-targets', 'telecom:gate=1.1.1.1') }
    )) {
      $planArguments = @('plan', '--lang', 'en', '--only', $planCase.Module, '--exposure', 'public', '--ip-version', $planCase.Family) + $planCase.Arguments
      $modulePlanOutput = @(& $ecsPath @planArguments 2>&1)
      if ($LASTEXITCODE -ne 0) { throw "E2E-$label ecs.exe plan --only $($planCase.Module) failed" }
      try { $modulePlan = ($modulePlanOutput -join "`n") | ConvertFrom-Json -ErrorAction Stop }
      catch { throw "E2E-$label $($planCase.Module) plan returned invalid JSON: $($_.Exception.Message)" }
      Assert-EcsBootstrapPlan -Plan $modulePlan -Module $planCase.Module -Family $planCase.Family
    }
  } finally {
    [Environment]::SetEnvironmentVariable('ECS_TOOL_BIN', $(if ($hadPlanToolBin) { [string]$oldPlanToolBin } else { $null }), [EnvironmentVariableTarget]::Process)
    [Environment]::SetEnvironmentVariable('PATH', $(if ($hadPlanPath) { [string]$oldPlanPath } else { $null }), [EnvironmentVariableTarget]::Process)
    [Environment]::SetEnvironmentVariable('NO_COLOR', $(if ($hadPlanNoColor) { [string]$oldPlanNoColor } else { $null }), [EnvironmentVariableTarget]::Process)
  }

  New-Item -ItemType Directory -Force -Path (Join-Path $fixtureRoot 'main'), (Join-Path $fixtureRoot 'bundle') | Out-Null
  Copy-Item -LiteralPath $mainArchive.FullName -Destination (Join-Path $fixtureRoot 'main/ecs_windows_amd64.zip')
  Copy-Item -LiteralPath $mainChecksums.FullName -Destination (Join-Path $fixtureRoot 'main/checksums.txt')
  Copy-Item -LiteralPath $bundleArchive.FullName -Destination (Join-Path $fixtureRoot 'bundle/ecs-tools_windows_amd64.zip')
  Copy-Item -LiteralPath $bundleChecksums.FullName -Destination (Join-Path $fixtureRoot 'bundle/checksums.txt')
  Copy-Item -LiteralPath $corpusArchive.FullName -Destination (Join-Path $fixtureRoot 'bundle/ecs-corpus_silesia-v1.tar.gz')

  $certificate = New-SelfSignedCertificate -DnsName 'localhost' -CertStoreLocation Cert:\CurrentUser\My -NotAfter (Get-Date).AddHours(2) -KeyExportPolicy Exportable -Type SSLServerAuthentication
  $certificateFile = Join-Path $fixtureRoot 'fixture.cer'
  $pfxFile = Join-Path $fixtureRoot 'fixture.pfx'
  $certificatePassword = 'ecs-phase8-fixture-password'
  $securePassword = ConvertTo-SecureString $certificatePassword -AsPlainText -Force
  Export-Certificate -Cert $certificate -FilePath $certificateFile | Out-Null
  Export-PfxCertificate -Cert $certificate -FilePath $pfxFile -Password $securePassword | Out-Null
  $serverScript = {
    param([string]$Root, [string]$PfxPath, [string]$Password)
    $ErrorActionPreference = 'Stop'
    $cert = [Security.Cryptography.X509Certificates.X509Certificate2]::new($PfxPath, $Password, [Security.Cryptography.X509Certificates.X509KeyStorageFlags]::DefaultKeySet)
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::IPv6Loopback, 0)
    $listener.Start()
    $endpoint = [Net.IPEndPoint]$listener.LocalEndpoint
    Write-Output ("READY:{0};family={1};address={2}" -f $endpoint.Port, $endpoint.Address.AddressFamily, $endpoint.Address.IPAddressToString)
    try {
      while ($true) {
        $client = $listener.AcceptTcpClient()
        try {
          $ssl = [Net.Security.SslStream]::new($client.GetStream(), $false)
          try {
            $ssl.AuthenticateAsServer($cert, $false, [Security.Authentication.SslProtocols]::Tls12, $false)
            $reader = [IO.StreamReader]::new($ssl, [Text.Encoding]::ASCII, $false, 4096, $true)
            $requestLine = $reader.ReadLine()
            $headerLine = $null
            while ($null -ne ($headerLine = $reader.ReadLine()) -and $headerLine -ne '') { }
            $status = '404 Not Found'
            $body = [Text.Encoding]::UTF8.GetBytes('not found')
            if ($requestLine -match '^GET\s+(?<path>/[^\s?]*)\s+HTTP/') {
              $relative = [Uri]::UnescapeDataString($Matches.path.TrimStart('/'))
              if ($relative -notmatch '(^|/)\.\.(/|$)' -and $relative -notmatch '\\') {
                $candidate = [IO.Path]::GetFullPath((Join-Path $Root $relative.Replace('/', [IO.Path]::DirectorySeparatorChar)))
                $rootFull = [IO.Path]::GetFullPath($Root).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
                if ($candidate.StartsWith($rootFull, [StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $candidate -PathType Leaf)) {
                  $status = '200 OK'
                  $body = [IO.File]::ReadAllBytes($candidate)
                }
              }
            }
            $response = "HTTP/1.1 $status`r`nContent-Length: $($body.Length)`r`nContent-Type: application/octet-stream`r`nConnection: close`r`n`r`n"
            $responseBytes = [Text.Encoding]::ASCII.GetBytes($response)
            $ssl.Write($responseBytes, 0, $responseBytes.Length)
            $ssl.Write($body, 0, $body.Length)
            $ssl.Flush()
          } finally { $ssl.Dispose() }
        } finally { $client.Dispose() }
      }
    } finally { $listener.Stop(); $cert.Dispose() }
  }
  $serverJob = Start-Job -ScriptBlock $serverScript -ArgumentList $fixtureRoot, $pfxFile, $certificatePassword
  $readyLine = $null
  for ($attempt = 0; $attempt -lt 100 -and $null -eq $readyLine; $attempt++) {
    if ($serverJob.State -eq 'Failed') { throw "HTTPS fixture failed: $($serverJob.ChildJobs[0].JobStateInfo.Reason)" }
    $readyLine = Receive-Job -Job $serverJob -Keep | Where-Object { [string]$_ -match '^READY:[0-9]+;family=[^;]+;address=[^;]+$' } | Select-Object -Last 1
    if ($null -eq $readyLine) { Start-Sleep -Milliseconds 100 }
  }
  if ($null -eq $readyLine) { throw 'HTTPS fixture did not become ready' }
  $readyText = ([string]$readyLine).Trim()
  Write-Output ("bootstrap fixture ready signal: <{0}>" -f $readyText)
  $readyMatch = [regex]::Match($readyText, '^READY:(?<port>[0-9]+);family=(?<family>[^;]+);address=(?<address>[^;]+)$')
  if (-not $readyMatch.Success) { throw 'HTTPS fixture readiness endpoint was malformed' }
  $port = [int]$readyMatch.Groups['port'].Value
  Write-Output ("bootstrap fixture listener endpoint: family={0}; address={1}; port={2}" -f $readyMatch.Groups['family'].Value, $readyMatch.Groups['address'].Value, $port)
  $localhostAddresses = @([System.Net.Dns]::GetHostAddresses('localhost'))
  if ($localhostAddresses.Count -eq 0) { throw 'localhost resolved to no addresses' }
  foreach ($localhostAddress in $localhostAddresses) {
    Write-Output ("bootstrap localhost resolution family={0}; address={1}" -f $localhostAddress.AddressFamily, $localhostAddress.IPAddressToString)
  }
  $mainBase = "https://localhost:$port/main"
  $bundleBase = "https://localhost:$port/bundle"

  $runWorkBefore = @(Get-ChildItem ([IO.Path]::GetTempPath()) -Directory -Filter 'ecs-run-*' | ForEach-Object { $_.FullName })
  $sentinelToolBin = Join-Path $env:RUNNER_TEMP ("ecs-tool-bin-sentinel-$label")
  New-Item -ItemType Directory -Force -Path $sentinelToolBin | Out-Null
  $env:ECS_TOOL_BIN = $sentinelToolBin
  $env:ECS_RELEASE_BASE = $mainBase
  $env:ECS_BUNDLE_RELEASE_BASE = $bundleBase
  $releaseBaseUri = [Uri]::new([string]$env:ECS_RELEASE_BASE)
  $bundleReleaseBaseUri = [Uri]::new([string]$env:ECS_BUNDLE_RELEASE_BASE)
  if (-not $releaseBaseUri.IsAbsoluteUri -or $releaseBaseUri.Scheme -cne 'https' -or $releaseBaseUri.Host -cne 'localhost' -or $releaseBaseUri.Port -ne $port) {
    throw 'ECS_RELEASE_BASE must be the current local HTTPS fixture'
  }
  if (-not $bundleReleaseBaseUri.IsAbsoluteUri -or $bundleReleaseBaseUri.Scheme -cne 'https' -or $bundleReleaseBaseUri.Host -cne 'localhost' -or $bundleReleaseBaseUri.Port -ne $port) {
    throw 'ECS_BUNDLE_RELEASE_BASE must be the current local HTTPS fixture'
  }
  Write-Output ("bootstrap fixture URIs main={0}; bundle={1}" -f [string]$env:ECS_RELEASE_BASE, [string]$env:ECS_BUNDLE_RELEASE_BASE)
  $env:ECS_REPOSITORY = 'CST-Cat/ecs'
  $env:ECS_VERSION = 'phase8-current'
  $runReportRoot = Join-Path $env:RUNNER_TEMP ("ecs-bootstrap-report-$label")
  $machinePathBaseline = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Machine)
  $userPathBaseline = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
  $invokeWebRequestCommand = Get-Command Invoke-WebRequest -ErrorAction Stop
  if (-not $invokeWebRequestCommand.Parameters.ContainsKey('NoProxy')) { throw 'current pwsh Invoke-WebRequest lacks NoProxy' }
  $noProxyParameterName = 'Invoke-WebRequest:NoProxy'
  $noProxyDefaultWasPresent = $PSDefaultParameterValues.ContainsKey($noProxyParameterName)
  $noProxyDefaultOriginalValue = $null
  if ($noProxyDefaultWasPresent) {
    $noProxyDefaultOriginalValue = $PSDefaultParameterValues[$noProxyParameterName]
  }
  $certificateCheckParameterName = 'Invoke-WebRequest:SkipCertificateCheck'
  $certificateCheckDefaultWasPresent = $PSDefaultParameterValues.ContainsKey($certificateCheckParameterName)
  $certificateCheckDefaultOriginalValue = $null
  if ($certificateCheckDefaultWasPresent) {
    $certificateCheckDefaultOriginalValue = $PSDefaultParameterValues[$certificateCheckParameterName]
  }
  try {
    $PSDefaultParameterValues[$certificateCheckParameterName] = $true
    $PSDefaultParameterValues[$noProxyParameterName] = $true
    $probeFile = Join-Path $fixtureRoot 'probe-checksums.txt'
    Invoke-WebRequest -UseBasicParsing -Uri "$($env:ECS_RELEASE_BASE)/checksums.txt" -OutFile $probeFile -NoProxy -ErrorAction Stop
    if (-not (Test-Path -LiteralPath $probeFile -PathType Leaf) -or (Get-Item -LiteralPath $probeFile).Length -eq 0) {
      throw 'localhost HTTPS fixture probe returned no file'
    }
    Write-Output ("bootstrap localhost HTTPS probe succeeded uri={0}/checksums.txt" -f [string]$env:ECS_RELEASE_BASE)
    & $runScript.FullName --lang en --profile standard --only zstd --exposure any --yes --format json --output $runReportRoot
    if (-not $?) { throw 'run.ps1 failed against the current artifact fixture' }
    $machinePathAfterRun = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Machine)
    $userPathAfterRun = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
    if ($machinePathBaseline -cne $machinePathAfterRun -or $userPathBaseline -cne $userPathAfterRun) { throw 'run.ps1 changed the Machine or User PATH' }
    if ([string]$env:ECS_TOOL_BIN -cne $sentinelToolBin) { throw 'run.ps1 did not restore ECS_TOOL_BIN' }
    $runReportItem = Get-ArtifactFile -Root $runReportRoot -Filter '*.json'
    $runReport = Get-Content -Raw -LiteralPath $runReportItem.FullName | ConvertFrom-Json
    $zstdResults = @($runReport.results | Where-Object { [string]$_.id -eq 'zstd' })
    if ($zstdResults.Count -ne 1 -or [string]$zstdResults[0].status -eq 'error') { throw 'run.ps1 did not execute the only required zstd tool' }

    $routeTarget4 = '1.1.1.1'
    $routeReportRoot = Join-Path $env:RUNNER_TEMP ("ecs-bootstrap-route-report-$label")
    & $runScript.FullName --lang en --profile standard --only route --exposure public --ip-version 4 --route-targets "gate=$routeTarget4" --yes --format json --output $routeReportRoot --no-color
    if ($LASTEXITCODE -ne 0 -or -not $?) { throw "E2E-$label run.ps1 route bootstrap failed" }
    $routeReportItem = Get-ArtifactFile -Root $routeReportRoot -Filter '*.json'
    try {
      $routeReport = Get-Content -Raw -LiteralPath $routeReportItem.FullName -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
    } catch {
      throw "E2E-$label route bootstrap report is invalid JSON: $($_.Exception.Message)"
    }
    $routeAssertion = Assert-EcsCanonicalTraceReport -Report $routeReport -Module route -Family 4 -FamilyName ipv4 -MaxHops 12 -Target $routeTarget4 `
      -CapabilityDecision $capability.Decision -CapabilityLiveNetworkNotProven $capability.LiveNetworkNotProven
    Write-EcsBootstrapTraceReportResult -Assertion $routeAssertion -Capability $capability
    if ([string]$env:ECS_TOOL_BIN -cne $sentinelToolBin) { throw "E2E-$label route bootstrap did not restore ECS_TOOL_BIN" }

    $backtraceTarget4 = '1.1.1.1'
    $backtraceReportRoot = Join-Path $env:RUNNER_TEMP ("ecs-bootstrap-backtrace-report-$label")
    & $runScript.FullName --lang en --profile standard --only backtrace --exposure public --ip-version 4 --backtrace-targets "telecom:gate=$backtraceTarget4" --yes --format json --output $backtraceReportRoot --no-color
    if ($LASTEXITCODE -ne 0 -or -not $?) { throw "E2E-$label run.ps1 backtrace bootstrap failed" }
    $backtraceReportItem = Get-ArtifactFile -Root $backtraceReportRoot -Filter '*.json'
    try {
      $backtraceReport = Get-Content -Raw -LiteralPath $backtraceReportItem.FullName -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop
    } catch {
      throw "E2E-$label backtrace bootstrap report is invalid JSON: $($_.Exception.Message)"
    }
    $backtraceAssertion = Assert-EcsCanonicalTraceReport -Report $backtraceReport -Module backtrace -Family 4 -FamilyName ipv4 -MaxHops 20 -Target $backtraceTarget4 `
      -CapabilityDecision $capability.Decision -CapabilityLiveNetworkNotProven $capability.LiveNetworkNotProven
    Write-EcsBootstrapTraceReportResult -Assertion $backtraceAssertion -Capability $capability
    if ([string]$env:ECS_TOOL_BIN -cne $sentinelToolBin) { throw "E2E-$label backtrace bootstrap did not restore ECS_TOOL_BIN" }

    if (Test-EcsGlobalIPv6Capability) {
      $hadTraceToolBin = Test-Path Env:ECS_TOOL_BIN
      $oldTraceToolBin = [Environment]::GetEnvironmentVariable('ECS_TOOL_BIN', [EnvironmentVariableTarget]::Process)
      $hadTracePath = Test-Path Env:PATH
      $oldTracePath = [Environment]::GetEnvironmentVariable('PATH', [EnvironmentVariableTarget]::Process)
      $hadTraceNoColor = Test-Path Env:NO_COLOR
      $oldTraceNoColor = [Environment]::GetEnvironmentVariable('NO_COLOR', [EnvironmentVariableTarget]::Process)
      try {
        $env:ECS_TOOL_BIN = Join-Path $stage 'bin'
        $env:PATH = "$env:SystemRoot\System32;$env:SystemRoot"
        $env:NO_COLOR = '1'
        $target6 = '2606:4700:4700::1111'
        $routeReportRoot6 = Join-Path $runnerTemp ("ecs-bootstrap-route-ipv6-report-$label")
        $routeOutput6 = @(& $ecsPath run --lang en --only route --format json --exposure public --ip-version 6 --route-targets "gate=$target6" --yes --output $routeReportRoot6 --no-color 2>&1)
        if ($LASTEXITCODE -ne 0) { throw "E2E-$label ecs.exe IPv6 route run failed: $($routeOutput6 -join "`n")" }
        $routeReportItem6 = Get-ArtifactFile -Root $routeReportRoot6 -Filter '*.json'
        try { $routeReport6 = Get-Content -Raw -LiteralPath $routeReportItem6.FullName -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop }
        catch { throw "E2E-$label IPv6 route report is invalid JSON: $($_.Exception.Message)" }
        $routeAssertion6 = Assert-EcsCanonicalTraceReport -Report $routeReport6 -Module route -Family 6 -FamilyName ipv6 -MaxHops 12 -Target $target6
        Write-Output ("E2E-$label IPv6 route report passed: schema=ecs.report/v1; status={0}; target={1}; responding_hops={2}" -f $routeAssertion6.Status, $routeAssertion6.Target, $routeAssertion6.RespondingHopCount)

        $backtraceReportRoot6 = Join-Path $runnerTemp ("ecs-bootstrap-backtrace-ipv6-report-$label")
        $backtraceOutput6 = @(& $ecsPath run --lang en --only backtrace --format json --exposure public --ip-version 6 --backtrace-targets "telecom:gate=$target6" --yes --output $backtraceReportRoot6 --no-color 2>&1)
        if ($LASTEXITCODE -ne 0) { throw "E2E-$label ecs.exe IPv6 backtrace run failed: $($backtraceOutput6 -join "`n")" }
        $backtraceReportItem6 = Get-ArtifactFile -Root $backtraceReportRoot6 -Filter '*.json'
        try { $backtraceReport6 = Get-Content -Raw -LiteralPath $backtraceReportItem6.FullName -ErrorAction Stop | ConvertFrom-Json -ErrorAction Stop }
        catch { throw "E2E-$label IPv6 backtrace report is invalid JSON: $($_.Exception.Message)" }
        $backtraceAssertion6 = Assert-EcsCanonicalTraceReport -Report $backtraceReport6 -Module backtrace -Family 6 -FamilyName ipv6 -MaxHops 20 -Target $target6
        Write-Output ("E2E-$label IPv6 backtrace report passed: schema=ecs.report/v1; status={0}; target={1}; responding_hops={2}" -f $backtraceAssertion6.Status, $backtraceAssertion6.Target, $backtraceAssertion6.RespondingHopCount)
      } finally {
        [Environment]::SetEnvironmentVariable('ECS_TOOL_BIN', $(if ($hadTraceToolBin) { [string]$oldTraceToolBin } else { $null }), [EnvironmentVariableTarget]::Process)
        [Environment]::SetEnvironmentVariable('PATH', $(if ($hadTracePath) { [string]$oldTracePath } else { $null }), [EnvironmentVariableTarget]::Process)
        [Environment]::SetEnvironmentVariable('NO_COLOR', $(if ($hadTraceNoColor) { [string]$oldTraceNoColor } else { $null }), [EnvironmentVariableTarget]::Process)
      }
    }

    $runWorkAfter = @(Get-ChildItem ([IO.Path]::GetTempPath()) -Directory -Filter 'ecs-run-*' | ForEach-Object { $_.FullName })
    if (@($runWorkAfter | Where-Object { $runWorkBefore -notcontains $_ }).Count -ne 0) { throw 'run.ps1 left private staging behind' }

    $protectedInstallTargets = @(
      (Join-Path $env:ProgramFiles 'ecs-e2e-forbidden')
      (Join-Path $env:ProgramData 'ecs-e2e-forbidden')
      (Join-Path $env:SystemRoot 'ecs-e2e-forbidden')
      (Join-Path $PWD.Path 'ecs-e2e-forbidden')
      (Join-Path $env:RUNNER_TEMP 'ecs-e2e-forbidden')
    )
    $expectedProtectedInstallError = 'ecs install: install directory must remain under the current user local-data directory'
    foreach ($protectedInstallTarget in $protectedInstallTargets) {
      if (Test-Path -LiteralPath $protectedInstallTarget) { throw "protected install target already exists: $protectedInstallTarget" }
      $tempInstallBefore = @(Get-ChildItem ([IO.Path]::GetTempPath()) -Directory -Filter 'ecs-install-*' | ForEach-Object { $_.FullName })
      $protectedInstallOutput = @()
      $protectedInstallException = $null
      $protectedInstallSucceeded = $true
      try {
        $protectedInstallOutput = @(& $installScript.FullName -Repository 'CST-Cat/ecs' -Version 'phase8-current' -ReleaseBase $mainBase -InstallDirectory $protectedInstallTarget 2>&1)
      } catch {
        $protectedInstallSucceeded = $false
        $protectedInstallException = $_
      }
      if (Test-Path -LiteralPath $protectedInstallTarget) { throw "protected install target was created: $protectedInstallTarget" }
      $tempInstallAfter = @(Get-ChildItem ([IO.Path]::GetTempPath()) -Directory -Filter 'ecs-install-*' | ForEach-Object { $_.FullName })
      if (@($tempInstallAfter | Where-Object { $tempInstallBefore -notcontains $_ }).Count -ne 0) { throw "protected install created a temporary work directory for $protectedInstallTarget" }
      if ($protectedInstallSucceeded) { throw "install.ps1 unexpectedly accepted protected install target: $protectedInstallTarget" }
      $protectedInstallFailureText = ((@($protectedInstallOutput) + @($protectedInstallException)) | Out-String)
      if ($protectedInstallFailureText -notmatch [regex]::Escape($expectedProtectedInstallError)) { throw "protected install did not preserve the expected path rejection for $protectedInstallTarget" }
    }

    $localAppDataFull = [IO.Path]::GetFullPath($env:LOCALAPPDATA).TrimEnd('\')
    $localAppDataEcsFull = [IO.Path]::GetFullPath($localAppDataFull + '\ecs').TrimEnd('\')
    $installDirectory = Join-Path $env:LOCALAPPDATA ("ecs\phase8-e2e-$label-" + [guid]::NewGuid().ToString('N'))
    $installFull = [IO.Path]::GetFullPath($installDirectory)
    if (-not $installFull.StartsWith($localAppDataEcsFull + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'install E2E path escaped LOCALAPPDATA\ecs' }
    & $installScript.FullName -Repository 'CST-Cat/ecs' -Version 'phase8-current' -ReleaseBase $mainBase -InstallDirectory $installDirectory
    $machinePathAfterInstall = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Machine)
    $userPathAfterInstall = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
    if ($machinePathBaseline -cne $machinePathAfterInstall -or $userPathBaseline -cne $userPathAfterInstall) { throw 'install.ps1 changed the Machine or User PATH' }
    $installedEcs = Join-Path $installDirectory 'ecs.exe'
    if (-not (Test-Path -LiteralPath $installedEcs -PathType Leaf)) { throw 'install.ps1 did not create the user-directory ecs.exe' }
    $installedVersion = @(& $installedEcs --version 2>&1)
    if ($LASTEXITCODE -ne 0 -or ($installedVersion -join "`n") -notmatch '(?i)ecs\s') { throw 'installed ecs.exe did not run' }
    $machinePathAfterVersion = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::Machine)
    $userPathAfterVersion = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
    if ($machinePathBaseline -cne $machinePathAfterVersion -or $userPathBaseline -cne $userPathAfterVersion) { throw 'installed ecs.exe --version changed the Machine or User PATH' }
    Remove-Item -LiteralPath $installDirectory -Recurse -Force
    if (Test-Path -LiteralPath $installDirectory) { throw 'install E2E user directory cleanup failed' }
    $installDirectory = $null
    Write-Output "E2E-$label verified packaged ZIP execution, HTTPS run.ps1/install.ps1 downloads, private staging, only required_tools=zstd, canonical IPv4 route/backtrace reports, ECS_TOOL_BIN restoration, user-directory install, and cleanup"
  } finally {
    try {
      if ($noProxyDefaultWasPresent) {
        $PSDefaultParameterValues[$noProxyParameterName] = $noProxyDefaultOriginalValue
        if (-not $PSDefaultParameterValues.ContainsKey($noProxyParameterName) -or
            $PSDefaultParameterValues[$noProxyParameterName] -cne $noProxyDefaultOriginalValue) {
          throw 'Invoke-WebRequest no-proxy default was not restored exactly'
        }
      } else {
        [void]$PSDefaultParameterValues.Remove($noProxyParameterName)
        if ($PSDefaultParameterValues.ContainsKey($noProxyParameterName)) {
          throw 'Invoke-WebRequest no-proxy default was not removed'
        }
      }
    } finally {
      if ($certificateCheckDefaultWasPresent) {
        $PSDefaultParameterValues[$certificateCheckParameterName] = $certificateCheckDefaultOriginalValue
        if (-not $PSDefaultParameterValues.ContainsKey($certificateCheckParameterName) -or
            $PSDefaultParameterValues[$certificateCheckParameterName] -cne $certificateCheckDefaultOriginalValue) {
          throw 'Invoke-WebRequest certificate-check default was not restored exactly'
        }
      } else {
        [void]$PSDefaultParameterValues.Remove($certificateCheckParameterName)
        if ($PSDefaultParameterValues.ContainsKey($certificateCheckParameterName)) {
          throw 'Invoke-WebRequest certificate-check default was not removed'
        }
      }
    }
  }
} finally {
  $cleanupErrors = @()
  foreach ($temporaryRoot in @(
    [pscustomobject]@{ Label = 'packaged bundle extraction'; Path = $bundle },
    [pscustomobject]@{ Label = 'main executable extraction'; Path = $mainExtract }
  )) {
    if ($null -ne $temporaryRoot.Path -and (Test-Path -LiteralPath $temporaryRoot.Path)) {
      try {
        Remove-Item -LiteralPath $temporaryRoot.Path -Recurse -Force -ErrorAction Stop
      } catch {
        $cleanupErrors += "$($temporaryRoot.Label) cleanup failed: $($_.Exception.Message)"
      }
    }
    if ($null -ne $temporaryRoot.Path -and (Test-Path -LiteralPath $temporaryRoot.Path)) {
      $cleanupErrors += "$($temporaryRoot.Label) remains: $($temporaryRoot.Path)"
    }
  }
  if ($null -ne $serverJob) {
    try {
      if ($serverJob.State -notin @('Completed', 'Failed', 'Stopped')) {
        Stop-Job -Job $serverJob -ErrorAction Stop
      }
    } catch {
      $cleanupErrors += "server job stop failed: $($_.Exception.Message)"
    }
    try {
      Remove-Job -Job $serverJob -Force -ErrorAction Stop
    } catch {
      $cleanupErrors += "server job removal failed: $($_.Exception.Message)"
    }
  }
  if ($null -ne $certificate) {
    $certificatePath = "Cert:\CurrentUser\My\$($certificate.Thumbprint)"
    try {
      if (Test-Path -LiteralPath $certificatePath) {
        Remove-Item -LiteralPath $certificatePath -Force -ErrorAction Stop
      }
    } catch {
      $cleanupErrors += "fixture certificate cleanup failed: $($_.Exception.Message)"
    }
    if (Test-Path -LiteralPath $certificatePath) {
      $cleanupErrors += "fixture certificate remains in CurrentUser\My: $certificatePath"
    }
  }
  if ($null -ne $installDirectory -and (Test-Path -LiteralPath $installDirectory)) {
    try {
      Remove-Item -LiteralPath $installDirectory -Recurse -Force -ErrorAction Stop
    } catch {
      $cleanupErrors += "install directory cleanup failed: $($_.Exception.Message)"
    }
  }
  if ($null -ne $installDirectory -and (Test-Path -LiteralPath $installDirectory)) {
    $cleanupErrors += "install directory remains: $installDirectory"
  }
  if (Test-Path -LiteralPath $fixtureRoot) {
    try {
      Remove-Item -LiteralPath $fixtureRoot -Recurse -Force -ErrorAction Stop
    } catch {
      $cleanupErrors += "fixture directory cleanup failed: $($_.Exception.Message)"
    }
  }
  if (Test-Path -LiteralPath $fixtureRoot) {
    $cleanupErrors += "fixture directory remains: $fixtureRoot"
  }
  if ($null -ne $runWorkBefore) {
    try {
      $runWorkAfterFinal = @(Get-ChildItem ([IO.Path]::GetTempPath()) -Directory -Filter 'ecs-run-*' | ForEach-Object { $_.FullName })
      $runWorkLeftover = @($runWorkAfterFinal | Where-Object { $runWorkBefore -notcontains $_ })
      if ($runWorkLeftover.Count -ne 0) {
        $cleanupErrors += "private staging remains: $($runWorkLeftover -join ', ')"
      }
    } catch {
      $cleanupErrors += "private staging cleanup check failed: $($_.Exception.Message)"
    }
  }
  # This outer bootstrap finally owns the workflow-only runner prerequisite cleanup.
  try {
    & $icmpPrerequisite -Action Cleanup -Label "E2E-$label" -OwnerToken $icmpOwnerToken
  } catch {
    $cleanupErrors += "runner ICMP prerequisite cleanup failed: $($_.Exception.Message)"
  }
  if ($cleanupErrors.Count -ne 0) {
    throw ("E2E-$label cleanup failed: " + ($cleanupErrors -join '; '))
  }
}
