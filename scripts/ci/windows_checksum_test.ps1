$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$runPath = Join-Path $repoRoot 'run.ps1'
$installPath = Join-Path $repoRoot 'install.ps1'
$fixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ('ecs checksum test ' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixtureRoot | Out-Null

function Import-ProductionFunction {
  param(
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$Name
  )
  $tokens = $null
  $errors = $null
  $ast = [System.Management.Automation.Language.Parser]::ParseFile($Path, [ref]$tokens, [ref]$errors)
  if ($errors.Count -ne 0) { throw "production script has PowerShell parse errors: $Path" }
  $functions = @($ast.Find({
      param($node)
      $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $Name
    }, $true))
  if ($functions.Count -ne 1) { throw "production script does not contain exactly one $Name function: $Path" }
  $body = $functions[0].Body.Extent.Text
  $body = $body.Substring(1, $body.Length - 2)
  Set-Item -Path "Function:\global:$Name" -Value ([scriptblock]::Create($body))
}

function Write-ChecksumFixture {
  param(
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string[]]$Lines
  )
  [IO.File]::WriteAllText($Path, [string]::Join("`r`n", $Lines), [Text.Encoding]::ASCII)
}

function Invoke-ChecksumAssertion {
  param(
    [Parameter(Mandatory)][string]$Implementation,
    [Parameter(Mandatory)][string]$Checksums,
    [Parameter(Mandatory)][string]$AssetPath,
    [Parameter(Mandatory)][string]$Asset
  )
  $expected = & 'Get-EcsChecksum' -ChecksumFile $Checksums -Asset $Asset
  if ($Implementation -eq 'run') {
    Assert-EcsSha256 -Path $AssetPath -Expected $expected -Label $Asset
  } else {
    Assert-EcsSha256 -Path $AssetPath -Expected $expected
  }
}

function Assert-ChecksumRejected {
  param(
    [Parameter(Mandatory)][string]$Name,
    [Parameter(Mandatory)][string]$Implementation,
    [Parameter(Mandatory)][string]$Checksums,
    [Parameter(Mandatory)][string]$AssetPath,
    [Parameter(Mandatory)][string]$Asset,
    [Parameter(Mandatory)][string]$MessagePattern
  )
  $message = $null
  try {
    Invoke-ChecksumAssertion -Implementation $Implementation -Checksums $Checksums -AssetPath $AssetPath -Asset $Asset
  } catch {
    $message = $_.Exception.Message
  }
  if ($null -eq $message) { throw "$Name unexpectedly accepted invalid checksum input" }
  if ($message -notmatch $MessagePattern) { throw "$Name failed for an unexpected reason: $message" }
}

function Test-ChecksumImplementation {
  param(
    [Parameter(Mandatory)][string]$Name,
    [Parameter(Mandatory)][string]$Implementation,
    [Parameter(Mandatory)][string]$Checksums,
    [Parameter(Mandatory)][string]$AssetPath,
    [Parameter(Mandatory)][string]$Asset,
    [Parameter(Mandatory)][string]$Digest,
    [Parameter(Mandatory)][string]$ValidEntry
  )
  Write-ChecksumFixture -Path $Checksums -Lines @('not-a-digest  unrelated.zip', $ValidEntry)
  Invoke-ChecksumAssertion -Implementation $Implementation -Checksums $Checksums -AssetPath $AssetPath -Asset $Asset

  Write-ChecksumFixture -Path $Checksums -Lines @("bad  $Asset")
  Assert-ChecksumRejected -Name "$Name malformed target digest" -Implementation $Implementation -Checksums $Checksums -AssetPath $AssetPath -Asset $Asset -MessagePattern 'SHA-256 mismatch'

  Write-ChecksumFixture -Path $Checksums -Lines @("bad  $Asset", "$Digest  $Asset")
  Assert-ChecksumRejected -Name "$Name malformed duplicate target" -Implementation $Implementation -Checksums $Checksums -AssetPath $AssetPath -Asset $Asset -MessagePattern 'exactly one|unique entry'

  Write-ChecksumFixture -Path $Checksums -Lines @("$Digest  $Asset", "$Digest  $Asset")
  Assert-ChecksumRejected -Name "$Name duplicate target" -Implementation $Implementation -Checksums $Checksums -AssetPath $AssetPath -Asset $Asset -MessagePattern 'exactly one|unique entry'

  Write-ChecksumFixture -Path $Checksums -Lines @('not-a-digest  another.zip')
  Assert-ChecksumRejected -Name "$Name missing target" -Implementation $Implementation -Checksums $Checksums -AssetPath $AssetPath -Asset $Asset -MessagePattern 'exactly one|unique entry'
}

try {
  Import-ProductionFunction -Path $runPath -Name 'Stop-EcsWindowsRun'
  Import-ProductionFunction -Path $runPath -Name 'Get-EcsChecksum'
  Import-ProductionFunction -Path $runPath -Name 'Assert-EcsSha256'
  $runAsset = Join-Path $fixtureRoot 'run asset.zip'
  $runChecksums = Join-Path $fixtureRoot 'run checksums.txt'
  [IO.File]::WriteAllBytes($runAsset, [Text.Encoding]::UTF8.GetBytes('real run artifact'))
  $runDigest = (Get-FileHash -Algorithm SHA256 -LiteralPath $runAsset).Hash
  Test-ChecksumImplementation -Name 'run.ps1' -Implementation 'run' -Checksums $runChecksums -AssetPath $runAsset -Asset 'asset.zip' -Digest $runDigest -ValidEntry ("  {0}  *asset.zip  " -f $runDigest.ToUpperInvariant())

  Import-ProductionFunction -Path $installPath -Name 'Stop-EcsWindowsInstall'
  Import-ProductionFunction -Path $installPath -Name 'Get-EcsChecksum'
  Import-ProductionFunction -Path $installPath -Name 'Assert-EcsSha256'
  $installAsset = Join-Path $fixtureRoot 'install asset.zip'
  $installChecksums = Join-Path $fixtureRoot 'install checksums.txt'
  [IO.File]::WriteAllBytes($installAsset, [Text.Encoding]::UTF8.GetBytes('real install artifact'))
  $installDigest = (Get-FileHash -Algorithm SHA256 -LiteralPath $installAsset).Hash.ToLowerInvariant()
  Test-ChecksumImplementation -Name 'install.ps1' -Implementation 'install' -Checksums $installChecksums -AssetPath $installAsset -Asset 'asset.zip' -Digest $installDigest -ValidEntry ("{0}  asset.zip" -f $installDigest)

  Write-Output 'Windows run.ps1/install.ps1 checksum selection tests passed using the production functions and real file hashes.'
} finally {
  Remove-Item -LiteralPath $fixtureRoot -Recurse -Force
}
