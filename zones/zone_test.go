package zones

import (
	"testing"
	"time"

	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/mqtt"
	"github.com/rob/bedwetter/store"
)

type fakeMQTTClient struct {
	published []string
}

func (f *fakeMQTTClient) Publish(topic string, qos byte, retained bool, payload string) error {
	f.published = append(f.published, topic+":"+payload)
	return nil
}

func (f *fakeMQTTClient) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) error {
	return nil
}

func (f *fakeMQTTClient) SubscribeMultiple(topics map[string]byte, handler mqtt.MessageHandler) error {
	return nil
}

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

func newTestManager(t *testing.T, zones []config.ZoneConfig) *Manager {
	cfg := &config.Config{Zones: zones}
	mq := &fakeMQTTClient{}
	st := newTestStore(t)
	// Use the interface-compatible store - Manager takes *store.Store which embeds these methods
	m := NewManager(cfg, mq, st, nil, nil)
	return m
}

func TestNewManager(t *testing.T) {
	zones := []config.ZoneConfig{
		{Name: "Zone A", MoistureSensorTopic: "a/b", ThresholdLow: 30, ThresholdHigh: 60},
		{Name: "Zone B", MoistureSensorTopic: "c/d", ThresholdLow: 20, ThresholdHigh: 50},
	}

	m := newTestManager(t, zones)

	all := m.GetAllZones()
	if len(all) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(all))
	}
	// Should be sorted by name
	if all[0].Config.Name != "Zone A" || all[1].Config.Name != "Zone B" {
		t.Errorf("order: %v", []string{all[0].Config.Name, all[1].Config.Name})
	}

	z := m.GetZone("Zone A")
	if z == nil {
		t.Fatal("Zone A not found")
	}
	if z.State != StateIdle {
		t.Errorf("expected StateIdle, got %v", z.State)
	}
	if z.Config.ThresholdLow != 30 {
		t.Errorf("ThresholdLow: got %d", z.Config.ThresholdLow)
	}
}

func TestGetZoneNotFound(t *testing.T) {
	m := newTestManager(t, nil)
	z := m.GetZone("NONEXISTENT")
	if z != nil {
		t.Errorf("expected nil for nonexistent zone")
	}
}

func TestAddZone(t *testing.T) {
	m := newTestManager(t, nil)
	m.AddZone(config.ZoneConfig{Name: "New Zone", ThresholdLow: 25})

	z := m.GetZone("New Zone")
	if z == nil {
		t.Fatal("expected zone to exist after AddZone")
	}
	if z.State != StateIdle {
		t.Errorf("expected StateIdle, got %v", z.State)
	}
	if z.Config.ThresholdLow != 25 {
		t.Errorf("ThresholdLow: got %d", z.Config.ThresholdLow)
	}

	all := m.GetAllZones()
	if len(all) != 1 {
		t.Errorf("expected 1 zone, got %d", len(all))
	}
}

func TestAddZoneDuplicate(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{{Name: "Z1"}})
	m.AddZone(config.ZoneConfig{Name: "Z1"})

	all := m.GetAllZones()
	if len(all) != 1 {
		t.Errorf("expected 1 zone (duplicate ignored), got %d", len(all))
	}
}

func TestRemoveZone(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{{Name: "Z1", ValveCommandTopic: "v/z1"}})
	fake := m.client.(*fakeMQTTClient)
	m.RemoveZone("Z1")

	z := m.GetZone("Z1")
	if z != nil {
		t.Errorf("expected nil after RemoveZone")
	}
	all := m.GetAllZones()
	if len(all) != 0 {
		t.Errorf("expected 0 zones, got %d", len(all))
	}

	if !containsPublish(fake.published, "v/z1:OFF") {
		t.Errorf("expected valve close on remove, got %v", fake.published)
	}
}

func TestRemoveZoneNonexistent(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{{Name: "Z1"}})
	m.RemoveZone("NONEXISTENT")

	all := m.GetAllZones()
	if len(all) != 1 {
		t.Errorf("expected 1 zone still, got %d", len(all))
	}
}

func TestUpdateZoneConfig(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{{Name: "Z1", ThresholdLow: 10}})
	m.UpdateZoneConfig("Z1", config.ZoneConfig{Name: "Z1", ThresholdLow: 50})

	z := m.GetZone("Z1")
	if z == nil {
		t.Fatal("zone not found")
	}
	if z.Config.ThresholdLow != 50 {
		t.Errorf("ThresholdLow: got %d, want 50", z.Config.ThresholdLow)
	}
}

func TestUpdateZoneConfigNonexistent(t *testing.T) {
	m := newTestManager(t, nil)
	m.UpdateZoneConfig("NONEXISTENT", config.ZoneConfig{Name: "NONEXISTENT"})
	// Should not panic
}

func TestOpenCloseValveMQTT(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ValveCommandTopic: "valve/z1"},
	})
	fake := m.client.(*fakeMQTTClient)

	m.OpenValve("Z1")
	if !containsPublish(fake.published, "valve/z1:ON") {
		t.Errorf("OpenValve publish missing valve/z1:ON: got %v", fake.published)
	}

	z := m.GetZone("Z1")
	if z.State != StateManualOpen {
		t.Errorf("expected StateManualOpen, got %v", z.State)
	}

	m.CloseValve("Z1")
	if !containsPublish(fake.published, "valve/z1:OFF") {
		t.Errorf("CloseValve publish missing valve/z1:OFF: got %v", fake.published)
	}

	if z.State != StateIdle {
		t.Errorf("expected StateIdle after close, got %v", z.State)
	}
}

func containsPublish(pub []string, needle string) bool {
	for _, p := range pub {
		if p == needle {
			return true
		}
	}
	return false
}

func TestOpenCloseValveNonexistent(t *testing.T) {
	m := newTestManager(t, nil)
	m.OpenValve("NONEXISTENT")
	m.CloseValve("NONEXISTENT")
	// Should not panic
}

func TestHandleSensorReading(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{{Name: "Z1"}})

	m.handleSensorReading("Z1", []byte("45.5"))

	z := m.GetZone("Z1")
	if z == nil {
		t.Fatal("zone not found")
	}
	if z.Moisture != 45.5 {
		t.Errorf("Moisture: got %f, want 45.5", z.Moisture)
	}
}

func TestHandleSensorReadingInvalid(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{{Name: "Z1"}})

	m.handleSensorReading("Z1", []byte("not-a-number"))
	z := m.GetZone("Z1")
	if z.Moisture != 0 {
		t.Errorf("expected moisture 0 for invalid input, got %f", z.Moisture)
	}
}

func TestHandleSensorReadingBounds(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{{Name: "Z1"}})

	tests := []struct {
		input string
		want  float64
	}{
		{"-10", 0},
		{"150", 100},
		{"NaN", 0},
		{"0", 0},
		{"100", 100},
	}

	for _, tt := range tests {
		m.handleSensorReading("Z1", []byte(tt.input))
		z := m.GetZone("Z1")
		if z.Moisture != tt.want {
			t.Errorf("input %q: got moisture %f, want %f", tt.input, z.Moisture, tt.want)
		}
	}
}

func TestHandleSensorReadingNonexistent(t *testing.T) {
	m := newTestManager(t, nil)
	m.handleSensorReading("NONEXISTENT", []byte("50"))
	// Should not panic
}

func TestHandleValveStateOn(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{{Name: "Z1"}})

	m.handleValveState("Z1", []byte("ON"))
	z := m.GetZone("Z1")
	if z.State != StateManualOpen {
		t.Errorf("expected StateManualOpen, got %v", z.State)
	}

	m.handleValveState("Z1", []byte("OFF"))
	if z.State != StateIdle {
		t.Errorf("expected StateIdle after OFF, got %v", z.State)
	}
}

func TestHandleValveStateVariants(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{{Name: "Z1"}})

	for _, val := range []string{"on", "open", "true", "1"} {
		m.handleValveState("Z1", []byte(val))
		z := m.GetZone("Z1")
		if z.State != StateManualOpen {
			t.Errorf("value %q: expected StateManualOpen, got %v", val, z.State)
		}
		m.handleValveState("Z1", []byte("OFF"))
	}
}

func TestHandleValveStateNonexistent(t *testing.T) {
	m := newTestManager(t, nil)
	m.handleValveState("NONEXISTENT", []byte("ON"))
	// Should not panic
}

func TestEvaluateZoneWatering(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, ThresholdHigh: 70, MaxWateringSeconds: 300, MaxActivationsPerDay: 5, CooldownMinutes: 60, ValveCommandTopic: "valve/z1"},
	})
	fake := m.client.(*fakeMQTTClient)

	// Set moisture below threshold
	m.handleSensorReading("Z1", []byte("30"))

	// evaluateZone launches OpenValve in a goroutine; yield for it
	time.Sleep(time.Millisecond)

	// evaluateZone is called by handleSensorReading internally
	z := m.GetZone("Z1")
	if z.State != StateWatering {
		t.Errorf("expected StateWatering, got %v", z.State)
	}

	if !containsPublish(fake.published, "valve/z1:ON") {
		t.Errorf("expected valve/z1:ON, got %v", fake.published)
	}
}

func TestEvaluateZoneAboveThreshold(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50},
	})

	// Moisture above threshold - should stay idle
	m.handleSensorReading("Z1", []byte("70"))
	z := m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle (moisture above threshold), got %v", z.State)
	}
}

func TestEvaluateZoneManualOpen(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300},
	})

	m.handleValveState("Z1", []byte("ON"))
	m.handleSensorReading("Z1", []byte("30"))

	z := m.GetZone("Z1")
	if z.State != StateManualOpen {
		t.Errorf("expected StateManualOpen (manual overrides auto-watering), got %v", z.State)
	}
}

func TestEvaluateZoneFailsafe(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300},
	})

	m.handleValveState("Z1", []byte("ON"))
	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateFailsafe
	z.mu.Unlock()

	m.handleSensorReading("Z1", []byte("30"))
	// Should stay in failsafe
	if z.State != StateFailsafe {
		t.Errorf("expected StateFailsafe, got %v", z.State)
	}
}

func TestWatchdogNoData(t *testing.T) {
	cfg := &config.Config{
		Zones: []config.ZoneConfig{{Name: "Z1"}},
		Alerts: config.AlertsConfig{StaleSensorMinutes: 60},
	}
	mq := &fakeMQTTClient{}
	st := newTestStore(t)
	m := NewManager(cfg, mq, st, nil, nil)

	m.Watchdog()
	z := m.GetZone("Z1")
	if z.State != StateFailsafe {
		t.Errorf("expected StateFailsafe (no data), got %v", z.State)
	}
}

func TestWatchdogWithRecentData(t *testing.T) {
	cfg := &config.Config{
		Zones: []config.ZoneConfig{{Name: "Z1"}},
		Alerts: config.AlertsConfig{StaleSensorMinutes: 60},
	}
	mq := &fakeMQTTClient{}
	st := newTestStore(t)
	m := NewManager(cfg, mq, st, nil, nil)

	m.handleSensorReading("Z1", []byte("50"))
	m.Watchdog()

	z := m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle (recent data), got %v", z.State)
	}
}

func TestSplitEntityID(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"sensor.test", []string{"sensor", "test"}},
		{"switch.garden_valve", []string{"switch", "garden_valve"}},
		{"invalid", nil},
		{"too.many.parts", []string{"too", "many.parts"}},
		{"", nil},
	}

	for _, tt := range tests {
		got := splitEntityID(tt.input)
		if tt.want == nil {
			if got != nil {
				t.Errorf("splitEntityID(%q): expected nil, got %v", tt.input, got)
			}
		} else {
			if len(got) != 2 || got[0] != tt.want[0] || got[1] != tt.want[1] {
				t.Errorf("splitEntityID(%q): got %v, want %v", tt.input, got, tt.want)
			}
		}
	}
}

func TestStopClosesValves(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ValveCommandTopic: "v/z1"},
		{Name: "Z2", ValveCommandTopic: "v/z2"},
	})
	fake := m.client.(*fakeMQTTClient)

	m.Stop()
	// wait a tiny bit for goroutines
	time.Sleep(10 * time.Millisecond)

	// Should have published OFF for each zone
	offCount := 0
	for _, p := range fake.published {
		if p == "v/z1:OFF" || p == "v/z2:OFF" {
			offCount++
		}
	}
	if offCount != 2 {
		t.Errorf("expected 2 OFF publishes, got %d: %v", offCount, fake.published)
	}
}

func TestGetAllZonesEmpty(t *testing.T) {
	m := newTestManager(t, nil)
	all := m.GetAllZones()
	if len(all) != 0 {
		t.Errorf("expected empty, got %d", len(all))
	}
}

func TestEvaluateZoneCooldown(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, CooldownMinutes: 60},
	})
	m.handleSensorReading("Z1", []byte("30"))

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.LastWaterEnd = time.Now().Add(-2 * time.Hour)
	z.State = StateCooldown
	z.mu.Unlock()

	// Set moisture above threshold to prevent re-watering
	m.handleSensorReading("Z1", []byte("60"))
	if z.State != StateIdle {
		t.Errorf("expected StateIdle after cooldown expired, got %v", z.State)
	}
}
