package app

import (
	"strings"
	"testing"
	"time"
)

func TestExpiryReminderLanguagesAndContactIsolation(t *testing.T) {
	end := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	details := expiryMailDetails{ProductName: "Atlas Pro", PlanName: "Professional annual", PolicyName: "Pro device policy", CustomerEmail: "customer@example.test", Brand: "Example merchant", EndsAt: &end}
	settings := ExpiryReminderSettingsView{}
	if len(expiryMailTemplates) != len(localeOptions) {
		t.Fatal("email templates must cover every Launch locale")
	}
	seen := map[string]bool{}
	for _, language := range localeOptions {
		if !validExpiryLocale(language.Code) {
			t.Fatalf("missing %s", language.Code)
		}
		message := makeExpiryMail(details, settings, language.Code, 7)
		for _, value := range []string{details.ProductName, details.PlanName, details.PolicyName, "2026-11-12 10:00:00 UTC"} {
			if !strings.Contains(message.Body, value) {
				t.Fatalf("%s omitted purchase identity or expiry: %s", language.Code, value)
			}
		}
		if !strings.Contains(message.Subject, details.ProductName) || !strings.Contains(message.Subject, details.PolicyName) {
			t.Fatalf("%s subject omitted product or actual policy", language.Code)
		}
		if message.ReplyTo != "" || strings.Contains(message.Body, "http") || strings.Contains(message.Body, "@example") || strings.Contains(message.Body, "{days}") {
			t.Fatalf("%s leaked a contact, portal link, or unresolved template", language.Code)
		}
		if seen[message.Body] {
			t.Fatalf("%s silently reused another language", language.Code)
		}
		seen[message.Body] = true
		settings.ContactEmail = "support@merchant.test"
		message = makeExpiryMail(details, settings, language.Code, 7)
		if message.ReplyTo != settings.ContactEmail || !strings.Contains(message.Body, settings.ContactEmail) {
			t.Fatalf("%s ignored the explicitly supplied contact", language.Code)
		}
		settings.ContactEmail = ""
	}
	if validExpiryLocale("unknown") {
		t.Fatal("unsupported language must not silently fall back to English")
	}
}

func TestExpiryReminderWindows(t *testing.T) {
	at := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		remaining time.Duration
		day       int
	}{{8 * 24 * time.Hour, 0}, {7 * 24 * time.Hour, 7}, {3*24*time.Hour + time.Second, 7}, {3 * 24 * time.Hour, 3}, {24 * time.Hour, 1}, {time.Second, 1}, {0, 0}, {-time.Second, 0}} {
		if got := eligibleExpiryReminder([]int{1, 3, 7}, at.Add(test.remaining), at); got != test.day {
			t.Fatalf("%v: got %d want %d", test.remaining, got, test.day)
		}
	}
	for _, days := range [][]int{nil, {0}, {91}, {7, 7}, {1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}} {
		if _, err := normalizeExpiryReminderDays(days); err == nil {
			t.Fatalf("accepted invalid days %v", days)
		}
	}
}

func TestMailQuotaReserveBoundaries(t *testing.T) {
	q := MailPeriodQuota{Total: 10, Reserve: 4, Limit: 5}
	for _, test := range []struct{ used, reminders, want int64 }{{0, 0, 5}, {5, 1, 1}, {6, 1, 0}, {3, 5, 0}, {10, 2, 0}, {20, 0, 0}} {
		got := remainingReminderQuota(q, MailQuotaUsage{TotalUsed: test.used, ReminderUsed: test.reminders})
		if got != test.want {
			t.Fatalf("used=%d reminders=%d got=%d want=%d", test.used, test.reminders, got, test.want)
		}
	}
	valid := MailQuotaPolicy{Hour: q, Day: q, Month: q}
	if validateMailQuotas(valid) != nil {
		t.Fatal("valid quota rejected")
	}
	for _, bad := range []MailPeriodQuota{{Total: 0}, {Total: 10, Reserve: 0, Limit: 3}, {Total: 10, Reserve: 10}, {Total: 10, Reserve: 4, Limit: 7}, {Total: 10, Reserve: 4, Limit: -1}} {
		policy := valid
		policy.Hour = bad
		if validateMailQuotas(policy) == nil {
			t.Fatalf("accepted a quota that can consume the protected pool: %+v", bad)
		}
	}
	at := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	for _, period := range quotaPeriods(at) {
		if !period.end.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("bad UTC reset at year boundary: %+v", period)
		}
	}
}
