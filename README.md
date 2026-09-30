# arisu

基于 [kei](https://github.com/RandomLemon/kei) 框架的聊天机器人，内置
[kei-plugin-persona](https://github.com/RandomLemon/kei-plugin-persona)「LLM 人格代理」插件：
在群聊里按人格预设偶尔插话，私聊里只要对方开口就必回。（插件仓库、module path、插件名与
命令前缀统一为 `kei-plugin-persona` / `github.com/RandomLemon/kei-plugin-persona` / `persona`。）

- 平台：OneBot v11（QQ）、mock（本地联调）。适配器与插件全部经 kei 的注册表装配，
  `main.go` 内不出现任何平台名分支。
- 插件：`persona`（LLM 人格代理）、`echo`、`manage`。
- 工具链：Go 1.25+，由 `flake.nix` + direnv 提供（`nix develop`）；arisu 自身不引入任何
  直接第三方依赖，GORM 等间接依赖来自上游 kei 的存储后端。

## 目录结构

```text
main.go                 入口薄壳：flag + 信号 + 空导入 + 一次 kei.Run（装配全部委托 pkg/kei）
e2e_test.go             端到端测试：真实引擎 + mock 适配器 + LLM 桩
config.yaml             配置示例（mock/OneBot + echo/manage/persona）
flake.nix               devShell（go 工具链）与 formatter
.envrc                  direnv：进入目录自动 `nix develop`
```

源码与示例配置直接放在模块根目录：本仓库只有一个 `main` 包，多一层 `cmd/`/`configs/`
只增加路径噪音，`go build .` 产出的可执行文件即与 module path 末段同名（`arisu`）。

## 为什么入口在根目录（`main.go`）

kei 提供公开装配门面 `github.com/RandomLemon/kei/pkg/kei`：加载 YAML 配置、装配适配器与
插件、启动引擎、优雅退出，全部由一次
`kei.Run(ctx, kei.Options{ConfigFile: "config.yaml"})` 完成。因此本仓库的入口只是一个
薄壳——模块根目录的 `main.go` 与 kei 的 `cmd/bot/main.go` 同构，只做 flag、信号与空导入：

- 源码（`main.go`、`e2e_test.go`）与示例配置 `config.yaml` 直接放在模块根目录，没有
  `cmd/`、`configs/` 子目录：宿主只有一个 `main` 包，多一层目录换不来任何隔离。
- 与 kei cmd/bot 的差异只有四处：额外空导入 `kei-plugin-persona`；`run(ctx, args)` 接收调用方
  传入的 context（信号处理留在 `main`，测试可直接驱动全链路）；flags 集名、错误前缀与版本
  输出为 `arisu`；默认配置路径是根目录的 `config.yaml`（上游为 `configs/config.yaml`）。
- module path 是 `github.com/RandomLemon/arisu`（独立 module）。历史形态是
  `github.com/RandomLemon/kei/arisu`：kei 曾不公开装配 API，宿主必须让 import path 落在
  `github.com/RandomLemon/kei/` 之下才能访问 `internal/`；`pkg/kei` 门面公开后该约束消失，
  本仓库不再依赖任何 `internal/` 包，module path 随之回到顶层。
- 升级 kei 后同步 `main.go`；装配逻辑不在本仓库，不需要复制。

## 环境准备（Nix + direnv）

```bash
# arisu 依赖同级目录的 kei 与 kei-plugin-persona 检出（go.mod 的 replace 指向它们）
cd <父目录>
git clone https://github.com/RandomLemon/kei
git clone https://github.com/RandomLemon/kei-plugin-persona
git clone https://github.com/RandomLemon/arisu

cd arisu
direnv allow .          # 首次执行一次；之后 cd 进目录即自动加载 devShell
go version              # go1.26.7（来自 flake devShell，GOTOOLCHAIN=local）

# 或者显式进入 devShell
nix develop
nix develop --command go test -race ./...
```

`flake.nix` 只提供 `devShell` 与 `formatter`：构建依赖同级检出，而 flake 输入与 path 字面量
都逃不出 store，沙箱里拿不到 `../kei`，所以不提供 `packages`/`checks`（与 `../kei-plugin-persona`
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

# 2) 起机器人（把 persona 的 LLM 指向桩服务；密钥类配置一律走环境变量）
go build -o bin/arisu .
KEI_PLUGINS_PERSONA_LLM_BASE_URL=http://127.0.0.1:19100/v1 \
KEI_PLUGINS_PERSONA_LLM_MODEL=stub \
KEI_AUTH_ADMIN_USERS=u1 \
./bin/arisu -config config.yaml

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
  -d '{"text":"/persona status","user_id":"u1","channel_id":"g1"}'
# 群里会收到：persona: 开 · persona=xia(default) · 历史 0 条 · 近 1 小时回复 0/6 · 上次回复 从未 · llm 错误 0 · 已跳 0

# 6) 指标与健康检查
curl -sS 127.0.0.1:19090/healthz
curl -sS 127.0.0.1:19090/metrics | grep kei_events
```

群聊随机插话同样可测：`{"text":"...","user_id":"u2","channel_id":"g1"}` 注入若干条，
在 `random_probability` / `random_min_participants` / `random_cooldown` 允许时会插一句。
把 `plugins.persona.llm_*` 指向任意 OpenAI 兼容服务（OpenAI、DeepSeek、Ollama、vLLM 等）即可。

## 接入真实平台

### OneBot v11（QQ）

`config.yaml` 里 `qq-main` 用 `mode: reverse_ws`（缺省）：OneBot 实现主动连入
`ws://127.0.0.1:18082/onebot/v11/ws`。NapCat / Lagrange 里把反向 WebSocket 地址填成该地址即可；
`secret` 非空时两者都要带同一令牌。其他三种模式（`forward_http`/`reverse_http`/`forward_ws`）
只需改 `mode` 与 `api_url`/`ws_url`，键的语义见 kei 的
[docs/configuration.md](https://github.com/RandomLemon/kei/blob/main/docs/configuration.md)。

建议把 `plugins.persona.self_ids` 填成机器人自己的 QQ 号，这样只有 @ 到它才算寻址（否则任意 @ 都算）。

## 配置

配置键的权威说明在 kei 的 `docs/configuration.md`（框架配置）与 kei-plugin-persona 的
`docs/configuration.md`（`plugins.persona` 全量键表），本仓库只给一份可直接用的示例：
`config.yaml`。

顶层段：`log`、`metrics`、`storage`、`limits`、`auth`、`adapters`、`bots`、`plugins`。
全部键都可用环境变量覆盖（前缀 `KEI_`，`-`/`.`/`_` 等价、大小写不敏感）：

```bash
KEI_LOG_LEVEL=debug
KEI_STORAGE_TYPE=sqlite                        # memory（默认）| sqlite | mysql
KEI_STORAGE_DSN=/var/lib/arisu/storage.db      # sqlite 为文件路径，mysql 需带 parseTime=true
KEI_PLUGINS_PERSONA_LLM_API_KEY=sk-xxx        # 密钥只走环境变量，不写进文件
KEI_PLUGINS_PERSONA_LLM_MODEL=qwen2.5:7b
KEI_PLUGINS_PERSONA_SELF_IDS=123456789         # 机器人自己的 QQ；不填则任意 At 都算寻址
KEI_PLUGINS_PERSONA_PRIVATE_POLICY=whitelist
KEI_BOTS_QQ_MAIN_ENABLED=false                # 只停用这一个实例
KEI_ADAPTERS_QQ_ENABLED=false                 # 停用整个平台
```

`storage` 段可整体省略，缺省即 `memory`（重启丢历史与 `/persona` 运行时覆盖）；改 `type` 为
`sqlite`/`mysql` 让插件状态持久化，`cleanup_interval` 等其余键原样交给对应后端（全表见 kei 的
`docs/configuration.md` §12.5）。

`personas` / `bindings` / `llm_extra_headers` 这类结构建议写在 YAML 里：环境变量值会再走一层 YAML 解析，
写成内联字面量也能生效（如 `KEI_PLUGINS_PERSONA_BINDINGS='[{channel_id: "389372103", persona: default}]'`），
但多行 prompt 只能写成 `\n` 转义、引号要配对，可读性差。
`self_ids` / `group_list` / `private_list` 这类扁平列表键没有这个问题，裸标量或逗号分隔即可：
`KEI_PLUGINS_PERSONA_SELF_IDS=123456789`、`KEI_PLUGINS_PERSONA_GROUP_LIST="389372103,389372104"`。
覆盖是否命中可在启动日志确认（`log.level: debug`，文案 `环境变量覆盖配置`）。

## 命令

| 命令 | 说明 | 权限 |
| --- | --- | --- |
| `/echo <文字>` | 原样回显，用来确认链路通 | 所有人 |
| `/manage ping`、`/manage version`、`/manage adapters`、`/manage help` | 存活、版本、适配器绑定、子命令清单 | 所有人 |
| `/manage plugins` | 已注册插件 | 管理员（`manage.plugins_admin_only`） |
| `/manage admin` | 管理员权限自检（Auth 中间件演示） | 管理员 |
| `/manage status` | 主机 CPU/内存/磁盘/GPU 状态 | 管理员（始终） |
| `/persona status \| persona [name] \| on \| off \| reset` | 人格代理的查看与开关 | 管理员 |
| `/persona policy [group\|private off\|open\|whitelist\|blacklist]` | 名单策略 | 管理员 |
| `/persona list [group\|private [add\|del id]]` | 名单增删与查看 | 管理员 |

管理员名单来自 `auth.admin_users`（或 `KEI_AUTH_ADMIN_USERS`）；私聊命令也要求发送者在名单内。

## 测试

```bash
nix develop --command gofmt -l .        # 必须无输出
nix develop --command go build ./...
nix develop --command go vet ./...
nix develop --command go test ./...
nix develop --command go test -race ./...
```

`e2e_test.go` 起一个真实引擎（读临时配置 -> 装配 mock 适配器与全部进程内插件 ->
注入事件 -> persona 调 LLM 桩 -> 断言发出的消息与发送目标 -> 取消 ctx 断言优雅退出），
LLM 用 `httptest.Server` 桩，不依赖外部网络与真实平台。

## 部署

```bash
nix develop --command go build -trimpath -ldflags '-s -w' -o bin/arisu .
KEI_PLUGINS_PERSONA_LLM_API_KEY=sk-xxx ./bin/arisu -config /etc/arisu/config.yaml
```

- 构建需要 cgo：上游 kei 的 sqlite 存储后端经 `mattn/go-sqlite3` 编译，`flake.nix` 的 devShell
  已装 `gcc` 并固定 `CGO_ENABLED=1`；在 devShell 外构建请自行保证。
- 健康检查 `GET /metrics 所在 addr 的 /healthz`，指标 `GET /metrics`（Prometheus 文本格式）。
- 收到 SIGINT/SIGTERM 后优雅退出：排空已入队事件、停止适配器与插件。

## 已知限制

- 存储缺省是 kei 的内存实现（`storage.type: memory`），重启丢历史与 `/persona` 的运行时覆盖；
  改成 `storage.type: sqlite`（`dsn` 为文件路径）或 `mysql` 即持久化，或把自己的
  `bot.Storage` 实现经 `kei.Run(ctx, kei.Options{Storage: ...})` 注入。
- 同一平台多个 bot 实例时，主动发送必须在 `bot.Target.BotID` 指定实例名；回复当前会话由
  `TargetFromEvent` 自动带上，不受影响。
