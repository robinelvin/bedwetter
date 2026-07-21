---
title: "Zones"
weight: 30
---

Zones are the core building blocks of BedWetter. Each zone represents an independent watering area with its own sensors, valve, thresholds, and schedule.

## Zone Configuration

### Sensor Source (pick one)

Each zone requires a **moisture sensor**. You can source readings either directly via MQTT or through a Home Assistant entity.

**Direct MQTT** — set `moisture_sensor_topic` to the MQTT topic publishing moisture percentage values.

**Home Assistant entity** — set `moisture_sensor_entity` to the HA entity ID (e.g., `sensor.soil_moisture`). BedWetter will poll HA every 10 seconds for the current state.

Optional sensors follow the same pattern:

| Sensor | MQTT Field | HA Entity Field |
|--------|-----------|-----------------|
| Moisture (required) | `moisture_sensor_topic` | `moisture_sensor_entity` |
| Humidity | `humidity_sensor_topic` | `humidity_sensor_entity` |
| Temperature | `temperature_sensor_topic` | `temperature_sensor_entity` |

### Valve Control (pick one)

**Direct MQTT** — set `valve_command_topic` for ON/OFF commands and `valve_state_topic` for state feedback.

**Home Assistant switch** — set `valve_switch_entity` to the HA switch entity ID (e.g., `switch.solenoid_valve`).

### Thresholds

| Field | Default | Description |
|-------|---------|-------------|
| `threshold_low` | `40` | Moisture percentage below which the valve opens |
| `threshold_high` | `60` | Moisture percentage above which the valve closes |

Having separate low and high thresholds prevents oscillation — the system opens at 40%, fills until 60%, then stops.

### Limits

| Field | Default | Description |
|-------|---------|-------------|
| `max_watering_seconds` | `300` | Maximum seconds the valve stays open per cycle |
| `max_activations_per_day` | `5` | Hard cap on valve activations per day |
| `cooldown_minutes` | `90` | Minutes after closing before re-evaluation |

### Watering Window

| Field | Default | Description |
|-------|---------|-------------|
| `earliest_watering_time` | `06:00` | Earliest time a watering cycle may start |
| `latest_watering_time` | `10:00` | Latest time a watering cycle may start |

A cycle that starts within the window is allowed to complete even if it runs past the window end. The window only gates the *start* of a cycle.

### Seasonal Multipliers

Scale watering duration by month. For example, `1.5` in July means 50% longer watering during summer.

```yaml
seasonal_multipliers:
  1: 0.5    # January: half duration
  4: 0.8    # April: slightly less
  7: 1.5    # July: 50% more
  10: 1.0   # October: normal
```

### Indoor Zones

| Field | Default | Description |
|-------|---------|-------------|
| `indoor` | `false` | When `true`, rain detection is bypassed for this zone |

## Example Zone Configurations

### Direct MQTT Zone

```yaml
- name: Raised Bed 1
  moisture_sensor_topic: esp01/sensor/soil_moisture/state
  valve_command_topic: esp01/switch/solenoid_valve/command
  valve_state_topic: esp01/switch/solenoid_valve/state
  threshold_low: 40
  threshold_high: 60
  max_watering_seconds: 300
  max_activations_per_day: 5
  cooldown_minutes: 90
  earliest_watering_time: "06:00"
  latest_watering_time: "10:00"
  heartbeat_timeout: 0
  seasonal_multipliers:
    1: 0.5
    7: 1.5
```

### Home Assistant Entity Zone

```yaml
- name: Herb Garden
  moisture_sensor_entity: sensor.garden_sensor_soil_moisture
  valve_switch_entity: switch.garden_sensor_water_valve
  threshold_low: 35
  threshold_high: 55
  max_watering_seconds: 240
  max_activations_per_day: 4
  cooldown_minutes: 120
  earliest_watering_time: "07:00"
  latest_watering_time: "09:00"
  heartbeat_timeout: 0
```

## Zone State Machine

Each zone operates as a state machine:

```
         ┌─────────────────────────────────┐
         │                                 ▼
      [IDLE] ──trigger──► [WATERING] ──duration──► [COOLDOWN]
         ▲                      │                        │
         │               safety shutoff             cooldown expires
         │                      │                        │
         │                      ▼                        │
         │                  [FAULT]                      │
         │                                               │
         └───────────────────────────────────────────────┘
```

| State | Description |
|-------|-------------|
| **Idle** | Waiting for a trigger (schedule, sensor, or manual) |
| **Watering** | Valve is open, actively watering |
| **Cooldown** | Waiting after watering before re-evaluation |
| **ManualOpen** | User-initiated valve open |
| **Failsafe** | Error condition (stale sensor, valve unavailable, max duration exceeded) |
| **ForceClosed** | User-initiated emergency stop — prevents automatic reopening |

## MQTT Topic Structure

BedWetter publishes and subscribes to the following MQTT topics per zone:

| Topic | Direction | Description |
|-------|-----------|-------------|
| `bedwetter/zone/<slug>/state` | Publish | Current zone state (idle/watering/cooldown/etc.) |
| `bedwetter/timeout/<slug>` | Publish | Watering duration for remote device timer |
| `bedwetter/heartbeat/<slug>` | Publish | JSON heartbeat while valve is open |
| `<zone>/moisture/state` | Subscribe | Soil moisture readings |
| `<zone>/valve/command` | Publish | ON/OFF valve commands |
| `<zone>/valve/state` | Subscribe | Valve state feedback |

The `<slug>` is the zone name with spaces replaced by underscores (e.g., `Raised_Bed_1`).

## Managing Zones

Zones can be created, edited, and deleted through the web UI at **Configuration > Zones**. After changes, the zone is updated in the SQLite database immediately.
