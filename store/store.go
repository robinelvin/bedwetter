package store

import (
	"time"

	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/models"
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

func (s *Store) SaveSensorReading(zoneName string, moisture, humidity, temperature float64) error {
	return s.db.Create(&models.SensorReading{
		ZoneName:    zoneName,
		Moisture:    moisture,
		Humidity:    humidity,
		Temperature: temperature,
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
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
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

func (s *Store) GetAllZoneConfigs() ([]models.ZoneConfig, error) {
	var zones []models.ZoneConfig
	err := s.db.Order("name").Find(&zones).Error
	return zones, err
}

func (s *Store) GetZoneConfigByID(id uint) (*models.ZoneConfig, error) {
	var zc models.ZoneConfig
	err := s.db.First(&zc, id).Error
	if err != nil {
		return nil, err
	}
	return &zc, nil
}

func (s *Store) CreateZoneConfig(zc *models.ZoneConfig) error {
	return s.db.Create(zc).Error
}

func (s *Store) UpdateZoneConfig(id uint, zc *models.ZoneConfig) error {
	return s.db.Model(&models.ZoneConfig{}).Where("id = ?", id).Updates(zc).Error
}

func (s *Store) DeleteZoneConfig(id uint) error {
	return s.db.Delete(&models.ZoneConfig{}, id).Error
}

func (s *Store) GetMQTTConfig() (*models.MQTTConfig, error) {
	var cfg models.MQTTConfig
	err := s.db.First(&cfg, 1).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *Store) SaveMQTTConfig(cfg *models.MQTTConfig) error {
	cfg.ID = 1
	return s.db.Save(cfg).Error
}

func (s *Store) GetHAConfig() (*models.HAConfig, error) {
	var cfg models.HAConfig
	err := s.db.First(&cfg, 1).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *Store) SaveHAConfig(cfg *models.HAConfig) error {
	cfg.ID = 1
	return s.db.Save(cfg).Error
}

func (s *Store) GetAlertSettings() (*models.AlertSettings, error) {
	var cfg models.AlertSettings
	err := s.db.First(&cfg, 1).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *Store) SaveAlertSettings(cfg *models.AlertSettings) error {
	cfg.ID = 1
	return s.db.Save(cfg).Error
}

func (s *Store) GetNtfyConfig() (*models.NtfyConfig, error) {
	var cfg models.NtfyConfig
	err := s.db.First(&cfg, 1).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *Store) SaveNtfyConfig(cfg *models.NtfyConfig) error {
	cfg.ID = 1
	return s.db.Save(cfg).Error
}

func (s *Store) GetWeatherConfig() (*models.WeatherConfig, error) {
	var cfg models.WeatherConfig
	err := s.db.First(&cfg, 1).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *Store) SaveWeatherConfig(cfg *models.WeatherConfig) error {
	cfg.ID = 1
	return s.db.Save(cfg).Error
}

func (s *Store) GetMasterValveConfig() (*models.MasterValveConfig, error) {
	var cfg models.MasterValveConfig
	err := s.db.First(&cfg, 1).Error
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *Store) SaveMasterValveConfig(cfg *models.MasterValveConfig) error {
	cfg.ID = 1
	return s.db.Save(cfg).Error
}

func (s *Store) CreateEventLog(event *models.EventLog) error {
	return s.db.Create(event).Error
}

type EventLogPage struct {
	Events  []models.EventLog
	Total   int64
	Page    int
	PerPage int
	TotalPages int
}

func (s *Store) GetEventLogsByZone(zoneName string, page, perPage int) (*EventLogPage, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	var total int64
	s.db.Model(&models.EventLog{}).Where("zone_name = ?", zoneName).Count(&total)

	var events []models.EventLog
	err := s.db.Where("zone_name = ?", zoneName).
		Order("created_at desc").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}
	return &EventLogPage{
		Events:     events,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
	}, nil
}

func (s *Store) GetEventLogs(page, perPage int) (*EventLogPage, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 50
	}
	var total int64
	s.db.Model(&models.EventLog{}).Count(&total)

	var events []models.EventLog
	err := s.db.Order("created_at desc").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	totalPages := int(total) / perPage
	if int(total)%perPage > 0 {
		totalPages++
	}
	return &EventLogPage{
		Events:     events,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
	}, nil
}

func (s *Store) LoadConfigZones(yamlZones []config.ZoneConfig) error {
	var count int64
	s.db.Model(&models.ZoneConfig{}).Count(&count)
	if count > 0 {
		return nil
	}
	for _, z := range yamlZones {
		mz := &models.ZoneConfig{
			Name:                 z.Name,
			MoistureSensorTopic:  z.MoistureSensorTopic,
			MoistureSensorEntity: z.MoistureSensorEntity,
			ValveCommandTopic:    z.ValveCommandTopic,
			ValveStateTopic:      z.ValveStateTopic,
			ValveSwitchEntity:    z.ValveSwitchEntity,
			ThresholdLow:         z.ThresholdLow,
			ThresholdHigh:        z.ThresholdHigh,
			MaxWateringSeconds:   z.MaxWateringSeconds,
			MaxActivationsPerDay: z.MaxActivationsPerDay,
			CooldownMinutes:      z.CooldownMinutes,
		}
		mz.FromConfigZoneConfig(z)
		if err := s.db.Create(mz).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateUser(username, passwordHash string) error {
	return s.db.Create(&models.User{
		Username:     username,
		PasswordHash: passwordHash,
	}).Error
}

func (s *Store) GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	err := s.db.Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Store) CountUsers() (int64, error) {
	var count int64
	err := s.db.Model(&models.User{}).Count(&count).Error
	return count, err
}

func (s *Store) CreateSession(sessionID, username string) error {
	return s.db.Create(&models.Session{SessionID: sessionID, Username: username}).Error
}

func (s *Store) GetSessionByID(sessionID string) (string, error) {
	var session models.Session
	err := s.db.Where("session_id = ?", sessionID).First(&session).Error
	if err != nil {
		return "", err
	}
	return session.Username, nil
}

func (s *Store) DeleteSession(sessionID string) error {
	return s.db.Where("session_id = ?", sessionID).Delete(&models.Session{}).Error
}

func (s *Store) CountSessions() (int64, error) {
	var count int64
	err := s.db.Model(&models.Session{}).Count(&count).Error
	return count, err
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
