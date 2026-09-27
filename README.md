# arisu

基于 [kei](https://github.com/RandomLemon/kei) 框架的聊天机器人，内置
[kei-plugin-agent](https://github.com/RandomLemon/kei-plugin-agent)「LLM 人格代理」插件：
在群聊里按人格预设偶尔插话，私聊里只要对方开口就必回。

- 平台：OneBot v11（QQ）、mock（本地联调）。适配器与插件全部经 kei 的注册表装配，
  `cmd/arisu` 内不出现任何平台名分支。
- 插件：`agent`（LLM 人格代理）、`echo`、`manage`。
- 工具链：Go 1.25+，由 `flake.nix` + direnv 提供（`nix develop`），零第三方运行时依赖
  由 arisu 自身引入。

## 目录结构

```text
cmd/arisu/main.go       入口薄壳：flag + 信号 + 空导入 + 一次 kei.Run（装配全部委托 pkg/kei）
cmd/arisu/e2e_test.go   端到端测试：真实引擎 + mock 适配器 + LLM 桩
configs/config.yaml     配置示例（mock/OneBot + echo/manage/agent）
flake.nix devShell       go / gopls / golangci-lint / dlv / jq / curl / python3
.envrc                  direnv：进入目录自动 `nix develop`
```

## 为什么有 `cmd/arisu`

kei 提供公开装配门面 `github.com/RandomLemon/kei/pkg/kei`：加载 YAML 配置、装配适配器与
插件（含外部 gRPC 通道）、启动引擎、优雅退出，全部由一次
`kei.Run(ctx, kei.Options{ConfigFile: "configs/config.yaml"})` 完成。因此本仓库的入口只是
一个薄壳——`cmd/arisu/main.go` 与 kei 的 `cmd/bot/main.go` 同构，只做 flag、信号与空导入：

- 差异只有三处：额外空导入 `kei-plugin-agent`；`run(ctx, args)` 接收调用方传入的 context
  （信号处理留在 `main`，测试可直接驱动全链路）；flags 集名与错误前缀为 `arisu`。
- module path 是 `github.com/RandomLemon/arisu`（独立 module）。历史形态是
  `github.com/RandomLemon/kei/arisu`：kei 曾不公开装配 API，宿主必须让 import path 落在
  `github.com/RandomLemon/kei/` 之下才能访问 `internal/`；`pkg/kei` 门面公开后该约束消失，
  本仓库不再依赖任何 `internal/` 包，module path 随之回到顶层。
- 升级 kei 后同步 `cmd/arisu/main.go`；装配逻辑（含外部插件/外部适配器 gRPC 通道）不在本
  仓库，不需要复制。

## 环境准备（Nix + direnv）

```bash
# arisu 依赖同级目录的 kei 与 kei-plugin-agent 检出（go.mod 的 replace 指向它们）
cd <父目录>
git clone https://github.com/RandomLemon/kei
git clone https://github.com/RandomLemon/kei-plugin-agent
git clone https://github.com/RandomLemon/arisu

cd arisu
direnv allow .          # 首次执行一次；之后 cd 进目录即自动加载 devShell
go version              # go1.26.7（来自 flake devShell，GOTOOLCHAIN=local）

# 或者显式进入 devShell
nix develop
nix develop --command go test -race ./...
```

`flake.nix` 只提供 `devShell` 与 `formatter`：构建依赖同级检出，而 flake 输入与 path 字面量
都逃不出 store，沙箱里拿不到 `../kei`，所以不提供 `packages`/`checks`（与 `../kei-plugin-agent`
的做法一致）。二进制在 devShell 内用 go 产出。

## 快速开始（本地联调）

mock 适配器不连真实平台：事件经 HTTP 控制面注入，发送结果记录在内存里。

```bash
# 1) 起一个 OpenAI 兼容的 LLM 桩（任何 provider 都可以，这里用标准库起一个固定的）
python3 - <<'PY' &
import json
from http.server import BaseHTTPRequestHandler, HTTPServer
class H(BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get('content-length', 0)))
        body = json.dumps({"choices": [{"message": {"content": "在的，怎么了？"}}]}).encode()
        self.send_response(200); self.send_header('content-type', 'application/json')
        self.send_header('content-length', str(len(body))); self.end_headers()
        self.wfile.write(body)
    def log_message(self, *a): pass
HTTPServer(('127.0.0.1', 19100), H).serve_forever()
PY

# 2) 起机器人（把 agent 的 LLM 指向桩服务；密钥类配置一律走环境变量）
go build -o bin/arisu ./cmd/arisu
KEI_PLUGINS_AGENT_LLM_BASE_URL=http://127.0.0.1:19100/v1 \
KEI_PLUGINS_AGENT_LLM_MODEL=stub \
KEI_AUTH_ADMIN_USERS=u1 \
./bin/arisu -config configs/config.yaml

# 3) 另开一个终端：注入私聊消息（第一条只建立会话状态，第二条起触发回复）
curl -sS -XPOST 127.0.0.1:18080/inject -H 'content-type: application/json' \
  -d '{"kind":"private","text":"爱丽丝在吗","user_id":"u1","user_name":"张三"}'
sleep 1
curl -sS -XPOST 127.0.0.1:18080/inject -H 'content-type: application/json' \
  -d '{"kind":"private","text":"爱丽丝在吗","user_id":"u1","user_name":"张三"}'

# 4) 观察机器人实际发出的消息
curl -sS 127.0.0.1:18080/sent | jq -c '.[] | {target: .Request.Target, text: .Request.Message.Segments[0].Data.text}'
# {"target":{"Platform":"mock","BotID":"mock-main","ChannelID":"","UserID":"u1","Kind":"private"},"text":"在的，怎么了？"}

# 5) 管理员命令（auth.admin_users 里要有 u1）
curl -sS -XPOST 127.0.0.1:18080/inject -H 'content-type: application/json' \
  -d '{"text":"/agent status","user_id":"u1","channel_id":"g1"}'
# 群里会收到：agent: 开 · persona=arisu(default) · 历史 0 条 · ...

# 6) 指标与健康检查
curl -sS 127.0.0.1:19090/healthz
curl -sS 127.0.0.1:19090/metrics | grep kei_events
```

群聊随机插话同样可测：`{"text":"...","user_id":"u2","channel_id":"g1"}` 注入若干条，
在 `random_probability` / `random_min_participants` / `random_cooldown` 允许时会插一句。
把 `plugins.agent.llm_*` 指向任意 OpenAI 兼容服务（OpenAI、DeepSeek、Ollama、vLLM 等）即可。

## 接入真实平台

### OneBot v11（QQ）

`configs/config.yaml` 里 `qq-main` 用 `mode: reverse_ws`（缺省）：OneBot 实现主动连入
`ws://127.0.0.1:18082/onebot/ws`。NapCat / Lagrange 里把反向 WebSocket 地址填成该地址即可；
`secret` 非空时两者都要带同一令牌。其他三种模式（`forward_http`/`reverse_http`/`forward_ws`）
只需改 `mode` 与 `api_url`/`ws_url`，键的语义见 kei 的
[docs/configuration.md](https://github.com/RandomLemon/kei/blob/main/docs/configuration.md)。

建议把 `bots[].self_ids` 填成机器人自己的 QQ 号，这样只有 @ 到它才算寻址（否则任意 @ 都算）。

## 配置

配置键的权威说明在 kei 的 `docs/configuration.md`（框架配置）与 kei-plugin-agent 的
`docs/configuration.md`（`plugins.agent` 全量键表），本仓库只给一份可直接用的示例：
`configs/config.yaml`。

顶层段：`log`、`metrics`、`limits`、`auth`、`grpc`、`adapters`、`bots`、`plugins`。
全部键都可用环境变量覆盖（前缀 `KEI_`，`-`/`.`/`_` 等价、大小写不敏感）：

```bash
KEI_LOG_LEVEL=debug
KEI_PLUGINS_AGENT_LLM_API_KEY=sk-xxx          # 密钥只走环境变量，不写进文件
KEI_PLUGINS_AGENT_LLM_MODEL=qwen2.5:7b
KEI_PLUGINS_AGENT_PRIVATE_POLICY=whitelist
KEI_BOTS_QQ_MAIN_ENABLED=false                # 只停用这一个实例
KEI_ADAPTERS_QQ_ENABLED=false                 # 停用整个平台
```

`personas` / `bindings` / `group_list` / `private_list` 这类复合结构不支持环境变量覆盖，写在 YAML 里。

## 命令

| 命令 | 说明 | 权限 |
| --- | --- | --- |
| `/echo <文字>` | 原样回显，用来确认链路通 | 所有人 |
| `/ping`、`/version`、`/adapters` | 存活、版本、适配器绑定 | 所有人 |
| `/plugins` | 已注册插件 | 管理员（`manage.plugins_admin_only`） |
| `/agent status \| persona [name] \| on \| off \| reset` | 人格代理的查看与开关 | 管理员 |
| `/agent policy [group\|private off\|open\|whitelist\|blacklist]` | 名单策略 | 管理员 |
| `/agent list [group\|private [add\|del id]]` | 名单增删与查看 | 管理员 |

管理员名单来自 `auth.admin_users`（或 `KEI_AUTH_ADMIN_USERS`）；私聊命令也要求发送者在名单内。

## 测试

```bash
nix develop --command gofmt -l .        # 必须无输出
nix develop --command go build ./...
nix develop --command go vet ./...
nix develop --command go test ./...
nix develop --command go test -race ./...
```

`cmd/arisu/e2e_test.go` 起一个真实引擎（读临时配置 -> 装配 mock 适配器与全部进程内插件 ->
注入事件 -> agent 调 LLM 桩 -> 断言发出的消息与发送目标 -> 取消 ctx 断言优雅退出），
LLM 用 `httptest.Server` 桩，不依赖外部网络与真实平台。

## 部署

```bash
nix develop --command go build -trimpath -ldflags '-s -w' -o bin/arisu ./cmd/arisu
KEI_PLUGINS_AGENT_LLM_API_KEY=sk-xxx ./bin/arisu -config /etc/arisu/config.yaml
```

- 健康检查 `GET /metrics 所在 addr 的 /healthz`，指标 `GET /metrics`（Prometheus 文本格式）。
- 收到 SIGINT/SIGTERM 后优雅退出：排空已入队事件、停止适配器与插件。

## 已知限制

- `Storage` 是 kei 的内存实现，重启丢历史与 `/agent` 的运行时覆盖；要持久化就把自己的
  `bot.Storage` 实现（如 Redis/SQLite）经 `kei.Run(ctx, kei.Options{Storage: ...})` 注入。
- 同一平台多个 bot 实例时，主动发送必须在 `bot.Target.BotID` 指定实例名；回复当前会话由
  `TargetFromEvent` 自动带上，不受影响。
