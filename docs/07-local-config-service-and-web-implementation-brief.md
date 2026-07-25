# 本地配置服务与交互网页实施说明

> 用途：作为新开发 session 的任务入口。
> 前提：当前无法连接实机，本阶段只实现可在电脑上运行和测试的 API、MockAdapter 与交互网页，不进行设备部署或动态协议验证。

> 进度更新（2026-07-25）：本文定义的 Mock 阶段已完成；已新增 ADB `RealAdapter`、真实状态网页、AirPlay 恢复和自动恢复持久配置。原“无法连接实机”的前提已经失效，后续工作转入设备本机部署与其他配置能力的 S4 验证。

## 1. 实施目标

先稳定本地配置 API 契约和模拟数据，再基于同一套 API 开发交互网页。设备重新可用后，只新增 `RealAdapter` 并进行实机验收，不重写前端。

实施顺序：

1. 定义领域模型和 HTTP API；
2. 实现 `MockAdapter` 与自动化测试；
3. 基于 Mock API 实现交互网页；
4. 预留 `RealAdapter`，本阶段不连接或修改设备。

## 2. 当前事实与边界

协议和配置依据见：

- [本地配置 API 与参数清单](05-local-config-api-and-parameter-inventory.md)
- [本地配置协议确认记录](06-local-config-protocol-verification.md)

当前可直接作为只读数据展示的能力：

- 设备基本信息与固件；
- 服务健康状态；
- AirPlay 运行状态；
- Wi-Fi 连接状态和脱敏后的信号强度；
- 音量、静音、麦克风和 EQ 的被动观测状态；
- 已确认服务的在线状态。

本阶段禁止：

- 直接修改厂商 SQLite；
- 伪造尚未验证的 D-Bus 写操作；
- 将 Mock 操作结果描述成真实设备成功；
- 暴露 BSSID、MAC、IP、Wi-Fi 密码、设备标识、令牌或原始云端日志；当前 SSID 可按用户明确需求返回；
- 实现 Wi-Fi 切换、解绑、恢复出厂等高风险操作。

## 3. 建议项目结构

可根据实际技术栈调整，但需保持领域模型与设备访问解耦。

```text
service/
  cmd/                 # 服务入口
  internal/api/        # HTTP 路由与响应模型
  internal/domain/     # 能力、状态、事件和操作模型
  internal/adapter/    # DeviceAdapter、MockAdapter、未来的 RealAdapter
  internal/store/      # 后续自有配置库接口
  internal/testdata/   # 脱敏测试样本
web/
  src/api/             # 唯一的 API 访问层
  src/pages/           # 页面
  src/components/      # 状态卡片、能力提示、操作流程等
  src/mocks/           # 仅在需要前端独立运行时使用
```

后端建议使用 Go，优先标准库或轻量依赖，便于后续生成 ARMv7/OpenWrt 单文件程序。网页构建结果必须是本地静态资源，不依赖 CDN。

## 4. 核心模型

所有配置项必须通过能力模型驱动网页，不能仅用一个布尔值表示是否可用。

```json
{
  "id": "bluetooth.enabled",
  "readability": "partial",
  "writability": "not_verified",
  "availability": "available",
  "cloudDependency": false,
  "reason": "缺少无副作用状态查询、异常回滚和重启验证"
}
```

建议枚举：

- `readability`：`full`、`partial`、`none`；
- `writability`：`safe`、`not_verified`、`unsupported`；
- `availability`：`available`、`degraded`、`offline`、`unknown`；
- 状态来源：`system`、`dbus_event`、`mock`、`vendor_snapshot`、`derived`。

状态数据至少包含：

```json
{
  "value": "示例值",
  "observedAt": "2026-07-23T00:00:00+08:00",
  "source": "mock",
  "freshness": "fresh",
  "revision": 1
}
```

EQ 必须分别建模：

- `selectedMode`：业务选中模式；
- `appliedMode`：硬件已应用模式；
- `applyState`：`applied`、`pending_local_playback`、`unknown`。

蓝牙在无法确认当前状态时必须返回 `unknown`，不得用最近一次操作结果伪装成实时状态。

## 5. 第一批 API

基础路径建议为 `/api/v1`。

| 方法与路径 | 用途 |
| --- | --- |
| `GET /capabilities` | 返回全部配置项的能力等级与不可用原因 |
| `GET /device` | 返回脱敏设备信息和固件信息 |
| `GET /status` | 返回服务、播放器、音频和网络聚合状态 |
| `GET /airplay` | 返回 AirPlay 运行态和端口状态 |
| `GET /network` | 返回连接状态和信号等级，不返回网络标识 |
| `GET /audio` | 返回音量、静音、麦克风及 EQ 的分层状态 |
| `GET /events` | 通过 SSE 推送归一化状态变化 |

本阶段写接口可以保留路由模型或交互原型，但默认返回 `capability_not_ready`。即使 AirPlay 恢复已经过实机验证，本阶段也不在无设备环境中声称完成真实调用。

统一错误响应至少包含：

```json
{
  "error": {
    "code": "capability_not_ready",
    "message": "该操作尚未完成安全写入验证",
    "operationId": null
  }
}
```

## 6. MockAdapter 场景

MockAdapter 不能只有一份正常数据，至少提供以下可切换场景：

1. `healthy`：核心服务全部在线；
2. `airplay_down`：SPlayer 或 5002 不可用；
3. `wifi_offline`：Wi-Fi 断开；
4. `controller_down`：控制中心不可用，部分状态降级为未知；
5. `bluetooth_unknown`：服务在线但开关状态未知；
6. `eq_pending`：选中模式与硬件应用模式不同；
7. `stale_state`：状态超过有效期；
8. `operation_failed`：模拟写操作超时和回滚提示。

Mock 响应必须明确标记 `source=mock`。网页顶部应显示“模拟数据”环境标识。

## 7. 交互网页范围

网页采用移动端优先布局，同时支持桌面浏览器。

第一版页面：

- **总览**：设备、网络、AirPlay、播放器及核心服务健康卡片；
- **AirPlay**：运行状态、端口和恢复操作原型；
- **网络**：连接状态、信号等级，切网入口禁用并显示原因；
- **音频**：音量、输出静音、麦克风、EQ 选中态和实际应用态；
- **蓝牙**：服务状态、开关状态；未知状态必须有明确视觉提示；
- **灯光与计划任务**：展示当前快照，写操作保持禁用；
- **事件与诊断**：展示归一化事件、状态来源、更新时间和错误，不展示原始敏感日志。

写操作交互统一设计为：

```text
确认 → 执行中 → 等待状态验收 → 成功
                         └→ 失败 → 回滚中 → 已恢复/需人工处理
```

Mock 阶段允许演示完整流程，但必须显示“模拟操作”，不能与真实设备操作混淆。

## 8. 自动化测试

后端至少覆盖：

- 能力枚举和 JSON 契约；
- 不同 Mock 场景的聚合状态；
- 敏感字段不进入响应；
- 状态新鲜度和 `unknown` 语义；
- SSE 事件格式；
- 未验证写接口返回 `capability_not_ready`；
- EQ 选中态与硬件应用态不被错误合并。

前端至少覆盖：

- 正常、离线、降级、未知和陈旧状态展示；
- 能力不足时按钮禁用并展示原因；
- Mock 环境标识；
- EQ pending 状态；
- 操作失败及回滚流程；
- 手机和桌面关键布局。

## 9. 本阶段交付标准

完成时应满足：

1. 服务和网页均可在电脑上通过一条开发命令启动；
2. 网页只通过 HTTP API 获取状态，不直接引用 Mock 常量；
3. 所有 Mock 场景可切换并有自动化测试；
4. 页面能明确区分可写、只读、待验证、云端专属和状态未知；
5. 无敏感设备数据进入代码、测试快照或浏览器响应；
6. 后端接口为未来 `RealAdapter` 留出稳定边界；
7. README 增加本地开发、测试和构建说明；
8. 不部署到音箱，不修改现有 AirPlay 恢复服务。

## 10. 后续实机阶段

设备重新可用后再执行：

1. `RealAdapter` 与 ADB runner 已实现；下一步在音箱上验证 device runner；
2. Mock 与真实响应已复用同一 v1 契约；继续补足真实音频状态源；
3. ARMv7 静态构建已通过，仍需验证 OpenWrt 运行、资源占用和启动服务；
4. AirPlay 恢复操作和自动恢复持久配置已接入并完成网页实机往返；
5. 蓝牙、EQ 等写能力达到安全标准后，逐项解除能力开关；
6. 局域网服务按当前产品约定默认无登录密码，保留可选 Basic Auth；CSRF、同源校验、参数校验和操作结果验收必须始终启用。
