# shellcheck shell=bash

phase_manifest() {
  require_sources
  local tool
  for tool in sysbench zstd npb-ep npb-ft openssl stream fio iperf3; do
    [[ -x "$stage/bin/$tool" ]] || die "manifest phase is missing executable: $tool"
  done

  cp "$sysbench_src/COPYING" "$stage/LICENSES/SYSBENCH-COPYING"
  cp "$zstd_src/LICENSE" "$stage/LICENSES/ZSTD-LICENSE"
  cp "$zstd_src/COPYING" "$stage/LICENSES/ZSTD-COPYING"
  sed -n '1,31p' "$npb_src/EP/ep.f90" >"$stage/LICENSES/NPB-LICENSE.txt"
  cp "$work/$npb_tag/README" "$stage/LICENSES/NPB-README.txt"
  cp "$openssl_src/LICENSE.txt" "$stage/LICENSES/OPENSSL-LICENSE.txt"
  cp "$fio_src/COPYING" "$stage/LICENSES/FIO-COPYING"
  cp "$iperf3_src/LICENSE" "$stage/LICENSES/IPERF3-LICENSE"
  sed -n '1,/^ \*\/$/p' "$stream_src" >"$stage/LICENSES/STREAM-LICENSE.txt"
  cp "$luajit_license_dir/MIT" "$stage/LICENSES/LUAJIT-MIT"
  cp "$luajit_license_dir/PD" "$stage/LICENSES/LUAJIT-PUBLIC-DOMAIN"
  cp "$ck_license_dir/BSD2CLAUSE" "$stage/LICENSES/CONCURRENCY-KIT-BSD2CLAUSE"
  chmod 0644 "$stage/LICENSES/"*

  local sysbench_source zstd_source openssl_source fio_source iperf3_source
  local sysbench_version zstd_version openssl_version fio_version iperf3_version
  local sysbench_upstream zstd_upstream npb_upstream openssl_upstream fio_upstream iperf3_upstream
  local stream_revision npb_gfortran_version npb_ieee_provider
  local openssl_build_flags_json

  sysbench_source=$(git_source "$sysbench_repository" "$sysbench_commit")
  zstd_source=$(git_source "$zstd_repository" "$zstd_commit")
  openssl_source=$(git_source "$openssl_repository" "$openssl_commit")
  fio_source=$(git_source "$fio_repository" "$fio_commit")
  iperf3_source=$(git_source "$iperf3_repository" "$iperf3_commit")
  sysbench_version=$(lock_tool_field sysbench version)
  zstd_version=$(lock_tool_field zstd version)
  openssl_version=$(lock_tool_field openssl version)
  fio_version=$(lock_tool_field fio version)
  iperf3_version=$(lock_tool_field iperf3 version)
  sysbench_upstream=$(lock_tool_field sysbench upstream)
  zstd_upstream=$(lock_tool_field zstd upstream)
  npb_upstream=$(lock_tool_field npb-ep upstream)
  openssl_upstream=$(lock_tool_field openssl upstream)
  fio_upstream=$(lock_tool_field fio upstream)
  iperf3_upstream=$(lock_tool_field iperf3 upstream)
  stream_revision=$(sed -n 's@^/\* Revision: \$Id: stream\.c,v \([^ ]*\) \([0-9/]\{10\}\).*\*/$@\1-\2@p' "$stream_src")
  [[ -n "$stream_revision" ]] || die 'could not read official STREAM revision'
  npb_gfortran_version=$("$fc_command" --version | sed -n '1p')
  npb_ieee_provider=none
  set_openssl_build_flags
  openssl_build_flags_json=$(printf '%s\n' "${openssl_build_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
  local smoke_runner_label=$smoke_runner
  local toolchain_mode_label=$toolchain_mode

  jq -n \
    --arg target "$target" \
    --arg goarch "$goarch" \
    --arg architecture "$package_arch" \
    --arg cc "$cc_command" \
    --arg cxx "$cxx_command" \
    --arg fc "$fc_command" \
    --arg toolchain_mode "$toolchain_mode_label" \
    --arg smoke_runner "$smoke_runner_label" \
    --argjson supported_architectures "$supported_architectures_json" \
    --argjson supported_targets "$supported_targets_json" \
    --arg build_triplet "$build_triplet" \
    --arg target_triplet "$target_triplet" \
    --arg sysbench_version "$sysbench_version" --arg sysbench_tag "$sysbench_tag" \
    --arg sysbench_source "$sysbench_source" --arg sysbench_commit "$sysbench_commit" \
    --arg sysbench_luajit_version "$sysbench_luajit_version" --arg sysbench_ck_version "$sysbench_ck_version" \
    --arg sysbench_luajit_package "$sysbench_luajit_package" --arg sysbench_ck_package "$sysbench_ck_package" \
    --arg zstd_version "$zstd_version" --arg zstd_tag "$zstd_tag" \
    --arg zstd_source "$zstd_source" --arg zstd_commit "$zstd_commit" \
    --arg npb_version "$npb_version" --arg npb_tag "$npb_tag" --arg npb_url "$npb_url" --arg npb_sha "$npb_sha" \
    --arg npb_gfortran_version "$npb_gfortran_version" --arg npb_ieee_provider "$npb_ieee_provider" \
    --arg openssl_version "$openssl_version" --arg openssl_tag "$openssl_tag" \
    --arg openssl_source "$openssl_source" --arg openssl_commit "$openssl_commit" --arg openssl_target "$openssl_target" \
    --argjson openssl_build_flags "$openssl_build_flags_json" \
    --arg fio_version "$fio_version" --arg fio_tag "$fio_tag" --arg fio_source "$fio_source" --arg fio_commit "$fio_commit" \
    --arg iperf3_version "$iperf3_version" --arg iperf3_tag "$iperf3_tag" --arg iperf3_source "$iperf3_source" --arg iperf3_commit "$iperf3_commit" \
    --arg stream_version "${stream_revision%%-*}" --arg stream_revision "$stream_revision" --arg stream_url "$stream_url" --arg stream_sha "$stream_sha" \
    --arg zstd_corpus_name "$zstd_corpus_name" --arg zstd_corpus_url "$zstd_corpus_url" \
    --arg zstd_corpus_sha "$zstd_corpus_sha" --arg zstd_corpus_source_sha "$zstd_corpus_source_sha" \
    --argjson zstd_corpus_bytes "$zstd_corpus_bytes" \
    --arg sysbench_upstream "$sysbench_upstream" --arg zstd_upstream "$zstd_upstream" --arg npb_upstream "$npb_upstream" \
    --arg openssl_upstream "$openssl_upstream" --arg fio_upstream "$fio_upstream" --arg iperf3_upstream "$iperf3_upstream" \
    --argjson stream_array_size "$stream_array_size" --argjson stream_ntimes "$stream_ntimes" \
    ' {
        schema_version: "ecs-tools.manifest/v1",
        target: $target,
        goos: "freebsd",
        goarch: $goarch,
        architecture: $architecture,
        supported_architectures: $supported_architectures,
        supported_targets: $supported_targets,
        build: {toolchain_mode: $toolchain_mode, build_triplet: $build_triplet, target_triplet: $target_triplet, smoke_runner: $smoke_runner, validation: {scope: "functional", performance_valid: false}},
        tools: [
          {name: "sysbench", upstream: $sysbench_upstream, version: $sysbench_version, tag_or_commit: $sysbench_tag, source: $sysbench_source, build_flags: [("CC=" + $cc), ("CXX=" + $cxx), "LDFLAGS=-static", "--without-gcc-arch", "--with-system-luajit", "--with-system-ck", "--with-extra-ldflags=-all-static -static-libgcc -Wl,--as-needed", "--without-mysql", "--without-pgsql", "--without-drizzle", "--without-attachsql", "--without-oracle"], enabled_features: ["cpu", "LuaJIT", "Concurrency Kit"], disabled_features: ["database-drivers", "host-CPU-specific architecture flags", "mysql", "pgsql", "drizzle", "attachsql", "oracle"], architecture: $architecture, license: "GPL-2.0-only", parameters: {source_commit: $sysbench_commit, system_luajit_version: $sysbench_luajit_version, system_luajit_package: $sysbench_luajit_package, system_ck_version: $sysbench_ck_version, system_ck_package: $sysbench_ck_package, fully_static: true, stripped: false}},
          {name: "zstd", upstream: $zstd_upstream, version: $zstd_version, tag_or_commit: $zstd_tag, source: $zstd_source, build_flags: [$cc, "-O3", "-static", "-static-libgcc", "-DZSTD_NODICT", "-DZSTD_NOTRACE", "HAVE_ZLIB=0", "HAVE_LZMA=0", "HAVE_LZ4=0", "ZSTD_LEGACY_SUPPORT=0"], enabled_features: ["benchmark", "multithread", "compression", "decompression"], disabled_features: ["zlib", "lzma", "lz4", "legacy-formats", "dictionary-builder", "trace"], architecture: $architecture, license: "BSD-3-Clause OR GPL-2.0-only", parameters: {source_commit: $zstd_commit, level: 3, evaluation_seconds: 5, thread_modes: ["1T", "NT"], corpus_name: $zstd_corpus_name, corpus_path: ("runtime/" + $zstd_corpus_name), corpus_bytes: $zstd_corpus_bytes, corpus_sha256: $zstd_corpus_sha, corpus_source_url: $zstd_corpus_url, corpus_source_sha256: $zstd_corpus_source_sha, corpus_construction: "raw concatenation: dickens,mozilla,mr,nci,ooffice,osdb,reymont,samba,sao,webster,x-ray,xml", fully_static: true, stripped: false}},
          {name: "npb-ep", upstream: $npb_upstream, version: $npb_version, tag_or_commit: $npb_tag, source: $npb_url, build_flags: [$fc, "-O3", "-fopenmp", "-static", "CLASS=A", "RAND=randi8", "OMP"], enabled_features: ["NPB3.4-OMP", "EP", "Class A", "OpenMP"], disabled_features: ["MPI", "other NPB kernels", "other problem classes"], architecture: $architecture, license: "NASA-NPB-permissive", parameters: {source_sha256: $npb_sha, source_patches: [], intrinsic_module_provider: $npb_ieee_provider, compiler: $npb_gfortran_version, compiler_flags: "-O3 -fopenmp -static", random_generator: "randi8", ci_smoke_class: "A", ci_smoke_scope: "release Class A binary", fully_static: true, stripped: false}},
          {name: "npb-ft", upstream: $npb_upstream, version: $npb_version, tag_or_commit: $npb_tag, source: $npb_url, build_flags: [$fc, "-O3", "-fopenmp", "-static", "CLASS=A", "RAND=randi8", "OMP"], enabled_features: ["NPB3.4-OMP", "FT", "Class A", "OpenMP", "3D FFT"], disabled_features: ["MPI", "other NPB kernels", "other problem classes"], architecture: $architecture, license: "NASA-NPB-permissive", parameters: {source_sha256: $npb_sha, source_patches: [], intrinsic_module_provider: $npb_ieee_provider, compiler: $npb_gfortran_version, compiler_flags: "-O3 -fopenmp -static", random_generator: "randi8", ci_smoke_class: "A", ci_smoke_scope: "release Class A binary", fully_static: true, stripped: false}},
          {name: "openssl", upstream: $openssl_upstream, version: $openssl_version, tag_or_commit: $openssl_tag, source: $openssl_source, build_flags: $openssl_build_flags, enabled_features: ["speed", "EVP", "AES-256-GCM", "ChaCha20-Poly1305", "SHA-256", "multi-process", "architecture assembly"], disabled_features: ["TLS/DTLS/QUIC", "network/HTTP", "shared libraries/modules/engines", "EC/DH/DSA/PQ families", "unrequested cipher/digest families", "tests/documentation"], architecture: $architecture, license: "Apache-2.0", parameters: {source_commit: $openssl_commit, configure_target: $openssl_target, generated_target: "build_generated", build_target: "apps/openssl", algorithms: ["aes-256-gcm", "chacha20-poly1305", "sha256"], block_bytes: 16384, duration_seconds: 5, worker_modes: [1, "detected_cpu_allowance"], elapsed_wall_clock: true, machine_readable: true, fully_static: true, stripped: false}},
          {name: "stream", upstream: "https://www.cs.virginia.edu/stream/", version: $stream_version, tag_or_commit: $stream_revision, source: $stream_url, build_flags: [$cc, "-O3", "-fopenmp", "-static", "-static-libgcc", ("-DSTREAM_ARRAY_SIZE=" + ($stream_array_size|tostring)), ("-DNTIMES=" + ($stream_ntimes|tostring))], enabled_features: ["Copy", "Scale", "Add", "Triad", "OpenMP"], disabled_features: [], architecture: $architecture, license: "STREAM-custom", parameters: {source_sha256: $stream_sha, array_size: $stream_array_size, ntimes: $stream_ntimes, fully_static: true, stripped: false}},
          {name: "fio", upstream: $fio_upstream, version: $fio_version, tag_or_commit: $fio_tag, source: $fio_source, build_flags: ["--build-static", "--disable-numa", "--disable-rdma", "--disable-rados", "--disable-rbd", "--disable-gfapi", "--disable-http", "--disable-pmem", "--disable-libzbc", "--disable-xnvme", "--disable-libblkio", "--disable-libnfs", "--disable-dfs", "--disable-tcmalloc", "--disable-native", "generated-config: require CONFIG_POSIXAIO=y"], enabled_features: ["posixaio", "psync"], disabled_features: ["io_uring", "libaio", "ceph", "rbd", "rados", "gluster", "gfapi", "rdma"], architecture: $architecture, license: "GPL-2.0-only", parameters: {source_commit: $fio_commit, engine_order: ["posixaio", "psync"], qd_validation: [32, 64], fully_static: true, stripped: false}},
          {name: "iperf3", upstream: $iperf3_upstream, version: $iperf3_version, tag_or_commit: $iperf3_tag, source: $iperf3_source, build_flags: ["--enable-static-bin", "--without-sctp", "--without-openssl", "--without-ldconfig"], enabled_features: ["tcp", "udp", "ipv4", "ipv6", "parallel", "reverse", "json"], disabled_features: ["sctp", "openssl/auth"], architecture: $architecture, license: "BSD-3-Clause", parameters: {source_commit: $iperf3_commit, fully_static: true, stripped: false}}
        ]
      }' | jq . >"$stage/manifest.json"

  echo "completed $toolchain_mode FreeBSD tools stage: $stage"
}
