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
	cfg          *config.Config
	store        *store.Store
	zoneManager  *zones.Manager
	done         chan struct{}
	weatherCache *WeatherCache
}

type WeatherCache struct {
	Current      WeatherCurrent
	Hourly       []WeatherHourly
	Forecast     []WeatherForecastDay
	RainToday    bool
	RainTomorrow bool
	FetchedAt    time.Time
	TTL          time.Duration
}

type WeatherCurrent struct {
	Temperature   float64
	ApparentTemp  float64
	Precipitation float64
	WeatherCode   int
	WindSpeed     float64
}

type WeatherHourly struct {
	Time          string
	Temperature   float64
	Precipitation float64
	WeatherCode   int
}

type WeatherForecastDay struct {
	Date          string
	TempMax       float64
	TempMin       float64
	Precipitation float64
	WeatherCode   int
}

func (wc *WeatherCache) RainDetected() bool {
	return wc.RainToday || wc.RainTomorrow
}

func (wc *WeatherCache) UpcomingHours(n int) []WeatherHourly {
	result := make([]WeatherHourly, 0, n)
	now := time.Now()
	for _, h := range wc.Hourly {
		if len(result) >= n {
			break
		}
		t, err := time.Parse("2006-01-02T15:04", h.Time)
		if err != nil {
			continue
		}
		if t.Before(now.Add(-1 * time.Hour)) {
			continue
		}
		result = append(result, h)
	}
	return result
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

	s.CheckWeather()

	forecastRain := s.weatherCache.RainDetected() && time.Since(s.weatherCache.FetchedAt) < s.weatherCache.TTL
	s.zoneManager.SetForecastRain(forecastRain)

	if s.zoneManager.RainDetected() {
		log.Println("Schedule: rain sensor active, skipping all watering")
		return
	}

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
		scheduleMinute := zones.ParseTimeToMinutes(sc.Time)
		if scheduleMinute < 0 {
			continue
		}
		if currentMinute != scheduleMinute {
			continue
		}

		z := s.zoneManager.GetZone(sc.ZoneName)
		if z == nil {
			continue
		}

		multiplier := getSeasonalMultiplier(z.Config.SeasonalMultipliers, month)
		adjustedDuration := int(float64(sc.Duration) * multiplier)
		if adjustedDuration < 1 {
			adjustedDuration = sc.Duration
		}

		log.Printf("Schedule: evaluating %s (base: %ds, adjusted: %ds, multiplier: %.2f)",
			sc.ZoneName, sc.Duration, adjustedDuration, multiplier)

		s.zoneManager.TriggerScheduledWatering(sc.ZoneName, adjustedDuration)
	}
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

func (s *Scheduler) GetWeather() *WeatherCache {
	s.CheckWeather()
	return s.weatherCache
}

func (s *Scheduler) CheckWeather() {
	if s.cfg.Weather.Lat == 0 && s.cfg.Weather.Lon == 0 {
		return
	}
	if time.Since(s.weatherCache.FetchedAt) < s.weatherCache.TTL {
		return
	}

	wc, err := s.fetchOpenMeteo()
	if err != nil {
		log.Printf("Weather fetch failed: %v", err)
		return
	}

	s.weatherCache.Current = wc.Current
	s.weatherCache.Hourly = wc.Hourly
	s.weatherCache.Forecast = wc.Forecast
	s.weatherCache.RainToday = wc.RainToday
	s.weatherCache.RainTomorrow = wc.RainTomorrow
	s.weatherCache.FetchedAt = time.Now()
}

type rawOpenMeteoResponse struct {
	Current struct {
		Temperature2m      float64 `json:"temperature_2m"`
		ApparentTemperature float64 `json:"apparent_temperature"`
		Precipitation      float64 `json:"precipitation"`
		WeatherCode        int     `json:"weather_code"`
		WindSpeed10m       float64 `json:"wind_speed_10m"`
	} `json:"current"`
	Hourly struct {
		Time          []string  `json:"time"`
		Temperature2m []float64 `json:"temperature_2m"`
		Precipitation []float64 `json:"precipitation"`
		WeatherCode   []int     `json:"weather_code"`
	} `json:"hourly"`
	Daily struct {
		Time              []string  `json:"time"`
		Temperature2mMax  []float64 `json:"temperature_2m_max"`
		Temperature2mMin  []float64 `json:"temperature_2m_min"`
		PrecipitationSum  []float64 `json:"precipitation_sum"`
		WeatherCode       []int     `json:"weather_code"`
	} `json:"daily"`
}

func (s *Scheduler) fetchOpenMeteo() (*WeatherCache, error) {
	threshold := s.cfg.Weather.RainThresholdMm
	if threshold <= 0 {
		threshold = 5.0
	}

	u := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&current=temperature_2m,apparent_temperature,precipitation,weather_code,wind_speed_10m&hourly=temperature_2m,precipitation,weather_code&daily=temperature_2m_max,temperature_2m_min,precipitation_sum,weather_code&timezone=auto&forecast_days=3",
		s.cfg.Weather.Lat, s.cfg.Weather.Lon,
	)

	resp, err := http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("weather request failed: %w", err)
	}
	defer resp.Body.Close()

	var raw rawOpenMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("weather decode failed: %w", err)
	}

	wc := &WeatherCache{
		Current: WeatherCurrent{
			Temperature:   raw.Current.Temperature2m,
			ApparentTemp:  raw.Current.ApparentTemperature,
			Precipitation: raw.Current.Precipitation,
			WeatherCode:   raw.Current.WeatherCode,
			WindSpeed:     raw.Current.WindSpeed10m,
		},
	}

	today := time.Now().Format("2006-01-02")
	tomorrow := time.Now().Add(24 * time.Hour).Format("2006-01-02")

	for i, t := range raw.Hourly.Time {
		wh := WeatherHourly{
			Time:          t,
			Temperature:   raw.Hourly.Temperature2m[i],
			Precipitation: raw.Hourly.Precipitation[i],
			WeatherCode:   raw.Hourly.WeatherCode[i],
		}
		wc.Hourly = append(wc.Hourly, wh)

		datePart := t
		if len(t) >= 10 {
			datePart = t[:10]
		}

		if datePart == today && wh.Precipitation >= threshold {
			wc.RainToday = true
		}
		if datePart == tomorrow && wh.Precipitation >= threshold {
			wc.RainTomorrow = true
		}
	}

	for i, date := range raw.Daily.Time {
		fd := WeatherForecastDay{
			Date:          date,
			TempMax:       raw.Daily.Temperature2mMax[i],
			TempMin:       raw.Daily.Temperature2mMin[i],
			Precipitation: raw.Daily.PrecipitationSum[i],
			WeatherCode:   raw.Daily.WeatherCode[i],
		}
		wc.Forecast = append(wc.Forecast, fd)
	}

	return wc, nil
}
