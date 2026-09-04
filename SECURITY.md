# Security

## 运行与网络边界

`ecs` 只支持 Linux；原生探针无需 root。默认运行不会安装软件、修改内核参数或系统目录，也不会上传报告。`install.sh` 只安装 `ecs`，标准 `run.sh` 只在临时目录 staging 已校验的固定工具，不调用系统包管理器安装基准工具。

`--exposure` 是外联上限：`local` 禁止联网，`public` 只允许公共基础设施，`thirdparty`（默认）允许已登记的第三方情报服务，`any` 允许所有已登记的外部服务。越过上限的默认模块会被过滤，显式点名则报错。任何联网目标，包括 STUN、测速节点、路由目标、情报接口和 Ookla，都能看到请求的公网出口 IP；该信息不会因本地报告遮盖而对远端隐藏。

磁盘模块只在用户指定的测试目录创建随机命名的 `.ecs-fio-*` 文件，文件大小不超过测试前可用空间的 20%，并在成功、错误或取消时清理。标准 `run.sh` 路径只运行本次临时 staging 的固定 `fio`，解析本地 JSON 输出并记录程序版本和完整参数。

NAT 探测以标准库实现 STUN（RFC 5389/5780）。请求只包含协议头、随机事务 ID 和必要属性；响应只有在 magic cookie 与事务 ID 都匹配时才会采信。STUN 服务仍能看到 UDP 源地址，报告中的映射地址按本机敏感字段处理。

三网测速节点清单固定到每个 `ecs` 版本审计过的上游 commit。节点 URL 必须是绝对 HTTP(S) URL，拒绝 userinfo、fragment、非法端口和特殊用途地址；专用客户端忽略环境代理，在实际拨号处解析并筛选公网地址，每次重定向也重新校验。因此内网/回环目标、DNS rebinding 和重定向不能把它变成 SSRF 通道。部分节点只有 HTTP，测速流量可能被链路观察或篡改，结果不构成机密性或完整性证明。

路由与回程模块只使用官方 NextTrace Tiny，以参数数组调用无启动横幅的 JSON 模式，不经过 shell，并记录实际版本和完整参数。`run.sh` 先校验当前架构 `ecs-tools` 归档在 Release `checksums.txt` 中的摘要，再只把本次需要的成员 staging 到私有 `$WORK/bin`；工具准备失败或 `ECS_AUTO_DEPS=0` 时终止运行，退出时清理 `$WORK`，不安装到系统。

Ookla 是独立的外部适配器，`standard` 不默认运行，`full` 或显式选择才会调用官方客户端。若 `run.sh` 需要临时准备客户端，Debian/Ubuntu 路径会在 `$WORK` 内校验固定 GPG 指纹、验证官方 Packagecloud 签名并解包，不写 `/etc`，也不执行供应商安装脚本；无法安全临时解包的平台会终止运行。Ookla 可独立接收出口 IP、客户端、服务器和测量元数据，因此该模式不属于本地零上传边界。

## 报告隐私与不可信输入

报告默认在写出前遮盖已知本机 IP：IPv4 保留 `/16`，IPv6 保留 `/32`，端口保留。遮盖会遍历报告 schema 的全部导出字符串值；主机名、远端目标、BGP 前缀和路由跳点不会自动遮盖。原始运行对象标记 `run.redacted=false`，遮盖副本才是 `true`。`--reveal` 会写入完整本机 IP，分享前应检查原始路径、错误和证据字段。

报告文件默认权限为 `0600`；由 `ecs` 新建的输出目录为 `0700`。API 密钥只从环境变量读取，不进入配置、命令参数或报告，但运行环境、进程转储和 shell 历史仍由用户保护。

`ecs render`、`ecs compare` 会处理外部 JSON。终端文本输出会把 C0、DEL 和 C1 控制字符替换为空格，再由 `ecs` 自己添加 SGR 颜色，阻止 OSC/CSI、剪贴板、清屏和伪造布局等终端注入；原始 JSON 不会因此改写。

## 安装与供应链

建议从 Release 下载资产后核对 `checksums.txt`，或从源码自行构建。`install.sh` 只接受 HTTPS，强制校验 Release 资产 SHA-256，不关闭证书验证，也不执行下载到的其他脚本。`run.sh` 的临时 staging 仅在私有运行目录中准备已校验工具，不修改主机软件包数据库。

发布链为：

```text
preflight → tools × 7 → assemble → verify → publish
```

发布入口确认候选提交等于当时远端 `main`，随后所有阶段只使用该冻结 SHA；发布构建要求 Git 工作区洁净。工具构建使用固定上游 release tag 与完整 commit、或固定 HTTPS 来源与 SHA-256；NextTrace 资产还必须匹配上游发布的 SHA-256 digest，缺失 digest 即失败。工具 manifest 记录来源与构建参数。完整性校验只设在下载边界：发布链内部不重复校验自己刚产出的字节，最终资产的 `checksums.txt` 供下载方核对。

普通 CI、排行榜重建、security 与 Release workflow 直接通过 `actions/setup-go@v7` 的 `stable` 和 `check-latest` 选择当前官方稳定 Go；根 `go.mod` 的 `go 1.22` 仅声明最低源码兼容版本，`ci.yml` 的 `compat` job 仍固定使用 Go `1.22.x` 并设置 `GOTOOLCHAIN=local`。`devtools/go.mod` 只记录工具 module 的最低 Go 版本要求与工具依赖清单，不是 compiler selector。项目不根据漏洞记录中的修复版本字段自动作升级判断，也不自动创建拉取请求。供应链完整性与漏洞运营是不同问题；上述门禁只说明发布字节与固定输入、提交及工作流之间的关系。

`actions/setup-go@v7` 是本任务为跟随官方稳定版本明确允许的浮动引用例外；除此之外，所有 GitHub Actions `uses` 引用仍固定到完整 40 位 commit SHA。组装阶段记录实际 `go env GOVERSION`；验证阶段解包每个实际主程序，用 `go version -m` 确认 Go 工具链、`vcs.revision` 等于冻结 SHA、`vcs.modified=false`。本项目不生成 GitHub artifact attestation：请用 Release 附带的 `checksums.txt` 核对下载资产。

## 报告安全问题

请通过最终仓库的私有安全报告渠道提交命令注入、路径穿越、任意文件覆盖、未经请求的上传、本机 IP 遮盖失效、临时文件越界、Release 校验绕过、HTML 脚本注入、终端控制序列注入或由远端节点造成的内网请求。请提供 `ecs` 版本、系统/架构、最小复现和预期影响，不要附带真实凭据或未经遮盖的生产报告。
