package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/kerolt/clashctl-go/internal/output"
)

func TestSelfUpgradeHelp(t *testing.T) {
	stderr := &bytes.Buffer{}
	application := &App{Printer: output.New(&bytes.Buffer{}, stderr)}

	if err := application.selfUpgrade(context.Background(), []string{"--help"}); err != nil {
		t.Fatalf("selfUpgrade(--help) error = %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage of self-upgrade:") {
		t.Fatalf("self-upgrade help missing usage:\n%s", stderr.String())
	}
}

func TestSelfUpgradeRejectsPositionalArgument(t *testing.T) {
	application := &App{Printer: output.New(&bytes.Buffer{}, &bytes.Buffer{})}

	if err := application.selfUpgrade(context.Background(), []string{"v1.0.0"}); err == nil {
		t.Fatal("selfUpgrade() error = nil for positional argument")
	}
}
