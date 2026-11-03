package app

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/kerolt/clashctl-go/internal/selfupgrade"
)

// selfUpgrade 更新当前运行的 clashctl 程序。
func (a *App) selfUpgrade(ctx context.Context, args []string) error {
	version := ""
	flags := flag.NewFlagSet("self-upgrade", flag.ContinueOnError)
	flags.SetOutput(a.Printer.Err)
	flags.StringVar(&version, "version", "", "目标 release tag")

	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("无法识别的 self-upgrade 参数：%s", flags.Arg(0))
	}

	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位 clashctl 可执行文件: %w", err)
	}
	goos, goarch := selfupgrade.CurrentPlatform()

	a.Printer.OKWithEmoji("⏳", "正在查询 clashctl release...")
	client := selfupgrade.NewClient(nil)
	result, err := client.Upgrade(ctx, version, executablePath, goos, goarch)
	if err != nil {
		return fmt.Errorf("clashctl 自升级失败: %w", err)
	}
	a.Printer.OKWithEmoji("🎉", "clashctl 已更新到 "+result.Tag)
	return nil
}
