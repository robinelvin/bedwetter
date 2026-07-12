package zones

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/ha"
	"github.com/robinelvin/bedwetter/models"
	"github.com/robinelvin/bedwetter/mqtt"
	"github.com/robinelvin/bedwetter/store"
)

type ZoneState int

const (
	StateIdle ZoneState = iota
	StateWatering
	StateCooldown
	StateManualOpen
	StateFailsafe
	StateForceClosed
)

type Zone struct {
	Config           config.ZoneConfig
	Moisture         float64
	Humidity         float64
	Temperature      float64
	State            ZoneState
	LastMoistureTime time.Time
	LastWaterEnd     time.Time
	LastStateChange  time.Time
	WateringStarted  time.Time
	mu               sync.RWMutex
}

type ZoneSnapshot struct {
	Config           config.ZoneConfig
	Moisture         float64
	Humidity         float64
	Temperature      float64
	State            ZoneState
	LastMoistureTime time.Time
	LastWaterEnd     time.Time
	LastStateChange  time.Time
	WateringStarted  time.Time
}

type Manager struct {
	zones             map[string]*Zone
	client            mqtt.ClientInterface
	store             *store.Store
	cfg               *config.Config
	resolver          *ha.EntityResolver
	haAPI             *ha.APIClient
	mu                sync.RWMutex
	done              chan struct{}
	rainMu            sync.RWMutex
	rainDetected      bool
	forecastRainActive bool
	sendNtfy          func(level, title, message string)
}

func NewManager(cfg *config.Config, client mqtt.ClientInterface, store *store.Store, resolver *ha.EntityResolver, haAPI *ha.APIClient) *Manager {
	m := &Manager{
		zones:    make(map[string]*Zone),
		client:   client,
		store:    store,
		cfg:      cfg,
		resolver: resolver,
		haAPI:    haAPI,
		done:     make(chan struct{}),
	}
	for _, zc := range cfg.Zones {
		m.zones[zc.Name] = &Zone{
			Config:      zc,
			State:       StateIdle,
			Moisture:    math.NaN(),
			Humidity:    math.NaN(),
			Temperature: math.NaN(),
		}
	}
	return m
}

func (m *Manager) SetNtfySender(fn func(level, title, message string)) {
	m.sendNtfy = fn
}

func (m *Manager) Start() {
	if m.resolver != nil {
		m.resolver.OnResolved(func(zoneName string) {
			if zoneName == "__rain__" {
				log.Println("Rain sensor HA entity resolved, re-subscribing")
				m.subscribeRainSensor()
				return
			}
			m.mu.RLock()
			z, ok := m.zones[zoneName]
			m.mu.RUnlock()
			if !ok {
				return
			}
			log.Printf("Zone %q: HA entity resolved, subscribing to sensor/valve topics", zoneName)
			m.subscribeSensor(z)
			m.subscribeValveState(z)
		})
		for _, z := range m.zones {
			ha.ResolveZoneAsync(m.resolver, &z.Config)
		}
		if m.cfg.Weather.RainSensorEntity != "" && m.cfg.Weather.RainSensorTopic == "" {
			m.resolver.ResolveEntity("__rain__", m.cfg.Weather.RainSensorEntity)
		}
	}

	for _, z := range m.zones {
		m.subscribeSensor(z)
		m.subscribeHumidity(z)
		m.subscribeTemperature(z)
		m.subscribeValveState(z)
		m.watchHAEntity(z)
		m.watchHAHumidity(z)
		m.watchHATemperature(z)
	}

	m.subscribeRainSensor()
	m.watchHARainSensor()

	go m.syncHAValveStates()
	go m.watchdogLoop()
}

func (m *Manager) syncHAValveStates() {
	if m.haAPI == nil {
		return
	}
	m.pollHAValveStates()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.pollHAValveStates()
		}
	}
}

func (m *Manager) pollHAValveStates() {
	for _, z := range m.zones {
		if z.Config.ValveSwitchEntity == "" {
			continue
		}
		state, err := m.haAPI.GetEntityState(z.Config.ValveSwitchEntity)
		if err != nil {
			log.Printf("Zone %q: failed to fetch valve switch state: %v", z.Config.Name, err)
			continue
		}
		z.mu.Lock()
		if state == "on" || state == "open" {
			if z.State == StateIdle || z.State == StateCooldown {
				z.State = StateManualOpen
				z.LastStateChange = time.Now()
				log.Printf("Zone %q: synced valve state from HA = %s", z.Config.Name, state)
			}
		} else if z.State == StateManualOpen || z.State == StateWatering {
			z.State = StateIdle
			z.LastWaterEnd = time.Now()
			z.LastStateChange = time.Now()
			log.Printf("Zone %q: synced valve state from HA = %s", z.Config.Name, state)
		}
		z.mu.Unlock()
	}
}

func (m *Manager) watchHAEntity(z *Zone) {
	if m.haAPI == nil {
		return
	}
	entityID := z.Config.MoistureSensorEntity
	if entityID == "" || z.Config.MoistureSensorTopic != "" {
		return
	}
	log.Printf("Zone %q: watching HA entity %s via API", z.Config.Name, entityID)
	m.haAPI.Watch(entityID, func(eid string, value float64) {
		z.mu.Lock()
		z.Moisture = value
		z.LastMoistureTime = time.Now()
		z.mu.Unlock()
		log.Printf("Zone %q: HA API update %s = %.1f%%", z.Config.Name, eid, value)
		if err := m.store.SaveSensorReading(z.Config.Name, value, z.Humidity, z.Temperature); err != nil {
			log.Printf("Failed to save sensor reading: %v", err)
		}
		m.evaluateZone(z.Config.Name)
	})
}

func (m *Manager) watchHAHumidity(z *Zone) {
	if m.haAPI == nil {
		return
	}
	entityID := z.Config.HumiditySensorEntity
	if entityID == "" || z.Config.HumiditySensorTopic != "" {
		return
	}
	log.Printf("Zone %q: watching HA humidity entity %s via API", z.Config.Name, entityID)
	m.haAPI.Watch(entityID, func(eid string, value float64) {
		z.mu.Lock()
		z.Humidity = value
		z.mu.Unlock()
		log.Printf("Zone %q: HA API humidity update %s = %.1f%%", z.Config.Name, eid, value)
	})
}

func (m *Manager) watchHATemperature(z *Zone) {
	if m.haAPI == nil {
		return
	}
	entityID := z.Config.TemperatureSensorEntity
	if entityID == "" || z.Config.TemperatureSensorTopic != "" {
		return
	}
	log.Printf("Zone %q: watching HA temperature entity %s via API", z.Config.Name, entityID)
	m.haAPI.Watch(entityID, func(eid string, value float64) {
		z.mu.Lock()
		z.Temperature = value
		z.mu.Unlock()
		log.Printf("Zone %q: HA API temperature update %s = %.1f", z.Config.Name, eid, value)
	})
}

func (m *Manager) subscribeSensor(z *Zone) {
	topic := z.Config.MoistureSensorTopic
	if topic == "" && z.Config.MoistureSensorEntity != "" && m.resolver != nil {
		topics := m.resolver.GetTopics(z.Config.MoistureSensorEntity)
		if topics != nil {
			topic = topics.StateTopic
		}
	}
	if topic == "" {
		return
	}
	log.Printf("Zone %q: subscribing to sensor topic %s", z.Config.Name, topic)
	if err := m.client.Subscribe(topic, 0, func(t string, p []byte) {
		m.handleSensorReading(z.Config.Name, p)
	}); err != nil {
		log.Printf("Zone %q: failed to subscribe to sensor topic %s: %v", z.Config.Name, topic, err)
	}
}

func (m *Manager) subscribeHumidity(z *Zone) {
	topic := z.Config.HumiditySensorTopic
	if topic == "" && z.Config.HumiditySensorEntity != "" && m.resolver != nil {
		topics := m.resolver.GetTopics(z.Config.HumiditySensorEntity)
		if topics != nil {
			topic = topics.StateTopic
		}
	}
	if topic == "" {
		return
	}
	log.Printf("Zone %q: subscribing to humidity topic %s", z.Config.Name, topic)
	if err := m.client.Subscribe(topic, 0, func(t string, p []byte) {
		m.handleHumidityReading(z.Config.Name, p)
	}); err != nil {
		log.Printf("Zone %q: failed to subscribe to humidity topic %s: %v", z.Config.Name, topic, err)
	}
}

func (m *Manager) subscribeTemperature(z *Zone) {
	topic := z.Config.TemperatureSensorTopic
	if topic == "" && z.Config.TemperatureSensorEntity != "" && m.resolver != nil {
		topics := m.resolver.GetTopics(z.Config.TemperatureSensorEntity)
		if topics != nil {
			topic = topics.StateTopic
		}
	}
	if topic == "" {
		return
	}
	log.Printf("Zone %q: subscribing to temperature topic %s", z.Config.Name, topic)
	if err := m.client.Subscribe(topic, 0, func(t string, p []byte) {
		m.handleTemperatureReading(z.Config.Name, p)
	}); err != nil {
		log.Printf("Zone %q: failed to subscribe to temperature topic %s: %v", z.Config.Name, topic, err)
	}
}

func (m *Manager) subscribeValveState(z *Zone) {
	topic := z.Config.ValveStateTopic
	if topic == "" && z.Config.ValveSwitchEntity != "" && m.resolver != nil {
		topics := m.resolver.GetTopics(z.Config.ValveSwitchEntity)
		if topics != nil {
			topic = topics.StateTopic
			if z.Config.ValveCommandTopic == "" {
				z.Config.ValveCommandTopic = topics.CommandTopic
			}
		}
	}
	if topic == "" {
		return
	}
	log.Printf("Zone %q: subscribing to valve state topic %s", z.Config.Name, topic)
	if err := m.client.Subscribe(topic, 0, func(t string, p []byte) {
		m.handleValveState(z.Config.Name, p)
	}); err != nil {
		log.Printf("Zone %q: failed to subscribe to valve state topic %s: %v", z.Config.Name, topic, err)
	}
}

func (m *Manager) setRainDetected(detected bool, source string) {
	m.rainMu.Lock()
	was := m.rainDetected
	m.rainDetected = detected
	m.rainMu.Unlock()

	if detected && !was {
		log.Printf("Rain detected via %s, closing all valves", source)
		m.CloseAllValves()
	} else if !detected && was {
		log.Printf("Rain cleared via %s", source)
	}
}

func (m *Manager) subscribeRainSensor() {
	topic := m.cfg.Weather.RainSensorTopic
	entity := m.cfg.Weather.RainSensorEntity

	// If entity is set but no topic, try to resolve via HA discovery
	if topic == "" && entity != "" && m.resolver != nil {
		topics := m.resolver.GetTopics(entity)
		if topics != nil {
			topic = topics.StateTopic
		}
	}

	if topic != "" {
		log.Printf("Subscribing to rain sensor topic %s", topic)
		if err := m.client.Subscribe(topic, 0, func(t string, p []byte) {
			val := strings.TrimSpace(string(p))
			detected := val == "1" || strings.EqualFold(val, "ON") || strings.EqualFold(val, "true")
			m.setRainDetected(detected, "MQTT topic "+t)
		}); err != nil {
			log.Printf("Failed to subscribe to rain sensor topic %s: %v", topic, err)
		}
	}
}

func (m *Manager) watchHARainSensor() {
	entity := m.cfg.Weather.RainSensorEntity
	if entity == "" || m.haAPI == nil || m.cfg.Weather.RainSensorTopic != "" {
		return
	}
	// Only poll via HA API if we don't have a direct MQTT topic subscription
	log.Printf("Watching rain sensor HA entity %s via API", entity)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-m.done:
				return
			case <-ticker.C:
				state, err := m.haAPI.GetEntityState(entity)
				if err != nil {
					log.Printf("Rain sensor HA poll failed for %s: %v", entity, err)
					continue
				}
				detected := state == "on" || state == "true" || state == "1"
				m.setRainDetected(detected, "HA entity "+entity)
			}
		}
	}()
}

func (m *Manager) RainDetected() bool {
	m.rainMu.RLock()
	defer m.rainMu.RUnlock()
	return m.rainDetected
}

func (m *Manager) SetForecastRain(active bool) {
	m.rainMu.Lock()
	defer m.rainMu.Unlock()
	m.forecastRainActive = active
}

func (m *Manager) Stop() {
	close(m.done)
	for _, z := range m.zones {
		z.mu.Lock()
		if z.State == StateWatering || z.State == StateManualOpen {
			z.State = StateFailsafe
			z.LastStateChange = time.Now()
		}
		z.mu.Unlock()
		m.CloseValve(z.Config.Name)
	}
	m.closeMasterValve()
}

func (m *Manager) handleSensorReading(zoneName string, payload []byte) {
	m.mu.RLock()
	z, ok := m.zones[zoneName]
	m.mu.RUnlock()
	if !ok {
		return
	}
	val := strings.TrimSpace(string(payload))
	moisture, err := strconv.ParseFloat(val, 64)
	if err != nil {
		log.Printf("Invalid moisture reading %q for zone %s: %v", val, zoneName, err)
		return
	}
	if math.IsNaN(moisture) || moisture < 0 {
		moisture = 0
	}
	if moisture > 100 {
		moisture = 100
	}

	z.mu.Lock()
	z.Moisture = moisture
	z.LastMoistureTime = time.Now()
	z.mu.Unlock()

	if err := m.store.SaveSensorReading(zoneName, moisture, z.Humidity, z.Temperature); err != nil {
		log.Printf("Failed to save sensor reading: %v", err)
	}

	m.evaluateZone(zoneName)
}

func (m *Manager) handleHumidityReading(zoneName string, payload []byte) {
	m.mu.RLock()
	z, ok := m.zones[zoneName]
	m.mu.RUnlock()
	if !ok {
		return
	}
	val := strings.TrimSpace(string(payload))
	humidity, err := strconv.ParseFloat(val, 64)
	if err != nil {
		log.Printf("Invalid humidity reading %q for zone %s: %v", val, zoneName, err)
		return
	}
	if math.IsNaN(humidity) || humidity < 0 {
		humidity = 0
	}
	if humidity > 100 {
		humidity = 100
	}

	z.mu.Lock()
	z.Humidity = humidity
	z.mu.Unlock()
	log.Printf("Zone %s: humidity update = %.1f%%", zoneName, humidity)
}

func (m *Manager) handleTemperatureReading(zoneName string, payload []byte) {
	m.mu.RLock()
	z, ok := m.zones[zoneName]
	m.mu.RUnlock()
	if !ok {
		return
	}
	val := strings.TrimSpace(string(payload))
	temp, err := strconv.ParseFloat(val, 64)
	if err != nil {
		log.Printf("Invalid temperature reading %q for zone %s: %v", val, zoneName, err)
		return
	}
	if math.IsNaN(temp) {
		temp = 0
	}

	z.mu.Lock()
	z.Temperature = temp
	z.mu.Unlock()
	log.Printf("Zone %s: temperature update = %.1f", zoneName, temp)
}

func (m *Manager) handleValveState(zoneName string, payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	z, ok := m.zones[zoneName]
	if !ok {
		return
	}
	val := strings.TrimSpace(string(payload))
	state := strings.ToLower(val)

	z.mu.Lock()
	defer z.mu.Unlock()

	if state == "on" || state == "open" || state == "true" || state == "1" {
		if z.State == StateIdle || z.State == StateCooldown {
			z.State = StateManualOpen
			z.LastStateChange = time.Now()
			m.LogEvent("info", "valve", "Valve manually opened: "+zoneName, zoneName)
			if m.sendNtfy != nil {
				go m.sendNtfy("info", "Valve Opened", fmt.Sprintf("Zone '%s' valve has been manually opened", zoneName))
			}
		}
	} else {
		if z.State == StateManualOpen || z.State == StateWatering {
			z.State = StateIdle
			z.LastWaterEnd = time.Now()
			z.LastStateChange = time.Now()
			m.LogEvent("info", "valve", "Valve manually closed: "+zoneName, zoneName)
			if m.sendNtfy != nil {
				go m.sendNtfy("info", "Valve Closed", fmt.Sprintf("Zone '%s' valve has been manually closed", zoneName))
			}
			go func() {
				m.store.SaveValveEvent(zoneName, "close", 0)
			}()
		}
	}
}

func (m *Manager) evaluateZone(zoneName string) {
	m.mu.RLock()
	z, ok := m.zones[zoneName]
	m.mu.RUnlock()
	if !ok {
		return
	}

	z.mu.Lock()
	defer z.mu.Unlock()

	if z.State == StateManualOpen || z.State == StateFailsafe || z.State == StateWatering || z.State == StateForceClosed {
		if z.State == StateWatering {
			if z.Config.ThresholdHigh > 0 && z.Moisture >= float64(z.Config.ThresholdHigh) {
				log.Printf("Zone %s: moisture %.1f%% reached target %d%%, closing valve", zoneName, z.Moisture, z.Config.ThresholdHigh)
				m.LogEvent("info", "valve", fmt.Sprintf("Target moisture reached for %s (%.1f%% >= %d%%), closing valve", zoneName, z.Moisture, z.Config.ThresholdHigh), zoneName)
				go m.CloseValve(zoneName)
				return
			}
			elapsed := time.Since(z.WateringStarted)
			maxDur := time.Duration(z.Config.MaxWateringSeconds) * time.Second
			if elapsed >= maxDur {
				log.Printf("Zone %s: max watering duration reached (%ds) — safety shutoff", zoneName, z.Config.MaxWateringSeconds)
				m.LogEvent("error", "valve", "Safety shutoff: max duration exceeded for "+zoneName, zoneName)
				if m.sendNtfy != nil {
					go m.sendNtfy("alarm", "Safety Shutoff", fmt.Sprintf("Zone '%s': valve open too long", zoneName))
				}
				z.State = StateFailsafe
				z.LastStateChange = time.Now()
				go m.closeMasterValve()
				go m.CloseAllValves()
			}
			return
		}
		if z.State == StateManualOpen {
			if z.Config.ThresholdHigh > 0 && z.Moisture >= float64(z.Config.ThresholdHigh) {
				log.Printf("Zone %s: moisture %.1f%% reached target %d%%, closing manually-opened valve", zoneName, z.Moisture, z.Config.ThresholdHigh)
				m.LogEvent("info", "valve", fmt.Sprintf("Target moisture reached for %s (%.1f%% >= %d%%), closing valve", zoneName, z.Moisture, z.Config.ThresholdHigh), zoneName)
				go m.CloseValve(zoneName)
				z.State = StateIdle
				z.LastWaterEnd = time.Now()
				z.LastStateChange = time.Now()
			}
			return
		}
		return
	}

	if z.State == StateCooldown {
		cooldown := time.Duration(z.Config.CooldownMinutes) * time.Minute
		if time.Since(z.LastWaterEnd) >= cooldown {
			z.State = StateIdle
			z.LastStateChange = time.Now()
		} else {
			return
		}
	}

	if !IsWithinWateringWindow(z.Config.EarliestWateringTime, z.Config.LatestWateringTime, time.Now()) {
		log.Printf("Zone %q: outside watering window (%s-%s)", zoneName, z.Config.EarliestWateringTime, z.Config.LatestWateringTime)
		return
	}

	if math.IsNaN(z.Moisture) {
		return
	}

	staleThreshold := time.Duration(m.cfg.Alerts.StaleSensorMinutes) * time.Minute
	if m.cfg.Alerts.StaleSensorMinutes > 0 && (z.LastMoistureTime.IsZero() || time.Since(z.LastMoistureTime) > staleThreshold) {
		log.Printf("Zone %q: skipping evaluation, sensor reading stale", zoneName)
		return
	}

	if m.RainDetected() {
		return
	}
	m.rainMu.RLock()
	forecastRain := m.forecastRainActive
	m.rainMu.RUnlock()
	if forecastRain {
		return
	}

	if z.Moisture >= float64(z.Config.ThresholdLow) {
		return
	}

	count, err := m.store.ActivationsToday(zoneName)
	if err != nil {
		log.Printf("Error checking activations for %s: %v", zoneName, err)
		return
	}
	if z.Config.MaxActivationsPerDay > 0 && count >= int64(z.Config.MaxActivationsPerDay) {
		log.Printf("Zone %s: max daily activations reached (%d)", zoneName, z.Config.MaxActivationsPerDay)
		return
	}

	log.Printf("Zone %s: moisture %.1f%% below threshold %d%%, opening valve", zoneName, z.Moisture, z.Config.ThresholdLow)
	m.LogEvent("info", "valve", fmt.Sprintf("Watering started: %s (moisture %.1f%% below threshold %d%%)", zoneName, z.Moisture, z.Config.ThresholdLow), zoneName)
	go m.openValveIO(zoneName)
	go m.openMasterValve()
	z.State = StateWatering
	z.WateringStarted = time.Now()
	z.LastStateChange = time.Now()
	go func() {
		m.store.SaveValveEvent(zoneName, "open", z.Config.MaxWateringSeconds)
	}()
}

func (m *Manager) TriggerScheduledWatering(zoneName string, adjustedDuration int) {
	m.mu.RLock()
	z, ok := m.zones[zoneName]
	m.mu.RUnlock()
	if !ok {
		return
	}

	z.mu.Lock()
	defer z.mu.Unlock()

	if z.State == StateForceClosed || z.State == StateFailsafe || z.State == StateManualOpen || z.State == StateWatering {
		return
	}

	if z.State == StateCooldown {
		cooldown := time.Duration(z.Config.CooldownMinutes) * time.Minute
		if time.Since(z.LastWaterEnd) >= cooldown {
			z.State = StateIdle
			z.LastStateChange = time.Now()
		} else {
			return
		}
	}

	if !IsWithinWateringWindow(z.Config.EarliestWateringTime, z.Config.LatestWateringTime, time.Now()) {
		log.Printf("Schedule: skipping %s, outside watering window (%s-%s)", zoneName, z.Config.EarliestWateringTime, z.Config.LatestWateringTime)
		return
	}

	if math.IsNaN(z.Moisture) {
		return
	}

	staleThreshold := time.Duration(m.cfg.Alerts.StaleSensorMinutes) * time.Minute
	if m.cfg.Alerts.StaleSensorMinutes > 0 && (z.LastMoistureTime.IsZero() || time.Since(z.LastMoistureTime) > staleThreshold) {
		log.Printf("Schedule: skipping %s, sensor reading stale", zoneName)
		return
	}

	if m.RainDetected() {
		log.Printf("Schedule: skipping %s, rain sensor active", zoneName)
		return
	}
	m.rainMu.RLock()
	forecastRain := m.forecastRainActive
	m.rainMu.RUnlock()
	if forecastRain {
		log.Printf("Schedule: skipping %s, rain forecast active", zoneName)
		return
	}

	if z.Config.ThresholdHigh > 0 && z.Moisture >= float64(z.Config.ThresholdHigh) {
		log.Printf("Schedule: skipping %s, moisture %.1f%% above threshold_high %d%%", zoneName, z.Moisture, z.Config.ThresholdHigh)
		return
	}

	count, err := m.store.ActivationsToday(zoneName)
	if err != nil {
		log.Printf("Schedule: error checking activations for %s: %v", zoneName, err)
		return
	}
	if z.Config.MaxActivationsPerDay > 0 && count >= int64(z.Config.MaxActivationsPerDay) {
		log.Printf("Schedule: skipping %s, max daily activations reached (%d)", zoneName, z.Config.MaxActivationsPerDay)
		return
	}

	log.Printf("Schedule: starting watering for %s (duration: %ds)", zoneName, adjustedDuration)
	m.LogEvent("info", "valve", fmt.Sprintf("Watering started: %s (schedule, %ds)", zoneName, adjustedDuration), zoneName)
	go m.openValveIO(zoneName)
	go m.openMasterValve()
	z.Config.MaxWateringSeconds = adjustedDuration
	z.State = StateWatering
	z.WateringStarted = time.Now()
	z.LastStateChange = time.Now()
	go func() {
		m.store.SaveValveEvent(zoneName, "open", adjustedDuration)
	}()
}

func (m *Manager) ForceClose(zoneName string) {
	m.CloseValve(zoneName)
	z, ok := m.zones[zoneName]
	if !ok {
		return
	}
	z.mu.Lock()
	z.State = StateForceClosed
	z.LastStateChange = time.Now()
	z.mu.Unlock()
	m.LogEvent("warn", "valve", "Force-close activated: "+zoneName, zoneName)
	if m.sendNtfy != nil {
		go m.sendNtfy("warn", "Force Close", fmt.Sprintf("Zone '%s' force-closed by user", zoneName))
	}
}

func (m *Manager) ClearForceClose(zoneName string) {
	z, ok := m.zones[zoneName]
	if !ok {
		return
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.State == StateForceClosed {
		z.State = StateIdle
		z.LastStateChange = time.Now()
		m.LogEvent("info", "valve", "Force-close cleared: "+zoneName, zoneName)
	}
}

func (m *Manager) AcknowledgeFault(zoneName string) {
	z, ok := m.zones[zoneName]
	if !ok {
		return
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.State == StateFailsafe {
		z.State = StateIdle
		z.LastStateChange = time.Now()
		m.LogEvent("info", "system", "Failsafe acknowledged: "+zoneName, zoneName)
	}
}

func (m *Manager) openValveIO(zoneName string) {
	m.mu.RLock()
	z, ok := m.zones[zoneName]
	m.mu.RUnlock()
	if !ok {
		return
	}
	topic := z.Config.ValveCommandTopic
	if topic != "" {
		m.client.Publish(topic, 1, false, "ON")
	} else if m.haAPI != nil && z.Config.ValveSwitchEntity != "" {
		entityID := z.Config.ValveSwitchEntity
		parts := splitEntityID(entityID)
		if parts != nil {
			go func() {
				if err := m.haAPI.CallService(parts[0], "turn_on", entityID); err != nil {
					log.Printf("Zone %q: HA API turn_on failed for %s: %v", zoneName, entityID, err)
				} else {
					log.Printf("Zone %q: HA API turn_on %s", zoneName, entityID)
				}
			}()
		}
	}
}

func (m *Manager) openMasterValve() {
	topic := m.cfg.MasterValve.CommandTopic
	if topic != "" {
		m.client.Publish(topic, 1, false, "ON")
	} else if m.haAPI != nil && m.cfg.MasterValve.SwitchEntity != "" {
		entityID := m.cfg.MasterValve.SwitchEntity
		parts := splitEntityID(entityID)
		if parts != nil {
			go func() {
				if err := m.haAPI.CallService(parts[0], "turn_on", entityID); err != nil {
					log.Printf("Master valve: HA API turn_on failed for %s: %v", entityID, err)
				} else {
					log.Printf("Master valve: HA API turn_on %s", entityID)
				}
			}()
		}
	}
}

func (m *Manager) closeMasterValve() {
	topic := m.cfg.MasterValve.CommandTopic
	if topic != "" {
		m.client.Publish(topic, 1, false, "OFF")
	} else if m.haAPI != nil && m.cfg.MasterValve.SwitchEntity != "" {
		entityID := m.cfg.MasterValve.SwitchEntity
		parts := splitEntityID(entityID)
		if parts != nil {
			go func() {
				if err := m.haAPI.CallService(parts[0], "turn_off", entityID); err != nil {
					log.Printf("Master valve: HA API turn_off failed for %s: %v", entityID, err)
				} else {
					log.Printf("Master valve: HA API turn_off %s", entityID)
				}
			}()
		}
	}
}

func (m *Manager) OpenValve(zoneName string) {
	m.openValveIO(zoneName)
	z, ok := m.zones[zoneName]
	if !ok {
		return
	}
	z.mu.Lock()
	if z.State == StateIdle || z.State == StateCooldown {
		z.State = StateManualOpen
		z.LastStateChange = time.Now()
	}
	z.mu.Unlock()
	m.LogEvent("info", "valve", "Valve manually opened: "+zoneName, zoneName)
	if m.sendNtfy != nil {
		go m.sendNtfy("info", "Valve Opened", fmt.Sprintf("Zone '%s' valve has been manually opened", zoneName))
	}
}

func (m *Manager) CloseValve(zoneName string) {
	m.mu.RLock()
	z, ok := m.zones[zoneName]
	m.mu.RUnlock()
	if !ok {
		return
	}
	topic := z.Config.ValveCommandTopic
	if topic != "" {
		m.client.Publish(topic, 1, false, "OFF")
	} else if m.haAPI != nil && z.Config.ValveSwitchEntity != "" {
		entityID := z.Config.ValveSwitchEntity
		parts := splitEntityID(entityID)
		if parts != nil {
			go func() {
				if err := m.haAPI.CallService(parts[0], "turn_off", entityID); err != nil {
					log.Printf("Zone %q: HA API turn_off failed for %s: %v", zoneName, entityID, err)
				} else {
					log.Printf("Zone %q: HA API turn_off %s", zoneName, entityID)
				}
			}()
		}
	}
	z.mu.Lock()
	if z.State == StateWatering {
		z.State = StateCooldown
		z.LastWaterEnd = time.Now()
		z.LastStateChange = time.Now()
	} else if z.State == StateManualOpen {
		z.State = StateIdle
		z.LastWaterEnd = time.Now()
		z.LastStateChange = time.Now()
	}
	z.mu.Unlock()
	m.LogEvent("info", "valve", "Valve closed: "+zoneName, zoneName)
	if m.sendNtfy != nil {
		go m.sendNtfy("info", "Valve Closed", fmt.Sprintf("Zone '%s' valve has been closed", zoneName))
	}
}

// ParseTimeToMinutes converts a "HH:MM" time string to minutes since midnight.
// Returns -1 on parse error.
func ParseTimeToMinutes(t string) int {
	tm, err := time.Parse("15:04", t)
	if err != nil {
		return -1
	}
	return tm.Hour()*60 + tm.Minute()
}

// IsWithinWateringWindow reports whether the current time falls within the
// configured earliest-to-latest watering window. Defaults to 06:00-10:00
// when either bound is empty. Returns true (permissive) on parse errors.
func IsWithinWateringWindow(earliest, latest string, now time.Time) bool {
	if earliest == "" {
		earliest = "06:00"
	}
	if latest == "" {
		latest = "10:00"
	}

	earliestMin := ParseTimeToMinutes(earliest)
	latestMin := ParseTimeToMinutes(latest)
	if earliestMin < 0 || latestMin < 0 {
		return true
	}

	currentMin := now.Hour()*60 + now.Minute()
	return currentMin >= earliestMin && currentMin <= latestMin
}

func (m *Manager) LogEvent(level, category, message, zoneName string) {
	event := &models.EventLog{
		Level:    level,
		Category: category,
		Message:  message,
		ZoneName: zoneName,
	}
	if err := m.store.CreateEventLog(event); err != nil {
		log.Printf("Failed to log event: %v", err)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal event: %v", err)
		return
	}
	m.client.Publish("bedwetter/event", 0, false, string(payload))
}

func splitEntityID(entityID string) []string {
	parts := strings.SplitN(entityID, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	return parts
}

func (m *Manager) AddZone(zc config.ZoneConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.zones[zc.Name]; exists {
		return
	}

	z := &Zone{
		Config:      zc,
		State:       StateIdle,
		Moisture:    math.NaN(),
		Humidity:    math.NaN(),
		Temperature: math.NaN(),
	}
	m.zones[zc.Name] = z

	m.subscribeSensor(z)
	m.subscribeHumidity(z)
	m.subscribeTemperature(z)
	m.subscribeValveState(z)
	m.watchHAEntity(z)
	m.watchHAHumidity(z)
	m.watchHATemperature(z)

	if m.resolver != nil {
		ha.ResolveZoneAsync(m.resolver, &z.Config)
	}

	m.LogEvent("info", "config", "Zone added: "+zc.Name, zc.Name)
}

func (m *Manager) RemoveZone(name string) {
	m.CloseValve(name)

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.zones[name]; ok {
		delete(m.zones, name)
		m.LogEvent("info", "config", "Zone removed: "+name, name)
	}
}

func (m *Manager) UpdateZoneConfig(name string, zc config.ZoneConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	z, ok := m.zones[name]
	if !ok {
		return
	}
	z.Config = zc
	log.Printf("Zone %q: config updated dynamically", name)

	m.subscribeSensor(z)
	m.subscribeHumidity(z)
	m.subscribeTemperature(z)
	m.subscribeValveState(z)
	m.watchHAEntity(z)
	m.watchHAHumidity(z)
	m.watchHATemperature(z)
}

func (m *Manager) OpenAllValves() {
	zones := m.GetAllZones()
	for _, z := range zones {
		m.OpenValve(z.Config.Name)
	}
}

func (m *Manager) CloseAllValves() {
	zones := m.GetAllZones()
	for _, z := range zones {
		m.CloseValve(z.Config.Name)
	}
}

func (m *Manager) GetZone(name string) *Zone {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.zones[name]
}

func (m *Manager) GetAllZones() []*Zone {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Zone, 0, len(m.zones))
	for _, z := range m.zones {
		result = append(result, z)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Config.Name < result[j].Config.Name
	})
	return result
}

func (z *Zone) Snapshot() ZoneSnapshot {
	z.mu.RLock()
	defer z.mu.RUnlock()
	return ZoneSnapshot{
		Config:           z.Config,
		Moisture:         z.Moisture,
		Humidity:         z.Humidity,
		Temperature:      z.Temperature,
		State:            z.State,
		LastMoistureTime: z.LastMoistureTime,
		LastWaterEnd:     z.LastWaterEnd,
		LastStateChange:  z.LastStateChange,
		WateringStarted:  z.WateringStarted,
	}
}

func (m *Manager) GetAllZoneSnapshots() []ZoneSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ZoneSnapshot, 0, len(m.zones))
	for _, z := range m.zones {
		result = append(result, z.Snapshot())
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Config.Name < result[j].Config.Name
	})
	return result
}

func (m *Manager) Watchdog() {
	for _, z := range m.zones {
		z.mu.Lock()
		since := time.Since(z.LastMoistureTime)
		stale := time.Duration(m.cfg.Alerts.StaleSensorMinutes) * time.Minute
		if z.LastMoistureTime.IsZero() || since > stale*2 {
			z.State = StateFailsafe
			m.LogEvent("warn", "system", "Failsafe activated: stale sensor for "+z.Config.Name, z.Config.Name)
			if m.sendNtfy != nil {
				go m.sendNtfy("alarm", "Failsafe Activated", fmt.Sprintf("Stale sensor detected for zone '%s'", z.Config.Name))
			}
			go m.CloseValve(z.Config.Name)
		}
		z.mu.Unlock()
	}
}

func (m *Manager) watchdogLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.Watchdog()
		}
	}
}
