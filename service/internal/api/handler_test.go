package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"sanyin.local/config/service/internal/adapter"
	"sanyin.local/config/service/internal/api"
	"sanyin.local/config/service/internal/domain"
	"sanyin.local/config/service/internal/updater"
)

var fixedTime = time.Date(2026, 7, 23, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60))

func xmlTextForTest(payload []byte, name string) string {
	value := string(payload)
	startTag := "<" + name + ">"
	endTag := "</" + name + ">"
	start := strings.Index(value, startTag)
	end := strings.Index(value, endTag)
	if start < 0 || end < 0 || end < start {
		return ""
	}
	return value[start+len(startTag) : end]
}

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	handler := api.NewHandler(adapter.NewMockProviderWithClock(func() time.Time { return fixedTime }))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func getJSON(t *testing.T, server *httptest.Server, path string) map[string]any {
	t.Helper()
	response, err := server.Client().Get(server.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s returned %d: %s", path, response.StatusCode, body)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCapabilityContractAndEnums(t *testing.T) {
	server := testServer(t)
	body := getJSON(t, server, api.BasePath+"/capabilities")
	if body["environment"] != "mock" || body["scenario"] != "healthy" {
		t.Fatalf("missing mock identity: %#v", body)
	}
	capabilities := body["data"].([]any)
	if len(capabilities) < 16 {
		t.Fatalf("expected complete capability inventory, got %d", len(capabilities))
	}
	readability := map[string]bool{"full": true, "partial": true, "none": true}
	writability := map[string]bool{"safe": true, "experimental": true, "not_verified": true, "unsupported": true}
	availability := map[string]bool{"available": true, "degraded": true, "offline": true, "unknown": true}
	for _, raw := range capabilities {
		capability := raw.(map[string]any)
		if !readability[capability["readability"].(string)] {
			t.Fatalf("invalid readability: %#v", capability)
		}
		if !writability[capability["writability"].(string)] {
			t.Fatalf("invalid writability: %#v", capability)
		}
		if !availability[capability["availability"].(string)] {
			t.Fatalf("invalid availability: %#v", capability)
		}
	}
}

func TestAllMockScenariosHaveDistinctAggregateState(t *testing.T) {
	server := testServer(t)
	expectations := map[string]struct{ path, value string }{
		"healthy":           {"overall", "healthy"},
		"airplay_down":      {"overall", "degraded"},
		"wifi_offline":      {"network", "offline"},
		"controller_down":   {"audio", "unknown"},
		"bluetooth_unknown": {"overall", "healthy"},
		"eq_pending":        {"overall", "healthy"},
		"stale_state":       {"overall", "healthy"},
		"operation_failed":  {"overall", "healthy"},
	}
	for scenario, expected := range expectations {
		t.Run(scenario, func(t *testing.T) {
			body := getJSON(t, server, api.BasePath+"/status?scenario="+scenario)
			data := body["data"].(map[string]any)
			state := data[expected.path].(map[string]any)
			if state["value"] != expected.value {
				t.Fatalf("expected %s, got %#v", expected.value, state)
			}
			if state["source"] != "mock" {
				t.Fatalf("state is not marked mock: %#v", state)
			}
		})
	}
}

func TestSensitiveFieldsNeverEnterResponses(t *testing.T) {
	server := testServer(t)
	paths := []string{"/capabilities", "/device", "/status", "/airplay", "/network", "/audio", "/bluetooth", "/lighting", "/schedules", "/player"}
	for _, path := range paths {
		body := getJSON(t, server, api.BasePath+path)
		assertNoSensitiveKey(t, body, path)
	}
}

func assertNoSensitiveKey(t *testing.T, value any, path string) {
	t.Helper()
	blocked := map[string]bool{
		"ssid": true, "bssid": true, "mac": true, "ip": true, "password": true,
		"token": true, "deviceid": true, "serial": true, "hostname": true,
	}
	switch item := value.(type) {
	case map[string]any:
		for key, nested := range item {
			if blocked[strings.ToLower(key)] {
				t.Fatalf("sensitive key %q found in %s", key, path)
			}
			assertNoSensitiveKey(t, nested, path+"."+key)
		}
	case []any:
		for _, nested := range item {
			assertNoSensitiveKey(t, nested, path)
		}
	}
}

func TestStaleAndUnknownSemantics(t *testing.T) {
	server := testServer(t)
	stale := getJSON(t, server, api.BasePath+"/network?scenario=stale_state")
	connection := stale["data"].(map[string]any)["connection"].(map[string]any)
	if connection["freshness"] != "stale" || connection["observedAt"] != "2026-07-23T08:45:00+08:00" {
		t.Fatalf("unexpected stale state: %#v", connection)
	}

	bluetooth := getJSON(t, server, api.BasePath+"/bluetooth?scenario=bluetooth_unknown")
	enabled := bluetooth["data"].(map[string]any)["enabled"].(map[string]any)
	if enabled["value"] != "unknown" || enabled["freshness"] != "unknown" {
		t.Fatalf("unknown state was inferred: %#v", enabled)
	}
}

func TestEQSelectedAndAppliedModesRemainSeparate(t *testing.T) {
	server := testServer(t)
	body := getJSON(t, server, api.BasePath+"/audio?scenario=eq_pending")
	eq := body["data"].(map[string]any)["eq"].(map[string]any)
	selected := eq["selectedMode"].(map[string]any)["value"]
	applied := eq["appliedMode"].(map[string]any)["value"]
	applyState := eq["applyState"].(map[string]any)["value"]
	if selected == applied || applyState != "pending_local_playback" {
		t.Fatalf("EQ layers were merged: %#v", eq)
	}
}

func TestSSEEventContract(t *testing.T) {
	server := testServer(t)
	response, err := server.Client().Get(server.URL + api.BasePath + "/events?scenario=wifi_offline")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("unexpected content type: %s", got)
	}
	scanner := bufio.NewScanner(response.Body)
	lines := []string{}
	for scanner.Scan() {
		if scanner.Text() == "" {
			break
		}
		lines = append(lines, scanner.Text())
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "event: snapshot") || !strings.Contains(joined, `"source":"mock"`) || !strings.Contains(joined, `"scenario":"wifi_offline"`) {
		t.Fatalf("invalid SSE event:\n%s", joined)
	}
}

func TestUnverifiedWritesReturnCapabilityNotReady(t *testing.T) {
	server := testServer(t)
	requests := []struct{ method, path string }{
		{http.MethodPost, "/airplay/recover"},
		{http.MethodPut, "/airplay/auto-recover"},
		{http.MethodPost, "/network/switch"},
		{http.MethodPatch, "/bluetooth"},
		{http.MethodPatch, "/audio"},
		{http.MethodPatch, "/audio/effect"},
		{http.MethodPatch, "/lighting"},
		{http.MethodPut, "/microphone/schedule"},
		{http.MethodPost, "/player/control"},
	}
	for _, item := range requests {
		req, _ := http.NewRequestWithContext(context.Background(), item.method, server.URL+api.BasePath+item.path, nil)
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body domain.APIError
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusConflict || body.Error.Code != "capability_not_ready" || body.Error.OperationID != nil {
			t.Fatalf("%s %s returned %d %#v", item.method, item.path, response.StatusCode, body)
		}
	}
}

func TestSimulationIsExplicitAndRollbackIsVisible(t *testing.T) {
	server := testServer(t)
	request, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/airplay/recover?scenario=operation_failed&simulate=true", nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	operation := body["data"].(map[string]any)
	if response.StatusCode != http.StatusAccepted || operation["simulation"] != true || operation["outcome"] != "rolled_back" || operation["rollbackAttempted"] != true {
		t.Fatalf("rollback simulation is ambiguous: %#v", operation)
	}
	states := []string{}
	for _, raw := range operation["timeline"].([]any) {
		states = append(states, raw.(map[string]any)["state"].(string))
	}
	expected := []string{"confirmed", "running", "verifying", "failed", "rolling_back", "restored"}
	if !reflect.DeepEqual(states, expected) {
		t.Fatalf("unexpected timeline: %#v", states)
	}
}

func TestUnknownScenarioIsRejected(t *testing.T) {
	server := testServer(t)
	response, err := server.Client().Get(server.URL + api.BasePath + "/status?scenario=not-real")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.StatusCode)
	}
}

func TestScenariosUseCommonEnvelope(t *testing.T) {
	server := testServer(t)
	body := getJSON(t, server, api.BasePath+"/mock/scenarios")
	if body["environment"] != "mock" || body["scenario"] != "healthy" {
		t.Fatalf("scenario list missing common envelope: %#v", body)
	}
	if scenarios, ok := body["data"].([]any); !ok || len(scenarios) != 8 {
		t.Fatalf("expected eight scenarios in data: %#v", body["data"])
	}
}

func TestOpenAPIContractListsImplementedRoutes(t *testing.T) {
	content, err := os.ReadFile("../../openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract map[string]any
	if err := json.Unmarshal(content, &contract); err != nil {
		t.Fatalf("OpenAPI is not valid JSON: %v", err)
	}
	paths := contract["paths"].(map[string]any)
	expected := []string{
		"/capabilities", "/device", "/status", "/airplay", "/network", "/audio",
		"/bluetooth", "/lighting", "/schedules", "/events", "/airplay/recover",
		"/airplay/auto-recover", "/network/switch", "/microphone/schedule", "/mock/scenarios",
		"/audio/effect", "/player", "/player/control", "/system", "/system/update",
	}
	for _, path := range expected {
		if _, ok := paths[path]; !ok {
			t.Errorf("OpenAPI missing implemented route %s", path)
		}
	}
}

type fakeUpdateManager struct {
	info   domain.SystemInfo
	result domain.UpdateAccepted
	err    error
	staged []byte
}

func (f *fakeUpdateManager) Info() domain.SystemInfo {
	return f.info
}

func (f *fakeUpdateManager) Stage(_ context.Context, reader io.Reader, _ int64) (domain.UpdateAccepted, error) {
	f.staged, _ = io.ReadAll(reader)
	return f.result, f.err
}

func TestSystemEndpointReportsBuildAndSignedUpdateState(t *testing.T) {
	manager := &fakeUpdateManager{info: domain.SystemInfo{
		Build:         domain.BuildInfo{Version: "1.8.0", Commit: "abc123", BuiltAt: "2026-07-26T00:00:00Z"},
		UpdateEnabled: true,
		Update:        domain.UpdateStatus{State: "succeeded", Version: "1.8.0"},
	}}
	server := httptest.NewServer(api.NewHandler(
		adapter.NewMockProviderWithClock(func() time.Time { return fixedTime }),
		api.Options{Build: manager.info.Build, Updater: manager},
	))
	defer server.Close()

	body := getJSON(t, server, api.BasePath+"/system")
	data := body["data"].(map[string]any)
	build := data["build"].(map[string]any)
	if build["version"] != "1.8.0" || data["updateEnabled"] != true {
		t.Fatalf("unexpected system info: %#v", data)
	}

	request, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/system/update", bytes.NewReader([]byte("package")))
	request.Header.Set("Content-Type", "application/vnd.sanyin.update+zip")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("mock mode accepted a system update: %d", response.StatusCode)
	}
}

func TestDeviceUpdateEndpointMapsVerificationResults(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		manager := &fakeUpdateManager{
			info:   domain.SystemInfo{UpdateEnabled: true},
			result: domain.UpdateAccepted{Version: "1.8.1", State: "staged", Message: "ready"},
		}
		server := httptest.NewServer(api.NewHandler(adapter.NewRealProvider(realRunner{}), api.Options{Updater: manager}))
		defer server.Close()
		request, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/system/update", bytes.NewReader([]byte("signed-package")))
		request.Header.Set("Content-Type", "application/vnd.sanyin.update+zip")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted || string(manager.staged) != "signed-package" {
			t.Fatalf("update was not staged: status=%d body=%q", response.StatusCode, manager.staged)
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		manager := &fakeUpdateManager{info: domain.SystemInfo{UpdateEnabled: true}, err: updater.ErrInvalidSignature}
		server := httptest.NewServer(api.NewHandler(adapter.NewRealProvider(realRunner{}), api.Options{Updater: manager}))
		defer server.Close()
		request, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/system/update", bytes.NewReader([]byte("tampered")))
		request.Header.Set("Content-Type", "application/vnd.sanyin.update+zip")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("invalid signature returned %d", response.StatusCode)
		}
	})
}

type realRunner struct {
	playerPort int
}

func (r realRunner) Run(_ context.Context, script string) (string, error) {
	if strings.Contains(script, "kplayer_port") && r.playerPort != 0 {
		return fmt.Sprintf("result=ready\nkplayer_port=%d\n", r.playerPort), nil
	}
	if strings.Contains(script, "result=already_listening") {
		return "result=already_listening\n", nil
	}
	if strings.Contains(script, "result=configuration_updated") {
		return "result=configuration_updated\n", nil
	}
	if strings.Contains(script, "sanyin-bt-event") {
		return "result=confirmed\n", nil
	}
	if strings.HasPrefix(strings.TrimSpace(script), "/usr/bin/sanyin_eq_probe.sh ") {
		return "result=selected_confirmed\n", nil
	}
	if strings.TrimSpace(script) == "/usr/bin/sanyin_wifi_switch.sh switch" {
		return "result=succeeded\n", nil
	}
	return `product=C930 系列
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
`, nil
}

func (r realRunner) DeviceHTTPHost(_ context.Context) (string, error) {
	return "127.0.0.1", nil
}

func (realRunner) WriteDeviceFile(_ context.Context, _ string, _ []byte, _ os.FileMode) error {
	return nil
}

func (realRunner) RemoveDeviceFile(_ string) error {
	return nil
}

func TestRealModeReturnsDeviceEnvelopeAndExecutesVerifiedRecovery(t *testing.T) {
	server := httptest.NewServer(api.NewHandler(adapter.NewRealProviderWithClock(realRunner{}, func() time.Time { return fixedTime })))
	defer server.Close()

	status := getJSON(t, server, api.BasePath+"/status?scenario=operation_failed")
	if status["environment"] != "device" || status["scenario"] != "live" {
		t.Fatalf("real response retained mock identity: %#v", status)
	}

	request, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/airplay/recover", nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	operation := body["data"].(map[string]any)
	if response.StatusCode != http.StatusAccepted || operation["simulation"] != false || operation["verified"] != true || operation["outcome"] != "succeeded" {
		t.Fatalf("real recovery was not executed and verified: status=%d body=%#v", response.StatusCode, operation)
	}
}

func TestRealModeUpdatesAirPlayAutoRecoverConfiguration(t *testing.T) {
	server := httptest.NewServer(api.NewHandler(adapter.NewRealProviderWithClock(realRunner{}, func() time.Time { return fixedTime })))
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPut, server.URL+api.BasePath+"/airplay/auto-recover", strings.NewReader(`{"enabled":false}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	operation := body["data"].(map[string]any)
	if response.StatusCode != http.StatusAccepted || operation["simulation"] != false || operation["applied"] != true || operation["verified"] != true {
		t.Fatalf("real configuration was not written and verified: status=%d body=%#v", response.StatusCode, operation)
	}
}

func TestAirPlayAutoRecoverRejectsAmbiguousInput(t *testing.T) {
	server := httptest.NewServer(api.NewHandler(adapter.NewRealProvider(realRunner{})))
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPut, server.URL+api.BasePath+"/airplay/auto-recover", strings.NewReader(`{"enabled":false,"extra":true}`))
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("ambiguous configuration input returned %d", response.StatusCode)
	}
}

func TestRealModeUpdatesBluetoothAndRequiresEventVerification(t *testing.T) {
	server := httptest.NewServer(api.NewHandler(adapter.NewRealProviderWithClock(realRunner{}, func() time.Time { return fixedTime })))
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPatch, server.URL+api.BasePath+"/bluetooth", strings.NewReader(`{"enabled":true}`))
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	operation := body["data"].(map[string]any)
	if response.StatusCode != http.StatusAccepted || operation["applied"] != true || operation["verified"] != true {
		t.Fatalf("Bluetooth write was not event-verified: status=%d body=%#v", response.StatusCode, operation)
	}
}

func TestRealModeUpdatesEQAndRejectsOutOfRangeModes(t *testing.T) {
	server := httptest.NewServer(api.NewHandler(adapter.NewRealProviderWithClock(realRunner{}, func() time.Time { return fixedTime })))
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPatch, server.URL+api.BasePath+"/audio/effect", strings.NewReader(`{"mode":2}`))
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	operation := body["data"].(map[string]any)
	if response.StatusCode != http.StatusAccepted || operation["verified"] != true || operation["outcome"] != "succeeded" {
		t.Fatalf("EQ write was not event-verified: status=%d body=%#v", response.StatusCode, operation)
	}

	invalid, _ := http.NewRequest(http.MethodPatch, server.URL+api.BasePath+"/audio/effect", strings.NewReader(`{"mode":-1}`))
	invalidResponse, err := server.Client().Do(invalid)
	if err != nil {
		t.Fatal(err)
	}
	invalidResponse.Body.Close()
	if invalidResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("out-of-range EQ mode returned %d", invalidResponse.StatusCode)
	}
}

func TestRealModeSwitchesWiFiAndRejectsInvalidCredentials(t *testing.T) {
	server := httptest.NewServer(api.NewHandler(adapter.NewRealProviderWithClock(realRunner{}, func() time.Time { return fixedTime })))
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/network/switch", strings.NewReader(`{"ssid":"Target WiFi","password":"password123"}`))
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	operation := body["data"].(map[string]any)
	if response.StatusCode != http.StatusAccepted || operation["verified"] != true || operation["outcome"] != "succeeded" {
		t.Fatalf("Wi-Fi transaction was not verified: status=%d body=%#v", response.StatusCode, operation)
	}

	invalid, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/network/switch", strings.NewReader(`{"ssid":"Target WiFi","password":"short"}`))
	invalidResponse, err := server.Client().Do(invalid)
	if err != nil {
		t.Fatal(err)
	}
	invalidResponse.Body.Close()
	if invalidResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid Wi-Fi password returned %d", invalidResponse.StatusCode)
	}
}

func TestPlayerReadContractIncludesProgressQueueAndStations(t *testing.T) {
	server := testServer(t)
	body := getJSON(t, server, api.BasePath+"/player")
	data := body["data"].(map[string]any)
	if data["transport"].(map[string]any)["value"] != "stopped" {
		t.Fatalf("unexpected player transport: %#v", data)
	}
	if _, ok := data["positionSeconds"]; !ok {
		t.Fatalf("player progress is missing: %#v", data)
	}
	if _, ok := data["volume"]; !ok {
		t.Fatalf("player volume is missing: %#v", data)
	}
	if len(data["queue"].([]any)) != 1 || len(data["stations"].([]any)) != 1 {
		t.Fatalf("player queue or stations are missing: %#v", data)
	}
}

func TestSceneReadContractAndMockWriteGuard(t *testing.T) {
	server := testServer(t)
	body := getJSON(t, server, api.BasePath+"/scenes")
	scenes := body["data"].([]any)
	if len(scenes) != 2 {
		t.Fatalf("expected mock scene examples, got %#v", scenes)
	}
	first := scenes[0].(map[string]any)
	if first["name"] != "专注阅读" || first["volume"] != float64(24) || first["timerMinutes"] != float64(45) {
		t.Fatalf("scene contract is incomplete: %#v", first)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/scenes", strings.NewReader(`{"name":"测试","icon":"music","title":"音乐","url":"https://media.example/test.mp3","volume":20,"timerMinutes":0}`))
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("mock mode accepted scene write: %d", response.StatusCode)
	}
}

func TestRealModeManagesAndAppliesPersistentPlayerScenes(t *testing.T) {
	transport := "STOPPED"
	volume := 30
	kplayer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<?xml version="1.0"?><root><device><serviceList><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/av/control.xml</controlURL></service><service><serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType><controlURL>/render/control.xml</controlURL></service></serviceList></device></root>`)
			return
		}
		action := strings.Trim(r.Header.Get("SOAPACTION"), `"`)
		payload, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(action, "#Play"):
			transport = "PLAYING"
		case strings.HasSuffix(action, "#GetTransportInfo"):
			fmt.Fprintf(w, `<CurrentTransportState>%s</CurrentTransportState>`, transport)
			return
		case strings.HasSuffix(action, "#GetPositionInfo"):
			fmt.Fprint(w, `<TrackDuration>00:03:00</TrackDuration><RelTime>00:00:01</RelTime>`)
			return
		case strings.HasSuffix(action, "#SetVolume"):
			volume, _ = strconv.Atoi(xmlTextForTest(payload, "DesiredVolume"))
		case strings.HasSuffix(action, "#GetVolume"):
			fmt.Fprintf(w, `<CurrentVolume>%d</CurrentVolume>`, volume)
			return
		}
		fmt.Fprint(w, `<ok/>`)
	}))
	defer kplayer.Close()
	parsed, _ := url.Parse(kplayer.URL)
	port, _ := strconv.Atoi(parsed.Port())
	server := httptest.NewServer(api.NewHandler(adapter.NewRealProviderWithClock(realRunner{playerPort: port}, func() time.Time { return fixedTime })))
	defer server.Close()

	requestJSON := func(method, path, payload string, expected int) map[string]any {
		t.Helper()
		request, _ := http.NewRequest(method, server.URL+api.BasePath+path, strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != expected {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("%s %s returned %d: %s", method, path, response.StatusCode, body)
		}
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}

	created := requestJSON(http.MethodPost, "/scenes", `{"name":"睡前放松","icon":"sleep","title":"睡眠音乐","url":"https://media.example/sleep.mp3?token=private","volume":16,"timerMinutes":30,"schedule":{"enabled":true,"time":"22:30","weekdays":[1,2,3,4,5,6,7]}}`, http.StatusCreated)
	scenes := created["data"].([]any)
	if len(scenes) != 1 {
		t.Fatalf("scene was not created: %#v", created)
	}
	scene := scenes[0].(map[string]any)
	id := scene["id"].(string)
	if strings.Contains(scene["source"].(string), "token") {
		t.Fatalf("scene source leaked a private query: %#v", scene)
	}
	schedule := scene["schedule"].(map[string]any)
	if schedule["enabled"] != true || schedule["time"] != "22:30" || schedule["nextRunAt"] == nil {
		t.Fatalf("scene schedule was not returned with its next run: %#v", schedule)
	}

	updated := requestJSON(http.MethodPut, "/scenes/"+id, `{"name":"夜间模式","icon":"sleep","title":"睡眠音乐","url":"","volume":14,"timerMinutes":15,"schedule":{"enabled":true,"time":"22:30","weekdays":[1,2,3,4,5,6,7]}}`, http.StatusOK)
	updatedScene := updated["data"].([]any)[0].(map[string]any)
	if updatedScene["name"] != "夜间模式" || updatedScene["volume"] != float64(14) {
		t.Fatalf("scene was not updated while preserving its URL: %#v", updatedScene)
	}
	conflict := requestJSON(http.MethodPost, "/scenes", `{"name":"冲突场景","icon":"music","title":"另一首歌","url":"https://media.example/other.mp3","volume":20,"timerMinutes":0,"schedule":{"enabled":true,"time":"22:30","weekdays":[7]}}`, http.StatusConflict)
	if conflict["error"].(map[string]any)["code"] != "scene_schedule_conflict" {
		t.Fatalf("schedule conflict did not return a specific error: %#v", conflict)
	}

	applied := requestJSON(http.MethodPost, "/scenes/"+id+"/apply", "", http.StatusOK)
	application := applied["data"].(map[string]any)
	player := application["player"].(map[string]any)
	if player["transport"].(map[string]any)["value"] != "playing" || player["volume"].(map[string]any)["value"] != float64(14) || player["stopTimer"].(map[string]any)["active"] != true {
		t.Fatalf("scene was not fully applied: %#v", application)
	}

	deleted := requestJSON(http.MethodDelete, "/scenes/"+id, "", http.StatusOK)
	if len(deleted["data"].([]any)) != 0 {
		t.Fatalf("scene was not deleted: %#v", deleted)
	}
}

func TestRealModeControlsKPlayerThroughValidatedPlayerAPI(t *testing.T) {
	transport := "STOPPED"
	volume := 30
	kplayer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<?xml version="1.0"?><root><device><serviceList><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/av/control.xml</controlURL></service><service><serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType><controlURL>/render/control.xml</controlURL></service></serviceList></device></root>`)
			return
		}
		action := strings.Trim(r.Header.Get("SOAPACTION"), `"`)
		payload, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(action, "#Play"):
			transport = "PLAYING"
		case strings.HasSuffix(action, "#Pause"):
			transport = "PAUSED_PLAYBACK"
		case strings.HasSuffix(action, "#Stop"):
			transport = "STOPPED"
		case strings.HasSuffix(action, "#GetTransportInfo"):
			fmt.Fprintf(w, `<CurrentTransportState>%s</CurrentTransportState>`, transport)
			return
		case strings.HasSuffix(action, "#GetPositionInfo"):
			fmt.Fprint(w, `<TrackDuration>00:00:13</TrackDuration><RelTime>00:00:02</RelTime>`)
			return
		case strings.HasSuffix(action, "#SetVolume"):
			volume, _ = strconv.Atoi(xmlTextForTest(payload, "DesiredVolume"))
		case strings.HasSuffix(action, "#GetVolume"):
			fmt.Fprintf(w, `<CurrentVolume>%d</CurrentVolume>`, volume)
			return
		}
		fmt.Fprint(w, `<ok/>`)
	}))
	defer kplayer.Close()
	parsed, _ := url.Parse(kplayer.URL)
	port, _ := strconv.Atoi(parsed.Port())
	server := httptest.NewServer(api.NewHandler(adapter.NewRealProviderWithClock(realRunner{playerPort: port}, func() time.Time { return fixedTime })))
	defer server.Close()

	control := func(payload string) map[string]any {
		t.Helper()
		request, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/player/control", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("player control returned %d: %s", response.StatusCode, body)
		}
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body["data"].(map[string]any)
	}

	player := control(`{"action":"radio_add","title":"A 电台","url":"http://radio.example/a.mp3"}`)
	player = control(`{"action":"radio_add","title":"B 电台","url":"http://radio.example/b.mp3"}`)
	stations := player["stations"].([]any)
	secondStationID := stations[1].(map[string]any)["id"].(string)
	player = control(fmt.Sprintf(`{"action":"radio_move_up","itemId":%q}`, secondStationID))
	stations = player["stations"].([]any)
	if stations[0].(map[string]any)["name"] != "B 电台" {
		t.Fatalf("radio station order was not changed: %#v", stations)
	}

	player = control(`{"action":"play_url","title":"测试音乐","url":"http://media.example/test.mp3?token=private"}`)
	if player["transport"].(map[string]any)["value"] != "playing" || player["positionSeconds"].(map[string]any)["value"] != float64(2) {
		t.Fatalf("KPlayer play was not verified: %#v", player)
	}
	current := player["current"].(map[string]any)
	if strings.Contains(current["source"].(string), "token") {
		t.Fatalf("media query leaked into API response: %#v", current)
	}
	player = control(`{"action":"volume_set","volume":25}`)
	if player["volume"].(map[string]any)["value"] != float64(25) {
		t.Fatalf("KPlayer volume was not verified: %#v", player)
	}
	player = control(`{"action":"timer_set","durationMinutes":60}`)
	stopTimer := player["stopTimer"].(map[string]any)
	if stopTimer["active"] != true || stopTimer["remainingSeconds"].(float64) != float64(3600) {
		t.Fatalf("stop timer was not scheduled: %#v", stopTimer)
	}
	player = control(`{"action":"timer_cancel"}`)
	if player["stopTimer"].(map[string]any)["active"] != false {
		t.Fatalf("stop timer was not cancelled: %#v", player["stopTimer"])
	}
	for _, step := range []struct{ action, expected string }{{"pause", "paused"}, {"resume", "playing"}, {"stop", "stopped"}} {
		player = control(fmt.Sprintf(`{"action":%q}`, step.action))
		if player["transport"].(map[string]any)["value"] != step.expected {
			t.Fatalf("%s was not verified: %#v", step.action, player)
		}
	}

	invalid, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/player/control", strings.NewReader(`{"action":"play_url","url":"file:///etc/passwd"}`))
	invalidResponse, err := server.Client().Do(invalid)
	if err != nil {
		t.Fatal(err)
	}
	invalidResponse.Body.Close()
	if invalidResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsafe player URL returned %d", invalidResponse.StatusCode)
	}

	invalidTimer, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/player/control", strings.NewReader(`{"action":"timer_set","durationMinutes":61}`))
	invalidTimer.Header.Set("Content-Type", "application/json")
	invalidTimerResponse, err := server.Client().Do(invalidTimer)
	if err != nil {
		t.Fatal(err)
	}
	invalidTimerResponse.Body.Close()
	if invalidTimerResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized stop timer returned %d", invalidTimerResponse.StatusCode)
	}

	invalidVolume, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/player/control", strings.NewReader(`{"action":"volume_set","volume":101}`))
	invalidVolume.Header.Set("Content-Type", "application/json")
	invalidVolumeResponse, err := server.Client().Do(invalidVolume)
	if err != nil {
		t.Fatal(err)
	}
	invalidVolumeResponse.Body.Close()
	if invalidVolumeResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("out-of-range volume returned %d", invalidVolumeResponse.StatusCode)
	}

	inactiveVolume, _ := http.NewRequest(http.MethodPost, server.URL+api.BasePath+"/player/control", strings.NewReader(`{"action":"volume_set","volume":25}`))
	inactiveVolume.Header.Set("Content-Type", "application/json")
	inactiveVolumeResponse, err := server.Client().Do(inactiveVolume)
	if err != nil {
		t.Fatal(err)
	}
	inactiveVolumeResponse.Body.Close()
	if inactiveVolumeResponse.StatusCode != http.StatusConflict {
		t.Fatalf("inactive player volume returned %d", inactiveVolumeResponse.StatusCode)
	}
}
