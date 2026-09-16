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
  (.windows_tools == ["zstd", "npb-ep", "npb-ft", "openssl", "stream", "fio"]) and
  (([.windows_tools[]] - [.tools[].name]) | length == 0) and
  ([.windows_tools[] | select(. == "nexttrace-tiny" or . == "ping" or . == "sysbench" or . == "iperf3")] | length == 0) and
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
    (all($digests[]; test("^[0-9a-f]{64}$")))) and
  (.corpus.name == "ecs-silesia-v1.corpus") and
  (.corpus.bytes == 211938580) and
  (.corpus.sha256 | test("^[0-9a-f]{64}$")) and
  (.corpus.source_sha256 | test("^[0-9a-f]{64}$")) and
  (.corpus.order | length == 12)
' "$ECS_LOCK_FILE" >/dev/null || die "lock contents failed the schema invariants"

if grep -Eq -- '--nodeps|-Sy|-S( |$)' "$repo_root/scripts/build_tools_windows.ps1"; then
	die "Windows builder must install only the locked local package transaction"
fi

windows_gate="$repo_root/scripts/ci/windows_tools_gate.ps1"
for required_gate_fact in CreateJobObjectW SetInformationJobObject AssignProcessToJobObject JobObjectLimitKillOnJobClose ReadToEndAsync; do
	grep -Fq "$required_gate_fact" "$windows_gate" || die "Windows gate is missing required process-tree/output contract: $required_gate_fact"
done
if grep -Eq '\.Kill\(|Stop-Process|taskkill' "$windows_gate"; then
	die "Windows gate must not use root-process or shell process-tree termination"
fi

if grep -Fq 'tools/lock.json' "$repo_root/run.sh"; then
	die "public run.sh must not depend on the repository tools lock"
fi

echo "tools-lock: lock schema and build facts are valid"
