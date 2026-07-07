package models

import (
	"time"

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

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&SensorReading{},
		&ValveEvent{},
		&ScheduleConfig{},
		&AlertConfig{},
	)
}
