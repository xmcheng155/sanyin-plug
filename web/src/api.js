export class ApiClient {
  constructor(baseURL = "/api/v1") {
    this.baseURL = baseURL;
    this.scenario = "healthy";
  }

  setScenario(scenario) {
    this.scenario = scenario || "healthy";
  }

  url(path, parameters = {}) {
    const url = new URL(`${this.baseURL}${path}`, window.location.origin);
    url.searchParams.set("scenario", this.scenario);
    Object.entries(parameters).forEach(([key, value]) => url.searchParams.set(key, String(value)));
    return `${url.pathname}${url.search}`;
  }

  async request(path, options = {}, parameters = {}) {
	const method = options.method || "GET";
    const response = await fetch(this.url(path, parameters), {
      ...options,
	  headers: { Accept: "application/json", ...(method === "GET" ? {} : { "X-Sanyin-CSRF": "1" }), ...(options.headers || {}) },
    });
    const payload = await response.json();
    if (!response.ok) {
      const error = new Error(payload?.error?.message || `HTTP ${response.status}`);
      error.code = payload?.error?.code || "request_failed";
      error.status = response.status;
      throw error;
    }
    return payload;
  }

  scenarios() { return this.request("/mock/scenarios"); }
  capabilities() { return this.request("/capabilities"); }
  device() { return this.request("/device"); }
  status() { return this.request("/status"); }
  airplay() { return this.request("/airplay"); }
  network() { return this.request("/network"); }
  audio() { return this.request("/audio"); }
  bluetooth() { return this.request("/bluetooth"); }
  lighting() { return this.request("/lighting"); }
	schedules() { return this.request("/schedules"); }
	player() { return this.request("/player"); }
	scenes() { return this.request("/scenes"); }
	system() { return this.request("/system"); }

	updateSystem(file) {
		return this.request("/system/update", {
			method: "POST",
			headers: { "Content-Type": "application/vnd.sanyin.update+zip" },
			body: file,
		});
	}

	controlPlayer(action, payload = {}) {
		return this.request("/player/control", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ action, ...payload }),
		});
	}

	createScene(payload) {
		return this.request("/scenes", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(payload),
		});
	}

	updateScene(id, payload) {
		return this.request(`/scenes/${encodeURIComponent(id)}`, {
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(payload),
		});
	}

	deleteScene(id) {
		return this.request(`/scenes/${encodeURIComponent(id)}`, { method: "DELETE" });
	}

	applyScene(id) {
		return this.request(`/scenes/${encodeURIComponent(id)}/apply`, { method: "POST" });
	}

  simulateAirplayRecovery() {
    return this.request("/airplay/recover", { method: "POST" }, { simulate: true });
  }

  recoverAirplay() {
	return this.request("/airplay/recover", { method: "POST" });
  }

  setAirplayAutoRecover(enabled) {
	return this.request("/airplay/auto-recover", {
	  method: "PUT",
	  headers: { "Content-Type": "application/json" },
	  body: JSON.stringify({ enabled: Boolean(enabled) }),
	});
  }

  setBluetooth(enabled) {
	return this.request("/bluetooth", {
	  method: "PATCH",
	  headers: { "Content-Type": "application/json" },
	  body: JSON.stringify({ enabled: Boolean(enabled) }),
	});
  }

  setEQ(mode) {
	return this.request("/audio/effect", {
	  method: "PATCH",
	  headers: { "Content-Type": "application/json" },
	  body: JSON.stringify({ mode: Number(mode) }),
	});
  }

  switchWiFi(ssid, password) {
	return this.request("/network/switch", {
	  method: "POST",
	  headers: { "Content-Type": "application/json" },
	  body: JSON.stringify({ ssid, password }),
	});
  }

  events() {
    return new EventSource(this.url("/events"));
  }
}
