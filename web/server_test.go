package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rob/bedwetter/alerts"
	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/models"
	"github.com/rob/bedwetter/mqtt"
	"github.com/rob/bedwetter/store"
	"github.com/rob/bedwetter/zones"
)

func newTestServer(t *testing.T) *Server {
	cfg := &config.Config{
		Web:     config.WebConfig{ListenAddr: ":0"},
		Alerts:  config.AlertsConfig{Email: "test@example.com", StaleSensorMinutes: 60},
		MQTT:    config.MQTTConfig{Broker: "localhost", Port: 1883},
		Zones:   []config.ZoneConfig{{Name: "Z1", ThresholdLow: 30, ThresholdHigh: 60}},
		DBPath:  t.TempDir() + "/test.db",
	}

	st, err := store.New(cfg.DBPath)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}

	// Create zone in DB for testing
	st.CreateZoneConfig(&models.ZoneConfig{Name: "Z1", ThresholdLow: 30, ThresholdHigh: 60})

	zoneManager := zones.NewManager(cfg, &mqttClientMock{}, st, nil, nil)
	alertMgr := alerts.New(cfg, zoneManager)

	return New(cfg, st, zoneManager, alertMgr)
}

type mqttClientMock struct{}

func (m *mqttClientMock) Publish(topic string, qos byte, retained bool, payload string) error { return nil }
func (m *mqttClientMock) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) error { return nil }
func (m *mqttClientMock) SubscribeMultiple(topics map[string]byte, handler mqtt.MessageHandler) error { return nil }
func (m *mqttClientMock) IsConnected() bool { return true }
func (m *mqttClientMock) Unsubscribe(topics ...string) {}
func (m *mqttClientMock) Disconnect(quiesce uint) {}

func setupGin() {
	gin.SetMode(gin.TestMode)
}

func TestConfigPage(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/config", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Configuration") {
		t.Error("expected page to contain Configuration")
	}
	if !strings.Contains(body, "Z1") {
		t.Error("expected page to contain zone Z1")
	}
	if !strings.Contains(body, "test@example.com") {
		t.Error("expected page to contain alert email")
	}
}

func TestConfigPageEdit(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	// First create a zone in DB
	sv.store.CreateZoneConfig(&models.ZoneConfig{Name: "Edit Zone", ThresholdLow: 20, ThresholdHigh: 80})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/config?edit=1", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Edit Zone") {
		t.Error("expected page to contain edit zone name")
	}
	if !strings.Contains(body, "Update Zone") {
		t.Error("expected page to contain Update Zone button")
	}
}

func TestSaveAlerts(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{"email": {"new@example.com"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/alerts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	if sv.cfg.Alerts.Email != "new@example.com" {
		t.Errorf("email not updated: got %q", sv.cfg.Alerts.Email)
	}
}

func TestCreateZone(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{
		"name":                   {"New Zone"},
		"threshold_low":          {"20"},
		"threshold_high":         {"80"},
		"max_watering_seconds":   {"300"},
		"max_activations_per_day": {"5"},
		"cooldown_minutes":       {"90"},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/zones", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	// Verify zone was added to zone manager
	z := sv.zoneManager.GetZone("New Zone")
	if z == nil {
		t.Fatal("expected zone to be in manager")
	}
	if z.Config.ThresholdLow != 20 {
		t.Errorf("ThresholdLow: got %d", z.Config.ThresholdLow)
	}

	// Verify zone was added to DB
	dbZones, _ := sv.store.GetAllZoneConfigs()
	found := false
	for _, dz := range dbZones {
		if dz.Name == "New Zone" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected zone in DB")
	}
}

func TestUpdateZone(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	// Get existing zone ID
	dbZones, _ := sv.store.GetAllZoneConfigs()
	if len(dbZones) == 0 {
		t.Fatal("expected zones in DB")
	}
	zoneID := dbZones[0].ID

	form := url.Values{
		"id":                     {strconv.FormatUint(uint64(zoneID), 10)},
		"name":                   {"Z1"},
		"threshold_low":          {"50"},
		"threshold_high":         {"90"},
		"max_watering_seconds":   {"600"},
		"max_activations_per_day": {"10"},
		"cooldown_minutes":       {"30"},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/zones", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	z := sv.zoneManager.GetZone("Z1")
	if z == nil {
		t.Fatal("expected zone in manager")
	}
	if z.Config.ThresholdLow != 50 {
		t.Errorf("ThresholdLow not updated: got %d", z.Config.ThresholdLow)
	}
}

func TestDeleteZone(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	// Create a zone
	sv.zoneManager.AddZone(config.ZoneConfig{Name: "Delete Me"})

	// Create it in DB too
	sv.store.CreateZoneConfig(&models.ZoneConfig{Name: "Delete Me"})

	dbZones, _ := sv.store.GetAllZoneConfigs()
	var id uint
	for _, dz := range dbZones {
		if dz.Name == "Delete Me" {
			id = dz.ID
			break
		}
	}
	if id == 0 {
		t.Fatal("expected zone in DB with ID")
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/zones/"+strconv.FormatUint(uint64(id), 10)+"/delete", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	z := sv.zoneManager.GetZone("Delete Me")
	if z != nil {
		t.Error("expected zone to be removed from manager")
	}
}

func TestSchedulesPage(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/schedules", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Watering Schedules") {
		t.Error("expected schedules page heading")
	}
	if !strings.Contains(body, "Z1") {
		t.Error("expected zone in zone dropdown")
	}
}

func TestSaveSchedule(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{
		"zone_name":  {"Z1"},
		"day_of_week": {"Mon"},
		"time":       {"06:00"},
		"duration":   {"300"},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/schedules", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	schedules, _ := sv.store.GetAllSchedules()
	if len(schedules) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(schedules))
	}
	if schedules[0].DayOfWeek != "Mon" {
		t.Errorf("DayOfWeek: got %q", schedules[0].DayOfWeek)
	}
}

func TestDeleteSchedule(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	// Create a schedule
	sv.store.CreateScheduleEntry(&models.ScheduleConfig{ZoneName: "Z1", DayOfWeek: "Mon", Time: "06:00", Duration: 300})

	schedules, _ := sv.store.GetAllSchedules()
	if len(schedules) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(schedules))
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/schedules/"+strconv.FormatUint(uint64(schedules[0].ID), 10)+"/delete", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", w.Code)
	}

	schedules, _ = sv.store.GetAllSchedules()
	if len(schedules) != 0 {
		t.Errorf("expected 0 schedules after delete, got %d", len(schedules))
	}
}

func TestDashboard(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Z1") {
		t.Error("expected dashboard to contain zone name")
	}
}

func TestDashboardZones(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dashboard/zones", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestOpenValve(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/zones/Z1/open", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	z := sv.zoneManager.GetZone("Z1")
	if z.State != zones.StateManualOpen {
		t.Errorf("expected StateManualOpen, got %v", z.State)
	}
}

func TestCloseValve(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	sv.zoneManager.OpenValve("Z1")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/zones/Z1/close", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	z := sv.zoneManager.GetZone("Z1")
	if z.State != zones.StateIdle {
		t.Errorf("expected StateIdle, got %v", z.State)
	}
}

func TestZoneHistory(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	sv.store.SaveSensorReading("Z1", 45.0, 60.0)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/zones/Z1/history?hours=24", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Z1") {
		t.Error("expected history page to contain zone name")
	}
}

func TestAPIZones(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/zones", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var zones []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&zones); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(zones) != 1 {
		t.Fatalf("expected 1 zone, got %d", len(zones))
	}
	if zones[0]["name"] != "Z1" {
		t.Errorf("zone name: got %v", zones[0]["name"])
	}
}


