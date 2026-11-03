#!/usr/bin/env bash

# 提供 shell 函数，把代理变量导出到当前终端；其余命令直接转交二进制。
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
