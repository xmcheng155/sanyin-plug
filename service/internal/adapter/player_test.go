package adapter

import (
	"context"
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
	if status.State != "paused" || status.DurationSeconds != 3723 || status.PositionSeconds != 67 {
		t.Fatalf("unexpected transport status: %#v", status)
	}
	joined := strings.Join(bodies, "\n")
	if !strings.Contains(joined, "token=a&amp;part=2") || strings.Contains(joined, "token=a&part=2") {
		t.Fatalf("media URL was not XML escaped: %s", joined)
	}
	if len(actions) != 4 || !strings.HasSuffix(actions[0], "#SetAVTransportURI") || !strings.HasSuffix(actions[1], "#Play") {
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
