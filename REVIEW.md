# Branch review

日期：2026-08-22
基准：main@6e85039301fc817e8c4a290ffb9b111fc37e55a8
当前分支：codex/architecture-machine-facts-cleanup
审查基线：08bd29f；本记录覆盖其后的完整工作区差异。

本记录用于阶段 24 的总审查，区分已经验证的代码路径和当前环境无法执行的外部路径。

## 实质变更追踪

| 范围 | 审查结论 |
| --- | --- |
| Probe 与 i18n | 内建探针输出改为 stable key/Message；报告在展示边界渲染；旧 source-text translation 与 report Localize 路径已删除。 |
| Report 与 compare | 主报告和 comparison 都先完成全部格式渲染再写文件；compare 契约和文档统一为 ecs.compare/v1。 |
| Plan、run 与 retry | ecs plan --json 复用唯一 resolver；run.sh 只消费计划；干扰重试策略来自 ModuleDescriptor，不再由 runner ID 白名单决定。 |
| Config、model 与 probe helper | 大文件按职责拆分；模型、配置和工具执行辅助保持同 package/既有边界，没有引入新框架。 |
| 工具供应链 | tools/lock.json 成为构建、语料和发布校验的固定事实源；构建脚本按工具拆分；devtools 缓存绑定 go.mod/go.sum。 |
| CI、安装器与文档 | security 独立为定时/手动 workflow；leaderboard 排除生成 baseline；安装器和中英文文档同步默认仓库及 benchmark opt-in 语义。 |

## 负向审查

针对活动实现代码的搜索未发现以下旧入口仍被调用：report.Localize、i18n.Text、ECS_PLAN_FILE、GenericBenchmarkRunner，以及 ToolBuilder/provider/plugin 式新框架。run.sh 没有读取 tools/lock.json；锁文件由本地构建链消费。

没有发现将外部工具 stdout/stderr 重新翻译成 ECS canonical 文案的新增路径。原始证据仍按原样保存，稳定语义只覆盖 ECS 自己生成的标题、字段、状态和摘要。

## 未执行项

当前工作区没有把真实公网服务、完整 benchmark 工具构建、七架构发布归档和 release publish 当作本地已通过项。已执行的是源码测试、静态检查、脚本契约、工具锁校验、安装/run/compare/package fixture，以及 build-tools 的参数打印。

本机 Go 为 1.22.2。govulncheck v1.6.0 可以成功构建和运行，但本地扫描输出的是该旧标准库/工具链的已知 finding，因此不能把它表述为 clean security scan。security workflow 使用仓库固定的 .go-version（1.26.5），应由 GitHub Actions 单独提供该环境下的证据。

交付方式：用户已授权将本轮审查通过的修改直接提交并推送到当前 tracking 分支；不创建或更新 Pull Request。
推送结果：a938385 已由非强制 push 写入 origin/codex/architecture-machine-facts-cleanup；本地工作区保持干净。
