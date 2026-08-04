#!/bin/sh
# ecs 一键运行脚本
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
#       └ 自动下载已校验的 ecs，按当前配置准备缺失组件，运行并在 ${TMPDIR:-/tmp} 生成本地报告
#   curl -fsSL .../run.sh | sh -s -- --profile full --lang en
#       └ 带参数时跳过向导；组件仍会自动准备，测试结束后只清理本次安装的组件
#   curl -fsSL .../run.sh | sh -s -- --submit --profile full --yes --provider vultr --region jp-tokyo
#       └ 一次完成测试，并在 ${TMPDIR:-/tmp} 生成可公开提交的 ecs.submission/v1 文件
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

# Help must be local and side-effect free.  In particular, asking the wrapper
# for help must not download a release or prepare system packages first.
case "${1:-}" in
  -h|--help)
    if [ "$UI" = "en" ]; then
      printf '%s\n' \
        'Usage: run.sh [--profile quick|standard|full] [--only MODULES] [options]' \
        '       run.sh --submit [run options] [--provider NAME] [--region REGION] [--output PATH]' \
        '' \
        'Downloads a checksummed ecs release, prepares missing distro packages, and writes reports directly to ${TMPDIR:-/tmp} by default.' \
        'No report directory is created by default; pass --output PATH to choose a destination.' \
        'With --submit, runs one test and writes a small ecs.submission/v1 JSON; --output chooses its file or directory.' \
        'Common options: --profile, --only, --skip, --config, --exposure, --lang, --yes.' \
        'Ookla is never installed automatically; use the ecs CLI with --accept ookla.'
    else
      printf '%s\n' \
        '用法：run.sh [--profile quick|standard|full] [--only 模块] [选项]' \
        '      run.sh --submit [测试选项] [--provider 商家] [--region 地区] [--output 路径]' \
        '' \
        '下载并校验 ecs Release，准备缺失的发行版组件，并默认直接在 ${TMPDIR:-/tmp} 生成报告。' \
        '默认不会创建新的报告目录；请用 --output PATH 指定输出位置。' \
        '使用 --submit 会一次完成测试并生成精简的 ecs.submission/v1 JSON；--output 指定文件或目录。' \
        '常用选项：--profile、--only、--skip、--config、--exposure、--lang、--yes。' \
        'Ookla 不会自动安装；请用 ecs --accept ookla 显式启用。'
    fi
    exit 0
    ;;
esac

# --submit is a wrapper-only mode.  Parse and remove its options before any
# arguments reach `ecs run`; keeping the filtering in positional parameters
# avoids eval/string-splitting and therefore preserves spaces and shell
# metacharacters in ordinary user arguments.
SUBMIT_MODE=0
SUBMIT_PROVIDER=""
SUBMIT_REGION=""
SUBMIT_OUTPUT=""
SUBMIT_PROVIDER_GIVEN=0
SUBMIT_REGION_GIVEN=0
SUBMIT_OUTPUT_GIVEN=0

reject_submit_value() {
  submit_label=$1
  submit_value=$2
  [ -n "$submit_value" ] ||
    die "--$submit_label 参数不能为空" "--$submit_label requires a non-empty value"
  case "$submit_value" in
    -*) die "--$submit_label 的值不能是另一个选项" "--$submit_label value must not be another option" ;;
  esac
  submit_clean=$(LC_ALL=C printf '%s' "$submit_value" | LC_ALL=C tr -d '\001-\037\177')
  [ "$submit_clean" = "$submit_value" ] ||
    die "--$submit_label 的值不能包含控制字符" "--$submit_label value contains a control character"
}

for arg in "$@"; do
  case "$arg" in
    --submit) SUBMIT_MODE=1 ;;
    --submit=*) die "--submit 不接受参数" "--submit does not take a value" ;;
  esac
done
if [ "$SUBMIT_MODE" -eq 1 ]; then
  for arg in "$@"; do
    case "$arg" in
      --reveal|--reveal=*)
        die "提交模式不允许 --reveal（中间报告不会公开）" "--reveal is not allowed in submit mode (the intermediate report is private)"
        ;;
    esac
  done
fi

if [ "$SUBMIT_MODE" -eq 1 ]; then
  SUBMIT_SENTINEL="__ecs_run_submit_filter_$$__"
  # Append a sentinel, then rotate ordinary arguments behind it.  When the
  # sentinel reaches the front, the remaining positional parameters are the
  # filtered argv in their original order.
  set -- "$@" "$SUBMIT_SENTINEL"
  while [ "$#" -gt 0 ] && [ "$1" != "$SUBMIT_SENTINEL" ]; do
    arg=$1
    shift
    case "$arg" in
      --submit)
        ;;
      --provider)
        [ "$#" -gt 0 ] && [ "$1" != "$SUBMIT_SENTINEL" ] ||
          die "--provider 缺少参数" "--provider requires a value"
        [ "$SUBMIT_PROVIDER_GIVEN" -eq 0 ] ||
          die "--provider 不能重复" "--provider must not be repeated"
        SUBMIT_PROVIDER=$1
        shift
        reject_submit_value provider "$SUBMIT_PROVIDER"
        SUBMIT_PROVIDER_GIVEN=1
        ;;
      --provider=*)
        [ "$SUBMIT_PROVIDER_GIVEN" -eq 0 ] ||
          die "--provider 不能重复" "--provider must not be repeated"
        SUBMIT_PROVIDER=${arg#--provider=}
        reject_submit_value provider "$SUBMIT_PROVIDER"
        SUBMIT_PROVIDER_GIVEN=1
        ;;
      --region)
        [ "$#" -gt 0 ] && [ "$1" != "$SUBMIT_SENTINEL" ] ||
          die "--region 缺少参数" "--region requires a value"
        [ "$SUBMIT_REGION_GIVEN" -eq 0 ] ||
          die "--region 不能重复" "--region must not be repeated"
        SUBMIT_REGION=$1
        shift
        reject_submit_value region "$SUBMIT_REGION"
        SUBMIT_REGION_GIVEN=1
        ;;
      --region=*)
        [ "$SUBMIT_REGION_GIVEN" -eq 0 ] ||
          die "--region 不能重复" "--region must not be repeated"
        SUBMIT_REGION=${arg#--region=}
        reject_submit_value region "$SUBMIT_REGION"
        SUBMIT_REGION_GIVEN=1
        ;;
      --output)
        [ "$#" -gt 0 ] && [ "$1" != "$SUBMIT_SENTINEL" ] ||
          die "--output 缺少路径" "--output requires a path"
        [ "$SUBMIT_OUTPUT_GIVEN" -eq 0 ] ||
          die "--output 不能重复" "--output must not be repeated"
        SUBMIT_OUTPUT=$1
        shift
        reject_submit_value output "$SUBMIT_OUTPUT"
        SUBMIT_OUTPUT_GIVEN=1
        ;;
      --output=*)
        [ "$SUBMIT_OUTPUT_GIVEN" -eq 0 ] ||
          die "--output 不能重复" "--output must not be repeated"
        SUBMIT_OUTPUT=${arg#--output=}
        reject_submit_value output "$SUBMIT_OUTPUT"
        SUBMIT_OUTPUT_GIVEN=1
        ;;
      *)
        set -- "$@" "$arg"
        ;;
    esac
  done
  [ "$#" -gt 0 ] && [ "$1" = "$SUBMIT_SENTINEL" ] ||
    die "提交参数解析失败" "failed to parse submit arguments"
  shift
fi

OUTPUT_GIVEN=0
EXPECT_OUTPUT=0
NAME_GIVEN=0
EXPECT_NAME=0
NAME_VALUE=""
for arg in "$@"; do
  if [ "$EXPECT_OUTPUT" -eq 1 ]; then
    EXPECT_OUTPUT=0
    continue
  fi
  if [ "$EXPECT_NAME" -eq 1 ]; then
    NAME_VALUE="$arg"
    EXPECT_NAME=0
    continue
  fi
  case "$arg" in
    --output) OUTPUT_GIVEN=1; EXPECT_OUTPUT=1 ;;
    --output=*) OUTPUT_GIVEN=1 ;;
    --name|-name) NAME_GIVEN=1; EXPECT_NAME=1 ;;
    --name=*|-name=*) NAME_GIVEN=1; NAME_VALUE="${arg#*=}" ;;
  esac
done
[ "$SUBMIT_MODE" -eq 0 ] || OUTPUT_GIVEN=1

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
REPORT_DIR=""
REPORT_NAME=""
REPORT_BEFORE=""
SUBMIT_REPORT_DIR=""
SUBMIT_REPORT=""
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

# 包管理器的正常输出很长，收进临时日志，避免把 curl|sh 的终端刷满。
# 失败时只显示末尾诊断；日志会随 ECS_KEEP=1 或清理失败一起保留。
package_command() {
  PACKAGE_LOG="$WORK/package-manager.log"
  if "$@" >"$PACKAGE_LOG" 2>&1; then
    return 0
  fi
  say "组件操作失败，日志末尾如下：$PACKAGE_LOG" "component operation failed; log tail: $PACKAGE_LOG"
  tail -n 80 "$PACKAGE_LOG" >&2 || true
  return 1
}

# 从命令行预读会影响依赖规划的选项。带 --config 时配置文件可能改写模块，
# 因而按完整配置准备，避免先删掉用户实际需要的工具。
PROFILE=standard
ONLY=""
SKIP=""
# 只需要区分"完全不联网"：public 及以上的依赖集完全相同（network 是纯 HTTP，
# 不需要外部程序；ookla 从不自动安装）。因此这里不复刻完整的分级表。
LOCAL_ONLY=0
CONFIG_GIVEN=0
EXPECT=""
for arg in "$@"; do
  if [ -n "$EXPECT" ]; then
    case "$EXPECT" in
      profile) PROFILE="$arg" ;;
      only) ONLY="$arg" ;;
      skip) SKIP="$arg" ;;
      config) CONFIG_GIVEN=1 ;;
      exposure) [ "$arg" = "local" ] && LOCAL_ONLY=1 ;;
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
    --exposure) EXPECT=exposure ;;
    --exposure=local) LOCAL_ONLY=1 ;;
  esac
done

module_enabled() {
  module=$1
  if [ "$CONFIG_GIVEN" -eq 1 ]; then
    base_modules="system,network,bgp,cpu,memory,disk,dns,latency,speed,ports,nat,blacklist,apps,cnspeed,ookla,media,route,backtrace"
  elif [ -n "$ONLY" ]; then
    base_modules="$ONLY"
  else
    case "$PROFILE" in
      quick) base_modules="system,network,cpu,memory,disk,dns,latency" ;;
      standard) base_modules="system,network,bgp,cpu,memory,disk,dns,latency,speed,ports,nat,blacklist,apps,media,route,backtrace" ;;
      full) base_modules="system,network,bgp,cpu,memory,disk,dns,latency,speed,ports,nat,blacklist,apps,cnspeed,media,route,backtrace" ;;
      *) base_modules="system,network,bgp,cpu,memory,disk,dns,latency,speed,ports,nat,blacklist,apps,cnspeed,media,route,backtrace" ;;
    esac
  fi
  list_contains "$base_modules" "$module" || return 1
  list_contains "$SKIP" "$module" && return 1
  if [ "$LOCAL_ONLY" -eq 1 ]; then
    case "$module" in
      network|bgp|dns|latency|speed|ports|nat|blacklist|apps|cnspeed|ookla|media|route|backtrace) return 1 ;;
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
  elif module_enabled system; then
    # The system inventory can include read-only SMART summaries even when
    # the disk benchmark itself is not selected.
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
      package_command as_root env DEBIAN_FRONTEND=noninteractive apt-get update || return 1
      package_command as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y $PACKAGES || return 1
      ;;
    dnf) package_command as_root dnf install -y $PACKAGES || return 1 ;;
    yum) package_command as_root yum install -y $PACKAGES || return 1 ;;
    apk) package_command as_root apk add --no-cache $PACKAGES || return 1 ;;
    pacman) package_command as_root pacman -Sy --noconfirm $PACKAGES || return 1 ;;
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
  if [ "$(id -u)" -ne 0 ]; then
    if ! command -v sudo >/dev/null 2>&1; then
      die "准备测试组件需要 root 或 sudo；可设置 ECS_AUTO_DEPS=0 运行降级测试" "preparing test components requires root or sudo; set ECS_AUTO_DEPS=0 to run with a degraded report"
    fi
    if ! sudo -n true 2>/dev/null; then
      if [ -r /dev/tty ] && [ -w /dev/tty ]; then
        say "准备组件需要 sudo 权限；请按提示授权。" "sudo permission is required to prepare components; follow the prompt"
        if ! sudo -v </dev/tty >/dev/tty 2>/dev/tty; then
          die "sudo 授权失败；可设置 ECS_AUTO_DEPS=0 运行降级测试" "sudo authorization failed; set ECS_AUTO_DEPS=0 to run with a degraded report"
        fi
      else
        die "当前不是 root 且没有可用的 sudo 会话；请先运行 sudo -v，或设置 ECS_AUTO_DEPS=0" "not root and no cached sudo session is available; run sudo -v first or set ECS_AUTO_DEPS=0"
      fi
    fi
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
    apt) package_command as_root env DEBIAN_FRONTEND=noninteractive apt-get purge -y $NEW_PACKAGES ;;
    dnf) package_command as_root dnf remove -y $NEW_PACKAGES ;;
    yum) package_command as_root yum remove -y $NEW_PACKAGES ;;
    apk) package_command as_root apk del $NEW_PACKAGES ;;
    pacman) package_command as_root pacman -R --noconfirm $NEW_PACKAGES ;;
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

snapshot_report_paths() {
  for report_glob in "$REPORT_DIR"/*.json "$REPORT_DIR"/*.md "$REPORT_DIR"/*.html "$REPORT_DIR"/*.txt; do
    [ -f "$report_glob" ] && printf '%s\n' "$report_glob"
  done | sort
}

sanitize_report_name() {
  # reporter.sanitizeBaseName 只保留 ASCII 字母、数字、点、短横线和下划线。
  REPORT_NAME=$(printf '%s' "$1" | sed 's/[^A-Za-z0-9_.-]/-/g; s/^[.-]*//; s/[.-]*$//')
  [ -n "$REPORT_NAME" ] || REPORT_NAME="ecs-report"
}

if [ "$OUTPUT_GIVEN" -eq 0 ]; then
  REPORT_DIR="${TMPDIR:-/tmp}"
  [ -d "$REPORT_DIR" ] ||
    die "临时目录不存在：$REPORT_DIR" "temporary directory does not exist: $REPORT_DIR"
  REPORT_BEFORE="$WORK/reports.before"
  snapshot_report_paths >"$REPORT_BEFORE" ||
    die "无法记录报告目录状态：$REPORT_DIR" "failed to record report directory state: $REPORT_DIR"
  if [ "$NAME_GIVEN" -eq 1 ]; then
    sanitize_report_name "$NAME_VALUE"
  else
    # WORK 本身由 mktemp 唯一命名；把它带入默认前缀可避免同一秒内的覆盖。
    sanitize_report_name "ecs-report-${WORK##*/}"
  fi
  say "报告目录：$REPORT_DIR" "report directory: $REPORT_DIR"
fi

if [ "$SUBMIT_MODE" -eq 1 ]; then
  # The complete report is an implementation detail of submit mode.  Keep it
  # beside the downloaded binary under WORK so the EXIT trap removes both.
  SUBMIT_REPORT_DIR="$WORK/report"
  mkdir -p "$SUBMIT_REPORT_DIR" ||
    die "无法创建提交临时目录：$SUBMIT_REPORT_DIR" "failed to create submit temporary directory: $SUBMIT_REPORT_DIR"
  if [ "$SUBMIT_OUTPUT_GIVEN" -eq 0 ]; then
    SUBMIT_OUTPUT="${TMPDIR:-/tmp}"
  fi
  [ -n "$SUBMIT_OUTPUT" ] ||
    die "--output 路径不能为空" "--output path must not be empty"

  # Fail before downloading/running benchmarks when the final destination is
  # plainly unusable.  Submit mode never creates a user directory and never
  # overwrites an existing file; the downloaded ecs binary repeats the same
  # checks at the final write to cover races.
  if [ -L "$SUBMIT_OUTPUT" ]; then
    die "提交输出不能是符号链接：$SUBMIT_OUTPUT" "submit output must not be a symlink: $SUBMIT_OUTPUT"
  elif [ -d "$SUBMIT_OUTPUT" ]; then
    [ -w "$SUBMIT_OUTPUT" ] ||
      die "提交输出目录不可写：$SUBMIT_OUTPUT" "submit output directory is not writable: $SUBMIT_OUTPUT"
  else
    case "$SUBMIT_OUTPUT" in
      */*) SUBMIT_OUTPUT_PARENT=${SUBMIT_OUTPUT%/*}; [ -n "$SUBMIT_OUTPUT_PARENT" ] || SUBMIT_OUTPUT_PARENT=/ ;;
      *) SUBMIT_OUTPUT_PARENT=. ;;
    esac
    [ -d "$SUBMIT_OUTPUT_PARENT" ] ||
      die "提交输出父目录不存在：$SUBMIT_OUTPUT_PARENT" "submit output parent does not exist: $SUBMIT_OUTPUT_PARENT"
    [ ! -L "$SUBMIT_OUTPUT_PARENT" ] ||
      die "提交输出父目录不能是符号链接：$SUBMIT_OUTPUT_PARENT" "submit output parent must not be a symlink: $SUBMIT_OUTPUT_PARENT"
    [ -w "$SUBMIT_OUTPUT_PARENT" ] ||
      die "提交输出父目录不可写：$SUBMIT_OUTPUT_PARENT" "submit output parent is not writable: $SUBMIT_OUTPUT_PARENT"
    [ ! -e "$SUBMIT_OUTPUT" ] ||
      die "提交输出文件已存在：$SUBMIT_OUTPUT" "submit output file already exists: $SUBMIT_OUTPUT"
  fi
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

if [ "$SUBMIT_MODE" -eq 1 ]; then
  # Check the downloaded binary before dependencies or benchmarks.  This is a
  # side-effect-free command and gives old releases a clear error up front.
  "${WORK}/ecs" submit --help >/dev/null 2>&1 ||
    die "下载的 ecs 不支持 submit 子命令，请使用最新 Release" "the downloaded ecs does not support submit; use a current Release"
fi

# 决定要不要进交互向导。
# stdin 是 curl 管道，交互输入必须走 /dev/tty；无终端时直接按默认配置运行。
INTERACTIVE=""
PLAN_FILE=""
if [ "$SUBMIT_MODE" -eq 0 ] && [ "$#" -eq 0 ] && [ -r /dev/tty ] && [ -w /dev/tty ]; then
  INTERACTIVE="--interactive"
fi
if [ "$SUBMIT_MODE" -eq 0 ] && [ "$#" -le 2 ] && [ -z "$INTERACTIVE" ] && [ -r /dev/tty ] && [ -w /dev/tty ] && [ "$#" -ge 1 ]; then
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

report_paths() {
  REPORT_AFTER="$WORK/reports.after"
  REPORT_MATCHES="$WORK/reports.matches"
  REPORT_PATHS="$WORK/reports.paths"
  snapshot_report_paths >"$REPORT_AFTER" || true
  # 新文件来自本次运行；不会把 REPORT_DIR 中原有的无关 JSON 当成结果。
  comm -13 "$REPORT_BEFORE" "$REPORT_AFTER" >"$REPORT_MATCHES" || true
  # 显式 --name 可能覆盖同名旧文件，补上其实际路径；默认名称已由 WORK 唯一化。
  for report_format in json md html txt; do
    report_path="$REPORT_DIR/$REPORT_NAME.$report_format"
    if [ -f "$report_path" ]; then
      printf '%s\n' "$report_path" >>"$REPORT_MATCHES"
    fi
  done
  sort -u "$REPORT_MATCHES" >"$REPORT_PATHS" || true

  if [ -s "$REPORT_PATHS" ]; then
    while IFS= read -r report_path; do
      [ -n "$report_path" ] || continue
      report_format=${report_path##*.}
      case "$report_format" in
        json) say "JSON 报告：$report_path" "JSON report: $report_path" ;;
        *) say "${report_format} 报告：$report_path" "${report_format} report: $report_path" ;;
      esac
    done <"$REPORT_PATHS"
  else
    say "未找到本次生成的报告；报告目录：$REPORT_DIR" "no report generated by this run; report directory: $REPORT_DIR"
  fi
}

# 不使用 exec：必须等 ecs 退出后运行清理逻辑，并原样保留退出码。
if [ "$SUBMIT_MODE" -eq 1 ]; then
  # Force a JSON report in WORK.  Wrapper-only submit/provider/region/output
  # options were removed above, while every ordinary run option remains
  # quoted in "$@".
  if ECS_PLAN_FILE= "${WORK}/ecs" "$@" --format json --output "$SUBMIT_REPORT_DIR"; then
    RUN_STATUS=0
  else
    RUN_STATUS=$?
  fi
elif [ -n "$PLAN_FILE" ]; then
  if [ -n "$REPORT_DIR" ]; then
    if [ "$NAME_GIVEN" -eq 1 ]; then
      if ECS_PLAN_FILE= "${WORK}/ecs" "$@" --profile "$PLAN_PROFILE" --only "$PLAN_MODULES" --yes --output "$REPORT_DIR"; then
        RUN_STATUS=0
      else
        RUN_STATUS=$?
      fi
    elif ECS_PLAN_FILE= "${WORK}/ecs" "$@" --profile "$PLAN_PROFILE" --only "$PLAN_MODULES" --yes --output "$REPORT_DIR" --name "$REPORT_NAME"; then
      RUN_STATUS=0
    else
      RUN_STATUS=$?
    fi
  else
    if ECS_PLAN_FILE= "${WORK}/ecs" "$@" --profile "$PLAN_PROFILE" --only "$PLAN_MODULES" --yes; then
      RUN_STATUS=0
    else
      RUN_STATUS=$?
    fi
  fi
else
  if [ -n "$REPORT_DIR" ]; then
    if [ "$NAME_GIVEN" -eq 1 ]; then
      if "${WORK}/ecs" "$@" --output "$REPORT_DIR"; then
        RUN_STATUS=0
      else
        RUN_STATUS=$?
      fi
    elif "${WORK}/ecs" "$@" --output "$REPORT_DIR" --name "$REPORT_NAME"; then
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
fi
if [ "$SUBMIT_MODE" -eq 1 ]; then
  if [ "$RUN_STATUS" -ne 0 ]; then
    exit "$RUN_STATUS"
  fi

  # The temporary output directory is unique to this invocation and is forced
  # to JSON-only, so exactly one regular JSON is expected.
  for submit_path in "$SUBMIT_REPORT_DIR"/*.json; do
    [ -f "$submit_path" ] || continue
    if [ -n "$SUBMIT_REPORT" ]; then
      die "提交临时目录里有多个 JSON 报告" "multiple JSON reports found in submit temporary directory"
    fi
    SUBMIT_REPORT=$submit_path
  done
  [ -n "$SUBMIT_REPORT" ] ||
    die "测试没有生成 JSON 报告，无法提交" "the test produced no JSON report to submit"

  # ecs submit treats an existing directory as a directory target and a
  # non-existent path as a file target.  This preserves the CLI's file-or-
  # directory contract while keeping the default in TMPDIR.
  if [ "$SUBMIT_PROVIDER_GIVEN:$SUBMIT_REGION_GIVEN" = "1:1" ]; then
    if ECS_PLAN_FILE= "${WORK}/ecs" submit --input "$SUBMIT_REPORT" --output "$SUBMIT_OUTPUT" \
        --provider "$SUBMIT_PROVIDER" --region "$SUBMIT_REGION"; then
      SUBMIT_STATUS=0
    else
      SUBMIT_STATUS=$?
    fi
  elif [ "$SUBMIT_PROVIDER_GIVEN" -eq 1 ]; then
    if ECS_PLAN_FILE= "${WORK}/ecs" submit --input "$SUBMIT_REPORT" --output "$SUBMIT_OUTPUT" \
        --provider "$SUBMIT_PROVIDER"; then
      SUBMIT_STATUS=0
    else
      SUBMIT_STATUS=$?
    fi
  elif [ "$SUBMIT_REGION_GIVEN" -eq 1 ]; then
    if ECS_PLAN_FILE= "${WORK}/ecs" submit --input "$SUBMIT_REPORT" --output "$SUBMIT_OUTPUT" \
        --region "$SUBMIT_REGION"; then
      SUBMIT_STATUS=0
    else
      SUBMIT_STATUS=$?
    fi
  elif ECS_PLAN_FILE= "${WORK}/ecs" submit --input "$SUBMIT_REPORT" --output "$SUBMIT_OUTPUT"; then
    SUBMIT_STATUS=0
  else
    SUBMIT_STATUS=$?
  fi
  exit "$SUBMIT_STATUS"
fi
if [ -n "$REPORT_DIR" ]; then
  report_paths
fi
exit "$RUN_STATUS"
