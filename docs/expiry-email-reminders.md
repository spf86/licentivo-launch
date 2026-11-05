# Authorization expiry email reminders

This feature belongs to each independently deployed **Licentivo Launch** site. It does not add reminder jobs, customer-contact settings, or mail budgets to the Licentivo licensing server or the application SDKs.

The authenticated administration endpoints are described in [the reminder OpenAPI specification](expiry-reminders-openapi.json).

## Configure the site

1. As a site administrator, open **System settings → Email & registration** and configure an SMTP service.
2. In **Email sending limits**, enter the site's actual hourly, daily, and monthly allowances, a protected reserve for other mail, and a maximum number of expiry reminders. All customers, products, worker processes, and server instances using this site's database share these limits. Saving limits never clears usage.
3. Open **System settings → Expiry reminders**. Choose whether to enable reminders, the default authorization preference, between 1 and 10 distinct reminder days from 1–90, and the default email language. Reminders are disabled until explicitly enabled, and require configured email limits.
4. Optionally enter a merchant contact email specifically for these reminders. Leaving it blank omits the contact line and `Reply-To`. The administrator's registration email, website support address, and SMTP sender are never inferred as the reminder contact.
5. Under **Customers**, expand a customer's purchased products and use **Reminder settings** on the relevant authorization. Choose inherit, on, or off, and optionally override that authorization's email language. One customer's different products can use different preferences.

The global switch always applies, including to individually enabled authorizations. Staff with the operations role can manage individual authorization preferences; only administrators can change site settings or sending limits. Support may read preferences but cannot change them.

## Eligibility and content

The database coordinates one scan per hour across site instances. Scans consider live authorizations only. An eligible authorization must be active, fully synchronized, linked to an active customer with a valid email, and have a known future expiry and an identified licensing policy. Perpetual, suspended, revoked, refunded/voided, or unsynchronized authorizations do not receive reminders. Missing policy names are not replaced with invented names.

An hourly scan does not send an hourly email. With reminder days `7, 3, 1`, the same authorization and expiry receive at most one reminder at each configured point. Successful reminders remain deduplicated after delivery, across subsequent scans and server restarts.

The expiry is derived from Launch's purchase coverage ledger, including continuous advance renewals, rather than a short SDK lease. The nearest applicable reminder window is queued once for the authorization, exact expiry, and reminder day. Enabling reminders late does not send every missed earlier reminder at once. Before every send and retry, Launch rechecks authorization status, consent, customer email, coverage, reminder window, language, and contact settings. A renewal, opt-out, changed recipient, or obsolete window cancels the old job.

Both subject and body identify the actual **product** and **license policy name**. The body also identifies the **purchased plan** and expiry in UTC. Messages contain no license key and no “View license” button. Templates are available in Simplified Chinese, English, Japanese, Korean, German, French, Spanish, Brazilian Portuguese, Italian, and Russian. Unsupported locales are rejected rather than silently falling back to English. Merchant-authored names retain their original text.

Administrators can preview saved settings, explicitly queue a `[TEST]` message to a chosen recipient, and inspect paginated delivery metadata. Preview does not send. Test reminders use the same lowest priority and shared reminder budget as actual reminders. “Sent” means the provider accepted the message; inbox placement is separate.

## Shared budget and priority

Every SMTP attempt reserves capacity atomically in database counters for the current UTC hour, day, and calendar month. The same site-wide locks apply across concurrent workers. Authentication mail has priority over transaction/invoice mail, and expiry reminders have the lowest priority. Ready or running higher-priority jobs cause reminders to yield.

For **every** period, a reminder is allowed only while both conditions hold:

```
total_attempts + 1 <= total_limit - protected_reserve
reminder_attempts + 1 <= reminder_limit
```

The smallest remaining allowance across all three periods wins. For example, with a total of 10, a reserve of 4, and 5 attempts already used, only one further reminder may send. Subsequent registration or password-reset messages can still use the remaining four attempts. Other mail also respects the configured total; the reminder reserve cannot overcome a provider's exhausted overall allowance.

Failed or uncertain SMTP attempts consume allowance conservatively. Waiting consumes neither allowance nor a retry attempt. Deferred jobs resume after the blocking period resets, or after an administrator changes the limits. Ordinary SMTP connection tests count toward the same total, so they cannot invisibly bypass it. When no limits have yet been configured, existing account mail continues to work and is counted, while automatic expiry reminders remain disabled.

These are application-level limits for this Launch database. The administrator must account for other applications sharing the same upstream SMTP account when entering the site's actual allowance. Launch does not read provider billing quotas automatically.

## Storage and verification

Settings, individual preferences, scan coordination, and counters are included in encrypted backups. Older archives without the new optional tables restore with reminders disabled; the normal recovery review must complete before automatic reminders resume. Message payloads remain encrypted in the outbox and are cleared after a reminder is sent or cancelled. History exposes metadata only, without ciphertext, credentials, or lease tokens.

`TestExpiryRemindersDatabase` and `TestExpiryReminderHTTPPermissionsAndQuotaContract` use isolated `launch_test_expiry_*` MySQL databases and recording transports. They cover concurrent instances, protected reserves, hourly/daily/monthly limits, retry accounting, authentication and transaction priority, opt-out/renewal cancellation, hourly scan deduplication, per-authorization locale, administrator authorization, CSRF, and stale settings versions. Set `LAUNCH_TEST_EXPIRY_DSN` to a dedicated disposable test database; the test user must be able to create fresh databases under that prefix. No real SMTP messages are sent by these tests.
