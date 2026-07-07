package store

import (
	"time"

	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Store struct {
	db *gorm.DB
}

func New(dbPath string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := models.AutoMigrate(db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) DB() *gorm.DB {
	return s.db
}

func (s *Store) SaveSensorReading(zoneName string, moisture, humidity float64) error {
	return s.db.Create(&models.SensorReading{
		ZoneName: zoneName,
		Moisture: moisture,
		Humidity: humidity,
	}).Error
}

func (s *Store) SaveValveEvent(zoneName, state string, duration int) error {
	return s.db.Create(&models.ValveEvent{
		ZoneName: zoneName,
		State:    state,
		Duration: duration,
	}).Error
}

func (s *Store) RecentReadings(zoneName string, hours int) ([]models.SensorReading, error) {
	var readings []models.SensorReading
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	err := s.db.Where("zone_name = ? AND created_at >= ?", zoneName, since).
		Order("created_at asc").Find(&readings).Error
	return readings, err
}

func (s *Store) RecentValveEvents(zoneName string, limit int) ([]models.ValveEvent, error) {
	var events []models.ValveEvent
	err := s.db.Where("zone_name = ?", zoneName).
		Order("created_at desc").Limit(limit).Find(&events).Error
	return events, err
}

func (s *Store) ActivationsToday(zoneName string) (int64, error) {
	var count int64
	today := time.Now().Truncate(24 * time.Hour)
	err := s.db.Model(&models.ValveEvent{}).
		Where("zone_name = ? AND state = ? AND created_at >= ?", zoneName, "open", today).
		Count(&count).Error
	return count, err
}

func (s *Store) SaveSchedule(entries []models.ScheduleConfig) error {
	if len(entries) == 0 {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		zoneName := entries[0].ZoneName
		tx.Where("zone_name = ?", zoneName).Delete(&models.ScheduleConfig{})
		for _, e := range entries {
			if err := tx.Create(&e).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) GetSchedule(zoneName string) ([]models.ScheduleConfig, error) {
	var entries []models.ScheduleConfig
	err := s.db.Where("zone_name = ?", zoneName).Order("month asc, day_of_week asc").Find(&entries).Error
	return entries, err
}

func (s *Store) GetAllSchedules() ([]models.ScheduleConfig, error) {
	var entries []models.ScheduleConfig
	err := s.db.Order("zone_name, month, day_of_week").Find(&entries).Error
	return entries, err
}

func (s *Store) CreateScheduleEntry(entry *models.ScheduleConfig) error {
	return s.db.Create(entry).Error
}

func (s *Store) DeleteScheduleByID(id uint) error {
	return s.db.Delete(&models.ScheduleConfig{}, id).Error
}

func (s *Store) LoadConfigSchedules(zoneSchedules []config.ZoneSchedule) error {
	for _, zs := range zoneSchedules {
		var entries []models.ScheduleConfig
		for _, se := range zs.Schedule {
			entries = append(entries, models.ScheduleConfig{
				ZoneName:  zs.ZoneName,
				DayOfWeek: se.DayOfWeek,
				Time:      se.Time,
				Duration:  se.Duration,
				Month:     0,
			})
		}
		for _, mo := range zs.MonthOverride {
			for _, se := range mo.Schedule {
				entries = append(entries, models.ScheduleConfig{
					ZoneName:  zs.ZoneName,
					DayOfWeek: se.DayOfWeek,
					Time:      se.Time,
					Duration:  se.Duration,
					Month:     mo.Month,
				})
			}
		}
		if len(entries) > 0 {
			if err := s.SaveSchedule(entries); err != nil {
				return err
			}
		}
	}
	return nil
}
