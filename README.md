# clashctl-go

这是 `clashctl` 的 Go 实现，命令名称和资源目录约定沿用原项目。Go 程序默认读取 `CLASHCTL_HOME` 指向的目录；未设置时使用 `$XDG_DATA_HOME/clashctl`，再回退到 `$HOME/.local/share/clashctl`。内核名称读取 `CLASHCTL_KERNEL`，默认值为 `mihomo`。

目录职责如下：

- `cmd/clashctl`：程序入口和信号处理。
- `internal/app`：命令分发、终端输出和命令流程。
- `internal/config`：YAML 读取、Mixin 合并、运行配置生成和内核校验。
- `internal/install`：依赖下载、完整性校验和压缩包解压。
- `internal/service`：systemd、OpenRC、runit、SysVinit 与 nohup 服务管理，以及服务单元生成。
- `internal/subscription`：订阅下载、格式检测、编码转换、订阅元数据和 subconverter 调用。
- `internal/process`：外部进程调用与进程状态查询。
- `internal/output`：终端颜色和提示信息。
- `scripts/clashctl.bash`：兼容原有 shell 函数的环境变量行为。

构建和检查：

```bash
make build
make verify
make race
```

## 首次安装

安装命令完成依赖准备、目录建立、服务注册、配置生成和 shell 配置写入：

```bash
./bin/clashctl install mihomo https://example.com/sub
```

命令也接受标志写法，标志与位置参数可以任意顺序出现：

```bash
./bin/clashctl install \
  --home ~/.local/share/clashctl \
  --assets ./archives \
  --resources ./resources \
  --sub-url https://example.com/sub
```

需要准备的文件：

- `archives/`：内核与 subconverter 压缩包。缺失时按 `.env.install` 或标志中的版本号从 Github 下载，`GH_PROXY` 可作为加速前缀。
- `archives/dist.zip`：Web UI 面板。缺少时安装会失败，可用 `--skip-ui` 明确跳过。
- `resources/Country.mmdb`、`resources/geosite.dat`：规则库。缺失时内核首次启动会自行下载。

安装目录如果已经存在，命令会直接报错退出，不会覆盖现有安装。

常用选项：

| 选项 | 说明 |
| --- | --- |
| `--kernel mihomo\|clash` | 安装的内核 |
| `--sub-url <url>` | 初始订阅，支持 `http`、`https`、`file` |
| `--home <dir>` | 安装目录 |
| `--assets <dir>` | 依赖压缩包目录，默认 `archives` |
| `--resources <dir>` | 规则库与 `dist.zip` 目录，默认 `resources` |
| `--gh-proxy <prefix>` | Github 加速前缀 |
| `--version-mihomo`、`--version-subconverter` | 固定依赖版本 |
| `--skip-ui` | 不安装 Web UI |
| `--no-service` | 不注册服务单元 |
| `--no-start` | 只安装不启动 |
| `--no-shell-rc` | 不写入 `~/.bashrc` 和 `~/.zshrc` |

安装会生成 `bin/`、`resources/`（含 `mixin.yaml`、`profiles.yaml`、`runtime.yaml`、`dist/`）、`.env`、`scripts/cmd/clashctl.sh`，并在 `.bashrc`、`.zshrc` 中追加三行：`CLASHCTL_HOME`、`CLASHCTL_GO_BIN` 和入口脚本的 `source`。

当前安装命令不写 fish 配置，也不提供卸载命令。

## 代理环境变量

`bin/clashctl` 是独立二进制。直接执行 `bin/clashctl on` 时，Go 进程无法修改调用它的父 shell 环境；`scripts/clashctl.bash` 提供了兼容函数，安装命令会把该脚本写入 `$CLASHCTL_HOME/scripts/cmd/clashctl.sh` 并自动加载：

```bash
export CLASHCTL_GO_BIN=/path/to/clashctl-go/bin/clashctl
source scripts/clashctl.bash
```

`CLASHCTL_GO_BIN` 默认取 `PATH` 中的 `clashctl`，函数内部用 `command` 调用，避免同名函数递归。

迁移前可以把二进制复制到临时目录，并设置临时的 `CLASHCTL_HOME` 进行检查。验收完成后，再由使用者决定是否更新 shell 配置和现有二进制入口。程序本身不会自动替换当前安装目录，也不会在构建和测试阶段启动或停止 mihomo。
