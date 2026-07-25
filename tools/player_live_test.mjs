#!/usr/bin/env node

const [baseURL, firstURL, secondURL] = process.argv.slice(2);
if (!baseURL || !firstURL || !secondURL) {
  console.error("用法：node tools/player_live_test.mjs <服务地址> <第一条媒体 URL> <第二条媒体 URL>");
  process.exit(2);
}

const apiBase = `${baseURL.replace(/\/$/, "")}/api/v1`;
const headers = { Accept: "application/json", "Content-Type": "application/json", Origin: baseURL, "X-Sanyin-CSRF": "1" };

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

async function player() {
  const response = await fetch(`${apiBase}/player`);
  const body = await response.json();
  if (!response.ok) throw new Error(`GET player: ${response.status} ${JSON.stringify(body)}`);
  return body.data;
}

async function control(action, payload = {}) {
  const response = await fetch(`${apiBase}/player/control`, { method: "POST", headers, body: JSON.stringify({ action, ...payload }) });
  const body = await response.json();
  if (!response.ok) throw new Error(`${action}: ${response.status} ${JSON.stringify(body)}`);
  return body.data;
}

const sleep = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));
const summary = [];

try {
  let state = await control("play_url", { title: "实机测试 A", url: firstURL });
  assert(state.transport.value === "playing" && state.current?.title === "实机测试 A", "URL 播放未进入 playing");
  await sleep(4000);
  state = await player();
  assert(state.positionSeconds.value > 0 && state.durationSeconds.value > 0, "播放进度或时长不可读");
  summary.push(`play_progress=${state.positionSeconds.value}/${state.durationSeconds.value}`);

  state = await control("pause");
  assert(state.transport.value === "paused", "暂停未通过状态回读");
  summary.push("pause=verified");

  state = await control("resume");
  assert(state.transport.value === "playing", "恢复未通过状态回读");
  summary.push("resume=verified");

  state = await control("queue_add", { title: "实机测试 B", url: secondURL });
  assert(state.queue.length === 2, "第二条 URL 未加入队列");
  state = await control("next");
  assert(state.current?.title === "实机测试 B" && state.transport.value === "playing", "手动下一首失败");
  summary.push("manual_next=verified");

  state = await control("stop");
  assert(state.transport.value === "stopped", "停止未通过状态回读");
  state = await control("queue_clear");
  assert(state.queue.length === 0 && state.currentIndex === -1, "队列清空失败");
  summary.push("stop_and_clear=verified");

  state = await control("play_url", { title: "自动续播 A", url: firstURL });
  state = await control("queue_add", { title: "自动续播 B", url: secondURL });
  const autoDeadline = Date.now() + 22000;
  while (Date.now() < autoDeadline) {
    await sleep(1000);
    state = await player();
    if (state.current?.title === "自动续播 B" && state.transport.value === "playing") break;
  }
  assert(state.current?.title === "自动续播 B", "第一项完成后未自动续播第二项");
  summary.push("automatic_advance=verified");
  await control("stop");
  await control("queue_clear");

  state = await control("radio_add", { title: "实机测试电台", url: firstURL });
  const station = state.stations.find((item) => item.name === "实机测试电台");
  assert(station, "网络电台未保存");
  state = await control("radio_play", { itemId: station.id });
  assert(state.current?.kind === "radio" && state.transport.value === "playing", "网络电台未播放");
  await control("stop");
  state = await control("radio_queue", { itemId: station.id });
  assert(state.queue.some((item) => item.kind === "radio"), "网络电台未加入队列");
  state = await control("radio_remove", { itemId: station.id });
  assert(!state.stations.some((item) => item.id === station.id), "网络电台未删除");
  await control("queue_clear");
  summary.push("radio_add_play_queue_remove=verified");

  console.log(summary.join("\n"));
} catch (error) {
  try { await control("stop"); } catch {}
  try { await control("queue_clear"); } catch {}
  console.error(error.stack || error.message);
  process.exit(1);
}
