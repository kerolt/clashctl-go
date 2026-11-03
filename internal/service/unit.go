package service

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed templates/*.tmpl
var unitTemplates embed.FS

// UnitParams 保存渲染服务单元所需的参数。
type UnitParams struct {
	Kernel      string
	Description string
	CommandPath string
	CommandArgs string
	CommandFull string
	LogPath     string
	PIDPath     string
	// IsUser 表示用户级 systemd 单元，需要去掉特权设置。
	IsUser    bool
	FileLimit int
}

// RenderUnit 按服务管理器渲染单元文件内容。
func RenderUnit(manager string, params UnitParams) (string, error) {
	name := manager + ".tmpl"
	if manager == "nohup" {
		return "", fmt.Errorf("nohup 模式不生成服务单元")
	}

	content, err := unitTemplates.ReadFile("templates/" + name)
	if err != nil {
		return "", fmt.Errorf("读取 %s 服务模板: %w", manager, err)
	}

	rendered := strings.NewReplacer(
		"__KERNEL_NAME__", params.Kernel,
		"__KERNEL_DESC__", params.Description,
		"__KERNEL_PATH__", params.CommandPath,
		"__KERNEL_ARGS__", params.CommandArgs,
		"__KERNEL_CMD__", params.CommandFull,
		"__LOG_PATH__", params.LogPath,
		"__PID_PATH__", params.PIDPath,
	).Replace(string(content))

	if manager != "systemd" || !params.IsUser {
		return rendered, nil
	}
	return trimUserUnit(rendered, params.FileLimit), nil
}

// trimUserUnit 去掉用户级 systemd 单元不支持的特权设置。
func trimUserUnit(content string, fileLimit int) string {
	if fileLimit <= 0 {
		fileLimit = 524288
	}

	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "CapabilityBoundingSet="):
			continue
		case strings.HasPrefix(line, "AmbientCapabilities="):
			continue
		case strings.HasPrefix(line, "LimitNOFILE="):
			kept = append(kept, fmt.Sprintf("LimitNOFILE=%d", fileLimit))
			continue
		case line == "WantedBy=multi-user.target":
			kept = append(kept, "WantedBy=default.target")
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
