package app

import (
	"testing"
	"time"
)

func TestSalesWindowUsesCalendarDaysAcrossDST(t *testing.T) {
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	q, w, err := normalizeSalesQuery(SalesQuery{From: "2026-10-31", To: "2026-11-02", TimeZone: "America/New_York"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if q.Environment != "live" || len(w.dates) != 3 || w.end.Sub(w.start) != 73*time.Hour {
		t.Fatalf("incorrect DST window: %v %v", q, w)
	}
	if w.previous.In(w.location).Format(time.DateOnly) != "2026-10-28" {
		t.Fatal("previous window is not a same-length calendar period")
	}
	for _, bad := range []SalesQuery{{From: "2026-11-04", To: "2026-11-03"}, {To: "2026-11-06"}, {TimeZone: "Not/AZone"}, {Currency: "USD;DROP"}, {Environment: "everything"}, {From: "2024-01-01", To: "2026-11-01"}} {
		if _, _, err := normalizeSalesQuery(bad, at); err == nil {
			t.Fatalf("invalid query accepted: %+v", bad)
		}
	}
}

func TestSalesKeepsCaptureAndSubsequentRefundAsSeparateFacts(t *testing.T) {
	b := newSalesBucket()
	for _, f := range []salesFact{{Kind: "payment", Amount: 10000, OrderID: "one", UserID: "alice"}, {Kind: "payment", Amount: 10000, OrderID: "one", UserID: "alice"}, {Kind: "payment", Amount: 4000, OrderID: "two", UserID: "alice"}, {Kind: "refund", Amount: 10000, OrderID: "one", UserID: "alice"}, {Kind: "reversal", Amount: 2000, OrderID: "two", UserID: "alice"}} {
		if err := b.add(f); err != nil {
			t.Fatal(err)
		}
	}
	m := b.finish()
	if m.Gross != 24000 || m.Refunds != 10000 || m.Reversals != 2000 || m.Net != 12000 || m.Orders != 2 || m.Customers != 1 || m.Average != 12000 {
		t.Fatalf("incorrect transaction accounting: %+v", m)
	}
	later := newSalesBucket()
	_ = later.add(salesFact{Kind: "refund", Amount: 10000, OrderID: "old", UserID: "alice"})
	if later.finish().Net != -10000 || later.finish().Orders != 0 {
		t.Fatal("a later refund must not manufacture a paid order")
	}
	if b.add(salesFact{Kind: "payment", Amount: 9007199254740991, OrderID: "large"}) == nil {
		t.Fatal("inexact browser amount accepted")
	}
}
