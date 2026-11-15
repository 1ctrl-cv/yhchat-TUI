// Command yhchat-tui is a terminal user interface client for the Yunhu (云湖)
// social platform. Current version implements login (email/password and SMS
// verification code) plus the account overview home screen.
//
// Build for Windows and Linux:
//
//	go build -o yhchat-tui .
//	GOOS=linux GOARCH=amd64 go build -o yhchat-tui-linux .
//
// Usage:
//
//	yhchat-tui                 # start the TUI
//	yhchat-tui -version        # print version
//	yhchat-tui -config FILE    # use a custom config file
//	yhchat-tui -server URL     # point at another API server (debug/mock)
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"yhchat-tui/internal/api"
	"yhchat-tui/internal/config"
	"yhchat-tui/internal/logging"
	"yhchat-tui/internal/tui"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "打印版本号并退出")
		configPath  = flag.String("config", "", "配置文件路径（默认: ~/.yhchat-tui/config.json）")
		serverURL   = flag.String("server", "", "API 服务器地址（默认: "+api.DefaultBaseURL+"）")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("yhchat-tui %s\n", tui.Version)
		fmt.Printf("  commit: %s\n", tui.Commit)
		fmt.Printf("  built:  %s\n", tui.BuildTime)
		fmt.Printf("  go:     %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return
	}

	if initErr := logging.Init(os.Getenv("YHCHAT_DEBUG") == "1"); initErr != nil {
		fmt.Fprintf(os.Stderr, "日志提示: %v\n", initErr)
	} else {
		defer logging.Close()
		if logging.IsFallback() {
			fmt.Fprintf(os.Stderr, "日志提示: 主目录不可写，日志已改用临时文件 %s\n", logging.Path())
		}
		logging.Info("启动 yhchat-tui %s (commit %s, %s/%s, server=%s)",
			tui.Version, tui.Commit, runtime.GOOS, runtime.GOARCH, serverOrDefault(*serverURL))
	}

	server := *serverURL
	if server == "" {
		server = os.Getenv("YHCHAT_API_BASE")
	}

	store, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置提示: %v\n", err)
		if store == nil {
			os.Exit(1)
		}
	}

	client := api.New(store.Data.DeviceID)
	if server != "" {
		client.BaseURL = server
	}

	program := tea.NewProgram(tui.New(client, store), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, runErr := program.Run(); runErr != nil {
		logging.Error("程序运行失败: %v", runErr)
		fmt.Fprintf(os.Stderr, "程序运行失败: %v\n", runErr)
		os.Exit(1)
	}
	logging.Info("正常退出")
}

// serverOrDefault returns the configured API base URL for logging.
func serverOrDefault(configured string) string {
	if configured != "" {
		return configured
	}
	if env := os.Getenv("YHCHAT_API_BASE"); env != "" {
		return env
	}
	return api.DefaultBaseURL
}
