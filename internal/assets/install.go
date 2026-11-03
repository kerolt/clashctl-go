// Package assets 负责安装依赖的下载、校验和解压。
package assets

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	KernelRepository = "MetaCubeX/mihomo"
	latestReleaseAPI = "https://api.github.com/repos/%s/releases/latest"
)

// NewClient 构造安装阶段使用的 HTTP 客户端，允许不校验证书以适配自建下载代理。
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// WithProxy 在下载地址前加上 gh-proxy 前缀。
func WithProxy(prefix, address string) string {
	if prefix == "" {
		return address
	}
	return strings.TrimSuffix(prefix, "/") + "/" + address
}

// Architecture 返回 uname -m 风格的架构名。
func Architecture() string {
	return runtime.GOARCH
}

// Amd64Level 依据 CPU flags 选择 mihomo 的指令集版本。
func Amd64Level(flags string) string {
	level := "v1"
	has := func(name string) bool {
		for _, field := range strings.Fields(flags) {
			if field == name {
				return true
			}
		}
		return false
	}

	if has("sse4_2") && has("popcnt") {
		level = "v2"
	}
	if has("avx2") && has("fma") {
		level = "v3"
	}
	return level
}

// CPUFlags 读取 /proc/cpuinfo 中的第一条 flags 行。
func CPUFlags() (string, error) {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "", fmt.Errorf("read cpuinfo: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "flags") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return parts[1], nil
			}
		}
	}
	return "", fmt.Errorf("cpu flags not found")
}

// ResolveLatestTag 查询仓库最新发布版本号。
func ResolveLatestTag(ctx context.Context, client *http.Client, repository string) (string, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf(latestReleaseAPI, repository),
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("create release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("query latest release of %s: %w", repository, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("query latest release of %s: http status %s", repository, response.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode latest release of %s: %w", repository, err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("repository %s returned an empty tag", repository)
	}
	return payload.TagName, nil
}

// KernelURL 返回 mihomo 压缩包的下载地址。
func KernelURL(kernel, arch, level, version string) (string, error) {
	if kernel != "mihomo" {
		return "", fmt.Errorf("不支持的内核：%s", kernel)
	}
	if version == "" {
		return "", fmt.Errorf("缺少 mihomo 版本号")
	}

	switch arch {
	case "amd64":
		return mihomoURL("amd64", level, version), nil
	case "arm64":
		return mihomoURL("arm64", level, version), nil
	case "386":
		return mihomoURL("386", level, version), nil
	case "arm":
		return mihomoURL("armv7", level, version), nil
	}
	return "", fmt.Errorf("mihomo 暂不支持架构 %s", arch)
}

func mihomoURL(arch, level, version string) string {
	tag := strings.TrimPrefix(version, level+"-")
	return fmt.Sprintf(
		"https://github.com/%s/releases/download/%s/mihomo-linux-%s-%s-%s.gz",
		KernelRepository,
		tag,
		arch,
		level,
		tag,
	)
}

// SubconverterURL 返回 subconverter 压缩包的下载地址。
func SubconverterURL(repository, arch, version string) (string, error) {
	var suffix string
	switch arch {
	case "amd64":
		suffix = "linux64"
	case "386":
		suffix = "linux32"
	case "arm64":
		suffix = "aarch64"
	case "arm":
		suffix = "armv7"
	default:
		return "", fmt.Errorf("subconverter 暂不支持架构 %s", arch)
	}

	if version == "" {
		return "", fmt.Errorf("缺少 subconverter 版本号")
	}
	return fmt.Sprintf(
		"https://github.com/%s/releases/download/%s/subconverter_%s.tar.gz",
		repository,
		version,
		suffix,
	), nil
}

// Download 下载文件到 dest，失败时重试一次。
func Download(ctx context.Context, client *http.Client, address, dest, userAgent string) error {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := downloadOnce(ctx, client, address, dest, userAgent); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func downloadOnce(ctx context.Context, client *http.Client, address, dest, userAgent string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download %s: %w", address, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download %s: http status %s", address, response.Status)
	}

	file, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}

	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("write %s: %w", dest, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", dest, closeErr)
	}
	return nil
}

// ValidateGzip 校验 gzip 文件可以完整解压。
func ValidateGzip(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read gzip header of %s: %w", path, err)
	}
	defer reader.Close()

	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("decompress %s: %w", path, err)
	}
	return nil
}

// ValidateZip 校验 zip 文件目录结构可读。
func ValidateZip(path string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", path, err)
	}
	return reader.Close()
}

// ExtractGzip 解压单文件 gzip 到 dest，并设置可执行权限。
func ExtractGzip(source, dest string) error {
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read gzip header of %s: %w", source, err)
	}
	defer reader.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", dest, err)
	}

	target, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}

	_, copyErr := io.Copy(target, reader)
	closeErr := target.Close()
	if copyErr != nil {
		return fmt.Errorf("write %s: %w", dest, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", dest, closeErr)
	}
	return os.Chmod(dest, 0o755)
}

// ExtractTarGz 解压 tar.gz 到 destDir。
func ExtractTarGz(source, destDir string) error {
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read gzip header of %s: %w", source, err)
	}
	defer reader.Close()

	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar entry of %s: %w", source, err)
		}

		target, err := safePath(destDir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := writeArchiveFile(target, archive, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create directory for %s: %w", target, err)
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("create symlink %s: %w", target, err)
			}
		}
	}
}

// ExtractZip 解压 zip 到 destDir。
func ExtractZip(source, destDir string) error {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return fmt.Errorf("open zip %s: %w", source, err)
	}
	defer reader.Close()

	for _, entry := range reader.File {
		target, err := safePath(destDir, entry.Name)
		if err != nil {
			return err
		}

		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}
			continue
		}

		content, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %s: %w", entry.Name, err)
		}
		writeErr := writeArchiveFile(target, content, entry.Mode())
		content.Close()
		if writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func writeArchiveFile(target string, reader io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", target, err)
	}

	permission := mode.Perm()
	if permission == 0 {
		permission = 0o644
	}

	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, permission)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}

	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("write %s: %w", target, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", target, closeErr)
	}
	return os.Chmod(target, permission)
}

func safePath(root, name string) (string, error) {
	target := filepath.Join(root, name)

	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", fmt.Errorf("解析压缩包路径 %s: %w", name, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("压缩包包含越界路径：%s", name)
	}
	return target, nil
}
