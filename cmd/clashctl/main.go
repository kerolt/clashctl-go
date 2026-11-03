package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nelvko/clashctl-go/internal/app"
)

// main 启动 clashctl 命令，并把终端信号传递给上下文。
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(os.Stdout, os.Stderr, os.Stdin)
	if err == nil {
		err = application.Run(ctx, os.Args[1:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
