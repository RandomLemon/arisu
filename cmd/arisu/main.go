// Command arisu 是基于 kei 框架的聊天机器人宿主程序。
//
// 职责与 kei 的 cmd/bot 一致：加载配置 -> 初始化日志/指标/存储 -> 注册编译期适配器
// 与插件（空导入）-> 按配置装配适配器 -> 收集已启用的编译期插件与外部插件 -> 启动
// 引擎 -> 收到信号后优雅退出。所有平台细节都在 adapters/ 或第三方适配器包内，本文件
// 不含任何平台名分支。
//
// 与 kei cmd/bot 的差异只有两点（见 README「为什么有 cmd/arisu」）：
//
//  1. 额外空导入 github.com/RandomLemon/kei-plugin-agent，把「LLM 人格代理」插件打进
//     本二进制，配置文件里 plugins.agent.enabled: true 即启用；
//  2. run 接收调用方传入的 context，而不是在 run 内部自己装信号处理器：main 负责
//     SIGINT/SIGTERM，测试可以直接驱动「启动 -> 收事件 -> 优雅退出」全链路。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/RandomLemon/kei/internal/adaptermgr"
	"github.com/RandomLemon/kei/internal/config"
	"github.com/RandomLemon/kei/internal/engine"
	"github.com/RandomLemon/kei/internal/metrics"
	"github.com/RandomLemon/kei/internal/storage"
	"github.com/RandomLemon/kei/pkg/bot"

	// 内置适配器与插件通过空导入注册到各自的注册表，是否启用由配置决定。
	// 第三方适配器（独立包或独立 module）以同样方式接入，无需改动本文件以外的代码。
	_ "github.com/RandomLemon/kei/adapters/mock"
	_ "github.com/RandomLemon/kei/adapters/onebot"
	_ "github.com/RandomLemon/kei/plugins/echo"
	_ "github.com/RandomLemon/kei/plugins/manage"

	// arisu 相对 kei cmd/bot 的唯一功能性差异：LLM 人格代理插件。
	_ "github.com/RandomLemon/kei-plugin-agent"
)

const httpTimeout = 15 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "arisu:", err)
		os.Exit(1)
	}
}

// run 装配并运行机器人，ctx 结束时优雅退出。
func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("arisu", flag.ContinueOnError)
	configPath := fs.String("config", "configs/config.yaml", "配置文件路径")
	showVersion := fs.Bool("version", false, "打印版本并退出")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Printf("arisu v%s (kei v%s)\n", bot.Version, bot.Version)
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	logger := newLogger(cfg.Log)
	slog.SetDefault(logger)

	store := storage.NewMemory()
	defer func() { _ = store.Close() }()

	registry := metrics.New()
	if cfg.Metrics.Addr != "" {
		if err := serveMetrics(ctx, cfg.Metrics.Addr, registry.Handler(), logger); err != nil {
			return err
		}
	}

	httpClient := &http.Client{Timeout: httpTimeout}

	clientTLS, err := clientTLSConfig(cfg.Grpc)
	if err != nil {
		return err
	}

	// 适配器一律经注册表装配：进程内适配器来自 pkg/bot 注册表，外部适配器由
	// adapters 段声明并走 gRPC 通道；本文件不出现任何平台名分支。
	bindings, err := adaptermgr.Build(ctx, cfg, adaptermgr.Deps{
		Logger:     logger,
		Storage:    store,
		HTTPClient: httpClient,
		TLS:        clientTLS,
		Recorder:   registry,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := bindings.Close(); err != nil {
			logger.Warn("adapter channel close failed", "error", err)
		}
	}()

	list := bindings.List()
	if len(list) == 0 {
		logger.Warn("没有启用的适配器，核心将只运行插件")
	}
	adapters := make([]engine.AdapterBinding, 0, len(list))
	for _, b := range list {
		adapters = append(adapters, engine.AdapterBinding{
			BotID:    b.BotID,
			Adapter:  b.Adapter,
			Metadata: b.Info.Metadata,
			External: b.Info.External,
		})
	}

	plugins := enabledPlugins(cfg, logger)

	external, err := setupExternal(cfg, logger, httpClient, bindings)
	if err != nil {
		return err
	}
	defer external.close()

	eng, err := engine.New(engine.Options{
		Config:          cfg,
		Logger:          logger,
		Metrics:         registry,
		Storage:         store,
		HTTPClient:      httpClient,
		Adapters:        adapters,
		Plugins:         plugins,
		ExternalPlugins: external.hook,
	})
	if err != nil {
		return err
	}

	logger.Info("starting arisu",
		"version", bot.Version,
		"config", *configPath,
		"bots", len(adapters),
		"plugins", len(plugins),
	)
	return eng.Run(ctx)
}

// newLogger 依据配置构造结构化日志器。
func newLogger(c config.LogConfig) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(c.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if strings.EqualFold(c.Format, "json") {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

// serveMetrics 在独立 HTTP 服务上暴露 Prometheus 指标，随 ctx 结束关闭。
func serveMetrics(ctx context.Context, addr string, handler http.Handler, logger *slog.Logger) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("metrics listen %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server stopped", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Warn("metrics shutdown incomplete", "error", err)
		}
	}()

	logger.Info("metrics listening", "addr", ln.Addr().String(), "path", "/metrics")
	return nil
}

// enabledPlugins 返回配置中启用、且已经通过空导入注册的编译期插件。
func enabledPlugins(cfg *config.Config, logger *slog.Logger) []bot.Plugin {
	registered := bot.RegisteredPlugins()
	seen := make(map[string]bool, len(registered))

	var out []bot.Plugin
	for _, p := range registered {
		meta := p.Metadata()
		seen[meta.Name] = true

		pc, ok := cfg.Plugins[meta.Name]
		if external, _ := pc.Settings["grpc_addr"].(string); ok && external != "" {
			logger.Info("plugin is configured as external, skipping in-process", "plugin", meta.Name)
			continue
		}
		if !ok || !pc.Enabled {
			logger.Info("plugin disabled", "plugin", meta.Name)
			continue
		}
		out = append(out, p)
	}
	for name, pc := range cfg.Plugins {
		if pc.Enabled && !seen[name] {
			if external, _ := pc.Settings["grpc_addr"].(string); external == "" {
				logger.Warn("enabled plugin is not registered", "plugin", name)
			}
		}
	}
	return out
}
