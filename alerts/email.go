package alerts

import (
	"fmt"
	"log"
	"net/smtp"
	"time"

	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/zones"
)

type AlertType string

const (
	AlertStaleSensor  AlertType = "stale_sensor"
	AlertMaxWatering  AlertType = "max_watering"
	AlertValveStuck   AlertType = "valve_stuck"
)

type AlertManager struct {
	cfg         *config.Config
	zoneManager *zones.Manager
	done        chan struct{}
	sentAlerts  map[string]time.Time
}

func New(cfg *config.Config, zoneManager *zones.Manager) *AlertManager {
	return &AlertManager{
		cfg:         cfg,
		zoneManager: zoneManager,
		done:        make(chan struct{}),
		sentAlerts:  make(map[string]time.Time),
	}
}

func (a *AlertManager) Start() {
	go a.loop()
}

func (a *AlertManager) Stop() {
	close(a.done)
}

func (a *AlertManager) loop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
			a.checkAlerts()
		}
	}
}

func (a *AlertManager) checkAlerts() {
	if a.cfg.Alerts.Email == "" {
		return
	}
	zones := a.zoneManager.GetAllZones()
	staleThreshold := time.Duration(a.cfg.Alerts.StaleSensorMinutes) * time.Minute

	for _, z := range zones {
		if z.LastMoistureTime.IsZero() {
			continue
		}
		since := time.Since(z.LastMoistureTime)
		key := fmt.Sprintf("%s:%s", AlertStaleSensor, z.Config.Name)
		if since > staleThreshold && !a.wasSentRecently(key) {
			a.sendAlert(fmt.Sprintf("Stale Sensor: %s", z.Config.Name),
				fmt.Sprintf("No sensor reading from %s for %v (threshold: %v). Last value: %.1f%%",
					z.Config.Name, since.Round(time.Minute), staleThreshold, z.Moisture))
			a.sentAlerts[key] = time.Now()
		}
	}
}

func (a *AlertManager) SendMaxWateringAlert(zoneName string) {
	key := fmt.Sprintf("%s:%s", AlertMaxWatering, zoneName)
	if a.wasSentRecently(key) {
		return
	}
	a.sendAlert(fmt.Sprintf("Max Watering Cycles: %s", zoneName),
		fmt.Sprintf("Zone %s has reached its maximum daily watering activations but moisture remains below threshold.", zoneName))
	a.sentAlerts[key] = time.Now()
}

func (a *AlertManager) SendValveStuckAlert(zoneName string, duration time.Duration) {
	key := fmt.Sprintf("%s:%s", AlertValveStuck, zoneName)
	if a.wasSentRecently(key) {
		return
	}
	a.sendAlert(fmt.Sprintf("Valve Stuck Open: %s", zoneName),
		fmt.Sprintf("Valve for zone %s has been open for %v, exceeding expected duration.", zoneName, duration.Round(time.Minute)))
	a.sentAlerts[key] = time.Now()
}

func (a *AlertManager) wasSentRecently(key string) bool {
	t, ok := a.sentAlerts[key]
	if !ok {
		return false
	}
	return time.Since(t) < time.Hour
}

func (a *AlertManager) sendAlert(subject, body string) {
	cfg := a.cfg.Alerts
	if cfg.Email == "" {
		return
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		cfg.FromEmail, cfg.Email, subject, body)

	addr := fmt.Sprintf("%s:%d", cfg.SMTPServer, cfg.SMTPPort)
	auth := smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPServer)

	if err := smtp.SendMail(addr, auth, cfg.FromEmail, []string{cfg.Email}, []byte(msg)); err != nil {
		log.Printf("Failed to send alert email: %v", err)
		return
	}
	log.Printf("Alert sent: %s", subject)
}
