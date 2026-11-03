package assets

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
	"testing"
	"time"
)

// TestAmd64Level 核验按 CPU 指令集选择 mihomo 版本。
func TestAmd64Level(t *testing.T) {
	tests := []struct {
		name     string
		flags    string
		expected string
	}{
		{name: "baseline", flags: "fpu sse sse2", expected: "v1"},
		{name: "sse4 and popcnt", flags: "fpu sse sse2 sse4_2 popcnt", expected: "v2"},
		{name: "avx2 and fma", flags: "sse4_2 popcnt avx2 fma", expected: "v3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Amd64Level(tt.flags); got != tt.expected {
				t.Fatalf("Amd64Level(%q) = %q, want %q", tt.flags, got, tt.expected)
			}
		})
	}
}

// TestWithProxy 核验 Github 加速前缀的拼接规则。
func TestWithProxy(t *testing.T) {
	const address = "https://github.com/example/repo/releases/download/v1/file.gz"

	if got := WithProxy("", address); got != address {
		t.Fatalf("WithProxy without prefix = %q", got)
	}
	want := "https://gh-proxy.org/" + address
	if got := WithProxy("https://gh-proxy.org/", address); got != want {
		t.Fatalf("WithProxy with prefix = %q, want %q", got, want)
	}
}

// TestKernelURL 核验内核下载地址。
func TestKernelURL(t *testing.T) {
	got, err := KernelURL("mihomo", "amd64", "v3", "v3-v1.19.25")
	if err != nil {
		t.Fatalf("KernelURL() error = %v", err)
	}
	want := "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.25/mihomo-linux-amd64-v3-v1.19.25.gz"
	if got != want {
		t.Fatalf("KernelURL() = %q, want %q", got, want)
	}

	if _, err := KernelURL("clash", "amd64", "v1", "v1-v1.19.25"); err == nil {
		t.Fatal("KernelURL() error = nil for unsupported kernel")
	}
	if _, err := KernelURL("mihomo", "riscv64", "v1", "v1-v1.19.25"); err == nil {
		t.Fatal("KernelURL() error = nil for unsupported arch")
	}
}

// TestSubconverterURL 核验 subconverter 下载地址。
func TestSubconverterURL(t *testing.T) {
	got, err := SubconverterURL("asdlokj1qpi233/subconverter", "arm64", "v0.9.0")
	if err != nil {
		t.Fatalf("SubconverterURL() error = %v", err)
	}
	want := "https://github.com/asdlokj1qpi233/subconverter/releases/download/v0.9.0/subconverter_aarch64.tar.gz"
	if got != want {
		t.Fatalf("SubconverterURL() = %q, want %q", got, want)
	}
}

// TestExtractGzip 核验单文件 gzip 解压与可执行权限。
func TestExtractGzip(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "kernel.gz")
	if err := writeGzip(source, []byte("#!/bin/sh\nexit 0\n")); err != nil {
		t.Fatalf("prepare gzip: %v", err)
	}

	target := filepath.Join(dir, "bin", "mihomo")
	if err := ExtractGzip(source, target); err != nil {
		t.Fatalf("ExtractGzip() error = %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("target mode = %v, want executable", info.Mode().Perm())
	}
}

// TestExtractTarGz 核验 tar.gz 解压包含文件与目录。
func TestExtractTarGz(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "subconverter.tar.gz")
	if err := writeTarGz(source, map[string]string{
		"subconverter/subconverter":     "binary",
		"subconverter/pref.example.yml": "server:\n  port: 25500\n",
	}); err != nil {
		t.Fatalf("prepare tar.gz: %v", err)
	}

	target := filepath.Join(dir, "bin")
	if err := ExtractTarGz(source, target); err != nil {
		t.Fatalf("ExtractTarGz() error = %v", err)
	}

	for _, name := range []string{"subconverter", "pref.example.yml"} {
		if _, err := os.Stat(filepath.Join(target, "subconverter", name)); err != nil {
			t.Fatalf("missing extracted file %s: %v", name, err)
		}
	}
}

// TestExtractZip 核验 zip 解压保持目录结构。
func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "dist.zip")
	if err := writeZip(source, map[string]string{
		"dist/index.html": "<html></html>",
		"dist/icon.svg":   "<svg/>",
	}); err != nil {
		t.Fatalf("prepare zip: %v", err)
	}

	target := filepath.Join(dir, "resources")
	if err := ExtractZip(source, target); err != nil {
		t.Fatalf("ExtractZip() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "dist", "index.html")); err != nil {
		t.Fatalf("missing extracted page: %v", err)
	}
}

// TestExtractZipRejectsEscape 核验越界路径被拒绝。
func TestExtractZipRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "evil.zip")
	if err := writeZip(source, map[string]string{"../escape.txt": "evil"}); err != nil {
		t.Fatalf("prepare zip: %v", err)
	}

	if err := ExtractZip(source, filepath.Join(dir, "resources")); err == nil {
		t.Fatal("ExtractZip() error = nil for escaping path")
	}
}

// TestDownloadRetries 核验下载失败后重试一次。
func TestDownloadRetries(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Write([]byte("payload"))
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "file.bin")
	client := NewClient(5 * time.Second)
	if err := Download(context.Background(), client, server.URL, target, "clashctl"); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

// writeGzip 生成测试用 gzip 文件。
func writeGzip(path string, content []byte) error {
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

// writeTarGz 生成测试用 tar.gz 文件。
func writeTarGz(path string, files map[string]string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	for name, content := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		}
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

// writeZip 生成测试用 zip 文件。
func writeZip(path string, files map[string]string) error {
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
