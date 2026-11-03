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

	"github.com/kerolt/clashctl-go/internal/config"
	"github.com/kerolt/clashctl-go/internal/output"
	"github.com/kerolt/clashctl-go/internal/service"
	"github.com/kerolt/clashctl-go/internal/subscription"
)

// App 保存命令所需的依赖与终端输入输出。
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

// New 按当前环境构造 App。
func New(out, errOut io.Writer, input io.Reader) (*App, error) {
	// 固定使用 mihomo 作为内核。
	kernel := "mihomo"
	paths, err := config.FromEnvironment(kernel)
	if err != nil {
		return nil, err
	}
	loadDotEnv(paths.Home)

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

// Run 分发一条 clashctl 命令并返回执行错误。
func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.help()
	}
	if args[0] == "--home" {
		return fmt.Errorf("global options must be configured with CLASHCTL_HOME")
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
	case "self-upgrade":
		return a.selfUpgrade(ctx, arguments)
	case "version", "--version":
		return a.version()
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

// help 输出命令总览。
func (a *App) help() error {
	fmt.Fprint(a.Printer.Out, `
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
  upgrade               升级 mihomo 内核
  self-upgrade          升级 clashctl 自身
  version               查看 clashctl 版本

Global Options:
  -h, --help            显示帮助信息
  --version             显示版本信息
`)
	return nil
}

// on 启动代理服务，并按需输出终端代理设置。
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

// off 停止代理服务，并按需清除终端代理设置。
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

// stopService 停止受管服务，并处理残留的 TUN 进程。
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

// status 输出服务状态，并返回运行状态结果。
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

// writeProxyEnv 在 shell 输出模式下生成导出语句。
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

// writeProxyUnset 在 shell 输出模式下生成清除语句。
func (a *App) writeProxyUnset() error {
	for _, key := range []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY", "all_proxy", "ALL_PROXY", "no_proxy", "NO_PROXY"} {
		if a.ShellOutput {
			fmt.Fprintf(a.Printer.Out, "unset %s\n", key)
		}
	}
	return nil
}

// bindAddress 解析终端代理地址中使用的监听地址。
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

// localIP 查找供局域网访问使用的本地地址。
func localIP() string {
	connection, err := net.Dial("udp", "1.1.1.1:80")
	if err == nil {
		defer connection.Close()
		return strings.Split(connection.LocalAddr().String(), ":")[0]
	}
	return "127.0.0.1"
}

// shellQuote 为 POSIX shell 赋值语句引用一个值。
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// parseModeArgs 解析服务专用、环境专用与帮助三个共享选项。
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

// loadDotEnv 读取简单的键值对，不执行任何 shell 代码。
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
