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

// FromEnvironment resolves the installation home from CLASHCTL_HOME or the user home.
func FromEnvironment(kernel string) (Paths, error) {
	home := os.Getenv("CLASHCTL_HOME")
	if home == "" {
		currentUser, err := user.Current()
		if err != nil {
			return Paths{}, fmt.Errorf("find current user: %w", err)
		}
		home = filepath.Join(currentUser.HomeDir, ".local", "bin", "clashctl")
	}
	return NewPaths(home, kernel), nil
}

// KernelFromEnvironment returns the configured kernel name or mihomo by default.
func KernelFromEnvironment() string {
	if kernel := os.Getenv("CLASHCTL_KERNEL"); kernel != "" {
		return kernel
	}
	return "mihomo"
}
