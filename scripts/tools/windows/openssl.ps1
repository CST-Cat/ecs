Set-StrictMode -Version Latest

function Build-EcsWindowsOpenSSL {
    param([Parameter(Mandatory)][pscustomobject]$Context)

    $tool = $Context.Tools['openssl']
    $source = ConvertTo-EcsMsysPath $Context.Sources['openssl']
    $output = ConvertTo-EcsMsysPath $Context.Binaries['openssl']
    $prefix = ConvertTo-EcsMsysPath (Join-Path $Context.WorkRoot 'openssl-prefix')
    $configureFlags = @($Context.Toolchain.build_flags.openssl) + @(
        "--prefix=$prefix",
        "--openssldir=$prefix/ssl"
    )
    $flagString = $configureFlags -join ' '
    $script = @"
$($Context.Preamble)
set -eu
cd $(ConvertTo-EcsBashLiteral $source)
perl ./Configure $flagString
make -j$($Context.Jobs) build_generated
make -j$($Context.Jobs) apps/openssl
cp apps/openssl.exe $(ConvertTo-EcsBashLiteral $output)
test -s $(ConvertTo-EcsBashLiteral $output)
"@
    Invoke-EcsWindowsBash -Context $Context -Script $script
    $Context.BuildFacts['openssl'] = [ordered]@{
        build_flags = @($configureFlags)
        source_commit = [string]$tool.commit
        configure_target = [string]$Context.TargetFacts.openssl_target
        license = 'Apache-2.0'
    }
}
