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

	if err := db.LoadConfigSchedules(cfg.Schedules); err != nil {
		log.Printf("Failed to load config schedules into DB: %v", err)
	}

	mqtt := mqttclient.New(cfg.MQTT.Broker, cfg.MQTT.Port, cfg.MQTT.Username, cfg.MQTT.Password)

	if err := mqtt.Connect(); err != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", err)
	}
	log.Println("Connected to MQTT broker")

	zoneManager := zones.NewManager(cfg, mqtt, db)
	zoneManager.Start()

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
	zoneManager.Stop()
	sched.Stop()
	alertMgr.Stop()
	mqtt.Disconnect(250)
	log.Println("Shutdown complete")
}
