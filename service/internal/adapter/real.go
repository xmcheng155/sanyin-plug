package adapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"sanyin.local/config/service/internal/domain"
)

const LiveScenario = "live"

// ShellRunner executes fixed, application-owned shell probes. Callers must not
// concatenate browser input into scripts passed to this interface.
type ShellRunner interface {
	Run(context.Context, string) (string, error)
}

type deviceFileStore interface {
	WriteDeviceFile(context.Context, string, []byte, os.FileMode) error
	RemoveDeviceFile(string) error
}

// ExecShellRunner supports both ADB-connected development and direct execution
// on the speaker. Arguments are passed without an intermediate host shell.
type ExecShellRunner struct {
	command string
	args    []string
	local   bool
}

func NewADBShellRunner(adbPath, serial string) *ExecShellRunner {
	return &ExecShellRunner{command: adbPath, args: []string{"-s", serial, "shell"}}
}

func NewSSHShellRunner(sshPath, user, host, identity, knownHosts string, port int) *ExecShellRunner {
	args := []string{"-p", strconv.Itoa(port), "-o", "BatchMode=yes", "-o", "ConnectTimeout=8"}
	if identity != "" {
		args = append(args, "-o", "IdentitiesOnly=yes", "-i", identity)
	}
	if knownHosts != "" {
		args = append(args, "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile="+knownHosts)
	}
	args = append(args, user+"@"+host)
	return &ExecShellRunner{command: sshPath, args: args}
}

func NewLocalShellRunner() *ExecShellRunner {
	return &ExecShellRunner{command: "/bin/sh", args: []string{"-c"}, local: true}
}

func (r *ExecShellRunner) Run(ctx context.Context, script string) (string, error) {
	args := append(append([]string{}, r.args...), script)
	command := exec.CommandContext(ctx, r.command, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("device shell: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.ReplaceAll(string(output), "\r", ""), nil
}

func (r *ExecShellRunner) WriteDeviceFile(ctx context.Context, filename string, content []byte, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validManagedDevicePath(filename) {
		return errors.New("write device file: path is outside managed config directory")
	}
	temporary := filename + ".tmp"
	if !r.local {
		script := fmt.Sprintf("umask 077; cat > '%s' && chmod %04o '%s' && mv '%s' '%s'",
			temporary, mode.Perm(), temporary, temporary, filename)
		args := append(append([]string{}, r.args...), script)
		command := exec.CommandContext(ctx, r.command, args...)
		command.Stdin = bytes.NewReader(content)
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("write device file: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	if err := os.WriteFile(temporary, content, mode); err != nil {
		return fmt.Errorf("write device file: %w", err)
	}
	if err := os.Chmod(temporary, mode); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("chmod device file: %w", err)
	}
	if err := os.Rename(temporary, filename); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("install device file: %w", err)
	}
	return nil
}

func (r *ExecShellRunner) RemoveDeviceFile(filename string) error {
	if !validManagedDevicePath(filename) {
		return errors.New("remove device file: path is outside managed config directory")
	}
	if !r.local {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		args := append(append([]string{}, r.args...), fmt.Sprintf("rm -f '%s'", filename))
		output, err := exec.CommandContext(ctx, r.command, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("remove device file: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	return os.Remove(filename)
}

func (r *ExecShellRunner) ReadDeviceFile(ctx context.Context, filename string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validManagedDevicePath(filename) {
		return nil, errors.New("read device file: path is outside managed config directory")
	}
	if !r.local {
		args := append(append([]string{}, r.args...), fmt.Sprintf("cat '%s'", filename))
		output, err := exec.CommandContext(ctx, r.command, args...).Output()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				return nil, fmt.Errorf("read device file: %w: %s", err, strings.TrimSpace(string(exit.Stderr)))
			}
			return nil, fmt.Errorf("read device file: %w", err)
		}
		return output, nil
	}
	return os.ReadFile(filename)
}

func (r *ExecShellRunner) DeviceHTTPHost(ctx context.Context) (string, error) {
	if r.local {
		return "127.0.0.1", nil
	}
	output, err := r.Run(ctx, `address="$(ifconfig wlan0 2>/dev/null | sed -n 's/.*inet addr:\([^ ]*\).*/\1/p' | head -n 1)"; printf 'device_ip=%s\n' "$address"`)
	if err != nil {
		return "", err
	}
	address := parseKeyValues(output)["device_ip"]
	if net.ParseIP(address) == nil {
		return "", errors.New("device Wi-Fi address is unavailable")
	}
	return address, nil
}

func validManagedDevicePath(filename string) bool {
	const prefix = "/mnt/UDISK/sanyin-config/"
	if !strings.HasPrefix(filename, prefix) {
		return false
	}
	name := strings.TrimPrefix(filename, prefix)
	if name == "" || strings.Contains(name, "/") {
		return false
	}
	for _, char := range name {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '.' && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

type RealProvider struct {
	device *RealAdapter
}

func NewRealProvider(runner ShellRunner) *RealProvider {
	return NewRealProviderWithClock(runner, time.Now)
}

func NewRealProviderWithClock(runner ShellRunner, now func() time.Time) *RealProvider {
	device := &RealAdapter{runner: runner, now: now, cacheTTL: time.Second}
	device.playback = newPlaybackManager(newUPnPPlayer(runner), runner, now)
	return &RealProvider{device: device}
}

func (p *RealProvider) Environment() string {
	return "device"
}

// StartBackgroundTasks is called by the executable after the provider has
// completed its startup probes. Keeping lifecycle start explicit avoids
// surprising goroutines for API consumers that only construct a provider.
func (p *RealProvider) StartBackgroundTasks() {
	p.device.playback.startSceneScheduler()
}

func (p *RealProvider) DefaultScenario() string {
	return LiveScenario
}

func (p *RealProvider) Scenarios() []Scenario {
	return []Scenario{{ID: LiveScenario, Name: "真实设备", Description: "通过设备适配层读取当前音箱状态"}}
}

func (p *RealProvider) ForScenario(_ string) (DeviceAdapter, error) {
	return p.device, nil
}

type deviceSnapshot struct {
	observedAt time.Time
	revision   uint64
	values     map[string]string
}

type RealAdapter struct {
	runner   ShellRunner
	now      func() time.Time
	cacheTTL time.Duration
	playback *playbackManager

	mu          sync.Mutex
	cached      deviceSnapshot
	revision    uint64
	operationMu sync.Mutex
}

const snapshotScript = `
printf 'product=C930 系列\n'
firmware="$(cat /etc/openwrt_version 2>/dev/null | head -n 1)"
platform="$(sed -n "s/^DISTRIB_DESCRIPTION='\(.*\)'/\1/p" /etc/openwrt_release 2>/dev/null | head -n 1)"
storage_mib="$(df -k /mnt/UDISK 2>/dev/null | awk 'NR == 2 { print int($4 / 1024) }')"
printf 'firmware=%s\n' "$firmware"
printf 'platform=%s\n' "$platform"
printf 'storage_mib=%s\n' "$storage_mib"
for item in 'splayer:SPlayer' 'kplayer:KPlayer' 'controller:netease_control_center' 'alarm:alarmer' 'bluetooth:app_nevsps_bt' 'player:ihwplayer' 'restore:airplay_restore.sh'; do
  key="${item%%:*}"
  process="${item#*:}"
  if pidof "$process" >/dev/null 2>&1; then value=online; else value=offline; fi
  printf '%s=%s\n' "$key" "$value"
done
if grep -q 'airplay-auto-recover' /usr/bin/airplay_restore.sh 2>/dev/null; then printf 'autorecover_supported=true\n'; else printf 'autorecover_supported=false\n'; fi
if [ -f /mnt/UDISK/sanyin-config/airplay-auto-recover ] && grep -qx 'disabled' /mnt/UDISK/sanyin-config/airplay-auto-recover; then printf 'autorecover=disabled\n'; else printf 'autorecover=enabled\n'; fi
if [ -f /mnt/UDISK/sanyin-config/bluetooth-last-confirmed ]; then
  case "$(cat /mnt/UDISK/sanyin-config/bluetooth-last-confirmed)" in enabled|disabled) sed 's/^/bluetooth_last=/' /mnt/UDISK/sanyin-config/bluetooth-last-confirmed ;; *) printf 'bluetooth_last=unknown\n' ;; esac
else
  printf 'bluetooth_last=unknown\n'
fi
if [ -x /usr/bin/sanyin_eq_probe.sh ]; then printf 'eq_probe_supported=true\n'; else printf 'eq_probe_supported=false\n'; fi
if [ -x /usr/bin/sanyin_wifi_switch.sh ]; then printf 'wifi_switch_supported=true\n'; else printf 'wifi_switch_supported=false\n'; fi
if wpa_cli -p /mnt/UDISK/wifi/sockets -i wlan0 ping 2>/dev/null | grep -qx PONG; then printf 'wifi_control=online\n'; else printf 'wifi_control=offline\n'; fi
network_ssid="$(wpa_cli -p /mnt/UDISK/wifi/sockets -i wlan0 status 2>/dev/null | sed -n 's/^ssid=//p' | head -n 1)"
printf 'network_ssid=%s\n' "$network_ssid"
if [ -f /mnt/UDISK/sanyin-config/wifi-last-result ]; then
  case "$(cat /mnt/UDISK/sanyin-config/wifi-last-result)" in succeeded|rolled_back|rollback_failed|recovered_after_restart) sed 's/^/wifi_last=/' /mnt/UDISK/sanyin-config/wifi-last-result ;; *) printf 'wifi_last=unknown\n' ;; esac
else
  printf 'wifi_last=unknown\n'
fi
if [ -f /mnt/UDISK/sanyin-config/eq-last-confirmed ]; then
  case "$(cat /mnt/UDISK/sanyin-config/eq-last-confirmed)" in 0|1|2|3|4|5|6) sed 's/^/eq_last=/' /mnt/UDISK/sanyin-config/eq-last-confirmed ;; *) printf 'eq_last=unknown\n' ;; esac
else
  printf 'eq_last=unknown\n'
fi
eq_applied=unknown
if [ -f /lib/firmware/adau1761.bin ]; then
  if cmp -s /lib/firmware/adau1761.bin /usr/share/golang/adau1761/Normal.bin; then eq_applied=normal
  elif cmp -s /lib/firmware/adau1761.bin /usr/share/golang/adau1761/Vocal.bin; then eq_applied=vocal
  elif cmp -s /lib/firmware/adau1761.bin /usr/share/golang/adau1761/Live.bin; then eq_applied=live
  elif cmp -s /lib/firmware/adau1761.bin /usr/share/golang/adau1761/double_bass.bin; then eq_applied=double_bass
  elif cmp -s /lib/firmware/adau1761.bin /usr/share/golang/adau1761/Electronic_music.bin; then eq_applied=electronic_music
  elif cmp -s /lib/firmware/adau1761.bin /usr/share/golang/adau1761/ACG.bin; then eq_applied=acg
  fi
fi
printf 'eq_applied=%s\n' "$eq_applied"
if netstat -lnt 2>/dev/null | grep -q ':5002 '; then printf 'airplay_port=listening\n'; else printf 'airplay_port=closed\n'; fi
if ifconfig wlan0 2>/dev/null | grep -q 'inet addr:'; then printf 'network=connected\n'; else printf 'network=offline\n'; fi
signal_dbm="$(iwconfig wlan0 2>/dev/null | sed -n 's/.*Signal level=\(-*[0-9]*\).*/\1/p' | head -n 1)"
case "$signal_dbm" in
  '') signal=unknown ;;
  *) if [ "$signal_dbm" -ge -55 ]; then signal=strong; elif [ "$signal_dbm" -ge -70 ]; then signal=medium; else signal=weak; fi ;;
esac
printf 'signal=%s\n' "$signal"
`

func (m *RealAdapter) snapshot(ctx context.Context) (deviceSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now().Truncate(time.Second)
	if !m.cached.observedAt.IsZero() && now.Sub(m.cached.observedAt) <= m.cacheTTL {
		return m.cached, nil
	}
	output, err := m.runner.Run(ctx, snapshotScript)
	if err != nil {
		return deviceSnapshot{}, err
	}
	values := parseKeyValues(output)
	if values["product"] == "" || values["network"] == "" || values["airplay_port"] == "" {
		return deviceSnapshot{}, errors.New("device snapshot is incomplete")
	}
	m.revision++
	m.cached = deviceSnapshot{observedAt: now, revision: m.revision, values: values}
	return m.cached, nil
}

func parseKeyValues(output string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && key != "" {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values
}

func state(snapshot deviceSnapshot, value any) domain.State {
	return domain.State{Value: value, ObservedAt: snapshot.observedAt, Source: domain.SourceSystem, Freshness: domain.Fresh, Revision: snapshot.revision}
}

func unknownState(snapshot deviceSnapshot) domain.State {
	return domain.State{Value: "unknown", ObservedAt: snapshot.observedAt, Source: domain.SourceSystem, Freshness: domain.UnknownFreshness, Revision: snapshot.revision}
}

func derivedStaleState(snapshot deviceSnapshot, value any) domain.State {
	return domain.State{Value: value, ObservedAt: snapshot.observedAt, Source: domain.SourceDerived, Freshness: domain.Stale, Revision: snapshot.revision}
}

func (m *RealAdapter) Capabilities(ctx context.Context) ([]domain.Capability, error) {
	snapshot, err := m.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	available := func(key string) domain.Availability {
		if snapshot.values[key] == "online" {
			return domain.Available
		}
		return domain.Offline
	}
	return []domain.Capability{
		{ID: "device.info", Readability: domain.ReadFull, Writability: domain.WriteUnsupported, Availability: domain.Available},
		{ID: "device.health", Readability: domain.ReadFull, Writability: domain.WriteUnsupported, Availability: domain.Available},
		{ID: "player.localPlayback", Readability: domain.ReadFull, Writability: domain.WriteExperimental, Availability: available("kplayer"), Reason: "URL 播放、RenderingControl 音量及原厂音频链路已实机验收；暂停、恢复、队列、电台及异常路径由本地服务管理并保持实验标识"},
		{ID: "airplay.runtime", Readability: domain.ReadFull, Writability: domain.WriteUnsupported, Availability: available("splayer")},
		{ID: "airplay.recover", Readability: domain.ReadFull, Writability: domain.WriteSafe, Availability: available("splayer"), Reason: "原生启动命令、端口验收、幂等与重启恢复已通过实机验证"},
		autoRecoverCapability(snapshot),
		wifiCapability(snapshot),
		{ID: "audio.volume", Readability: domain.ReadPartial, Writability: domain.WriteNotVerified, Availability: available("player"), Reason: "业务音量入口和重启状态一致性尚未完成验收"},
		{ID: "audio.outputMuted", Readability: domain.ReadPartial, Writability: domain.WriteNotVerified, Availability: available("controller"), Reason: "输出静音写入及回滚尚未验证"},
		{ID: "audio.micMuted", Readability: domain.ReadPartial, Writability: domain.WriteNotVerified, Availability: available("controller"), Reason: "当前没有可安全调用的业务协议"},
		eqCapability(snapshot),
		{ID: "bluetooth.enabled", Readability: domain.ReadPartial, Writability: domain.WriteExperimental, Availability: available("bluetooth"), Reason: "开关命令与成功事件已实机验收；当前值仍缺无副作用查询，连接中关闭和异常回滚待验证"},
		{ID: "lighting.iconEnabled", Readability: domain.ReadPartial, Writability: domain.WriteNotVerified, Availability: domain.Unknown, Reason: "灯控协议与实时回读尚未验证"},
		{ID: "lighting.brightness", Readability: domain.ReadPartial, Writability: domain.WriteNotVerified, Availability: domain.Unknown, Reason: "亮度范围与实时回读尚未验证"},
		{ID: "lighting.playMode", Readability: domain.ReadPartial, Writability: domain.WriteNotVerified, Availability: domain.Unknown, Reason: "模式语义与实时回读尚未验证"},
		{ID: "microphone.schedule", Readability: domain.ReadPartial, Writability: domain.WriteUnsupported, Availability: domain.Degraded, CloudDependency: true, Reason: "当前下发链依赖厂商通道，仅保留诊断能力"},
		{ID: "alarm.items", Readability: domain.ReadPartial, Writability: domain.WriteUnsupported, Availability: domain.Degraded, CloudDependency: true, Reason: "同步依赖厂商云"},
		{ID: "reminder.items", Readability: domain.ReadPartial, Writability: domain.WriteUnsupported, Availability: domain.Degraded, CloudDependency: true, Reason: "同步依赖厂商云"},
	}, nil
}

func wifiCapability(snapshot deviceSnapshot) domain.Capability {
	capability := domain.Capability{ID: "wifi.connection", Readability: domain.ReadFull, Writability: domain.WriteExperimental, Availability: availabilityForValue(snapshot.values["wifi_control"]), Reason: "切换使用独立事务、目标 SSID/IPv4/默认路由/网关可达验收和 45 秒失败自动回退；新网络与整机断电场景仍需继续验收"}
	if snapshot.values["wifi_switch_supported"] != "true" {
		capability.Writability = domain.WriteNotVerified
		capability.Availability = domain.Degraded
		capability.Reason = "设备尚未安装 Wi-Fi 切换与启动恢复脚本"
	}
	return capability
}

func eqCapability(snapshot deviceSnapshot) domain.Capability {
	capability := domain.Capability{ID: "audio.effect", Readability: domain.ReadPartial, Writability: domain.WriteExperimental, Availability: availabilityForValue(snapshot.values["controller"]), Reason: "模式 0..6、选中态事件和硬件文件映射已验证；离线云端报告、服务异常与自动回滚仍待完整验收"}
	if snapshot.values["eq_probe_supported"] != "true" {
		capability.Writability = domain.WriteNotVerified
		capability.Availability = domain.Degraded
		capability.Reason = "设备尚未安装 EQ 事件验收脚本"
	}
	return capability
}

func autoRecoverCapability(snapshot deviceSnapshot) domain.Capability {
	capability := domain.Capability{ID: "airplay.autoRecover", Readability: domain.ReadFull, Writability: domain.WriteSafe, Availability: domain.Available, Reason: "自有配置原子写入、回读验收和默认兼容行为已验证"}
	if snapshot.values["restore"] != "online" {
		capability.Availability = domain.Offline
		capability.Reason = "AirPlay 恢复服务当前离线"
	}
	if snapshot.values["autorecover_supported"] != "true" {
		capability.Writability = domain.WriteNotVerified
		capability.Availability = domain.Degraded
		capability.Reason = "设备上的恢复脚本版本尚不支持可配置开关"
	}
	return capability
}

func availabilityForValue(value string) domain.Availability {
	switch value {
	case "online", "connected", "listening":
		return domain.Available
	case "offline", "closed":
		return domain.Offline
	default:
		return domain.Unknown
	}
}

func (m *RealAdapter) Device(ctx context.Context) (domain.Device, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return domain.Device{}, err
	}
	storage, parseErr := strconv.ParseFloat(s.values["storage_mib"], 64)
	storageState := unknownState(s)
	if parseErr == nil {
		storageState = state(s, storage)
	}
	valueState := func(key string) domain.State {
		if s.values[key] == "" {
			return unknownState(s)
		}
		return state(s, s.values[key])
	}
	return domain.Device{
		ProductFamily:       valueState("product"),
		Firmware:            valueState("firmware"),
		Platform:            valueState("platform"),
		StorageRemainingMiB: storageState,
	}, nil
}

func (m *RealAdapter) Status(ctx context.Context) (domain.Status, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return domain.Status{}, err
	}
	services := map[string]domain.State{}
	for _, key := range []string{"splayer", "kplayer", "controller", "alarm", "bluetooth"} {
		services[key] = state(s, s.values[key])
	}
	overall := "healthy"
	if s.values["controller"] != "online" || s.values["network"] != "connected" || s.values["airplay_port"] != "listening" {
		overall = "degraded"
	}
	player := unknownState(s)
	if s.values["player"] != "online" {
		player = state(s, "offline")
	} else if m.playback != nil {
		playback, playbackErr := m.playback.status(ctx)
		if playbackErr == nil {
			player = playback.Transport
		}
	}
	audio := state(s, "available")
	if s.values["player"] != "online" || s.values["controller"] != "online" {
		audio = state(s, "degraded")
	}
	return domain.Status{Overall: state(s, overall), Services: services, Player: player, Network: state(s, s.values["network"]), Audio: audio}, nil
}

func (m *RealAdapter) AirPlay(ctx context.Context) (domain.AirPlay, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return domain.AirPlay{}, err
	}
	runtime := "running"
	if s.values["splayer"] != "online" || s.values["airplay_port"] != "listening" {
		runtime = "stopped"
	}
	restore := "running"
	if s.values["restore"] != "online" {
		restore = "offline"
	}
	autoRecover := unknownState(s)
	if s.values["autorecover_supported"] == "true" {
		autoRecover = state(s, s.values["autorecover"] == "enabled")
	}
	return domain.AirPlay{Runtime: state(s, runtime), Port: state(s, s.values["airplay_port"]), RestoreService: state(s, restore), AutoRecoverEnabled: autoRecover}, nil
}

func (m *RealAdapter) Network(ctx context.Context) (domain.Network, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return domain.Network{}, err
	}
	signal := state(s, s.values["signal"])
	if s.values["signal"] == "unknown" {
		signal = unknownState(s)
	}
	ssid := unknownState(s)
	if s.values["network"] == "connected" && s.values["network_ssid"] != "" {
		ssid = state(s, s.values["network_ssid"])
	}
	lastSwitch := unknownState(s)
	if s.values["wifi_last"] != "" && s.values["wifi_last"] != "unknown" {
		lastSwitch = derivedStaleState(s, s.values["wifi_last"])
	}
	return domain.Network{Connection: state(s, s.values["network"]), Signal: signal, CurrentSSID: ssid, LastSwitchResult: lastSwitch}, nil
}

func (m *RealAdapter) Audio(ctx context.Context) (domain.Audio, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return domain.Audio{}, err
	}
	selected := unknownState(s)
	selectedMode, selectedOK := eqModeName(s.values["eq_last"])
	if selectedOK {
		selected = derivedStaleState(s, selectedMode)
	}
	applied := unknownState(s)
	if s.values["eq_applied"] != "" && s.values["eq_applied"] != "unknown" {
		applied = state(s, s.values["eq_applied"])
	}
	applyState := unknownState(s)
	if selectedOK && applied.Value != "unknown" {
		value := "pending_local_playback"
		if eqHardwareMode(selectedMode) == applied.Value {
			value = "applied"
		}
		applyState = state(s, value)
	}
	return domain.Audio{SystemVolume: unknownState(s), OutputMuted: unknownState(s), Microphone: unknownState(s), EQ: domain.EQ{SelectedMode: selected, AppliedMode: applied, ApplyState: applyState}}, nil
}

func eqModeName(value string) (string, bool) {
	names := map[string]string{"0": "normal", "1": "smart", "2": "vocal", "3": "live", "4": "double_bass", "5": "electronic_music", "6": "acg"}
	name, ok := names[value]
	return name, ok
}

func eqHardwareMode(mode string) string {
	if mode == "smart" {
		return "normal"
	}
	return mode
}

func (m *RealAdapter) Bluetooth(ctx context.Context) (domain.Bluetooth, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return domain.Bluetooth{}, err
	}
	lastConfirmed := unknownState(s)
	if s.values["bluetooth_last"] == "enabled" || s.values["bluetooth_last"] == "disabled" {
		lastConfirmed = derivedStaleState(s, s.values["bluetooth_last"] == "enabled")
	}
	return domain.Bluetooth{Service: state(s, s.values["bluetooth"]), Enabled: unknownState(s), LastConfirmedEnabled: lastConfirmed}, nil
}

func (m *RealAdapter) Lighting(ctx context.Context) (domain.Lighting, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return domain.Lighting{}, err
	}
	return domain.Lighting{IconEnabled: unknownState(s), Brightness: unknownState(s), PlayMode: unknownState(s)}, nil
}

func (m *RealAdapter) Schedules(ctx context.Context) (domain.Schedules, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return domain.Schedules{}, err
	}
	return domain.Schedules{MicrophoneSchedule: unknownState(s), Alarms: unknownState(s), Reminders: unknownState(s)}, nil
}

func (m *RealAdapter) Player(ctx context.Context) (domain.Player, error) {
	if m.playback == nil {
		return domain.Player{}, ErrCapabilityNotReady
	}
	return m.playback.status(ctx)
}

func (m *RealAdapter) ControlPlayer(ctx context.Context, command domain.PlayerCommand) (domain.Player, error) {
	if m.playback == nil {
		return domain.Player{}, ErrCapabilityNotReady
	}
	return m.playback.control(ctx, command)
}

func (m *RealAdapter) PlayerScenes(ctx context.Context) ([]domain.PlayerScene, error) {
	if m.playback == nil {
		return nil, ErrCapabilityNotReady
	}
	return m.playback.listScenes(ctx)
}

func (m *RealAdapter) CreatePlayerScene(ctx context.Context, input domain.PlayerSceneInput) ([]domain.PlayerScene, error) {
	if m.playback == nil {
		return nil, ErrCapabilityNotReady
	}
	return m.playback.createScene(ctx, input)
}

func (m *RealAdapter) UpdatePlayerScene(ctx context.Context, id string, input domain.PlayerSceneInput) ([]domain.PlayerScene, error) {
	if m.playback == nil {
		return nil, ErrCapabilityNotReady
	}
	return m.playback.updateScene(ctx, id, input)
}

func (m *RealAdapter) DeletePlayerScene(ctx context.Context, id string) ([]domain.PlayerScene, error) {
	if m.playback == nil {
		return nil, ErrCapabilityNotReady
	}
	return m.playback.deleteScene(ctx, id)
}

func (m *RealAdapter) ApplyPlayerScene(ctx context.Context, id string) (domain.PlayerSceneApplication, error) {
	if m.playback == nil {
		return domain.PlayerSceneApplication{}, ErrCapabilityNotReady
	}
	return m.playback.applyScene(ctx, id)
}

func (m *RealAdapter) Event(ctx context.Context) (domain.Event, error) {
	s, err := m.snapshot(ctx)
	if err != nil {
		return domain.Event{}, err
	}
	return domain.Event{Type: "snapshot", Scenario: LiveScenario, ObservedAt: s.observedAt, Source: domain.SourceSystem, Revision: s.revision, Changes: []string{"status", "airplay", "network", "audio", "bluetooth", "player"}}, nil
}

func (m *RealAdapter) SimulateOperation(_ context.Context, _ string) (domain.Operation, error) {
	return domain.Operation{}, errors.New("simulation is unavailable in device mode")
}

const recoverAirPlayScript = `
if netstat -lnt 2>/dev/null | grep -q ':5002 '; then
  printf 'result=already_listening\n'
  exit 0
fi
if [ ! -s /tmp/dbus_env.sh ] || ! pidof SPlayer >/dev/null 2>&1; then
  printf 'result=not_ready\n'
  exit 0
fi
. /tmp/dbus_env.sh || { printf 'result=not_ready\n'; exit 0; }
payload='{"port":5002,"mac":"112233445577","device":"三音云音箱-C931"}'
if ! /usr/bin/dbus-send --session --type=method_call --dest=netease.ihw.splayer /netease/ihw/splayer netease.ihw.SmartAudio.API uint32:0 uint32:2048 uint32:7425 uint32:66 string:"$payload"; then
  printf 'result=send_failed\n'
  exit 0
fi
attempt=0
while [ "$attempt" -lt 10 ]; do
  if netstat -lnt 2>/dev/null | grep -q ':5002 '; then
    printf 'result=recovered\n'
    exit 0
  fi
  attempt=$((attempt + 1))
  sleep 1
done
printf 'result=verification_timeout\n'
`

func (m *RealAdapter) RecoverAirPlay(ctx context.Context) (domain.Operation, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()

	operation := domain.Operation{
		OperationID: fmt.Sprintf("device-airplay-%d", m.now().UnixMilli()),
		Simulation:  false,
		Timeline: []domain.OperationStep{
			{State: "confirmed", Label: "已确认真实设备操作"},
			{State: "running", Label: "发送原生 AirPlay 恢复命令"},
			{State: "verifying", Label: "验收 TCP 5002 监听状态"},
		},
	}
	output, err := m.runner.Run(ctx, recoverAirPlayScript)
	if err != nil {
		return domain.Operation{}, err
	}
	result := parseKeyValues(output)["result"]
	switch result {
	case "already_listening":
		operation.Verified = true
		operation.Outcome = "succeeded"
		operation.Message = "AirPlay 已在监听，无需重复修改设备"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "succeeded", Label: "端口已处于可用状态"})
	case "recovered":
		operation.Applied = true
		operation.Verified = true
		operation.Outcome = "succeeded"
		operation.Message = "AirPlay 已恢复并通过 TCP 5002 监听验收"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "succeeded", Label: "真实设备验收成功"})
	case "not_ready":
		operation.Outcome = "failed"
		operation.Message = "设备 D-Bus 环境或 SPlayer 尚未就绪，未修改设备"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "设备尚未就绪"})
	case "send_failed":
		operation.Outcome = "failed"
		operation.Message = "原生 AirPlay 恢复命令发送失败"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "命令发送失败"})
	case "verification_timeout":
		operation.Applied = true
		operation.Outcome = "failed"
		operation.Message = "命令已发送，但 TCP 5002 未在限定时间内开始监听"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "端口验收超时"})
	default:
		return domain.Operation{}, fmt.Errorf("unexpected AirPlay recovery result %q", result)
	}

	m.mu.Lock()
	m.cached = deviceSnapshot{}
	m.mu.Unlock()
	return operation, nil
}

const setAutoRecoverEnabledScript = `
if ! grep -q 'airplay-auto-recover' /usr/bin/airplay_restore.sh 2>/dev/null; then printf 'result=not_supported\n'; exit 0; fi
config_dir=/mnt/UDISK/sanyin-config
config_file=$config_dir/airplay-auto-recover
temp_file=$config_dir/.airplay-auto-recover.tmp
mkdir -p "$config_dir" || { printf 'result=write_failed\n'; exit 0; }
if [ -f "$config_file" ]; then old_value="$(cat "$config_file")"; old_exists=true; else old_value=enabled; old_exists=false; fi
printf 'enabled\n' > "$temp_file" && mv "$temp_file" "$config_file" || { rm -f "$temp_file"; printf 'result=write_failed\n'; exit 0; }
if grep -qx 'enabled' "$config_file"; then printf 'result=configuration_updated\n'; exit 0; fi
if [ "$old_exists" = true ]; then printf '%s\n' "$old_value" > "$temp_file" && mv "$temp_file" "$config_file"; else rm -f "$config_file"; fi
printf 'result=verification_failed\n'
`

const setAutoRecoverDisabledScript = `
if ! grep -q 'airplay-auto-recover' /usr/bin/airplay_restore.sh 2>/dev/null; then printf 'result=not_supported\n'; exit 0; fi
config_dir=/mnt/UDISK/sanyin-config
config_file=$config_dir/airplay-auto-recover
temp_file=$config_dir/.airplay-auto-recover.tmp
mkdir -p "$config_dir" || { printf 'result=write_failed\n'; exit 0; }
if [ -f "$config_file" ]; then old_value="$(cat "$config_file")"; old_exists=true; else old_value=enabled; old_exists=false; fi
printf 'disabled\n' > "$temp_file" && mv "$temp_file" "$config_file" || { rm -f "$temp_file"; printf 'result=write_failed\n'; exit 0; }
if grep -qx 'disabled' "$config_file"; then printf 'result=configuration_updated\n'; exit 0; fi
if [ "$old_exists" = true ]; then printf '%s\n' "$old_value" > "$temp_file" && mv "$temp_file" "$config_file"; else rm -f "$config_file"; fi
printf 'result=verification_failed\n'
`

func (m *RealAdapter) SetAirPlayAutoRecover(ctx context.Context, enabled bool) (domain.Operation, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()

	label := "关闭"
	script := setAutoRecoverDisabledScript
	if enabled {
		label = "开启"
		script = setAutoRecoverEnabledScript
	}
	operation := domain.Operation{
		OperationID: fmt.Sprintf("device-autorecover-%d", m.now().UnixMilli()),
		Simulation:  false,
		Timeline: []domain.OperationStep{
			{State: "confirmed", Label: "已确认真实配置变更"},
			{State: "running", Label: label + " AirPlay 自动恢复"},
			{State: "verifying", Label: "回读自有配置文件"},
		},
	}
	output, err := m.runner.Run(ctx, script)
	if err != nil {
		return domain.Operation{}, err
	}
	result := parseKeyValues(output)["result"]
	switch result {
	case "configuration_updated":
		operation.Applied = true
		operation.Verified = true
		operation.Outcome = "succeeded"
		operation.Message = "AirPlay 自动恢复已" + label + "，配置已持久化并通过回读验收"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "succeeded", Label: "配置写入与回读一致"})
	case "not_supported":
		return domain.Operation{}, ErrCapabilityNotReady
	case "write_failed":
		operation.Outcome = "failed"
		operation.Message = "自有配置文件写入失败，原配置保持不变"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "配置写入失败"})
	case "verification_failed":
		operation.RollbackAttempted = true
		operation.Outcome = "rolled_back"
		operation.Message = "配置回读不一致，已尝试恢复原值"
		operation.Timeline = append(operation.Timeline,
			domain.OperationStep{State: "failed", Label: "配置回读不一致"},
			domain.OperationStep{State: "rolling_back", Label: "恢复原配置"},
			domain.OperationStep{State: "restored", Label: "已完成回滚尝试"},
		)
	default:
		return domain.Operation{}, fmt.Errorf("unexpected AirPlay auto-recover result %q", result)
	}
	m.mu.Lock()
	m.cached = deviceSnapshot{}
	m.mu.Unlock()
	return operation, nil
}

const setBluetoothEnabledScript = `
set -u
if [ ! -s /tmp/dbus_env.sh ] || ! pidof app_nevsps_bt >/dev/null 2>&1; then printf 'result=not_ready\n'; exit 0; fi
. /tmp/dbus_env.sh || { printf 'result=not_ready\n'; exit 0; }
capture=/tmp/sanyin-bt-event.$$
dbus-monitor --session "type='signal',path='/netease/ihw/bt',interface='netease.ihw.SmartAudio',member='Notify'" > "$capture" 2>/dev/null &
monitor_pid=$!
sleep 1
if ! /usr/bin/dbus-send --session --type=method_call --dest=netease.ihw.bt /netease/ihw/bt netease.ihw.SmartAudio.API uint32:0 uint32:256 uint32:2843 uint32:0 string:""; then
  kill "$monitor_pid" 2>/dev/null || true; rm -f "$capture"; printf 'result=send_failed\n'; exit 0
fi
attempt=0
result=verification_timeout
while [ "$attempt" -lt 8 ]; do
  if grep -q 'uint32 2863' "$capture"; then result=confirmed; break; fi
  attempt=$((attempt + 1)); sleep 1
done
kill "$monitor_pid" 2>/dev/null || true
rm -f "$capture"
if [ "$result" = confirmed ]; then
  mkdir -p /mnt/UDISK/sanyin-config
  printf 'enabled\n' > /mnt/UDISK/sanyin-config/.bluetooth-last-confirmed.tmp && mv /mnt/UDISK/sanyin-config/.bluetooth-last-confirmed.tmp /mnt/UDISK/sanyin-config/bluetooth-last-confirmed || result=confirmed_no_persist
fi
printf 'result=%s\n' "$result"
`

const setBluetoothDisabledScript = `
set -u
if [ ! -s /tmp/dbus_env.sh ] || ! pidof app_nevsps_bt >/dev/null 2>&1; then printf 'result=not_ready\n'; exit 0; fi
. /tmp/dbus_env.sh || { printf 'result=not_ready\n'; exit 0; }
capture=/tmp/sanyin-bt-event.$$
dbus-monitor --session "type='signal',path='/netease/ihw/bt',interface='netease.ihw.SmartAudio',member='Notify'" > "$capture" 2>/dev/null &
monitor_pid=$!
sleep 1
if ! /usr/bin/dbus-send --session --type=method_call --dest=netease.ihw.bt /netease/ihw/bt netease.ihw.SmartAudio.API uint32:0 uint32:256 uint32:2844 uint32:0 string:""; then
  kill "$monitor_pid" 2>/dev/null || true; rm -f "$capture"; printf 'result=send_failed\n'; exit 0
fi
attempt=0
result=verification_timeout
while [ "$attempt" -lt 8 ]; do
  if grep -q 'uint32 2864' "$capture"; then result=confirmed; break; fi
  attempt=$((attempt + 1)); sleep 1
done
kill "$monitor_pid" 2>/dev/null || true
rm -f "$capture"
if [ "$result" = confirmed ]; then
  mkdir -p /mnt/UDISK/sanyin-config
  printf 'disabled\n' > /mnt/UDISK/sanyin-config/.bluetooth-last-confirmed.tmp && mv /mnt/UDISK/sanyin-config/.bluetooth-last-confirmed.tmp /mnt/UDISK/sanyin-config/bluetooth-last-confirmed || result=confirmed_no_persist
fi
printf 'result=%s\n' "$result"
`

func (m *RealAdapter) SetBluetooth(ctx context.Context, enabled bool) (domain.Operation, error) {
	m.operationMu.Lock()
	defer m.operationMu.Unlock()

	label := "关闭"
	script := setBluetoothDisabledScript
	if enabled {
		label = "开启"
		script = setBluetoothEnabledScript
	}
	operation := domain.Operation{
		OperationID: fmt.Sprintf("device-bluetooth-%d", m.now().UnixMilli()),
		Simulation:  false,
		Timeline: []domain.OperationStep{
			{State: "confirmed", Label: "已确认实验性蓝牙操作"},
			{State: "running", Label: label + "蓝牙"},
			{State: "verifying", Label: "等待蓝牙服务成功事件"},
		},
	}
	output, err := m.runner.Run(ctx, script)
	if err != nil {
		return domain.Operation{}, err
	}
	result := parseKeyValues(output)["result"]
	switch result {
	case "confirmed", "confirmed_no_persist":
		operation.Applied = true
		operation.Verified = true
		operation.Outcome = "succeeded"
		operation.Message = "蓝牙已" + label + "，并收到设备服务成功事件"
		if result == "confirmed_no_persist" {
			operation.Message += "；最近验收状态未能持久保存"
		}
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "succeeded", Label: "蓝牙服务事件验收成功"})
	case "not_ready":
		operation.Outcome = "failed"
		operation.Message = "蓝牙服务或 D-Bus 环境尚未就绪"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "设备尚未就绪"})
	case "send_failed":
		operation.Outcome = "failed"
		operation.Message = "蓝牙命令发送失败"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "命令发送失败"})
	case "verification_timeout":
		operation.Outcome = "failed"
		operation.Message = "蓝牙命令已发送，但未在限定时间内收到成功事件；当前状态保持未知"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "成功事件验收超时"})
	default:
		return domain.Operation{}, fmt.Errorf("unexpected Bluetooth result %q", result)
	}
	m.mu.Lock()
	m.cached = deviceSnapshot{}
	m.mu.Unlock()
	return operation, nil
}

func (m *RealAdapter) SetEQ(ctx context.Context, mode int) (domain.Operation, error) {
	commands := map[int]string{
		0: "/usr/bin/sanyin_eq_probe.sh 0",
		1: "/usr/bin/sanyin_eq_probe.sh 1",
		2: "/usr/bin/sanyin_eq_probe.sh 2",
		3: "/usr/bin/sanyin_eq_probe.sh 3",
		4: "/usr/bin/sanyin_eq_probe.sh 4",
		5: "/usr/bin/sanyin_eq_probe.sh 5",
		6: "/usr/bin/sanyin_eq_probe.sh 6",
	}
	command, ok := commands[mode]
	if !ok {
		return domain.Operation{}, fmt.Errorf("invalid EQ mode %d", mode)
	}

	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	modeName, _ := eqModeName(strconv.Itoa(mode))
	operation := domain.Operation{
		OperationID: fmt.Sprintf("device-eq-%d", m.now().UnixMilli()),
		Simulation:  false,
		Timeline: []domain.OperationStep{
			{State: "confirmed", Label: "已确认实验性 EQ 操作"},
			{State: "running", Label: "请求切换至 " + modeName},
			{State: "verifying", Label: "等待 commonStatus 选中态事件"},
		},
	}
	output, err := m.runner.Run(ctx, command)
	if err != nil {
		return domain.Operation{}, err
	}
	result := parseKeyValues(output)["result"]
	switch result {
	case "selected_confirmed", "confirmed_no_persist":
		operation.Applied = true
		operation.Verified = true
		operation.Outcome = "succeeded"
		operation.Message = "EQ 业务模式已切换并通过设备状态事件验收"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "succeeded", Label: "业务选中态验收成功"})
		if result == "confirmed_no_persist" {
			operation.Message += "；最近验收状态未能持久保存"
		}
	case "not_ready":
		operation.Outcome = "failed"
		operation.Message = "控制中心、D-Bus 环境或 EQ 验收脚本尚未就绪"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "设备尚未就绪"})
	case "http_failed":
		operation.Outcome = "failed"
		operation.Message = "控制中心 EQ 接口调用失败，未确认配置变更"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "本地控制接口失败"})
	case "verification_timeout":
		operation.Outcome = "failed"
		operation.Message = "EQ 请求已发送，但未在限定时间内收到对应选中态事件"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "选中态事件验收超时"})
	default:
		return domain.Operation{}, fmt.Errorf("unexpected EQ result %q", result)
	}
	m.mu.Lock()
	m.cached = deviceSnapshot{}
	m.mu.Unlock()
	return operation, nil
}

const (
	wifiPendingConfigPath = "/mnt/UDISK/sanyin-config/wifi-pending.conf"
	wifiPendingSSIDPath   = "/mnt/UDISK/sanyin-config/wifi-pending-ssid"
)

func validateWiFiCredentials(ssid, password string) error {
	if !utf8.ValidString(ssid) || len([]byte(ssid)) < 1 || len([]byte(ssid)) > 32 {
		return fmt.Errorf("%w: SSID must contain 1..32 UTF-8 bytes", ErrInvalidInput)
	}
	for _, value := range ssid {
		if unicode.IsControl(value) {
			return fmt.Errorf("%w: SSID contains control characters", ErrInvalidInput)
		}
	}
	if password == "" {
		return nil
	}
	if !utf8.ValidString(password) || len([]byte(password)) < 8 || len([]byte(password)) > 64 {
		return fmt.Errorf("%w: Wi-Fi password must contain 8..64 UTF-8 bytes", ErrInvalidInput)
	}
	for _, value := range password {
		if unicode.IsControl(value) {
			return fmt.Errorf("%w: Wi-Fi password contains control characters", ErrInvalidInput)
		}
	}
	if len(password) == 64 && !isHexString(password) {
		return fmt.Errorf("%w: 64-byte Wi-Fi password must be hexadecimal", ErrInvalidInput)
	}
	return nil
}

func isHexString(value string) bool {
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}

func quoteWPAValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func buildWiFiConfig(ssid, password string) []byte {
	var content strings.Builder
	content.WriteString("ctrl_interface=/mnt/UDISK/wifi/sockets\n")
	content.WriteString("disable_scan_offload=1\n")
	content.WriteString("update_config=1\n\n")
	content.WriteString("network={\n")
	content.WriteString("\tssid=")
	content.WriteString(quoteWPAValue(ssid))
	content.WriteByte('\n')
	if password == "" {
		content.WriteString("\tkey_mgmt=NONE\n")
	} else if len(password) == 64 {
		content.WriteString("\tpsk=")
		content.WriteString(password)
		content.WriteByte('\n')
		content.WriteString("\tkey_mgmt=WPA-PSK\n")
	} else {
		content.WriteString("\tpsk=")
		content.WriteString(quoteWPAValue(password))
		content.WriteByte('\n')
		content.WriteString("\tkey_mgmt=WPA-PSK\n")
	}
	content.WriteString("\tscan_ssid=1\n")
	content.WriteString("}\n")
	return []byte(content.String())
}

func (m *RealAdapter) SetWiFi(ctx context.Context, ssid, password string) (domain.Operation, error) {
	if err := validateWiFiCredentials(ssid, password); err != nil {
		return domain.Operation{}, err
	}
	store, ok := m.runner.(deviceFileStore)
	if !ok {
		return domain.Operation{}, ErrCapabilityNotReady
	}

	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	if err := store.WriteDeviceFile(ctx, wifiPendingConfigPath, buildWiFiConfig(ssid, password), 0o600); err != nil {
		return domain.Operation{}, err
	}
	if err := store.WriteDeviceFile(ctx, wifiPendingSSIDPath, []byte(ssid+"\n"), 0o600); err != nil {
		_ = store.RemoveDeviceFile(wifiPendingConfigPath)
		return domain.Operation{}, err
	}

	operation := domain.Operation{
		OperationID: fmt.Sprintf("device-wifi-%d", m.now().UnixMilli()),
		Simulation:  false,
		Timeline: []domain.OperationStep{
			{State: "confirmed", Label: "已确认 Wi-Fi 切换"},
			{State: "running", Label: "备份当前配置并连接目标网络"},
			{State: "verifying", Label: "验收目标 SSID、IPv4、默认路由和网关"},
		},
	}
	transactionContext, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	output, err := m.runner.Run(transactionContext, "/usr/bin/sanyin_wifi_switch.sh switch")
	if err != nil {
		_ = store.RemoveDeviceFile(wifiPendingConfigPath)
		_ = store.RemoveDeviceFile(wifiPendingSSIDPath)
		return domain.Operation{}, err
	}
	result := parseKeyValues(output)["result"]
	switch result {
	case "succeeded":
		operation.Applied = true
		operation.Verified = true
		operation.Outcome = "succeeded"
		operation.Message = "Wi-Fi 已切换到目标网络，并通过 SSID、IPv4、默认路由和网关可达验收"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "succeeded", Label: "目标网络连接验收成功"})
	case "rolled_back":
		operation.Applied = true
		operation.Verified = true
		operation.RollbackAttempted = true
		operation.Outcome = "rolled_back"
		operation.Message = "目标网络连接失败，已自动恢复原 Wi-Fi 配置并重新联网"
		operation.Timeline = append(operation.Timeline,
			domain.OperationStep{State: "failed", Label: "目标网络连接超时"},
			domain.OperationStep{State: "rolling_back", Label: "恢复原 Wi-Fi 配置"},
			domain.OperationStep{State: "restored", Label: "原网络已恢复"},
		)
	case "rollback_failed":
		operation.Applied = true
		operation.RollbackAttempted = true
		operation.Outcome = "failed"
		operation.Message = "目标网络连接失败，自动回退也未能恢复联网；请使用 USB ADB 检查设备"
		operation.Timeline = append(operation.Timeline,
			domain.OperationStep{State: "failed", Label: "目标网络连接失败"},
			domain.OperationStep{State: "rolling_back", Label: "尝试恢复原配置"},
			domain.OperationStep{State: "failed", Label: "原网络恢复验收失败"},
		)
	case "invalid_pending_config", "write_failed", "backup_failed":
		operation.Outcome = "failed"
		operation.Message = "Wi-Fi 事务准备失败，未开始切换网络"
		operation.Timeline = append(operation.Timeline, domain.OperationStep{State: "failed", Label: "事务准备失败"})
	default:
		return domain.Operation{}, fmt.Errorf("unexpected Wi-Fi result %q", result)
	}
	m.mu.Lock()
	m.cached = deviceSnapshot{}
	m.mu.Unlock()
	return operation, nil
}
