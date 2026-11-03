// Package subscription 管理订阅配置和 subconverter 下载流程。
package subscription

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/nelvko/clashctl-go/internal/config"
	"github.com/nelvko/clashctl-go/internal/process"
	"gopkg.in/yaml.v3"
)

// Profile 描述一条已保存的订阅。
type Profile struct {
	ID   int    `yaml:"id"`
	Path string `yaml:"path"`
	URL  string `yaml:"url"`
}

// Metadata 保存全部订阅与当前使用的订阅 id。
type Metadata struct {
	Profiles []Profile `yaml:"profiles"`
	Use      int       `yaml:"use,omitempty"`
}

// Manager 保存订阅路径、HTTP 参数与内核校验所需信息。
type Manager struct {
	Paths      config.Paths
	KernelPath string
	Timeout    time.Duration
	UserAgent  string
	HTTPClient *http.Client
}

// NewManager 构造成阅管理器。
func NewManager(paths config.Paths, kernelPath string, timeout time.Duration, userAgent string) *Manager {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // 允许不校验证书，适配自建下载代理
	}
	return &Manager{
		Paths:      paths,
		KernelPath: kernelPath,
		Timeout:    timeout,
		UserAgent:  userAgent,
		HTTPClient: &http.Client{Transport: transport, Timeout: timeout},
	}
}

// LoadMetadata 读取订阅元数据，文件不存在时返回空列表。
func (m *Manager) LoadMetadata() (Metadata, error) {
	data, err := os.ReadFile(m.Paths.ProfilesMeta)
	if errors.Is(err, os.ErrNotExist) {
		return Metadata{Profiles: []Profile{}}, nil
	}
	if err != nil {
		return Metadata{}, fmt.Errorf("read profile metadata: %w", err)
	}
	var metadata Metadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return Metadata{}, fmt.Errorf("parse profile metadata: %w", err)
	}
	if metadata.Profiles == nil {
		metadata.Profiles = []Profile{}
	}
	return metadata, nil
}

// SaveMetadata 把订阅元数据写入配置的元数据文件。
func (m *Manager) SaveMetadata(metadata Metadata) error {
	data, err := yaml.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode profile metadata: %w", err)
	}
	if err := os.WriteFile(m.Paths.ProfilesMeta, data, 0o644); err != nil {
		return fmt.Errorf("write profile metadata: %w", err)
	}
	return nil
}

// Add 下载、校验并保存一条新订阅。
func (m *Manager) Add(ctx context.Context, sourceURL string) (Profile, error) {
	// 先检查重复订阅，再下载到临时文件。
	metadata, err := m.LoadMetadata()
	if err != nil {
		return Profile{}, err
	}
	for _, profile := range metadata.Profiles {
		if profile.URL == sourceURL {
			return Profile{}, fmt.Errorf("subscription already exists: [%d] %s", profile.ID, sourceURL)
		}
	}

	if err := m.DownloadConfig(ctx, m.Paths.TemporaryConfig, sourceURL, false); err != nil {
		return Profile{}, err
	}
	if err := config.Validate(ctx, m.KernelPath, m.Paths.TemporaryConfig); err != nil {
		return Profile{}, fmt.Errorf("invalid subscription: %w", err)
	}

	id := nextID(metadata.Profiles)
	profile := Profile{
		ID:   id,
		Path: filepath.Join(m.Paths.ProfilesDir, strconv.Itoa(id)+".yaml"),
		URL:  sourceURL,
	}
	if err := os.MkdirAll(m.Paths.ProfilesDir, 0o755); err != nil {
		return Profile{}, fmt.Errorf("create profile directory: %w", err)
	}
	if err := os.Rename(m.Paths.TemporaryConfig, profile.Path); err != nil {
		return Profile{}, fmt.Errorf("save subscription profile: %w", err)
	}
	metadata.Profiles = append(metadata.Profiles, profile)
	if err := m.SaveMetadata(metadata); err != nil {
		return Profile{}, err
	}
	return profile, m.log("➕ 已添加订阅：[%d] %s", profile.ID, profile.URL)
}

// Delete 删除一条未被使用的订阅。
func (m *Manager) Delete(id int) (Profile, error) {
	metadata, err := m.LoadMetadata()
	if err != nil {
		return Profile{}, err
	}
	index := profileIndex(metadata.Profiles, id)
	if index < 0 {
		return Profile{}, fmt.Errorf("subscription id does not exist")
	}
	if metadata.Use == id {
		return Profile{}, fmt.Errorf("subscription %d is currently in use", id)
	}
	profile := metadata.Profiles[index]
	if err := os.Remove(profile.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Profile{}, fmt.Errorf("remove subscription profile: %w", err)
	}
	metadata.Profiles = append(metadata.Profiles[:index], metadata.Profiles[index+1:]...)
	if err := m.SaveMetadata(metadata); err != nil {
		return Profile{}, err
	}
	return profile, m.log("➖ 已删除订阅：[%d] %s", profile.ID, profile.URL)
}

// Update 下载并校验一条已存在的订阅。
func (m *Manager) Update(ctx context.Context, id int, convert bool) (Profile, error) {
	metadata, err := m.LoadMetadata()
	if err != nil {
		return Profile{}, err
	}
	index := profileIndex(metadata.Profiles, id)
	if index < 0 {
		return Profile{}, fmt.Errorf("subscription id does not exist")
	}
	profile := metadata.Profiles[index]
	if err := m.DownloadConfig(ctx, m.Paths.TemporaryConfig, profile.URL, convert); err != nil {
		_ = m.log("❌ 订阅更新失败：[%d] %s", profile.ID, profile.URL)
		return Profile{}, err
	}
	if err := config.Validate(ctx, m.KernelPath, m.Paths.TemporaryConfig); err != nil {
		_ = m.log("❌ 订阅更新失败：[%d] %s", profile.ID, profile.URL)
		return Profile{}, fmt.Errorf("invalid updated subscription: %w", err)
	}
	if err := copyFile(m.Paths.TemporaryConfig, profile.Path); err != nil {
		return Profile{}, fmt.Errorf("save updated profile: %w", err)
	}
	if err := m.log("✅ 订阅更新成功：[%d] %s", profile.ID, profile.URL); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// DownloadConfig 获取、标准化、转换并校验订阅文件。
func (m *Manager) DownloadConfig(ctx context.Context, destination, sourceURL string, convert bool) error {
	if convert {
		if err := m.convertConfig(ctx, destination, sourceURL); err != nil {
			return err
		}
		return normalize(destination)
	}
	if err := m.downloadRaw(ctx, destination, sourceURL); err != nil {
		return err
	}

	// 标准化编码后再判断原生配置和订阅转换路径。
	if err := normalize(destination); err != nil {
		return err
	}
	if isHTML(destination) {
		return fmt.Errorf("subscription response looks like an html page")
	}

	if isNative(destination) {
		if err := config.Validate(ctx, m.KernelPath, destination); err == nil && hasNodes(destination) {
			return nil
		}
		if err := copyFile(destination, destination+".raw"); err != nil {
			return fmt.Errorf("save raw subscription: %w", err)
		}
		if err := m.convertConfig(ctx, destination, sourceURL); err != nil {
			return err
		}
		if err := normalize(destination); err != nil {
			return err
		}
		return m.validateNodes(destination)
	}

	if err := config.Validate(ctx, m.KernelPath, destination); err == nil && hasNodes(destination) {
		return nil
	}
	if err := copyFile(destination, destination+".raw"); err != nil {
		return fmt.Errorf("save raw subscription: %w", err)
	}
	if err := m.convertConfig(ctx, destination, sourceURL); err != nil {
		return err
	}
	if err := normalize(destination); err != nil {
		return err
	}
	return m.validateNodes(destination)
}

// validateNodes 确认订阅至少包含一个节点或节点提供者。
func (m *Manager) validateNodes(path string) error {
	if !hasNodes(path) {
		return fmt.Errorf("subscription did not contain any proxy nodes")
	}
	return nil
}

// downloadRaw 下载一份订阅响应，或读取 file 地址指向的文件。
func (m *Manager) downloadRaw(ctx context.Context, destination, sourceURL string) error {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return fmt.Errorf("parse subscription url: %w", err)
	}
	if parsed.Scheme == "file" {
		return copyFile(parsed.Path, destination)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return fmt.Errorf("create subscription request: %w", err)
	}
	request.Header.Set("User-Agent", m.UserAgent)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		response, requestErr := m.HTTPClient.Do(request)
		if requestErr != nil {
			lastErr = requestErr
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			lastErr = fmt.Errorf("http status %s", response.Status)
			continue
		}
		file, createErr := os.Create(destination)
		if createErr != nil {
			response.Body.Close()
			return fmt.Errorf("create subscription file: %w", createErr)
		}
		_, copyErr := io.Copy(file, response.Body)
		closeErr := file.Close()
		response.Body.Close()
		if copyErr != nil {
			lastErr = copyErr
			continue
		}
		if closeErr != nil {
			return fmt.Errorf("close subscription file: %w", closeErr)
		}
		return nil
	}
	return fmt.Errorf("download subscription: %w", lastErr)
}

// convertConfig 启动或复用 subconverter，并写入转换后的配置。
func (m *Manager) convertConfig(ctx context.Context, destination, sourceURL string) error {
	// 转换流程结束时停止 subconverter，避免遗留后台进程。
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return fmt.Errorf("parse subscription url: %w", err)
	}
	if parsed.Scheme == "file" {
		return nil
	}
	port, err := m.converterPort()
	if err != nil {
		return err
	}
	defer func() {
		_ = process.TerminateByName(ctx, "subconverter", 200*time.Millisecond)
	}()
	checkURL := fmt.Sprintf("http://127.0.0.1:%d/version", port)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err == nil {
		response, requestErr := m.HTTPClient.Do(request)
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return m.fetchConverted(ctx, destination, sourceURL, port)
			}
		}
	}

	logFile, err := os.OpenFile(m.Paths.SubconverterLog, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open subconverter log: %w", err)
	}
	defer logFile.Close()
	command := exec.Command(m.Paths.SubconverterPath)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		return fmt.Errorf("start subconverter: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
		if requestErr != nil {
			return requestErr
		}
		response, requestErr := m.HTTPClient.Do(request)
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return m.fetchConverted(ctx, destination, sourceURL, port)
			}
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("subconverter did not start; check log %s", m.Paths.SubconverterLog)
}

// fetchConverted 从 subconverter 请求 Clash 格式配置。
func (m *Manager) fetchConverted(ctx context.Context, destination, sourceURL string, port int) error {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/sub", port)
	query := url.Values{}
	query.Set("target", "clash")
	query.Set("url", sourceURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return fmt.Errorf("create converter request: %w", err)
	}
	request.Header.Set("User-Agent", m.UserAgent)
	response, err := m.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("convert subscription: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("convert subscription: http status %s", response.Status)
	}
	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create converted subscription: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, response.Body); err != nil {
		return fmt.Errorf("save converted subscription: %w", err)
	}
	return nil
}

// converterPort 读取 subconverter 端口，被占用时选择并写回新端口。
func (m *Manager) converterPort() (int, error) {
	// 读取配置端口，冲突时写入新的随机端口。
	value, err := config.Value(m.Paths.SubconverterConf, "server", "port")
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid subconverter port: %w", err)
	}
	if !portUsed(port) {
		return port, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("choose subconverter port: %w", err)
	}
	newPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	if err := config.SetInt(m.Paths.SubconverterConf, []string{"server", "port"}, newPort); err != nil {
		return 0, err
	}
	return newPort, nil
}

// log 向订阅日志追加一条操作记录。
func (m *Manager) log(format string, args ...any) error {
	// 订阅日志使用本地文本格式，便于直接查看。
	file, err := os.OpenFile(m.Paths.ProfilesLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open profile log: %w", err)
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	return err
}

// nextID 返回当前最大订阅 id 之后的下一个 id。
func nextID(profiles []Profile) int {
	id := 0
	for _, profile := range profiles {
		if profile.ID > id {
			id = profile.ID
		}
	}
	return id + 1
}

// profileIndex 返回订阅 id 在列表中的位置，不存在时返回 -1。
func profileIndex(profiles []Profile, id int) int {
	for index, profile := range profiles {
		if profile.ID == id {
			return index
		}
	}
	return -1
}

// copyFile 把源文件的完整内容复制到目标路径。
func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o644)
}

// normalize 修正订阅内容的常见格式问题，并转换遗留编码。
func normalize(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data = bytesTrimUTF8BOM(data)
	data = bytes.ReplaceAll(data, []byte{'\r'}, nil)
	if utf8.Valid(data) {
		return os.WriteFile(path, data, 0o644)
	}
	if _, err := exec.LookPath("iconv"); err != nil {
		return os.WriteFile(path, data, 0o644)
	}
	for _, charset := range []string{"GB18030", "GBK", "BIG5"} {
		command := exec.Command("iconv", "-f", charset, "-t", "UTF-8")
		command.Stdin = bytes.NewReader(data)
		converted, runErr := command.Output()
		if runErr == nil && utf8.Valid(converted) {
			return os.WriteFile(path, converted, 0o644)
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// isHTML 判断响应是否包含 HTML 文档标记。
func isHTML(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && htmlPattern.Match(data)
}

// isNative 判断 YAML 文件是否包含原生代理数据。
func isNative(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var document yaml.Node
	if yaml.Unmarshal(data, &document) != nil || len(document.Content) == 0 {
		return false
	}
	root := document.Content[0]
	proxies := mappingValue(root, "proxies")
	providers := mappingValue(root, "proxy-providers")
	return proxies != nil && proxies.Kind == yaml.SequenceNode && len(proxies.Content) > 0 || providers != nil && providers.Kind == yaml.MappingNode && len(providers.Content) > 0
}

// hasNodes 判断 YAML 文件是否包含至少一个节点或提供者。
func hasNodes(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var document yaml.Node
	if yaml.Unmarshal(data, &document) != nil || len(document.Content) == 0 {
		return false
	}
	root := document.Content[0]
	proxies := mappingValue(root, "proxies")
	providers := mappingValue(root, "proxy-providers")
	return proxies != nil && proxies.Kind == yaml.SequenceNode && len(proxies.Content) > 0 || providers != nil && providers.Kind == yaml.MappingNode && len(providers.Content) > 0
}

// mappingValue 从 YAML 映射中取出一个值。
func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

// portUsed 判断本地 TCP 端口是否已可连接。
func portUsed(port int) bool {
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
	if err != nil {
		return false
	}
	connection.Close()
	return true
}

// bytesTrimUTF8BOM 去掉 UTF-8 BOM。
func bytesTrimUTF8BOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
}

// utf8Valid 判断数据是否为合法 UTF-8。
func utf8Valid(data []byte) bool {
	return utf8.Valid(data)
}

var htmlPattern = regexp.MustCompile(`(?i)<\s*(!doctype|html|head|body|title)([\s>]|$)`)
