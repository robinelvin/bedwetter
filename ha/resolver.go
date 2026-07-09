package ha

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/robinelvin/bedwetter/config"
	"github.com/robinelvin/bedwetter/mqtt"
)

type ResolvedTopics struct {
	StateTopic   string
	CommandTopic string
}

type EntityResolver struct {
	client   mqtt.ClientInterface
	resolved map[string]*ResolvedTopics
	mu       sync.RWMutex
	handlers []func(zoneName string)
}

func NewEntityResolver(client mqtt.ClientInterface) *EntityResolver {
	return &EntityResolver{
		client:   client,
		resolved: make(map[string]*ResolvedTopics),
	}
}

func (r *EntityResolver) OnResolved(handler func(zoneName string)) {
	r.handlers = append(r.handlers, handler)
}

func (r *EntityResolver) ResolveEntity(zoneName, entityID string) {
	if entityID == "" {
		return
	}
	configTopic := entityConfigTopic(entityID)
	if configTopic == "" {
		log.Printf("Zone %q: invalid entity ID %q", zoneName, entityID)
		return
	}

	log.Printf("Zone %q: watching HA entity %q on %s", zoneName, entityID, configTopic)

	if err := r.client.Subscribe(configTopic, 1, func(t string, p []byte) {
		var dp DiscoveryPayload
		if err := json.Unmarshal(p, &dp); err != nil {
			log.Printf("Zone %q: failed to parse HA config for %s: %v", zoneName, entityID, err)
			return
		}
		topics := &ResolvedTopics{
			StateTopic:   dp.StateTopic,
			CommandTopic: dp.CommandTopic,
		}

		r.mu.Lock()
		r.resolved[entityID] = topics
		r.mu.Unlock()

		log.Printf("Zone %q: resolved HA entity %q → state_topic=%s command_topic=%s",
			zoneName, entityID, topics.StateTopic, topics.CommandTopic)

		for _, h := range r.handlers {
			h(zoneName)
		}
	}); err != nil {
		log.Printf("Zone %q: failed to subscribe to HA config for %s: %v", zoneName, entityID, err)
	}
}

func (r *EntityResolver) GetTopics(entityID string) *ResolvedTopics {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolved[entityID]
}

func entityConfigTopic(entityID string) string {
	parts := strings.SplitN(entityID, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s/config", discoveryPrefix, parts[0], parts[1])
}

func ResolveZoneAsync(resolver *EntityResolver, zone *config.ZoneConfig) {
	if zone.MoistureSensorTopic == "" && zone.MoistureSensorEntity != "" {
		resolver.ResolveEntity(zone.Name, zone.MoistureSensorEntity)
	}
	if zone.ValveCommandTopic == "" && zone.ValveSwitchEntity != "" {
		resolver.ResolveEntity(zone.Name, zone.ValveSwitchEntity)
	}
}
