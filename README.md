# 网易三音云音箱 AirPlay 恢复工具

本目录保存网易三音云音箱（SING 系列）AirPlay 恢复过程中的分析结论、设备端守护脚本、ADB 操作工具和 NAND 备份说明。

## 本地配置服务（Mock + 真实设备）

仓库现已包含本地配置 HTTP API、可切换场景的 `MockAdapter`、可在音箱本机运行的 `RealAdapter` 和移动端优先的交互网页。

- Mock 模式不连接设备，响应明确标记为 `environment=mock` 和 `source=mock`；
- 真实模式响应明确标记为 `environment=device`，状态来源为 `system`；
- 两种模式都不读取或修改厂商 SQLite；
- 当前 SSID 按用户配置需求返回；BSSID、MAC、IP、密码、设备标识、令牌与原始云端日志不会进入 API。
- AirPlay 恢复和自有自动恢复开关为安全写能力；蓝牙、EQ、Wi-Fi 与本地播放为明确标识的实验写能力，成功必须由设备事件或运行态回读验收；其他设置继续按能力矩阵禁用。

### 环境要求

- Go 1.26 或更新版本；
- Node.js 20 或更新版本（仅用于运行无依赖的前端测试）。

macOS 尚未安装 Go 时可执行：

```bash
brew install go
go version
```

### 本地启动

Mock 开发模式：

```bash
npm run dev
```

然后访问 [http://127.0.0.1:8787](http://127.0.0.1:8787)。也可直接使用：

```bash
go run ./service/cmd/sanyin-config
```

音箱通过 USB ADB 连接后，可在 Mac 上启动真实设备开发模式：

```bash
npm run dev:real
```

检测到多个 ADB 设备时可明确指定：

```bash
go run ./service/cmd/sanyin-config -mode adb -serial netease_XXXXXXXX
```

服务默认只监听 loopback。正式使用时通过 `config-install` 把 ARMv7 单文件服务、EQ 验收脚本和 procd 启动项安装到音箱；服务在音箱上监听局域网 TCP 8787。按当前产品约定，默认不设置登录密码，同一局域网内可直接访问；同源检查和 CSRF 请求头仍强制启用。真实设备模式下，网页操作会先显示确认说明，并在返回前验收设备状态或配置回读结果。

```bash
npm run build:armv7
./tools/sanyinctl --serial netease_XXXXXXXX config-install
./tools/sanyinctl --serial netease_XXXXXXXX config-status
./tools/sanyinctl --serial netease_XXXXXXXX config-password
```

安装后使用音箱当前局域网 IP 直接访问 `http://音箱IP:8787/`。如需可选的 Basic Auth，可运行 `sanyinctl config-auth-enable` 生成密码；`config-auth-disable` 恢复默认无密码模式，`config-password` 查看当前认证状态。

### 测试与构建

```bash
# Go API、MockAdapter 和前端逻辑测试
npm test

# 分别运行
go test ./service/...
npm run test:web

# 构建包含静态网页的单文件本机程序
npm run build

# 构建 Linux/ARMv7 静态程序，供后续 OpenWrt 部署验证
npm run build:armv7
```

构建结果为 `dist/sanyin-config`，网页资源通过 Go `embed` 进入程序，不依赖 CDN。

### API 与 Mock 场景

API 基础路径为 `/api/v1`。机器可读契约见 [`service/openapi.json`](service/openapi.json)，字段语义与安全边界见 [`docs/08-local-config-api-contract.md`](docs/08-local-config-api-contract.md)。

网页和所有读取接口支持以下场景：

- `healthy`
- `airplay_down`
- `wifi_offline`
- `controller_down`
- `bluetooth_unknown`
- `eq_pending`
- `stale_state`
- `operation_failed`

可以通过网页顶部选择，也可以直接请求，例如：

```bash
curl 'http://127.0.0.1:8787/api/v1/audio?scenario=eq_pending'
```

Mock 模式的设备写接口默认返回 `capability_not_ready`，AirPlay 页面只演示显式的 `simulate=true` 流程。真实设备模式当前开放：

- `POST /api/v1/airplay/recover`：恢复或确认 TCP 5002 监听；
- `PUT /api/v1/airplay/auto-recover`：持久修改自动恢复开关，原子写入自有配置并回读验收；
- `PATCH /api/v1/bluetooth`：请求体 `{"enabled":true|false}`，等待蓝牙服务对应成功事件；
- `PATCH /api/v1/audio/effect`：请求体 `{"mode":0..6}`，等待 `commonStatus.eqType` 事件，并独立回读 ADAU1761 硬件效果文件；
- `POST /api/v1/network/switch`：请求体包含 `ssid` 和 `password`；先备份当前配置，45 秒内未验收目标 SSID、IPv4、默认路由和网关可达则自动恢复原配置。
- `GET /api/v1/player`：读取 KPlayer 播放状态、进度、当前媒体、队列、网络电台和定时停止状态；
- `POST /api/v1/player/control`：播放 HTTP/HTTPS URL，暂停、恢复、停止、切歌并维护播放队列和网络电台；`timer_set` 可设置 1..60 分钟后由音箱自动停止，`timer_cancel` 取消定时。

蓝牙、EQ、Wi-Fi 和本地播放仍标记为 `experimental`，不会伪装成已经历所有异常和断电场景的安全能力。本地播放 URL 会被 KPlayer 直接访问，应只添加可信的音频或电台地址。音量、静音、灯光和计划任务尚未达到可可靠回读及恢复的标准，继续返回 `capability_not_ready`。

### 代码边界

```text
浏览器静态资源 web/
       │ HTTP + SSE
       ▼
service/internal/api
       │ DeviceAdapter
	   ├── MockAdapter（模拟场景）
	   └── RealAdapter（ADB/设备本机 shell + KPlayer UPnP AVTransport）
```

HTTP 路由不会直接访问 D-Bus、系统命令或厂商数据；真实设备访问统一经过 `RealAdapter`。电脑开发阶段使用 ADB runner，ARMv7 程序预留设备本机 runner，两者复用同一套 v1 契约和网页。

当前实机信息：

- 主控：Allwinner R16/A33，ARMv7
- 系统：OpenWrt，Linux 3.4.39
- 固件：`1.1.17.4`
- ADB 序列号示例：`netease_XXXXXXXX`（请以 `adb devices` 输出为准）
- 实机 AirPlay 名称：`三音云音箱-C931`
- AirPlay/RAOP 端口：`5002`

## 快速使用

连接音箱底部 Mini-USB 接口，然后执行：

```bash
# 首次使用：下载 Google 官方 macOS Platform Tools
./tools/get_adb_macos.sh

# 检查连接、硬件和修复环境
./tools/sanyinctl doctor

# 查看当前 AirPlay 状态
./tools/sanyinctl status

# 安装或重新安装持久化恢复服务
./tools/sanyinctl install

# 从音箱和 Mac 两侧验证
./tools/sanyinctl verify
```

如果连接了多个 ADB 设备，可显式指定序列号：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX status
```

也可以通过环境变量指定工具位置和设备：

```bash
ADB=/自定义路径/adb SANYIN_SERIAL=netease_XXXXXXXX ./tools/sanyinctl status
```

## 目录结构

```text
.
├── README.md
├── docs/
│   ├── 01-hardware-and-firmware.md
│   ├── 02-airplay-repair.md
│   ├── 03-operations-and-recovery.md
│   ├── 04-troubleshooting.md
│   ├── 05-local-config-api-and-parameter-inventory.md
│   ├── 06-local-config-protocol-verification.md
│   ├── 07-local-config-service-and-web-implementation-brief.md
│   └── 08-local-config-api-contract.md
├── service/
│   ├── cmd/sanyin-config/      # 本地服务入口
│   ├── internal/api/           # HTTP/SSE 路由与契约测试
│   ├── internal/adapter/       # DeviceAdapter 与 MockAdapter
│   ├── internal/domain/        # 领域模型
│   └── openapi.json            # OpenAPI 3.1 契约
├── web/
│   ├── src/                    # API 访问层、页面逻辑与样式
│   └── tests/                  # 前端状态与响应式逻辑测试
├── scripts/
│   ├── airplay_restore.sh       # 音箱端守护脚本
│   └── airplay_restore.init     # OpenWrt/procd 启动服务
├── tools/
│   ├── sanyinctl                # 日常操作统一入口
│   ├── get_adb_macos.sh         # 下载 Google 官方 ADB
│   ├── package.sh               # 生成不含设备数据的便携 ZIP
│   ├── disasm_elf.py            # ARM ELF 符号反汇编辅助工具
│   ├── sanyin_protocol_capture.sh # 只读协议抓包
│   ├── redact_protocol_capture.py # 抓包敏感字段脱敏
│   ├── player_live_test.mjs      # KPlayer URL/控制/队列/电台实机回归
│   └── requirements-analysis.txt
└── backups/                     # 本机 NAND 备份，不纳入 Git
```

## 常用命令

| 命令 | 用途 | 是否写入音箱 |
| --- | --- | --- |
| `sanyinctl doctor` | 检查 ADB、系统和必要组件 | 否 |
| `sanyinctl status` | 查看服务、端口与日志 | 否 |
| `sanyinctl verify` | 检查端口和 Bonjour 广播 | 否 |
| `sanyinctl logs` | 查看恢复日志 | 否 |
| `sanyinctl install` | 安装并启用恢复服务 | 是，仅 overlay |
| `sanyinctl backup [目录]` | 备份 11 个 NAND 分区 | 仅读 NAND；使用 `/tmp` 中转 |
| `sanyinctl uninstall` | 删除持久化恢复服务 | 是，仅 overlay |
| `sanyinctl reboot` | 重启并等待 AirPlay 恢复 | 会重启设备 |
| `sanyin_protocol_capture.sh` | 采集 D-Bus、增量服务日志和前后状态 | 否 |

执行写入或重启命令时，工具会要求确认；自动化场景可加 `--yes`。若目标设备已有同名但内容不同的脚本，安装会停止，可在确认后使用 `--force` 覆盖。

## 安全说明

- 修复不修改 bootloader、内核或只读 SquashFS，只在 `/overlay` 中增加两个小文件和启动链接。
- 写入任何裸 NAND 分区之前，必须先校验备份，并再次确认分区名和镜像大小。
- `backups/` 可能包含 Wi-Fi 密码、云端令牌、播放记录等敏感信息，不要上传网盘或公开仓库。
- 本工具仅针对已经验证过的三音固件结构。其他型号先运行 `doctor`，不要直接写入。

详细原理见 [AirPlay 修复原理](docs/02-airplay-repair.md)，完整操作见 [操作与恢复手册](docs/03-operations-and-recovery.md)。

需要复制到其他电脑时，可生成便携包：

```bash
./tools/package.sh
```

生成的 `dist/sanyin-airplay-toolkit-*.zip` 不包含 `.git`、`.tools` 和 `backups`。ADB 可从 [Google 官方 Android SDK Platform Tools 页面](https://developer.android.com/tools/releases/platform-tools) 获取，或在新 Mac 上运行包内的 `tools/get_adb_macos.sh`。
