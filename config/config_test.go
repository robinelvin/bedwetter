package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	content := []byte("mqtt:\n  broker: test\n")
	tmp := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(tmp, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Web.ListenAddr != ":8080" {
		t.Errorf("expected ListenAddr :8080, got %q", cfg.Web.ListenAddr)
	}
	if cfg.Alerts.SMTPPort != 587 {
		t.Errorf("expected SMTPPort 587, got %d", cfg.Alerts.SMTPPort)
	}
	if cfg.DBPath != "bedwetter.db" {
		t.Errorf("expected DBPath bedwetter.db, got %q", cfg.DBPath)
	}
	if cfg.MQTT.Broker != "test" {
		t.Errorf("expected MQTT broker test, got %q", cfg.MQTT.Broker)
	}
}

func TestLoadFull(t *testing.T) {
	content := []byte(`
mqtt:
  broker: mqtt.local
  port: 1883
  username: user
  password: pass

zones:
  - name: Zone 1
    moisture_sensor_topic: topic/sensor
    valve_command_topic: topic/cmd
    valve_state_topic: topic/state
    threshold_low: 30
    threshold_high: 60
    max_watering_seconds: 120
    max_activations_per_day: 3
    cooldown_minutes: 45

alerts:
  email: test@example.com

web:
  listen_addr: ":9090"

db_path: /tmp/test.db
`)
	tmp := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(tmp, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Web.ListenAddr != ":9090" {
		t.Errorf("expected :9090, got %q", cfg.Web.ListenAddr)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("expected /tmp/test.db, got %q", cfg.DBPath)
	}
	if len(cfg.Zones) != 1 {
		t.Fatalf("expected 1 zone, got %d", len(cfg.Zones))
	}
	z := cfg.Zones[0]
	if z.Name != "Zone 1" {
		t.Errorf("expected Zone 1, got %q", z.Name)
	}
	if z.ThresholdLow != 30 {
		t.Errorf("expected ThresholdLow 30, got %d", z.ThresholdLow)
	}
	if z.CooldownMinutes != 45 {
		t.Errorf("expected CooldownMinutes 45, got %d", z.CooldownMinutes)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	content := []byte("invalid: [yaml: broken")
	tmp := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(tmp, content, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(tmp)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestZoneConfigStruct(t *testing.T) {
	z := ZoneConfig{
		Name:                 "Test Zone",
		MoistureSensorTopic:  "a/b/c",
		MoistureSensorEntity: "sensor.test",
		ValveCommandTopic:    "d/e/f",
		ValveStateTopic:      "d/e/g",
		ValveSwitchEntity:    "switch.test",
		ThresholdLow:         10,
		ThresholdHigh:        90,
		MaxWateringSeconds:   600,
		MaxActivationsPerDay: 10,
		CooldownMinutes:      30,
	}

	if z.Name != "Test Zone" {
		t.Errorf("Name mismatch")
	}
	if z.ThresholdLow != 10 || z.ThresholdHigh != 90 {
		t.Errorf("Thresholds mismatch")
	}
}
