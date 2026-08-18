#!/usr/bin/env bash
set -euo pipefail

# 静态质量门禁：格式、静态规则、源码漏洞、数据契约、脚本语法。
#
# CI 的 quality job 与 release 的 preflight 调用的是同一个文件。发布前检查
# 与合并前检查一旦是两份实现，就一定会在某次改动后悄悄分叉，而分叉的那一侧
# 通常是发布路径——因为它跑得少。
#
# 这里只做不联网、可重复的检查。需要真实工具的归 integration，需要第三方
# 服务的归 live。

source "$(dirname "${BASH_SOURCE[0]}")/../lib/common.sh"
cd "$ECS_REPO_ROOT"

# 三种 build tag 组合都要过：tagged 代码的接口变动不能等到定时任务才暴露，
# 只被 tagged 测试使用的辅助函数也不能在默认构建里变成死代码。
ECS_BUILD_TAGS=("" integration live)

# 对三种 tag 组合各跑一次某个检查器。空 tag 不传 -tags，否则 go 会把空字符串
# 当成一个真实的 tag 名。
for_each_build_tag() {
  local label=$1 tags
  shift
  for tags in "${ECS_BUILD_TAGS[@]}"; do
    if [[ -n "$tags" ]]; then
      ecs_step "$label -tags=$tags"
      "$@" -tags="$tags" ./...
    else
      ecs_step "$label"
      "$@" ./...
    fi
  done
}

ecs_step "gofmt"
unformatted=$(gofmt -l ./cmd ./internal)
if [[ -n "$unformatted" ]]; then
  echo "以下文件未通过 gofmt：" >&2
  echo "$unformatted" >&2
  exit 1
fi

for_each_build_tag "go vet" go vet

# 先赋值再用：命令替换直接当实参展开时，构建失败不会触发 set -e，
# 只会静默传进去一个空字符串。赋值失败则会立刻中止。
staticcheck=$(ecs_devtool staticcheck)
for_each_build_tag staticcheck "$staticcheck"

ecs_step "govulncheck（源码）"
govulncheck=$(ecs_devtool govulncheck)
"$govulncheck" ./...

python=$(ecs_python)

ecs_step "ecs-tools JSON Schema 与示例"
"$python" - <<'PY'
import json
from pathlib import Path

from jsonschema import Draft202012Validator

schema = json.loads(Path("tools/ecs-tools-manifest.schema.json").read_text())
example = json.loads(Path("tools/manifest.example.json").read_text())
Draft202012Validator.check_schema(schema)
Draft202012Validator(schema).validate(example)
print("schema 与示例一致")
PY

ecs_step "workflow YAML 语法"
"$python" - <<'PY'
from pathlib import Path

import yaml

workflows = sorted(Path(".github/workflows").glob("*.yml"))
if not workflows:
    raise SystemExit("没有找到任何 workflow")
for path in workflows:
    document = yaml.safe_load(path.read_text())
    if not isinstance(document, dict) or "jobs" not in document:
        raise SystemExit(f"{path} 不是一个带 jobs 的 workflow")
    print(f"{path}: {len(document['jobs'])} 个 job")
PY

ecs_step "shell 语法"
sh -n install.sh
sh -n run.sh
sh -n compare.sh
for script in scripts/*.sh scripts/*/*.sh; do
  [[ -e "$script" ]] || continue
  bash -n "$script"
done

# 发布过程的中间目录一旦入库，就会把 CI 产物混进正在审核的代码变更，
# 也会让"发布源码必须洁净"的检查永远失败。
ecs_step "发布中间目录已被忽略"
for directory in /.ci-tools-stage/ /artifact/ /tools-artifacts/ /tools-stage/ /.devtools-bin/ /dist/; do
  if ! grep -qxF "$directory" .gitignore; then
    echo ".gitignore 没有忽略发布中间目录 $directory" >&2
    exit 1
  fi
done

ecs_step "工具包布局回归"
bash scripts/package_tools_test.sh

# 这段逻辑决定要不要自动开一个升级 Go 的 PR，判错的两个方向都很糟。
ecs_step "安全 triage 判定表"
bash scripts/security/triage_test.sh

ecs_step "构建定义可解析"
for arch in "${ECS_ARCHES[@]}"; do
  scripts/build_tools_container.sh --arch "$arch" --print-params >/dev/null
done

echo
echo "check: 全部静态检查通过"
