package models

import (
	"testing"

	"github.com/robinelvin/bedwetter/config"
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
		EarliestWateringTime: "06:00",
		LatestWateringTime:   "10:00",
		SeasonalMultipliers:  `{"1":0.5,"7":1.5}`,
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
	if c.EarliestWateringTime != "06:00" {
		t.Errorf("EarliestWateringTime: got %q", c.EarliestWateringTime)
	}
	if c.LatestWateringTime != "10:00" {
		t.Errorf("LatestWateringTime: got %q", c.LatestWateringTime)
	}
	if c.SeasonalMultipliers[1] != 0.5 {
		t.Errorf("SeasonalMultipliers[1]: got %f", c.SeasonalMultipliers[1])
	}
	if c.SeasonalMultipliers[7] != 1.5 {
		t.Errorf("SeasonalMultipliers[7]: got %f", c.SeasonalMultipliers[7])
	}
}

func TestFromConfigZoneConfig(t *testing.T) {
	c := config.ZoneConfig{
		Name:                 "From Config",
		MoistureSensorTopic:  "x/y",
		ThresholdLow:         20,
		EarliestWateringTime: "07:00",
		LatestWateringTime:   "09:00",
		SeasonalMultipliers:  map[int]float64{1: 0.3, 12: 0.1},
	}

	var m ZoneConfig
	m.FromConfigZoneConfig(c)

	if m.Name != "From Config" {
		t.Errorf("Name: got %q", m.Name)
	}
	if m.ThresholdLow != 20 {
		t.Errorf("ThresholdLow: got %d", m.ThresholdLow)
	}
	if m.EarliestWateringTime != "07:00" {
		t.Errorf("EarliestWateringTime: got %q", m.EarliestWateringTime)
	}
	if m.LatestWateringTime != "09:00" {
		t.Errorf("LatestWateringTime: got %q", m.LatestWateringTime)
	}
	if m.SeasonalMultipliers != `{"1":0.3,"12":0.1}` && m.SeasonalMultipliers != `{"12":0.1,"1":0.3}` {
		t.Errorf("SeasonalMultipliers: got %q", m.SeasonalMultipliers)
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
