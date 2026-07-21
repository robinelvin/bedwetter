---
title: "Alerts & Notifications"
weight: 80
---

BedWetter supports two notification channels: email (SMTP) and push notifications (ntfy).

## Email Alerts (SMTP)

### Configuration

| Field | Default | Description |
|-------|---------|-------------|
| `email` | — | Recipient email address |
| `stale_sensor_minutes` | `60` | Minutes without sensor update before triggering a stale sensor alert |
| `smtp_server` | `smtp.gmail.com` | SMTP server hostname |
| `smtp_port` | `587` | SMTP server port |
| `smtp_username` | — | SMTP authentication username |
| `smtp_password` | — | SMTP authentication password |
| `from_email` | — | Sender address |

```yaml
alerts:
  email: you@example.com
  stale_sensor_minutes: 60
  smtp_server: smtp.gmail.com
  smtp_port: 587
  smtp_username: you@gmail.com
  smtp_password: your_app_password
  from_email: bedwetter@example.com
```

### Alert Types

| Alert | Trigger |
|-------|---------|
| **Stale sensor** | No sensor reading received within `stale_sensor_minutes` |
| **Max watering cycles** | Daily activation limit reached and soil is still dry |
| **Valve stuck open** | Valve has been open longer than `max_watering_seconds` |

Email alerts are rate-limited to **once per alert type per hour** to avoid flooding your inbox.

## Push Notifications (ntfy)

[ntfy](https://ntfy.sh) is a simple HTTP-based pub-sub notification service.

### Configuration

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Enable ntfy notifications |
| `server` | `https://ntfy.sh` | ntfy server URL |
| `uuid` | — | Topic UUID (auto-generated on first run if empty) |
| `token` | — | Auth token for private ntfy servers (optional) |
| `alert_info` | `true` | Send info-level notifications |
| `alert_warn` | `true` | Send warning-level notifications |
| `alert_alarm` | `true` | Send alarm-level notifications |

```yaml
ntfy:
  enabled: true
  server: https://ntfy.sh
  uuid: ""
  token: ""
  alert_info: true
  alert_warn: true
  alert_alarm: true
```

### Severity Levels

| Level | Priority | Examples |
|-------|----------|---------|
| **info** | 3 | Valve opened, valve closed |
| **warn** | 4 | Stale sensor, max daily activations reached, force close |
| **alarm** | 5 | Failsafe activated, safety shutoff triggered |

### Subscribing

On first run, BedWetter generates a UUID-based topic. Subscribe to it using:

- **ntfy web app:** `https://ntfy.sh/<uuid>`
- **ntfy mobile app:** Add the topic `https://ntfy.sh/<uuid>`
- **curl:** `curl https://ntfy.sh/<uuid>`

The UUID is displayed on the Configuration page in the web UI after setup.
