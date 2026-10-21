# Licentivo Launch

**A self-hosted software storefront, customer account portal, and purchase-to-license integration for [Licentivo](https://www.licentivo.com/).**

Give customers a branded place to discover your software, create an account, purchase a plan, and sign in to your application. Launch connects verified payments to Licentivo licenses so customers can activate software through their merchant account instead of copying license keys.

Built with **Go, GORM, MySQL, and embedded HTML/CSS/JavaScript**. Released under the [MIT license](LICENSE).

[Download binaries](https://github.com/spf86/licentivo-launch/releases) · [Licentivo website](https://www.licentivo.com/) · [Installation](docs/installation.md) · [Configuration](docs/configuration.md) · [Application integration](docs/software-sdk-integration.md)

## Why use Launch?

- **Own your customer relationship.** Run your own instance, domain, and database. Customer accounts, contact information, orders, and payment records stay in the merchant database.
- **Connect purchases to software access.** Use the same merchant account on the website and in your application. Launch retrieves the customer's own license after authentication; the official Licentivo SDK handles activation and runtime checks.
- **Sell multiple products under your brand.** Manage product pages, plans, documentation, policies, and platform-specific installers from one administration area.
- **Recover from interrupted operations.** Persistent queues, stable operation identifiers, retries, and an entitlement ledger help reconcile payments with licensing after timeouts or restarts.
- **Keep deployment compact.** The frontend is embedded in the Go executable. No separate frontend server or message broker is required.
- **Customize an open source foundation.** Modify the interface and integrations while retaining the project and dependency license notices.

## Current status

This is a **beta development release**. The source includes the workflows below and local automated verification, but it is not a claim of production readiness. Live sales remain gated. Real Stripe/PayPal sandbox transactions, email delivery, translation quality, complete payment-to-desktop activation, and Linux/Compose operation still require deployment-specific acceptance testing. See [progress](docs/progress.md) and the [verification record](docs/verification.md).

The customer website has ten locales. The administration interface currently uses Simplified Chinese; repository documentation and setup instructions are in English.

## Features

| Area | Implemented capabilities |
| --- | --- |
| Storefront | Merchant branding and themes, multiple products, plan presentation, dedicated product documentation and policy pages |
| Downloads | Separate Windows, macOS, iOS, and Android installers; platform requirements, versions, file sizes, and checksums |
| Customer accounts | Email/password registration, verification, password reset, email changes, revocable website and desktop sessions |
| Payments | Stripe hosted Checkout and PayPal Orders/Capture adapters, webhook verification, payment queries, and full-refund workflows |
| Licensing | One encrypted Licentivo management connection, product/policy discovery, versioned plan mappings, license issuance, renewals, policy changes, suspension, and device release |
| Entitlements | Per-account/product/payment-environment isolation, perpetual and fixed-term plans, UTC calendar terms, paid coverage ledger, renewal and upgrade quotes |
| Reliability | Persistent payment and licensing events, task leases, stable request identifiers, retries, reconciliation, and recovery of interrupted remote operations |
| Administration | Customer and order management, separate administrator/operator/support roles, audit records, CSV exports, and encrypted backup/restore |
| Email and languages | Resend SMTP queue and administrator test email; DeepL Free/Pro, ten customer locales, deduplication, retries, and source-language fallback |
| HTTPS | External TLS termination or an optional built-in HTTPS listener with encrypted certificate storage and hot replacement |
| Application integration | Go account client plus C, C++, C#/.NET 8, Go, Python, and trusted Web backend reference examples |

Automatic recurring billing, partial refunds, and additional payment providers are future work. Fixed-term purchases and manual renewals do not automatically charge the customer.

## How it works

1. A merchant deploys Launch and configures their brand, products, payment accounts, and Licentivo management API key.
2. A customer registers, verifies their email, and buys a published plan through the merchant's Stripe or PayPal account.
3. Launch verifies the payment, records the paid entitlement, and queues a licensing operation.
4. A worker creates or updates the Licentivo license with a stable request identifier.
5. The customer signs in to the merchant's application. Launch supplies that account's product credential to the authenticated application.
6. The official Licentivo SDK validates signatures, binds devices or obtains floating seats, and enforces runtime features and offline rules.

Licentivo receives the data required for licensing, such as opaque customer references and device/runtime metadata. Launch does not send customer passwords, contact details, or complete orders to Licentivo. Payment providers receive the information needed to process the merchant's transactions.

## Quick start from source

For ready-to-run Windows and Linux packages, see [GitHub Releases](https://github.com/spf86/licentivo-launch/releases) and the [binary installation guide](docs/binary-installation.md). Both **amd64** and **arm64** are provided for each operating system. No Go or Node.js installation is needed to run a binary package; MySQL is still required.

### Prerequisites

- Go **1.26 or newer** (the project has been built with Go 1.26.6).
- MySQL **8.0.16 or newer**, an empty database, and a database user allowed to create and alter tables.
- Git. Node.js is optional for JavaScript syntax checks; it is not needed to serve the website.

### 1. Download and build

```sh
git clone https://github.com/spf86/licentivo-launch.git
cd licentivo-launch
go build -o bin/launch ./cmd/launch
```

On Windows, use `go build -o bin/launch.exe ./cmd/launch` and invoke `.\bin\launch.exe` in the commands below.

### 2. Create private configuration

Initialize a private configuration with an automatically generated master key:

```sh
./bin/launch init
```

Edit `LAUNCH_DATABASE_DSN` in `.local/dev/launch-config.json` with your MySQL address, database, username, and password. Keep the generated master key: the same value is needed after a restart or restore. The private `.local` directory is excluded from Git. `init` refuses to overwrite an existing configuration. A [manual template](config/launch-config.example.json) is also available.

### 3. Start and install

```sh
./bin/launch dev
```

The first database startup automatically creates this initial administrator:

| Field | Initial value |
| --- | --- |
| Email | `admin@example.com` |
| Password | `LaunchAdmin123!` |

Open **http://localhost:18080/#/login**. Your first sign-in opens a required password-change form. Choose a different password of 12-128 bytes, then sign in again. Administration and desktop sessions stay blocked until the change completes. Complete this local first-login step before publishing the site. No installation token is required. Existing installations retain their accounts and passwords.

Go/GORM creates and maintains the tables at startup. There is no separate SQL migration command. `dev` connects to the configured database; it does not launch MySQL.

The server then runs at **http://localhost:18080**. On Windows, `scripts/dev.ps1` is an optional build-and-run wrapper. Use `Ctrl+C` to stop a foreground process.

For complete PowerShell and Linux examples, deployment, service setup, and troubleshooting, see the [installation guide](docs/installation.md).

### Preview without a database

```sh
go run ./cmd/launch
```

With no database configuration in the process environment, this serves the uninstalled interface at `http://localhost:8080`. `/healthz` returns 200; `/readyz` returns 503 until a database is configured. Accounts, purchases, and administration require a configured database.

## Configure your services

After installation, configure the services through the administration area:

| Service | Purpose | Guide |
| --- | --- | --- |
| [Licentivo](https://www.licentivo.com/) | Product policies, software licenses, devices, and runtime SDKs | [Management API integration](docs/licentivo-contract.md) |
| Stripe and PayPal | Payments collected directly into your merchant accounts | [Payment integration](docs/payments.md) |
| Resend | Verification, password reset, and account email | [Email setup](docs/resend-email.md) |
| DeepL | Translation of merchant content into customer locales | [Language setup](docs/multilingual-deepl.md) |
| HTTPS | Secure public origin and callbacks | [Certificates and HTTPS](docs/ssl-https.md) |

Use a Licentivo management key with product/policy read access and license write access. There is one Licentivo connection shared by Launch's test and live payment modes. The Licentivo service origin is fixed to `https://www.licentivo.com`.

See [external service setup](docs/external-services.md) for the account preparation checklist. Store real credentials only in your private server configuration or the authenticated administration area.

## Configuration reference

The executable reads process environment variables and an optional JSON file selected by `LAUNCH_CONFIG_FILE`. It does **not** automatically load `.env`; Docker Compose reads `.env` for its own interpolation. Process environment values take precedence over JSON values.

All variables, defaults, and examples are documented in [configuration.md](docs/configuration.md). Use [.env.example](.env.example) for the development Compose template and [launch-config.example.json](config/launch-config.example.json) for a direct deployment.

## Deployment and operations

- Use HTTPS and a persistent MySQL database for a public deployment. `LAUNCH_ENV=production` requires an HTTPS `LAUNCH_PUBLIC_URL`, a database DSN, and a master key; external acceptance checks still gate live sales.
- Persist `LAUNCH_UPLOAD_DIR` and back it up with the encrypted database backup. Uploaded installers are not part of the executable.
- Keep the process running: licensing, expiration, payment, email, and translation work depends on persistent background jobs.
- Launch's sales term starts from verified payment; Licentivo's policy term can start at first activation. Launch reconciles the difference and suspends remote licenses at the local contract deadline. A long server outage can delay that suspension. See the [contract limitations](docs/licentivo-contract.md).
- The included [Compose file](compose.yaml) is a development scaffold. Container execution, production hardening, and writable installer storage must be validated for your environment.

Backup, restore, upgrade, and monitoring procedures are in the [installation guide](docs/installation.md).

## Commands

| Command | Purpose |
| --- | --- |
| `launch init` | Create a private local configuration with a new master key, without overwriting existing configuration |
| `launch dev` | Read `.local/dev/launch-config.json` and start using the configured MySQL database |
| `launch serve` | Start the server; create/update tables when a database is configured |
| `launch check` | Check database connectivity and required tables without modifying the schema |
| `launch keygen` | Print a random base64-encoded 32-byte key |
| `launch version` | Print the application version |
| `launch backup PATH` | Write an encrypted backup using `LAUNCH_BACKUP_PASSPHRASE` |
| `launch restore PATH` | Restore into an empty database; reconcile transactions before releasing the restore pause |

Commands shown as `launch` refer to your built executable, for example `./bin/launch` or `.\bin\launch.exe`. `dev` automatically selects the local JSON file; other commands need `LAUNCH_CONFIG_FILE` or equivalent process variables.

## Development and verification

```sh
go test ./...
go vet ./...
node --check internal/httpapi/web/app.js
```

Database integration tests are skipped unless explicitly configured. The Windows helper starts its own isolated MySQL instance with new test databases:

```powershell
./scripts/test-mysql.ps1
# Optional custom MySQL location:
./scripts/test-mysql.ps1 -MySQLBin 'C:\path\to\mysql\bin'
```

`-Preview` keeps a disposable test preview running after the test suite. It is not the daily development startup path. Do not point integration tests at an existing customer database.

## Application integration examples

See [examples/README.md](examples/README.md) and the [application integration guide](docs/software-sdk-integration.md). Prepare the separately obtained Licentivo SDK with `scripts/prepare-examples.ps1` before building SDK examples. The server itself does not depend on this ignored vendor directory.

## Repository layout

```text
cmd/launch/                    Application entry point and CLI
config/                        Public configuration examples
internal/config/               Environment/JSON configuration validation
internal/storage/              GORM models, MySQL connection, schema checks
internal/domain/               UTC contract terms and integer money arithmetic
internal/security/             Passwords, sessions, and secret encryption
internal/app/                  Accounts, commerce, entitlements, and jobs
internal/payments/             Stripe and PayPal adapters
internal/adapters/licentivo/   Licentivo management API and event verification
internal/httpapi/              HTTP API and embedded website
internal/backup/               Encrypted backup format
pkg/launchclient/              Go customer account client
examples/                      Desktop and Web backend integrations
scripts/                       Local development and isolated verification
third_party/                   Dependency license notices
docs/                          Installation, configuration, contracts, and records
```

## More documentation

- [Installation and operations](docs/installation.md)
- [Configuration reference](docs/configuration.md)
- [External services](docs/external-services.md)
- [Product requirements](Licentivo%20Launch.md)
- [Development progress and open work](docs/progress.md)
- [Historical architecture decisions](docs/m0.md)
- [Desktop interface review](docs/ui-review.md)
- [System-endpoint OpenAPI subset](docs/openapi.json) (not a complete application API specification)

## License

Launch's own code is available under the [MIT license](LICENSE). Dependencies retain their respective licenses; see [third_party/README.md](third_party/README.md). External payment, email, translation, and licensing services have their own accounts and terms.
