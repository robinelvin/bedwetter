package zones

import (
	"encoding/json"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/ha"
	"github.com/rob/bedwetter/models"
	"github.com/rob/bedwetter/mqtt"
	"github.com/rob/bedwetter/store"
)

type ZoneState int

const (
	StateIdle ZoneState = iota
	StateWatering
	StateCooldown
	StateManualOpen
	StateFailsafe
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

type Manager struct {
	zones    map[string]*Zone
	client   mqtt.ClientInterface
	store    *store.Store
	cfg      *config.Config
	resolver *ha.EntityResolver
	haAPI    *ha.APIClient
	mu       sync.RWMutex
	done     chan struct{}
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
			Config: zc,
			State:  StateIdle,
		}
	}
	return m
}

func (m *Manager) Start() {
	if m.resolver != nil {
		m.resolver.OnResolved(func(zoneName string) {
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
	}

	for _, z := range m.zones {
		m.subscribeSensor(z)
		m.subscribeValveState(z)
		m.watchHAEntity(z)
	}

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
		if err := m.store.SaveSensorReading(z.Config.Name, value, z.Humidity); err != nil {
			log.Printf("Failed to save sensor reading: %v", err)
		}
		m.evaluateZone(z.Config.Name)
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

func (m *Manager) Stop() {
	close(m.done)
	for _, z := range m.zones {
		m.CloseValve(z.Config.Name)
	}
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

	if err := m.store.SaveSensorReading(zoneName, moisture, z.Humidity); err != nil {
		log.Printf("Failed to save sensor reading: %v", err)
	}

	m.evaluateZone(zoneName)
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
			m.logEvent("info", "valve", "Valve manually opened: "+zoneName, zoneName)
		}
	} else {
		if z.State == StateManualOpen || z.State == StateWatering {
			z.State = StateIdle
			z.LastWaterEnd = time.Now()
			z.LastStateChange = time.Now()
			m.logEvent("info", "valve", "Valve manually closed: "+zoneName, zoneName)
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

	if z.State == StateManualOpen || z.State == StateFailsafe || z.State == StateWatering {
		if z.State == StateWatering {
			elapsed := time.Since(z.WateringStarted)
			maxDur := time.Duration(z.Config.MaxWateringSeconds) * time.Second
			if elapsed >= maxDur {
				log.Printf("Zone %s: max watering duration reached (%ds)", zoneName, z.Config.MaxWateringSeconds)
				m.logEvent("warn", "valve", "Max watering duration reached for "+zoneName, zoneName)
				go m.CloseValve(zoneName)
				z.State = StateCooldown
				z.LastWaterEnd = time.Now()
				z.LastStateChange = time.Now()
			}
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

	if z.Moisture >= float64(z.Config.ThresholdLow) {
		return
	}

	count, err := m.store.ActivationsToday(zoneName)
	if err != nil {
		log.Printf("Error checking activations for %s: %v", zoneName, err)
		return
	}
	if count >= int64(z.Config.MaxActivationsPerDay) {
		log.Printf("Zone %s: max daily activations reached (%d)", zoneName, z.Config.MaxActivationsPerDay)
		return
	}

	log.Printf("Zone %s: moisture %.1f%% below threshold %d%%, opening valve", zoneName, z.Moisture, z.Config.ThresholdLow)
	m.logEvent("info", "valve", "Watering started: "+zoneName, zoneName)
	go m.OpenValve(zoneName)
	z.State = StateWatering
	z.WateringStarted = time.Now()
	z.LastStateChange = time.Now()
	go func() {
		m.store.SaveValveEvent(zoneName, "open", z.Config.MaxWateringSeconds)
	}()
}

func (m *Manager) OpenValve(zoneName string) {
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
	z.mu.Lock()
	if z.State == StateIdle || z.State == StateCooldown {
		z.State = StateManualOpen
		z.LastStateChange = time.Now()
	}
	z.mu.Unlock()
	m.logEvent("info", "valve", "Valve opened: "+zoneName, zoneName)
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
	if z.State == StateManualOpen || z.State == StateWatering {
		z.State = StateIdle
		z.LastWaterEnd = time.Now()
		z.LastStateChange = time.Now()
	}
	z.mu.Unlock()
	m.logEvent("info", "valve", "Valve closed: "+zoneName, zoneName)
}

func (m *Manager) logEvent(level, category, message, zoneName string) {
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
		Config: zc,
		State:  StateIdle,
	}
	m.zones[zc.Name] = z

	m.subscribeSensor(z)
	m.subscribeValveState(z)
	m.watchHAEntity(z)

	if m.resolver != nil {
		ha.ResolveZoneAsync(m.resolver, &z.Config)
	}

	m.logEvent("info", "config", "Zone added: "+zc.Name, zc.Name)
}

func (m *Manager) RemoveZone(name string) {
	m.CloseValve(name)

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.zones[name]; ok {
		delete(m.zones, name)
		m.logEvent("info", "config", "Zone removed: "+name, name)
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

func (m *Manager) Watchdog() {
	for _, z := range m.zones {
		z.mu.Lock()
		since := time.Since(z.LastMoistureTime)
		stale := time.Duration(m.cfg.Alerts.StaleSensorMinutes) * time.Minute
		if z.LastMoistureTime.IsZero() || since > stale*2 {
			z.State = StateFailsafe
			m.logEvent("warn", "system", "Failsafe activated: stale sensor for "+z.Config.Name, z.Config.Name)
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
