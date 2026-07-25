package adapter

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

const healthyDeviceSnapshot = `product=C930 系列
firmware=2.1.2
platform=tina Neptune
storage_mib=46
splayer=online
kplayer=online
controller=online
alarm=online
bluetooth=online
player=online
restore=online
autorecover_supported=true
autorecover=enabled
bluetooth_last=unknown
wifi_switch_supported=true
wifi_control=online
network_ssid=My-WIFI
wifi_last=unknown
eq_probe_supported=true
eq_last=1
eq_applied=normal
airplay_port=listening
network=connected
signal=strong
`

type recordingRunner struct {
	recoveryResult  string
	configResult    string
	bluetoothResult string
	eqResult        string
	wifiResult      string
	scripts         []string
	files           map[string][]byte
}

func (r *recordingRunner) Run(_ context.Context, script string) (string, error) {
	r.scripts = append(r.scripts, script)
	if strings.Contains(script, "result=already_listening") {
		return "result=" + r.recoveryResult + "\n", nil
	}
	if strings.Contains(script, "result=configuration_updated") {
		result := r.configResult
		if result == "" {
			result = "configuration_updated"
		}
		return "result=" + result + "\n", nil
	}
	if strings.Contains(script, "sanyin-bt-event") {
		result := r.bluetoothResult
		if result == "" {
			result = "confirmed"
		}
		return "result=" + result + "\n", nil
	}
	if strings.HasPrefix(strings.TrimSpace(script), "/usr/bin/sanyin_eq_probe.sh ") {
		result := r.eqResult
		if result == "" {
			result = "selected_confirmed"
		}
		return "result=" + result + "\n", nil
	}
	if strings.TrimSpace(script) == "/usr/bin/sanyin_wifi_switch.sh switch" {
		result := r.wifiResult
		if result == "" {
			result = "succeeded"
		}
		return "result=" + result + "\n", nil
	}
	return healthyDeviceSnapshot, nil
}

func (r *recordingRunner) WriteDeviceFile(_ context.Context, filename string, content []byte, _ os.FileMode) error {
	if r.files == nil {
		r.files = map[string][]byte{}
	}
	r.files[filename] = append([]byte(nil), content...)
	return nil
}

func (r *recordingRunner) RemoveDeviceFile(filename string) error {
	delete(r.files, filename)
	return nil
}

func TestRealAdapterReturnsCurrentDeviceStateAndSafeCapabilities(t *testing.T) {
	runner := &recordingRunner{recoveryResult: "already_listening"}
	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	provider := NewRealProviderWithClock(runner, func() time.Time { return now })
	provider.device.playback = newPlaybackManager(&fakePlayerController{state: playerTransport{State: "stopped"}}, runner, func() time.Time { return now })
	device, err := provider.ForScenario(LiveScenario)
	if err != nil {
		t.Fatal(err)
	}

	status, err := device.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Overall.Value != "healthy" || status.Network.Value != "connected" || status.Services["splayer"].Value != "online" || status.Player.Value != "stopped" {
		t.Fatalf("unexpected real status: %#v", status)
	}
	if status.Overall.Source != "system" || status.Overall.ObservedAt != now {
		t.Fatalf("real state is not marked as current system observation: %#v", status.Overall)
	}
	info, err := device.Device(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Firmware.Value != "2.1.2" || info.Platform.Value != "tina Neptune" {
		t.Fatalf("firmware and platform fields were merged: %#v", info)
	}

	capabilities, err := device.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writability := map[string]string{}
	for _, capability := range capabilities {
		writability[capability.ID] = string(capability.Writability)
	}
	if writability["airplay.recover"] != "safe" {
		t.Fatalf("AirPlay recovery must be the verified real write, got %q", writability["airplay.recover"])
	}
	if writability["airplay.autoRecover"] != "safe" {
		t.Fatalf("AirPlay auto-recover config must be safely writable, got %q", writability["airplay.autoRecover"])
	}
	if writability["bluetooth.enabled"] != "experimental" {
		t.Fatalf("Bluetooth write must remain explicitly experimental, got %q", writability["bluetooth.enabled"])
	}
	if writability["audio.effect"] != "experimental" {
		t.Fatalf("EQ write must remain explicitly experimental, got %q", writability["audio.effect"])
	}
	if writability["wifi.connection"] != "experimental" {
		t.Fatalf("Wi-Fi write must remain explicitly experimental, got %q", writability["wifi.connection"])
	}
	for _, id := range []string{"wifi.connection", "audio.volume", "audio.effect", "bluetooth.enabled", "lighting.brightness"} {
		if writability[id] == "safe" {
			t.Fatalf("unverified capability %s was exposed as safe", id)
		}
	}
}

func TestWiFiSwitchUsesProtectedFilesAndReportsRollback(t *testing.T) {
	runner := &recordingRunner{}
	device, _ := NewRealProviderWithClock(runner, func() time.Time { return time.UnixMilli(3344) }).ForScenario(LiveScenario)
	operation, err := device.SetWiFi(context.Background(), `Lab "5G"`, `safe\password`)
	if err != nil {
		t.Fatal(err)
	}
	if !operation.Applied || !operation.Verified || operation.Outcome != "succeeded" {
		t.Fatalf("successful Wi-Fi transaction was not verified: %#v", operation)
	}
	if len(runner.scripts) != 1 || runner.scripts[0] != "/usr/bin/sanyin_wifi_switch.sh switch" {
		t.Fatalf("Wi-Fi credentials reached the shell command: %#v", runner.scripts)
	}
	config := string(runner.files[wifiPendingConfigPath])
	if !strings.Contains(config, `ssid="Lab \"5G\""`) || !strings.Contains(config, `psk="safe\\password"`) {
		t.Fatalf("Wi-Fi config was not safely escaped: %q", config)
	}
	if _, err := device.SetWiFi(context.Background(), "", "short"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid Wi-Fi input returned %v", err)
	}

	rollbackRunner := &recordingRunner{wifiResult: "rolled_back"}
	rollbackDevice, _ := NewRealProvider(rollbackRunner).ForScenario(LiveScenario)
	rollbackOperation, err := rollbackDevice.SetWiFi(context.Background(), "missing-network", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if rollbackOperation.Outcome != "rolled_back" || !rollbackOperation.RollbackAttempted || !rollbackOperation.Verified {
		t.Fatalf("verified Wi-Fi rollback was not represented correctly: %#v", rollbackOperation)
	}
}

func TestEQWriteAcceptsOnlyFixedModeCommandsAndRequiresSelectionEvent(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewRealProviderWithClock(runner, func() time.Time { return time.UnixMilli(1122) })
	device, _ := provider.ForScenario(LiveScenario)
	operation, err := device.SetEQ(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if !operation.Applied || !operation.Verified || operation.Outcome != "succeeded" {
		t.Fatalf("confirmed EQ selection event was not accepted: %#v", operation)
	}
	if len(runner.scripts) < 1 || runner.scripts[0] != "/usr/bin/sanyin_eq_probe.sh 2" {
		t.Fatalf("EQ did not use a fixed validated command: %#v", runner.scripts)
	}
	if _, err := device.SetEQ(context.Background(), -1); err == nil {
		t.Fatal("negative EQ mode was accepted")
	}

	timeoutRunner := &recordingRunner{eqResult: "verification_timeout"}
	timeoutDevice, _ := NewRealProvider(timeoutRunner).ForScenario(LiveScenario)
	timeoutOperation, err := timeoutDevice.SetEQ(context.Background(), 6)
	if err != nil {
		t.Fatal(err)
	}
	if timeoutOperation.Verified || timeoutOperation.Outcome != "failed" {
		t.Fatalf("EQ timeout was presented as success: %#v", timeoutOperation)
	}
}

func TestBluetoothWriteRequiresDeviceSuccessEvent(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewRealProviderWithClock(runner, func() time.Time { return time.UnixMilli(9876) })
	device, _ := provider.ForScenario(LiveScenario)
	operation, err := device.SetBluetooth(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !operation.Applied || !operation.Verified || operation.Outcome != "succeeded" {
		t.Fatalf("confirmed Bluetooth event was not accepted: %#v", operation)
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "uint32:2843") || !strings.Contains(runner.scripts[0], "uint32 2863") {
		t.Fatalf("Bluetooth enable command or event verification is missing")
	}

	timeoutRunner := &recordingRunner{bluetoothResult: "verification_timeout"}
	timeoutProvider := NewRealProvider(timeoutRunner)
	timeoutDevice, _ := timeoutProvider.ForScenario(LiveScenario)
	timeoutOperation, err := timeoutDevice.SetBluetooth(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if timeoutOperation.Verified || timeoutOperation.Outcome != "failed" {
		t.Fatalf("Bluetooth timeout was presented as success: %#v", timeoutOperation)
	}
}

func TestRealAirPlayAutoRecoverConfigIsWrittenAndReadBack(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewRealProviderWithClock(runner, func() time.Time { return time.UnixMilli(5678) })
	device, _ := provider.ForScenario(LiveScenario)
	operation, err := device.SetAirPlayAutoRecover(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !operation.Applied || !operation.Verified || operation.Outcome != "succeeded" || operation.Simulation {
		t.Fatalf("configuration write was not verified: %#v", operation)
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "printf 'disabled") || !strings.Contains(runner.scripts[0], "mv \"$temp_file\"") {
		t.Fatalf("configuration was not written atomically using the disabled value")
	}
}

func TestRealAirPlayAutoRecoverRollsBackOnReadbackFailure(t *testing.T) {
	runner := &recordingRunner{configResult: "verification_failed"}
	provider := NewRealProvider(runner)
	device, _ := provider.ForScenario(LiveScenario)
	operation, err := device.SetAirPlayAutoRecover(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Outcome != "rolled_back" || !operation.RollbackAttempted || operation.Verified {
		t.Fatalf("readback failure did not trigger rollback: %#v", operation)
	}
}

func TestRealAirPlayRecoveryIsExplicitAndVerified(t *testing.T) {
	tests := []struct {
		result   string
		applied  bool
		verified bool
		outcome  string
	}{
		{result: "already_listening", verified: true, outcome: "succeeded"},
		{result: "recovered", applied: true, verified: true, outcome: "succeeded"},
		{result: "verification_timeout", applied: true, outcome: "failed"},
	}
	for _, item := range tests {
		t.Run(item.result, func(t *testing.T) {
			runner := &recordingRunner{recoveryResult: item.result}
			provider := NewRealProviderWithClock(runner, func() time.Time { return time.UnixMilli(1234) })
			device, _ := provider.ForScenario(LiveScenario)
			operation, err := device.RecoverAirPlay(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if operation.Simulation || operation.Applied != item.applied || operation.Verified != item.verified || operation.Outcome != item.outcome {
				t.Fatalf("unexpected operation for %s: %#v", item.result, operation)
			}
			if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "uint32:7425") || !strings.Contains(runner.scripts[0], "TCP") && !strings.Contains(runner.scripts[0], ":5002") {
				t.Fatalf("recovery did not use the verified command and port check")
			}
		})
	}
}

func TestSnapshotProbeDoesNotReturnSensitiveNetworkIdentifiers(t *testing.T) {
	for _, blocked := range []string{"ESSID", "Access Point", "inet addr", "wifi_ssid", "wifi_mac"} {
		if strings.Contains(snapshotScript, "printf '"+blocked) {
			t.Fatalf("snapshot script emits sensitive field %q", blocked)
		}
	}
}
