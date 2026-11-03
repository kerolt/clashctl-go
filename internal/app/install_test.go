package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kerolt/clashctl-go/internal/config"
	"github.com/kerolt/clashctl-go/internal/output"
)

// TestInstallHelp 核验安装帮助正常返回成功状态。
func TestInstallHelp(t *testing.T) {
	stderr := &bytes.Buffer{}
	application := &App{
		Printer: output.New(&bytes.Buffer{}, stderr),
		Paths:   config.NewPaths(filepath.Join(t.TempDir(), "clashctl"), "mihomo"),
	}

	if err := application.install(context.Background(), []string{"--help"}); err != nil {
		t.Fatalf("install(--help) error = %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage of install:") {
		t.Fatalf("install help missing usage:\n%s", stderr.String())
	}
}

// TestInstall 在临时目录中完整执行一次安装，并核验生成的文件。
func TestInstall(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "archives")
	resources := filepath.Join(root, "resources")
	home := filepath.Join(root, "prefix", "clashctl")

	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatalf("create assets dir: %v", err)
	}
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatalf("create resources dir: %v", err)
	}

	prepareInstallAssets(t, assets, resources)
	baseConfig := writeTestBaseConfig(t, root)

	stdout := &bytes.Buffer{}
	application := &App{
		Printer: output.New(stdout, &bytes.Buffer{}),
		Paths:   config.NewPaths(home, "mihomo"),
	}

	err := application.install(context.Background(), []string{
		"--home", home,
		"--assets", assets,
		"--resources", resources,
		"--kernel", "mihomo",
		"--sub-url", "file://" + baseConfig,
		"--no-service",
		"--no-start",
		"--no-shell-rc",
	})
	if err != nil {
		t.Fatalf("install() error = %v\noutput:\n%s", err, stdout.String())
	}

	paths := config.NewPaths(home, "mihomo")
	expectFile(t, paths.KernelPath)
	expectFile(t, paths.SubconverterPath)
	expectFile(t, paths.SubconverterConf)
	expectFile(t, filepath.Join(paths.Resources, "dist", "index.html"))
	expectFile(t, filepath.Join(paths.Resources, "Country.mmdb"))
	expectFile(t, filepath.Join(paths.ProfilesDir, "1.yaml"))
	expectFile(t, filepath.Join(home, ".env"))
	expectFile(t, filepath.Join(home, "scripts", "cmd", "clashctl.sh"))

	runtime := readTestFile(t, paths.RuntimeConfig)
	for _, expected := range []string{"test-proxy", "secret: ", "mixed-port:"} {
		if !strings.Contains(runtime, expected) {
			t.Fatalf("runtime config missing %q:\n%s", expected, runtime)
		}
	}
	if strings.Contains(runtime, "secret: \n") {
		t.Fatalf("runtime config has an empty secret:\n%s", runtime)
	}

	profiles := readTestFile(t, paths.ProfilesMeta)
	if !strings.Contains(profiles, "use: 1") {
		t.Fatalf("profiles metadata missing use id:\n%s", profiles)
	}

	env := readTestFile(t, filepath.Join(home, ".env"))
	if !strings.Contains(env, "CLASHCTL_SUB_UA=mihomo") {
		t.Fatalf(".env missing subscription user agent:\n%s", env)
	}

	wrapper := readTestFile(t, filepath.Join(home, "scripts", "cmd", "clashctl.sh"))
	if !strings.Contains(wrapper, "CLASHCTL_GO_BIN") {
		t.Fatalf("shell wrapper missing runner variable:\n%s", wrapper)
	}
}

// TestInstallDoesNotTouchStartupHome 核验安装不会写入启动时的 CLASHCTL_HOME。
func TestInstallDoesNotTouchStartupHome(t *testing.T) {
	root := t.TempDir()
	startupHome := filepath.Join(root, "startup", "clashctl")
	targetHome := filepath.Join(root, "target", "clashctl")
	assets := filepath.Join(root, "archives")
	resources := filepath.Join(root, "resources")

	if err := os.MkdirAll(startupHome, 0o755); err != nil {
		t.Fatalf("create startup home: %v", err)
	}
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatalf("create assets: %v", err)
	}
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatalf("create resources: %v", err)
	}

	prepareInstallAssets(t, assets, resources)
	baseConfig := writeTestBaseConfig(t, root)

	t.Setenv("CLASHCTL_HOME", startupHome)

	application, err := New(&bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = application.install(context.Background(), []string{
		"--home", targetHome,
		"--kernel", "mihomo",
		"--assets", assets,
		"--resources", resources,
		"--sub-url", "file://" + baseConfig,
		"--no-service",
		"--no-start",
		"--no-shell-rc",
	})
	if err != nil {
		t.Fatalf("install() error = %v", err)
	}

	entries, err := os.ReadDir(startupHome)
	if err != nil {
		t.Fatalf("read startup home: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("startup home received %d entries, want 0", len(entries))
	}

	expectFile(t, filepath.Join(targetHome, "resources", "profiles", "1.yaml"))
}

// TestInstallRejectsExistingHome 核验已存在的安装目录会被拒绝。
func TestInstallRejectsExistingHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "clashctl")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create home: %v", err)
	}

	application := &App{
		Printer: output.New(&bytes.Buffer{}, &bytes.Buffer{}),
		Paths:   config.NewPaths(home, "mihomo"),
	}

	err := application.install(context.Background(), []string{"--home", home, "--no-start"})
	if err == nil {
		t.Fatal("install() error = nil for existing home")
	}
	if !strings.Contains(err.Error(), "已存在") {
		t.Fatalf("install() error = %v", err)
	}
}

// TestParseInstallArgs 核验位置参数与默认值。
func TestParseInstallArgs(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "clashctl")

	application := &App{
		Printer: output.New(&bytes.Buffer{}, &bytes.Buffer{}),
		Paths:   config.NewPaths(home, "mihomo"),
	}

	options, err := application.parseInstallArgs([]string{
		"mihomo",
		"https://example.com/sub",
		"--home", home,
		"--assets", root,
	})
	if err != nil {
		t.Fatalf("parseInstallArgs() error = %v", err)
	}
	if options.Kernel != "mihomo" {
		t.Fatalf("Kernel = %q, want mihomo", options.Kernel)
	}
	if options.SubURL != "https://example.com/sub" {
		t.Fatalf("SubURL = %q", options.SubURL)
	}
	if options.Home != home {
		t.Fatalf("Home = %q, want %q", options.Home, home)
	}

	if _, err := application.parseInstallArgs([]string{"--kernel", "clash"}); err == nil {
		t.Fatal("parseInstallArgs() error = nil for unsupported kernel")
	}
}

// prepareInstallAssets 生成安装流程所需的最小依赖包。
func prepareInstallAssets(t *testing.T, assets, resources string) {
	t.Helper()

	prepareInstallBinaries(t, assets, resources)
	if err := writeTestZip(filepath.Join(assets, "dist.zip"), map[string]string{
		"dist/index.html": "<html>panel</html>",
	}); err != nil {
		t.Fatalf("create web ui archive: %v", err)
	}
}

// prepareInstallBinaries 生成内核、subconverter 与规则库，不包含 Web UI。
func prepareInstallBinaries(t *testing.T, assets, resources string) {
	t.Helper()

	// 假内核与假 subconverter 只需要成功退出，用于校验配置与安装流程。
	if err := writeTestGzip(
		filepath.Join(assets, "mihomo-linux-amd64-test.gz"),
		[]byte("#!/bin/sh\nexit 0\n"),
	); err != nil {
		t.Fatalf("create kernel archive: %v", err)
	}
	if err := writeTestTarGz(filepath.Join(assets, "subconverter_linux64.tar.gz"), map[string]string{
		"subconverter/subconverter":     "#!/bin/sh\nexit 0\n",
		"subconverter/pref.example.yml": "server:\n  port: 25500\n",
	}); err != nil {
		t.Fatalf("create subconverter archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resources, "Country.mmdb"), []byte("mmdb"), 0o644); err != nil {
		t.Fatalf("create geodata: %v", err)
	}
}

// writeTestBaseConfig 生成安装时导入的基础配置。
func writeTestBaseConfig(t *testing.T, root string) string {
	t.Helper()

	path := filepath.Join(root, "base.yaml")
	content := `proxies:
  - name: test-proxy
    type: ss
    server: 127.0.0.1
    port: 8388
    cipher: aes-128-gcm
    password: test
proxy-groups:
  - name: 节点选择
    type: select
    proxies:
      - test-proxy
rules:
  - MATCH,节点选择
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("create base config: %v", err)
	}
	return path
}

func expectFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("missing file %s: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("%s is a directory", path)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func writeTestGzip(path string, content []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := gzip.NewWriter(file)
	if _, err := writer.Write(content); err != nil {
		return err
	}
	return writer.Close()
}

func writeTestTarGz(path string, files map[string]string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	for name, content := range files {
		header := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			return err
		}
	}

	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func writeTestZip(path string, files map[string]string) error {
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)

	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			return err
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buffer.Bytes(), 0o644)
}

// TestInstallDownloadsUI 核验缺少本地压缩包时按地址下载面板。
func TestInstallDownloadsUI(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "archives")
	resources := filepath.Join(root, "resources")
	home := filepath.Join(root, "target", "clashctl")

	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatalf("create assets: %v", err)
	}
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatalf("create resources: %v", err)
	}
	prepareInstallBinaries(t, assets, resources)
	baseConfig := writeTestBaseConfig(t, root)

	panelZip := filepath.Join(root, "panel.zip")
	if err := writeTestZip(panelZip, map[string]string{"dist/index.html": "<html>remote</html>"}); err != nil {
		t.Fatalf("create panel archive: %v", err)
	}
	payload, err := os.ReadFile(panelZip)
	if err != nil {
		t.Fatalf("read panel archive: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Write(payload)
	}))
	defer server.Close()

	application := &App{
		Printer: output.New(&bytes.Buffer{}, &bytes.Buffer{}),
		Paths:   config.NewPaths(home, "mihomo"),
	}
	err = application.install(context.Background(), []string{
		"--home", home,
		"--kernel", "mihomo",
		"--assets", assets,
		"--resources", resources,
		"--sub-url", "file://" + baseConfig,
		"--ui-dist", server.URL + "/dist.zip",
		"--no-service",
		"--no-start",
		"--no-shell-rc",
	})
	if err != nil {
		t.Fatalf("install() error = %v", err)
	}

	expectFile(t, filepath.Join(assets, "dist.zip"))
	expectFile(t, filepath.Join(home, "resources", "dist", "index.html"))
}

// TestInstallUsesLocalUIDist 核验 --ui-dist 指向本地文件时直接使用。
func TestInstallUsesLocalUIDist(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "archives")
	resources := filepath.Join(root, "resources")
	home := filepath.Join(root, "target", "clashctl")

	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatalf("create assets: %v", err)
	}
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatalf("create resources: %v", err)
	}
	prepareInstallBinaries(t, assets, resources)
	baseConfig := writeTestBaseConfig(t, root)

	localPanel := filepath.Join(root, "custom-panel.zip")
	if err := writeTestZip(localPanel, map[string]string{"dist/index.html": "<html>local</html>"}); err != nil {
		t.Fatalf("create panel archive: %v", err)
	}

	application := &App{
		Printer: output.New(&bytes.Buffer{}, &bytes.Buffer{}),
		Paths:   config.NewPaths(home, "mihomo"),
	}
	err := application.install(context.Background(), []string{
		"--home", home,
		"--kernel", "mihomo",
		"--assets", assets,
		"--resources", resources,
		"--sub-url", "file://" + baseConfig,
		"--ui-dist", localPanel,
		"--no-service",
		"--no-start",
		"--no-shell-rc",
	})
	if err != nil {
		t.Fatalf("install() error = %v", err)
	}

	expectFile(t, filepath.Join(home, "resources", "dist", "index.html"))
}

// TestInstallRejectsBrokenUIDist 核验面板压缩包无效时报错。
func TestInstallRejectsBrokenUIDist(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "archives")
	resources := filepath.Join(root, "resources")
	home := filepath.Join(root, "target", "clashctl")

	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatalf("create assets: %v", err)
	}
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatalf("create resources: %v", err)
	}
	prepareInstallBinaries(t, assets, resources)

	application := &App{
		Printer: output.New(&bytes.Buffer{}, &bytes.Buffer{}),
		Paths:   config.NewPaths(home, "mihomo"),
	}
	err := application.install(context.Background(), []string{
		"--home", home,
		"--kernel", "mihomo",
		"--assets", assets,
		"--resources", resources,
		"--ui-dist", filepath.Join(root, "missing.zip"),
		"--no-service",
		"--no-start",
		"--no-shell-rc",
	})
	if err == nil {
		t.Fatal("install() error = nil for broken ui archive")
	}
	if !strings.Contains(err.Error(), "Web UI") {
		t.Fatalf("install() error = %v", err)
	}
}

// TestParseInstallArgsEnvDefaults 核验环境变量作为安装默认值。
func TestParseInstallArgsEnvDefaults(t *testing.T) {
	home := filepath.Join(t.TempDir(), "clashctl")

	t.Setenv("CLASHCTL_SUB_UA", "mihomo-ua")
	t.Setenv("CLASHCTL_DOWNLOAD_TIMEOUT", "30")

	application := &App{
		Printer: output.New(&bytes.Buffer{}, &bytes.Buffer{}),
		Paths:   config.NewPaths(home, "mihomo"),
	}

	options, err := application.parseInstallArgs(nil)
	if err != nil {
		t.Fatalf("parseInstallArgs() error = %v", err)
	}
	if options.Kernel != "mihomo" {
		t.Fatalf("Kernel = %q, want mihomo", options.Kernel)
	}
	if options.SubUA != "mihomo-ua" {
		t.Fatalf("SubUA = %q", options.SubUA)
	}
	if options.DownloadTimeout != 30*time.Second {
		t.Fatalf("DownloadTimeout = %v, want 30s", options.DownloadTimeout)
	}
	if options.UIDist != "" {
		t.Fatalf("UIDist = %q, want empty", options.UIDist)
	}
}

// TestParseInstallArgsIgnoresLegacyFile 核验旧参数文件不再影响安装参数。
func TestParseInstallArgsIgnoresLegacyFile(t *testing.T) {
	workDir := t.TempDir()
	legacy := `CLASHCTL_HOME=/tmp/legacy-home
ZIP_UI=/tmp/legacy-dist.zip
`
	if err := os.WriteFile(filepath.Join(workDir, ".env.install"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	t.Chdir(workDir)

	home := filepath.Join(t.TempDir(), "clashctl")
	t.Setenv("CLASHCTL_SUB_UA", "")

	application := &App{
		Printer: output.New(&bytes.Buffer{}, &bytes.Buffer{}),
		Paths:   config.NewPaths(home, "mihomo"),
	}

	options, err := application.parseInstallArgs(nil)
	if err != nil {
		t.Fatalf("parseInstallArgs() error = %v", err)
	}
	if options.Kernel != "mihomo" {
		t.Fatalf("Kernel = %q, want mihomo", options.Kernel)
	}
	if options.Home != home {
		t.Fatalf("Home = %q, want %q", options.Home, home)
	}
	if options.UIDist != "" {
		t.Fatalf("UIDist = %q, want empty", options.UIDist)
	}
}
