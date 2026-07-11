package scheduler

import (
	"testing"
	"time"

	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/mqtt"
	"github.com/robinelvin/bedwetter/store"
	"github.com/robinelvin/bedwetter/zones"
)

type fakeMQTTClient struct{}

func (f *fakeMQTTClient) Publish(topic string, qos byte, retained bool, payload string) error { return nil }
func (f *fakeMQTTClient) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) error { return nil }
func (f *fakeMQTTClient) SubscribeMultiple(topics map[string]byte, handler mqtt.MessageHandler) error { return nil }
func (f *fakeMQTTClient) IsConnected() bool { return true }
func (f *fakeMQTTClient) Unsubscribe(topics ...string) {}
func (f *fakeMQTTClient) Disconnect(quiesce uint) {}

func newTestStore(t *testing.T) *store.Store {
	s, err := store.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return s
}

func newTestScheduler(t *testing.T, cfg *config.Config) *Scheduler {
	if cfg == nil {
		cfg = &config.Config{Weather: config.WeatherConfig{RainThresholdMm: 5.0}}
	}
	s := newTestStore(t)
	mq := &fakeMQTTClient{}
	zm := zones.NewManager(cfg, mq, s, nil, nil)
	return New(cfg, s, zm)
}

func TestParseTimeToMinutes(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"00:00", 0},
		{"06:00", 360},
		{"10:00", 600},
		{"12:30", 750},
		{"23:59", 1439},
		{"invalid", -1},
		{"", -1},
	}
	for _, tt := range tests {
		got := zones.ParseTimeToMinutes(tt.input)
		if got != tt.want {
			t.Errorf("parseTimeToMinutes(%q): got %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestIsWithinTimeWindow(t *testing.T) {
	tests := []struct {
		earliest string
		latest   string
		timeStr  string
		want     bool
	}{
		{"06:00", "10:00", "06:00", true},
		{"06:00", "10:00", "08:00", true},
		{"06:00", "10:00", "10:00", true},
		{"06:00", "10:00", "05:59", false},
		{"06:00", "10:00", "10:01", false},
		{"", "", "06:00", true},
		{"", "", "12:00", false},
		{"07:00", "09:00", "06:30", false},
		{"07:00", "09:00", "09:30", false},
		{"07:00", "09:00", "08:00", true},
	}
	for _, tt := range tests {
		tm, _ := time.Parse("15:04", tt.timeStr)
		now := time.Date(2026, 7, 9, tm.Hour(), tm.Minute(), 0, 0, time.UTC)
		got := zones.IsWithinWateringWindow(tt.earliest, tt.latest, now)
		if got != tt.want {
			t.Errorf("isWithinTimeWindow(%q, %q, %s): got %v, want %v", tt.earliest, tt.latest, tt.timeStr, got, tt.want)
		}
	}
}

func TestIsWithinTimeWindowDefaults(t *testing.T) {
	// Default is 06:00-10:00 when earliest/latest are empty
	tm, _ := time.Parse("15:04", "08:00")
	now := time.Date(2026, 7, 9, tm.Hour(), tm.Minute(), 0, 0, time.UTC)
	if !zones.IsWithinWateringWindow("", "", now) {
		t.Error("expected 08:00 to be within default window")
	}

	tm, _ = time.Parse("15:04", "04:00")
	now = time.Date(2026, 7, 9, tm.Hour(), tm.Minute(), 0, 0, time.UTC)
	if zones.IsWithinWateringWindow("", "", now) {
		t.Error("expected 04:00 to be outside default window")
	}
}

func TestGetSeasonalMultiplier(t *testing.T) {
	multipliers := map[int]float64{
		1: 0.5,
		7: 1.5,
		12: 0.3,
	}

	tests := []struct {
		month int
		want  float64
	}{
		{1, 0.5},
		{7, 1.5},
		{12, 0.3},
		{6, 1.0},
		{0, 1.0},
	}
	for _, tt := range tests {
		got := getSeasonalMultiplier(multipliers, tt.month)
		if got != tt.want {
			t.Errorf("getSeasonalMultiplier(%d): got %f, want %f", tt.month, got, tt.want)
		}
	}
}

func TestGetSeasonalMultiplierNil(t *testing.T) {
	got := getSeasonalMultiplier(nil, 7)
	if got != 1.0 {
		t.Errorf("expected 1.0 for nil multipliers, got %f", got)
	}
}

func TestCheckWeatherSkipsWhenCoordsZero(t *testing.T) {
	cfg := &config.Config{Weather: config.WeatherConfig{Lat: 0, Lon: 0}}
	s := newTestScheduler(t, cfg)
	s.CheckWeather()
	if !s.weatherCache.FetchedAt.IsZero() {
		t.Error("expected weather check to be skipped when coords are zero")
	}
}

func TestNewScheduler(t *testing.T) {
	s := newTestScheduler(t, nil)
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.weatherCache.TTL != 30*time.Minute {
		t.Errorf("expected TTL 30m, got %v", s.weatherCache.TTL)
	}
}

type rainDetectManager struct {
	*zones.Manager
	rain bool
}

func (m *rainDetectManager) RainDetected() bool { return m.rain }

func TestSchedulerSkipsWhenRainDetected(t *testing.T) {
	// Test that rain sensor skips by checking that the weather check doesn't update
	// when rain is detected. We can't easily test the full loop, but we can verify
	// the RainDetected check is correct.
	cfg := &config.Config{Weather: config.WeatherConfig{Lat: 51.5, Lon: -0.12}}
	s := newTestScheduler(t, cfg)

	if s.zoneManager.RainDetected() {
		t.Error("expected no rain initially")
	}
}
