#!/bin/sh
# ecs 报告对比脚本
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/compare.sh | sh -s -- before.json after.json
#       └ 下载并校验 ecs，对比给定的本地报告，把结果留在 /tmp，然后删掉二进制
#   curl -fsSL .../compare.sh | sh -s -- --install before.json after.json
#       └ 同上，并把校验过的这份二进制交给 install.sh 装到本地
#   curl -fsSL .../compare.sh | sh -s -- --format txt,json a.json b.json c.json
#       └ 未识别的参数原样透传给 ecs compare
#
# 为什么不用 run.sh：
#   run.sh 一千七百行里绝大部分是工具包下载、manifest 校验、apt 临时安装、
#   Ookla 官方源、语料准备和临时 PATH 拼装。对比是纯本地计算——不跑基准、
#   不联网测速、不需要任何基准工具，只需要 ecs 二进制本身。为对比两个 JSON
#   而拉一个一千七百行的脚本不合比例。
#
# 这个脚本做什么、不做什么：
#   - 只下载 ecs 主程序，校验 SHA-256 之后才执行它；
#   - 不安装任何系统包，不调用 sudo，不改动包管理器；
#   - 不在当前目录创建任何东西。二进制放在一个 /tmp 工作目录里，退出时删除；
#     对比结果放在另一个 /tmp 目录里，**保留**并打印路径。
#   - ECS_KEEP=1 连工作目录一起保留，用于排障。

set -eu

REPO="${ECS_REPOSITORY:-CST-Cat/ecs}"
VERSION="${ECS_VERSION:-latest}"
KEEP="${ECS_KEEP:-0}"
INSTALL=0
CACHE="${ECS_CACHE:-1}"
case "$CACHE" in
  0|no|NO|false|FALSE) CACHE=0 ;;
  *) CACHE=1 ;;
esac

# 脚本自身的提示跟随语言：先看参数里的 --lang，其次看环境变量。
LANG_SEL=""
for arg in "$@"; do
  case "$arg" in
    --lang=*) LANG_SEL="${arg#--lang=}" ;;
    -lang=*) LANG_SEL="${arg#-lang=}" ;;
    --lang|-lang) LANG_SEL="__next__" ;;
    *) [ "$LANG_SEL" = "__next__" ] && LANG_SEL="$arg" ;;
  esac
done
[ -n "$LANG_SEL" ] && [ "$LANG_SEL" != "__next__" ] || LANG_SEL="${ECS_LANG:-${LC_ALL:-${LANG:-}}}"
case "$LANG_SEL" in
  en*|EN*) UI=en ;;
  *) UI=zh ;;
esac

say() {
  if [ "$UI" = "en" ]; then printf 'ecs: %s\n' "$2" >&2; else printf 'ecs: %s\n' "$1" >&2; fi
}
die() {
  if [ "$UI" = "en" ]; then printf 'ecs: %s\n' "$2" >&2; else printf 'ecs: %s\n' "$1" >&2; fi
  exit 1
}

usage() {
  if [ "$UI" = "en" ]; then
    cat >&2 <<'EN'
ecs compare — compare two or more local ecs JSON reports

Usage:
  compare.sh [--install] [ecs compare options] REPORT.json REPORT.json [REPORT.json ...]

  --install    also install the verified binary through install.sh
  --no-cache   do not reuse or populate the cached binary
  --help       show this message

Any other option is passed straight to `ecs compare` (--format, --reference,
--lang, --name, --color ...), in either `--flag value` or `--flag=value` form.

The verified binary is cached under ${XDG_CACHE_HOME:-~/.cache}/ecs/<tag> and
reused on later runs; its digest is re-checked every time before it is run.
Reports are written under /tmp and kept unless --output selects another
directory; the downloaded binary is removed on exit unless ECS_KEEP=1.
EN
  else
    cat >&2 <<'ZH'
ecs compare —— 对比两份或更多本地 ecs JSON 报告

用法：
  compare.sh [--install] [ecs compare 的参数] 报告.json 报告.json [报告.json ...]

  --install    顺便把校验过的二进制交给 install.sh 装到本地
  --no-cache   不复用也不写入缓存
  --help       显示本说明

其余参数原样透传给 `ecs compare`（--format、--reference、--lang、--name、
--color 等），`--选项 值` 和 `--选项=值` 两种写法都可以。

校验过的二进制缓存在 ${XDG_CACHE_HOME:-~/.cache}/ecs/<tag> 下供下次复用，
每次使用前都会重新核对摘要。对比结果写在 /tmp 下并保留（用 --output 可改到
别处）；下载来的二进制在退出时删除，设 ECS_KEEP=1 可保留。
ZH
  fi
}

# 分离出本脚本自己的开关，其余原样交给 ecs compare。
# 位置参数（报告路径）也一并交给它——路径相对调用者的当前目录，这里不 cd。
PASSTHROUGH=""
REPORT_COUNT=0
OUTPUT_GIVEN=0
EXPECT_VALUE=""
quote() { printf "%s" "$1" | sed "s/'/'\\\\''/g"; }

# 需要单独取值的选项，与 ecs compare 的 normalizeCompareArgs 保持一致。
#
# 少列一个，它的取值就会掉进下面的位置参数分支被当成报告路径，报出
# "找不到报告文件：txt" 这种与真实原因毫无关系的错误。
takes_value() {
  case "$1" in
    --lang | -lang | --format | -format | --output | -output) return 0 ;;
    --name | -name | --reference | -reference | --color | -color) return 0 ;;
  esac
  return 1
}

for arg in "$@"; do
  if [ -n "$EXPECT_VALUE" ]; then
    EXPECT_VALUE=""
    PASSTHROUGH="$PASSTHROUGH '$(quote "$arg")'"
    continue
  fi
  case "$arg" in
    --install) INSTALL=1; continue ;;
    --no-cache) CACHE=0; continue ;;
    -h|--help) usage; exit 0 ;;
    --output|-output|--output=*|-output=*) OUTPUT_GIVEN=1 ;;
  esac
  if takes_value "$arg"; then
    EXPECT_VALUE=$arg
    PASSTHROUGH="$PASSTHROUGH '$(quote "$arg")'"
    continue
  fi
  case "$arg" in
    -*) : ;;
    *)
      [ -f "$arg" ] || die "找不到报告文件：$arg" "no such report file: $arg"
      [ -r "$arg" ] || die "报告文件不可读：$arg" "report file is not readable: $arg"
      REPORT_COUNT=$((REPORT_COUNT + 1))
      ;;
  esac
  PASSTHROUGH="$PASSTHROUGH '$(quote "$arg")'"
done
[ -z "$EXPECT_VALUE" ] || die "$EXPECT_VALUE 缺少取值" "$EXPECT_VALUE requires a value"

[ "$REPORT_COUNT" -ge 2 ] || {
  usage
  die "至少需要两份报告" "at least two reports are required"
}

case "$(uname -s)" in
  Linux) : ;;
  *) die "ecs 只支持 Linux" "ecs only supports Linux" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)   ARCH=amd64 ;;
  aarch64|arm64)  ARCH=arm64 ;;
  armv7l|armv7)   ARCH=armv7 ;;
  i386|i686|x86)  ARCH=386 ;;
  s390x)          ARCH=s390x ;;
  riscv64)        ARCH=riscv64 ;;
  ppc64le)        ARCH=ppc64le ;;
  *) die "不支持的架构：$(uname -m)" "unsupported architecture: $(uname -m)" ;;
esac

if [ "$VERSION" = "latest" ]; then
  BASE="https://github.com/${REPO}/releases/latest/download"
else
  BASE="https://github.com/${REPO}/releases/download/${VERSION}"
fi
ASSET="ecs_linux_${ARCH}.tar.gz"

# 两个目录职责分开，都在 /tmp 下，都不碰调用者的当前目录：
#   WORK 放二进制和下载中间文件，退出时删除；
#   OUT  放对比结果，保留。
# 产出必须在 WORK 之外，否则会被清理逻辑一并删掉。
WORK_ROOT="/tmp"
if [ -n "${TMPDIR:-}" ]; then
  case "$TMPDIR" in
    /*) WORK_ROOT=$TMPDIR ;;
    *) die "TMPDIR 必须是绝对路径" "TMPDIR must be an absolute path" ;;
  esac
fi
[ -d "$WORK_ROOT" ] || die "临时目录不存在：$WORK_ROOT" "temporary directory does not exist: $WORK_ROOT"
WORK=$(mktemp -d "$WORK_ROOT/ecs-compare.XXXXXX")
# 用户自己给了 --output 时不建这个目录：建了就是在 /tmp 下留一个空目录，
# 而且随后那句"结果保留在 …"会指向一个根本没有结果的地方。
OUT=""
[ "$OUTPUT_GIVEN" -eq 1 ] || OUT=$(mktemp -d "$WORK_ROOT/ecs-comparison.XXXXXX")

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  if [ "$KEEP" = "1" ]; then
    say "工作目录保留在 $WORK" "work directory kept at $WORK"
  else
    rm -rf "$WORK" || say "工作目录清理失败：$WORK" "failed to remove the work directory: $WORK"
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --https-only --tries=3 --timeout=20 -O "$2" "$1"
  else
    die "需要 curl 或 wget" "curl or wget is required"
  fi
}

# ---- 二进制缓存 ----
#
# 对比天然是反复执行的操作：比一次、改点什么、再比一次。每次重下 10MB 没有
# 道理。缓存目录不在 PATH，不算安装——要装进 PATH 用 --install。
#
# 缓存只在能确定具体版本时启用：latest 通过 releases/latest 的重定向解析成
# tag。解析不出来就不缓存，宁可多下一次，也不要把不同版本混进同一个键。

cache_root() {
  printf '%s/ecs\n' "${XDG_CACHE_HOME:-$HOME/.cache}"
}

digest_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}' | tr '[:upper:]' '[:lower:]'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}' | tr '[:upper:]' '[:lower:]'
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$1" | awk '{print $NF}' | tr '[:upper:]' '[:lower:]'
  else
    return 1
  fi
}

resolve_tag() {
  if [ "$VERSION" != "latest" ]; then
    printf '%s\n' "$VERSION"
    return 0
  fi
  command -v curl >/dev/null 2>&1 || return 1
  resolved=$(curl -fsSL -I -o /dev/null -w '%{url_effective}' \
    --proto '=https' --tlsv1.2 --retry 2 --connect-timeout 10 \
    "https://github.com/${REPO}/releases/latest" 2>/dev/null) || return 1
  case "$resolved" in
    */releases/tag/*) printf '%s\n' "${resolved##*/releases/tag/}" ;;
    *) return 1 ;;
  esac
}

# ecs 二进制的落点。它是一个路径定义，与这一份是下载来的还是缓存来的无关，
# 因此必须在分支之外——放进分支里会让缓存命中那条路径拿到一个未设的变量。
BINARY="$WORK/ecs"

CACHED=""
if [ "$CACHE" -eq 1 ]; then
  if TAG=$(resolve_tag); then
    CACHED="$(cache_root)/${TAG}/ecs"
  else
    say "无法解析 Release 版本，本次不使用缓存" "could not resolve the release tag; not using the cache"
    CACHE=0
  fi
fi

# 命中缓存也要重新校验摘要再用。10MB 的 SHA-256 约 50 毫秒，换来的是"本地
# 缓存被改过也不会被执行"——这是缓存能默认打开的前提。
FROM_CACHE=0
if [ -n "$CACHED" ] && [ -f "$CACHED" ] && [ -f "${CACHED}.sha256" ]; then
  cache_want=$(cat "${CACHED}.sha256" 2>/dev/null || printf '')
  cache_have=$(digest_of "$CACHED" 2>/dev/null || printf '')
  if [ -n "$cache_want" ] && [ "$cache_want" = "$cache_have" ]; then
    cp "$CACHED" "$BINARY" && chmod +x "$BINARY" && FROM_CACHE=1
    say "使用缓存的 ecs（$TAG）" "using the cached ecs ($TAG)"
  else
    say "缓存摘要不符，重新下载" "cached digest mismatch; downloading again"
    rm -f "$CACHED" "${CACHED}.sha256"
  fi
fi

if [ "$FROM_CACHE" -eq 0 ]; then
say "下载 $ASSET" "downloading $ASSET"
fetch "${BASE}/${ASSET}" "${WORK}/${ASSET}" ||
  die "下载失败；仓库是否已发布 Release？" "download failed; has the repository published a Release?"
fetch "${BASE}/checksums.txt" "${WORK}/checksums.txt" ||
  die "下载校验文件失败" "failed to download the checksum file"
# 校验不可跳过：这是 curl|sh 这条路径上唯一能自证内容未被替换的环节。
EXPECTED=$(awk -v f="$ASSET" '$2 == f {print $1; exit}' "${WORK}/checksums.txt" | tr '[:upper:]' '[:lower:]')
[ -n "$EXPECTED" ] || die "校验文件里没有 ${ASSET} 的条目" "no checksum entry for ${ASSET}"
case "$EXPECTED" in
  *[!a-f0-9]*) die "校验值格式非法" "malformed checksum value" ;;
esac
[ "${#EXPECTED}" -eq 64 ] || die "校验值长度非法" "malformed checksum length"

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "${WORK}/${ASSET}" | awk '{print $1}' | tr '[:upper:]' '[:lower:]')
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "${WORK}/${ASSET}" | awk '{print $1}' | tr '[:upper:]' '[:lower:]')
elif command -v openssl >/dev/null 2>&1; then
  ACTUAL=$(openssl dgst -sha256 "${WORK}/${ASSET}" | awk '{print $NF}' | tr '[:upper:]' '[:lower:]')
else
  die "需要 sha256sum、shasum 或 openssl 才能校验下载内容" \
    "sha256sum, shasum or openssl is required to verify the download"
fi
[ "$ACTUAL" = "$EXPECTED" ] || die "校验失败，拒绝执行下载内容" "checksum mismatch; refusing to run the download"
say "校验通过" "checksum verified"

tar -xzf "${WORK}/${ASSET}" -C "$WORK" ecs || die "解包失败" "failed to extract the archive"
[ -f "$BINARY" ] && [ ! -L "$BINARY" ] || die "归档里没有 ecs 可执行文件" "the archive does not contain an ecs binary"
chmod +x "$BINARY"

# 写进缓存：先写 .partial 再改名，中途失败不会留下一个能被当作有效缓存的
# 半截文件。缓存失败一律不致命——它只是加速，不是功能。
if [ -n "$CACHED" ]; then
  cache_dir=$(dirname "$CACHED")
  if mkdir -p "$cache_dir" 2>/dev/null &&
    cache_digest=$(digest_of "$BINARY" 2>/dev/null) &&
    cp "$BINARY" "${CACHED}.partial" 2>/dev/null; then
    chmod 0700 "$(cache_root)" 2>/dev/null || true
    chmod 0755 "${CACHED}.partial" 2>/dev/null || true
    printf '%s\n' "$cache_digest" >"${CACHED}.sha256.partial" 2>/dev/null &&
      mv "${CACHED}.partial" "$CACHED" 2>/dev/null &&
      mv "${CACHED}.sha256.partial" "${CACHED}.sha256" 2>/dev/null &&
      say "已缓存到 $CACHED" "cached at $CACHED" ||
      rm -f "${CACHED}.partial" "${CACHED}.sha256.partial" 2>/dev/null || true
  fi
fi
fi

say "对比 $REPORT_COUNT 份报告" "comparing $REPORT_COUNT reports"
# eval 只用于展开上面逐项加过引号的参数，没有未经引用的用户输入进入这一行。
if [ "$OUTPUT_GIVEN" -eq 1 ]; then
  eval "\"\$BINARY\" compare$PASSTHROUGH"
else
  eval "\"\$BINARY\" compare --output \"\$OUT\"$PASSTHROUGH"
fi

if [ "$INSTALL" = "1" ]; then
  say "安装这份已校验的二进制" "installing the verified binary"
  fetch "https://raw.githubusercontent.com/${REPO}/main/install.sh" "$WORK/install.sh" ||
    die "下载 install.sh 失败" "failed to download install.sh"
  ECS_REPOSITORY="$REPO" sh "$WORK/install.sh" --from "$BINARY"
fi

# 只有目录是本脚本挑的，才由本脚本负责报告它。用户自己指定 --output 时，
# ecs 已经逐个格式打印过路径，再复述一遍只会多一个可能说错的地方。
if [ -n "$OUT" ]; then
  say "对比结果保留在 $OUT" "comparison written to $OUT"
  printf '%s\n' "$OUT"
fi
