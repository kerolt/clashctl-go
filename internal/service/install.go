package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/kerolt/clashctl-go/internal/process"
)

// InstallUnit 写入并启用当前服务管理器的服务单元，返回单元文件路径。
func (m *Manager) InstallUnit(ctx context.Context) (string, error) {
	run := m.Command
	if run == nil {
		run = process.Run
	}

	// 首次安装时还没有单元文件，非 root 用户在可用 systemd 主机上注册用户级单元。
	if m.Manager == "nohup" {
		if os.Geteuid() != 0 && systemctlUserAvailable() {
			m.Manager = "systemd"
			m.SystemdScope = "--user"
			m.LogPath = filepath.Join(m.Paths.Resources, m.Kernel+".log")
		} else {
			// nohup 方式由进程直接托管，没有单元文件需要写入。
			return "", nil
		}
	}

	params := UnitParams{
		Kernel:      m.Kernel,
		Description: m.Kernel + " Daemon (clashctl)",
		CommandPath: m.KernelPath,
		CommandArgs: fmt.Sprintf("-d %s -f %s", m.Paths.Resources, m.Paths.RuntimeConfig),
		CommandFull: fmt.Sprintf("%s -d %s -f %s", m.KernelPath, m.Paths.Resources, m.Paths.RuntimeConfig),
		LogPath:     m.LogPath,
		PIDPath:     m.PIDPath(),
		IsUser:      m.SystemdScope != "",
		FileLimit:   fileLimit(),
	}

	target, err := m.unitPath()
	if err != nil {
		return "", err
	}

	content, err := RenderUnit(m.Manager, params)
	if err != nil {
		return "", err
	}

	mode := os.FileMode(0o644)
	if m.Manager != "systemd" {
		mode = 0o755
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("创建服务目录: %w", err)
	}
	if err := os.WriteFile(target, []byte(content), mode); err != nil {
		return "", fmt.Errorf("写入服务单元 %s: %w", target, err)
	}
	if err := os.Chmod(target, mode); err != nil {
		return "", fmt.Errorf("设置服务单元权限: %w", err)
	}

	if err := m.enableUnit(ctx, run); err != nil {
		return "", err
	}
	return target, nil
}

// unitPath 返回当前服务管理器的单元文件路径。
func (m *Manager) unitPath() (string, error) {
	switch m.Manager {
	case "systemd":
		if m.SystemdScope == "" {
			return filepath.Join("/etc/systemd/system", m.Kernel+".service"), nil
		}
		return userUnitPath(m.Kernel), nil
	case "sysvinit", "openrc":
		return filepath.Join("/etc/init.d", m.Kernel), nil
	case "runit":
		return filepath.Join("/etc/sv", m.Kernel, "run"), nil
	}
	return "", fmt.Errorf("服务管理器 %s 不写入单元文件", m.Manager)
}

// enableUnit 让服务单元在开机时启动。
func (m *Manager) enableUnit(ctx context.Context, run func(context.Context, string, ...string) error) error {
	switch m.Manager {
	case "systemd":
		args := []string{"daemon-reload"}
		if m.SystemdScope != "" {
			args = append([]string{m.SystemdScope}, args...)
		}
		if err := run(ctx, "systemctl", args...); err != nil {
			return fmt.Errorf("重载 systemd 配置: %w", err)
		}

		enableArgs := []string{"enable", "--quiet", m.Kernel}
		if m.SystemdScope != "" {
			enableArgs = append([]string{m.SystemdScope}, enableArgs...)
		}
		if err := run(ctx, "systemctl", enableArgs...); err != nil {
			return fmt.Errorf("设置开机自启: %w", err)
		}
		return nil
	case "openrc":
		if err := run(ctx, "rc-update", "add", m.Kernel, "default"); err != nil {
			return fmt.Errorf("设置开机自启: %w", err)
		}
		return nil
	case "sysvinit":
		if _, err := exec.LookPath("chkconfig"); err == nil {
			if err := run(ctx, "chkconfig", "--add", m.Kernel); err != nil {
				return fmt.Errorf("注册 SysVinit 服务: %w", err)
			}
			if err := run(ctx, "chkconfig", m.Kernel, "on"); err != nil {
				return fmt.Errorf("设置开机自启: %w", err)
			}
			return nil
		}
		if _, err := exec.LookPath("update-rc.d"); err == nil {
			if err := run(ctx, "update-rc.d", m.Kernel, "defaults"); err != nil {
				return fmt.Errorf("注册 SysVinit 服务: %w", err)
			}
			if err := run(ctx, "update-rc.d", m.Kernel, "enable"); err != nil {
				return fmt.Errorf("设置开机自启: %w", err)
			}
			return nil
		}
		return fmt.Errorf("未找到 SysVinit 服务管理命令：chkconfig / update-rc.d")
	case "runit":
		link := filepath.Join("/etc/runit/runsvdir/default", m.Kernel)
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return fmt.Errorf("创建 runit 自启目录: %w", err)
		}
		if err := os.Symlink(filepath.Join("/etc/sv", m.Kernel), link); err != nil && !os.IsExist(err) {
			return fmt.Errorf("设置开机自启: %w", err)
		}
		return nil
	}
	return nil
}

// PIDPath 返回当前服务管理器使用的 pid 文件路径。
func (m *Manager) PIDPath() string {
	if m.Manager == "nohup" || m.SystemdScope != "" {
		return filepath.Join(m.Paths.Resources, m.Kernel+".pid")
	}
	return filepath.Join("/run", m.Kernel+".pid")
}

// fileLimit 返回当前进程的打开文件上限。
func fileLimit() int {
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return 524288
	}
	return int(limit.Max)
}
