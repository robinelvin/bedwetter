---
title: "Home Assistant Integration"
weight: 60
---

BedWetter integrates with Home Assistant at three levels: MQTT Discovery, REST API polling, and entity resolution.

## MQTT Discovery

BedWetter publishes MQTT Discovery configuration messages so that zones appear as native Home Assistant entities. This includes:

- **Moisture sensors** — per-zone soil moisture percentage
- **Valve switches** — per-zone valve control
- **Zone state sensors** — current state (idle, watering, cooldown, etc.)

Discovery topics follow the standard HA MQTT Discovery format:

```
homeassistant/sensor/bedwetter/<zone_slug>/moisture/config
homeassistant/switch/bedwetter/<zone_slug>/valve/config
homeassistant/sensor/bedwetter/<zone_slug>/state/config
```

Once BedWetter publishes these configs, the entities automatically appear in Home Assistant's device registry.

## REST API Polling

For zones configured with Home Assistant entity IDs (via `moisture_sensor_entity` or `valve_switch_entity`), BedWetter:

1. **Polls** the HA REST API every 10 seconds to get current entity states
2. **Calls HA services** (`switch.turn_on` / `switch.turn_off`) to control valves

### Configuration

```yaml
homeassistant:
  url: http://homeassistant.local:8123/
  token: "your_long_lived_access_token"
```

The `token` is a long-lived access token created in **Home Assistant > Profile > Security > Long-Lived Access Tokens**.

## Entity Resolution

BedWetter subscribes to HA MQTT Discovery config topics to resolve entity IDs to their underlying MQTT state and command topics. This allows zones configured with HA entity IDs to receive MQTT-level subscriptions for faster updates.

## Zone Configuration for HA

To use Home Assistant entities for a zone:

```yaml
zones:
  - name: Herb Garden
    moisture_sensor_entity: sensor.garden_sensor_soil_moisture
    humidity_sensor_entity: sensor.garden_sensor_humidity
    temperature_sensor_entity: sensor.garden_sensor_temperature
    valve_switch_entity: switch.garden_sensor_water_valve
    threshold_low: 35
    threshold_high: 55
```

{{% note %}}
You can mix MQTT topics and HA entities within a single zone. For example, a zone can use a direct MQTT moisture sensor topic but control the valve via a Home Assistant switch entity.
{{% /note %}}
