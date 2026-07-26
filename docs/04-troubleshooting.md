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

## 8. SSH 无法连接

先保留 USB ADB，并确认端口、公钥以及实际使用的 SSH 服务：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX ssh-enable .tools/sanyin-ssh.pub
.tools/platform-tools/adb -s netease_XXXXXXXX shell 'pidof dropbear; netstat -lntp | grep ":22 "; ls -l /etc/dropbear/authorized_keys'
```

若固件没有 Dropbear，检查项目自带服务：

```bash
.tools/platform-tools/adb -s netease_XXXXXXXX shell 'ubus call service list "{\"name\":\"sanyin_sshd\"}"; ls -l /mnt/UDISK/sanyin-ssh'
```

再使用详细日志检查主机指纹、密钥算法和公钥选择：

```bash
ssh -vv -i .tools/sanyin-ssh root@音箱IP
```

常见原因：

- 音箱 DHCP 地址已变化；
- 首次主机指纹尚未确认或 `known_hosts` 中留有旧记录；
- 私钥路径不正确，或公钥未写入 `/etc/dropbear/authorized_keys` 或 `/mnt/UDISK/sanyin-ssh/authorized_keys`；
- 旧版 Dropbear 不支持客户端默认密钥/签名算法；
- UDISK 在开机脚本执行时尚未就绪；重新运行 `ssh-enable` 安装带启动等待的当前脚本；
- 路由器启用了客户端隔离。

不要全局关闭主机指纹验证或放宽 SSH 算法；兼容性配置应只作用于该音箱地址。

## 9. 网页更新被拒绝或自动回滚

先通过 SSH 查看版本、状态和日志：

```bash
./tools/sanyinctl --host 音箱IP --identity .tools/sanyin-ssh config-status
ssh -i .tools/sanyin-ssh root@音箱IP 'cat /tmp/sanyin_update.log; cat /tmp/sanyin_update_restart.log'
```

- `web_update=disabled`：本次完整安装没有找到 `dist/update-public-key`；先运行 `npm run package:update -- X.Y.Z`，再通过 SSH/ADB 执行完整 `config-install`；
- “签名无效”：更新包不是由当前 `.tools/update-signing-key` 生成，或文件已损坏；
- “版本不高于当前版本”：使用更高的 `X.Y.Z`，不支持网页降级；
- “平台不匹配”：上传的不是 `package:update` 生成的 Linux/ARMv7 包；
- `rolled_back`：新程序没有在 20 秒内恢复进程和 8787 端口，设备已恢复上一版；
- `rollback_failed`：立即改用 SSH 或 USB ADB 检查 `/mnt/UDISK/sanyin-config/sanyin-config*` 和 procd 状态。

网页更新期间不要断电。即使浏览器连接短暂失败，设备侧更新与回滚脚本仍会继续。
