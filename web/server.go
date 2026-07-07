package web

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rob/bedwetter/alerts"
	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/models"
	"github.com/rob/bedwetter/store"
	"github.com/rob/bedwetter/zones"
)

type Server struct {
	cfg         *config.Config
	store       *store.Store
	zoneManager *zones.Manager
	alertMgr    *alerts.AlertManager
	router      *gin.Engine
	templates   map[string]*template.Template
}

func New(cfg *config.Config, s *store.Store, zm *zones.Manager, am *alerts.AlertManager) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	sv := &Server{
		cfg:         cfg,
		store:       s,
		zoneManager: zm,
		alertMgr:    am,
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
		"floatVal": func(f float64) float64 { return f },
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
	s.router.POST("/config/zones", s.saveZone)
	s.router.POST("/config/zones/:id/delete", s.deleteZone)
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
	alertConfigs := []models.AlertConfig{
		{Type: string(alerts.AlertStaleSensor), Email: s.cfg.Alerts.Email, Enabled: true},
	}

	dbZones, _ := s.store.GetAllZoneConfigs()

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
		"alerts":   alertConfigs,
		"cfg":      s.cfg,
		"dbZones":  dbZones,
		"editZone": editZone,
	})
}

func (s *Server) saveAlerts(c *gin.Context) {
	email := c.PostForm("email")
	s.cfg.Alerts.Email = email
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
			s.zoneManager.RemoveZone(oldName)
			s.zoneManager.AddZone(cfgZc)
		} else {
			s.zoneManager.UpdateZoneConfig(zc.Name, cfgZc)
		}
	} else {
		if err := s.store.CreateZoneConfig(&zc); err != nil {
			log.Printf("Failed to create zone: %v", err)
			c.Redirect(http.StatusFound, "/config")
			return
		}

		s.zoneManager.AddZone(zc.ToConfigZoneConfig())
	}

	c.Redirect(http.StatusFound, "/config")
}

func (s *Server) deleteZone(c *gin.Context) {
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 64)

	var z models.ZoneConfig
	if err := s.store.DB().First(&z, uint(id)).Error; err == nil {
		s.zoneManager.RemoveZone(z.Name)
	}

	s.store.DeleteZoneConfig(uint(id))
	c.Redirect(http.StatusFound, "/config")
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
