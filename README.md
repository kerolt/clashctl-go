# clashctl-go

在 Linux 上部署和管理 mihomo / clash 内核的命令行工具。它负责内核服务、代理环境、订阅、Mixin 配置、TUN 模式和 Web 面板入口，全部功能集中在一个二进制中。

## 功能

- 代理开关：启动或停止内核服务，并把代理变量导出到当前终端。
- 服务托管：支持 systemd（系统级与用户级）、OpenRC、runit、SysVinit、nohup。
- 配置管理：由基础订阅配置和 Mixin 生成运行配置，生成后调用内核自身校验。
- 订阅管理：添加、删除、切换、更新订阅，支持原生 Clash 配置与订阅格式转换。
- 面板入口：输出内网、公网和公共服务面板地址。
- TUN 模式：开关 TUN，必要时以提权方式重启内核。
- 其他：Web 访问密钥、Mixin 编辑、日志查看、内核升级、首次安装。

## 首次使用

### 第 1 步：前置条件

- Linux，架构为 amd64、arm64、386 或 arm。
- 已安装 `pgrep`、`pkill`，以及 `ip` 或 `hostname`。查看订阅日志需要 `tail`，定时更新订阅需要 `crontab`，编码转换需要 `iconv`，这些命令缺失时对应功能不可用。
- 一个可用的订阅链接，或者一份本地 Clash / mihomo 配置。
- 一个 Web UI 面板包 `dist.zip`，不提供时安装会按默认地址下载。

### 第 2 步：取得二进制

```bash
git clone <仓库地址> clashctl-go
cd clashctl-go
make build
```

构建产物是 `bin/clashctl`，可以复制到任意位置执行。

### 第 3 步：准备依赖目录

在当前目录建立 `archives/` 和 `resources/`：

| 路径 | 内容 | 是否必需 |
| --- | --- | --- |
| `archives/` | 内核与 subconverter 压缩包 | 否，缺失时自动下载 |
| `archives/dist.zip` | Web UI 面板 | 否，缺失时按 `--ui-dist` 或默认地址下载 |
| `resources/Country.mmdb` | 国家与地区规则库 | 否，内核首次启动自行下载 |
| `resources/geosite.dat` | 域名规则库 | 否，内核首次启动自行下载 |

### 第 4 步：执行安装

```bash
./bin/clashctl install --sub-url https://example.com/sub
```

安装完成后会写入 shell 配置，新开终端或执行 `source ~/.bashrc` 后即可用 `clashctl` 命令。

### 第 5 步：确认结果

```bash
clashctl status     # 服务是否在运行
clashctl ui         # 面板地址与访问密钥
clashctl on         # 在当前终端启用代理
curl -I https://www.google.com
```

如果 `status` 返回失败，用 `clashctl log` 查看内核日志。

## 安装

### 命令形式

```bash
clashctl install [选项] [内核] [订阅链接]
```

位置参数可以省略，等价于使用 `--kernel` 与 `--sub-url`。位置参数与标志可以任意顺序混合。

```bash
clashctl install mihomo https://example.com/sub
clashctl install --kernel clash --sub-url file:///path/to/config.yaml
```

### 选项

| 选项 | 默认值 | 说明 |
| --- | --- | --- |
| `--kernel mihomo\|clash` | `mihomo` | 安装的内核 |
| `--sub-url <url>` | 空 | 初始订阅，支持 `http`、`https`、`file` |
| `--ui-dist <path\|url>` | zashboard 最新版 | Web UI 压缩包路径或下载地址 |
| `--home <dir>` | `~/.local/share/clashctl` | 安装目录 |
| `--assets <dir>` | `archives` | 依赖压缩包目录 |
| `--resources <dir>` | `resources` | 规则库与 `dist.zip` 目录 |
| `--gh-proxy <prefix>` | 空 | 下载加速前缀 |
| `--version-mihomo <tag>` | 最新发布 | mihomo 版本 |
| `--version-subconverter <tag>` | 最新发布 | subconverter 版本 |
| `--subconverter-repo <owner/repo>` | `asdlokj1qpi233/subconverter` | subconverter 仓库 |
| `--sub-ua <ua>` | `mihomo` | 订阅下载 User-Agent |
| `--skip-ui` | 否 | 不安装 Web UI |
| `--no-service` | 否 | 不注册服务单元 |
| `--no-start` | 否 | 只安装不启动 |
| `--no-shell-rc` | 否 | 不写入 shell 配置 |
| `--download-timeout <时长>` | `60s` | 依赖下载超时，支持 `30s`、`2m` 写法 |

### 默认值来源

安装参数来自环境变量与内置默认值，命令行标志优先级最高。相关环境变量见「配置」章节的路径与环境变量表。

Web UI 压缩包默认取 zashboard 的发布包，可用 `--ui-dist` 指定本地路径或其他 `http`、`https` 地址。

### 安装流程

1. 检查依赖命令，确认安装目录不存在且父目录可写。
2. 查找依赖压缩包，缺失时按版本号下载；Web UI 缺失时按 `--ui-dist` 或默认地址下载；amd64 上依据 CPU 指令集选择 mihomo 的 v1、v2、v3 版本。
3. 校验压缩包完整性，解压内核、subconverter 与 Web UI，复制规则库。
4. 建立目录并写入默认 `mixin.yaml`、`profiles.yaml`、`.env` 与 shell 入口脚本。
5. 按运行方式渲染服务单元，重载配置并设置开机自启。
6. 导入初始订阅，生成并校验运行配置。
7. 缺少密钥时生成随机密钥；端口被占用时重新分配。
8. 启动服务，输出面板地址，最后写入 shell 配置。

安装目录已存在时命令直接报错退出，不会覆盖现有安装。

### 安装后的目录

| 路径 | 说明 |
| --- | --- |
| `.env` | 订阅超时与 User-Agent |
| `bin/<内核>` | 内核二进制 |
| `bin/subconverter/subconverter` | 订阅转换服务 |
| `bin/subconverter/pref.yml` | 订阅转换配置 |
| `resources/config.yaml` | 基础订阅配置 |
| `resources/mixin.yaml` | Mixin 配置 |
| `resources/runtime.yaml` | 交给内核的运行配置 |
| `resources/temp.yaml` | 合并前的临时副本，校验失败时用于恢复 |
| `resources/profiles.yaml` | 订阅元数据与当前订阅 id |
| `resources/profiles/<id>.yaml` | 各订阅的配置内容 |
| `resources/profiles.log` | 订阅操作日志 |
| `resources/Country.mmdb`、`resources/geosite.dat` | 规则库 |
| `resources/dist/` | Web UI 静态文件 |
| `resources/<内核>.log` | 用户级与 nohup 场景的内核日志 |
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
clashctl on        # 启动服务并导出终端代理变量
clashctl on -s     # 只启动服务
clashctl on -e     # 只导出终端代理变量
clashctl off -s    # 只停止服务
clashctl off -e    # 只清除终端代理变量
```

`on` 在启动前检查代理端口，冲突时把新端口写入 `mixin.yaml` 并重新生成运行配置。`off` 先正常停止服务，仍存在提权实例且处于 TUN 模式时再执行提权停止。

导出到终端的变量包括 `http_proxy`、`https_proxy`、`all_proxy`、`no_proxy` 及对应大写形式。认证信息取自运行配置的 `authentication`，监听地址依据 `allow-lan` 与 `bind-address` 决定。

### 订阅

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

1. 下载原始内容，或读取 `file://` 指向的本地文件。
2. 去掉 UTF-8 BOM 与回车符；不是合法 UTF-8 时用 `iconv` 依次尝试 GB18030、GBK、BIG5。
3. 响应内容像 HTML 页面时直接报错。
4. 含 `proxies` 或 `proxy-providers` 时按原生配置处理，否则交给 subconverter 转换。
5. 校验运行配置与节点数量，失败时保留 `${dest}.raw` 便于排查。

### Mixin 合并规则

| 字段 | 行为 |
| --- | --- |
| `rules.prepend`、`rules.append` | 插入到规则列表头部或尾部 |
| `proxies.prepend`、`proxies.append` | 在基础节点前后追加节点 |
| `proxies.override` | 按 `name` 替换同名的整个节点 |
| `proxy-groups.prepend`、`append`、`override` | 对代理组执行相同规则 |
| `proxy-groups.inject` | 按组名向组的 `proxies` 列表注入节点并去重 |
| `_custom` | 合并前丢弃 |

规则中的 `__CLASHCTL_PROXY_GROUP__` 会替换为默认代理组：优先取名称含 `节点选择` 的 `select` 组，没有则取第一个 `select` 组。

合并结果写入 `runtime.yaml`，再由内核做 `-t` 校验；校验失败时恢复上一份运行配置。

### TUN

```bash
clashctl tun        # 查看状态
clashctl tun on     # 开启
clashctl tun off    # 关闭
```

开启 TUN 需要提权。mihomo 首次启用失败时会关闭 `auto-redirect` 后重试一次。

### 日志与升级

`clashctl log` 在 systemd 方式下读取 `journalctl -u <内核>`，其他方式读取安装目录或 `/var/log` 下的日志文件。

`clashctl upgrade` 通过 `external-controller` 的 `/upgrade` 接口升级内核，`-r` 使用稳定版，`-a` 使用测试版，`-v` 同时跟随升级日志。

## 配置

### 路径与环境变量

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CLASHCTL_HOME` | `$XDG_DATA_HOME/clashctl`，否则 `~/.local/share/clashctl` | 安装目录 |
| `CLASHCTL_KERNEL` | `mihomo` | 内核名称 |
| `CLASHCTL_SUB_TIMEOUT` | `5` | 订阅下载超时秒数 |
| `CLASHCTL_SUB_UA` | `mihomo` | 订阅下载 User-Agent |
| `CLASHCTL_DOWNLOAD_TIMEOUT` | `60` | 依赖下载超时秒数 |
| `CLASHCTL_GO_BIN` | `clashctl` | shell 函数调用的二进制 |

安装目录下的 `.env` 会被读取，已经存在的同名环境变量优先。

### 服务管理

启动时按以下顺序识别运行方式：

1. 非 Linux 或非 root 时使用 nohup。
2. `/proc/1/cgroup` 命中容器标记时使用 nohup。
3. 依据 `/proc/1/exe` 判定 systemd、OpenRC、runit、SysVinit。
4. nohup 场景下存在用户级 systemd 单元时，改用 `systemctl --user`。

日志位置：系统级 systemd 与 root 场景写入 `/var/log/<内核>.log`，用户级与 nohup 场景写入 `$CLASHCTL_HOME/resources/<内核>.log`。

### shell 入口

Go 进程无法修改父 shell 的环境变量，代理变量的导出与清除由 shell 函数完成。安装命令会写入 `$CLASHCTL_HOME/scripts/cmd/clashctl.sh`：

- 执行 `on` 或 `off` 时先调用二进制，再用 `eval` 应用返回的 `export` 或 `unset` 语句。
- 其余命令直接转交二进制。
- 调用时使用 `command "$CLASHCTL_GO_BIN"`，避免与同名函数递归。

手动加载：

```bash
export CLASHCTL_GO_BIN=/path/to/clashctl
source /path/to/clashctl/scripts/cmd/clashctl.sh
```

## 常见问题

**当前终端没有代理变量**

`on` 需要经过 shell 入口脚本执行，脚本未加载时只会启动服务。执行 `source ~/.bashrc` 或新开终端。

**面板地址返回 404**

Web UI 未安装或下载失败。重新安装时不加 `--skip-ui`，或把面板文件放到 `resources/dist/`。

**服务启动后立刻退出**

用 `clashctl log` 查看内核日志，常见原因是订阅节点不可用、端口被占用、规则库缺失。

**安装提示目录已存在**

工具不会覆盖已有安装。确认旧目录无用后删除，或改用 `--home` 指定其他路径。

**普通用户无法注册服务**

用户级 systemd 需要 `~/.config/systemd/user` 下存在单元文件且 `systemctl --user` 可用，否则自动退回 nohup 方式。

**端口冲突**

`on` 与安装命令都会检查端口占用，冲突时把新端口写回 `mixin.yaml`，重新生成运行配置。

## 构建与测试

```bash
make build     # 生成 bin/clashctl
make test      # 单元测试
make vet       # go vet
make race      # 竞态检测
make verify    # test + vet + gofmt 检查
```

## 范围说明

- 不写 fish 配置。
- 不提供卸载命令，删除安装目录并清理 shell 配置中的三行即可。
- 内核升级依赖内核自身的控制接口，不下载内核文件。
