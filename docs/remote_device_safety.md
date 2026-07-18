## Remote Device Safety

BedWetter controls valves over the network. If BedWetter goes offline (crash, network
loss, power cut) while a valve is open, the valve stays open and wastes water — or floods.

Two independent mechanisms let the remote device enforce its own timeout:

| Path | Mechanism | Who enforces the timeout |
|------|-----------|-------------------------|
| Home Assistant / ESPHome | MQTT timeout topic + heartbeat | ESPHome firmware |
| Direct MQTT | MQTT timeout topic + heartbeat | Device firmware |

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

### Solution A: ESPHome via MQTT Timeout

When BedWetter controls a valve through Home Assistant (via `valve_switch_entity`), it
publishes the watering duration to an MQTT topic before calling `switch.turn_on`:

1. BedWetter publishes the duration (in seconds) to `bedwetter/timeout/<zone_slug>`
2. BedWetter calls `switch.turn_on` via the HA REST API (no extra service data)
3. BedWetter publishes periodic heartbeats to `bedwetter/heartbeat/<zone_slug>`

ESPHome subscribes to the timeout topic, sets an internal timer, then the switch's
`on_turn_on` automation uses that timer for auto-off.

#### MQTT Topics

| Topic | Payload | Description |
|-------|---------|-------------|
| `bedwetter/timeout/<slug>` | `"300"` (plain integer) | Sent once when valve opens |
| `bedwetter/heartbeat/<slug>` | JSON (see below) | Sent every `heartbeat_interval` seconds |

#### Required ESPHome Configuration

```yaml
mqtt:
  broker: mqtt.local
  topic_prefix: ""
  on_message:
    - topic: bedwetter/timeout/front_lawn
      payload: "+"
      then:
        - lambda: |-
            id(watering_duration).set_state(atoi(x.c_str()));

number:
  - platform: template
    name: "Watering Duration"
    id: watering_duration
    optimistic: true
    min_value: 1
    max_value: 3600
    initial_value: 300

switch:
  - platform: gpio
    id: valve
    name: "Front Lawn Valve"
    pin: GPIO5
    on_turn_on:
      - script.execute: auto_off_timer
    on_turn_off:
      - script.stop: auto_off_timer

script:
  - id: auto_off_timer
    then:
      - delay: !lambda "return id(watering_duration).state * 1000;"
      - switch.turn_off: valve

# Heartbeat timeout safety: if no heartbeat in 90s, turn off
globals:
  - id: last_heartbeat_ms
    type: int
    restore_value: no
    initial_value: "0"

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
            - logger.log: "Heartbeat timeout - valve turned off"

mqtt:
  on_message:
    - topic: bedwetter/heartbeat/front_lawn
      payload: "+"
      then:
        - lambda: |-
            id(last_heartbeat_ms) = millis();
```

**How it works:**
1. BedWetter publishes `"300"` to `bedwetter/timeout/front_lawn`
2. ESPHome receives it and sets `watering_duration` to 300
3. BedWetter calls `switch.turn_on` via HA
4. ESPHome turns on the GPIO pin and starts the auto-off timer (5 minutes)
5. BedWetter sends periodic heartbeats; ESPHome updates `last_heartbeat_ms`
6. After 5 minutes, ESPHome turns off the switch automatically
7. If heartbeats stop (BedWetter offline), the heartbeat timeout fires after 90s

**Notes:**
- Duration is always set to `max_watering_seconds` for the zone
- For manual opens from the web UI / HA dashboard, the same duration applies as a safety cap
- The timeout topic is only published while the valve is open

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

For a zone using HA (ESPHome) with MQTT enabled:

1. BedWetter publishes `"300"` to `bedwetter/timeout/<zone>` via MQTT
2. BedWetter simultaneously calls `switch.turn_on` via HA REST API
3. BedWetter publishes periodic heartbeats to `bedwetter/heartbeat/<zone>`
4. The ESPHome device has two safety nets:
   - Its internal auto-off timer (set from the timeout topic)
   - The heartbeat timeout (90 seconds by default)
5. If BedWetter goes offline:
   - The heartbeat stops → device turns off after timeout
   - If the heartbeat mechanism fails, the auto-off timer still fires

---

### Disabling Heartbeats

Set `heartbeat_interval: 0` in the global config to disable heartbeat publishing entirely.
The MQTT timeout topic is still published when the valve opens.

Per-zone, set `heartbeat_timeout: 0` to use the default (3 × interval). There is no
way to disable heartbeats for a single zone without disabling them globally.
