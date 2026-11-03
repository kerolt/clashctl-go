package app

import (
	_ "embed"

	"github.com/nelvko/clashctl-go/scripts"
)

//go:embed templates/mixin.yaml
var mixinTemplate string

//go:embed templates/profiles.yaml
var profilesTemplate string

// clashctlShell 是安装时写入的 shell 入口脚本。
var clashctlShell = scripts.ClashctlBash
