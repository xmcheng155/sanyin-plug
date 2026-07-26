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
	toneRank,
	validateMediaURL,
	validateSSID,
	validateTimerMinutes,
} from "./model.js";

const navItems = [
  { id: "overview", label: "总览", icon: "home", title: "设备总览" },
  { id: "airplay", label: "AirPlay", icon: "airplay", title: "AirPlay" },
	{ id: "player", label: "播放", icon: "play", title: "本地播放" },
  { id: "network", label: "网络", icon: "wifi", title: "网络状态" },
  { id: "audio", label: "音频", icon: "audio", title: "音频与 EQ" },
  { id: "bluetooth", label: "蓝牙", icon: "bluetooth", title: "蓝牙" },
  { id: "diagnostics", label: "诊断", icon: "diagnostics", title: "灯光、计划与诊断" },
	{ id: "system", label: "版本", icon: "update", title: "版本与更新" },
];

const mobilePrimaryOrder = ["overview", "player", "airplay"];
const mobilePrimaryRoutes = new Set(mobilePrimaryOrder);

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
	playerVolumeUpdating: false,
	playerVolumePending: null,
	playerVolumeDesired: null,
	playerVolumeTimer: null,
	playerVolumeRevision: 0,
	pendingUpdateFile: null,
	pendingPlayerAction: null,
	mobileMoreOpen: false,
	lastUpdatedAt: null,
	refreshError: null,
	offline: !navigator.onLine,
	operationBusy: false,
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
	deviceContext: document.querySelector("#device-context"),
	refreshStatus: document.querySelector("#refresh-status"),
	connectionBanner: document.querySelector("#connection-banner"),
	connectionDetail: document.querySelector("#connection-detail"),
};

function iconMarkup(name, className = "ui-icon") {
	const paths = {
		home: '<path d="M3 10.8 12 3l9 7.8"/><path d="M5.5 9.5V21h13V9.5M9.5 21v-7h5v7"/>',
		airplay: '<path d="M5 17.5H3.8A1.8 1.8 0 0 1 2 15.7V5.8A1.8 1.8 0 0 1 3.8 4h16.4A1.8 1.8 0 0 1 22 5.8v9.9a1.8 1.8 0 0 1-1.8 1.8H19"/><path d="m12 13-5 7h10l-5-7Z"/>',
		play: '<path d="m8 5 11 7-11 7V5Z"/>',
		wifi: '<path d="M3.5 9.5a13 13 0 0 1 17 0M6.5 13a8.7 8.7 0 0 1 11 0M9.5 16.5a4.3 4.3 0 0 1 5 0"/><circle cx="12" cy="20" r=".8"/>',
		audio: '<path d="M9 18V6l10-2v12"/><circle cx="6" cy="18" r="3"/><circle cx="16" cy="16" r="3"/>',
		bluetooth: '<path d="M7 7.5 17 16l-5 4V4l5 4-10 8.5"/>',
		diagnostics: '<path d="M4 19V9M10 19V5M16 19v-7M22 19V3"/>',
		update: '<path d="M12 3v12m0-12L7 8m5-5 5 5"/><path d="M5 14v6h14v-6"/>',
		more: '<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
		chevron: '<path d="m9 5 7 7-7 7"/>',
	};
	return `<svg class="${className}" viewBox="0 0 24 24" aria-hidden="true">${paths[name] || paths.diagnostics}</svg>`;
}

function navMarkup(item, { menu = false } = {}) {
	const active = app.route === item.id;
  return `<button class="nav-link ${active ? "active" : ""} ${menu ? "menu-nav-link" : ""}" data-route="${item.id}" type="button" ${active ? 'aria-current="page"' : ""}><span class="nav-icon">${iconMarkup(item.icon)}</span><span>${item.label}</span>${menu ? iconMarkup("chevron", "nav-chevron") : ""}</button>`;
}

function renderNavigation() {
	const secondaryActive = !mobilePrimaryRoutes.has(app.route);
  elements.desktopNav.innerHTML = navItems.map((item) => navMarkup(item)).join("");
	const primaryMarkup = mobilePrimaryOrder.map((id) => navItems.find((item) => item.id === id)).map((item) => navMarkup(item)).join("");
	const moreMarkup = navItems.filter((item) => !mobilePrimaryRoutes.has(item.id)).map((item) => navMarkup(item, { menu: true })).join("");
  elements.mobileNav.innerHTML = `${primaryMarkup}
		<button class="nav-link mobile-more-trigger ${secondaryActive ? "active" : ""}" data-action="toggle-mobile-more" type="button" aria-expanded="${app.mobileMoreOpen}" aria-controls="mobile-more-menu"><span class="nav-icon">${iconMarkup("more")}</span><span>更多</span></button>
		<div id="mobile-more-menu" class="mobile-more-menu" ${app.mobileMoreOpen ? "" : "hidden"}>
			<div class="mobile-more-header"><strong>更多配置</strong><button class="dialog-close compact-close" data-action="close-mobile-more" type="button" aria-label="关闭更多配置">×</button></div>
			<div class="mobile-more-list">${moreMarkup}</div>
		</div>`;
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
		app.mobileMoreOpen = false;
      location.hash = app.route;
      renderNavigation();
      renderPage();
      window.scrollTo({ top: 0, behavior: "smooth" });
		window.setTimeout(() => elements.title.focus({ preventScroll: true }), 0);
    }
	if (event.target.closest("[data-action='toggle-mobile-more']")) {
		app.mobileMoreOpen = !app.mobileMoreOpen;
		renderNavigation();
		return;
	}
	if (event.target.closest("[data-action='close-mobile-more']")) {
		app.mobileMoreOpen = false;
		renderNavigation();
		return;
	}
	if (app.mobileMoreOpen && !event.target.closest("#mobile-nav")) {
		app.mobileMoreOpen = false;
		renderNavigation();
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
	if (event.target.closest("#confirm-system-update")) runSystemUpdate();
		const playerButton = event.target.closest("[data-player-action]");
		if (playerButton) {
			const action = playerButton.dataset.playerAction;
			const itemId = playerButton.dataset.itemId || "";
			if (["queue_clear", "radio_remove"].includes(action)) openPlayerActionConfirmation(action, itemId);
			else runPlayerAction(action, itemId, playerButton);
		}
		if (event.target.closest("#confirm-player-action")) confirmPlayerAction();
		const timerPreset = event.target.closest("[data-timer-minutes]");
		if (timerPreset) applyTimerPreset(timerPreset);
		const urlUtility = event.target.closest("[data-url-action]");
		if (urlUtility) handleURLUtility(urlUtility);
		const passwordToggle = event.target.closest("[data-action='toggle-password']");
		if (passwordToggle) togglePasswordVisibility(passwordToggle);
		if (event.target.closest("[data-action='retry-load']")) loadData();
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
		if (event.target.id === "system-update-form") {
			event.preventDefault();
			openSystemUpdateConfirmation(event.target);
		}
	});

	document.addEventListener("input", (event) => {
		clearFieldError(event.target);
		if (event.target.id === "player-volume-range") queuePlayerVolume(event.target);
	});

	document.addEventListener("change", (event) => {
		clearFieldError(event.target);
		if (event.target.id === "player-volume-range") queuePlayerVolume(event.target, { immediate: true });
		if (event.target.matches("[data-media-url]")) validateMediaURLInput(event.target, { allowEmpty: true });
	});

	document.addEventListener("focusout", (event) => {
		if (event.target.matches("[data-media-url]")) validateMediaURLInput(event.target, { allowEmpty: true });
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
	document.addEventListener("keydown", (event) => {
		if (event.key === "Escape" && app.mobileMoreOpen) {
			app.mobileMoreOpen = false;
			renderNavigation();
		}
	});
	elements.dialog.addEventListener("cancel", (event) => {
		if (app.operationBusy) event.preventDefault();
	});
	elements.dialog.addEventListener("close", () => {
		if (!app.operationBusy) {
			app.pendingPlayerAction = null;
			app.pendingUpdateFile = null;
		}
	});
	window.addEventListener("online", () => {
		app.offline = false;
		renderConnectionState();
		loadData();
	});
	window.addEventListener("offline", () => {
		app.offline = true;
		renderConnectionState();
	});
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
	elements.banner.hidden = real;
	if (!real) elements.banner.innerHTML = `<span class="banner-icon">M</span><span><strong>模拟数据环境</strong><small>所有状态和操作均为本机 Mock，不会连接或修改音箱。</small></span>`;
	renderDeviceContext();
}

function formatSyncTime(date) {
	if (!date) return "尚未同步";
	return `已同步 ${new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(date)}`;
}

function renderDeviceContext() {
	if (!app.data) {
		elements.deviceContext.setAttribute("aria-busy", "true");
		return;
	}
	const real = app.data.environment === "device";
	const family = displayValue(app.data.device.productFamily?.value || "unknown");
	const ssid = displayValue(app.data.network.currentSSID?.value || "unknown");
	const endpoint = location.host || "本地服务";
	elements.deviceContext.setAttribute("aria-busy", "false");
	elements.deviceContext.innerHTML = `
		<span class="context-indicator ${real ? "device" : "mock"}"></span>
		<div class="context-primary"><strong>${escapeHTML(family)}</strong><small>${real ? "真实设备" : "模拟环境"}</small></div>
		<div class="context-item"><span>访问地址</span><strong>${escapeHTML(endpoint)}</strong></div>
		<div class="context-item"><span>当前网络</span><strong>${escapeHTML(ssid)}</strong></div>
		<div class="context-item"><span>固件版本</span><strong>${escapeHTML(displayValue(app.data.device.firmware?.value || "unknown"))}</strong></div>`;
}

function updateRefreshStatus() {
	if (app.loading) elements.refreshStatus.textContent = "正在同步…";
	else if (app.refreshError || app.offline) elements.refreshStatus.textContent = "同步失败";
	else elements.refreshStatus.textContent = formatSyncTime(app.lastUpdatedAt);
	elements.refresh.disabled = app.loading;
	elements.content.setAttribute("aria-busy", String(app.loading));
}

function renderConnectionState() {
	const hasIssue = app.offline || Boolean(app.refreshError);
	elements.connectionBanner.hidden = !hasIssue;
	if (hasIssue) {
		elements.connectionDetail.textContent = app.offline
			? "浏览器当前离线，页面正在保留最近一次设备状态。"
			: `${app.refreshError.message}；已保留最近一次设备状态。`;
	}
}

async function loadData() {
  app.loading = true;
	app.refreshError = null;
	if (!app.data) app.error = null;
  elements.refresh.classList.add("spinning");
	updateRefreshStatus();
  renderPage();
  try {
    const [capabilities, device, status, airplay, network, audio, bluetooth, lighting, schedules, player, system] = await Promise.all([
      api.capabilities(), api.device(), api.status(), api.airplay(), api.network(), api.audio(), api.bluetooth(), api.lighting(), api.schedules(), api.player(), api.system(),
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
			system: system.data,
    };
		app.environment = status.environment;
		app.error = null;
		app.lastUpdatedAt = new Date();
  } catch (error) {
		if (app.data) app.refreshError = error;
		else app.error = error;
  } finally {
    app.loading = false;
    elements.refresh.classList.remove("spinning");
		updateRefreshStatus();
		renderDeviceContext();
		renderConnectionState();
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
	elements.content.innerHTML = `<div class="skeleton-layout" aria-label="正在读取本地配置 API">
		<div class="skeleton skeleton-hero"></div>
		<div class="skeleton-grid">${Array.from({ length: 4 }, () => '<div class="skeleton skeleton-card"></div>').join("")}</div>
		<p>正在读取本地配置 API…</p>
	</div>`;
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
		system: renderSystem,
  };
  elements.content.innerHTML = (pages[app.route] || renderOverview)();
}

function pill(state) {
  const presentation = statePresentation(state);
	const symbols = { ok: "✓", warning: "!", danger: "×", unknown: "?", neutral: "i" };
  return `<span class="state-pill tone-${presentation.tone}" title="${escapeHTML(presentation.detail)}" aria-label="${escapeHTML(presentation.label)}：${escapeHTML(presentation.detail)}"><span class="state-symbol" aria-hidden="true">${symbols[presentation.tone] || "?"}</span>${escapeHTML(presentation.label)}</span>`;
}

function meta(state) {
  return `<div class="state-meta"><span>来源 ${escapeHTML(state?.source || "unknown")}</span><span>${escapeHTML(formatObservedAt(state?.observedAt))}</span></div>`;
}

function detailRow(label, state, unit = "") {
  return `<div class="detail-row"><dl><dt>${escapeHTML(label)}</dt><dd>${escapeHTML(displayValue(state?.value ?? "unknown", unit))}</dd></dl>${meta(state)}</div>`;
}

function statusCard(title, icon, state, unit = "", { route = "" } = {}) {
	const presentation = statePresentation(state);
	const body = `<div class="status-card-head"><span class="card-icon">${iconMarkup(icon)}</span>${pill(state)}</div><h3>${escapeHTML(title)}</h3><div class="status-main">${escapeHTML(displayValue(state?.value ?? "unknown", unit))}</div>${route ? '<span class="status-card-hint">查看配置 ' + iconMarkup("chevron", "status-card-chevron") + '</span>' : ""}`;
	return `<article class="status-card tone-card-${presentation.tone} ${route ? "is-actionable" : ""}">${route ? `<button class="status-card-action" data-route="${route}" type="button" aria-label="查看${escapeHTML(title)}配置">${body}</button>` : body}</article>`;
}

function sectionHeading(title, description, note = "") {
  return `<div class="section-heading"><div><h2>${escapeHTML(title)}</h2><p>${escapeHTML(description)}</p></div>${note ? `<span class="section-note">${escapeHTML(note)}</span>` : ""}</div>`;
}

function setFieldError(input, message) {
	if (!input) return;
	const errorId = input.getAttribute("aria-describedby");
	const error = errorId ? document.getElementById(errorId) : null;
	input.classList.toggle("is-invalid", Boolean(message));
	input.setAttribute("aria-invalid", String(Boolean(message)));
	if (error) error.textContent = message;
}

function clearFieldError(input) {
	if (!(input instanceof HTMLInputElement)) return;
	if (input.getAttribute("aria-invalid") === "true") setFieldError(input, "");
	const formError = input.closest("form")?.querySelector(".form-error");
	if (formError) formError.textContent = "";
}

function setFormError(form, message) {
	const error = form?.querySelector(".form-error");
	if (error) error.textContent = message;
	else toast(message, "error");
}

function setFormBusy(form, busy, busyLabel = "处理中…") {
	if (!form) return;
	form.setAttribute("aria-busy", String(busy));
	form.querySelectorAll("button").forEach((button) => {
		if (busy) {
			button.dataset.wasDisabled = String(button.disabled);
			button.dataset.idleLabel = button.textContent;
			button.disabled = true;
			if (button.type === "submit") button.textContent = busyLabel;
		} else {
			button.disabled = button.dataset.wasDisabled === "true";
			if (button.dataset.idleLabel) button.textContent = button.dataset.idleLabel;
			delete button.dataset.wasDisabled;
			delete button.dataset.idleLabel;
		}
	});
}

function validateMediaURLInput(input, { allowEmpty = false } = {}) {
	if (!input) return false;
	const url = input.value.trim();
	if (url !== input.value) input.value = url;
	if (!url && allowEmpty) {
		setFieldError(input, "");
		return true;
	}
	const message = validateMediaURL(url);
	setFieldError(input, message);
	return !message;
}

async function handleURLUtility(button) {
	const form = button.closest("form");
	const input = form?.querySelector("[data-media-url]");
	if (!input) return;
	if (button.dataset.urlAction === "clear") {
		input.value = "";
		setFieldError(input, "");
		setFormError(form, "");
		input.focus();
		return;
	}

	let pasted = "";
	try {
		if (!window.isSecureContext || !navigator.clipboard?.readText) throw new Error("clipboard_unavailable");
		pasted = await navigator.clipboard.readText();
	} catch {
		const manual = window.prompt("浏览器未允许直接读取剪贴板，请在此粘贴媒体 URL：", input.value);
		if (manual === null) {
			input.focus();
			return;
		}
		pasted = manual;
	}
	input.value = pasted.trim();
	validateMediaURLInput(input);
	input.focus();
}

function applyTimerPreset(button) {
	const form = button.closest("form");
	const input = form?.elements.durationMinutes;
	if (!form || !input) return;
	input.value = button.dataset.timerMinutes;
	setFieldError(input, "");
	form.requestSubmit();
}

function togglePasswordVisibility(button) {
	const inputId = button.dataset.target;
	const input = inputId ? document.getElementById(inputId) : null;
	if (!input) return;
	const showing = input.type === "text";
	input.type = showing ? "password" : "text";
	button.setAttribute("aria-pressed", String(!showing));
	button.textContent = showing ? "显示" : "隐藏";
	input.focus();
}

function setDialogBusy(busy) {
	app.operationBusy = busy;
	elements.dialog.classList.toggle("is-busy", busy);
	elements.dialog.querySelectorAll(".dialog-close, [value='cancel']").forEach((button) => { button.disabled = busy; });
}

function showOperationLoading(kicker, title, description, steps = []) {
	elements.dialogContent.innerHTML = `<span class="dialog-kicker">${escapeHTML(kicker)}</span><h2>${escapeHTML(title)}</h2><p>${escapeHTML(description)}</p>
		<div class="operation-timeline operation-progress">${steps.map((step, index) => `<div class="operation-step ${index === 0 ? "current" : "pending"}"><span class="step-dot">${index + 1}</span><span>${escapeHTML(step)}</span></div>`).join("")}</div>`;
	setDialogBusy(true);
}

function finishDialogOperation() {
	setDialogBusy(false);
}

function operationStepsMarkup(steps) {
	return steps.map((step) => {
		const symbol = step.className === "failed" ? "×" : step.className === "rollback" ? "↶" : "✓";
		return `<div class="operation-step ${step.className}"><span class="step-dot" aria-hidden="true">${symbol}</span><span>${escapeHTML(step.label)}</span></div>`;
	}).join("");
}

function renderOverview() {
  const { device, status, airplay, network, audio } = app.data;
	const real = app.data.environment === "device";
	const overviewItems = [
		{ title: "AirPlay", icon: "airplay", state: airplay.runtime, route: "airplay" },
		{ title: "Wi-Fi", icon: "wifi", state: network.connection, route: "network" },
		{ title: "系统音量", icon: "audio", state: audio.systemVolume, unit: "%", route: "audio" },
		{ title: "播放器", icon: "play", state: status.player, route: "player" },
	].map((item) => ({ ...item, presentation: statePresentation(item.state) }))
		.sort((a, b) => toneRank(a.presentation.tone) - toneRank(b.presentation.tone));
	const serviceItems = Object.entries(status.services).map(([name, state]) => ({ name, state, presentation: statePresentation(state) }))
		.sort((a, b) => toneRank(a.presentation.tone) - toneRank(b.presentation.tone));
	const attention = overviewItems.filter((item) => ["danger", "warning", "unknown"].includes(item.presentation.tone));
	const summaryTone = attention.some((item) => item.presentation.tone === "danger") ? "danger" : attention.length ? "warning" : "ok";
  return `
	<section class="health-summary tone-summary-${summaryTone}" aria-label="设备健康摘要">
		<div class="health-summary-icon" aria-hidden="true">${summaryTone === "ok" ? "✓" : "!"}</div>
		<div><strong>${attention.length ? `${attention.length} 项状态需要关注` : "核心状态全部正常"}</strong><small>${attention.length ? `${attention.map((item) => item.title).join("、")} 已按风险优先排列` : "AirPlay、网络、音频与播放器均已取得可靠观测"}</small></div>
		${attention.length ? `<button class="button secondary compact" data-route="${attention[0].route}" type="button">查看首项</button>` : `<span class="health-summary-time">${escapeHTML(formatSyncTime(app.lastUpdatedAt))}</span>`}
	</section>
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
		${overviewItems.map((item) => statusCard(item.title, item.icon, item.state, item.unit || "", { route: item.route })).join("")}
    </div>
    ${sectionHeading("核心服务", "服务在线状态不等同于对应设置可安全写入")}
	<section class="panel panel-flat"><div class="service-list">${serviceItems.map(({ name, state }) => `<div class="service-item"><span class="service-name">${escapeHTML(name)}</span>${pill(state)}</div>`).join("")}</div></section>`;
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
	  <div class="action-row"><button class="button warning" data-action="configure-wifi" type="button" ${action.disabled ? "disabled" : ""} title="${escapeHTML(action.reason)}">切换 Wi-Fi</button></div>
    </section>`;
}

function renderAudio() {
  const eq = eqPresentation(app.data.audio.eq);
  const volumeAction = actionPresentation(app.data.capabilityMap["audio.volume"]);
  const eqAction = actionPresentation(app.data.capabilityMap["audio.effect"]);
  return `
    <div class="card-grid three">
      ${statusCard("系统音量", "audio", app.data.audio.systemVolume, "%")}
      ${statusCard("输出静音", "audio", app.data.audio.outputMuted)}
      ${statusCard("麦克风", "diagnostics", app.data.audio.microphone)}
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
            <button class="button secondary" disabled title="${escapeHTML(volumeAction.reason)}">调整音量</button>
			${eqModes.map((item) => `<button class="button secondary ${eq.selected === item.label ? "selected" : ""}" data-action="configure-eq" data-mode="${item.mode}" type="button" aria-pressed="${eq.selected === item.label}" ${eqAction.disabled ? "disabled" : ""}>${escapeHTML(item.label)}</button>`).join("")}
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
	  <div class="action-row"><button class="button" data-action="configure-bluetooth" data-enabled="true" ${action.disabled ? "disabled" : ""} type="button">开启蓝牙</button><button class="button warning" data-action="configure-bluetooth" data-enabled="false" ${action.disabled ? "disabled" : ""} type="button">关闭蓝牙</button></div>
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
	const rawVolume = Number(player.volume?.value);
	const volumeKnown = Number.isFinite(rawVolume) && rawVolume >= 0 && rawVolume <= 100;
	const desiredVolume = Number(app.playerVolumeDesired);
	const hasDesiredVolume = app.playerVolumeDesired !== null && Number.isInteger(desiredVolume) && desiredVolume >= 0 && desiredVolume <= 100;
	const volume = hasDesiredVolume ? desiredVolume : (volumeKnown ? Math.round(rawVolume) : 0);
	const volumeAdjustable = transport === "playing";
	const stopTimer = player.stopTimer || { active: false, remainingSeconds: 0 };
	const timerRemaining = Math.max(0, Number(stopTimer.remainingSeconds || 0));
	const compatibilitySummary = capability.disabled ? "当前设备暂不可用" : "主要播放路径已完成实机验收";
	const queue = player.queue.length ? player.queue.map((item, index) => {
		const isCurrent = index === player.currentIndex;
		const removeDisabled = capability.disabled || (isCurrent && active);
		return `<div class="queue-item ${isCurrent ? "queue-current" : ""}">
			<div class="queue-order">${index + 1}</div>
			<div class="queue-copy"><strong>${escapeHTML(item.title)}</strong><small>${escapeHTML(item.kind === "radio" ? "网络电台" : item.source)}</small></div>
			<div class="queue-actions"><button class="button secondary compact" data-player-action="queue_play" data-item-id="${escapeHTML(item.id)}" type="button" ${capability.disabled ? "disabled" : ""}>播放</button><button class="icon-button compact-icon danger-icon" data-player-action="queue_remove" data-item-id="${escapeHTML(item.id)}" type="button" aria-label="移除 ${escapeHTML(item.title)}" ${removeDisabled ? "disabled" : ""}>×</button></div>
		</div>`;
	}).join("") : `<div class="empty-state compact-empty">播放队列为空，可从 URL 或网络电台加入。</div>`;
	const stations = player.stations.length ? player.stations.map((station, index) => `<article class="station-card">
		<span class="station-icon">${iconMarkup("airplay")}</span><div><strong>${escapeHTML(station.name)}</strong><small>${escapeHTML(station.source)}</small></div>
		<div class="station-actions"><span class="station-order-actions"><button class="icon-button compact-icon" data-player-action="radio_move_up" data-item-id="${escapeHTML(station.id)}" type="button" aria-label="上移 ${escapeHTML(station.name)}" ${capability.disabled || index === 0 ? "disabled" : ""}>↑</button><button class="icon-button compact-icon" data-player-action="radio_move_down" data-item-id="${escapeHTML(station.id)}" type="button" aria-label="下移 ${escapeHTML(station.name)}" ${capability.disabled || index === player.stations.length - 1 ? "disabled" : ""}>↓</button></span><button class="button compact" data-player-action="radio_play" data-item-id="${escapeHTML(station.id)}" type="button" ${capability.disabled ? "disabled" : ""}>播放</button><button class="button secondary compact" data-player-action="radio_queue" data-item-id="${escapeHTML(station.id)}" type="button" ${capability.disabled ? "disabled" : ""}>加入队列</button><button class="icon-button compact-icon danger-icon" data-player-action="radio_remove" data-item-id="${escapeHTML(station.id)}" type="button" aria-label="删除 ${escapeHTML(station.name)}" ${capability.disabled ? "disabled" : ""}>×</button></div>
	</article>`).join("") : `<div class="empty-state compact-empty">尚未收藏网络电台。</div>`;
	return `
		<section class="panel player-now">
			<div class="panel-header"><div><h2>正在播放</h2><p>音箱通过原厂 KPlayer 自主拉取媒体 URL，关闭当前网页不会中断播放。</p></div>${pill(player.transport)}</div>
			<div class="now-playing-copy"><span class="now-playing-icon">${iconMarkup("audio")}</span><div><strong>${escapeHTML(current?.title || "暂无媒体")}</strong><small>${escapeHTML(current?.source || "等待播放 URL 或网络电台")}</small></div></div>
			<div class="player-progress" role="progressbar" aria-valuemin="0" aria-valuemax="${Math.max(0, duration)}" aria-valuenow="${Math.max(0, position)}"><span style="width:${progress.toFixed(2)}%"></span></div>
			<div class="progress-labels"><span>${formatDuration(position)}</span><span>${duration > 0 ? formatDuration(duration) : "直播"}</span></div>
			<div class="player-controls">
				<button class="button secondary" data-player-action="pause" type="button" ${capability.disabled || transport !== "playing" ? "disabled" : ""}>暂停</button>
				<button class="button" data-player-action="resume" type="button" ${capability.disabled || transport !== "paused" ? "disabled" : ""}>恢复</button>
				<button class="button danger" data-player-action="stop" type="button" ${capability.disabled || !active ? "disabled" : ""}>停止</button>
				<button class="button secondary" data-player-action="next" type="button" ${capability.disabled || !hasNext ? "disabled" : ""}>下一首</button>
			</div>
			<div class="player-volume">
				<div class="player-volume-copy"><strong>本地播放音量</strong><span id="player-volume-value">${volumeKnown || hasDesiredVolume ? `${volume}%` : "未知"}</span></div>
				<label class="player-volume-control"><span>0</span><input id="player-volume-range" name="volume" type="range" min="0" max="100" step="1" value="${volume}" aria-label="本地播放音量，拖动时自动生效" ${capability.disabled || !volumeAdjustable || !volumeKnown ? "disabled" : ""}><span>100</span></label>
				<span class="player-volume-hint">拖动时自动生效</span>
			</div>
			<div class="stop-timer">
				<div class="stop-timer-copy"><strong>定时停止</strong><span>${stopTimer.active ? `剩余 ${formatDuration(timerRemaining)}` : "未设置 · 最长 60 分钟"}</span></div>
				<form id="stop-timer-form" class="stop-timer-form" novalidate>
					<span class="timer-field-label timer-preset-label">一键设置</span>
					<label class="timer-field-label timer-minute-label" for="stop-timer-minutes">自定义分钟</label>
					<span class="timer-field-label timer-action-label">操作</span>
					<div class="timer-presets" role="group" aria-label="定时停止快捷时长">
						${[15, 30, 45, 60].map((minutes) => `<button class="timer-preset" data-timer-minutes="${minutes}" type="button" ${capability.disabled ? "disabled" : ""}>${minutes}</button>`).join("")}
					</div>
					<input class="text-input timer-minute-input" id="stop-timer-minutes" name="durationMinutes" type="number" min="1" max="60" step="1" value="30" inputmode="numeric" aria-describedby="stop-timer-error" required>
					<div class="timer-actions"><button class="button" type="submit" ${capability.disabled ? "disabled" : ""}>设置</button><button class="button secondary" data-player-action="timer_cancel" type="button" ${capability.disabled || !stopTimer.active ? "disabled" : ""}>取消</button></div>
					<small id="stop-timer-error" class="field-error timer-feedback" aria-live="polite"></small>
				</form>
			</div>
			<details class="compatibility-note">
				<summary><span class="compatibility-icon" aria-hidden="true">i</span><strong>兼容性说明</strong><small>${escapeHTML(compatibilitySummary)} · 点击查看</small></summary>
				<div class="compatibility-detail"><strong>${escapeHTML(capability.label)}</strong><span>${escapeHTML(capability.reason)}</span></div>
			</details>
		</section>
		${sectionHeading("播放 URL", "支持 HTTP/HTTPS 音乐文件和可直接拉流的音频地址")}
		<section class="panel">
			<form id="player-url-form" class="media-form player-media-form" novalidate>
				<label class="media-field-label media-primary-label" for="player-media-url">媒体 URL</label>
				<label class="media-field-label media-secondary-label" for="player-media-title">标题（可选）</label>
				<span class="media-field-label media-action-label">播放方式</span>
				<div class="url-input-group media-primary-control"><input class="text-input" id="player-media-url" name="url" type="url" maxlength="2048" required placeholder="https://media.example/music.mp3" autocomplete="off" spellcheck="false" aria-describedby="player-media-url-error" data-media-url><button class="input-utility-button" data-url-action="paste" type="button">粘贴</button><button class="input-utility-button" data-url-action="clear" type="button">清空</button></div>
				<input class="text-input media-secondary-control" id="player-media-title" name="title" type="text" maxlength="100" placeholder="例如：客厅歌单" aria-describedby="player-media-title-hint">
				<div class="media-form-actions"><button class="button" data-submit-action="play_url" type="submit" ${capability.disabled ? "disabled" : ""}>立即播放</button><button class="button secondary" data-submit-action="queue_add" type="submit" ${capability.disabled ? "disabled" : ""}>加入队列</button></div>
				<small id="player-media-url-error" class="field-error media-primary-feedback" aria-live="polite"></small>
				<small id="player-media-title-hint" class="field-hint media-secondary-feedback">用于播放状态和队列识别</small>
				<div class="media-action-hints"><span>替换当前播放</span><span>添加到队列末尾</span></div>
				<p class="form-error" role="alert"></p>
			</form>
		</section>
		${sectionHeading("播放队列", "当前项结束后自动播放下一项", `${player.queue.length} 项`)}
		<section class="panel"><div class="queue-list">${queue}</div><div class="action-row"><button class="button danger" data-player-action="queue_clear" type="button" ${capability.disabled || player.queue.length === 0 ? "disabled" : ""}>停止并清空队列</button></div></section>
		${sectionHeading("网络电台", "保存可直接播放的 HTTP/HTTPS 电台流地址", `${player.stations.length} 个收藏`)}
		<section class="panel">
			<form id="radio-station-form" class="media-form station-form" novalidate>
				<label class="media-field-label media-primary-label" for="radio-station-name">电台名称</label>
				<label class="media-field-label media-secondary-label" for="radio-station-url">电台流 URL</label>
				<span class="media-field-label media-action-label">收藏操作</span>
				<input class="text-input media-primary-control" id="radio-station-name" name="title" type="text" maxlength="100" required placeholder="例如：爵士电台" aria-describedby="radio-station-name-error">
				<div class="url-input-group media-secondary-control"><input class="text-input" id="radio-station-url" name="url" type="url" maxlength="2048" required placeholder="https://radio.example/live.mp3" autocomplete="off" spellcheck="false" aria-describedby="radio-station-url-error" data-media-url><button class="input-utility-button" data-url-action="paste" type="button">粘贴</button><button class="input-utility-button" data-url-action="clear" type="button">清空</button></div>
				<div class="media-form-actions"><button class="button" type="submit" ${capability.disabled ? "disabled" : ""}>收藏电台</button></div>
				<small id="radio-station-name-error" class="field-error media-primary-feedback" aria-live="polite"></small>
				<small id="radio-station-url-error" class="field-error media-secondary-feedback" aria-live="polite"></small>
				<div class="media-action-hints"><span>保存到本机收藏</span></div>
				<p class="form-error" role="alert"></p>
			</form>
			<div class="station-grid">${stations}</div>
		</section>`;
}

async function refreshPlayer() {
	if (!app.data || app.environment !== "device" || app.playerRefreshing || playerVolumeBusy()) return;
	app.playerRefreshing = true;
	try {
		const response = await api.player();
		app.data.player = response.data;
		if (app.route === "player" && document.activeElement?.id !== "player-volume-range") renderPage();
	} catch (error) {
		if (app.route === "player") console.warn("播放器状态刷新失败", error);
	} finally {
		app.playerRefreshing = false;
	}
}

async function runPlayerAction(action, itemId = "", trigger = null) {
	const previousLabel = trigger?.textContent || "";
	if (trigger) {
		trigger.disabled = true;
		trigger.setAttribute("aria-busy", "true");
		if (!trigger.classList.contains("icon-button")) trigger.textContent = "处理中…";
	}
	try {
		const response = await api.controlPlayer(action, itemId ? { itemId } : {});
		app.data.player = response.data;
		renderPage();
		toast(action === "timer_cancel" ? "定时停止已取消" : "播放器操作已完成并回读状态", "success");
	} catch (error) {
		toast(error.message, "error");
		if (trigger) {
			trigger.disabled = false;
			trigger.removeAttribute("aria-busy");
			if (!trigger.classList.contains("icon-button")) trigger.textContent = previousLabel;
		}
	}
}

function openPlayerActionConfirmation(action, itemId) {
	const station = app.data.player.stations.find((item) => item.id === itemId);
	const clearing = action === "queue_clear";
	app.pendingPlayerAction = { action, itemId };
	elements.dialogContent.innerHTML = `
		<span class="dialog-kicker">${clearing ? "PLAYBACK QUEUE" : "RADIO FAVORITE"}</span>
		<h2>${clearing ? "停止播放并清空队列？" : `删除“${escapeHTML(station?.name || "该电台")}”？`}</h2>
		<p>${clearing ? `当前队列中的 ${app.data.player.queue.length} 项将被移除，正在播放的媒体也会停止。` : "删除后需要重新填写电台名称和流地址才能恢复。"}</p>
		<div class="capability-callout"><strong>此操作不可撤销</strong><span>${clearing ? "不会删除网络电台收藏。" : "不会停止当前播放，也不会自动从播放队列移除同一地址。"}</span></div>
		<div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-player-action" class="button danger" type="button">确认${clearing ? "清空" : "删除"}</button></div>`;
	setDialogBusy(false);
	elements.dialog.showModal();
}

async function confirmPlayerAction() {
	const pending = app.pendingPlayerAction;
	if (!pending) return;
	app.pendingPlayerAction = null;
	showOperationLoading("PLAYER OPERATION", pending.action === "queue_clear" ? "正在清空播放队列" : "正在删除网络电台", "等待播放器回读最终状态…", ["发送操作", "回读播放器", "确认最终状态"]);
	try {
		const response = await api.controlPlayer(pending.action, pending.itemId ? { itemId: pending.itemId } : {});
		app.data.player = response.data;
		elements.dialogContent.innerHTML = `<span class="dialog-kicker">PLAYER OPERATION</span><h2>操作已完成</h2><p>播放器已返回最新状态。</p><div class="operation-timeline"><div class="operation-step complete"><span class="step-dot">✓</span><span>请求已执行并完成状态回读</span></div></div><div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
		finishDialogOperation();
		renderPage();
	} catch (error) {
		elements.dialogContent.innerHTML = `<span class="dialog-kicker">PLAYER OPERATION</span><h2>操作未完成</h2><p>${escapeHTML(error.message)}</p><div class="dialog-actions"><button class="button secondary" value="done">关闭</button></div>`;
		finishDialogOperation();
	}
}

async function submitStopTimer(form) {
	const durationMinutes = Number(form.elements.durationMinutes.value);
	const errorMessage = validateTimerMinutes(durationMinutes);
	if (errorMessage) {
		setFieldError(form.elements.durationMinutes, errorMessage);
		form.elements.durationMinutes.focus();
		return;
	}
	setFormBusy(form, true, "设置中…");
	try {
		const response = await api.controlPlayer("timer_set", { durationMinutes });
		app.data.player = response.data;
		renderPage();
		toast(`已设置 ${durationMinutes} 分钟后停止播放`, "success");
	} catch (error) {
		setFormBusy(form, false);
		setFieldError(form.elements.durationMinutes, error.message);
	}
}

function playerVolumeBusy() {
	return app.playerVolumeUpdating || app.playerVolumePending !== null || app.playerVolumeTimer !== null;
}

function updatePlayerVolumeDisplay(volume, state = "") {
	const input = document.querySelector("#player-volume-range");
	if (input) input.value = String(volume);
	const output = document.querySelector("#player-volume-value");
	if (output) output.textContent = `${volume}%${state ? ` · ${state}` : ""}`;
}

function queuePlayerVolume(input, { immediate = false } = {}) {
	const volume = Number(input.value);
	if (!Number.isInteger(volume) || volume < 0 || volume > 100) {
		toast("请输入 0 到 100 的整数音量", "error");
		return;
	}
	app.playerVolumeDesired = volume;
	app.playerVolumePending = volume;
	app.playerVolumeRevision += 1;
	updatePlayerVolumeDisplay(volume);
	if (immediate) {
		if (app.playerVolumeTimer !== null) window.clearTimeout(app.playerVolumeTimer);
		app.playerVolumeTimer = null;
		void flushPlayerVolume();
		return;
	}
	if (app.playerVolumeTimer === null) {
		app.playerVolumeTimer = window.setTimeout(flushPlayerVolume, 150);
	}
}

async function flushPlayerVolume() {
	if (app.playerVolumeTimer !== null) window.clearTimeout(app.playerVolumeTimer);
	app.playerVolumeTimer = null;
	if (app.playerVolumeUpdating || app.playerVolumePending === null) return;
	const volume = app.playerVolumePending;
	const revision = app.playerVolumeRevision;
	app.playerVolumePending = null;
	app.playerVolumeUpdating = true;
	updatePlayerVolumeDisplay(app.playerVolumeDesired, "同步中");
	try {
		const response = await api.controlPlayer("volume_set", { volume });
		app.data.player = response.data;
		if (revision === app.playerVolumeRevision && app.playerVolumePending === null) {
			const confirmed = Number(response.data.volume?.value);
			app.playerVolumeDesired = null;
			updatePlayerVolumeDisplay(Number.isFinite(confirmed) ? Math.round(confirmed) : volume);
		}
	} catch (error) {
		if (revision === app.playerVolumeRevision && app.playerVolumePending === null) {
			toast(error.message, "error");
			try {
				const response = await api.player();
				if (revision === app.playerVolumeRevision && app.playerVolumePending === null) {
					app.data.player = response.data;
					app.playerVolumeDesired = null;
					const actual = Number(response.data.volume?.value);
					updatePlayerVolumeDisplay(Number.isFinite(actual) ? Math.round(actual) : volume, "设置失败");
				}
			} catch {
				if (revision === app.playerVolumeRevision && app.playerVolumePending === null) {
					app.playerVolumeDesired = null;
					updatePlayerVolumeDisplay(volume, "设置失败");
				}
			}
		}
	} finally {
		app.playerVolumeUpdating = false;
		if (app.playerVolumePending !== null) void flushPlayerVolume();
	}
}

async function submitPlayerURL(event) {
	const form = event.target;
	const action = event.submitter?.dataset.submitAction || "play_url";
	const title = form.elements.title.value.trim();
	const url = form.elements.url.value.trim();
	const errorMessage = validateMediaURL(url);
	if (errorMessage) {
		setFieldError(form.elements.url, errorMessage);
		form.elements.url.focus();
		return;
	}
	setFormBusy(form, true, action === "play_url" ? "正在播放…" : "正在加入…");
	try {
		const response = await api.controlPlayer(action, { title, url });
		app.data.player = response.data;
		form.reset();
		renderPage();
		toast(action === "play_url" ? "已开始播放" : "已加入播放队列", "success");
	} catch (error) {
		setFormBusy(form, false);
		setFormError(form, error.message);
	}
}

async function submitRadioStation(form) {
	const title = form.elements.title.value.trim();
	const url = form.elements.url.value.trim();
	let invalid = false;
	if (!title) {
		setFieldError(form.elements.title, "请输入电台名称");
		invalid = true;
	}
	const urlError = validateMediaURL(url);
	if (urlError) {
		setFieldError(form.elements.url, urlError);
		invalid = true;
	}
	if (invalid) {
		(title ? form.elements.url : form.elements.title).focus();
		return;
	}
	setFormBusy(form, true, "正在收藏…");
	try {
		const response = await api.controlPlayer("radio_add", { title, url });
		app.data.player = response.data;
		form.reset();
		renderPage();
		toast("网络电台已收藏", "success");
	} catch (error) {
		setFormBusy(form, false);
		setFormError(form, error.message);
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

function updateStatePresentation(update) {
	const states = {
		idle: ["尚无更新记录", "neutral"],
		staged: ["等待应用", "warning"],
		applying: ["正在更新", "warning"],
		succeeded: ["更新成功", "ok"],
		rolled_back: ["已自动回滚", "warning"],
		failed: ["更新失败", "danger"],
		rollback_failed: ["回滚失败", "danger"],
	};
	const [label, tone] = states[update?.state] || [update?.state || "未知", "unknown"];
	return `<span class="state-pill tone-${tone}">${escapeHTML(label)}</span>`;
}

function renderSystem() {
	const system = app.data.system;
	const build = system.build;
	const update = system.update || { state: "idle" };
	const enabled = app.data.environment === "device" && system.updateEnabled;
	return `
		<section class="panel">
			<div class="panel-header"><div><h2>当前版本</h2><p>网页资源与后端服务打包在同一个 ARMv7 程序中，更新后会一起切换。</p></div><span class="state-pill tone-ok">运行中</span></div>
			<div class="detail-grid">
				<div class="detail-row"><dl><dt>应用版本</dt><dd>${escapeHTML(build.version)}</dd></dl></div>
				<div class="detail-row"><dl><dt>提交</dt><dd>${escapeHTML(build.commit)}</dd></dl></div>
				<div class="detail-row"><dl><dt>构建时间</dt><dd>${escapeHTML(build.builtAt)}</dd></dl></div>
				<div class="detail-row"><dl><dt>最近更新状态</dt><dd>${escapeHTML(update.version || "—")}</dd></dl>${updateStatePresentation(update)}</div>
			</div>
			${update.message ? `<div class="capability-callout ${update.state === "succeeded" ? "callout-info" : ""}"><strong>更新记录</strong><span>${escapeHTML(update.message)}${update.updatedAt ? ` · ${escapeHTML(update.updatedAt)}` : ""}</span></div>` : ""}
		</section>
		${sectionHeading("签名更新", "只接受由本项目更新私钥签名、目标为 Linux/ARMv7 且版本更高的 .sanyin-update 文件")}
		<section class="panel">
			<form id="system-update-form" class="update-form" novalidate>
				<label>更新包
					<input id="system-update-file" class="text-input file-input" type="file" name="update" accept=".sanyin-update,application/vnd.sanyin.update+zip" aria-describedby="system-update-error" ${enabled ? "" : "disabled"} required>
					<small id="system-update-error" class="field-error" aria-live="polite"></small>
				</label>
				<button class="button warning" type="submit" ${enabled ? "" : "disabled"}>校验并更新</button>
				<p class="form-error" role="alert"></p>
			</form>
			<div class="capability-callout ${enabled ? "callout-info" : ""}">
				<strong>${enabled ? "签名验证已启用" : "网页更新未启用"}</strong>
				<span>${enabled ? "上传后先验证 Ed25519 签名、版本、SHA-256 与 ELF 平台，再原子替换；20 秒内未恢复 8787 端口会自动回滚。" : (app.data.environment === "device" ? "请先通过 SSH 或 ADB 安装 update-public-key 与设备侧更新脚本。" : "Mock 模式不会接收或执行更新包。")}</span>
			</div>
			<p class="update-note">网页更新只替换 sanyin-config 单文件；启动脚本或设备辅助脚本发生变化时，请通过 SSH 执行完整的 config-install。</p>
		</section>`;
}

function openSystemUpdateConfirmation(form) {
	const file = form.querySelector("#system-update-file")?.files?.[0];
	if (!file) {
		setFieldError(form.querySelector("#system-update-file"), "请选择 .sanyin-update 文件");
		return;
	}
	if (!file.name.endsWith(".sanyin-update")) {
		setFieldError(form.querySelector("#system-update-file"), "文件扩展名必须为 .sanyin-update");
		return;
	}
	if (file.size <= 0 || file.size > 32 * 1024 * 1024) {
		setFieldError(form.querySelector("#system-update-file"), "更新包必须大于 0 且小于 32 MiB");
		return;
	}
	app.pendingUpdateFile = file;
	elements.dialogContent.innerHTML = `
		<span class="dialog-kicker">SIGNED SYSTEM UPDATE</span>
		<h2>安装 ${escapeHTML(file.name)}</h2>
		<p>设备将验证更新包签名和目标平台。验证通过后，服务会短暂重启；如果新版本无法启动或 8787 端口未恢复，将自动切回上一版。</p>
		<div class="capability-callout"><strong>连接会短暂中断</strong><span>更新期间不要断电。网页恢复后请核对版本号和最近更新状态。</span></div>
		<div class="operation-impact"><span>目标设备</span><strong>${escapeHTML(displayValue(app.data.device.productFamily?.value || "unknown"))} · ${escapeHTML(location.host)}</strong></div>
		<div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-system-update" class="button warning" type="button">确认更新</button></div>`;
	setDialogBusy(false);
	elements.dialog.showModal();
}

async function runSystemUpdate() {
	const file = app.pendingUpdateFile;
	if (!file) return;
	app.pendingUpdateFile = null;
	showOperationLoading("SIGNED SYSTEM UPDATE", "正在验证更新包", "更新期间不要关闭设备或切断电源。", ["验证 Ed25519 签名", "校验 SHA-256 与 ARMv7 平台", "原子替换并重启", "健康检查或自动回滚"]);
	try {
		const response = await api.updateSystem(file);
		await waitForSystemUpdate(response.data.version);
	} catch (error) {
		elements.dialogContent.innerHTML = `<span class="dialog-kicker">UPDATE REJECTED</span><h2>更新未开始</h2><p>${escapeHTML(error.message)}</p><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
		finishDialogOperation();
	}
}

async function waitForSystemUpdate(expectedVersion) {
	showOperationLoading("UPDATE ACCEPTED", `正在重启到 ${expectedVersion}`, "等待新服务恢复 8787 端口；失败时设备会自动恢复上一版。", ["更新包已通过验证", "服务正在重启", "等待健康检查", "确认版本或回滚"]);
	for (let attempt = 0; attempt < 70; attempt += 1) {
		await new Promise((resolve) => setTimeout(resolve, 1000));
		try {
			const response = await api.system();
			const system = response.data;
			if (system.build.version === expectedVersion && system.update.state === "succeeded") {
					elements.dialogContent.innerHTML = `<span class="dialog-kicker">UPDATE SUCCEEDED</span><h2>已更新到 ${escapeHTML(expectedVersion)}</h2><p>${escapeHTML(system.update.message)}</p><div class="dialog-actions"><button class="button" data-action="reload" value="done">重新加载页面</button></div>`;
					finishDialogOperation();
					return;
			}
			if (["rolled_back", "rollback_failed", "failed"].includes(system.update.state)) {
					elements.dialogContent.innerHTML = `<span class="dialog-kicker">UPDATE ROLLBACK</span><h2>${system.update.state === "rolled_back" ? "更新失败，已恢复上一版" : "更新或回滚需要人工处理"}</h2><p>${escapeHTML(system.update.message || "请通过 SSH 或 ADB 查看 /tmp/sanyin_update.log。")}</p><div class="dialog-actions"><button class="button" data-action="reload" value="done">重新加载页面</button></div>`;
					finishDialogOperation();
					return;
			}
		} catch {
			// 服务重启窗口内连接失败是预期行为。
		}
	}
	elements.dialogContent.innerHTML = `<span class="dialog-kicker">UPDATE TIMEOUT</span><h2>尚未确认更新结果</h2><p>设备在 70 秒内没有恢复网页连接。请通过 SSH 或 ADB 检查 sanyin_config 服务和 /tmp/sanyin_update.log。</p><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
	finishDialogOperation();
}

function openOperationConfirmation(simulation) {
  elements.dialogContent.innerHTML = `
	<span class="dialog-kicker">${simulation ? "MOCK OPERATION" : "DEVICE OPERATION"}</span>
	<h2>${simulation ? "演示 AirPlay 恢复流程" : "恢复真实设备的 AirPlay"}</h2>
	<p>${simulation ? "仅演示“确认 → 执行 → 验收 → 成功/回滚”交互，不会连接或修改音箱。" : "服务将发送已验证的原生启动命令，并等待 TCP 5002 开始监听。当前已在监听时不会重复写入。"}</p>
	<div class="capability-callout ${simulation ? "" : "callout-info"}"><strong>${simulation ? "模拟操作" : "真实设备操作"}</strong><span>${simulation ? "响应会明确包含 simulation=true。" : "仅恢复或确认 AirPlay 可用，不修改 Wi-Fi、厂商数据库或其他配置。"}</span></div>
	<div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-operation" data-simulation="${simulation}" class="button" type="button">${simulation ? "确认演示" : "确认恢复"}</button></div>`;
	setDialogBusy(false);
  elements.dialog.showModal();
}

function openAutoRecoverConfirmation(enabled) {
	const action = enabled ? "开启" : "关闭";
	elements.dialogContent.innerHTML = `
	  <span class="dialog-kicker">DEVICE CONFIGURATION</span>
	  <h2>${action} AirPlay 自动恢复</h2>
	  <p>${enabled ? "开启后，守护服务会在 Wi-Fi 和 SPlayer 就绪但 TCP 5002 未监听时自动恢复 AirPlay。" : "关闭后，守护服务仍保持运行，但不会再自动发送 AirPlay 启动命令；当前已经建立的监听不会被强制停止。"}</p>
	  <div class="capability-callout callout-info"><strong>持久配置</strong><span>只写入自有配置文件，不修改厂商数据库；写入后会立即回读验收。</span></div>
	  <div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-autorecover" data-enabled="${enabled}" class="button ${enabled ? "" : "warning"}" type="button">确认${action}</button></div>`;
	setDialogBusy(false);
	elements.dialog.showModal();
}

async function runAirPlayOperation(simulation) {
	showOperationLoading(simulation ? "MOCK OPERATION" : "DEVICE OPERATION", simulation ? "正在执行模拟流程" : "正在恢复 AirPlay", "等待 TCP 5002 端口状态回读。", ["确认设备状态", "发送恢复命令", "验收端口监听"]);
  try {
	const response = simulation ? await api.simulateAirplayRecovery() : await api.recoverAirplay();
    const operation = response.data;
    const presentation = operationPresentation(operation);
    elements.dialogContent.innerHTML = `
	  <span class="dialog-kicker">${operation.simulation ? "SIMULATION" : "DEVICE"} · ${escapeHTML(operation.operationId)}</span>
      <h2>${escapeHTML(presentation.title)}</h2>
      <p>${escapeHTML(operation.message)}</p>
      <div class="operation-timeline">${operationStepsMarkup(presentation.steps)}</div>
	  <div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
	finishDialogOperation();
	if (!simulation) await loadData();
  } catch (error) {
	elements.dialogContent.innerHTML = `<span class="dialog-kicker">REQUEST ERROR</span><h2>操作请求失败</h2><p>${escapeHTML(error.message)}</p><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
	finishDialogOperation();
  }
}

async function runAutoRecoverConfiguration(enabled) {
	showOperationLoading("DEVICE CONFIGURATION", "正在写入自动恢复配置", "配置写入后会立即回读确认。", ["写入自有配置", "原子替换", "回读验收"]);
	try {
	  const response = await api.setAirplayAutoRecover(enabled);
	  const operation = response.data;
	  const presentation = operationPresentation(operation);
	  elements.dialogContent.innerHTML = `
		<span class="dialog-kicker">DEVICE · ${escapeHTML(operation.operationId)}</span>
		<h2>${escapeHTML(presentation.title)}</h2>
		<p>${escapeHTML(operation.message)}</p>
		<div class="operation-timeline">${operationStepsMarkup(presentation.steps)}</div>
		<div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
	  finishDialogOperation();
	  await loadData();
	} catch (error) {
	  elements.dialogContent.innerHTML = `<span class="dialog-kicker">REQUEST ERROR</span><h2>配置写入失败</h2><p>${escapeHTML(error.message)}</p><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
	  finishDialogOperation();
	}
}

function openBluetoothConfirmation(enabled) {
	const action = enabled ? "开启" : "关闭";
	elements.dialogContent.innerHTML = `
	  <span class="dialog-kicker">EXPERIMENTAL DEVICE CONFIGURATION</span>
	  <h2>${action}蓝牙</h2>
	  <p>将发送已完成独立回放的蓝牙${action}命令，并等待设备服务返回对应成功事件。当前固件仍没有无副作用状态查询，因此页面会把“当前开关”保持为未知，只单独记录最近一次验收结果。</p>
	  <div class="capability-callout"><strong>实验能力</strong><span>连接中的设备行为、服务异常回滚和重启状态仍待完整验证。</span></div>
	  <div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-bluetooth" data-enabled="${enabled}" class="button ${enabled ? "" : "warning"}" type="button">确认${action}</button></div>`;
	setDialogBusy(false);
	elements.dialog.showModal();
}

async function runBluetoothConfiguration(enabled) {
	showOperationLoading("EXPERIMENTAL DEVICE CONFIGURATION", `正在${enabled ? "开启" : "关闭"}蓝牙`, "等待设备服务返回成功事件。", ["发送蓝牙命令", "等待服务事件", "记录最近验收状态"]);
	try {
	  const response = await api.setBluetooth(enabled);
	  const operation = response.data;
	  const presentation = operationPresentation(operation);
	  elements.dialogContent.innerHTML = `
		<span class="dialog-kicker">DEVICE · ${escapeHTML(operation.operationId)}</span>
		<h2>${escapeHTML(presentation.title)}</h2>
		<p>${escapeHTML(operation.message)}</p>
		<div class="operation-timeline">${operationStepsMarkup(presentation.steps)}</div>
		<div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
	  finishDialogOperation();
	  await loadData();
	} catch (error) {
	  elements.dialogContent.innerHTML = `<span class="dialog-kicker">REQUEST ERROR</span><h2>蓝牙操作失败</h2><p>${escapeHTML(error.message)}</p><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
	  finishDialogOperation();
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
	setDialogBusy(false);
	elements.dialog.showModal();
}

async function runEQConfiguration(mode) {
	const item = eqModes.find((candidate) => candidate.mode === mode);
	if (!item) return;
	showOperationLoading("EXPERIMENTAL DEVICE CONFIGURATION", `正在切换至${item.label}`, "等待业务选中态事件与硬件文件回读，两类状态将分别验收。", ["发送固定 EQ 模式", "等待业务选中态", "回读硬件应用态"]);
	try {
	  const response = await api.setEQ(mode);
	  const operation = response.data;
	  const presentation = operationPresentation(operation);
	  elements.dialogContent.innerHTML = `
		<span class="dialog-kicker">DEVICE · ${escapeHTML(operation.operationId)}</span>
		<h2>${escapeHTML(presentation.title)}</h2>
		<p>${escapeHTML(operation.message)}</p>
		<div class="operation-timeline">${operationStepsMarkup(presentation.steps)}</div>
		<div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
	  finishDialogOperation();
	  await loadData();
	} catch (error) {
	  elements.dialogContent.innerHTML = `<span class="dialog-kicker">REQUEST ERROR</span><h2>EQ 操作失败</h2><p>${escapeHTML(error.message)}</p><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
	  finishDialogOperation();
	}
}

function openWiFiConfiguration() {
	const currentSSID = app.data.network.currentSSID?.value === "unknown" ? "" : app.data.network.currentSSID?.value || "";
	elements.dialogContent.innerHTML = `
	  <span class="dialog-kicker">NETWORK TRANSACTION</span>
	  <h2>切换 Wi-Fi</h2>
	  <p>当前设备 ${escapeHTML(displayValue(app.data.device.productFamily?.value || "unknown"))} 正通过“${escapeHTML(currentSSID || "未知网络")}”访问。服务会先备份配置，再连接目标网络；45 秒内未同时确认目标 SSID、IPv4、默认路由和网关可达时自动恢复。</p>
	  <label class="field-label" for="wifi-ssid">Wi-Fi 名称（SSID）</label>
	  <input class="text-input" id="wifi-ssid" type="text" maxlength="32" value="${escapeHTML(currentSSID)}" autocomplete="off" spellcheck="false" aria-describedby="wifi-ssid-error">
	  <small id="wifi-ssid-error" class="field-error" aria-live="polite"></small>
	  <label class="field-label" for="wifi-password">Wi-Fi 密码</label>
	  <div class="password-field"><input class="text-input" id="wifi-password" type="password" maxlength="64" autocomplete="new-password" placeholder="开放网络请留空"><button class="password-toggle" data-action="toggle-password" data-target="wifi-password" type="button" aria-pressed="false">显示</button></div>
	  <div class="capability-callout"><strong>连接可能暂时中断</strong><span>请只在可信局域网中操作。页面请求断开不代表事务停止；音箱端脚本会继续验收或自动回退。密码只写入权限 0600 的临时文件，事务结束后删除。</span></div>
	  <div class="operation-impact"><span>预计中断</span><strong>最多 45 秒验收，失败时最多再等待 45 秒恢复原网络</strong></div>
	  <div class="dialog-actions"><button class="button secondary" value="cancel">取消</button><button id="confirm-wifi" class="button warning" type="button">确认切换</button></div>`;
	setDialogBusy(false);
	elements.dialog.showModal();
}

async function runWiFiConfiguration() {
	const ssidInput = elements.dialogContent.querySelector("#wifi-ssid");
	const passwordInput = elements.dialogContent.querySelector("#wifi-password");
	const ssid = ssidInput?.value.trim() || "";
	const password = passwordInput?.value || "";
	const ssidError = validateSSID(ssid);
	if (ssidError) {
		setFieldError(ssidInput, ssidError);
		ssidInput?.focus();
		return;
	}
	passwordInput.value = "";
	showOperationLoading("NETWORK TRANSACTION", `正在切换至“${ssid}”`, "页面连接中断属于预期情况，音箱端事务仍会继续。", ["备份当前网络", "连接目标 Wi-Fi", "验收 SSID、路由与网关", "失败时恢复原网络"]);
	try {
	  const response = await api.switchWiFi(ssid, password);
	  const operation = response.data;
	  const presentation = operationPresentation(operation);
	  elements.dialogContent.innerHTML = `
		<span class="dialog-kicker">DEVICE · ${escapeHTML(operation.operationId)}</span>
		<h2>${escapeHTML(presentation.title)}</h2>
		<p>${escapeHTML(operation.message)}</p>
			<div class="operation-timeline">${operationStepsMarkup(presentation.steps)}</div>
			<div class="dialog-actions"><button class="button" value="done">完成</button></div>`;
		  finishDialogOperation();
		  await loadData();
	} catch (error) {
		  elements.dialogContent.innerHTML = `<span class="dialog-kicker">CONNECTION INTERRUPTED</span><h2>与原地址的连接已中断</h2><p>Wi-Fi 事务仍会在音箱上继续运行。请等待最多 90 秒：成功时使用音箱在新网络取得的地址；失败时重新打开原地址，音箱应已自动回退。</p><div class="capability-callout"><strong>不会因页面断开而停止</strong><span>如 90 秒后两个网络都无法访问，请通过 USB ADB 运行 sanyinctl config-status。</span></div><div class="dialog-actions"><button class="button" value="done">关闭</button></div>`;
		  finishDialogOperation();
	}
}

function toast(message, tone = "info") {
  const item = document.createElement("div");
	item.className = `toast toast-${tone}`;
	item.setAttribute("role", tone === "error" ? "alert" : "status");
  item.textContent = message;
  elements.toast.appendChild(item);
	setTimeout(() => item.remove(), tone === "error" ? 6000 : 3500);
}

initialize();
