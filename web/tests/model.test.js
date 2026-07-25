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
  assert.equal(layoutMode(900), "compact-desktop");
  assert.equal(layoutMode(1440), "desktop");
  const css = await readFile(new URL("../src/styles.css", import.meta.url), "utf8");
  assert.match(css, /@media \(max-width: 760px\)/);
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
	assert.match(appSource, /id="stop-timer-form"/);
	assert.match(appSource, /data-player-action="timer_cancel"/);
	assert.match(appSource, /最长 60 分钟/);
	assert.match(apiSource, /player\(\)/);
	assert.match(apiSource, /controlPlayer\(action/);
	assert.match(apiSource, /request\("\/player\/control"/);
	assert.match(css, /\.player-progress/);
	assert.match(css, /\.queue-list/);
	assert.match(css, /\.station-grid/);
});
