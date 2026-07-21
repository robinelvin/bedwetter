---
title: "Schedules"
weight: 40
---

Schedules define *when to check* whether watering is needed. They do not directly open the valve — they initiate an evaluation of the zone's conditions.

## Weekly Schedules

Each zone can have a weekly schedule: one or more day-and-time entries that trigger evaluation.

```yaml
schedules:
  - zone_name: Raised Bed 1
    schedule:
      - day_of_week: Mon
        time: "06:00"
        duration: 300
      - day_of_week: Wed
        time: "06:00"
        duration: 300
      - day_of_week: Fri
        time: "06:00"
        duration: 300
```

At each scheduled time, BedWetter evaluates:

1. Is the current time within the watering window?
2. Is a manual force-close active?
3. Is the rain sensor triggered?
4. Is rain forecast above threshold?
5. Is moisture above `threshold_high`?
6. Has the daily activation limit been reached?
7. Is a cooldown period active?

If all checks pass, a watering cycle begins.

## Day-of-Week Format

Days use three-letter abbreviations: `Mon`, `Tue`, `Wed`, `Thu`, `Fri`, `Sat`, `Sun`.

## Month Overrides

Month overrides provide completely different schedules for specific months. When a month override is active for the current month, it takes full precedence over the weekly schedule.

```yaml
schedules:
  - zone_name: Raised Bed 1
    schedule:
      - day_of_week: Mon
        time: "06:00"
        duration: 300
    month_overrides:
      - month: 7
        schedule:
          - day_of_week: Mon
            time: "06:00"
            duration: 360
          - day_of_week: Wed
            time: "06:00"
            duration: 360
          - day_of_week: Fri
            time: "06:00"
            duration: 360
          - day_of_week: Sun
            time: "06:00"
            duration: 360
```

In this example, during July the zone waters Mon/Wed/Fri/Sun at 06:00 for 360 seconds instead of the normal Mon/Wed/Fri schedule at 300 seconds.

## Seasonal Multipliers

In addition to (or instead of) month overrides, seasonal multipliers scale the scheduled duration per month. This is configured per-zone, not per-schedule.

```yaml
# In zone config:
seasonal_multipliers:
  1: 0.5    # January: 50% duration (150s instead of 300s)
  4: 0.8    # April: 80% duration (240s)
  7: 1.5    # July: 150% duration (450s)
  10: 1.0   # October: normal duration
```

Multipliers apply to the duration from the schedule or month override. Months not listed use a multiplier of `1.0`.

## Schedule Evaluation Flow

```
Scheduled time arrives
  → Is it within the watering window?       No → Skip
  → Is a force-close override active?       Yes → Skip
  → Is the rain sensor triggered?           Yes → Skip
  → Is rain forecast above threshold?       Yes → Skip
  → Is moisture above threshold_high?       Yes → Skip (already wet)
  → Has daily activation limit been reached? Yes → Skip
  → Is cooldown active?                     Yes → Skip
  → All checks passed → OPEN VALVE
```

## Managing Schedules

Schedules are managed through the web UI at **Schedules**. You can:

- Add new schedule entries for any zone
- Edit day, time, and duration
- Create month overrides for specific months
- Delete schedule entries

All schedule data is stored in SQLite and takes effect immediately.
