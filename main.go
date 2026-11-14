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
	"yhchat-tui/internal/tui"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "打印版本号并退出")
		configPath  = flag.String("config", "", "配置文件路径（默认: 用户配置目录/yhchat-tui/config.json）")
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

	program := tea.NewProgram(tui.New(client, store), tea.WithAltScreen())
	if _, runErr := program.Run(); runErr != nil {
		fmt.Fprintf(os.Stderr, "程序运行失败: %v\n", runErr)
		os.Exit(1)
	}
}
