package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RandomLemon/kei/adapters/mock"
	"github.com/RandomLemon/kei/pkg/bot"
)

// stubReply 是 LLM 桩固定返回的文本。
const stubReply = "在的，怎么了？"

// TestRunEndToEnd 走一遍真实装配链路：读配置 -> 装配 mock 适配器与进程内插件
// （含 agent）-> HTTP 控制面注入事件 -> agent 判决策、调 LLM 桩、发送 -> 优雅退出。
//
// 断言的都是可观察行为：机器人实际发出的消息内容与发送目标。
func TestRunEndToEnd(t *testing.T) {
	llm := newStubLLM(t, stubReply)
	defer llm.Close()

	ctrl := freeAddr(t)
	cfgPath := writeConfig(t, llm.URL+"/v1", ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx, []string{"-config", cfgPath}) }()

	waitHealthy(t, "http://"+ctrl+"/healthz", 10*time.Second)

	// 私聊必回，且新会话首条即回：状态经 Storage 懒加载，加载未完成时入站消息被暂存，
	// 完成后补判一次（见 kei-plugin-persona 的 docs/architecture.md §4.1/4.5）。
	inject(t, ctrl, map[string]any{"kind": "private", "text": "爱丽丝在吗", "user_id": "u1", "user_name": "张三"})

	rec := waitSend(t, ctrl, 10*time.Second, func(r mock.SendRecord) bool {
		return r.Request.Target.Kind == bot.MessagePrivate
	})
	if got := segText(t, rec.Request.Message); got != stubReply {
		t.Fatalf("私聊回复 = %q，期望 LLM 桩的 %q", got, stubReply)
	}
	if want := (bot.Target{Platform: "mock", BotID: "mock-main", UserID: "u1", Kind: bot.MessagePrivate}); rec.Request.Target != want {
		t.Fatalf("私聊发送目标 = %+v，期望 %+v", rec.Request.Target, want)
	}
	if rec.Request.Message.Kind != bot.MessagePrivate {
		t.Fatalf("私聊消息类型 = %q，期望 %q", rec.Request.Message.Kind, bot.MessagePrivate)
	}

	// /agent status：管理员命令（auth.admin_users 里的 u1），回复经 Reply 通道回到原会话。
	inject(t, ctrl, map[string]any{"text": "/agent status", "user_id": "u1", "user_name": "张三", "channel_id": "g1"})
	cmd := waitSend(t, ctrl, 10*time.Second, func(r mock.SendRecord) bool {
		return strings.Contains(segText(t, r.Request.Message), "persona=")
	})
	if got := segText(t, cmd.Request.Message); !strings.Contains(got, "persona=arisu") {
		t.Fatalf("/agent status 输出 = %q，期望含 persona=arisu", got)
	}
	if cmd.Request.Target.ChannelID != "g1" || cmd.Request.Target.Kind != bot.MessageGroup {
		t.Fatalf("命令回复目标 = %+v，期望回到 g1 群", cmd.Request.Target)
	}

	// 优雅退出：ctx 取消后 run 必须收尾并返回 nil。
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run 退出返回错误: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run 未在 15s 内优雅退出")
	}
}

// TestRunConfigError 配置读不到时必须直接返回错误（启动阶段不静默降级）。
func TestRunConfigError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-exist.yaml")
	if err := run(context.Background(), []string{"-config", path}); err == nil {
		t.Fatal("缺失配置文件应返回错误")
	}
}

// newStubLLM 起一个 OpenAI 兼容的最小桩服务：任何请求都返回固定 content。
func newStubLLM(t *testing.T, content string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("读取 LLM 请求体: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}}},
		})
	}))
	return srv
}

// writeConfig 写一份最小可用配置：只启用 mock 适配器与 agent 插件。
func writeConfig(t *testing.T, llmBaseURL, mockAddr string) string {
	t.Helper()
	cfg := fmt.Sprintf(`
log:
  level: error
  format: text

metrics:
  addr: ""

auth:
  admin_users: ["u1"]

bots:
  - name: mock-main
    adapter: mock
    platform: mock
    listen_addr: %s

plugins:
  agent:
    enabled: true
    personas:
      arisu:
        prompt: "群友，说话简短。"
    default_persona: arisu
    private_policy: open
    group_policy: open
    mention_min_interval: 0s
    random_enabled: false
    batch_window: 20ms
    batch_max_window: 40ms
    llm_base_url: %s
    llm_api_key: ""
    llm_model: stub
    llm_max_retries: 0
    llm_timeout: 5s
    reply_dedupe: false
    limits_max_concurrent: 1
`, mockAddr, llmBaseURL)

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("写配置: %v", err)
	}
	return path
}

// freeAddr 返回一个本机空闲的 127.0.0.1 地址。
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("申请空闲端口: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("释放空闲端口: %v", err)
	}
	return addr
}

// waitHealthy 轮询等待 mock 适配器控制面就绪。
func waitHealthy(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("mock 控制面 %s 未就绪: %v", url, lastErr)
}

// inject 经 mock 控制面注入一个事件。
func inject(t *testing.T, ctrl string, body map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("编码注入事件: %v", err)
	}
	resp, err := http.Post("http://"+ctrl+"/inject", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("注入事件: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("注入事件返回 %d: %s", resp.StatusCode, msg)
	}
}

// waitSend 轮询等待第一条满足条件的发送记录，超时则打印全部记录便于定位。
func waitSend(t *testing.T, ctrl string, timeout time.Duration, match func(mock.SendRecord) bool) mock.SendRecord {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var records []mock.SendRecord
	for time.Now().Before(deadline) {
		records = fetchSends(t, ctrl)
		for _, r := range records {
			if r.Request != nil && match(r) {
				return r
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, _ := json.MarshalIndent(records, "", "  ")
	t.Fatalf("等待发送超时，已有记录:\n%s", raw)
	return mock.SendRecord{}
}

// fetchSends 读取 mock 适配器记录的发送列表。
func fetchSends(t *testing.T, ctrl string) []mock.SendRecord {
	t.Helper()
	resp, err := http.Get("http://" + ctrl + "/sent")
	if err != nil {
		t.Fatalf("读取 /sent: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var records []mock.SendRecord
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		t.Fatalf("解析 /sent: %v", err)
	}
	return records
}

// segText 拼接消息里的全部文本段。
func segText(t *testing.T, msg *bot.Message) string {
	t.Helper()
	if msg == nil {
		t.Fatal("消息为空")
	}
	var b strings.Builder
	for _, seg := range msg.Segments {
		if seg.Type != bot.SegText {
			continue
		}
		if s, ok := seg.Data[bot.KeyText].(string); ok {
			b.WriteString(s)
		}
	}
	return b.String()
}
