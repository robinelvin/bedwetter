package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/rob/bedwetter/alerts"
	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/ha"
	mqttclient "github.com/rob/bedwetter/mqtt"
	"github.com/rob/bedwetter/scheduler"
	"github.com/rob/bedwetter/store"
	"github.com/rob/bedwetter/web"
	"github.com/rob/bedwetter/zones"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

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

	mqtt := mqttclient.New(cfg.MQTT.Broker, cfg.MQTT.Port, cfg.MQTT.Username, cfg.MQTT.Password)

	if err := mqtt.Connect(); err != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", err)
	}
	log.Println("Connected to MQTT broker")

	haAPI := ha.NewAPIClient(cfg.HomeAssistant.URL, cfg.HomeAssistant.Token)

	resolver := ha.NewEntityResolver(mqtt)
	for i := range cfg.Zones {
		ha.ResolveZoneAsync(resolver, &cfg.Zones[i])
	}

	zoneManager := zones.NewManager(cfg, mqtt, db, resolver, haAPI)
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

	webServer := web.New(cfg, db, zoneManager, alertMgr)

	go func() {
		if err := webServer.Start(cfg.Web.ListenAddr); err != nil {
			log.Fatalf("Web server failed: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	haAPI.Stop()
	zoneManager.Stop()
	sched.Stop()
	alertMgr.Stop()
	mqtt.Disconnect(250)
	log.Println("Shutdown complete")
}
