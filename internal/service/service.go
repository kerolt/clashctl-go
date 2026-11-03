// Package service 通过主机服务管理器管理 mihomo 进程。
package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nelvko/clashctl-go/internal/config"
	"github.com/nelvko/clashctl-go/internal/process"
)

// Manager stores the detected service manager and mihomo paths.
type Manager struct {
	Paths        config.Paths
	Kernel       string
	KernelPath   string
	Manager      string
	SystemdScope string
	LogPath      string
	// Command 执行服务管理命令，测试可以替换。
	Command func(ctx context.Context, name string, args ...string) error
}

// New constructs a service Manager and detects the available service manager.
func New(paths config.Paths, kernel string) *Manager {
	manager := &Manager{
		Paths:      paths,
		Kernel:     kernel,
		KernelPath: paths.KernelPath,
		Command:    process.Run,
	}
	manager.detect()
	return manager
}

// detect selects the service manager and log path for the current environment.
func (m *Manager) detect() {
	// 先判断系统级 init，再检查当前用户的 systemd 服务。
	initType := "nohup"
	if target, err := os.Readlink("/proc/1/exe"); err == nil {
		initType = filepath.Base(target)
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		initType = "nohup"
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		text := string(data)
		if strings.Contains(text, "docker") || strings.Contains(text, "kubepods") || strings.Contains(text, "containerd") || strings.Contains(text, "podman") || strings.Contains(text, "lxc") {
			initType = "nohup"
		}
	}

	switch {
	case strings.Contains(initType, "systemd"):
		m.Manager = "systemd"
	case strings.Contains(initType, "openrc"):
		m.Manager = "openrc"
	case strings.Contains(initType, "runit"):
		m.Manager = "runit"
	case strings.HasSuffix(initType, "init"):
		m.Manager = "sysvinit"
	default:
		m.Manager = "nohup"
	}

	if m.Manager == "nohup" && userSystemdAvailable(m.Kernel) {
		m.Manager = "systemd"
		m.SystemdScope = "--user"
	}
	if m.Manager == "systemd" && os.Geteuid() != 0 {
		m.SystemdScope = "--user"
	}

	m.LogPath = filepath.Join("/var", "log", m.Kernel+".log")
	if m.Manager == "nohup" || m.SystemdScope != "" {
		// 普通用户和 nohup 服务只能写入安装目录。
		m.LogPath = filepath.Join(m.Paths.Resources, m.Kernel+".log")
	}
}

// userSystemdAvailable reports whether a user service unit can be controlled.
func userSystemdAvailable(kernel string) bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	if err := exec.Command("systemctl", "--user", "show-environment").Run(); err != nil {
		return false
	}
	unitPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "systemd", "user", kernel+".service")
	if os.Getenv("XDG_CONFIG_HOME") == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		unitPath = filepath.Join(home, ".config", "systemd", "user", kernel+".service")
	}
	_, err := os.Stat(unitPath)
	return err == nil
}

// systemctl runs systemctl with the detected user scope when required.
func (m *Manager) systemctl(ctx context.Context, args ...string) error {
	commandArgs := make([]string, 0, len(args)+1)
	if m.SystemdScope != "" {
		commandArgs = append(commandArgs, m.SystemdScope)
	}
	commandArgs = append(commandArgs, args...)
	return process.Run(ctx, "systemctl", commandArgs...)
}

// systemctlOutput runs systemctl and sends both output streams to writer.
func (m *Manager) systemctlOutput(ctx context.Context, writer io.Writer, args ...string) error {
	commandArgs := make([]string, 0, len(args)+1)
	if m.SystemdScope != "" {
		commandArgs = append(commandArgs, m.SystemdScope)
	}
	commandArgs = append(commandArgs, args...)
	command := exec.CommandContext(ctx, "systemctl", commandArgs...)
	command.Stdout = writer
	command.Stderr = writer
	return command.Run()
}

// Start starts mihomo through the detected service manager.
func (m *Manager) Start(ctx context.Context) error {
	// 服务管理器负责托管的进程由对应命令启动。
	switch m.Manager {
	case "systemd":
		return m.systemctl(ctx, "start", m.Kernel)
	case "sysvinit":
		return process.Run(ctx, "service", m.Kernel, "start")
	case "openrc":
		return process.Run(ctx, "rc-service", m.Kernel, "start")
	case "runit":
		return process.Run(ctx, "sv", "up", m.Kernel)
	default:
		if err := os.MkdirAll(filepath.Dir(m.LogPath), 0o755); err != nil {
			return fmt.Errorf("create log directory: %w", err)
		}
		logFile, err := os.OpenFile(m.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("open service log: %w", err)
		}
		defer logFile.Close()
		_, err = process.StartDetached(
			"nohup",
			append([]string{m.KernelPath}, "-d", m.Paths.Resources, "-f", m.Paths.RuntimeConfig),
			logFile,
			logFile,
		)
		return err
	}
}

// SudoStart starts mihomo with elevated privileges for TUN mode.
func (m *Manager) SudoStart(ctx context.Context) error {
	if os.Geteuid() == 0 {
		return m.Start(ctx)
	}
	if err := os.MkdirAll(filepath.Dir(m.LogPath), 0o755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	logFile, err := os.OpenFile(m.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open service log: %w", err)
	}
	defer logFile.Close()
	command := exec.CommandContext(
		ctx,
		"sudo",
		"nohup",
		m.KernelPath,
		"-d",
		m.Paths.Resources,
		"-f",
		m.Paths.RuntimeConfig,
	)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		return fmt.Errorf("start privileged service: %w", err)
	}
	return nil
}

// Stop stops mihomo through the detected service manager.
func (m *Manager) Stop(ctx context.Context) error {
	switch m.Manager {
	case "systemd":
		return m.systemctl(ctx, "stop", m.Kernel)
	case "sysvinit":
		return process.Run(ctx, "service", m.Kernel, "stop")
	case "openrc":
		return process.Run(ctx, "rc-service", m.Kernel, "stop")
	case "runit":
		return process.Run(ctx, "sv", "down", m.Kernel)
	default:
		return process.TerminateByName(ctx, m.Kernel, 200*time.Millisecond)
	}
}

// SudoStop stops a privileged mihomo process used by TUN mode.
func (m *Manager) SudoStop(ctx context.Context) error {
	if os.Geteuid() == 0 {
		return m.Stop(ctx)
	}
	if err := exec.CommandContext(ctx, "sudo", "pkill", "-TERM", "-x", m.Kernel).Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); !ok || exitError.ExitCode() != 1 {
			return fmt.Errorf("stop privileged service: %w", err)
		}
	}
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	if err := exec.CommandContext(ctx, "sudo", "pkill", "-KILL", "-x", m.Kernel).Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); !ok || exitError.ExitCode() != 1 {
			return fmt.Errorf("force stop privileged service: %w", err)
		}
	}
	return nil
}

// Restart restarts mihomo through the detected service manager.
func (m *Manager) Restart(ctx context.Context) error {
	switch m.Manager {
	case "systemd":
		return m.systemctl(ctx, "restart", m.Kernel)
	case "sysvinit":
		return process.Run(ctx, "service", m.Kernel, "restart")
	case "openrc":
		return process.Run(ctx, "rc-service", m.Kernel, "restart")
	case "runit":
		return process.Run(ctx, "sv", "restart", m.Kernel)
	default:
		if err := m.Stop(ctx); err != nil {
			return err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		return m.Start(ctx)
	}
}

// IsActive reports whether mihomo is currently active.
func (m *Manager) IsActive(ctx context.Context) (bool, error) {
	// 用户级 systemd 还需要兼容 sudo 启动的未托管进程。
	switch m.Manager {
	case "systemd":
		args := []string{"is-active", m.Kernel}
		if m.SystemdScope != "" {
			args = append([]string{m.SystemdScope}, args...)
		}
		if err := exec.CommandContext(ctx, "systemctl", args...).Run(); err == nil {
			return true, nil
		}
		if m.SystemdScope != "" {
			return process.IsRunning(ctx, m.KernelPath)
		}
		return false, nil
	case "sysvinit":
		return commandActive(ctx, "service", m.Kernel, "status")
	case "openrc":
		return commandActive(ctx, "rc-service", m.Kernel, "status")
	case "runit":
		output, err := process.Output(ctx, "sv", "status", m.Kernel)
		return err == nil && strings.HasPrefix(string(output), "run"), nil
	default:
		return process.IsRunning(ctx, m.KernelPath)
	}
}

// commandActive converts a service status command result to a boolean.
func commandActive(ctx context.Context, name string, args ...string) (bool, error) {
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Status prints service status details and returns the command result.
func (m *Manager) Status(ctx context.Context, writer io.Writer, args ...string) error {
	if m.Manager == "systemd" && m.SystemdScope != "" {
		statusErr := m.systemctlOutput(ctx, writer, append([]string{"status", m.Kernel}, args...)...)
		if statusErr == nil {
			return nil
		}
		active, activeErr := process.IsRunning(ctx, m.KernelPath)
		if activeErr == nil && active {
			return commandOutput(ctx, writer, "pgrep", append([]string{"-fa", m.KernelPath}, args...)...)
		}
		return statusErr
	}
	switch m.Manager {
	case "systemd":
		return m.systemctlOutput(ctx, writer, append([]string{"status", m.Kernel}, args...)...)
	case "sysvinit":
		return commandOutput(ctx, writer, "service", append([]string{m.Kernel, "status"}, args...)...)
	case "openrc":
		return commandOutput(ctx, writer, "rc-service", append([]string{m.Kernel, "status"}, args...)...)
	case "runit":
		return commandOutput(ctx, writer, "sv", append([]string{"status", m.Kernel}, args...)...)
	default:
		command := exec.CommandContext(ctx, "pgrep", append([]string{"-fa", m.KernelPath}, args...)...)
		command.Stdout = writer
		command.Stderr = writer
		return command.Run()
	}
}

// commandOutput runs a command and writes both output streams to writer.
func commandOutput(ctx context.Context, writer io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = writer
	command.Stderr = writer
	return command.Run()
}

// journalctlOutput runs journalctl with the detected user scope when required.
func (m *Manager) journalctlOutput(ctx context.Context, writer io.Writer, args ...string) error {
	commandArgs := make([]string, 0, len(args)+1)
	if m.SystemdScope != "" {
		commandArgs = append(commandArgs, m.SystemdScope)
	}
	commandArgs = append(commandArgs, args...)
	command := exec.CommandContext(ctx, "journalctl", commandArgs...)
	command.Stdout = writer
	command.Stderr = writer
	return command.Run()
}

// Log prints stored service logs.
func (m *Manager) Log(ctx context.Context, writer io.Writer, args ...string) error {
	if m.Manager == "systemd" {
		return m.journalctlOutput(ctx, writer, append([]string{"-u", m.Kernel}, args...)...)
	}
	if len(args) > 0 {
		return commandOutput(ctx, writer, "tail", append(args, m.LogPath)...)
	}
	return commandOutput(ctx, writer, "cat", m.LogPath)
}

// FollowLog follows new service log entries until the context ends.
func (m *Manager) FollowLog(ctx context.Context, writer io.Writer) error {
	if m.Manager == "systemd" {
		return m.journalctlOutput(ctx, writer, "-u", m.Kernel, "-q", "-f", "-n", "0")
	}
	return commandOutput(ctx, writer, "tail", "-f", "-n", "0", m.LogPath)
}

// TunStatus reports whether the configured TUN device exists.
func (m *Manager) TunStatus(ctx context.Context, device string) (bool, error) {
	if device == "" {
		device = "Meta"
	}
	command := exec.CommandContext(ctx, "ip", "link", "show")
	output, err := command.Output()
	if err != nil {
		return false, fmt.Errorf("query network devices: %w", err)
	}
	return strings.Contains(string(output), device), nil
}
