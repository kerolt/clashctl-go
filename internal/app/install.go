package app

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nelvko/clashctl-go/internal/config"
	"github.com/nelvko/clashctl-go/internal/install"
	"github.com/nelvko/clashctl-go/internal/service"
	"github.com/nelvko/clashctl-go/internal/subscription"
)

const (
	defaultSubconverterRepo = "asdlokj1qpi233/subconverter"
	defaultDownloadTimeout  = 60 * time.Second
	subconverterName        = "subconverter"
)

// installOptions 保存安装命令的输入参数。
type installOptions struct {
	Kernel              string
	SubURL              string
	Home                string
	AssetsDir           string
	ResourcesDir        string
	DownloadTimeout     time.Duration
	GHProxy             string
	VersionMihomo       string
	VersionSubconverter string
	SubconverterRepo    string
	SubUA               string
	SkipUI              bool
	NoService           bool
	NoStart             bool
	NoShellRC           bool
}

// install 执行首次安装：准备依赖、生成配置、注册服务并写入 shell 配置。
func (a *App) install(ctx context.Context, args []string) error {
	options, err := a.parseInstallArgs(args)
	if err != nil {
		return err
	}

	if err := checkRequiredCommands(); err != nil {
		return err
	}
	if err := a.validateInstallHome(options.Home); err != nil {
		return err
	}

	a.Paths = config.NewPaths(options.Home, options.Kernel)
	a.Kernel = options.Kernel
	a.KernelPath = a.Paths.KernelPath

	// 订阅管理器必须指向本次安装目录，不能沿用启动时的 CLASHCTL_HOME。
	a.Subscriptions = subscription.NewManager(
		a.Paths,
		a.Paths.KernelPath,
		options.DownloadTimeout,
		options.SubUA,
	)

	a.Printer.OKWithEmoji("📦", "安装内核："+options.Kernel)
	a.Printer.OKWithEmoji("📦", "安装路径："+options.Home)

	client := install.NewClient(options.DownloadTimeout)
	region := installRegion{architecture: install.Architecture()}
	if region.architecture == "amd64" {
		flags, err := install.CPUFlags()
		if err != nil {
			return err
		}
		region.level = install.Amd64Level(flags)
	}

	kernelArchive, err := a.prepareKernel(ctx, client, options, region)
	if err != nil {
		return err
	}
	subconverterArchive, err := a.prepareSubconverter(ctx, client, options, region)
	if err != nil {
		return err
	}
	uiArchive, err := findArchive(options.AssetsDir, "dist")
	if err != nil {
		uiArchive, err = findArchive(options.ResourcesDir, "dist")
	}
	if err != nil && !options.SkipUI {
		return fmt.Errorf("未找到 Web UI 压缩包 dist.zip，请放入 %s 或使用 --skip-ui", options.AssetsDir)
	}

	if err := os.MkdirAll(a.Paths.Resources, 0o755); err != nil {
		return fmt.Errorf("创建资源目录: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(a.Paths.KernelPath), 0o755); err != nil {
		return fmt.Errorf("创建程序目录: %w", err)
	}

	if err := install.ExtractGzip(kernelArchive, a.Paths.KernelPath); err != nil {
		return fmt.Errorf("解压内核: %w", err)
	}
	if err := install.ExtractTarGz(subconverterArchive, filepath.Dir(filepath.Dir(a.Paths.SubconverterPath))); err != nil {
		return fmt.Errorf("解压 subconverter: %w", err)
	}
	if err := installSubconverterConfig(a.Paths); err != nil {
		return err
	}
	if uiArchive != "" {
		if err := install.ExtractZip(uiArchive, a.Paths.Resources); err != nil {
			return fmt.Errorf("解压 Web UI: %w", err)
		}
	}
	if err := a.copyGeoData(options.ResourcesDir); err != nil {
		return err
	}
	if err := a.scaffoldInstallFiles(options); err != nil {
		return err
	}

	a.Service = service.New(a.Paths, options.Kernel)
	if !options.NoService && a.Service.Manager != "nohup" {
		target, err := a.Service.InstallUnit(ctx)
		if err != nil {
			return err
		}
		a.Printer.OKWithEmoji("🧩", "已注册服务："+target)
		a.Service = service.New(a.Paths, options.Kernel)
	}

	// 先把初始订阅导入基础配置，合并与内核校验才有内容可处理。
	var profile *subscription.Profile
	if options.SubURL != "" {
		imported, err := a.importSubscription(ctx, options.SubURL)
		if err != nil {
			return err
		}
		profile = &imported
	} else if isEmptyFile(a.Paths.BaseConfig) {
		return fmt.Errorf("基础配置为空，请使用 --sub-url 提供订阅链接")
	}

	if err := config.Merge(ctx, a.Paths, a.Paths.KernelPath); err != nil {
		return err
	}
	if profile != nil {
		if err := a.markSubscriptionUsed(*profile); err != nil {
			return err
		}
	}
	if err := a.ensureSecret(ctx); err != nil {
		return err
	}
	if err := a.detectProxyPort(ctx); err != nil {
		return err
	}

	if !options.NoStart {
		if err := a.ui(ctx, nil); err != nil {
			return err
		}
	}
	if profile == nil {
		a.Printer.OK("未提供订阅链接，可使用 clashctl sub add <url> 添加")
	}

	if !options.NoShellRC {
		if err := a.writeShellRC(); err != nil {
			return err
		}
	}

	a.Printer.OKWithEmoji("🎉", "安装完成，请执行 source ~/.bashrc 为当前 SHELL 加载 clashctl 命令")
	return nil
}

// installRegion 保存依赖下载所需的架构信息。
type installRegion struct {
	architecture string
	level        string
}

// parseInstallArgs 解析安装参数，并读取 .env.install 中的默认值。
func (a *App) parseInstallArgs(args []string) (installOptions, error) {
	envFile := readInstallEnv(".")

	options := installOptions{
		Kernel:              firstNonEmpty(envFile["CLASHCTL_KERNEL"], "mihomo"),
		SubURL:              envFile["CLASHCTL_SUB_URL"],
		Home:                firstNonEmpty(envFile["CLASHCTL_HOME"], a.Paths.Home),
		AssetsDir:           "archives",
		ResourcesDir:        "resources",
		DownloadTimeout:     defaultDownloadTimeout,
		GHProxy:             envFile["GH_PROXY"],
		VersionMihomo:       envFile["VERSION_MIHOMO"],
		VersionSubconverter: envFile["VERSION_SUBCONVERTER"],
		SubconverterRepo:    firstNonEmpty(envFile["SUBCONVERTER_REPO"], defaultSubconverterRepo),
		SubUA:               firstNonEmpty(envFile["CLASHCTL_SUB_UA"], "mihomo"),
	}

	if value := envFile["CLASHCTL_DOWNLOAD_TIMEOUT"]; value != "" {
		seconds, err := strconv.Atoi(value)
		if err != nil {
			return installOptions{}, fmt.Errorf("无效的下载超时时间: %w", err)
		}
		options.DownloadTimeout = time.Duration(seconds) * time.Second
	}

	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(a.Printer.Err)
	flags.StringVar(&options.Kernel, "kernel", options.Kernel, "内核名称：mihomo 或 clash")
	flags.StringVar(&options.SubURL, "sub-url", options.SubURL, "初始订阅链接")
	flags.StringVar(&options.Home, "home", options.Home, "安装路径")
	flags.StringVar(&options.AssetsDir, "assets", options.AssetsDir, "依赖压缩包目录")
	flags.StringVar(&options.ResourcesDir, "resources", options.ResourcesDir, "geodata 与 dist.zip 所在目录")
	flags.StringVar(&options.GHProxy, "gh-proxy", options.GHProxy, "Github 加速前缀")
	flags.StringVar(&options.VersionMihomo, "version-mihomo", options.VersionMihomo, "mihomo 版本号")
	flags.StringVar(&options.VersionSubconverter, "version-subconverter", options.VersionSubconverter, "subconverter 版本号")
	flags.StringVar(&options.SubconverterRepo, "subconverter-repo", options.SubconverterRepo, "subconverter 仓库")
	flags.StringVar(&options.SubUA, "sub-ua", options.SubUA, "订阅下载 User-Agent")
	flags.BoolVar(&options.SkipUI, "skip-ui", false, "不安装 Web UI")
	flags.BoolVar(&options.NoService, "no-service", false, "不注册服务单元")
	flags.BoolVar(&options.NoStart, "no-start", false, "只安装不启动")
	flags.BoolVar(&options.NoShellRC, "no-shell-rc", false, "不写入 shell 配置")

	// 分段解析，允许标志与位置参数以任意顺序出现。
	positionals := []string{}
	rest := args
	for {
		if err := flags.Parse(rest); err != nil {
			return installOptions{}, err
		}

		remaining := flags.Args()
		if len(remaining) == 0 {
			break
		}

		index := 0
		for index < len(remaining) && !strings.HasPrefix(remaining[index], "-") {
			positionals = append(positionals, remaining[index])
			index++
		}
		if index == len(remaining) {
			break
		}
		rest = remaining[index:]
	}

	// 位置参数：内核名称在前，订阅链接在后。
	for _, arg := range positionals {
		switch {
		case arg == "mihomo" || arg == "clash":
			options.Kernel = arg
		case strings.HasPrefix(arg, "http://"), strings.HasPrefix(arg, "https://"), strings.HasPrefix(arg, "file://"):
			options.SubURL = arg
		default:
			return installOptions{}, fmt.Errorf("无法识别的安装参数：%s", arg)
		}
	}

	if options.Kernel != "mihomo" && options.Kernel != "clash" {
		return installOptions{}, fmt.Errorf("不支持的内核：%s", options.Kernel)
	}
	if options.Home == "" {
		return installOptions{}, fmt.Errorf("安装路径不能为空")
	}

	home, err := expandHome(options.Home)
	if err != nil {
		return installOptions{}, err
	}
	options.Home = home

	assetsDir, err := filepath.Abs(options.AssetsDir)
	if err != nil {
		return installOptions{}, fmt.Errorf("解析依赖目录: %w", err)
	}
	options.AssetsDir = assetsDir

	resourcesDir, err := filepath.Abs(options.ResourcesDir)
	if err != nil {
		return installOptions{}, fmt.Errorf("解析资源目录: %w", err)
	}
	options.ResourcesDir = resourcesDir

	return options, nil
}

// validateInstallHome 确认安装目录尚未存在且父目录可写。
func (a *App) validateInstallHome(home string) error {
	if _, err := os.Stat(home); err == nil {
		return fmt.Errorf("安装目录已存在：%s，请先移除", home)
	}

	parent := filepath.Dir(home)
	for {
		info, err := os.Stat(parent)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s 不是目录", parent)
			}
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("检查安装路径: %w", err)
		}
		parent = filepath.Dir(parent)
	}

	probe, err := os.CreateTemp(parent, ".clashctl-probe-*")
	if err != nil {
		return fmt.Errorf("%s 当前路径不可写: %w", home, err)
	}
	probeName := probe.Name()
	probe.Close()
	return os.Remove(probeName)
}

// prepareKernel 确保内核压缩包可用，缺失时下载。
func (a *App) prepareKernel(
	ctx context.Context,
	client *http.Client,
	options installOptions,
	region installRegion,
) (string, error) {
	if archive, err := findArchive(options.AssetsDir, options.Kernel); err == nil {
		if err := install.ValidateGzip(archive); err != nil {
			return "", err
		}
		return archive, nil
	}

	level := region.level
	version := options.VersionMihomo
	if options.Kernel == "mihomo" && version == "" {
		latest, err := install.ResolveLatestTag(ctx, client, install.KernelRepository)
		if err != nil {
			return "", fmt.Errorf("获取 mihomo 版本失败，可在 .env.install 指定 VERSION_MIHOMO: %w", err)
		}
		a.Printer.OKWithEmoji("🏷️", "mihomo → "+latest)
		version = latest
	}
	if options.Kernel != "mihomo" {
		level = ""
		version = ""
	}
	if version != "" && level != "" {
		version = level + "-" + version
	}

	address, err := install.KernelURL(options.Kernel, region.architecture, level, version)
	if err != nil {
		return "", err
	}
	return a.downloadArchive(ctx, client, options, address)
}

// prepareSubconverter 确保 subconverter 压缩包可用，缺失时下载。
func (a *App) prepareSubconverter(
	ctx context.Context,
	client *http.Client,
	options installOptions,
	region installRegion,
) (string, error) {
	if archive, err := findArchive(options.AssetsDir, subconverterName); err == nil {
		if err := install.ValidateGzip(archive); err != nil {
			return "", err
		}
		return archive, nil
	}

	version := options.VersionSubconverter
	if version == "" {
		latest, err := install.ResolveLatestTag(ctx, client, options.SubconverterRepo)
		if err != nil {
			return "", fmt.Errorf("获取 subconverter 版本失败，可在 .env.install 指定 VERSION_SUBCONVERTER: %w", err)
		}
		a.Printer.OKWithEmoji("🏷️", options.SubconverterRepo+" → "+latest)
		version = latest
	}

	address, err := install.SubconverterURL(options.SubconverterRepo, region.architecture, version)
	if err != nil {
		return "", err
	}
	return a.downloadArchive(ctx, client, options, address)
}

// downloadArchive 下载压缩包到依赖目录并校验完整性。
func (a *App) downloadArchive(
	ctx context.Context,
	client *http.Client,
	options installOptions,
	address string,
) (string, error) {
	if err := os.MkdirAll(options.AssetsDir, 0o755); err != nil {
		return "", fmt.Errorf("创建依赖目录: %w", err)
	}

	proxied := install.WithProxy(options.GHProxy, address)
	target := filepath.Join(options.AssetsDir, filepath.Base(address))
	a.Printer.OKWithEmoji("⏳", "正在下载："+proxied)

	if err := install.Download(ctx, client, proxied, target, "clashctl"); err != nil {
		return "", err
	}
	if err := install.ValidateGzip(target); err != nil {
		return "", err
	}
	return target, nil
}

func checkRequiredCommands() error {
	required := []string{"pgrep", "pkill"}
	missing := make([]string, 0, len(required))
	for _, name := range required {
		if _, err := exec.LookPath(name); err != nil {
			missing = append(missing, name)
		}
	}
	if _, err := exec.LookPath("ip"); err != nil {
		if _, hostErr := exec.LookPath("hostname"); hostErr != nil {
			missing = append(missing, "ip/hostname")
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("请先安装以下命令：%s", strings.Join(missing, " "))
	}
	return nil
}

func findArchive(dir string, prefixes ...string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("目录不存在：%s", dir)
		}
		return "", fmt.Errorf("读取目录 %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(entry.Name(), prefix) {
				return filepath.Join(dir, entry.Name()), nil
			}
		}
	}
	return "", fmt.Errorf("目录 %s 中没有匹配 %s 的文件", dir, strings.Join(prefixes, "/"))
}

func installSubconverterConfig(paths config.Paths) error {
	example := filepath.Join(paths.SubconverterDir, "pref.example.yml")
	if _, err := os.Stat(example); err != nil {
		return fmt.Errorf("缺少 %s: %w", example, err)
	}
	return config.Copy(example, paths.SubconverterConf)
}

func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("查找用户目录: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	return "", fmt.Errorf("无法展开路径：%s", path)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func readInstallEnv(dir string) map[string]string {
	values := map[string]string{}

	data, err := os.ReadFile(filepath.Join(dir, ".env.install"))
	if err != nil {
		return values
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(strings.SplitN(value, "#", 2)[0])
		values[key] = strings.Trim(value, "\"'")
	}
	return values
}

// ensureSecret 在运行配置缺少密钥时生成随机密钥。
func (a *App) ensureSecret(ctx context.Context) error {
	secret, err := config.Value(a.Paths.RuntimeConfig, "secret")
	if err != nil {
		return err
	}
	if secret != "" {
		return nil
	}

	generated, err := randomSecret()
	if err != nil {
		return err
	}
	if err := config.SetString(a.Paths.MixinConfig, "secret", generated); err != nil {
		return err
	}
	if err := config.Merge(ctx, a.Paths, a.Paths.KernelPath); err != nil {
		return err
	}
	a.Printer.OKWithEmoji("🔑", "已生成 Web 访问密钥："+generated)
	return nil
}

func randomSecret() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	result := make([]byte, 6)
	for index := range result {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", fmt.Errorf("生成随机密钥: %w", err)
		}
		result[index] = alphabet[value.Int64()]
	}
	return string(result), nil
}

// copyGeoData 复制规则库，缺失时由内核自行下载。
func (a *App) copyGeoData(resourcesDir string) error {
	copied := false
	for _, name := range []string{"Country.mmdb", "geosite.dat"} {
		source := filepath.Join(resourcesDir, name)
		if _, err := os.Stat(source); err != nil {
			continue
		}
		if err := config.Copy(source, filepath.Join(a.Paths.Resources, name)); err != nil {
			return fmt.Errorf("复制 %s: %w", name, err)
		}
		copied = true
	}
	if !copied {
		a.Printer.OK("未提供规则库文件，内核首次启动时会自行下载")
	}
	return nil
}

// scaffoldInstallFiles 建立安装目录并写入默认配置文件。
func (a *App) scaffoldInstallFiles(options installOptions) error {
	if err := os.MkdirAll(a.Paths.ProfilesDir, 0o755); err != nil {
		return fmt.Errorf("创建订阅目录: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(a.Paths.Home, "scripts", "cmd"), 0o755); err != nil {
		return fmt.Errorf("创建脚本目录: %w", err)
	}

	for path, content := range map[string]string{
		a.Paths.MixinConfig:  mixinTemplate,
		a.Paths.ProfilesMeta: profilesTemplate,
	} {
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("写入 %s: %w", path, err)
		}
	}

	if _, err := os.Stat(a.Paths.BaseConfig); os.IsNotExist(err) {
		if err := os.WriteFile(a.Paths.BaseConfig, nil, 0o644); err != nil {
			return fmt.Errorf("创建基础配置: %w", err)
		}
	}

	if err := a.writeEnvFile(options); err != nil {
		return err
	}

	wrapper := filepath.Join(a.Paths.Home, "scripts", "cmd", "clashctl.sh")
	if err := os.WriteFile(wrapper, []byte(clashctlShell), 0o644); err != nil {
		return fmt.Errorf("写入 shell 入口: %w", err)
	}
	return nil
}

// writeEnvFile 写入安装目录下的 .env。
func (a *App) writeEnvFile(options installOptions) error {
	content := fmt.Sprintf(`######################################
#
#   用户自定义参数 更改后需重新加载 shell 后生效
#
######################################

# 订阅下载超时时间（秒）
CLASHCTL_SUB_TIMEOUT=%d

# 订阅下载 UA
CLASHCTL_SUB_UA=mihomo

######################################
#
#   以下参数由安装命令生成
#
######################################

# 实际安装的内核
CLASHCTL_KERNEL=%s
`, int(options.DownloadTimeout.Seconds()), options.Kernel)

	path := filepath.Join(a.Paths.Home, ".env")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("写入 %s: %w", path, err)
	}
	return nil
}

// importSubscription 下载并保存初始订阅，同时应用到基础配置。
func (a *App) importSubscription(ctx context.Context, sourceURL string) (subscription.Profile, error) {
	profile, err := a.Subscriptions.Add(ctx, sourceURL)
	if err != nil {
		return subscription.Profile{}, err
	}
	a.Printer.OKWithEmoji("🎉", fmt.Sprintf("订阅已添加：[%d] %s", profile.ID, profile.URL))

	if err := config.Copy(profile.Path, a.Paths.BaseConfig); err != nil {
		return subscription.Profile{}, fmt.Errorf("应用初始订阅: %w", err)
	}
	return profile, nil
}

// markSubscriptionUsed 记录当前生效的订阅 id。
func (a *App) markSubscriptionUsed(profile subscription.Profile) error {
	metadata, err := a.Subscriptions.LoadMetadata()
	if err != nil {
		return err
	}
	metadata.Use = profile.ID
	return a.Subscriptions.SaveMetadata(metadata)
}

// isEmptyFile 报告文件是否不存在或为空。
func isEmptyFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return info.Size() == 0
}

// writeShellRC 把环境变量与入口脚本写入 shell 配置。
func (a *App) writeShellRC() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位可执行文件: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("查找用户目录: %w", err)
	}

	block := fmt.Sprintf(
		"export CLASHCTL_HOME=%s\nexport CLASHCTL_GO_BIN=%s\n. $CLASHCTL_HOME/scripts/cmd/clashctl.sh\n",
		a.Paths.Home,
		executable,
	)

	written := []string{}
	for _, name := range []string{".bashrc", ".zshrc"} {
		path := filepath.Join(home, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "CLASHCTL_HOME") {
			continue
		}

		content := string(data)
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += block

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("写入 %s: %w", path, err)
		}
		written = append(written, path)
	}

	if len(written) == 0 {
		a.Printer.OK("未修改 shell 配置，请手动加载 " + filepath.Join(a.Paths.Home, "scripts", "cmd", "clashctl.sh"))
		return nil
	}
	a.Printer.OKWithEmoji("📄", "已写入 shell 配置："+strings.Join(written, " "))
	return nil
}
