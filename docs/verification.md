# Historical verification record

Original baseline: 2026-10-15, Windows, Go 1.26.6, isolated MySQL 8.0.32. This record preserves historical development checks and subsequent notes. It does not certify production readiness or replace a fresh acceptance run.

| Check | Recorded result |
| --- | --- |
| `go test ./...` | Passed; MySQL integration tests explicitly skipped without test configuration |
| `scripts/test-mysql.ps1` | Passed with isolated data directory, random credentials, and temporary port |
| First/repeated GORM AutoMigrate | Passed; core tables created from Go models |
| Database constraints | Rejected negative amounts; enforced idempotency, transaction/event deduplication, one current license binding, historical replacement, task versions, and payment-environment relationships |
| Licentivo settings | One connection, no licensing environment selector, versioned shared mapping; purchase/renewal/upgrade/refund/synchronization doubles passed |
| DeepL | Ten locales, UI/brand/product/plan translations, key encryption, queue deduplication/resume, and fallback verified with doubles |
| Contract arithmetic | Month-end anchors, leap years, UTC conversion, calendar months versus 30 days, and expired renewal passed |
| Upgrade amounts | Integer rounding, overflow prevention, and cross-currency/expired quote rejection passed |
| Secret encryption | AES-256-GCM nonce/context handling and wrong-key/tamper/truncation rejection passed |
| Licentivo event signatures | Raw bytes, timestamp window, explicit legacy headers, and conflicting-header rejection passed |
| `go vet ./...` | Passed |
| Windows amd64 build | Passed |
| Linux amd64, CGO disabled | Cross-build passed; Linux runtime not exercised |
| HTTP smoke check | Root, CSS, JavaScript, health, and system status returned 200 in a test preview |
| Desktop connection settings | No duplicate Licentivo environment/connection fields; separate payment environments; no overflow at width 1280 |
| Unimplemented payment route at M0 | Returned 404 instead of acknowledging an unimplemented event; this is a historical state, not the current adapter inventory |
| OpenAPI subset | JSON parsing passed for the three system endpoints |

An initial MySQL run was blocked by sandbox TCP permissions. The same isolated script later passed in an approved host execution environment. Disposable previews used test-only data.

Docker/Compose was not run because Docker was unavailable. A real Licentivo catalog/policy read and plan mapping were subsequently verified; management operation/recovery tests used automation. First real license issuance remains part of test-payment acceptance. Real DeepL, Stripe/PayPal sandbox payments, Resend delivery, complete language/OS SDK flows, full accessibility, and production deployment remain unaccepted. Local tests must not be presented as a verified external commercial workflow.

## Public source preparation, 2026-10-21

`go test ./...`, `go vet ./...`, and `node --check internal/httpapi/web/app.js` passed. This run did not enable MySQL integration tests or real provider calls. All repository Markdown and configuration documentation was checked for remaining Chinese text, and relative Markdown links were checked for existing local targets. Publication files were checked to exclude private development configuration, local database files, uploads, backups, binaries, caches, and screenshots. The source includes the notices for all server dependencies listed in `go.mod`.
