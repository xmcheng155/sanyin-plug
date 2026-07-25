# AirPlay 修复原理

## 1. 原生命令协议

音箱内部组件通过 D-Bus 会话总线通信。`SPlayer` 的接口为：

| 字段 | 值 |
| --- | --- |
| Service | `netease.ihw.splayer` |
| Object path | `/netease/ihw/splayer` |
| Interface/member | `netease.ihw.SmartAudio.API` |
| 启动命令 | `0x1d01`，十进制 `7425` |
| 关闭命令 | `0x1d02`，十进制 `7426` |
| SPlayer 目标掩码 | `1 << 11`，十进制 `2048` |

D-Bus 方法参数依次为：

1. 来源模块，`uint32:0`；
2. 目标模块掩码，`uint32:2048`；
3. 命令，`uint32:7425`；
4. UTF-8 负载字节数，`uint32:66`；
5. JSON 字符串负载。

已验证的负载：

```json
{"port":5002,"mac":"112233445577","device":"三音云音箱-C931"}
```

该字符串是 66 个 UTF-8 字节。原控制中心使用端口 `5002` 和固定种子 `112233445577`。启动后 `SPlayer` 初始化 RAOP、监听 `0.0.0.0:5002`，再通过 Avahi 发布 `_raop._tcp.local` 服务。

手工命令示例：

```sh
. /tmp/dbus_env.sh
/usr/bin/dbus-send \
  --session \
  --type=method_call \
  --dest=netease.ihw.splayer \
  /netease/ihw/splayer \
  netease.ihw.SmartAudio.API \
  uint32:0 \
  uint32:2048 \
  uint32:7425 \
  uint32:66 \
  string:'{"port":5002,"mac":"112233445577","device":"三音云音箱-C931"}'
```

## 2. 持久化方式

仅手工发送命令只对当前开机有效。重启或云端配置刷新后，控制中心仍可能重新关闭 AirPlay。

持久化方案包含：

- `/usr/bin/airplay_restore.sh`：每 20 秒检查一次；
- `/etc/init.d/airplay_restore`：由 OpenWrt `procd` 托管并自动重启；
- `/etc/rc.d/S122airplay_restore`：开机启动链接。

守护逻辑只在以下条件同时成立时发送命令：

1. `/tmp/dbus_env.sh` 已生成；
2. `SPlayer` 进程存在；
3. `wlan0` 已获得 IPv4 地址；
4. TCP 5002 当前没有监听。

这避免了频繁重复发送启动命令。实机重启验证记录：

```text
21:53:50 AirPlay restore watchdog started
21:54:11 AirPlay listener missing; sending native SPlayer start command
21:54:14 AirPlay enabled on TCP port 5002
```

## 3. 为什么不直接修改云端配置或二进制

可选方案包括修改数据库、屏蔽云端配置、补丁控制中心或重做 rootfs，但风险更高：

- 云端配置可能覆盖本地数据库；
- 修改控制中心会影响其他智能功能；
- 二进制补丁难以跨固件版本复用；
- 重做 SquashFS 或写裸 NAND 存在变砖风险。

当前方案只增加 overlay 文件，删除文件和启动链接即可回滚，同时继续使用厂商原生音频驱动、RAOP 库与播放调度。

## 4. 可预期行为

- 开机或 Wi-Fi 重连后，AirPlay 可能延迟约 20 秒出现；
- AirPlay 名称为 `三音云音箱-C931`；
- Bonjour 实例在当前实机上显示为 `11226844a8c0@三音云音箱-C931`；
- IPv6 RAOP socket 初始化失败不影响 IPv4 局域网播放；
- 厂商云服务是否可用，不影响已经恢复的局域网 AirPlay。
