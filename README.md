# clashctl-go

这是 `clashctl` 的 Go 实现，命令名称和资源目录约定沿用原项目。Go 程序默认读取 `CLASHCTL_HOME` 指向的目录；未设置时使用 `$HOME/.local/bin/clashctl`。内核名称读取 `CLASHCTL_KERNEL`，默认值为 `mihomo`。

目录职责如下：

- `cmd/clashctl`：程序入口和信号处理。
- `internal/app`：命令分发、终端输出和命令流程。
- `internal/config`：YAML 读取、Mixin 合并、运行配置生成和内核校验。
- `internal/service`：systemd、OpenRC、runit、SysVinit 与 nohup 服务管理。
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

`bin/clashctl` 是独立二进制。直接执行 `bin/clashctl on` 时，Go 进程无法修改调用它的父 shell 环境；`scripts/clashctl.bash` 提供了兼容函数，使用时设置 `CLASHCTL_GO_BIN` 指向二进制并加载该文件：

```bash
export CLASHCTL_GO_BIN=/path/to/clashctl-go/bin/clashctl
source scripts/clashctl.bash
```

迁移前可以把二进制复制到临时目录，并设置临时的 `CLASHCTL_HOME` 进行检查。验收完成后，再由使用者决定是否更新 shell 配置和现有二进制入口。程序本身不会自动替换当前安装目录，也不会在构建和测试阶段启动或停止 mihomo。
