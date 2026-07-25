package adapter

import (
	"context"
	"errors"

	"sanyin.local/config/service/internal/domain"
)

var ErrUnknownScenario = errors.New("unknown mock scenario")
var ErrCapabilityNotReady = errors.New("capability is not ready")
var ErrInvalidInput = errors.New("invalid input")
var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")
var ErrPlaybackInactive = errors.New("local playback is not active")

// DeviceAdapter is the stable boundary for the future device-backed adapter.
// No HTTP handler may reach D-Bus, vendor storage, or system commands directly.
type DeviceAdapter interface {
	Capabilities(context.Context) ([]domain.Capability, error)
	Device(context.Context) (domain.Device, error)
	Status(context.Context) (domain.Status, error)
	AirPlay(context.Context) (domain.AirPlay, error)
	Network(context.Context) (domain.Network, error)
	Audio(context.Context) (domain.Audio, error)
	Bluetooth(context.Context) (domain.Bluetooth, error)
	Lighting(context.Context) (domain.Lighting, error)
	Schedules(context.Context) (domain.Schedules, error)
	Player(context.Context) (domain.Player, error)
	ControlPlayer(context.Context, domain.PlayerCommand) (domain.Player, error)
	Event(context.Context) (domain.Event, error)
	SimulateOperation(context.Context, string) (domain.Operation, error)
	RecoverAirPlay(context.Context) (domain.Operation, error)
	SetAirPlayAutoRecover(context.Context, bool) (domain.Operation, error)
	SetBluetooth(context.Context, bool) (domain.Operation, error)
	SetEQ(context.Context, int) (domain.Operation, error)
	SetWiFi(context.Context, string, string) (domain.Operation, error)
}

type Scenario struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Provider selects an isolated adapter for each request. RealAdapter can later
// ignore the scenario argument while keeping all route contracts unchanged.
type Provider interface {
	Environment() string
	DefaultScenario() string
	ForScenario(string) (DeviceAdapter, error)
	Scenarios() []Scenario
}
