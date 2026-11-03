package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/nelvko/clashctl-go/internal/config"
	"github.com/nelvko/clashctl-go/internal/process"
	"github.com/nelvko/clashctl-go/internal/subscription"
	"gopkg.in/yaml.v3"
)

// tun handles TUN status queries and the on/off subcommands.
func (a *App) tun(ctx context.Context, args []string) error {
	if len(args) == 0 {
		enabled, err := a.tunStatus(ctx)
		if err != nil {
			return err
		}
		if enabled {
			a.Printer.OK("Tun 状态：启用")
			return nil
		}
		a.Printer.Fail("Tun 状态：关闭")
		return fmt.Errorf("tun is disabled")
	}
	if args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(a.Printer.Out, `
- 查看 Tun 状态
  clashctl tun

- 开启 Tun 模式
  clashctl tun on

- 关闭 Tun 模式
  clashctl tun off
`)
		return nil
	}
	switch args[0] {
	case "on":
		return a.tunOn(ctx)
	case "off":
		return a.tunOff(ctx)
	default:
		return a.tun(ctx, nil)
	}
}

// tunStatus reports whether the configured TUN device exists.
func (a *App) tunStatus(ctx context.Context) (bool, error) {
	device, err := config.Value(a.Paths.RuntimeConfig, "tun", "device")
	if err != nil {
		return false, err
	}
	return a.Service.TunStatus(ctx, device)
}

// tunOn enables TUN mode, rebuilds the runtime configuration, and starts it.
func (a *App) tunOn(ctx context.Context) error {
	// 先停止普通服务，再写入 TUN 配置。
	active, err := a.tunStatus(ctx)
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	if err := a.Service.Stop(ctx); err != nil {
		return err
	}
	if err := config.SetBoolPath(a.Paths.MixinConfig, []string{"tun", "enable"}, true); err != nil {
		return err
	}
	if err := config.Merge(ctx, a.Paths, a.KernelPath); err != nil {
		return err
	}
	if err := a.Service.SudoStart(ctx); err != nil {
		return fmt.Errorf("start tun service: %w", err)
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	<-timer.C
	active, err = a.tunStatus(ctx)
	if err != nil {
		return err
	}
	if active {
		a.Printer.OK("Tun 模式已开启")
		return nil
	}
	if a.Kernel == "mihomo" {
		// mihomo 首次启用失败时，按原脚本关闭 auto-redirect 后重试。
		if err := config.SetBoolPath(a.Paths.MixinConfig, []string{"tun", "auto-redirect"}, false); err != nil {
			return err
		}
		if err := config.Merge(ctx, a.Paths, a.KernelPath); err != nil {
			return err
		}
		if err := a.Service.SudoStop(ctx); err != nil {
			return err
		}
		if err := a.Service.SudoStart(ctx); err != nil {
			return err
		}
		timer.Reset(time.Second)
		<-timer.C
		active, err = a.tunStatus(ctx)
		if err != nil {
			return err
		}
		if active {
			a.Printer.OK("Tun 模式已开启")
			return nil
		}
	}
	return fmt.Errorf("tun 模式开启失败，请检查代理内核日志")
}

// tunOff disables TUN mode and restarts the normal proxy service.
func (a *App) tunOff(ctx context.Context) error {
	active, err := a.tunStatus(ctx)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	if err := a.Service.SudoStop(ctx); err != nil {
		return err
	}
	serviceActive, err := a.Service.IsActive(ctx)
	if err != nil {
		return err
	}
	if serviceActive {
		return fmt.Errorf("tun 模式关闭失败")
	}
	if err := config.SetBoolPath(a.Paths.MixinConfig, []string{"tun", "enable"}, false); err != nil {
		return err
	}
	if err := config.Merge(ctx, a.Paths, a.KernelPath); err != nil {
		return err
	}
	if err := a.Service.Start(ctx); err != nil {
		return err
	}
	active, err = a.tunStatus(ctx)
	if err != nil {
		return err
	}
	if !active {
		a.Printer.OK("Tun 模式已关闭")
		return nil
	}
	return fmt.Errorf("tun 模式关闭失败")
}

// mixin opens, displays, or applies the Mixin configuration.
func (a *App) mixin(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintf(a.Printer.Out, `
- 查看 Mixin 配置：%s
  clashctl mixin

- 编辑 Mixin 配置
  clashctl mixin -e

- 查看原始订阅配置：%s
  clashctl mixin -c

- 查看运行时配置：%s
  clashctl mixin -r

`, a.Paths.MixinConfig, a.Paths.BaseConfig, a.Paths.RuntimeConfig)
		return nil
	}
	path := a.Paths.MixinConfig
	if len(args) > 0 {
		switch args[0] {
		case "-e":
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vim"
			}
			if err := runInteractive(ctx, editor, path); err != nil {
				return err
			}
			if err := a.mergeAndRestart(ctx); err != nil {
				return err
			}
			a.Printer.OK("配置更新成功，已重启生效")
			return nil
		case "-r":
			path = a.Paths.RuntimeConfig
		case "-c":
			path = a.Paths.BaseConfig
		}
	}
	return runInteractive(ctx, "less", path)
}

// secret displays or updates the Web control secret.
func (a *App) secret(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(a.Printer.Out, `
- 查看 Web 密钥
  clashctl secret

- 修改 Web 密钥
  clashctl secret <new_secret>
`)
		return nil
	}
	if len(args) == 0 {
		secret, err := config.Value(a.Paths.RuntimeConfig, "secret")
		if err != nil {
			return err
		}
		a.Printer.OK("Web 访问密钥：" + secret)
		return nil
	}
	if len(args) != 1 {
		return fmt.Errorf("参数错误，请使用 -h 查看帮助")
	}
	if err := config.SetString(a.Paths.MixinConfig, "secret", args[0]); err != nil {
		return err
	}
	if err := a.mergeAndRestart(ctx); err != nil {
		return err
	}
	a.Printer.OK("密钥更新成功，已重启生效")
	return nil
}

// mergeAndRestart rebuilds the runtime configuration and restarts the matching service mode.
func (a *App) mergeAndRestart(ctx context.Context) error {
	// 合并前记录 TUN 状态，合并后依据配置选择启动方式。
	tunActive, err := a.tunStatus(ctx)
	if err != nil {
		return err
	}
	if err := config.Merge(ctx, a.Paths, a.KernelPath); err != nil {
		return err
	}
	tunEnabled, err := config.Bool(a.Paths.RuntimeConfig, "tun", "enable")
	if err != nil {
		tunEnabled = false
	}
	if tunActive {
		if err := a.Service.SudoStop(ctx); err != nil {
			return err
		}
		active, activeErr := a.Service.IsActive(ctx)
		if activeErr != nil {
			return activeErr
		}
		if active {
			return fmt.Errorf("请先关闭 Tun 模式")
		}
	} else if err := a.Service.Stop(ctx); err != nil {
		return err
	}
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	if tunEnabled {
		if err := a.Service.SudoStart(ctx); err != nil {
			return err
		}
		timer.Reset(time.Second)
		<-timer.C
		active, activeErr := a.tunStatus(ctx)
		if activeErr != nil {
			return activeErr
		}
		if !active {
			return fmt.Errorf("Tun 模式重启失败，请检查代理内核日志")
		}
		return nil
	}
	return a.Service.Start(ctx)
}

// ui starts the service when needed and prints Web control panel addresses.
func (a *App) ui(ctx context.Context, _ []string) error {
	extIP, extPort, err := a.externalAddress(ctx)
	if err != nil {
		return err
	}
	active, err := a.Service.IsActive(ctx)
	if err != nil {
		return err
	}
	if !active {
		if err := a.Service.Start(ctx); err != nil {
			return err
		}
		active, err = a.Service.IsActive(ctx)
		if err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("无法启动服务，请检查日志")
		}
	}
	publicIP := "公网"
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, "https://api64.ipify.org", nil)
	if requestErr == nil {
		response, responseErr := client.Do(request)
		if responseErr == nil {
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr == nil && response.StatusCode >= 200 && response.StatusCode < 300 && len(body) > 0 {
				publicIP = string(body)
			}
		}
	}
	localAddress := "http://" + extIP + ":" + strconv.Itoa(extPort) + "/ui"
	publicAddress := "http://" + publicIP + ":" + strconv.Itoa(extPort) + "/ui"
	fmt.Fprintf(a.Printer.Out, `
╔═══════════════════════════════════════════════╗
║                Web 控制台                     ║
║═══════════════════════════════════════════════║
║                                               ║
║     🔓 注意放行端口：%-5d                    ║
║     🏠 内网：%-31s  ║
║     🌏 公网：%-31s  ║
║     ☁️  公共：http://board.zash.run.place       ║
║                                               ║
╚═══════════════════════════════════════════════╝

`, extPort, localAddress, publicAddress)
	return nil
}

// externalAddress reads and, when required, repairs the external controller address.
func (a *App) externalAddress(ctx context.Context) (string, int, error) {
	address, err := config.Value(a.Paths.RuntimeConfig, "external-controller")
	if err != nil {
		return "", 0, err
	}
	extIP, portText, splitErr := net.SplitHostPort(address)
	if splitErr != nil {
		parts := strings.Split(address, ":")
		if len(parts) != 2 {
			return "", 0, fmt.Errorf("invalid external controller address %q", address)
		}
		extIP, portText = parts[0], parts[1]
	}
	extPort, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, err
	}
	active, err := a.Service.IsActive(ctx)
	if err != nil {
		return "", 0, err
	}
	if !active && portUsed(extPort) {
		newPort, portErr := randomPort()
		if portErr != nil {
			return "", 0, portErr
		}
		if err := config.SetString(a.Paths.MixinConfig, "external-controller", extIP+":"+strconv.Itoa(newPort)); err != nil {
			return "", 0, err
		}
		extPort = newPort
		if err := config.Merge(ctx, a.Paths, a.KernelPath); err != nil {
			return "", 0, err
		}
	}
	if extIP == "0.0.0.0" {
		extIP = localIP()
	}
	return extIP, extPort, nil
}

// log prints service logs using the detected service manager.
func (a *App) log(ctx context.Context, args []string) error {
	return a.Service.Log(ctx, a.Printer.Out, args...)
}

// upgrade requests a kernel upgrade through the local external controller.
func (a *App) upgrade(ctx context.Context, args []string) error {
	channel := ""
	verbose := false
	for _, arg := range args {
		switch arg {
		case "-v", "--verbose":
			verbose = true
		case "-r", "--release":
			channel = "release"
		case "-a", "--alpha":
			channel = "alpha"
		case "-h", "--help":
			fmt.Fprintln(a.Printer.Out, "Usage:\n  clashctl upgrade [OPTIONS]")
			return nil
		}
	}
	extIP, extPort, err := a.externalAddress(ctx)
	if err != nil {
		return err
	}
	active, err := a.Service.IsActive(ctx)
	if err != nil {
		return err
	}
	if !active {
		if err := a.Service.Start(ctx); err != nil {
			return err
		}
	}
	a.Printer.OKWithEmoji("⏳", "请求内核升级...")
	var followCancel context.CancelFunc
	if verbose {
		followContext, cancel := context.WithCancel(ctx)
		followCancel = cancel
		go func() {
			_ = a.Service.FollowLog(followContext, a.Printer.Out)
		}()
	}
	if followCancel != nil {
		defer followCancel()
	}
	secret, err := config.Value(a.Paths.RuntimeConfig, "secret")
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("http://%s:%d/upgrade?channel=%s", extIP, extPort, url.QueryEscape(channel))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("request kernel upgrade: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	text := string(body)
	if strings.Contains(text, `"status":"ok"`) {
		a.Printer.OK("内核升级成功")
		return nil
	}
	if strings.Contains(text, "already using latest version") {
		a.Printer.OK("已是最新版本")
		return nil
	}
	return fmt.Errorf("内核升级失败，请检查网络或稍后重试")
}

// sub dispatches subscription management subcommands.
func (a *App) sub(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "ls" || args[0] == "list" {
		return a.subList()
	}
	switch args[0] {
	case "-h", "--help":
		return a.subHelp()
	case "add":
		return a.subAdd(ctx, args[1:])
	case "del", "delete":
		return a.subDelete(args[1:])
	case "use":
		return a.subUse(ctx, args[1:])
	case "update":
		return a.subUpdate(ctx, args[1:])
	case "log":
		return commandOutputTo(ctx, a.Printer.Out, "tail", append(args[1:], a.Paths.ProfilesLog)...)
	default:
		return a.subHelp()
	}
}

// subHelp prints subscription command usage.
func (a *App) subHelp() error {
	fmt.Fprint(a.Printer.Out, `
clashctl sub - 订阅管理工具

Usage:
  clashctl sub COMMAND [OPTIONS]

Commands:
  add <url>       添加订阅
  ls              查看订阅
  del <id>        删除订阅
  use <id>        使用订阅
  update [id]     更新订阅
  log             订阅日志
`)
	return nil
}

// subList prints the stored subscription metadata.
func (a *App) subList() error {
	metadata, err := a.Subscriptions.LoadMetadata()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = a.Printer.Out.Write(data)
	return err
}

// subAdd downloads, validates, and stores a subscription profile.
func (a *App) subAdd(ctx context.Context, args []string) error {
	// 解析订阅参数并保留原有的交互输入方式。
	useAfter := false
	sourceURL := ""
	for _, arg := range args {
		switch arg {
		case "-u", "--use":
			useAfter = true
		case "-h", "--help":
			fmt.Fprintln(a.Printer.Out, "clashctl sub add [-u|--use] <url>")
			return nil
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("未知选项：%s", arg)
			}
			if sourceURL != "" {
				return fmt.Errorf("仅支持一个订阅链接")
			}
			sourceURL = arg
		}
	}
	if sourceURL == "" {
		fmt.Fprint(a.Printer.Out, "请输入要添加的订阅链接：")
		reader := bufio.NewReader(a.In)
		value, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		sourceURL = strings.TrimSpace(value)
	}
	if sourceURL == "" {
		return fmt.Errorf("订阅链接不能为空")
	}
	profile, err := a.Subscriptions.Add(ctx, sourceURL)
	if err != nil {
		return err
	}
	a.Printer.OKWithEmoji("🎉", fmt.Sprintf("订阅已添加：[%d] %s", profile.ID, profile.URL))
	if useAfter {
		return a.useProfile(ctx, profile.ID)
	}
	return nil
}

// subDelete removes an unused subscription profile by id.
func (a *App) subDelete(args []string) error {
	id, err := parseID(args)
	if err != nil {
		return err
	}
	profile, err := a.Subscriptions.Delete(id)
	if err != nil {
		return err
	}
	a.Printer.OKWithEmoji("🎉", fmt.Sprintf("订阅已删除：[%d] %s", profile.ID, profile.URL))
	return nil
}

// subUse selects a subscription profile and applies it.
func (a *App) subUse(ctx context.Context, args []string) error {
	id, err := a.subscriptionID(args)
	if err != nil {
		return err
	}
	return a.useProfile(ctx, id)
}

// subscriptionID resolves a profile id from arguments or interactive input.
func (a *App) subscriptionID(args []string) (int, error) {
	if len(args) > 0 {
		return parseID(args)
	}
	metadata, err := a.Subscriptions.LoadMetadata()
	if err != nil {
		return 0, err
	}
	data, marshalErr := yaml.Marshal(metadata)
	if marshalErr != nil {
		return 0, marshalErr
	}
	a.Printer.Out.Write(data)
	fmt.Fprint(a.Printer.Out, "请输入要使用的订阅 id：")
	reader := bufio.NewReader(a.In)
	value, readErr := reader.ReadString('\n')
	if readErr != nil && readErr != io.EOF {
		return 0, readErr
	}
	return strconv.Atoi(strings.TrimSpace(value))
}

// useProfile copies a profile into the base configuration and activates it.
func (a *App) useProfile(ctx context.Context, id int) error {
	metadata, err := a.Subscriptions.LoadMetadata()
	if err != nil {
		return err
	}
	var profile subscription.Profile
	found := false
	for _, item := range metadata.Profiles {
		if item.ID == id {
			profile = item
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("订阅 id 不存在，请检查")
	}
	if err := config.Copy(profile.Path, a.Paths.BaseConfig); err != nil {
		return fmt.Errorf("apply subscription profile: %w", err)
	}
	if err := a.mergeAndRestart(ctx); err != nil {
		return err
	}
	metadata.Use = id
	if err := a.Subscriptions.SaveMetadata(metadata); err != nil {
		return err
	}
	a.Printer.OKWithEmoji("🔥", "订阅已生效")
	return nil
}

// subUpdate downloads and validates an existing subscription profile.
func (a *App) subUpdate(ctx context.Context, args []string) error {
	// 先处理自动更新和转换选项，再确定目标订阅。
	convert := false
	idArgs := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--convert" {
			convert = true
			continue
		}
		if arg == "--auto" {
			return a.enableAutoUpdate(ctx)
		}
		idArgs = append(idArgs, arg)
	}
	id := 0
	var err error
	if len(idArgs) > 0 {
		id, err = parseID(idArgs)
	} else {
		metadata, loadErr := a.Subscriptions.LoadMetadata()
		if loadErr != nil {
			return loadErr
		}
		id = metadata.Use
		if id == 0 {
			id = 1
		}
	}
	if err != nil {
		return err
	}
	profile, err := a.Subscriptions.Update(ctx, id, convert)
	if err != nil {
		return err
	}
	a.Printer.OKWithEmoji("✅", fmt.Sprintf("订阅已更新：[%d] %s", profile.ID, profile.URL))
	metadata, err := a.Subscriptions.LoadMetadata()
	if err != nil {
		return err
	}
	if metadata.Use == id {
		return a.useProfile(ctx, id)
	}
	return nil
}

// enableAutoUpdate installs the existing clashctl cron schedule.
func (a *App) enableAutoUpdate(ctx context.Context) error {
	if _, err := exec.LookPath("crontab"); err != nil {
		return fmt.Errorf("未检测到 crontab 命令，请先安装 cron 服务")
	}
	current, err := process.Output(ctx, "crontab", "-l")
	if err != nil {
		current = nil
	}
	if strings.Contains(string(current), "# clashctl-auto-update") {
		a.Printer.OK("已设置定时更新订阅")
		return nil
	}
	path, err := exec.LookPath("clashctl")
	if err != nil {
		path = "clashctl"
	}
	content := strings.TrimRight(string(current), "\n") + "\n0 0 */2 * * " + path + " sub update # clashctl-auto-update\n"
	command := exec.CommandContext(ctx, "crontab", "-")
	command.Stdin = strings.NewReader(content)
	if output, runErr := command.CombinedOutput(); runErr != nil {
		return fmt.Errorf("set subscription schedule: %w: %s", runErr, strings.TrimSpace(string(output)))
	}
	a.Printer.OK("已设置定时更新订阅")
	return nil
}

// parseID parses exactly one subscription id.
func parseID(args []string) (int, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("需要一个订阅 id")
	}
	id, err := strconv.Atoi(args[0])
	if err != nil {
		return 0, fmt.Errorf("订阅 id 无效：%w", err)
	}
	return id, nil
}

// runInteractive runs a terminal command with the current process streams.
func runInteractive(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// commandOutputTo runs a command and sends both output streams to writer.
func commandOutputTo(ctx context.Context, writer io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = writer
	command.Stderr = writer
	return command.Run()
}

// detectProxyPort repairs occupied proxy ports before starting the kernel.
func (a *App) detectProxyPort(ctx context.Context) error {
	// 服务未运行时检查每个代理端口，冲突端口写入 Mixin。
	active, err := a.Service.IsActive(ctx)
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	values := []struct {
		key  string
		port string
	}{
		{"mixed-port", ""},
		{"port", ""},
		{"socks-port", ""},
	}
	for index := range values {
		values[index].port, err = config.Value(a.Paths.RuntimeConfig, values[index].key)
		if err != nil {
			return err
		}
	}
	for _, item := range values {
		if item.port == "" {
			continue
		}
		port, parseErr := strconv.Atoi(item.port)
		if parseErr != nil || !portUsed(port) {
			continue
		}
		newPort, portErr := randomPort()
		if portErr != nil {
			return portErr
		}
		a.Printer.FailWithEmoji("🎯", fmt.Sprintf("端口冲突：[%s] %s 🎲 随机分配 %d", item.key, item.port, newPort))
		if err := config.SetInt(a.Paths.MixinConfig, []string{item.key}, newPort); err != nil {
			return err
		}
	}
	return config.Merge(ctx, a.Paths, a.KernelPath)
}

// portUsed reports whether a TCP port is unavailable for binding.
func portUsed(port int) bool {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return true
	}
	listener.Close()
	return false
}

// randomPort selects an available non-privileged TCP port.
func randomPort() (int, error) {
	for attempt := 0; attempt < 100; attempt++ {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			return 0, err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		if port >= 1024 {
			return port, nil
		}
	}
	return 0, fmt.Errorf("未找到可用的代理端口")
}
