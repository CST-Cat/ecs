#!/usr/bin/env bash
set -euo pipefail

# 校验某个完整 platform target 的工具 stage，并把 Silesia 语料从中摘出去。
#
# 校验分两层：
#
#   1. cmd/tools-manifest-check 用 ecs 自己的解析器读 manifest——发布物必须能被
#      将要读它的那份代码认下来，而不是只能被另一份 shell parser 认下来；
#      build mode、smoke runner 和 NPB smoke class 的 stage 口径也通过同一入口
#      与构建容器的解析结果比对；
#   2. 下面只保留 stage 特有检查：十个 executable、LICENSES 与独立语料。
#
# 构建口径（cross 还是 native、smoke 运行器是谁）不在这里第二次定义，而是向
# build_tools_container.sh --print-params 索取。manifest checker 只负责把 stage
# 中记录的值与这份解析结果比对。
#
# FreeBSD 的 stage 自 Stage 6 起由 scripts/ci/merge_freebsd_tools_stage.sh 合并
# 产生，验证不再依赖任何 builder：事实只来自 manifest 本身、钉死的目标合同
# （tools/freebsd-sysroot.lock.json、tools/freebsd-gnu-openmp.lock.json）、预期
# 工具集合与直接 binary inspection。FreeBSD 路径不得调用任何 builder 的
# --print-params——stage 的生命周期必须独立于产生它的构建器。
#
# 语料是独立发布物：它 200 MB 出头，七个架构各带一份会让 Release 膨胀到没有
# 必要的体积。它的内容在下载入口（build_tools.sh）已按 lock.json 校验过，这里
# 只负责把它从 stage 中摘出去。

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
cd "$ECS_REPO_ROOT"

usage() {
  echo "usage: scripts/verify_tools_stage.sh --target TARGET --stage-root DIR [--keep-corpus]" >&2
}

die() {
  echo "verify-tools-stage: $*" >&2
  exit 1
}

# ---- FreeBSD structural verification（Stage 6）---------------------------
#
# 只依据 manifest、目标合同、预期工具集合、stage 级 SHA256SUMS、binary
# inspection 与 manifest 的非 sha256 记录字段；不再向任何 builder 索取构建
# 口径，也不再逐工具重算 sha256。

ecs_verify_freebsd_stage() {
  local target=$1 stage_dir=$2 manifest=$3
  local sysroot_lock="$ECS_REPO_ROOT/tools/freebsd-sysroot.lock.json"
  local gnu_lock="$ECS_REPO_ROOT/tools/freebsd-gnu-openmp.lock.json"

  # `file` 打印的 CPU token：amd64→x86-64、arm64→aarch64（Stage 5 判定口径；
  # sysroot lock 的 elf_machine 标签不会出现在 file 输出里）。
  local file_machine
  case "$target" in
    freebsd_amd64) file_machine=x86-64 ;;
    freebsd_arm64) file_machine=aarch64 ;;
    *) die "unsupported FreeBSD target: $target" ;;
  esac

  # 1. Canonical parser owns the manifest schema, field naming, tool set and
  #    architecture semantics. toolchain_mode=cross 与 smoke_runner=none 是
  #    FreeBSD 结构验证的固定合同：结构验证不对目标端执行任何程序。
  go run "$ECS_REPO_ROOT/cmd/tools-manifest-check" \
    --target "$target" \
    --toolchain-mode cross \
    --smoke-runner none \
    "$manifest"

  # 2. 目标合同：manifest 的 target triple 必须是钉死的 release triple。
  local triple gnu_triple release
  triple=$(jq -er --arg t "$target" '.targets[$t].clang_target_triple' "$sysroot_lock") ||
    die "sysroot lock has no clang_target_triple for $target"
  gnu_triple=$(jq -er --arg t "$target" '.targets[$t].gnu_target_triple' "$gnu_lock") ||
    die "GNU lock has no gnu_target_triple for $target"
  [[ "$triple" == "$gnu_triple" ]] ||
    die "clang and GNU target triples disagree: $triple != $gnu_triple"
  release=$(jq -er '.freebsd_release' "$sysroot_lock") ||
    die "sysroot lock has no freebsd_release"
  [[ $(jq -er '.build.target_triplet' "$manifest") == "$triple" ]] ||
    die "manifest build.target_triplet does not match the pinned $release triple $triple"

  # 3. Stage 布局：恰好 bin/、LICENSES/、manifest.json 与 SHA256SUMS；明确
  #    断言不携带 ping、nexttrace-tiny 与 Silesia 语料（corpus 是独立发布物）。
  local top
  top=$(find "$stage_dir" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
  [[ "$top" == "$(printf 'LICENSES\nSHA256SUMS\nbin\nmanifest.json')" ]] ||
    die "$target stage must contain exactly bin/, LICENSES/, manifest.json and SHA256SUMS; found:
$top"
  local absent
  for absent in bin/ping bin/nexttrace-tiny share; do
    [[ ! -e "$stage_dir/$absent" ]] || die "$target stage unexpectedly contains $absent"
  done

  # 4. 包级完整性：merge 写出的 SHA256SUMS 一次覆盖 bin/、LICENSES/ 与
  #    manifest.json 的全部文件（SHA256SUMS 自身不在清单内）。逐工具 sha256
  #    只是 manifest 里的记录字段，不再逐工具重算比对。
  echo "verify-tools-stage: verifying $target stage SHA256SUMS" >&2
  (
    cd "$stage_dir"
    sha256sum -c SHA256SUMS
  ) >&2 || die "$target stage failed its package-level SHA256SUMS verification"

  # 5. 工具集合：恰好 8 个，一个不多、一个不少。
  mapfile -t expected_tools < <(ecs_target_tool_names "$target")
  [[ "${#expected_tools[@]}" -eq 8 ]] ||
    die "expected 8 FreeBSD tools in the tools lock, got ${#expected_tools[@]}"
  local actual_tools expected_sorted
  actual_tools=$(find "$stage_dir/bin" -mindepth 1 -maxdepth 1 -type f -printf '%f\n' | LC_ALL=C sort)
  expected_sorted=$(printf '%s\n' "${expected_tools[@]}" | LC_ALL=C sort)
  [[ "$actual_tools" == "$expected_sorted" ]] ||
    die "$target stage bin must contain exactly: ${expected_tools[*]}
found:
$actual_tools"
  local nonfile
  nonfile=$(find "$stage_dir/bin" -mindepth 1 -maxdepth 1 ! -type f -printf '%f\n')
  [[ -z "$nonfile" ]] || die "$target stage bin contains non-file entries: $nonfile"

  # 6. 逐工具 binary inspection + manifest 非 sha256 字段一致性。
  local tool bin_path file_out mtriple mfamily momp mversion mhost
  for tool in "${expected_tools[@]}"; do
    bin_path="$stage_dir/bin/$tool"
    [[ -f "$bin_path" && -s "$bin_path" ]] ||
      die "$target stage is missing a non-empty $tool"
    # upload-artifact v4 不保留权限位：artifact 往返后的可执行位由 package.sh
    # 打包时统一恢复（0755），这里不断言 -x，只验内容。
    file_out=$(file -b "$bin_path")
    case "$file_out" in
      *"$file_machine"*) ;;
      *) die "$tool architecture is not $file_machine: $file_out" ;;
    esac
    # FreeBSD 身份沿用 Stage 5 的判定口径：amd64 lld 产物的 OS/ABI 直接是
    # FreeBSD；arm64 GNU ld 产物 OS/ABI 保持 SYSV 但带 FreeBSD ABI note——
    # 两种形态的 file 输出都含 FreeBSD。
    case "$file_out" in
      *FreeBSD*) ;;
      *) die "$tool is not a FreeBSD ELF: $file_out" ;;
    esac
    case "$file_out" in
      *static* | *statically*) ;;
      *) die "$tool is not static: $file_out" ;;
    esac
    if readelf -d "$bin_path" 2>/dev/null | grep -q NEEDED; then
      die "$tool has dynamic NEEDED entries"
    fi
    if strings "$bin_path" 2>/dev/null | grep -q 'GLIBC_'; then
      die "$tool contains glibc symbols"
    fi

    mtriple=$(jq -er --arg t "$tool" \
      '.tools[] | select(.name == $t) | .parameters.target_triple' "$manifest") ||
      die "manifest has no parameters.target_triple for $tool"
    [[ "$mtriple" == "$triple" ]] ||
      die "$tool target_triple $mtriple does not match the pinned $triple"
    mfamily=$(jq -er --arg t "$tool" \
      '.tools[] | select(.name == $t) | .parameters.compiler_family' "$manifest") ||
      die "manifest has no parameters.compiler_family for $tool"
    momp=$(jq -er --arg t "$tool" \
      '.tools[] | select(.name == $t) | .parameters.openmp_runtime' "$manifest") ||
      die "manifest has no parameters.openmp_runtime for $tool"
    mversion=$(jq -er --arg t "$tool" \
      '.tools[] | select(.name == $t) | .parameters.compiler_version' "$manifest") ||
      die "manifest has no parameters.compiler_version for $tool"
    mhost=$(jq -er --arg t "$tool" \
      '.tools[] | select(.name == $t) | .parameters.build_host' "$manifest") ||
      die "manifest has no parameters.build_host for $tool"
    case "$tool" in
      sysbench | zstd | openssl | fio | iperf3)
        [[ "$mfamily" == "clang" ]] ||
          die "$tool compiler_family must be clang, got $mfamily"
        [[ "$momp" == "none" ]] ||
          die "$tool openmp_runtime must be none, got $momp"
        ;;
      npb-ep | npb-ft | stream)
        [[ "$mfamily" == "gcc" ]] ||
          die "$tool compiler_family must be gcc, got $mfamily"
        [[ "$momp" == "libgomp" ]] ||
          die "$tool openmp_runtime must be libgomp, got $momp"
        ;;
    esac
    [[ -n "$mversion" ]] || die "$tool parameters.compiler_version is empty"
    [[ -n "$mhost" ]] || die "$tool parameters.build_host is empty"
  done

  [[ -d "$stage_dir/LICENSES" ]] || die "$target stage is missing LICENSES"
  [[ -n "$(find "$stage_dir/LICENSES" -mindepth 1 -maxdepth 1 -type f -printf '%f\n')" ]] ||
    die "$target stage LICENSES is empty"

  echo "verify-tools-stage: $target verified (FreeBSD structural, $release contract)" >&2
}

target=""
stage_root=""
keep_corpus=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --target)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--target requires a value"
      target=$2
      shift 2
      ;;
    --stage-root)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--stage-root requires a value"
      stage_root=$2
      shift 2
      ;;
    --keep-corpus)
      keep_corpus=1
      shift
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

[[ -n "$target" ]] || {
  usage
  die "--target is required"
}
[[ -n "$stage_root" ]] || {
  usage
  die "--stage-root is required"
}
stage_dir="$stage_root/$target"
manifest="$stage_dir/manifest.json"
[[ -s "$manifest" ]] || die "missing manifest: $manifest"

case "$target" in
  freebsd_*)
    ecs_verify_freebsd_stage "$target" "$stage_dir" "$manifest"
    exit 0
    ;;
esac

# 构建口径来自构建脚本本身。
case "$target" in
  linux_*) params=$("$ECS_REPO_ROOT/scripts/build_tools_container.sh" --target "$target" --print-params) ;;
  *) die "unsupported target: $target" ;;
esac
toolchain_mode=$(awk -F= '$1 == "toolchain_mode" { print $2 }' <<<"$params")
target_runner=$(awk -F= '$1 == "target_runner" { print $2 }' <<<"$params")
npb_class=$(awk -F= '$1 == "npb_ci_smoke_class" { print $2 }' <<<"$params")
[[ -n "$toolchain_mode" && -n "$target_runner" && -n "$npb_class" ]] ||
  die "could not resolve build parameters for $target"

echo "verify-tools-stage: $target mode=$toolchain_mode runner=$target_runner npb_class=$npb_class" >&2

# Canonical parser/validator owns manifest structure, fields, tool set, and
# architecture semantics. The expected build mode, smoke runner, and NPB
# class are stage-specific values supplied by the build container, so they are
# checked by the same Go entry point rather than duplicated in jq.
go run "$ECS_REPO_ROOT/cmd/tools-manifest-check" \
  --target "$target" \
  --toolchain-mode "$toolchain_mode" \
  --smoke-runner "$target_runner" \
  --npb-smoke-class "$npb_class" \
  "$manifest"

# 当前 target 的全部工具都必须真的在 stage 里，且可执行。
mapfile -t target_tools < <(ecs_target_tool_names "$target")
for tool in "${target_tools[@]}"; do
  tool_path="$stage_dir/bin/$tool"
  [[ -x "$tool_path" ]] || die "$target stage is missing an executable $tool"
done
[[ -d "$stage_dir/LICENSES" ]] || die "$target stage is missing LICENSES"

corpus="$stage_dir/share/ecs/corpus/$ECS_CORPUS_NAME"
goos=$(ecs_lock_target_field "$target" goos) || die "could not resolve OS for $target"
if [[ "$goos" == freebsd ]]; then
  [[ ! -e "$corpus" ]] || die "$target stage unexpectedly contains the separately packaged Silesia corpus"
  echo "verify-tools-stage: $target verified; the shared Silesia corpus is packaged separately" >&2
  exit 0
fi
if [[ "$keep_corpus" -eq 1 ]]; then
  echo "verify-tools-stage: $target verified, corpus kept" >&2
  exit 0
fi

[[ -f "$corpus" ]] || die "$target stage is missing the Silesia corpus"

# 容器以 root 写出这些文件，宿主上的普通用户需要 sudo 才能删。
remove() {
  if rm -f -- "$1" 2>/dev/null; then
    return 0
  fi
  sudo rm -f -- "$1"
}
remove "$corpus"
rmdir "$stage_dir/share/ecs/corpus" "$stage_dir/share/ecs" "$stage_dir/share" 2>/dev/null ||
  sudo rmdir "$stage_dir/share/ecs/corpus" "$stage_dir/share/ecs" "$stage_dir/share" 2>/dev/null || true
[[ ! -e "$corpus" ]] || die "corpus is still present after removal"

echo "verify-tools-stage: $target verified, corpus removed" >&2
