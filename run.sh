#!/bin/sh
# ecs 一键运行脚本
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
#       └ 自动下载已校验的 ecs，按当前配置准备缺失组件，运行并生成本地报告
#   curl -fsSL .../run.sh | sh -s -- --profile full --lang en
#       └ 带参数时跳过向导；组件仍会自动准备，测试结束后只清理本次安装的组件
#
# 依赖策略：
#   - 已有的 sysbench/fio/iperf3 等组件不会改动。
#   - 缺失组件只从系统包管理器的官方配置源安装；不下载未经校验的裸二进制。
#   - 运行前记录已安装包集合，结束时只移除本次新增的包；不执行 autoremove 或全局 clean。
#   - ECS_AUTO_DEPS=0 可关闭自动安装，让 ecs 自己报告缺失组件。
#   - ECS_KEEP=1 只保留临时工作目录用于排障，不会阻止已安装组件的清理。

set -eu

REPO="${ECS_REPOSITORY:-CST-Cat/ecs}"
VERSION="${ECS_VERSION:-latest}"
KEEP="${ECS_KEEP:-0}"
AUTO_DEPS="${ECS_AUTO_DEPS:-1}"

case "$AUTO_DEPS" in
  0|no|NO|false|FALSE) AUTO_DEPS=0 ;;
  *) AUTO_DEPS=1 ;;
esac

# 脚本自身的提示也跟随语言：从参数里取 --lang，其次看环境变量。
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
PACKAGE_MANAGER=""
BEFORE_PACKAGES=""
AFTER_INSTALL_PACKAGES=""
DEPS_ATTEMPTED=0
CLEANUP_DONE=0
CLEANUP_FAILED=0
MISSING_TOOLS=""
PACKAGES=""

as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    return 1
  fi
}

package_state() {
  case "$PACKAGE_MANAGER" in
    apt)
      dpkg-query -W -f='${binary:Package}\t${db:Status-Status}\n' 2>/dev/null |
        awk -F '\t' '$2 == "installed" {print $1}' | sort -u
      ;;
    dnf|yum|rpm)
      rpm -qa --qf '%{NAME}\n' 2>/dev/null | sort -u
      ;;
    apk)
      apk info -e 2>/dev/null | sed -E 's/-[0-9][^-]*-[^-]*$//' | sort -u
      ;;
    pacman)
      pacman -Qq 2>/dev/null | sort -u
      ;;
    *) return 1 ;;
  esac
}

select_package_manager() {
  if command -v apt-get >/dev/null 2>&1 && command -v dpkg-query >/dev/null 2>&1; then
    PACKAGE_MANAGER=apt
  elif command -v dnf >/dev/null 2>&1 && command -v rpm >/dev/null 2>&1; then
    PACKAGE_MANAGER=dnf
  elif command -v yum >/dev/null 2>&1 && command -v rpm >/dev/null 2>&1; then
    PACKAGE_MANAGER=yum
  elif command -v apk >/dev/null 2>&1; then
    PACKAGE_MANAGER=apk
  elif command -v pacman >/dev/null 2>&1; then
    PACKAGE_MANAGER=pacman
  else
    return 1
  fi
}

package_for_tool() {
  case "$PACKAGE_MANAGER:$1" in
    apt:ping)      printf '%s\n' iputils-ping ;;
    dnf:ping|yum:ping|apk:ping|pacman:ping) printf '%s\n' iputils ;;
    *:smartctl)    printf '%s\n' smartmontools ;;
    *)             printf '%s\n' "$1" ;;
  esac
}

add_package() {
  case " $PACKAGES " in
    *" $1 "*) ;;
    *) PACKAGES="${PACKAGES:+$PACKAGES }$1" ;;
  esac
}

add_missing_tool() {
  case " $MISSING_TOOLS " in
    *" $1 "*) ;;
    *) MISSING_TOOLS="${MISSING_TOOLS:+$MISSING_TOOLS }$1" ;;
  esac
}

tool_exists() {
  case "$1" in
    smartctl) command -v smartctl >/dev/null 2>&1 || [ -x /usr/sbin/smartctl ] ;;
    *) command -v "$1" >/dev/null 2>&1 ;;
  esac
}

list_contains() {
  case ",$1," in
    *,"$2",*) return 0 ;;
    *) return 1 ;;
  esac
}

# 从命令行预读会影响依赖规划的选项。带 --config 时配置文件可能改写模块，
# 因而按完整配置准备，避免先删掉用户实际需要的工具。
PROFILE=standard
ONLY=""
SKIP=""
OFFLINE=0
CONFIG_GIVEN=0
EXPECT=""
for arg in "$@"; do
  if [ -n "$EXPECT" ]; then
    case "$EXPECT" in
      profile) PROFILE="$arg" ;;
      only) ONLY="$arg" ;;
      skip) SKIP="$arg" ;;
      config) CONFIG_GIVEN=1 ;;
    esac
    EXPECT=""
    continue
  fi
  case "$arg" in
    --profile) EXPECT=profile ;;
    --profile=*) PROFILE="${arg#--profile=}" ;;
    --only) EXPECT=only ;;
    --only=*) ONLY="${arg#--only=}" ;;
    --skip) EXPECT=skip ;;
    --skip=*) SKIP="${arg#--skip=}" ;;
    --config) EXPECT=config ;;
    --config=*) CONFIG_GIVEN=1 ;;
    --offline|--offline=true) OFFLINE=1 ;;
  esac
done

module_enabled() {
  module=$1
  if [ "$CONFIG_GIVEN" -eq 1 ]; then
    base_modules="system,network,cpu,memory,disk,dns,latency,speed,ports,nat,blacklist,apps,cnspeed,media,route,backtrace"
  elif [ -n "$ONLY" ]; then
    base_modules="$ONLY"
  else
    case "$PROFILE" in
      quick) base_modules="system,network,cpu,memory,disk,dns,latency" ;;
      standard) base_modules="system,network,cpu,memory,disk,dns,latency,speed,ports,nat,blacklist,apps,media,route,backtrace" ;;
      full) base_modules="system,network,cpu,memory,disk,dns,latency,speed,ports,nat,blacklist,apps,cnspeed,media,route,backtrace" ;;
      *) base_modules="system,network,cpu,memory,disk,dns,latency,speed,ports,nat,blacklist,apps,cnspeed,media,route,backtrace" ;;
    esac
  fi
  list_contains "$base_modules" "$module" || return 1
  list_contains "$SKIP" "$module" && return 1
  if [ "$OFFLINE" -eq 1 ]; then
    case "$module" in
      network|dns|latency|speed|ports|nat|blacklist|apps|cnspeed|media|route|backtrace) return 1 ;;
    esac
  fi
  return 0
}

collect_missing_tools() {
  MISSING_TOOLS=""
  if module_enabled cpu || module_enabled memory; then
    tool_exists sysbench || add_missing_tool sysbench
  fi
  if module_enabled memory; then
    tool_exists mbw || add_missing_tool mbw
  fi
  if module_enabled disk; then
    tool_exists fio || add_missing_tool fio
    tool_exists ioping || add_missing_tool ioping
    tool_exists smartctl || add_missing_tool smartctl
  fi
  if module_enabled speed; then
    tool_exists iperf3 || add_missing_tool iperf3
  fi
  if module_enabled latency; then
    tool_exists ping || add_missing_tool ping
  fi
  if module_enabled route || module_enabled backtrace; then
    if ! tool_exists nexttrace && ! tool_exists traceroute && ! tool_exists tracepath; then
      add_missing_tool traceroute
    fi
  fi
}

install_packages() {
  say "准备测试组件：$PACKAGES" "preparing test components: $PACKAGES"
  case "$PACKAGE_MANAGER" in
    apt)
      as_root env DEBIAN_FRONTEND=noninteractive apt-get update
      as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y $PACKAGES
      ;;
    dnf) as_root dnf install -y $PACKAGES ;;
    yum) as_root yum install -y $PACKAGES ;;
    apk) as_root apk add --no-cache $PACKAGES ;;
    pacman) as_root pacman -Sy --noconfirm $PACKAGES ;;
    *) return 1 ;;
  esac
}

prepare_dependencies() {
  collect_missing_tools
  if [ -z "$MISSING_TOOLS" ]; then
    say "测试组件已就绪" "test components are ready"
    return 0
  fi

  say "缺少组件：$MISSING_TOOLS" "missing components: $MISSING_TOOLS"
  if [ "$AUTO_DEPS" -eq 0 ]; then
    say "已关闭自动安装，继续运行；报告会标明未运行的标准基准。" "automatic dependency setup is disabled; continuing with explicit missing-tool warnings"
    return 0
  fi
  if ! select_package_manager; then
    die "找不到支持的包管理器；可设置 ECS_AUTO_DEPS=0 运行并接受降级报告" "no supported package manager; set ECS_AUTO_DEPS=0 to run with a degraded report"
  fi
  if ! as_root true 2>/dev/null; then
    die "准备测试组件需要 root 或免密 sudo；可设置 ECS_AUTO_DEPS=0 运行降级测试" "preparing test components requires root or sudo; set ECS_AUTO_DEPS=0 to run with a degraded report"
  fi
  for tool in $MISSING_TOOLS; do
    add_package "$(package_for_tool "$tool")"
  done
  BEFORE_PACKAGES="$WORK/packages.before"
  if ! package_state >"$BEFORE_PACKAGES"; then
    die "无法记录安装前的软件包状态，已停止以避免误删" "cannot record the pre-install package state; stopping to avoid unsafe cleanup"
  fi
  DEPS_ATTEMPTED=1
  AFTER_INSTALL_PACKAGES="$WORK/packages.after-install"
  if ! install_packages; then
    package_state >"$AFTER_INSTALL_PACKAGES" 2>/dev/null || true
    die "测试组件安装失败，未开始测试" "test component installation failed; tests were not started"
  fi
  for tool in $MISSING_TOOLS; do
    if ! tool_exists "$tool"; then
      die "组件安装后仍找不到 $tool，未开始测试" "${tool} is still unavailable after installation; tests were not started"
    fi
  done
  if ! package_state >"$AFTER_INSTALL_PACKAGES"; then
    die "无法记录安装后的软件包状态，已停止以避免不安全清理" "cannot record the post-install package state; stopping to avoid unsafe cleanup"
  fi
  say "测试组件准备完成；测试结束后将只清理本次新增包。" "test components ready; only packages added by this run will be removed afterward"
}

cleanup_packages() {
  [ "$DEPS_ATTEMPTED" -eq 1 ] || return 0
  [ -s "$BEFORE_PACKAGES" ] || return 1
  [ -s "$AFTER_INSTALL_PACKAGES" ] || return 1
  AFTER_PACKAGES="$WORK/packages.after"
  package_state >"$AFTER_PACKAGES" || return 1
  if ! cmp -s "$AFTER_INSTALL_PACKAGES" "$AFTER_PACKAGES"; then
    say "安装完成后软件包状态发生变化，已跳过清理以避免误删；临时目录保留在 $WORK" "package state changed after installation; cleanup skipped to avoid collateral removal; temporary state kept at $WORK"
    return 1
  fi
  NEW_PACKAGES=$(comm -13 "$BEFORE_PACKAGES" "$AFTER_PACKAGES")
  [ -n "$NEW_PACKAGES" ] || return 0
  say "清理本次新增组件：$NEW_PACKAGES" "removing packages added by this run: $NEW_PACKAGES"
  case "$PACKAGE_MANAGER" in
    apt) as_root env DEBIAN_FRONTEND=noninteractive apt-get purge -y $NEW_PACKAGES ;;
    dnf) as_root dnf remove -y $NEW_PACKAGES ;;
    yum) as_root yum remove -y $NEW_PACKAGES ;;
    apk) as_root apk del $NEW_PACKAGES ;;
    pacman) as_root pacman -R --noconfirm $NEW_PACKAGES ;;
    *) return 1 ;;
  esac
}

cleanup() {
  exit_status=$?
  [ "$CLEANUP_DONE" -eq 0 ] || exit "$exit_status"
  CLEANUP_DONE=1
  trap - EXIT INT TERM HUP

  if ! cleanup_packages; then
    CLEANUP_FAILED=1
    say "组件清理失败；临时目录保留在 $WORK，请勿直接重复删除包，先检查 packages.after" "component cleanup failed; temporary state kept at $WORK; inspect packages.after before manual removal"
    [ "$exit_status" -eq 0 ] && exit_status=1
  fi
  if [ "$KEEP" = "1" ] || [ "$CLEANUP_FAILED" -eq 1 ]; then
    say "临时目录保留在 $WORK" "temporary directory kept at $WORK"
  else
    rm -rf "$WORK"
  fi
  exit "$exit_status"
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

say "下载 $ASSET" "downloading $ASSET"
fetch "${BASE}/${ASSET}" "${WORK}/${ASSET}" || die "下载失败；仓库是否已发布 Release？" "download failed; has the repository published a Release?"
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
# stdin 是 curl 管道，交互输入必须走 /dev/tty；无终端时直接按默认配置运行。
INTERACTIVE=""
PLAN_FILE=""
if [ "$#" -eq 0 ] && [ -r /dev/tty ] && [ -w /dev/tty ]; then
  INTERACTIVE="--interactive"
fi
if [ "$#" -le 2 ] && [ -z "$INTERACTIVE" ] && [ -r /dev/tty ] && [ -w /dev/tty ] && [ "$#" -ge 1 ]; then
  case "$1" in
    --lang|--lang=*|-lang|-lang=*) INTERACTIVE="--interactive" ;;
  esac
fi

# 向导会改变档位和模块，先让 ecs 只写出临时计划，再据此准备最小组件集合。
if [ -n "$INTERACTIVE" ]; then
  PLAN_FILE="$WORK/modules.plan"
  if ECS_PLAN_FILE="$PLAN_FILE" "${WORK}/ecs" "$@" "$INTERACTIVE"; then
    PLAN_STATUS=0
  else
    PLAN_STATUS=$?
  fi
  [ "$PLAN_STATUS" -eq 0 ] || exit "$PLAN_STATUS"
  # 用户在向导中取消或没有选中模块时，计划文件不存在；按成功取消退出。
  [ -s "$PLAN_FILE" ] || exit 0
  PLAN_PROFILE=$(sed -n '1p' "$PLAN_FILE")
  PLAN_MODULES=$(sed -n '2p' "$PLAN_FILE")
  [ -n "$PLAN_PROFILE" ] && [ -n "$PLAN_MODULES" ] || die "向导没有生成有效测试计划" "the wizard did not produce a valid test plan"
  PROFILE="$PLAN_PROFILE"
  ONLY="$PLAN_MODULES"
fi

prepare_dependencies

# 不使用 exec：必须等 ecs 退出后运行清理逻辑，并原样保留退出码。
if [ -n "$PLAN_FILE" ]; then
  if ECS_PLAN_FILE= "${WORK}/ecs" "$@" --profile "$PLAN_PROFILE" --only "$PLAN_MODULES" --yes; then
    RUN_STATUS=0
  else
    RUN_STATUS=$?
  fi
else
  if "${WORK}/ecs" "$@"; then
    RUN_STATUS=0
  else
    RUN_STATUS=$?
  fi
fi
exit "$RUN_STATUS"
