## BedWetter — Valve Control Logic

---

### Core Concepts

There are four mechanisms that can affect a valve:

1. **Schedule** — time-based trigger
2. **Sensor** — moisture threshold trigger
3. **Manual override** — user-initiated from web UI, REST API, or HA
4. **Safety shutoff** — system-initiated force close

These are evaluated in strict priority order. Higher priority always wins.

---

### Priority Order (highest to lowest)

```
1. Safety shutoff
2. Manual override (force close)
3. Manual override (force open)
4. Schedule + sensor logic
```

---

### Watering Window

A watering window defines when the system is *permitted* to water. Outside the window, no schedule or sensor trigger will open a valve.

Each zone has:
- `window_start` — earliest permitted open time (e.g. `06:00`)
- `window_end` — latest permitted *start* time (e.g. `10:00`)

A watering cycle that starts within the window is allowed to complete even if it runs past `window_end`. The window only gates the *start* of a cycle, not its completion.

```
Timeline example:

06:00         09:55      10:00           18:00
  |---- window ---|         |               |
        ↑                ↑
   cycle may start    cycle blocked
```

---

### Schedule

A schedule defines *when to check* whether watering is needed. It does not directly open the valve — it initiates an evaluation.

Each zone has a weekly schedule: a set of days and a time at which the system evaluates whether to water.

```yaml
schedule:
  days: [Monday, Wednesday, Friday]
  time: "07:00"
```

At the scheduled time, the system checks:
1. Is the current time within the watering window? → if not, skip
2. Is there a manual force-close override active? → if so, skip
3. Is the rain sensor triggered? → if so, skip
4. Is rain forecast above threshold? → if so, skip
5. Is the moisture reading above `threshold_high`? → if so, skip (already wet enough)
6. Has the daily activation limit been reached? → if so, skip
7. Is a cooldown period active from a previous cycle? → if so, skip

If all checks pass → open the valve and begin a watering cycle.

---

### Sensor-Triggered Watering

Independent of the schedule, the system continuously monitors moisture. If the sensor drops below `threshold_low` and the system is in `auto` mode, it can trigger an unscheduled watering cycle.

Sensor triggers are subject to the same gate checks as scheduled triggers, plus one additional check:

- Is the sensor reading stale (last update older than `stale_timeout`)? → if so, do not act on it; log a warning

This prevents a disconnected sensor from causing continuous watering.

```
Sensor evaluation loop (runs every sensor_check_interval, e.g. 5 minutes):

moisture < threshold_low?
  → within window?
    → no rain / forecast?
      → not at daily limit?
        → cooldown expired?
          → OPEN VALVE
```

---

### A Watering Cycle

Once a valve opens, a cycle proceeds as follows:

```
1. Record cycle start time and moisture reading
2. Open valve
3. Wait watering_duration seconds
4. Close valve
5. Record cycle end time
6. Start cooldown timer (cooldown_minutes)
7. Increment daily activation counter
8. Wait cooldown period
9. Re-read moisture sensor
10. If moisture still below threshold_high → repeat cycle (up to max_activations_per_day)
11. If max_activations reached and still dry → send alert, do not water further today
```

The `threshold_low` triggers a cycle. The `threshold_high` determines whether *another* cycle is needed after cooldown. These being separate values prevents oscillation around a single threshold.

```
Example with threshold_low=40, threshold_high=60:

moisture=35 → open valve for 5 min → cooldown 90 min → re-check
moisture=50 → still below 60 → open valve again → cooldown 90 min → re-check
moisture=65 → above 60 → stop cycling
```

---

### Cooldown

The cooldown period begins immediately after a valve closes. During cooldown:

- No sensor trigger will open the valve
- No scheduled trigger will open the valve
- Manual override CAN still open the valve

The purpose of cooldown is to allow moisture to equalise and percolate through the soil before re-evaluating. Without it the sensor near the surface reads wet immediately while the root zone is still dry, causing the system to stop watering too soon.

---

### Daily Activation Limit

Each zone has a `max_activations_per_day` counter, reset at midnight.

This is a hard cap. Once reached, the valve will not open again until midnight regardless of moisture level, forecasts, or schedule. Only a manual force-open overrides this.

---

### Manual Overrides

There are two types:

**Force open** — opens the valve immediately for a specified duration, bypassing window, schedule, sensor, cooldown, and rain checks. Does *not* bypass the safety shutoff. Does not increment the daily activation counter (it is a deliberate human action).

**Force close** — closes the valve immediately and sets a flag that prevents any automatic or scheduled trigger from reopening it until the override is cleared. This is the emergency stop. Remains active until explicitly cleared by the user.

Manual overrides are available from:
- Web UI
- REST API (`POST /api/zones/{id}/water`, `POST /api/zones/{id}/stop`)
- Home Assistant (via MQTT switch command topic)

---

### Safety Shutoff

The safety shutoff is system-initiated and overrides everything including manual force-open. It triggers when:

- Valve has been open longer than `max_open_duration` (stuck-open detection)
- System is shutting down gracefully
- Watchdog timer fires (process hang)

On safety shutoff:
1. All valves closed immediately
2. Master shutoff valve closed
3. Alert sent
4. All overrides cleared
5. System enters `fault` state — no further automatic watering until fault acknowledged in web UI

---

### State Machine Per Zone

```
         ┌─────────────────────────────┐
         │                             ▼
      [IDLE] ──schedule/sensor──► [WATERING] ──duration expires──► [COOLDOWN]
         ▲                            │                                  │
         │                      safety shutoff                    cooldown expires
         │                            │                                  │
         │                            ▼                                  │
         │                        [FAULT]                                │
         │                                                               │
         └───────────────────────────────────────────────────────────────┘

Manual force-open:  IDLE/COOLDOWN → WATERING
Manual force-close: WATERING → IDLE (clears cooldown)
Safety shutoff:     any → FAULT
Fault acknowledge:  FAULT → IDLE
```

---

### Interaction Summary Table

| Trigger | Respects window? | Respects cooldown? | Respects daily limit? | Respects rain skip? | Overrides force-close? |
|---|---|---|---|---|---|
| Schedule | Yes | Yes | Yes | Yes | No |
| Sensor | Yes | Yes | Yes | Yes | No |
| Manual force-open | No | No | No | No | Yes |
| Manual force-close | — | Clears it | — | — | — |
| Safety shutoff | — | — | — | — | Yes |
