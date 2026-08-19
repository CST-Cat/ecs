#!/bin/sh
# ecs 一键运行脚本
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/CST-Cat/ecs/main/run.sh | sh
#       └ 自动下载已校验的 ecs 和按架构匹配的工具包，把缺失组件放入临时 PATH，运行并在 ${TMPDIR:-/tmp} 生成本地报告
#   curl -fsSL .../run.sh | sh -s -- --profile full --lang en
#       └ 带参数时跳过向导；组件仍会自动准备，测试结束后删除本次临时前缀
#   curl -fsSL .../run.sh | sh -s -- --submit --profile full --yes
#       └ 一次完成测试，并在 ${TMPDIR:-/tmp} 生成可公开提交的 ecs.submission/v1 文件
#   curl -fsSL .../run.sh | sh -s -- --compare 昨天.json 今天.json
#       └ 转交给 compare.sh 对比已有报告；对比不需要任何基准工具，因此跳过全部依赖准备
#         compare.sh 也可以单独 curl 使用，两个入口等价
#
# 依赖策略：
#   - 已有的通用组件优先使用；固定口径基准选中时使用发行版提供的指定版本与数据。
#   - 缺失的随 ecs 发行的工具按当前架构从 ecs-tools_linux_<arch>.tar.gz 获取，
#     校验 checksums.txt 后，只把本次需要的 binary 放入 WORK/bin；
#     zstd 选中时再从独立 corpus 发行资产准备固定输入。
#     绝不调用系统包安装器，也不改动系统数据库。
#   - standard 默认包含 cnspeed，不包含多源 IP 质量与 Ookla；full 增加后两项。
#     Ookla speedtest 不放入工具包；--only ookla 仍可在任意档位显式单独选择，缺失时才走独立的 Ookla
#     官方签名软件源路径。
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

# fetch 定义在这里而不是靠近首次下载的地方：sh 的函数定义按顺序生效，而
# --compare 的转发必须发生在全部工具准备之前，那时还远没走到下载 Release 那一段。
fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --https-only --tries=3 --timeout=20 -O "$2" "$1"
  else
    die "需要 curl 或 wget" "curl or wget is required"
  fi
}

# Help must be local and side-effect free.  In particular, asking the wrapper
# for help must not download a release or prepare system packages first.
case "${1:-}" in
  -h|--help)
    if [ "$UI" = "en" ]; then
      printf '%s\n' \
        'Usage: run.sh [--profile standard|full] [--only MODULES] [options]' \
        '       run.sh --submit [run options] [--provider NAME] [--region REGION] [--output PATH]' \
        '       run.sh --compare REPORT.json REPORT.json [more reports] [compare options]' \
        '' \
        'Downloads a checksummed ecs release, then stages missing architecture-matched tools under a temporary PATH (never system-installs them), and writes reports directly to ${TMPDIR:-/tmp} by default.' \
        'When route/backtrace needs NextTrace Tiny, run.sh stages the official nexttrace-tiny asset; ECS_AUTO_DEPS=0 skips tool preparation.' \
        'No report directory is created by default; pass --output PATH to choose a destination.' \
        'With --submit, runs one test and writes a small ecs.submission/v1 JSON; --output chooses its file or directory.' \
        'With --compare, hands over to compare.sh, which needs no benchmark tools at all; compare.sh is equally reachable on its own.' \
        'Provider and region are auto-detected from safe local report metadata when available; --provider/--region override them, otherwise they remain blank.' \
        'Common options: --profile, --only, --skip, --config, --exposure, --lang, --yes.' \
        'The standard profile includes cnspeed and omits multi-source IP quality and Ookla; the full profile adds both omitted modules. Explicit --only may select any module. If speedtest is missing, run.sh uses its separate verified official package-source path under WORK.'
    else
      printf '%s\n' \
        '用法：run.sh [--profile standard|full] [--only 模块] [选项]' \
        '      run.sh --submit [测试选项] [--provider 商家] [--region 地区] [--output 路径]' \
        '      run.sh --compare 报告.json 报告.json [更多报告] [对比选项]' \
        '' \
        '下载并校验 ecs Release，再按架构下载并校验缺失工具包，把需要的 binary 放入临时 PATH（不会安装到系统），并默认直接在 ${TMPDIR:-/tmp} 生成报告。' \
        '选中 route/backtrace 时使用官方 nexttrace-tiny，缺失时跳过路由探测；ECS_AUTO_DEPS=0 会跳过依赖准备。' \
        '默认不会创建新的报告目录；请用 --output PATH 指定输出位置。' \
        '使用 --submit 会一次完成测试并生成精简的 ecs.submission/v1 JSON；--output 指定文件或目录。' \
        '使用 --compare 会转交给 compare.sh，它完全不需要基准工具；compare.sh 也可以单独使用。' \
        '有安全的本机报告元数据时会自动识别云厂商和地区；--provider/--region 可显式覆盖，无法识别时留空。' \
        '常用选项：--profile、--only、--skip、--config、--exposure、--lang、--yes。' \
        'standard 默认包含 cnspeed，不包含多源 IP 质量与 Ookla；full 增加后两项。显式使用 --only 可在任意档位选择任意模块。缺少 speedtest 时，脚本会走独立的临时、已验证官方包源路径。'
    fi
    exit 0
    ;;
esac

# --submit is a wrapper-only mode.  Parse and remove its options before any
# arguments reach `ecs run`; keeping the filtering in positional parameters
# avoids eval/string-splitting and therefore preserves spaces and shell
# metacharacters in ordinary user arguments.
SUBMIT_MODE=0
COMPARE_MODE=0
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
    --compare) COMPARE_MODE=1 ;;
    --compare=*) die "--compare 不接受参数" "--compare does not take a value" ;;
  esac
done
# 提交的是一次测试的结果，对比产出的不是测试结果，两者不能叠加。
if [ "$SUBMIT_MODE" -eq 1 ] && [ "$COMPARE_MODE" -eq 1 ]; then
  die "--compare 不能与 --submit 同用" "--compare cannot be combined with --submit"
fi
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
APT_HOST_LISTS="/var/lib/apt/lists"
APT_STATE_MODE="host"
APT_TEMP_SOURCES=""
MISSING_TOOLS=""
PACKAGES=""
NEXTTRACE_REQUESTED=0
NEXTTRACE_FAILED=0
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
ZSTD_CORPUS_SHA256='8df8cf2a9456a3765834b7cd8b7c1114df9dca708dd505e4d37bc12e536395b0'
ZSTD_CORPUS_ASSET='ecs-corpus_silesia-v1.tar.gz'
ZSTD_CORPUS_NAME='ecs-silesia-v1.corpus'
ZSTD_CORPUS_BYTES=211938580
ZSTD_CORPUS_BASE="${ECS_CORPUS_BASE_URL:-$BASE}"
ZSTD_CORPUS_BASE=${ZSTD_CORPUS_BASE%/}
ZSTD_CORPUS_ARCHIVE="$WORK/$ZSTD_CORPUS_ASSET"
ZSTD_CORPUS_CHECKSUMS_FILE="$WORK/ecs-corpus-checksums.txt"
ZSTD_CORPUS_EXTRACT_ROOT="$WORK/zstd-corpus"

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

# --compare 在这里就转走：对比是纯本地计算，不需要基准工具、语料、apt，也不需要
# 临时 PATH——下面那一千多行准备逻辑对它一样都用不上。
#
# 转发而不是在这里重写一遍：缓存、--install、输出目录语义、参数透传都已经在
# compare.sh 里实现并测试过，写第二份只会制造一处迟早分叉的实现。
#
# 关于这个未校验的下载：run.sh 下载的其它东西都对得上摘要（Release 资产对
# checksums.txt），而 compare.sh 不是 Release 资产，
# 没有摘要可对。它从 run.sh 自身被取回的同一个 origin 与同一个 ref 拉取，能篡改
# 它的人同样能篡改 run.sh，因此对 curl|sh 的用户边际风险为零。Release 资产的逐个
# 校验不受影响。
#
# 不用 exec：exec 会替换掉当前 shell，EXIT trap 不再触发，WORK 会永远留在 /tmp。
if [ "$COMPARE_MODE" -eq 1 ]; then
  # 先把 --compare 这个 token 滤掉，其余原样转发。用哨兵旋转而不是字符串拼接：
  # 报告路径里的空格和 shell 元字符必须原样保留，与 --submit 的处理同一套路。
  COMPARE_SENTINEL="__ecs_run_compare_filter_$$__"
  set -- "$@" "$COMPARE_SENTINEL"
  while [ "$#" -gt 0 ] && [ "$1" != "$COMPARE_SENTINEL" ]; do
    arg=$1
    shift
    case "$arg" in
      --compare) continue ;;
    esac
    set -- "$@" "$arg"
  done
  shift

  COMPARE_SCRIPT_URL="${ECS_COMPARE_SCRIPT_URL:-https://raw.githubusercontent.com/${REPO}/main/compare.sh}"
  say "转交给 compare.sh" "handing over to compare.sh"
  fetch "$COMPARE_SCRIPT_URL" "$WORK/compare.sh" ||
    die "下载 compare.sh 失败" "failed to download compare.sh"
  COMPARE_STATUS=0
  sh "$WORK/compare.sh" "$@" || COMPARE_STATUS=$?
  exit "$COMPARE_STATUS"
fi

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

tool_exists() {
  command -v "$1" >/dev/null 2>&1
}

nexttrace_tool_exists() {
  tool_exists nexttrace-tiny
}

stream_is_official() {
  stream_command=$(command -v stream 2>/dev/null || true)
  [ -n "$stream_command" ] && [ -f "$stream_command" ] && [ -x "$stream_command" ] || return 1
  # Do not execute a candidate `stream` merely to identify it: ImageMagick
  # ships a command with the same name.  These strings are stable markers of
  # the official STREAM benchmark output and are absent from that utility.
  command -v strings >/dev/null 2>&1 || return 1
  stream_strings=$(strings "$stream_command" 2>/dev/null || true)
  printf '%s\n' "$stream_strings" | grep -F 'Number of Threads requested' >/dev/null || return 1
  printf '%s\n' "$stream_strings" | grep -F 'Best Rate' >/dev/null || return 1
  printf '%s\n' "$stream_strings" | grep -F 'Function' >/dev/null || return 1
  printf '%s\n' "$stream_strings" | grep -F 'STREAM version' >/dev/null || return 1
  return 0
}

tool_available() {
  case "$1" in
    stream) stream_is_official ;;
    nexttrace-tiny) nexttrace_tool_exists ;;
    *) tool_exists "$1" ;;
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
  if [ "$ZSTD_CORPUS_BASE" = "$BASE" ]; then
    cp "$WORK/checksums.txt" "$ZSTD_CORPUS_CHECKSUMS_FILE" || return 1
  else
    fetch "${ZSTD_CORPUS_BASE}/checksums.txt" "$ZSTD_CORPUS_CHECKSUMS_FILE" || return 1
  fi
  fetch "${ZSTD_CORPUS_BASE}/${ZSTD_CORPUS_ASSET}" "$ZSTD_CORPUS_ARCHIVE" || return 1

  zstd_corpus_expected=$(awk -v f="$ZSTD_CORPUS_ASSET" '$2 == f {print $1; exit}' \
    "$ZSTD_CORPUS_CHECKSUMS_FILE" | tr '[:upper:]' '[:lower:]')
  case "$zstd_corpus_expected" in
    ''|*[!A-Fa-f0-9]*) return 1 ;;
  esac
  [ "${#zstd_corpus_expected}" -eq 64 ] || return 1
  if command -v sha256sum >/dev/null 2>&1; then
    zstd_corpus_archive_actual=$(sha256sum "$ZSTD_CORPUS_ARCHIVE" | awk '{print $1}' | tr '[:upper:]' '[:lower:]')
  elif command -v shasum >/dev/null 2>&1; then
    zstd_corpus_archive_actual=$(shasum -a 256 "$ZSTD_CORPUS_ARCHIVE" | awk '{print $1}' | tr '[:upper:]' '[:lower:]')
  else
    return 1
  fi
  [ "$zstd_corpus_archive_actual" = "$zstd_corpus_expected" ] || return 1

  tar -tzf "$ZSTD_CORPUS_ARCHIVE" >"$WORK/ecs-corpus.list" || return 1
  [ "$(wc -l <"$WORK/ecs-corpus.list")" -eq 1 ] || return 1
  grep -F -x "$ZSTD_CORPUS_NAME" "$WORK/ecs-corpus.list" >/dev/null || return 1
  mkdir -p "$ZSTD_CORPUS_EXTRACT_ROOT" || return 1
  tar -xzf "$ZSTD_CORPUS_ARCHIVE" -C "$ZSTD_CORPUS_EXTRACT_ROOT" "$ZSTD_CORPUS_NAME" || return 1
  zstd_corpus_path="$ZSTD_CORPUS_EXTRACT_ROOT/$ZSTD_CORPUS_NAME"
  [ -f "$zstd_corpus_path" ] && [ ! -L "$zstd_corpus_path" ] || return 1
  [ "$(stat -c %s "$zstd_corpus_path")" -eq "$ZSTD_CORPUS_BYTES" ] || return 1
  if command -v sha256sum >/dev/null 2>&1; then
    zstd_corpus_actual=$(sha256sum "$zstd_corpus_path" | awk '{print $1}' | tr '[:upper:]' '[:lower:]')
  elif command -v shasum >/dev/null 2>&1; then
    zstd_corpus_actual=$(shasum -a 256 "$zstd_corpus_path" | awk '{print $1}' | tr '[:upper:]' '[:lower:]')
  else
    return 1
  fi
  [ "$zstd_corpus_actual" = "$ZSTD_CORPUS_SHA256" ] || return 1
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
  case "$TOOLS_EXPECTED" in
    ''|*[!A-Fa-f0-9]*) return 1 ;;
  esac
  [ "${#TOOLS_EXPECTED}" -eq 64 ] || return 1
  if command -v sha256sum >/dev/null 2>&1; then
    TOOLS_ACTUAL=$(sha256sum "$TOOLS_ARCHIVE" | awk '{print $1}' | tr '[:upper:]' '[:lower:]')
  elif command -v shasum >/dev/null 2>&1; then
    TOOLS_ACTUAL=$(shasum -a 256 "$TOOLS_ARCHIVE" | awk '{print $1}' | tr '[:upper:]' '[:lower:]')
  else
    return 1
  fi
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
  TEMP_PACKAGE_DIGESTS="$WORK/packages.sha256"
  : >"$TEMP_PACKAGE_DIGESTS"
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
    say "无法从测试机已配置的 apt 源下载测试组件，相关测试将跳过（日志：$APT_LOG）" \
      "could not download the test components from the test machine's configured apt sources; related tests will be skipped (log: $APT_LOG)"
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
    say "系统缺少 dpkg-deb，无法安全临时解包 Debian 包；相关测试将跳过" \
      "dpkg-deb is unavailable, so Debian packages cannot be safely extracted temporarily; related tests will be skipped"
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

prepare_ookla_rpm() {
  # RPM metadata can be verified without installation, but extracting an RPM
  # safely requires rpm2cpio/cpio (and dnf's download plugin).  Do not guess or
  # install those helpers globally: report a controlled degradation instead.
  #
  # 这里刻意不再生成 .repo 文件与导入 GPG key：既然本路径必然以跳过收尾，
  # 提前建立仓库配置只是在 WORK 里留下用不到的产物。
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
# 只需要区分"完全不联网"：public 及以上的依赖集完全相同（network 与 standard
# 默认包含的 cnspeed 都是纯 HTTP，不需要外部程序）。
# full 或显式 --only ookla 时，speedtest 进入依赖规划。
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
  TOOLS_REQUESTED=""
  NEXTTRACE_REQUESTED=0

  # RequiredTools is metadata consumed as an execution contract.  Route and
  # backtrace both require the one supported engine, NextTrace Tiny.
  module_words=$(printf '%s' "$MODULE_ALL_MODULES" | tr ',' ' ')
  for module in $module_words; do
    module_enabled "$module" || continue
    tools=$(manifest_tools_for "$module" || true)
    [ -n "$tools" ] || continue
    tool_words=$(printf '%s' "$tools" | tr ',' ' ')
    for tool in $tool_words; do
      case "$tool" in
        zstd)
          # The benchmark contract pins both executable and corpus. An
          # arbitrary system command named zstd is not interchangeable.
          add_missing_tool zstd
          add_tools_request zstd
          ;;
        npb-ep|npb-ft)
          # Class, OpenMP implementation and compiler flags are embedded in
          # the release binaries and verified from every benchmark output.
          add_missing_tool "$tool"
          add_tools_request "$tool"
          ;;
        openssl)
          # Crypto results use the pinned LTS build rather than the host TLS
          # utility, whose version and Configure options are distribution-specific.
          add_missing_tool openssl
          add_tools_request openssl
          ;;
        nexttrace-tiny)
          NEXTTRACE_REQUESTED=1
          if ! nexttrace_tool_exists; then
            add_missing_tool nexttrace-tiny
            add_tools_request nexttrace-tiny
          fi
          ;;
        *)
          if ! tool_available "$tool"; then
            add_missing_tool "$tool"
            # Every non-Ookla RequiredTools entry is resolved from the
            # checksummed architecture archive. The archive extraction below
            # accepts only the exact requested bin/<name> member, so a future
            # descriptor entry that is not shipped there fails closed instead
            # of needing a second shell-side tool registry.
            [ "$tool" = speedtest ] || add_tools_request "$tool"
          fi
          ;;
      esac
    done
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
    if [ "$NEXTTRACE_REQUESTED" -eq 1 ] && ! nexttrace_tool_exists; then
      NEXTTRACE_FAILED=1
      say "已关闭自动依赖准备，NextTrace 路由模块将跳过。" "automatic dependency setup is disabled; NextTrace route modules will be skipped"
    fi
    say "继续运行；报告会明确标注缺少的组件。" "continuing with explicit missing-tool warnings"
    return 0
  fi

  if [ -n "$TOOLS_REQUESTED" ]; then
    if prepare_tools_archive; then
      say "缺失工具已放入本次临时 PATH" "missing tools staged in this run's temporary PATH"
    else
      say "架构工具包下载、SHA-256 或可执行文件校验失败，相关测试将跳过" \
        "architecture tool package download, SHA-256, or executable verification failed; related tests will be skipped"
    fi
  fi
  if [ "$NEXTTRACE_REQUESTED" -eq 1 ] && ! nexttrace_tool_exists; then
    NEXTTRACE_FAILED=1
    say "NextTrace Tiny 不可用，路由模块将跳过" "NextTrace Tiny is unavailable; route modules will be skipped"
  fi
  if [ -z "$MISSING_TOOLS" ]; then
    if [ "$NEXTTRACE_FAILED" -eq 1 ]; then
	      say "其他测试组件已就绪；NextTrace Tiny 不可用，路由模块将跳过" "other test components are ready; NextTrace Tiny is unavailable and route modules will be skipped"
    else
      say "测试组件已就绪" "test components are ready"
    fi
    return 0
  fi
  if [ "$OOKLA_MISSING" -eq 1 ]; then
    if ! select_package_manager; then
      say "找不到 Ookla 所需的包管理器，speedtest 将跳过" \
        "no package manager is available for the explicit Ookla path; speedtest will be skipped"
    else
      install_packages
    fi
  fi
  if [ -n "$MISSING_TOOLS" ]; then
    say "以下组件未能安全放入临时前缀，将按缺失组件降级：$MISSING_TOOLS" \
      "the following components could not be safely staged in the temporary prefix; tests will degrade: $MISSING_TOOLS"
  elif [ "$NEXTTRACE_FAILED" -eq 1 ]; then
	    say "测试组件已就绪；NextTrace Tiny 不可用，路由模块将跳过" \
	      "test components are ready; NextTrace Tiny is unavailable and route modules will be skipped"
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
