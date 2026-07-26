package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"sanyin.local/config/service/internal/domain"
)

type kplayerDiscoveryRunner struct {
	port int
}

func (r kplayerDiscoveryRunner) Run(_ context.Context, script string) (string, error) {
	if script == discoverKPlayerScript {
		return fmt.Sprintf("result=ready\nkplayer_port=%d\n", r.port), nil
	}
	return "", nil
}

func TestUPnPPlayerDiscoversAVTransportAndEscapesMediaURL(t *testing.T) {
	actions := []string{}
	bodies := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<?xml version="1.0"?><root><device><serviceList><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/av/control.xml</controlURL></service></serviceList></device></root>`)
			return
		}
		action := strings.Trim(r.Header.Get("SOAPACTION"), `"`)
		actions = append(actions, action)
		payload, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(payload))
		switch {
		case strings.HasSuffix(action, "#GetTransportInfo"):
			fmt.Fprint(w, `<CurrentTransportState>PAUSED_PLAYBACK</CurrentTransportState>`)
		case strings.HasSuffix(action, "#GetPositionInfo"):
			fmt.Fprint(w, `<TrackDuration>01:02:03</TrackDuration><RelTime>00:01:07</RelTime>`)
		case strings.HasSuffix(action, "#GetMediaInfo"):
			fmt.Fprint(w, `<CurrentURI>https://media.example/song.mp3?token=a&amp;part=2</CurrentURI>`)
		default:
			fmt.Fprint(w, `<ok/>`)
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(parsed.Port())
	player := newUPnPPlayer(kplayerDiscoveryRunner{port: port})

	mediaURL := "https://media.example/song.mp3?token=a&part=2"
	if err := player.SetURI(context.Background(), mediaURL); err != nil {
		t.Fatal(err)
	}
	if err := player.Play(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := player.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "paused" || status.DurationSeconds != 3723 || status.PositionSeconds != 67 || status.CurrentURI != mediaURL {
		t.Fatalf("unexpected transport status: %#v", status)
	}
	joined := strings.Join(bodies, "\n")
	if !strings.Contains(joined, "token=a&amp;part=2") || strings.Contains(joined, "token=a&part=2") {
		t.Fatalf("media URL was not XML escaped: %s", joined)
	}
	if len(actions) != 5 || !strings.HasSuffix(actions[0], "#SetAVTransportURI") || !strings.HasSuffix(actions[1], "#Play") || !strings.HasSuffix(actions[4], "#GetMediaInfo") {
		t.Fatalf("unexpected SOAP actions: %#v", actions)
	}
}

func TestUPnPPlayerUsesRenderingControlForVolume(t *testing.T) {
	volume := 30
	actions := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<?xml version="1.0"?><root><device><serviceList><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/av/control.xml</controlURL></service><service><serviceType>urn:schemas-upnp-org:service:RenderingControl:1</serviceType><controlURL>/render/control.xml</controlURL></service></serviceList></device></root>`)
			return
		}
		action := strings.Trim(r.Header.Get("SOAPACTION"), `"`)
		actions = append(actions, action)
		payload, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(action, "#SetVolume"):
			value, _ := strconv.Atoi(xmlText(payload, "DesiredVolume"))
			volume = value
		case strings.HasSuffix(action, "#GetVolume"):
			fmt.Fprintf(w, `<CurrentVolume>%d</CurrentVolume>`, volume)
		case strings.HasSuffix(action, "#GetTransportInfo"):
			fmt.Fprint(w, `<CurrentTransportState>PLAYING</CurrentTransportState>`)
		case strings.HasSuffix(action, "#GetPositionInfo"):
			fmt.Fprint(w, `<TrackDuration>00:01:00</TrackDuration><RelTime>00:00:10</RelTime>`)
		default:
			fmt.Fprint(w, `<ok/>`)
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(parsed.Port())
	player := newUPnPPlayer(kplayerDiscoveryRunner{port: port})

	if err := player.SetVolume(context.Background(), 25); err != nil {
		t.Fatal(err)
	}
	status, err := player.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Volume == nil || *status.Volume != 25 {
		t.Fatalf("RenderingControl volume was not read back: %#v", status)
	}
	if len(actions) < 2 || !strings.Contains(actions[0], "RenderingControl:1#SetVolume") || !strings.Contains(actions[len(actions)-1], "RenderingControl:1#GetVolume") {
		t.Fatalf("unexpected RenderingControl actions: %#v", actions)
	}
}

type fakePlayerController struct {
	mu                    sync.Mutex
	state                 playerTransport
	uri                   string
	played                []string
	volumeSet             bool
	failStatusAfterVolume bool
	setVolumeStarted      chan struct{}
	setVolumeRelease      chan struct{}
}

func (f *fakePlayerController) Status(_ context.Context) (playerTransport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.volumeSet && f.failStatusAfterVolume {
		return playerTransport{}, errors.New("transient GetTransportInfo failure")
	}
	return f.state, nil
}

func (f *fakePlayerController) Volume(_ context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state.Volume == nil {
		return 0, errors.New("volume unavailable")
	}
	return *f.state.Volume, nil
}

func (f *fakePlayerController) SetURI(_ context.Context, value string) error {
	f.mu.Lock()
	f.uri = value
	f.mu.Unlock()
	return nil
}

func (f *fakePlayerController) Play(_ context.Context) error {
	f.mu.Lock()
	f.state = playerTransport{State: "playing", PositionSeconds: 0, DurationSeconds: 30}
	f.played = append(f.played, f.uri)
	f.mu.Unlock()
	return nil
}

func (f *fakePlayerController) Pause(_ context.Context) error {
	f.mu.Lock()
	f.state.State = "paused"
	f.mu.Unlock()
	return nil
}

func (f *fakePlayerController) Stop(_ context.Context) error {
	f.mu.Lock()
	f.state.State = "stopped"
	f.mu.Unlock()
	return nil
}

func (f *fakePlayerController) SetVolume(_ context.Context, volume int) error {
	if f.setVolumeStarted != nil {
		close(f.setVolumeStarted)
		<-f.setVolumeRelease
	}
	f.mu.Lock()
	f.state.Volume = &volume
	f.volumeSet = true
	f.mu.Unlock()
	return nil
}

func (f *fakePlayerController) setState(state string) {
	f.mu.Lock()
	f.state.State = state
	f.mu.Unlock()
}

func (f *fakePlayerController) playedURLs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.played...)
}

func TestPlaybackManagerControlsQueueAndRedactsSources(t *testing.T) {
	controller := &fakePlayerController{state: playerTransport{State: "stopped"}}
	manager := newPlaybackManager(controller, kplayerDiscoveryRunner{}, func() time.Time { return time.UnixMilli(1234) })
	manager.pollEvery = time.Hour
	ctx := context.Background()

	player, err := manager.control(ctx, domain.PlayerCommand{Action: "play_url", Title: "第一首", URL: "https://media.example/one.mp3?token=secret"})
	if err != nil {
		t.Fatal(err)
	}
	if player.Transport.Value != "playing" || player.Current == nil || player.Current.Title != "第一首" {
		t.Fatalf("direct playback did not start: %#v", player)
	}
	if strings.Contains(player.Current.Source, "token") {
		t.Fatalf("signed query leaked into response: %#v", player.Current)
	}
	player, err = manager.control(ctx, domain.PlayerCommand{Action: "queue_add", Title: "第二首", URL: "http://media.example/two.mp3"})
	if err != nil || len(player.Queue) != 2 {
		t.Fatalf("queue item was not added: %#v %v", player, err)
	}
	if _, err := manager.control(ctx, domain.PlayerCommand{Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	if player, err = manager.control(ctx, domain.PlayerCommand{Action: "resume"}); err != nil || player.Transport.Value != "playing" {
		t.Fatalf("resume failed: %#v %v", player, err)
	}
	if player, err = manager.control(ctx, domain.PlayerCommand{Action: "next"}); err != nil || player.CurrentIndex != 1 || player.Current.Title != "第二首" {
		t.Fatalf("next item did not play: %#v %v", player, err)
	}
	if player, err = manager.control(ctx, domain.PlayerCommand{Action: "stop"}); err != nil || player.Transport.Value != "stopped" {
		t.Fatalf("stop failed: %#v %v", player, err)
	}
}

func TestPlaybackManagerSetsVolumeOnlyDuringLocalPlayback(t *testing.T) {
	initialVolume := 30
	controller := &fakePlayerController{
		state:                 playerTransport{State: "paused", Volume: &initialVolume},
		failStatusAfterVolume: true,
	}
	manager := newPlaybackManager(controller, kplayerDiscoveryRunner{}, time.Now)
	requestedVolume := 25
	if _, err := manager.control(context.Background(), domain.PlayerCommand{Action: "volume_set", Volume: &requestedVolume}); !errors.Is(err, ErrPlaybackInactive) {
		t.Fatalf("paused player accepted volume change: %v", err)
	}
	controller.setState("playing")
	player, err := manager.control(context.Background(), domain.PlayerCommand{Action: "volume_set", Volume: &requestedVolume})
	if err != nil || player.Volume.Value != 25 {
		t.Fatalf("volume was not set and read back: %#v %v", player, err)
	}
	controller.failStatusAfterVolume = false
	controller.setState("stopped")
	if _, err := manager.control(context.Background(), domain.PlayerCommand{Action: "volume_set", Volume: &requestedVolume}); !errors.Is(err, ErrPlaybackInactive) {
		t.Fatalf("inactive player accepted volume change: %v", err)
	}
}

func TestPlaybackManagerSerializesStatusWhileSettingVolume(t *testing.T) {
	initialVolume := 30
	controller := &fakePlayerController{
		state:            playerTransport{State: "playing", Volume: &initialVolume},
		setVolumeStarted: make(chan struct{}),
		setVolumeRelease: make(chan struct{}),
	}
	manager := newPlaybackManager(controller, kplayerDiscoveryRunner{}, time.Now)
	requestedVolume := 25
	controlDone := make(chan error, 1)
	go func() {
		_, err := manager.control(context.Background(), domain.PlayerCommand{Action: "volume_set", Volume: &requestedVolume})
		controlDone <- err
	}()

	select {
	case <-controller.setVolumeStarted:
	case <-time.After(time.Second):
		t.Fatal("volume update did not start")
	}

	statusDone := make(chan error, 1)
	go func() {
		_, err := manager.status(context.Background())
		statusDone <- err
	}()
	select {
	case err := <-statusDone:
		t.Fatalf("status overlapped the volume transaction: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(controller.setVolumeRelease)
	if err := <-controlDone; err != nil {
		t.Fatal(err)
	}
	if err := <-statusDone; err != nil {
		t.Fatal(err)
	}
}

func TestPlaybackQueueAutomaticallyAdvancesAfterCompletion(t *testing.T) {
	controller := &fakePlayerController{state: playerTransport{State: "stopped"}}
	manager := newPlaybackManager(controller, kplayerDiscoveryRunner{}, time.Now)
	manager.pollEvery = 10 * time.Millisecond
	ctx := context.Background()
	if _, err := manager.control(ctx, domain.PlayerCommand{Action: "play_url", Title: "A", URL: "http://media.example/a.mp3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.control(ctx, domain.PlayerCommand{Action: "queue_add", Title: "B", URL: "http://media.example/b.mp3"}); err != nil {
		t.Fatal(err)
	}
	controller.setState("stopped")
	deadline := time.Now().Add(time.Second)
	for len(controller.playedURLs()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	played := controller.playedURLs()
	if len(played) != 2 || played[1] != "http://media.example/b.mp3" {
		t.Fatalf("queue did not advance: %#v", played)
	}
	_, _ = manager.control(ctx, domain.PlayerCommand{Action: "stop"})
}

func TestPlaybackManagerMaintainsNetworkRadioStations(t *testing.T) {
	controller := &fakePlayerController{state: playerTransport{State: "stopped"}}
	manager := newPlaybackManager(controller, kplayerDiscoveryRunner{}, func() time.Time { return time.UnixMilli(5678) })
	manager.pollEvery = time.Hour
	ctx := context.Background()
	player, err := manager.control(ctx, domain.PlayerCommand{Action: "radio_add", Title: "测试电台", URL: "https://radio.example/live.mp3?key=private"})
	if err != nil || len(player.Stations) != 1 || strings.Contains(player.Stations[0].Source, "key") {
		t.Fatalf("radio station was not stored safely: %#v %v", player, err)
	}
	stationID := player.Stations[0].ID
	player, err = manager.control(ctx, domain.PlayerCommand{Action: "radio_play", ItemID: stationID})
	if err != nil || player.Transport.Value != "playing" || player.Current == nil || player.Current.Kind != "radio" {
		t.Fatalf("radio station did not play: %#v %v", player, err)
	}
	controller.mu.Lock()
	controller.state = playerTransport{State: "stopped", PositionSeconds: 2147483, DurationSeconds: 2147483}
	controller.mu.Unlock()
	player, err = manager.status(ctx)
	if err != nil || player.Transport.Value != "playing" || player.PositionSeconds.Value != 0 || player.DurationSeconds.Value != 0 {
		t.Fatalf("live radio status was not normalized: %#v %v", player, err)
	}
	if _, err := manager.control(ctx, domain.PlayerCommand{Action: "stop"}); err != nil {
		t.Fatal(err)
	}
	player, err = manager.control(ctx, domain.PlayerCommand{Action: "radio_remove", ItemID: stationID})
	if err != nil || len(player.Stations) != 0 {
		t.Fatalf("radio station was not removed: %#v %v", player, err)
	}
}

func TestPlaybackManagerRecoversCurrentStationFromKPlayerURI(t *testing.T) {
	controller := &fakePlayerController{state: playerTransport{
		State:      "playing",
		CurrentURI: "http://radio.example/fm101.m3u8?",
	}}
	manager := newPlaybackManager(controller, kplayerDiscoveryRunner{}, time.Now)
	manager.stationsLoaded = true
	manager.stations = []radioEntry{{ID: "fm101", Name: "江苏交通广播 FM101.1", URL: "http://radio.example/fm101.m3u8"}}

	player, err := manager.status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if player.Current == nil || player.Current.Title != "江苏交通广播 FM101.1" || player.Current.Kind != "radio" || player.Current.Source != "http://radio.example/fm101.m3u8" {
		t.Fatalf("current station was not recovered from KPlayer: %#v", player.Current)
	}
}

func TestPlaybackManagerReordersAndPersistsRadioStations(t *testing.T) {
	runner := &recordingRunner{}
	controller := &fakePlayerController{state: playerTransport{State: "stopped"}}
	manager := newPlaybackManager(controller, runner, func() time.Time { return time.UnixMilli(6789) })
	ctx := context.Background()

	for index, name := range []string{"A 电台", "B 电台", "C 电台"} {
		if _, err := manager.control(ctx, domain.PlayerCommand{Action: "radio_add", Title: name, URL: fmt.Sprintf("https://radio.example/%d.mp3", index)}); err != nil {
			t.Fatal(err)
		}
	}
	player, err := manager.status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cID := player.Stations[2].ID
	player, err = manager.control(ctx, domain.PlayerCommand{Action: "radio_move_up", ItemID: cID})
	if err != nil || player.Stations[1].Name != "C 电台" {
		t.Fatalf("station was not moved up: %#v %v", player.Stations, err)
	}
	player, err = manager.control(ctx, domain.PlayerCommand{Action: "radio_move_down", ItemID: cID})
	if err != nil || player.Stations[2].Name != "C 电台" {
		t.Fatalf("station was not moved down: %#v %v", player.Stations, err)
	}

	var persisted []radioEntry
	if err := json.Unmarshal(runner.files[radioStationsPath], &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 3 || persisted[2].Name != "C 电台" {
		t.Fatalf("station order was not persisted: %#v", persisted)
	}
}

func TestPlaybackManagerPersistsAndRedactsPlayerScenes(t *testing.T) {
	runner := &recordingRunner{}
	controller := &fakePlayerController{state: playerTransport{State: "stopped"}}
	manager := newPlaybackManager(controller, runner, func() time.Time { return time.UnixMilli(7890) })
	ctx := context.Background()

	scenes, err := manager.createScene(ctx, domain.PlayerSceneInput{
		Name: "  专注阅读  ", Icon: "focus", Title: "轻音乐", URL: "https://media.example/focus.mp3?token=private", Volume: 24, TimerMinutes: 45,
	})
	if err != nil || len(scenes) != 1 {
		t.Fatalf("scene was not created: %#v %v", scenes, err)
	}
	if scenes[0].Name != "专注阅读" || scenes[0].Volume != 24 || strings.Contains(scenes[0].Source, "token") {
		t.Fatalf("scene response was not normalized or redacted: %#v", scenes[0])
	}
	if scenes[0].Schedule.Enabled || scenes[0].Schedule.Time != "07:30" || len(scenes[0].Schedule.Weekdays) != 7 {
		t.Fatalf("legacy scene did not receive a safe disabled schedule: %#v", scenes[0].Schedule)
	}
	var persisted []sceneEntry
	if err := json.Unmarshal(runner.files[playerScenesPath], &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || !strings.Contains(persisted[0].URL, "token=private") {
		t.Fatalf("complete private URL was not persisted on-device: %#v", persisted)
	}

	id := scenes[0].ID
	scenes, err = manager.updateScene(ctx, id, domain.PlayerSceneInput{
		Name: "深度工作", Icon: "music", Title: "白噪音", URL: "", Volume: 18, TimerMinutes: 60,
	})
	if err != nil || len(scenes) != 1 || scenes[0].ID != id || scenes[0].Name != "深度工作" {
		t.Fatalf("scene was not updated in place: %#v %v", scenes, err)
	}
	if err := json.Unmarshal(runner.files[playerScenesPath], &persisted); err != nil || !strings.Contains(persisted[0].URL, "token=private") {
		t.Fatalf("empty scene update did not preserve the complete URL: %#v %v", persisted, err)
	}
	scenes, err = manager.deleteScene(ctx, id)
	if err != nil || len(scenes) != 0 {
		t.Fatalf("scene was not deleted: %#v %v", scenes, err)
	}
	if _, err := manager.createScene(ctx, domain.PlayerSceneInput{Name: "坏场景", Icon: "unknown", URL: "file:///etc/passwd", Volume: 101}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid scene was accepted: %v", err)
	}
}

func TestPlaybackManagerRejectsOverlappingSceneSchedules(t *testing.T) {
	runner := &recordingRunner{}
	manager := newPlaybackManager(&fakePlayerController{state: playerTransport{State: "stopped"}}, runner, func() time.Time {
		return time.Date(2026, 7, 27, 6, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	})
	ctx := context.Background()

	if _, err := manager.createScene(ctx, domain.PlayerSceneInput{
		Name: "工作日清晨", Icon: "morning", URL: "https://media.example/morning.mp3", Volume: 25,
		Schedule: domain.PlayerSceneScheduleInput{Enabled: true, Time: "07:30", Weekdays: []int{1, 2, 3, 4, 5}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.createScene(ctx, domain.PlayerSceneInput{
		Name: "周一提醒", Icon: "music", URL: "https://media.example/monday.mp3", Volume: 20,
		Schedule: domain.PlayerSceneScheduleInput{Enabled: true, Time: "07:30", Weekdays: []int{1}},
	}); !errors.Is(err, ErrScheduleConflict) {
		t.Fatalf("overlapping scene schedule was accepted: %v", err)
	}
	if _, err := manager.createScene(ctx, domain.PlayerSceneInput{
		Name: "周末清晨", Icon: "relax", URL: "https://media.example/weekend.mp3", Volume: 18,
		Schedule: domain.PlayerSceneScheduleInput{Enabled: true, Time: "07:30", Weekdays: []int{6, 7}},
	}); err != nil {
		t.Fatalf("non-overlapping scene schedule was rejected: %v", err)
	}
}

func TestPlaybackManagerRunsScheduledSceneOncePerDeviceMinute(t *testing.T) {
	runner := &recordingRunner{}
	volume := 10
	controller := &fakePlayerController{state: playerTransport{State: "stopped", Volume: &volume}}
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 7, 27, 7, 30, 10, 0, location)
	manager := newPlaybackManager(controller, runner, func() time.Time { return now })
	manager.pollEvery = time.Hour
	ctx := context.Background()

	scenes, err := manager.createScene(ctx, domain.PlayerSceneInput{
		Name: "自动早晨", Icon: "morning", Title: "清晨音乐", URL: "https://media.example/morning.mp3?token=private", Volume: 28,
		Schedule: domain.PlayerSceneScheduleInput{Enabled: true, Time: "07:30", Weekdays: []int{1, 2, 3, 4, 5, 6, 7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager.checkScheduledScenes(ctx)
	manager.checkScheduledScenes(ctx)
	if played := controller.playedURLs(); len(played) != 1 || !strings.Contains(played[0], "token=private") {
		t.Fatalf("scheduled scene did not run exactly once in the minute: %#v", played)
	}

	var persisted []sceneEntry
	if err := json.Unmarshal(runner.files[playerScenesPath], &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0].Schedule.LastTriggeredKey != "2026-07-27@07:30" || persisted[0].Schedule.LastRunOutcome != "succeeded" || persisted[0].Schedule.LastRunAt == nil {
		t.Fatalf("scheduled run metadata was not persisted: %#v", persisted)
	}
	listed := manager.sceneSnapshot()
	if listed[0].Schedule.NextRunAt == nil || listed[0].Schedule.NextRunAt.Format("2006-01-02 15:04 -0700") != "2026-07-28 07:30 +0800" {
		t.Fatalf("next scheduled run was not calculated in device time: %#v", listed[0].Schedule)
	}

	now = now.AddDate(0, 0, 1)
	manager.checkScheduledScenes(ctx)
	if played := controller.playedURLs(); len(played) != 2 {
		t.Fatalf("scheduled scene did not run again on the next day: %#v", played)
	}
	if scenes[0].ID == "" {
		t.Fatal("scene id was not generated")
	}
}

func TestParseDeviceTimezoneOffset(t *testing.T) {
	location, err := parseDeviceTimezoneOffset("offset=+0800\n")
	if err != nil || time.Unix(0, 0).In(location).Format("-0700") != "+0800" {
		t.Fatalf("valid timezone offset was not parsed: %v %v", location, err)
	}
	if _, err := parseDeviceTimezoneOffset("offset=+2460\n"); err == nil {
		t.Fatal("invalid timezone offset was accepted")
	}
}

func TestPlaybackManagerAppliesSceneAsSerializedPlaybackSettings(t *testing.T) {
	initialVolume := 12
	controller := &fakePlayerController{state: playerTransport{State: "stopped", Volume: &initialVolume}}
	manager := newPlaybackManager(controller, kplayerDiscoveryRunner{}, func() time.Time { return time.UnixMilli(8901) })
	manager.pollEvery = time.Hour
	ctx := context.Background()
	scenes, err := manager.createScene(ctx, domain.PlayerSceneInput{
		Name: "睡前放松", Icon: "sleep", Title: "睡眠音乐", URL: "https://media.example/sleep.mp3?signature=secret", Volume: 16, TimerMinutes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	application, err := manager.applyScene(ctx, scenes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.cancelStopTimer()
	if application.Player.Transport.Value != "playing" || application.Player.Volume.Value != 16 || !application.Player.StopTimer.Active {
		t.Fatalf("scene settings were not applied and read back: %#v", application)
	}
	if application.Player.Current == nil || application.Player.Current.Title != "睡眠音乐" || strings.Contains(application.Scene.Source, "signature") {
		t.Fatalf("scene playback response is incomplete or unsafe: %#v", application)
	}
	played := controller.playedURLs()
	if len(played) != 1 || !strings.Contains(played[0], "signature=secret") {
		t.Fatalf("player did not receive the complete stored URL: %#v", played)
	}
}

func TestPlaybackStopTimerStopsAndCanBeCancelled(t *testing.T) {
	controller := &fakePlayerController{state: playerTransport{State: "playing"}}
	manager := newPlaybackManager(controller, kplayerDiscoveryRunner{}, time.Now)
	if err := manager.scheduleStop(30 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	player := manager.domainPlayer(time.Now())
	if !player.StopTimer.Active || player.StopTimer.RemainingSeconds < 1 {
		t.Fatalf("stop timer was not exposed: %#v", player.StopTimer)
	}
	deadline := time.Now().Add(time.Second)
	for {
		status, _ := controller.Status(context.Background())
		if status.State == "stopped" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stop timer did not stop playback")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if player = manager.domainPlayer(time.Now()); player.StopTimer.Active {
		t.Fatalf("expired stop timer remained active: %#v", player.StopTimer)
	}

	controller.setState("playing")
	if err := manager.scheduleStop(30 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	manager.cancelStopTimer()
	time.Sleep(60 * time.Millisecond)
	status, _ := controller.Status(context.Background())
	if status.State != "playing" || manager.domainPlayer(time.Now()).StopTimer.Active {
		t.Fatalf("cancelled stop timer still fired: %#v", status)
	}
}

func TestMediaURLValidationRejectsUnsafeForms(t *testing.T) {
	for _, value := range []string{"", "file:///etc/passwd", "ftp://media.example/file", "http://user:pass@example.com/file", "http://example.com/file#fragment", "http://example.com/line\nbreak"} {
		if _, err := validateMediaURL(value); err == nil {
			t.Fatalf("unsafe media URL was accepted: %q", value)
		}
	}
}
