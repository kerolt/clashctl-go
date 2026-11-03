// Package process 提供外部进程操作的简单封装。
package process

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Run executes a command with the current process streams.
func Run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// Output executes a command and returns its combined output.
func Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	result, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(result))
		if message == "" {
			return nil, fmt.Errorf("run %s: %w", name, err)
		}
		return nil, fmt.Errorf("run %s: %w: %s", name, err, message)
	}
	return result, nil
}

// StartDetached starts a command with the supplied output writers.
func StartDetached(name string, args []string, stdout, stderr io.Writer) (*exec.Cmd, error) {
	command := exec.Command(name, args...)
	command.Stdin = nil
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	return command, nil
}

// IsRunning reports whether a process command line contains executable.
func IsRunning(ctx context.Context, executable string) (bool, error) {
	command := exec.CommandContext(ctx, "pgrep", "-fa", executable)
	output, err := command.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("find process %q: %w", executable, err)
	}
	return strings.Contains(string(output), executable), nil
}

// TerminateByName sends TERM and then KILL to processes with an exact name.
func TerminateByName(ctx context.Context, name string, wait time.Duration) error {
	if err := exec.CommandContext(ctx, "pkill", "-TERM", "-x", name).Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); !ok || exitError.ExitCode() != 1 {
			return fmt.Errorf("terminate %s: %w", name, err)
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	<-timer.C
	if err := exec.CommandContext(ctx, "pkill", "-KILL", "-x", name).Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); !ok || exitError.ExitCode() != 1 {
			return fmt.Errorf("force terminate %s: %w", name, err)
		}
	}
	return nil
}
