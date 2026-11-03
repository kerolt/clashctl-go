// Package config 处理 clashctl 路径、YAML 值和运行配置生成。
package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// Paths contains the files and binaries used by one clashctl installation.
type Paths struct {
	Home             string
	Resources        string
	BaseConfig       string
	MixinConfig      string
	RuntimeConfig    string
	TemporaryConfig  string
	KernelPath       string
	YQPath           string
	SubconverterDir  string
	SubconverterPath string
	SubconverterConf string
	SubconverterLog  string
	ProfilesDir      string
	ProfilesMeta     string
	ProfilesLog      string
}

// NewPaths builds all installation paths below home for the selected kernel.
func NewPaths(home string, kernel string) Paths {
	resources := filepath.Join(home, "resources")
	bin := filepath.Join(home, "bin")
	subconverterDir := filepath.Join(bin, "subconverter")

	return Paths{
		Home:             home,
		Resources:        resources,
		BaseConfig:       filepath.Join(resources, "config.yaml"),
		MixinConfig:      filepath.Join(resources, "mixin.yaml"),
		RuntimeConfig:    filepath.Join(resources, "runtime.yaml"),
		TemporaryConfig:  filepath.Join(resources, "temp.yaml"),
		KernelPath:       filepath.Join(bin, kernel),
		YQPath:           filepath.Join(bin, "yq"),
		SubconverterDir:  subconverterDir,
		SubconverterPath: filepath.Join(subconverterDir, "subconverter"),
		SubconverterConf: filepath.Join(subconverterDir, "pref.yml"),
		SubconverterLog:  filepath.Join(subconverterDir, "latest.log"),
		ProfilesDir:      filepath.Join(resources, "profiles"),
		ProfilesMeta:     filepath.Join(resources, "profiles.yaml"),
		ProfilesLog:      filepath.Join(resources, "profiles.log"),
	}
}

// FromEnvironment 解析安装目录：优先 CLASHCTL_HOME，其次 XDG 数据目录下的 clashctl。
func FromEnvironment(kernel string) (Paths, error) {
	if home := os.Getenv("CLASHCTL_HOME"); home != "" {
		return NewPaths(home, kernel), nil
	}

	home, err := defaultHome()
	if err != nil {
		return Paths{}, err
	}
	return NewPaths(home, kernel), nil
}

// defaultHome 返回默认安装目录：$XDG_DATA_HOME/clashctl 或 ~/.local/share/clashctl。
func defaultHome() (string, error) {
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, "clashctl"), nil
	}

	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("find current user: %w", err)
	}
	return filepath.Join(currentUser.HomeDir, ".local", "share", "clashctl"), nil
}

// KernelFromEnvironment returns the configured kernel name or mihomo by default.
func KernelFromEnvironment() string {
	if kernel := os.Getenv("CLASHCTL_KERNEL"); kernel != "" {
		return kernel
	}
	return "mihomo"
}
