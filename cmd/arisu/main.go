// Command arisu 是基于 kei 框架的聊天机器人宿主程序。
//
// 职责只剩命令行参数、信号与空导入：装配逻辑全部在 kei 的公开门面 pkg/kei
// （加载配置 -> 装配适配器与插件 -> 启动引擎 -> 优雅退出），因此本文件与
// kei 的 cmd/bot/main.go 同构。所有平台细节都在 adapters/ 或第三方适配器包内，
// 本文件不含任何平台名分支。
//
// 与 kei cmd/bot 的差异只有三处（见 README「为什么有 cmd/arisu」）：
//
//  1. 额外空导入 github.com/RandomLemon/kei-plugin-agent，把「LLM 人格代理」插件打进
//     本二进制，配置文件里 plugins.agent.enabled: true 即启用；
//  2. run 接收调用方传入的 context，而不是在 run 内部自己装信号处理器：main 负责
//     SIGINT/SIGTERM，测试可以直接驱动「启动 -> 收事件 -> 优雅退出」全链路；
//  3. flag 集名与错误前缀用 arisu。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/RandomLemon/kei/pkg/bot"
	kei "github.com/RandomLemon/kei/pkg/kei"

	// 内置适配器与插件通过空导入注册到各自的注册表，是否启用由配置决定。
	// 第三方适配器（独立包或独立 module）以同样方式接入，无需改动本文件以外的代码。
	_ "github.com/RandomLemon/kei/adapters/mock"
	_ "github.com/RandomLemon/kei/adapters/onebot"
	_ "github.com/RandomLemon/kei/plugins/echo"
	_ "github.com/RandomLemon/kei/plugins/manage"

	// arisu 相对 kei cmd/bot 的唯一功能性差异：LLM 人格代理插件。
	_ "github.com/RandomLemon/kei-plugin-agent"
)

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

	return kei.Run(ctx, kei.Options{ConfigFile: *configPath})
}
