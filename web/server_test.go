package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/approvals/go-approval-tests"
	"github.com/gin-gonic/gin"
	"github.com/robinelvin/bedwetter/alerts"
	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/models"
	"github.com/robinelvin/bedwetter/mqtt"
	"github.com/robinelvin/bedwetter/scheduler"
	"github.com/robinelvin/bedwetter/store"
	"github.com/robinelvin/bedwetter/zones"
	"golang.org/x/crypto/bcrypt"
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
	sched := scheduler.New(cfg, st, zoneManager)

	return New(cfg, st, zoneManager, alertMgr, &mqttClientMock{}, nil, sched)
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

	verifyPage(t, sv, "/config")
}

func TestConfigPageEdit(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	sv.store.CreateZoneConfig(&models.ZoneConfig{Name: "Edit Zone", ThresholdLow: 20, ThresholdHigh: 80})

	verifyPage(t, sv, "/config?edit=1")
}

func TestSaveAlerts(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{
		"email":                {"new@example.com"},
		"from_email":           {"from@example.com"},
		"stale_sensor_minutes": {"30"},
		"smtp_server":          {"smtp.example.com"},
		"smtp_port":            {"587"},
		"smtp_username":        {"user"},
		"smtp_password":        {"pass"},
	}
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
	if sv.cfg.Alerts.StaleSensorMinutes != 30 {
		t.Errorf("stale sensor minutes: got %d", sv.cfg.Alerts.StaleSensorMinutes)
	}
}

func TestSaveMQTT(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{
		"broker":   {"mqtt.example.com"},
		"port":     {"8883"},
		"username": {"mqttuser"},
		"password": {"mqttpass"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/mqtt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	if sv.cfg.MQTT.Broker != "mqtt.example.com" {
		t.Errorf("broker: got %q", sv.cfg.MQTT.Broker)
	}
	if sv.cfg.MQTT.Port != 8883 {
		t.Errorf("port: got %d", sv.cfg.MQTT.Port)
	}
	if sv.cfg.MQTT.Username != "mqttuser" {
		t.Errorf("username: got %q", sv.cfg.MQTT.Username)
	}

	// Verify persisted to DB
	dbCfg, err := sv.store.GetMQTTConfig()
	if err != nil {
		t.Fatalf("GetMQTTConfig: %v", err)
	}
	if dbCfg.Broker != "mqtt.example.com" {
		t.Errorf("DB broker: got %q", dbCfg.Broker)
	}
}

func TestSaveHA(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{
		"url":   {"http://ha:8123"},
		"token": {"hatoken"},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/ha", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	if sv.cfg.HomeAssistant.URL != "http://ha:8123" {
		t.Errorf("url: got %q", sv.cfg.HomeAssistant.URL)
	}
	if sv.cfg.HomeAssistant.Token != "hatoken" {
		t.Errorf("token: got %q", sv.cfg.HomeAssistant.Token)
	}

	dbCfg, err := sv.store.GetHAConfig()
	if err != nil {
		t.Fatalf("GetHAConfig: %v", err)
	}
	if dbCfg.URL != "http://ha:8123" {
		t.Errorf("DB url: got %q", dbCfg.URL)
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

	verifyPage(t, sv, "/schedules")
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

	verifyPage(t, sv, "/")
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

func TestDashboardZonesUnsetBadge(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	z := sv.zoneManager.GetZone("Z1")
	if z == nil {
		t.Fatal("expected zone Z1 to exist")
	}

	t.Run("defaults show unset", func(t *testing.T) {
		verifyPage(t, sv, "/dashboard/zones")
	})

	z.Moisture = 45.0
	z.Humidity = 62.0
	z.Temperature = 22.5

	t.Run("values rendered", func(t *testing.T) {
		verifyPage(t, sv, "/dashboard/zones")
	})
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

	sv.store.SaveSensorReading("Z1", 45.0, 60.0, 0)

	verifyPage(t, sv, "/zones/Z1/history?hours=24")
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

// --- Auth tests ---

func TestLoginPage_NoUsers(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	verifyPage(t, sv, "/login")
}

func TestLoginPage_WithUsers(t *testing.T) {
	setupGin()
	sv := newTestServer(t)
	sv.store.CreateUser("testuser", "hash")

	verifyPage(t, sv, "/login")
}

func TestLogin_DefaultCreds(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{"username": {"admin"}, "password": {"bedwetter"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}
	if sessionCookie.Value == "" {
		t.Error("expected non-empty session cookie value")
	}

	username, err := sv.store.GetSessionByID(sessionCookie.Value)
	if err != nil {
		t.Errorf("expected session to exist in server: %v", err)
	}
	if username != "admin" {
		t.Errorf("expected username 'admin', got %q", username)
	}
}

func TestLogin_DefaultCreds_Wrong(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{"username": {"admin"}, "password": {"wrong"}}
	verifyPagePost(t, sv, "/login", form)

	sessionCount, _ := sv.store.CountSessions()
	if sessionCount != 0 {
		t.Errorf("expected 0 sessions, got %d", sessionCount)
	}
}

func TestLogin_RealCreds(t *testing.T) {
	setupGin()
	sv := newTestServer(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	sv.store.CreateUser("alice", string(hash))

	form := url.Values{"username": {"alice"}, "password": {"secret"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}

	username, err := sv.store.GetSessionByID(sessionCookie.Value)
	if err != nil {
		t.Errorf("expected session to exist in server: %v", err)
	}
	if username != "alice" {
		t.Errorf("expected username 'alice', got %q", username)
	}
}

func TestLogin_RealCreds_Wrong(t *testing.T) {
	setupGin()
	sv := newTestServer(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	sv.store.CreateUser("alice", string(hash))

	form := url.Values{"username": {"alice"}, "password": {"wrong"}}
	verifyPagePost(t, sv, "/login", form)
}

func TestLogin_RealCreds_UnknownUser(t *testing.T) {
	setupGin()
	sv := newTestServer(t)
	sv.store.CreateUser("alice", "hash")

	form := url.Values{"username": {"bob"}, "password": {"secret"}}
	verifyPagePost(t, sv, "/login", form)
}

func TestSetupPage_NoUsers(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	verifyPage(t, sv, "/setup")
}

func TestSetupPage_WithUsers(t *testing.T) {
	setupGin()
	sv := newTestServer(t)
	sv.store.CreateUser("existing", "hash")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/setup", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Result().Header.Get("Location")
	if loc != "/" {
		t.Errorf("expected redirect to /, got %q", loc)
	}
}

func TestSetupCreate(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{"username": {"newuser"}, "password": {"mypassword"}, "confirm_password": {"mypassword"}}
	verifyPagePost(t, sv, "/setup", form)

	user, err := sv.store.GetUserByUsername("newuser")
	if err != nil {
		t.Fatalf("expected user to exist in DB: %v", err)
	}
	if user.Username != "newuser" {
		t.Errorf("expected username 'newuser', got %q", user.Username)
	}
	if user.PasswordHash == "" {
		t.Error("expected password hash to be non-empty")
	}
}

func TestSetupCreate_PasswordMismatch(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{"username": {"newuser"}, "password": {"mypassword"}, "confirm_password": {"different"}}
	verifyPagePost(t, sv, "/setup", form)
}

func TestSetupCreate_ShortPassword(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{"username": {"newuser"}, "password": {"ab"}, "confirm_password": {"ab"}}
	verifyPagePost(t, sv, "/setup", form)
}

func TestSetupCreate_DuplicateUser(t *testing.T) {
	setupGin()
	sv := newTestServer(t)
	sv.store.CreateUser("existing", "hash")

	form := url.Values{"username": {"existing"}, "password": {"password123"}, "confirm_password": {"password123"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect (setup not available), got %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	sessionID := sv.createSession("testuser")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Result().Header.Get("Location")
	if loc != "/login" {
		t.Errorf("expected redirect to /login, got %q", loc)
	}

	_, err := sv.store.GetSessionByID(sessionID)
	if err == nil {
		t.Error("expected session to be removed after logout")
	}

	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "session" {
			if c.MaxAge >= 0 {
				t.Error("expected session cookie to be cleared (MaxAge < 0)")
			}
		}
	}
}

func TestLogout_NoSession(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/logout", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
}

func TestAuthMiddleware_RedirectToSetup(t *testing.T) {
	prevMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	defer gin.SetMode(prevMode)

	sv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dashboard", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Result().Header.Get("Location")
	if loc != "/setup" {
		t.Errorf("expected redirect to /setup, got %q", loc)
	}
}

func TestAuthMiddleware_RedirectToLogin(t *testing.T) {
	prevMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	defer gin.SetMode(prevMode)

	sv := newTestServer(t)
	sv.store.CreateUser("testuser", "hash")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dashboard", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Result().Header.Get("Location")
	if loc != "/login" {
		t.Errorf("expected redirect to /login, got %q", loc)
	}
}

func TestAuthMiddleware_AllowsLoginAndSetup(t *testing.T) {
	prevMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	defer gin.SetMode(prevMode)

	sv := newTestServer(t)

	// Login page should be accessible without auth
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/login", nil)
	sv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /login, got %d", w.Code)
	}

	// Setup page should be accessible without auth when no users exist
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/setup", nil)
	sv.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /setup, got %d", w.Code)
	}
}

func TestAuthMiddleware_StaticBypassesAuth(t *testing.T) {
	prevMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	defer gin.SetMode(prevMode)

	sv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/static/tailwind.css", nil)
	sv.router.ServeHTTP(w, req)

	// Should get a valid response (either the file or 404), not a redirect
	if w.Code == http.StatusFound || w.Code == http.StatusTemporaryRedirect {
		t.Errorf("expected non-redirect for static file, got %d", w.Code)
	}
}

func TestAuthMiddleware_AuthenticatedSessionProceeds(t *testing.T) {
	prevMode := gin.Mode()
	gin.SetMode(gin.DebugMode)
	defer gin.SetMode(prevMode)

	sv := newTestServer(t)
	sessionID := sv.createSession("testuser")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for authenticated request, got %d", w.Code)
	}

	approvals.VerifyString(t, w.Body.String(), standardScrubbers())
}

func TestOpenAllValves(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	sv.zoneManager.AddZone(config.ZoneConfig{Name: "Z2", ThresholdLow: 30, ThresholdHigh: 60})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/zones/all/open", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	for _, name := range []string{"Z1", "Z2"} {
		z := sv.zoneManager.GetZone(name)
		if z == nil {
			t.Fatalf("expected zone %q to exist", name)
		}
		if z.State != zones.StateManualOpen {
			t.Errorf("zone %q: expected StateManualOpen, got %v", name, z.State)
		}
	}
}

func TestCloseAllValves(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	sv.zoneManager.AddZone(config.ZoneConfig{Name: "Z2", ThresholdLow: 30, ThresholdHigh: 60})

	sv.zoneManager.OpenValve("Z1")
	sv.zoneManager.OpenValve("Z2")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/zones/all/close", nil)
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	for _, name := range []string{"Z1", "Z2"} {
		z := sv.zoneManager.GetZone(name)
		if z == nil {
			t.Fatalf("expected zone %q to exist", name)
		}
		if z.State != zones.StateIdle {
			t.Errorf("zone %q: expected StateIdle, got %v", name, z.State)
		}
	}
}

func TestConfigPageContainsWeatherSection(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	verifyPage(t, sv, "/config")
}

func TestSaveWeatherConfigMQTT(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{}
	form.Set("lat", "51.5")
	form.Set("lon", "-0.12")
	form.Set("rain_threshold_mm", "3.5")
	form.Set("rain_source", "mqtt")
	form.Set("rain_sensor_topic", "sensor/rain")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/weather", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	if sv.cfg.Weather.Lat != 51.5 {
		t.Errorf("expected Lat 51.5, got %f", sv.cfg.Weather.Lat)
	}
	if sv.cfg.Weather.Lon != -0.12 {
		t.Errorf("expected Lon -0.12, got %f", sv.cfg.Weather.Lon)
	}
	if sv.cfg.Weather.RainThresholdMm != 3.5 {
		t.Errorf("expected RainThresholdMm 3.5, got %f", sv.cfg.Weather.RainThresholdMm)
	}
	if sv.cfg.Weather.RainSensorTopic != "sensor/rain" {
		t.Errorf("expected RainSensorTopic sensor/rain, got %q", sv.cfg.Weather.RainSensorTopic)
	}
	if sv.cfg.Weather.RainSensorEntity != "" {
		t.Errorf("expected empty RainSensorEntity, got %q", sv.cfg.Weather.RainSensorEntity)
	}
}

func TestSaveWeatherConfigHA(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{}
	form.Set("lat", "52.0")
	form.Set("lon", "13.0")
	form.Set("rain_threshold_mm", "2.0")
	form.Set("rain_source", "ha")
	form.Set("rain_sensor_entity", "binary_sensor.rain")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/weather", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	if sv.cfg.Weather.RainSensorEntity != "binary_sensor.rain" {
		t.Errorf("expected RainSensorEntity binary_sensor.rain, got %q", sv.cfg.Weather.RainSensorEntity)
	}
	if sv.cfg.Weather.RainSensorTopic != "" {
		t.Errorf("expected empty RainSensorTopic, got %q", sv.cfg.Weather.RainSensorTopic)
	}
}

func TestSaveZoneWithNewFields(t *testing.T) {
	setupGin()
	sv := newTestServer(t)

	form := url.Values{}
	form.Set("name", "Z2")
	form.Set("moisture_source", "mqtt")
	form.Set("moisture_sensor_topic", "sensor/moisture")
	form.Set("valve_source", "mqtt")
	form.Set("valve_command_topic", "valve/cmd")
	form.Set("threshold_low", "30")
	form.Set("threshold_high", "70")
	form.Set("max_watering_seconds", "300")
	form.Set("max_activations_per_day", "5")
	form.Set("cooldown_minutes", "90")
	form.Set("earliest_watering_time", "07:00")
	form.Set("latest_watering_time", "11:00")
	form.Set("seasonal_multiplier_1", "0.5")
	form.Set("seasonal_multiplier_7", "1.5")

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/config/zones", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sv.router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}

	dbZones, _ := sv.store.GetAllZoneConfigs()
	var z2 *models.ZoneConfig
	for i, z := range dbZones {
		if z.Name == "Z2" {
			z2 = &dbZones[i]
			break
		}
	}
	if z2 == nil {
		t.Fatal("expected zone Z2 to exist")
	}
	if z2.EarliestWateringTime != "07:00" {
		t.Errorf("expected EarliestWateringTime 07:00, got %q", z2.EarliestWateringTime)
	}
	if z2.LatestWateringTime != "11:00" {
		t.Errorf("expected LatestWateringTime 11:00, got %q", z2.LatestWateringTime)
	}
	if z2.SeasonalMultipliers == "" {
		t.Error("expected SeasonalMultipliers to be set")
	}
}


