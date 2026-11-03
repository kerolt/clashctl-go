// Package selfupgrade 负责查询 GitHub Release 并原子替换当前程序。
package selfupgrade

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultRepository = "kerolt/clashctl-go"
	defaultAPIURL     = "https://api.github.com"
	maxBinarySize     = 100 << 20
	maxChecksumSize   = 1 << 20
)

// Asset 描述 GitHub Release 中的一个下载资源。
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Release 描述 GitHub Release 的必要字段。
type Release struct {
	TagName    string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Result 描述一次自升级结果。
type Result struct {
	Tag        string
	AssetName  string
	TargetPath string
}

// Client 访问 GitHub Releases 并更新当前程序。
type Client struct {
	HTTPClient *http.Client
	APIBaseURL string
	Repository string
	UserAgent  string
}

// NewClient 创建自升级客户端。
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		HTTPClient: httpClient,
		APIBaseURL: defaultAPIURL,
		Repository: DefaultRepository,
		UserAgent:  "clashctl-selfupgrade",
	}
}

// ResolveRelease 查询最新稳定 Release，或查询指定 tag 的 Release。
func (c *Client) ResolveRelease(ctx context.Context, tag string) (Release, error) {
	endpoint := strings.TrimRight(c.APIBaseURL, "/") + "/repos/" + c.Repository + "/releases/latest"
	if tag != "" {
		endpoint = strings.TrimRight(c.APIBaseURL, "/") + "/repos/" + c.Repository + "/releases/tags/" + url.PathEscape(tag)
	}

	response, err := c.get(ctx, endpoint)
	if err != nil {
		return Release{}, err
	}
	defer response.Body.Close()

	var release Release
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return Release{}, fmt.Errorf("decode release metadata: %w", err)
	}
	if release.TagName == "" {
		return Release{}, fmt.Errorf("release metadata has an empty tag")
	}
	if tag == "" && (release.Draft || release.Prerelease) {
		return Release{}, fmt.Errorf("latest release %s is not stable", release.TagName)
	}
	return release, nil
}

// SelectAssets 按当前系统选择程序文件和 SHA256SUMS 文件。
func SelectAssets(release Release, goos, goarch string) (Asset, Asset, error) {
	if goos != "linux" {
		return Asset{}, Asset{}, fmt.Errorf("unsupported operating system %s", goos)
	}

	binaryName := fmt.Sprintf("clashctl-%s-%s", goos, goarch)
	var binary Asset
	var checksums Asset
	for _, asset := range release.Assets {
		switch asset.Name {
		case binaryName:
			binary = asset
		case "SHA256SUMS":
			checksums = asset
		}
	}
	if binary.Name == "" {
		return Asset{}, Asset{}, fmt.Errorf("release %s has no asset %s", release.TagName, binaryName)
	}
	if binary.BrowserDownloadURL == "" {
		return Asset{}, Asset{}, fmt.Errorf("release asset %s has no download URL", binary.Name)
	}
	if checksums.Name == "" {
		return Asset{}, Asset{}, fmt.Errorf("release %s has no SHA256SUMS asset", release.TagName)
	}
	if checksums.BrowserDownloadURL == "" {
		return Asset{}, Asset{}, fmt.Errorf("release asset %s has no download URL", checksums.Name)
	}
	return binary, checksums, nil
}

// Upgrade 下载目标 Release，校验资源并原子替换 executablePath。
func (c *Client) Upgrade(
	ctx context.Context,
	tag string,
	executablePath string,
	goos string,
	goarch string,
) (Result, error) {
	release, err := c.ResolveRelease(ctx, tag)
	if err != nil {
		return Result{}, err
	}
	binary, checksums, err := SelectAssets(release, goos, goarch)
	if err != nil {
		return Result{}, err
	}

	target, err := resolveExecutablePath(executablePath)
	if err != nil {
		return Result{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".clashctl-selfupgrade-*")
	if err != nil {
		return Result{}, fmt.Errorf("create temporary upgrade file: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return Result{}, fmt.Errorf("close temporary upgrade file: %w", err)
	}
	defer os.Remove(temporaryPath)

	if err := c.downloadFile(ctx, binary.BrowserDownloadURL, temporaryPath); err != nil {
		return Result{}, err
	}
	checksumData, err := c.downloadBytes(ctx, checksums.BrowserDownloadURL, maxChecksumSize)
	if err != nil {
		return Result{}, err
	}
	if err := verifyChecksum(temporaryPath, binary.Name, checksumData); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(temporaryPath, 0o755); err != nil {
		return Result{}, fmt.Errorf("set upgraded executable permission: %w", err)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return Result{}, fmt.Errorf("replace executable %s: %w", target, err)
	}

	return Result{
		Tag:        release.TagName,
		AssetName:  binary.Name,
		TargetPath: target,
	}, nil
}

func (c *Client) get(ctx context.Context, endpoint string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", c.UserAgent)

	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request release metadata: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		response.Body.Close()
		return nil, fmt.Errorf("request %s: http status %s", endpoint, response.Status)
	}
	return response, nil
}

func (c *Client) downloadFile(ctx context.Context, endpoint, destination string) error {
	response, err := c.get(ctx, endpoint)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.ContentLength > maxBinarySize {
		return fmt.Errorf("release asset %s is too large", filepath.Base(destination))
	}

	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open temporary upgrade file: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maxBinarySize+1))
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("download release asset: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close temporary upgrade file: %w", closeErr)
	}
	if written > maxBinarySize {
		return fmt.Errorf("release asset is too large")
	}
	return nil
}

func (c *Client) downloadBytes(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	response, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.ContentLength > limit {
		return nil, fmt.Errorf("checksum asset is too large")
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read checksum asset: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("checksum asset is too large")
	}
	return data, nil
}

func resolveExecutablePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("executable path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlink: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat executable: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("executable path %s is a directory", resolved)
	}
	return resolved, nil
}

func verifyChecksum(path, assetName string, checksumData []byte) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open downloaded executable: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("calculate executable checksum: %w", err)
	}
	actual := hash.Sum(nil)

	var expected string
	scanner := bufio.NewScanner(bytes.NewReader(checksumData))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == assetName {
			expected = fields[0]
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read checksum asset: %w", err)
	}
	if expected == "" {
		return fmt.Errorf("checksum asset has no entry for %s", assetName)
	}
	if len(expected) != sha256.Size*2 {
		return fmt.Errorf("invalid SHA256 checksum for %s", assetName)
	}
	expectedBytes, err := hex.DecodeString(expected)
	if err != nil {
		return fmt.Errorf("decode SHA256 checksum for %s: %w", assetName, err)
	}
	if subtle.ConstantTimeCompare(actual, expectedBytes) != 1 {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return nil
}

// CurrentPlatform 返回当前程序运行平台。
func CurrentPlatform() (string, string) {
	return runtime.GOOS, runtime.GOARCH
}
