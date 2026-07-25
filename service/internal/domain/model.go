package domain

import "time"

type Readability string

const (
	ReadFull    Readability = "full"
	ReadPartial Readability = "partial"
	ReadNone    Readability = "none"
)

type Writability string

const (
	WriteSafe         Writability = "safe"
	WriteExperimental Writability = "experimental"
	WriteNotVerified  Writability = "not_verified"
	WriteUnsupported  Writability = "unsupported"
)

type Availability string

const (
	Available Availability = "available"
	Degraded  Availability = "degraded"
	Offline   Availability = "offline"
	Unknown   Availability = "unknown"
)

type Source string

const (
	SourceSystem         Source = "system"
	SourceDBusEvent      Source = "dbus_event"
	SourceMock           Source = "mock"
	SourceVendorSnapshot Source = "vendor_snapshot"
	SourceDerived        Source = "derived"
)

type Freshness string

const (
	Fresh            Freshness = "fresh"
	Stale            Freshness = "stale"
	UnknownFreshness Freshness = "unknown"
)

type Capability struct {
	ID              string       `json:"id"`
	Readability     Readability  `json:"readability"`
	Writability     Writability  `json:"writability"`
	Availability    Availability `json:"availability"`
	CloudDependency bool         `json:"cloudDependency"`
	Reason          string       `json:"reason,omitempty"`
}

// State is the normalized envelope used for every observed value.
// Value intentionally remains JSON-shaped so a future RealAdapter can preserve
// this contract without exposing vendor protocol payloads.
type State struct {
	Value      any       `json:"value"`
	ObservedAt time.Time `json:"observedAt"`
	Source     Source    `json:"source"`
	Freshness  Freshness `json:"freshness"`
	Revision   uint64    `json:"revision"`
}

type Envelope[T any] struct {
	Environment string `json:"environment"`
	Scenario    string `json:"scenario"`
	Data        T      `json:"data"`
}

type Device struct {
	ProductFamily       State `json:"productFamily"`
	Firmware            State `json:"firmware"`
	Platform            State `json:"platform"`
	StorageRemainingMiB State `json:"storageRemainingMiB"`
}

type Status struct {
	Overall  State            `json:"overall"`
	Services map[string]State `json:"services"`
	Player   State            `json:"player"`
	Network  State            `json:"network"`
	Audio    State            `json:"audio"`
}

type AirPlay struct {
	Runtime            State `json:"runtime"`
	Port               State `json:"port"`
	RestoreService     State `json:"restoreService"`
	AutoRecoverEnabled State `json:"autoRecoverEnabled"`
}

type Network struct {
	Connection       State `json:"connection"`
	Signal           State `json:"signal"`
	CurrentSSID      State `json:"currentSSID"`
	LastSwitchResult State `json:"lastSwitchResult"`
}

type EQ struct {
	SelectedMode State `json:"selectedMode"`
	AppliedMode  State `json:"appliedMode"`
	ApplyState   State `json:"applyState"`
}

type Audio struct {
	SystemVolume State `json:"systemVolume"`
	OutputMuted  State `json:"outputMuted"`
	Microphone   State `json:"microphone"`
	EQ           EQ    `json:"eq"`
}

type Bluetooth struct {
	Service              State `json:"service"`
	Enabled              State `json:"enabled"`
	LastConfirmedEnabled State `json:"lastConfirmedEnabled"`
}

type Lighting struct {
	IconEnabled State `json:"iconEnabled"`
	Brightness  State `json:"brightness"`
	PlayMode    State `json:"playMode"`
}

type Schedules struct {
	MicrophoneSchedule State `json:"microphoneSchedule"`
	Alarms             State `json:"alarms"`
	Reminders          State `json:"reminders"`
}

type MediaItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Source string `json:"source"`
	Kind   string `json:"kind"`
}

type RadioStation struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

type StopTimer struct {
	Active           bool       `json:"active"`
	StopAt           *time.Time `json:"stopAt"`
	RemainingSeconds int        `json:"remainingSeconds"`
}

type Player struct {
	Transport       State          `json:"transport"`
	Volume          State          `json:"volume"`
	PositionSeconds State          `json:"positionSeconds"`
	DurationSeconds State          `json:"durationSeconds"`
	Current         *MediaItem     `json:"current"`
	Queue           []MediaItem    `json:"queue"`
	CurrentIndex    int            `json:"currentIndex"`
	Stations        []RadioStation `json:"stations"`
	StopTimer       StopTimer      `json:"stopTimer"`
}

type PlayerCommand struct {
	Action          string
	ItemID          string
	Title           string
	URL             string
	DurationMinutes int
	Volume          *int
}

type Event struct {
	Type       string    `json:"type"`
	Scenario   string    `json:"scenario"`
	ObservedAt time.Time `json:"observedAt"`
	Source     Source    `json:"source"`
	Revision   uint64    `json:"revision"`
	Changes    []string  `json:"changes"`
}

type OperationStep struct {
	State string `json:"state"`
	Label string `json:"label"`
}

type Operation struct {
	OperationID       string          `json:"operationId"`
	Simulation        bool            `json:"simulation"`
	Applied           bool            `json:"applied"`
	Verified          bool            `json:"verified"`
	RollbackAttempted bool            `json:"rollbackAttempted"`
	Outcome           string          `json:"outcome"`
	Message           string          `json:"message"`
	Timeline          []OperationStep `json:"timeline"`
}

type APIError struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code        string  `json:"code"`
	Message     string  `json:"message"`
	OperationID *string `json:"operationId"`
}
