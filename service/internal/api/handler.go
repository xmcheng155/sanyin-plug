package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"sanyin.local/config/service/internal/adapter"
	"sanyin.local/config/service/internal/domain"
)

const BasePath = "/api/v1"

type Handler struct {
	provider adapter.Provider
}

func NewHandler(provider adapter.Provider) http.Handler {
	return &Handler{provider: provider}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}

	if path == BasePath && r.Method == http.MethodGet {
		h.writeJSON(w, http.StatusOK, map[string]any{
			"apiVersion":  "v1",
			"environment": h.provider.Environment(),
			"basePath":    BasePath,
		})
		return
	}
	if path == BasePath+"/mock/scenarios" && r.Method == http.MethodGet {
		h.writeJSON(w, http.StatusOK, domain.Envelope[[]adapter.Scenario]{Environment: h.provider.Environment(), Scenario: h.provider.DefaultScenario(), Data: h.provider.Scenarios()})
		return
	}

	device, scenario, ok := h.adapterForRequest(w, r)
	if !ok {
		return
	}

	if path == BasePath+"/events" {
		if r.Method != http.MethodGet {
			h.methodNotAllowed(w, http.MethodGet)
			return
		}
		h.writeEvent(w, r, device)
		return
	}

	if r.Method == http.MethodGet {
		h.handleRead(w, r, path, scenario, device)
		return
	}

	if path == BasePath+"/airplay/recover" && r.Method == http.MethodPost {
		if r.URL.Query().Get("simulate") == "true" {
			if h.provider.Environment() != "mock" {
				h.writeError(w, http.StatusConflict, "simulation_unavailable", "真实设备模式不提供模拟操作")
				return
			}
			operation, err := device.SimulateOperation(r.Context(), "airplay-recover")
			if err != nil {
				h.internalError(w)
				return
			}
			h.writeJSON(w, http.StatusAccepted, domain.Envelope[domain.Operation]{Environment: h.provider.Environment(), Scenario: scenario, Data: operation})
			return
		}
		if h.provider.Environment() == "mock" {
			h.notReady(w)
			return
		}
		operation, err := device.RecoverAirPlay(r.Context())
		if err != nil {
			h.internalError(w)
			return
		}
		h.writeJSON(w, http.StatusAccepted, domain.Envelope[domain.Operation]{Environment: h.provider.Environment(), Scenario: scenario, Data: operation})
		return
	}

	if path == BasePath+"/airplay/auto-recover" && r.Method == http.MethodPut {
		if h.provider.Environment() == "mock" {
			h.notReady(w)
			return
		}
		var input struct {
			Enabled *bool `json:"enabled"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input.Enabled == nil {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "请求必须只包含布尔字段 enabled")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "请求只能包含一个 JSON 对象")
			return
		}
		operation, err := device.SetAirPlayAutoRecover(r.Context(), *input.Enabled)
		if errors.Is(err, adapter.ErrCapabilityNotReady) {
			h.notReady(w)
			return
		}
		if err != nil {
			h.internalError(w)
			return
		}
		h.writeJSON(w, http.StatusAccepted, domain.Envelope[domain.Operation]{Environment: h.provider.Environment(), Scenario: scenario, Data: operation})
		return
	}

	if path == BasePath+"/bluetooth" && r.Method == http.MethodPatch {
		if h.provider.Environment() == "mock" {
			h.notReady(w)
			return
		}
		enabled, ok := h.readEnabled(w, r)
		if !ok {
			return
		}
		operation, err := device.SetBluetooth(r.Context(), enabled)
		if err != nil {
			h.internalError(w)
			return
		}
		h.writeJSON(w, http.StatusAccepted, domain.Envelope[domain.Operation]{Environment: h.provider.Environment(), Scenario: scenario, Data: operation})
		return
	}

	if path == BasePath+"/audio/effect" && r.Method == http.MethodPatch {
		if h.provider.Environment() == "mock" {
			h.notReady(w)
			return
		}
		var input struct {
			Mode *int `json:"mode"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input.Mode == nil || *input.Mode < 0 || *input.Mode > 6 {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "请求必须只包含整数 mode，且范围为 0..6")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "请求只能包含一个 JSON 对象")
			return
		}
		operation, err := device.SetEQ(r.Context(), *input.Mode)
		if err != nil {
			h.internalError(w)
			return
		}
		h.writeJSON(w, http.StatusAccepted, domain.Envelope[domain.Operation]{Environment: h.provider.Environment(), Scenario: scenario, Data: operation})
		return
	}

	if path == BasePath+"/network/switch" && r.Method == http.MethodPost {
		if h.provider.Environment() == "mock" {
			h.notReady(w)
			return
		}
		var input struct {
			SSID     *string `json:"ssid"`
			Password *string `json:"password"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input.SSID == nil || input.Password == nil {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "请求必须只包含字符串字段 ssid 和 password；开放网络的 password 使用空字符串")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "请求只能包含一个 JSON 对象")
			return
		}
		operation, err := device.SetWiFi(r.Context(), *input.SSID, *input.Password)
		if errors.Is(err, adapter.ErrCapabilityNotReady) {
			h.notReady(w)
			return
		}
		if errors.Is(err, adapter.ErrInvalidInput) {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "SSID 必须为 1..32 字节；密码为空表示开放网络，否则必须为 8..63 字节或 64 位十六进制 PSK")
			return
		}
		if err != nil {
			h.internalError(w)
			return
		}
		h.writeJSON(w, http.StatusAccepted, domain.Envelope[domain.Operation]{Environment: h.provider.Environment(), Scenario: scenario, Data: operation})
		return
	}

	if path == BasePath+"/player/control" && r.Method == http.MethodPost {
		if h.provider.Environment() == "mock" {
			h.notReady(w)
			return
		}
		var input struct {
			Action          *string `json:"action"`
			ItemID          string  `json:"itemId"`
			Title           string  `json:"title"`
			URL             string  `json:"url"`
			DurationMinutes int     `json:"durationMinutes"`
			Volume          *int    `json:"volume"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input.Action == nil {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "请求必须包含有效的播放器 action")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "请求只能包含一个 JSON 对象")
			return
		}
		command := domain.PlayerCommand{Action: *input.Action, ItemID: input.ItemID, Title: input.Title, URL: input.URL, DurationMinutes: input.DurationMinutes, Volume: input.Volume}
		if !validPlayerCommand(command) {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "播放器 action 与参数不匹配")
			return
		}
		player, err := device.ControlPlayer(r.Context(), command)
		switch {
		case errors.Is(err, adapter.ErrCapabilityNotReady):
			h.notReady(w)
			return
		case errors.Is(err, adapter.ErrInvalidInput):
			h.writeError(w, http.StatusBadRequest, "invalid_request", "媒体地址必须为有效的 HTTP/HTTPS URL，标题最长 100 个字符；定时停止范围为 1..60 分钟；本地播放音量范围为 0..100")
			return
		case errors.Is(err, adapter.ErrNotFound):
			h.writeError(w, http.StatusNotFound, "player_item_not_found", "播放项或网络电台不存在")
			return
		case errors.Is(err, adapter.ErrConflict):
			h.writeError(w, http.StatusConflict, "player_conflict", "当前播放项不能在播放中删除，请先停止或切换")
			return
		case errors.Is(err, adapter.ErrPlaybackInactive):
			h.writeError(w, http.StatusConflict, "player_not_active", "调整本地播放音量前请先开始或恢复播放")
			return
		case err != nil:
			h.internalError(w)
			return
		}
		h.writeJSON(w, http.StatusOK, domain.Envelope[domain.Player]{Environment: h.provider.Environment(), Scenario: scenario, Data: player})
		return
	}

	if isReservedWrite(path, r.Method) {
		h.notReady(w)
		return
	}

	h.writeError(w, http.StatusNotFound, "not_found", "接口不存在")
}

func validPlayerCommand(command domain.PlayerCommand) bool {
	switch command.Action {
	case "play_url", "queue_add", "radio_add":
		return command.URL != "" && command.ItemID == "" && command.DurationMinutes == 0 && command.Volume == nil
	case "pause", "resume", "stop", "next", "queue_clear":
		return command.ItemID == "" && command.Title == "" && command.URL == "" && command.DurationMinutes == 0 && command.Volume == nil
	case "queue_play", "queue_remove", "radio_remove", "radio_play", "radio_queue", "radio_move_up", "radio_move_down":
		return command.ItemID != "" && command.Title == "" && command.URL == "" && command.DurationMinutes == 0 && command.Volume == nil
	case "timer_set":
		return command.ItemID == "" && command.Title == "" && command.URL == "" && command.DurationMinutes >= 1 && command.DurationMinutes <= 60 && command.Volume == nil
	case "timer_cancel":
		return command.ItemID == "" && command.Title == "" && command.URL == "" && command.DurationMinutes == 0 && command.Volume == nil
	case "volume_set":
		return command.ItemID == "" && command.Title == "" && command.URL == "" && command.DurationMinutes == 0 && command.Volume != nil && *command.Volume >= 0 && *command.Volume <= 100
	default:
		return false
	}
}

func (h *Handler) adapterForRequest(w http.ResponseWriter, r *http.Request) (adapter.DeviceAdapter, string, bool) {
	scenario := r.URL.Query().Get("scenario")
	if scenario == "" || h.provider.Environment() != "mock" {
		scenario = h.provider.DefaultScenario()
	}
	device, err := h.provider.ForScenario(scenario)
	if err != nil {
		if errors.Is(err, adapter.ErrUnknownScenario) {
			h.writeError(w, http.StatusBadRequest, "unknown_mock_scenario", "未知的 Mock 场景")
			return nil, scenario, false
		}
		h.internalError(w)
		return nil, scenario, false
	}
	return device, scenario, true
}

func (h *Handler) handleRead(w http.ResponseWriter, r *http.Request, path, scenario string, device adapter.DeviceAdapter) {
	ctx := r.Context()
	var value any
	var err error

	switch path {
	case BasePath + "/capabilities":
		value, err = device.Capabilities(ctx)
	case BasePath + "/device":
		value, err = device.Device(ctx)
	case BasePath + "/status":
		value, err = device.Status(ctx)
	case BasePath + "/airplay":
		value, err = device.AirPlay(ctx)
	case BasePath + "/network":
		value, err = device.Network(ctx)
	case BasePath + "/audio":
		value, err = device.Audio(ctx)
	case BasePath + "/bluetooth":
		value, err = device.Bluetooth(ctx)
	case BasePath + "/lighting":
		value, err = device.Lighting(ctx)
	case BasePath + "/schedules":
		value, err = device.Schedules(ctx)
	case BasePath + "/player":
		value, err = device.Player(ctx)
	default:
		h.writeError(w, http.StatusNotFound, "not_found", "接口不存在")
		return
	}
	if err != nil {
		h.internalError(w)
		return
	}
	h.writeJSON(w, http.StatusOK, domain.Envelope[any]{Environment: h.provider.Environment(), Scenario: scenario, Data: value})
}

func (h *Handler) writeEvent(w http.ResponseWriter, r *http.Request, device adapter.DeviceAdapter) {
	event, err := device.Event(r.Context())
	if err != nil {
		h.internalError(w)
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		h.internalError(w)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "retry: 5000\nid: %d\nevent: snapshot\ndata: %s\n\n", event.Revision, data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func isReservedWrite(path, method string) bool {
	reserved := map[string]map[string]bool{
		BasePath + "/network/switch":      {http.MethodPost: true},
		BasePath + "/bluetooth":           {http.MethodPatch: true},
		BasePath + "/audio":               {http.MethodPatch: true},
		BasePath + "/audio/effect":        {http.MethodPatch: true},
		BasePath + "/lighting":            {http.MethodPatch: true},
		BasePath + "/microphone/schedule": {http.MethodPut: true},
		BasePath + "/player/control":      {http.MethodPost: true},
	}
	return reserved[path][method]
}

func (h *Handler) notReady(w http.ResponseWriter) {
	h.writeError(w, http.StatusConflict, "capability_not_ready", "该操作尚未完成安全写入验证")
}

func (h *Handler) readEnabled(w http.ResponseWriter, r *http.Request) (bool, bool) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Enabled == nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "请求必须只包含布尔字段 enabled")
		return false, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "请求只能包含一个 JSON 对象")
		return false, false
	}
	return *input.Enabled, true
}

func (h *Handler) internalError(w http.ResponseWriter) {
	h.writeError(w, http.StatusInternalServerError, "internal_error", "服务暂时无法生成状态")
}

func (h *Handler) methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	h.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不受支持")
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	h.writeJSON(w, status, domain.APIError{Error: domain.ErrorBody{Code: code, Message: message, OperationID: nil}})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
