#!/usr/bin/env bash
# FreeBSD Clang static build of fio.
# Hard contract: CONFIG_POSIXAIO=y and CONFIG_LIBAIO!=y.

ecs_freebsd_c_build_fio() {
  local work=$1 stage=$2 jobs=$3
  local repository tag commit
  repository=$(ecs_lock_tool_field fio repository)
  tag=$(ecs_lock_tool_field fio tag)
  commit=$(ecs_lock_tool_field fio commit)
  local src="$work/src-fio"
  ecs_freebsd_c_clone_tool "$repository" "$tag" "$commit" "$src"

  (
    cd "$src"
    ./configure \
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
    # FreeBSD requires POSIX AIO. Linux libaio must stay off.
    if ! grep -Eq '^CONFIG_POSIXAIO=y$' config-host.mak; then
      echo 'CONFIG_POSIXAIO=y' >>config-host.mak
    fi
    sed -i '/^CONFIG_LIBAIO=y$/d' config-host.mak
    if grep -Eq '^CONFIG_(RDMA|RADOS|RBD|GFAPI|LIBAIO)=y$' config-host.mak; then
      echo 'fio configuration enabled an excluded engine' >&2
      grep -E '^CONFIG_(RDMA|RADOS|RBD|GFAPI|LIBAIO)=y$' config-host.mak >&2
      exit 1
    fi
    grep -Eq '^CONFIG_POSIXAIO=y$' config-host.mak || {
      echo 'fio configure did not enable POSIXAIO' >&2
      exit 1
    }
    make -j"$jobs"
  )
  cp "$src/fio" "$stage/bin/fio"
  ecs_freebsd_c_assert_static_freebsd_elf "$stage/bin/fio" "$ecs_freebsd_elf_machine"
}
