package app

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"licentivo-launch/internal/mailer"
)

//go:embed expiry_reminder_templates.json
var expiryReminderTemplateJSON []byte

type expiryMailTemplate struct {
	Title   string   `json:"title"`
	Intro   string   `json:"intro"`
	Labels  []string `json:"labels"`
	Ending  string   `json:"ending"`
	Footer  string   `json:"footer"`
	Contact string   `json:"contact"`
}

var expiryMailTemplates = func() map[string]expiryMailTemplate {
	var templates map[string]expiryMailTemplate
	if err := json.Unmarshal(expiryReminderTemplateJSON, &templates); err != nil {
		panic("invalid built-in expiry email templates")
	}
	return templates
}()

func validExpiryLocale(locale string) bool { _, ok := expiryMailTemplates[locale]; return ok }

type expiryMailDetails struct {
	EntitlementID string     `json:"entitlement_id"`
	ProductName   string     `json:"product_name"`
	PlanName      string     `json:"plan_name"`
	PolicyName    string     `json:"policy_name"`
	CustomerEmail string     `json:"customer_email"`
	Environment   string     `json:"environment"`
	Brand         string     `json:"brand"`
	EndsAt        *time.Time `json:"ends_at"`
}

func makeExpiryMail(details expiryMailDetails, settings ExpiryReminderSettingsView, locale string, days int) mailer.Message {
	template := expiryMailTemplates[locale]
	format := strings.NewReplacer("{product}", details.ProductName, "{policy}", details.PolicyName, "{days}", strconv.Itoa(days)).Replace
	end := "—"
	if details.EndsAt != nil {
		end = details.EndsAt.UTC().Format("2006-01-02 15:04:05 UTC")
	}
	lines := []string{format(template.Intro), "", template.Labels[0] + ": " + details.ProductName, template.Labels[1] + ": " + details.PlanName, template.Labels[2] + ": " + details.PolicyName, template.Labels[3] + ": " + end, "", template.Ending, "", details.Brand + " · " + template.Footer}
	// ContactEmail is only the administrator's explicit reminder setting. In
	// particular, neither the staff login nor the SMTP sender is a fallback.
	if settings.ContactEmail != "" {
		lines = append(lines, template.Contact+settings.ContactEmail)
	}
	subject := strings.NewReplacer("\r", " ", "\n", " ").Replace(details.Brand + " · " + format(template.Title))
	if details.Environment == "sandbox" {
		subject = "[SANDBOX] " + subject
	}
	return mailer.Message{To: details.CustomerEmail, Subject: subject, Body: strings.Join(lines, "\r\n"), ReplyTo: settings.ContactEmail}
}
