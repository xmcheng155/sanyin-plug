import { ApiClient } from "./api.js";
import {
  actionPresentation,
  capabilityMap,
  displayValue,
  eqPresentation,
  escapeHTML,
	formatDuration,
  formatObservedAt,
  operationPresentation,
  statePresentation,
} from "./model.js";

const navItems = [
  { id: "overview", label: "总览", icon: "⌂", title: "设备总览" },
  { id: "airplay", label: "AirPlay", icon: "◉", title: "AirPlay" },
	{ id: "player", label: "播放", icon: "▶", title: "本地播放" },
  { id: "network", label: "网络", icon: "⌁", title: "网络状态" },
  { id: "audio", label: "音频", icon: "♫", title: "音频与 EQ" },
  { id: "bluetooth", label: "蓝牙", icon: "ᛒ", title: "蓝牙" },
  { id: "diagnostics", label: "诊断", icon: "◫", title: "灯光、计划与诊断" },
];

const eqModes = [
  { mode: 0, label: "普通" },
  { mode: 1, label: "智能" },
  { mode: 2, label: "人声" },
  { mode: 3, label: "现场" },
  { mode: 4, label: "重低音" },
  { mode: 5, label: "电子乐" },
  { mode: 6, label: "ACG" },
];

const api = new ApiClient();
const app = {
  route: location.hash.slice(1) || "overview",
  scenario: localStorage.getItem("sanyin.mockScenario") || "healthy",
  data: null,
  events: [],
  eventSource: null,
  loading: true,
	error: null,
	environment: null,
	playerRefreshing: false,
};

const elements = {
  desktopNav: document.querySelector("#desktop-nav"),
  mobileNav: document.querySelector("#mobile-nav"),
  title: document.querySelector("#page-title"),
  content: document.querySelector("#page-content"),
  scenario: document.querySelector("#scenario-select"),
  refresh: document.querySelector("#refresh-button"),
  dialog: document.querySelector("#operation-dialog"),
  dialogContent: document.querySelector("#operation-dialog-content"),
  toast: document.querySelector("#toast-region"),
	banner: document.querySelector("#environment-banner"),
	runtimeModeTitle: document.querySelector("#runtime-mode-title"),
	runtimeModeDetail: document.querySelector("#runtime-mode-detail"),
};

function navMarkup(item) {
  return `<button class="nav-link ${app.route === item.id ? "active" : ""}" data-route="${item.id}" type="button"><span class="nav-icon">${item.icon}</span><span>${item.label}</span></button>`;
}

function renderNavigation() {
  const markup = navItems.map(navMarkup).join("");
  elements.desktopNav.innerHTML = markup;
  elements.mobileNav.innerHTML = markup;
  const current = navItems.find((item) => item.id === app.route) || navItems[0];
  elements.title.textContent = current.title;
}

async function initialize() {
  api.setScenario(app.scenario);
  renderNavigation();
  bindEvents();
  try {
    const scenarioResponse = await api.scenarios();
	app.environment = scenarioResponse.environment;
    renderScenarioOptions(scenarioResponse.data);
	if (app.environment === "mock") {
	  if (!scenarioResponse.data.some((scenario) => scenario.id === app.scenario)) app.scenario = scenarioResponse.scenario;
	} else {
	  app.scenario = scenarioResponse.scenario;
	}
	api.setScenario(app.scenario);
	elements.scenario.value = app.scenario;
	renderEnvironment();
  } catch (error) {
    app.error = error;
  }
  await loadData();
  connectEvents();
	window.setInterval(refreshPlayer, 1000);
}

function bindEvents() {
  document.addEventListener("click", (event) => {
    const routeButton = event.target.closest("[data-route]");
    if (routeButton) {
      app.route = routeButton.dataset.route;
      location.hash = app.route;
      renderNavigation();
      renderPage();
      window.scrollTo({ top: 0, behavior: "smooth" });
    }
	if (event.target.closest("[data-action='simulate-airplay']")) openOperationConfirmation(true);
	if (event.target.closest("[data-action='recover-airplay']")) openOperationConfirmation(false);
	const autoRecoverButton = event.target.closest("[data-action='configure-autorecover']");
	if (autoRecoverButton) openAutoRecoverConfirmation(autoRecoverButton.dataset.enabled === "true");
	if (event.target.closest("#confirm-operation")) runAirPlayOperation(event.target.closest("#confirm-operation").dataset.simulation === "true");
	if (event.target.closest("#confirm-autorecover")) runAutoRecoverConfiguration(event.target.closest("#confirm-autorecover").dataset.enabled === "true");
	const bluetoothButton = event.target.closest("[data-action='configure-bluetooth']");
	if (bluetoothButton) openBluetoothConfirmation(bluetoothButton.dataset.enabled === "true");
	if (event.target.closest("#confirm-bluetooth")) runBluetoothConfiguration(event.target.closest("#confirm-bluetooth").dataset.enabled === "true");
	const eqButton = event.target.closest("[data-action='configure-eq']");
	if (eqButton) openEQConfirmation(Number(eqButton.dataset.mode));
	if (event.target.closest("#confirm-eq")) runEQConfiguration(Number(event.target.closest("#confirm-eq").dataset.mode));
	if (event.target.closest("[data-action='configure-wifi']")) openWiFiConfiguration();
	if (event.target.closest("#confirm-wifi")) runWiFiConfiguration();
	const playerButton = event.target.closest("[data-player-action]");
	if (playerButton) runPlayerAction(playerButton.dataset.playerAction, playerButton.dataset.itemId || "");
    if (event.target.closest("[data-action='reload']")) location.reload();
  });

	document.addEventListener("submit", (event) => {
		if (event.target.id === "player-url-form") {
			event.preventDefault();
			submitPlayerURL(event);
		}
		if (event.target.id === "radio-station-form") {
			event.preventDefault();
			submitRadioStation(event.target);
		}
		if (event.target.id === "stop-timer-form") {
			event.preventDefault();
			submitStopTimer(event.target);
		}
	});

  elements.scenario.addEventListener("change", async (event) => {
	if (app.environment !== "mock") return;
    app.scenario = event.target.value;
    localStorage.setItem("sanyin.mockScenario", app.scenario);
    api.setScenario(app.scenario);
    app.events = [];
    await loadData();
    connectEvents();
    toast(`已切换至“${event.target.selectedOptions[0].textContent}”场景`);
  });

  elements.refresh.addEventListener("click", loadData);
  window.addEventListener("hashchange", () => {
    app.route = location.hash.slice(1) || "overview";
    if (!navItems.some((item) => item.id === app.route)) app.route = "overview";
    renderNavigation();
    renderPage();
  });
}

function renderScenarioOptions(scenarios) {
  elements.scenario.innerHTML = scenarios.map((scenario) => `<option value="${escapeHTML(scenario.id)}">${escapeHTML(scenario.name)}</option>`).join("");
  elements.scenario.value = app.scenario;
}

function renderEnvironment() {
	const real = app.environment === "device";
	elements.scenario.closest(".scenario-control").hidden = real;
	elements.runtimeModeTitle.textContent = real ? "真实设备" : "本地模式";
	elements.runtimeModeDetail.textContent = real ? "DeviceAdapter 已连接" : "Mock 服务";
	elements.banner.classList.toggle("environment-real", real);
	elements.banner.innerHTML = real
	  ? `<span class="banner-icon">D</span><span><strong>真实设备环境</strong><small>状态来自当前音箱；可写操作将明确确认并验收结果。</small></span>`
	  : `<span class="banner-icon">M</span><span><strong>模拟数据环境</strong><small>所有状态和操作均为本机 Mock，不会连接或修改音箱。</small></span>`;
}

async function loadData() {
  app.loading = true;
  app.error = null;
  elements.refresh.classList.add("spinning");
  renderPage();
  try {
    const [capabilities, device, status, airplay, network, audio, bluetooth, lighting, schedules, player] = await Promise.all([
      api.capabilities(), api.device(), api.status(), api.airplay(), api.network(), api.audio(), api.bluetooth(), api.lighting(), api.schedules(), api.player(),
    ]);
    app.data = {
      environment: status.environment,
      capabilities: capabilities.data,
      capabilityMap: capabilityMap(capabilities.data),
      device: device.data,
      status: status.data,
      airplay: airplay.data,
      network: network.data,
      audio: audio.data,
      bluetooth: bluetooth.data,
      lighting: lighting.data,
      schedules: schedules.data,
		player: player.data,
    };
  } catch (error) {
    app.error = error;
  } finally {
    app.loading = false;
    elements.refresh.classList.remove("spinning");
    renderPage();
  }
}

function connectEvents() {
  if (app.eventSource) app.eventSource.close();
  app.eventSource = api.events();
  app.eventSource.addEventListener("snapshot", (event) => {
    try {
      const item = JSON.parse(event.data);
      const signature = `${item.scenario}-${item.revision}-${item.observedAt}`;
      if (!app.events.some((existing) => existing.signature === signature)) {
        app.events.unshift({ ...item, signature });
        app.events = app.events.slice(0, 20);
        if (app.route === "diagnostics") renderPage();
      }
    } catch (error) {
      console.warn("无法解析归一化事件", error);
    }
  });
}

function renderPage() {
  if (app.loading && !app.data) {
	elements.content.innerHTML = `<div class="loading-state"><span class="loader"></span><p>正在读取本地配置 API…</p></div>`;
    return;
  }
  if (app.error) {
    elements.content.innerHTML = `<div class="error-state"><strong>无法读取本地配置服务</strong><p>${escapeHTML(app.error.message)}</p><button class="button" data-action="reload" type="button">重新加载</button></div>`;
    return;
  }
  const pages = {
    overview: renderOverview,
    airplay: renderAirPlay,
    network: renderNetwork,
    audio: renderAudio,
    bluetooth: renderBluetooth,
		player: renderPlayer,
    diagnostics: renderDiagnostics,
  };
  elements.content.innerHTML = (pages[app.route] || renderOverview)();
}

function pill(state) {
  const presentation = statePresentation(state);
  return `<span class="state-pill tone-${presentation.tone}" title="${escapeHTML(presentation.detail)}">${escapeHTML(presentation.label)}</span>`;
}

function meta(state) {
  return `<div class="state-meta"><span>来源 ${escapeHTML(state?.source || "unknown")}</span><span>${escapeHTML(formatObservedAt(state?.observedAt))}</span></div>`;
}

function detailRow(label, state, unit = "") {
  return `<div class="detail-row"><dl><dt>${escapeHTML(label)}</dt><dd>${escapeHTML(displayValue(state?.value ?? "unknown", unit))}</dd></dl>${meta(state)}</div>`;
}

function statusCard(title, icon, state, unit = "") {
  return `<article class="status-card"><div class="status-card-head"><span class="card-icon">${icon}</span>${pill(state)}</div><h3>${escapeHTML(title)}</h3><div class="status-main">${escapeHTML(displayValue(state?.value ?? "unknown", unit))}</div></article>`;
}

function sectionHeading(title, description, note = "") {
  return `<div class="section-heading"><div><h2>${escapeHTML(title)}</h2><p>${escapeHTML(description)}</p></div>${note ? `<span class="section-note">${escapeHTML(note)}</span>` : ""}</div>`;
}

function renderOverview() {
  const { device, status, airplay, network, audio } = app.data;
	const real = app.data.environment === "device";
  return `
    <div class="hero-grid">
      <article class="hero-card">
		<span class="hero-label">● ${real ? "真实设备已连接" : "Mock 设备在线"}</span>
        <h2>${escapeHTML(displayValue(device.productFamily.value))}</h2>
		<p>${real ? "状态由设备适配层实时读取，并在返回浏览器前完成脱敏。" : "本地配置契约演示环境。所有数据均为脱敏模拟状态，页面不会读取设备标识或网络标识。"}</p>
        <div class="hero-meta"><span>固件 ${escapeHTML(device.firmware.value)}</span><span>${escapeHTML(device.platform.value)}</span></div>
        <figure class="hero-device">
          <img src="/assets/sanyin-speaker-red.jpg" alt="红色网易三音云音箱实拍">
          <figcaption><a href="https://www.52audio.com/archives/27175.html" target="_blank" rel="noreferrer">实拍来源 · 我爱音频网 ↗</a></figcaption>
        </figure>
      </article>
      <div class="mini-stack">
        <article class="mini-card"><div class="mini-card-head"><p>总体健康</p>${pill(status.overall)}</div><strong>${escapeHTML(displayValue(status.overall.value))}</strong></article>
        <article class="mini-card"><div class="mini-card-head"><p>可用空间</p>${pill(device.storageRemainingMiB)}</div><strong>${escapeHTML(displayValue(device.storageRemainingMiB.value, " MiB"))}</strong></article>
      </div>
    </div>
	${sectionHeading("核心状态", "来自同一套 HTTP API 的归一化观测", real ? "实时设备" : `场景 · ${app.scenario}`)}
    <div class="card-grid">
      ${statusCard("AirPlay", "A", airplay.runtime)}
      ${statusCard("Wi-Fi", "W", network.connection)}
      ${statusCard("系统音量", "♫", audio.systemVolume, "%")}
      ${statusCard("播放器", "▶", status.player)}
    </div>
    ${sectionHeading("核心服务", "服务在线状态不等同于对应设置可安全写入")}
    <section class="panel"><div class="service-list">${Object.entries(status.services).map(([name, state]) => `<div class="service-item"><span class="service-name">${escapeHTML(name)}</span>${pill(state)}</div>`).join("")}</div></section>`;
}

function renderAirPlay() {
  const capability = app.data.capabilityMap["airplay.recover"];
	const autoRecoverCapability = app.data.capabilityMap["airplay.autoRecover"];
  const realAction = actionPresentation(capability);
	const autoRecoverAction = actionPresentation(autoRecoverCapability);
  const simulation = actionPresentation(capability, { simulation: true });
	const real = app.data.environment === "device";
	const autoRecoverEnabled = app.data.airplay.autoRecoverEnabled?.value === true;
  return `
    <section class="panel">
	  <div class="panel-header"><div><h2>运行状态</h2><p>${real ? "实时检查 SPlayer、TCP 5002 和持久恢复服务。" : "当前仅观察 Mock 运行态和端口抽象状态，不调用已有恢复服务。"}</p></div>${pill(app.data.airplay.runtime)}</div>
      <div class="detail-grid">
        ${detailRow("SPlayer", app.data.airplay.runtime)}
        ${detailRow("TCP 5002", app.data.airplay.port)}
        ${detailRow("恢复服务", app.data.airplay.restoreService)}
		${detailRow("自动恢复", app.data.airplay.autoRecoverEnabled)}
        ${detailRow("总体服务", app.data.status.services.splayer)}
      </div>
	  <div class="capability-callout ${realAction.disabled ? "" : "callout-info"}"><strong>${realAction.disabled ? "待验证" : "安全写入"}</strong><span>${escapeHTML(realAction.reason)}</span></div>
      <div class="action-row">
		${real
		  ? `<div><button class="button" data-action="recover-airplay" type="button" ${realAction.disabled ? "disabled" : ""}>恢复 AirPlay</button><small class="button-sublabel">真实设备操作 · 自动验收端口</small></div><div><button class="button secondary" data-action="configure-autorecover" data-enabled="${!autoRecoverEnabled}" type="button" ${autoRecoverAction.disabled ? "disabled" : ""}>${autoRecoverEnabled ? "关闭" : "开启"}自动恢复</button><small class="button-sublabel">持久配置 · 原子写入并回读</small></div>`
		  : `<div><button class="button" data-action="simulate-airplay" type="button">${escapeHTML(simulation.label)}</button><small class="button-sublabel">不会连接或修改设备</small></div><div><button class="button secondary" type="button" disabled title="${escapeHTML(realAction.reason)}">真实恢复</button><small class="button-sublabel">Mock 模式禁用</small></div>`}
      </div>
    </section>`;
}

function renderNetwork() {
  const action = actionPresentation(app.data.capabilityMap["wifi.connection"]);
  return `
    <section class="panel">
	  <div class="panel-header"><div><h2>无线网络</h2><p>显示当前 SSID，但不返回密码、BSSID、MAC 或 IP。切换失败会自动恢复原配置。</p></div>${pill(app.data.network.connection)}</div>
	  <div class="detail-grid">${detailRow("当前 Wi-Fi", app.data.network.currentSSID)}${detailRow("连接状态", app.data.network.connection)}${detailRow("信号等级", app.data.network.signal)}${detailRow("最近切换", app.data.network.lastSwitchResult)}</div>
      <div class="capability-callout"><strong>${escapeHTML(action.label)}</strong><span>${escapeHTML(action.reason)}</span></div>
	  <div class="action-row"><button class="button" data-action="configure-wifi" type="button" ${action.disabled ? "disabled" : ""} title="${escapeHTML(action.reason)}">切换 Wi-Fi</button></div>
    </section>`;
}

function renderAudio() {
  const eq = eqPresentation(app.data.audio.eq);
  const volumeAction = actionPresentation(app.data.capabilityMap["audio.volume"]);
  const eqAction = actionPresentation(app.data.capabilityMap["audio.effect"]);
  return `
    <div class="card-grid three">
      ${statusCard("系统音量", "♫", app.data.audio.systemVolume, "%")}
      ${statusCard("输出静音", "S", app.data.audio.outputMuted)}
      ${statusCard("麦克风", "M", app.data.audio.microphone)}
    </div>
    ${sectionHeading("EQ 分层状态", "业务选中态与硬件应用态不会合并展示")}
    <section class="panel">
      <div class="eq-flow">
        <div class="eq-box"><span>业务选中模式</span><strong>${escapeHTML(eq.selected)}</strong>${pill(app.data.audio.eq.selectedMode)}</div>
        <div class="eq-arrow">→</div>
        <div class="eq-box"><span>硬件已应用模式</span><strong>${escapeHTML(eq.applied)}</strong>${pill(app.data.audio.eq.applyState)}</div>
      </div>
      <div class="capability-callout ${eq.pending ? "" : "callout-info"}"><strong>${eq.pending ? "待应用" : "状态说明"}</strong><span>${escapeHTML(eq.label)}</span></div>
		  <div class="capability-callout"><strong>${escapeHTML(eqAction.label)}</strong><span>${escapeHTML(eqAction.reason)}</span></div>
          <div class="action-row">
            <button class="button" disabled title="${escapeHTML(volumeAction.reason)}">调整音量</button>
			${eqModes.map((item) => `<button class="button secondary" data-action="configure-eq" data-mode="${item.mode}" type="button" ${eqAction.disabled ? "disabled" : ""}>${escapeHTML(item.label)}</button>`).join("")}
          </div>
    </section>`;
}

function renderBluetooth() {
  const enabled = statePresentation(app.data.bluetooth.enabled);
  const action = actionPresentation(app.data.capabilityMap["bluetooth.enabled"]);
  return `
    <section class="panel">
      <div class="panel-header"><div><h2>蓝牙</h2><p>服务在线不代表可可靠读取当前开关；未知状态不会用最近一次操作结果代替。</p></div>${pill(app.data.bluetooth.enabled)}</div>
	  <div class="detail-grid">${detailRow("蓝牙服务", app.data.bluetooth.service)}${detailRow("当前开关", app.data.bluetooth.enabled)}${detailRow("最近验收状态", app.data.bluetooth.lastConfirmedEnabled)}</div>
      <div class="capability-callout"><strong>${escapeHTML(enabled.label)}</strong><span>${escapeHTML(action.reason)}</span></div>
	  <div class="action-row"><button class="button" data-action="configure-bluetooth" data-enabled="true" ${action.disabled ? "disabled" : ""} type="button">开启蓝牙</button><button class="button secondary" data-action="configure-bluetooth" data-enabled="false" ${action.disabled ? "disabled" : ""} type="button">关闭蓝牙</button></div>
    </section>`;
}

function renderPlayer() {
	const player = app.data.player;
	const capability = actionPresentation(app.data.capabilityMap["player.localPlayback"]);
	const transport = String(player.transport?.value || "unknown");
	const position = Number(player.positionSeconds?.value || 0);
	const duration = Number(player.durationSeconds?.value || 0);
	const progress = duration > 0 ? Math.min(100, Math.max(0, (position / duration) * 100)) : 0;
	const active = transport === "playing" || transport === "paused" || transport === "transitioning";
	const hasNext = player.currentIndex >= 0 && player.currentIndex + 1 < player.queue.length;
	const current = player.current;
	const stopTimer = player.stopTimer || { active: false, remainingSeconds: 0 };
	const timerRemaining = Math.max(0, Number(stopTimer.remainingSeconds || 0));
	const queue = player.queue.length ? player.queue.map((item, index) => {
		const isCurrent = index === player.currentIndex;
		const removeDisabled = capability.disabled || (isCurrent && active);
		return `<div class="queue-item ${isCurrent ? "queue-current" : ""}">
			<div class="queue-order">${index + 1}</div>
			<div class="queue-copy"><strong>${escapeHTML(item.title)}</strong><small>${escapeHTML(item.kind === "radio" ? "网络电台" : item.source)}</small></div>
			<div class="queue-actions"><button class="button secondary compact" data-player-action="queue_play" data-item-id="${escapeHTML(item.id)}" type="button" ${capability.disabled ? "disabled" : ""}>播放</button><button class="icon-button compact-icon" data-player-action="queue_remove" data-item-id="${escapeHTML(item.id)}" type="button" aria-label="移除 ${escapeHTML(item.title)}" ${removeDisabled ? "disabled" : ""}>×</button></div>
		</div>`;
	}).join("") : `<div class="empty-state compact-empty">播放队列为空，可从 URL 或网络电台加入。</div>`;
	const stations = player.stations.length ? player.stations.map((station) => `<article class="station-card">
		<span class="station-icon">◉</span><div><strong>${escapeHTML(station.name)}</strong><small>${escapeHTML(station.source)}</small></div>
		<div class="station-actions"><button class="button compact" data-player-action="radio_play" data-item-id="${escapeHTML(station.id)}" type="button" ${capability.disabled ? "disabled" : ""}>播放</button><button class="button secondary compact" data-player-action="radio_queue" data-item-id="${escapeHTML(station.id)}" type="button" ${capability.disabled ? "disabled" : ""}>加入队列</button><button class="icon-button compact-icon" data-player-action="radio_remove" data-item-id="${escapeHTML(station.id)}" type="button" aria-label="删除 ${escapeHTML(station.name)}" ${capability.disabled ? "disabled" : ""}>×</button></div>
	</article>`).join("") : `<div class="empty-state compact-empty">尚未收藏网络电台。</div>`;
	return `
		<section class="panel player-now">
			<div class="panel-header"><div><h2>正在播放</h2><p>音箱通过原厂 KPlayer 自主拉取媒体 URL，关闭当前网页不会中断播放。</p></div>${pill(player.transport)}</div>
			<div class="now-playing-copy"><span class="now-playing-icon">♫</span><div><strong>${escapeHTML(current?.title || "暂无媒体")}</strong><small>${escapeHTML(current?.source || "等待播放 URL 或网络电台")}</small></div></div>
			<div class="player-progress" role="progressbar" aria-valuemin="0" aria-valuemax="${Math.max(0, duration)}" aria-valuenow="${Math.max(0, position)}"><span style="width:${progress.toFixed(2)}%"></span></div>
			<div class="progress-labels"><span>${formatDuration(position)}</span><span>${duration > 0 ? formatDuration(duration) : "直播"}</span></div>
			<div class="player-controls">
				<button class="button secondary" data-player-action="pause" type="button" ${capability.disabled || transport !== "playing" ? "disabled" : ""}>暂停</button>
				<button class="button" data-player-action="resume" type="button" ${capability.disabled || transport !== "paused" ? "disabled" : ""}>恢复</button>
				<button class="button secondary" data-player-action="stop" type="button" ${capability.disabled || !active ? "disabled" : ""}>停止</button>
				<button class="button secondary" data-player-action="next" type="button" ${capability.disabled || !hasNext ? "disabled" : ""}>下一首</button>
			</div>
			<div class="stop-timer">
				<div class="stop-timer-copy"><strong>定时停止</strong><span>${stopTimer.active ? `剩余 ${formatDuration(timerRemaining)}` : "未设置 · 最长 60 分钟"}</span></div>
				<form id="stop-timer-form" class="stop-timer-form">
					<label><span>分钟</span><input class="text-input" name="durationMinutes" type="number" min="1" max="60" step="1" value="30" required></label>
					<button class="button" type="submit" ${capability.disabled ? "disabled" : ""}>设置</button>
					<button class="button secondary" data-player-action="timer_cancel" type="button" ${capability.disabled || !stopTimer.active ? "disabled" : ""}>取消</button>
				</form>
			</div>
			<div class="capability-callout"><strong>${escapeHTML(capability.label)}</strong><span>${escapeHTML(capability.reason)}</span></div>
		</section>
		${sectionHeading("播放 URL", "支持 HTTP/HTTPS 音乐文件和可直接拉流的音频地址")}
		<section class="panel">
			<form id="player-url-form" class="media-form">
				<label><span>标题（可选）</span><input class="text-input" name="title" type="text" maxlength="100" placeholder="例如：客厅歌单"></label>
				<label class="media-url-field"><span>媒体 URL</span><input class="text-input" name="url" type="url" maxlength="2048" required placeholder="https://media.example/music.mp3" autocomplete="off" spellcheck="false"></label>
				<div class="media-form-actions"><button class="button" data-submit-action="play_url" type="submit" ${capability.disabled ? "disabled" : ""}>立即播放</button><button class="button secondary" data-submit-action="queue_add" type="submit" ${capability.disabled ? "disabled" : ""}>加入队列</button></div>
			</form>
		</section>
		${sectionHeading("播放队列", "当前项结束后自动播放下一项", `${player.queue.length} 项`)}
		<section class="panel"><div class="queue-list">${queue}</div><div class="action-row"><button class="button secondary" data-player-action="queue_clear" type="button" ${capability.disabled || player.queue.length === 0 ? "disabled" : ""}>停止并清空队列</button></div></section>
		${sectionHeading("网络电台", "保存可直接播放的 HTTP/HTTPS 电台流地址", `${player.stations.length} 个收藏`)}
		<section class="panel">
			<form id="radio-station-form" class="media-form station-form">
				<label><span>电台名称</span><input class="text-input" name="title" type="text" maxlength="100" required placeholder="例如：爵士电台"></label>
				<label class="media-url-field"><span>电台流 URL</span><input class="text-input" name="url" type="url" maxlength="2048" required placeholder="https://radio.example/live.mp3" autocomplete="off" spellcheck="false"></label>
				<div class="media-form-actions"><button class="button" type="submit" ${capability.disabled ? "disabled" : ""}>收藏电台</button></div>
			</form>
			<div class="station-grid">${stations}</div>
		</section>`;
}

async function refreshPlayer() {
	if (!app.data || app.environment !== "device" || app.playerRefreshing) return;
	app.playerRefreshing = true;
	try {
		const response = await api.player();
		app.data.player = response.data;
		if (app.route === "player") renderPage();
	} catch (error) {
		if (app.route === "player") console.warn("播放器状态刷新失败", error);
	} finally {
		app.playerRefreshing = false;
	}
}

async function runPlayerAction(action, itemId = "") {
	try {
		const response = await api.controlPlayer(action, itemId ? { itemId } : {});
		app.data.player = response.data;
		renderPage();
		toast(action === "timer_cancel" ? "定时停止已取消" : "播放器操作已完成并回读状态");
	} catch (error) {
		toast(error.message);
	}
}

async function submitStopTimer(form) {
	const durationMinutes = Number(form.elements.durationMinutes.value);
	if (!Number.isInteger(durationMinutes) || durationMinutes < 1 || durationMinutes > 60) {
		toast("请输入 1 到 60 分钟的整数");
		return;
	}
	try {
		const response = await api.controlPlayer("timer_set", { durationMinutes });
		app.data.player = response.data;
		renderPage();
		toast(`已设置 ${durationMinutes} 分钟后停止播放`);
	} catch (error) {
		toast(error.message);
	}
}

async function submitPlayerURL(event) {
	const form = event.target;
	const action = event.submitter?.dataset.submitAction || "play_url";
	const title = form.elements.title.value.trim();
	const url = form.elements.url.value.trim();
	if (!url) return;
	try {
		const response = await api.controlPlayer(action, { title, url });
		app.data.player = response.data;
		form.reset();
		renderPage();
		toast(action === "play_url" ? "已开始播放" : "已加入播放队列");
	} catch (error) {
		toast(error.message);
	}
}

async function submitRadioStation(form) {
	const title = form.elements.title.value.trim();
	const url = form.elements.url.value.trim();
	if (!title || !url) return;
	try {
		const response = await api.controlPlayer("radio_add", { title, url });
		app.data.player = response.data;
		form.reset();
		renderPage();
		toast("网络电台已收藏");
	} catch (error) {
		toast(error.message);
	}
}

function renderDiagnostics() {
  const lightingAction = actionPresentation(app.data.capabilityMap["lighting.brightness"]);
  const scheduleAction = actionPresentation(app.data.capabilityMap["microphone.schedule"]);
  const events = app.events.length ? app.events.map((event) => `
    <div class="event-item"><span class="event-mark"></span><div><strong>${escapeHTML(event.type)} · ${escapeHTML(event.changes.join("、"))}</strong><p>来源 ${escapeHTML(event.source)} · 修订 ${event.revision} · 场景 ${escapeHTML(event.scenario)}</p></div><span class="event-time">${escapeHTML(formatObservedAt(event.observedAt))}</span></div>`).join("") : `<div class="empty-state">等待归一化 SSE 事件…</div>`;
  return `
    <section class="panel">
      <div class="panel-header"><div><h2>灯光快照</h2><p>仅展示脱敏诊断快照，写入保持禁用。</p></div>${pill(app.data.lighting.iconEnabled)}</div>
      <div class="detail-grid">${detailRow("图标灯", app.data.lighting.iconEnabled)}${detailRow("亮度快照", app.data.lighting.brightness, "%")}${detailRow("播放模式", app.data.lighting.playMode)}</div>
      <div class="capability-callout"><strong>${escapeHTML(lightingAction.label)}</strong><span>${escapeHTML(lightingAction.reason)}</span></div>
    </section>
    <section class="panel">
      <div class="panel-header"><div><h2>计划任务</h2><p>计划、闹钟和提醒保留云端依赖标识，仅作诊断读取。</p></div><span class="state-pill tone-neutral">云端依赖</span></div>
      <div class="detail-grid">${detailRow("麦克风计划", app.data.schedules.microphoneSchedule)}${detailRow("闹钟", app.data.schedules.alarms)}${detailRow("提醒", app.data.schedules.reminders)}</div>
      <div class="capability-callout"><strong>${escapeHTML(scheduleAction.label)}</strong><span>${escapeHTML(scheduleAction.reason)}</span></div>
    </section>
    <section class="panel">
      <div class="panel-header"><div><h2>事件与诊断</h2><p>只显示归一化事件、来源和更新时间，不包含原始设备日志。</p></div><span class="state-pill tone-ok">SSE</span></div>
      <div class="event-list">${events}</div>
    </section>`;
}

function openOperationConfirmation(simulation) {
  elements.dialogContent.innerHTML = `
	<span class="dialog-kicker">${simulation ? "MOCK OPERATION" : "DEVICE OPERATION"}</span>
	<h2>${simulation ? "演示 AirPlay 恢复流程" : "恢复真实设备的 AirPlay"}</h2>
	<p>${simulation ? "仅演示“确认 → 执行 → 验收 → 成功/回滚”交互，不会连接或修改音箱。" : "服务将发送已验证的原生启动命令，并等待 TCP 5002 开始监听。当前已在监听时不会重复写入。"}</p>
	<div class="capability-callout ${simulation ? "" : "callout-info"}"><strong>${simulation ? "模拟操作" : "真实设备操作"}</strong><span>${simulation ? "响应会明确包含 simulation=true。" : "仅恢复或确认 AirPlay 可用，不修改 Wi-Fi、厂商数据库或其他配置。"}</span></div>
	<div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-operation" data-simulation="${simulation}" class="button" type="button">${simulation ? "确认演示" : "确认恢复"}</button></div>`;
  elements.dialog.showModal();
}

function openAutoRecoverConfirmation(enabled) {
	const action = enabled ? "开启" : "关闭";
	elements.dialogContent.innerHTML = `
	  <span class="dialog-kicker">DEVICE CONFIGURATION</span>
	  <h2>${action} AirPlay 自动恢复</h2>
	  <p>${enabled ? "开启后，守护服务会在 Wi-Fi 和 SPlayer 就绪但 TCP 5002 未监听时自动恢复 AirPlay。" : "关闭后，守护服务仍保持运行，但不会再自动发送 AirPlay 启动命令；当前已经建立的监听不会被强制停止。"}</p>
	  <div class="capability-callout callout-info"><strong>持久配置</strong><span>只写入自有配置文件，不修改厂商数据库；写入后会立即回读验收。</span></div>
	  <div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-autorecover" data-enabled="${enabled}" class="button" type="button">确认${action}</button></div>`;
	elements.dialog.showModal();
}

async function runAirPlayOperation(simulation) {
	elements.dialogContent.innerHTML = `<span class="dialog-kicker">${simulation ? "MOCK OPERATION" : "DEVICE OPERATION"}</span><h2>${simulation ? "正在执行模拟流程" : "正在恢复 AirPlay"}</h2><div class="loading-state operation-loading"><span class="loader"></span><p>等待端口状态验收…</p></div>`;
  try {
	const response = simulation ? await api.simulateAirplayRecovery() : await api.recoverAirplay();
    const operation = response.data;
    const presentation = operationPresentation(operation);
    elements.dialogContent.innerHTML = `
	  <span class="dialog-kicker">${operation.simulation ? "SIMULATION" : "DEVICE"} · ${escapeHTML(operation.operationId)}</span>
      <h2>${escapeHTML(presentation.title)}</h2>
      <p>${escapeHTML(operation.message)}</p>
      <div class="operation-timeline">${presentation.steps.map((step) => `<div class="operation-step ${step.className}"><span class="step-dot">✓</span><span>${escapeHTML(step.label)}</span></div>`).join("")}</div>
	  <div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
	if (!simulation) await loadData();
  } catch (error) {
	elements.dialogContent.innerHTML = `<span class="dialog-kicker">REQUEST ERROR</span><h2>操作请求失败</h2><p>${escapeHTML(error.message)}</p><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
  }
}

async function runAutoRecoverConfiguration(enabled) {
	elements.dialogContent.innerHTML = `<span class="dialog-kicker">DEVICE CONFIGURATION</span><h2>正在写入配置</h2><div class="loading-state operation-loading"><span class="loader"></span><p>等待配置回读验收…</p></div>`;
	try {
	  const response = await api.setAirplayAutoRecover(enabled);
	  const operation = response.data;
	  const presentation = operationPresentation(operation);
	  elements.dialogContent.innerHTML = `
		<span class="dialog-kicker">DEVICE · ${escapeHTML(operation.operationId)}</span>
		<h2>${escapeHTML(presentation.title)}</h2>
		<p>${escapeHTML(operation.message)}</p>
		<div class="operation-timeline">${presentation.steps.map((step) => `<div class="operation-step ${step.className}"><span class="step-dot">✓</span><span>${escapeHTML(step.label)}</span></div>`).join("")}</div>
		<div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
	  await loadData();
	} catch (error) {
	  elements.dialogContent.innerHTML = `<span class="dialog-kicker">REQUEST ERROR</span><h2>配置写入失败</h2><p>${escapeHTML(error.message)}</p><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
	}
}

function openBluetoothConfirmation(enabled) {
	const action = enabled ? "开启" : "关闭";
	elements.dialogContent.innerHTML = `
	  <span class="dialog-kicker">EXPERIMENTAL DEVICE CONFIGURATION</span>
	  <h2>${action}蓝牙</h2>
	  <p>将发送已完成独立回放的蓝牙${action}命令，并等待设备服务返回对应成功事件。当前固件仍没有无副作用状态查询，因此页面会把“当前开关”保持为未知，只单独记录最近一次验收结果。</p>
	  <div class="capability-callout"><strong>实验能力</strong><span>连接中的设备行为、服务异常回滚和重启状态仍待完整验证。</span></div>
	  <div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-bluetooth" data-enabled="${enabled}" class="button" type="button">确认${action}</button></div>`;
	elements.dialog.showModal();
}

async function runBluetoothConfiguration(enabled) {
	elements.dialogContent.innerHTML = `<span class="dialog-kicker">EXPERIMENTAL DEVICE CONFIGURATION</span><h2>正在${enabled ? "开启" : "关闭"}蓝牙</h2><div class="loading-state operation-loading"><span class="loader"></span><p>等待蓝牙服务成功事件…</p></div>`;
	try {
	  const response = await api.setBluetooth(enabled);
	  const operation = response.data;
	  const presentation = operationPresentation(operation);
	  elements.dialogContent.innerHTML = `
		<span class="dialog-kicker">DEVICE · ${escapeHTML(operation.operationId)}</span>
		<h2>${escapeHTML(presentation.title)}</h2>
		<p>${escapeHTML(operation.message)}</p>
		<div class="operation-timeline">${presentation.steps.map((step) => `<div class="operation-step ${step.className}"><span class="step-dot">✓</span><span>${escapeHTML(step.label)}</span></div>`).join("")}</div>
		<div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
	  await loadData();
	} catch (error) {
	  elements.dialogContent.innerHTML = `<span class="dialog-kicker">REQUEST ERROR</span><h2>蓝牙操作失败</h2><p>${escapeHTML(error.message)}</p><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
	}
}

function openEQConfirmation(mode) {
	const item = eqModes.find((candidate) => candidate.mode === mode);
	if (!item) return;
	elements.dialogContent.innerHTML = `
	  <span class="dialog-kicker">EXPERIMENTAL DEVICE CONFIGURATION</span>
	  <h2>切换 EQ 至${escapeHTML(item.label)}</h2>
	  <p>配置服务只会从固定的 0..6 模式中选择，通过音箱内部 loopback 接口下发，并等待 commonStatus 中对应的选中态事件。硬件是否立即加载会根据当前播放场景独立回读。</p>
	  <div class="capability-callout"><strong>可能等待本地播放</strong><span>空闲或 AirPlay 场景通常只更新业务选中态；网易本地播放器开始播放后才可能把效果加载到 ADAU1761。</span></div>
	  <div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-eq" data-mode="${item.mode}" class="button" type="button">确认切换</button></div>`;
	elements.dialog.showModal();
}

async function runEQConfiguration(mode) {
	const item = eqModes.find((candidate) => candidate.mode === mode);
	if (!item) return;
	elements.dialogContent.innerHTML = `<span class="dialog-kicker">EXPERIMENTAL DEVICE CONFIGURATION</span><h2>正在切换至${escapeHTML(item.label)}</h2><div class="loading-state operation-loading"><span class="loader"></span><p>等待业务选中态事件与硬件文件回读…</p></div>`;
	try {
	  const response = await api.setEQ(mode);
	  const operation = response.data;
	  const presentation = operationPresentation(operation);
	  elements.dialogContent.innerHTML = `
		<span class="dialog-kicker">DEVICE · ${escapeHTML(operation.operationId)}</span>
		<h2>${escapeHTML(presentation.title)}</h2>
		<p>${escapeHTML(operation.message)}</p>
		<div class="operation-timeline">${presentation.steps.map((step) => `<div class="operation-step ${step.className}"><span class="step-dot">✓</span><span>${escapeHTML(step.label)}</span></div>`).join("")}</div>
		<div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
	  await loadData();
	} catch (error) {
	  elements.dialogContent.innerHTML = `<span class="dialog-kicker">REQUEST ERROR</span><h2>EQ 操作失败</h2><p>${escapeHTML(error.message)}</p><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
	}
}

function openWiFiConfiguration() {
	const currentSSID = app.data.network.currentSSID?.value === "unknown" ? "" : app.data.network.currentSSID?.value || "";
	elements.dialogContent.innerHTML = `
	  <span class="dialog-kicker">NETWORK TRANSACTION</span>
	  <h2>切换 Wi-Fi</h2>
	  <p>服务会先备份当前配置，再连接目标网络。45 秒内未同时确认目标 SSID、IPv4、默认路由和网关可达时，将自动恢复原配置并重新联网。</p>
	  <label class="field-label" for="wifi-ssid">Wi-Fi 名称（SSID）</label>
	  <input class="text-input" id="wifi-ssid" type="text" maxlength="32" value="${escapeHTML(currentSSID)}" autocomplete="off" spellcheck="false">
	  <label class="field-label" for="wifi-password">Wi-Fi 密码</label>
	  <input class="text-input" id="wifi-password" type="password" maxlength="64" autocomplete="new-password" placeholder="开放网络请留空">
	  <div class="capability-callout"><strong>连接可能暂时中断</strong><span>请只在可信局域网中操作。页面请求断开不代表事务停止；音箱端脚本会继续验收或自动回退。密码只写入权限 0600 的临时文件，事务结束后删除。</span></div>
	  <div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-wifi" class="button" type="button">确认切换</button></div>`;
	elements.dialog.showModal();
}

async function runWiFiConfiguration() {
	const ssidInput = elements.dialogContent.querySelector("#wifi-ssid");
	const passwordInput = elements.dialogContent.querySelector("#wifi-password");
	const ssid = ssidInput?.value || "";
	const password = passwordInput?.value || "";
	if (!ssid) {
		toast("请输入 Wi-Fi 名称");
		ssidInput?.focus();
		return;
	}
	passwordInput.value = "";
	elements.dialogContent.innerHTML = `<span class="dialog-kicker">NETWORK TRANSACTION</span><h2>正在切换 Wi-Fi</h2><div class="loading-state operation-loading"><span class="loader"></span><p>等待目标网络验收；失败时最多再等待 45 秒恢复原网络…</p></div>`;
	try {
	  const response = await api.switchWiFi(ssid, password);
	  const operation = response.data;
	  const presentation = operationPresentation(operation);
	  elements.dialogContent.innerHTML = `
		<span class="dialog-kicker">DEVICE · ${escapeHTML(operation.operationId)}</span>
		<h2>${escapeHTML(presentation.title)}</h2>
		<p>${escapeHTML(operation.message)}</p>
		<div class="operation-timeline">${presentation.steps.map((step) => `<div class="operation-step ${step.className}"><span class="step-dot">✓</span><span>${escapeHTML(step.label)}</span></div>`).join("")}</div>
		<div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
	  await loadData();
	} catch (error) {
	  elements.dialogContent.innerHTML = `<span class="dialog-kicker">CONNECTION INTERRUPTED</span><h2>与原地址的连接已中断</h2><p>Wi-Fi 事务仍会在音箱上继续运行。请等待最多 90 秒：成功时使用音箱在新网络取得的地址；失败时重新打开原地址，音箱应已自动回退。</p><div class="capability-callout"><strong>不会因页面断开而停止</strong><span>如 90 秒后两个网络都无法访问，请通过 USB ADB 运行 sanyinctl config-status。</span></div><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
	}
}

function toast(message) {
  const item = document.createElement("div");
  item.className = "toast";
  item.textContent = message;
  elements.toast.appendChild(item);
  setTimeout(() => item.remove(), 3000);
}

initialize();
