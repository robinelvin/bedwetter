package ha

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rob/bedwetter/config"
	"github.com/rob/bedwetter/mqtt"
)

type fakeMQTT struct {
	published   []pubCall
	subscribed  []subCall
}

type pubCall struct {
	topic    string
	payload  string
	retained bool
}

type subCall struct {
	topic   string
	handler mqtt.MessageHandler
}

func (f *fakeMQTT) Publish(topic string, qos byte, retained bool, payload string) error {
	f.published = append(f.published, pubCall{topic, payload, retained})
	return nil
}

func (f *fakeMQTT) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) error {
	f.subscribed = append(f.subscribed, subCall{topic, handler})
	return nil
}

func (f *fakeMQTT) SubscribeMultiple(topics map[string]byte, handler mqtt.MessageHandler) error {
	return nil
}

func (f *fakeMQTT) IsConnected() bool { return true }

func (f *fakeMQTT) Unsubscribe(topics ...string) {}

func (f *fakeMQTT) Disconnect(quiesce uint) {}

func init() {
	// silence logging in tests
}

func TestSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Raised Bed 1", "Raised_Bed_1"},
		{"Herb Garden", "Herb_Garden"},
		{"simple", "simple"},
		{"tab\there", "tab_here"},
		{"a/b/c", "a_b_c"},
		{"  spaces  ", "__spaces__"},
		{"", ""},
	}

	for _, tt := range tests {
		got := slug(tt.input)
		if got != tt.want {
			t.Errorf("slug(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPublishAllSkippedHAEntityZones(t *testing.T) {
	fake := &fakeMQTT{}
	cfg := &config.Config{
		Zones: []config.ZoneConfig{
			{
				Name:                 "HA Zone",
				MoistureSensorEntity: "sensor.test",
				ValveSwitchEntity:    "switch.test",
			},
		},
	}

	PublishAll(fake, cfg)

	if len(fake.published) != 0 {
		t.Errorf("expected no publishes for HA-only zone, got %d: %v", len(fake.published), fake.published)
	}
}

func TestPublishAllMQTTZone(t *testing.T) {
	fake := &fakeMQTT{}
	cfg := &config.Config{
		Zones: []config.ZoneConfig{
			{
				Name:                "MQTT Zone",
				MoistureSensorTopic: "sensor/topic",
				ValveCommandTopic:   "valve/cmd",
				ValveStateTopic:     "valve/state",
			},
		},
	}

	PublishAll(fake, cfg)

	if len(fake.published) != 2 {
		t.Fatalf("expected 2 publishes (sensor+switch), got %d", len(fake.published))
	}

	sensorPub := fake.published[0]
	if !strings.Contains(sensorPub.topic, "bedwetter_moisture_MQTT_Zone") {
		t.Errorf("sensor topic unexpected: %q", sensorPub.topic)
	}
	var sensorPayload DiscoveryPayload
	if err := json.Unmarshal([]byte(sensorPub.payload), &sensorPayload); err != nil {
		t.Fatalf("sensor payload parse error: %v", err)
	}
	if sensorPayload.StateTopic != "sensor/topic" {
		t.Errorf("sensor StateTopic: got %q, want sensor/topic", sensorPayload.StateTopic)
	}
	if sensorPayload.DeviceClass != "humidity" {
		t.Errorf("sensor DeviceClass: got %q", sensorPayload.DeviceClass)
	}

	switchPub := fake.published[1]
	if !strings.Contains(switchPub.topic, "bedwetter_valve_MQTT_Zone") {
		t.Errorf("switch topic unexpected: %q", switchPub.topic)
	}
	var switchPayload DiscoveryPayload
	if err := json.Unmarshal([]byte(switchPub.payload), &switchPayload); err != nil {
		t.Fatalf("switch payload parse error: %v", err)
	}
	if switchPayload.CommandTopic != "valve/cmd" {
		t.Errorf("switch CommandTopic: got %q", switchPayload.CommandTopic)
	}
	if switchPayload.StateTopic != "valve/state" {
		t.Errorf("switch StateTopic: got %q", switchPayload.StateTopic)
	}
}

func TestPublishAllMixedZones(t *testing.T) {
	fake := &fakeMQTT{}
	cfg := &config.Config{
		Zones: []config.ZoneConfig{
			{Name: "HA Zone", MoistureSensorEntity: "sensor.a", ValveSwitchEntity: "switch.a"},
			{Name: "MQTT Zone", MoistureSensorTopic: "s/b", ValveCommandTopic: "v/c", ValveStateTopic: "v/d"},
		},
	}

	PublishAll(fake, cfg)

	if len(fake.published) != 2 {
		t.Errorf("expected 2 publishes (only MQTT zone), got %d", len(fake.published))
	}
}

func TestPublishAllDeviceInfo(t *testing.T) {
	fake := &fakeMQTT{}
	cfg := &config.Config{
		Zones: []config.ZoneConfig{
			{Name: "Test", MoistureSensorTopic: "a/b", ValveCommandTopic: "c/d", ValveStateTopic: "c/e"},
		},
	}

	PublishAll(fake, cfg)

	if len(fake.published) == 0 {
		t.Fatal("expected publishes")
	}

	var payload DiscoveryPayload
	json.Unmarshal([]byte(fake.published[0].payload), &payload)

	if payload.Device == nil {
		t.Fatal("expected Device info")
	}
	if len(payload.Device.Identifiers) == 0 || payload.Device.Identifiers[0] != "bedwetter" {
		t.Errorf("expected identifier bedwetter, got %v", payload.Device.Identifiers)
	}
	if payload.Device.Name != "BedWetter Irrigation" {
		t.Errorf("Device.Name: got %q", payload.Device.Name)
	}
	if payload.Origin == nil {
		t.Fatal("expected Origin info")
	}
	if payload.Origin.Name != "BedWetter" {
		t.Errorf("Origin.Name: got %q", payload.Origin.Name)
	}
	if payload.Origin.SWVersion != "1.0.0" {
		t.Errorf("Origin.SWVersion: got %q", payload.Origin.SWVersion)
	}
}

func TestPublishAllRetained(t *testing.T) {
	fake := &fakeMQTT{}
	cfg := &config.Config{
		Zones: []config.ZoneConfig{
			{Name: "Test", MoistureSensorTopic: "a/b", ValveCommandTopic: "c/d"},
		},
	}

	PublishAll(fake, cfg)

	for i, p := range fake.published {
		if !p.retained {
			t.Errorf("publish %d not retained", i)
		}
	}
}

func TestSubscribeToCommands(t *testing.T) {
	fake := &fakeMQTT{}
	cfg := &config.Config{
		Zones: []config.ZoneConfig{
			{Name: "Z1", ValveCommandTopic: "valve/z1"},
			{Name: "Z2", ValveCommandTopic: "valve/z2"},
			{Name: "Z3", MoistureSensorEntity: "sensor.z3", ValveSwitchEntity: "switch.z3"},
		},
	}

	var gotCommands []string
	SubscribeToCommands(fake, cfg, func(zoneName, state string) {
		gotCommands = append(gotCommands, zoneName+":"+state)
	})

	if len(fake.subscribed) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(fake.subscribed))
	}

	if fake.subscribed[0].topic != "valve/z1/set" {
		t.Errorf("subscribed topic: got %q, want valve/z1/set", fake.subscribed[0].topic)
	}
	if fake.subscribed[1].topic != "valve/z2/set" {
		t.Errorf("subscribed topic: got %q, want valve/z2/set", fake.subscribed[1].topic)
	}

	fake.subscribed[0].handler("valve/z1/set", []byte("ON"))
	fake.subscribed[1].handler("valve/z2/set", []byte("OFF"))

	if len(gotCommands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(gotCommands))
	}
	if gotCommands[0] != "Z1:ON" {
		t.Errorf("got %q", gotCommands[0])
	}
	if gotCommands[1] != "Z2:OFF" {
		t.Errorf("got %q", gotCommands[1])
	}
}

func TestEntityConfigTopic(t *testing.T) {
	tests := []struct {
		entityID string
		want     string
	}{
		{"sensor.test", "homeassistant/sensor/test/config"},
		{"switch.garden_valve", "homeassistant/switch/garden_valve/config"},
		{"invalid", ""},
		{"too.many.dots", "homeassistant/too/many.dots/config"},
		{"", ""},
	}

	for _, tt := range tests {
		got := entityConfigTopic(tt.entityID)
		if got != tt.want {
			t.Errorf("entityConfigTopic(%q) = %q, want %q", tt.entityID, got, tt.want)
		}
	}
}

func TestResolveZoneAsync(t *testing.T) {
	fake := &fakeMQTT{}
	r := NewEntityResolver(fake)

	zone := &config.ZoneConfig{
		Name:                 "Test",
		MoistureSensorEntity: "sensor.moisture",
		ValveSwitchEntity:    "switch.valve",
	}

	ResolveZoneAsync(r, zone)

	if len(fake.subscribed) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(fake.subscribed))
	}
}

func TestResolveZoneAsyncDirectMQTT(t *testing.T) {
	fake := &fakeMQTT{}
	r := NewEntityResolver(fake)

	zone := &config.ZoneConfig{
		Name:                "Test",
		MoistureSensorTopic: "a/b/c",
		ValveCommandTopic:   "d/e/f",
	}

	ResolveZoneAsync(r, zone)

	if len(fake.subscribed) != 0 {
		t.Errorf("expected 0 subscriptions for direct MQTT zone, got %d", len(fake.subscribed))
	}
}

func TestEntityResolverGetTopics(t *testing.T) {
	r := NewEntityResolver(nil)

	got := r.GetTopics("sensor.test")
	if got != nil {
		t.Errorf("expected nil for unset entity, got %v", got)
	}
}
