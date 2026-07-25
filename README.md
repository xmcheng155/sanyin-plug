# 三音 Plug：网易三音云音箱本地增强工具

三音 Plug 面向网易三音云音箱（SING 系列），在保留原厂固件和音频链路的前提下，为已经失去部分云端能力的设备补充局域网控制能力。

项目最初只用于恢复 AirPlay，现在已经扩展为运行在音箱上的本地配置与播放服务：浏览器可以查看设备状态、恢复 AirPlay、切换 Wi-Fi、控制蓝牙和 EQ，也可以调用原厂 KPlayer 播放音乐 URL、管理网络电台和设置定时停止。

> 当前主要实机基线：`NeteaseC930`、OpenWrt、ARMv7、固件 `1.1.17.4`。其他型号或固件请先运行 `doctor`，不要直接安装。

## 功能概览

| 功能 | 当前能力 | 验收状态 |
| --- | --- | --- |
| AirPlay 恢复 | 恢复原厂 RAOP 服务，开机和 Wi-Fi 重连后自动检查 | 安全写能力，已完成实机与重启验证 |
| AirPlay 自动恢复 | 在网页中开启或关闭自有守护策略 | 安全写能力，配置可持久化 |
| 设备状态 | 查看固件、核心服务、网络、播放器和音频状态 | 只读 |
| Wi-Fi | 查看当前 SSID，切换网络；失败时自动恢复原配置 | 实验能力，已验证 45 秒超时回退与启动恢复 |
| 蓝牙 | 请求开启或关闭，并等待原厂服务成功事件 | 实验能力；当前实时开关仍可能显示未知 |
| EQ | 切换普通、智能、人声、现场、重低音、电子乐和 ACG | 实验能力；区分业务选中态与硬件应用态 |
| URL 播放 | 由原厂 KPlayer 直接拉取 HTTP/HTTPS 音频地址 | 实验能力，支持播放、暂停、恢复、停止、下一首和 0–100 音量调节 |
| 播放队列 | 添加、选择、移除、清空并自动续播 | 实验能力 |
| 网络电台 | 收藏、播放、加入队列、调整顺序和删除直播流 | 实验能力，收藏列表及顺序重启后保留 |
| 定时停止 | 设置 1–60 分钟后由音箱自动停止播放 | 实验能力，关闭浏览器后仍有效 |
| Mock 开发模式 | 无需音箱即可运行完整网页和状态场景 | 不连接或修改真实设备 |

音量、静音、灯光、麦克风计划、闹钟和提醒尚未达到可靠写入标准，目前只展示可确认的状态或保持操作禁用。

## 工作方式

```text
手机或电脑浏览器
        │  HTTP / SSE（局域网 TCP 8787）
        ▼
音箱本机 sanyin-config
        │
        ├── 原厂服务状态与 D-Bus 事件
        ├── KPlayer UPnP AVTransport
        ├── 自有 AirPlay 恢复配置
        ├── 自有 Wi-Fi 回退事务
        └── 自有网络电台收藏
```

正式安装后，网页和 API 都在音箱本机运行，不需要电脑持续在线。电脑只在首次安装、更新或故障恢复时通过 USB ADB 使用。

## 快速安装

### 1. 准备环境

- 一台 macOS 电脑；
- Go 1.26 或更新版本；
- Node.js 20 或更新版本；
- 音箱底部 Mini-USB 数据线；
- 音箱与准备访问网页的设备处于可信局域网。

如果尚未安装 Go：

```bash
brew install go
go version
```

下载 Google 官方 ADB 并检查音箱：

```bash
./tools/get_adb_macos.sh
./tools/sanyinctl doctor
```

连接多个 ADB 设备时，请把示例序列号替换为 `adb devices` 的实际输出：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX doctor
```

### 2. 安装 AirPlay 守护服务

```bash
./tools/sanyinctl --serial netease_XXXXXXXX install
./tools/sanyinctl --serial netease_XXXXXXXX verify
```

该服务只在原厂 AirPlay 端口缺失且音箱网络已就绪时发送已验证的恢复命令。

### 3. 安装完整网页服务

```bash
npm run build:armv7
./tools/sanyinctl --serial netease_XXXXXXXX config-install
./tools/sanyinctl --serial netease_XXXXXXXX config-status
```

安装完成后，从路由器管理页面或系统网络列表找到音箱 IP，然后访问：

```text
http://音箱IP:8787/
```

完整操作步骤见 [三音 Plug 使用手册](docs/09-user-guide.md)。

## 网页怎么用

- **总览**：确认音箱、网络、AirPlay、播放器和后台服务是否正常；
- **AirPlay**：查看 TCP 5002，手动恢复 AirPlay，配置自动恢复；
- **播放**：播放音频 URL、调节本地播放音量、维护队列和电台、设置最长 60 分钟的定时停止；
- **网络**：查看当前 SSID，输入目标 Wi-Fi 和密码后执行带回退的切换；
- **音频与 EQ**：查看音频状态并切换固定 EQ 模式；
- **蓝牙**：发送开启或关闭请求，并查看最近一次验收结果；
- **诊断**：查看灯光、计划任务和归一化事件，不展示原始敏感日志。

页面中的“实验能力”表示正常路径已经通过实机验证，但异常网络、断电窗口、音源抢占或不同固件覆盖仍不完整。它不是模拟功能，也不等同于完全无风险。

## 播放与网络电台

媒体 URL 必须是音箱能够直接访问的 HTTP/HTTPS 音频文件或直播流，不能填写普通网页地址。播放请求由原厂 KPlayer 拉流，浏览器和安装电脑不代理音频。

- 普通曲目支持进度、暂停、恢复、停止和队列自动续播；
- 播放中可在 0–100 范围内调节 KPlayer 音量，写入后必须回读一致；暂停或停止时保持禁用；
- 服务重启或原厂入口接管播放后，会从 KPlayer 当前 URI 恢复正在播放的媒体名称；
- 网络电台保存到音箱自有配置目录，服务重启后仍会恢复；
- 电台首次缓冲可能需要约 6–12 秒；
- 定时停止范围为 1–60 分钟，在音箱端执行；
- 手动停止或清空队列会取消定时，换歌或切换电台不会取消；
- 配置服务重启后会清除定时器，避免旧任务误停。

只应添加可信媒体地址。API 对外返回 URL 时会去除用户信息、查询参数和片段，但完整地址仍会在音箱内存中交给 KPlayer 使用。

## Wi-Fi 切换与回退

Wi-Fi 切换由音箱端事务执行：先备份当前配置，再尝试连接目标网络，并验收目标 SSID、连接状态、IPv4、默认路由和网关可达性。

45 秒内未完成验收时，音箱会自动恢复原配置并重新联网。切换期间浏览器断开是正常现象，不代表事务停止。建议保留 USB ADB 连接，直到新地址能够访问。

## 登录认证

默认不设置网页登录密码，适合隔离且可信的家庭局域网。写请求仍要求同源检查和 CSRF 请求头，但同一网络中的其他设备仍可能访问页面。

需要密码时：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX config-auth-enable
./tools/sanyinctl --serial netease_XXXXXXXX config-password
```

用户名固定为 `admin`，密码由工具随机生成并保存在音箱自有配置目录。恢复默认无密码模式：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX config-auth-disable
```

## 常用命令

| 命令 | 用途 | 是否修改音箱 |
| --- | --- | --- |
| `sanyinctl doctor` | 检查 ADB、型号、系统和依赖 | 否 |
| `sanyinctl status` | 查看 AirPlay 守护服务、端口和日志 | 否 |
| `sanyinctl verify` | 从音箱和电脑两侧验证 AirPlay | 否 |
| `sanyinctl logs` | 查看 AirPlay 恢复日志 | 否 |
| `sanyinctl install` | 安装或更新 AirPlay 恢复服务 | 是，仅 overlay |
| `sanyinctl uninstall` | 移除 AirPlay 恢复服务 | 是，仅 overlay |
| `sanyinctl reboot` | 重启并等待 AirPlay 恢复 | 会重启设备 |
| `sanyinctl config-install` | 安装或更新完整网页配置服务 | 是，UDISK 与 overlay |
| `sanyinctl config-status` | 查看网页服务、8787 端口和认证状态 | 否 |
| `sanyinctl config-auth-enable` | 生成并启用网页登录密码 | 是，自有配置 |
| `sanyinctl config-auth-disable` | 禁用网页登录密码 | 是，自有配置 |
| `sanyinctl backup [目录]` | 备份 11 个 NAND 分区 | 仅读 NAND，使用 `/tmp` 中转 |

写入或重启命令会要求确认；自动化场景可加 `--yes`。目标设备存在内容不同的同名文件时，安装默认停止，可在核对后使用 `--force`。

## 本地开发

不连接音箱的 Mock 模式：

```bash
npm run dev
```

访问 [http://127.0.0.1:8787](http://127.0.0.1:8787)，可切换正常、AirPlay 离线、Wi-Fi 离线、控制器不可用、蓝牙未知、EQ 待应用、状态陈旧和操作失败等场景。

USB ADB 真实设备开发模式：

```bash
npm run dev:real
```

存在多个设备时：

```bash
go run ./service/cmd/sanyin-config -mode adb -serial netease_XXXXXXXX
```

开发模式默认只监听 `127.0.0.1:8787`，不会自动向局域网开放。

## 测试与构建

```bash
# Go API、适配器和网页逻辑
npm test

# 构建当前电脑可执行文件
npm run build

# 构建音箱使用的 Linux/ARMv7 单文件程序
npm run build:armv7
```

网页资源通过 Go `embed` 进入单文件程序，不依赖 CDN。构建产物位于 `dist/`，不会进入 Git 仓库。

## API

API 基础路径为 `/api/v1`：

- OpenAPI 3.1：[service/openapi.json](service/openapi.json)
- 字段与安全边界：[本地配置 API 契约](docs/08-local-config-api-contract.md)
- 能力与参数清单：[本地配置 API 与参数清单](docs/05-local-config-api-and-parameter-inventory.md)
- 动态回放证据：[本地配置协议确认记录](docs/06-local-config-protocol-verification.md)

Mock 与真实设备共用同一套 HTTP 契约。HTTP 路由不会直接访问 D-Bus 或系统命令，真实设备访问统一经过 `RealAdapter`。

## 文档导航

| 文档 | 内容 |
| --- | --- |
| [阶段更新说明](CHANGELOG.md) | 项目从 AirPlay 恢复工具扩展为本地增强服务的功能变化 |
| [使用手册](docs/09-user-guide.md) | 安装、网页功能、播放、Wi-Fi、认证、升级和恢复 |
| [硬件与固件](docs/01-hardware-and-firmware.md) | 已确认设备、分区与系统基线 |
| [AirPlay 修复原理](docs/02-airplay-repair.md) | 原生命令、守护策略和持久化方式 |
| [AirPlay 操作与恢复](docs/03-operations-and-recovery.md) | AirPlay 安装、验证、备份和卸载 |
| [故障排查](docs/04-troubleshooting.md) | ADB、端口、Bonjour、安装与备份问题 |
| [能力与参数清单](docs/05-local-config-api-and-parameter-inventory.md) | 当前能力等级、来源和风险边界 |
| [协议确认记录](docs/06-local-config-protocol-verification.md) | 静态分析、动态观察和独立回放证据 |
| [历史实施说明](docs/07-local-config-service-and-web-implementation-brief.md) | 从 Mock 原型到真实设备服务的实施记录 |
| [API 契约](docs/08-local-config-api-contract.md) | v1 HTTP、状态、错误与写操作语义 |

## 安全边界

- 不修改 bootloader、内核或只读 SquashFS；自有服务和配置位于 overlay 与 UDISK；
- 不直接修改厂商 SQLite，不把原厂无认证内部接口暴露给浏览器；
- `backups/`、`.tools/protocol-captures/` 和 `dist/` 不进入 Git；
- NAND 备份可能包含 Wi-Fi 密码、设备令牌和播放记录，不要上传网盘或公开仓库；
- 默认无密码模式只适合可信局域网，跨网段、访客网络或端口映射场景应启用认证；
- 不要把 TCP 8787 或原厂播放器端口暴露到互联网。

需要复制到其他电脑时可生成便携包：

```bash
./tools/package.sh
```

生成的 ZIP 不包含 Git 元数据、ADB、构建缓存和设备备份。
