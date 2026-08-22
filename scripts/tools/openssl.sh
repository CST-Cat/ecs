#!/usr/bin/env bash

ecs_tool_build_openssl() {
	echo "building OpenSSL ${openssl_tag} (${openssl_commit})"
	openssl_version=$(ecs_lock_tool_field openssl version)
	openssl_prefix='/opt/ecs-openssl'
	openssl_build_flags=(
		"$openssl_target" '-O3' 'no-shared' 'no-module' 'no-pinshared' 'no-tests' 'no-docs'
		'no-ssl' 'no-sock' 'no-dgram' 'no-http' 'no-cmp' 'no-cms' 'no-ct' 'no-ocsp'
		'no-dso' 'no-engine' 'no-static-engine' 'no-legacy' 'no-async' 'no-atexit'
		'no-autoload-config' 'no-cached-fetch' 'no-comp' 'no-dh' 'no-dsa' 'no-ec'
		'no-aria' 'no-bf' 'no-blake2' 'no-camellia' 'no-cast' 'no-cmac' 'no-des'
		'no-idea' 'no-md4' 'no-mdc2' 'no-ocb' 'no-rc2' 'no-rc4' 'no-rmd160'
		'no-scrypt' 'no-seed' 'no-siphash' 'no-siv' 'no-sm2' 'no-sm3' 'no-sm4'
		'no-whirlpool' 'no-ml-dsa' 'no-ml-kem' 'no-slh-dsa' 'no-rfc3779' 'no-srp'
		'no-srtp' 'no-ts' '-static' "--prefix=$openssl_prefix" "--openssldir=$openssl_prefix/ssl"
	)
	openssl_build_flags_json=$(printf '%s\n' "${openssl_build_flags[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')
	(
		cd "$openssl_src"
		perl ./Configure "${openssl_build_flags[@]}"
		make -j"$jobs" build_generated
		make -j"$jobs" apps/openssl
	)
	cp "$openssl_src/apps/openssl" "$stage/bin/openssl"
}

ecs_tool_smoke_openssl() {
	local expected_version expected_version_regex
	expected_version=$(ecs_lock_tool_field openssl version)
	expected_version_regex=${expected_version//./\\.}
	run_target "$openssl_bin" version >"$work/openssl-version.txt" 2>&1
	grep -Eq "^OpenSSL ${expected_version_regex}([[:space:]]|$)" "$work/openssl-version.txt" || {
		cat "$work/openssl-version.txt" >&2
		die "OpenSSL version smoke did not report ${expected_version}"
	}
	mkdir -p "$work/openssl-smoke/modules" "$work/openssl-smoke/engines"
	for openssl_algorithm in aes-256-gcm chacha20-poly1305 sha256; do
		case "$openssl_algorithm" in
			aes-256-gcm) openssl_output_name='AES-256-GCM'; openssl_aead=(-aead) ;;
			chacha20-poly1305) openssl_output_name='ChaCha20-Poly1305'; openssl_aead=(-aead) ;;
			sha256) openssl_output_name='sha256'; openssl_aead=() ;;
		esac
		OPENSSL_CONF=/dev/null OPENSSL_MODULES="$work/openssl-smoke/modules" OPENSSL_ENGINES="$work/openssl-smoke/engines" \
			run_target "$openssl_bin" speed -elapsed -seconds 1 -bytes 16384 -mr -multi 1 -evp "$openssl_algorithm" "${openssl_aead[@]}" \
			>"$work/openssl-${openssl_algorithm}-smoke.txt" 2>&1
		openssl_smoke_output="$work/openssl-${openssl_algorithm}-smoke.txt"
		grep -F "+DT:${openssl_output_name}:1:16384" "$openssl_smoke_output" >/dev/null || die "OpenSSL speed ${openssl_algorithm} smoke omitted fixed parameters"
		grep -Eq "^\\+F:[0-9]+:${openssl_output_name}:[0-9]+(\\.[0-9]+)?[[:space:]]*$" "$openssl_smoke_output" || die "OpenSSL speed ${openssl_algorithm} smoke omitted aggregate machine-readable throughput"
	done
}
