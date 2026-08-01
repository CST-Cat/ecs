#!/bin/sh
# ecs 一键引导脚本
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
#   curl -fsSL .../run.sh | sh -s -- --profile full --lang en
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

fail() { printf 'ecs: %s\n' "$1" >&2; exit 1; }

[ "$(uname -s)" = "Linux" ] || fail "只支持 Linux（检测到 $(uname -s)）"

case "$(uname -m)" in
  x86_64|amd64)   ARCH=amd64 ;;
  aarch64|arm64)  ARCH=arm64 ;;
  armv7l|armv7)   ARCH=armv7 ;;
  i386|i686|x86)  ARCH=386 ;;
  s390x)          ARCH=s390x ;;
  riscv64)        ARCH=riscv64 ;;
  ppc64le)        ARCH=ppc64le ;;
  *) fail "不支持的架构：$(uname -m)" ;;
esac

if [ "$VERSION" = "latest" ]; then
  BASE="https://github.com/${REPO}/releases/latest/download"
else
  BASE="https://github.com/${REPO}/releases/download/${VERSION}"
fi
ASSET="ecs_linux_${ARCH}.tar.gz"

WORK=$(mktemp -d "${TMPDIR:-/tmp}/ecs-run.XXXXXX")
if [ "$KEEP" = "1" ]; then
  printf 'ecs: 二进制保留在 %s\n' "$WORK" >&2
else
  trap 'rm -rf "$WORK"' EXIT HUP INT TERM
fi

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --https-only --tries=3 --timeout=20 -O "$2" "$1"
  else
    fail "需要 curl 或 wget"
  fi
}

printf 'ecs: 下载 %s\n' "$ASSET" >&2
fetch "${BASE}/${ASSET}" "${WORK}/${ASSET}" || fail "下载失败；仓库是否已发布 Release？"
fetch "${BASE}/checksums.txt" "${WORK}/checksums.txt" || fail "下载校验文件失败"

# 校验不可跳过：这是 curl|sh 这条路径上唯一能自证内容未被替换的环节。
EXPECTED=$(awk -v f="$ASSET" '$2 == f {print $1; exit}' "${WORK}/checksums.txt")
[ -n "$EXPECTED" ] || fail "校验文件里没有 ${ASSET} 的条目"
case "$EXPECTED" in
  *[!A-Fa-f0-9]*|"") fail "校验值格式非法" ;;
esac
[ "${#EXPECTED}" -eq 64 ] || fail "校验值长度非法"

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "${WORK}/${ASSET}" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "${WORK}/${ASSET}" | awk '{print $1}')
else
  fail "需要 sha256sum 或 shasum 才能校验"
fi
[ "$ACTUAL" = "$EXPECTED" ] || fail "SHA-256 校验失败：内容与发布版本不一致"
printf 'ecs: SHA-256 已校验\n' >&2

tar -xzf "${WORK}/${ASSET}" -C "$WORK" ecs
[ -f "${WORK}/ecs" ] && [ ! -L "${WORK}/ecs" ] || fail "压缩包里没有常规的 ecs 文件"
chmod +x "${WORK}/ecs"

# 全部参数原样透传，因此 run.sh 支持 ecs 的每一个选项。
exec "${WORK}/ecs" "$@"
