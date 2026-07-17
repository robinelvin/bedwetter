## Remote Device Safety

BedWetter controls valves over the network. If BedWetter goes offline (crash, network
loss, power cut) while a valve is open, the valve stays open and wastes water — or floods.

Two independent mechanisms let the remote device enforce its own timeout:

| Path | Mechanism | Who enforces the timeout |
|------|-----------|-------------------------|
| Home Assistant / ESPHome | `duration` in `switch.turn_on` service data | ESPHome firmware |
| Direct MQTT | Heartbeat topic with timeout metadata | Device firmware |

Both run simultaneously. A zone uses whichever path matches its configuration.

---

### Configuration

#### Global

```yaml
# config.yaml
heartbeat_interval: 30   # seconds between heartbeats (default 30, 0 to disable)
```

#### Per-zone

```yaml
zones:
  - name: Front Lawn
    heartbeat_timeout: 90   # device turns off if no heartbeat for 90s
                             # 0 = auto (3 × heartbeat_interval)
```

Both values are also editable in the web UI under Zone Configuration.

---

### Solution A: ESPHome Duration

When BedWetter controls a valve through Home Assistant (via `valve_switch_entity`), it
calls `switch.turn_on` with a `duration` parameter:

```json
POST /api/services/switch/turn_on
{
  "entity_id": "switch.front_lawn_valve",
  "duration": "00:05:00"
}
```

ESPHome natively supports this. The device starts an internal timer and automatically
turns off the switch after the duration — even if BedWetter crashes.

#### Required ESPHome Configuration

Your ESPHome device must be configured to respect the `duration` parameter. Example:

```yaml
# esphome valve device
switch:
  - platform: gpio
    id: valve
    name: "Front Lawn Valve"
    pin: GPIO5

    # Optional: manual fallback timer
    # This is a belt-and-suspenders safety net.
    # The HA duration parameter takes precedence.
    on_turn_on:
      - delay: 10m
      - switch.turn_off:
          id: valve

    on_turn_off:
      - switch.turn_off:
          id: valve
```

**How it works:**
1. BedWetter calls `switch.turn_on` with `duration: "00:05:00"`
2. ESPHome turns on the GPIO pin
3. ESPHome starts an internal 5-minute timer
4. After 5 minutes, ESPHome turns off the switch automatically
5. If BedWetter closes the valve early, it calls `switch.turn_off`, cancelling the timer

**Notes:**
- The `duration` parameter format is `HH:MM:SS`
- Duration is always set to `max_watering_seconds` for the zone
- For manual opens from the web UI / HA dashboard, the same duration applies as a safety cap

---

### Solution B: MQTT Heartbeat

For devices using direct MQTT control (via `valve_command_topic`), BedWetter publishes
periodic heartbeat messages while a valve is open. The device must subscribe to its
heartbeat topic and turn off locally if heartbeats stop.

#### Topic Structure

```
bedwetter/heartbeat/<zone_slug>
```

Where `<zone_slug>` is the zone name with spaces replaced by underscores
(e.g., `Front_Lawn` for zone "Front Lawn").

#### Payload

```json
{
  "zone": "Front Lawn",
  "duration": 300,
  "timeout": 90
}
```

| Field | Type | Description |
|-------|------|-------------|
| `zone` | string | Zone name |
| `duration` | int | Total watering duration in seconds (from `max_watering_seconds`) |
| `timeout` | int | Seconds the device should wait before turning off if no heartbeat received |

#### Timing

- Heartbeats are published every `heartbeat_interval` seconds (default 30)
- A heartbeat is published immediately when the valve opens
- Heartbeats stop when the valve is closed or BedWetter shuts down
- The device should turn off if it doesn't receive a heartbeat within `timeout` seconds
- Default timeout = 3 × heartbeat_interval (e.g., 90 seconds)

#### Device Firmware Contract

```python
# Pseudocode for an MQTT-controlled valve device

HEARTBEAT_TOPIC = "bedwetter/heartbeat/my_zone"  # subscribe to this
VALVE_TOPIC = "my_zone/valve/command"              # publish ON/OFF here
HEARTBEAT_TIMEOUT = 90  # seconds, or read from first heartbeat payload

last_heartbeat = None
valve_on = False

def on_heartbeat(topic, payload):
    global last_heartbeat, HEARTBEAT_TIMEOUT
    last_heartbeat = now()
    HEARTBEAT_TIMEOUT = payload["timeout"]  # use server-advertised timeout

def on_valve_command(topic, payload):
    global valve_on
    if payload == "ON":
        turn_valve_on()
        valve_on = True
    elif payload == "OFF":
        turn_valve_off()
        valve_on = False

def loop():
    """Run periodically (e.g., every 5 seconds)."""
    if valve_on and last_heartbeat:
        if now() - last_heartbeat > HEARTBEAT_TIMEOUT:
            log("Heartbeat timeout — turning off valve")
            turn_valve_off()
            valve_on = False
```

**Key points:**
- The device must subscribe to `bedwetter/heartbeat/<zone_slug>`
- Parse the JSON payload to get `timeout` (and optionally `duration`)
- Track time since last heartbeat
- Turn off the valve if the timeout is exceeded
- The heartbeat topic is only published while the valve is open

#### Example: ESPHome with MQTT (not HA API)

If your ESPHome device uses MQTT directly (not through HA), add an `on_message`
automation to handle heartbeats:

```yaml
mqtt:
  broker: mqtt.local
  topic_prefix: ""

switch:
  - platform: gpio
    id: valve
    name: "Valve"
    pin: GPIO5

sensor:
  - platform: wifi_signal
    name: "WiFi Signal"

# Heartbeat timeout safety: if no heartbeat in 90s, turn off
interval:
  - interval: 10s
    then:
      - if:
          condition:
            and:
              - switch.is_on: valve
              - lambda: |-
                  return (millis() - id(last_heartbeat_ms)) > 90000;
          then:
            - switch.turn_off: valve
            - logger.log: "Heartbeat timeout — valve turned off"

# Track last heartbeat time
globals:
  - id: last_heartbeat_ms
    type: int
    restore_value: no
    initial_value: "0"

mqtt:
  on_message:
    - topic: bedwetter/heartbeat/my_zone
      payload: "+"
      then:
        - lambda: |-
            id(last_heartbeat_ms) = millis();
```

---

### How Both Mechanisms Work Together

For a zone using both HA (ESPHome) and MQTT:

1. BedWetter sends `switch.turn_on` with `duration: "00:05:00"` to ESPHome via HA
2. BedWetter simultaneously publishes heartbeats to `bedwetter/heartbeat/<zone>`
3. The ESPHome device has two safety nets:
   - Its internal duration timer (5 minutes)
   - The heartbeat timeout (90 seconds by default)
4. If BedWetter goes offline:
   - The heartbeat stops → device turns off after timeout
   - If the heartbeat mechanism fails, the ESPHome duration timer still fires
5. If the MQTT broker goes down:
   - The HA REST API may still work (if on a different network path)
   - The ESPHome duration timer runs on-device, independent of MQTT

---

### Disabling Heartbeats

Set `heartbeat_interval: 0` in the global config to disable heartbeat publishing entirely.
ESPHome duration still works independently.

Per-zone, set `heartbeat_timeout: 0` to use the default (3 × interval). There is no
way to disable heartbeats for a single zone without disabling them globally.
