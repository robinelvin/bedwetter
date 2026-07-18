package scheduler

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/robinelvin/bedwetter/models"
	"github.com/robinelvin/bedwetter/zones"
)

func WeekdayFromString(s string) (time.Weekday, bool) {
	if s == "" {
		return time.Sunday, false
	}
	key := strings.ToLower(strings.TrimSpace(s))
	if len(key) >= 3 {
		key = key[:3]
	}
	switch key {
	case "mon":
		return time.Monday, true
	case "tue":
		return time.Tuesday, true
	case "wed":
		return time.Wednesday, true
	case "thu":
		return time.Thursday, true
	case "fri":
		return time.Friday, true
	case "sat":
		return time.Saturday, true
	case "sun":
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}

func nextOccurrenceForEntry(now time.Time, entry models.ScheduleConfig, loc *time.Location) (time.Time, bool) {
	if entry.Time == "" {
		return time.Time{}, false
	}
	schedTime, err := time.ParseInLocation("15:04", entry.Time, loc)
	if err != nil {
		return time.Time{}, false
	}
	for days := 0; days <= 366; days++ {
		candidateDate := now.AddDate(0, 0, days)
		if entry.Month > 0 && int(candidateDate.Month()) != entry.Month {
			continue
		}
		if entry.DayOfWeek != "" {
			if wd, ok := WeekdayFromString(entry.DayOfWeek); ok {
				if candidateDate.Weekday() != wd {
					continue
				}
			} else {
				continue
			}
		}
		candidate := time.Date(candidateDate.Year(), candidateDate.Month(), candidateDate.Day(), schedTime.Hour(), schedTime.Minute(), 0, 0, loc)
		if candidate.After(now) {
			return candidate, true
		}
	}
	return time.Time{}, false
}

func NextScheduledOccurrence(now time.Time, entries []models.ScheduleConfig) (time.Time, bool) {
	if len(entries) == 0 {
		return time.Time{}, false
	}
	loc := now.Location()
	var best time.Time
	for _, entry := range entries {
		if t, ok := nextOccurrenceForEntry(now, entry, loc); ok {
			if best.IsZero() || t.Before(best) {
				best = t
			}
		}
	}
	if best.IsZero() {
		return time.Time{}, false
	}
	return best, true
}

// nextWindowOpen computes the next time the watering window opens from now.
// If now is before today's earliest, returns today at earliest.
// If now is within the window (between earliest and latest), returns now.
// If now is past today's latest, returns tomorrow at earliest.
func nextWindowOpen(now time.Time, earliestStr, latestStr string) (time.Time, bool) {
	if earliestStr == "" {
		earliestStr = "06:00"
	}
	earliestMin := zones.ParseTimeToMinutes(earliestStr)
	if earliestMin < 0 {
		return time.Time{}, false
	}

	latestStr = strings.TrimSpace(latestStr)
	if latestStr == "" {
		latestStr = "10:00"
	}
	latestMin := zones.ParseTimeToMinutes(latestStr)

	currentMin := now.Hour()*60 + now.Minute()
	loc := now.Location()
	todayEarliest := time.Date(now.Year(), now.Month(), now.Day(), earliestMin/60, earliestMin%60, 0, 0, loc)

	if currentMin < earliestMin {
		return todayEarliest, true
	}

	if latestMin >= 0 && currentMin > latestMin {
		tomorrowEarliest := todayEarliest.AddDate(0, 0, 1)
		return tomorrowEarliest, true
	}

	return now, true
}

// NextWateringForZone returns the next time the zone will water and the reason.
// When moisture is below threshold_low and the zone is in an actionable state,
// it considers both the next sensor-triggered window open and the next scheduled
// occurrence, returning whichever is sooner.
func NextWateringForZone(now time.Time, snap zones.ZoneSnapshot, scheduleEntries []models.ScheduleConfig) (time.Time, string) {
	schedTime, hasSched := NextScheduledOccurrence(now, scheduleEntries)

	if !math.IsNaN(snap.Moisture) && snap.Config.ThresholdLow > 0 && snap.Moisture < float64(snap.Config.ThresholdLow) {
		if snap.State != zones.StateFailsafe && snap.State != zones.StateForceClosed {
			earliest := snap.Config.EarliestWateringTime
			latest := snap.Config.LatestWateringTime

			if snap.Config.CooldownMinutes > 0 && !snap.LastWaterEnd.IsZero() {
				cooldownEnd := snap.LastWaterEnd.Add(time.Duration(snap.Config.CooldownMinutes) * time.Minute)
				if now.Before(cooldownEnd) {
					return cooldownEnd, fmt.Sprintf("Cooldown until %s", cooldownEnd.Format("15:04"))
				}
			}

			if snap.State == zones.StateWatering && !snap.WateringStarted.IsZero() {
				wateringEnd := snap.WateringStarted.Add(time.Duration(snap.Config.MaxWateringSeconds) * time.Second)
				earliestAfterWatering := wateringEnd
				if snap.Config.CooldownMinutes > 0 {
					earliestAfterWatering = wateringEnd.Add(time.Duration(snap.Config.CooldownMinutes) * time.Minute)
				}
				if now.Before(earliestAfterWatering) {
					if windowTime, ok := nextWindowOpen(earliestAfterWatering, earliest, latest); ok {
						if !hasSched || !windowTime.After(schedTime) {
							return windowTime, "After current watering + cooldown"
						}
					}
					if hasSched {
						return schedTime, "Schedule"
					}
				}
			}

			if windowTime, ok := nextWindowOpen(now, earliest, latest); ok {
				if !hasSched || !windowTime.After(schedTime) {
					return windowTime, "Soil moisture low"
				}
			}
		}
	}

	if hasSched {
		return schedTime, "Schedule"
	}
	return time.Time{}, ""
}

type ScheduleBar struct {
	StartPct float64
	WidthPct float64
	Start    string
	End      string
	Label    string
}

type WeekDaySchedule struct {
	Date string
	Day  string
	Bars []ScheduleBar
}

func BuildWeekSchedule(now time.Time, entries []models.ScheduleConfig) []WeekDaySchedule {
	month := int(now.Month())
	var result []WeekDaySchedule

	for days := 0; days < 7; days++ {
		d := now.AddDate(0, 0, days)
		dateStr := d.Format("Mon 2 Jan")
		weekdayStr := d.Weekday().String()[:3]

		var monthEntries []models.ScheduleConfig
		var weekdayEntries []models.ScheduleConfig
		for _, e := range entries {
			if e.Month > 0 {
				if e.Month == month {
					if wd, ok := WeekdayFromString(e.DayOfWeek); ok && d.Weekday() == wd {
						monthEntries = append(monthEntries, e)
					}
				}
			} else if e.DayOfWeek != "" {
				if wd, ok := WeekdayFromString(e.DayOfWeek); ok && d.Weekday() == wd {
					weekdayEntries = append(weekdayEntries, e)
				}
			}
		}

		active := weekdayEntries
		if len(monthEntries) > 0 {
			active = monthEntries
		}

		var bars []ScheduleBar
		for _, e := range active {
			t, err := time.Parse("15:04", e.Time)
			if err != nil {
				continue
			}
			startMin := t.Hour()*60 + t.Minute()
			endMin := startMin + e.Duration
			if endMin > 24*60 {
				endMin = 24 * 60
			}

			startPct := float64(startMin) / float64(24*60) * 100
			widthPct := float64(e.Duration) / float64(24*60*60) * 100
			endTime := t.Add(time.Duration(e.Duration) * time.Second)

			bars = append(bars, ScheduleBar{
				StartPct: startPct,
				WidthPct: widthPct,
				Start:    t.Format("15:04"),
				End:      endTime.Format("15:04"),
				Label:    fmt.Sprintf("%dm", e.Duration/60),
			})
		}

		result = append(result, WeekDaySchedule{
			Date: dateStr,
			Day:  weekdayStr,
			Bars: bars,
		})
	}
	return result
}
