# Development progress

Documentation prepared for public source publication on 2026-10-21. The implementation remains in local development and verification. Desktop browsers are the primary administration target. Historical records describe their original test dates; they are not current production certifications.

## Implemented and locally verified

- Go, GORM, MySQL, automatic schema creation from Go models, and the MIT license.
- Automatic first-start administrator (`admin@example.com` / `LaunchAdmin123!`) with mandatory password change, API enforcement, and preservation of existing installations. No setup token is needed.
- Argon2id email/password accounts, website cookies/CSRF, separate desktop Bearer sessions, and session revocation.
- Email verification, password reset, and email changes with expiring one-time credentials and sensitive-change session invalidation.
- Encrypted durable email jobs, leases, backoff, and retries; configurable Resend SMTP TLS/STARTTLS and a real administrator test-email action.
- Branding, multiple software products, plan versions, draft/preview/publication, customer management, and audit records. Draft edits do not rewrite published prices.
- Stripe hosted Checkout and PayPal Orders/Capture, payment queries, refunds, and event verification adapters. HTTP contract tests use doubles; real payment sandboxes remain unverified.
- Server-calculated purchase amounts, stable payment operation IDs, verified event persistence before processing, and distinct payment/licensing states.
- Durable licensing tasks with leases, retries, stable IDs, and recovery after remote success with a lost response.
- Authenticated software credential retrieval scoped to account, product, and payment environment; website sessions cannot retrieve full license keys.
- One encrypted Licentivo API connection with product/policy discovery and plan-policy selection. Licensing limits, features, offline settings, heartbeats, seats, and release quotas are synchronized from policy snapshots.
- PEM certificate/key upload and validation, encrypted private-key storage, built-in HTTPS hot replacement, public protocol/origin changes, and expiry checks.
- Embedded storefront, setup, account, administration, plans, service settings, checkout, and order interfaces.
- Ten customer locales; DeepL Free/Pro, encrypted keys, usage display, source hashes, persistent jobs, retry, and source-language fallback.
- Renewals, encrypted expiring upgrade quotes, fixed-term prorating, and paid-coverage recalculation after refunds.
- Full-refund requests/review, stable provider operation IDs, and query/replay recovery after uncertain responses.
- Device listing and self-service release with a 30-day quota, idempotent reservations, and device-generation isolation.
- Separate customers and staff; administrator/operator/support roles, staff creation and disable/enable, and last-administrator protection. Disabled customer accounts trigger licensing suspension.
- Safe-field CSV export, encrypted backup, empty-database restore, and a post-restore transaction pause.
- Signed Licentivo events with remote verification, terminal revocation handling, and late-payment reconciliation.
- Wrappers and examples for all nine official SDK languages: Go, Python, C#/.NET, Java, JavaScript/Node.js, Rust, Ruby, C and C++, plus a trusted Go Web backend. See the dated verification record for actual builds and test evidence; external commercial acceptance remains pending.
- Desktop sidebar/workspace administration, complete plan tables, and keyboard navigation improvements.
- Product documentation, product-specific policy pages, document images, and separate platform installer metadata/storage.
- Removal of the global header documentation shortcut; product-specific documentation remains accessible from product pages.
- English repository documentation, public setup/configuration templates, and expanded installation/operations guidance.

## Recorded checks

- `go test ./...` passed in earlier development runs; MySQL tests skip without explicit test configuration.
- The isolated Windows MySQL helper covered constraints, accounts, plans, purchases, renewals, upgrades, refunds, devices, roles, notifications, encrypted recovery, and replay.
- Payment HTTP-transport tests do not make real provider requests.
- Resend SMTP doubles cover implicit TLS, STARTTLS, certificate verification, and disconnects after DATA acceptance. The test-email endpoint verifies saved settings and idempotency with a sending double.
- `node --check internal/httpapi/web/app.js` passed.
- Desktop browser checks at 1440x900 and width 1024 covered plan columns, menus, actions, and editor layout; see [ui-review.md](ui-review.md).
- Local Licentivo connection, product/policy reads, and plan mappings have been checked with a real read connection. First real issuance still needs a complete test-payment workflow and a read/write key.

## Remaining acceptance work

- Review the remaining administration pages, long lists, empty states, role failures, and full accessibility.
- Complete concurrency boundaries and real SDK activation, restart, offline behavior, account switching, and each release target's acceptance matrix.
- Extend OpenAPI coverage beyond the current 18 system/account/integration/basic order operations to the remaining administrative and provider-webhook APIs.
- Validate actual Docker/Compose and Linux operation, persistent upload mounts, backup drills, and performance.
- Complete real Stripe, PayPal, Resend, and DeepL acceptance with merchant-supplied credentials.
- Verify the complete test payment-to-license-to-application path. A successful catalog read is not license-issuance acceptance.
- Keep production release/live-sales gates in place until the applicable acceptance criteria pass.

## Local test isolation

Earlier Windows development used Go 1.26.6, Node.js 24, and MySQL 8.0.32; Docker was unavailable. Test instances are created beneath `.cache/mysql-test-*` with temporary credentials and new databases. Some sandbox sessions require approved host execution for loopback networking. Never stop unrelated MySQL services or use existing customer databases for integration tests. Daily development uses the private `.local/dev` configuration, not disposable test previews.
