package models

import (
	"time"

	"github.com/rob/bedwetter/config"
	"gorm.io/gorm"
)

type SensorReading struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ZoneName  string    `gorm:"index;size:128" json:"zone_name"`
	Moisture  float64   `json:"moisture"`
	Humidity  float64   `json:"humidity"`
	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

type ValveEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ZoneName  string    `gorm:"index;size:128" json:"zone_name"`
	State     string    `gorm:"size:16" json:"state"`
	Duration  int       `json:"duration"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

type ScheduleConfig struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	ZoneName      string `gorm:"index;size:128" json:"zone_name"`
	DayOfWeek     string `gorm:"size:16" json:"day_of_week"`
	Time          string `gorm:"size:8" json:"time"`
	Duration      int    `json:"duration"`
	Month         int    `gorm:"default:0" json:"month"`
}

type AlertConfig struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	Type    string `gorm:"size:32;uniqueIndex" json:"type"`
	Email   string `gorm:"size:256" json:"email"`
	Enabled bool   `gorm:"default:true" json:"enabled"`
}

type ZoneConfig struct {
	ID                   uint   `gorm:"primaryKey" json:"id"`
	Name                 string `gorm:"size:128;uniqueIndex" json:"name"`
	MoistureSensorTopic  string `gorm:"size:256" json:"moisture_sensor_topic"`
	MoistureSensorEntity string `gorm:"size:256" json:"moisture_sensor_entity"`
	ValveCommandTopic    string `gorm:"size:256" json:"valve_command_topic"`
	ValveStateTopic      string `gorm:"size:256" json:"valve_state_topic"`
	ValveSwitchEntity    string `gorm:"size:256" json:"valve_switch_entity"`
	ThresholdLow         int    `json:"threshold_low"`
	ThresholdHigh        int    `json:"threshold_high"`
	MaxWateringSeconds   int    `json:"max_watering_seconds"`
	MaxActivationsPerDay int    `json:"max_activations_per_day"`
	CooldownMinutes      int    `json:"cooldown_minutes"`
}

func (m *ZoneConfig) ToConfigZoneConfig() config.ZoneConfig {
	return config.ZoneConfig{
		Name:                 m.Name,
		MoistureSensorTopic:  m.MoistureSensorTopic,
		MoistureSensorEntity: m.MoistureSensorEntity,
		ValveCommandTopic:    m.ValveCommandTopic,
		ValveStateTopic:      m.ValveStateTopic,
		ValveSwitchEntity:    m.ValveSwitchEntity,
		ThresholdLow:         m.ThresholdLow,
		ThresholdHigh:        m.ThresholdHigh,
		MaxWateringSeconds:   m.MaxWateringSeconds,
		MaxActivationsPerDay: m.MaxActivationsPerDay,
		CooldownMinutes:      m.CooldownMinutes,
	}
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&SensorReading{},
		&ValveEvent{},
		&ScheduleConfig{},
		&AlertConfig{},
		&ZoneConfig{},
	)
}

