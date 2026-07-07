package models

import (
	"testing"
)

func TestToConfigZoneConfig(t *testing.T) {
	m := ZoneConfig{
		ID:                   1,
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

	c := m.ToConfigZoneConfig()

	if c.Name != "Test Zone" {
		t.Errorf("Name: got %q, want %q", c.Name, "Test Zone")
	}
	if c.MoistureSensorTopic != "a/b/c" {
		t.Errorf("MoistureSensorTopic: got %q, want %q", c.MoistureSensorTopic, "a/b/c")
	}
	if c.MoistureSensorEntity != "sensor.test" {
		t.Errorf("MoistureSensorEntity: got %q, want %q", c.MoistureSensorEntity, "sensor.test")
	}
	if c.ValveCommandTopic != "d/e/f" {
		t.Errorf("ValveCommandTopic: got %q", c.ValveCommandTopic)
	}
	if c.ValveStateTopic != "d/e/g" {
		t.Errorf("ValveStateTopic: got %q", c.ValveStateTopic)
	}
	if c.ValveSwitchEntity != "switch.test" {
		t.Errorf("ValveSwitchEntity: got %q", c.ValveSwitchEntity)
	}
	if c.ThresholdLow != 10 {
		t.Errorf("ThresholdLow: got %d", c.ThresholdLow)
	}
	if c.ThresholdHigh != 90 {
		t.Errorf("ThresholdHigh: got %d", c.ThresholdHigh)
	}
	if c.MaxWateringSeconds != 600 {
		t.Errorf("MaxWateringSeconds: got %d", c.MaxWateringSeconds)
	}
	if c.MaxActivationsPerDay != 10 {
		t.Errorf("MaxActivationsPerDay: got %d", c.MaxActivationsPerDay)
	}
	if c.CooldownMinutes != 30 {
		t.Errorf("CooldownMinutes: got %d", c.CooldownMinutes)
	}
}

func TestToConfigZoneConfigEmpty(t *testing.T) {
	m := ZoneConfig{}
	c := m.ToConfigZoneConfig()

	if c.Name != "" {
		t.Errorf("expected empty name, got %q", c.Name)
	}
	if c.ThresholdLow != 0 {
		t.Errorf("expected 0 ThresholdLow, got %d", c.ThresholdLow)
	}
}
