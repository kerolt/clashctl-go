package service

import (
	"strings"
	"testing"
)

// TestRenderUnitSystemd 核验系统级 systemd 单元保留特权设置。
func TestRenderUnitSystemd(t *testing.T) {
	content, err := RenderUnit("systemd", UnitParams{
		Kernel:      "mihomo",
		Description: "mihomo Daemon",
		CommandPath: "/opt/clashctl/bin/mihomo",
		CommandArgs: "-d /opt/clashctl/resources -f /opt/clashctl/resources/runtime.yaml",
		CommandFull: "/opt/clashctl/bin/mihomo -d /opt/clashctl/resources",
		LogPath:     "/var/log/mihomo.log",
		PIDPath:     "/run/mihomo.pid",
	})
	if err != nil {
		t.Fatalf("RenderUnit() error = %v", err)
	}

	for _, expected := range []string{
		"CapabilityBoundingSet=",
		"AmbientCapabilities=",
		"WantedBy=multi-user.target",
		"ExecStart=/opt/clashctl/bin/mihomo -d /opt/clashctl/resources",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("systemd unit missing %q", expected)
		}
	}
}

// TestRenderUnitUserSystemd 核验用户级 systemd 单元去掉特权设置。
func TestRenderUnitUserSystemd(t *testing.T) {
	content, err := RenderUnit("systemd", UnitParams{
		Kernel:      "mihomo",
		Description: "mihomo Daemon",
		CommandPath: "/opt/clashctl/bin/mihomo",
		CommandFull: "/opt/clashctl/bin/mihomo -d /opt/clashctl/resources",
		IsUser:      true,
		FileLimit:   65536,
	})
	if err != nil {
		t.Fatalf("RenderUnit() error = %v", err)
	}

	for _, unexpected := range []string{"CapabilityBoundingSet=", "AmbientCapabilities=", "WantedBy=multi-user.target"} {
		if strings.Contains(content, unexpected) {
			t.Fatalf("user unit still contains %q", unexpected)
		}
	}
	for _, expected := range []string{"WantedBy=default.target", "LimitNOFILE=65536"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("user unit missing %q", expected)
		}
	}
}

// TestRenderUnitOpenRC 核验 OpenRC 脚本填入占位符。
func TestRenderUnitOpenRC(t *testing.T) {
	content, err := RenderUnit("openrc", UnitParams{
		Kernel:      "mihomo",
		Description: "mihomo Daemon",
		CommandPath: "/opt/clashctl/bin/mihomo",
		CommandArgs: "-d /opt/clashctl/resources",
		LogPath:     "/var/log/mihomo.log",
		PIDPath:     "/run/mihomo.pid",
	})
	if err != nil {
		t.Fatalf("RenderUnit() error = %v", err)
	}
	if strings.Contains(content, "__") {
		t.Fatalf("openrc unit contains unresolved placeholder:\n%s", content)
	}
	if !strings.Contains(content, `command="/opt/clashctl/bin/mihomo"`) {
		t.Fatalf("openrc unit missing command path")
	}
}

// TestRenderUnitRejectsNohup 核验 nohup 模式不生成单元文件。
func TestRenderUnitRejectsNohup(t *testing.T) {
	if _, err := RenderUnit("nohup", UnitParams{}); err == nil {
		t.Fatal("RenderUnit() error = nil for nohup")
	}
}
