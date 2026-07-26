const labels = {
  healthy: "健康",
  available: "可用",
  online: "在线",
  running: "运行中",
  listening: "正在监听",
  observed_only: "仅观测",
  connected: "已连接",
  strong: "信号强",
	medium: "信号中等",
	weak: "信号较弱",
  idle: "空闲",
	playing: "播放中",
	paused: "已暂停",
	transitioning: "正在加载",
	no_media: "暂无媒体",
  active: "已开启",
  applied: "已应用",
  smart: "智能",
  normal: "标准",
  vocal: "人声",
  live: "现场",
  double_bass: "重低音",
  electronic_music: "电子乐",
  acg: "ACG",
  stopped: "已停止",
  closed: "未监听",
  offline: "离线",
  degraded: "已降级",
  unknown: "未知",
  pending_local_playback: "等待本地播放",
  succeeded: "切换成功",
  rolled_back: "已自动回退",
  rollback_failed: "回退失败",
  recovered_after_restart: "启动时已恢复",
  none: "暂无切换记录",
  snapshot_mode_2: "快照模式 2",
  true: "开启",
  false: "关闭",
};

const dangerValues = new Set(["offline", "stopped", "closed", "failed"]);
const warningValues = new Set(["degraded", "pending_local_playback", "rolling_back"]);

export function statePresentation(state) {
  if (!state) return { label: "无数据", tone: "unknown", detail: "尚未收到状态" };
  const value = String(state.value);
  if (state.freshness === "unknown" || value === "unknown") {
    return { label: "状态未知", tone: "unknown", detail: "没有可靠的当前状态，未使用历史操作结果推断" };
  }
  if (state.freshness === "stale") {
    return { label: "状态陈旧", tone: "warning", detail: "该观测已超过有效期" };
  }
  if (dangerValues.has(value)) return { label: labels[value] || value, tone: "danger", detail: "当前服务不可用" };
  if (warningValues.has(value)) return { label: labels[value] || value, tone: "warning", detail: "当前状态需要关注" };
  return { label: labels[value] || value, tone: value === "observed_only" ? "neutral" : "ok", detail: "状态来自当前观测" };
}

export function displayValue(value, unit = "") {
  const key = String(value);
  if (Object.hasOwn(labels, key)) return labels[key];
  if (typeof value === "number") return `${value}${unit}`;
  if (value && typeof value === "object") return JSON.stringify(value);
  return key;
}

export function capabilityMap(capabilities = []) {
  return Object.fromEntries(capabilities.map((capability) => [capability.id, capability]));
}

export function actionPresentation(capability, { simulation = false } = {}) {
  if (simulation) {
    return { disabled: false, label: "运行模拟操作", reason: "只演示交互流程，不连接设备" };
  }
  if (!capability) return { disabled: true, label: "能力未知", reason: "未收到能力声明" };
  if (capability.writability === "safe" && capability.availability === "available") {
    return { disabled: false, label: "执行", reason: "已完成安全写入验证" };
  }
	if (capability.writability === "experimental" && capability.availability === "available") {
	  return { disabled: false, label: "实验操作", reason: capability.reason || "该操作仍有未完成的异常与重启验证" };
	}
  const reason = capability.reason || (capability.writability === "unsupported" ? "当前不支持本地写入" : "安全写入尚未验证");
  return { disabled: true, label: capability.cloudDependency ? "云端专属" : "暂不可用", reason };
}

export function eqPresentation(eq) {
  const selected = displayValue(eq?.selectedMode?.value ?? "unknown");
  const applied = displayValue(eq?.appliedMode?.value ?? "unknown");
  const pending = eq?.applyState?.value === "pending_local_playback";
  return {
    selected,
    applied,
    pending,
    label: pending ? "业务模式已选中，等待网易本地播放后应用到硬件" : "业务选中态与硬件应用态已分别观测",
  };
}

export function operationPresentation(operation) {
  const failureStates = new Set(["failed"]);
  const rollbackStates = new Set(["rolling_back", "restored"]);
	const simulation = operation.simulation !== false;
  return {
	title: operation.outcome === "succeeded"
	  ? (simulation ? "模拟操作完成" : "真实设备操作完成")
	  : (operation.outcome === "rolled_back" ? (simulation ? "模拟操作已回滚" : "操作已回滚") : "操作未完成"),
    tone: operation.outcome === "succeeded" ? "ok" : "warning",
    steps: operation.timeline.map((step) => ({
      ...step,
      className: failureStates.has(step.state) ? "failed" : rollbackStates.has(step.state) ? "rollback" : "complete",
    })),
  };
}

export function layoutMode(width) {
  if (width <= 760) return "mobile";
	if (width <= 900) return "tablet";
  if (width <= 1080) return "compact-desktop";
  return "desktop";
}

export function formatObservedAt(dateTime) {
  if (!dateTime) return "未知时间";
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(new Date(dateTime));
}

export function formatDuration(seconds) {
	const value = Number.isFinite(Number(seconds)) ? Math.max(0, Math.floor(Number(seconds))) : 0;
	const hours = Math.floor(value / 3600);
	const minutes = Math.floor((value % 3600) / 60);
	const remainder = value % 60;
	const minuteText = String(minutes).padStart(2, "0");
	const secondText = String(remainder).padStart(2, "0");
	return hours > 0 ? `${String(hours).padStart(2, "0")}:${minuteText}:${secondText}` : `${minuteText}:${secondText}`;
}

export function toneRank(tone) {
	return { danger: 0, warning: 1, unknown: 2, neutral: 3, ok: 4 }[tone] ?? 5;
}

export function validateMediaURL(value) {
	const input = String(value || "").trim();
	if (!input) return "请输入媒体 URL";
	try {
		const url = new URL(input);
		if (!["http:", "https:"].includes(url.protocol)) return "仅支持 HTTP 或 HTTPS 地址";
		if (!url.hostname) return "请输入完整的媒体地址";
		return "";
	} catch {
		return "请输入完整的 HTTP/HTTPS 地址";
	}
}

export function validateSSID(value) {
	const input = String(value || "").trim();
	if (!input) return "请输入 Wi-Fi 名称";
	if (new TextEncoder().encode(input).length > 32) return "Wi-Fi 名称不能超过 32 字节";
	return "";
}

export function validateTimerMinutes(value) {
	const minutes = Number(value);
	return Number.isInteger(minutes) && minutes >= 1 && minutes <= 60 ? "" : "请输入 1 到 60 分钟的整数";
}

export function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}
