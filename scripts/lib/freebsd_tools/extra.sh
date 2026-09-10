# shellcheck shell=bash

set_openssl_build_flags() {
  openssl_prefix="$work/openssl-prefix"
  openssl_build_flags=(
    "$openssl_target" -O3 no-shared no-module no-pinshared no-tests no-docs
    no-ssl no-sock no-dgram no-http no-cmp no-cms no-ct no-ocsp no-dso
    no-engine no-static-engine no-legacy no-async no-atexit no-autoload-config
    no-cached-fetch no-comp no-dh no-dsa no-ec no-aria no-bf no-blake2
    no-camellia no-cast no-cmac no-des no-idea no-md4 no-mdc2 no-ocb
    no-rc2 no-rc4 no-rmd160 no-scrypt no-seed no-siphash no-siv no-sm2
    no-sm3 no-sm4 no-whirlpool no-ml-dsa no-ml-kem no-slh-dsa no-rfc3779
    no-srp no-srtp no-ts -static "--prefix=$openssl_prefix"
    "--openssldir=$openssl_prefix/ssl"
  )
}

phase_openssl() {
  require_sources
  set_openssl_build_flags
  echo "building OpenSSL $openssl_tag ($openssl_commit)"
  (
    cd "$openssl_src"
    CC="$cc_command" perl ./Configure "${openssl_build_flags[@]}"
    gmake -j"$jobs" apps/openssl
  )
  cp "$openssl_src/apps/openssl" "$stage/bin/openssl"
  validate_binary openssl

  local version algorithm output_name
  local -a aead
  version=$(lock_tool_field openssl version)
  OPENSSL_CONF=/dev/null run_target "$stage/bin/openssl" version >"$work/openssl-version.txt" 2>&1
  grep -Eq "^OpenSSL ${version//./\\.}([[:space:]]|$)" "$work/openssl-version.txt" ||
    die "OpenSSL version smoke did not report $version"
  mkdir -p "$work/openssl-smoke/modules" "$work/openssl-smoke/engines"
  for algorithm in aes-256-gcm chacha20-poly1305 sha256; do
    case "$algorithm" in
      aes-256-gcm) output_name=AES-256-GCM; aead=(-aead) ;;
      chacha20-poly1305) output_name=ChaCha20-Poly1305; aead=(-aead) ;;
      sha256) output_name=sha256; aead=() ;;
    esac
    OPENSSL_CONF=/dev/null \
      OPENSSL_MODULES="$work/openssl-smoke/modules" \
      OPENSSL_ENGINES="$work/openssl-smoke/engines" \
      run_target "$stage/bin/openssl" speed -elapsed -seconds 1 -bytes 16384 -mr -multi 1 \
      -evp "$algorithm" "${aead[@]}" >"$work/openssl-${algorithm}-smoke.txt" 2>&1
    grep -F "+DT:${output_name}:1:16384" "$work/openssl-${algorithm}-smoke.txt" >/dev/null ||
      die "OpenSSL speed $algorithm smoke omitted fixed parameters"
    grep -Eq "^\\+F:[0-9]+:${output_name}:[0-9]+(\\.[0-9]+)?[[:space:]]*$" \
      "$work/openssl-${algorithm}-smoke.txt" ||
      die "OpenSSL speed $algorithm smoke omitted machine-readable throughput"
  done
}

phase_stream() {
  require_sources
  local stream_revision
  stream_revision=$(sed -n 's@^/\* Revision: \$Id: stream\.c,v \([^ ]*\) \([0-9/]\{10\}\).*\*/$@\1-\2@p' "$stream_src")
  [[ -n "$stream_revision" ]] || die 'could not read official STREAM revision'
  echo "building STREAM $stream_revision"
  "$cc_command" -O3 -fopenmp -static -static-libgcc \
    -DSTREAM_ARRAY_SIZE="$stream_array_size" -DNTIMES="$stream_ntimes" \
    "$stream_src" -o "$stage/bin/stream"
  validate_binary stream

  OMP_NUM_THREADS=1 run_target "$stage/bin/stream" >"$work/stream-smoke.txt"
  local kernel
  for kernel in Copy Scale Add Triad; do
    grep -q "$kernel:" "$work/stream-smoke.txt" || die "STREAM smoke omitted $kernel"
  done
  grep -q 'Solution Validates' "$work/stream-smoke.txt" || die 'STREAM smoke did not validate'
}

phase_fio() {
  require_sources
  echo "building fio $fio_tag ($fio_commit) with posixaio + psync"
  (
    cd "$fio_src"
    local -a fio_configure=(--prefix="$work/fio-prefix" --build-static --disable-numa --disable-rdma --disable-rados --disable-rbd --disable-gfapi --disable-http --disable-pmem --disable-libzbc --disable-xnvme --disable-libblkio --disable-libnfs --disable-dfs --disable-tcmalloc --disable-native)
    if [[ "$toolchain_mode" == cross ]]; then
      fio_configure+=(--cpu=aarch64 --cc="$cc_command")
    fi
    CC="$cc_command" ./configure "${fio_configure[@]}"
    grep -Eq '^CONFIG_POSIXAIO=y$' config-host.mak || die 'FreeBSD fio did not enable CONFIG_POSIXAIO'
    grep -Eq '^CONFIG_LIBAIO=y$' config-host.mak && die 'FreeBSD fio unexpectedly enabled Linux libaio'
    gmake -j"$jobs"
  )
  cp "$fio_src/fio" "$stage/bin/fio"
  validate_binary fio

  local version required_engine requested_depth fio_json
  dd if=/dev/zero of="$work/fio-smoke.data" bs=4096 count=2048 >/dev/null 2>&1
  version=$(lock_tool_field fio version)
  run_target "$stage/bin/fio" --version >"$work/fio-version.txt" 2>&1
  grep -Eq "^fio-${version//./\\.}([[:space:]]|$)" "$work/fio-version.txt" ||
    die "fio version smoke did not report $version"
  run_target "$stage/bin/fio" --enghelp >"$work/fio-engines.txt"
  for required_engine in posixaio psync; do
    grep -Eiq "(^|[^[:alnum:]_])${required_engine}([^[:alnum:]_]|$)" "$work/fio-engines.txt" ||
      die "FreeBSD fio omitted required engine: $required_engine"
  done
  for requested_depth in 32 64; do
    fio_json="$work/fio-qd${requested_depth}.json"
    run_target "$stage/bin/fio" --name="ecs-qd${requested_depth}" --filename="$work/fio-smoke.data" \
      --rw=read --bs=4k --size=4m --runtime=1 --time_based=1 \
      --ioengine=posixaio --iodepth="$requested_depth" --numjobs=1 --direct=1 \
      --output-format=json --output="$fio_json"
    jq -e --argjson depth "$requested_depth" \
      '(.jobs | length == 1) and
       (.jobs[0].error == 0) and
       (.jobs[0]["job options"].ioengine == "posixaio") and
       ((.jobs[0]["job options"].iodepth | tonumber) == $depth) and
       ([.jobs[0].iodepth_level | to_entries[] | select(.key != "1") | .value] | any(. > 0))' \
      "$fio_json" >/dev/null || {
      cat "$fio_json" >&2
      die "FreeBSD fio posixaio QD${requested_depth} did not show effective depth"
    }
  done
}

phase_iperf3() {
  require_sources
  echo "building iperf3 $iperf3_tag ($iperf3_commit)"
  (
    cd "$iperf3_src"
    CC="$cc_command" ./configure \
      ${configure_cross_args[@]+"${configure_cross_args[@]}"} \
      --prefix="$work/iperf3-prefix" \
      --enable-static-bin \
      --without-sctp --without-openssl --without-ldconfig
    gmake -j"$jobs"
  )
  cp "$iperf3_src/src/iperf3" "$stage/bin/iperf3"
  validate_binary iperf3

  local version iperf_port iperf_server iperf_json iperf_ok
  version=$(lock_tool_field iperf3 version)
  run_target "$stage/bin/iperf3" --version >"$work/iperf3-version.txt" 2>&1
  grep -Eq "iperf ${version//./\\.}([^0-9]|$)" "$work/iperf3-version.txt" ||
    die "iperf3 version smoke did not report $version"
  iperf_port=$((42000 + (${RANDOM:-1} % 1000)))
  run_target "$stage/bin/iperf3" -s -1 -p "$iperf_port" >"$work/iperf3-server.txt" 2>&1 &
  iperf_server=$!
  iperf_json="$work/iperf3-smoke.json"
  iperf_ok=0
  for _ in {1..50}; do
    if run_target "$stage/bin/iperf3" -J -c 127.0.0.1 -p "$iperf_port" -t 1 -P 1 >"$iperf_json" 2>"$work/iperf3-client.txt"; then
      iperf_ok=1
      break
    fi
    kill -0 "$iperf_server" 2>/dev/null || die 'iperf3 server exited before loopback client connected'
    sleep 0.1
  done
  [[ "$iperf_ok" -eq 1 ]] || {
    kill "$iperf_server" 2>/dev/null || true
    wait "$iperf_server" 2>/dev/null || true
    die 'iperf3 loopback smoke failed'
  }
  kill -0 "$iperf_server" 2>/dev/null && kill "$iperf_server" 2>/dev/null || true
  wait "$iperf_server" 2>/dev/null || true
  jq -e 'type == "object" and (.start | type == "object") and (.end | type == "object")' \
    "$iperf_json" >/dev/null || die 'iperf3 loopback JSON smoke failed'
}
