# 本地配置服务 API 契约（v1 / Mock 与真实设备）

> 状态：Mock、ADB 开发模式和音箱本机 ARMv7 模式均已实现并由自动化测试约束。机器可读契约见 [`service/openapi.json`](../service/openapi.json)。

## 1. 边界

- 基础路径为 `/api/v1`，响应使用 UTF-8 JSON；事件流使用 SSE。
- `environment=mock` 表示模拟场景；`environment=device` 表示经 `RealAdapter` 读取真实音箱。服务不修改厂商 SQLite；正式音箱端服务只维护 `/mnt/UDISK/sanyin-config` 下的自有状态文件。
- 所有状态读取均经过 `DeviceAdapter`；未来 `RealAdapter` 只能替换适配层，不能改变 v1 HTTP 字段语义。
- 当前 SSID 可以按用户配置需求进入 `GET /network`；BSSID、MAC、IP、Wi-Fi 密码、主机名、序列号、设备标识、令牌与原始云端日志不得进入响应。
- 本地播放内部需要保留完整 URL 才能交给 KPlayer；对外响应的 `source` 必须去除用户名、密码、查询参数和片段，避免电台令牌进入 API 或日志。
- 音箱端局域网服务默认不设置登录密码；同源 Origin 和 `X-Sanyin-CSRF: 1` 写入检查继续强制执行。存在 `/mnt/UDISK/sanyin-config/web-password` 时才启用可选的 HTTP Basic Auth。
- 网页更新不接受裸二进制，只接受由设备已配 Ed25519 公钥验证的签名容器；签名私钥不得出现在音箱、请求、响应或仓库中。

## 2. 通用响应

读取响应统一使用环境信封：

```json
{
  "environment": "mock",
  "scenario": "healthy",
  "data": {}
}
```

每个被动观测值使用统一状态信封：

```json
{
  "value": "running",
  "observedAt": "2026-07-23T09:00:00+08:00",
  "source": "mock",
  "freshness": "fresh",
  "revision": 1
}
```

`unknown` 是一等状态：值为 `unknown` 时，`freshness` 同时为 `unknown`，前端不得用最近一次操作结果补齐。`stale` 表示观测存在但已过期，也不得展示为实时正常。

## 3. 能力枚举

| 字段 | 枚举 |
| --- | --- |
| `readability` | `full`、`partial`、`none` |
| `writability` | `safe`、`experimental`、`not_verified`、`unsupported` |
| `availability` | `available`、`degraded`、`offline`、`unknown` |
| `source` | `system`、`dbus_event`、`mock`、`vendor_snapshot`、`derived` |
| `freshness` | `fresh`、`stale`、`unknown` |

能力不足或不支持时必须提供 `reason`。云端依赖由独立的 `cloudDependency` 表示，不能和本地协议未验证混为一类。

## 4. 读取接口

| 方法与路径 | `data` 语义 |
| --- | --- |
| `GET /capabilities` | 完整能力矩阵 |
| `GET /device` | 产品系列、固件、平台和脱敏存储余量 |
| `GET /status` | 总体、核心服务、播放器、网络和音频摘要 |
| `GET /airplay` | 运行态、5002 端口抽象状态、恢复服务观测态 |
| `GET /network` | 当前 SSID、连接状态、信号等级和最近切换结果，不含密码、BSSID、MAC 或 IP |
| `GET /audio` | 系统音量、输出静音、麦克风和 EQ 分层状态 |
| `GET /bluetooth` | 服务状态、可靠性不足时保持 `unknown` 的当前开关，以及最近一次设备事件确认结果 |
| `GET /player` | KPlayer 传输状态、播放进度、当前媒体、内存队列和自有网络电台列表 |
| `GET /media-library` | 最多 100 个 URL/网络电台收藏和 100 条播放历史；只返回脱敏公开地址 |
| `GET /lighting` | 灯光诊断快照 |
| `GET /schedules` | 麦克风计划、闹钟和提醒的诊断快照 |
| `GET /system` | 应用版本、提交、构建时间、网页更新是否启用及最近更新/回滚状态 |
| `GET /events` | `snapshot` 类型的归一化 SSE 事件 |

EQ 的 `selectedMode`、`appliedMode`、`applyState` 必须分别保留。`pending_local_playback` 表示业务模式已选中，但硬件等待网易本地播放后应用。

## 5. Mock 场景

所有读取接口接受 `?scenario=<id>`；省略时使用 `healthy`。场景清单由 `GET /mock/scenarios` 返回：

`healthy`、`airplay_down`、`wifi_offline`、`controller_down`、`bluetooth_unknown`、`eq_pending`、`stale_state`、`operation_failed`。

未知场景返回 HTTP 400 和 `unknown_mock_scenario`，不会静默回退为健康状态。

## 6. 写接口与模拟操作

以下预留路由默认统一返回 HTTP 409：

```json
{
  "error": {
    "code": "capability_not_ready",
    "message": "该操作尚未完成安全写入验证",
    "operationId": null
  }
}
```

- `POST /airplay/recover`
- `PATCH /audio`
- `PATCH /lighting`
- `PUT /microphone/schedule`

网页可用 `POST /airplay/recover?simulate=true` 演示交互状态机。响应必须包含 `simulation: true`；`operation_failed` 场景会展示失败、回滚中和已恢复，整个流程不调用设备或现有恢复服务。

真实设备模式当前开放七类写能力：

- `POST /airplay/recover`：发送已验证的原生启动命令，或在端口已监听时幂等返回；成功必须以 TCP 5002 回读为准；
- `PUT /airplay/auto-recover`：请求体为 `{"enabled": true|false}`，只原子写入 `/mnt/UDISK/sanyin-config/airplay-auto-recover`，回读一致后才返回 `verified=true`；
- `PATCH /bluetooth`：请求体为 `{"enabled": true|false}`，等待 `0x0b2f`（开启）或 `0x0b30`（关闭）成功事件；当前开关仍缺无副作用查询，因此只把最近验收结果作为 `derived/stale` 单独返回；
- `PATCH /audio/effect`：请求体为 `{"mode": 0..6}`，只调用音箱 loopback 的固定 EQ 路由，等待 `commonStatus.eqType` 对应事件；`selectedMode` 和基于固件文件回读的 `appliedMode` 必须分开返回；
- `POST /network/switch`：请求体为 `{"ssid":"...","password":"..."}`；开放网络使用空密码。事务先备份当前配置，45 秒内未同时验收目标 SSID、`COMPLETED`、IPv4、默认路由和网关可达时自动恢复原配置，并再次等待原网络联网。服务启动时也会恢复带有未完成标记的事务。
- `POST /player/control`：统一接收 `action`。`play_url` 直接播放 URL；`pause`、`resume`、`stop`、`next` 控制传输；`volume_set` 携带整数 `volume=0..100`，只在 KPlayer 播放中通过 RenderingControl 设置并以 `GetVolume` 回读验收，暂停或停止时拒绝；`queue_add`、`queue_play`、`queue_remove`、`queue_clear` 管理队列；`radio_add`、`radio_remove`、`radio_play`、`radio_queue` 管理网络电台，`radio_move_up`、`radio_move_down` 携带 `itemId` 调整收藏顺序；`timer_set` 携带整数 `durationMinutes=1..60`，`timer_cancel` 取消。媒体只接受 HTTP/HTTPS，播放状态、当前 URI 和进度必须由原厂 KPlayer UPnP AVTransport 回读；普通曲目自然结束后由设备端后台监视器自动续播下一项。
- 媒体库接口：`POST /media-library/favorites` 使用完整 `url`、已有 `historyId` 或已有网络电台 `radioStationId` 三者之一新增收藏；电台完整流地址由设备端关联读取，重复完整 URL 更新原条目。`DELETE /media-library/{favorites|history}/{itemId}` 删除单条，`DELETE /media-library/history` 清空历史；`POST /media-library/{favorites|history}/{itemId}/{play|queue}` 通过设备端条目 ID 播放或入队。媒体确认进入 `playing` 后才记录历史，同一完整 URL 会前移、更新标题与类型并累计 `playCount`。
- `POST /system/update`：请求体媒体类型固定为 `application/vnd.sanyin.update+zip`，最大 32 MiB。更新包必须只包含 `manifest.json`、`sanyin-config` 和 `signature.ed25519`。服务验证 Ed25519 签名、二进制 SHA-256、严格递增的 `X.Y.Z` 版本和 Linux/ARMv7 小端 ELF；设备侧再验证程序内置版本，原子替换后检查进程及 TCP 8787，20 秒内失败自动恢复上一版。Mock 模式、无公钥模式、同版本和降级包均拒绝。

网页更新状态使用 `idle`、`staged`、`applying`、`succeeded`、`failed`、`rolled_back`、`rollback_failed`。状态文件位于 `/mnt/UDISK/sanyin-config/update/update-status`。该接口只更新 `sanyin-config` 单文件及其内嵌网页，不能更新 init 或其他辅助脚本；跨组件升级必须通过 SSH/ADB 完整安装。

蓝牙、EQ、Wi-Fi 和本地播放标记为 `experimental`：表示正常路径已有设备验收，Wi-Fi 也完成了超时回退和启动恢复验收；本地播放已验证 URL 播放、暂停、恢复、停止、0..100 音量写入回读、进度、手动/自动切歌、队列及电台的正常路径。新网络覆盖面、断电窗口、异常媒体格式、播放网络中断，以及本地播放与 AirPlay/蓝牙抢占等场景仍未全部达到 S4，不得显示为 `safe`。仅应添加可信 URL；服务不会替用户代理、缓存或扫描远端内容。

网络电台及排列顺序保存到 `/mnt/UDISK/sanyin-config/radio-stations.json`，以 `0600` 权限原子写入并在服务重启后恢复；播放队列仅保存在内存中，服务重启后清空，但正在播放的媒体会通过 KPlayer `GetMediaInfo.CurrentURI` 回读，并优先匹配收藏电台名称。直播 MP3 首次缓冲最多等待 12 秒；原厂 KPlayer 对无限流会回报伪时长和伪停止状态，服务在已确认进入播放后将电台进度归一化为 `0/0`，并在用户主动暂停或停止前维持相应的直播传输状态。

播放历史和 URL/网络电台收藏共同保存到 `/mnt/UDISK/sanyin-config/media-library.json`，以 `0600` 权限原子写入，每类最多 100 条。文件中保留完整 URL 供设备重新播放；所有 API 响应只返回删除查询参数、用户信息和片段后的 `source`。清空历史不影响收藏、播放队列或当前播放。

定时停止由音箱端服务执行，不依赖浏览器保持打开。`GET /player` 的 `stopTimer` 返回 `active`、`stopAt` 和 `remainingSeconds`；手动 `stop` 或 `queue_clear` 会取消定时，换歌和切换电台不会取消。定时器只保存在内存中，服务重启后清除，避免过期任务在重启后误停。

自动恢复配置缺失时按“开启”处理，以兼容既有守护服务。关闭该配置不会主动停止当前 AirPlay，只阻止守护程序后续自动发送启动命令。

## 7. SSE 事件

事件使用 `event: snapshot`，`data` 为 JSON：

```json
{
  "type": "snapshot",
  "scenario": "healthy",
  "observedAt": "2026-07-23T09:00:00+08:00",
  "source": "mock",
  "revision": 1,
  "changes": ["status", "airplay", "network", "audio", "bluetooth", "player"]
}
```

事件仅包含归一化变化类别，不透传 D-Bus 命令码、原始载荷或厂商日志。
