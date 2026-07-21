---
title: "Web UI"
weight: 90
---

BedWetter includes a built-in web interface for monitoring and managing your irrigation system. The UI is built with Go templates, htmx, Tailwind CSS, and DaisyUI.

## Accessing the UI

Open your browser to:

```
http://<your-host>:8080
```

The default port is `8080`. Change it with the `web.listen_addr` configuration option.

## Dashboard

The main dashboard provides an at-a-glance view of your irrigation system:

- **Zone cards** — each zone shows its current state (idle, watering, cooldown, etc.), moisture percentage, and status badges
- **Weather widget** — current conditions and hourly forecast using Meteocons weather icons
- **Open All / Close All** — bulk valve control buttons
- **Zone cards auto-refresh** via htmx partial updates

### Zone Card Actions

From a zone card on the dashboard, you can:

- **Open** — manually open the valve
- **Close** — close the valve
- **Force Close** — emergency stop, prevents automatic reopening
- **Acknowledge** — clear a failsafe state

## Zone Detail

Click a zone card to view its detail page:

- **Schedule timeline** — visual representation of the watering schedule
- **Event log** — history of valve opens/closes and state changes for this zone
- **Sensor history** — moisture, humidity, and temperature readings over time
- **Configuration** — view and edit zone settings

## Schedules

The Schedules page lets you manage watering schedules:

- Create new schedule entries (zone, day, time, duration)
- Edit existing entries
- Create month overrides for specific months
- Delete schedule entries

## Configuration

The Configuration page is divided into sections:

- **MQTT** — broker connection details (host, port, credentials)
- **Home Assistant** — URL and access token
- **Alerts** — email SMTP settings and stale sensor timeout
- **ntfy** — push notification server and topic settings
- **Weather** — latitude, longitude, rain threshold, rain sensor
- **Master Valve** — MQTT topic or HA entity for the master shutoff
- **Zones** — add, edit, or delete watering zones

Each section saves independently via HTMX form submissions.

## Events

The Events page shows a paginated log of all system events:

- Valve open/close events
- State transitions
- Sensor readings
- Alert triggers
- Filterable by zone

## Profile

The Profile page allows you to:

- Edit your name and email
- Change your password (minimum 6 characters)
- Passwords are stored as bcrypt hashes

## Authentication

- **First-run setup** at `/setup` creates the admin account
- Default credentials before any users exist: `admin` / `bedwetter`
- Session-based authentication using HTTP-only cookies
- Session IDs are 32-byte random hex strings
