---
title: "Configuration"
weight: 20
---

After the initial YAML seed, all configuration is managed through the web UI and stored in the SQLite database. The YAML file is only read on first run to seed the database.

## Configuration Reference

### MQTT

| Field | Default | Description |
|-------|---------|-------------|
| `broker` | — | MQTT broker hostname or IP |
| `port` | `1883` | MQTT broker port |
| `username` | — | MQTT authentication username |
| `password` | — | MQTT authentication password |

### Home Assistant

| Field | Default | Description |
|-------|---------|-------------|
| `url` | — | Home Assistant base URL (e.g., `http://homeassistant.local:8123/`) |
| `token` | — | Long-lived access token from HA |

### Web Server

| Field | Default | Description |
|-------|---------|-------------|
| `listen_addr` | `:8080` | Address and port to serve the web UI |

### Database

| Field | Default | Description |
|-------|---------|-------------|
| `db_path` | `bedwetter.db` | Path to the SQLite database file |

### Heartbeat

| Field | Default | Description |
|-------|---------|-------------|
| `heartbeat_interval` | `30` | Seconds between heartbeats while a valve is open. Set to `0` to disable. |

## Full YAML Reference

```yaml
mqtt:
  broker: mqtt
  port: 1883
  username: mqttuser
  password: mqttpass

homeassistant:
  url: http://homeassistant.local:8123/
  token: ""

web:
  listen_addr: ":8080"

db_path: bedwetter.db

heartbeat_interval: 30

zones:
  - name: Zone Name
    moisture_sensor_topic: ""
    moisture_sensor_entity: ""
    humidity_sensor_topic: ""
    humidity_sensor_entity: ""
    temperature_sensor_topic: ""
    temperature_sensor_entity: ""
    valve_command_topic: ""
    valve_state_topic: ""
    valve_switch_entity: ""
    threshold_low: 40
    threshold_high: 60
    max_watering_seconds: 300
    max_activations_per_day: 5
    cooldown_minutes: 90
    earliest_watering_time: "06:00"
    latest_watering_time: "10:00"
    heartbeat_timeout: 0
    indoor: false
    seasonal_multipliers:
      1: 0.5
      7: 1.5

schedules:
  - zone_name: Zone Name
    schedule:
      - day_of_week: Mon
        time: "06:00"
        duration: 300
    month_overrides:
      - month: 7
        schedule:
          - day_of_week: Mon
            time: "06:00"
            duration: 360

weather:
  lat: 51.5
  lon: -0.12
  rain_threshold_mm: 5.0
  rain_sensor_topic: ""
  rain_sensor_entity: ""

alerts:
  email: you@example.com
  stale_sensor_minutes: 60
  smtp_server: smtp.gmail.com
  smtp_port: 587
  smtp_username: you@gmail.com
  smtp_password: app_password
  from_email: bedwetter@example.com

ntfy:
  enabled: false
  server: https://ntfy.sh
  uuid: ""
  token: ""
  alert_info: true
  alert_warn: true
  alert_alarm: true

master_valve:
  command_topic: ""
  switch_entity: ""
```

## Configuring via the Web UI

After initial setup, all configuration sections are accessible from the **Configuration** page in the web UI:

- **MQTT** — broker connection details
- **Home Assistant** — URL and access token
- **Alerts** — email SMTP settings and stale sensor timeout
- **ntfy** — push notification server and topic settings
- **Weather** — latitude, longitude, rain threshold, rain sensor
- **Master Valve** — MQTT topic or HA entity for the master shutoff
- **Zones** — add, edit, or delete watering zones
