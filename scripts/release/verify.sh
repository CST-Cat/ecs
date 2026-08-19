#!/usr/bin/env bash
set -euo pipefail

# 发布前校验：这组制品是不是真的由这次提交、这条工具链产出的。
#
# 校验对象是**解包出来的实际二进制**，不是构建日志。构建日志说了什么不重要，
# 二进制里 go version -m 记的东西才是用户拿到的东西：
#
#   - Go 工具链必须等于本次构建实测的版本（--build-go-version），而不是某个
#     写死的期望值。这个值只能由构建方给出：让校验方自己去问 go env 就等于
#     用同一个假设验证它自己。正式 Release 编译器由 release.yml 的
#     ECS_RELEASE_GO 选择；根 go.mod 只声明源码最低兼容版本，devtools/go.mod
#     只管理 staticcheck/govulncheck 等开发工具自身的构建环境。安全升级只改
#     Release compiler pin，不提高源码最低兼容版本。
#   - vcs.revision 必须等于冻结的发布 SHA；
#   - vcs.modified 必须为 false，否则构建时工作区是脏的。
#
# 此外校验语料的尺寸与摘要、发布物齐全性、checksums 与实际文件一一对应，
# 以及工具包里没有混进语料。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/release/verify.sh --dist DIR --build-go-version GOVERSION --revision SHA [--no-tools]
       scripts/release/verify.sh --dist DIR --dry-run

  --build-go-version  本次构建实测的工具链，如 go1.26.5（由构建方给出）
  --revision          冻结的发布提交 SHA
  --no-tools          只校验主程序与语料
  --dry-run           本地演练：隐含 --no-tools，自行取工具链与 HEAD，并跳过
                      提交相关断言（本地工作区通常是脏的）。发布路径绝不能用：
                      那两条断言正是用来挡住脏工作区构建的。
USAGE
}

die() {
  echo "release-verify: $*" >&2
  exit 1
}

dist=""
build_go_version=""
revision=""
with_tools=1
dry_run=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --dist)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--dist requires a value"
      dist=$2
      shift 2
      ;;
    --build-go-version)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--build-go-version requires a value"
      build_go_version=$2
      shift 2
      ;;
    --revision)
      [[ "$#" -ge 2 && -n "$2" ]] || die "--revision requires a value"
      revision=$2
      shift 2
      ;;
    --no-tools)
      with_tools=0
      shift
      ;;
    --dry-run)
      dry_run=1
      with_tools=0
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

[[ -n "$dist" ]] || {
  usage
  die "--dist is required"
}
if [[ "$dry_run" -eq 1 ]]; then
  build_go_version=${build_go_version:-$(go env GOVERSION)}
  revision=${revision:-$(git -C "$ECS_REPO_ROOT" rev-parse HEAD)}
else
  [[ -n "$build_go_version" && -n "$revision" ]] || {
    usage
    die "--build-go-version 与 --revision 必填（或用 --dry-run 做本地演练）"
  }
fi
[[ "$dist" == /* ]] || dist="$ECS_REPO_ROOT/$dist"
[[ -d "$dist" ]] || die "no such dist directory: $dist"
[[ "$build_go_version" == go* ]] || die "--build-go-version must look like go1.26.5, got $build_go_version"

verify_root=$(mktemp -d)
trap 'rm -rf -- "$verify_root"' EXIT

echo "release-verify: 期望工具链 $build_go_version，提交 $revision" >&2

# ---- 主程序二进制 ----
listing=$(ecs_release_binaries "$dist" "$verify_root") || die "无法解出主程序二进制"
while IFS=$'\t' read -r name binary; do
  metadata=$(go version -m "$binary")
  grep -F -x "$binary: $build_go_version" <<<"$metadata" >/dev/null ||
    die "$name 的 Go 工具链不是 $build_go_version：$(head -1 <<<"$metadata")"

  if [[ "$dry_run" -eq 1 ]]; then
    echo "release-verify: $name 工具链一致（--dry-run：跳过提交断言）" >&2
    continue
  fi
  grep -F $'\tbuild\tvcs=git' <<<"$metadata" >/dev/null ||
    die "$name 缺少 VCS 元数据"
  grep -F $'\tbuild\tvcs.revision='"$revision" <<<"$metadata" >/dev/null ||
    die "$name 的 vcs.revision 不是 $revision"
  grep -F $'\tbuild\tvcs.modified=false' <<<"$metadata" >/dev/null ||
    die "$name 构建自一个脏工作区"
  echo "release-verify: $name 元数据一致" >&2
done <<<"$listing"

# ---- 语料 ----
corpus_archive="$dist/ecs-corpus_silesia-v1.tar.gz"
[[ -s "$corpus_archive" ]] || die "缺少语料发布物"
corpus_listing=$(tar -tzf "$corpus_archive")
[[ "$corpus_listing" == ecs-silesia-v1.corpus ]] || die "语料归档内容异常：$corpus_listing"
tar -xzf "$corpus_archive" -C "$verify_root"
corpus="$verify_root/ecs-silesia-v1.corpus"
[[ "$(stat -c %s "$corpus")" -eq "$ECS_CORPUS_BYTES" ]] || die "语料尺寸不符"
[[ "$(sha256sum "$corpus" | awk '{print $1}')" == "$ECS_CORPUS_SHA256" ]] || die "语料摘要不符"
echo "release-verify: 语料尺寸与摘要一致" >&2

# ---- 发布物清单 ----
assets=()
for arch in "${ECS_ARCHES[@]}"; do
  assets+=("ecs_linux_${arch}.tar.gz")
done
if [[ "$with_tools" -eq 1 ]]; then
  for arch in "${ECS_ARCHES[@]}"; do
    assets+=("ecs-tools_linux_${arch}.tar.gz")
  done
fi
assets+=(ecs-corpus_silesia-v1.tar.gz)

for asset in "${assets[@]}"; do
  [[ -s "$dist/$asset" ]] || die "缺少发布物 $asset"
done

if [[ "$with_tools" -eq 1 ]]; then
  for arch in "${ECS_ARCHES[@]}"; do
    asset="$dist/ecs-tools_linux_$arch.tar.gz"
    if tar -tzf "$asset" | grep -E '(^|/)(share|ecs-silesia-v1[.]corpus)(/|$)' >/dev/null; then
      die "$asset 里混进了语料或 share 目录"
    fi
  done
  echo "release-verify: ${#ECS_ARCHES[@]} 个工具包都不含语料" >&2
fi

# ---- checksums ----
checksums="$dist/checksums.txt"
[[ -s "$checksums" ]] || die "缺少 checksums.txt"
[[ "$(wc -l <"$checksums")" -eq "${#assets[@]}" ]] ||
  die "checksums 行数 = $(wc -l <"$checksums")，want ${#assets[@]}"
for asset in "${assets[@]}"; do
  grep -F "  $asset" "$checksums" >/dev/null || die "checksums 缺少 $asset"
done
(cd "$dist" && sha256sum -c checksums.txt >/dev/null)
echo "release-verify: checksums 与 ${#assets[@]} 个发布物一一对应" >&2

echo "release-verify: 全部校验通过"
