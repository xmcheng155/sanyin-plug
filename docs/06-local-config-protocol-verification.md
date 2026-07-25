# 本地配置协议确认记录

> 设备：网易三音云音箱 `NeteaseC930`
> 固件：`1.1.17.4`
> 首次确认时间：`2026-07-22`
> 原则：静态枚举只作为候选；必须经过动态观察、独立回放、状态验收和回滚测试后才能开放写接口。

## 1. 证据等级

| 等级 | 条件 | 能力结论 |
| --- | --- | --- |
| S1 静态候选 | 在固件符号、DWARF、字符串或反汇编中定位到命令 | 不允许调用 |
| S2 动态观察 | 由原厂语音/App/按键触发，并抓到完整调用和状态事件 | 可建立协议映射，仍不允许网页写入 |
| S3 独立回放 | 本地按相同参数调用，真实状态发生预期变化 | 可进入受控测试 |
| S4 安全写入 | 回读、幂等、超时、失败回滚和重启测试均通过 | 可将 `safeWritable` 标为 `true` |

## 2. 固件和通信基线

当前固件中的主要服务二进制包含调试信息且未剥离符号，包括：

- `netease_control_center`
- `netease_voice`
- `app_wifi_manager`
- `app_nevsps_bt`
- `ihwplayer`、`KPlayer`、`SPlayer`
- `alarmer`

会话总线已确认存在以下服务：

`netease.ihw.controller`、`netease.ihw.voice_engine`、`netease.ihw.player`、
`netease.ihw.kplayer`、`netease.ihw.splayer`、`netease.ihw.bt`、
`netease.ihw.wifi`、`netease.ihw.alarm`、`netease.ihw.skins`、`netease.ihw.ota`。

这些对象虽然响应 `Introspect`，但返回空节点，无法依赖 D-Bus 自描述信息获得方法和参数。

### 2.1 `SmartAudio.API` 参数

动态抓包确认普通调用使用五个参数：

1. 来源模块编号 `uint32`；
2. 目标模块位掩码 `uint32`，通常为 `1 << 模块编号`；
3. 命令编号 `uint32`；
4. 负载 UTF-8 字节数 `uint32`；
5. JSON 或普通字符串负载。

`SmartAudio.Notify` 广播使用四个参数：来源模块、目标/订阅掩码、命令编号、字符串负载。

### 2.2 已提取的模块编号

| 模块 | 编号 | 目标掩码 |
| --- | ---: | ---: |
| Controller | 0 | 1 |
| Alarm | 1 | 2 |
| Voice engine | 3 | 8 |
| Player | 4 | 16 |
| Configure | 5 | 32 |
| Wi-Fi | 7 | 128 |
| Bluetooth | 8 | 256 |
| KPlayer | 9 | 512 |
| SPlayer | 11 | 2048 |
| Light MCU | 13 | 8192 |
| Manufacture | 16 | 65536 |
| Skins | 18 | 262144 |

## 3. 蓝牙开关动态确认

设备使用 Broadcom BSA 服务，不是标准 BlueZ 管理路径。因此 `hciconfig` 没有可用控制器输出，不能作为蓝牙开关的主要回读来源。

### 3.1 打开蓝牙

通过原厂语音“打开蓝牙”触发，抓到：

| 字段 | 值 |
| --- | --- |
| D-Bus 目标 | `netease.ihw.bt`、`/netease/ihw/bt` |
| 方法 | `netease.ihw.SmartAudio.API` |
| 来源模块 | `0`，Controller |
| 目标掩码 | `256`，Bluetooth |
| 请求命令 | `0x0b1b` / `2843`，`CMD_BT_BREDR_ENABLE` |
| 负载 | 长度 `0`，空字符串 |
| 状态事件 | `0x0b2f` / `2863`，`CMD_BT_AVK_CRTED` |
| 服务日志 | `CMD_BT_AVK_OPEN`，等待启用结果为 `0` |
| 原厂反馈 | “蓝牙已打开” |

### 3.2 关闭蓝牙

通过原厂语音“关闭蓝牙”触发，抓到：

| 字段 | 值 |
| --- | --- |
| D-Bus 目标 | `netease.ihw.bt`、`/netease/ihw/bt` |
| 方法 | `netease.ihw.SmartAudio.API` |
| 来源模块 | `0`，Controller |
| 目标掩码 | `256`，Bluetooth |
| 请求命令 | `0x0b1c` / `2844`，`CMD_BT_BREDR_DISABLE` |
| 负载 | 长度 `0`，空字符串 |
| 状态事件 | `0x0b30` / `2864`，`CMD_BT_AVK_DELETED` |
| 服务日志 | 关闭可发现/可连接状态，控制中心收到 `CMD_BT_AVK_DELETED` |
| 原厂反馈 | “蓝牙已关闭” |

当前证据等级为 **S3 独立回放**。本地使用相同的五参数 `SmartAudio.API` 调用成功复现了打开和关闭，并在每次调用后分别收到 `0x0b2f` 和 `0x0b30`。

幂等测试结果：

- 已关闭时再次发送 `0x0b1c`，仍返回 `0x0b30`；
- 已打开时再次发送 `0x0b1b`，仍返回 `0x0b2f`；
- 测试结束后已发送关闭命令，恢复到测试前状态。

因此正常路径和重复调用已经确认。完成连接中关闭、服务异常、重启后状态及超时回滚测试之前，`PATCH /bluetooth` 仍保持不可用。

### 3.3 其他蓝牙线索

控制中心会周期性发送 `0x200c`（`CMD_TEST_BT_ADDR_REQ`），蓝牙服务以 `0x200d` 返回本机地址。该负载包含蓝牙 MAC，只可用于本机诊断，不得进入网页 API 或普通日志。

## 4. 音量与音频状态确认

### 4.1 原厂业务路径

通过原厂语音分别设置 50% 和 40%，动态确认：

- 语音识别结果命令为 `cmd=3021`，`detail.type=2`，`detail.value` 为目标百分比；
- Controller 随后广播 `CMD_CONTROLLER_RSPMSG_PLAYERSTATUS=0x0a0a`；
- `commonStatus.volPer` 分别变为 `50` 和 `40`，`volScale=20`，`action=4`；
- 底层音量分别为档位 `10` 和 `8`，所以系统音量路径为 20 档、每档 5%；
- 音量测试结束后曾恢复并复读为档位 `8`，即系统音量 40%。

语音入口最终调用 `modules/player.VolChangeByPercent`，再调用 `/dev/adau1761` 的 `SetVol`，并触发 Controller 业务状态广播。动态抓包中没有出现 Controller 向 Player 发送独立的音量设置 D-Bus 调用。

### 4.2 制造测试读写通道

反汇编和独立回放确认 Controller 还保留一组制造测试命令：

| 操作 | 请求 | 响应 | 负载 |
| --- | --- | --- | --- |
| 读取硬件音量 | `0x2008` / `8200` | `0x2009` / `8201` | 请求为空；响应 `{"vol":8}` |
| 设置硬件音量 | `0x200a` / `8202` | `0x200b` / `8203` | 请求和响应均为 `{"vol":8}` |

请求发往 `netease.ihw.controller`，来源模块为 Manufacture `16`、目标掩码为 Controller `1`。Controller 的响应发往 `netease.ihw.manufacture`，来源模块为 `0`、目标掩码为 `65536`。

该通道有三个重要边界：

1. 字段必须是整数 `vol`，范围由底层钳制为 `0..20`；不是语音结果中的 `volume` 或百分比。
2. 未知字段不会使 D-Bus 调用失败。例如 `{"volume":8}` 会被解析为默认值 0 并真的写入，因此调用前必须做严格 JSON Schema 和范围校验。
3. 该分支直接调用 `adau1761.SetVol`，没有调用 `TrigBroadcastStatus`。它能改变硬件，但尚未证明会同步 `player.sysVolPer`、`commonStatus` 和云端会话状态。
4. `GET_VOLUME` 读到的是编解码器当前硬件档位，不一定是用户保存的系统音量。AirPlay 播放过程中，发送端通过 `0x1e05` 下发 `{"volume":27}` 后，Controller 仍记录 `sysCurVol=8 / 40%`，但投影音量变为档位 `6`，广播的 `commonStatus.volPer` 变为 `27`。

所以制造测试音量通道只达到“**硬件层 S3**”，不能直接作为 `PATCH /audio`。正式实现还需找到能同时更新业务状态的入口，或证明写入后可安全完成状态对账。

这也说明 `audio.volume` 至少包含两层语义：保存的系统音量，以及 AirPlay 等当前播放源投影到硬件的会话音量。API 不能把 `0x2008` 的瞬时硬件档位直接展示成系统音量，也不能在播放期间用制造命令覆盖发送端音量。

`commonStatus` 还能被动提供 `muteMic`、`muteVolume` 和 `eqType`。主动请求 `0x0a09` 的空负载没有得到 `0x0a0a` 响应，说明主动刷新仍缺原厂请求参数，当前只能依赖已有广播或会话创建消息。

### 4.3 麦克风实体开关

实体操作的禁麦和恢复已经完成成对采集：

| 项目 | 禁麦 | 恢复 |
| --- | --- | --- |
| 输入设备 | `/dev/input/event2`，`r16kbd` | 相同 |
| 输入事件 | `KEY_HOME(102)` 双击；单击记录为 `do nothing` | 相同双击切换 |
| Controller | `SetMicClose IsMicClose:true` | `SetMicClose IsMicClose:false` |
| 灯光 | `Light Direct led: 40 1` | `Light Direct led: 40 2` |
| 提示音 | `/rom/usr/share/resource/voice/b-m-1.mp3` | `/rom/usr/share/resource/voice/b-m-2.mp3` |
| 审计事件 | `H111 status=true` | `H111 status=false` |

实体路径没有产生 `0x0a0b/0x0a0c` 调用，而是由 Controller 的按键处理直接进入 `SetMicClose`。恢复后周期状态广播确认 `commonStatus.muteMic=false`。

因此麦克风实体行为达到 **S2 动态观察**，但尚无可供网页调用的业务协议。不要直接写麦克风 I²C，也不要用零时间戳的原始 `input_event` 模拟按键：实测会被 Controller 计算成超长按键并进入 BLE 配网。该异常已通过重启恢复，重启后确认 `curTask=0`、`deviceMode=4`、原 Wi-Fi 正常、`muteMic=false` 和 AirPlay 端口恢复。

### 4.4 禁麦计划 `micCron` 配置链

符号、DWARF 和反汇编已确认禁麦计划的完整内部结构：

```json
{
  "startT": 1534496400000,
  "endT": 1534458600000,
  "updateT": 1611324572000,
  "mainSwitch": true,
  "lightSwitch": false
}
```

`CronMic` 结构依次包含 `StartTimeStamp int64`、`EndTimeStamp int64`、`UpdateTimeStamp int64`、`MainSwitch bool` 和 `LightSwitch bool`。实机当前快照被换算为每日 17:00 到次日 06:30，时间戳中的日期不是一次性执行日期，不能按普通绝对时间直接改写。

调用链已经定位为：

```text
App / 语音结果 / 厂商 HTTP 配置刷新
  -> globals.SetAndSaveCfgList
  -> globals.SetCfgList
  -> CfgKey("micCron") 注册回调
  -> JSON 解析并比较新旧 CronMic
  -> UpdateMicStatus + CrontabInit + UpdateCronTime
  -> micCronStart / micCronEnd
  -> SetMicClose 与灯光反馈
```

关键静态证据：

- `cronMic.go` 初始化时以长度 7 注册键 `micCron`，回调为 `init.2.func1`；
- 回调参数为 `CallCbType` 和 `interface{}`，只接受字符串，解析 `CronMic` 后比较新旧值；变化或强制调用会重算并重建计划；
- `AppSuccessCommand` 和 `MscSuccessCommand` 都会调用 `SetAndSaveCfgList`；启动流程还会调用 `ForceExecCfgListCb`；
- App 只读命令枚举为 `APP_CMD_CFGLIST_GET=5998`，响应为 `5999`，但结果通过 `SendCmdByYunxin` 返回厂商云信通道，不是本地 D-Bus 返回值；
- App 配置变化会保存整个 `CfgListType`，不是面向 `micCron` 的独立事务。

当前会话总线没有 `netease.ihw.configure` owner。控制中心 TCP 1705 静态确认只注册 `/wificonfig`、`/connect`、`/httpenv/:mode`、`/test`、`/unixtime`、`/cloudlisten` 和 `/eq/:mode`，没有 `cfglist` 或通用配置路由。因此 `micCron` 虽然达到 **S1 完整实现确认**，目前仍是厂商 App/语音/云端配置能力，不是可直接复用的本地 API。

本轮没有执行“当前值回写”。相同 JSON 仍会经过保存，并可能由强制回调重建计划；时间字段又采用“旧日期承载每日时刻”的语义，在没有独立回读、状态事件和原子回滚前不能视为无副作用操作。

### 4.5 音效/EQ 本地路由

控制中心在 `0.0.0.0:1705` 注册了 `GET /eq/:mode`。处理函数把 `mode` 解析为整数后直接调用 `globals.EffectUpdate`，HTTP 响应为 `200`、空响应体；实际效果加载在异步 goroutine 中完成，所以 HTTP 成功不等于硬件已经切换。

DWARF 常量给出了完整的业务编号：

| `mode` | 固件常量 | 业务名称 | ADAU1761 文件 |
| ---: | --- | --- | --- |
| 0 | `EFFECT_TYPE_NORMAL` | 普通 | `Normal.bin` |
| 1 | `EFFECT_TYPE_INTELLIGENT` | 智能 | `Normal.bin` |
| 2 | `EFFECT_TYPE_VOCAL` | 人声 | `Vocal.bin` |
| 3 | `EFFECT_TYPE_LIVE` | 现场 | `Live.bin` |
| 4 | `EFFECT_TYPE_DOUBLE_BASS` | 重低音 | `double_bass.bin` |
| 5 | `EFFECT_TYPE_ELECTRONIC_MUSIC` | 电子乐 | `Electronic_music.bin` |
| 6 | `EFFECT_TYPE_ACG` | ACG | `ACG.bin` |

业务编号大于 0 时，`SetEffect` 会先减 1，再索引六个硬件效果文件，因此业务模式 0 和 1 最终都使用 `Normal.bin`。当固件决定立即应用时，底层会把选中的文件复制到 `/lib/firmware/adau1761.bin`，再通过 `/dev/adau1761` ioctl 加载，并恢复原音量。

但 `EffectUpdate` 并不无条件调用 `SetEffect`。它先通过 `modules/player.getEffPara` 获取 `(set_now, set_type)`，只有 `set_now=true` 才加载硬件效果；无论是否加载，随后都会更新选中态、广播 `commonStatus.eqType` 并向云端报告。因此 `eqType` 表示业务选中模式，不能单独作为硬件已经生效的证据。

动态验证过程：

1. 操作前，周期状态为 `commonStatus.eqType=1`，云端配置为 `soundEffectKey={"soundEffect":1}`，`/lib/firmware/adau1761.bin` 与 `Normal.bin` 的 MD5 一致；
2. 本地调用 `/eq/2` 后，控制中心记录 `SoundEffect:2, set_now:false, set_type:0`，广播 `0x0a0a/commonStatus.eqType=2`，并以 `informType=113` 向厂商服务器成功上报 `{"soundEffect":2}`；硬件文件仍与 `Normal.bin` 一致，并未切为 `Vocal.bin`；
3. 第一次重启后，启动流程先从本地数据库读到旧值 1；约 19 秒后云端 `cfglist` 返回 2 并覆盖运行态，最终广播 `eqType=2`。这确认 `/eq/2` 上报的选中模式已由云端持久化；
4. 调用 `/eq/1` 后，同样记录 `set_now:false`，选中态广播和云端报告恢复为 1；
5. 第二次重启后，本地数据库先读到上一轮的值 2；约 19 秒后云端返回 1 并覆盖，最终广播 `eqType=1`。这确认恢复值也已由云端持久化，同时证明启动期间存在“本地旧快照→云端最终值”的短暂过渡；
6. 整个 `1→2→重启→1→重启` 过程中，硬件文件始终与 `Normal.bin` 一致。这不是“硬件切换后成功回滚”的证据，而是证明空闲/AirPlay 场景只改变业务选中态，没有立即应用 Vocal 硬件效果；
7. 随后通过语音随机播放启动网易 `KPlayer`，状态为 `musicStatus.status=3` 且播放进度持续增长。在该播放态调用 `/eq/2`，日志变为 `SoundEffect:2, set_now:true, set_type:2`，固件复制 `Vocal.bin` 并把 `curEffectType` 从 0 更新为 1，目标文件 MD5 与 `Vocal.bin` 一致；
8. 播放未中断时调用 `/eq/1`，日志为 `set_now:true, set_type:0`，固件复制 `Normal.bin` 并把 `curEffectType` 从 1 恢复为 0，目标文件 MD5 与 `Normal.bin` 一致；两次切换后歌曲状态仍为播放中，进度继续增长，音量百分比保持不变。

该路径的“选中态变更、状态广播、云端持久性和网易本地播放中的 Normal/Vocal 硬件切换”均达到 **S3 独立回放**，但仍不能直接标为安全写入：

- 原厂参数校验只有 `mode <= 6`，没有下界校验；负数会绕过这一层并进入底层，正式 API 必须只接受整数 `0..6`；
- 原厂使用改变状态的 GET 路由，且监听所有网卡，没有认证、CSRF 和操作审计；本地配置服务只能通过 loopback 调用，不能把它原样暴露给浏览器；
- HTTP 200 早于异步处理结果；匹配的 `0x0a0a/commonStatus.eqType` 只能验收选中态，不能验收硬件应用。正式实现还需结合 `set_now` 路径、硬件文件或播放器状态判断；
- 路由会向厂商服务器报告状态，且重启后云端值会覆盖本地快照；离线报告失败、控制中心异常和超时回滚仍未验证；
- 已确认网易本地播放器播放时返回 `set_now=true` 并立即加载硬件效果；空闲或 AirPlay 时则为 `false`，API 必须分别展示 `selectedMode` 与 `appliedMode`，并明确标记等待本地播放后应用的状态；
- 模式 3..6 的文件映射来自静态实现，未逐一动态切换；由于它们与已验证的模式 2 共用同一套文件选择、复制和硬件加载路径，本阶段接受代表性验证结果，不将逐项回放作为实现阻塞项。ioctl 失败、文件复制失败和播放切歌后的重选逻辑仍需验证；
- 设备端 BusyBox `wget` 对这个空响应路由出现过挂起，测试工具应使用 ADB 端口转发和带超时的主机 HTTP 客户端。

## 5. Wi-Fi 状态读取

独立回放确认以下只读调用：

| 字段 | 值 |
| --- | --- |
| D-Bus 目标 | `netease.ihw.wifi`、`/netease/ihw/wifi` |
| 来源/目标 | Controller `0` → Wi-Fi 掩码 `128` |
| 请求 | `CMD_WIFI_STATE_REQ=0x0b05` / `2821`，空负载 |
| 响应 | `CMD_WIFI_STATE_RESP=0x0b06` / `2822` |
| 响应字段 | `wifi_state`、`wifi_rssi`、`wifi_ssid`、`wifi_mac` |
| 状态码 | `4099` 为已连接，`4097` 为未连接 |

实机响应为已连接，采集时 RSSI 为 `-42 dBm`。底层 D-Bus 读取路径达到 **S3 独立回放**。正式服务改用本机 `wpa_cli` 读取当前 SSID 和 `COMPLETED` 状态，用 `iwconfig` 归一化信号等级；API 允许返回用户明确要求查看的当前 SSID，但必须删除密码、BSSID、MAC 和 IP。

`0x0b3b/0x0b3c` Wi-Fi 列表、`0x0b37..0x0b3a` 开关以及配网命令尚未回放。状态读取成功不代表切网可安全实现。

### 5.1 本机配置切换事务

网页切换没有复用未验证的厂商配网命令，而是通过 root-only 的音箱端事务脚本管理实际生效的 `/mnt/UDISK/wifi/wpa_supplicant.conf`：

1. Go 适配层严格限制 SSID 为 1..32 UTF-8 字节，开放网络密码为空，WPA/WPA2 密码为 8..63 字节或 64 位十六进制 PSK；
2. SSID 和密码写入权限 `0600` 的临时配置文件，不拼接进 shell 命令，不进入响应或日志；
3. 切换前备份当前配置并写入事务标记，再调用 `wpa_cli reconfigure`；
4. 45 秒内必须同时满足目标 SSID、`wpa_state=COMPLETED`、wlan0 IPv4、默认路由和默认网关可达；
5. 目标网络失败时恢复原配置，再等待原网络满足连接、IPv4 和默认路由；
6. 成功或回退完成后删除包含凭据的临时文件；若服务重启时仍发现事务标记，启动脚本会先恢复备份再启动网页服务。

实机已完成当前配置原地重载成功、不可达测试 SSID 超时后自动回退、原 SSID/IPv4/默认路由/网页端口恢复、临时凭据清理，以及模拟未完成事务后的服务启动恢复。因此该能力开放为 **experimental**；在更多路由器、安全类型和断电时机覆盖前暂不提升为 S4 `safe`。

## 6. KPlayer 局域网 URL 播放

实机确认原厂 `KPlayer` 不只接受私有 D-Bus 消息，它还完整发布了标准 UPnP/DLNA MediaRenderer：

- SSDP `M-SEARCH` 能发现 `urn:schemas-upnp-org:device:MediaRenderer:1`；
- 设备描述为 `三音云音箱-C931`、`KPlayer: audio render`、`DMR-1.50`；
- KPlayer 监听 UDP `1900` 和动态 HTTP 控制端口，设备描述中提供 `AVTransport`、`ConnectionManager` 与 `RenderingControl`；
- `AVTransport` 服务明确提供 `SetAVTransportURI`、`Play`、`Pause`、`Stop`、`Seek` 和状态查询。

2026-07-25 使用一段 13.844 秒、44.1 kHz MP3 公共领域测试音乐完成独立回放：

1. 在同一局域网临时提供 `http://<测试主机>:18080/test.mp3`，音箱主动下载完整文件并返回 HTTP 200；
2. 初始 `GetTransportInfo` 返回 `NO_MEDIA_PRESENT/OK`；
3. 依次调用 `SetAVTransportURI` 与 `Play`，两个 SOAP 请求均成功返回；
4. 两秒内 `GetTransportInfo` 返回 `PLAYING/OK`，`GetPositionInfo` 返回总时长 `00:00:13`；
5. KPlayer 日志记录 `OnSetAVTransportURI`、`OnPlay`，并连续报告 0、2、5、8..13 秒播放进度；
6. `ihwplayer` 收到原厂播放命令 `0x0601`，`playerId=2`，状态依次进入 Preparing、Prepared、Playing；底层 TinaPlayer 识别实际时长为 13844 ms；
7. 文件结束后出现 `TINA_NOTIFY_PLAYBACK_COMPLETE`，`ihwplayer` 进入 Completed，KPlayer 最终回到 `STOPPED/OK`。

这证明“由音箱主动拉取局域网或互联网 URL，并通过原厂 KPlayer → ihwplayer → TinaPlayer 音频链路播放”已经达到 **S3 独立回放**，可作为替代厂商云端播放的首选入口，无需自行占用 ALSA 设备或移植新的解码器。

`2026-07-25` 随后完成设备端 API 与后台队列的实机验收：URL 播放后进度可读为 `2/13`，`Pause`、`Play` 恢复、`Stop`、手动下一首、曲目自然结束后自动续播，以及网络电台的保存、播放、入队、删除均通过；电台列表在配置服务重启后仍能恢复。本地播放因此已作为 `experimental` 能力开放。

同日对中文直播 MP3 做扩展验证：原厂播放器通常需约 6–7 秒完成首次缓冲，超过普通文件的 4 秒窗口；进入 Playing 后又会把无限流时长报告为约 `2147483` 秒，并很快将 UPnP 传输态误报为 stopped，但底层 `ihwplayer` 仍持续播放。服务因此只对 `kind=radio` 延长首次验收至 12 秒，隐藏无意义的直播进度，并在主动停止前维护已确认的直播状态；普通 URL 文件仍完全依赖 KPlayer 实时回读。

同日完成设备端定时停止验收：播放江苏交通广播 FM101.1 后设置 1 分钟定时，API 倒计时依次回读为 48、32、17、2 秒，到时自动调用 KPlayer Stop，最终回读 `transport=stopped`、`stopTimer.active=false`。60 分钟上限可设置，61 分钟返回 HTTP 400；设置 60 分钟后重启配置服务，定时状态被清除且 15 个电台收藏保持不变。

升为 `safe` 前仍需补齐：

- `Seek` 的动态验收；
- URL 失效、HTTP 超时、断网、格式不支持、解码失败和 KPlayer 重启后的状态收敛；
- AirPlay、蓝牙、语音播报和 KPlayer 之间的抢占与恢复策略；
- 可信媒体源白名单、重定向/超时限制和 SSRF 防护；播放写接口不能直接暴露原厂无认证的 UPnP 控制端口；
- 播放接口在默认无登录密码产品策略下的局域网误操作风险，至少应支持可选认证或设备配对。

## 7. 闹钟和提醒的云端边界

闹钟同步的正确方向和负载已经动态确认：

1. Alarm 模块 `1` 向 Controller 掩码 `1` 发送 `CMD_ALARM_SYNC_REQ=0xf000`，负载为 `null`；
2. Controller 请求厂商云接口；
3. 云端返回后，Controller 向 Alarm 掩码 `2` 发送 `CMD_ALARM_SYNC_RSP=0xf001`；
4. 当前实机负载为 `{"data":[]}`，Alarm 服务完成同步后再回送确认消息。

因此该协议虽然已独立回放，但不是纯本地数据库查询。提醒使用对应的 `0xf005/0xf006`；新增、删除和全部删除分别位于 `0xf002..0xf004` 与 `0xf007..0xf009`。在证明离线持久化、云端冲突和重启恢复语义前，不应提供本地闹钟/提醒 CRUD，也不应通过伪造同步响应制造只存在于内存的条目。

## 8. 已提取但尚未动态确认的命令

| 配置 | 静态命令 | 当前等级 | 下一验证动作 |
| --- | --- | --- | --- |
| 业务层播放音量 | `CMD_PLAY_VOL_SET=0x060f` | S1；制造通道硬件层 S3 | 找到能同步 `commonStatus` 的调用参数 |
| 麦克风控制 | 实体 `KEY_HOME(102)` 双击已确认；静态请求 `0x0a0b`、响应 `0x0a0c` | 实体路径 S2；可调用协议 S1 | 从原厂 App/计划任务采集业务入口，禁止原始 input 注入 |
| Wi-Fi 状态 | `0x0b05` / `0x0b06` | 读取 S3 | 补断开态样本和超时行为 |
| Wi-Fi 开关 | `0x0b37` / `0x0b38`，响应 `0x0b39` / `0x0b3a` | S1 | 暂不回放，避免失联 |
| Wi-Fi 列表 | `0x0b3b` / `0x0b3c` | S1 | 确认扫描对当前连接的影响和完整脱敏 |
| Wi-Fi 配网 | `0x0b00` / `0x0b01` | S1 | 取得 USB 恢复和自动回滚方案后测试 |
| KPlayer URL 播放 | UPnP `AVTransport`：`SetAVTransportURI`、`Play`、`Pause`、`Stop`、状态与进度查询；自有后台队列与电台持久化 | S3，已开放实验 API | 补 Seek、异常媒体/断网与音源抢占测试 |
| KPlayer 音量/静音 | `0x1c08` / `0x1c09` | S1 | 播放状态下动态抓包 |
| AirPlay 启停 | `0x1d01` / `0x1d02` | 启动 S4，关闭 S1 | 补关闭后的回滚与守护协调测试 |
| AirPlay 音量事件 | `0x1e05`，已观察 `{"volume":27}` | S2 日志观察 | 补完整 D-Bus 抓包并验证 0%、100% 和播放结束恢复 |
| 音效/EQ | `GET /eq/:mode`，`mode=0..6`；状态广播 `0x0a0a` | 选中态/云端持久性 S3；本地播放中 Normal/Vocal 硬件切换 S3 | 补离线、服务异常、切歌和超时回滚；模式 3..6 不再逐项回放 |
| 闹钟/提醒增删 | `0xf002..0xf009` | S1，且云端耦合 | 先确定是否保留为只读诊断能力 |
| 灯控 | `MODULE_LIGHT_MCU=13`；测试命令 `0x2900..0x2903` | S1 | 分离状态灯、音量灯和音乐灯效抓包 |

通用配置枚举还包含 `CMD_CFG_ADD/CHANGE/GET=0x0900..0x0905`，目标模块为 Configure `5`。重启后再次枚举会话总线，仍没有 `netease.ihw.configure` owner；TCP 1705 的已注册路由也没有通用配置入口，不能仅凭枚举推断它是可调用的本地设置服务。

## 9. 可重复采集

使用只读采集工具：

```bash
./tools/sanyin_protocol_capture.sh --label bluetooth-open --duration 40
```

工具同时采集 D-Bus、相关服务的增量日志及操作前后状态。原始文件保存在 `.tools/protocol-captures/`，该目录不进入 Git；分析、引用或分享时只使用 `*.redacted.log`。

每次采集只允许执行一个动作，并记录操作前状态、触发方式、听到的反馈及操作后状态。原始日志可能包含 SSID、IP、MAC、设备标识、会话 ID 和云端 URL，不能复制到正式文档。

## 10. 下一步

1. KPlayer 本地播放 API、暂停、恢复、停止、连续队列和网络电台正常路径已完成；下一步补 Seek、异常 URL/断网和 AirPlay/蓝牙抢占测试。
2. 验证蓝牙已连接设备时关闭、服务异常和重启后的行为，并建立无副作用状态读取。
3. 找到业务层音量入口，验证写入后 `commonStatus`、硬件档位和重启状态一致。
4. 音效/EQ 已借助语音随机播放完成 `set_now=true` 和 Normal/Vocal 硬件切换；模式 3..6 接受共用代码路径和静态映射，不再逐项回放。后续只补切歌重选、离线报告失败、控制中心异常和超时回滚，完成前保持 S3。
5. 禁麦计划已确认走厂商 `cfglist/micCron` 链；如原厂 App 仍可操作，再采集一次真实下发用于 S2 对照，但在没有本地返回通道前不做伪造 App 消息或当前值回写。
6. Wi-Fi 当前连接读取、切网、45 秒失败自动回退与启动事务恢复已完成；后续扩大路由器类型和断电窗口测试。
7. 将闹钟和提醒标记为云端耦合；除非能证明离线持久语义，否则不实现本地 CRUD。
8. 通过原厂 App 或设备动作分别采集灯光设置，避免调用制造测试 LED 命令代替业务配置。
