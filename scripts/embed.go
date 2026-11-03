// Package scripts 提供随二进制嵌入的 shell 兼容脚本。
package scripts

import _ "embed"

// ClashctlBash 是加载 clashctl shell 函数所需的内容。
//
//go:embed clashctl.bash
var ClashctlBash string
