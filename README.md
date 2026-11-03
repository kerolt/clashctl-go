# clashctl-go

`clashctl` 是 mihomo / clash 内核的部署与管理工具，本仓库是它的 Go 实现。命令名称、安装目录结构、Mixin 配置格式和订阅元数据与原 bash 版本保持一致，单个二进制替代原先散布在 `scripts/cmd` 与 `scripts/lib` 中的 shell 函数。

## 功能

- 代理开关：启动或停止内核服务，并把代理变量导出到当前终端。
- 服务托管：识别 systemd（系统级与用户级）、OpenRC、runit、SysVinit、nohup 五种运行方式。
- 配置管理：以基础订阅配置加上 Mixin 生成运行配置，生成前调用内核自身做语法校验。
- 订阅管理：添加、删除、切换、更新订阅，支持原生 Clash 配置识别与 subconverter 转换兜底。
- 面板信息：输出内网、公网和公共服务面板地址。
- TUN 模式：开关 TUN，必要时以提权方式重启内核。
- 其他：Web 访问密钥、Mixin 编辑、日志查看、内核升级、首次安装。

## 运行环境

- Linux，amd64、arm64、386 或 arm 架构。
- 内核二进制、subconverter 与规则库由安装命令准备。
- 服务托管依赖对应 init 系统的命令；普通用户在没有用户级 systemd 单元时使用 nohup。

## 安装

### 准备文件

安装命令需要一个依赖目录和一个资源目录。

依赖目录默认是当前目录下的 `archives/`：

| 文件 | 是否必需 | 说明 |
| --- | --- | --- |
| `mihomo-*.gz` 或 `clash-*.gz` | 否 | 内核压缩包，缺失时按版本号从 Github 下载 |
| `subconverter_*.tar.gz` | 否 | 订阅转换服务，缺失时从 Github 下载 |
| `dist.zip` | 是 | Web UI 面板，缺失时安装失败，可用 `--skip-ui` 明确跳过 |

资源目录默认是当前目录下的 `resources/`：

| 文件 | 说明 |
| --- | --- |
| `Country.mmdb`、`geosite.dat` | 规则库，存在时复制到安装目录，缺失时内核首次启动自行下载 |

下载版本来自 `.env.install` 或命令行标志。`GH_PROXY` 或 `--gh-proxy` 可以加下载加速前缀。

`.env.install` 支持以下键：

```ini
CLASHCTL_HOME=~/.local/share/clashctl
CLASHCTL_KERNEL=mihomo
CLASHCTL_SUB_URL=
CLASHCTL_DOWNLOAD_TIMEOUT=60
GH_PROXY=
VERSION_MIHOMO=
VERSION_SUBCONVERTER=
SUBCONVERTER_REPO=asdlokj1qpi233/subconverter
```

### 安装命令

```bash
./bin/clashctl install mihomo https://example.com/sub
```

位置参数沿用原脚本写法：第一个是内核名，第二个是订阅链接。也可以用标志：

```bash
./bin/clashctl install \
  --home ~/.local/share/clashctl \
  --assets ./archives \
  --resources ./resources \
  --sub-url https://example.com/sub
```

| 选项 | 说明 |
| --- | --- |
| `--kernel mihomo\|clash` | 安装的内核 |
| `--sub-url <url>` | 初始订阅，支持 `http`、`https`、`file` |
| `--home <dir>` | 安装目录 |
| `--assets <dir>` | 依赖压缩包目录，默认 `archives` |
| `--resources <dir>` | 规则库与 `dist.zip` 目录，默认 `resources` |
| `--gh-proxy <prefix>` | Github 加速前缀 |
| `--version-mihomo <tag>` | 固定 mihomo 版本 |
| `--version-subconverter <tag>` | 固定 subconverter 版本 |
| `--subconverter-repo <owner/repo>` | subconverter 仓库 |
| `--sub-ua <ua>` | 订阅下载 User-Agent |
| `--skip-ui` | 不安装 Web UI |
| `--no-service` | 不注册服务单元 |
| `--no-start` | 只安装不启动 |
| `--no-shell-rc` | 不写入 `~/.bashrc` 和 `~/.zshrc` |

安装目录已经存在时命令直接报错退出，不会覆盖现有安装。

### 安装流程

1. 检查 `pgrep`、`pkill` 以及 `ip` 或 `hostname` 是否存在。
2. 确认安装目录不存在、最近的已存在父目录可写。
3. 查找或下载依赖压缩包；amd64 上依据 CPU 指令集在 mihomo 的 v1、v2、v3 版本间选择。
4. 校验压缩包完整性，解压内核、subconverter 和 Web UI，复制规则库。
5. 建立 `bin/`、`resources/`、`profiles/` 与 `scripts/cmd/`，写入默认 `mixin.yaml`、`profiles.yaml`、`.env` 和 shell 入口脚本。
6. 按运行方式渲染服务单元，重载配置并设置开机自启。
7. 导入初始订阅，合并配置并调用内核校验运行配置。
8. 运行配置缺少密钥时生成随机密钥；代理端口或控制端口被占用时重新分配。
9. 启动服务并输出面板地址。
10. 向 `~/.bashrc` 和 `~/.zshrc` 追加三行：`CLASHCTL_HOME`、`CLASHCTL_GO_BIN` 和入口脚本的 `source`。

### 生成目录

| 路径 | 说明 |
| --- | --- |
| `.env` | 安装参数，用户可编辑订阅超时与 UA |
| `bin/<内核>` | 内核二进制 |
| `bin/subconverter/subconverter` | 订阅转换服务 |
| `bin/subconverter/pref.yml` | 订阅转换配置，由 `pref.example.yml` 复制而来 |
| `resources/config.yaml` | 基础订阅配置 |
| `resources/mixin.yaml` | Mixin 配置 |
| `resources/runtime.yaml` | 合并后交给内核的运行配置 |
| `resources/temp.yaml` | 合并前的临时副本，校验失败时用于恢复 |
| `resources/profiles.yaml` | 订阅元数据，含订阅列表与当前使用的 id |
| `resources/profiles/<id>.yaml` | 各订阅的配置内容 |
| `resources/profiles.log` | 订阅操作日志 |
| `resources/Country.mmdb`、`resources/geosite.dat` | 规则库 |
| `resources/dist/` | Web UI 静态文件 |
| `resources/mihomo.log` | 用户级与 nohup 场景的内核日志 |
| `scripts/cmd/clashctl.sh` | shell 入口脚本 |

## 命令

| 命令 | 说明 |
| --- | --- |
| `clashctl on [-s\|-e]` | 启动服务并导出终端代理变量 |
| `clashctl off [-s\|-e]` | 停止服务并清除终端代理变量 |
| `clashctl status` | 输出服务状态，服务未运行时返回非零 |
| `clashctl ui` | 输出面板地址 |
| `clashctl sub ...` | 订阅管理 |
| `clashctl tun [on\|off]` | 查看或切换 TUN 模式 |
| `clashctl mixin [-e\|-r\|-c]` | 查看或编辑 Mixin、基础配置、运行配置 |
| `clashctl secret [新密钥]` | 查看或修改 Web 访问密钥 |
| `clashctl log [参数]` | 查看服务日志 |
| `clashctl upgrade [-v\|-r\|-a]` | 通过本地控制接口升级内核 |
| `clashctl install [选项]` | 首次安装 |
| `clashctl help` | 输出命令列表 |

### 代理开关

```bash
clashctl on              # 启动服务并导出终端代理变量
clashctl on -s           # 只启动服务
clashctl on -e           # 只导出终端代理变量
clashctl off -s          # 只停止服务
clashctl off -e          # 只清除终端代理变量
```

`on` 在启动前检查代理端口占用情况，冲突时把新端口写入 `mixin.yaml` 并重新生成运行配置。`off` 会先尝试正常停止服务，仍存在提权实例且处于 TUN 模式时再执行提权停止。

导出到终端的变量为 `http_proxy`、`https_proxy`、`all_proxy`、`no_proxy` 及对应大写形式。认证信息取自运行配置的 `authentication`，监听地址依据 `allow-lan` 与 `bind-address` 决定。

### 订阅管理

```bash
clashctl sub add <url>        # 添加订阅
clashctl sub add -u <url>     # 添加并立即使用
clashctl sub ls               # 查看订阅
clashctl sub use <id>         # 切换订阅
clashctl sub del <id>         # 删除未使用的订阅
clashctl sub update [id]      # 更新订阅
clashctl sub update --convert # 强制走订阅转换
clashctl sub update --auto    # 写入 cron 定时更新
clashctl sub log              # 查看订阅日志
```

订阅处理顺序：

1. 下载原始内容或读取 `file://` 路径。
2. 去掉 UTF-8 BOM，移除回车符；不是合法 UTF-8 时用 `iconv` 依次尝试 GB18030、GBK、BIG5。
3. 识别 HTML 响应并报错。
4. 含 `proxies` 或 `proxy-providers` 时按原生配置处理，否则调用 subconverter 转换。
5. 校验运行配置与节点数量，失败时保留 `${dest}.raw` 供排查。

### Mixin 合并规则

运行配置由基础配置与 Mixin 合并而成：

| 字段 | 行为 |
| --- | --- |
| `rules.prepend` / `rules.append` | 插入到规则列表头部或尾部 |
| `proxies.prepend` / `append` | 在基础节点前后追加节点 |
| `proxies.override` | 按 `name` 替换同名的整个节点 |
| `proxy-groups.prepend` / `append` / `override` | 对代理组执行相同规则 |
| `proxy-groups.inject` | 按组名向组的 `proxies` 列表注入节点并去重 |
| `_custom` | 合并前丢弃 |

规则中的 `__CLASHCTL_PROXY_GROUP__` 会替换为订阅中的默认代理组：优先取名称含 `节点选择` 的 `select` 组，没有则取第一个 `select` 组。

合并结果写入 `runtime.yaml` 后交给内核做 `-t` 校验，校验失败时恢复上一份运行配置。

### 日志与升级

`clashctl log` 在 systemd 方式下读取 `journalctl -u <内核>`，其他方式读取安装目录或 `/var/log` 下的日志文件。

`clashctl upgrade` 通过 `external-controller` 的 `/upgrade` 接口升级内核，`-r` 使用稳定版，`-a` 使用测试版，`-v` 同时跟随升级日志。

## 配置

### 路径

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CLASHCTL_HOME` | `$XDG_DATA_HOME/clashctl`，否则 `~/.local/share/clashctl` | 安装目录 |
| `CLASHCTL_KERNEL` | `mihomo` | 内核名称 |
| `CLASHCTL_SUB_TIMEOUT` | `5` | 订阅下载超时秒数 |
| `CLASHCTL_SUB_UA` | `mihomo` | 订阅下载 User-Agent |
| `CLASHCTL_GO_BIN` | `clashctl` | shell 函数调用的二进制 |

安装目录下的 `.env` 会被读取，已存在的同名环境变量优先。

### 服务管理

启动时按以下顺序识别运行方式：

1. 非 Linux 或非 root 时使用 nohup。
2. `/proc/1/cgroup` 命中容器标记时使用 nohup。
3. 依据 `/proc/1/exe` 判定 systemd、OpenRC、runit、SysVinit。
4. nohup 场景下如果存在用户级 systemd 单元，则改为 `systemctl --user`。

日志位置：系统级 systemd 与其他 root 场景写入 `/var/log/<内核>.log`，用户级与 nohup 场景写入 `$CLASHCTL_HOME/resources/<内核>.log`。

## 与 shell 的交互

Go 进程无法修改父 shell 的环境变量，导出与清除代理变量由 shell 函数完成。安装命令会把入口脚本写到 `$CLASHCTL_HOME/scripts/cmd/clashctl.sh`，该脚本定义 `clashctl` 函数：

- 执行 `on` 或 `off` 时先调用二进制，再用 `eval` 应用返回的 `export` 或 `unset` 语句。
- 其余命令直接转交二进制。
- 调用时统一使用 `command "$CLASHCTL_GO_BIN"`，避免与同名函数递归。

手动加载：

```bash
export CLASHCTL_GO_BIN=/path/to/clashctl
source /path/to/clashctl/scripts/cmd/clashctl.sh
```

## 构建与测试

```bash
make build     # 生成 bin/clashctl
make test      # 单元测试
make vet       # go vet
make race      # 竞态检测
make verify    # test + vet + gofmt 检查
```

## 当前范围

- 安装命令不写 fish 配置。
- 不提供卸载命令，删除安装目录与 `~/.bashrc`、`~/.zshrc` 中的三行即可。
- 不再安装 `yq`，Go 实现自带 YAML 处理。
- 内核升级依赖内核自身的控制接口，不下载内核文件。
