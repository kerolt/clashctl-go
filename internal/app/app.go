// Package app 实现 clashctl 命令流程。
package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nelvko/clashctl-go/internal/config"
	"github.com/nelvko/clashctl-go/internal/output"
	"github.com/nelvko/clashctl-go/internal/service"
	"github.com/nelvko/clashctl-go/internal/subscription"
)

// App contains the command dependencies and terminal I/O used by clashctl.
type App struct {
	Paths         config.Paths
	Kernel        string
	KernelPath    string
	Service       *service.Manager
	Subscriptions *subscription.Manager
	Printer       *output.Printer
	In            io.Reader
	ShellOutput   bool
}

// New constructs an App from the current clashctl environment.
func New(out, errOut io.Writer, input io.Reader) (*App, error) {
	// 读取安装位置和内核名称。
	kernel := config.KernelFromEnvironment()
	paths, err := config.FromEnvironment(kernel)
	if err != nil {
		return nil, err
	}
	loadDotEnv(paths.Home)

	// .env 中的内核设置优先于默认值。
	if value := os.Getenv("CLASHCTL_KERNEL"); value != "" {
		kernel = value
		paths = config.NewPaths(paths.Home, kernel)
	}
	timeout := 5 * time.Second
	if value := os.Getenv("CLASHCTL_SUB_TIMEOUT"); value != "" {
		seconds, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid subscription timeout: %w", parseErr)
		}
		timeout = time.Duration(seconds) * time.Second
	}
	userAgent := os.Getenv("CLASHCTL_SUB_UA")
	if userAgent == "" {
		userAgent = "mihomo"
	}

	// 组装命令处理所需的全部依赖。
	return &App{
		Paths:         paths,
		Kernel:        kernel,
		KernelPath:    paths.KernelPath,
		Service:       service.New(paths, kernel),
		Subscriptions: subscription.NewManager(paths, paths.KernelPath, timeout, userAgent),
		Printer:       output.New(out, errOut),
		In:            input,
	}, nil
}

// Run dispatches one clashctl command and returns its execution error.
func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.help()
	}
	if args[0] == "--home" || args[0] == "--kernel" {
		return fmt.Errorf("global options must be configured with CLASHCTL_HOME and CLASHCTL_KERNEL")
	}
	command := args[0]
	arguments := args[1:]
	if command == "-h" || command == "--help" || command == "help" {
		return a.help()
	}
	switch command {
	case "on":
		return a.on(ctx, arguments)
	case "off":
		return a.off(ctx, arguments)
	case "status":
		return a.status(ctx, arguments)
	case "ui":
		return a.ui(ctx, arguments)
	case "sub":
		return a.sub(ctx, arguments)
	case "tun":
		return a.tun(ctx, arguments)
	case "mixin":
		return a.mixin(ctx, arguments)
	case "secret":
		return a.secret(ctx, arguments)
	case "log":
		return a.log(ctx, arguments)
	case "install":
		return a.install(ctx, arguments)
	case "upgrade":
		return a.upgrade(ctx, arguments)
	case "__env-on":
		a.ShellOutput = true
		return a.on(ctx, []string{"--env-only"})
	case "__env-off":
		a.ShellOutput = true
		return a.off(ctx, []string{"--env-only"})
	default:
		a.Printer.Fail("Unknown subcommand: clash" + command)
		a.Printer.Fail("Use 'clashctl help' for usage information.")
		return fmt.Errorf("unknown subcommand %q", command)
	}
}

// help prints the top-level command usage.
func (a *App) help() error {
	fmt.Fprintln(a.Printer.Out, `
Usage:
  clashctl COMMAND [OPTIONS]

Commands:
  on                    开启代理
  off                   关闭代理
  status                内核状态
  ui                    面板地址
  sub                   订阅管理
  tun                   Tun 模式
  mixin                 Mixin 配置
  secret                Web 密钥
  log                   查看日志
  install               首次安装
  upgrade               升级内核

Global Options:
  -h, --help            显示帮助信息

For more help on how to use clashctl, head to https://github.com/nelvko/clash-for-linux-install`)
	return nil
}

// on starts the proxy service and optionally emits terminal proxy settings.
func (a *App) on(ctx context.Context, args []string) error {
	serviceOnly, envOnly, help := parseModeArgs(args)
	if help {
		fmt.Fprintf(a.Printer.Out, `
clashctl on - 开启代理环境

Usage:
  clashctl on [OPTIONS]

Options:
  -s, --service-only 仅启动 %s 服务
  -e, --env-only     仅开启终端代理
  -h, --help         显示帮助信息

`, a.Kernel)
		return nil
	}
	if !envOnly {
		// 先保证内核服务处于运行状态。
		active, err := a.Service.IsActive(ctx)
		if err != nil {
			return err
		}
		if active {
			a.Printer.OK(a.Kernel + " 已运行")
		} else {
			if err := a.detectProxyPort(ctx); err != nil {
				return err
			}
			if err := a.Service.Start(ctx); err != nil {
				return err
			}
			active, err = a.Service.IsActive(ctx)
			if err != nil {
				return err
			}
			if !active {
				return fmt.Errorf("%s 启动失败", a.Kernel)
			}
			a.Printer.OK(a.Kernel + " 已启动")
		}
	}
	if !serviceOnly {
		// 服务运行后再处理当前终端的代理变量。
		active, err := a.Service.IsActive(ctx)
		if err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("%s 未运行，请使用 clashctl on 开启代理环境", a.Kernel)
		}
		if a.ShellOutput {
			return a.writeProxyEnv()
		}
		if err := a.writeProxyEnv(); err != nil {
			return err
		}
		a.Printer.OK("终端代理已启用")
	}
	return nil
}

// off stops the proxy service and optionally clears terminal proxy settings.
func (a *App) off(ctx context.Context, args []string) error {
	serviceOnly, envOnly, help := parseModeArgs(args)
	if help {
		fmt.Fprintf(a.Printer.Out, `
clashctl off - 关闭代理环境

Usage:
  clashctl off [OPTIONS]

Options:
  -s, --service-only 仅关闭 %s 服务
  -e, --env-only     仅关闭终端代理
  -h, --help         显示帮助信息

`, a.Kernel)
		return nil
	}
	if !envOnly {
		if err := a.stopService(ctx); err != nil {
			return err
		}
		a.Printer.OK(a.Kernel + " 已停止")
	}
	if !serviceOnly {
		if a.ShellOutput {
			return a.writeProxyUnset()
		}
		a.writeProxyUnset()
		a.Printer.OK("终端代理已关闭")
	}
	return nil
}

// stopService stops the managed service and handles a remaining TUN process.
func (a *App) stopService(ctx context.Context) error {
	active, err := a.Service.IsActive(ctx)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	if err := a.Service.Stop(ctx); err != nil {
		return err
	}
	active, err = a.Service.IsActive(ctx)
	if err != nil {
		return err
	}
	if active {
		tun, tunErr := a.tunStatus(ctx)
		if tunErr != nil {
			return tunErr
		}
		if tun {
			if err := a.Service.SudoStop(ctx); err != nil {
				return fmt.Errorf("stop tun service: %w", err)
			}
		}
	}
	active, err = a.Service.IsActive(ctx)
	if err != nil {
		return err
	}
	if active {
		return fmt.Errorf("%s 停止失败", a.Kernel)
	}
	return nil
}

// status prints the service status and returns its active-state result.
func (a *App) status(ctx context.Context, args []string) error {
	statusErr := a.Service.Status(ctx, a.Printer.Out, args...)
	active, err := a.Service.IsActive(ctx)
	if err != nil {
		return err
	}
	if !active && statusErr == nil {
		return fmt.Errorf("%s is not active", a.Kernel)
	}
	return statusErr
}

// writeProxyEnv writes shell export statements when shell output is enabled.
func (a *App) writeProxyEnv() error {
	// 读取端口、认证信息和绑定地址。
	mixedPort, err := config.Value(a.Paths.RuntimeConfig, "mixed-port")
	if err != nil {
		return err
	}
	httpPort, err := config.Value(a.Paths.RuntimeConfig, "port")
	if err != nil {
		return err
	}
	socksPort, err := config.Value(a.Paths.RuntimeConfig, "socks-port")
	if err != nil {
		return err
	}
	authValues, err := config.Values(a.Paths.RuntimeConfig, "authentication")
	if err != nil {
		return err
	}
	auth := ""
	if len(authValues) > 0 && authValues[0] != "" {
		auth = authValues[0] + "@"
	}
	bindAddress, err := a.bindAddress()
	if err != nil {
		return err
	}
	if httpPort == "" {
		httpPort = mixedPort
	}
	if socksPort == "" {
		socksPort = mixedPort
	}
	env := map[string]string{
		"http_proxy":  "http://" + auth + bindAddress + ":" + httpPort,
		"HTTP_PROXY":  "http://" + auth + bindAddress + ":" + httpPort,
		"https_proxy": "http://" + auth + bindAddress + ":" + httpPort,
		"HTTPS_PROXY": "http://" + auth + bindAddress + ":" + httpPort,
		"all_proxy":   "socks5h://" + auth + bindAddress + ":" + socksPort,
		"ALL_PROXY":   "socks5h://" + auth + bindAddress + ":" + socksPort,
		"no_proxy":    "localhost,127.0.0.1,::1",
		"NO_PROXY":    "localhost,127.0.0.1,::1",
	}
	for _, key := range []string{"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY", "all_proxy", "ALL_PROXY", "no_proxy", "NO_PROXY"} {
		if a.ShellOutput {
			fmt.Fprintf(a.Printer.Out, "export %s=%s\n", key, shellQuote(env[key]))
		}
	}
	return nil
}

// writeProxyUnset writes shell unset statements when shell output is enabled.
func (a *App) writeProxyUnset() error {
	for _, key := range []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY", "all_proxy", "ALL_PROXY", "no_proxy", "NO_PROXY"} {
		if a.ShellOutput {
			fmt.Fprintf(a.Printer.Out, "unset %s\n", key)
		}
	}
	return nil
}

// bindAddress resolves the address used in terminal proxy URLs.
func (a *App) bindAddress() (string, error) {
	bindAddress, err := config.Value(a.Paths.RuntimeConfig, "bind-address")
	if err != nil {
		return "", err
	}
	allowLan, err := config.Value(a.Paths.RuntimeConfig, "allow-lan")
	if err != nil {
		return "", err
	}
	if allowLan != "true" {
		return "127.0.0.1", nil
	}
	if bindAddress == "*" || bindAddress == "" {
		return localIP(), nil
	}
	return bindAddress, nil
}

// localIP finds the local address used for LAN proxy access.
func localIP() string {
	connection, err := net.Dial("udp", "1.1.1.1:80")
	if err == nil {
		defer connection.Close()
		return strings.Split(connection.LocalAddr().String(), ":")[0]
	}
	return "127.0.0.1"
}

// shellQuote quotes a value for use in a POSIX shell assignment.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// parseModeArgs parses the shared service-only, environment-only, and help flags.
func parseModeArgs(args []string) (serviceOnly, envOnly, help bool) {
	for _, arg := range args {
		switch arg {
		case "-s", "--service-only":
			serviceOnly = true
		case "-e", "--env-only":
			envOnly = true
		case "-h", "--help":
			help = true
		}
	}
	return
}

// loadDotEnv loads simple KEY=VALUE entries without evaluating shell code.
func loadDotEnv(home string) {
	data, err := os.ReadFile(filepath.Join(home, ".env"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if os.Getenv(parts[0]) == "" {
			os.Setenv(parts[0], strings.Trim(parts[1], "\"'"))
		}
	}
}
