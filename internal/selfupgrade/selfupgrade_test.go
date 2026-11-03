package selfupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgrade(t *testing.T) {
	oldBinary := []byte("old binary")
	newBinary := []byte("new binary")
	checksum := sha256.Sum256(newBinary)
	checksumText := hex.EncodeToString(checksum[:]) + "  clashctl-linux-amd64\n"

	root := t.TempDir()
	target := filepath.Join(root, "clashctl")
	if err := os.WriteFile(target, oldBinary, 0o755); err != nil {
		t.Fatalf("write old executable: %v", err)
	}

	var releasePath string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/kerolt/clashctl-go/releases/latest":
			releasePath = request.URL.Path
			writeJSON(t, writer, Release{
				TagName: "v1.2.3",
				Assets: []Asset{
					{Name: "clashctl-linux-amd64", BrowserDownloadURL: "http://" + request.Host + "/downloads/clashctl-linux-amd64"},
					{Name: "SHA256SUMS", BrowserDownloadURL: "http://" + request.Host + "/downloads/SHA256SUMS"},
				},
			})
		case "/downloads/clashctl-linux-amd64":
			writer.Write(newBinary)
		case "/downloads/SHA256SUMS":
			writer.Write([]byte(checksumText))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.APIBaseURL = server.URL
	result, err := client.Upgrade(t.Context(), "", target, "linux", "amd64")
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if result.Tag != "v1.2.3" {
		t.Fatalf("Tag = %q, want v1.2.3", result.Tag)
	}
	if releasePath != "/repos/kerolt/clashctl-go/releases/latest" {
		t.Fatalf("release path = %q", releasePath)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read upgraded executable: %v", err)
	}
	if string(content) != string(newBinary) {
		t.Fatalf("executable content = %q, want %q", content, newBinary)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat upgraded executable: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("executable mode = %o, want 755", info.Mode().Perm())
	}
}

func TestResolveReleaseByTag(t *testing.T) {
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestPath = request.URL.Path
		writeJSON(t, writer, Release{TagName: "v2.0.0"})
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.APIBaseURL = server.URL
	release, err := client.ResolveRelease(t.Context(), "v2.0.0")
	if err != nil {
		t.Fatalf("ResolveRelease() error = %v", err)
	}
	if release.TagName != "v2.0.0" {
		t.Fatalf("TagName = %q, want v2.0.0", release.TagName)
	}
	if requestPath != "/repos/kerolt/clashctl-go/releases/tags/v2.0.0" {
		t.Fatalf("request path = %q", requestPath)
	}
}

func TestSelectAssets(t *testing.T) {
	tests := []struct {
		name    string
		release Release
		goos    string
		goarch  string
		want    string
	}{
		{
			name: "select linux amd64",
			release: Release{
				TagName: "v1.0.0",
				Assets: []Asset{
					{Name: "clashctl-linux-amd64", BrowserDownloadURL: "https://example.com/amd64"},
					{Name: "SHA256SUMS", BrowserDownloadURL: "https://example.com/checksums"},
				},
			},
			goos:   "linux",
			goarch: "amd64",
			want:   "clashctl-linux-amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary, checksums, err := SelectAssets(tt.release, tt.goos, tt.goarch)
			if err != nil {
				t.Fatalf("SelectAssets() error = %v", err)
			}
			if binary.Name != tt.want {
				t.Fatalf("binary = %q, want %q", binary.Name, tt.want)
			}
			if checksums.Name != "SHA256SUMS" {
				t.Fatalf("checksums = %q, want SHA256SUMS", checksums.Name)
			}
		})
	}

	if _, _, err := SelectAssets(Release{TagName: "v1.0.0"}, "darwin", "amd64"); err == nil {
		t.Fatal("SelectAssets() error = nil for unsupported operating system")
	}
}

func TestUpgradeRejectsChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "clashctl")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("write old executable: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/kerolt/clashctl-go/releases/latest":
			writeJSON(t, writer, Release{
				TagName: "v1.0.0",
				Assets: []Asset{
					{Name: "clashctl-linux-amd64", BrowserDownloadURL: "http://" + request.Host + "/binary"},
					{Name: "SHA256SUMS", BrowserDownloadURL: "http://" + request.Host + "/checksums"},
				},
			})
		case "/binary":
			writer.Write([]byte("new"))
		case "/checksums":
			writer.Write([]byte(strings.Repeat("0", sha256.Size*2) + "  clashctl-linux-amd64\n"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.APIBaseURL = server.URL
	if _, err := client.Upgrade(t.Context(), "", target, "linux", "amd64"); err == nil {
		t.Fatal("Upgrade() error = nil for checksum mismatch")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read executable after failed upgrade: %v", err)
	}
	if string(content) != "old" {
		t.Fatalf("executable changed after failed upgrade: %q", content)
	}
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
}
