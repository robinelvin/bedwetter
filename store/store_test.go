package store

import (
	"testing"
	"time"

	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/models"
)

func newTestStore(t *testing.T) *Store {
	s, err := New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return s
}

func TestZoneCRUD(t *testing.T) {
	s := newTestStore(t)

	zc := &models.ZoneConfig{Name: "Test Zone", ThresholdLow: 30, ThresholdHigh: 60}
	if err := s.CreateZoneConfig(zc); err != nil {
		t.Fatalf("CreateZoneConfig failed: %v", err)
	}
	if zc.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	all, err := s.GetAllZoneConfigs()
	if err != nil {
		t.Fatalf("GetAllZoneConfigs failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 zone, got %d", len(all))
	}
	if all[0].Name != "Test Zone" {
		t.Errorf("zone name: got %q", all[0].Name)
	}

	all[0].ThresholdLow = 20
	if err := s.UpdateZoneConfig(all[0].ID, &all[0]); err != nil {
		t.Fatalf("UpdateZoneConfig failed: %v", err)
	}
	updated, _ := s.GetAllZoneConfigs()
	if updated[0].ThresholdLow != 20 {
		t.Errorf("ThresholdLow not updated: got %d", updated[0].ThresholdLow)
	}

	if err := s.DeleteZoneConfig(all[0].ID); err != nil {
		t.Fatalf("DeleteZoneConfig failed: %v", err)
	}
	afterDelete, _ := s.GetAllZoneConfigs()
	if len(afterDelete) != 0 {
		t.Errorf("expected 0 zones after delete, got %d", len(afterDelete))
	}
}

func TestZoneCRUDMultiple(t *testing.T) {
	s := newTestStore(t)

	zones := []*models.ZoneConfig{
		{Name: "Zone B", ThresholdLow: 20},
		{Name: "Zone A", ThresholdLow: 30},
		{Name: "Zone C", ThresholdLow: 40},
	}
	for _, z := range zones {
		if err := s.CreateZoneConfig(z); err != nil {
			t.Fatalf("create %s: %v", z.Name, err)
		}
	}

	all, _ := s.GetAllZoneConfigs()
	if len(all) != 3 {
		t.Fatalf("expected 3 zones, got %d", len(all))
	}
	if all[0].Name != "Zone A" || all[1].Name != "Zone B" || all[2].Name != "Zone C" {
		t.Errorf("order: %v", []string{all[0].Name, all[1].Name, all[2].Name})
	}
}

func TestLoadConfigZones(t *testing.T) {
	s := newTestStore(t)

	yamlZones := []config.ZoneConfig{
		{Name: "YAML Zone 1", ThresholdLow: 10, ThresholdHigh: 50},
		{Name: "YAML Zone 2", ThresholdLow: 20, ThresholdHigh: 60},
	}

	if err := s.LoadConfigZones(yamlZones); err != nil {
		t.Fatalf("LoadConfigZones failed: %v", err)
	}

	all, _ := s.GetAllZoneConfigs()
	if len(all) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(all))
	}
	if all[0].Name != "YAML Zone 1" || all[1].Name != "YAML Zone 2" {
		t.Errorf("loaded: %v", []string{all[0].Name, all[1].Name})
	}
}

func TestLoadConfigZonesIdempotent(t *testing.T) {
	s := newTestStore(t)

	yamlZones := []config.ZoneConfig{{Name: "Z1"}}
	if err := s.LoadConfigZones(yamlZones); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadConfigZones(yamlZones); err != nil {
		t.Fatal(err)
	}

	all, _ := s.GetAllZoneConfigs()
	if len(all) != 1 {
		t.Errorf("expected 1 zone (idempotent), got %d", len(all))
	}
}

func TestSensorReadings(t *testing.T) {
	s := newTestStore(t)

	if err := s.SaveSensorReading("Z1", 45.5, 60.0); err != nil {
		t.Fatalf("SaveSensorReading failed: %v", err)
	}
	if err := s.SaveSensorReading("Z1", 50.0, 62.0); err != nil {
		t.Fatalf("SaveSensorReading failed: %v", err)
	}
	if err := s.SaveSensorReading("Z2", 30.0, 55.0); err != nil {
		t.Fatalf("SaveSensorReading failed: %v", err)
	}

	readings, err := s.RecentReadings("Z1", 24)
	if err != nil {
		t.Fatalf("RecentReadings failed: %v", err)
	}
	if len(readings) != 2 {
		t.Fatalf("expected 2 readings for Z1, got %d", len(readings))
	}
	if readings[0].Moisture != 45.5 || readings[1].Moisture != 50.0 {
		t.Errorf("unexpected moisture values: %v", readings)
	}

	z2readings, _ := s.RecentReadings("Z2", 24)
	if len(z2readings) != 1 {
		t.Errorf("expected 1 reading for Z2, got %d", len(z2readings))
	}
}

func TestValveEvents(t *testing.T) {
	s := newTestStore(t)

	s.SaveValveEvent("Z1", "open", 300)
	s.SaveValveEvent("Z1", "close", 0)
	s.SaveValveEvent("Z1", "open", 120)
	s.SaveValveEvent("Z2", "open", 200)

	events, err := s.RecentValveEvents("Z1", 10)
	if err != nil {
		t.Fatalf("RecentValveEvents failed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events for Z1, got %d", len(events))
	}
	if events[0].State != "open" || events[0].Duration != 120 {
		t.Errorf("expected most recent first, got state=%s dur=%d", events[0].State, events[0].Duration)
	}
}

func TestActivationsToday(t *testing.T) {
	s := newTestStore(t)

	count, err := s.ActivationsToday("Z1")
	if err != nil {
		t.Fatalf("ActivationsToday failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 activations, got %d", count)
	}

	s.SaveValveEvent("Z1", "open", 300)
	s.SaveValveEvent("Z1", "open", 200)

	count, _ = s.ActivationsToday("Z1")
	if count != 2 {
		t.Errorf("expected 2 activations, got %d", count)
	}

	count, _ = s.ActivationsToday("Z2")
	if count != 0 {
		t.Errorf("expected 0 for Z2, got %d", count)
	}
}

func TestScheduleCRUD(t *testing.T) {
	s := newTestStore(t)

	entry := &models.ScheduleConfig{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 300}
	if err := s.CreateScheduleEntry(entry); err != nil {
		t.Fatalf("CreateScheduleEntry failed: %v", err)
	}
	if entry.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	all, err := s.GetAllSchedules()
	if err != nil {
		t.Fatalf("GetAllSchedules failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(all))
	}

	sched, err := s.GetSchedule("Z1")
	if err != nil {
		t.Fatalf("GetSchedule failed: %v", err)
	}
	if len(sched) != 1 {
		t.Fatalf("expected 1 schedule for Z1, got %d", len(sched))
	}
	if sched[0].Time != "06:00" {
		t.Errorf("time: got %q", sched[0].Time)
	}

	if err := s.DeleteScheduleByID(entry.ID); err != nil {
		t.Fatalf("DeleteScheduleByID failed: %v", err)
	}
	after, _ := s.GetAllSchedules()
	if len(after) != 0 {
		t.Errorf("expected 0 after delete, got %d", len(after))
	}
}

func TestSaveScheduleReplace(t *testing.T) {
	s := newTestStore(t)

	entries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 300},
		{ZoneName: "Z1", DayOfWeek: "Wed", Time: "07:00", Duration: 200},
	}
	if err := s.SaveSchedule(entries); err != nil {
		t.Fatalf("SaveSchedule failed: %v", err)
	}

	all, _ := s.GetAllSchedules()
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}

	newEntries := []models.ScheduleConfig{
		{ZoneName: "Z1", DayOfWeek: "Fri", Time: "08:00", Duration: 400},
	}
	if err := s.SaveSchedule(newEntries); err != nil {
		t.Fatalf("SaveSchedule replace failed: %v", err)
	}

	all, _ = s.GetAllSchedules()
	if len(all) != 1 {
		t.Fatalf("expected 1 after replace, got %d", len(all))
	}
	if all[0].DayOfWeek != "Fri" {
		t.Errorf("expected Fri, got %s", all[0].DayOfWeek)
	}
}

func TestLoadConfigSchedules(t *testing.T) {
	s := newTestStore(t)

	zoneSchedules := []config.ZoneSchedule{
		{
			ZoneName: "Z1",
			Schedule: []config.ScheduleEntry{
				{DayOfWeek: "Mon", Time: "06:00", Duration: 300},
			},
			MonthOverride: []config.MonthOverride{
				{
					Month: 7,
					Schedule: []config.ScheduleEntry{
						{DayOfWeek: "Sun", Time: "07:00", Duration: 400},
					},
				},
			},
		},
	}

	if err := s.LoadConfigSchedules(zoneSchedules); err != nil {
		t.Fatalf("LoadConfigSchedules failed: %v", err)
	}

	all, _ := s.GetAllSchedules()
	if len(all) != 2 {
		t.Fatalf("expected 2 schedules, got %d", len(all))
	}
}

func TestGetScheduleEmpty(t *testing.T) {
	s := newTestStore(t)

	sched, err := s.GetSchedule("NONEXISTENT")
	if err != nil {
		t.Fatalf("GetSchedule failed: %v", err)
	}
	if len(sched) != 0 {
		t.Errorf("expected empty, got %d", len(sched))
	}
}

func TestSaveSensorReadingBoundaries(t *testing.T) {
	s := newTestStore(t)

	tests := []struct {
		moisture float64
		humidity float64
	}{
		{0, 0},
		{100, 100},
		{45.5, 62.3},
		{-1, -1},
	}

	for _, tt := range tests {
		if err := s.SaveSensorReading("Z1", tt.moisture, tt.humidity); err != nil {
			t.Errorf("SaveSensorReading(%f, %f): %v", tt.moisture, tt.humidity, err)
		}
	}
}

func TestRecentReadingsTimeFilter(t *testing.T) {
	s := newTestStore(t)

	s.SaveSensorReading("Z1", 50, 60)

	readings, err := s.RecentReadings("Z1", 1)
	if err != nil {
		t.Fatalf("RecentReadings failed: %v", err)
	}
	if len(readings) != 1 {
		t.Errorf("expected 1 recent reading, got %d", len(readings))
	}

	oldReadings, err := s.RecentReadings("Z1", 0)
	if err != nil {
		t.Fatalf("RecentReadings(0) failed: %v", err)
	}
	if len(oldReadings) != 0 {
		t.Errorf("expected 0 readings for 0 hours, got %d", len(oldReadings))
	}
}

func TestDB(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()
	if db == nil {
		t.Fatal("DB() returned nil")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("getting underlying sql.DB: %v", err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestNewInvalidPath(t *testing.T) {
	_, err := New("/nonexistent/dir/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

func TestSaveScheduleEmpty(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSchedule(nil); err != nil {
		t.Errorf("SaveSchedule(nil) should be ok: %v", err)
	}
	if err := s.SaveSchedule([]models.ScheduleConfig{}); err != nil {
		t.Errorf("SaveSchedule(empty) should be ok: %v", err)
	}
}

func timeSince(t time.Time) time.Duration {
	return time.Since(t)
}

func TestDeleteNonExistent(t *testing.T) {
	s := newTestStore(t)
	if err := s.DeleteZoneConfig(999); err != nil {
		t.Errorf("DeleteZoneConfig non-existent: %v", err)
	}
	if err := s.DeleteScheduleByID(999); err != nil {
		t.Errorf("DeleteScheduleByID non-existent: %v", err)
	}
}
