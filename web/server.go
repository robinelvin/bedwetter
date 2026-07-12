package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robinelvin/bedwetter/alerts"
	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/ha"
	"github.com/robinelvin/bedwetter/models"
	"github.com/robinelvin/bedwetter/mqtt"
	"github.com/robinelvin/bedwetter/scheduler"
	"github.com/robinelvin/bedwetter/store"
	"github.com/robinelvin/bedwetter/zones"
	"golang.org/x/crypto/bcrypt"
)

type Server struct {
	cfg         *config.Config
	store       *store.Store
	zoneManager *zones.Manager
	alertMgr    *alerts.AlertManager
	mqttClient  mqtt.ClientInterface
	haAPI       *ha.APIClient
	scheduler   *scheduler.Scheduler
	router      *gin.Engine
	templates   map[string]*template.Template
}

type zoneView struct {
	zones.ZoneSnapshot
	NextWatering       time.Time
	NextWateringReason string
	StatusNote         string
	StatusNoteVariant  string
	StatusBadgeClass   string
}

func New(cfg *config.Config, s *store.Store, zm *zones.Manager, am *alerts.AlertManager, mqttClient mqtt.ClientInterface, haAPI *ha.APIClient, sched *scheduler.Scheduler) *Server {
	r := gin.Default()

	sv := &Server{
		cfg:         cfg,
		store:       s,
		zoneManager: zm,
		alertMgr:    am,
		mqttClient:  mqttClient,
		haAPI:       haAPI,
		scheduler:   sched,
		router:      r,
		templates:   make(map[string]*template.Template),
	}

	funcMap := template.FuncMap{
		"isUnset": func(f float64) bool { return math.IsNaN(f) },
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "never"
			}
			return t.Format("15:04:05")
		},
		"formatDateTime": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("2006-01-02 15:04:05")
		},
		"floatVal": func(f float64) float64 { return f },
		"add":      func(a, b int) int { return a + b },
		"sub":      func(a, b int) int { return a - b },
		"weatherDesc": func(code int) string {
			switch {
			case code == 0:
				return "Clear"
			case code == 1:
				return "Mostly clear"
			case code == 2:
				return "Partly cloudy"
			case code == 3:
				return "Overcast"
			case code == 45 || code == 48:
				return "Fog"
			case code >= 51 && code <= 55:
				return "Drizzle"
			case code >= 56 && code <= 57:
				return "Freezing drizzle"
			case code >= 61 && code <= 65:
				return "Rain"
			case code >= 66 && code <= 67:
				return "Freezing rain"
			case code >= 71 && code <= 77:
				return "Snow"
			case code >= 80 && code <= 82:
				return "Showers"
			case code >= 85 && code <= 86:
				return "Snow showers"
			case code >= 95 && code <= 99:
				return "Thunderstorm"
			default:
				return "Unknown"
			}
		},
		"formatHour": func(s string) string {
			if len(s) < 16 {
				return s
			}
			return s[11:16]
		},
		"substr": func(s string, start, length int) string {
			if start < 0 {
				start = 0
			}
			if start > len(s) {
				return ""
			}
			end := start + length
			if end > len(s) {
				end = len(s)
			}
			return s[start:end]
		},
		"weatherIcon": func(code int) string {
			base := "/static/icons/"
			switch {
			case code == 0:
				return base + "clear-day.svg"
			case code == 1:
				return base + "mostly-clear-day.svg"
			case code == 2:
				return base + "partly-cloudy-day.svg"
			case code == 3:
				return base + "overcast-day.svg"
			case code >= 45 && code <= 48:
				return base + "fog-day.svg"
			case code >= 51 && code <= 57:
				return base + "overcast-drizzle.svg"
			case code >= 61 && code <= 67:
				return base + "overcast-rain.svg"
			case code >= 71 && code <= 77:
				return base + "overcast-snow.svg"
			case code >= 80 && code <= 82:
				return base + "overcast-rain.svg"
			case code >= 85 && code <= 86:
				return base + "overcast-snow.svg"
			case code == 95:
				return base + "thunderstorms.svg"
			case code == 96 || code == 99:
				return base + "thunderstorms-hail.svg"
			default:
				return base + "not-available.svg"
			}
		},
	}

	sv.templates["dashboard"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS,
			"templates/base.html", "templates/dashboard.html", "templates/_zone_cards.html", "templates/_zone_card.html", "templates/_weather.html"),
	)
	sv.templates["schedules"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/base.html", "templates/schedules.html"),
	)
	sv.templates["config"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/base.html", "templates/config.html"),
	)
	sv.templates["events"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/base.html", "templates/events.html"),
	)
	sv.templates["zone_detail"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/base.html", "templates/zone_detail.html", "templates/_zone_card.html", "templates/_zone_card_fragment.html", "templates/_schedule_timeline.html"),
	)

	sv.templates["_zone_cards"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_zone_cards.html", "templates/_zone_card.html"),
	)
	sv.templates["_zone_card_fragment"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_zone_card_fragment.html", "templates/_zone_card.html"),
	)
	sv.templates["_weather"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_weather.html"),
	)

	sv.templates["_moisture_mqtt"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_moisture_mqtt.html"),
	)
	sv.templates["_moisture_ha"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_moisture_ha.html"),
	)
	sv.templates["_valve_mqtt"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_valve_mqtt.html"),
	)
	sv.templates["_valve_ha"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_valve_ha.html"),
	)

	sv.templates["_humidity_mqtt"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_humidity_mqtt.html"),
	)
	sv.templates["_humidity_ha"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_humidity_ha.html"),
	)
	sv.templates["_temperature_mqtt"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_temperature_mqtt.html"),
	)
	sv.templates["_temperature_ha"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_temperature_ha.html"),
	)

	sv.templates["_rain_mqtt"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_rain_mqtt.html"),
	)
	sv.templates["_rain_ha"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_rain_ha.html"),
	)

	sv.templates["_master_mqtt"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_master_mqtt.html"),
	)
	sv.templates["_master_ha"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_master_ha.html"),
	)

	sv.templates["_schedule_timeline"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_schedule_timeline.html"),
	)

	sv.templates["login"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/base.html", "templates/login.html"),
	)
	sv.templates["setup"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/base.html", "templates/setup.html"),
	)

	sv.setupRoutes()
	return sv
}

func (s *Server) render(c *gin.Context, page string, code int, data gin.H) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(code)
	if data == nil {
		data = gin.H{}
	}
	if _, ok := data["page"]; !ok {
		data["page"] = page
	}
	if _, ok := data["authenticated"]; !ok {
		data["authenticated"] = s.isAuthenticated(c)
	}
	if err := s.templates[page].ExecuteTemplate(c.Writer, "base", data); err != nil {
		log.Printf("Template render error: %v", err)
	}
}

func (s *Server) renderPartial(c *gin.Context, name string, code int, data gin.H) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(code)
	if data == nil {
		data = gin.H{}
	}
	if _, ok := data["authenticated"]; !ok {
		data["authenticated"] = s.isAuthenticated(c)
	}
	if err := s.templates[name].ExecuteTemplate(c.Writer, name, data); err != nil {
		log.Printf("Template render error: %v", err)
	}
}

func (s *Server) isAuthenticated(c *gin.Context) bool {
	cookie, err := c.Cookie("session")
	if err != nil {
		return false
	}
	_, err = s.store.GetSessionByID(cookie)
	return err == nil
}

func (s *Server) generateSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) createSession(username string) string {
	id := s.generateSessionID()
	if err := s.store.CreateSession(id, username); err != nil {
		log.Printf("Failed to create session: %v", err)
	}
	return id
}

func (s *Server) authRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if gin.Mode() == gin.TestMode {
			c.Set("username", "test")
			c.Next()
			return
		}

		path := c.Request.URL.Path
		if len(path) >= 8 && path[:8] == "/static/" {
			c.Next()
			return
		}

		cookie, err := c.Cookie("session")
		if err == nil {
			username, err := s.store.GetSessionByID(cookie)
			if err == nil {
				c.Set("username", username)
				c.Next()
				return
			}
		}

		count, err := s.store.CountUsers()
		if err != nil || count == 0 {
			if path == "/setup" || path == "/login" {
				c.Next()
				return
			}
			if c.GetHeader("HX-Request") == "true" {
				c.Header("HX-Redirect", "/setup")
				c.AbortWithStatus(http.StatusOK)
			} else {
				c.Redirect(http.StatusFound, "/setup")
				c.Abort()
			}
			return
		}

		if path == "/login" || path == "/setup" {
			c.Next()
			return
		}
		if c.GetHeader("HX-Request") == "true" {
			c.Header("HX-Redirect", "/login")
			c.AbortWithStatus(http.StatusOK)
		} else {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
		}
	}
}

func (s *Server) setupRoutes() {
	s.router.Use(s.authRequired())

	s.router.Static("/static", "./web/static")
	s.router.GET("/", s.dashboard)
	s.router.GET("/dashboard", s.dashboard)
	s.router.GET("/dashboard/zones", s.dashboardZones)
	s.router.GET("/dashboard/weather", s.dashboardWeather)
	s.router.POST("/zones/:name/open", s.openValve)
	s.router.POST("/zones/:name/close", s.closeValve)
	s.router.POST("/zones/:name/force-close", s.forceCloseZone)
	s.router.POST("/zones/:name/clear-force-close", s.clearForceCloseZone)
	s.router.POST("/zones/:name/acknowledge", s.acknowledgeFault)
	s.router.POST("/zones/all/open", s.openAllValves)
	s.router.POST("/zones/all/close", s.closeAllValves)
	s.router.GET("/zones/:id", s.zoneDetail)
	s.router.GET("/zones/:id/card", s.zoneCard)
	s.router.GET("/zones/:id/history", s.zoneHistory)
	s.router.GET("/schedules", s.schedulesPage)
	s.router.POST("/schedules", s.saveSchedule)
	s.router.POST("/schedules/:id/delete", s.deleteSchedule)
	s.router.GET("/config", s.configPage)
	s.router.POST("/config/alerts", s.saveAlerts)
	s.router.POST("/config/ntfy", s.saveNtfy)
	s.router.POST("/config/mqtt", s.saveMQTT)
	s.router.POST("/config/ha", s.saveHA)
	s.router.POST("/config/weather", s.saveWeather)
	s.router.POST("/config/master-valve", s.saveMasterValve)
	s.router.GET("/config/master-valve/fields", s.masterValveFields)
	s.router.POST("/config/zones", s.saveZone)
	s.router.POST("/config/zones/:id/delete", s.deleteZone)
	s.router.GET("/events", s.eventsPage)
	s.router.GET("/api/zones", s.apiZones)
	s.router.POST("/api/zones/:name/water", s.apiWaterZone)
	s.router.POST("/api/zones/:name/stop", s.apiStopZone)
	s.router.POST("/api/zones/:name/acknowledge", s.apiAcknowledgeFault)
	s.router.GET("/login", s.loginPage)
	s.router.POST("/login", s.login)
	s.router.POST("/logout", s.logout)
	s.router.GET("/setup", s.setupPage)
	s.router.POST("/setup", s.setupCreate)
	s.router.GET("/config/zones/fields/moisture", s.zoneMoistureFields)
	s.router.GET("/config/zones/fields/valve", s.zoneValveFields)
	s.router.GET("/config/zones/fields/humidity", s.zoneHumidityFields)
	s.router.GET("/config/zones/fields/temperature", s.zoneTemperatureFields)
	s.router.GET("/config/weather/fields/rain", s.rainSensorFields)
}

func (s *Server) zoneViews() []zoneView {
	snapshots := s.zoneManager.GetAllZoneSnapshots()

	scheduleMap := make(map[string][]models.ScheduleConfig)
	if schedules, err := s.store.GetAllSchedules(); err == nil {
		for _, sc := range schedules {
			scheduleMap[sc.ZoneName] = append(scheduleMap[sc.ZoneName], sc)
		}
	} else {
		log.Printf("Failed to load schedules for zone view: %v", err)
	}

	now := time.Now()
	rainActive := s.zoneManager.RainDetected()
	results := make([]zoneView, len(snapshots))
	for i, snap := range snapshots {
		nextTime, reason := nextWateringForZone(now, snap, scheduleMap[snap.Config.Name])
		activations := int64(0)
		if count, err := s.store.ActivationsToday(snap.Config.Name); err == nil {
			activations = count
		} else {
			log.Printf("Failed to fetch activations for %s: %v", snap.Config.Name, err)
		}
		status, statusVariant := s.statusNote(now, snap, rainActive, activations)
		badgeClass := ""
		if status != "" {
			badgeClass = badgeClassForVariant(statusVariant)
		}
		results[i] = zoneView{
			ZoneSnapshot:       snap,
			NextWatering:       nextTime,
			NextWateringReason: reason,
			StatusNote:         status,
			StatusNoteVariant:  statusVariant,
			StatusBadgeClass:   badgeClass,
		}
	}
	return results
}

func (s *Server) loginPage(c *gin.Context) {
	count, _ := s.store.CountUsers()
	s.render(c, "login", http.StatusOK, gin.H{
		"title":     "Sign In",
		"showSetup": count == 0,
	})
}

func (s *Server) login(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	count, err := s.store.CountUsers()
	if err != nil {
		s.render(c, "login", http.StatusOK, gin.H{"title": "Sign In", "error": "Internal error"})
		return
	}

	if count == 0 {
		if username == "admin" && password == "bedwetter" {
			sessionID := s.createSession(username)
			c.SetCookie("session", sessionID, 86400, "/", "", false, true)
			c.Redirect(http.StatusFound, "/setup")
			return
		}
		s.render(c, "login", http.StatusOK, gin.H{
			"title":     "Sign In",
			"error":     "Invalid credentials",
			"showSetup": true,
		})
		return
	}

	user, err := s.store.GetUserByUsername(username)
	if err != nil {
		s.render(c, "login", http.StatusOK, gin.H{
			"title": "Sign In",
			"error": "Invalid username or password",
		})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		s.render(c, "login", http.StatusOK, gin.H{
			"title": "Sign In",
			"error": "Invalid username or password",
		})
		return
	}

	sessionID := s.createSession(username)
	c.SetCookie("session", sessionID, 86400, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}

func (s *Server) logout(c *gin.Context) {
	cookie, err := c.Cookie("session")
	if err == nil {
		s.store.DeleteSession(cookie)
	}
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

func (s *Server) setupPage(c *gin.Context) {
	count, _ := s.store.CountUsers()
	if count > 0 {
		c.Redirect(http.StatusFound, "/")
		return
	}
	s.render(c, "setup", http.StatusOK, gin.H{"title": "First-Time Setup"})
}

func (s *Server) setupCreate(c *gin.Context) {
	count, _ := s.store.CountUsers()
	if count > 0 {
		c.Redirect(http.StatusFound, "/")
		return
	}

	username := c.PostForm("username")
	password := c.PostForm("password")
	confirm := c.PostForm("confirm_password")

	if username == "" || password == "" {
		s.render(c, "setup", http.StatusOK, gin.H{"title": "First-Time Setup", "error": "All fields are required"})
		return
	}
	if password != confirm {
		s.render(c, "setup", http.StatusOK, gin.H{"title": "First-Time Setup", "error": "Passwords do not match"})
		return
	}
	if len(password) < 6 {
		s.render(c, "setup", http.StatusOK, gin.H{"title": "First-Time Setup", "error": "Password must be at least 6 characters"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.render(c, "setup", http.StatusOK, gin.H{"title": "First-Time Setup", "error": "Failed to create user"})
		return
	}

	if err := s.store.CreateUser(username, string(hash)); err != nil {
		s.render(c, "setup", http.StatusOK, gin.H{"title": "First-Time Setup", "error": "Username already exists"})
		return
	}

	s.logEvent("info", "auth", "First admin user created: "+username, "")
	s.render(c, "login", http.StatusOK, gin.H{
		"title": "Sign In",
		"info":  "Account created successfully. Please sign in.",
	})
}

func (s *Server) dashboard(c *gin.Context) {
	zoneStates := s.zoneViews()
	weather := s.scheduler.GetWeather()
	s.render(c, "dashboard", http.StatusOK, gin.H{
		"title":         "Dashboard",
		"zones":         zoneStates,
		"weather":       weather,
		"upcomingHours": weather.UpcomingHours(8),
	})
}

func (s *Server) dashboardZones(c *gin.Context) {
	zoneStates := s.zoneViews()
	s.renderPartial(c, "_zone_cards", http.StatusOK, gin.H{
		"zones": zoneStates,
	})
}

func (s *Server) dashboardWeather(c *gin.Context) {
	weather := s.scheduler.GetWeather()
	s.renderPartial(c, "_weather", http.StatusOK, gin.H{
		"weather":       weather,
		"upcomingHours": weather.UpcomingHours(8),
	})
}

func (s *Server) openValve(c *gin.Context) {
	name := c.Param("name")
	s.zoneManager.OpenValve(name)
	c.Redirect(http.StatusFound, "/dashboard")
}

func (s *Server) closeValve(c *gin.Context) {
	name := c.Param("name")
	s.zoneManager.CloseValve(name)
	c.Redirect(http.StatusFound, "/dashboard")
}

func (s *Server) openAllValves(c *gin.Context) {
	s.zoneManager.OpenAllValves()
	c.Redirect(http.StatusFound, "/dashboard")
}

func (s *Server) closeAllValves(c *gin.Context) {
	s.zoneManager.CloseAllValves()
	c.Redirect(http.StatusFound, "/dashboard")
}

func (s *Server) forceCloseZone(c *gin.Context) {
	name := c.Param("name")
	s.zoneManager.ForceClose(name)
	c.Redirect(http.StatusFound, "/dashboard")
}

func (s *Server) clearForceCloseZone(c *gin.Context) {
	name := c.Param("name")
	s.zoneManager.ClearForceClose(name)
	c.Redirect(http.StatusFound, "/dashboard")
}

func (s *Server) acknowledgeFault(c *gin.Context) {
	name := c.Param("name")
	s.zoneManager.AcknowledgeFault(name)
	c.Redirect(http.StatusFound, "/dashboard")
}

func (s *Server) apiWaterZone(c *gin.Context) {
	name := c.Param("name")
	z := s.zoneManager.GetZone(name)
	if z == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "zone not found"})
		return
	}
	s.zoneManager.OpenValve(name)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": name, "state": "manual_open"})
}

func (s *Server) apiStopZone(c *gin.Context) {
	name := c.Param("name")
	z := s.zoneManager.GetZone(name)
	if z == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "zone not found"})
		return
	}
	s.zoneManager.CloseValve(name)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": name, "state": "idle"})
}

func (s *Server) apiAcknowledgeFault(c *gin.Context) {
	name := c.Param("name")
	z := s.zoneManager.GetZone(name)
	if z == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "zone not found"})
		return
	}
	s.zoneManager.AcknowledgeFault(name)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "zone": name})
}

func (s *Server) zoneDetail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		s.render(c, "dashboard", http.StatusNotFound, gin.H{
			"title": "Zone Not Found",
			"error": "Invalid zone ID",
		})
		return
	}

	zc, err := s.store.GetZoneConfigByID(uint(id))
	if err != nil {
		s.render(c, "dashboard", http.StatusNotFound, gin.H{
			"title": "Zone Not Found",
			"error": fmt.Sprintf("Zone with ID %d not found", id),
		})
		return
	}

	zone := s.zoneManager.GetZone(zc.Name)
	if zone == nil {
		s.render(c, "dashboard", http.StatusNotFound, gin.H{
			"title": "Zone Not Found",
			"error": fmt.Sprintf("Zone '%s' not found", zc.Name),
		})
		return
	}

	snap := zone.Snapshot()

	schedules, _ := s.store.GetSchedule(zc.Name)
	now := time.Now()
	rainActive := s.zoneManager.RainDetected()
	nextTime, reason := nextWateringForZone(now, snap, schedules)

	activations := int64(0)
	if count, err := s.store.ActivationsToday(zc.Name); err == nil {
		activations = count
	}

	status, statusVariant := s.statusNote(now, snap, rainActive, activations)
	badgeClass := ""
	if status != "" {
		badgeClass = badgeClassForVariant(statusVariant)
	}

	zv := zoneView{
		ZoneSnapshot:       snap,
		NextWatering:       nextTime,
		NextWateringReason: reason,
		StatusNote:         status,
		StatusNoteVariant:  statusVariant,
		StatusBadgeClass:   badgeClass,
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	events, _ := s.store.GetEventLogsByZone(zc.Name, page, 50)

	weekSchedule := buildWeekSchedule(time.Now(), schedules)

	s.render(c, "zone_detail", http.StatusOK, gin.H{
		"title":         zc.Name,
		"zone":          zv,
		"events":        events,
		"weekSchedule":  weekSchedule,
	})
}

func (s *Server) zoneCard(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	zc, err := s.store.GetZoneConfigByID(uint(id))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	zone := s.zoneManager.GetZone(zc.Name)
	if zone == nil {
		c.Status(http.StatusNotFound)
		return
	}

	snap := zone.Snapshot()

	schedules, _ := s.store.GetSchedule(zc.Name)
	now := time.Now()
	rainActive := s.zoneManager.RainDetected()
	nextTime, reason := nextWateringForZone(now, snap, schedules)

	activations := int64(0)
	if count, err := s.store.ActivationsToday(zc.Name); err == nil {
		activations = count
	}

	status, statusVariant := s.statusNote(now, snap, rainActive, activations)
	badgeClass := ""
	if status != "" {
		badgeClass = badgeClassForVariant(statusVariant)
	}

	zv := zoneView{
		ZoneSnapshot:       snap,
		NextWatering:       nextTime,
		NextWateringReason: reason,
		StatusNote:         status,
		StatusNoteVariant:  statusVariant,
		StatusBadgeClass:   badgeClass,
	}

	s.renderPartial(c, "_zone_card_fragment", http.StatusOK, gin.H{
		"zone": zv,
	})
}

func (s *Server) zoneHistory(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		s.render(c, "dashboard", http.StatusNotFound, gin.H{
			"title": "Zone Not Found",
			"error": "Invalid zone ID",
		})
		return
	}

	zc, err := s.store.GetZoneConfigByID(uint(id))
	if err != nil {
		s.render(c, "dashboard", http.StatusNotFound, gin.H{
			"title": "Zone Not Found",
			"error": fmt.Sprintf("Zone with ID %d not found", id),
		})
		return
	}
	name := zc.Name

	hoursStr := c.DefaultQuery("hours", "24")
	hours, err := strconv.Atoi(hoursStr)
	if err != nil || hours < 1 {
		hours = 24
	}

	readings, err := s.store.RecentReadings(name, hours)
	if err != nil {
		s.render(c, "dashboard", http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	events, _ := s.store.RecentValveEvents(name, 20)

	s.render(c, "dashboard", http.StatusOK, gin.H{
		"title":    fmt.Sprintf("History: %s", name),
		"readings": readings,
		"events":   events,
		"zoneName": name,
		"hours":    hours,
	})
}

func (s *Server) schedulesPage(c *gin.Context) {
	schedules, err := s.store.GetAllSchedules()
	if err != nil {
		s.render(c, "dashboard", http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	dbZones, _ := s.store.GetAllZoneConfigs()
	zoneNames := make([]config.ZoneConfig, len(dbZones))
	for i, z := range dbZones {
		zoneNames[i] = z.ToConfigZoneConfig()
	}

	var editEntry *models.ScheduleConfig
	editIDStr := c.Query("edit")
	if editIDStr != "" {
		if id, err := strconv.ParseUint(editIDStr, 10, 64); err == nil {
			editEntry, _ = s.store.GetScheduleByID(uint(id))
		}
	}

	s.render(c, "schedules", http.StatusOK, gin.H{
		"title":     "Watering Schedules",
		"schedules": schedules,
		"zones":     zoneNames,
		"days":      []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
		"months":    []string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		"editEntry": editEntry,
	})
}

func (s *Server) saveSchedule(c *gin.Context) {
	idStr := c.PostForm("id")
	zoneName := c.PostForm("zone_name")
	dayOfWeek := c.PostForm("day_of_week")
	time := c.PostForm("time")
	durationStr := c.PostForm("duration")
	duration, _ := strconv.Atoi(durationStr)
	if duration <= 0 {
		duration = 300
	}

	monthStr := c.PostForm("month")
	month := 0
	if monthStr != "" {
		monthLabels := map[string]int{"Jan": 1, "Feb": 2, "Mar": 3, "Apr": 4, "May": 5, "Jun": 6, "Jul": 7, "Aug": 8, "Sep": 9, "Oct": 10, "Nov": 11, "Dec": 12}
		month = monthLabels[monthStr]
	}

	if idStr != "" {
		id, _ := strconv.ParseUint(idStr, 10, 64)
		entry, err := s.store.GetScheduleByID(uint(id))
		if err != nil {
			c.Redirect(http.StatusFound, "/schedules")
			return
		}
		entry.ZoneName = zoneName
		entry.DayOfWeek = dayOfWeek
		entry.Time = time
		entry.Duration = duration
		entry.Month = month
		if err := s.store.UpdateScheduleEntry(entry); err != nil {
			log.Printf("Failed to update schedule: %v", err)
		}
	} else {
		entry := &models.ScheduleConfig{
			ZoneName:  zoneName,
			DayOfWeek: dayOfWeek,
			Time:      time,
			Duration:  duration,
			Month:     month,
		}
		if err := s.store.CreateScheduleEntry(entry); err != nil {
			log.Printf("Failed to save schedule: %v", err)
		}
	}
	c.Redirect(http.StatusFound, "/schedules")
}

func (s *Server) deleteSchedule(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 64)
	s.store.DeleteScheduleByID(uint(id))
	c.Redirect(http.StatusFound, "/schedules")
}

func (s *Server) configPage(c *gin.Context) {
	dbZones, _ := s.store.GetAllZoneConfigs()

	mqttCfg, err := s.store.GetMQTTConfig()
	if err != nil {
		mqttCfg = &models.MQTTConfig{Broker: s.cfg.MQTT.Broker, Port: s.cfg.MQTT.Port, Username: s.cfg.MQTT.Username, Password: s.cfg.MQTT.Password}
	}
	haCfg, err := s.store.GetHAConfig()
	if err != nil {
		haCfg = &models.HAConfig{URL: s.cfg.HomeAssistant.URL, Token: s.cfg.HomeAssistant.Token}
	}
	alertCfg, err := s.store.GetAlertSettings()
	if err != nil {
		alertCfg = &models.AlertSettings{
			Email: s.cfg.Alerts.Email, StaleSensorMinutes: s.cfg.Alerts.StaleSensorMinutes,
			SMTPServer: s.cfg.Alerts.SMTPServer, SMTPPort: s.cfg.Alerts.SMTPPort,
			SMTPUsername: s.cfg.Alerts.SMTPUsername, SMTPPassword: s.cfg.Alerts.SMTPPassword,
			FromEmail: s.cfg.Alerts.FromEmail,
		}
	}

	ntfyCfg, err := s.store.GetNtfyConfig()
	if err != nil {
		ntfyCfg = &models.NtfyConfig{
			Enabled:    s.cfg.Ntfy.Enabled,
			Server:     s.cfg.Ntfy.Server,
			UUID:       s.cfg.Ntfy.UUID,
			Token:      s.cfg.Ntfy.Token,
			AlertInfo:  s.cfg.Ntfy.AlertInfo,
			AlertWarn:  s.cfg.Ntfy.AlertWarn,
			AlertAlarm: s.cfg.Ntfy.AlertAlarm,
		}
	}

	editIDStr := c.Query("edit")
	var editZone *models.ZoneConfig
	if editIDStr != "" {
		var id uint
		if parsed, err := strconv.ParseUint(editIDStr, 10, 64); err == nil {
			id = uint(parsed)
			for _, z := range dbZones {
				if z.ID == id {
					editZone = &z
					break
				}
			}
		}
	}

	moistureType := "mqtt"
	valveType := "mqtt"
	humidityType := "mqtt"
	temperatureType := "mqtt"
	if editZone != nil {
		if editZone.MoistureSensorEntity != "" {
			moistureType = "ha"
		}
		if editZone.ValveSwitchEntity != "" {
			valveType = "ha"
		}
		if editZone.HumiditySensorEntity != "" {
			humidityType = "ha"
		}
		if editZone.TemperatureSensorEntity != "" {
			temperatureType = "ha"
		}
	}

	monthNames := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	parsedMults := make(map[string]float64)
	if editZone != nil && editZone.SeasonalMultipliers != "" {
		json.Unmarshal([]byte(editZone.SeasonalMultipliers), &parsedMults)
	}
	type monthEntry struct {
		Num   int
		Name  string
		Value float64
	}
	var months []monthEntry
	for i := 1; i <= 12; i++ {
		key := fmt.Sprintf("%d", i)
		val := 1.0
		if v, ok := parsedMults[key]; ok {
			val = v
		}
		months = append(months, monthEntry{Num: i, Name: monthNames[i-1], Value: val})
	}

	s.render(c, "config", http.StatusOK, gin.H{
		"title":           "Configuration",
		"cfg":             s.cfg,
		"mqtt":            mqttCfg,
		"ha":              haCfg,
		"alerts":          alertCfg,
		"ntfy":            ntfyCfg,
		"dbZones":         dbZones,
		"editZone":        editZone,
		"moistureType":    moistureType,
		"valveType":       valveType,
		"humidityType":    humidityType,
		"temperatureType": temperatureType,
		"weather":         s.cfg.Weather,
		"months":          months,
		"masterValve":     s.cfg.MasterValve,
	})
}

func (s *Server) saveMQTT(c *gin.Context) {
	cfg := &models.MQTTConfig{
		ID:       1,
		Broker:   c.PostForm("broker"),
		Port:     1883,
		Username: c.PostForm("username"),
		Password: c.PostForm("password"),
	}
	if p, err := strconv.Atoi(c.PostForm("port")); err == nil && p > 0 {
		cfg.Port = p
	}
	if err := s.store.SaveMQTTConfig(cfg); err != nil {
		log.Printf("Failed to save MQTT config: %v", err)
	}
	s.cfg.MQTT.Broker = cfg.Broker
	s.cfg.MQTT.Port = cfg.Port
	s.cfg.MQTT.Username = cfg.Username
	s.cfg.MQTT.Password = cfg.Password
	s.logEvent("info", "config", "MQTT config updated", "")
	c.Redirect(http.StatusFound, "/config")
}

func (s *Server) saveHA(c *gin.Context) {
	cfg := &models.HAConfig{
		ID:    1,
		URL:   c.PostForm("url"),
		Token: c.PostForm("token"),
	}
	if err := s.store.SaveHAConfig(cfg); err != nil {
		log.Printf("Failed to save HA config: %v", err)
	}
	s.cfg.HomeAssistant.URL = cfg.URL
	s.cfg.HomeAssistant.Token = cfg.Token
	if s.haAPI != nil {
		s.haAPI.UpdateConfig(cfg.URL, cfg.Token)
	}
	s.logEvent("info", "config", "HA config updated", "")
	c.Redirect(http.StatusFound, "/config")
}

func (s *Server) rainSensorFields(c *gin.Context) {
	sourceType := c.DefaultQuery("type", "mqtt")
	name := "_rain_mqtt"
	if sourceType == "ha" {
		name = "_rain_ha"
	}
	s.renderPartial(c, name, http.StatusOK, gin.H{"weather": s.cfg.Weather})
}

func (s *Server) saveWeather(c *gin.Context) {
	lat, _ := strconv.ParseFloat(c.PostForm("lat"), 64)
	lon, _ := strconv.ParseFloat(c.PostForm("lon"), 64)
	rainThreshold, _ := strconv.ParseFloat(c.PostForm("rain_threshold_mm"), 64)
	if rainThreshold <= 0 {
		rainThreshold = 5.0
	}

	s.cfg.Weather.Lat = lat
	s.cfg.Weather.Lon = lon
	s.cfg.Weather.RainThresholdMm = rainThreshold

	rainSource := c.PostForm("rain_source")
	if rainSource == "ha" {
		s.cfg.Weather.RainSensorEntity = c.PostForm("rain_sensor_entity")
		s.cfg.Weather.RainSensorTopic = ""
	} else {
		s.cfg.Weather.RainSensorTopic = c.PostForm("rain_sensor_topic")
		s.cfg.Weather.RainSensorEntity = ""
	}

	if err := s.store.SaveWeatherConfig(&models.WeatherConfig{
		Lat:              s.cfg.Weather.Lat,
		Lon:              s.cfg.Weather.Lon,
		RainThresholdMm:  s.cfg.Weather.RainThresholdMm,
		RainSensorTopic:  s.cfg.Weather.RainSensorTopic,
		RainSensorEntity: s.cfg.Weather.RainSensorEntity,
	}); err != nil {
		log.Printf("Failed to persist weather config: %v", err)
	}

	s.logEvent("info", "config", "Weather config updated", "")
	c.Redirect(http.StatusFound, "/config")
}

func (s *Server) saveMasterValve(c *gin.Context) {
	source := c.PostForm("master_source")
	var cfg config.MasterValveConfig
	if source == "ha" {
		cfg.SwitchEntity = c.PostForm("switch_entity")
	} else {
		cfg.CommandTopic = c.PostForm("command_topic")
	}
	if err := s.store.SaveMasterValveConfig(&models.MasterValveConfig{
		CommandTopic: cfg.CommandTopic,
		SwitchEntity: cfg.SwitchEntity,
	}); err != nil {
		log.Printf("Failed to persist master valve config: %v", err)
	}
	s.cfg.MasterValve = cfg
	s.logEvent("info", "config", "Master valve config updated", "")
	c.Redirect(http.StatusFound, "/config")
}

func (s *Server) masterValveFields(c *gin.Context) {
	sourceType := c.DefaultQuery("type", "mqtt")
	name := "_master_mqtt"
	if sourceType == "ha" {
		name = "_master_ha"
	}
	s.renderPartial(c, name, http.StatusOK, gin.H{"masterValve": s.cfg.MasterValve})
}

func (s *Server) saveAlerts(c *gin.Context) {
	port, _ := strconv.Atoi(c.PostForm("smtp_port"))
	if port <= 0 {
		port = 587
	}
	staleMin, _ := strconv.Atoi(c.PostForm("stale_sensor_minutes"))
	if staleMin <= 0 {
		staleMin = 60
	}

	cfg := &models.AlertSettings{
		ID:                 1,
		Email:              c.PostForm("email"),
		StaleSensorMinutes: staleMin,
		SMTPServer:         c.PostForm("smtp_server"),
		SMTPPort:           port,
		SMTPUsername:       c.PostForm("smtp_username"),
		SMTPPassword:       c.PostForm("smtp_password"),
		FromEmail:          c.PostForm("from_email"),
		Enabled:            c.PostForm("enabled") != "false",
	}
	if err := s.store.SaveAlertSettings(cfg); err != nil {
		log.Printf("Failed to save alert settings: %v", err)
	}
	s.cfg.Alerts.Email = cfg.Email
	s.cfg.Alerts.StaleSensorMinutes = cfg.StaleSensorMinutes
	s.cfg.Alerts.SMTPServer = cfg.SMTPServer
	s.cfg.Alerts.SMTPPort = cfg.SMTPPort
	s.cfg.Alerts.SMTPUsername = cfg.SMTPUsername
	s.cfg.Alerts.SMTPPassword = cfg.SMTPPassword
	s.cfg.Alerts.FromEmail = cfg.FromEmail
	s.logEvent("info", "config", "Alert settings updated", "")
	c.Redirect(http.StatusFound, "/config")
}

func (s *Server) saveNtfy(c *gin.Context) {
	uuid := c.PostForm("uuid")

	existing, err := s.store.GetNtfyConfig()
	if err == nil && existing.UUID != "" {
		if uuid == "" {
			uuid = existing.UUID
		}
	}
	if uuid == "" {
		uuid = alerts.GenerateNtfyUUID()
	}

	cfg := &models.NtfyConfig{
		ID:         1,
		Enabled:    c.PostForm("enabled") == "true",
		Server:     c.PostForm("server"),
		UUID:       uuid,
		Token:      c.PostForm("token"),
		AlertInfo:  c.PostForm("alert_info") == "true",
		AlertWarn:  c.PostForm("alert_warn") == "true",
		AlertAlarm: c.PostForm("alert_alarm") == "true",
	}

	if cfg.Server == "" {
		cfg.Server = "https://ntfy.sh"
	}

	if err := s.store.SaveNtfyConfig(cfg); err != nil {
		log.Printf("Failed to save ntfy config: %v", err)
	}

	s.cfg.Ntfy.Enabled = cfg.Enabled
	s.cfg.Ntfy.Server = cfg.Server
	s.cfg.Ntfy.UUID = cfg.UUID
	s.cfg.Ntfy.Token = cfg.Token
	s.cfg.Ntfy.AlertInfo = cfg.AlertInfo
	s.cfg.Ntfy.AlertWarn = cfg.AlertWarn
	s.cfg.Ntfy.AlertAlarm = cfg.AlertAlarm

	s.logEvent("info", "config", "Push notification config updated", "")
	c.Redirect(http.StatusFound, "/config")
}

func (s *Server) saveZone(c *gin.Context) {
	idStr := c.PostForm("id")

	zc := models.ZoneConfig{
		Name:                    c.PostForm("name"),
		MoistureSensorTopic:     c.PostForm("moisture_sensor_topic"),
		MoistureSensorEntity:    c.PostForm("moisture_sensor_entity"),
		HumiditySensorTopic:     c.PostForm("humidity_sensor_topic"),
		HumiditySensorEntity:    c.PostForm("humidity_sensor_entity"),
		TemperatureSensorTopic:  c.PostForm("temperature_sensor_topic"),
		TemperatureSensorEntity: c.PostForm("temperature_sensor_entity"),
		ValveCommandTopic:       c.PostForm("valve_command_topic"),
		ValveStateTopic:         c.PostForm("valve_state_topic"),
		ValveSwitchEntity:       c.PostForm("valve_switch_entity"),
	}

	zc.ThresholdLow, _ = strconv.Atoi(c.PostForm("threshold_low"))
	zc.ThresholdHigh, _ = strconv.Atoi(c.PostForm("threshold_high"))
	zc.MaxWateringSeconds, _ = strconv.Atoi(c.PostForm("max_watering_seconds"))
	zc.MaxActivationsPerDay, _ = strconv.Atoi(c.PostForm("max_activations_per_day"))
	zc.CooldownMinutes, _ = strconv.Atoi(c.PostForm("cooldown_minutes"))
	zc.EarliestWateringTime = c.PostForm("earliest_watering_time")
	if zc.EarliestWateringTime == "" {
		zc.EarliestWateringTime = "06:00"
	}
	zc.LatestWateringTime = c.PostForm("latest_watering_time")
	if zc.LatestWateringTime == "" {
		zc.LatestWateringTime = "10:00"
	}

	multipliers := make(map[int]float64)
	for month := 1; month <= 12; month++ {
		key := fmt.Sprintf("seasonal_multiplier_%d", month)
		if val := c.PostForm(key); val != "" {
			if m, err := strconv.ParseFloat(val, 64); err == nil && m > 0 {
				multipliers[month] = m
			}
		}
	}
	multipliersJSON, _ := json.Marshal(multipliers)
	zc.SeasonalMultipliers = string(multipliersJSON)

	if idStr != "" {
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			c.Redirect(http.StatusFound, "/config")
			return
		}

		oldName := ""
		var old models.ZoneConfig
		if err := s.store.DB().First(&old, uint(id)).Error; err == nil {
			oldName = old.Name
		}

		if err := s.store.UpdateZoneConfig(uint(id), &zc); err != nil {
			log.Printf("Failed to update zone: %v", err)
		}

		zc.ID = uint(id)
		cfgZc := zc.ToConfigZoneConfig()

		if oldName != "" && oldName != zc.Name {
			ha.ClearZoneDiscovery(s.mqttClient, oldName)
			s.zoneManager.RemoveZone(oldName)
			s.zoneManager.AddZone(cfgZc)
			s.logEvent("info", "config", "Zone renamed: "+oldName+" → "+zc.Name, zc.Name)
		} else {
			s.zoneManager.UpdateZoneConfig(zc.Name, cfgZc)
			s.logEvent("info", "config", "Zone updated: "+zc.Name, zc.Name)
		}
	} else {
		if err := s.store.CreateZoneConfig(&zc); err != nil {
			log.Printf("Failed to create zone: %v", err)
			c.Redirect(http.StatusFound, "/config")
			return
		}

		s.zoneManager.AddZone(zc.ToConfigZoneConfig())
		s.logEvent("info", "config", "Zone created: "+zc.Name, zc.Name)
	}

	s.refreshHADiscovery()
	c.Redirect(http.StatusFound, "/config")
}

func (s *Server) deleteZone(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 64)

	var z models.ZoneConfig
	if err := s.store.DB().First(&z, uint(id)).Error; err == nil {
		ha.ClearZoneDiscovery(s.mqttClient, z.Name)
		s.zoneManager.RemoveZone(z.Name)
	}

	s.store.DeleteZoneConfig(uint(id))
	s.refreshHADiscovery()
	s.logEvent("info", "config", "Zone deleted: "+z.Name, z.Name)
	c.Redirect(http.StatusFound, "/config")
}

func (s *Server) zoneMoistureFields(c *gin.Context) {
	sourceType := c.DefaultQuery("type", "mqtt")
	var editZone *models.ZoneConfig
	if idStr := c.Query("edit_id"); idStr != "" {
		if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			z, _ := s.store.GetAllZoneConfigs()
			for _, zc := range z {
				if zc.ID == uint(id) {
					editZone = &zc
					break
				}
			}
		}
	}
	name := "_moisture_mqtt"
	if sourceType == "ha" {
		name = "_moisture_ha"
	}
	s.renderPartial(c, name, http.StatusOK, gin.H{"editZone": editZone})
}

func (s *Server) zoneValveFields(c *gin.Context) {
	sourceType := c.DefaultQuery("type", "mqtt")
	var editZone *models.ZoneConfig
	if idStr := c.Query("edit_id"); idStr != "" {
		if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			z, _ := s.store.GetAllZoneConfigs()
			for _, zc := range z {
				if zc.ID == uint(id) {
					editZone = &zc
					break
				}
			}
		}
	}
	name := "_valve_mqtt"
	if sourceType == "ha" {
		name = "_valve_ha"
	}
	s.renderPartial(c, name, http.StatusOK, gin.H{"editZone": editZone})
}

func (s *Server) zoneHumidityFields(c *gin.Context) {
	sourceType := c.DefaultQuery("type", "mqtt")
	var editZone *models.ZoneConfig
	if idStr := c.Query("edit_id"); idStr != "" {
		if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			z, _ := s.store.GetAllZoneConfigs()
			for _, zc := range z {
				if zc.ID == uint(id) {
					editZone = &zc
					break
				}
			}
		}
	}
	name := "_humidity_mqtt"
	if sourceType == "ha" {
		name = "_humidity_ha"
	}
	s.renderPartial(c, name, http.StatusOK, gin.H{"editZone": editZone})
}

func (s *Server) zoneTemperatureFields(c *gin.Context) {
	sourceType := c.DefaultQuery("type", "mqtt")
	var editZone *models.ZoneConfig
	if idStr := c.Query("edit_id"); idStr != "" {
		if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			z, _ := s.store.GetAllZoneConfigs()
			for _, zc := range z {
				if zc.ID == uint(id) {
					editZone = &zc
					break
				}
			}
		}
	}
	name := "_temperature_mqtt"
	if sourceType == "ha" {
		name = "_temperature_ha"
	}
	s.renderPartial(c, name, http.StatusOK, gin.H{"editZone": editZone})
}

func (s *Server) refreshHADiscovery() {
	dbZones, err := s.store.GetAllZoneConfigs()
	if err != nil {
		log.Printf("Failed to load zones for HA discovery refresh: %v", err)
		return
	}
	cfgZones := make([]config.ZoneConfig, len(dbZones))
	for i, z := range dbZones {
		cfgZones[i] = z.ToConfigZoneConfig()
	}
	cfg := &config.Config{Zones: cfgZones}
	ha.PublishAll(s.mqttClient, cfg)
}

func (s *Server) logEvent(level, category, message, zoneName string) {
	event := &models.EventLog{
		Level:    level,
		Category: category,
		Message:  message,
		ZoneName: zoneName,
	}
	if err := s.store.CreateEventLog(event); err != nil {
		log.Printf("Failed to log event: %v", err)
	}
	if s.mqttClient != nil && s.mqttClient.IsConnected() {
		payload, err := json.Marshal(event)
		if err != nil {
			log.Printf("Failed to marshal event: %v", err)
			return
		}
		s.mqttClient.Publish("bedwetter/event", 0, false, string(payload))
	}
}

func (s *Server) eventsPage(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "1")
	perPageStr := c.DefaultQuery("per_page", "50")
	page, _ := strconv.Atoi(pageStr)
	perPage, _ := strconv.Atoi(perPageStr)
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}

	result, err := s.store.GetEventLogs(page, perPage)
	if err != nil {
		s.render(c, "events", http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	s.render(c, "events", http.StatusOK, gin.H{
		"title":  "Event Log",
		"events": result,
	})
}

func (s *Server) apiZones(c *gin.Context) {
	zoneStates := s.zoneViews()
	results := make([]gin.H, len(zoneStates))
	for i, z := range zoneStates {
		results[i] = gin.H{
			"name":        z.Config.Name,
			"moisture":    nullableFloat(z.Moisture),
			"humidity":    nullableFloat(z.Humidity),
			"temperature": nullableFloat(z.Temperature),
			"state":       z.State,
			"last_update": z.LastMoistureTime.Format(time.RFC3339),
		}
		if !z.NextWatering.IsZero() {
			results[i]["next_watering"] = z.NextWatering.Format(time.RFC3339)
			results[i]["next_watering_reason"] = z.NextWateringReason
		}
		if z.StatusNote != "" {
			results[i]["status_note"] = z.StatusNote
			results[i]["status_variant"] = z.StatusNoteVariant
		}
	}
	c.JSON(http.StatusOK, results)
}

func (s *Server) statusNote(now time.Time, snap zones.ZoneSnapshot, rainActive bool, activations int64) (string, string) {
	if snap.State == zones.StateFailsafe {
		return "Failsafe active", "error"
	}
	if snap.State == zones.StateForceClosed {
		return "Force-closed", "warn"
	}
	if snap.State == zones.StateManualOpen {
		return "Manual open", "info"
	}
	if snap.State == zones.StateWatering {
		elapsed := time.Since(snap.WateringStarted)
		remaining := time.Duration(snap.Config.MaxWateringSeconds)*time.Second - elapsed
		if remaining > 0 {
			return fmt.Sprintf("Watering (%s remaining)", remaining.Truncate(time.Second)), "info"
		}
		return "Watering", "info"
	}
	if rainActive {
		return "Rain detected", "info"
	}
	if snap.State == zones.StateCooldown && snap.Config.CooldownMinutes > 0 && !snap.LastWaterEnd.IsZero() {
		cooldownEnd := snap.LastWaterEnd.Add(time.Duration(snap.Config.CooldownMinutes) * time.Minute)
		if cooldownEnd.After(now) {
			return fmt.Sprintf("Cooldown until %s", cooldownEnd.Format("15:04")), "info"
		}
	}
	if snap.Config.MaxActivationsPerDay > 0 && activations >= int64(snap.Config.MaxActivationsPerDay) {
		if math.IsNaN(snap.Moisture) || snap.Moisture < float64(snap.Config.ThresholdLow) {
			return "Max daily activations reached", "warn"
		}
	}
	if math.IsNaN(snap.Moisture) {
		return "Awaiting sensor", "muted"
	}
	if snap.Config.ThresholdLow > 0 && snap.Moisture >= float64(snap.Config.ThresholdLow) {
		return fmt.Sprintf("Soil moisture OK (%.1f%%)", snap.Moisture), "success"
	}
	return "", ""
}

func nullableFloat(f float64) interface{} {
	if math.IsNaN(f) {
		return nil
	}
	return f
}

func badgeClassForVariant(variant string) string {
	switch variant {
	case "error":
		return "badge-error"
	case "warn":
		return "badge-warning"
	case "info":
		return "badge-info"
	case "success":
		return "badge-success"
	default:
		return "badge-neutral"
	}
}

func nextWateringForZone(now time.Time, snap zones.ZoneSnapshot, scheduleEntries []models.ScheduleConfig) (time.Time, string) {
	if t, ok := nextScheduledOccurrence(now, scheduleEntries); ok {
		return t, "Schedule"
	}
	return time.Time{}, ""
}

func nextScheduledOccurrence(now time.Time, entries []models.ScheduleConfig) (time.Time, bool) {
	if len(entries) == 0 {
		return time.Time{}, false
	}
	loc := now.Location()
	var best time.Time
	for _, entry := range entries {
		if t, ok := nextOccurrenceForEntry(now, entry, loc); ok {
			if best.IsZero() || t.Before(best) {
				best = t
			}
		}
	}
	if best.IsZero() {
		return time.Time{}, false
	}
	return best, true
}

func nextOccurrenceForEntry(now time.Time, entry models.ScheduleConfig, loc *time.Location) (time.Time, bool) {
	if entry.Time == "" {
		return time.Time{}, false
	}
	schedTime, err := time.ParseInLocation("15:04", entry.Time, loc)
	if err != nil {
		return time.Time{}, false
	}
	for days := 0; days <= 366; days++ {
		candidateDate := now.AddDate(0, 0, days)
		if entry.Month > 0 && int(candidateDate.Month()) != entry.Month {
			continue
		}
		if entry.DayOfWeek != "" {
			if wd, ok := weekdayFromString(entry.DayOfWeek); ok {
				if candidateDate.Weekday() != wd {
					continue
				}
			} else {
				continue
			}
		}
		candidate := time.Date(candidateDate.Year(), candidateDate.Month(), candidateDate.Day(), schedTime.Hour(), schedTime.Minute(), 0, 0, loc)
		if candidate.After(now) {
			return candidate, true
		}
	}
	return time.Time{}, false
}

func weekdayFromString(s string) (time.Weekday, bool) {
	if s == "" {
		return time.Sunday, false
	}
	key := strings.ToLower(strings.TrimSpace(s))
	if len(key) >= 3 {
		key = key[:3]
	}
	switch key {
	case "mon":
		return time.Monday, true
	case "tue":
		return time.Tuesday, true
	case "wed":
		return time.Wednesday, true
	case "thu":
		return time.Thursday, true
	case "fri":
		return time.Friday, true
	case "sat":
		return time.Saturday, true
	case "sun":
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}

type scheduleBar struct {
	StartPct float64
	WidthPct float64
	Start    string
	End      string
	Label    string
}

type weekDaySchedule struct {
	Date string
	Day  string
	Bars []scheduleBar
}

func buildWeekSchedule(now time.Time, entries []models.ScheduleConfig) []weekDaySchedule {
	month := int(now.Month())
	var result []weekDaySchedule

	for days := 0; days < 7; days++ {
		d := now.AddDate(0, 0, days)
		dateStr := d.Format("Mon 2 Jan")
		weekdayStr := d.Weekday().String()[:3]

		var monthEntries []models.ScheduleConfig
		var weekdayEntries []models.ScheduleConfig
		for _, e := range entries {
			if e.Month > 0 {
				if e.Month == month {
					if wd, ok := weekdayFromString(e.DayOfWeek); ok && d.Weekday() == wd {
						monthEntries = append(monthEntries, e)
					}
				}
			} else if e.DayOfWeek != "" {
				if wd, ok := weekdayFromString(e.DayOfWeek); ok && d.Weekday() == wd {
					weekdayEntries = append(weekdayEntries, e)
				}
			}
		}

		active := weekdayEntries
		if len(monthEntries) > 0 {
			active = monthEntries
		}

		var bars []scheduleBar
		for _, e := range active {
			t, err := time.Parse("15:04", e.Time)
			if err != nil {
				continue
			}
			startMin := t.Hour()*60 + t.Minute()
			endMin := startMin + e.Duration
			if endMin > 24*60 {
				endMin = 24 * 60
			}

			startPct := float64(startMin) / float64(24*60) * 100
			widthPct := float64(e.Duration) / float64(24*60*60) * 100
			endTime := t.Add(time.Duration(e.Duration) * time.Second)

			bars = append(bars, scheduleBar{
				StartPct: startPct,
				WidthPct: widthPct,
				Start:    t.Format("15:04"),
				End:      endTime.Format("15:04"),
				Label:    fmt.Sprintf("%dm", e.Duration/60),
			})
		}

		result = append(result, weekDaySchedule{
			Date: dateStr,
			Day:  weekdayStr,
			Bars: bars,
		})
	}
	return result
}

func (s *Server) Start(addr string) error {
	log.Printf("Web UI starting on %s", addr)
	return s.router.Run(addr)
}
