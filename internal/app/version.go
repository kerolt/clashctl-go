package app

import "fmt"

// Version 是当前 clashctl 的版本号，由发布构建注入。
var Version = "dev"

// version 输出当前 clashctl 版本。
func (a *App) version() error {
	fmt.Fprintln(a.Printer.Out, "clashctl "+Version)
	return nil
}
