# Security

## 运行与网络边界

`ecs` 支持 Linux、FreeBSD 与 Windows Server 2022+ x64；原生探针无需 root。默认运行不会安装软件、修改内核参数或系统目录，也不会上传报告。`install.sh` 只安装 `ecs`，标准 `run.sh` 只在临时目录 staging 已校验的固定工具，不调用系统包管理器安装基准工具。

`--exposure` 是外联上限：`local` 禁止联网，`public` 只允许公共基础设施，`thirdparty`（默认）允许已登记的第三方情报服务，`any` 允许所有已登记的外部服务。越过上限的默认模块会被过滤，显式点名则报错。任何联网目标，包括 STUN、测速节点、路由目标、情报接口和 Ookla，都能看到请求的公网出口 IP；该信息不会因本地报告遮盖而对远端隐藏。

磁盘模块只在用户指定的测试目录创建随机命名的 `.ecs-fio-*` 文件，文件大小不超过测试前可用空间的 20%，并在成功、错误或取消时清理。标准 `run.sh` 路径只运行本次临时 staging 的固定 `fio`，解析本地 JSON 输出并记录程序版本和完整参数。

NAT 探测以标准库实现 STUN（RFC 5389/5780）。请求只包含协议头、随机事务 ID 和必要属性；响应只有在 magic cookie 与事务 ID 都匹配时才会采信。STUN 服务仍能看到 UDP 源地址，报告中的映射地址按本机敏感字段处理。

三网测速节点清单固定到每个 `ecs` 版本审计过的上游 commit。节点 URL 必须是绝对 HTTP(S) URL，拒绝 userinfo、fragment、非法端口和特殊用途地址；专用客户端忽略环境代理，在实际拨号处解析并筛选公网地址，每次重定向也重新校验。因此内网/回环目标、DNS rebinding 和重定向不能把它变成 SSRF 通道。部分节点只有 HTTP，测速流量可能被链路观察或篡改，结果不构成机密性或完整性证明。

路由与回程模块在 Linux 和 Windows 上只使用官方 NextTrace Tiny，以参数数组调用无启动横幅的 JSON 模式，不经过 shell，并记录实际版本和完整参数；Windows Server 2022+ x64 使用锁定的官方 v1.7.1 预编译资产，IPv4 必须通过最终 runner gate，有 global IPv6/default route 时也必须验证 IPv6。FreeBSD 改用 base-system 的 `/usr/sbin/traceroute`（同样以参数数组调用、不经 shell），不下载特权网络程序，并把 `adapter` 与 `arguments` 记为比较参数，避免不同 backend 的结果被当成同一口径。`run.sh` 先校验当前架构 `ecs-tools` 归档在 Bundle Release `checksums.txt` 中的摘要，再只把本次需要的成员 staging 到私有 `$WORK/bin`；工具准备失败或 `ECS_AUTO_DEPS=0` 时终止运行，退出时清理 `$WORK`，不安装到系统。

Ookla 是独立的外部适配器，`standard` 不默认运行，`full` 或显式选择才会调用官方客户端。若 `run.sh` 需要临时准备客户端，Debian/Ubuntu 路径会在 `$WORK` 内校验固定 GPG 指纹、验证官方 Packagecloud 签名并解包，不写 `/etc`，也不执行供应商安装脚本；无法安全临时解包的平台会终止运行。FreeBSD 没有官方 Ookla 客户端，`run.sh` 在进入包管理器路径前就直接失败，不会回退到 Linux 的 Packagecloud 路径。Ookla 可独立接收出口 IP、客户端、服务器和测量元数据，因此该模式不属于本地零上传边界。

## 报告隐私与不可信输入

报告默认在写出前遮盖已知本机 IP：IPv4 保留 `/16`，IPv6 保留 `/32`，端口保留。遮盖会遍历报告 schema 的全部导出字符串值；主机名、远端目标、BGP 前缀和路由跳点不会自动遮盖。原始运行对象标记 `run.redacted=false`，遮盖副本才是 `true`。`--reveal` 会写入完整本机 IP，分享前应检查原始路径、错误和证据字段。

报告文件默认权限为 `0600`；由 `ecs` 新建的输出目录为 `0700`。API 密钥只从环境变量读取，不进入配置、命令参数或报告，但运行环境、进程转储和 shell 历史仍由用户保护。

`ecs render`、`ecs compare` 会处理外部 JSON。终端文本输出会把 C0、DEL 和 C1 控制字符替换为空格，再由 `ecs` 自己添加 SGR 颜色，阻止 OSC/CSI、剪贴板、清屏和伪造布局等终端注入；原始 JSON 不会因此改写。

## 安装与供应链

建议从 ECS Release 下载主程序资产后核对该 Release 的 `checksums.txt`，或从源码自行构建。`install.sh` 只接受 HTTPS，强制校验 ECS Release 资产 SHA-256，不关闭证书验证，也不执行下载到的其他脚本。`run.sh` 的临时 staging 仅在私有运行目录中准备已校验工具；工具和 corpus 的校验信息来自对应 Bundle Release 的 `checksums.txt`，不修改主机软件包数据库。

### Release 供应链拓扑

仓库有三条彼此独立、版本线不同的发布链：ECS application Release、Bundle Release 和仅供 CI 消费的 FreeBSD GNU SDK immutable snapshot。

```text
ECS Release:    freeze → source-checks / ecs-build × 10 → assemble → verify → rehearsal | publish
Bundle Release: tools × 10 (Linux ×7 + FreeBSD ×2 + Windows ×1) → assemble → rehearsal | publish
GNU SDK:        intent → SDK build × 2 → prepare → rehearsal | publish
```

ECS Release 只发布十目标主程序归档及其 `checksums.txt`（七个 Linux 架构、FreeBSD `amd64`/`arm64` 和 Windows `amd64`）。Bundle Release 独立发布固定的 benchmark runtime、工具归档、corpus 及其自己的 `checksums.txt`；ECS 主程序携带所依赖的 Bundle 标识，客户端据此选择 Bundle，而不是从移动中的 `main` 读取或接受用户覆盖。GNU SDK snapshot 不属于用户工具包或 ECS 软件发布，只作为 FreeBSD GNU 构建链按 lock 中 URL+SHA256 消费的 immutable CI 依赖。

三条链的“彩排”都具有**零永久远端副作用**。ECS 与 Bundle 的 `workflow_dispatch` 无条件代表 rehearsal：即使维护者在 Actions UI 中选择了一个形如 `v*` / `bundle-v*` 的 tag ref，正式 `publish` 仍额外要求 `github.event_name == push`，因此 dispatch 不可能获得发布写路径。ECS/Bundle 彩排会消费与正式发布相同的最终 Actions artifact，并让 `publish.sh --check-only` 校验发布边界；该模式在任何 `gh release` 操作之前返回。FreeBSD GNU SDK 的手动入口默认 `release_mode=rehearsal`，会完成双架构源码构建、裁剪、验证、consumer gate、打包、SHA256SUMS 和 release notes，只留下 7 天 Actions artifact；只有显式选择 `release_mode=publish` 并提供合法的新 `sdk_version` 才进入唯一的 `contents:write` job。

ECS Release 发布入口确认候选提交等于当时远端 `main`，随后该流程只使用冻结 SHA。正式 Bundle push 还要求触发 tag 精确等于该提交中的 `tools/BUNDLE`，防止“推 A tag 却按文件内容创建 B Release/tag”。GNU SDK 的正式 publish 在昂贵构建前确认候选 SHA 是当时远端 `main` 并检查目标 Release/tag 尚不存在，构建完成后在真正写入前再次检查一次；rehearsal 不要求 `sdk_version`，也不会消耗 append-only tag 名。Bundle rehearsal 与正式发布都只使用 workflow 触发时的固定 SHA。三条链的构建、校验、prepare/rehearsal job 都只有 `contents:read`；每条链只有唯一正式 `publish` job 持有 `contents:write`。

Bundle 的工具构建使用固定上游 release tag 与完整 commit、或固定 HTTPS 来源与 SHA-256；Windows NextTrace v1.7.1 预编译资产也必须匹配锁定的上游 SHA-256 digest，缺失 digest 即失败，并以逐字节相同的文件进入 Bundle。FreeBSD 工具归档不含 NextTrace 与 `ping`，继续使用 base-system ping/traceroute 合同。工具 manifest 记录来源与构建参数。每条用户可下载 Release 各自产生 `checksums.txt`；GNU SDK snapshot 产生独立 `SHA256SUMS`。完整性校验只设在下载边界，发布链内部不重复校验自己刚产出的字节。

### Immutable Releases

已发布的 ECS/Bundle Release 使用 GitHub Immutable Releases。发布流程先创建 draft、上传资产再 publish；发布后 Release 的不可变性由 GitHub 平台保证：资产不可替换或删除、对应 Git tag 不可移动。GNU SDK snapshot 采用 create-only append-only tag：prepare 阶段先把最终 tarball、`SHA256SUMS` 与 release notes 固定为同一 Actions artifact，正式 publish 只消费这份候选字节并创建新的 immutable Release/tag，不删除或重建旧 tag。CI 不在内部重复执行 attestation verification。

Release immutability 是 repository-level administrative prerequisite：管理员必须在首次正式发布前启用它。Release workflows 不持有 repository Administration 权限，也不在每次发布中重复查询这一 repository-level invariant；该策略由仓库管理员维护。

Go 工具链按职责分开：根 `go.mod` 的 `go 1.22` 只声明最低源码兼容版本，`ci.yml` 的 `compat` job 固定验证 Go `1.22.x`；普通 CI、ECS Release、Bundle 与 FreeBSD 工具链中需要 Go 的 job 固定使用 Go `1.27.1`、`check-latest: false` 和 `GOTOOLCHAIN=local`，以免发布字节随官方 stable 漂移。`security.yml` 与 `leaderboard.yml` 则有意使用 `stable` + `check-latest: true`，分别承担当前官方稳定工具链上的漏洞检查和排行榜维护。`devtools/go.mod` 只记录工具 module 的最低 Go 版本要求与工具依赖清单，不是 compiler selector。项目不根据漏洞记录中的修复版本字段自动作升级判断，也不自动创建拉取请求。

所有 GitHub Actions `uses` 引用（包括 `actions/setup-go`）都固定到完整 40 位 commit SHA；`stable` 只是少数 workflow 传给 setup-go 的编译器选择值，不是浮动的 Action 引用。ECS Release 组装阶段记录实际 `go env GOVERSION`；验证阶段解包每个实际主程序，用 `go version -m` 确认 Go 工具链、`vcs.revision` 等于冻结 SHA、`vcs.modified=false`。客户端下载边界是各自 Release 的 `checksums.txt` 与 SHA-256：`install.sh` 和 `compare.sh` 只校验 ECS Release，`run.sh` 先校验 ECS Release，再校验 Bundle Release 的工具和 corpus；CI 不替代下载方校验，也不在 CI 内重复执行 attestation verification。

## 报告安全问题

请通过最终仓库的私有安全报告渠道提交命令注入、路径穿越、任意文件覆盖、未经请求的上传、本机 IP 遮盖失效、临时文件越界、Release 校验绕过、HTML 脚本注入、终端控制序列注入或由远端节点造成的内网请求。请提供 `ecs` 版本、系统/架构、最小复现和预期影响，不要附带真实凭据或未经遮盖的生产报告。
