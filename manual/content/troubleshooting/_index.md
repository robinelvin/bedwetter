---
title: "Troubleshooting"
weight: 110
---

Common issues and solutions for BedWetter.

## Application Won't Start

### Port already in use

```
listen tcp :8080: bind: address already in use
```

Change the port in your configuration:

```yaml
web:
  listen_addr: ":8081"
```

### SQLite / CGO errors

Ensure CGO is enabled when building:

```bash
CGO_ENABLED=1 go build -o bedwetter .
```

## Zones Not Watering

### Check the watering window

Zones only water within their configured window (`earliest_watering_time` to `latest_watering_time`). If the current time is outside the window, no watering will occur.

### Check the cooldown

After each watering cycle, the zone enters a cooldown period (`cooldown_minutes`). No watering will occur during cooldown.

### Check daily activation limit

Each zone has a `max_activations_per_day` cap. Once reached, the zone won't water again until midnight.

### Check rain detection

- **Forecast rain:** If the Open-Meteo forecast predicts rain ≥ `rain_threshold_mm` in the next 24 hours, outdoor zones skip watering.
- **Rain sensor:** If the physical rain sensor is active, all outdoor zones are locked out.
- **Indoor zones:** Set `indoor: true` on zones that should bypass rain detection.

### Check stale sensor

If no sensor reading is received within `stale_sensor_minutes`, the zone enters a failsafe state and will not water. Check that your MQTT sensor is publishing to the correct topic.

## Sensor Readings Not Updating

1. Verify the MQTT topic in your zone configuration matches the topic your sensor publishes to
2. Check that your MQTT broker is running and the sensor is connected
3. Use an MQTT client (e.g., `mosquitto_sub`) to subscribe to the topic and verify messages are arriving
4. If using HA entities, verify BedWetter can reach the Home Assistant REST API

## Valve Not Responding

1. Verify the valve command topic or HA entity ID in your zone configuration
2. Check that your MQTT broker is routing messages to the correct topic
3. If using HA, verify the access token and HA URL are correct
4. Check the zone's event log in the web UI for error messages

## Home Assistant Entities Not Appearing

1. Ensure BedWetter can reach your HA instance (check URL and token)
2. Verify MQTT Discovery is enabled in your HA configuration
3. Restart HA's MQTT integration to pick up new discovery messages
4. Check HA's MQTT logs for incoming discovery config messages

## Failsafe State

A zone enters failsafe when:

- The sensor is stale (no readings for `stale_sensor_minutes`)
- The valve has been open longer than `max_watering_seconds`
- The valve is unavailable

To clear a failsafe, click **Acknowledge** on the zone card in the web UI, or call:

```bash
curl -X POST http://localhost:8080/api/zones/:name/acknowledge
```

## Stuck Valves

If a valve remains open due to a BedWetter failure:

1. The heartbeat safety protocol should cause the remote device to time out and close automatically
2. As a manual fallback, close the valve from the web UI or REST API
3. Check the master shutoff valve (if configured) — it closes on safety shutoff

## Database Issues

BedWetter uses SQLite and stores the database at the path configured in `db_path` (default: `bedwetter.db`).

- The database is created automatically on first run
- To reset, delete the `bedwetter.db` file and restart BedWetter
- After a reset, you'll need to go through the setup wizard again and reconfigure everything

## Logs

BedWetter logs to stdout. When running as a systemd service, view logs with:

```bash
journalctl -u bedwetter -f
```

Event details are also available in the web UI under the **Events** page and via the `bedwetter/event` MQTT topic.
