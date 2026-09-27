# AGENTS.md

`arisu` 仓库的硬性规则与结构概览。设计细节、配置键语义、接口契约**不在这里**：
它们属于上游框架与插件，权威文档是 [`../kei/docs/`](../kei/docs/) 与
[`../kei-plugin-agent/docs/`](../kei-plugin-agent/docs/)。改动前先读对应上游文档与既有实现。

## 1. 项目定位

- 项目名 `arisu`，**module path 必须是 `github.com/RandomLemon/kei/arisu`**，根目录只有
  `cmd/arisu` 与 `configs/`。
- 它是 [`kei`](https://github.com/RandomLemon/kei) 的宿主二进制：装配适配器与插件、启动引擎、
  优雅退出；业务逻辑一律在上游仓库或独立 module 里，本仓库不实现平台协议、不实现插件逻辑。
- 内置 [`kei-plugin-agent`](https://github.com/RandomLemon/kei-plugin-agent)（插件名 `agent`）。

### 1.1 为什么 module path 嵌在 kei 之下

kei 把 Engine/config/adaptermgr/pluginmgr 放在 `internal/`，`cmd/bot` 是 `package main`
不可导入，也没有公开的引擎构造 API。Go 的 internal 可见性按 **import path 前缀**判定，
因此只有 import path 落在 `github.com/RandomLemon/kei/` 之下的 module 才能组装宿主：

```text
module github.com/RandomLemon/arisu       -> use of internal package ... not allowed
module github.com/RandomLemon/kei/arisu   -> 可以
```

**不要**把 module path 改成 `github.com/RandomLemon/arisu`：`go build ./...` 会立刻失败。
这是在 kei 不公开引擎 API 的前提下的唯一可行形态，换来的代价是宿主代码是 `cmd/bot` 的副本。

### 1.2 与 kei 的同步契约

| 文件 | 与上游的关系 |
| --- | --- |
| `cmd/arisu/external.go` | 与 `../kei/cmd/bot/external.go` **逐字一致**（只多一段来源注释） |
| `cmd/arisu/main.go` | 与 `../kei/cmd/bot/main.go` 等价，差异只有三处：少空导入 `kei/adapters/feishu`（本仓库只接 OneBot 与 mock）；多空导入 `kei-plugin-agent`；`run(ctx, args)` 接收外部 context（信号处理留在 `main`） |

升级 kei 后同步这两个文件；`external.go` 的差异应保持为空，`main.go` 的差异应保持为上面三处。

## 2. 硬性规则

### 2.1 依赖与分层

- 只依赖 `github.com/RandomLemon/kei` 与 `github.com/RandomLemon/kei-plugin-agent`（本地走
  `replace ... => ../kei`、`=> ../kei-plugin-agent`）。**禁止新增第三方依赖**——需要新依赖时先在上游
  仓库解决，`go.mod` 里出现新的 `require` 即视为回归。
- 适配器与插件只能经注册表接入：新增平台/插件时只加空导入与配置，禁止在 `cmd/arisu` 里写
  `switch adapter`、`if platform == "onebot"` 之类的分支。
- 配置与密钥只经配置对象读取；禁止在代码里 `os.Getenv` 或读配置文件。
- 配置键的权威清单在上游文档；本仓库只在 `configs/config.yaml` 给示例，不定义新键。
  上游新增/改名的键要同步示例配置与 `README.md`。

### 2.2 运行时契约（继承 kei）

- 所有阻塞操作必须接收 `context.Context`，不得存在无超时等待。
- 错误必须返回，禁止用 panic 表达运行期失败；只有启动阶段不可恢复的错误可以让 `run` 返回 error。
- 所有 goroutine 必须有退出机制：`ctx` 取消后 `run` 必须收尾返回。
- 时间统一 UTC；日志用 `log/slog`，字段化输出，键名小写下划线，与 kei 核心保持一致。

### 2.3 环境

- `flake.nix` 只提供 `devShell`（go / gopls / golangci-lint / dlv / jq / curl / python3）与
  `formatter`；`.envrc` 走 direnv `use flake`。`GOTOOLCHAIN=local` 已固定。
- **不提供 `packages`/`checks`/`apps`**：构建依赖同级 kei 与 kei-plugin-agent 检出，而 flake 输入与
  path 字面量都逃不出 store（`path:../kei` 会被解析到 flake store 副本的 `../kei`，
  `builtins.path { path = ../kei; }` 在 pure 模式下直接报错）。二进制在 devShell 内用 `go build` 产出。
  与 `../kei-plugin-agent` 的 flake 保持同一种做法。

## 3. 质量门（合并前必须全绿）

在 devShell 内执行（`direnv allow .` 后直接跑，或 `nix develop --command ...`）：

```bash
gofmt -l .        # 必须无输出
go build ./...
go vet ./...
go test ./...
go test -race ./...   # 必过：e2e 里有多协程与定时器
```

- 公开符号必须有文档注释；注释与实现不符视为缺陷。
- 行为变更（默认配置、命令集合、启动参数、日志字段）必须同步更新 `README.md` 与
  `configs/config.yaml`。

## 4. 测试约定

- `cmd/arisu/e2e_test.go` 是唯一的测试文件：起真实引擎（临时配置 -> `adaptermgr` -> `pluginmgr`
  -> `engine.Run`），断言**可观察行为**（mock 适配器记录到的消息内容、发送目标、优雅退出）。
- LLM 用 `httptest.Server` 桩，禁止依赖真实网络与真实平台；端口用 `freeAddr` 现取，禁止写死。
- 禁止断言实现细节（wiring、字段拷贝、日志文案），禁止为测试引入第三方 mock 库。
- 单测覆盖不到的平台真机行为（OneBot 反向 WebSocket）由上游仓库的测试保证；
  本仓库的验证手段是 README「快速开始」的 mock 冒烟。

## 5. 项目结构

```text
cmd/arisu/main.go       入口：加载配置 -> 装配适配器/插件 -> 启动引擎 -> 优雅退出（无平台分支）
cmd/arisu/external.go   外部插件与外部适配器的 gRPC 通道装配（与 kei cmd/bot 逐字一致）
cmd/arisu/e2e_test.go   端到端测试（真实引擎 + mock 适配器 + LLM 桩 + 优雅退出）
configs/config.yaml     示例配置：mock/OneBot + echo/manage/agent
flake.nix               devShell（go 工具链）与 formatter
.envrc                  direnv：进入目录自动 `nix develop`
```

## 6. 工作流

- 改代码前先读上游对应领域文档与既有实现，沿用仓库既有模式；禁止并存第二套约定。
- 改配置键或插件行为 → 先改上游仓库，再在本仓库同步示例配置与文档。
- 设计歧义时以上游 `docs/` 为准；上游未覆盖时选最简单、可测试的方案。
