#!/usr/bin/env bash
set -euo pipefail

# 按 main 上当前的提交重建排行榜参考，有变化就写回。
#
# 这是 CI 侧唯一需要仓库写权限的动作，所以实现放在仓库里可审阅。它只碰两个
# 生成文件，别的什么都不改：
#
#   submissions/baseline.json              社区提交聚合出的参考
#   internal/score/embedded/baseline.json  随二进制发布的内嵌副本
#
# 两份必须同步：内嵌副本才是用户运行 ecs 时实际读到的那一份，只更新仓库里的
# 那份等于让发布出去的二进制继续用旧基线。
#
# 无变化时不提交。定时重建一个没变的文件会制造出一串空洞的提交历史。

source "$(dirname "${BASH_SOURCE[0]}")/lib/common.sh"
cd "$ECS_REPO_ROOT"

die() {
  echo "leaderboard: $*" >&2
  exit 1
}

tracked=(submissions/baseline.json internal/score/embedded/baseline.json)

if ! find submissions -mindepth 2 -maxdepth 2 -type f -name '*.json' -print -quit | grep -q .; then
  echo "leaderboard: 当前没有排行榜提交，保持无基线状态" >&2
  exit 0
fi

ecs_step "重建排行榜参考"
go run ./cmd/ecs baseline \
  --source "社区提交聚合" \
  --output submissions/baseline.json \
  submissions
cp submissions/baseline.json internal/score/embedded/baseline.json

# 重建出来的东西必须能被将要读它的那份代码认下来，否则写回去就是把一个
# 坏基线发给所有人。
ecs_step "校验重建结果"
go build ./...
go test ./internal/score/

if git diff --quiet -- "${tracked[@]}"; then
  echo "leaderboard: 参考无变化，跳过提交" >&2
  exit 0
fi

ecs_step "写回"
git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git add -- "${tracked[@]}"
git commit -m "CI：按最新提交重建排行榜参考"
git push

echo "leaderboard: 已写回" >&2
