#!/usr/bin/env bash

# 兼容原有 shell 函数的环境变量行为，服务管理与配置逻辑由 Go 二进制完成。
: "${CLASHCTL_GO_BIN:=clashctl-go}"

clashctl() {
    case "${1:-help}:${2:-}" in
    on:-e|on:--env-only)
        eval "$("$CLASHCTL_GO_BIN" __env-on)"
        ;;
    on:)
        "$CLASHCTL_GO_BIN" "$@" || return
        eval "$("$CLASHCTL_GO_BIN" __env-on)"
        ;;
    off:-e|off:--env-only)
        eval "$("$CLASHCTL_GO_BIN" __env-off)"
        ;;
    off:)
        "$CLASHCTL_GO_BIN" "$@" || return
        eval "$("$CLASHCTL_GO_BIN" __env-off)"
        ;;
    *)
        "$CLASHCTL_GO_BIN" "$@"
        ;;
    esac
}
