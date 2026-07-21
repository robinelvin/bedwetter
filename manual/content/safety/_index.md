---
title: "Safety"
weight: 70
---

BedWetter controls valves over the network. If BedWetter goes offline (crash, network loss, power cut) while a valve is open, the valve stays open and wastes water — or floods. The heartbeat safety protocol mitigates this risk.

## Overview

Two independent mechanisms let remote devices enforce their own timeout:

| Mechanism | Who enforces the timeout |
|-----------|-------------------------|
| MQTT timeout topic + heartbeat | Device firmware (ESPHome or custom) |
| Heartbeat-only timeout | Device firmware |

Both can run simultaneously for defense in depth.

## Configuration

### Global

```yaml
heartbeat_interval: 30   # seconds between heartbeats (default 30, 0 to disable)
```

### Per-Zone

```yaml
zones:
  - name: Front Lawn
    heartbeat_timeout: 90   # device turns off if no heartbeat for 90s
                             # 0 = auto (3 × heartbeat_interval)
```

Both values are editable in the web UI under Zone Configuration.

## MQTT Timeout Topic

When BedWetter opens a valve, it publishes the watering duration to:

```
bedwetter/timeout/<zone_slug>
```

The payload is a plain integer (e.g., `"300"` for 300 seconds). Remote devices subscribe to this topic and set a local auto-off timer.

## MQTT Heartbeat

While a valve is open, BedWetter publishes periodic heartbeat messages to:

```
bedwetter/heartbeat/<zone_slug>
```

### Heartbeat Payload

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
| `duration` | int | Total watering duration in seconds |
| `timeout` | int | Seconds before device turns off if no heartbeat received |

### Timing

- Heartbeats are published every `heartbeat_interval` seconds (default 30)
- A heartbeat is published immediately when the valve opens
- Heartbeats stop when the valve closes or BedWetter shuts down
- The device should turn off if it doesn't receive a heartbeat within `timeout` seconds

## ESPHome Integration

### Solution A: MQTT Timeout + Heartbeat (via HA)

This is the recommended approach for ESPHome devices controlled through Home Assistant:

1. BedWetter publishes `"300"` to `bedwetter/timeout/<zone>`
2. BedWetter calls `switch.turn_on` via the HA REST API
3. BedWetter publishes periodic heartbeats to `bedwetter/heartbeat/<zone>`
4. ESPHome subscribes to the timeout topic and sets an auto-off timer
5. ESPHome also monitors heartbeats for a second safety net

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

### Solution B: Direct MQTT (no HA)

For devices using direct MQTT control, the device firmware must:

1. Subscribe to `bedwetter/heartbeat/<zone_slug>`
2. Parse the JSON payload to get `timeout`
3. Track time since last heartbeat
4. Turn off the valve if the timeout is exceeded

#### Pseudocode

```python
HEARTBEAT_TOPIC = "bedwetter/heartbeat/my_zone"
VALVE_TOPIC = "my_zone/valve/command"
HEARTBEAT_TIMEOUT = 90

last_heartbeat = None
valve_on = False

def on_heartbeat(topic, payload):
    global last_heartbeat, HEARTBEAT_TIMEOUT
    last_heartbeat = now()
    HEARTBEAT_TIMEOUT = payload["timeout"]

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
            turn_valve_off()
            valve_on = False
```

## Disabling Heartbeats

Set `heartbeat_interval: 0` in the global config to disable heartbeat publishing entirely. The MQTT timeout topic is still published when the valve opens.

Per-zone, set `heartbeat_timeout: 0` to use the default (3 × interval). There is no way to disable heartbeats for a single zone without disabling them globally.
