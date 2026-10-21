package domain

import (
	"errors"
	"time"
)

type TermUnit string

const (
	Day   TermUnit = "day"
	Month TermUnit = "month"
	Year  TermUnit = "year"
)

// Schedule retains the original UTC calendar anchor. Periods is the number of
// paid intervals, not the day/month number of the most recently clamped date.
type Schedule struct {
	Anchor   time.Time
	Unit     TermUnit
	Interval int
	Periods  int
}

func (s Schedule) End() (time.Time, error) {
	if s.Anchor.IsZero() || s.Anchor.Year() < 1 || s.Anchor.Year() > 9999 || s.Interval < 1 || s.Interval > 36500 || s.Periods < 1 || s.Periods > 36500 {
		return time.Time{}, errors.New("invalid contract schedule")
	}
	anchor := s.Anchor.UTC()
	steps := int64(s.Interval) * int64(s.Periods)
	if steps > 36500 {
		return time.Time{}, errors.New("contract exceeds supported interval range")
	}
	if s.Unit == Day {
		end := anchor.AddDate(0, 0, int(steps))
		if end.Year() > 9999 {
			return time.Time{}, errors.New("contract exceeds supported date range")
		}
		return end, nil
	}
	if s.Unit != Month && s.Unit != Year {
		return time.Time{}, errors.New("term unit must be day, month or year")
	}
	months := steps
	if s.Unit == Year {
		months *= 12
	}
	index := int64(anchor.Year()-1)*12 + int64(anchor.Month()-1) + months
	year, month := int(index/12)+1, time.Month(index%12)+1
	if year > 9999 {
		return time.Time{}, errors.New("contract exceeds supported date range")
	}
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	day := min(anchor.Day(), last)
	return time.Date(year, month, day, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), time.UTC), nil
}

// Renew joins an active contract; after expiry the new payment starts an anchor.
// Call only once per verified payment, inside the eventual ledger transaction.
func (s Schedule) Renew(paidAt time.Time) (Schedule, error) {
	end, err := s.End()
	if err != nil {
		return Schedule{}, err
	}
	if paidAt.IsZero() || paidAt.Before(s.Anchor) {
		return Schedule{}, errors.New("payment time precedes the contract")
	}
	if paidAt.After(end) {
		s.Anchor, s.Periods = paidAt.UTC(), 1
	} else {
		s.Periods++
	}
	_, err = s.End()
	return s, err
}
