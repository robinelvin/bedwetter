package zones

import (
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rob/bedwetter/config"
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
	zones  map[string]*Zone
	client *mqtt.Client
	store  *store.Store
	cfg    *config.Config
	mu     sync.RWMutex
	done   chan struct{}
}

func NewManager(cfg *config.Config, client *mqtt.Client, store *store.Store) *Manager {
	m := &Manager{
		zones:  make(map[string]*Zone),
		client: client,
		store:  store,
		cfg:    cfg,
		done:   make(chan struct{}),
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
	for _, z := range m.zones {
		z := z
		topic := z.Config.MoistureSensorTopic
		if err := m.client.Subscribe(topic, 0, func(t string, p []byte) {
			m.handleSensorReading(z.Config.Name, p)
		}); err != nil {
			log.Printf("Failed to subscribe to %s: %v", topic, err)
		}
		if z.Config.ValveStateTopic != "" {
			if err := m.client.Subscribe(z.Config.ValveStateTopic, 0, func(t string, p []byte) {
				m.handleValveState(z.Config.Name, p)
			}); err != nil {
				log.Printf("Failed to subscribe to %s: %v", z.Config.ValveStateTopic, err)
			}
		}
	}
	go m.watchdogLoop()
}

func (m *Manager) Stop() {
	close(m.done)
	for _, z := range m.zones {
		m.CloseValve(z.Config.Name)
	}
}

func (m *Manager) handleSensorReading(zoneName string, payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	z, ok := m.zones[zoneName]
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
		}
	} else {
		if z.State == StateManualOpen || z.State == StateWatering {
			z.State = StateIdle
			z.LastWaterEnd = time.Now()
			z.LastStateChange = time.Now()
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
	if topic == "" {
		return
	}
	if err := m.client.Publish(topic, 1, false, "ON"); err != nil {
		log.Printf("Failed to open valve for %s: %v", zoneName, err)
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
	if topic == "" {
		return
	}
	if err := m.client.Publish(topic, 1, false, "OFF"); err != nil {
		log.Printf("Failed to close valve for %s: %v", zoneName, err)
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
	return result
}

func (m *Manager) Watchdog() {
	for _, z := range m.zones {
		z.mu.Lock()
		since := time.Since(z.LastMoistureTime)
		stale := time.Duration(m.cfg.Alerts.StaleSensorMinutes) * time.Minute
		if z.LastMoistureTime.IsZero() || since > stale*2 {
			z.State = StateFailsafe
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
