#!/usr/bin/env bash

# Private, sourced machinery for the low-frequency FreeBSD GNU SDK publisher.
# The orchestration script owns argument parsing, acquisition, source builds,
# runtime probes and final provenance.  These helpers own the publisher-only
# tree slimming/evidence pass and the proof gates that consume its final bytes.
# They intentionally use the orchestration script's globals and `die` helper.

ecs_freebsd_gnu_sdk_phase2_strip_and_verify() {
  local rep inode_id paths path tmp
  while IFS=$'\t' read -r rep inode_id paths; do
    path="$prefix/$rep"
    tmp="$path.ecs-strip-tmp"
    if ! strip --strip-debug -o "$tmp" "$path"; then
      rm -f "$tmp"
      die "phase2: strip --strip-debug failed for $rep"
    fi
    # Write the stripped bytes back through the original path: the inode
    # (and therefore every hardlink alias of this object) keeps its identity
    # and picks up the stripped content in place.
    cat "$tmp" >"$path"
    rm -f "$tmp"
  done <"$evidence_dir/host-elf-inodes.tsv"
  python3 "$sdk_tree_verifier" manifest "$prefix" \
    "$evidence_dir/manifest.after.tsv"
  python3 "$sdk_tree_verifier" verify-tree \
    "$evidence_dir/manifest.before.tsv" \
    "$evidence_dir/manifest.after.tsv" \
    "$evidence_dir/host-elf-inodes.tsv"
}

# Stage 3 (contract 3.4): the drivers must resolve a complete internal
# toolchain on the final SDK bytes. Every cc1/f951/collect2/as/ld/plugin/
# wrapper path the driver prints for -### must exist on disk, and any
# "cannot find / cannot load / missing lto" report is a hard failure (a
# --disable-lto build must not reference absent LTO components). -### only
# prints the resolved specs and executes nothing, so this adds seconds.
ecs_freebsd_gnu_sdk_chain_case() {
  local name=$1
  shift
  local log="chain-$name.log"
  if ! "$@" >"$log" 2>&1; then
    sed -n '1,40p' "$log" >&2
    die "driver chain ($name): driver exited non-zero (contract 3.4)"
  fi
  if grep -Eqi 'cannot find|cannot load|missing lto' "$log"; then
    grep -Ei 'cannot find|cannot load|missing lto' "$log" | head -5 >&2
    die "driver chain ($name): unresolved component reference (contract 3.4)"
  fi
  local tok base cand problems=0
  while IFS= read -r tok; do
    tok=${tok//\"/}
    [[ "$tok" == */* ]] || continue
    base=${tok##*/}
    case "$base" in
      cc1 | f951 | collect2 | as | ld | lto1 | lto-wrapper | lto-dump | liblto_plugin.so | liblto_plugin.la)
        cand="/${tok#*/}"
        if [[ ! -e "$cand" ]]; then
          echo "driver chain ($name): referenced tool does not exist: $cand" >&2
          problems=$((problems + 1))
        fi
        ;;
    esac
  done < <(tr ' ' '\n' <"$log")
  [[ "$problems" -eq 0 ]] ||
    die "driver chain ($name): $problems referenced tool(s) missing (contract 3.4)"
  echo "freebsd-gnu-openmp-sdk: [stage3] driver chain OK: $name" >&2
}

ecs_freebsd_gnu_sdk_driver_chain_check() {
  # Runs inside the probe output dir (the probe sources are already there)
  # and keeps the five -### spec logs next to the probe binaries.
  local out_dir=$1
  (
    cd "$out_dir"
    ecs_freebsd_gnu_sdk_chain_case c-compile "$gcc_bin" -### --sysroot="$sysroot" -c hello.c
    ecs_freebsd_gnu_sdk_chain_case c-static-link "$gcc_bin" -### --sysroot="$sysroot" -static hello.c -o chain-c
    ecs_freebsd_gnu_sdk_chain_case f-static-link "$gfortran_bin" -### --sysroot="$sysroot" -static hello.f90 -o chain-f
    ecs_freebsd_gnu_sdk_chain_case c-openmp-link "$gcc_bin" -### --sysroot="$sysroot" -fopenmp omp.c -o chain-omp-c
    ecs_freebsd_gnu_sdk_chain_case f-openmp-link "$gfortran_bin" -### --sysroot="$sysroot" -fopenmp omp.f90 -o chain-omp-f
  )
  # The target assembler/linker are always part of the SDK contract (0.3).
  [[ -x "$prefix/bin/${gnu_triple}-as" ]] ||
    die "driver chain: missing $prefix/bin/${gnu_triple}-as (contract 3.4)"
  [[ -x "$prefix/bin/${gnu_triple}-ld" ]] ||
    die "driver chain: missing $prefix/bin/${gnu_triple}-ld (contract 3.4)"
}

# Stages 3/4 (contracts 3.3/4.3): record what the --disable-lto and
# --disable-gcov builds still install. Existence is evidence only — hand
# deleting LTO or gcov components is forbidden (contract 3.2), so whatever
# GCC still installs stays in the snapshot.
ecs_freebsd_gnu_sdk_feature_inventory() {
  local out=$1
  : >"$out"
  local pattern paths p
  for pattern in lto1 lto-wrapper lto-dump liblto_plugin* '*gcov*'; do
    paths=$(find "$prefix" -name "$pattern" | LC_ALL=C sort)
    if [[ -n "$paths" ]]; then
      while IFS= read -r p; do
        printf '%s\tpresent\t%s\n' "$pattern" "$p"
      done <<<"$paths" >>"$out"
    else
      printf '%s\tabsent\t-\n' "$pattern" >>"$out"
    fi
  done
  cat "$out" >&2
}

# Stage 5 (contract 5.2): remove exactly $prefix/share/man and
# $prefix/share/info from the snapshot. The full share/ listings before and
# after prove the removal stayed inside the two whitelisted directories.
ecs_freebsd_gnu_sdk_slim_remove_docs() {
  if [[ -d "$prefix/share" ]]; then
    (cd "$prefix" && find share -mindepth 1 | LC_ALL=C sort) \
      >"$evidence_dir/share-before.txt"
  else
    : >"$evidence_dir/share-before.txt"
  fi
  rm -rf "$prefix/share/man" "$prefix/share/info"
  if [[ -d "$prefix/share" ]]; then
    (cd "$prefix" && find share -mindepth 1 | LC_ALL=C sort) \
      >"$evidence_dir/share-after.txt"
  else
    : >"$evidence_dir/share-after.txt"
  fi
  echo "freebsd-gnu-openmp-sdk: [stage5] share/ entries $(wc -l <"$evidence_dir/share-before.txt") -> $(wc -l <"$evidence_dir/share-after.txt") (removed share/man + share/info only)" >&2
}

ecs_freebsd_gnu_sdk_consumer_build_gate() {
  # Contract 2.9: the release must build NPB EP/FT Class A + STREAM through
  # the unmodified Stage 5 builder on the final bytes. Nothing is kept after
  # the builder returns. Prebuilt mode skips this because the gnu-bench chain
  # already builds the same workloads through this builder on every run.
  echo "freebsd-gnu-openmp-sdk: [stage2] consumer build gate (contract 2.9)" >&2
  bash "$ECS_REPO_ROOT/scripts/build_tools_freebsd_gnu.sh" \
    --target "$target" \
    --stage-root "$work_dir/consumer-stage" \
    --sdk-prefix "$prefix"
}
