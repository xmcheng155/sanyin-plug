package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"sanyin.local/config/service/internal/domain"
)

const radioStationsPath = "/mnt/UDISK/sanyin-config/radio-stations.json"

const (
	avTransportService      = "urn:schemas-upnp-org:service:AVTransport:1"
	renderingControlService = "urn:schemas-upnp-org:service:RenderingControl:1"
)

const discoverKPlayerScript = `
port="$(netstat -lntp 2>/dev/null | awk '$7 ~ /\/KPlayer$/ { address=$4; sub(/^.*:/, "", address); print address; exit }')"
case "$port" in ''|*[!0-9]*) printf 'result=not_ready\n' ;; *) printf 'result=ready\nkplayer_port=%s\n' "$port" ;; esac
`

type playerTransport struct {
	State           string
	Volume          *int
	CurrentURI      string
	PositionSeconds int
	DurationSeconds int
}

type playerController interface {
	Status(context.Context) (playerTransport, error)
	Volume(context.Context) (int, error)
	SetURI(context.Context, string) error
	Play(context.Context) error
	Pause(context.Context) error
	Stop(context.Context) error
	SetVolume(context.Context, int) error
}

type deviceHTTPHostProvider interface {
	DeviceHTTPHost(context.Context) (string, error)
}

type upnpPlayer struct {
	runner ShellRunner
	client *http.Client

	mu             sync.Mutex
	controlURLs    map[string]string
	controlExpires time.Time
}

func newUPnPPlayer(runner ShellRunner) *upnpPlayer {
	return &upnpPlayer{runner: runner, client: &http.Client{Timeout: 5 * time.Second}, controlURLs: map[string]string{}}
}

func (p *upnpPlayer) endpoint(ctx context.Context, serviceType string) (string, error) {
	p.mu.Lock()
	if endpoint := p.controlURLs[serviceType]; endpoint != "" && time.Now().Before(p.controlExpires) {
		p.mu.Unlock()
		return endpoint, nil
	}
	p.mu.Unlock()

	output, err := p.runner.Run(ctx, discoverKPlayerScript)
	if err != nil {
		return "", err
	}
	values := parseKeyValues(output)
	port, err := strconv.Atoi(values["kplayer_port"])
	if values["result"] != "ready" || err != nil || port < 1 || port > 65535 {
		return "", ErrCapabilityNotReady
	}
	host := "127.0.0.1"
	if provider, ok := p.runner.(deviceHTTPHostProvider); ok {
		host, err = provider.DeviceHTTPHost(ctx)
		if err != nil {
			return "", err
		}
	}
	if net.ParseIP(host) == nil {
		return "", errors.New("invalid device HTTP host")
	}
	base := "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
	if err != nil {
		return "", err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("discover KPlayer description: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("KPlayer description returned HTTP %d", response.StatusCode)
	}
	var description struct {
		Device struct {
			Services []struct {
				Type       string `xml:"serviceType"`
				ControlURL string `xml:"controlURL"`
			} `xml:"serviceList>service"`
		} `xml:"device"`
	}
	if err := xml.NewDecoder(io.LimitReader(response.Body, 256*1024)).Decode(&description); err != nil {
		return "", fmt.Errorf("decode KPlayer description: %w", err)
	}
	controlURLs := map[string]string{}
	for _, service := range description.Device.Services {
		if service.Type != avTransportService && service.Type != renderingControlService {
			continue
		}
		parsedPath, err := url.Parse(service.ControlURL)
		if err != nil || service.ControlURL == "" || !strings.HasPrefix(parsedPath.Path, "/") || parsedPath.IsAbs() || parsedPath.Host != "" {
			continue
		}
		controlURLs[service.Type] = base + parsedPath.EscapedPath()
	}
	endpoint := controlURLs[serviceType]
	if endpoint == "" {
		return "", fmt.Errorf("%w: KPlayer %s control URL is unavailable", ErrCapabilityNotReady, serviceType)
	}
	p.mu.Lock()
	p.controlURLs = controlURLs
	p.controlExpires = time.Now().Add(5 * time.Minute)
	p.mu.Unlock()
	return endpoint, nil
}

func (p *upnpPlayer) invalidateEndpoint() {
	p.mu.Lock()
	p.controlURLs = map[string]string{}
	p.controlExpires = time.Time{}
	p.mu.Unlock()
}

func soapEnvelope(serviceType, action string, arguments map[string]string) ([]byte, error) {
	var body bytes.Buffer
	body.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:`)
	body.WriteString(action)
	body.WriteString(` xmlns:u="`)
	body.WriteString(serviceType)
	body.WriteString(`">`)
	order := []string{"InstanceID", "Channel", "DesiredVolume", "CurrentURI", "CurrentURIMetaData", "Speed"}
	for _, key := range order {
		value, ok := arguments[key]
		if !ok {
			continue
		}
		body.WriteByte('<')
		body.WriteString(key)
		body.WriteByte('>')
		if err := xml.EscapeText(&body, []byte(value)); err != nil {
			return nil, err
		}
		body.WriteString("</")
		body.WriteString(key)
		body.WriteByte('>')
	}
	body.WriteString("</u:")
	body.WriteString(action)
	body.WriteString(`></s:Body></s:Envelope>`)
	return body.Bytes(), nil
}

func (p *upnpPlayer) call(ctx context.Context, serviceType, action string, arguments map[string]string) ([]byte, error) {
	endpoint, err := p.endpoint(ctx, serviceType)
	if err != nil {
		return nil, err
	}
	payload, err := soapEnvelope(serviceType, action, arguments)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	request.Header.Set("SOAPACTION", `"`+serviceType+`#`+action+`"`)
	response, err := p.client.Do(request)
	if err != nil {
		p.invalidateEndpoint()
		return nil, fmt.Errorf("KPlayer %s: %w", action, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("KPlayer %s returned HTTP %d", action, response.StatusCode)
	}
	if bytes.Contains(responseBody, []byte("<s:Fault>")) || bytes.Contains(responseBody, []byte("<SOAP-ENV:Fault>")) {
		return nil, fmt.Errorf("KPlayer %s returned SOAP fault", action)
	}
	return responseBody, nil
}

func xmlText(payload []byte, localName string) string {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != localName {
			continue
		}
		var value string
		if decoder.DecodeElement(&value, &start) == nil {
			return strings.TrimSpace(value)
		}
	}
}

func parseUPnPDuration(value string) int {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0
	}
	hours, errH := strconv.Atoi(parts[0])
	minutes, errM := strconv.Atoi(parts[1])
	secondsPart := strings.SplitN(parts[2], ".", 2)[0]
	seconds, errS := strconv.Atoi(secondsPart)
	if errH != nil || errM != nil || errS != nil || hours < 0 || minutes < 0 || seconds < 0 {
		return 0
	}
	return hours*3600 + minutes*60 + seconds
}

func normalizeTransport(value string) string {
	switch strings.ToUpper(value) {
	case "PLAYING":
		return "playing"
	case "PAUSED_PLAYBACK", "PAUSED_RECORDING":
		return "paused"
	case "STOPPED":
		return "stopped"
	case "TRANSITIONING":
		return "transitioning"
	case "NO_MEDIA_PRESENT":
		return "no_media"
	default:
		return "unknown"
	}
}

func (p *upnpPlayer) Status(ctx context.Context) (playerTransport, error) {
	transportBody, err := p.call(ctx, avTransportService, "GetTransportInfo", map[string]string{"InstanceID": "0"})
	if err != nil {
		return playerTransport{}, err
	}
	result := playerTransport{State: normalizeTransport(xmlText(transportBody, "CurrentTransportState"))}
	positionBody, positionErr := p.call(ctx, avTransportService, "GetPositionInfo", map[string]string{"InstanceID": "0"})
	if positionErr == nil {
		result.PositionSeconds = parseUPnPDuration(xmlText(positionBody, "RelTime"))
		result.DurationSeconds = parseUPnPDuration(xmlText(positionBody, "TrackDuration"))
		result.CurrentURI = xmlText(positionBody, "TrackURI")
	}
	if result.CurrentURI == "" {
		mediaBody, mediaErr := p.call(ctx, avTransportService, "GetMediaInfo", map[string]string{"InstanceID": "0"})
		if mediaErr == nil {
			result.CurrentURI = xmlText(mediaBody, "CurrentURI")
		}
	}
	if volume, volumeErr := p.Volume(ctx); volumeErr == nil {
		result.Volume = &volume
	}
	return result, nil
}

func (p *upnpPlayer) Volume(ctx context.Context) (int, error) {
	volumeBody, err := p.call(ctx, renderingControlService, "GetVolume", map[string]string{"InstanceID": "0", "Channel": "Master"})
	if err != nil {
		return 0, err
	}
	volume, err := strconv.Atoi(xmlText(volumeBody, "CurrentVolume"))
	if err != nil || volume < 0 || volume > 100 {
		return 0, errors.New("KPlayer returned invalid volume")
	}
	return volume, nil
}

func (p *upnpPlayer) SetURI(ctx context.Context, mediaURL string) error {
	_, err := p.call(ctx, avTransportService, "SetAVTransportURI", map[string]string{"InstanceID": "0", "CurrentURI": mediaURL, "CurrentURIMetaData": ""})
	return err
}

func (p *upnpPlayer) Play(ctx context.Context) error {
	_, err := p.call(ctx, avTransportService, "Play", map[string]string{"InstanceID": "0", "Speed": "1"})
	return err
}

func (p *upnpPlayer) Pause(ctx context.Context) error {
	_, err := p.call(ctx, avTransportService, "Pause", map[string]string{"InstanceID": "0"})
	return err
}

func (p *upnpPlayer) Stop(ctx context.Context) error {
	_, err := p.call(ctx, avTransportService, "Stop", map[string]string{"InstanceID": "0"})
	return err
}

func (p *upnpPlayer) SetVolume(ctx context.Context, volume int) error {
	if volume < 0 || volume > 100 {
		return ErrInvalidInput
	}
	_, err := p.call(ctx, renderingControlService, "SetVolume", map[string]string{
		"InstanceID":    "0",
		"Channel":       "Master",
		"DesiredVolume": strconv.Itoa(volume),
	})
	return err
}

type playerEntry struct {
	ID    string
	Title string
	URL   string
	Kind  string
}

type radioEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type playbackManager struct {
	controller playerController
	runner     ShellRunner
	now        func() time.Time
	pollEvery  time.Duration

	operationMu         sync.Mutex
	mu                  sync.Mutex
	queue               []playerEntry
	currentIndex        int
	stations            []radioEntry
	stationsLoaded      bool
	last                playerTransport
	radioTransport      string
	revision            uint64
	sequence            uint64
	monitor             uint64
	stopAt              time.Time
	stopTimer           *time.Timer
	stopTimerGeneration uint64
}

func newPlaybackManager(controller playerController, runner ShellRunner, now func() time.Time) *playbackManager {
	return &playbackManager{controller: controller, runner: runner, now: now, pollEvery: time.Second, currentIndex: -1, last: playerTransport{State: "unknown"}}
}

func (m *playbackManager) ensureStationsLoaded(ctx context.Context) {
	m.mu.Lock()
	if m.stationsLoaded {
		m.mu.Unlock()
		return
	}
	m.stationsLoaded = true
	m.mu.Unlock()
	reader, ok := m.runner.(interface {
		ReadDeviceFile(context.Context, string) ([]byte, error)
	})
	if !ok {
		return
	}
	content, err := reader.ReadDeviceFile(ctx, radioStationsPath)
	if err != nil {
		return
	}
	var stations []radioEntry
	if json.Unmarshal(content, &stations) != nil || len(stations) > 100 {
		return
	}
	valid := make([]radioEntry, 0, len(stations))
	for _, station := range stations {
		if _, err := validateMediaURL(station.URL); err == nil && station.ID != "" && station.Name != "" {
			valid = append(valid, station)
		}
	}
	m.mu.Lock()
	m.stations = valid
	m.mu.Unlock()
}

func (m *playbackManager) saveStations(ctx context.Context) error {
	store, ok := m.runner.(deviceFileStore)
	if !ok {
		return nil
	}
	m.mu.Lock()
	content, err := json.Marshal(m.stations)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	err = store.WriteDeviceFile(ctx, radioStationsPath, append(content, '\n'), 0600)
	if errors.Is(err, ErrCapabilityNotReady) {
		return nil
	}
	return err
}

func validateMediaURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 2048 || !utf8.ValidString(value) {
		return "", ErrInvalidInput
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", ErrInvalidInput
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return "", ErrInvalidInput
		}
	}
	return parsed.String(), nil
}

func mediaTitle(title, mediaURL string) (string, error) {
	value := strings.TrimSpace(title)
	if utf8.RuneCountInString(value) > 100 || !utf8.ValidString(value) {
		return "", ErrInvalidInput
	}
	if value != "" {
		return value, nil
	}
	parsed, _ := url.Parse(mediaURL)
	value = path.Base(strings.TrimSuffix(parsed.Path, "/"))
	if value == "." || value == "/" || value == "" {
		value = parsed.Hostname()
	}
	if decoded, err := url.PathUnescape(value); err == nil {
		value = decoded
	}
	if utf8.RuneCountInString(value) > 100 {
		value = string([]rune(value)[:100])
	}
	return value, nil
}

func publicSource(mediaURL string) string {
	parsed, err := url.Parse(mediaURL)
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.User = nil
	return parsed.String()
}

func observedMediaItem(status playerTransport, stations []radioEntry) *domain.MediaItem {
	if status.State != "playing" && status.State != "paused" && status.State != "transitioning" {
		return nil
	}
	mediaURL, err := validateMediaURL(status.CurrentURI)
	if err != nil {
		return nil
	}
	source := publicSource(mediaURL)
	for _, station := range stations {
		if publicSource(station.URL) == source {
			return &domain.MediaItem{ID: "observed-" + station.ID, Title: station.Name, Source: source, Kind: "radio"}
		}
	}
	title, err := mediaTitle("", mediaURL)
	if err != nil {
		title = "外部媒体"
	}
	return &domain.MediaItem{ID: "observed-current", Title: title, Source: source, Kind: "url"}
}

func (m *playbackManager) nextID(prefix string) string {
	m.sequence++
	return fmt.Sprintf("%s-%d-%d", prefix, m.now().UnixMilli(), m.sequence)
}

func (m *playbackManager) domainPlayer(observedAt time.Time) domain.Player {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revision++
	state := func(value any) domain.State {
		return domain.State{Value: value, ObservedAt: observedAt, Source: domain.SourceSystem, Freshness: domain.Fresh, Revision: m.revision}
	}
	queue := make([]domain.MediaItem, len(m.queue))
	for index, item := range m.queue {
		queue[index] = domain.MediaItem{ID: item.ID, Title: item.Title, Source: publicSource(item.URL), Kind: item.Kind}
	}
	stations := make([]domain.RadioStation, len(m.stations))
	for index, station := range m.stations {
		stations[index] = domain.RadioStation{ID: station.ID, Name: station.Name, Source: publicSource(station.URL)}
	}
	var current *domain.MediaItem
	if m.currentIndex >= 0 && m.currentIndex < len(queue) {
		item := queue[m.currentIndex]
		current = &item
	} else {
		current = observedMediaItem(m.last, m.stations)
	}
	stopTimer := domain.StopTimer{}
	if !m.stopAt.IsZero() {
		stopAt := m.stopAt
		remaining := int(math.Ceil(stopAt.Sub(m.now()).Seconds()))
		if remaining < 1 {
			remaining = 1
		}
		stopTimer = domain.StopTimer{Active: true, StopAt: &stopAt, RemainingSeconds: remaining}
	}
	volume := state("unknown")
	volume.Freshness = domain.UnknownFreshness
	if m.last.Volume != nil {
		volume = state(*m.last.Volume)
	}
	return domain.Player{
		Transport:       state(m.last.State),
		Volume:          volume,
		PositionSeconds: state(m.last.PositionSeconds),
		DurationSeconds: state(m.last.DurationSeconds),
		Current:         current,
		Queue:           queue,
		CurrentIndex:    m.currentIndex,
		Stations:        stations,
		StopTimer:       stopTimer,
	}
}

func (m *playbackManager) cancelStopTimerLocked() {
	m.stopTimerGeneration++
	if m.stopTimer != nil {
		m.stopTimer.Stop()
	}
	m.stopTimer = nil
	m.stopAt = time.Time{}
}

func (m *playbackManager) cancelStopTimer() {
	m.mu.Lock()
	m.cancelStopTimerLocked()
	m.mu.Unlock()
}

func (m *playbackManager) scheduleStop(duration time.Duration) error {
	if duration <= 0 || duration > time.Hour {
		return ErrInvalidInput
	}
	m.mu.Lock()
	m.cancelStopTimerLocked()
	generation := m.stopTimerGeneration
	m.stopAt = m.now().Add(duration)
	m.stopTimer = time.AfterFunc(duration, func() {
		m.mu.Lock()
		if generation != m.stopTimerGeneration || m.stopAt.IsZero() {
			m.mu.Unlock()
			return
		}
		m.stopTimer = nil
		m.stopAt = time.Time{}
		m.monitor++
		m.radioTransport = ""
		m.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.operationMu.Lock()
		defer m.operationMu.Unlock()
		if err := m.controller.Stop(ctx); err == nil {
			_ = m.waitFor(ctx, "stopped")
		}
	})
	m.mu.Unlock()
	return nil
}

func (m *playbackManager) status(ctx context.Context) (domain.Player, error) {
	m.ensureStationsLoaded(ctx)
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	return m.statusLocked(ctx)
}

// statusLocked keeps a complete KPlayer status read in the same serialized
// operation as control transactions. The embedded player is sensitive to
// overlapping SOAP requests from browser polling, queue monitoring and writes.
func (m *playbackManager) statusLocked(ctx context.Context) (domain.Player, error) {
	status, err := m.controller.Status(ctx)
	if err != nil {
		return domain.Player{}, err
	}
	m.mu.Lock()
	if m.currentIndex >= 0 && m.currentIndex < len(m.queue) && m.queue[m.currentIndex].Kind == "radio" && m.radioTransport != "" {
		if status.State == "playing" || status.State == "paused" {
			m.radioTransport = status.State
		} else if status.State == "stopped" {
			status.State = m.radioTransport
		}
		status.PositionSeconds = 0
		status.DurationSeconds = 0
	}
	m.last = status
	m.mu.Unlock()
	return m.domainPlayer(m.now().Truncate(time.Second)), nil
}

func (m *playbackManager) waitForTimeout(ctx context.Context, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := m.controller.Status(ctx)
		if err == nil {
			m.mu.Lock()
			m.last = status
			m.mu.Unlock()
			if status.State == expected {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("KPlayer state verification timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (m *playbackManager) waitFor(ctx context.Context, expected string) error {
	return m.waitForTimeout(ctx, expected, 4*time.Second)
}

func (m *playbackManager) setAndWaitForVolume(ctx context.Context, expected int) error {
	deadline := time.Now().Add(4 * time.Second)
	nextWrite := time.Time{}
	var lastErr error
	for {
		now := time.Now()
		if nextWrite.IsZero() || !now.Before(nextWrite) {
			if err := m.controller.SetVolume(ctx, expected); err != nil {
				lastErr = err
			}
			nextWrite = now.Add(750 * time.Millisecond)
		}
		volume, err := m.controller.Volume(ctx)
		if err == nil {
			m.mu.Lock()
			m.last.Volume = &volume
			m.mu.Unlock()
			if volume == expected {
				return nil
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("KPlayer volume verification timeout: %w", lastErr)
			}
			return errors.New("KPlayer volume verification timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func (m *playbackManager) playIndex(ctx context.Context, index int) error {
	m.mu.Lock()
	if index < 0 || index >= len(m.queue) {
		m.mu.Unlock()
		return ErrInvalidInput
	}
	entry := m.queue[index]
	m.currentIndex = index
	m.last = playerTransport{State: "transitioning"}
	m.radioTransport = ""
	m.monitor++
	m.mu.Unlock()
	if err := m.controller.SetURI(ctx, entry.URL); err != nil {
		return err
	}
	if err := m.controller.Play(ctx); err != nil {
		return err
	}
	waitTimeout := 4 * time.Second
	if entry.Kind == "radio" {
		waitTimeout = 12 * time.Second
	}
	if err := m.waitForTimeout(ctx, "playing", waitTimeout); err != nil {
		return err
	}
	if entry.Kind == "radio" {
		m.mu.Lock()
		m.radioTransport = "playing"
		m.last.PositionSeconds = 0
		m.last.DurationSeconds = 0
		m.mu.Unlock()
		return nil
	}
	m.startMonitor()
	return nil
}

func (m *playbackManager) startMonitor() {
	m.mu.Lock()
	m.monitor++
	generation := m.monitor
	m.mu.Unlock()
	go func() {
		wasActive := true
		ticker := time.NewTicker(m.pollEvery)
		defer ticker.Stop()
		for range ticker.C {
			m.mu.Lock()
			if generation != m.monitor {
				m.mu.Unlock()
				return
			}
			m.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			m.operationMu.Lock()
			status, err := m.controller.Status(ctx)
			cancel()
			if err != nil {
				m.operationMu.Unlock()
				continue
			}
			m.mu.Lock()
			m.last = status
			if status.State == "playing" || status.State == "paused" || status.State == "transitioning" {
				wasActive = true
				m.mu.Unlock()
				m.operationMu.Unlock()
				continue
			}
			if status.State != "stopped" || !wasActive {
				m.mu.Unlock()
				m.operationMu.Unlock()
				continue
			}
			next := m.currentIndex + 1
			hasNext := next >= 0 && next < len(m.queue)
			m.mu.Unlock()
			if !hasNext {
				m.operationMu.Unlock()
				return
			}
			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
			err = m.playIndex(ctx, next)
			cancel()
			m.operationMu.Unlock()
			if err != nil {
				return
			}
			return
		}
	}()
}

func (m *playbackManager) control(ctx context.Context, command domain.PlayerCommand) (domain.Player, error) {
	m.ensureStationsLoaded(ctx)
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	switch command.Action {
	case "play_url", "queue_add":
		mediaURL, err := validateMediaURL(command.URL)
		if err != nil {
			return domain.Player{}, err
		}
		title, err := mediaTitle(command.Title, mediaURL)
		if err != nil {
			return domain.Player{}, err
		}
		m.mu.Lock()
		entry := playerEntry{ID: m.nextID("media"), Title: title, URL: mediaURL, Kind: "url"}
		if command.Action == "play_url" {
			m.queue = []playerEntry{entry}
			m.currentIndex = 0
		} else {
			m.queue = append(m.queue, entry)
		}
		index := m.currentIndex
		m.mu.Unlock()
		if command.Action == "play_url" {
			if err := m.playIndex(ctx, index); err != nil {
				return domain.Player{}, err
			}
		}
	case "pause":
		if err := m.controller.Pause(ctx); err != nil {
			return domain.Player{}, err
		}
		if err := m.waitFor(ctx, "paused"); err != nil {
			return domain.Player{}, err
		}
		m.mu.Lock()
		if m.currentIndex >= 0 && m.currentIndex < len(m.queue) && m.queue[m.currentIndex].Kind == "radio" {
			m.radioTransport = "paused"
		}
		m.mu.Unlock()
	case "resume":
		if err := m.controller.Play(ctx); err != nil {
			return domain.Player{}, err
		}
		if err := m.waitFor(ctx, "playing"); err != nil {
			return domain.Player{}, err
		}
		m.mu.Lock()
		isRadio := m.currentIndex >= 0 && m.currentIndex < len(m.queue) && m.queue[m.currentIndex].Kind == "radio"
		if isRadio {
			m.radioTransport = "playing"
		}
		m.mu.Unlock()
		if !isRadio {
			m.startMonitor()
		}
	case "volume_set":
		if command.Volume == nil || *command.Volume < 0 || *command.Volume > 100 {
			return domain.Player{}, ErrInvalidInput
		}
		current, err := m.statusLocked(ctx)
		if err != nil {
			return domain.Player{}, err
		}
		if current.Transport.Value != "playing" {
			return domain.Player{}, ErrPlaybackInactive
		}
		if err := m.setAndWaitForVolume(ctx, *command.Volume); err != nil {
			return domain.Player{}, err
		}
		// GetVolume 已独立验证写入结果。直接返回缓存的播放状态，避免
		// KPlayer 在暂停/恢复切换后短暂拒绝 GetTransportInfo 导致误报失败。
		return m.domainPlayer(m.now().Truncate(time.Second)), nil
	case "stop":
		m.mu.Lock()
		m.cancelStopTimerLocked()
		m.monitor++
		m.radioTransport = ""
		m.mu.Unlock()
		if err := m.controller.Stop(ctx); err != nil {
			return domain.Player{}, err
		}
		if err := m.waitFor(ctx, "stopped"); err != nil {
			return domain.Player{}, err
		}
	case "next":
		m.mu.Lock()
		next := m.currentIndex + 1
		m.mu.Unlock()
		if err := m.playIndex(ctx, next); err != nil {
			return domain.Player{}, err
		}
	case "queue_play":
		m.mu.Lock()
		index := -1
		for candidate, item := range m.queue {
			if item.ID == command.ItemID {
				index = candidate
				break
			}
		}
		m.mu.Unlock()
		if index < 0 {
			return domain.Player{}, ErrNotFound
		}
		if err := m.playIndex(ctx, index); err != nil {
			return domain.Player{}, err
		}
	case "queue_remove":
		m.mu.Lock()
		index := -1
		for candidate, item := range m.queue {
			if item.ID == command.ItemID {
				index = candidate
				break
			}
		}
		if index < 0 {
			m.mu.Unlock()
			return domain.Player{}, ErrNotFound
		}
		if index == m.currentIndex && (m.last.State == "playing" || m.last.State == "paused" || m.last.State == "transitioning") {
			m.mu.Unlock()
			return domain.Player{}, ErrConflict
		}
		m.queue = append(m.queue[:index], m.queue[index+1:]...)
		if index < m.currentIndex {
			m.currentIndex--
		} else if m.currentIndex >= len(m.queue) {
			m.currentIndex = -1
		}
		m.mu.Unlock()
	case "queue_clear":
		m.mu.Lock()
		m.cancelStopTimerLocked()
		m.monitor++
		m.radioTransport = ""
		m.mu.Unlock()
		if err := m.controller.Stop(ctx); err != nil {
			return domain.Player{}, err
		}
		m.mu.Lock()
		m.queue = nil
		m.currentIndex = -1
		m.last = playerTransport{State: "stopped"}
		m.mu.Unlock()
	case "timer_set":
		if err := m.scheduleStop(time.Duration(command.DurationMinutes) * time.Minute); err != nil {
			return domain.Player{}, err
		}
	case "timer_cancel":
		m.cancelStopTimer()
	case "radio_add":
		mediaURL, err := validateMediaURL(command.URL)
		if err != nil {
			return domain.Player{}, err
		}
		name, err := mediaTitle(command.Title, mediaURL)
		if err != nil {
			return domain.Player{}, err
		}
		m.mu.Lock()
		m.stations = append(m.stations, radioEntry{ID: m.nextID("radio"), Name: name, URL: mediaURL})
		m.mu.Unlock()
		if err := m.saveStations(ctx); err != nil {
			return domain.Player{}, err
		}
	case "radio_remove":
		m.mu.Lock()
		index := -1
		for candidate, station := range m.stations {
			if station.ID == command.ItemID {
				index = candidate
				break
			}
		}
		if index < 0 {
			m.mu.Unlock()
			return domain.Player{}, ErrNotFound
		}
		m.stations = append(m.stations[:index], m.stations[index+1:]...)
		m.mu.Unlock()
		if err := m.saveStations(ctx); err != nil {
			return domain.Player{}, err
		}
	case "radio_move_up", "radio_move_down":
		m.mu.Lock()
		index := -1
		for candidate, station := range m.stations {
			if station.ID == command.ItemID {
				index = candidate
				break
			}
		}
		if index < 0 {
			m.mu.Unlock()
			return domain.Player{}, ErrNotFound
		}
		target := index - 1
		if command.Action == "radio_move_down" {
			target = index + 1
		}
		if target < 0 || target >= len(m.stations) {
			m.mu.Unlock()
			break
		}
		previous := append([]radioEntry(nil), m.stations...)
		m.stations[index], m.stations[target] = m.stations[target], m.stations[index]
		m.mu.Unlock()
		if err := m.saveStations(ctx); err != nil {
			m.mu.Lock()
			m.stations = previous
			m.mu.Unlock()
			return domain.Player{}, err
		}
	case "radio_play", "radio_queue":
		m.mu.Lock()
		var station *radioEntry
		for index := range m.stations {
			if m.stations[index].ID == command.ItemID {
				copy := m.stations[index]
				station = &copy
				break
			}
		}
		if station == nil {
			m.mu.Unlock()
			return domain.Player{}, ErrNotFound
		}
		entry := playerEntry{ID: m.nextID("media"), Title: station.Name, URL: station.URL, Kind: "radio"}
		if command.Action == "radio_play" {
			m.queue = []playerEntry{entry}
			m.currentIndex = 0
		} else {
			m.queue = append(m.queue, entry)
		}
		index := m.currentIndex
		m.mu.Unlock()
		if command.Action == "radio_play" {
			if err := m.playIndex(ctx, index); err != nil {
				return domain.Player{}, err
			}
		}
	default:
		return domain.Player{}, ErrInvalidInput
	}
	return m.statusLocked(ctx)
}
