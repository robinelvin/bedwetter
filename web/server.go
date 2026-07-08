package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rob/bedwetter/alerts"
	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/ha"
	"github.com/rob/bedwetter/models"
	"github.com/rob/bedwetter/mqtt"
	"github.com/rob/bedwetter/store"
	"github.com/rob/bedwetter/zones"
)

type Server struct {
	cfg         *config.Config
	store       *store.Store
	zoneManager *zones.Manager
	alertMgr    *alerts.AlertManager
	mqttClient  mqtt.ClientInterface
	router      *gin.Engine
	templates   map[string]*template.Template
}

func New(cfg *config.Config, s *store.Store, zm *zones.Manager, am *alerts.AlertManager, mqttClient mqtt.ClientInterface) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	sv := &Server{
		cfg:         cfg,
		store:       s,
		zoneManager: zm,
		alertMgr:    am,
		mqttClient:  mqttClient,
		router:      r,
		templates:   make(map[string]*template.Template),
	}

	funcMap := template.FuncMap{
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
	}

	sv.templates["dashboard"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS,
			"templates/base.html", "templates/dashboard.html", "templates/_zone_cards.html"),
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

	sv.templates["_zone_cards"] = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/_zone_cards.html"),
	)

	sv.setupRoutes()
	return sv
}

func (s *Server) render(c *gin.Context, page string, code int, data gin.H) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(code)
	if err := s.templates[page].ExecuteTemplate(c.Writer, "base", data); err != nil {
		log.Printf("Template render error: %v", err)
	}
}

func (s *Server) renderPartial(c *gin.Context, name string, code int, data gin.H) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(code)
	if err := s.templates[name].ExecuteTemplate(c.Writer, name, data); err != nil {
		log.Printf("Template render error: %v", err)
	}
}

func (s *Server) setupRoutes() {
	s.router.Static("/static", "./web/static")
	s.router.GET("/", s.dashboard)
	s.router.GET("/dashboard", s.dashboard)
	s.router.GET("/dashboard/zones", s.dashboardZones)
	s.router.POST("/zones/:name/open", s.openValve)
	s.router.POST("/zones/:name/close", s.closeValve)
	s.router.GET("/zones/:name/history", s.zoneHistory)
	s.router.GET("/schedules", s.schedulesPage)
	s.router.POST("/schedules", s.saveSchedule)
	s.router.POST("/schedules/:id/delete", s.deleteSchedule)
	s.router.GET("/config", s.configPage)
	s.router.POST("/config/alerts", s.saveAlerts)
	s.router.POST("/config/mqtt", s.saveMQTT)
	s.router.POST("/config/ha", s.saveHA)
	s.router.POST("/config/zones", s.saveZone)
	s.router.POST("/config/zones/:id/delete", s.deleteZone)
	s.router.GET("/events", s.eventsPage)
	s.router.GET("/api/zones", s.apiZones)
}

func (s *Server) dashboard(c *gin.Context) {
	zoneStates := s.zoneManager.GetAllZones()
	s.render(c, "dashboard", http.StatusOK, gin.H{
		"title": "Dashboard",
		"zones": zoneStates,
	})
}

func (s *Server) dashboardZones(c *gin.Context) {
	zoneStates := s.zoneManager.GetAllZones()
	s.renderPartial(c, "_zone_cards", http.StatusOK, gin.H{
		"zones": zoneStates,
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

func (s *Server) zoneHistory(c *gin.Context) {
	name := c.Param("name")
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
	s.render(c, "schedules", http.StatusOK, gin.H{
		"title":     "Watering Schedules",
		"schedules": schedules,
		"zones":     zoneNames,
		"days":      []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
	})
}

func (s *Server) saveSchedule(c *gin.Context) {
	zoneName := c.PostForm("zone_name")
	dayOfWeek := c.PostForm("day_of_week")
	time := c.PostForm("time")
	durationStr := c.PostForm("duration")
	duration, _ := strconv.Atoi(durationStr)
	if duration <= 0 {
		duration = 300
	}

	entry := &models.ScheduleConfig{
		ZoneName:  zoneName,
		DayOfWeek: dayOfWeek,
		Time:      time,
		Duration:  duration,
	}
	if err := s.store.CreateScheduleEntry(entry); err != nil {
		log.Printf("Failed to save schedule: %v", err)
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

	s.render(c, "config", http.StatusOK, gin.H{
		"title":    "Configuration",
		"cfg":      s.cfg,
		"mqtt":     mqttCfg,
		"ha":       haCfg,
		"alerts":   alertCfg,
		"dbZones":  dbZones,
		"editZone": editZone,
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
	s.logEvent("info", "config", "HA config updated", "")
	c.Redirect(http.StatusFound, "/config")
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

func (s *Server) saveZone(c *gin.Context) {
	idStr := c.PostForm("id")

	zc := models.ZoneConfig{
		Name:                 c.PostForm("name"),
		MoistureSensorTopic:  c.PostForm("moisture_sensor_topic"),
		MoistureSensorEntity: c.PostForm("moisture_sensor_entity"),
		ValveCommandTopic:    c.PostForm("valve_command_topic"),
		ValveStateTopic:      c.PostForm("valve_state_topic"),
		ValveSwitchEntity:    c.PostForm("valve_switch_entity"),
	}

	zc.ThresholdLow, _ = strconv.Atoi(c.PostForm("threshold_low"))
	zc.ThresholdHigh, _ = strconv.Atoi(c.PostForm("threshold_high"))
	zc.MaxWateringSeconds, _ = strconv.Atoi(c.PostForm("max_watering_seconds"))
	zc.MaxActivationsPerDay, _ = strconv.Atoi(c.PostForm("max_activations_per_day"))
	zc.CooldownMinutes, _ = strconv.Atoi(c.PostForm("cooldown_minutes"))

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
	zoneStates := s.zoneManager.GetAllZones()
	results := make([]gin.H, len(zoneStates))
	for i, z := range zoneStates {
		results[i] = gin.H{
			"name":        z.Config.Name,
			"moisture":    z.Moisture,
			"humidity":    z.Humidity,
			"temperature": z.Temperature,
			"state":       z.State,
			"last_update": z.LastMoistureTime.Format(time.RFC3339),
		}
	}
	c.JSON(http.StatusOK, results)
}

func (s *Server) Start(addr string) error {
	log.Printf("Web UI starting on %s", addr)
	return s.router.Run(addr)
}
