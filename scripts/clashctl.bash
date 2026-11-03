#!/usr/bin/env bash

# 兼容原有 shell 函数的环境变量行为，服务管理与配置逻辑由 Go 二进制完成。
# CLASHCTL_GO_BIN 默认取 PATH 中的 clashctl；这里用 command 调用，避免同名函数递归。
: "${CLASHCTL_GO_BIN:=clashctl}"

clashctl() {
    case "${1:-help}:${2:-}" in
    on:-e|on:--env-only)
        eval "$(command "$CLASHCTL_GO_BIN" __env-on)"
        ;;
    on:)
        command "$CLASHCTL_GO_BIN" "$@" || return
        eval "$(command "$CLASHCTL_GO_BIN" __env-on)"
        ;;
    off:-e|off:--env-only)
        eval "$(command "$CLASHCTL_GO_BIN" __env-off)"
        ;;
    off:)
        command "$CLASHCTL_GO_BIN" "$@" || return
        eval "$(command "$CLASHCTL_GO_BIN" __env-off)"
        ;;
    *)
        command "$CLASHCTL_GO_BIN" "$@"
        ;;
    esac
}
