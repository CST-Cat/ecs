#!/usr/bin/env bash

ecs_tool_build_fio() {
	echo "building fio ${fio_tag} (${fio_commit})"
	(
		cd "$fio_src"
		./configure \
			"${fio_cross_args[@]}" \
			--prefix="$work/fio-prefix" \
			--build-static \
			--disable-numa \
			--disable-rdma \
			--disable-rados \
			--disable-rbd \
			--disable-gfapi \
			--disable-http \
			--disable-pmem \
			--disable-libzbc \
			--disable-xnvme \
			--disable-libblkio \
			--disable-libnfs \
			--disable-dfs \
			--disable-tcmalloc \
			--disable-native
		sed -i '/^CONFIG_POSIXAIO=y$/d; /^CONFIG_POSIXAIO_FSYNC=y$/d' config-host.mak
		if grep -Eq '^CONFIG_(RDMA|RADOS|RBD|GFAPI|POSIXAIO)=y$' config-host.mak; then
			echo 'fio generated configuration enabled an excluded engine' >&2
			grep -E '^CONFIG_(RDMA|RADOS|RBD|GFAPI|POSIXAIO)=y$' config-host.mak >&2
			exit 1
		fi
		grep -Eq '^CONFIG_LIBAIO=y$' config-host.mak || {
			echo 'fio configure did not enable libaio' >&2
			exit 1
		}
		make -j"$jobs"
	)
	cp "$fio_src/fio" "$stage/bin/fio"
}

ecs_tool_smoke_fio() {
	dd if=/dev/zero of="$work/fio-smoke.data" bs=4096 count=1 status=none
	fio_json="$work/fio-smoke.json"
	run_target "$fio_bin" \
		--name=ecs-smoke --filename="$work/fio-smoke.data" --rw=read --bs=4k --size=4k \
		--ioengine=psync --iodepth=1 --numjobs=1 --direct=1 --output-format=json --output="$fio_json"
	jq -e '(.jobs | length == 1) and .jobs[0].jobname == "ecs-smoke"' "$fio_json" >/dev/null || {
		cat "$fio_json" >&2
		die 'fio JSON/QD1 smoke failed'
	}
	fio_io_uring_json="$work/fio-io-uring-smoke.json"
	fio_io_uring_error="$work/fio-io-uring-smoke.err"
	fio_io_uring_output="$work/fio-io-uring-smoke.out"
	if run_target "$fio_bin" \
		--name=ecs-io-uring-smoke --filename="$work/fio-smoke.data" --rw=read --bs=4k --size=4k \
		--ioengine=io_uring --iodepth=1 --numjobs=1 --direct=1 --output-format=json --output="$fio_io_uring_json" \
		>"$fio_io_uring_output" 2>"$fio_io_uring_error"; then
		jq -e '(.jobs | length == 1) and .jobs[0].jobname == "ecs-io-uring-smoke"' "$fio_io_uring_json" >/dev/null || {
			cat "$fio_io_uring_json" "$fio_io_uring_error" >&2
			die 'fio io_uring JSON/QD1 smoke failed'
		}
	else
		if grep -Eiq "kernel.*support.*io_uring|io_uring[^[:alpha:]]+(is )?not supported" "$fio_io_uring_error" "$fio_io_uring_output"; then
			echo 'fio io_uring runtime smoke skipped: the architecture test kernel does not support io_uring'
		else
			cat "$fio_io_uring_error" "$fio_io_uring_output" "$fio_io_uring_json" 2>/dev/null >&2 || true
			die 'fio io_uring runtime smoke failed for a reason other than kernel support'
		fi
	fi
	run_target "$fio_bin" --version
	run_target "$fio_bin" --enghelp >"$work/fio-engines.txt"
	for required_engine in io_uring libaio psync; do
		grep -Eiq "(^|[^[:alnum:]_])${required_engine}([^[:alnum:]_]|$)" "$work/fio-engines.txt" || {
			cat "$work/fio-engines.txt" >&2
			die "fio omitted required engine: $required_engine"
		}
	done
	if grep -Eiq '(^|[^[:alnum:]])(rados|rbd|gfapi|gluster|rdma)([^[:alnum:]]|$)' "$work/fio-engines.txt"; then
		cat "$work/fio-engines.txt" >&2
		die 'fio exposed a disabled external engine'
	fi
}
