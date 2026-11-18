package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

type AIScheduleSpec struct {
	Frequency string `json:"frequency,omitempty"`
	TimeZone  string `json:"time_zone,omitempty"`
	At        string `json:"at,omitempty"`
	Weekdays  []int  `json:"weekdays,omitempty"`
	MonthDay  int    `json:"month_day,omitempty"`
	OnceAt    string `json:"once_at,omitempty"`
}

func aiScheduleSpec(raw json.RawMessage) AIScheduleSpec {
	var spec AIScheduleSpec
	_ = json.Unmarshal(raw, &spec)
	return spec
}
func aiNextSchedule(spec AIScheduleSpec, minutes int, after time.Time) (time.Time, error) {
	if spec.Frequency == "" || spec.Frequency == "interval" {
		if minutes < 15 || minutes > 43200 {
			return time.Time{}, ErrInvalid
		}
		return after.Add(time.Duration(minutes) * time.Minute), nil
	}
	if spec.Frequency == "once" {
		at, err := time.Parse(time.RFC3339, spec.OnceAt)
		if err != nil || !at.After(after) {
			return time.Time{}, ErrInvalid
		}
		return at.UTC(), nil
	}
	if spec.Frequency != "daily" && spec.Frequency != "weekly" && spec.Frequency != "monthly" {
		return time.Time{}, ErrInvalid
	}
	location, err := time.LoadLocation(spec.TimeZone)
	if err != nil {
		return time.Time{}, ErrInvalid
	}
	clock, err := time.Parse("15:04", spec.At)
	if err != nil {
		return time.Time{}, ErrInvalid
	}
	if spec.Frequency == "weekly" {
		if len(spec.Weekdays) < 1 || len(spec.Weekdays) > 7 {
			return time.Time{}, ErrInvalid
		}
		for _, d := range spec.Weekdays {
			if d < 0 || d > 6 {
				return time.Time{}, ErrInvalid
			}
		}
	}
	if spec.Frequency == "monthly" && (spec.MonthDay < 1 || spec.MonthDay > 31) {
		return time.Time{}, ErrInvalid
	}
	local := after.In(location)
	for d := 0; d < 400; d++ {
		date := local.AddDate(0, 0, d)
		at := time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
		if !at.After(after) {
			continue
		}
		if at.Hour() != clock.Hour() || at.Minute() != clock.Minute() {
			continue
		}
		if spec.Frequency == "weekly" && !slices.Contains(spec.Weekdays, int(at.Weekday())) {
			continue
		}
		if spec.Frequency == "monthly" && at.Day() != spec.MonthDay {
			continue
		}
		return at.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("未找到下次执行时间")
}
