#!/bin/sh
# ecs 一键运行脚本
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
#       └ 自动下载已校验的 ecs，把缺失组件解包到临时前缀，运行并在 ${TMPDIR:-/tmp} 生成本地报告
#   curl -fsSL .../run.sh | sh -s -- --profile full --lang en
#       └ 带参数时跳过向导；组件仍会自动准备，测试结束后删除本次临时前缀
#   curl -fsSL .../run.sh | sh -s -- --submit --profile full --yes
#       └ 一次完成测试，并在 ${TMPDIR:-/tmp} 生成可公开提交的 ecs.submission/v1 文件
#
# 依赖策略：
#   - 已有的 sysbench/fio/iperf3/speedtest 等组件不会改动。
#   - 缺失组件只从发行版的已配置、签名软件源下载到本次 WORK，并解包到
#     WORK/root；绝不调用系统包安装器，也不改动系统数据库。当前 Debian/Ubuntu
#     路径使用 apt-get download + dpkg-deb -x；其它发行版无法安全解包时明确跳过。
#   - 路由模块仅例外地从 NextTrace 官方 GitHub Release 下载 full 二进制，并校验
#     API digest 后放入本次 WORK。
#   - ECS_AUTO_DEPS=0 可关闭自动依赖准备，让 ecs 自己报告缺失组件。
#   - ECS_KEEP=1 只保留临时工作目录用于排障；没有系统包需要清理。
#   - WORK 默认位于 /tmp；显式 TMPDIR 仅作为高级覆盖，必须是绝对路径。

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
        'Usage: run.sh [--profile standard|full] [--only MODULES] [options]' \
        '       run.sh --submit [run options] [--provider NAME] [--region REGION] [--output PATH]' \
        '' \
        'Downloads a checksummed ecs release, stages missing tools under a temporary prefix (never system-installs them), and writes reports directly to ${TMPDIR:-/tmp} by default.' \
        'When route/backtrace needs NextTrace, run.sh may download a verified temporary full Linux asset; ECS_AUTO_DEPS=0 skips it.' \
        'No report directory is created by default; pass --output PATH to choose a destination.' \
        'With --submit, runs one test and writes a small ecs.submission/v1 JSON; --output chooses its file or directory.' \
        'Provider and region are auto-detected from safe local report metadata when available; --provider/--region override them, otherwise they remain blank.' \
        'Common options: --profile, --only, --skip, --config, --exposure, --lang, --yes.' \
        'The full profile includes Ookla; if speedtest is missing, run.sh downloads and extracts it from a verified official package source under WORK.'
    else
      printf '%s\n' \
        '用法：run.sh [--profile standard|full] [--only 模块] [选项]' \
        '      run.sh --submit [测试选项] [--provider 商家] [--region 地区] [--output 路径]' \
        '' \
        '下载并校验 ecs Release，把缺失组件解包到临时前缀（不会安装到系统），并默认直接在 ${TMPDIR:-/tmp} 生成报告。' \
        '选中 route/backtrace 且缺少 NextTrace 时，run.sh 可下载并校验临时 full Linux 组件；ECS_AUTO_DEPS=0 会跳过。' \
        '默认不会创建新的报告目录；请用 --output PATH 指定输出位置。' \
        '使用 --submit 会一次完成测试并生成精简的 ecs.submission/v1 JSON；--output 指定文件或目录。' \
        '有安全的本机报告元数据时会自动识别云厂商和地区；--provider/--region 可显式覆盖，无法识别时留空。' \
        '常用选项：--profile、--only、--skip、--config、--exposure、--lang、--yes。' \
        'full 档包含 Ookla；缺少 speedtest 时，脚本会从临时、已验证的官方包源下载并解包到 WORK。'
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

# Keep the default work root unambiguously under /tmp.  TMPDIR remains an
# explicit advanced override for administrators who need a different
# filesystem (for example, a larger encrypted tmpfs); mktemp still creates a
# private 0700 directory below that root.
WORK_ROOT="/tmp"
if [ -n "${TMPDIR:-}" ]; then
  case "$TMPDIR" in
    /*) WORK_ROOT=$TMPDIR ;;
    *) die "TMPDIR 必须是绝对路径" "TMPDIR must be an absolute path" ;;
  esac
fi
[ -d "$WORK_ROOT" ] || die "临时工作根目录不存在：$WORK_ROOT" "temporary work root does not exist: $WORK_ROOT"
WORK=$(mktemp -d "$WORK_ROOT/ecs-run.XXXXXX")
REPORT_DIR=""
REPORT_NAME=""
REPORT_BEFORE=""
SUBMIT_REPORT_DIR=""
SUBMIT_REPORT=""
PACKAGE_MANAGER=""
TEMP_TOOL_ROOT="$WORK/root"
TEMP_TOOL_BIN="$WORK/bin"
TEMP_TOOL_CACHE="$WORK/packages"
ORIGINAL_PATH=${PATH:-}
TEMP_TOOL_PATH_READY=0
TEMP_PREPARED_TOOLS=""
APT_TEMP_LISTS="$WORK/apt/lists"
APT_TEMP_CACHE="$WORK/apt/cache"
APT_TEMP_SOURCES=""
MISSING_TOOLS=""
PACKAGES=""
NEXTTRACE_BIN_DIR="$WORK/bin"
NEXTTRACE_API_FILE="$WORK/nexttrace-release.json"
NEXTTRACE_DOWNLOAD_FILE="$WORK/nexttrace.download"
NEXTTRACE_READY=0
NEXTTRACE_REQUESTED=0
NEXTTRACE_FAILED=0
OOKLA_MISSING=0
OOKLA_REPO_READY=0
OOKLA_KEY_ASC=""
OOKLA_KEYRING=""
OOKLA_GNUPGHOME=""
OOKLA_APT_SOURCES=""
OOKLA_APT_LISTS=""
OOKLA_RPM_REPO=""
OOKLA_RPM_ARCH=""
OOKLA_RPM_OS=""
OOKLA_RPM_VERSION=""
OOKLA_DEB_SEEN=""
OOKLA_KEY_FINGERPRINT="C525F88FCF3A7E56CE2CF59131EB3981E723ACAA"

# Install the cleanup trap as soon as WORK exists.  This also covers argument
# validation and manifest failures that happen before the test body starts.
cleanup() {
  exit_status=$?
  trap - EXIT INT TERM HUP

  if [ "$KEEP" = "1" ]; then
    say "临时目录保留在 $WORK" "temporary directory kept at $WORK"
  else
    if ! rm -rf "$WORK"; then
      say "临时目录清理失败，现场保留在 $WORK" "failed to remove the temporary directory; preserving it at $WORK"
      [ "$exit_status" -eq 0 ] && exit_status=1
    fi
  fi
  exit "$exit_status"
}

trap cleanup EXIT
trap 'exit 130' INT TERM HUP

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

# Package-manager output is kept in the private work directory.  This helper
# is used only for download/update/extraction commands; no package-manager
# command in this script is allowed to mutate the host package database.
package_command() {
  PACKAGE_LOG="$WORK/package-manager.log"
  if "$@" >"$PACKAGE_LOG" 2>&1; then
    return 0
  fi
  say "组件操作失败，日志末尾如下：$PACKAGE_LOG" "component operation failed; log tail: $PACKAGE_LOG"
  tail -n 80 "$PACKAGE_LOG" >&2 || true
  return 1
}

prepare_temp_tool_dirs() {
  mkdir -p "$TEMP_TOOL_ROOT" "$TEMP_TOOL_BIN" "$TEMP_TOOL_CACHE" \
    "$APT_TEMP_LISTS/partial" "$APT_TEMP_CACHE/archives/partial" || return 1
  EXTRACTED_DEBS="$WORK/extracted.debs"
  : >"$EXTRACTED_DEBS"
  TEMP_PACKAGE_DIGESTS="$WORK/packages.sha256"
  : >"$TEMP_PACKAGE_DIGESTS"
}

# Point apt at private list/cache directories.  The host's sources and trust
# database are read-only inputs; apt-get update/download never invokes dpkg,
# writes /var/lib/dpkg, or acquires the host package lock.
apt_temp_command() {
  apt-get \
    -o "Dir::Etc::sourcelist=/etc/apt/sources.list" \
    -o "Dir::Etc::sourceparts=/etc/apt/sources.list.d" \
    -o "Dir::State::lists=$APT_TEMP_LISTS" \
    -o "Dir::Cache::archives=$APT_TEMP_CACHE/archives" \
    -o "Dir::Cache::pkgcache=$APT_TEMP_CACHE/pkgcache.bin" \
    -o "Dir::Cache::srcpkgcache=$APT_TEMP_CACHE/srcpkgcache.bin" \
    "$@"
}

apt_cache_command() {
  apt-cache \
    -o "Dir::Etc::sourcelist=/etc/apt/sources.list" \
    -o "Dir::Etc::sourceparts=/etc/apt/sources.list.d" \
    -o "Dir::State::lists=$APT_TEMP_LISTS" \
    -o "Dir::Cache::pkgcache=$APT_TEMP_CACHE/pkgcache.bin" \
    -o "Dir::Cache::srcpkgcache=$APT_TEMP_CACHE/srcpkgcache.bin" \
    "$@"
}

apt_update_temp() {
  [ "${APT_TEMP_UPDATED:-0}" -eq 1 ] && return 0
  prepare_temp_tool_dirs || return 1
  if ! package_command apt_temp_command update; then
    return 1
  fi
  APT_TEMP_UPDATED=1
  return 0
}

apt_download_one() {
  apt_package=$1
  [ -n "$apt_package" ] || return 1
  case " ${APT_DOWNLOADED_PACKAGES:-} " in
    *" $apt_package "*) return 0 ;;
  esac
  APT_DOWNLOADED_PACKAGES="${APT_DOWNLOADED_PACKAGES:+$APT_DOWNLOADED_PACKAGES }$apt_package"
  APT_LOG="$WORK/package-manager.log"
  if ! (cd "$TEMP_TOOL_CACHE" && apt_temp_command download "$apt_package") \
      >>"$APT_LOG" 2>&1; then
    say "无法从已配置的 apt 源下载 $apt_package，相关测试将跳过（日志：$APT_LOG）" \
      "could not download $apt_package from the configured apt sources; related tests will be skipped (log: $APT_LOG)"
    return 1
  fi
  return 0
}

extract_debs() {
  for deb in "$TEMP_TOOL_CACHE"/*.deb; do
    [ -f "$deb" ] || continue
    if grep -F -x "$deb" "$EXTRACTED_DEBS" >/dev/null 2>&1; then
      continue
    fi
    if ! dpkg-deb --extract "$deb" "$TEMP_TOOL_ROOT" >>"$WORK/package-manager.log" 2>&1; then
      say "无法安全解包 $deb，相关测试将跳过" "could not safely extract $deb; related tests will be skipped"
      return 1
    fi
    if command -v sha256sum >/dev/null 2>&1; then
      deb_digest=$(sha256sum "$deb" | awk '{print $1}')
      printf '%s  %s\n' "$deb_digest" "${deb##*/}" >>"$TEMP_PACKAGE_DIGESTS"
    fi
    printf '%s\n' "$deb" >>"$EXTRACTED_DEBS"
  done
}

temp_candidate_for_tool() {
  temp_tool=$1
  for temp_candidate in \
    "$TEMP_TOOL_ROOT/usr/local/bin/$temp_tool" \
    "$TEMP_TOOL_ROOT/usr/bin/$temp_tool" \
    "$TEMP_TOOL_ROOT/bin/$temp_tool" \
    "$TEMP_TOOL_ROOT/usr/local/sbin/$temp_tool" \
    "$TEMP_TOOL_ROOT/usr/sbin/$temp_tool" \
    "$TEMP_TOOL_ROOT/sbin/$temp_tool"; do
    [ -x "$temp_candidate" ] || continue
    temp_resolved=$(readlink -f "$temp_candidate" 2>/dev/null || true)
    case "$temp_resolved" in
      "$TEMP_TOOL_ROOT"/*) printf '%s\n' "$temp_candidate"; return 0 ;;
    esac
  done
  return 1
}

stage_temp_tool() {
  stage_tool=$1
  stage_candidate=$(temp_candidate_for_tool "$stage_tool" || true)
  [ -n "$stage_candidate" ] || return 1
  rm -f "$TEMP_TOOL_BIN/$stage_tool"
  ln -s "$stage_candidate" "$TEMP_TOOL_BIN/$stage_tool" || return 1
  case " $TEMP_PREPARED_TOOLS " in
    *" $stage_tool "*) ;;
    *) TEMP_PREPARED_TOOLS="${TEMP_PREPARED_TOOLS:+$TEMP_PREPARED_TOOLS }$stage_tool" ;;
  esac
  return 0
}

activate_temp_tool_path() {
  [ "$TEMP_TOOL_PATH_READY" -eq 1 ] && return 0
  # Only the explicitly staged tool links are prepended.  Keeping the rest of
  # the host PATH intact guarantees that a pre-installed helper is never
  # shadowed by an incidental executable shipped in a dependency package.
  TEMP_PATH="$TEMP_TOOL_BIN"
  if [ -n "$ORIGINAL_PATH" ]; then
    PATH="$TEMP_PATH:$ORIGINAL_PATH"
  else
    PATH=$TEMP_PATH
  fi
  export PATH
  TEMP_LIB_PATH=""
  for temp_lib_part in \
    "$TEMP_TOOL_ROOT/lib" "$TEMP_TOOL_ROOT/lib64" \
    "$TEMP_TOOL_ROOT/usr/lib" "$TEMP_TOOL_ROOT/usr/lib64"; do
    [ -d "$temp_lib_part" ] || continue
    TEMP_LIB_PATH="${TEMP_LIB_PATH:+$TEMP_LIB_PATH:}$temp_lib_part"
  done
  # Debian/Ubuntu place most ELF objects below a multi-arch directory (for
  # example usr/lib/aarch64-linux-gnu).  Add only the conventional ABI
  # directory names, all of which are confined below WORK/root.
  for temp_lib_parent in "$TEMP_TOOL_ROOT/lib" "$TEMP_TOOL_ROOT/usr/lib"; do
    for temp_lib_part in "$temp_lib_parent"/*; do
      [ -d "$temp_lib_part" ] || continue
      case "${temp_lib_part##*/}" in
        *-linux-gnu*|*-linux-musl*|*-gnu*|*-musl*)
          TEMP_LIB_PATH="${TEMP_LIB_PATH:+$TEMP_LIB_PATH:}$temp_lib_part"
          ;;
      esac
    done
  done
  if [ -n "$TEMP_LIB_PATH" ]; then
    if [ -n "${LD_LIBRARY_PATH:-}" ]; then
      LD_LIBRARY_PATH="$TEMP_LIB_PATH:$LD_LIBRARY_PATH"
    else
      LD_LIBRARY_PATH=$TEMP_LIB_PATH
    fi
    export LD_LIBRARY_PATH
  fi
  # A staged Ookla/dependency package may be the only trust bundle on a
  # minimal image.  Prefer it without touching the host's /etc/ssl tree.
  if [ -f "$TEMP_TOOL_ROOT/etc/ssl/certs/ca-certificates.crt" ]; then
    SSL_CERT_FILE="$TEMP_TOOL_ROOT/etc/ssl/certs/ca-certificates.crt"
    export SSL_CERT_FILE
  fi
  if [ -d "$TEMP_TOOL_ROOT/etc/ssl/certs" ]; then
    SSL_CERT_DIR="$TEMP_TOOL_ROOT/etc/ssl/certs"
    export SSL_CERT_DIR
  fi
  TEMP_TOOL_PATH_READY=1
}

apt_dependency_names() {
  apt_cache_command depends --no-recommends "$1" 2>/dev/null |
    awk '$1 == "Depends:" || $1 == "PreDepends:" || $1 == "|Depends:" || $1 == "|PreDepends:" {dep=$2; if (dep ~ /^</) next; sub(/\(.*/, "", dep); if (dep != "") print dep}'
}

prepare_apt_tools() {
  command -v dpkg-deb >/dev/null 2>&1 || {
    say "系统缺少 dpkg-deb，无法安全临时解包 Debian 包；相关测试将跳过" \
      "dpkg-deb is unavailable, so Debian packages cannot be safely extracted temporarily; related tests will be skipped"
    return 1
  }
  apt_update_temp || return 1
  APT_DOWNLOADED_PACKAGES=""
  APT_PENDING="$WORK/apt.pending"
  APT_SEEN="$WORK/apt.seen"
  : >"$APT_PENDING"
  : >"$APT_SEEN"
  for apt_package in $PACKAGES; do
    printf '%s\n' "$apt_package" >>"$APT_PENDING"
  done

  # Resolve the small dependency closure using apt's signed package metadata,
  # then download and unpack every .deb under WORK.  This intentionally uses
  # apt-get's `download` subcommand rather than `install`: dpkg is never
  # invoked and no privilege authorization is requested.
  while IFS= read -r apt_package; do
    [ -n "$apt_package" ] || continue
    if grep -F -x "$apt_package" "$APT_SEEN" >/dev/null 2>&1; then
      continue
    fi
    printf '%s\n' "$apt_package" >>"$APT_SEEN"
    if ! apt_download_one "$apt_package"; then
      continue
    fi
    apt_dependency_names "$apt_package" >>"$APT_PENDING" || true
  done <"$APT_PENDING"

  extract_debs || return 1
  prepared_any=0
  for missing_tool in $MISSING_TOOLS; do
    if stage_temp_tool "$missing_tool"; then
      prepared_any=1
      remove_missing_tool "$missing_tool"
    else
      say "无法在临时前缀中找到 $missing_tool，相关测试将跳过" \
        "could not find $missing_tool in the temporary prefix; related tests will be skipped"
    fi
  done
  [ "$prepared_any" -eq 1 ] && activate_temp_tool_path
  return 0
}

# Ookla publishes the CLI through a signed Packagecloud repository.  The
# repository setup is deliberately kept under WORK: no source list, keyring,
# or repo file is installed in /etc, and apt is pointed at the temporary
# metadata/cache directories below.  Never execute the vendor's install
# script (the documented curl|sh command); fetch the key as data and verify
# its pinned fingerprint before asking apt to verify metadata.
verify_ookla_key() {
  [ -s "$OOKLA_KEY_ASC" ] || return 1
  command -v gpg >/dev/null 2>&1 || return 1
  OOKLA_KEY_FPRS=$(GNUPGHOME="$OOKLA_GNUPGHOME" gpg --batch --no-options --show-keys --with-colons "$OOKLA_KEY_ASC" 2>/dev/null |
    awk -F: '$1 == "fpr" {print toupper($10)}') || return 1
  printf '%s\n' "$OOKLA_KEY_FPRS" |
    awk -v expected="$OOKLA_KEY_FINGERPRINT" '$0 == expected {found=1} END {exit !found}'
}

prepare_ookla_key() {
  OOKLA_KEY_ASC="$WORK/ookla-packagecloud-key.asc"
  OOKLA_KEYRING="$WORK/ookla-packagecloud-keyring.gpg"
  OOKLA_GNUPGHOME="$WORK/gnupg"
  mkdir -m 700 -p "$OOKLA_GNUPGHOME" || return 1
  fetch "https://packagecloud.io/ookla/speedtest-cli/gpgkey" "$OOKLA_KEY_ASC" || return 1
  verify_ookla_key || return 1
  if ! GNUPGHOME="$OOKLA_GNUPGHOME" gpg --batch --no-options --dearmor --yes -o "$OOKLA_KEYRING" "$OOKLA_KEY_ASC"; then
    return 1
  fi
  [ -s "$OOKLA_KEYRING" ]
}

apt_ookla_command() {
  apt-get \
    -o "Dir::Etc::sourcelist=$OOKLA_APT_SOURCES" \
    -o "Dir::Etc::sourceparts=-" \
    -o "Dir::State::lists=$OOKLA_APT_LISTS" \
    -o "Dir::Cache::archives=$WORK/ookla-apt-cache" \
    -o "Dir::Cache::pkgcache=$WORK/ookla-apt-cache/pkgcache.bin" \
    -o "Dir::Cache::srcpkgcache=$WORK/ookla-apt-cache/srcpkgcache.bin" "$@"
}

select_ookla_deb_distribution() {
  OOKLA_DEB_OS=""
  OOKLA_DEB_CODENAME=""
  [ -r /etc/os-release ] || return 1
  # shellcheck disable=SC1091
  . /etc/os-release
  case "${ID:-}" in
    debian) OOKLA_DEB_OS=debian ;;
    ubuntu) OOKLA_DEB_OS=ubuntu ;;
    *) return 1 ;;
  esac
  OOKLA_DEB_CODENAME="${VERSION_CODENAME:-}"
  if [ -z "$OOKLA_DEB_CODENAME" ] && command -v lsb_release >/dev/null 2>&1; then
    OOKLA_DEB_CODENAME=$(lsb_release -sc 2>/dev/null || true)
  fi
  [ -n "$OOKLA_DEB_CODENAME" ] || return 1
}

prepare_ookla_apt() {
  select_ookla_deb_distribution || return 1
  mkdir -p "$WORK/ookla-apt/lists/partial" "$WORK/ookla-apt-cache/archives/partial" || return 1
  OOKLA_APT_LISTS="$WORK/ookla-apt/lists"
  OOKLA_APT_SOURCES="$WORK/ookla-packagecloud.list"
  prepare_ookla_key || return 1

  # Packagecloud's repository occasionally lags a newly released distro.  A
  # signed, older Debian/Ubuntu suite is safe for this package (it only
  # depends on ca-certificates); probe candidates and use the first official
  # suite that exists instead of silently falling back to an arbitrary mirror.
  OOKLA_DEB_CANDIDATES="$OOKLA_DEB_CODENAME"
  if [ "$OOKLA_DEB_OS" = ubuntu ]; then
    OOKLA_DEB_CANDIDATES="$OOKLA_DEB_CANDIDATES jammy focal bionic"
  else
    OOKLA_DEB_CANDIDATES="$OOKLA_DEB_CANDIDATES bookworm bullseye buster"
  fi
  OOKLA_DEB_SUITE=""
  for candidate in $OOKLA_DEB_CANDIDATES; do
    case " $OOKLA_DEB_SEEN " in *" $candidate "*) continue ;; esac
    OOKLA_DEB_SEEN="${OOKLA_DEB_SEEN:+$OOKLA_DEB_SEEN }$candidate"
    if fetch "https://packagecloud.io/ookla/speedtest-cli/$OOKLA_DEB_OS/dists/$candidate/Release" \
        "$WORK/ookla-release-$candidate"; then
      OOKLA_DEB_SUITE="$candidate"
      break
    fi
  done
  [ -n "$OOKLA_DEB_SUITE" ] || return 1
  printf '%s\n' "deb [signed-by=$OOKLA_KEYRING] https://packagecloud.io/ookla/speedtest-cli/$OOKLA_DEB_OS $OOKLA_DEB_SUITE main" >"$OOKLA_APT_SOURCES"

  OOKLA_LOG="$WORK/package-manager.log"
  if ! apt_ookla_command update >>"$OOKLA_LOG" 2>&1; then
    say "Ookla 签名源元数据下载失败，speedtest 将跳过（日志：$OOKLA_LOG）" \
      "Ookla signed-source metadata download failed; speedtest will be skipped (log: $OOKLA_LOG)"
    return 1
  fi
  # Download the vendor package only.  `apt-get download` verifies the
  # Packagecloud Release metadata with the pinned key but never invokes dpkg.
  if ! (cd "$TEMP_TOOL_CACHE" && apt_ookla_command download speedtest) >>"$OOKLA_LOG" 2>&1; then
    say "Ookla speedtest 包下载失败，相关测试将跳过（日志：$OOKLA_LOG）" \
      "Ookla speedtest package download failed; related tests will be skipped (log: $OOKLA_LOG)"
    return 1
  fi
  extract_debs || return 1
  stage_temp_tool speedtest || return 1
  activate_temp_tool_path
  OOKLA_REPO_READY=1
}

select_ookla_rpm_distribution() {
  OOKLA_RPM_OS=""
  OOKLA_RPM_VERSION=""
  [ -r /etc/os-release ] || return 1
  # shellcheck disable=SC1091
  . /etc/os-release
  case "${ID:-}" in
    fedora) OOKLA_RPM_OS=fedora ;;
    amzn|centos|rhel|rocky|almalinux|ol) OOKLA_RPM_OS=el ;;
    *) return 1 ;;
  esac
  OOKLA_RPM_VERSION="${VERSION_ID%%.*}"
  [ -n "$OOKLA_RPM_VERSION" ] || return 1
  # The upstream repository currently has EL 6–9 and Fedora 32–36.  Keep
  # newer compatible systems on the newest signed suite rather than guessing
  # a third-party package name.
  if [ "$OOKLA_RPM_OS" = el ] && [ "$OOKLA_RPM_VERSION" -gt 9 ] 2>/dev/null; then
    OOKLA_RPM_VERSION=9
  elif [ "$OOKLA_RPM_OS" = fedora ] && [ "$OOKLA_RPM_VERSION" -gt 36 ] 2>/dev/null; then
    OOKLA_RPM_VERSION=36
  fi
  case "$(uname -m)" in
    x86_64|amd64) OOKLA_RPM_ARCH=x86_64 ;;
    aarch64|arm64) OOKLA_RPM_ARCH=aarch64 ;;
    armv7l|armv7) OOKLA_RPM_ARCH=armhfp ;;
    i386|i686|x86) OOKLA_RPM_ARCH=i386 ;;
    *) return 1 ;;
  esac
}

prepare_ookla_rpm() {
  select_ookla_rpm_distribution || return 1
  prepare_ookla_key || return 1
  OOKLA_RPM_REPO="$WORK/ookla-packagecloud.repo"
  printf '%s\n' \
    '[ookla-speedtest-cli]' \
    'name=Ookla Speedtest CLI (official)' \
    "baseurl=https://packagecloud.io/ookla/speedtest-cli/$OOKLA_RPM_OS/$OOKLA_RPM_VERSION/\$basearch" \
    'enabled=1' \
    'repo_gpgcheck=1' \
    'gpgcheck=1' \
    "gpgkey=file://$OOKLA_KEY_ASC" \
    'sslverify=1' \
    'metadata_expire=300' >"$OOKLA_RPM_REPO"
  # RPM metadata can be verified without installation, but extracting an RPM
  # safely requires rpm2cpio/cpio (and dnf's download plugin).  Do not guess or
  # install those helpers globally: report a controlled degradation instead.
  say "当前 RPM 路径没有安全的临时解包器，speedtest 将跳过；请预先放入 PATH" \
    "the RPM path lacks a safe temporary extractor; speedtest will be skipped (preinstall it on PATH if needed)"
  return 1
}

install_ookla() {
  say "通过 Ookla 官方签名包源临时准备 speedtest" "temporarily preparing speedtest from Ookla's signed official package source"
  case "$PACKAGE_MANAGER" in
    apt) prepare_ookla_apt ;;
    dnf|yum) prepare_ookla_rpm ;;
    *) return 1 ;;
  esac
}

# 从命令行预读会影响依赖规划的选项。带 --config 时配置文件可能改写模块，
# 因而按完整配置准备，避免先删掉用户实际需要的工具。
PROFILE=standard
ONLY=""
SKIP=""
# 只需要区分"完全不联网"：public 及以上的依赖集完全相同（network 是纯 HTTP，
# 不需要外部程序）。full 或显式 --only ookla 时，speedtest 进入依赖规划。
LOCAL_ONLY=0
CONFIG_GIVEN=0
EXPECT=""
# The downloaded binary is the source of truth for module/profile membership
# and tool metadata.  The manifest is required after download so a stale
# wrapper can never silently prepare the wrong dependency set.
MODULE_MANIFEST=0
MODULE_STANDARD_MODULES=""
MODULE_FULL_MODULES=""
MODULE_ALL_MODULES=""
MODULE_EXPOSURES=""
MODULE_TOOLS=""
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

if [ "$CONFIG_GIVEN" -eq 0 ]; then
  case "$PROFILE" in
    standard|full) ;;
    *) die "未知配置档：$PROFILE，可选 standard、full" "unknown profile: $PROFILE; choose standard or full" ;;
  esac
fi

# Read the locale-independent descriptor manifest emitted by the downloaded
# ecs binary.  Any missing header, malformed field, or incomplete profile is a
# hard failure: continuing with a copied module list could install the wrong
# packages or skip a newly added dependency.
load_module_manifest() {
  MODULE_MANIFEST=0
  MODULE_STANDARD_MODULES=""
  MODULE_FULL_MODULES=""
  MODULE_ALL_MODULES=""
  MODULE_EXPOSURES=""
  MODULE_TOOLS=""
  MANIFEST_FILE="$WORK/modules.manifest"
  if ! "${WORK}/ecs" list --machine >"$MANIFEST_FILE" 2>/dev/null; then
    return 1
  fi
  MANIFEST_HEADER=$(sed -n '1p' "$MANIFEST_FILE" 2>/dev/null || true)
  [ "$MANIFEST_HEADER" = "ecs-module-manifest$(printf '\t')1" ] || return 1

  manifest_standard=""
  manifest_full=""
  manifest_all=""
  manifest_exposures=""
  manifest_tools=""
  manifest_valid=1
  manifest_modules=0
  while IFS="$(printf '\t')" read -r manifest_kind manifest_id manifest_field manifest_extra; do
    case "$manifest_kind" in
      profile)
        case "$manifest_id" in
          standard) manifest_standard="$manifest_field" ;;
          full) manifest_full="$manifest_field" ;;
          *) manifest_valid=0 ;;
        esac
        ;;
      module)
        case "$manifest_id" in
          ""|*[!A-Za-z0-9_-]*) manifest_valid=0; continue ;;
        esac
        if list_contains "$manifest_all" "$manifest_id"; then
          manifest_valid=0
          continue
        fi
        case "$manifest_field" in
          local|public|thirdparty|any) ;;
          *) manifest_valid=0; continue ;;
        esac
        manifest_modules=$((manifest_modules + 1))
        manifest_all="${manifest_all}${manifest_all:+,}${manifest_id}"
        manifest_exposures="${manifest_exposures}${manifest_exposures:+
}${manifest_id}$(printf '\t')${manifest_field}"
        manifest_tools="${manifest_tools}${manifest_tools:+
}${manifest_id}$(printf '\t')${manifest_extra}"
        ;;
      "ecs-module-manifest"|"")
        # Header is checked above; an empty line is harmless.
        ;;
      *) manifest_valid=0 ;;
    esac
  done <"$MANIFEST_FILE"

  [ "$manifest_valid" -eq 1 ] || return 1
  [ "$manifest_modules" -gt 0 ] || return 1
  [ -n "$manifest_standard" ] && [ -n "$manifest_full" ] || return 1
  for profile_modules in "$manifest_standard" "$manifest_full"; do
    profile_words=$(printf '%s' "$profile_modules" | tr ',' ' ')
    for profile_module in $profile_words; do
      list_contains "$manifest_all" "$profile_module" || return 1
    done
  done
  MODULE_STANDARD_MODULES="$manifest_standard"
  MODULE_FULL_MODULES="$manifest_full"
  MODULE_ALL_MODULES="$manifest_all"
  MODULE_EXPOSURES="$manifest_exposures"
  MODULE_TOOLS="$manifest_tools"
  MODULE_MANIFEST=1
}

manifest_exposure() {
  [ "$MODULE_MANIFEST" -eq 1 ] || return 1
  printf '%s\n' "$MODULE_EXPOSURES" | awk -F '\t' -v wanted="$1" '$1 == wanted {print $2; exit}'
}

manifest_tools_for() {
  [ "$MODULE_MANIFEST" -eq 1 ] || return 1
  printf '%s\n' "$MODULE_TOOLS" | awk -F '\t' -v wanted="$1" '$1 == wanted {print $2; exit}'
}

# NextTrace is the only route engine.  When the selected module set needs it,
# the wrapper may place the verified full release binary in WORK/bin.  Parsing
# the release metadata locally avoids jq/python dependencies while still
# requiring both the exact official asset URL and GitHub's SHA-256 digest.
nexttrace_asset_metadata() {
  wanted=$1
  awk -v wanted="$wanted" '
    function field(line, key, p, rest) {
      p = index(line, "\"" key "\"")
      if (!p) return ""
      rest = substr(line, p + length(key) + 2)
      sub(/^[[:space:]]*:[[:space:]]*/, "", rest)
      # The GitHub API declares name, digest, and browser_download_url as JSON
      # strings.  Reject an unquoted value rather than coercing arbitrary JSON
      # into a shell URL or digest.
      if (substr(rest, 1, 1) != "\"") return ""
      rest = substr(rest, 2)
      end = index(rest, "\"")
      if (!end) return ""
      return substr(rest, 1, end - 1)
    }
    {
      name = field($0, "name")
      if (name != "") active = (name == wanted)
      if (active) {
        value = field($0, "browser_download_url")
        if (value != "") url = value
        value = field($0, "digest")
        if (value != "") digest = value
      }
    }
    END {
      if (url != "" && digest != "") print url "\t" digest
    }
  ' "$NEXTTRACE_API_FILE"
}

nexttrace_cleanup_partial() {
  rm -f "$NEXTTRACE_API_FILE" "$NEXTTRACE_DOWNLOAD_FILE"
}

prepare_nexttrace() {
  # Keep this allow-list aligned with the Linux assets published by
  # nxtrace/NTrace-core.  The ECS release ARCH mapping above is intentionally
  # not reused blindly if a future wrapper adds another platform.
  case "$ARCH" in
    amd64|arm64|armv7|386|s390x|riscv64|ppc64le) ;;
    *) return 1 ;;
  esac
  NEXTTRACE_ASSET="nexttrace_linux_${ARCH}"
  NEXTTRACE_API_URL="https://api.github.com/repos/nxtrace/NTrace-core/releases/latest"
  NEXTTRACE_URL=""
  NEXTTRACE_DIGEST=""
  NEXTTRACE_EXPECTED=""
  nexttrace_cleanup_partial
  if ! fetch "$NEXTTRACE_API_URL" "$NEXTTRACE_API_FILE"; then
    nexttrace_cleanup_partial
    return 1
  fi
  NEXTTRACE_METADATA=$(nexttrace_asset_metadata "$NEXTTRACE_ASSET" 2>/dev/null || true)
  NEXTTRACE_TAB=$(printf '\t')
  case "$NEXTTRACE_METADATA" in
    *"$NEXTTRACE_TAB"*) ;;
    *) nexttrace_cleanup_partial; return 1 ;;
  esac
  NEXTTRACE_URL=${NEXTTRACE_METADATA%%"$NEXTTRACE_TAB"*}
  NEXTTRACE_DIGEST=${NEXTTRACE_METADATA#*"$NEXTTRACE_TAB"}
  # Never follow a URL supplied by a release field unless it is the expected
  # asset on the pinned official repository.  The asset name is also exact,
  # so a tiny build or a different platform cannot be substituted silently.
  case "$NEXTTRACE_URL" in
    https://github.com/nxtrace/NTrace-core/releases/download/*/"$NEXTTRACE_ASSET") ;;
    *) nexttrace_cleanup_partial; return 1 ;;
  esac
  case "$NEXTTRACE_DIGEST" in
    sha256:*) NEXTTRACE_EXPECTED=${NEXTTRACE_DIGEST#sha256:} ;;
    *) nexttrace_cleanup_partial; return 1 ;;
  esac
  case "$NEXTTRACE_EXPECTED" in
    ''|*[!A-Fa-f0-9]*) nexttrace_cleanup_partial; return 1 ;;
  esac
  [ "${#NEXTTRACE_EXPECTED}" -eq 64 ] || {
    nexttrace_cleanup_partial
    return 1
  }
  if ! fetch "$NEXTTRACE_URL" "$NEXTTRACE_DOWNLOAD_FILE"; then
    nexttrace_cleanup_partial
    return 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    NEXTTRACE_ACTUAL=$(sha256sum "$NEXTTRACE_DOWNLOAD_FILE" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    NEXTTRACE_ACTUAL=$(shasum -a 256 "$NEXTTRACE_DOWNLOAD_FILE" | awk '{print $1}')
  else
    nexttrace_cleanup_partial
    return 1
  fi
  [ "$NEXTTRACE_ACTUAL" = "$NEXTTRACE_EXPECTED" ] || {
    nexttrace_cleanup_partial
    return 1
  }
  mkdir -p "$NEXTTRACE_BIN_DIR" || {
    nexttrace_cleanup_partial
    return 1
  }
  if ! mv "$NEXTTRACE_DOWNLOAD_FILE" "$NEXTTRACE_BIN_DIR/nexttrace"; then
    nexttrace_cleanup_partial
    rm -f "$NEXTTRACE_BIN_DIR/nexttrace"
    return 1
  fi
  if ! chmod 700 "$NEXTTRACE_BIN_DIR/nexttrace" ||
    [ ! -f "$NEXTTRACE_BIN_DIR/nexttrace" ] ||
    [ -L "$NEXTTRACE_BIN_DIR/nexttrace" ]; then
    nexttrace_cleanup_partial
    rm -f "$NEXTTRACE_BIN_DIR/nexttrace"
    return 1
  fi
  rm -f "$NEXTTRACE_API_FILE"
  PATH="$NEXTTRACE_BIN_DIR:${PATH:-}"
  export PATH
  NEXTTRACE_READY=1
  return 0
}

remove_missing_tool() {
  wanted=$1
  remaining=""
  for tool in $MISSING_TOOLS; do
    [ "$tool" = "$wanted" ] && continue
    remaining="${remaining}${remaining:+ }$tool"
  done
  MISSING_TOOLS=$remaining
}

module_enabled() {
  module=$1
  if [ "$CONFIG_GIVEN" -eq 1 ]; then
    base_modules="$MODULE_FULL_MODULES"
  elif [ -n "$ONLY" ]; then
    base_modules="$ONLY"
  else
    case "$PROFILE" in
      standard)
        base_modules="$MODULE_STANDARD_MODULES"
        ;;
      full)
        base_modules="$MODULE_FULL_MODULES"
        ;;
      *)
        base_modules="$MODULE_FULL_MODULES"
        ;;
    esac
  fi
  list_contains "$base_modules" "$module" || return 1
  list_contains "$SKIP" "$module" && return 1
  if [ "$LOCAL_ONLY" -eq 1 ]; then
    [ "$(manifest_exposure "$module")" = local ] || return 1
  fi
  return 0
}

collect_missing_tools() {
  MISSING_TOOLS=""
  NEXTTRACE_REQUESTED=0

  # RequiredTools is metadata consumed as an execution contract.  Route and
  # backtrace both require the one supported engine, NextTrace.
  module_words=$(printf '%s' "$MODULE_ALL_MODULES" | tr ',' ' ')
  for module in $module_words; do
    module_enabled "$module" || continue
    tools=$(manifest_tools_for "$module" || true)
    [ -n "$tools" ] || continue
    tool_words=$(printf '%s' "$tools" | tr ',' ' ')
    for tool in $tool_words; do
      if [ "$tool" = "nexttrace" ]; then
        NEXTTRACE_REQUESTED=1
      fi
      tool_exists "$tool" || add_missing_tool "$tool"
    done
  done
}

install_packages() {
  install_status=0
  if [ -n "$PACKAGES" ]; then
    say "把测试组件下载并解包到临时前缀：$PACKAGES" "downloading and extracting test components into the temporary prefix: $PACKAGES"
    case "$PACKAGE_MANAGER" in
      apt)
        prepare_apt_tools || install_status=1
        # gpg is a helper for verifying the pinned Ookla key, not a benchmark
        # dependency.  If it was absent on the host, use only a staged copy.
        if [ "$OOKLA_MISSING" -eq 1 ] && ! command -v gpg >/dev/null 2>&1; then
          if stage_temp_tool gpg; then
            activate_temp_tool_path
          fi
        fi
        ;;
      dnf|yum|apk|pacman)
        say "当前发行版的包格式没有受支持的临时解包路径，跳过：$PACKAGES" \
          "this distro has no supported temporary extractor for its package format; skipping: $PACKAGES"
        install_status=1
        ;;
      *) install_status=1 ;;
    esac
  fi
  if [ "$OOKLA_MISSING" -eq 1 ]; then
    case "$PACKAGE_MANAGER" in
      apt)
        if install_ookla; then
          remove_missing_tool speedtest
        else
          install_status=1
        fi
        ;;
      *)
        say "speedtest 只能在 Debian/Ubuntu 临时校验解包；此处跳过" \
          "speedtest temporary verified extraction is supported only on Debian/Ubuntu; skipping here"
        install_status=1
        ;;
    esac
  fi
  # A failed download is a controlled degradation, not permission to fall back
  # to a global package install.  The caller leaves unresolved tools in the
  # missing list so ecs records the skipped modules explicitly.
  [ "$install_status" -eq 0 ] || return 0
  return 0
}

prepare_dependencies() {
  collect_missing_tools
  NEXTTRACE_FAILED=0
  OOKLA_MISSING=0
  for tool in $MISSING_TOOLS; do
    [ "$tool" = speedtest ] && OOKLA_MISSING=1
  done
  if [ -z "$MISSING_TOOLS" ]; then
    say "测试组件已就绪" "test components are ready"
    return 0
  fi

  say "缺少组件：$MISSING_TOOLS" "missing components: $MISSING_TOOLS"
  if [ "$AUTO_DEPS" -eq 0 ]; then
    if [ "$NEXTTRACE_REQUESTED" -eq 1 ]; then
      NEXTTRACE_FAILED=1
      say "已关闭自动依赖准备，NextTrace 路由模块将跳过。" "automatic dependency setup is disabled; NextTrace route modules will be skipped"
    fi
    say "继续运行；报告会明确标注缺少的组件。" "continuing with explicit missing-tool warnings"
    return 0
  fi

  # A missing NextTrace is intentionally handled outside the system package
  # manager.  The verified full asset lives only in WORK/bin and is removed by
  # the EXIT trap, so pre-existing binaries and packages are never changed.
  if [ "$NEXTTRACE_REQUESTED" -eq 1 ] && ! tool_exists nexttrace; then
    say "缺少 NextTrace，正在从官方 GitHub Release 临时准备已校验的 full 二进制" "NextTrace is missing; temporarily preparing its verified full binary from the official GitHub Release"
    if prepare_nexttrace; then
      remove_missing_tool nexttrace
      say "NextTrace 已就绪（仅在本次 WORK 中使用）" "NextTrace is ready (scoped to this run's temporary WORK directory)"
    else
      remove_missing_tool nexttrace
      NEXTTRACE_FAILED=1
      say "NextTrace 下载或 SHA-256 校验失败，路由模块将跳过" "NextTrace download or SHA-256 verification failed; route modules will be skipped"
    fi
  fi
  if [ -z "$MISSING_TOOLS" ]; then
    if [ "$NEXTTRACE_FAILED" -eq 1 ]; then
      say "其他测试组件已就绪；NextTrace 不可用，路由模块将跳过" "other test components are ready; NextTrace is unavailable and route modules will be skipped"
    else
      say "测试组件已就绪" "test components are ready"
    fi
    return 0
  fi
  if ! select_package_manager; then
    say "找不到可用的包管理器，缺失组件将跳过；可预先放入 PATH" \
      "no supported package manager; missing components will be skipped (preinstall them on PATH if needed)"
    return 0
  fi
  PACKAGES=""
  for tool in $MISSING_TOOLS; do
    case "$tool" in
      speedtest) OOKLA_MISSING=1 ;;
      *) add_package "$(package_for_tool "$tool")" ;;
    esac
  done
  # The official .deb metadata currently declares ca-certificates.  Download
  # it into WORK before touching the temporary Ookla source; it is never
  # installed into the host.
  if [ "$OOKLA_MISSING" -eq 1 ]; then
    add_package ca-certificates
  fi
  # gpg is only needed to turn the downloaded, pinned Ookla key into an apt
  # keyring.  If it is absent, unpack gnupg into WORK as a helper; never ask
  # the host package manager to install it.
  if [ "$OOKLA_MISSING" -eq 1 ] && [ "$PACKAGE_MANAGER" = apt ] && ! command -v gpg >/dev/null 2>&1; then
    add_package gnupg
  fi

  install_packages
  if [ -n "$MISSING_TOOLS" ]; then
    say "以下组件未能安全放入临时前缀，将按缺失组件降级：$MISSING_TOOLS" \
      "the following components could not be safely staged in the temporary prefix; tests will degrade: $MISSING_TOOLS"
  elif [ "$NEXTTRACE_FAILED" -eq 1 ]; then
    say "测试组件已就绪；NextTrace 不可用，路由模块将跳过" \
      "test components are ready; NextTrace is unavailable and route modules will be skipped"
  else
    say "测试组件已就绪（仅位于本次临时前缀）" \
      "test components are ready (scoped to this run's temporary prefix)"
  fi
}

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

  validate_submit_parent() {
    submit_parent=$1
    while :; do
      [ ! -L "$submit_parent" ] ||
        die "提交输出父目录不能是符号链接：$submit_parent" "submit output parent must not be a symlink: $submit_parent"
      [ -d "$submit_parent" ] ||
        die "提交输出父目录不存在：$submit_parent" "submit output parent does not exist: $submit_parent"
      case "$submit_parent" in
        /) break ;;
        */*) submit_next=${submit_parent%/*}; [ -n "$submit_next" ] || submit_next=/ ;;
        *) submit_next=. ;;
      esac
      [ "$submit_next" = "$submit_parent" ] && break
      submit_parent=$submit_next
    done
  }

  # Fail before downloading/running benchmarks when the final destination is
  # plainly unusable.  Submit mode never creates a user directory and never
  # overwrites an existing file; the downloaded ecs binary repeats the same
  # checks at the final write to cover races.
  if [ -L "$SUBMIT_OUTPUT" ]; then
    die "提交输出不能是符号链接：$SUBMIT_OUTPUT" "submit output must not be a symlink: $SUBMIT_OUTPUT"
  elif [ -d "$SUBMIT_OUTPUT" ]; then
    validate_submit_parent "$SUBMIT_OUTPUT"
    [ -w "$SUBMIT_OUTPUT" ] ||
      die "提交输出目录不可写：$SUBMIT_OUTPUT" "submit output directory is not writable: $SUBMIT_OUTPUT"
  else
    case "$SUBMIT_OUTPUT" in
      */*) SUBMIT_OUTPUT_PARENT=${SUBMIT_OUTPUT%/*}; [ -n "$SUBMIT_OUTPUT_PARENT" ] || SUBMIT_OUTPUT_PARENT=/ ;;
      *) SUBMIT_OUTPUT_PARENT=. ;;
    esac
    validate_submit_parent "$SUBMIT_OUTPUT_PARENT"
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

# The canonical module/profile registry is required for wrapper planning.  A
# stale release fails clearly here instead of letting copied module IDs drive
# package installation.
load_module_manifest ||
  die "下载的 ecs 不支持模块 manifest，请使用最新 Release" "the downloaded ecs does not provide a module manifest; use a current Release"

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
