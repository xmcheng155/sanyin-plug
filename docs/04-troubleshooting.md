# 故障排查

## 1. ADB 看不到设备

检查：

```bash
./tools/sanyinctl doctor
```

常见原因：

- Mini-USB 线只能充电，不能传输数据；
- USB Hub 或转接器兼容性问题；
- ADB server 状态异常；
- macOS 尚未识别 `18d1:d002` 接口。

可尝试：

```bash
.tools/platform-tools/adb kill-server
.tools/platform-tools/adb start-server
.tools/platform-tools/adb devices -l
```

若 Codex/沙箱环境中出现 macOS USB 错误 `e00002be`，应在具有原生 USB 权限的终端运行 ADB。它不是音箱拒绝连接。

## 2. 服务运行，但没有 5002 端口

```bash
./tools/sanyinctl status
./tools/sanyinctl logs
```

依次确认：

```sh
test -s /tmp/dbus_env.sh
pidof SPlayer
ifconfig wlan0
netstat -lntp | grep ':5002 '
```

如果 Wi-Fi 尚未取得 IPv4 地址，守护脚本会等待。正常开机后最多约 20 秒再次尝试。

## 3. 端口存在，但手机看不到音箱

在 Mac 上检查 Bonjour：

```bash
dns-sd -B _raop._tcp local.
```

预期包含：

```text
11226844a8c0@三音云音箱-C931
```

若端口存在但无广播：

```sh
pidof avahi-daemon
/etc/init.d/avahi-daemon restart
```

同时检查路由器是否启用了客户端隔离、访客网络隔离或阻止 mDNS/组播。手机、Mac 和音箱应处于同一局域网。

## 4. 手机能看到，但播放失败

检查：

- 音箱是否被其他 AirPlay 客户端占用；
- `SPlayer` 是否仍在监听；
- `ihwplayer` 和音频相关进程是否存活；
- 是否只是音量过低或静音；
- 路由器是否对客户端间 TCP 流量做了隔离。

先重启修复服务：

```bash
./tools/sanyinctl install
```

仍失败时保存以下输出：

```bash
./tools/sanyinctl status > sanyin-status.txt
./tools/sanyinctl logs > sanyin-logs.txt
```

## 5. AirPlay 重启后短暂消失

这是预期行为。开机顺序为 D-Bus、播放器、控制中心、Wi-Fi，然后守护脚本检测端口。实机验证中，守护脚本启动后约 21 秒发送恢复命令，3 秒后端口建立。

## 6. 安装提示目标脚本不同

说明音箱已有同名脚本，但内容与本地版本不一致。先查看差异或把远端文件拉回：

```bash
.tools/platform-tools/adb pull /usr/bin/airplay_restore.sh /tmp/remote-airplay_restore.sh
.tools/platform-tools/adb pull /etc/init.d/airplay_restore /tmp/remote-airplay_restore.init
```

确认可以覆盖后执行：

```bash
./tools/sanyinctl --force install
```

## 7. 备份中断

重新运行 `backup` 到一个新目录。工具每次只读取 NAND，不会写入分区；中断后可能在设备 `/tmp` 留下一个临时文件，可删除：

```bash
.tools/platform-tools/adb shell rm -f /tmp/sanyin-nand-backup.img
```

不要把尺寸不完整、校验失败的镜像用于恢复。
