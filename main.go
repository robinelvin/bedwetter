package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/robinelvin/bedwetter/alerts"
	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/ha"
	"github.com/robinelvin/bedwetter/models"
	mqttclient "github.com/robinelvin/bedwetter/mqtt"
	"github.com/robinelvin/bedwetter/scheduler"
	"github.com/robinelvin/bedwetter/store"
	"github.com/robinelvin/bedwetter/web"
	"github.com/robinelvin/bedwetter/zones"
)

func logEvent(db *store.Store, mqtt mqttclient.ClientInterface, level, category, message, zoneName string) {
	event := &models.EventLog{
		Level:    level,
		Category: category,
		Message:  message,
		ZoneName: zoneName,
	}
	if err := db.CreateEventLog(event); err != nil {
		log.Printf("Failed to log event: %v", err)
	}
	if mqtt != nil && mqtt.IsConnected() {
		payload, err := json.Marshal(event)
		if err != nil {
			log.Printf("Failed to marshal event: %v", err)
			return
		}
		mqtt.Publish("bedwetter/event", 0, false, string(payload))
	}
}

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Print(`
 ██████╗ ███████╗██████╗ ██╗    ██╗███████╗████████╗████████╗███████╗██████╗
 ██╔══██╗██╔════╝██╔══██╗██║    ██║██╔════╝╚══██╔══╝╚══██╔══╝██╔════╝██╔══██╗
 ██████╔╝█████╗  ██║  ██║██║ █╗ ██║█████╗     ██║      ██║   █████╗  ██████╔╝
 ██╔══██╗██╔══╝  ██║  ██║██║███╗██║██╔══╝     ██║      ██║   ██╔══╝  ██╔══██╗
 ██████╔╝███████╗██████╔╝╚███╔███╔╝███████╗   ██║      ██║   ███████╗██║  ██║
 ╚═════╝ ╚══════╝╚═════╝  ╚══╝╚══╝ ╚══════╝   ╚═╝      ╚═╝   ╚══════╝╚═╝  ╚═╝
`)

	db, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	if err := db.LoadConfigZones(cfg.Zones); err != nil {
		log.Printf("Failed to seed zones into DB: %v", err)
	}
	if err := db.LoadConfigSchedules(cfg.Schedules); err != nil {
		log.Printf("Failed to load config schedules into DB: %v", err)
	}

	dbZones, err := db.GetAllZoneConfigs()
	if err != nil {
		log.Fatalf("Failed to load zones from DB: %v", err)
	}
	cfg.Zones = make([]config.ZoneConfig, len(dbZones))
	for i, z := range dbZones {
		cfg.Zones[i] = z.ToConfigZoneConfig()
	}

	// Load MQTT config from DB, seed from YAML on first run
	if _, err := db.GetMQTTConfig(); err != nil {
		if err := db.SaveMQTTConfig(&models.MQTTConfig{
			Broker: cfg.MQTT.Broker, Port: cfg.MQTT.Port,
			Username: cfg.MQTT.Username, Password: cfg.MQTT.Password,
		}); err != nil {
			log.Printf("Failed to seed MQTT config: %v", err)
		}
	}
	if mqttCfg, err := db.GetMQTTConfig(); err == nil {
		cfg.MQTT.Broker = mqttCfg.Broker
		cfg.MQTT.Port = mqttCfg.Port
		cfg.MQTT.Username = mqttCfg.Username
		cfg.MQTT.Password = mqttCfg.Password
	}

	// Load HA config from DB, seed from YAML on first run
	if _, err := db.GetHAConfig(); err != nil {
		if err := db.SaveHAConfig(&models.HAConfig{
			URL: cfg.HomeAssistant.URL, Token: cfg.HomeAssistant.Token,
		}); err != nil {
			log.Printf("Failed to seed HA config: %v", err)
		}
	}
	if haCfg, err := db.GetHAConfig(); err == nil {
		cfg.HomeAssistant.URL = haCfg.URL
		cfg.HomeAssistant.Token = haCfg.Token
	}

	// Load weather config from DB, seed from YAML on first run
	if _, err := db.GetWeatherConfig(); err != nil {
		if err := db.SaveWeatherConfig(&models.WeatherConfig{
			Lat:              cfg.Weather.Lat,
			Lon:              cfg.Weather.Lon,
			RainThresholdMm:  cfg.Weather.RainThresholdMm,
			RainSensorTopic:  cfg.Weather.RainSensorTopic,
			RainSensorEntity: cfg.Weather.RainSensorEntity,
		}); err != nil {
			log.Printf("Failed to seed weather config: %v", err)
		}
	}
	if weatherCfg, err := db.GetWeatherConfig(); err == nil {
		cfg.Weather.Lat = weatherCfg.Lat
		cfg.Weather.Lon = weatherCfg.Lon
		cfg.Weather.RainThresholdMm = weatherCfg.RainThresholdMm
		cfg.Weather.RainSensorTopic = weatherCfg.RainSensorTopic
		cfg.Weather.RainSensorEntity = weatherCfg.RainSensorEntity
	}

	// Load master valve config from DB, seed from YAML on first run
	if _, err := db.GetMasterValveConfig(); err != nil {
		if err := db.SaveMasterValveConfig(&models.MasterValveConfig{
			CommandTopic: cfg.MasterValve.CommandTopic,
			SwitchEntity: cfg.MasterValve.SwitchEntity,
		}); err != nil {
			log.Printf("Failed to seed master valve config: %v", err)
		}
	}
	if mvCfg, err := db.GetMasterValveConfig(); err == nil {
		cfg.MasterValve.CommandTopic = mvCfg.CommandTopic
		cfg.MasterValve.SwitchEntity = mvCfg.SwitchEntity
	}

	// Load alert settings from DB, seed from YAML on first run
	if _, err := db.GetAlertSettings(); err != nil {
		if err := db.SaveAlertSettings(&models.AlertSettings{
			Email:              cfg.Alerts.Email,
			StaleSensorMinutes: cfg.Alerts.StaleSensorMinutes,
			SMTPServer:         cfg.Alerts.SMTPServer,
			SMTPPort:           cfg.Alerts.SMTPPort,
			SMTPUsername:       cfg.Alerts.SMTPUsername,
			SMTPPassword:       cfg.Alerts.SMTPPassword,
			FromEmail:          cfg.Alerts.FromEmail,
			Enabled:            true,
		}); err != nil {
			log.Printf("Failed to seed alert settings: %v", err)
		}
	}
	if alertCfg, err := db.GetAlertSettings(); err == nil {
		cfg.Alerts.Email = alertCfg.Email
		cfg.Alerts.StaleSensorMinutes = alertCfg.StaleSensorMinutes
		cfg.Alerts.SMTPServer = alertCfg.SMTPServer
		cfg.Alerts.SMTPPort = alertCfg.SMTPPort
		cfg.Alerts.SMTPUsername = alertCfg.SMTPUsername
		cfg.Alerts.SMTPPassword = alertCfg.SMTPPassword
		cfg.Alerts.FromEmail = alertCfg.FromEmail
	}

	// Load ntfy config from DB, seed from YAML on first run
	if _, err := db.GetNtfyConfig(); err != nil {
		uuid := alerts.GenerateNtfyUUID()
		if cfg.Ntfy.UUID != "" {
			uuid = cfg.Ntfy.UUID
		}
		if err := db.SaveNtfyConfig(&models.NtfyConfig{
			Enabled:    cfg.Ntfy.Enabled,
			Server:     cfg.Ntfy.Server,
			UUID:       uuid,
			Token:      cfg.Ntfy.Token,
			AlertInfo:  cfg.Ntfy.AlertInfo,
			AlertWarn:  cfg.Ntfy.AlertWarn,
			AlertAlarm: cfg.Ntfy.AlertAlarm,
		}); err != nil {
			log.Printf("Failed to seed ntfy config: %v", err)
		}
	}
	if ntfyCfg, err := db.GetNtfyConfig(); err == nil {
		cfg.Ntfy.Enabled = ntfyCfg.Enabled
		cfg.Ntfy.Server = ntfyCfg.Server
		cfg.Ntfy.UUID = ntfyCfg.UUID
		cfg.Ntfy.Token = ntfyCfg.Token
		cfg.Ntfy.AlertInfo = ntfyCfg.AlertInfo
		cfg.Ntfy.AlertWarn = ntfyCfg.AlertWarn
		cfg.Ntfy.AlertAlarm = ntfyCfg.AlertAlarm
	}

	mqtt := mqttclient.New(cfg.MQTT.Broker, cfg.MQTT.Port, cfg.MQTT.Username, cfg.MQTT.Password)

	if err := mqtt.Connect(); err != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", err)
	}
	log.Println("Connected to MQTT broker")

	logEvent(db, mqtt, "info", "system", "System started", "")

	haAPI := ha.NewAPIClient(cfg.HomeAssistant.URL, cfg.HomeAssistant.Token)

	resolver := ha.NewEntityResolver(mqtt)
	for i := range cfg.Zones {
		ha.ResolveZoneAsync(resolver, &cfg.Zones[i])
	}

	zoneManager := zones.NewManager(cfg, mqtt, db, resolver, haAPI)
	ntfyClient := alerts.NewNtfyClient(cfg)
	zoneManager.SetNtfySender(ntfyClient.Send)
	zoneManager.Start()
	haAPI.Start()

	ha.PublishAll(mqtt, cfg)
	ha.SubscribeToCommands(mqtt, cfg, func(zoneName, state string) {
		z := zoneManager.GetZone(zoneName)
		if z == nil {
			return
		}
		if state == "ON" || state == "on" || state == "1" {
			zoneManager.OpenValve(zoneName)
		} else {
			zoneManager.CloseValve(zoneName)
		}
	})

	sched := scheduler.New(cfg, db, zoneManager)
	sched.Start()

	alertMgr := alerts.New(cfg, zoneManager)
	alertMgr.Start()

	gin.SetMode(gin.ReleaseMode)
	webServer := web.New(cfg, db, zoneManager, alertMgr, mqtt, haAPI, sched)

	go func() {
		if err := webServer.Start(cfg.Web.ListenAddr); err != nil {
			log.Fatalf("Web server failed: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	logEvent(db, mqtt, "info", "system", "System shutting down", "")
	haAPI.Stop()
	zoneManager.Stop()
	sched.Stop()
	alertMgr.Stop()
	mqtt.Disconnect(250)
	log.Println("Shutdown complete")
}
