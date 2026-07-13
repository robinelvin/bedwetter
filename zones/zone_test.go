package zones

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/mqtt"
	"github.com/robinelvin/bedwetter/store"
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
	if !math.IsNaN(z.Moisture) {
		t.Errorf("expected moisture NaN for invalid input, got %f", z.Moisture)
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
		{Name: "Z1", ThresholdLow: 50, ThresholdHigh: 70, MaxWateringSeconds: 300, MaxActivationsPerDay: 5, CooldownMinutes: 60, ValveCommandTopic: "valve/z1", EarliestWateringTime: "00:00", LatestWateringTime: "23:59"},
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

func TestManualOpenClosesWhenMoistureReachesTarget(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 30, ThresholdHigh: 60, MaxWateringSeconds: 300, ValveCommandTopic: "valve/z1"},
	})
	fake := m.client.(*fakeMQTTClient)

	// Manually open the valve
	m.handleValveState("Z1", []byte("ON"))
	z := m.GetZone("Z1")
	if z.State != StateManualOpen {
		t.Fatalf("expected StateManualOpen, got %v", z.State)
	}

	// Simulate sensor reading reaching target moisture (>= ThresholdHigh)
	m.handleSensorReading("Z1", []byte("60"))
	time.Sleep(time.Millisecond)

	z = m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle after moisture reached target, got %v", z.State)
	}
	if !containsPublish(fake.published, "valve/z1:OFF") {
		t.Errorf("expected valve/z1:OFF, got %v", fake.published)
	}
}

func TestManualOpenStaysOpenWhenMoistureBelowTarget(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 30, ThresholdHigh: 60, MaxWateringSeconds: 300},
	})

	// Manually open the valve
	m.handleValveState("Z1", []byte("ON"))
	z := m.GetZone("Z1")
	if z.State != StateManualOpen {
		t.Fatalf("expected StateManualOpen, got %v", z.State)
	}

	// Simulate sensor reading still below target moisture (< ThresholdHigh)
	m.handleSensorReading("Z1", []byte("45"))

	z = m.GetZone("Z1")
	if z.State != StateManualOpen {
		t.Errorf("expected StateManualOpen (moisture below target), got %v", z.State)
	}
}

func TestManualOpenNoShutoffWhenThresholdHighZero(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 30, ThresholdHigh: 0, MaxWateringSeconds: 300},
	})

	// Manually open the valve
	m.handleValveState("Z1", []byte("ON"))

	// Simulate sensor reading — ThresholdHigh is 0, so no moisture shutoff
	m.handleSensorReading("Z1", []byte("80"))

	z := m.GetZone("Z1")
	if z.State != StateManualOpen {
		t.Errorf("expected StateManualOpen (ThresholdHigh=0 disables moisture shutoff), got %v", z.State)
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

func TestRainSensorNotDetectedByDefault(t *testing.T) {
	m := newTestManager(t, nil)
	if m.RainDetected() {
		t.Error("expected rain not detected by default")
	}
}

func TestRainSensorSubscribeWithTopic(t *testing.T) {
	cfg := &config.Config{
		Weather: config.WeatherConfig{RainSensorTopic: "bedwetter/rain"},
		Alerts:  config.AlertsConfig{StaleSensorMinutes: 60},
	}
	mq := &fakeMQTTClient{}
	st := newTestStore(t)
	m := NewManager(cfg, mq, st, nil, nil)

	m.subscribeRainSensor()
	// Should not panic, subscription attempted
}

func TestRainSensorSubscribeEmptyTopic(t *testing.T) {
	cfg := &config.Config{
		Alerts: config.AlertsConfig{StaleSensorMinutes: 60},
	}
	mq := &fakeMQTTClient{}
	st := newTestStore(t)
	m := NewManager(cfg, mq, st, nil, nil)

	m.subscribeRainSensor()
	// Should not panic, no subscription attempted
}

func TestManagerHasRainSensorField(t *testing.T) {
	m := newTestManager(t, nil)
	m.rainMu.Lock()
	m.rainDetected = true
	m.rainMu.Unlock()

	if !m.RainDetected() {
		t.Error("expected rain detected after setting")
	}
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
		got := ParseTimeToMinutes(tt.input)
		if got != tt.want {
			t.Errorf("ParseTimeToMinutes(%q): got %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestIsWithinWateringWindow(t *testing.T) {
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
		got := IsWithinWateringWindow(tt.earliest, tt.latest, now)
		if got != tt.want {
			t.Errorf("IsWithinWateringWindow(%q, %q, %s): got %v, want %v", tt.earliest, tt.latest, tt.timeStr, got, tt.want)
		}
	}
}

func TestEvaluateZoneRespectsTimeWindow(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: "06:00", LatestWateringTime: "10:00"},
	})
	fake := m.client.(*fakeMQTTClient)

	// Simulate a sensor reading arriving at 14:00 (outside window)
	z := m.GetZone("Z1")
	z.mu.Lock()
	z.LastMoistureTime = time.Now()
	z.mu.Unlock()

	// Directly call evaluateZone - it uses time.Now() internally for the window check
	// We can't easily mock time.Now(), so test with a zone whose window includes now
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m2 := newTestManager(t, []config.ZoneConfig{
		{Name: "Z2", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})
	_ = fake

	m2.handleSensorReading("Z2", []byte("30"))
	z2 := m2.GetZone("Z2")
	if z2.State != StateWatering {
		t.Errorf("expected StateWatering (within window), got %v", z2.State)
	}

	// Zone outside window should not water
	m3 := newTestManager(t, []config.ZoneConfig{
		{Name: "Z3", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: "02:00", LatestWateringTime: "03:00"},
	})
	m3.handleSensorReading("Z3", []byte("30"))
	z3 := m3.GetZone("Z3")
	if z3.State != StateIdle {
		t.Errorf("expected StateIdle (outside window), got %v", z3.State)
	}
}

func TestCloseValveWateringToCooldown(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ValveCommandTopic: "v/z1", ThresholdLow: 50, MaxWateringSeconds: 300, CooldownMinutes: 10},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateWatering
	z.mu.Unlock()

	m.CloseValve("Z1")

	z = m.GetZone("Z1")
	if z.State != StateCooldown {
		t.Errorf("expected StateCooldown after closing watering zone, got %v", z.State)
	}
}

func TestCloseValveManualOpenToIdle(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ValveCommandTopic: "v/z1"},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateManualOpen
	z.mu.Unlock()

	m.CloseValve("Z1")

	z = m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle after closing manual-open zone, got %v", z.State)
	}
}

func TestForceCloseSetsStateForceClosed(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ValveCommandTopic: "v/z1", ThresholdLow: 50, MaxWateringSeconds: 300},
	})

	m.ForceClose("Z1")
	z := m.GetZone("Z1")
	if z.State != StateForceClosed {
		t.Errorf("expected StateForceClosed, got %v", z.State)
	}
}

func TestForceCloseBlocksTriggerScheduledWatering(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateForceClosed
	z.Moisture = 30
	z.mu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateForceClosed {
		t.Errorf("expected StateForceClosed (force-close blocks trigger), got %v", z.State)
	}
}

func TestClearForceCloseSetsIdle(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ValveCommandTopic: "v/z1"},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateForceClosed
	z.mu.Unlock()

	m.ClearForceClose("Z1")

	z = m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle after ClearForceClose, got %v", z.State)
	}
}

func TestClearForceCloseOnlyAffectsForceClosed(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ValveCommandTopic: "v/z1"},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateIdle
	z.mu.Unlock()

	m.ClearForceClose("Z1")

	z = m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle unchanged, got %v", z.State)
	}
}

func TestAcknowledgeFaultClearsFailsafe(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ValveCommandTopic: "v/z1"},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateFailsafe
	z.mu.Unlock()

	m.AcknowledgeFault("Z1")

	z = m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle after AcknowledgeFault, got %v", z.State)
	}
}

func TestAcknowledgeFaultOnlyAffectsFailsafe(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ValveCommandTopic: "v/z1"},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateIdle
	z.mu.Unlock()

	m.AcknowledgeFault("Z1")

	z = m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle unchanged, got %v", z.State)
	}
}

func TestEvaluateZoneForceClosedBlocksAutoWatering(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateForceClosed
	z.mu.Unlock()

	m.handleSensorReading("Z1", []byte("30"))

	z = m.GetZone("Z1")
	if z.State != StateForceClosed {
		t.Errorf("expected StateForceClosed (evaluateZone should not override), got %v", z.State)
	}
}

func TestTriggerScheduledWateringBlockedByFailsafe(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateFailsafe
	z.Moisture = 30
	z.mu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateFailsafe {
		t.Errorf("expected StateFailsafe (failsafe blocks trigger), got %v", z.State)
	}
}

func TestTriggerScheduledWateringBlockedByManualOpen(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateManualOpen
	z.Moisture = 30
	z.mu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateManualOpen {
		t.Errorf("expected StateManualOpen (manual blocks trigger), got %v", z.State)
	}
}

func TestTriggerScheduledWateringBlockedByActiveWatering(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateWatering
	z.Moisture = 30
	z.mu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateWatering {
		t.Errorf("expected StateWatering (already watering blocks trigger), got %v", z.State)
	}
}

func TestTriggerScheduledWateringBlockedByActiveCooldown(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, CooldownMinutes: 60, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateCooldown
	z.LastWaterEnd = time.Now()
	z.Moisture = 30
	z.mu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateCooldown {
		t.Errorf("expected StateCooldown (active cooldown blocks trigger), got %v", z.State)
	}
}

func TestTriggerScheduledWateringAllowsAfterCooldown(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, CooldownMinutes: 60, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateCooldown
	z.LastWaterEnd = time.Now().Add(-2 * time.Hour)
	z.Moisture = 30
	z.mu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateWatering {
		t.Errorf("expected StateWatering (cooldown expired), got %v", z.State)
	}
}

func TestTriggerScheduledWateringBlockedByRainSensor(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.Moisture = 30
	z.mu.Unlock()

	m.rainMu.Lock()
	m.rainDetected = true
	m.rainMu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle (rain blocks trigger), got %v", z.State)
	}
}

func TestTriggerScheduledWateringBlockedByForecastRain(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.Moisture = 30
	z.mu.Unlock()

	m.SetForecastRain(true)

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle (forecast rain blocks trigger), got %v", z.State)
	}
}

func TestTriggerScheduledWateringBlockedByThresholdHigh(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, ThresholdHigh: 60, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.Moisture = 65
	z.mu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle (threshold_high blocks trigger), got %v", z.State)
	}
}

func TestTriggerScheduledWateringBlockedByMaxActivations(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, MaxActivationsPerDay: 3, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.Moisture = 30
	z.mu.Unlock()

	for i := 0; i < 3; i++ {
		m.store.SaveValveEvent("Z1", "open", 120)
	}

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle (max activations blocks trigger), got %v", z.State)
	}
}

func TestTriggerScheduledWateringSuccess(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, ThresholdHigh: 60, MaxWateringSeconds: 300, MaxActivationsPerDay: 5, CooldownMinutes: 60, ValveCommandTopic: "v/z1", EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})
	fake := m.client.(*fakeMQTTClient)

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.Moisture = 30
	z.mu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)
	time.Sleep(time.Millisecond)

	z = m.GetZone("Z1")
	if z.State != StateWatering {
		t.Errorf("expected StateWatering, got %v", z.State)
	}

	if !containsPublish(fake.published, "v/z1:ON") {
		t.Errorf("expected valve/z1:ON publish, got %v", fake.published)
	}
}

func TestEvaluateZoneMaxDurationExceededTriggersFailsafe(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, ValveCommandTopic: "v/z1", EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})
	fake := m.client.(*fakeMQTTClient)

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.State = StateWatering
	z.WateringStarted = time.Now().Add(-600 * time.Second)
	z.Moisture = 30
	z.mu.Unlock()

	m.handleSensorReading("Z1", []byte("30"))
	time.Sleep(10 * time.Millisecond)

	z = m.GetZone("Z1")
	if z.State != StateFailsafe {
		t.Errorf("expected StateFailsafe (max duration exceeded), got %v", z.State)
	}

	if !containsPublish(fake.published, "v/z1:OFF") {
		t.Errorf("expected safety shutoff OFF, got %v", fake.published)
	}
}

func TestMasterValveOpenedOnEvaluateZone(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, ValveCommandTopic: "v/z1", EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})
	m.cfg.MasterValve.CommandTopic = "v/master"
	fake := m.client.(*fakeMQTTClient)

	m.handleSensorReading("Z1", []byte("30"))
	time.Sleep(time.Millisecond)

	if !containsPublish(fake.published, "v/master:ON") {
		t.Errorf("expected master valve ON publish, got %v", fake.published)
	}
}

func TestMasterValveOpenedOnTriggerScheduledWatering(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, ValveCommandTopic: "v/z1", EarliestWateringTime: windowStart, LatestWateringTime: windowEnd},
	})
	m.cfg.MasterValve.CommandTopic = "v/master"
	fake := m.client.(*fakeMQTTClient)

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.Moisture = 30
	z.mu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)
	time.Sleep(time.Millisecond)

	if !containsPublish(fake.published, "v/master:ON") {
		t.Errorf("expected master valve ON publish, got %v", fake.published)
	}
}

func TestForecastRainSetAndClear(t *testing.T) {
	m := newTestManager(t, nil)

	if m.forecastRainActive {
		t.Error("expected forecastRainActive false by default")
	}

	m.SetForecastRain(true)
	if !m.forecastRainActive {
		t.Error("expected forecastRainActive true after SetForecastRain(true)")
	}

	m.SetForecastRain(false)
	if m.forecastRainActive {
		t.Error("expected forecastRainActive false after SetForecastRain(false)")
	}
}

func TestIndoorZoneDefaultFalse(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1"},
	})
	z := m.GetZone("Z1")
	if z.Config.Indoors {
		t.Error("expected Indoors false by default")
	}
}

func TestEvaluateZoneIndoorSkipsRainSensor(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd, Indoors: true},
	})

	m.rainMu.Lock()
	m.rainDetected = true
	m.rainMu.Unlock()

	m.handleSensorReading("Z1", []byte("30"))
	time.Sleep(time.Millisecond)

	z := m.GetZone("Z1")
	if z.State != StateWatering {
		t.Errorf("expected StateWatering (indoor skips rain), got %v", z.State)
	}
}

func TestEvaluateZoneIndoorSkipsForecastRain(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd, Indoors: true},
	})

	m.SetForecastRain(true)

	m.handleSensorReading("Z1", []byte("30"))
	time.Sleep(time.Millisecond)

	z := m.GetZone("Z1")
	if z.State != StateWatering {
		t.Errorf("expected StateWatering (indoor skips forecast rain), got %v", z.State)
	}
}

func TestEvaluateZoneOutdoorBlockedByRainSensor(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd, Indoors: false},
	})

	m.rainMu.Lock()
	m.rainDetected = true
	m.rainMu.Unlock()

	m.handleSensorReading("Z1", []byte("30"))

	z := m.GetZone("Z1")
	if z.State != StateIdle {
		t.Errorf("expected StateIdle (outdoor blocked by rain), got %v", z.State)
	}
}

func TestTriggerScheduledWateringIndoorSkipsRainSensor(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd, Indoors: true},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.Moisture = 30
	z.mu.Unlock()

	m.rainMu.Lock()
	m.rainDetected = true
	m.rainMu.Unlock()

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateWatering {
		t.Errorf("expected StateWatering (indoor skips rain), got %v", z.State)
	}
}

func TestTriggerScheduledWateringIndoorSkipsForecastRain(t *testing.T) {
	now := time.Now()
	windowStart := fmt.Sprintf("%02d:%02d", now.Hour()-1, now.Minute())
	windowEnd := fmt.Sprintf("%02d:%02d", now.Hour()+2, now.Minute())

	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Z1", ThresholdLow: 50, MaxWateringSeconds: 300, EarliestWateringTime: windowStart, LatestWateringTime: windowEnd, Indoors: true},
	})

	z := m.GetZone("Z1")
	z.mu.Lock()
	z.Moisture = 30
	z.mu.Unlock()

	m.SetForecastRain(true)

	m.TriggerScheduledWatering("Z1", 120)

	z = m.GetZone("Z1")
	if z.State != StateWatering {
		t.Errorf("expected StateWatering (indoor skips forecast rain), got %v", z.State)
	}
}

func TestSetRainDetectedSkipsIndoorZones(t *testing.T) {
	m := newTestManager(t, []config.ZoneConfig{
		{Name: "Outdoor", ValveCommandTopic: "valve/outdoor", ThresholdLow: 50, MaxWateringSeconds: 300},
		{Name: "Indoor", ValveCommandTopic: "valve/indoor", ThresholdLow: 50, MaxWateringSeconds: 300, Indoors: true},
	})

	fake := m.client.(*fakeMQTTClient)

	// Open both valves
	m.OpenValve("Outdoor")
	m.OpenValve("Indoor")
	time.Sleep(time.Millisecond)

	zOut := m.GetZone("Outdoor")
	zIn := m.GetZone("Indoor")
	if zOut.State != StateManualOpen {
		t.Fatalf("expected Outdoor StateManualOpen, got %v", zOut.State)
	}
	if zIn.State != StateManualOpen {
		t.Fatalf("expected Indoor StateManualOpen, got %v", zIn.State)
	}

	// Simulate rain detection
	m.setRainDetected(true, "test")
	time.Sleep(time.Millisecond)

	// Outdoor valve should be closed
	zOut = m.GetZone("Outdoor")
	if zOut.State != StateCooldown && zOut.State != StateIdle {
		t.Errorf("expected Outdoor closed after rain, got %v", zOut.State)
	}
	if !containsPublish(fake.published, "valve/outdoor:OFF") {
		t.Errorf("expected outdoor valve OFF publish, got %v", fake.published)
	}

	// Indoor valve should still be open
	zIn = m.GetZone("Indoor")
	if zIn.State != StateManualOpen {
		t.Errorf("expected Indoor still StateManualOpen after rain, got %v", zIn.State)
	}
	if containsPublish(fake.published, "valve/indoor:OFF") {
		t.Error("indoor valve should NOT be closed on rain")
	}
}
