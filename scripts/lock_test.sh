#!/usr/bin/env bash
set -euo pipefail

# Deterministic contract test for tools/lock.json. The build/release scripts
# consume this file through common.sh; keep the validation close to the lock so
# a missing pin fails before a long tool build starts.

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source "$repo_root/scripts/lib/common.sh"

die() {
	echo "tools-lock: $*" >&2
	exit 1
}

[[ "$ECS_LOCK_SCHEMA_VERSION" == "ecs.tools.lock/v1" ]] || die "unexpected schema"
[[ "${#ECS_TARGETS[@]}" -eq 9 ]] || die "expected nine platform targets"
[[ "${#ECS_LINUX_TARGETS[@]}" -eq 7 ]] || die "expected seven Linux targets"
[[ "${#ECS_FREEBSD_TARGETS[@]}" -eq 2 ]] || die "expected two FreeBSD targets"
[[ "${#ECS_WINDOWS_TARGETS[@]}" -eq 1 ]] || die "expected one Windows target"
[[ "${#ECS_TOOL_NAMES[@]}" -eq 10 ]] || die "expected ten locked tools"

bundle_file="$repo_root/tools/BUNDLE"
[[ -f "$bundle_file" ]] || die "missing tools/BUNDLE"
[[ "$(wc -l < "$bundle_file")" -eq 1 ]] || die "tools/BUNDLE must contain exactly one line"
bundle_name=$(<"$bundle_file")
# bundle 版本线允许 bundle-vN(.M)*（如 bundle-v1.2）；与 run.sh 的
# bundle-v[0-9A-Za-z._+-]* 校验和 bundle-release.yml 的 bundle-v* tag 对齐。
[[ "$bundle_name" =~ ^bundle-v[1-9][0-9]*(\.[0-9]+)*$ ]] || die "invalid tools/BUNDLE"
grep -Fq -- "## $bundle_name" "$repo_root/tools/BUNDLE_NOTES.md" ||
	die "tools/BUNDLE has no matching release-notes section: $bundle_name"

jq -e '
  .windows_toolchain.distribution as $distribution |
  (.architectures | length == 10) and
  ([.architectures[].target] | length == 10 and length == (unique | length)) and
  (all(.architectures[]; (.target | test("^(linux|freebsd|windows)_[a-z0-9]+$")) and (.goos | IN("linux", "freebsd", "windows")))) and
  ([.architectures[] | select(.goos == "linux")] | length == 7) and
  ([.architectures[] | select(.goos == "freebsd")] | length == 2) and
  ([.architectures[] | select(.goos == "windows")] | length == 1 and .[0].target == "windows_amd64" and .[0].goarch == "amd64" and .[0].package == "amd64" and .[0].openssl_target == "mingw64") and
  ([.architectures[] | select(.goos == "freebsd") | .goarch] | sort == ["amd64", "arm64"]) and
  ([.architectures[] | select(.goos == "freebsd") | .openssl_target] | sort == ["BSD-aarch64", "BSD-x86_64"]) and
  ([.architectures[] | select(.goos == "linux") | .package] | length == 7 and length == (unique | length)) and
  (.tools | length == 10) and
  ([.tools[].name] | length == 10 and length == (unique | length)) and
  (all(.tools[]; (.name | length > 0) and (.upstream | startswith("http")))) and
  (.windows_tools == ["zstd", "npb-ep", "npb-ft", "openssl", "stream", "fio", "nexttrace-tiny"]) and
  ((.windows_tools | length) == (.windows_tools | unique | length)) and
  (([.windows_tools[]] - [.tools[].name]) | length == 0) and
  ([.windows_tools[] | select(. == "ping" or . == "sysbench" or . == "iperf3")] | length == 0) and
  (.windows_toolchain.environment == "MSYS2 UCRT64") and
  (.windows_toolchain.distribution.version == "2025-08-30") and
  (.windows_toolchain.distribution.source_url | startswith("https://")) and
  (.windows_toolchain.distribution.source_sha256 | test("^[0-9a-f]{64}$")) and
  ([.windows_toolchain.base_packages[].name] == ["base", "bash", "coreutils", "gawk", "grep", "sed", "pacman", "perl", "libintl", "libiconv", "msys2-runtime", "filesystem", "zstd", "zlib"]) and
  (all(.windows_toolchain.base_packages[]; (.source_url == $distribution.source_url) and (.source_sha256 == $distribution.source_sha256) and (.version | length > 0) and (.license | length > 0))) and
  ([.windows_toolchain.packages[].name] == ["mingw-w64-ucrt-x86_64-gcc", "mingw-w64-ucrt-x86_64-gcc-fortran", "mingw-w64-ucrt-x86_64-gcc-libs", "mingw-w64-ucrt-x86_64-gcc-libgfortran", "mingw-w64-ucrt-x86_64-libwinpthread", "mingw-w64-ucrt-x86_64-nasm", "make", "mingw-w64-ucrt-x86_64-binutils", "mingw-w64-ucrt-x86_64-crt", "mingw-w64-ucrt-x86_64-headers", "mingw-w64-ucrt-x86_64-isl", "mingw-w64-ucrt-x86_64-gmp", "mingw-w64-ucrt-x86_64-mpfr", "mingw-w64-ucrt-x86_64-mpc", "mingw-w64-ucrt-x86_64-windows-default-manifest", "mingw-w64-ucrt-x86_64-winpthreads", "mingw-w64-ucrt-x86_64-zlib", "mingw-w64-ucrt-x86_64-zstd", "mingw-w64-ucrt-x86_64-tzdata", "mingw-w64-ucrt-x86_64-gettext-runtime", "mingw-w64-ucrt-x86_64-libiconv"]) and
  (all(.windows_toolchain.packages[]; ((.name == "make") or (.name | test("^mingw-w64-ucrt-x86_64-"))) and (.version | length > 0) and (.source_url | test("^https://repo\\.msys2\\.org/(mingw/ucrt64|msys/x86_64)/[^/]+\\.pkg\\.tar\\.zst$")) and (.source_sha256 | test("^[0-9a-f]{64}$")) and (.upstream | startswith("https://")) and (.license | length > 0) and (.depends | type == "array") and all(.depends[]; type == "string"))) and
  ([.windows_toolchain.packages[] | .name] + [.windows_toolchain.base_packages[] | .name] + [.windows_toolchain.packages[] | .provides[]?] + [.windows_toolchain.base_packages[] | .provides[]?]) as $provided |
  (all(.windows_toolchain.packages[] | .depends[]?; ((split("=")[0]) as $dependency | ($provided | index($dependency)) != null))) and
  (any(.windows_toolchain.packages[]; .name == "mingw-w64-ucrt-x86_64-gcc" and .version == "16.2.0-3")) and
  (any(.windows_toolchain.packages[]; .name == "mingw-w64-ucrt-x86_64-gcc-fortran" and .version == "16.2.0-3")) and
  (any(.windows_toolchain.packages[]; .name == "mingw-w64-ucrt-x86_64-gcc-libs" and (.runtime_components | index("libgomp")) != null)) and
  (any(.windows_toolchain.packages[]; .name == "mingw-w64-ucrt-x86_64-nasm" and .version == "3.02-1")) and
  (any(.windows_toolchain.packages[]; .name == "make" and .version == "4.4.1-3")) and
  (.windows_toolchain.build_flags.c == ["-O3", "-ffunction-sections", "-fdata-sections", "-D_WIN32_WINNT=0x0601"]) and
  (.windows_toolchain.build_flags.fortran == ["-O3", "-fopenmp"]) and
  (.windows_toolchain.build_flags.linker == ["-static", "-static-libgcc", "-Wl,--gc-sections"]) and
  (.windows_toolchain.build_flags.fortran_linker == ["-fopenmp", "-static-libgfortran"]) and
  ([.windows_toolchain.build_flags.c[], .windows_toolchain.build_flags.fortran[], .windows_toolchain.build_flags.linker[], .windows_toolchain.build_flags.fortran_linker[]] | all(. != "-static-libgomp" and . != "-static-libwinpthread")) and
  ([.windows_dll_allowlist[]] | length > 0 and all(. | test("^[A-Za-z0-9*_.-]+$"))) and
  (all(.tools[] | select(.repository != null); (.tag | length > 0) and (.commit | test("^[0-9a-f]{40}$")))) and
  ((.tools[] | select(.name == "nexttrace-tiny") | .asset_sha256) as $digests |
    ($digests | type == "object") and
    (($digests | keys | sort) == ([.architectures[] | select(.goos == "linux") | .package] | sort)) and
    (all($digests[]; test("^[0-9a-f]{64}$"))) and
    ($digests == {
      "amd64": "093849f1012b065c29d307b8e47fedec667206829c14e105f83a852f60c628d1",
      "arm64": "8b134f6c6a7864b1ecc98b1f7cfae1d058ef6dcf8f0da862e3260752ce1858bd",
      "armv7": "71014f2707372cee22ab80f546aa6cff79d869faab0fb516005e8bb0e2d2f000",
      "386": "ae188b8f4fb3f5fec70ddf4cf5adc4391d9536698ed29c0d4f25b3b4dd29ca34",
      "s390x": "64c80d850b06d09bfc1b154fabef90a7fdcf6856f5d0236877ab47ed12d359fb",
      "riscv64": "0bd74a31f399c799446670716d0a2c372dbc0909bc591af72961e6afde415912",
      "ppc64le": "a09d7a689ac53a6aac50378e9bbd0cac8dc16bfcdfa98941a14780417a2513d2"
    })) and
  ((.tools[] | select(.name == "nexttrace-tiny")) as $nexttrace |
    ($nexttrace.repository == "nxtrace/NTrace-core") and
    ($nexttrace.version == "1.7.1") and
    ($nexttrace.tag == "v1.7.1") and
    ($nexttrace.commit == "c9919828fcd8c3103827d08bb26d69e9bf538299") and
    ($nexttrace.windows_asset_pattern == "nexttrace-tiny_windows_<architecture>.exe") and
    ($nexttrace.windows_asset_pattern | test("latest"; "i") | not) and
    ($nexttrace.windows_asset_sha256 | type == "object") and
    ($nexttrace.windows_asset_sha256.amd64 == "16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b") and
    ($nexttrace.windows_asset_sha256.amd64 | test("^[0-9a-f]{64}$")) and
    (("https://github.com/" + $nexttrace.repository + "/releases/download/" + $nexttrace.tag + "/" + ($nexttrace.windows_asset_pattern | gsub("<architecture>"; "amd64"))) == "https://github.com/nxtrace/NTrace-core/releases/download/v1.7.1/nexttrace-tiny_windows_amd64.exe")) and
  (.corpus.name == "ecs-silesia-v1.corpus") and
  (.corpus.bytes == 211938580) and
  (.corpus.sha256 | test("^[0-9a-f]{64}$")) and
  (.corpus.source_sha256 | test("^[0-9a-f]{64}$")) and
  (.corpus.order | length == 12)
' "$ECS_LOCK_FILE" >/dev/null || die "lock contents failed the schema invariants"

if grep -Eq -- '--nodeps|-Sy|-S( |$)' "$repo_root/scripts/build_tools_windows.ps1"; then
	die "Windows builder must install only the locked local package transaction"
fi

windows_builder="$repo_root/scripts/build_tools_windows.ps1"
if grep -Eiq 'releases/latest|latest/download|fallback' "$windows_builder"; then
	die "Windows NextTrace builder must not use latest or fallback sources"
fi
for required_prebuilt_fact in Save-EcsVerifiedDownload windows_asset_pattern windows_asset_sha256 source_mode upstream_sha256 packaged_sha256; do
	grep -Fq -- "$required_prebuilt_fact" "$windows_builder" || die "Windows builder is missing prebuilt fact: $required_prebuilt_fact"
done
grep -Fq -- 'foreach ($name in $sourceBuiltToolNames) {' "$windows_builder" ||
	die "Windows builder must scope strip to source-built tools"

freebsd_tool_names=()
mapfile -t freebsd_tool_names < <(ecs_target_tool_names freebsd_amd64)
for forbidden_tool in ping nexttrace-tiny; do
	if printf '%s\n' "${freebsd_tool_names[@]}" | grep -Fqx -- "$forbidden_tool"; then
		die "FreeBSD bundle unexpectedly contains $forbidden_tool"
	fi
done

windows_gate="$repo_root/scripts/ci/windows_tools_gate.ps1"
for required_gate_fact in CreateJobObjectW SetInformationJobObject AssignProcessToJobObject JobObjectLimitKillOnJobClose ReadToEndAsync; do
	grep -Fq "$required_gate_fact" "$windows_gate" || die "Windows gate is missing required process-tree/output contract: $required_gate_fact"
done
for required_nexttrace_gate_fact in 'NextTrace verified-upstream-prebuilt metadata or byte hash mismatch' 'source_mode' 'upstream_sha256' 'packaged_sha256' 'NextTrace network gate=not run'; do
	grep -Fq -- "$required_nexttrace_gate_fact" "$windows_gate" || die "Windows gate is missing NextTrace fail-closed fact: $required_nexttrace_gate_fact"
done
if grep -Eq '\.Kill\(|Stop-Process|taskkill' "$windows_gate"; then
	die "Windows gate must not use root-process or shell process-tree termination"
fi

nexttrace_gate="$repo_root/scripts/ci/windows_nexttrace_gate.ps1"
[[ -f "$nexttrace_gate" ]] || die "Windows NextTrace production gate is missing"
for required_nexttrace_gate_fact in \
	'ECS_TOOL_BIN' \
	'plan --lang en --only route' \
	'plan --lang en --only backtrace' \
	'run --lang en --only route --format json' \
	'run --lang en --only backtrace --format json' \
	"result.status -notin @('ok', 'warning')" \
	'nexttrace-json-v1' \
	'--queries' \
	'--parallel-requests' \
	'--timeout' \
	'-M' \
	'responded' \
	"'ip'" \
	'respondingHops' \
	'no actual responding hop' \
	'Get-NetIPAddress' \
	'Get-NetRoute' \
	'NextTrace IPv4 canonical gate passed' \
	'not-tested capability=missing' \
	'16e13532f6e8ee75f63db61a6a98fe1ca217b5431b76531c8c5d4bcdbe7e6f9b'; do
	grep -Fq -- "$required_nexttrace_gate_fact" "$nexttrace_gate" ||
		die "Windows NextTrace production gate is missing fact: $required_nexttrace_gate_fact"
done
for forbidden_nexttrace_gate_fact in tracert Test-NetConnection '|| true' 'continue-on-error' 't.Skip' 'host PATH fallback' 'fake binary' 'hops.Count -lt 1'; do
	if grep -Fq -- "$forbidden_nexttrace_gate_fact" "$nexttrace_gate"; then
		die "Windows NextTrace production gate contains forbidden fallback/bypass: $forbidden_nexttrace_gate_fact"
	fi
done

nexttrace_report_assert="$repo_root/scripts/ci/windows_nexttrace_report_assert.ps1"
[[ -f "$nexttrace_report_assert" ]] || die "Windows NextTrace bootstrap report assertion helper is missing"
for required_report_assert_fact in \
	'ecs.report/v1' \
	'result IDs are missing or not unique' \
	'evidence' \
	'unsupported' \
	'tool_missing' \
	'parse_error' \
	'nexttrace-json-v1' \
	'1.7.1' \
	'--no-color' \
	'--json' \
	'--queries' \
	'--parallel-requests' \
	'--timeout' \
	'-M' \
	'--max-hops' \
	'probe.route.source.nexttrace.name' \
	'probe.route.normalized_trace_json' \
	'probe.backtrace.normalized_trace_json' \
	'hops' \
	'responded' \
	"'ip'" \
	'respondingHops' \
	'no actual responding hop'; do
	grep -Fq -- "$required_report_assert_fact" "$nexttrace_report_assert" ||
		die "Windows NextTrace bootstrap report assertion is missing fact: $required_report_assert_fact"
done
for forbidden_report_assert_fact in tracert Test-NetConnection '|| true' 'continue-on-error' 't.Skip' 'host PATH fallback' 'fake binary' 'hops.Count -lt 1'; do
	if grep -Fq -- "$forbidden_report_assert_fact" "$nexttrace_report_assert"; then
		die "Windows NextTrace bootstrap report assertion contains forbidden fallback/bypass: $forbidden_report_assert_fact"
	fi
done

grep -Fq -- '-CheckOrdinaryUser validates no-admin-operation only' "$windows_gate" ||
	die "Windows package gate must report the no-admin-operation contract"
grep -Fq -- 'ordinary-user token execution is not claimed' "$windows_gate" ||
	die "Windows package gate must not claim ordinary-user token execution"

if grep -Fq 'tools/lock.json' "$repo_root/run.sh"; then
	die "public run.sh must not depend on the repository tools lock"
fi

echo "tools-lock: lock schema and build facts are valid"
