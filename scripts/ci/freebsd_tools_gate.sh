#!/usr/bin/env bash
set -Eeuo pipefail

# Real FreeBSD runtime gate for the merged FreeBSD tools stage (Stage 8).
#
# This script is the consumer/judge of the stage artifact produced by
# freebsd-tools.yml: it runs INSIDE the real FreeBSD 15.1 VM
# (vmactions/freebsd-vm, full-system emulation, never qemu-user) and executes
# the 8 staged static FreeBSD binaries for real:
#
#   sysbench  --version against the lock + real 5s CPU smoke with throughput
#   zstd      --version against the lock + real compress -> decompress ->
#             byte-identical roundtrip
#   npb-ep    real NPB EP Class A run printing Verification SUCCESSFUL
#   npb-ft    real NPB FT Class A run printing Verification SUCCESSFUL
#   openssl   version against the lock + real 1s AES-256-GCM speed smoke
#   stream    real run of the binary as built (locked STREAM_ARRAY_SIZE /
#             NTIMES, no parameters are changed) printing "Solution Validates"
#             with the locked array size
#   fio       --version against the lock + posixaio QD32 and QD64 rounds whose
#             JSON iodepth_level distribution proves effective depth > 1
#             really happened (both rounds verified separately)
#   iperf3    --version against the lock + real loopback server/client with
#             valid JSON and a successful sum end block
#
# Hard rules this gate enforces on itself: no compiling in the VM, no
# downloading replacement binaries, no pkg-installing any tool under test and
# no fallback to system packages on failure. Every verdict comes from the
# staged binaries only. One PASS/FAIL line per tool, a final N/8 summary and a
# non-zero exit on any failure.

usage() {
  cat >&2 <<'USAGE'
usage: scripts/ci/freebsd_tools_gate.sh --target freebsd_amd64|freebsd_arm64
                                        --stage-dir DIR
USAGE
}

die() {
  echo "freebsd-tools-gate: $*" >&2
  exit 1
}

export PATH=/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin:/bin:/sbin

target=""
stage_dir=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--target requires a value"
      target=$2
      shift 2
      ;;
    --stage-dir)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--stage-dir requires a value"
      stage_dir=$2
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage
      die "unknown option: $1"
      ;;
  esac
done

[[ -n "$target" && -n "$stage_dir" ]] || { usage; exit 2; }

case "$target" in
  freebsd_amd64 | freebsd_arm64) ;;
  *) die "--target must be freebsd_amd64 or freebsd_arm64, got $target" ;;
esac
[[ "$stage_dir" = /* ]] || die "--stage-dir must be an absolute path: $stage_dir"

# ---- guest identity: the gate only judges on the real FreeBSD VM -----------

[[ "$(uname -s)" == FreeBSD ]] || die "gate must run on FreeBSD, got $(uname -s)"
guest_arch=$(uname -m)
case "$target:$guest_arch" in
  freebsd_amd64:amd64 | freebsd_amd64:x86_64 | \
    freebsd_arm64:arm64 | freebsd_arm64:aarch64) ;;
  *) die "guest arch $guest_arch does not match target $target" ;;
esac

for gate_command in bash jq file; do
  command -v "$gate_command" >/dev/null 2>&1 ||
    die "missing gate command: $gate_command (workflow prepare installs bash jq file ca_root_nss)"
done

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
lock_file="$repo_root/tools/lock.json"
[[ -s "$lock_file" ]] || die "missing tools lock: $lock_file"

lock_version() {
  jq -er --arg tool "$1" '.tools[] | select(.name == $tool) | .version' "$lock_file" ||
    die "tools lock has no version for $1"
}

echo "freebsd-tools-gate: guest=$(uname -s)/$guest_arch kernel=$(uname -r) target=$target"
freebsd-version || true
uname -a

# ---- stage identity: restore exec bits, prove these are our static ELFs ----

bin_dir="$stage_dir/bin"
[[ -d "$bin_dir" ]] || die "staged bin directory is missing: $bin_dir"
[[ -s "$stage_dir/manifest.json" ]] || die "staged manifest is missing: $stage_dir/manifest.json"

tools=(sysbench zstd npb-ep npb-ft openssl stream fio iperf3)

case "$target" in
  freebsd_amd64) machine_token='x86-64' ;;
  freebsd_arm64) machine_token='aarch64' ;;
esac

for tool in "${tools[@]}"; do
  binary="$bin_dir/$tool"
  [[ -f "$binary" && -s "$binary" ]] || die "staged $tool is missing or empty: $binary"
  # upload-artifact v4 drops permission bits on the artifact round trip;
  # restoring 0755 is not a rebuild - the bytes stay exactly as merged.
  chmod 0755 "$binary"
  identity=$(file -b "$binary")
  echo "$tool: $identity"
  case "$identity" in
    *FreeBSD*) ;;
    *) die "staged $tool is not a FreeBSD ELF: $identity" ;;
  esac
  case "$identity" in
    *static*) ;;
    *) die "staged $tool is not statically linked: $identity" ;;
  esac
  case "$identity" in
    *"$machine_token"*) ;;
    *) die "staged $tool is not a $machine_token ELF: $identity" ;;
  esac
done

work="${TMPDIR:-/tmp}/ecs-freebsd-tools-gate"
rm -rf -- "$work"
mkdir -p "$work"

declare -A verdicts=()
failed=0

verdict() {
  local tool=$1 rc=$2
  if [[ "$rc" -eq 0 ]]; then
    verdicts[$tool]=PASS
    echo "[PASS] $tool"
  else
    verdicts[$tool]=FAIL
    echo "[FAIL] $tool (reason printed above)" >&2
    failed=$((failed + 1))
  fi
}

# Every check runs with errexit suspended (called in a condition context), so
# each fallible step handles its own error explicitly and returns 1.

check_sysbench() {
  local bin="$bin_dir/sysbench" ver vout eps smoke
  ver=$(lock_version sysbench)
  vout=$("$bin" --version 2>&1) || { echo "sysbench --version failed" >&2; return 1; }
  printf '%s\n' "$vout"
  grep -Eq "^sysbench ${ver//./\\.}([[:space:]]|\$)" <<<"$vout" ||
    { echo "sysbench --version did not report the locked $ver" >&2; return 1; }

  smoke="$work/sysbench-cpu.log"
  "$bin" cpu --time=5 run >"$smoke" 2>&1 ||
    { echo "sysbench cpu smoke failed" >&2; cat "$smoke" >&2; return 1; }
  grep -q 'events per second' "$smoke" ||
    { echo "sysbench cpu smoke has no throughput output" >&2; cat "$smoke" >&2; return 1; }
  eps=$(awk -F: '/events per second/ { gsub(/[[:space:]]/, "", $2); print $2; exit }' "$smoke")
  [[ -n "$eps" ]] ||
    { echo "could not parse sysbench events per second" >&2; cat "$smoke" >&2; return 1; }
  awk -v v="$eps" 'BEGIN { exit !(v + 0 > 0) }' ||
    { echo "sysbench cpu throughput is not positive: $eps events/s" >&2; cat "$smoke" >&2; return 1; }
  echo "sysbench cpu smoke: $eps events/s"
}

check_zstd() {
  local bin="$bin_dir/zstd" ver vout src zst out
  ver=$(lock_version zstd)
  vout=$("$bin" --version 2>&1) || { echo "zstd --version failed" >&2; return 1; }
  printf '%s\n' "$vout"
  grep -Eq "v${ver//./\\.}([[:space:],]|\$)" <<<"$vout" ||
    { echo "zstd --version did not report the locked $ver" >&2; return 1; }

  src="$work/zstd.src"
  zst="$work/zstd.zst"
  out="$work/zstd.out"
  dd if=/dev/urandom of="$work/zstd-rand.bin" bs=1048576 count=2 >/dev/null 2>&1 ||
    { echo "creating zstd random input failed" >&2; return 1; }
  dd if=/dev/zero of="$work/zstd-zero.bin" bs=1048576 count=6 >/dev/null 2>&1 ||
    { echo "creating zstd zero input failed" >&2; return 1; }
  cat "$work/zstd-rand.bin" "$work/zstd-zero.bin" >"$src"

  "$bin" -q -f "$src" -o "$zst" || { echo "zstd compression failed" >&2; return 1; }
  "$bin" -d -q -f "$zst" -o "$out" || { echo "zstd decompression failed" >&2; return 1; }
  cmp -s "$src" "$out" || { echo "zstd roundtrip is not byte-identical" >&2; return 1; }
  echo "zstd roundtrip: $(wc -c <"$src") bytes -> $(wc -c <"$zst") compressed -> identical"
}

npb_check() {
  local name=$1 bin="$bin_dir/$1" log
  log="$work/$name.log"
  "$bin" >"$log" 2>&1 || { echo "$name execution failed" >&2; cat "$log" >&2; return 1; }
  grep -Eiq 'Verification[[:space:]]*=[[:space:]]*SUCCESSFUL' "$log" ||
    { echo "$name did not verify successfully" >&2; cat "$log" >&2; return 1; }
  grep -E 'Verification' "$log"
}

check_npb_ep() { npb_check npb-ep; }
check_npb_ft() { npb_check npb-ft; }

check_openssl() {
  local bin="$bin_dir/openssl" ver vout log
  ver=$(lock_version openssl)
  vout=$("$bin" version 2>&1) || { echo "openssl version failed" >&2; return 1; }
  printf '%s\n' "$vout"
  grep -Eq "OpenSSL ${ver//./\\.}([[:space:]]|\$)" <<<"$vout" ||
    { echo "openssl version did not report the locked $ver" >&2; return 1; }

  log="$work/openssl-speed.log"
  "$bin" speed -seconds 1 -elapsed -evp aes-256-gcm >"$log" 2>&1 ||
    { echo "openssl speed aes-256-gcm smoke failed" >&2; cat "$log" >&2; return 1; }
  grep -Eq 'aes-256-gcm' "$log" ||
    { echo "openssl speed output lacks aes-256-gcm results" >&2; cat "$log" >&2; return 1; }
  echo "openssl speed aes-256-gcm smoke completed:"
  tail -n 6 "$log"
}

check_stream() {
  local bin="$bin_dir/stream" log array_size
  array_size=$(jq -er '.tools[] | select(.name == "stream") | .array_size' "$lock_file") ||
    die "tools lock has no stream array_size"
  log="$work/stream.log"
  # No CLI parameters exist or are passed: the binary runs exactly as built
  # with the locked STREAM_ARRAY_SIZE / NTIMES compiled in.
  "$bin" >"$log" 2>&1 || { echo "stream execution failed" >&2; cat "$log" >&2; return 1; }
  grep -Fq 'Solution Validates' "$log" ||
    { echo "stream did not validate" >&2; cat "$log" >&2; return 1; }
  grep -Fq "Array size = $array_size (elements)" "$log" ||
    { echo "stream did not run the locked STREAM_ARRAY_SIZE=$array_size" >&2; cat "$log" >&2; return 1; }
  grep -E '^(Copy|Scale|Add|Triad):' "$log"
  grep -F 'Solution Validates' "$log"
}

check_fio() {
  local bin="$bin_dir/fio" ver vout data depth json
  ver=$(lock_version fio)
  vout=$("$bin" --version 2>&1) || { echo "fio --version failed" >&2; return 1; }
  printf '%s\n' "$vout"
  grep -Eq "^fio-${ver//./\\.}([[:space:]]|\$)" <<<"$vout" ||
    { echo "fio --version did not report the locked $ver" >&2; return 1; }

  data="$work/fio-smoke.data"
  dd if=/dev/zero of="$data" bs=1048576 count=64 >/dev/null 2>&1 ||
    { echo "creating fio smoke data failed" >&2; return 1; }

  for depth in 32 64; do
    json="$work/fio-qd${depth}.json"
    "$bin" --name="ecs-gate-qd${depth}" --filename="$data" \
      --rw=read --bs=4k --size=16m --runtime=2 --time_based=1 \
      --ioengine=posixaio --iodepth="$depth" --numjobs=1 --direct=1 \
      --output-format=json --output="$json" ||
      { echo "fio posixaio QD${depth} run failed" >&2; cat "$json" 2>/dev/null >&2; return 1; }
    # The verdict comes from the observed iodepth_level distribution, not from
    # the configured iodepth: at least one bucket other than depth 1 must have
    # recorded samples, proving depth > 1 really occurred.
    jq -e --argjson depth "$depth" \
      '(.jobs | length == 1) and
       (.jobs[0].error == 0) and
       (.jobs[0]["job options"].ioengine == "posixaio") and
       ((.jobs[0]["job options"].iodepth | tonumber) == $depth) and
       ([.jobs[0].iodepth_level | to_entries[] | select(.key != "1") | .value] | any(. > 0))' \
      "$json" >/dev/null ||
      { echo "fio posixaio QD${depth} did not prove effective queue depth > 1" >&2;
        cat "$json" >&2
        return 1; }
    echo "fio posixaio QD${depth}: effective depth > 1 proven from iodepth_level"
  done
}

check_iperf3() {
  local bin="$bin_dir/iperf3" ver vout
  ver=$(lock_version iperf3)
  vout=$("$bin" --version 2>&1) || { echo "iperf3 --version failed" >&2; return 1; }
  printf '%s\n' "$vout"
  grep -Eq "^iperf ${ver//./\\.}([[:space:]]|\$)" <<<"$vout" ||
    { echo "iperf3 --version did not report the locked $ver" >&2; return 1; }

  local port=15201
  local server_json="$work/iperf3-server.json" server_err="$work/iperf3-server.err"
  local client_json="$work/iperf3-client.json"
  local spid ready client_rc=0

  "$bin" -s -p "$port" >"$server_json" 2>"$server_err" &
  spid=$!
  stop_server() {
    kill "$spid" >/dev/null 2>&1 || true
    wait "$spid" 2>/dev/null || true
  }

  ready=0
  if command -v nc >/dev/null 2>&1; then
    for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25; do
      if nc -z 127.0.0.1 "$port" >/dev/null 2>&1; then
        ready=1
        break
      fi
      sleep 0.2
    done
  else
    sleep 2
    ready=1
  fi
  if [[ "$ready" -ne 1 ]]; then
    stop_server
    echo "iperf3 loopback server did not become ready" >&2
    cat "$server_err" >&2
    return 1
  fi

  "$bin" -c 127.0.0.1 -p "$port" -J -t 3 >"$client_json" 2>&1 || client_rc=$?
  stop_server
  if [[ "$client_rc" -ne 0 ]]; then
    echo "iperf3 loopback client failed (rc=$client_rc)" >&2
    cat "$client_json" >&2
    return 1
  fi

  jq -e '
    .end != null and
    .end.sum_received != null and
    ((.end.sum_received.bytes // 0) > 0) and
    ((.end.sum_received.seconds // 0) > 0)' "$client_json" >/dev/null ||
    { echo "iperf3 client JSON lacks a successful sum end block" >&2; cat "$client_json" >&2; return 1; }
  jq -r '"iperf3 loopback: \(.end.sum_received.bytes) bytes in \(.end.sum_received.seconds)s (\(.end.sum_received.bits_per_second) bits/s)"' \
    "$client_json"
}

run_one() {
  local tool=$1 fn=$2 rc=0
  echo
  echo "==== real execution: $tool ===="
  "$fn" || rc=$?
  verdict "$tool" "$rc"
}

run_one sysbench check_sysbench
run_one zstd check_zstd
run_one npb-ep check_npb_ep
run_one npb-ft check_npb_ft
run_one openssl check_openssl
run_one stream check_stream
run_one fio check_fio
run_one iperf3 check_iperf3

echo
echo "==== real FreeBSD runtime gate summary (target=$target guest=FreeBSD/$(uname -m)) ===="
passed=0
for tool in "${tools[@]}"; do
  printf '%s %s\n' "${verdicts[$tool]}" "$tool"
  if [[ "${verdicts[$tool]}" == PASS ]]; then
    passed=$((passed + 1))
  fi
done
printf 'SUMMARY: %d/8 PASS\n' "$passed"
if [[ "$passed" -eq 8 ]]; then
  echo "freebsd-tools-gate: $target 8/8 real executions passed on FreeBSD $(uname -r) $(uname -m)"
  exit 0
fi
echo "freebsd-tools-gate: $target only $passed/8 checks passed" >&2
exit 1
