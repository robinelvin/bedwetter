package scheduler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/models"
	"github.com/robinelvin/bedwetter/store"
	"github.com/robinelvin/bedwetter/zones"
)

type Scheduler struct {
	cfg           *config.Config
	store         *store.Store
	zoneManager   *zones.Manager
	done          chan struct{}
	weatherCache  *WeatherCache
}

type WeatherCache struct {
	ForecastRain bool
	FetchedAt    time.Time
	TTL          time.Duration
}

func New(cfg *config.Config, store *store.Store, zoneManager *zones.Manager) *Scheduler {
	return &Scheduler{
		cfg:         cfg,
		store:       store,
		zoneManager: zoneManager,
		done:        make(chan struct{}),
		weatherCache: &WeatherCache{
			TTL: 30 * time.Minute,
		},
	}
}

func (s *Scheduler) Start() {
	go s.loop()
}

func (s *Scheduler) Stop() {
	close(s.done)
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.evaluate()
		}
	}
}

func (s *Scheduler) evaluate() {
	now := time.Now()
	weekday := now.Weekday().String()[:3]
	currentMinute := now.Hour()*60 + now.Minute()

	schedules, err := s.store.GetAllSchedules()
	if err != nil {
		log.Printf("Failed to load schedules: %v", err)
		return
	}

	month := int(now.Month())
	monthSchedules := make([]models.ScheduleConfig, 0)
	weekdaySchedules := make([]models.ScheduleConfig, 0)
	for _, sc := range schedules {
		if sc.Month == month {
			monthSchedules = append(monthSchedules, sc)
		} else if sc.Month == 0 && sc.DayOfWeek == weekday {
			weekdaySchedules = append(weekdaySchedules, sc)
		}
	}

	active := monthSchedules
	if len(active) == 0 {
		active = weekdaySchedules
	}

	for _, sc := range active {
		scheduleMinute := parseTimeToMinutes(sc.Time)
		if scheduleMinute < 0 {
			continue
		}
		if currentMinute == scheduleMinute {
			z := s.zoneManager.GetZone(sc.ZoneName)
			if z == nil {
				continue
			}

			if z.Moisture >= float64(z.Config.ThresholdLow) {
				log.Printf("Schedule: skipping %s, moisture %.1f%% above threshold %d%%",
					sc.ZoneName, z.Moisture, z.Config.ThresholdLow)
				continue
			}

			if s.weatherCache.ForecastRain && time.Since(s.weatherCache.FetchedAt) < s.weatherCache.TTL {
				log.Printf("Schedule: skipping %s, rain forecast active", sc.ZoneName)
				continue
			}

			log.Printf("Schedule: starting watering for %s (duration: %ds)", sc.ZoneName, sc.Duration)
			s.zoneManager.OpenValve(sc.ZoneName)
		}
	}
}

func parseTimeToMinutes(t string) int {
	tm, err := time.Parse("15:04", t)
	if err != nil {
		log.Printf("Invalid schedule time %q: %v", t, err)
		return -1
	}
	return tm.Hour()*60 + tm.Minute()
}

func (s *Scheduler) CheckWeather() {
	if s.cfg.Weather.APIKey == "" {
		return
	}
	if time.Since(s.weatherCache.FetchedAt) < s.weatherCache.TTL {
		return
	}

	forecast, err := s.fetchWeather()
	if err != nil {
		log.Printf("Weather fetch failed: %v", err)
		return
	}

	s.weatherCache.ForecastRain = forecast
	s.weatherCache.FetchedAt = time.Now()
}

type OWMResponse struct {
	Hourly []struct {
		Weather []struct {
			Main string `json:"main"`
		} `json:"weather"`
	} `json:"hourly"`
}

func (s *Scheduler) fetchWeather() (bool, error) {
	u := fmt.Sprintf(
		"https://api.openweathermap.org/data/3.0/onecall?lat=%f&lon=%f&exclude=current,minutely,daily,alerts&appid=%s",
		s.cfg.Weather.Lat, s.cfg.Weather.Lon, url.QueryEscape(s.cfg.Weather.APIKey),
	)

	resp, err := http.Get(u)
	if err != nil {
		return false, fmt.Errorf("weather request failed: %w", err)
	}
	defer resp.Body.Close()

	var owm OWMResponse
	if err := json.NewDecoder(resp.Body).Decode(&owm); err != nil {
		return false, fmt.Errorf("weather decode failed: %w", err)
	}

	for _, h := range owm.Hourly {
		for _, w := range h.Weather {
			if w.Main == "Rain" || w.Main == "Drizzle" || w.Main == "Thunderstorm" {
				return true, nil
			}
		}
	}
	return false, nil
}
