#!/bin/sh
# ecs 一键引导脚本
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
#       └ 有终端时进交互向导；没有终端（cron/CI）则按 standard 档直接开跑
#   curl -fsSL .../run.sh | sh -s -- --yes
#       └ 跳过向导，直接按默认配置测
#   curl -fsSL .../run.sh | sh -s -- --profile full --lang en
#       └ 带了参数就不再打扰，直接按参数跑
#   wget -qO- .../run.sh | sh -s -- --only cpu,memory,disk
#
# 这个脚本只做三件事：拿到二进制、校验、执行。它刻意保持简短，方便你在
# 管道执行前先读一遍——`curl | sh` 把信任完全交给了下载源，这一点没有办法
# 靠脚本自身解决，能做的是让脚本短到可以一眼读完，并且不隐藏任何步骤。
#
# 与直接下载相比，这里不会：改包管理器、写系统目录、调用 sudo、留下后台进程。
# 二进制放在临时目录，跑完即删。想长期安装请用 install.sh。

set -eu

REPO="${ECS_REPOSITORY:-CST-Cat/ecs}"
VERSION="${ECS_VERSION:-latest}"
KEEP="${ECS_KEEP:-0}"

# 脚本自身的提示也跟随语言：从参数里取 --lang，其次看环境变量。
# 这些提示在下载 ecs 之前就要打印，所以只能在 shell 里判断。
LANG_SEL=""
for arg in "$@"; do
  case "$arg" in
    --lang=*) LANG_SEL="${arg#--lang=}" ;;
    -lang=*)  LANG_SEL="${arg#-lang=}" ;;
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

[ "$(uname -s)" = "Linux" ] || die "只支持 Linux（检测到 $(uname -s)）" "Linux only (detected $(uname -s))"

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

WORK=$(mktemp -d "${TMPDIR:-/tmp}/ecs-run.XXXXXX")
if [ "$KEEP" = "1" ]; then
  say "二进制保留在 $WORK" "binary kept at $WORK"
else
  trap 'rm -rf "$WORK"' EXIT HUP INT TERM
fi

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --https-only --tries=3 --timeout=20 -O "$2" "$1"
  else
    die "需要 curl 或 wget" "curl or wget is required"
  fi
}

say "下载 $ASSET" "downloading $ASSET"
fetch "${BASE}/${ASSET}" "${WORK}/${ASSET}" || die "下载失败；仓库是否已发布 Release？" "download failed; has a Release been published?"
fetch "${BASE}/checksums.txt" "${WORK}/checksums.txt" || die "下载校验文件失败" "failed to download the checksum file"

# 校验不可跳过：这是 curl|sh 这条路径上唯一能自证内容未被替换的环节。
EXPECTED=$(awk -v f="$ASSET" '$2 == f {print $1; exit}' "${WORK}/checksums.txt")
[ -n "$EXPECTED" ] || die "校验文件里没有 ${ASSET} 的条目" "no checksum entry for ${ASSET}"
case "$EXPECTED" in
  *[!A-Fa-f0-9]*|"") die "校验值格式非法" "malformed checksum value" ;;
esac
[ "${#EXPECTED}" -eq 64 ] || die "校验值长度非法" "malformed checksum length"

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "${WORK}/${ASSET}" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "${WORK}/${ASSET}" | awk '{print $1}')
else
  die "需要 sha256sum 或 shasum 才能校验" "sha256sum or shasum is required to verify"
fi
[ "$ACTUAL" = "$EXPECTED" ] || die "SHA-256 校验失败：内容与发布版本不一致" "SHA-256 mismatch: content differs from the published release"
say "SHA-256 已校验" "SHA-256 verified"

tar -xzf "${WORK}/${ASSET}" -C "$WORK" ecs
[ -f "${WORK}/ecs" ] && [ ! -L "${WORK}/ecs" ] || die "压缩包里没有常规的 ecs 文件" "the archive contains no regular ecs file"
chmod +x "${WORK}/ecs"

# 决定要不要进交互向导。
#
# 规则：用户没给任何测试参数、且确实有终端可用时才进向导。带了参数说明用户
# 已经想好要跑什么，这时再弹菜单只会碍事；没有终端（cron、CI、容器）则必须
# 直接开跑，否则任务会卡在等输入上。
#
# 注意判断的是 /dev/tty 而不是 stdin：`curl … | sh` 的 stdin 是那条管道，
# 脚本正从里面读，它永远不是终端，据此判断会导致所有管道安装都进不了向导。
INTERACTIVE=""
if [ "$#" -eq 0 ] && [ -r /dev/tty ] && [ -w /dev/tty ]; then
  INTERACTIVE="--interactive"
fi
# 只传了语言选项时仍然算"没想好要跑什么"，照样进向导。
if [ "$#" -le 2 ] && [ -z "$INTERACTIVE" ] && [ -r /dev/tty ] && [ -w /dev/tty ]; then
  case "$1" in
    --lang|--lang=*|-lang|-lang=*) INTERACTIVE="--interactive" ;;
  esac
fi

# 其余参数原样透传，因此 run.sh 支持 ecs 的每一个选项。
if [ -n "$INTERACTIVE" ]; then
  exec "${WORK}/ecs" "$@" "$INTERACTIVE"
fi
exec "${WORK}/ecs" "$@"
