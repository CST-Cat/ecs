#!/usr/bin/env bash

ecs_tool_build_iperf3() {
	echo "building iperf3 ${iperf3_tag} (${iperf3_commit})"
	(
		cd "$iperf3_src"
		./configure \
			"${configure_cross_args[@]}" \
			--prefix="$work/iperf3-prefix" \
			--enable-static-bin \
			--without-sctp \
			--without-openssl \
			--without-ldconfig
		make -j"$jobs"
	)
	cp "$iperf3_src/src/iperf3" "$stage/bin/iperf3"
}

stop_iperf_server() {
	if [[ -n "${iperf_server:-}" ]]; then
		kill "$iperf_server" 2>/dev/null || true
		wait "$iperf_server" 2>/dev/null || true
		iperf_server=""
	fi
}

ecs_tool_smoke_iperf3() {
	run_target "$iperf3_bin" --version
	iperf_port=$((42000 + (${RANDOM:-1} % 1000)))
	run_target "$iperf3_bin" -s -1 -p "$iperf_port" >"$work/iperf3-server.txt" 2>&1 &
	iperf_server=$!
	trap 'stop_iperf_server; cleanup' EXIT
	iperf_json="$work/iperf3-smoke.json"
	iperf_client_error="$work/iperf3-client.txt"
	iperf_client_status=1
	for _ in {1..50}; do
		if run_target "$iperf3_bin" -J -c 127.0.0.1 -p "$iperf_port" -t 1 -P 1 >"$iperf_json" 2>"$iperf_client_error"; then
			iperf_client_status=0
			break
		fi
		if ! kill -0 "$iperf_server" 2>/dev/null; then
			break
		fi
		sleep 0.1
	done
	if ((iperf_client_status != 0)); then
		cat "$work/iperf3-server.txt" "$iperf_client_error" >&2
		stop_iperf_server
		die 'iperf3 loopback smoke failed'
	fi
	wait "$iperf_server" || true
	iperf_server=""
	trap cleanup EXIT
	jq -e 'type == "object" and (.start | type == "object") and (.end | type == "object")' "$iperf_json" >/dev/null || {
		cat "$work/iperf3-server.txt" "$iperf_client_error" >&2
		cat "$iperf_json" >&2
		die 'iperf3 JSON parser smoke failed'
	}
}
