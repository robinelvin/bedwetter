package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type MQTTConfig struct {
	Broker   string `yaml:"broker"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type ZoneConfig struct {
	Name                  string `yaml:"name"`
	MoistureSensorTopic   string `yaml:"moisture_sensor_topic"`
	MoistureSensorEntity  string `yaml:"moisture_sensor_entity"`
	HumiditySensorTopic   string `yaml:"humidity_sensor_topic"`
	HumiditySensorEntity  string `yaml:"humidity_sensor_entity"`
	TemperatureSensorTopic   string `yaml:"temperature_sensor_topic"`
	TemperatureSensorEntity  string `yaml:"temperature_sensor_entity"`
	ValveCommandTopic     string `yaml:"valve_command_topic"`
	ValveStateTopic       string `yaml:"valve_state_topic"`
	ValveSwitchEntity     string `yaml:"valve_switch_entity"`
	ThresholdLow          int    `yaml:"threshold_low"`
	ThresholdHigh         int    `yaml:"threshold_high"`
	MaxWateringSeconds    int    `yaml:"max_watering_seconds"`
	MaxActivationsPerDay  int    `yaml:"max_activations_per_day"`
	CooldownMinutes       int    `yaml:"cooldown_minutes"`
}

type AlertsConfig struct {
	Email               string `yaml:"email"`
	StaleSensorMinutes  int    `yaml:"stale_sensor_minutes"`
	SMTPServer          string `yaml:"smtp_server"`
	SMTPPort            int    `yaml:"smtp_port"`
	SMTPUsername        string `yaml:"smtp_username"`
	SMTPPassword        string `yaml:"smtp_password"`
	FromEmail           string `yaml:"from_email"`
}

type ScheduleEntry struct {
	DayOfWeek string `yaml:"day_of_week"`
	Time      string `yaml:"time"`
	Duration  int    `yaml:"duration"`
}

type MonthOverride struct {
	Month    int             `yaml:"month"`
	Schedule []ScheduleEntry `yaml:"schedule"`
}

type ZoneSchedule struct {
	ZoneName      string          `yaml:"zone_name"`
	Schedule      []ScheduleEntry `yaml:"schedule"`
	MonthOverride []MonthOverride `yaml:"month_overrides"`
}

type WeatherConfig struct {
	APIKey string  `yaml:"api_key"`
	Lat    float64 `yaml:"lat"`
	Lon    float64 `yaml:"lon"`
}

type WebConfig struct {
	ListenAddr string `yaml:"listen_addr"`
}

type HomeAssistantConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type Config struct {
	MQTT          MQTTConfig           `yaml:"mqtt"`
	HomeAssistant HomeAssistantConfig  `yaml:"homeassistant"`
	Zones         []ZoneConfig         `yaml:"zones"`
	Alerts        AlertsConfig         `yaml:"alerts"`
	Schedules     []ZoneSchedule       `yaml:"schedules"`
	Weather       WeatherConfig        `yaml:"weather"`
	Web           WebConfig            `yaml:"web"`
	DBPath        string               `yaml:"db_path"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	cfg.Web.ListenAddr = ":8080"
	cfg.Alerts.SMTPPort = 587
	cfg.DBPath = "bedwetter.db"
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
