[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('2022', '2025')][string]$Label,
    [string]$ArtifactRoot = '',
    [string]$GateInputsRoot = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if ([string]::IsNullOrWhiteSpace($ArtifactRoot)) {
    $ArtifactRoot = Join-Path $PWD '.ci/windows-tools-dist'
}
if ([string]::IsNullOrWhiteSpace($GateInputsRoot)) {
    $GateInputsRoot = Join-Path $PWD '.ci/windows-tools-gate-inputs'
}
$ArtifactRoot = [IO.Path]::GetFullPath($ArtifactRoot)
$GateInputsRoot = [IO.Path]::GetFullPath($GateInputsRoot)

$lock = Get-Content -Raw 'tools/lock.json' | ConvertFrom-Json
$archiveItem = Get-ChildItem $artifactRoot -File -Filter 'ecs-tools_windows_amd64.zip' -Recurse | Select-Object -First 1
if ($null -eq $archiveItem) { throw 'packaged Windows benchmark bundle is missing' }
$archive = $archiveItem.FullName
$bundle = Join-Path $PWD ('.ci/windows-bundle-' + $Label)
Expand-Archive -LiteralPath $archive -DestinationPath $bundle -Force
$stage = $bundle
foreach ($tool in @('zstd.exe', 'npb-ep.exe', 'npb-ft.exe', 'stream.exe', 'openssl.exe', 'fio.exe', 'nexttrace-tiny.exe')) {
  if (-not (Test-Path -LiteralPath (Join-Path $stage "bin\$tool") -PathType Leaf)) { throw "packaged workload is missing $tool" }
}
$corpusItem = Get-ChildItem $GateInputsRoot -File -Filter ([string]$lock.corpus.name) -Recurse | Select-Object -First 1
$objdump = Join-Path $GateInputsRoot 'inspector\ucrt64\bin\objdump.exe'
if ($null -eq $corpusItem -or -not (Test-Path -LiteralPath $objdump -PathType Leaf)) { throw 'packaged E2E gate inputs are incomplete' }
$corpus = $corpusItem.FullName
$objdump = [IO.Path]::GetFullPath($objdump)
& ./scripts/ci/windows_tools_gate.ps1 -StageRoot $stage -ManifestPath (Join-Path $stage 'manifest.json') -LockPath (Join-Path $PWD 'tools/lock.json') -CorpusPath $corpus -ObjdumpPath $objdump
$packagedGateSucceeded = $?
$packagedGateExitCode = $LASTEXITCODE
if (-not $packagedGateSucceeded -or $packagedGateExitCode -ne 0) { throw "E2E-$Label packaged workload gate failed with exit code $packagedGateExitCode" }
Write-Output "E2E-$Label validated the seven-tool packaged bundle, including six real workloads with fio/windowsaio and NextTrace prebuilt metadata; performance_valid=false"

$label = $Label
$artifactRoot = [IO.Path]::GetFullPath($ArtifactRoot)
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

function Get-ArtifactChecksum {
  param(
    [Parameter(Mandatory)][object]$ChecksumFile,
    [Parameter(Mandatory)][string]$Asset
  )
  $checksumMatches = @(
    foreach ($line in Get-Content -LiteralPath $ChecksumFile.FullName) {
      $match = [regex]::Match($line, '^\s*([0-9A-Fa-f]{64})\s+\*?(.+?)\s*$')
      if ($match.Success -and $match.Groups[2].Value -ceq $Asset) {
        $match.Groups[1].Value.ToLowerInvariant()
      }
    }
  )
  if ($checksumMatches.Count -ne 1) { throw "checksums.txt has no unique entry for $Asset" }
  return $checksumMatches[0]
}

function Assert-ArtifactChecksum {
  param(
    [Parameter(Mandatory)][object]$ChecksumFile,
    [Parameter(Mandatory)][object]$AssetFile,
    [Parameter(Mandatory)][string]$Asset
  )
  $expected = Get-ArtifactChecksum -ChecksumFile $ChecksumFile -Asset $Asset
  $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $AssetFile.FullName).Hash.ToLowerInvariant()
  if ($actual -cne $expected) { throw "$Asset SHA-256 mismatch" }
}

try {
    & $icmpPrerequisite -Action Setup -Label "E2E-$label" -OwnerToken $icmpOwnerToken
    $setupSucceeded = $?
    $setupExitCode = $LASTEXITCODE
    if (-not $setupSucceeded -or $setupExitCode -ne 0) { throw "E2E-$label runner ICMP prerequisite setup failed with exit code $setupExitCode" }
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
  Assert-ArtifactChecksum -ChecksumFile $mainChecksums -AssetFile $mainArchive -Asset 'ecs_windows_amd64.zip'
  Assert-ArtifactChecksum -ChecksumFile $bundleChecksums -AssetFile $bundleArchive -Asset 'ecs-tools_windows_amd64.zip'
  Assert-ArtifactChecksum -ChecksumFile $bundleChecksums -AssetFile $corpusArchive -Asset 'ecs-corpus_silesia-v1.tar.gz'

  $nextTraceStage = [IO.Path]::GetFullPath((Join-Path $runnerTemp ("ecs-nexttrace-packaged-E2E-$label-$capabilityID")))
  Expand-Archive -LiteralPath $bundleArchive.FullName -DestinationPath $nextTraceStage -Force
  $nextTracePath = [IO.Path]::GetFullPath((Join-Path $nextTraceStage 'bin\nexttrace-tiny.exe'))
  if (-not (Test-Path -LiteralPath $nextTracePath -PathType Leaf)) { throw "E2E-$label packaged NextTrace is missing: $nextTracePath" }
  if ([IO.Path]::GetFileName($nextTracePath) -cne 'nexttrace-tiny.exe') { throw "E2E-$label packaged NextTrace has an unexpected file name: $nextTracePath" }
  $expectedNextTraceSha256 = '16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b'
  $nextTraceSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $nextTracePath).Hash.ToLowerInvariant()
  if ($nextTraceSha256 -cne $expectedNextTraceSha256) { throw "E2E-$label packaged NextTrace SHA-256 mismatch: got $nextTraceSha256" }
  $LASTEXITCODE = 0
  & ./scripts/ci/windows_nexttrace_capability.ps1 -Family IPv4 -Target '1.1.1.1' -MaxHops 12 -NextTracePath $nextTracePath -ExpectedSha256 $expectedNextTraceSha256 -EvidencePath $capabilityPath
  $capabilitySucceeded = $?
  $capabilityExitCode = $LASTEXITCODE
  if (-not $capabilitySucceeded) { throw "E2E-$label NextTrace capability probe invocation failed" }
  if ($capabilityExitCode -ne 0) { throw "E2E-$label NextTrace capability probe failed with exit code $capabilityExitCode" }
  if (-not (Test-Path -LiteralPath $capabilityPath -PathType Leaf)) { throw "E2E-$label NextTrace capability evidence is missing: $capabilityPath" }

  Add-Type -AssemblyName System.IO.Compression.FileSystem
  $zip = [IO.Compression.ZipFile]::OpenRead($mainArchive.FullName)
  try { $mainMembers = @($zip.Entries | ForEach-Object { [string]$_.FullName }) } finally { $zip.Dispose() }
  $expectedMainMembers = @('ecs.exe', 'LICENSE', 'NOTICE', 'README.md', 'README_EN.md', 'SECURITY.md', 'THIRD_PARTY.md')
  if ((@($mainMembers | Sort-Object) -join "`n") -cne (@($expectedMainMembers | Sort-Object) -join "`n")) { throw 'main Windows ZIP member set changed' }
  $mainExtract = Join-Path $env:RUNNER_TEMP ("ecs-bootstrap-main-$label")
  Expand-Archive -LiteralPath $mainArchive.FullName -DestinationPath $mainExtract -Force
  $ecsPath = Join-Path $mainExtract 'ecs.exe'
  if (-not (Test-Path -LiteralPath $ecsPath -PathType Leaf)) { throw 'main Windows ZIP did not extract ecs.exe' }
  $planOutput = @(& $ecsPath plan --lang en --profile standard --only zstd --exposure any 2>&1)
  if ($LASTEXITCODE -ne 0) { throw 'current main ZIP plan failed' }
  $plan = ($planOutput -join "`n") | ConvertFrom-Json
  $requiredTools = @($plan.required_tools)
  if ($requiredTools.Count -ne 1 -or [string]$requiredTools[0] -cne 'zstd') { throw "only-required-tools plan drifted: $($requiredTools -join ',')" }

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
    & ./scripts/ci/windows_nexttrace_report_assert.ps1 -ReportPath $routeReportItem.FullName -Module route -Family 4 -FamilyName ipv4 -MaxHops 12 -Target $routeTarget4 -CapabilityPath $capabilityPath -NextTracePath $nextTracePath
    $routeAssertionSucceeded = $?
    $routeAssertionExitCode = $LASTEXITCODE
    if (-not $routeAssertionSucceeded -or $routeAssertionExitCode -ne 0) { throw "E2E-$label bootstrap route report assertion failed with exit code $routeAssertionExitCode" }
    if ([string]$env:ECS_TOOL_BIN -cne $sentinelToolBin) { throw "E2E-$label route bootstrap did not restore ECS_TOOL_BIN" }

    $backtraceTarget4 = '1.1.1.1'
    $backtraceReportRoot = Join-Path $env:RUNNER_TEMP ("ecs-bootstrap-backtrace-report-$label")
    & $runScript.FullName --lang en --profile standard --only backtrace --exposure public --ip-version 4 --backtrace-targets "telecom:gate=$backtraceTarget4" --yes --format json --output $backtraceReportRoot --no-color
    if ($LASTEXITCODE -ne 0 -or -not $?) { throw "E2E-$label run.ps1 backtrace bootstrap failed" }
    $backtraceReportItem = Get-ArtifactFile -Root $backtraceReportRoot -Filter '*.json'
    & ./scripts/ci/windows_nexttrace_report_assert.ps1 -ReportPath $backtraceReportItem.FullName -Module backtrace -Family 4 -FamilyName ipv4 -MaxHops 20 -Target $backtraceTarget4 -CapabilityPath $capabilityPath -NextTracePath $nextTracePath
    $backtraceAssertionSucceeded = $?
    $backtraceAssertionExitCode = $LASTEXITCODE
    if (-not $backtraceAssertionSucceeded -or $backtraceAssertionExitCode -ne 0) { throw "E2E-$label bootstrap backtrace report assertion failed with exit code $backtraceAssertionExitCode" }
    if ([string]$env:ECS_TOOL_BIN -cne $sentinelToolBin) { throw "E2E-$label backtrace bootstrap did not restore ECS_TOOL_BIN" }

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
    $installSucceeded = $?
    $installExitCode = $LASTEXITCODE
    if (-not $installSucceeded -or $installExitCode -ne 0) { throw "install.ps1 failed against the current artifact fixture with exit code $installExitCode" }
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
    Write-Output "E2E-$label verified current-commit SHA-256, ZIP extraction, HTTPS run.ps1/install.ps1, private staging, only required_tools=zstd, bootstrap route/backtrace ecs.report/v1 assertions using E2E-runner-generated capability evidence, ECS_TOOL_BIN restoration, user-directory install, and cleanup"
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
    $cleanupSucceeded = $?
    $cleanupExitCode = $LASTEXITCODE
    if (-not $cleanupSucceeded -or $cleanupExitCode -ne 0) { $cleanupErrors += "runner ICMP prerequisite cleanup failed with exit code $cleanupExitCode" }
  } catch {
    $cleanupErrors += "runner ICMP prerequisite cleanup failed: $($_.Exception.Message)"
  }
  if ($cleanupErrors.Count -ne 0) {
    throw ("E2E-$label cleanup failed: " + ($cleanupErrors -join '; '))
  }
}
