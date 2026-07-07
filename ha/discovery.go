package ha

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/mqtt"
)

const discoveryPrefix = "homeassistant"

type DiscoveryPayload struct {
	Name              string `json:"name"`
	UniqueID          string `json:"unique_id"`
	StateTopic        string `json:"state_topic,omitempty"`
	CommandTopic      string `json:"command_topic,omitempty"`
	PayloadOn         string `json:"payload_on,omitempty"`
	PayloadOff        string `json:"payload_off,omitempty"`
	StateOn           string `json:"state_on,omitempty"`
	StateOff          string `json:"state_off,omitempty"`
	UnitOfMeasurement string `json:"unit_of_measurement,omitempty"`
	DeviceClass       string `json:"device_class,omitempty"`
	ValueTemplate     string `json:"value_template,omitempty"`
	QoS               int    `json:"qos,omitempty"`
	Retain            bool   `json:"retain,omitempty"`
	Device            *DeviceInfo `json:"device,omitempty"`
	Origin            *OriginInfo `json:"origin,omitempty"`
}

type DeviceInfo struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Model        string   `json:"model"`
	Manufacturer string   `json:"manufacturer"`
	SWVersion    string   `json:"sw_version"`
}

type OriginInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func PublishAll(client *mqtt.Client, cfg *config.Config) {
	device := &DeviceInfo{
		Identifiers:  []string{"bedwetter"},
		Name:         "BedWetter Irrigation",
		Model:        "BedWetter v1",
		Manufacturer: "BedWetter",
		SWVersion:    "1.0.0",
	}
	origin := &OriginInfo{
		Name:    "BedWetter",
		Version: "1.0.0",
	}

	for _, z := range cfg.Zones {
		publishSensor(client, z, device, origin)
		publishSwitch(client, z, device, origin)
	}

	log.Printf("Published HA discovery configs for %d zones", len(cfg.Zones))
}

func publishSensor(client *mqtt.Client, z config.ZoneConfig, device *DeviceInfo, origin *OriginInfo) {
	uid := fmt.Sprintf("bedwetter_moisture_%s", z.Name)
	topic := fmt.Sprintf("%s/sensor/%s/config", discoveryPrefix, uid)
	payload := DiscoveryPayload{
		Name:              fmt.Sprintf("%s Moisture", z.Name),
		UniqueID:          uid,
		StateTopic:        z.MoistureSensorTopic,
		UnitOfMeasurement: "%",
		DeviceClass:       "humidity",
		QoS:               1,
		Retain:            true,
		Device:            device,
		Origin:            origin,
	}
	data, _ := json.Marshal(payload)
	if err := client.Publish(topic, 1, true, string(data)); err != nil {
		log.Printf("Failed to publish HA sensor discovery for %s: %v", z.Name, err)
	}
}

func publishSwitch(client *mqtt.Client, z config.ZoneConfig, device *DeviceInfo, origin *OriginInfo) {
	if z.ValveCommandTopic == "" {
		return
	}
	uid := fmt.Sprintf("bedwetter_valve_%s", z.Name)
	stateTopic := z.ValveStateTopic
	if stateTopic == "" {
		stateTopic = z.ValveCommandTopic + "/state"
	}
	topic := fmt.Sprintf("%s/switch/%s/config", discoveryPrefix, uid)
	payload := DiscoveryPayload{
		Name:         fmt.Sprintf("%s Valve", z.Name),
		UniqueID:     uid,
		StateTopic:   stateTopic,
		CommandTopic: z.ValveCommandTopic,
		PayloadOn:    "ON",
		PayloadOff:   "OFF",
		StateOn:      "ON",
		StateOff:     "OFF",
		QoS:          1,
		Retain:       true,
		Device:       device,
		Origin:       origin,
	}
	data, _ := json.Marshal(payload)
	if err := client.Publish(topic, 1, true, string(data)); err != nil {
		log.Printf("Failed to publish HA switch discovery for %s: %v", z.Name, err)
	}
}

func SubscribeToCommands(client *mqtt.Client, cfg *config.Config, handler func(zoneName string, state string)) {
	for _, z := range cfg.Zones {
		if z.ValveCommandTopic == "" {
			continue
		}
		cmdTopic := fmt.Sprintf("%s/set", z.ValveCommandTopic)
		zoneName := z.Name
		if err := client.Subscribe(cmdTopic, 1, func(t string, p []byte) {
			handler(zoneName, string(p))
		}); err != nil {
			log.Printf("Failed to subscribe to HA command topic %s: %v", cmdTopic, err)
		}
	}
}
