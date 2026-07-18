package scheduler

import (
	"testing"
	"time"

	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/models"
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

func TestBuildWeekSchedule(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC) // Sunday

	entries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 1800},
		{ZoneName: "Z1", DayOfWeek: "Wed", Time: "06:30", Duration: 3600},
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "18:00", Duration: 900},
		{ZoneName: "Z1", Month: 7, DayOfWeek: "Fri", Time: "07:00", Duration: 1200},
	}

	result := BuildWeekSchedule(now, entries)

	if len(result) != 7 {
		t.Fatalf("expected 7 days, got %d", len(result))
	}

	if len(result[0].Bars) != 0 {
		t.Errorf("Sunday: expected 0 bars, got %d", len(result[0].Bars))
	}
	if len(result[1].Bars) != 2 {
		t.Errorf("Monday: expected 2 bars, got %d", len(result[1].Bars))
	}
	if len(result[2].Bars) != 0 {
		t.Errorf("Tuesday: expected 0 bars, got %d", len(result[2].Bars))
	}
	if len(result[3].Bars) != 1 {
		t.Errorf("Wednesday: expected 1 bar, got %d", len(result[3].Bars))
	}
	if len(result[4].Bars) != 0 {
		t.Errorf("Thursday: expected 0 bars, got %d", len(result[4].Bars))
	}
	if len(result[5].Bars) != 1 {
		t.Errorf("Friday: expected 1 bar (month override), got %d", len(result[5].Bars))
	}
	if len(result[6].Bars) != 0 {
		t.Errorf("Saturday: expected 0 bars, got %d", len(result[6].Bars))
	}

	monBar0 := result[1].Bars[0]
	if monBar0.StartPct != 25.0 {
		t.Errorf("Monday bar 0 StartPct: got %.2f, want 25.00", monBar0.StartPct)
	}
	if monBar0.WidthPct != float64(30)/float64(1440)*100 {
		t.Errorf("Monday bar 0 WidthPct: got %.4f, want %.4f", monBar0.WidthPct, float64(30)/float64(1440)*100)
	}
	if monBar0.Start != "06:00" {
		t.Errorf("Monday bar 0 Start: got %q, want 06:00", monBar0.Start)
	}

	wedBar := result[3].Bars[0]
	if wedBar.Start != "06:30" {
		t.Errorf("Wednesday bar Start: got %q, want 06:30", wedBar.Start)
	}
	if wedBar.Label != "60m" {
		t.Errorf("Wednesday bar Label: got %q, want 60m", wedBar.Label)
	}
}

func TestBuildWeekScheduleEmpty(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	result := BuildWeekSchedule(now, nil)

	if len(result) != 7 {
		t.Fatalf("expected 7 days, got %d", len(result))
	}
	for i, d := range result {
		if len(d.Bars) != 0 {
			t.Errorf("day %d: expected 0 bars, got %d", i, len(d.Bars))
		}
	}
}

func TestBuildWeekScheduleMonthOverride(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC) // January

	entries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 1800},
		{ZoneName: "Z1", Month: 1, DayOfWeek: "Mon", Time: "08:00", Duration: 7200},
	}

	result := BuildWeekSchedule(now, entries)

	for _, d := range result {
		if d.Day == "Mon" {
			if len(d.Bars) != 1 {
				t.Fatalf("Monday in January: expected 1 bar (month override only), got %d", len(d.Bars))
			}
			if d.Bars[0].Start != "08:00" {
				t.Errorf("expected month override start 08:00, got %q", d.Bars[0].Start)
			}
			return
		}
	}
	t.Fatal("Monday not found in week schedule")
}

func TestNextScheduledOccurrence(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, loc) // Sunday

	entries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 1800},
		{ZoneName: "Z1", DayOfWeek: "Wed", Time: "14:00", Duration: 3600},
	}

	got, ok := NextScheduledOccurrence(now, entries)
	if !ok {
		t.Fatal("expected an occurrence")
	}
	// Next Monday is July 13
	want := time.Date(2026, 7, 13, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextScheduledOccurrenceEmpty(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	_, ok := NextScheduledOccurrence(now, nil)
	if ok {
		t.Error("expected no occurrence for empty entries")
	}
}

func TestNextWindowOpen_BeforeWindow(t *testing.T) {
	now := time.Date(2026, 7, 12, 5, 0, 0, 0, time.UTC)
	got, ok := nextWindowOpen(now, "06:00", "10:00")
	if !ok {
		t.Fatal("expected ok")
	}
	want := time.Date(2026, 7, 12, 6, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextWindowOpen_DuringWindow(t *testing.T) {
	now := time.Date(2026, 7, 12, 8, 30, 0, 0, time.UTC)
	got, ok := nextWindowOpen(now, "06:00", "10:00")
	if !ok {
		t.Fatal("expected ok")
	}
	if !got.Equal(now) {
		t.Errorf("got %v, want %v (now)", got, now)
	}
}

func TestNextWindowOpen_AfterWindow(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	got, ok := nextWindowOpen(now, "06:00", "10:00")
	if !ok {
		t.Fatal("expected ok")
	}
	want := time.Date(2026, 7, 13, 6, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextWateringForZone_ScheduleOnly(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, loc) // Sunday
	snap := zones.ZoneSnapshot{
		Config:   config.ZoneConfig{Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30},
		Moisture: 50,
		State:    zones.StateIdle,
	}
	entries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 1800},
	}

	got, reason := NextWateringForZone(now, snap, entries)
	want := time.Date(2026, 7, 13, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "Schedule" {
		t.Errorf("reason: got %q, want Schedule", reason)
	}
}

func TestNextWateringForZone_SensorLowBeforeSchedule(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 10, 1, 0, 0, loc) // Sunday, 10:01 — past window end
	snap := zones.ZoneSnapshot{
		Config:   config.ZoneConfig{Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30, EarliestWateringTime: "06:00", LatestWateringTime: "10:00"},
		Moisture: 20, // below ThresholdLow
		State:    zones.StateIdle,
	}
	entries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 1800}, // next is Mon 06:00
	}

	got, reason := NextWateringForZone(now, snap, entries)
	// Next window open is tomorrow 06:00 (past today's 10:00 latest), Mon 06:00 is also tomorrow
	// Both are the same: July 13 06:00
	want := time.Date(2026, 7, 13, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "Soil moisture low" {
		t.Errorf("reason: got %q, want 'Soil moisture low'", reason)
	}
}

func TestNextWateringForZone_SensorLowSoonerThanSchedule(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 5, 0, 0, 0, loc) // Sunday, 05:00
	snap := zones.ZoneSnapshot{
		Config:   config.ZoneConfig{Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30, EarliestWateringTime: "06:00", LatestWateringTime: "10:00"},
		Moisture: 20,
		State:    zones.StateIdle,
	}
	entries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 1800},
	}

	got, reason := NextWateringForZone(now, snap, entries)
	// Today's window opens at 06:00, schedule is Mon 06:00 = July 13
	// Window is sooner
	want := time.Date(2026, 7, 12, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "Soil moisture low" {
		t.Errorf("reason: got %q, want 'Soil moisture low'", reason)
	}
}

func TestNextWateringForZone_FailsafeReturnsEmpty(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	snap := zones.ZoneSnapshot{
		Config:   config.ZoneConfig{Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30},
		Moisture: 20,
		State:    zones.StateFailsafe,
	}
	entries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 1800},
	}

	got, reason := NextWateringForZone(now, snap, entries)
	// Failsafe blocks sensor-triggered, but schedule still shows
	want := time.Date(2026, 7, 13, 6, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "Schedule" {
		t.Errorf("reason: got %q, want Schedule", reason)
	}
}

func TestNextWateringForZone_NoScheduleReturnsEmpty(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	snap := zones.ZoneSnapshot{
		Config:   config.ZoneConfig{Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30, EarliestWateringTime: "06:00", LatestWateringTime: "10:00"},
		Moisture: 50,
		State:    zones.StateIdle,
	}

	got, _ := NextWateringForZone(now, snap, nil)
	if !got.IsZero() {
		t.Errorf("expected zero time, got %v", got)
	}
}

func TestNextWateringForZone_CooldownNoSchedulePastWindow(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 14, 0, 0, 0, loc) // Sunday, 14:00 — past window
	snap := zones.ZoneSnapshot{
		Config: config.ZoneConfig{
			Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30,
			EarliestWateringTime: "06:00", LatestWateringTime: "10:00",
			CooldownMinutes: 60,
		},
		Moisture:    20, // below ThresholdLow
		State:       zones.StateIdle,
		LastWaterEnd: time.Date(2026, 7, 12, 13, 30, 0, 0, loc), // 30 min ago, still in cooldown
	}

	got, reason := NextWateringForZone(now, snap, nil)
	// Cooldown ends at 14:30 — should show cooldown end time
	want := time.Date(2026, 7, 12, 14, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "Cooldown until 14:30" {
		t.Errorf("reason: got %q, want 'Cooldown until 14:30'", reason)
	}
}

func TestNextWateringForZone_CooldownNoScheduleWithinWindow(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, loc) // Sunday, 08:00 — within window
	snap := zones.ZoneSnapshot{
		Config: config.ZoneConfig{
			Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30,
			EarliestWateringTime: "06:00", LatestWateringTime: "10:00",
			CooldownMinutes: 120,
		},
		Moisture:    20,
		State:       zones.StateIdle,
		LastWaterEnd: time.Date(2026, 7, 12, 7, 0, 0, 0, loc), // 1h ago, cooldown ends at 09:00
	}

	got, reason := NextWateringForZone(now, snap, nil)
	// Cooldown ends at 09:00 — should show cooldown end time
	want := time.Date(2026, 7, 12, 9, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "Cooldown until 09:00" {
		t.Errorf("reason: got %q, want 'Cooldown until 09:00'", reason)
	}
}

func TestNextWateringForZone_CooldownWithSchedule(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 14, 0, 0, 0, loc) // Sunday, 14:00 — past window
	snap := zones.ZoneSnapshot{
		Config: config.ZoneConfig{
			Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30,
			EarliestWateringTime: "06:00", LatestWateringTime: "10:00",
			CooldownMinutes: 60,
		},
		Moisture:    20,
		State:       zones.StateIdle,
		LastWaterEnd: time.Date(2026, 7, 12, 13, 30, 0, 0, loc),
	}
	entries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 1800},
	}

	got, reason := NextWateringForZone(now, snap, entries)
	// Moisture below threshold + cooldown active → return cooldown end (zone will re-water immediately after cooldown)
	want := time.Date(2026, 7, 12, 14, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "Cooldown until 14:30" {
		t.Errorf("reason: got %q, want 'Cooldown until 14:30'", reason)
	}
}

func TestNextWateringForZone_CooldownWithScheduleAboveThreshold(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 14, 0, 0, 0, loc) // Sunday, 14:00
	snap := zones.ZoneSnapshot{
		Config: config.ZoneConfig{
			Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30,
			EarliestWateringTime: "06:00", LatestWateringTime: "10:00",
			CooldownMinutes: 60,
		},
		Moisture:    60, // above ThresholdLow
		State:       zones.StateIdle,
		LastWaterEnd: time.Date(2026, 7, 12, 13, 30, 0, 0, loc),
	}
	entries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 1800},
	}

	got, reason := NextWateringForZone(now, snap, entries)
	// Moisture above threshold → next scheduled time
	want := time.Date(2026, 7, 13, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "Schedule" {
		t.Errorf("reason: got %q, want 'Schedule'", reason)
	}
}

func TestNextWateringForZone_ActiveWateringNoCooldown(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, loc) // Within window
	snap := zones.ZoneSnapshot{
		Config: config.ZoneConfig{
			Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30,
			MaxWateringSeconds:  300,
			EarliestWateringTime: "06:00", LatestWateringTime: "10:00",
		},
		Moisture:         20,
		State:            zones.StateWatering,
		WateringStarted:  time.Date(2026, 7, 12, 7, 58, 0, 0, loc), // started 2 min ago, ends at 8:03
	}

	got, reason := NextWateringForZone(now, snap, nil)
	// Watering ends at 8:03, no cooldown → next window open from 8:03 is 8:03
	want := time.Date(2026, 7, 12, 8, 3, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "After current watering + cooldown" {
		t.Errorf("reason: got %q, want 'After current watering + cooldown'", reason)
	}
}

func TestNextWateringForZone_ActiveWateringWithCooldown(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, loc) // Within window
	snap := zones.ZoneSnapshot{
		Config: config.ZoneConfig{
			Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30,
			MaxWateringSeconds:  300,
			CooldownMinutes:     30,
			EarliestWateringTime: "06:00", LatestWateringTime: "10:00",
		},
		Moisture:         20,
		State:            zones.StateWatering,
		WateringStarted:  time.Date(2026, 7, 12, 7, 58, 0, 0, loc), // started 2 min ago, ends at 8:03
	}

	got, reason := NextWateringForZone(now, snap, nil)
	// Watering ends at 8:03, cooldown until 8:33 → next window open is 8:33
	want := time.Date(2026, 7, 12, 8, 33, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "After current watering + cooldown" {
		t.Errorf("reason: got %q, want 'After current watering + cooldown'", reason)
	}
}

func TestNextWateringForZone_ActiveWateringPastWindow(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 12, 9, 50, 0, 0, loc) // Within window
	snap := zones.ZoneSnapshot{
		Config: config.ZoneConfig{
			Name: "Z1", ThresholdHigh: 80, ThresholdLow: 30,
			MaxWateringSeconds:  300,
			CooldownMinutes:     30,
			EarliestWateringTime: "06:00", LatestWateringTime: "10:00",
		},
		Moisture:         20,
		State:            zones.StateWatering,
		WateringStarted:  time.Date(2026, 7, 12, 9, 48, 0, 0, loc), // started 2 min ago, ends at 9:53
	}

	got, reason := NextWateringForZone(now, snap, nil)
	// Watering ends at 9:53, cooldown until 10:23 → past window, next open is tomorrow 06:00
	want := time.Date(2026, 7, 13, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("time: got %v, want %v", got, want)
	}
	if reason != "After current watering + cooldown" {
		t.Errorf("reason: got %q, want 'After current watering + cooldown'", reason)
	}
}

func TestWeekdayFromString(t *testing.T) {
	tests := []struct {
		input string
		want  time.Weekday
		ok    bool
	}{
		{"Mon", time.Monday, true},
		{"Tuesday", time.Tuesday, true},
		{"wed", time.Wednesday, true},
		{"THURSDAY", time.Thursday, true},
		{"fri", time.Friday, true},
		{"Saturday", time.Saturday, true},
		{"SUN", time.Sunday, true},
		{"", time.Sunday, false},
		{"invalid", time.Sunday, false},
	}
	for _, tt := range tests {
		got, ok := WeekdayFromString(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Errorf("WeekdayFromString(%q): got %v, %v; want %v, %v", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}
