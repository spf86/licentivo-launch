package app

import (
	"testing"
	"time"

	"licentivo-launch/internal/storage"
)

func TestUpgradeUsesPurchasedIntervalPrices(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(90 * 24 * time.Hour)
	three, seven := uint64(3), uint64(7)
	version := func(amount, threePeriodAmount int64) storage.PlanVersion {
		return storage.PlanVersion{Currency: "USD", AmountMinor: amount, PurchaseType: "fixed_term", PurchaseOptions: marshal([]PurchaseOption{{Periods: 3, AmountMinor: threePeriodAmount, Enabled: true}})}
	}
	for _, test := range []struct {
		name       string
		paid       int64
		target     int64
		elapsed    time.Duration
		cumulative *uint64
		overlay    bool
		legacy     bool
		want       int64
	}{
		{name: "three full periods", paid: 3000, target: 6000, want: 3000},
		{name: "half the purchased interval remains", paid: 3000, target: 6000, elapsed: 45 * 24 * time.Hour, want: 1500},
		{name: "rounds the final fractional cent up", paid: 3000, target: 6000, elapsed: 90*24*time.Hour - time.Nanosecond, want: 1},
		{name: "discounts on both purchased periods", paid: 2400, target: 5100, want: 2700},
		{name: "renewal does not price all cumulative periods", paid: 3000, target: 6000, cumulative: &seven, want: 3000},
		{name: "discounted renewal is prorated over its own interval", paid: 2550, target: 4800, cumulative: &seven, elapsed: 45 * 24 * time.Hour, want: 1125},
		{name: "successive upgrade uses saved full price", paid: 2700, target: 7800, overlay: true, elapsed: 60 * 24 * time.Hour, want: 1000},
		{name: "historical upgrade uses immutable period discount", paid: 2700, target: 7800, overlay: true, legacy: true, elapsed: 60 * 24 * time.Hour, want: 1000},
	} {
		t.Run(test.name, func(t *testing.T) {
			periods := &three
			if test.cumulative != nil {
				periods = test.cumulative
			}
			base := coverageRow{coverage: storage.EntitlementCoverage{ID: "base", ValidFrom: start, ValidUntil: &end, PaidPeriods: periods}, order: storage.Order{PurchasedPeriods: 3, Currency: "USD", AmountMinor: test.paid}, version: version(1000, 3000)}
			current := base
			if test.overlay {
				full := int64(4800)
				quote := saleQuote{UpgradePriceMinor: &full}
				if test.legacy {
					quote.UpgradePriceMinor = nil
				}
				current.coverage.BaseCoverageID = &base.coverage.ID
				current.order = storage.Order{Currency: "USD", AmountMinor: 1050, PurchasedPeriods: 1, QuoteSnapshot: marshal(quote)}
				current.version = version(2000, 4800)
				if !test.legacy {
					// Saved pricing remains authoritative even if a reconstruction
					// would yield a different full price.
					current.version = version(2000, 6000)
				}
			}
			money, fullPrice, err := upgradeAmount(&current, &base, version(3000, test.target), start.Add(test.elapsed))
			if err != nil || money.Currency != "USD" || money.Minor != test.want || fullPrice != test.target {
				t.Fatalf("got %v, full=%d, error=%v; want USD %d, full=%d", money, fullPrice, err, test.want, test.target)
			}
		})
	}
}

func TestUpgradeRejectsUnavailableOrCheaperInterval(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(90 * 24 * time.Hour)
	base := coverageRow{coverage: storage.EntitlementCoverage{ValidFrom: start, ValidUntil: &end}, order: storage.Order{PurchasedPeriods: 3, Currency: "USD", AmountMinor: 3000}}
	for _, target := range []storage.PlanVersion{
		{Currency: "USD", PurchaseType: "fixed_term", PurchaseOptions: marshal([]PurchaseOption{{Periods: 3, AmountMinor: 6000, Enabled: false}})},
		{Currency: "USD", PurchaseType: "fixed_term", PurchaseOptions: marshal([]PurchaseOption{{Periods: 3, AmountMinor: 2400, Enabled: true}})},
	} {
		if _, _, err := upgradeAmount(&base, &base, target, start); err == nil {
			t.Fatal("unavailable or cheaper interval produced a payable upgrade")
		}
	}
}
