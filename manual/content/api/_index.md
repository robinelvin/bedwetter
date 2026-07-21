---
title: "REST API"
weight: 100
---

BedWetter provides a REST API for programmatic control and monitoring. The API can be used alongside or instead of the web UI.

## Base URL

```
http://<your-host>:8080
```

## Endpoints

### List All Zones

```
GET /api/zones
```

Returns a JSON array of all zones with their current state, moisture readings, and next scheduled watering time.

**Response:**

```json
[
  {
    "name": "Raised Bed 1",
    "state": "idle",
    "moisture": 52,
    "next_watering": "2024-07-15T06:00:00Z"
  }
]
```

### Manually Water a Zone

```
POST /api/zones/:name/water
```

Opens the valve for the zone, bypassing the watering window, schedule, sensor, cooldown, and rain checks. The valve opens for `max_watering_seconds` or until manually stopped.

This is a force-open action and does not increment the daily activation counter.

### Stop Watering a Zone

```
POST /api/zones/:name/stop
```

Closes the valve immediately and sets a force-close flag that prevents automatic or scheduled triggers from reopening it until the flag is cleared.

### Acknowledge a Failsafe

```
POST /api/zones/:name/acknowledge
```

Clears the failsafe state for a zone, allowing it to return to normal operation.

## Example Usage

### List zones with curl

```bash
curl http://localhost:8080/api/zones
```

### Manually water a zone

```bash
curl -X POST http://localhost:8080/api/zones/Raised%20Bed%201/water
```

### Stop watering

```bash
curl -X POST http://localhost:8080/api/zones/Raised%20Bed%201/stop
```

### Acknowledge failsafe

```bash
curl -X POST http://localhost:8080/api/zones/Raised%20Bed%201/acknowledge
```

## Web Routes (htmx)

The web UI also exposes HTMX-friendly routes that return HTML partials:

| Route | Method | Description |
|-------|--------|-------------|
| `/dashboard/zones` | GET | Zone cards partial |
| `/dashboard/weather` | GET | Weather widget partial |
| `/zones/:id/card` | GET | Single zone card partial |
| `/zones/:id/history` | GET | Sensor reading history |
| `/zones/:name/open` | POST | Open valve |
| `/zones/:name/close` | POST | Close valve |
| `/zones/:name/force-close` | POST | Force-close valve |
| `/zones/:name/acknowledge` | POST | Acknowledge failsafe |
| `/zones/all/open` | POST | Open all valves |
| `/zones/all/close` | POST | Close all valves |
