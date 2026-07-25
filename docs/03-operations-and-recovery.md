# AirPlay 操作与恢复手册

> 本文只说明 AirPlay 恢复服务、NAND 备份和回滚。完整网页服务、Wi-Fi、蓝牙、EQ、本地播放、电台和定时停止请参阅 [三音 Plug 使用手册](09-user-guide.md)。

## 1. 准备环境

需要：

- macOS；
- 一根可传输数据的 Mini-USB 线；
- 音箱与 Mac/iPhone 连接到同一 Wi-Fi；
- Google Android Platform Tools 中的 `adb`。

下载 ADB：

```bash
./tools/get_adb_macos.sh
```

工具会优先依次查找：

1. 环境变量 `$ADB`；
2. 项目内 `.tools/platform-tools/adb`；
3. `PATH` 中的 `adb`；
4. 本次分析曾使用的 `/tmp/sanyin-platform-tools/platform-tools/adb`。

## 2. 检查设备

```bash
./tools/sanyinctl doctor
```

正常结果应包含：

- ADB 状态为 `device`；
- 当前用户为 `uid=0(root)`；
- `SPlayer`、`dbus-send` 和 `/tmp/dbus_env.sh` 存在；
- 系统为 ARMv7/OpenWrt；
- `/overlay` 有足够可用空间。

## 3. 安装持久化修复

```bash
./tools/sanyinctl install
```

安装过程只会：

1. 将脚本上传到音箱 `/tmp`；
2. 复制到 `/usr/bin/airplay_restore.sh`；
3. 复制启动脚本到 `/etc/init.d/airplay_restore`；
4. 执行 `enable` 和 `start`。

如果工具发现目标位置已有不同内容，会拒绝覆盖。确认目标文件可以替换时：

```bash
./tools/sanyinctl --force install
```

## 4. 查看状态与验证

```bash
./tools/sanyinctl status
./tools/sanyinctl verify
./tools/sanyinctl logs
```

验证标准：

- `procd` 显示 `airplay_restore` 正在运行；
- `SPlayer` 监听 `0.0.0.0:5002`；
- Mac 能连接音箱的 TCP 5002；
- `dns-sd` 能发现 `_raop._tcp.local` 中的音箱；
- iPhone/iPad 可以选择音箱并播放。

## 5. 重启验证

```bash
./tools/sanyinctl reboot
```

该命令会要求确认，然后：

1. 重启音箱；
2. 等待 ADB 重新出现；
3. 等待 Wi-Fi 和 TCP 5002，最长约 90 秒；
4. 输出恢复日志。

## 6. 备份 NAND

使用默认时间戳目录：

```bash
./tools/sanyinctl backup
```

或指定目录：

```bash
./tools/sanyinctl backup backups/我的音箱-刷机前
```

工具逐个读取 11 个分区。每次只在音箱 `/tmp/sanyin-nand-backup.img` 保留一个临时镜像，拉取完成后立即删除，不会把 223 MiB 镜像同时堆在 250 MiB 的 tmpfs 中。

备份完成后会：

- 检查每个文件的预期字节数；
- 生成 `SHA256SUMS`；
- 保存 `device-info.txt`；
- 在本机重新校验所有镜像。

## 7. 卸载与回滚

```bash
./tools/sanyinctl uninstall
```

它会停止并禁用服务，然后删除：

```text
/usr/bin/airplay_restore.sh
/etc/init.d/airplay_restore
/etc/rc.d/S122airplay_restore
/etc/rc.d/K122airplay_restore
```

卸载后，当前开机中的 AirPlay 监听可能继续存在，直到厂商控制中心再次关闭或设备重启。这不代表卸载失败。

完整 NAND 写回不是本修复的常规回滚方法。只有在 overlay 或系统分区严重损坏时才考虑写镜像，并应先通过串口或 FEL 工具确认救砖路径。

## 8. ARM 二进制分析工具

创建临时 Python 环境：

```bash
python3 -m venv .venv
. .venv/bin/activate
pip install -r tools/requirements-analysis.txt
```

按符号反汇编 ARM ELF：

```bash
python tools/disasm_elf.py /路径/SPlayer CtrlAirplayOnOff
python tools/disasm_elf.py /路径/SPlayer 某符号 --size 0x200
```

该工具依赖 ELF 符号表；被完全 strip 的二进制需要先通过 Ghidra、IDA 或 radare2 定位地址。
