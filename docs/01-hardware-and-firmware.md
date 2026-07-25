# 设备与固件分析

## 1. 已验证硬件

实机拆解和系统信息表明，该音箱不是常见的 Android 音箱，而是一台基于 OpenWrt 的 ARM Linux 设备。

| 项目 | 信息 |
| --- | --- |
| 产品 | 网易三音云音箱，设备名后缀 `C931` |
| 主控 | Allwinner R16/A33，ARMv7 |
| 内存 | 约 512 MiB DDR3L |
| NAND | 约 256 MiB |
| Wi-Fi/蓝牙 | AP6236 |
| 音频 Codec | ADAU1761 |
| MCU | STM32F105 |
| 系统 | OpenWrt，Linux 3.4.39 |
| 固件版本 | `1.1.17.4`，2019-04-25 构建 |

底部 Mini-USB 口会枚举为标准 ADB 接口：

- USB VID/PID：`18d1:d002`
- ADB 序列号示例：`netease_XXXXXXXX`（请以 `adb devices` 输出为准）
- ADB shell 默认用户：`root`

## 2. 存储结构

根文件系统由只读 SquashFS 与可写 ext4 overlay 组成：

```text
/rom                       只读 SquashFS
/dev/by-name/rootfs_data   ext4，挂载为 /overlay
rootfs                     overlay 合并后的 /
/dev/nandk                 ext4，挂载为 /mnt/UDISK
```

已验证分区如下：

| 设备 | 大小 | 用途/格式 |
| --- | ---: | --- |
| `/dev/nanda` | 1 MiB | boot-res |
| `/dev/nandb` | 1 MiB | env |
| `/dev/nandc` | 4 MiB | FAT，引导分区 |
| `/dev/nandd` | 70 MiB | SquashFS，当前 rootfs |
| `/dev/nande` | 4 MiB | FAT，备用引导分区 |
| `/dev/nandf` | 70 MiB | SquashFS，备用 rootfs |
| `/dev/nandg` | 1 MiB | IPL3 |
| `/dev/nandh` | 10 MiB | ext4，rootfs_data/overlay |
| `/dev/nandi` | 1 MiB | private |
| `/dev/nandj` | 1 MiB | misc |
| `/dev/nandk` | 60 MiB | ext4，UDISK |

当前完整备份位于 `backups/20260721-2145/`，共有 11 个镜像和 `SHA256SUMS`。该目录已加入 `.gitignore`。

## 3. 与播放有关的主要进程

| 进程 | 作用 |
| --- | --- |
| `SPlayer` | 原生 AirPlay/RAOP 服务 |
| `KPlayer` | 厂商播放器，监听 TCP 5005 |
| `ihwplayer` | 播放硬件抽象和调度 |
| `netease_control_center` | 云端配置、功能开关和服务协调 |
| `avahi-daemon` | Bonjour/mDNS 服务发布 |
| `mdnsd` / `mDNSResponder` | 固件内其他 mDNS 组件 |

固件包元数据表明 `SPlayer` 依赖：

- `libsraop.so`
- `libavahi-compat-libdnssd`
- `mdnsresponder`

设备中还保留了 `/usr/include/raop.h` 和 `/usr/include/dnssd.h`。因此 AirPlay 实现一直存在于固件中，并非硬件或协议栈缺失。

## 4. 关键结论

云端运行时配置返回 `airPlayPrivilege=0`。控制中心收到该配置后会向 `SPlayer` 发送关闭命令，并记录：

```text
cmd to off airplay
airplay is disabled!!!
```

所以故障根因是厂商功能开关，而不是：

- Wi-Fi 不支持组播；
- Avahi 缺失；
- AirPlay 库被删除；
- 音频硬件损坏。

修复策略是复用固件内原生 `SPlayer`，在它被关闭后重新发送原生启动命令。这样对音频链路的改动最小，也无需制作或刷写整套固件。
