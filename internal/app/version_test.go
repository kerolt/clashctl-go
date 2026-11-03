package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kerolt/clashctl-go/internal/output"
)

func TestVersion(t *testing.T) {
	stdout := &bytes.Buffer{}
	application := &App{Printer: output.New(stdout, &bytes.Buffer{})}

	if err := application.version(); err != nil {
		t.Fatalf("version() error = %v", err)
	}
	if !strings.Contains(stdout.String(), Version) {
		t.Fatalf("version output = %q, want version %q", stdout.String(), Version)
	}
}
