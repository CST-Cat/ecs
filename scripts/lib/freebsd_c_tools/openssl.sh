#!/usr/bin/env bash
# FreeBSD Clang static build of the OpenSSL CLI (same reduced feature set as Linux).

ecs_freebsd_c_build_openssl() {
  local work=$1 stage=$2 jobs=$3
  local repository tag commit openssl_target openssl_version
  repository=$(ecs_lock_tool_field openssl repository)
  tag=$(ecs_lock_tool_field openssl tag)
  commit=$(ecs_lock_tool_field openssl commit)
  openssl_target=$(ecs_lock_target_field "$ecs_freebsd_target" openssl_target)
  openssl_version=$(ecs_lock_tool_field openssl version)
  local src="$work/src-openssl"
  ecs_freebsd_c_clone_tool "$repository" "$tag" "$commit" "$src"

  local prefix="$work/openssl-prefix"
  local flags=(
    "$openssl_target" '-O3' 'no-shared' 'no-module' 'no-pinshared' 'no-tests' 'no-docs'
    'no-ssl' 'no-sock' 'no-dgram' 'no-http' 'no-cmp' 'no-cms' 'no-ct' 'no-ocsp'
    'no-dso' 'no-engine' 'no-static-engine' 'no-legacy' 'no-async' 'no-atexit'
    'no-autoload-config' 'no-cached-fetch' 'no-comp' 'no-dh' 'no-dsa' 'no-ec'
    'no-aria' 'no-bf' 'no-blake2' 'no-camellia' 'no-cast' 'no-cmac' 'no-des'
    'no-idea' 'no-md4' 'no-mdc2' 'no-ocb' 'no-rc2' 'no-rc4' 'no-rmd160'
    'no-scrypt' 'no-seed' 'no-siphash' 'no-siv' 'no-sm2' 'no-sm3' 'no-sm4'
    'no-whirlpool' 'no-ml-dsa' 'no-ml-kem' 'no-slh-dsa' 'no-rfc3779' 'no-srp'
    'no-srtp' 'no-ts' '-static' "--prefix=$prefix" "--openssldir=$prefix/ssl"
  )
  (
    cd "$src"
    # Force the FreeBSD cross compiler; Configure must not pick up host gcc.
    export CC
    perl ./Configure "${flags[@]}"
    make -j"$jobs" build_generated
    make -j"$jobs" apps/openssl
  )
  cp "$src/apps/openssl" "$stage/bin/openssl"
  ecs_freebsd_c_assert_static_freebsd_elf "$stage/bin/openssl" "$ecs_freebsd_elf_machine"
}
