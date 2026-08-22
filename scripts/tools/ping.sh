#!/usr/bin/env bash

ecs_tool_build_ping() {
	echo "building iputils ping ${iputils_tag} (${iputils_commit})"
	ping_build="$work/iputils-build"
	ping_install="$work/iputils-install"
	LDFLAGS='-static' meson setup "$ping_build" "$iputils_src" \
		"${meson_cross_args[@]}" \
		--prefix=/usr/local \
		--buildtype=release \
		-DUSE_CAP=false \
		-DUSE_IDN=false \
		-DUSE_GETTEXT=false \
		-DBUILD_ARPING=false \
		-DBUILD_CLOCKDIFF=false \
		-DBUILD_PING=true \
		-DBUILD_TRACEPATH=false \
		-DBUILD_MANS=false \
		-DBUILD_HTML_MANS=false \
		-DSKIP_TESTS=true \
		-DNO_SETCAP_OR_SUID=true
	meson compile -C "$ping_build" -j "$jobs"
	meson install -C "$ping_build" --destdir "$ping_install"
	cp "$ping_install/usr/local/bin/ping" "$stage/bin/ping"
}

ecs_tool_smoke_ping() {
	run_target "$ping_bin" -V
	run_target "$ping_bin" -c 1 -W 1 127.0.0.1 >"$work/ping-smoke.txt"
	grep -Eq '1 packets transmitted|1 packets received|1 received' "$work/ping-smoke.txt" || {
		cat "$work/ping-smoke.txt" >&2
		die 'iputils ping loopback smoke failed'
	}
}
