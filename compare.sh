#!/bin/sh
# ecs 报告对比脚本
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/compare.sh | sh -s -- before.json after.json
#       └ 下载并校验 ecs，对比给定的本地报告，把结果留在 /tmp，然后删掉二进制
#   curl -fsSL .../compare.sh | sh -s -- --format json,md,html a.json b.json c.json
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
#     对比结果放在另一个 /tmp 目录里，保留并打印路径。

set -eu

REPO="${ECS_REPOSITORY:-CST-Cat/ecs}"
VERSION="${ECS_VERSION:-latest}"

# wrapper 自身的提示跟随 ECS_LANG；二进制全局语言写作 `ecs --lang en compare ...`。
LANG_SEL="${ECS_LANG:-${LC_ALL:-${LANG:-}}}"
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
  compare.sh [ecs compare options] REPORT.json REPORT.json [REPORT.json ...]

  --help       show this message

All other arguments are passed unchanged to `ecs compare`.
The verified binary is downloaded into a temporary work directory and removed
when the wrapper exits. Reports are written under /tmp and kept unless
--output selects another directory.
EN
  else
    cat >&2 <<'ZH'
ecs compare —— 对比两份或更多本地 ecs JSON 报告

用法：
  compare.sh [ecs compare 的参数] 报告.json 报告.json [报告.json ...]

  --help       显示本说明

其余参数原样透传给 `ecs compare`。
校验过的二进制下载到临时工作目录，脚本退出时删除。对比结果写在 /tmp
下并保留；用 --output 可改到其他目录。
ZH
  fi
}

# 只有首个参数是 wrapper 自己的帮助；其余 --help 原样交给 ecs。这样值为
# --help 的 compare 参数不会被 wrapper 抢走，也不需要在 shell 里重建 grammar。
case "${1:-}" in
  -h|--help) usage; exit 0 ;;
esac

case "$(uname -s)" in
  Linux) : ;;
  *) die "ecs 只支持 Linux" "ecs only supports Linux" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)   ARCH=amd64 ;;
  aarch64|arm64)  ARCH=arm64 ;;
  armv7l|armv7)   ARCH=armv7 ;;
  i386|i686|x86)   ARCH=386 ;;
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
#   WORK 放二进制、下载中间文件和一次性的默认 output，退出时删除；
#   OUT  只在默认 output 实际被使用后移出 WORK 并保留。
# 用户参数始终原样放在 wrapper 注入的默认 output 之后，由 ecs 的
# normalizeCompareArgs 和 flag parser 决定其真正的 output；shell 不解析 compare grammar。
WORK_ROOT="/tmp"
if [ -n "${TMPDIR:-}" ]; then
  case "$TMPDIR" in
    /*) WORK_ROOT=$TMPDIR ;;
    *) die "TMPDIR 必须是绝对路径" "TMPDIR must be an absolute path" ;;
  esac
fi
[ -d "$WORK_ROOT" ] || die "临时目录不存在：$WORK_ROOT" "temporary directory does not exist: $WORK_ROOT"
WORK=$(mktemp -d "$WORK_ROOT/ecs-compare.XXXXXX")

cleanup() {
  status=$?
  trap - EXIT INT TERM HUP
  rm -rf "$WORK" || say "工作目录清理失败：$WORK" "failed to remove the work directory: $WORK"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

DEFAULT_OUT="$WORK/output"
OUT=""

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --https-only --tries=3 --timeout=20 -O "$2" "$1"
  else
    die "需要 curl 或 wget" "curl or wget is required"
  fi
}

BINARY="$WORK/ecs"
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

say "开始对比" "starting comparison"
if "$BINARY" compare --output "$DEFAULT_OUT" "$@"; then
  compare_status=0
else
  compare_status=$?
fi

# 如果用户参数中有后出现且生效的 output，ecs 会把结果写到用户路径，
# DEFAULT_OUT 不会存在；只有默认 output 真的承接了结果时才移出 WORK。
if [ "$compare_status" -eq 0 ] && [ -d "$DEFAULT_OUT" ]; then
  OUT=$(mktemp -d "$WORK_ROOT/ecs-comparison.XXXXXX")
  if ! rmdir "$OUT" || ! mv "$DEFAULT_OUT" "$OUT"; then
    rm -rf "$OUT" 2>/dev/null || :
    die "无法保留对比结果：$OUT" "failed to retain comparison output: $OUT"
  fi
fi

if [ "$compare_status" -ne 0 ]; then
  exit "$compare_status"
fi

if [ -n "$OUT" ]; then
  say "对比结果保留在 $OUT" "comparison written to $OUT"
  printf '%s\n' "$OUT"
fi
