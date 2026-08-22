# Validation record

日期：2026-08-22
分支：codex/architecture-machine-facts-cleanup

## 通过

以下命令已在当前工作区执行并通过：

~~~sh
go test ./...
go test -race ./internal/ui
git diff --check
sh -n run.sh install.sh compare.sh
bash -n scripts/*.sh scripts/*/*.sh scripts/tools/*.sh
bash scripts/ci/check.sh
bash scripts/install_test.sh
bash scripts/run_test.sh
bash scripts/compare_test.sh
bash scripts/lock_test.sh
bash scripts/devtools_cache_test.sh
bash scripts/package_tools_test.sh
scripts/build_tools_container.sh --arch amd64 --print-params
~~~

其中 scripts/ci/check.sh 已覆盖 gofmt、go vet（含 integration tag）、staticcheck（含 integration tag）、manifest/lock/cache 契约、shell 语法、安装/run/compare 回归、忽略目录、package tests 和 build definitions。

此外，internal/config、internal/model、internal/report、internal/probe、internal/runner、internal/app 等拆分或语义迁移后的相关包测试均已通过；新增的稳定语义、plan、retry、渲染原子性和工具锁回归也包含在 go test ./... 中。

## 仅作工具可运行性验证

固定版本 govulncheck v1.6.0 已从 devtools 模块成功构建并执行 ./... 扫描。由于执行环境使用 Go 1.22.2，扫描报告了旧标准库/工具链 finding；这不是当前仓库在 .go-version 1.26.5 环境中的 clean 结论，故未将其列入“通过”。

## 未执行或未宣称通过

- 需要公网或真实第三方服务的 live/integration 测量。
- 七架构工具的完整源码构建、归档发布、签名/attestation 和 publish。
- GitHub Actions security workflow 在 1.26.5 runner 上的实际结果。

这些项目没有被压缩成“已通过”；它们保留给相应外部环境执行。
## 交付确认

提交 a938385 已通过非强制 push 推送到 origin/codex/architecture-machine-facts-cleanup。推送前远端与本地的提交关系为 ahead 1，推送后工作区保持干净。
