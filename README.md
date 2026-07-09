 ██████╗ ███████╗██████╗ ██╗    ██╗███████╗████████╗████████╗███████╗██████╗
 ██╔══██╗██╔════╝██╔══██╗██║    ██║██╔════╝╚══██╔══╝╚══██╔══╝██╔════╝██╔══██╗
 ██████╔╝█████╗  ██║  ██║██║ █╗ ██║█████╗     ██║      ██║   █████╗  ██████╔╝
 ██╔══██╗██╔══╝  ██║  ██║██║███╗██║██╔══╝     ██║      ██║   ██╔══╝  ██╔══██╗
 ██████╔╝███████╗██████╔╝╚███╔███╔╝███████╗   ██║      ██║   ███████╗██║  ██║
 ╚═════╝ ╚══════╝╚═════╝  ╚══╝╚══╝ ╚══════╝   ╚═╝      ╚═╝   ╚══════╝╚═╝  ╚═╝

An automated garden irrigation controller — self-hosted, Go-powered, MQTT and Home
Assistant aware. Monitors soil moisture (plus humidity and temperature) across
multiple watering zones and automatically opens and closes solenoid valves to keep
your plants happy.

## Features

- **Multi-zone irrigation** — define independent watering zones, each with its own
  sensor, valve, moisture thresholds, watering limits, and cooldown period.
- **Dual backend support** — each zone works with direct MQTT topics (ESP sensors)
  or Home Assistant entities (via REST API + MQTT discovery).
- **Humidity & temperature sensors** — optional per-zone, in both MQTT and HA mode.
- **Auto-watering** — opens the valve when moisture drops below `threshold_low`,
  closes when it reaches `threshold_high` or after `max_watering_seconds`.
- **Scheduled watering** — weekly and month-override schedules per zone, with
  optional weather-aware skip (OpenWeatherMap integration).
- **Home Assistant MQTT Discovery** — sensors and valves appear as native HA entities.
- **Safeguards** — per-zone cooldown, max daily activations, stale-sensor failsafe,
  and email alerts.
- **Web UI** — dashboard, config, schedule management, and event log — built with
  Go templates, Tailwind CSS, and htmx.
- **User authentication** — first-run setup wizard with bcrypt password hashing and
  session cookies.

## Quick start

```
cp example-config.yaml config.yaml
$EDITOR config.yaml
make build
./bedwetter
```

On first run a `bedwetter.db` SQLite database is created and seeded from
`config.yaml`. Visit `http://<host>:8080/setup` to create the admin account.

## Requirements

- Go 1.26+
- MQTT broker (e.g. Mosquitto)
- Optional: Home Assistant instance
- Optional: OpenWeatherMap API key
- Linux with SQLite support

## Configuration

See `config.yaml` for all settings: MQTT broker, Home Assistant connection,
zone definitions (MQTT topics and/or HA entity IDs), thresholds, schedules,
weather API key, SMTP alerts, and web listen address.

After first run, all config is managed through the web UI and stored in SQLite.

## License

MIT
