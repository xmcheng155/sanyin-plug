# 三音 Plug 使用手册

本手册面向希望把三音 Plug 长期安装在网易三音云音箱上的使用者。完成安装后，服务运行在音箱本机；手机或电脑只需要浏览器，不必持续连接 USB，也不需要一直运行开发电脑。

## 1. 使用前须知

当前主要验证环境：

- 设备：网易三音云音箱 `NeteaseC930`；
- 系统：OpenWrt、ARMv7、Linux 3.4.39；
- 固件：`1.1.17.4`；
- AirPlay/RAOP 端口：TCP 5002；
- 三音 Plug 网页端口：TCP 8787。

请注意以下边界：

1. AirPlay 恢复与自动恢复已按安全写能力开放；
2. Wi-Fi、蓝牙、EQ 和本地播放属于实验能力，正常路径已通过实机验收，但不同固件和异常场景覆盖仍不完整；
3. 音量、静音、灯光、麦克风计划、闹钟和提醒目前不提供写入；
4. 不要把音箱的 8787 端口映射到互联网；
5. 首次安装和 SSH 公钥引导时保留 USB ADB；之后可以用 SSH 作为常规维护通道；
6. Wi-Fi 切换时最好同时保留 USB ADB 或确认 SSH 救援路径，便于目标网络不可达时确认回退状态。

## 2. 首次安装

### 2.1 获取代码

```bash
git clone https://github.com/xmcheng155/sanyin-plug.git
cd sanyin-plug
```

### 2.2 准备构建环境

建议使用 macOS，并安装：

- Go 1.26 或更新版本；
- Node.js 20 或更新版本；
- Google Android Platform Tools。

项目可以自动下载 Google 官方 macOS ADB：

```bash
./tools/get_adb_macos.sh
```

下载内容保存在项目的 `.tools/` 中，不会进入 Git。

### 2.3 连接音箱

1. 使用支持数据传输的 Mini-USB 线连接音箱底部接口；
2. 保持音箱正常开机；
3. 执行检查：

```bash
./tools/sanyinctl doctor
```

如果连接了多个 ADB 设备，先查看序列号，再显式指定目标：

```bash
.tools/platform-tools/adb devices
./tools/sanyinctl --serial netease_XXXXXXXX doctor
```

`doctor` 会检查设备型号、系统、分区和工具依赖，不会修改音箱。

### 2.4 安装 AirPlay 恢复服务

```bash
./tools/sanyinctl --serial netease_XXXXXXXX install
./tools/sanyinctl --serial netease_XXXXXXXX verify
```

安装内容位于可恢复的 overlay 中。守护服务会等待原厂播放器和 Wi-Fi 就绪，只在 TCP 5002 未监听时发送 AirPlay 恢复命令。

### 2.5 构建签名版本并安装三音 Plug 网页服务

```bash
npm run package:update -- 1.8.0
./tools/sanyinctl --serial netease_XXXXXXXX config-install
./tools/sanyinctl --serial netease_XXXXXXXX config-status
```

`package:update` 同时完成 ARMv7 构建和更新包签名。第一次运行会生成：

- `.tools/update-signing-key`：Ed25519 私钥，只保留在开发电脑，权限为 `0600`；
- `dist/update-public-key`：安装到音箱的验证公钥；
- `dist/sanyin-config-linux-armv7`：完整安装使用的程序；
- `dist/sanyin-plug-1.8.0.sanyin-update`：网页上传使用的签名包。

`config-install` 会安装：

- ARMv7 单文件服务；
- OpenWrt/procd 启动项；
- EQ 状态验收脚本；
- Wi-Fi 切换与自动回退脚本；
- 网页更新应用与回滚脚本；
- 已生成时的更新验证公钥。

服务会随音箱启动，并监听局域网 TCP 8787。

### 2.6 启用 SSH 公钥运维

先在开发电脑生成仅供音箱使用的 SSH 密钥：

```bash
ssh-keygen -t ed25519 -f .tools/sanyin-ssh -C sanyin-plug
```

通过首次 ADB 连接安装公钥并启用 SSH：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX ssh-enable .tools/sanyin-ssh.pub
```

找到音箱 IP 后验证：

```bash
ssh -i .tools/sanyin-ssh root@192.168.1.50 true
./tools/sanyinctl --host 192.168.1.50 --identity .tools/sanyin-ssh --known-hosts ~/.ssh/known_hosts doctor
```

`ssh-enable` 优先配置固件 Dropbear；固件没有 Dropbear 时会安装项目自带服务。两种实现都只保留 root 公钥认证；自带服务不提供端口转发、PTY 或 SFTP。运维工具使用公钥批处理模式，不会降级为交互密码。首次连接时应保留 ADB，核对目标 IP 后接受主机指纹；后续通过 `--known-hosts` 固定该指纹。若旧版 Dropbear 不支持所选密钥算法，请保留 ADB，改用该固件支持的 RSA/ECDSA 公钥，并把兼容性选项限制到该音箱的 Host 配置。

需要在电脑上运行真实设备开发服务时：

```bash
npm run dev:ssh -- -ssh-host 192.168.1.50 -ssh-identity .tools/sanyin-ssh -ssh-known-hosts ~/.ssh/known_hosts
```

浏览器仍只访问电脑的 `127.0.0.1:8787`；后端通过 SSH 执行项目内固定探针，并把自有配置写入限制在 `/mnt/UDISK/sanyin-config/` 的单层文件中。

## 3. 打开网页

从路由器管理页面、路由器 App 或 DHCP 客户端列表中找到音箱 IP，然后打开：

```text
http://音箱IP:8787/
```

例如音箱 IP 为 `192.168.1.50`：

```text
http://192.168.1.50:8787/
```

页面顶部显示“真实设备”时，状态和操作来自当前音箱。显示“模拟数据”时，当前运行的是电脑上的 Mock 开发服务，不会修改音箱。

如果页面打不开，先通过 USB 执行：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX config-status
```

应能看到进程运行、`0.0.0.0:8787` 正在监听以及当前认证状态。

## 4. 总览页面

总览用于快速确认：

- 设备固件与平台；
- 当前网络连接；
- AirPlay 是否在线；
- 播放器状态；
- 控制中心、SPlayer、KPlayer 等核心服务状态；
- 音频与蓝牙是否可读取。

状态卡片会区分正常、降级、离线、未知和陈旧。未知不等于关闭，例如蓝牙服务在线但缺少无副作用实时查询时，当前开关仍会显示未知。

## 5. AirPlay

AirPlay 页面提供：

- 原厂 SPlayer 运行状态；
- TCP 5002 监听状态；
- AirPlay 恢复服务状态；
- 手动恢复；
- 自动恢复开关。

### 手动恢复

当端口未监听时，点击恢复并确认。服务会发送原厂启动命令，并等待 TCP 5002 回读成功后才报告完成。

### 自动恢复

开启后，守护服务会在 Wi-Fi 和原厂播放器就绪、但 TCP 5002 缺失时自动恢复。关闭自动恢复不会强行停止已经启动的 AirPlay，只会阻止后续自动恢复。

在命令行验证：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX status
./tools/sanyinctl --serial netease_XXXXXXXX verify
./tools/sanyinctl --serial netease_XXXXXXXX logs
```

## 6. 本地播放

“本地播放”表示播放任务在音箱本机执行，不表示媒体文件必须位于局域网。只要音箱能够访问相应 HTTP/HTTPS URL，原厂 KPlayer 就可以直接拉取。

### 6.1 播放 URL

1. 打开“播放”页面；
2. 在“播放 URL”中填写标题；
3. 填写可直接访问的媒体 URL；
4. 选择“立即播放”或“加入队列”。

适合的地址：

- 直接返回音频内容的 MP3/AAC 文件；
- KPlayer 能够识别的网络直播流；
- 音箱可以访问的局域网 HTTP 文件服务器。

不适合的地址：

- 音乐网站的普通播放页面；
- 需要网页登录、Cookie 或复杂 JavaScript 的地址；
- `file://`、FTP 或包含用户名密码的 URL；
- 不可信或可能访问内网管理服务的 URL。

普通曲目会显示当前位置和总时长。播放结束后，设备端后台监视器会自动播放队列中的下一项。

### 6.2 播放控制

- **暂停**：暂停当前 KPlayer 媒体；
- **恢复**：从暂停状态继续；
- **停止**：停止播放，同时取消定时停止；
- **下一首**：立即播放队列中的下一项；
- **停止并清空队列**：停止播放、清空队列并取消定时器。

当前正在播放的队列项不能直接删除。请先停止、切换下一项或播放其他媒体。

### 6.3 本地播放音量

本地媒体正在播放时，可以在“正在播放”区域拖动 0–100 音量滑杆；松手后立即设置，不需要再点击确认按钮。拖动过程中页面实时显示目标值，服务随后通过原厂 KPlayer `RenderingControl` 下发设置，只有 `GetVolume` 回读与目标值一致时才更新为最终状态。

- 音量 0 表示本地播放静音级别，100 表示 KPlayer 最大百分比；
- 该控件只调整当前原厂 KPlayer/DLNA 播放链路，不替代 AirPlay 发送端音量；
- 播放器暂停、停止或尚未载入本地媒体时，音量控件保持禁用，避免原厂接口返回成功但没有实际应用；需要先恢复或开始播放；
- 页面每秒回读一次当前值，其他原厂控制路径改变音量后也会更新显示。

### 6.4 网络电台

1. 输入电台名称；
2. 输入可直接播放的 HTTP/HTTPS 流地址；
3. 点击“收藏电台”；
4. 在电台卡片中选择“播放”或“加入队列”；
5. 使用“↑”“↓”调整电台在列表中的顺序。

电台收藏及当前排列顺序写入音箱的自有配置目录，服务重启后仍会保留。播放过程中即使本地服务重启，也会通过 KPlayer 当前媒体 URI 匹配收藏电台并恢复“正在播放”名称。直播首次缓冲通常比普通文件更慢，页面最多等待约 12 秒确认播放。

部分直播源会被原厂 KPlayer 报告成极长时长或伪停止状态。三音 Plug 会对已经确认播放的电台隐藏无意义进度，并在用户主动停止前维持直播状态。

### 6.5 定时停止

在“正在播放”区域填写 1–60 分钟并点击“设置”。页面会显示剩余时间。

- 倒计时由音箱端服务执行，关闭浏览器不影响；
- 换歌或切换电台后继续倒计时；
- 手动停止或清空队列会取消倒计时；
- 点击“取消”只取消定时，不停止当前播放；
- 配置服务重启会清除定时器，避免旧任务误停。

## 7. Wi-Fi

网络页面会显示当前连接状态、SSID、信号等级和最近一次切换结果。

### 7.1 切换步骤

1. 保持 Mini-USB ADB 连接；
2. 点击“切换 Wi-Fi”；
3. 输入目标 SSID 和密码；开放网络密码留空；
4. 确认切换；
5. 等待音箱连接新网络；
6. 从路由器中找到音箱的新 IP，重新打开 8787 页面。

### 7.2 自动回退

切换前，音箱会备份当前 Wi-Fi 配置。目标网络必须同时满足：

- SSID 与目标一致；
- WPA 状态为已连接；
- 已获得 IPv4；
- 存在默认路由；
- 默认网关可达。

45 秒内未完成验收时，音箱会自动恢复旧配置，并再次等待原网络连接。浏览器在切换过程中断开是正常现象，事务仍在音箱上继续执行。

如果音箱在事务过程中重启，服务启动时会识别未完成标记并恢复原配置。

## 8. EQ

当前提供七种固件模式：

| 编号 | 模式 |
| --- | --- |
| 0 | 普通 |
| 1 | 智能 |
| 2 | 人声 |
| 3 | 现场 |
| 4 | 重低音 |
| 5 | 电子乐 |
| 6 | ACG |

页面分别展示：

- **业务选中模式**：原厂业务层当前记录的模式；
- **硬件已应用模式**：从 ADAU1761 效果文件回读的模式；
- **应用状态**：已应用、等待本地播放或未知。

音箱空闲或使用 AirPlay 时，EQ 可能只更新业务选中态。原厂 KPlayer 开始本地播放后，硬件效果才可能立即加载，因此“等待本地播放”不是操作失败。

## 9. 蓝牙

蓝牙页面可以请求开启或关闭，并等待原厂蓝牙服务返回对应成功事件。

当前固件缺少无副作用的实时开关查询，因此：

- “最近验收状态”表示最近一次操作收到成功事件；
- “当前开关未知”不能理解成蓝牙已经关闭；
- 已连接设备、服务异常和重启后的完整行为仍属于待扩展验证范围。

## 10. 登录认证

三音 Plug 默认不设置密码。建议只在可信家庭局域网使用，并禁止路由器把 8787 映射到互联网。

启用认证：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX config-auth-enable
```

工具会显示：

- 用户名：`admin`；
- 随机生成的密码。

查看认证状态和当前密码：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX config-password
```

禁用认证：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX config-auth-disable
```

密码保存在音箱 `/mnt/UDISK/sanyin-config` 自有目录中，权限为 `0600`。不要把密码写入 README、脚本、截图或 Git 仓库。

## 11. 更新版本

### 11.1 通过 SSH 完整更新

在项目目录执行：

```bash
git pull
npm test
npm run package:update -- 1.8.1
./tools/sanyinctl --host 192.168.1.50 --identity .tools/sanyin-ssh --known-hosts ~/.ssh/known_hosts --force config-install
./tools/sanyinctl --host 192.168.1.50 --identity .tools/sanyin-ssh --known-hosts ~/.ssh/known_hosts config-status
```

版本必须使用严格递增的 `X.Y.Z`。完整更新会同步二进制、procd 启动项、EQ/Wi-Fi 辅助脚本、更新回滚脚本和验证公钥，适合代码跨组件变化时使用。`--force` 只应在确认目标文件属于上一版三音 Plug 后使用。

更新程序不会删除电台收藏、网页登录密码或 AirPlay 自动恢复配置。如果 AirPlay 守护脚本也有更新，再执行：

```bash
./tools/sanyinctl --host 192.168.1.50 --identity .tools/sanyin-ssh --force install
./tools/sanyinctl --host 192.168.1.50 --identity .tools/sanyin-ssh verify
```

### 11.2 从网页更新单文件服务

当改动只涉及 Go 服务或内嵌网页时：

1. 在开发电脑执行 `npm test`；
2. 用更高版本执行 `npm run package:update -- 1.8.1`；
3. 打开音箱网页的“版本”页面；
4. 选择 `dist/sanyin-plug-1.8.1.sanyin-update`；
5. 确认更新并等待页面恢复。

设备会依次检查：

- 更新包只包含约定的三个文件；
- `manifest.json` 的 Ed25519 签名与设备公钥一致；
- 程序 SHA-256 与清单一致；
- 版本是严格递增的 `X.Y.Z`；
- 程序是 Linux/ARMv7 小端 ELF；
- 程序内置版本与清单一致。

验证通过后，设备在同一文件系统中原子切换程序并重启服务。20 秒内未同时确认新进程和 TCP 8787 时，会恢复上一版并再次启动。更新状态保存在 `/mnt/UDISK/sanyin-config/update/update-status`，详细日志在 `/tmp/sanyin_update.log`。

网页更新不会同步 init、EQ、Wi-Fi 或更新器脚本；这些文件发生变化时使用 11.1 的 SSH 完整更新。请备份 `.tools/update-signing-key` 到安全的离线位置；不要把它发送给音箱、提交 Git 或放入公开网盘。

## 12. 停止或恢复服务

移除 AirPlay 恢复服务：

```bash
./tools/sanyinctl --serial netease_XXXXXXXX uninstall
```

这不会修改 bootloader、内核或只读 SquashFS。

临时停止网页配置服务：

```bash
ssh -i .tools/sanyin-ssh root@192.168.1.50 /etc/init.d/sanyin_config stop
```

禁止下次开机启动：

```bash
ssh -i .tools/sanyin-ssh root@192.168.1.50 /etc/init.d/sanyin_config disable
```

重新启用：

```bash
ssh -i .tools/sanyin-ssh root@192.168.1.50 /etc/init.d/sanyin_config enable
ssh -i .tools/sanyin-ssh root@192.168.1.50 /etc/init.d/sanyin_config start
```

当前工具没有自动删除网页服务和用户配置的命令，以避免误删电台、密码和网络事务数据。

## 13. 常见问题

### 页面突然打不开

- 确认音箱 IP 是否因 DHCP 变化；
- 运行 `config-status` 检查服务和端口；
- 如果刚切换 Wi-Fi，等待事务完成并查询新地址；
- 检查手机是否位于访客网络或启用了客户端隔离。

### 电台或音乐 URL 无法播放

- 确认 URL 是媒体流而不是网页；
- 在同一网络使用 `curl -I` 检查地址是否可访问；
- 检查是否要求 Cookie、Referer 或登录；
- HTTPS 证书、重定向或媒体格式可能不受旧版 KPlayer 支持；
- 直播源首次缓冲请至少等待 12 秒。

### Wi-Fi 切换后原地址断开

这是正常现象。等待目标网络取得地址，再从路由器查找新 IP。目标连接失败时，音箱会在回退后重新出现在原网络。

### EQ 显示等待本地播放

表示业务模式已经选中，但硬件效果等待原厂 KPlayer 播放时应用。AirPlay 场景可能不会立即加载该效果。

### 蓝牙操作成功但当前状态未知

“操作成功”来自对应原厂事件；“当前未知”表示尚无安全的实时读取方式，两者并不矛盾。

更多问题见 [故障排查](04-troubleshooting.md) 和 [协议确认记录](06-local-config-protocol-verification.md)。

## 14. API 与自动化

网页使用的全部功能都通过 `/api/v1` 提供。开发者可以参考：

- [OpenAPI 3.1 契约](../service/openapi.json)；
- [API 字段和写操作语义](08-local-config-api-contract.md)；
- [能力等级与参数清单](05-local-config-api-and-parameter-inventory.md)。

写请求要求与网页同源，并携带 `X-Sanyin-CSRF: 1`。启用 Basic Auth 后还需要用户名 `admin` 和当前密码。不要绕过三音 Plug 直接把原厂 KPlayer、EQ 或其他内部控制端口暴露给局域网或互联网。

`POST /api/v1/system/update` 还要求媒体类型 `application/vnd.sanyin.update+zip`，并始终执行签名、哈希、版本与平台校验；不能上传裸二进制。
