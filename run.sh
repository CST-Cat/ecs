#!/bin/sh
# ecs 一键运行脚本
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
#       └ 自动下载已校验的 ecs 和按架构匹配的固定工具包，把本次所需工具放入临时 PATH，运行并在 ${TMPDIR:-/tmp} 生成本地报告
#   curl -fsSL .../run.sh | sh -s -- --profile full --lang en
#       └ 带参数时跳过向导；组件仍会自动准备，测试结束后删除本次临时前缀
#   curl -fsSL .../run.sh | sh -s -- --submit --provider vultr --region jp -- --profile full --yes
#       └ 一次完成测试，并在 ${TMPDIR:-/tmp} 生成可公开提交的 ecs.submission/v1 文件
#
# 依赖策略：
#   - 所有选中的基准工具都按当前架构从 ecs-tools_linux_<arch>.tar.gz 获取，
#     校验 checksums.txt 后，只把本次需要的固定 binary 放入 WORK/bin；
#     zstd 选中时再从独立 corpus 发行资产准备固定输入。
#     绝不调用系统包安装器，也不改动系统数据库。
#   - standard 默认包含 cnspeed，不包含多源 IP 质量与 Ookla；full 增加后两项。
#     Ookla speedtest 不放入工具包；--only ookla 仍可在任意档位显式单独选择，选中时走独立的 Ookla
#     官方签名软件源路径。
#   - ECS_AUTO_DEPS=0 可关闭自动依赖准备；选中固定工具时会直接终止运行。
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
# This scanner owns only the small early grammar needed for wrapper help:
# help, lang (including its value), and `--`.  The first other token prevents
# a later `--help` from becoming wrapper help.  Keep it before any work-directory
# or release setup so wrapper help remains entirely local and side-effect free.
LANG_SEL=""
LANG_EXPECTED=0
HELP_REQUESTED=0
for arg in "$@"; do
  if [ "$LANG_EXPECTED" -eq 1 ]; then
    [ "$arg" = "--" ] && break
    LANG_SEL="$arg"
    LANG_EXPECTED=0
    continue
  fi
  case "$arg" in
    --) break ;;
    --lang=*) LANG_SEL="${arg#--lang=}" ;;
    -lang=*) LANG_SEL="${arg#-lang=}" ;;
    --lang|-lang) LANG_EXPECTED=1 ;;
    -h|--help) HELP_REQUESTED=1 ;;
    *) break ;;
  esac
done
[ -n "$LANG_SEL" ] || LANG_SEL="${ECS_LANG:-${LC_ALL:-${LANG:-}}}"
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

# fetch 定义在这里而不是靠近首次下载的地方：sh 的函数定义按顺序生效。
fetch() {
  case "$1" in
    https://*) ;;
    *) die "远程下载地址必须使用 HTTPS：$1" "remote download URL must use HTTPS: $1" ;;
  esac
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --https-only --tries=3 --timeout=20 -O "$2" "$1"
  else
    die "需要 curl 或 wget" "curl or wget is required"
  fi
}

file_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}' | tr '[:upper:]' '[:lower:]'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}' | tr '[:upper:]' '[:lower:]'
  else
    return 1
  fi
}

# Help must be local and side-effect free.  In particular, asking the wrapper
# for help must not download a release or prepare system packages first.
if [ "$HELP_REQUESTED" -eq 1 ]; then
    if [ "$UI" = "en" ]; then
      printf '%s\n' \
        'Usage: run.sh [--profile standard|full] [--only MODULES] [options]' \
        '       run.sh --submit [--provider NAME] [--region REGION] [--output PATH] -- [run options]' \
        '' \
        'Downloads a checksummed ecs release, then stages the required frozen architecture-matched tools under a temporary PATH (never system-installs them), and writes reports directly to ${TMPDIR:-/tmp} by default.' \
        'When route/backtrace needs NextTrace Tiny, run.sh stages the official nexttrace-tiny asset; disabling automatic dependency setup makes a selected-tool run fail.' \
        'No report directory is created by default; pass --output PATH to choose a destination.' \
        'With --submit, wrapper options precede an exact -- and ecs run options follow it; wrapper --output chooses the submission file or directory.' \
        'Provider and region are auto-detected from safe local report metadata when available; --provider/--region override them, otherwise they remain blank.' \
        'Common options: --profile, --only, --skip, --config, --exposure, --lang, --yes.' \
        'The standard profile includes cnspeed and omits multi-source IP quality and Ookla; the full profile adds both omitted modules. Explicit --only may select any module. When Ookla is selected, run.sh uses its separate verified official package-source path under WORK.'
    else
      printf '%s\n' \
        '用法：run.sh [--profile standard|full] [--only 模块] [选项]' \
        '      run.sh --submit [--provider 商家] [--region 地区] [--output 路径] -- [测试选项]' \
        '' \
        '下载并校验 ecs Release，再按架构下载并校验固定工具包，把本次需要的 binary 放入临时 PATH（不会安装到系统），并默认直接在 ${TMPDIR:-/tmp} 生成报告。' \
        '选中 route/backtrace 时使用官方 nexttrace-tiny；固定工具准备失败或 ECS_AUTO_DEPS=0 时直接终止运行。' \
        '默认不会创建新的报告目录；请用 --output PATH 指定输出位置。' \
        '使用 --submit 时，wrapper 选项放在精确的 -- 之前，ecs run 选项放在其后；wrapper --output 指定提交文件或目录。' \
        '有安全的本机报告元数据时会自动识别云厂商和地区；--provider/--region 可显式覆盖，无法识别时留空。' \
        '常用选项：--profile、--only、--skip、--config、--exposure、--lang、--yes。' \
        'standard 默认包含 cnspeed，不包含多源 IP 质量与 Ookla；full 增加后两项。显式使用 --only 可在任意档位选择任意模块。选中 Ookla 时，脚本会走独立的临时、已验证官方包源路径。'
    fi
    exit 0
fi

# An exact -- activates the wrapper grammar.  Only the tokens before it may be
# wrapper submit options; every token after it is rebuilt unchanged as ecs run
# argv.  Without a boundary, the complete argv belongs to ecs as before.
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

WRAPPER_BOUNDARY=0
for arg in "$@"; do
  [ "$arg" != "--" ] || {
    WRAPPER_BOUNDARY=1
    break
  }
done

if [ "$WRAPPER_BOUNDARY" -eq 1 ]; then
  RUN_ARGUMENTS=0
  # Process exactly the original argc. Post-boundary tokens are rotated behind
  # the unprocessed argv, so the loop ends with only verbatim run argv in "$@".
  WRAPPER_REMAINING=$#
  while [ "$WRAPPER_REMAINING" -gt 0 ]; do
    arg=$1
    shift
    WRAPPER_REMAINING=$((WRAPPER_REMAINING - 1))
    if [ "$RUN_ARGUMENTS" -eq 1 ]; then
      set -- "$@" "$arg"
      continue
    fi
    case "$arg" in
      --)
        RUN_ARGUMENTS=1
        ;;
      --submit)
        SUBMIT_MODE=1
        ;;
      --submit=*)
        die "--submit 不接受参数" "--submit does not take a value"
        ;;
      --provider)
        [ "$WRAPPER_REMAINING" -gt 0 ] && [ "$1" != "--" ] ||
          die "--provider 缺少参数" "--provider requires a value"
        [ "$SUBMIT_PROVIDER_GIVEN" -eq 0 ] ||
          die "--provider 不能重复" "--provider must not be repeated"
        SUBMIT_PROVIDER=$1
        shift
        WRAPPER_REMAINING=$((WRAPPER_REMAINING - 1))
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
        [ "$WRAPPER_REMAINING" -gt 0 ] && [ "$1" != "--" ] ||
          die "--region 缺少参数" "--region requires a value"
        [ "$SUBMIT_REGION_GIVEN" -eq 0 ] ||
          die "--region 不能重复" "--region must not be repeated"
        SUBMIT_REGION=$1
        shift
        WRAPPER_REMAINING=$((WRAPPER_REMAINING - 1))
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
        [ "$WRAPPER_REMAINING" -gt 0 ] && [ "$1" != "--" ] ||
          die "--output 缺少路径" "--output requires a path"
        [ "$SUBMIT_OUTPUT_GIVEN" -eq 0 ] ||
          die "--output 不能重复" "--output must not be repeated"
        SUBMIT_OUTPUT=$1
        shift
        WRAPPER_REMAINING=$((WRAPPER_REMAINING - 1))
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
        die "wrapper 分隔符 -- 前存在不支持的参数：$arg" "unsupported argument before the wrapper boundary --: $arg"
        ;;
    esac
  done
  [ "$RUN_ARGUMENTS" -eq 1 ] ||
    die "wrapper 参数边界解析失败" "failed to parse the wrapper argument boundary"
fi

if [ "$SUBMIT_MODE" -eq 0 ] &&
    [ "$SUBMIT_PROVIDER_GIVEN:$SUBMIT_REGION_GIVEN:$SUBMIT_OUTPUT_GIVEN" != "0:0:0" ]; then
  die "--provider、--region 和 wrapper --output 只能与 --submit 同时使用" \
    "--provider, --region, and wrapper --output require --submit"
fi

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
APT_HOST_LISTS="/var/lib/apt/lists"
APT_STATE_MODE="host"
APT_TEMP_SOURCES=""
MISSING_TOOLS=""
PACKAGES=""
OOKLA_MISSING=0
OOKLA_REPO_READY=0
OOKLA_KEY_ASC=""
OOKLA_KEYRING=""
OOKLA_GNUPGHOME=""
OOKLA_APT_SOURCES=""
OOKLA_APT_LISTS=""
OOKLA_DEB_SEEN=""
OOKLA_KEY_FINGERPRINT="C525F88FCF3A7E56CE2CF59131EB3981E723ACAA"
TOOLS_ASSET="ecs-tools_linux_${ARCH}.tar.gz"
TOOLS_ARCHIVE="$WORK/$TOOLS_ASSET"
TOOLS_EXTRACT_ROOT="$WORK/tools"
TOOLS_STAGING_BIN="$WORK/tools-staging-bin"
TOOLS_REQUESTED=""
TOOLS_READY=0
TOOLS_BASE="${ECS_TOOLS_BASE_URL:-$BASE}"
TOOLS_BASE=${TOOLS_BASE%/}
TOOLS_CHECKSUMS_FILE="$WORK/ecs-tools-checksums.txt"
ZSTD_CORPUS_ASSET='ecs-corpus_silesia-v1.tar.gz'
ZSTD_CORPUS_NAME='ecs-silesia-v1.corpus'
ZSTD_CORPUS_BASE="${ECS_CORPUS_BASE_URL:-$BASE}"
ZSTD_CORPUS_BASE=${ZSTD_CORPUS_BASE%/}
ZSTD_CORPUS_ARCHIVE="$WORK/$ZSTD_CORPUS_ASSET"
ZSTD_CORPUS_EXTRACT_ROOT="$WORK/zstd-corpus"

# Install the cleanup trap as soon as WORK exists.  This also covers argument
# validation and execution-plan failures that happen before the test body starts.
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

add_tools_request() {
  tools_request=$1
  case " $TOOLS_REQUESTED " in
    *" $tools_request "*) ;;
    *) TOOLS_REQUESTED="${TOOLS_REQUESTED:+$TOOLS_REQUESTED }$tools_request" ;;
  esac
}

tools_tar_extract_member() {
  tools_archive_path=$1
  tools_extract_path=$2
  tools_member=$3
  tar -xzf "$tools_archive_path" -C "$tools_extract_path" "$tools_member"
}

prepare_zstd_corpus() {
  fetch "${ZSTD_CORPUS_BASE}/${ZSTD_CORPUS_ASSET}" "$ZSTD_CORPUS_ARCHIVE" || return 1
  mkdir -p "$ZSTD_CORPUS_EXTRACT_ROOT" || return 1
  tar -xzf "$ZSTD_CORPUS_ARCHIVE" -C "$ZSTD_CORPUS_EXTRACT_ROOT" "$ZSTD_CORPUS_NAME" || return 1
  zstd_corpus_path="$ZSTD_CORPUS_EXTRACT_ROOT/$ZSTD_CORPUS_NAME"
  [ -f "$zstd_corpus_path" ] && [ ! -L "$zstd_corpus_path" ] || return 1
  # The zstd probe owns the fixed corpus size and digest contract immediately
  # before use; repeating it in this wrapper only reads the 200 MiB file twice.
  export ECS_ZSTD_CORPUS="$zstd_corpus_path"
}

prepare_tools_archive() {
  [ -n "$TOOLS_REQUESTED" ] || return 0
  say "下载并校验架构工具包 $TOOLS_ASSET" "downloading and verifying architecture tool package $TOOLS_ASSET"
  if [ "$TOOLS_BASE" = "$BASE" ]; then
    cp "$WORK/checksums.txt" "$TOOLS_CHECKSUMS_FILE" || return 1
  else
    fetch "${TOOLS_BASE}/checksums.txt" "$TOOLS_CHECKSUMS_FILE" || return 1
  fi
  fetch "${TOOLS_BASE}/${TOOLS_ASSET}" "$TOOLS_ARCHIVE" || return 1

  TOOLS_EXPECTED=$(awk -v f="$TOOLS_ASSET" '$2 == f {print $1; exit}' "$TOOLS_CHECKSUMS_FILE" | tr '[:upper:]' '[:lower:]')
  [ -n "$TOOLS_EXPECTED" ] || return 1
  TOOLS_ACTUAL=$(file_sha256 "$TOOLS_ARCHIVE") || return 1
  [ "$TOOLS_ACTUAL" = "$TOOLS_EXPECTED" ] || return 1
  mkdir -p "$TOOLS_EXTRACT_ROOT" "$TOOLS_STAGING_BIN" || return 1
  for tools_requested in $TOOLS_REQUESTED; do
    tools_tar_extract_member "$TOOLS_ARCHIVE" "$TOOLS_EXTRACT_ROOT" "bin/$tools_requested" || return 1
    tools_source="$TOOLS_EXTRACT_ROOT/bin/$tools_requested"
    [ -f "$tools_source" ] && [ ! -L "$tools_source" ] && [ -x "$tools_source" ] || return 1
    # Copy, instead of exposing the whole extracted tree, so PATH contains
    # exactly the binaries needed by this invocation. The complete archive
    # SHA-256 was verified above; there is no second JSON digest contract.
    cp "$tools_source" "$TOOLS_STAGING_BIN/$tools_requested" || return 1
    chmod 0755 "$TOOLS_STAGING_BIN/$tools_requested" || return 1
  done
  case " $TOOLS_REQUESTED " in
    *" zstd "*)
      prepare_zstd_corpus || return 1
      ;;
  esac
  # Do not alter MISSING_TOOLS or PATH until every requested binary has passed
  # its regular-file/executable checks. A later failure therefore keeps the
  # whole request unresolved instead of exposing a partial tool set.
  [ ! -e "$TEMP_TOOL_BIN" ] || return 1
  mv "$TOOLS_STAGING_BIN" "$TEMP_TOOL_BIN" || return 1
  for tools_requested in $TOOLS_REQUESTED; do
    remove_missing_tool "$tools_requested"
  done
  activate_temp_tool_path
  TOOLS_READY=1
  return 0
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
    "$APT_TEMP_CACHE/archives/partial" || return 1
  EXTRACTED_DEBS="$WORK/extracted.debs"
  : >"$EXTRACTED_DEBS"
}

# Use the test machine's configured source list and already downloaded package
# metadata when available.  Only the package archives and extracted runtime
# files belong to WORK.  On a fresh image with no usable host metadata, fall
# back to a private apt state under WORK; that fallback still never invokes
# dpkg, writes /var/lib/dpkg, or acquires the host package lock.
apt_temp_command() {
  if [ "$APT_STATE_MODE" = "host" ]; then
    apt-get \
      -o "Dir::Etc::sourcelist=/etc/apt/sources.list" \
      -o "Dir::Etc::sourceparts=/etc/apt/sources.list.d" \
      -o "Dir::State::lists=$APT_HOST_LISTS" \
      -o "Dir::Cache::archives=$APT_TEMP_CACHE/archives" \
      -o "Dir::Cache::pkgcache=$APT_TEMP_CACHE/pkgcache.bin" \
      -o "Dir::Cache::srcpkgcache=$APT_TEMP_CACHE/srcpkgcache.bin" \
      "$@"
  else
    apt-get \
      -o "Dir::Etc::sourcelist=/etc/apt/sources.list" \
      -o "Dir::Etc::sourceparts=/etc/apt/sources.list.d" \
      -o "Dir::State::lists=$APT_TEMP_LISTS" \
      -o "Dir::Cache::archives=$APT_TEMP_CACHE/archives" \
      -o "Dir::Cache::pkgcache=$APT_TEMP_CACHE/pkgcache.bin" \
      -o "Dir::Cache::srcpkgcache=$APT_TEMP_CACHE/srcpkgcache.bin" \
      "$@"
  fi
}

apt_cache_command() {
  if [ "$APT_STATE_MODE" = "host" ]; then
    apt-cache \
      -o "Dir::Etc::sourcelist=/etc/apt/sources.list" \
      -o "Dir::Etc::sourceparts=/etc/apt/sources.list.d" \
      -o "Dir::State::lists=$APT_HOST_LISTS" \
      -o "Dir::Cache::pkgcache=$APT_TEMP_CACHE/pkgcache.bin" \
      -o "Dir::Cache::srcpkgcache=$APT_TEMP_CACHE/srcpkgcache.bin" \
      "$@"
  else
    apt-cache \
      -o "Dir::Etc::sourcelist=/etc/apt/sources.list" \
      -o "Dir::Etc::sourceparts=/etc/apt/sources.list.d" \
      -o "Dir::State::lists=$APT_TEMP_LISTS" \
      -o "Dir::Cache::pkgcache=$APT_TEMP_CACHE/pkgcache.bin" \
      -o "Dir::Cache::srcpkgcache=$APT_TEMP_CACHE/srcpkgcache.bin" \
      "$@"
  fi
}

apt_update_temp() {
  [ "${APT_TEMP_UPDATED:-0}" -eq 1 ] && return 0
  prepare_temp_tool_dirs || return 1
  APT_STATE_MODE="host"
  host_metadata=1
  [ -d "$APT_HOST_LISTS" ] || host_metadata=0
  if [ "$host_metadata" -eq 1 ]; then
    for apt_package in $PACKAGES; do
      apt_candidate=$(apt_cache_command policy "$apt_package" 2>/dev/null |
        awk '/^[[:space:]]*Candidate:/ {print $2; exit}')
      case "$apt_candidate" in
        ''|'(none)') host_metadata=0; break ;;
      esac
    done
  fi
  if [ "$host_metadata" -eq 1 ]; then
    APT_TEMP_UPDATED=1
    return 0
  fi

  APT_STATE_MODE="temp"
  mkdir -p "$APT_TEMP_LISTS/partial" "$APT_TEMP_CACHE/archives/partial" || return 1
  if ! package_command apt_temp_command update; then
    return 1
  fi
  APT_TEMP_UPDATED=1
  return 0
}

apt_download_resolved() {
  [ -n "${APT_RESOLVED_PACKAGES:-}" ] || return 1
  APT_LOG="$WORK/package-manager.log"
  if ! (cd "$TEMP_TOOL_CACHE" && apt_temp_command download $APT_RESOLVED_PACKAGES) \
      >>"$APT_LOG" 2>&1; then
    say "无法从测试机已配置的 apt 源准备测试组件，运行终止（日志：$APT_LOG）" \
      "could not prepare the test components from the test machine's configured apt sources; the run will stop (log: $APT_LOG)"
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
      say "无法安全解包 $deb，运行终止" "could not safely extract $deb; the run will stop"
      return 1
    fi
    printf '%s\n' "$deb" >>"$EXTRACTED_DEBS"
    # The executable runtime is what this run needs.  Keep the downloaded
    # archive out of the final WORK footprint after successful extraction.
    rm -f "$deb" || return 1
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

append_temp_library_path() {
  append_lib_dir=$1
  [ -d "$append_lib_dir" ] || return 0
  case ":${TEMP_LIB_PATH:-}:" in
    *":$append_lib_dir:"*) ;;
    *) TEMP_LIB_PATH="${TEMP_LIB_PATH:+$TEMP_LIB_PATH:}$append_lib_dir" ;;
  esac
}

temp_library_dir_has_shared_objects() {
  # A literal unmatched glob is harmless here: it is not a regular file.
  for temp_lib_object in "$1"/*.so "$1"/*.so.*; do
    [ -f "$temp_lib_object" ] && return 0
  done
  return 1
}

activate_temp_tool_path() {
  [ "$TEMP_TOOL_PATH_READY" -eq 1 ] && return 0
  # Only the explicitly staged tool links are prepended. ECS_TOOL_BIN separately
  # confines benchmark lookup to this directory; the host PATH remains available
  # to ordinary system inspection and download helpers.
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
    append_temp_library_path "$temp_lib_part"
  done
  # Debian/Ubuntu place most ELF objects below a multi-arch directory (for
  # example usr/lib/aarch64-linux-gnu).  Add only the conventional ABI
  # directory names, all of which are confined below WORK/root.
  for temp_lib_parent in "$TEMP_TOOL_ROOT/lib" "$TEMP_TOOL_ROOT/usr/lib"; do
    for temp_lib_part in "$temp_lib_parent"/*; do
      [ -d "$temp_lib_part" ] || continue
      case "${temp_lib_part##*/}" in
        *-linux-gnu*|*-linux-musl*|*-gnu*|*-musl*)
          append_temp_library_path "$temp_lib_part"
          # Some distro libraries keep a required SONAME in a private
          # directory and encode its installed absolute path as RUNPATH.
          # Ceph is the common fio case: librados needs
          # usr/lib/<triplet>/ceph/libceph-common.so.2.  The package is
          # extracted below WORK instead of /usr, so mirror each immediate
          # private directory containing shared objects in LD_LIBRARY_PATH.
          for temp_private_lib_dir in "$temp_lib_part"/*; do
            [ -d "$temp_private_lib_dir" ] || continue
            temp_library_dir_has_shared_objects "$temp_private_lib_dir" || continue
            append_temp_library_path "$temp_private_lib_dir"
          done
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
    say "系统缺少 dpkg-deb，无法安全临时解包 Debian 包；运行终止" \
      "dpkg-deb is unavailable, so Debian packages cannot be safely extracted temporarily; the run will stop"
    return 1
  }
  apt_update_temp || return 1
  APT_PENDING="$WORK/apt.pending"
  APT_SEEN="$WORK/apt.seen"
  APT_RESOLVED_PACKAGES=""
  : >"$APT_PENDING"
  : >"$APT_SEEN"
  for apt_package in $PACKAGES; do
    printf '%s\n' "$apt_package" >>"$APT_PENDING"
  done

  # Resolve the dependency closure using the test machine's signed package
  # metadata, then download the complete closure in one apt invocation and
  # unpack every .deb under WORK.  Batching avoids one network transaction per
  # dependency while still using apt-get's `download` subcommand rather than
  # `install`: dpkg is never invoked and no privilege authorization is requested.
  while IFS= read -r apt_package; do
    [ -n "$apt_package" ] || continue
    if grep -F -x "$apt_package" "$APT_SEEN" >/dev/null 2>&1; then
      continue
    fi
    printf '%s\n' "$apt_package" >>"$APT_SEEN"
    APT_RESOLVED_PACKAGES="${APT_RESOLVED_PACKAGES:+$APT_RESOLVED_PACKAGES }$apt_package"
    apt_dependency_names "$apt_package" >>"$APT_PENDING" || true
  done <"$APT_PENDING"

  apt_download_resolved || return 1
  extract_debs || return 1
  prepared_any=0
  for missing_tool in $MISSING_TOOLS; do
    if stage_temp_tool "$missing_tool"; then
      prepared_any=1
      remove_missing_tool "$missing_tool"
    else
      say "无法在临时前缀中准备测试组件，运行终止" \
        "could not prepare the test components in the temporary prefix; the run will stop"
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
    say "Ookla 签名源元数据下载失败，运行终止（日志：$OOKLA_LOG）" \
      "Ookla signed-source metadata download failed; the run will stop (log: $OOKLA_LOG)"
    return 1
  fi
  # Download the vendor package only.  `apt-get download` verifies the
  # Packagecloud Release metadata with the pinned key but never invokes dpkg.
  if ! (cd "$TEMP_TOOL_CACHE" && apt_ookla_command download speedtest) >>"$OOKLA_LOG" 2>&1; then
    say "Ookla speedtest 包下载失败，运行终止（日志：$OOKLA_LOG）" \
      "Ookla speedtest package download failed; the run will stop (log: $OOKLA_LOG)"
    return 1
  fi
  extract_debs || return 1
  stage_temp_tool speedtest || return 1
  activate_temp_tool_path
  OOKLA_REPO_READY=1
}

prepare_ookla_rpm() {
  # RPM metadata can be verified without installation, but extracting an RPM
  # safely requires rpm2cpio/cpio (and dnf's download plugin).  Do not guess or
  # install those helpers globally: terminate instead of running without the
  # selected fixed client.
  #
  # 这里刻意不再生成 .repo 文件与导入 GPG key：当前路径没有安全的临时
  # 解包器，提前建立仓库配置只是在 WORK 里留下用不到的产物。
  say "当前 RPM 路径没有安全的临时解包器，运行终止" \
    "the RPM path lacks a safe temporary extractor; the run will stop"
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

# Module selection and required tools come from the downloaded binary's
# `plan` output. The wrapper does not interpret profile, only, skip,
# config, or exposure flags itself.
PLAN_FILE="$WORK/execution.plan.json"
PLAN_SCHEMA=""
PLAN_PROFILE=""
PLAN_MODULES=""
PLAN_EXPOSURE=""
PLAN_REVEAL=""
PLAN_TOOLS=""

# Read only the machine plan emitted by the downloaded binary. The command's
# sole output is JSON, intentionally simple and stable: values extracted here
# are module/tool IDs, not user-facing text.
load_execution_plan() {
  plan_status=0
  if [ -n "$INTERACTIVE" ]; then
    if "${WORK}/ecs" plan "$@" "$INTERACTIVE" >"$PLAN_FILE"; then
      plan_status=0
    else
      plan_status=$?
    fi
  else
    if "${WORK}/ecs" plan "$@" >"$PLAN_FILE"; then
      plan_status=0
    else
      plan_status=$?
    fi
  fi
  [ "$plan_status" -eq 0 ] || exit "$plan_status"
  [ -s "$PLAN_FILE" ] || exit 0
  PLAN_SCHEMA=$(sed -n 's/^[[:space:]]*"schema_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$PLAN_FILE" | sed -n '1p')
  [ "$PLAN_SCHEMA" = "ecs.plan/v1" ] ||
    die "下载的 ecs 返回了不支持的执行计划 schema" "the downloaded ecs returned an unsupported execution-plan schema"
  PLAN_EXPOSURE_COUNT=$(awk '/^  "exposure"[[:space:]]*:/ {count++} END {print count + 0}' "$PLAN_FILE")
  [ "$PLAN_EXPOSURE_COUNT" -eq 1 ] ||
    die "执行计划缺少或重复 exposure" "the execution plan has a missing or duplicate exposure"
  PLAN_EXPOSURE=$(sed -n 's/^  "exposure"[[:space:]]*:[[:space:]]*"\([^"]*\)"[[:space:]]*,\{0,1\}[[:space:]]*$/\1/p' "$PLAN_FILE")
  case "$PLAN_EXPOSURE" in
    local|public|thirdparty|any) ;;
    *) die "执行计划的 exposure 非法（可选 local、public、thirdparty、any）" "the execution plan has an invalid exposure (choose local, public, thirdparty, or any)" ;;
  esac
  PLAN_REVEAL_COUNT=$(awk '/^  "reveal"[[:space:]]*:/ {count++} END {print count + 0}' "$PLAN_FILE")
  [ "$PLAN_REVEAL_COUNT" -eq 1 ] ||
    die "执行计划缺少或重复 reveal" "the execution plan has a missing or duplicate reveal"
  PLAN_REVEAL=$(sed -n 's/^  "reveal"[[:space:]]*:[[:space:]]*\([^,[:space:]]*\)[[:space:]]*,\{0,1\}[[:space:]]*$/\1/p' "$PLAN_FILE")
  case "$PLAN_REVEAL" in
    true|false) ;;
    *) die "执行计划的 reveal 非法（必须是 true 或 false）" "the execution plan has an invalid reveal (it must be true or false)" ;;
  esac
  PLAN_PROFILE=$(sed -n 's/^[[:space:]]*"profile"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$PLAN_FILE" | sed -n '1p')
  PLAN_MODULES=$(awk '
    /"modules"[[:space:]]*:/ {inside=1; next}
    inside && /"id"[[:space:]]*:/ {
      value=$0
      sub(/.*"id"[[:space:]]*:[[:space:]]*"/, "", value)
      sub(/\".*$/, "", value)
      printf "%s%s", separator, value
      separator=","
    }
    inside && /^[[:space:]]*\]/ {inside=0}
  ' "$PLAN_FILE")
  PLAN_TOOLS=$(awk '
    /"required_tools"[[:space:]]*:[[:space:]]*\[/ {inside=1; next}
    inside && /^[[:space:]]*\]/ {inside=0; next}
    inside && /"[A-Za-z0-9_-]+"/ {
      value=$0
      sub(/^[^\"]*\"/, "", value)
      sub(/\".*$/, "", value)
      print value
    }
  ' "$PLAN_FILE" | sort -u | tr '\n' ' ' | sed 's/[[:space:]]*$//')
  [ -n "$PLAN_PROFILE" ] && [ -n "$PLAN_MODULES" ] ||
    die "Go 执行计划缺少 profile/modules" "the Go execution plan has no profile/modules"
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

collect_missing_tools() {
  MISSING_TOOLS=""
  TOOLS_REQUESTED=""
  # RequiredTools is metadata consumed as an execution contract. The Go plan
  # is the only source of the selected module/tool set.
  for tool in $PLAN_TOOLS; do
    add_missing_tool "$tool"
    case "$tool" in
      zstd)
        # The benchmark contract pins both executable and corpus. An
        # arbitrary host command named zstd is not interchangeable.
        add_tools_request zstd
        ;;
      npb-ep|npb-ft)
        # Class, OpenMP implementation and compiler flags are embedded in
        # the release binaries and verified from every benchmark output.
        add_tools_request "$tool"
        ;;
      openssl)
        # Crypto results use the pinned LTS build rather than the host TLS
        # utility, whose version and Configure options are distribution-specific.
        add_tools_request openssl
        ;;
      nexttrace-tiny)
        add_tools_request nexttrace-tiny
        ;;
      speedtest) ;;
      *) add_tools_request "$tool" ;;
    esac
  done
}

install_packages() {
  install_status=0
  if [ "$OOKLA_MISSING" -eq 1 ]; then
    case "$PACKAGE_MANAGER" in
      apt)
        # Ookla remains a separate dependency from the ecs-tools archive.  Its
        # package and helper (ca-certificates/gpg) are staged privately, but
        # generic benchmark tools never use this package-manager path.
        OOKLA_MISSING_TOOLS="$MISSING_TOOLS"
        MISSING_TOOLS=""
        PACKAGES="ca-certificates"
        if ! command -v gpg >/dev/null 2>&1; then
          add_package gnupg
        fi
        if ! prepare_apt_tools; then
          install_status=1
        elif ! command -v gpg >/dev/null 2>&1 && stage_temp_tool gpg; then
          activate_temp_tool_path
        fi
        MISSING_TOOLS="$OOKLA_MISSING_TOOLS"
        if [ "$install_status" -eq 0 ] && install_ookla; then
          remove_missing_tool speedtest
        else
          install_status=1
        fi
        ;;
      *)
        say "speedtest 只能在 Debian/Ubuntu 临时校验解包，运行终止" \
          "speedtest temporary verified extraction is supported only on Debian/Ubuntu; the run will stop"
        install_status=1
        ;;
    esac
  fi
  # A failed download is not permission to fall back to a global package
  # install. The caller leaves unresolved selected tools in the list and stops
  # before invoking ecs.
  [ "$install_status" -eq 0 ] || return 0
  return 0
}

prepare_dependencies() {
  collect_missing_tools
  OOKLA_MISSING=0
  for tool in $MISSING_TOOLS; do
    [ "$tool" = speedtest ] && OOKLA_MISSING=1
  done
  if [ -z "$MISSING_TOOLS" ]; then
    say "测试组件已就绪" "test components are ready"
    return 0
  fi

  say "准备固定测试组件" "preparing frozen test components"
  if [ "$AUTO_DEPS" -eq 0 ]; then
    die "已关闭自动依赖准备，无法使用固定测试组件" "automatic dependency setup is disabled; frozen test components are required"
  fi

  if [ -n "$TOOLS_REQUESTED" ]; then
    prepare_tools_archive ||
      die "架构固定工具包下载、SHA-256 或可执行文件校验失败，运行终止" \
        "frozen architecture tool package download, SHA-256, or executable verification failed; the run will stop"
  fi
  if [ "$OOKLA_MISSING" -eq 1 ]; then
    if ! select_package_manager; then
      die "找不到 Ookla 所需的包管理器，运行终止" \
        "no package manager is available for the explicit Ookla path; the run will stop"
    else
      install_packages
    fi
  fi
  if [ -n "$MISSING_TOOLS" ]; then
    die "固定测试组件未能完成准备，运行终止" \
      "frozen test components were not fully prepared; the run will stop"
  fi
  say "固定测试组件已就绪（仅位于本次临时前缀）" \
    "frozen test components are ready (scoped to this run's temporary prefix)"
}

if [ "$SUBMIT_MODE" -eq 0 ]; then
  REPORT_DIR="${TMPDIR:-/tmp}"
  [ -d "$REPORT_DIR" ] ||
    die "临时目录不存在：$REPORT_DIR" "temporary directory does not exist: $REPORT_DIR"
  # These defaults precede the untouched user argv in the final Go command,
  # so an explicit Go --output or --name naturally wins without shell parsing.
  REPORT_NAME="ecs-report-${WORK##*/}"
  say "默认报告目录：$REPORT_DIR" "default report directory: $REPORT_DIR"
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

say "下载 $ASSET" "downloading $ASSET"
fetch "${BASE}/${ASSET}" "${WORK}/${ASSET}" || die "下载失败；仓库是否已发布 Release？" "download failed; has the repository published a Release?"
fetch "${BASE}/checksums.txt" "${WORK}/checksums.txt" || die "下载校验文件失败" "failed to download the checksum file"

# 确认下载归档与同一 Release 清单记录的字节一致。
EXPECTED=$(awk -v f="$ASSET" '$2 == f {print $1; exit}' "${WORK}/checksums.txt" | tr '[:upper:]' '[:lower:]')
[ -n "$EXPECTED" ] || die "校验文件里没有 ${ASSET} 的条目" "no checksum entry for ${ASSET}"
if ! ACTUAL=$(file_sha256 "${WORK}/${ASSET}"); then
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
if [ "$SUBMIT_MODE" -eq 0 ] && [ "$#" -eq 0 ] && [ -r /dev/tty ] && [ -w /dev/tty ]; then
  INTERACTIVE="--interactive"
fi
if [ "$SUBMIT_MODE" -eq 0 ] && [ "$#" -le 2 ] && [ -z "$INTERACTIVE" ] && [ -r /dev/tty ] && [ -w /dev/tty ] && [ "$#" -ge 1 ]; then
  case "$1" in
    --lang|--lang=*|-lang|-lang=*) INTERACTIVE="--interactive" ;;
  esac
fi

# Go resolves profile/config/only/skip/exposure once into the machine plan.
# The same selected IDs are passed to the final run only after the plan has
# been consumed, so the wrapper never maintains a second selection algorithm.
load_execution_plan "$@"

if [ "$SUBMIT_MODE" -eq 1 ] && [ "$PLAN_REVEAL" = true ]; then
  die "提交模式不允许 --reveal（中间报告不会公开）" \
    "--reveal is not allowed in submit mode (the intermediate report is private)"
fi

prepare_dependencies

# 不使用 exec：必须等 ecs 退出后运行清理逻辑，并原样保留退出码。
if [ "$SUBMIT_MODE" -eq 1 ]; then
  # Force a JSON report in WORK.  Wrapper-only submit/provider/region/output
  # options were removed above, while every ordinary run option remains
  # quoted in "$@".
  if ECS_TOOL_BIN="$TEMP_TOOL_BIN" "${WORK}/ecs" "$@" --format json --output "$SUBMIT_REPORT_DIR"; then
    RUN_STATUS=0
  else
    RUN_STATUS=$?
  fi
elif [ -n "$PLAN_FILE" ]; then
  if ECS_TOOL_BIN="$TEMP_TOOL_BIN" "${WORK}/ecs" --output "$REPORT_DIR" --name "$REPORT_NAME" "$@" \
      --profile "$PLAN_PROFILE" --only "$PLAN_MODULES" --yes \
      --exposure "$PLAN_EXPOSURE" --reveal="$PLAN_REVEAL"; then
    RUN_STATUS=0
  else
    RUN_STATUS=$?
  fi
else
  if ECS_TOOL_BIN="$TEMP_TOOL_BIN" "${WORK}/ecs" --output "$REPORT_DIR" --name "$REPORT_NAME" "$@"; then
    RUN_STATUS=0
  else
    RUN_STATUS=$?
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
    if "${WORK}/ecs" submit --input "$SUBMIT_REPORT" --output "$SUBMIT_OUTPUT" \
        --provider "$SUBMIT_PROVIDER" --region "$SUBMIT_REGION"; then
      SUBMIT_STATUS=0
    else
      SUBMIT_STATUS=$?
    fi
  elif [ "$SUBMIT_PROVIDER_GIVEN" -eq 1 ]; then
    if "${WORK}/ecs" submit --input "$SUBMIT_REPORT" --output "$SUBMIT_OUTPUT" \
        --provider "$SUBMIT_PROVIDER"; then
      SUBMIT_STATUS=0
    else
      SUBMIT_STATUS=$?
    fi
  elif [ "$SUBMIT_REGION_GIVEN" -eq 1 ]; then
    if "${WORK}/ecs" submit --input "$SUBMIT_REPORT" --output "$SUBMIT_OUTPUT" \
        --region "$SUBMIT_REGION"; then
      SUBMIT_STATUS=0
    else
      SUBMIT_STATUS=$?
    fi
  elif "${WORK}/ecs" submit --input "$SUBMIT_REPORT" --output "$SUBMIT_OUTPUT"; then
    SUBMIT_STATUS=0
  else
    SUBMIT_STATUS=$?
  fi
  exit "$SUBMIT_STATUS"
fi
exit "$RUN_STATUS"
