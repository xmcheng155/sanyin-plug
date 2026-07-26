import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import {
  actionPresentation,
  eqPresentation,
	formatDuration,
  layoutMode,
  operationPresentation,
  statePresentation,
	toneRank,
	validateMediaURL,
	validateSSID,
	validateTimerMinutes,
} from "../src/model.js";

const observed = (value, freshness = "fresh") => ({
  value,
  observedAt: "2026-07-23T09:00:00+08:00",
  source: "mock",
  freshness,
  revision: 1,
});

test("正常、离线和降级状态使用不同语义", () => {
  assert.deepEqual(statePresentation(observed("online")).tone, "ok");
  assert.deepEqual(statePresentation(observed("offline")).tone, "danger");
  assert.deepEqual(statePresentation(observed("degraded")).tone, "warning");
});

test("未知状态不会由最近操作结果推断", () => {
  const result = statePresentation(observed("unknown", "unknown"));
  assert.equal(result.tone, "unknown");
  assert.match(result.detail, /未使用历史操作结果推断/);
});

test("陈旧观测明确显示为状态陈旧", () => {
  const result = statePresentation(observed("online", "stale"));
  assert.equal(result.tone, "warning");
  assert.equal(result.label, "状态陈旧");
});

test("能力不足时操作禁用并返回原因", () => {
  const capability = {
    id: "wifi.connection",
    writability: "not_verified",
    availability: "available",
    reason: "扫描与回滚协议尚未验证",
  };
  assert.deepEqual(actionPresentation(capability), {
    disabled: true,
    label: "暂不可用",
    reason: "扫描与回滚协议尚未验证",
  });
  assert.equal(actionPresentation(capability, { simulation: true }).disabled, false);
});

test("实验性且设备可用的能力允许操作并明确标识风险", () => {
  const capability = {
    id: "bluetooth.enabled",
    writability: "experimental",
    availability: "available",
    reason: "成功事件已验证，异常回滚仍待验证",
  };
  assert.deepEqual(actionPresentation(capability), {
    disabled: false,
    label: "实验操作",
    reason: "成功事件已验证，异常回滚仍待验证",
  });
});

test("EQ pending 保留选中态与硬件态", () => {
  const result = eqPresentation({
    selectedMode: observed("vocal"),
    appliedMode: observed("normal"),
    applyState: observed("pending_local_playback"),
  });
  assert.equal(result.selected, "人声");
  assert.equal(result.applied, "标准");
  assert.equal(result.pending, true);
  assert.match(result.label, /等待网易本地播放/);
});

test("操作失败流程展示失败、回滚中和已恢复", () => {
  const result = operationPresentation({
    outcome: "rolled_back",
    timeline: [
      { state: "confirmed", label: "已确认" },
      { state: "failed", label: "失败" },
      { state: "rolling_back", label: "回滚中" },
      { state: "restored", label: "已恢复" },
    ],
  });
  assert.equal(result.title, "模拟操作已回滚");
  assert.deepEqual(result.steps.map((step) => step.className), ["complete", "failed", "rollback", "rollback"]);
});

test("真实设备操作不会被标记为模拟流程", () => {
  const result = operationPresentation({
	simulation: false,
	outcome: "succeeded",
	timeline: [{ state: "succeeded", label: "真实设备验收成功" }],
  });
  assert.equal(result.title, "真实设备操作完成");
  assert.equal(result.steps[0].className, "complete");
});

test("Mock 环境标识在初始页面中始终可见", async () => {
  const html = await readFile(new URL("../index.html", import.meta.url), "utf8");
  assert.match(html, /模拟数据环境/);
  assert.match(html, /不会连接或修改音箱/);
});

test("真实设备环境不显示额外提示横幅", async () => {
	const source = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
	assert.match(source, /elements\.banner\.hidden = real/);
	assert.doesNotMatch(source, /<strong>真实设备环境<\/strong>/);
});

test("总览使用本地设备实拍并标注原始来源", async () => {
  const source = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
  const photo = await readFile(new URL("../assets/sanyin-speaker-red.jpg", import.meta.url));
  assert.match(source, /\/assets\/sanyin-speaker-red\.jpg/);
  assert.match(source, /实拍来源 · 我爱音频网/);
  assert.match(source, /https:\/\/www\.52audio\.com\/archives\/27175\.html/);
  assert.ok(photo.length > 100_000);
  assert.equal(photo[0], 0xff);
  assert.equal(photo[1], 0xd8);
});

test("主题使用与旧设备界面匹配的石墨黑、暖白和网易红", async () => {
  const css = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
  assert.match(css, /--accent: #e53531/);
  assert.match(css, /background: #1b1a19/);
  assert.match(css, /--canvas: #f3f1ed/);
});

test("手机和桌面关键布局均有明确断点", async () => {
  assert.equal(layoutMode(390), "mobile");
	assert.equal(layoutMode(768), "tablet");
	assert.equal(layoutMode(900), "tablet");
	assert.equal(layoutMode(1000), "compact-desktop");
  assert.equal(layoutMode(1440), "desktop");
  const css = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
	assert.match(css, /@media \(max-width: 900px\)/);
  assert.match(css, /\.mobile-nav \{ position: fixed/);
  assert.match(css, /\.topbar-actions \{ width: 100%/);
  assert.match(css, /@media \(max-width: 1080px\)/);
});

test("网页状态只从 API 客户端加载，不导入 Mock 常量", async () => {
  const source = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
  assert.match(source, /api\.status\(\)/);
  assert.match(source, /api\.capabilities\(\)/);
  assert.doesNotMatch(source, /from ["'].*mocks/);
});

test("真实设备模式提供经确认的 AirPlay 恢复入口", async () => {
  const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
  const apiSource = await readFile(new URL("../src/api.js", import.meta.url), "utf8");
  assert.match(appSource, /data-action="recover-airplay"/);
  assert.match(appSource, /真实设备操作/);
  assert.match(apiSource, /recoverAirplay\(\)/);
  assert.match(apiSource, /request\("\/airplay\/recover", \{ method: "POST" \}\)/);
});

test("网页提供可回读的 AirPlay 自动恢复配置", async () => {
  const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
  const apiSource = await readFile(new URL("../src/api.js", import.meta.url), "utf8");
  assert.match(appSource, /data-action="configure-autorecover"/);
  assert.match(appSource, /持久配置 · 原子写入并回读/);
  assert.match(apiSource, /setAirplayAutoRecover\(enabled\)/);
  assert.match(apiSource, /request\("\/airplay\/auto-recover"/);
  assert.match(apiSource, /JSON\.stringify\(\{ enabled: Boolean\(enabled\) \}\)/);
});

test("网页提供经事件确认的蓝牙开启与关闭操作", async () => {
  const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
  const apiSource = await readFile(new URL("../src/api.js", import.meta.url), "utf8");
  assert.match(appSource, /data-action="configure-bluetooth"/);
  assert.match(appSource, /id="confirm-bluetooth"/);
  assert.match(appSource, /最近一次验收结果/);
  assert.match(apiSource, /setBluetooth\(enabled\)/);
  assert.match(apiSource, /request\("\/bluetooth"/);
});

test("网页提供 0 到 6 的 EQ 固定模式并区分业务与硬件状态", async () => {
  const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
  const apiSource = await readFile(new URL("../src/api.js", import.meta.url), "utf8");
  assert.match(appSource, /\{ mode: 0, label: "普通" \}/);
  assert.match(appSource, /\{ mode: 6, label: "ACG" \}/);
  assert.match(appSource, /data-action="configure-eq"/);
  assert.match(appSource, /id="confirm-eq"/);
  assert.match(appSource, /业务选中态事件与硬件文件回读/);
  assert.match(apiSource, /setEQ\(mode\)/);
  assert.match(apiSource, /request\("\/audio\/effect"/);
});

test("网页显示当前 Wi-Fi 并提供失败自动回退的切换流程", async () => {
  const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
  const apiSource = await readFile(new URL("../src/api.js", import.meta.url), "utf8");
  assert.match(appSource, /当前 Wi-Fi/);
  assert.match(appSource, /data-action="configure-wifi"/);
  assert.match(appSource, /45 秒内未同时确认目标 SSID、IPv4、默认路由和网关可达/);
  assert.match(appSource, /失败时最多再等待 45 秒恢复原网络/);
  assert.match(apiSource, /switchWiFi\(ssid, password\)/);
  assert.match(apiSource, /request\("\/network\/switch"/);
});

test("播放器进度使用稳定的时分秒格式", () => {
	assert.equal(formatDuration(0), "00:00");
	assert.equal(formatDuration(67), "01:07");
	assert.equal(formatDuration(3723), "01:02:03");
	assert.equal(formatDuration(-1), "00:00");
});

test("异常状态在总览中的排序高于正常状态", () => {
	assert.ok(toneRank("danger") < toneRank("warning"));
	assert.ok(toneRank("warning") < toneRank("unknown"));
	assert.ok(toneRank("unknown") < toneRank("ok"));
});

test("配置表单提供可复用的前端校验", () => {
	assert.equal(validateMediaURL("https://media.example/music.mp3"), "");
	assert.match(validateMediaURL("file:///tmp/music.mp3"), /HTTP/);
	assert.match(validateMediaURL("not-a-url"), /完整/);
	assert.equal(validateSSID("演示网络"), "");
	assert.match(validateSSID(""), /Wi-Fi/);
	assert.equal(validateTimerMinutes(30), "");
	assert.match(validateTimerMinutes(61), /1 到 60/);
});

test("移动端导航收敛为三个主入口和更多菜单", async () => {
	const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
	const css = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
	assert.match(appSource, /mobilePrimaryOrder = \["overview", "player", "airplay"\]/);
	assert.match(appSource, /mobilePrimaryRoutes = new Set\(mobilePrimaryOrder\)/);
	assert.match(appSource, /data-action="toggle-mobile-more"/);
	assert.match(appSource, /aria-current="page"/);
	assert.match(css, /\.mobile-nav \{[^}]*grid-template-columns: repeat\(4, 1fr\)/s);
	assert.doesNotMatch(css, /\.mobile-nav \{[^}]*repeat\(8/s);
});

test("页面提供设备上下文、刷新时间和断线保留状态", async () => {
	const html = await readFile(new URL("../index.html", import.meta.url), "utf8");
	const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
	assert.match(html, /id="device-context"/);
	assert.match(html, /id="refresh-status"/);
	assert.match(html, /id="connection-banner"/);
	assert.match(appSource, /lastUpdatedAt/);
	assert.match(appSource, /已保留最近一次设备状态/);
	assert.match(appSource, /overviewItems[\s\S]*sort\(\(a, b\) => toneRank/);
});

test("危险操作、表单反馈和降低动画均有明确样式", async () => {
	const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
	const css = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
	assert.match(appSource, /class="button danger" data-player-action="queue_clear"/);
	assert.match(appSource, /data-action="toggle-password"/);
	assert.match(appSource, /class="field-error"/);
	assert.match(appSource, /showOperationLoading/);
	assert.match(css, /\.button\.warning/);
	assert.match(css, /\.button\.danger/);
	assert.match(css, /\.icon-button \{ width: 44px; height: 44px; \}/);
	assert.match(css, /\.password-toggle \{[^}]*height: 44px/s);
	assert.match(css, /@media \(prefers-reduced-motion: reduce\)/);
});

test("网页提供本地 URL 播放、完整控制、队列和网络电台", async () => {
	const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
	const apiSource = await readFile(new URL("../src/api.js", import.meta.url), "utf8");
	const css = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
	assert.match(appSource, /id: "player"/);
	assert.match(appSource, /id="player-url-form"/);
	assert.match(appSource, /data-player-action="pause"/);
	assert.match(appSource, /data-player-action="resume"/);
	assert.match(appSource, /data-player-action="stop"/);
	assert.match(appSource, /播放队列/);
	assert.match(appSource, /网络电台/);
	assert.match(appSource, /id="radio-station-form"/);
	assert.match(appSource, /data-player-action="radio_move_up"/);
	assert.match(appSource, /data-player-action="radio_move_down"/);
	assert.match(appSource, /id="stop-timer-form"/);
	assert.match(appSource, /data-player-action="timer_cancel"/);
	assert.match(appSource, /最长 60 分钟/);
	assert.match(appSource, /id="player-volume-range"/);
	assert.match(appSource, /name="volume"[^>]*min="0"[^>]*max="100"/);
	assert.match(appSource, /volume_set/);
	assert.match(appSource, /volumeAdjustable = transport === "playing"/);
	assert.match(appSource, /拖动时自动生效/);
	assert.match(appSource, /queuePlayerVolume\(event\.target/);
	assert.match(appSource, /playerVolumePending/);
	assert.match(appSource, /window\.setTimeout\(flushPlayerVolume, 150\)/);
	assert.doesNotMatch(appSource, /input\.disabled = true/);
	assert.doesNotMatch(appSource, /input\.blur\(\)/);
	assert.doesNotMatch(appSource, />应用音量</);
	assert.match(apiSource, /player\(\)/);
	assert.match(apiSource, /controlPlayer\(action/);
	assert.match(apiSource, /request\("\/player\/control"/);
	assert.match(css, /\.player-progress/);
	assert.match(css, /\.queue-list/);
	assert.match(css, /\.station-grid/);
});

test("播放表单按控件基线对齐并优先展示媒体 URL", async () => {
	const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
	const css = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
	assert.ok(appSource.indexOf("media-primary-label\" for=\"player-media-url") < appSource.indexOf("media-secondary-label\" for=\"player-media-title"));
	assert.match(appSource, /media-primary-control/);
	assert.match(appSource, /media-secondary-control/);
	assert.match(appSource, /media-primary-feedback/);
	assert.match(appSource, /media-secondary-feedback/);
	assert.match(css, /grid-template-areas: "primary-label secondary-label action-label" "primary-control secondary-control actions" "primary-feedback secondary-feedback action-hints"/);
	assert.match(css, /width: min\(100%, 1040px\)/);
});

test("播放易用性提供定时快捷值、URL 工具和操作影响说明", async () => {
	const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
	assert.match(appSource, /\[15, 30, 45, 60\]\.map/);
	assert.match(appSource, /data-timer-minutes="\$\{minutes\}"/);
	assert.match(appSource, /data-url-action="paste"/);
	assert.match(appSource, /data-url-action="clear"/);
	assert.match(appSource, /替换当前播放/);
	assert.match(appSource, /添加到队列末尾/);
	assert.match(appSource, /validateMediaURLInput/);
	assert.match(appSource, /window\.isSecureContext/);
});

test("普通、警告和危险按钮使用不同颜色并提供轻量兼容性说明", async () => {
	const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
	const css = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
	assert.match(appSource, /<details class="compatibility-note">/);
	assert.match(appSource, /主要播放路径已完成实机验收/);
	assert.match(css, /\.button \{[^}]*background: var\(--primary\)/s);
	assert.match(css, /\.button\.warning \{[^}]*background: var\(--amber\)/s);
	assert.match(css, /\.button\.danger \{[^}]*background: var\(--red\)/s);
	assert.match(css, /@media \(max-width: 600px\)[\s\S]*grid-template-areas: "preset-label" "presets" "minute-label" "minute"/);
});

test("场景模式保存在设备并提供创建、预览、应用、编辑和删除流程", async () => {
	const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
	const apiSource = await readFile(new URL("../src/api.js", import.meta.url), "utf8");
	const css = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
	assert.match(appSource, /sectionHeading\("场景模式"/);
	assert.match(appSource, /场景和自动启动计划保存在音箱本机/);
	assert.match(appSource, /data-scene-action="create"/);
	assert.match(appSource, /data-scene-action="apply"/);
	assert.match(appSource, /openSceneApplyConfirmation/);
	assert.match(appSource, /替换当前播放 · 音量/);
	assert.match(appSource, /不填写新地址即可完整保留/);
	assert.match(appSource, /id="scene-schedule-enabled"/);
	assert.match(appSource, /data-scene-days="\$\{item\.id\}"/);
	assert.match(appSource, /name="sceneWeekday"/);
	assert.match(appSource, /schedule:\s*\{[\s\S]*enabled: scheduleEnabled[\s\S]*weekdays: scheduleWeekdays/);
	assert.match(appSource, /下次 .*（音箱时间）/);
	assert.match(appSource, /window\.setInterval\(refreshScenes, 30000\)/);
	assert.match(apiSource, /scenes\(\)/);
	assert.match(apiSource, /createScene\(payload\)/);
	assert.match(apiSource, /updateScene\(id, payload\)/);
	assert.match(apiSource, /deleteScene\(id\)/);
	assert.match(apiSource, /applyScene\(id\)/);
	assert.match(css, /\.scene-grid \{[^}]*grid-template-columns: repeat\(3/s);
	assert.match(css, /\.scene-schedule-editor \{/);
	assert.match(css, /\.scene-weekday-picker \{[^}]*repeat\(7/s);
	assert.match(css, /@media \(max-width: 600px\)[\s\S]*\.scene-grid \{ grid-template-columns: 1fr/);
	assert.match(css, /@media \(max-width: 600px\)[\s\S]*\.scene-weekday-picker \{ grid-template-columns: repeat\(4/);
});

test("网页显示应用版本并只上传签名更新包", async () => {
	const appSource = await readFile(new URL("../src/app.js", import.meta.url), "utf8");
	const apiSource = await readFile(new URL("../src/api.js", import.meta.url), "utf8");
	assert.match(appSource, /id: "system"/);
	assert.match(appSource, /id="system-update-form"/);
	assert.match(appSource, /\.sanyin-update/);
	assert.match(appSource, /Ed25519 签名/);
	assert.match(appSource, /自动回滚/);
	assert.match(apiSource, /system\(\)/);
	assert.match(apiSource, /updateSystem\(file\)/);
	assert.match(apiSource, /application\/vnd\.sanyin\.update\+zip/);
});
