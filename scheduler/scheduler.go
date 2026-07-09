package scheduler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/models"
	"github.com/robinelvin/bedwetter/store"
	"github.com/robinelvin/bedwetter/zones"
)

type Scheduler struct {
	cfg         *config.Config
	store       *store.Store
	zoneManager *zones.Manager
	done        chan struct{}
	weatherCache *WeatherCache
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

	if s.zoneManager.RainDetected() {
		log.Println("Schedule: rain sensor active, skipping all watering")
		return
	}

	s.CheckWeather()

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

			if !isWithinTimeWindow(z.Config.EarliestWateringTime, z.Config.LatestWateringTime, now) {
				log.Printf("Schedule: skipping %s, outside watering window (%s-%s)",
					sc.ZoneName, z.Config.EarliestWateringTime, z.Config.LatestWateringTime)
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

			multiplier := getSeasonalMultiplier(z.Config.SeasonalMultipliers, month)
			adjustedDuration := int(float64(sc.Duration) * multiplier)
			if adjustedDuration < 1 {
				adjustedDuration = sc.Duration
			}

			log.Printf("Schedule: starting watering for %s (base: %ds, adjusted: %ds, multiplier: %.2f)",
				sc.ZoneName, sc.Duration, adjustedDuration, multiplier)

			z.Config.MaxWateringSeconds = adjustedDuration
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

func isWithinTimeWindow(earliest, latest string, now time.Time) bool {
	if earliest == "" {
		earliest = "06:00"
	}
	if latest == "" {
		latest = "10:00"
	}

	earliestMin := parseTimeToMinutes(earliest)
	latestMin := parseTimeToMinutes(latest)
	if earliestMin < 0 || latestMin < 0 {
		return true
	}

	currentMin := now.Hour()*60 + now.Minute()
	return currentMin >= earliestMin && currentMin <= latestMin
}

func getSeasonalMultiplier(multipliers map[int]float64, month int) float64 {
	if multipliers == nil {
		return 1.0
	}
	m, ok := multipliers[month]
	if !ok {
		return 1.0
	}
	return m
}

func (s *Scheduler) CheckWeather() {
	if s.cfg.Weather.Lat == 0 && s.cfg.Weather.Lon == 0 {
		return
	}
	if time.Since(s.weatherCache.FetchedAt) < s.weatherCache.TTL {
		return
	}

	rain, err := s.fetchOpenMeteo()
	if err != nil {
		log.Printf("Weather fetch failed: %v", err)
		return
	}

	s.weatherCache.ForecastRain = rain
	s.weatherCache.FetchedAt = time.Now()
}

type OpenMeteoResponse struct {
	Daily struct {
		Time              []string  `json:"time"`
		PrecipitationSum  []float64 `json:"precipitation_sum"`
	} `json:"daily"`
}

func (s *Scheduler) fetchOpenMeteo() (bool, error) {
	threshold := s.cfg.Weather.RainThresholdMm
	if threshold <= 0 {
		threshold = 5.0
	}

	u := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&daily=precipitation_sum&timezone=auto&forecast_days=2",
		s.cfg.Weather.Lat, s.cfg.Weather.Lon,
	)

	resp, err := http.Get(u)
	if err != nil {
		return false, fmt.Errorf("weather request failed: %w", err)
	}
	defer resp.Body.Close()

	var om OpenMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&om); err != nil {
		return false, fmt.Errorf("weather decode failed: %w", err)
	}

	for _, precip := range om.Daily.PrecipitationSum {
		if precip >= threshold {
			return true, nil
		}
	}
	return false, nil
}
