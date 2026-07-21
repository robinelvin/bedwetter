---
title: "Weather Integration"
weight: 50
---

BedWetter integrates with weather data to skip watering when rain is predicted, and supports physical rain sensors for immediate shutoff.

## Open-Meteo Forecast

BedWetter uses [Open-Meteo](https://open-meteo.com/) for weather forecasts. Open-Meteo is free and requires no API key.

### Configuration

| Field | Default | Description |
|-------|---------|-------------|
| `lat` | — | Latitude for forecast location |
| `lon` | — | Longitude for forecast location |
| `rain_threshold_mm` | `5.0` | Skip watering if forecasted rain ≥ this value in the next 24 hours |

```yaml
weather:
  lat: 51.5
  lon: -0.12
  rain_threshold_mm: 5.0
```

### How It Works

- BedWetter fetches current conditions, hourly forecasts, and 3-day daily forecasts
- Weather data is cached for **30 minutes** to avoid excessive API calls
- If total forecasted rain in the next 24 hours ≥ `rain_threshold_mm`, all outdoor zones skip watering
- Weather data is displayed on the dashboard with Meteocons weather icons

## Physical Rain Sensor

For immediate rain detection, you can connect a physical rain sensor via MQTT or Home Assistant.

### Configuration

| Field | Default | Description |
|-------|---------|-------------|
| `rain_sensor_topic` | — | MQTT topic for a digital rain sensor |
| `rain_sensor_entity` | — | Home Assistant entity ID (e.g., `binary_sensor.rain`) |

```yaml
weather:
  rain_sensor_topic: home/sensors/rain/state
  # OR
  rain_sensor_entity: binary_sensor.rain_detector
```

### Behavior

- When the rain sensor is active, **all outdoor zones** are immediately closed and no new watering cycles start
- Indoor zones (configured with `indoor: true`) bypass rain detection entirely
- The rain sensor overrides both scheduled and sensor-triggered watering
