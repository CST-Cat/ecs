#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

[[ "$(go env GOHOSTOS)" == linux ]] || {
  echo "linux-trace-runtime: requires a Linux host" >&2
  exit 1
}

host_arch=$(go env GOHOSTARCH)
if [[ "$host_arch" == arm ]]; then host_arch=armv7; fi
target="linux_$host_arch"

if ! package_arch=$(ecs_lock_target_field "$target" package); then
  echo "linux-trace-runtime: no locked package architecture for $target" >&2
  exit 1
fi
repository=$(ecs_lock_tool_field nexttrace-tiny repository)
version=$(ecs_lock_tool_field nexttrace-tiny version)
tag=$(ecs_lock_tool_field nexttrace-tiny tag)
asset_pattern=$(ecs_lock_tool_field nexttrace-tiny asset_pattern)
expected_sha=$(jq -er --arg arch "$package_arch" \
  '.tools[] | select(.name == "nexttrace-tiny") | .asset_sha256[$arch]' "$ECS_LOCK_FILE")
asset=${asset_pattern//<architecture>/$package_arch}
url="https://github.com/$repository/releases/download/$tag/$asset"

work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/ecs-linux-trace.XXXXXX")
trap 'rm -rf -- "$work"' EXIT
tool_bin="$work/tool-bin"
mkdir -p "$tool_bin" "$work/reports"
nexttrace="$tool_bin/nexttrace-tiny"

ecs_step "download and verify locked NextTrace Tiny $version ($package_arch)"
ecs_download_sha256 "$url" "$expected_sha" "$nexttrace" "NextTrace Tiny $version"
chmod 0755 "$nexttrace"
printf 'NextTrace Tiny %s SHA-256 verified: %s\n' "$version" "$expected_sha"

# NextTrace needs raw sockets and TTL control. Grant those capabilities only
# to this verified executable in the private temporary staging directory.
if [[ "$(id -u)" == 0 ]]; then
  setcap cap_net_raw,cap_net_admin+ep "$nexttrace"
else
  sudo -n setcap cap_net_raw,cap_net_admin+ep "$nexttrace"
fi

ecs_bin="$work/ecs"
ecs_step "build current ECS for the native Linux host"
go build -o "$ecs_bin" ./cmd/ecs
export ECS_TOOL_BIN="$tool_bin"

assert_report() {
  local module=$1 max_hops=$2 expected_kind=$3 expected_label=$4 expected_methodology_engine=$5
  local expected_profile=$6 expected_scope=$7 report="$work/reports/$module.json"
  local trace_title="probe.$module.normalized_trace_json"
  local arguments="-4 --no-color --json -M --max-hops $max_hops --queries 1 --parallel-requests 1 --timeout 1000 <target>"

  if jq -e \
    --arg module "$module" \
    --arg target_ip "127.0.0.1" \
    --arg trace_title "$trace_title" \
    --arg arguments "$arguments" \
    --arg max_hops "$max_hops" \
    --arg version "$version" \
    --arg kind "$expected_kind" \
    --arg label "$expected_label" \
    --arg methodology_engine "$expected_methodology_engine" \
    --arg profile "$expected_profile" \
    --arg scope "$expected_scope" '
      def has_locked_version($v):
        test("(^|[^0-9.])" + ($v | split(".") | join("[.]")) + "([^0-9.]|$)");
      .schema_version == "ecs.report/v1" and
      (.run.redacted == false) and
      (.results | map(select(.id == $module)) as $matches |
        ($matches | length) == 1 and
        ($matches[0] |
          .status == "ok" and
          .evidence.valid == 1 and .evidence.expected == 1 and
          (.failures == null or ((.failures | type) == "array" and (.failures | length) == 0)) and
          .methodology.kind == $kind and
          .methodology.label == $label and
          .methodology.engine == $methodology_engine and
          .methodology.profile == $profile and
          .methodology.comparison_scope == $scope and
          .methodology.parameters.scope_revision == "2" and
          .methodology.parameters.ip_version == "4" and
          .methodology.parameters.max_hops == $max_hops and
          .methodology.parameters.adapter == "nexttrace-json-v1" and
          .methodology.parameters.arguments == $arguments and
          (.methodology.parameters.targets | contains($target_ip)) and
          (.methodology.parameters.tool_version | has_locked_version($version)) and
          ([.fields[] | select(.key == "engine") | .value.raw] == ["nexttrace-tiny"]) and
          ([.fields[] | select(.key == "version") | .value.raw] as $versions |
            ($versions | length) == 1 and ($versions[0] | has_locked_version($version))) and
          ([.fields[] | select(.key == "arguments") | .value.raw] == [$arguments]) and
          ([.sources[]? | select(.name == "probe.route.source.nexttrace.name" and .url == "https://github.com/nxtrace/NTrace-core")] | length) == 1 and
          ([.text_blocks[]? | select(.title == $trace_title and .language == "json")] | length) == 1 and
          ([.text_blocks[]? | select(.title == $trace_title and .language == "json")][0].content | fromjson |
            .engine == "nexttrace-tiny" and
            .adapter == "nexttrace-json-v1" and
            .family == "ipv4" and
            .target == $target_ip and
            any(.hops[]; .responded == true and .ip == $target_ip))
        )
      )
    ' "$report" >/dev/null; then
    echo "linux-trace-runtime: $module report passed: target=127.0.0.1; max_hops=$max_hops; NextTrace Tiny $version"
    return 0
  fi

  echo "linux-trace-runtime: $module report contract failure; compact result follows:" >&2
  jq -ce --arg module "$module" --arg trace_title "$trace_title" '
    .results[] | select(.id == $module) |
    {
      id, status, failures,
      provenance: [.fields[]? | select(.key == "engine" or .key == "version" or .key == "adapter" or .key == "arguments")],
      methodology: (.methodology | {kind, label, engine, profile, comparison_scope, parameters: (.parameters | {scope_revision, ip_version, max_hops, targets, tool_version, adapter, arguments})}),
      sources,
      normalized_trace: [.text_blocks[]? | select(.title == $trace_title and .language == "json") | (.content | try fromjson catch .)]
    }
  ' "$report" >&2 || cat "$report" >&2
  return 1
}

run_trace() {
  local module=$1 targets=$2
  local -a target_flags=()
  if [[ "$module" == route ]]; then
    target_flags=(--route-targets "$targets")
  else
    target_flags=(--backtrace-targets "telecom:$targets")
  fi
  ecs_step "run real NextTrace $module against IPv4 loopback"
  timeout 90s "$ecs_bin" run \
    --only "$module" \
    --exposure public \
    --ip-version 4 \
    "${target_flags[@]}" \
    --format json \
    --output "$work/reports" \
    --name "$module" \
    --reveal \
    --no-color \
    --yes
}

run_trace route loopback=127.0.0.1
assert_report route 12 protocol-measurement methodology.protocol-measurement \
  "NextTrace Tiny" probe.route.profile probe.route.comparison_scope
run_trace backtrace loopback=127.0.0.1
assert_report backtrace 20 heuristic methodology.heuristic \
  probe.backtrace.methodology.engine probe.backtrace.profile probe.backtrace.comparison_scope
