package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sanyin.local/config/service/internal/domain"
)

const DefaultScenario = "healthy"

var mockScenarios = []Scenario{
	{ID: "healthy", Name: "健康", Description: "核心服务在线，状态新鲜"},
	{ID: "airplay_down", Name: "AirPlay 异常", Description: "SPlayer 与 5002 端口不可用"},
	{ID: "wifi_offline", Name: "Wi-Fi 离线", Description: "无线网络断开且信号未知"},
	{ID: "controller_down", Name: "控制中心异常", Description: "控制中心离线，依赖状态降级"},
	{ID: "bluetooth_unknown", Name: "蓝牙未知", Description: "蓝牙服务在线，但开关状态无法确认"},
	{ID: "eq_pending", Name: "EQ 待应用", Description: "选中模式尚未应用至硬件"},
	{ID: "stale_state", Name: "状态陈旧", Description: "观测状态已超过有效期"},
	{ID: "operation_failed", Name: "操作失败", Description: "模拟写操作超时并完成回滚"},
}

type MockProvider struct {
	now func() time.Time
}

func NewMockProvider() *MockProvider {
	return &MockProvider{now: time.Now}
}

func NewMockProviderWithClock(now func() time.Time) *MockProvider {
	return &MockProvider{now: now}
}

func (p *MockProvider) Environment() string {
	return "mock"
}

func (p *MockProvider) DefaultScenario() string {
	return DefaultScenario
}

func (p *MockProvider) Scenarios() []Scenario {
	result := make([]Scenario, len(mockScenarios))
	copy(result, mockScenarios)
	return result
}

func (p *MockProvider) ForScenario(name string) (DeviceAdapter, error) {
	if name == "" {
		name = DefaultScenario
	}
	for index, scenario := range mockScenarios {
		if scenario.ID == name {
			return &MockAdapter{scenario: name, now: p.now, revision: uint64(index + 1)}, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknownScenario, name)
}

type MockAdapter struct {
	scenario string
	now      func() time.Time
	revision uint64
}

func (m *MockAdapter) observedAt() time.Time {
	now := m.now().Truncate(time.Second)
	if m.scenario == "stale_state" {
		return now.Add(-15 * time.Minute)
	}
	return now
}

func (m *MockAdapter) state(value any) domain.State {
	freshness := domain.Fresh
	if m.scenario == "stale_state" {
		freshness = domain.Stale
	}
	return domain.State{Value: value, ObservedAt: m.observedAt(), Source: domain.SourceMock, Freshness: freshness, Revision: m.revision}
}

func (m *MockAdapter) unknown() domain.State {
	state := m.state("unknown")
	state.Freshness = domain.UnknownFreshness
	return state
}

func (m *MockAdapter) Capabilities(_ context.Context) ([]domain.Capability, error) {
	capabilities := []domain.Capability{
		{ID: "device.info", Readability: domain.ReadFull, Writability: domain.WriteUnsupported, Availability: domain.Available},
		{ID: "device.health", Readability: domain.ReadFull, Writability: domain.WriteUnsupported, Availability: domain.Available},
		{ID: "player.localPlayback", Readability: domain.ReadFull, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "Mock 环境只展示播放器界面，不向真实 KPlayer 发送命令"},
		{ID: "airplay.runtime", Readability: domain.ReadFull, Writability: domain.WriteUnsupported, Availability: domain.Available},
		{ID: "airplay.recover", Readability: domain.ReadFull, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "当前仅提供模拟流程；本阶段不调用设备恢复服务"},
		{ID: "airplay.autoRecover", Readability: domain.ReadFull, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "Mock 环境不写入设备持久配置"},
		{ID: "wifi.connection", Readability: domain.ReadFull, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "扫描、切换、超时与回滚协议尚未验证"},
		{ID: "audio.volume", Readability: domain.ReadFull, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "系统、会话和硬件音量写入语义尚未完成验收"},
		{ID: "audio.outputMuted", Readability: domain.ReadFull, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "输出静音写入及回滚尚未验证"},
		{ID: "audio.micMuted", Readability: domain.ReadFull, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "仅确认实体按键路径，禁止原始 input 注入"},
		{ID: "audio.effect", Readability: domain.ReadFull, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "离线报告失败、服务异常和超时回滚尚未验证"},
		{ID: "bluetooth.enabled", Readability: domain.ReadPartial, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "缺少无副作用状态查询、连接中关闭、异常回滚和重启验证"},
		{ID: "lighting.iconEnabled", Readability: domain.ReadPartial, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "当前仅有脱敏快照，灯控协议与回读尚未验证"},
		{ID: "lighting.brightness", Readability: domain.ReadPartial, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "亮度范围与实时回读尚未验证"},
		{ID: "lighting.playMode", Readability: domain.ReadPartial, Writability: domain.WriteNotVerified, Availability: domain.Available, Reason: "模式语义与实时回读尚未验证"},
		{ID: "microphone.schedule", Readability: domain.ReadPartial, Writability: domain.WriteUnsupported, Availability: domain.Degraded, CloudDependency: true, Reason: "当前下发链依赖厂商通道，仅展示诊断快照"},
		{ID: "alarm.items", Readability: domain.ReadPartial, Writability: domain.WriteUnsupported, Availability: domain.Degraded, CloudDependency: true, Reason: "同步依赖厂商云"},
		{ID: "reminder.items", Readability: domain.ReadPartial, Writability: domain.WriteUnsupported, Availability: domain.Degraded, CloudDependency: true, Reason: "同步依赖厂商云"},
	}

	for i := range capabilities {
		switch {
		case m.scenario == "airplay_down" && (capabilities[i].ID == "airplay.runtime" || capabilities[i].ID == "airplay.recover"):
			capabilities[i].Availability = domain.Offline
		case m.scenario == "wifi_offline" && capabilities[i].ID == "wifi.connection":
			capabilities[i].Availability = domain.Offline
		case m.scenario == "controller_down" && (capabilities[i].ID == "device.health" || capabilities[i].ID == "audio.volume" || capabilities[i].ID == "audio.outputMuted" || capabilities[i].ID == "audio.micMuted" || capabilities[i].ID == "audio.effect"):
			capabilities[i].Availability = domain.Degraded
		case m.scenario == "bluetooth_unknown" && capabilities[i].ID == "bluetooth.enabled":
			capabilities[i].Availability = domain.Unknown
		}
	}
	return capabilities, nil
}

func (m *MockAdapter) Device(_ context.Context) (domain.Device, error) {
	return domain.Device{
		ProductFamily:       m.state("C930 系列"),
		Firmware:            m.state("1.1.17.4"),
		Platform:            m.state("OpenWrt / Tina Neptune"),
		StorageRemainingMiB: m.state(float64(46)),
	}, nil
}

func (m *MockAdapter) Status(_ context.Context) (domain.Status, error) {
	services := map[string]domain.State{
		"splayer":    m.state("online"),
		"kplayer":    m.state("online"),
		"controller": m.state("online"),
		"alarm":      m.state("online"),
		"bluetooth":  m.state("online"),
	}
	overall := m.state("healthy")
	player := m.state("idle")
	network := m.state("connected")
	audio := m.state("available")

	switch m.scenario {
	case "airplay_down":
		services["splayer"] = m.state("offline")
		overall = m.state("degraded")
	case "wifi_offline":
		network = m.state("offline")
		overall = m.state("degraded")
	case "controller_down":
		services["controller"] = m.state("offline")
		overall = m.state("degraded")
		player, audio = m.unknown(), m.unknown()
	}
	return domain.Status{Overall: overall, Services: services, Player: player, Network: network, Audio: audio}, nil
}

func (m *MockAdapter) AirPlay(_ context.Context) (domain.AirPlay, error) {
	result := domain.AirPlay{Runtime: m.state("running"), Port: m.state("listening"), RestoreService: m.state("observed_only"), AutoRecoverEnabled: m.state(true)}
	if m.scenario == "airplay_down" {
		result.Runtime = m.state("stopped")
		result.Port = m.state("closed")
	}
	return result, nil
}

func (m *MockAdapter) Network(_ context.Context) (domain.Network, error) {
	result := domain.Network{Connection: m.state("connected"), Signal: m.state("strong"), CurrentSSID: m.state("演示网络"), LastSwitchResult: m.state("none")}
	if m.scenario == "wifi_offline" {
		result.Connection = m.state("offline")
		result.Signal = m.unknown()
		result.CurrentSSID = m.unknown()
	}
	return result, nil
}

func (m *MockAdapter) Audio(_ context.Context) (domain.Audio, error) {
	result := domain.Audio{
		SystemVolume: m.state(float64(40)),
		OutputMuted:  m.state(false),
		Microphone:   m.state("active"),
		EQ:           domain.EQ{SelectedMode: m.state("smart"), AppliedMode: m.state("normal"), ApplyState: m.state("applied")},
	}
	if m.scenario == "controller_down" {
		result.SystemVolume, result.OutputMuted, result.Microphone = m.unknown(), m.unknown(), m.unknown()
		result.EQ = domain.EQ{SelectedMode: m.unknown(), AppliedMode: m.unknown(), ApplyState: m.unknown()}
	}
	if m.scenario == "eq_pending" {
		result.EQ = domain.EQ{SelectedMode: m.state("vocal"), AppliedMode: m.state("normal"), ApplyState: m.state("pending_local_playback")}
	}
	return result, nil
}

func (m *MockAdapter) Bluetooth(_ context.Context) (domain.Bluetooth, error) {
	result := domain.Bluetooth{Service: m.state("online"), Enabled: m.state(true), LastConfirmedEnabled: m.state(true)}
	if m.scenario == "bluetooth_unknown" || m.scenario == "controller_down" {
		result.Enabled = m.unknown()
	}
	return result, nil
}

func (m *MockAdapter) Lighting(_ context.Context) (domain.Lighting, error) {
	return domain.Lighting{IconEnabled: m.state(true), Brightness: m.state(float64(100)), PlayMode: m.state("snapshot_mode_2")}, nil
}

func (m *MockAdapter) Schedules(_ context.Context) (domain.Schedules, error) {
	return domain.Schedules{
		MicrophoneSchedule: m.state(map[string]any{"enabled": true, "window": "17:00–次日 06:30", "diagnosticOnly": true}),
		Alarms:             m.state(map[string]any{"count": float64(0), "cloudDependent": true}),
		Reminders:          m.state(map[string]any{"count": float64(0), "cloudDependent": true}),
	}, nil
}

func (m *MockAdapter) Player(_ context.Context) (domain.Player, error) {
	return domain.Player{
		Transport:       m.state("stopped"),
		Volume:          m.state(40),
		PositionSeconds: m.state(0),
		DurationSeconds: m.state(214),
		Current:         nil,
		Queue: []domain.MediaItem{
			{ID: "mock-media-1", Title: "局域网音乐示例", Source: "http://media.example/music.mp3", Kind: "url"},
		},
		CurrentIndex: -1,
		Stations: []domain.RadioStation{
			{ID: "mock-radio-1", Name: "网络电台示例", Source: "https://radio.example/live.mp3"},
		},
		StopTimer: domain.StopTimer{},
	}, nil
}

func (m *MockAdapter) ControlPlayer(_ context.Context, _ domain.PlayerCommand) (domain.Player, error) {
	return domain.Player{}, errors.New("real KPlayer control is unavailable in mock mode")
}

func (m *MockAdapter) Event(_ context.Context) (domain.Event, error) {
	return domain.Event{Type: "snapshot", Scenario: m.scenario, ObservedAt: m.observedAt(), Source: domain.SourceMock, Revision: m.revision, Changes: []string{"status", "airplay", "network", "audio", "bluetooth", "player"}}, nil
}

func (m *MockAdapter) SimulateOperation(_ context.Context, operation string) (domain.Operation, error) {
	steps := []domain.OperationStep{
		{State: "confirmed", Label: "已确认模拟操作"},
		{State: "running", Label: "模拟执行中"},
		{State: "verifying", Label: "等待模拟状态验收"},
	}
	result := domain.Operation{
		OperationID: fmt.Sprintf("mock-%s-%d", operation, m.revision), Simulation: true,
		Applied: true, Verified: true, Outcome: "succeeded", Message: "模拟操作已完成；未连接或修改任何设备",
		Timeline: append(steps, domain.OperationStep{State: "succeeded", Label: "模拟验收成功"}),
	}
	if m.scenario == "operation_failed" {
		result.Applied, result.Verified, result.RollbackAttempted = false, false, true
		result.Outcome, result.Message = "rolled_back", "模拟操作超时，已完成模拟回滚；未连接或修改任何设备"
		result.Timeline = append(steps,
			domain.OperationStep{State: "failed", Label: "模拟验收超时"},
			domain.OperationStep{State: "rolling_back", Label: "模拟回滚中"},
			domain.OperationStep{State: "restored", Label: "模拟状态已恢复"},
		)
	}
	return result, nil
}

func (m *MockAdapter) RecoverAirPlay(_ context.Context) (domain.Operation, error) {
	return domain.Operation{}, errors.New("real operation is unavailable in mock mode")
}

func (m *MockAdapter) SetAirPlayAutoRecover(_ context.Context, _ bool) (domain.Operation, error) {
	return domain.Operation{}, errors.New("real configuration is unavailable in mock mode")
}

func (m *MockAdapter) SetBluetooth(_ context.Context, _ bool) (domain.Operation, error) {
	return domain.Operation{}, errors.New("real Bluetooth configuration is unavailable in mock mode")
}

func (m *MockAdapter) SetEQ(_ context.Context, _ int) (domain.Operation, error) {
	return domain.Operation{}, errors.New("real EQ configuration is unavailable in mock mode")
}

func (m *MockAdapter) SetWiFi(_ context.Context, _, _ string) (domain.Operation, error) {
	return domain.Operation{}, errors.New("real Wi-Fi configuration is unavailable in mock mode")
}
