package domain

import (
	"math"
	"testing"
	"time"
)

func date(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCalendarAnchorSurvivesClampedMonths(t *testing.T) {
	s := Schedule{Anchor: date("2027-01-31T14:30:00Z"), Unit: Month, Interval: 1, Periods: 1}
	end, err := s.End()
	if err != nil || !end.Equal(date("2027-02-28T14:30:00Z")) {
		t.Fatalf("first month: %v %v", end, err)
	}
	s, err = s.Renew(date("2027-02-15T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	end, err = s.End()
	if err != nil || !end.Equal(date("2027-03-31T14:30:00Z")) {
		t.Fatalf("anchor drifted: %v %v", end, err)
	}
}

func TestCalendarLeapYearTimezoneAndExpiry(t *testing.T) {
	cases := []struct {
		name     string
		schedule Schedule
		want     string
	}{
		{"leap_month", Schedule{date("2028-01-31T10:00:00Z"), Month, 1, 1}, "2028-02-29T10:00:00Z"},
		{"leap_year", Schedule{date("2028-02-29T10:00:00Z"), Year, 1, 1}, "2029-02-28T10:00:00Z"},
		{"restore_leap_anchor", Schedule{date("2028-02-29T10:00:00Z"), Year, 1, 4}, "2032-02-29T10:00:00Z"},
		{"utc_contract", Schedule{date("2027-02-01T02:00:00+08:00"), Month, 1, 1}, "2027-02-28T18:00:00Z"},
		{"thirty_days", Schedule{date("2027-01-31T10:00:00Z"), Day, 30, 1}, "2027-03-02T10:00:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			end, err := c.schedule.End()
			if err != nil || !end.Equal(date(c.want)) {
				t.Fatalf("got %v, %v; want %s", end, err, c.want)
			}
		})
	}
	s := Schedule{date("2027-01-31T10:00:00Z"), Month, 1, 1}
	renewed, err := s.Renew(date("2027-03-03T11:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	end, _ := renewed.End()
	if !end.Equal(date("2027-04-03T11:00:00Z")) {
		t.Fatal("expired renewal did not restart at payment")
	}
	for _, bad := range []Schedule{{}, {date("9999-12-31T00:00:00Z"), Month, 1, 1}, {date("2027-01-01T00:00:00Z"), "unknown", 1, 1}} {
		if _, err := bad.End(); err == nil {
			t.Fatal("invalid schedule accepted")
		}
	}
}

func TestUpgradeUsesCeilingWithoutOverflow(t *testing.T) {
	start, end, quote := date("2027-01-01T00:00:00Z"), date("2027-01-04T00:00:00Z"), date("2027-01-03T00:00:00Z")
	money, err := UpgradeDifference(Money{"USD", 100}, Money{"USD", 200}, start, end, quote)
	if err != nil || money.Minor != 34 {
		t.Fatalf("rounding: %+v %v", money, err)
	}
	money, err = UpgradeDifference(Money{"USD", 0}, Money{"USD", math.MaxInt64}, start, end, start)
	if err != nil || money.Minor != math.MaxInt64 {
		t.Fatalf("overflow: %+v %v", money, err)
	}
	if _, err := UpgradeDifference(Money{"USD", 100}, Money{"EUR", 200}, start, end, quote); err == nil {
		t.Fatal("cross-currency upgrade accepted")
	}
	if _, err := UpgradeDifference(Money{"USD", 100}, Money{"USD", 200}, start, end, end); err == nil {
		t.Fatal("expired quote accepted")
	}
}
