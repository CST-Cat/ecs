$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
. (Join-Path $repoRoot 'scripts/tools/windows/common.ps1')

$pythonName = if ($env:OS -eq 'Windows_NT') { 'python' } else { 'python3' }
$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('ecs-windows-download-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $testRoot | Out-Null
$serverScript = Join-Path $testRoot 'server.py'
$serverReady = Join-Path $testRoot 'server.ready'
$serverAccessLog = Join-Path $testRoot 'server.access.log'
$serverProcess = $null

try {
    $pythonServer = @'
from http.server import HTTPServer, SimpleHTTPRequestHandler
from pathlib import Path
import os

root = Path.cwd()
access_log = root / "server.access.log"

class Handler(SimpleHTTPRequestHandler):
    def log_message(self, fmt, *args):
        with access_log.open("a", encoding="utf-8") as stream:
            stream.write((fmt % args) + "\n")

os.chdir(root)
server = HTTPServer(("127.0.0.1", 0), Handler)
(root / "server.ready").write_text(str(server.server_port), encoding="ascii")
try:
    server.serve_forever()
finally:
    server.server_close()
'@
    Set-Content -LiteralPath $serverScript -Value $pythonServer -Encoding utf8

    $source = Join-Path $testRoot 'source.bin'
    $matchingDestination = Join-Path $testRoot 'matching.bin'
    $mismatchDestination = Join-Path $testRoot 'mismatch.bin'
    [IO.File]::WriteAllText($source, 'real loopback download fixture')
    $sourceBytes = [IO.File]::ReadAllBytes($source)
    $expected = (Get-FileHash -Algorithm SHA256 -LiteralPath $source).Hash.ToLowerInvariant()
    $wrongHash = '0000000000000000000000000000000000000000000000000000000000000000'
    $serverProcess = Start-Process -FilePath $pythonName -ArgumentList 'server.py' -WorkingDirectory $testRoot `
        -RedirectStandardOutput (Join-Path $testRoot 'server.stdout') `
        -RedirectStandardError (Join-Path $testRoot 'server.stderr') -PassThru
    $deadline = [DateTime]::UtcNow.AddSeconds(10)
    while (-not (Test-Path -LiteralPath $serverReady -PathType Leaf)) {
        if ($serverProcess.HasExited) {
            throw "loopback HTTP server exited: $(Get-Content -Raw -LiteralPath (Join-Path $testRoot 'server.stderr'))"
        }
        if ([DateTime]::UtcNow -ge $deadline) {
            throw 'loopback HTTP server did not become ready within 10 seconds'
        }
        Start-Sleep -Milliseconds 50
    }
    $port = [int](Get-Content -Raw -LiteralPath $serverReady)
    $uri = "http://127.0.0.1:$port/source.bin"

    Save-EcsVerifiedDownload -Uri $uri -Sha256 $expected -Destination $matchingDestination -Description 'matching loopback fixture'
    $downloadedBytes = [IO.File]::ReadAllBytes($matchingDestination)
    if (-not [System.Linq.Enumerable]::SequenceEqual[byte]($sourceBytes, $downloadedBytes)) {
        throw 'matching loopback download changed the served bytes'
    }

    $mismatchError = $null
    try {
        Save-EcsVerifiedDownload -Uri $uri -Sha256 $wrongHash -Destination $mismatchDestination -Description 'wrong-hash loopback fixture'
    } catch {
        $mismatchError = $_.Exception.Message
    }
    if ($null -eq $mismatchError -or $mismatchError -notmatch 'SHA-256 mismatch') {
        throw "wrong-hash loopback download did not fail with a mismatch: $mismatchError"
    }
    if (Test-Path -LiteralPath $mismatchDestination) {
        throw 'wrong-hash loopback download retained its destination file'
    }

    $getCount = @([IO.File]::ReadAllLines($serverAccessLog) | Where-Object { $_ -match '"GET /source\.bin HTTP/1\.1" 200' }).Count
    if ($getCount -ne 2) {
        throw "expected one HTTP request per download, got $getCount"
    }

    Write-Output 'Windows verified-download loopback tests passed (standard-library HTTP server and Get-FileHash)'
} finally {
    if ($null -ne $serverProcess -and -not $serverProcess.HasExited) {
        Stop-Process -Id $serverProcess.Id -Force
        [void]$serverProcess.WaitForExit(5000)
    }
    Remove-Item -Force -Recurse -LiteralPath $testRoot
}
