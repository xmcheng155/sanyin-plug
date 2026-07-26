package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"sanyin.local/config/service/internal/adapter"
	"sanyin.local/config/service/internal/domain"
	"sanyin.local/config/service/internal/updater"
)

const BasePath = "/api/v1"

type Handler struct {
	provider adapter.Provider
	build    domain.BuildInfo
	updater  UpdateManager
}

type UpdateManager interface {
	Info() domain.SystemInfo
	Stage(context.Context, io.Reader, int64) (domain.UpdateAccepted, error)
}

type Options struct {
	Build   domain.BuildInfo
	Updater UpdateManager
}

func NewHandler(provider adapter.Provider, options ...Options) http.Handler {
	handler := &Handler{
		provider: provider,
		build:    domain.BuildInfo{Version: "dev", Commit: "unknown", BuiltAt: "unknown"},
	}
	if len(options) > 0 {
		if options[0].Build.Version != "" {
			handler.build = options[0].Build
		}
		handler.updater = options[0].Updater
	}
	return handler
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
			"appVersion":  h.build.Version,
			"environment": h.provider.Environment(),
			"basePath":    BasePath,
		})
		return
	}
	if path == BasePath+"/system" {
		if r.Method != http.MethodGet {
			h.methodNotAllowed(w, http.MethodGet)
			return
		}
		info := domain.SystemInfo{Build: h.build, UpdateEnabled: false, Update: domain.UpdateStatus{State: "idle"}}
		if h.updater != nil {
			info = h.updater.Info()
		}
		h.writeJSON(w, http.StatusOK, domain.Envelope[domain.SystemInfo]{
			Environment: h.provider.Environment(),
			Scenario:    h.provider.DefaultScenario(),
			Data:        info,
		})
		return
	}
	if path == BasePath+"/system/update" {
		h.handleUpdate(w, r)
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

	if path == BasePath+"/media-library/favorites" && r.Method == http.MethodPost {
		if h.provider.Environment() == "mock" {
			h.notReady(w)
			return
		}
		var input domain.MediaFavoriteInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "收藏必须包含媒体 URL、播放历史 historyId 或网络电台 radioStationId 三者之一")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			h.writeError(w, http.StatusBadRequest, "invalid_request", "请求只能包含一个 JSON 对象")
			return
		}
		library, err := device.CreateMediaFavorite(r.Context(), input)
		if h.writeMediaLibraryError(w, err) {
			return
		}
		h.writeJSON(w, http.StatusCreated, domain.Envelope[domain.MediaLibrary]{Environment: h.provider.Environment(), Scenario: scenario, Data: library})
		return
	}

	if path == BasePath+"/media-library/history" && r.Method == http.MethodDelete {
		if h.provider.Environment() == "mock" {
			h.notReady(w)
			return
		}
		library, err := device.ClearMediaHistory(r.Context())
		if h.writeMediaLibraryError(w, err) {
			return
		}
		h.writeJSON(w, http.StatusOK, domain.Envelope[domain.MediaLibrary]{Environment: h.provider.Environment(), Scenario: scenario, Data: library})
		return
	}

	if strings.HasPrefix(path, BasePath+"/media-library/") {
		relative := strings.TrimPrefix(path, BasePath+"/media-library/")
		parts := strings.Split(relative, "/")
		if len(parts) == 2 && parts[1] != "" && r.Method == http.MethodDelete && (parts[0] == "favorites" || parts[0] == "history") {
			if h.provider.Environment() == "mock" {
				h.notReady(w)
				return
			}
			var library domain.MediaLibrary
			var err error
			if parts[0] == "favorites" {
				library, err = device.DeleteMediaFavorite(r.Context(), parts[1])
			} else {
				library, err = device.DeleteMediaHistory(r.Context(), parts[1])
			}
			if h.writeMediaLibraryError(w, err) {
				return
			}
			h.writeJSON(w, http.StatusOK, domain.Envelope[domain.MediaLibrary]{Environment: h.provider.Environment(), Scenario: scenario, Data: library})
			return
		}
		if len(parts) == 3 && parts[1] != "" && r.Method == http.MethodPost && (parts[0] == "favorites" || parts[0] == "history") && (parts[2] == "play" || parts[2] == "queue") {
			if h.provider.Environment() == "mock" {
				h.notReady(w)
				return
			}
			player, err := device.ControlMediaLibraryItem(r.Context(), parts[0], parts[1], parts[2])
			if h.writeMediaLibraryError(w, err) {
				return
			}
			h.writeJSON(w, http.StatusOK, domain.Envelope[domain.Player]{Environment: h.provider.Environment(), Scenario: scenario, Data: player})
			return
		}
	}

	if path == BasePath+"/scenes" && r.Method == http.MethodPost {
		if h.provider.Environment() == "mock" {
			h.notReady(w)
			return
		}
		input, ok := h.readPlayerSceneInput(w, r)
		if !ok {
			return
		}
		scenes, err := device.CreatePlayerScene(r.Context(), input)
		if h.writePlayerSceneError(w, err) {
			return
		}
		h.writeJSON(w, http.StatusCreated, domain.Envelope[[]domain.PlayerScene]{Environment: h.provider.Environment(), Scenario: scenario, Data: scenes})
		return
	}

	if strings.HasPrefix(path, BasePath+"/scenes/") {
		relative := strings.TrimPrefix(path, BasePath+"/scenes/")
		parts := strings.Split(relative, "/")
		if len(parts) == 1 && parts[0] != "" && (r.Method == http.MethodPut || r.Method == http.MethodDelete) {
			if h.provider.Environment() == "mock" {
				h.notReady(w)
				return
			}
			var scenes []domain.PlayerScene
			var err error
			if r.Method == http.MethodPut {
				input, ok := h.readPlayerSceneInput(w, r)
				if !ok {
					return
				}
				scenes, err = device.UpdatePlayerScene(r.Context(), parts[0], input)
			} else {
				scenes, err = device.DeletePlayerScene(r.Context(), parts[0])
			}
			if h.writePlayerSceneError(w, err) {
				return
			}
			h.writeJSON(w, http.StatusOK, domain.Envelope[[]domain.PlayerScene]{Environment: h.provider.Environment(), Scenario: scenario, Data: scenes})
			return
		}
		if len(parts) == 2 && parts[0] != "" && parts[1] == "apply" && r.Method == http.MethodPost {
			if h.provider.Environment() == "mock" {
				h.notReady(w)
				return
			}
			application, err := device.ApplyPlayerScene(r.Context(), parts[0])
			if h.writePlayerSceneError(w, err) {
				return
			}
			h.writeJSON(w, http.StatusOK, domain.Envelope[domain.PlayerSceneApplication]{Environment: h.provider.Environment(), Scenario: scenario, Data: application})
			return
		}
	}

	if isReservedWrite(path, r.Method) {
		h.notReady(w)
		return
	}

	h.writeError(w, http.StatusNotFound, "not_found", "接口不存在")
}

func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.methodNotAllowed(w, http.MethodPost)
		return
	}
	if h.provider.Environment() != "device" || h.updater == nil {
		h.writeError(w, http.StatusConflict, "update_unavailable", "当前运行模式未启用网页更新")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/vnd.sanyin.update+zip" {
		h.writeError(w, http.StatusUnsupportedMediaType, "invalid_update_media_type", "请选择 .sanyin-update 签名更新包")
		return
	}
	accepted, err := h.updater.Stage(r.Context(), r.Body, r.ContentLength)
	if err != nil {
		switch {
		case errors.Is(err, updater.ErrDisabled):
			h.writeError(w, http.StatusConflict, "update_disabled", "设备尚未配置网页更新公钥")
		case errors.Is(err, updater.ErrBusy):
			h.writeError(w, http.StatusConflict, "update_busy", "已有更新正在处理")
		case errors.Is(err, updater.ErrInvalidSignature):
			h.writeError(w, http.StatusForbidden, "invalid_update_signature", "更新包签名无效")
		case errors.Is(err, updater.ErrNotNewer):
			h.writeError(w, http.StatusConflict, "update_not_newer", err.Error())
		case errors.Is(err, updater.ErrInvalidPackage):
			h.writeError(w, http.StatusBadRequest, "invalid_update_package", err.Error())
		default:
			h.writeError(w, http.StatusInternalServerError, "update_failed", "无法暂存更新包")
		}
		return
	}
	h.writeJSON(w, http.StatusAccepted, domain.Envelope[domain.UpdateAccepted]{
		Environment: h.provider.Environment(),
		Scenario:    h.provider.DefaultScenario(),
		Data:        accepted,
	})
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

func (h *Handler) readPlayerSceneInput(w http.ResponseWriter, r *http.Request) (domain.PlayerSceneInput, bool) {
	var input domain.PlayerSceneInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "场景必须包含名称、图标、媒体 URL、音量和定时设置")
		return domain.PlayerSceneInput{}, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		h.writeError(w, http.StatusBadRequest, "invalid_request", "请求只能包含一个 JSON 对象")
		return domain.PlayerSceneInput{}, false
	}
	return input, true
}

func (h *Handler) writePlayerSceneError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, adapter.ErrCapabilityNotReady):
		h.notReady(w)
	case errors.Is(err, adapter.ErrInvalidInput):
		h.writeError(w, http.StatusBadRequest, "invalid_request", "场景名称为 1..40 个字符，媒体地址必须为 HTTP/HTTPS URL，音量为 0..100，停止定时为 0..60 分钟，自动启动时间和星期必须有效")
	case errors.Is(err, adapter.ErrNotFound):
		h.writeError(w, http.StatusNotFound, "scene_not_found", "场景不存在或已被删除")
	case errors.Is(err, adapter.ErrScheduleConflict):
		h.writeError(w, http.StatusConflict, "scene_schedule_conflict", "同一启动时间的执行星期与已有场景重叠，请调整时间或星期")
	case errors.Is(err, adapter.ErrConflict):
		h.writeError(w, http.StatusConflict, "scene_limit_reached", "最多保存 20 个场景，请先删除不再使用的场景")
	default:
		h.internalError(w)
	}
	return true
}

func (h *Handler) writeMediaLibraryError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, adapter.ErrCapabilityNotReady):
		h.notReady(w)
	case errors.Is(err, adapter.ErrInvalidInput):
		h.writeError(w, http.StatusBadRequest, "invalid_request", "媒体地址必须为有效的 HTTP/HTTPS URL，标题最长 100 个字符；收藏来源必须是 URL、播放历史或网络电台三者之一")
	case errors.Is(err, adapter.ErrNotFound):
		h.writeError(w, http.StatusNotFound, "media_library_item_not_found", "媒体库条目或网络电台不存在，可能已被删除")
	case errors.Is(err, adapter.ErrConflict):
		h.writeError(w, http.StatusConflict, "media_favorite_limit_reached", "最多保存 100 个 URL 收藏，请先删除不再使用的条目")
	default:
		h.internalError(w)
	}
	return true
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
	case BasePath + "/media-library":
		value, err = device.MediaLibrary(ctx)
	case BasePath + "/scenes":
		value, err = device.PlayerScenes(ctx)
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
