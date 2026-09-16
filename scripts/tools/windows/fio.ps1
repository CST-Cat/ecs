Set-StrictMode -Version Latest

function Build-EcsWindowsFio {
    param([Parameter(Mandatory)][pscustomobject]$Context)

    $tool = $Context.Tools['fio']
    $source = ConvertTo-EcsMsysPath $Context.Sources['fio']
    $output = ConvertTo-EcsMsysPath $Context.Binaries['fio']
    $configureFlags = @($Context.Toolchain.build_flags.fio)
    $flagString = $configureFlags -join ' '
    $script = @"
$($Context.Preamble)
set -eu
cd $(ConvertTo-EcsBashLiteral $source)
./configure $flagString
grep -Fx 'CONFIG_WINDOWSAIO=y' config-host.mak
if grep -Eq '^CONFIG_(RBD|RADOS|GFAPI|RDMA)=y$' config-host.mak; then
  echo 'fio generated a forbidden external engine' >&2
  exit 1
fi
make -j$($Context.Jobs)
cp fio.exe $(ConvertTo-EcsBashLiteral $output)
test -s $(ConvertTo-EcsBashLiteral $output)
"@
    Invoke-EcsWindowsBash -Context $Context -Script $script
    $Context.BuildFacts['fio'] = [ordered]@{
        build_flags = @($configureFlags) + @('generated-config: require CONFIG_WINDOWSAIO=y', 'generated-config: omit external engines')
        source_commit = [string]$tool.commit
        enabled_engine = 'windowsaio'
        disabled_engines = @('rbd', 'rados', 'gfapi', 'rdma')
        license = 'GPL-2.0-only'
    }
}
