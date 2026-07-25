# 本地配置 API 与参数清单

> 状态：方案、静态协议提取、首轮动态回放、HTTP 配置服务和 ADB `RealAdapter` 已实现；真实写能力按 S4 标准逐项开放。
> 采样设备：网易三音云音箱 `NeteaseC930`，固件 `1.1.17.4`。
> 最近实机快照：`2026-07-22 23:04 CST`。

## 1. 结论与边界

不能直接修改厂商的 `/mnt/UDISK/golangsql.db` 来实现网页配置。

- 该库由 `netease_control_center` 持有并写入，字段名为不透明的 `0_x`；它保存了云端下发的快照，并不代表实时状态。
- 实机中数据库的 `airPlayPrivilege=0`，但 `SPlayer` 仍在监听 TCP `5002`；这是 AirPlay 恢复服务通过 D-Bus 发送原生启动命令后得到的运行态。该例证明“改库”不能作为实时控制机制。
- 厂商库、Wi-Fi 密码、令牌、设备标识和历史数据只读且不纳入网页 API。网页服务只维护自有配置库和标准化状态。

本地服务的职责如下：

```text
浏览器
  │ HTTP + SSE
  ▼
本地配置服务
  ├── 自有 SQLite：期望配置、版本、回滚信息、审计记录
  ├── D-Bus 适配器：发送 SmartAudio 命令、订阅 Notify 状态
  ├── 系统适配器：进程、端口、Wi-Fi 链路等只读检查
  └── 启动对账：服务就绪后，按自有配置恢复已验证的设置
```

### 1.1 状态来源优先级

| 优先级 | 来源 | 用途 |
| --- | --- | --- |
| 1 | D-Bus `Notify`、服务端口、设备状态 | 网页显示的实时状态与写入后的验收依据 |
| 2 | 自有 SQLite | 用户期望状态、重启恢复、失败回滚 |
| 3 | 厂商 `golangsql.db` | 只读分析与初始迁移参考，不作为写入目标 |

## 2. 已确认的设备与服务信息

| 项目 | 当前实机信息 | 可读取 | 说明 |
| --- | --- | --- | --- |
| 设备主机名 | `NeteaseC930` | 是 | 来自内核主机名 |
| 固件 | `1.1.17.4`，OpenWrt/Tina Neptune | 是 | 只读设备信息 |
| AirPlay | `SPlayer` 运行，TCP `5002` 监听 | 是 | 已由现有恢复服务验证 |
| 厂商播放 | `KPlayer` 运行，TCP `5005` 监听 | 是 | 仅做状态采集 |
| 控制中心 | `netease_control_center` 运行，监听 `1705`、`6060` | 是 | `1705` 仅注册 Wi-Fi、时间、云听、EQ 等原厂专用路由；`6060` 为运行指标/调试服务，没有通用配置路由，均不能当作网页 API 复用 |
| Wi-Fi | 已连接，动态查询信号约 `-42 dBm` | 是 | `0x0b05/0x0b06` 已回放；SSID、BSSID、密码不返回、不落盘 |
| 闹钟服务 | `alarmer` 运行 | 是，云端同步 | `0xf000/0xf001` 已回放；Controller 会请求厂商云接口，当前闹钟 0 条 |
| 蓝牙服务 | `netease.ihw.bt` D-Bus 服务在线 | 部分 | 开关命令、成功事件和重复调用已达到 S3，尚缺无副作用状态查询 |
| 可用空间 | overlay 约 7.4 MiB；UDISK 约 46 MiB | 是 | 启动脚本应小；服务、静态资源和自有数据优先放 UDISK |

## 3. D-Bus 实现约定

设备采用会话 D-Bus，不使用可自描述的标准属性接口。已确认的服务包括：

`netease.ihw.bt`、`netease.ihw.wifi`、`netease.ihw.alarm`、`netease.ihw.kplayer`、`netease.ihw.splayer`、`netease.ihw.voice_engine`、`netease.ihw.controller`。

已观察到的蓝牙事件格式：

```text
对象路径：/netease/ihw/bt
接口：netease.ihw.SmartAudio
成员：Notify
```

`dbus-monitor` 仅用于逆向和诊断。正式配置服务必须以 D-Bus 客户端连接总线，订阅相关 `Notify` 信号，并把原始命令码、位掩码和 JSON 负载转换成稳定的业务状态；网页不得直接接触原始 D-Bus 消息。

AirPlay 的启动命令已验证：向 `netease.ihw.splayer` 的 `/netease/ihw/splayer` 发送 `netease.ihw.SmartAudio.API`，命令为 `7425`，目标掩码为 `2048`，负载含端口和设备名称。现有 `airplay_restore.sh` 已封装该操作。

## 4. 参数清单

标记含义：

- **可读取**：已找到稳定的实机来源，或已由启动日志/运行态响应验证。
- **可安全写入**：当前就能通过已验证协议写入，并可验证结果与回滚；不是“理论上可修改文件”。
- **需要协议验证**：已确认功能/服务存在，但尚未获得完整命令、参数、成功判定和回滚方案。
- **云端专属**：依赖账号、内容服务或云端设备关系；本地配置页不实现等价功能。

| 参数 ID | 设置含义与当前实机值 | 真实来源 | 可读取 | 可安全写入 | 需要协议验证 | 云端专属 | 规划 API |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `device.info` | 主机名、固件、服务版本 | 系统文件、进程 | 是 | 不适用 | 否 | 否 | `GET /device` |
| `device.health` | AirPlay/KPlayer/控制中心/闹钟服务及端口状态 | 进程、端口、D-Bus owner | 是 | 不适用 | 否 | 否 | `GET /status` |
| `player.localPlayback` | HTTP/HTTPS 音频 URL、0..100 播放音量、队列、可排序网络电台及最长 60 分钟定时停止 | KPlayer UPnP AVTransport + RenderingControl + 自有后台队列与内存定时器管理；电台及顺序写入自有配置 | 是，含传输状态、当前 URI、音量、进度与倒计时 | **实验性：正常控制、当前媒体恢复、音量写入回读、手动/自动续播、电台排序持久化和定时停止已验收** | 是：异常格式、断网、AirPlay/蓝牙抢占和断电窗口仍需覆盖 | 否 | `GET /player`；`POST /player/control` |
| `airplay.runtime` | `SPlayer` 运行，5002 监听 | 进程、端口、D-Bus | 是 | 不适用 | 否 | 否 | `GET /airplay` |
| `airplay.recover` | 恢复 AirPlay 监听 | 已验证的 `SPlayer` D-Bus 启动命令 | 是 | **是，仅恢复/启用** | 否 | 否 | `POST /airplay/recover` |
| `airplay.enabled` | 厂商快照为 `airPlayPrivilege=0`，运行态由守护服务维持可用 | 厂商库 + D-Bus | 是 | 否 | 是：关闭、持久策略与守护协调 | 否 | 暂不开放普通开关 |
| `airplay.name` | 当前名称为 `三音云音箱-C931` | 现有启动负载、Bonjour | 是 | 否 | 是：改名后的 Bonjour/服务重启验证 | 否 | 预留 `PATCH /airplay` |
| `audio.volume` | 重启后系统音量 40%；曾观察 AirPlay 会话音量 27% 对应瞬时硬件档位 6/20 | `commonStatus`、`sysCurVol`、AirPlay `0x1e05`；制造测试 `0x2008/0x2009` | 是，需区分系统/会话/硬件三层 | 否 | 是：硬件读写已达 S3，但 `0x200a` 不同步业务状态；AirPlay 音量由发送端控制 | 否 | 预留 `PATCH /audio` |
| `audio.outputMuted` | 当前为 `false` | 控制中心状态广播 | 是 | 否 | 是 | 否 | 预留 `PATCH /audio` |
| `audio.micMuted` | 当前为 `false`；实体 `KEY_HOME(102)` 双击可切换 | Controller `SetMicClose` 日志、灯光和周期状态广播 | 是 | 否 | 是：实体路径已达 S2，但没有可调用业务协议；禁止原始 input 注入 | 否 | 预留 `PATCH /microphone` |
| `audio.effect` | 当前选中态和云端值为 `1`（智能），硬件文件为 `Normal.bin`；已完成重启持久性及网易本地播放中的 `1→2→1` 硬件切换验证 | `commonStatus.eqType`、`soundEffectKey`、本地 `/eq/:mode`、播放器状态、硬件文件校验 | 是，但 API 必须分开返回业务选中模式和硬件已应用模式 | 否 | 是：选中态、云端持久性和本地播放中 Normal/Vocal 硬件切换均达到 S3；模式 3..6 共用同一加载路径，不再要求逐项回放，仍缺离线报告失败、服务异常和超时回滚 | 否，但原厂路径会联网报告并由云端值覆盖本地快照 | 预留 `GET/PATCH /audio/effect` |
| `lighting.iconEnabled` | 持久快照 `iconLedSwitch=1` | 厂商库、播放日志 | 是 | 否 | 是：灯控 D-Bus 命令与回读 | 否 | 预留 `GET/PATCH /lighting` |
| `lighting.brightness` | 持久快照 `directLedBrightness=100` | 厂商库 | 是 | 否 | 是：亮度范围、实时回读 | 否 | 预留 `PATCH /lighting` |
| `lighting.playMode` | 持久快照 `iconLedMusicPlayMode=2` | 厂商库、播放日志 | 是 | 否 | 是：模式编号的语义与回读 | 否 | 预留 `PATCH /lighting` |
| `microphone.schedule` | 已保存 `mainSwitch=true`，17:00–次日 06:30；当前麦克风未静音 | 厂商 `cfglist/micCron`、运行态日志 | 是，当前只能从厂商快照和日志诊断 | 否 | 是：已确认参数和计划回调，但入口为 App/语音/云端配置链；缺少本地无副作用读写协议 | **是，当前下发链依赖厂商通道** | 仅保留诊断读取，暂不提供 PUT |
| `alarm.items` | 当前为空数组 | Controller 调用厂商云后以 `0xf001` 同步给 `alarmer` | 是，需联网 | 否 | 是：离线持久化、冲突和增删语义 | **是，当前同步依赖厂商云** | 仅保留诊断读取 |
| `reminder.items` | 当前为空数组 | 与闹钟相同的云端同步链路，命令为 `0xf005/0xf006` | 是，需联网 | 否 | 是：离线持久化、冲突和增删语义 | **是，当前同步依赖厂商云** | 仅保留诊断读取 |
| `wifi.connection` | 已连接；当前 SSID 由本机 `wpa_cli` 安全读取，RSSI 由 `iwconfig` 分级 | `wpa_cli`、`iwconfig`、受保护的配置事务 | 是，返回当前 SSID但不返回密码/BSSID/MAC/IP | **实验性：失败回退和启动恢复已验收** | 是：仍需扩大新网络与断电窗口测试 | 否 | `GET /network`；`POST /network/switch` |
| `bluetooth.enabled` | 已完成语音观察、独立回放和重复调用；尚缺无副作用状态查询、连接中关闭、重启和异常回滚 | `netease.ihw.bt` D-Bus | 部分 | 否 | **是，已达到 S3 独立回放** | 否 | 预留 `GET/PATCH /bluetooth` |
| `bluetooth.devices` | 未确认可安全读取已配对/已连接设备列表 | 蓝牙服务 | 否 | 否 | 是 | 否 | 暂不开放 |
| `cloudListen.enabled` | 持久快照为 `status=1`，运行态 `yunTingMode=false` | 厂商库、控制中心状态 | 是 | 否 | 是 | **是，内容服务依赖云端** | 只在诊断状态中展示 |
| `player.sleepTimer` | 截图中存在，但尚未定位当前状态或本地写协议 | 播放器/控制中心 | 否 | 否 | 是 | 否 | 暂不开放 |
| `player.history` | 云端播放历史/账号数据 | 云端与 NIM 数据 | 否 | 否 | 否 | 是 | 不实现 |
| `voice.shortcuts` | 截图中的快捷指令未发现独立本地持久化模型 | 云端任务模型 | 否 | 否 | 否 | 是 | 不实现 |
| `speaker.switch` | App 中选择账号下的其他音箱，不是当前设备的本地设置 | 云端设备绑定关系 | 否 | 否 | 否 | 是 | 不实现 |
| `account.unbind` | 解绑账号 | 云端账号关系 | 否 | 否 | 否 | 是 | 不实现 |
| `support.feedback` | 帮助与反馈 | 云端服务 | 否 | 否 | 否 | 是 | 不实现 |

## 5. API 实现状态

基础路径为 `/api/v1`。所有响应应包含 `observedAt`、`source` 和 `revision`；写接口还应包含 `operationId`、`applied`、`verified`、`rollbackAttempted`。

| 方法与路径 | 作用 | 当前状态 |
| --- | --- | --- |
| `GET /capabilities` | 返回每个参数的四类标记及接口可用性 | Mock/真实设备均已实现 |
| `GET /device` | 返回脱敏后的设备信息、固件与存储余量 | 真实设备已实现 |
| `GET /status` | 返回 Wi-Fi、AirPlay、播放器、音频、闹钟服务等实时状态 | 真实设备已实现；无可靠来源的字段返回 `unknown` |
| `GET /events` | SSE 推送状态变化；不透传原始 D-Bus 数据 | 已实现快照事件 |
| `GET /airplay` | 返回运行态、端口、恢复服务及自动恢复设置 | 真实设备已实现 |
| `POST /airplay/recover` | 调用已验证的恢复动作，并验证 5002 监听 | 真实设备已实现并验收 |
| `PUT /airplay/auto-recover` | 修改自有的自动恢复开关 | 已实现原子写入、回读和失败回滚 |
| `GET /network` | 返回当前 SSID、链路状态、信号等级和最近切换结果 | 真实设备已实现，不返回密码、BSSID、MAC 或 IP |
| `GET /player` | 返回 KPlayer 传输状态、进度、当前媒体、队列和网络电台 | Mock/真实设备均已实现；媒体 URL 对外脱敏 |
| `POST /player/control` | URL 播放、暂停、恢复、停止、切歌、队列和网络电台管理 | 原厂 KPlayer 实机正常路径已验收；电台以 `0600` 权限持久化，能力标记为 `experimental` |
| `GET /bluetooth` | 返回服务在线状态；当前开关无法可靠读取时为 `unknown` | 真实设备已实现只读语义 |
| `POST /network/switch` | 切换 Wi-Fi | 已实现配置备份、目标 SSID/IPv4/默认路由验收、45 秒失败回退和启动时未完成事务恢复；能力标记为 `experimental` |
| `PATCH /bluetooth` | 开/关蓝牙 | 无副作用回读、连接中关闭、异常和重启测试完成前返回 `capability_not_ready` |
| `PATCH /audio`、`PATCH /lighting` | 修改音频、灯效 | 协议验证完成前不开放 |
| `/alarms`、`/reminders`、`/microphone/schedule` | 本地计划任务管理 | 闹钟/语音命令与重启恢复验证完成前不开放 |

示例能力响应：

```json
{
  "id": "bluetooth.enabled",
  "readable": "partial",
  "safeWritable": false,
  "requiresProtocolVerification": true,
  "cloudOnly": false,
  "reason": "开关协议已达 S3，但尚无无副作用当前状态查询，未完成连接中关闭、异常和重启测试"
}
```

## 6. 写入与实时生效标准

每一个写接口必须遵循以下事务，不满足则不开放：

1. 读取并保存旧的**期望状态**和当前**观测状态**。
2. 校验输入范围、服务在线状态与互斥条件。
3. 发送已验证的 D-Bus/系统命令，绝不编辑厂商 SQLite。
4. 等待匹配的 `Notify` 或主动读取运行态；例如 AirPlay 检查 5002 监听，Wi-Fi 检查获得 IPv4。
5. 验证成功后，写入自有 SQLite 的期望状态与审计日志。
6. 超时或失败时下发旧值；网络切换还必须保留原网络，并在新网络验证失败后自动恢复。
7. 重启后，待依赖服务就绪再按自有 SQLite 对账和补偿下发。

## 7. 蓝牙测试项

蓝牙是第一项“监听—写入—网页同步”验证项。`2026-07-22` 已通过现有语音能力分别执行“打开蓝牙”和“关闭蓝牙”，确认：

1. 打开命令为 `0x0b1b`，目标掩码 `256`，空负载；成功事件为 `0x0b2f`。
2. 关闭命令为 `0x0b1c`，目标掩码 `256`，空负载；成功事件为 `0x0b30`。
3. 设备使用 Broadcom BSA，`hciconfig` 没有可用控制器输出，不能作为主要回读依据。
4. 已完成独立回放和重复开关幂等性；尚需连接中关闭、无副作用状态查询、超时回滚和重启状态验证。

完整记录见 [本地配置协议确认记录](06-local-config-protocol-verification.md)。产出成功判定、失败码和回滚逻辑后，才将 `PATCH /bluetooth` 从“需要协议验证”提升为“可安全写入”。

## 8. 安全要求

- 配置网页仅允许局域网访问；按当前产品约定默认不设置登录密码，但保留可选 Basic Auth、同源检查和 CSRF 防护。无密码时，同一局域网内的其他用户也可以操作设备。
- API 与日志不得返回 Wi-Fi 密码、BSSID、蓝牙 MAC、设备 UUID、云端令牌、NIM 聊天/历史数据库内容；当前 SSID 按用户明确需求返回。
- 网页只显示归一化状态；原始 D-Bus 报文仅在受保护的本机诊断日志中短期保存。
- 所有可写操作均需记录审计信息；网络、解绑、恢复出厂等高风险动作必须二次确认。

## 9. 下一步

1. 完成蓝牙连接中关闭、无副作用回读、异常回滚及重启验证。
2. 已完成首批只读 API、ADB `RealAdapter`、KPlayer 本地播放和 ARMv7 音箱本机部署；下一步补齐音量的可靠业务状态源。
3. 已完成真实 AirPlay 恢复、自动恢复配置及 OpenWrt 本机 runner 验收；后续扩大异常和断电窗口覆盖。
4. 找到能同步 Controller 业务状态的音量入口；制造测试 `0x200a` 不直接用于网页写入。
5. 按“命令—状态事件—回滚—重启恢复”标准逐项验证 Wi-Fi 和灯效；禁麦计划在找到本地业务入口前保持只读诊断。
6. 闹钟和提醒按云端耦合能力处理，不将 D-Bus 同步响应误当成本地持久化接口。
7. 只有 API 能力稳定后，再设计网页信息架构和页面。
