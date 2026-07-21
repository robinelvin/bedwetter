---
title: BedWetter User Manual
weight: 1
---

# BedWetter User Manual

**BedWetter** is a self-hosted, automated garden irrigation controller. It monitors soil moisture across multiple watering zones and automatically opens and closes solenoid valves to keep your plants watered.

## Key Features

- **Multi-zone irrigation** — independently control multiple watering zones, each with its own sensor, valve, and thresholds
- **Automatic watering** — moisture-based triggers with configurable low/high thresholds to prevent over- and under-watering
- **Scheduled watering** — weekly schedules with seasonal multipliers and per-month overrides
- **Weather integration** — uses Open-Meteo forecasts to skip watering when rain is predicted; optional physical rain sensor support
- **Home Assistant integration** — MQTT discovery, REST API polling, and entity resolution for seamless HA operation
- **Heartbeat safety** — remote devices enforce their own timeouts to prevent stuck-open valves
- **Alerts & notifications** — email (SMTP) and push notifications (ntfy) for stale sensors, failsafe events, and more
- **Web UI** — dashboard with live zone status, weather widget, schedule management, and configuration — built with htmx and Tailwind CSS
- **REST API** — programmatic control of valves and zone status for automation

## Architecture Overview

BedWetter connects to an MQTT broker and optionally to a Home Assistant instance. Each zone consists of:

1. **Moisture sensor** — reports soil moisture percentage via MQTT or a Home Assistant entity
2. **Valve** — a solenoid controlled via MQTT commands or a Home Assistant switch
3. **Optional sensors** — humidity and temperature readings
4. **Optional rain sensor** — physical rain detection via MQTT or HA entity

The system evaluates moisture levels against configurable thresholds and watering windows, then opens or closes valves accordingly. All state is persisted in a local SQLite database.

## Documentation Sections

- [Getting Started](getting-started/) — installation, building, and first-time setup
- [Configuration](configuration/) — full configuration reference
- [Zones](zones/) — zone setup, thresholds, and state machine
- [Schedules](schedules/) — weekly schedules, month overrides, and seasonal multipliers
- [Weather](weather/) — Open-Meteo forecasts and rain sensor integration
- [Home Assistant](home-assistant/) — MQTT discovery, REST API polling, and entity resolution
- [Safety](safety/) — heartbeat protocol and remote device safety
- [Alerts](alerts/) — email and ntfy push notifications
- [Web UI](web-ui/) — dashboard, zone detail, and configuration interface
- [REST API](api/) — programmatic control endpoints
- [Troubleshooting](troubleshooting/) — common issues and solutions
