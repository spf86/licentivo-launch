# Licentivo Launch

**Integrate software sales, account login, and licensing in just one day.**

Run a branded software storefront and customer portal on your own domain. Customers discover your software, buy a plan, and sign in to your application with their store account. Launch connects verified payments to [Licentivo](https://www.licentivo.com/) authorization while customer and order records stay in your own database.

**Free and [MIT-licensed](LICENSE), self-hosted, with SDKs in nine languages.** Use one Launch package and ready-to-use connection code to add account login and protected features to your existing application.

[One-day integration guide](docs/one-day-integration.md) · [Download binaries](https://github.com/spf86/licentivo-launch/releases) · [Installation](docs/installation.md) · [Application integration](docs/software-sdk-integration.md) · [Licentivo website](https://www.licentivo.com/)

## Integrate in one day

With an existing application, a server/MySQL database, an HTTPS domain, and Licentivo/payment sandbox accounts ready, follow this working-day plan for a standard Licentivo integration:

| Stage | Result |
| --- | --- |
| Deploy your store | Start Launch and set up your brand, product and sales plan. |
| Connect payments and licensing | Configure one payment sandbox and map the plan to an existing Licentivo product/policy. |
| Integrate your application | Install one Launch SDK, copy the generated connection function, and add login plus feature guards. Manual coding and generated AI instructions follow the same APIs. |
| Verify the complete flow | Complete a sandbox purchase and run a protected operation in your application; check restart and logout behavior. |

The one-day path targets a working integration in your existing app. Payment-provider approval follows the provider's schedule, and production deployment still follows the release's acceptance requirements. See the [step-by-step plan and completion checklist](docs/one-day-integration.md).

## Why use Launch?

- **Own your customer relationship.** Run your own instance, domain, and database. Customer accounts, contact information, orders, and payment records stay in the merchant database.
- **Connect purchases to software access.** Use the same merchant account on the website and in your application. Launch supplies a short-lived account identity assertion; the embedded official Licentivo SDK exchanges it for device credentials and signed permission, then checks protected operations.
- **Sell multiple products under your brand.** Manage product pages, plans, documentation, policies, and platform-specific installers from one administration area.
- **Recover from interrupted operations.** Persistent queues, stable operation identifiers, retries, and an entitlement ledger help reconcile payments with licensing after timeouts or restarts.
- **Keep deployment compact.** The frontend is embedded in the Go executable. No separate frontend server or message broker is required.
- **Customize an open source foundation.** Modify the interface and integrations while retaining the project and dependency license notices.

## Current status

This is a **beta development release**. The source includes the workflows below and local automated verification, but it is not a claim of production readiness. Live sales remain gated. Real Stripe/PayPal/Waffo/Paddle sandbox transactions, email delivery, translation quality, complete payment-to-desktop activation, and Linux/Compose operation still require deployment-specific acceptance testing. See [progress](docs/progress.md) and the [verification record](docs/verification.md).

The customer website has ten locales. The administration interface currently uses Simplified Chinese; repository documentation is primarily in English, with a Chinese application integration quick start.

## Custom licensing services

Launch 0.3.5-beta can synchronize purchases with an existing licensing service using signed HTTP requests. Select one store-wide provider, map products and plans per environment, and keep runtime authorization in your own service and SDK. See the [receiver protocol and API guide](docs/external-licensing.md), [OpenAPI contract](docs/external-licensing-openapi.json), and [Python adapter example](examples/custom-licensing/README.md).

## Features

| Area | Implemented capabilities |
| --- | --- |
| Storefront | Merchant branding and themes, multiple products, plan presentation, dedicated product documentation and policy pages |
| Downloads | Separate Windows, macOS, iOS, and Android installers; platform requirements, versions, file sizes, and checksums |
| Customer accounts | Direct email/password registration without a mail provider, optional required email verification, password recovery, confirmed email changes, and revocable sessions |
| Payments | Stripe, PayPal, Waffo Payment API, and Paddle Billing; verified callbacks, payment queries, full refunds, and interrupted-operation recovery |
| Licensing | One encrypted Licentivo management connection, product/policy discovery, versioned plan mappings, absolute purchase synchronization, short-lived account identity exchange, and SDK-controlled device/runtime authorization |
| Custom licensing | Choose Licentivo or a merchant-owned HTTP licensing service per product/environment; signed purchase snapshots, durable receipt recovery, and owned license/activation/instructions delivery |
| Entitlements | Per-account/product/payment-environment isolation, perpetual and fixed-term plans, UTC calendar terms, paid coverage ledger, renewal and upgrade quotes |
| Reliability | Persistent payment and licensing events, task leases, stable request identifiers, retries, reconciliation, and recovery of interrupted remote operations |
| Administration | Customer and order management, separate administrator/operator/support roles, audit records, CSV exports, and encrypted backup/restore |
| Email and languages | Optional Resend or generic SMTP, independent verification and order-notification preferences, encrypted queue, and test email; DeepL Free/Pro, ten customer locales, deduplication, retries, and source-language fallback |
| HTTPS | External TLS termination or an optional built-in HTTPS listener with encrypted certificate storage and hot replacement |
| Application integration | Public-configuration wizard, local AI integration prompts, actionable entitlement status, lifecycle wrappers for all nine official SDK languages (Go, Python, C#, Java, JavaScript, Rust, Ruby, C and C++), protected session restoration, and trusted Web backend examples |

Automatic recurring billing and partial refunds are future work. Fixed-term purchases and manual renewals do not automatically charge the customer.

The redesigned interface uses a persistent administration sidebar, focused settings tabs, one payment provider editor at a time, and customer tabs for software, orders, refunds, and security. See [Waffo/Paddle setup](docs/waffo-paddle.md) and [email preferences](docs/resend-email.md).

## How it works

1. A merchant deploys Launch and configures their brand, products, payment accounts, and Licentivo management API key.
2. A customer registers and buys a published plan using a configured provider. Email verification is required only when the merchant enables it.
3. Launch verifies the payment and records the order, paid coverage and synchronization task in one transaction.
4. A worker sends an absolute, versioned purchase snapshot to Licentivo with a stable event identifier. Only an `applied` receipt confirms synchronization.
5. The customer signs in to the merchant's application. Launch returns a scoped identity assertion lasting at most 120 seconds; the Launch package passes it to the embedded official SDK in memory.
6. The official SDK exchanges that assertion with Licentivo for device credentials and signed permission, then enforces every protected operation, device/seat limits and offline rules.

Launch does not return a long-term license key in this account flow. The old `/api/v1/software/products/{id}/credential` endpoint returns **HTTP 410** (`LICENSE_CREDENTIAL_REMOVED`); applications must use `/identity` through a matching Launch package. A paid order or successful identity response does not authorize protected work. See [application integration and migration requirements](docs/software-sdk-integration.md) and the [current Licentivo contract](docs/licentivo-contract.md).

Licentivo receives the data required for licensing, such as opaque customer references and device/runtime metadata. Launch does not send customer passwords, contact details, or complete orders to Licentivo. Payment providers receive the information needed to process the merchant's transactions.

## Install the release package

Download the package for your server from [GitHub Releases](https://github.com/spf86/licentivo-launch/releases/tag/v0.3.5-beta) and extract it. **Running the package requires MySQL 8.0.16+; Go, Git, Node.js and a compiler are not required.**

| Server | Package |
| --- | --- |
| Windows, Intel/AMD x64 | [windows_amd64.zip](https://github.com/spf86/licentivo-launch/releases/download/v0.3.5-beta/licentivo-launch_0.3.5-beta_windows_amd64.zip) |
| Windows, ARM64 | [windows_arm64.zip](https://github.com/spf86/licentivo-launch/releases/download/v0.3.5-beta/licentivo-launch_0.3.5-beta_windows_arm64.zip) |
| Linux, Intel/AMD x64 | [linux_amd64.tar.gz](https://github.com/spf86/licentivo-launch/releases/download/v0.3.5-beta/licentivo-launch_0.3.5-beta_linux_amd64.tar.gz) |
| Linux, ARM64 | [linux_arm64.tar.gz](https://github.com/spf86/licentivo-launch/releases/download/v0.3.5-beta/licentivo-launch_0.3.5-beta_linux_arm64.tar.gz) |

`amd64` works on compatible Intel and AMD 64-bit processors. Choose the package for the machine hosting Launch. Customer installer platforms are configured separately in the storefront. Prebuilt server packages are currently provided for Windows and Linux.

### 1. Create configuration


Prepare an empty MySQL database and a dedicated database user. In the extracted folder containing the executable, run:

```powershell
# Windows PowerShell
.\launch.exe init
notepad config.yaml
```

```sh
# Linux
./launch init
nano config.yaml
```

Change `database_dsn` in `config.yaml` to match your database, for example:

```text
launch:YOUR_DATABASE_PASSWORD@tcp(127.0.0.1:3306)/launch?parseTime=true&loc=UTC&charset=utf8mb4
```

To choose your own initial administrator, set these values in `config.yaml` **before the first `serve` against an uninitialized database**, replacing the example values:

```yaml
admin_email: "owner@example.com"
admin_password: "REPLACE_WITH_A_UNIQUE_12_TO_128_BYTE_PASSWORD"
```

Leave these fields empty to use the built-in initial credentials below. Nonempty `LAUNCH_ADMIN_EMAIL` / `LAUNCH_ADMIN_PASSWORD` process variables override the corresponding YAML fields.

Save the file. `init` generates the configuration and instance master key; it does not connect to MySQL or create an administrator. Keep that key and the configuration for future starts and upgrades. The command refuses to overwrite an existing configuration.

### 2. Start Launch

```powershell
# Windows PowerShell
.\launch.exe serve
```

```sh
# Linux
./launch serve
```

`serve` loads `config.yaml` from the working directory and connects to your configured MySQL database. It maintains application tables automatically; it does not install or start MySQL. Leave the terminal open while using this foreground server.

### 3. Sign in and change the initial password

With the configuration generated by `init`, open **http://localhost:18080/#/login** on the machine running Launch. The first `serve` against an uninitialized database creates one administrator. **Only when no initial-account overrides are set**, its credentials are:

| Field | Built-in initial value |
| --- | --- |
| Email | `admin@example.com` |
| Initial password | `LaunchAdmin123!` |

If you configured your own initial email or password, use those values instead. Account creation prints the selected credentials in the server terminal. The default password is a fixed bootstrap value, not a randomly generated password or the instance master key.

Enter a different password of 12–128 bytes in the required first-login form, save, and sign in again. This change is required even for a custom initial password; administration and desktop sessions remain blocked until it is complete. Complete this local step before exposing the site publicly.

**Existing installations keep their accounts and passwords.** Changing `admin_email` / `admin_password`, setting their environment overrides, restarting, or upgrading does not reset an installed account. If the initial credentials fail, check your existing account, selected configuration, environment overrides, and database; see [login troubleshooting](docs/installation.md#health-and-troubleshooting).

`init` selects port **18080**. The built-in listener/public URL values when absent from configuration, and the development Compose template, use **8080**. If you changed these settings, use the configured address. For a remote server, `localhost` refers to the machine running the browser; see [remote first login](docs/installation.md#first-login-on-a-remote-server).

To start again later, open the same extracted folder and run `serve`. Keep `config.yaml`, `.local`, the original master key, MySQL data and uploaded files when replacing the executable.

See [binary installation](docs/binary-installation.md) for checksum verification, and [installation and operations](docs/installation.md) for HTTPS, service startup, backups and upgrades.

## Configure your services

After installation, configure the services through the administration area:

| Service | Purpose | Guide |
| --- | --- | --- |
| [Licentivo](https://www.licentivo.com/) | Product policies, software licenses, devices, and runtime SDKs | [Management API integration](docs/licentivo-contract.md) |
| Stripe, PayPal, Waffo, and Paddle | Payments collected directly into your merchant accounts | [Payment integration](docs/payments.md) |
| Resend or generic SMTP (optional) | Configurable verification, account recovery, and transaction notifications | [Email setup](docs/resend-email.md) |
| DeepL | Translation of merchant content into customer locales | [Language setup](docs/multilingual-deepl.md) |
| HTTPS | Secure public origin and callbacks | [Certificates and HTTPS](docs/ssl-https.md) · [HTTP test guide in 14 languages](docs/http-testing/README.md) |

Use a Licentivo management key with product/policy read access and license write access. There is one Licentivo connection shared by Launch's test and live payment modes. The Licentivo service origin is fixed to `https://www.licentivo.com`.

Merchants with an existing licensing system can instead select **System settings → 授权选择与自定义 → 自定义授权服务**. This mode works without configuring Licentivo. The merchant service implements the [external licensing protocol and integration flow](docs/external-licensing.md), with a separate [OpenAPI specification](docs/external-licensing-openapi.json) and [Python adapter example](examples/custom-licensing/README.md). Launch handles verified purchases and delivery; the merchant retains its own license generation, activation and runtime SDK. Existing orders cannot be silently switched to another provider.

See [external service setup](docs/external-services.md) for the account preparation checklist. Store real credentials only in your private server configuration or the authenticated administration area.

## Configuration reference

The executable automatically reads `config.yaml`. Choose another YAML file with `--config PATH` or `LAUNCH_CONFIG_FILE`; nonempty process environment values override file values. The executable does **not** automatically load `.env`; Docker Compose reads `.env` for its own interpolation.

All settings, defaults, and examples are documented in [configuration.md](docs/configuration.md). Use [.env.example](.env.example) for the development Compose template and [config.example.yaml](config.example.yaml) for a direct deployment. Keep completed server configuration private; it is different from the public JSON exported for customer applications.

## Deployment and operations

- Use HTTPS and a persistent MySQL database for a public deployment. `LAUNCH_ENV=production` requires an HTTPS `LAUNCH_PUBLIC_URL`, a database DSN, and a master key; external acceptance checks still gate live sales.
- Persist `LAUNCH_UPLOAD_DIR` and back it up with the encrypted database backup. Uploaded installers are not part of the executable.
- Keep the process running: licensing, expiration, payment, email, and translation work depends on persistent background jobs.
- Launch calculates absolute purchase dates from verified payments and sends them to Licentivo as versioned snapshots. Launch downtime can delay new purchase, refund and account-revocation synchronization; runtime validity and already-issued offline permission remain governed by Licentivo and its SDK. See the [current contract](docs/licentivo-contract.md).
- The included [Compose file](compose.yaml) is a development scaffold. Container execution, production hardening, and writable installer storage must be validated for your environment.

Backup, restore, upgrade, and monitoring procedures are in the [installation guide](docs/installation.md).

## Commands

| Command | Purpose |
| --- | --- |
| `launch init` | Create a private local configuration with a new master key, without overwriting existing configuration |
| `launch dev` | Start using `config.yaml`; require a configuration file |
| `launch serve` | Start the server; create/update tables when a database is configured |
| `launch check` | Check database connectivity and required tables without modifying the schema |
| `launch keygen` | Print a random base64-encoded 32-byte key |
| `launch version` | Print the application version |
| `launch backup PATH` | Write an encrypted backup using `LAUNCH_BACKUP_PASSPHRASE` |
| `launch restore PATH` | Restore into an empty database; reconcile transactions before releasing the restore pause |

Commands shown as `launch` refer to the executable in your extracted package, for example `./launch` or `.\launch.exe`. Server commands automatically select `config.yaml`; `launch serve --config /path/to/config.yaml` selects a specific file. `--config` takes precedence over `LAUNCH_CONFIG_FILE`.

## Build from source (optional)

Launch is built with **Go, GORM, MySQL, and embedded HTML/CSS/JavaScript**.

Use this path when developing or modifying Launch. It requires Go 1.26+ and Git. Ordinary installations can use the release package above.

```sh
git clone https://github.com/spf86/licentivo-launch.git
cd licentivo-launch
go build -trimpath -o launch ./cmd/launch
./launch init
# Edit database_dsn in config.yaml; retain the generated master_key.
./launch serve
```

On Windows build with `go build -trimpath -o launch.exe ./cmd/launch`, then use `.\launch.exe init` and `.\launch.exe serve`. The frontend is embedded; it does not need a separate build or Node.js server.

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

`-Preview` keeps a disposable test preview running after the test suite. `-KeepMySQL` leaves the isolated MySQL process running when the helper exits. It is not the daily development startup path. Do not point integration tests at an existing customer database.

## Application integration examples

The nine-language Launch application packages below use Licentivo. Custom licensing products use the merchant's own SDK and the [external licensing integration flow](docs/external-licensing.md).

See the public [installation and development documentation](https://www.licentivo.com/launch-docs.html), [manual nine-language guide](docs/manual-integration.md), [examples/README.md](examples/README.md) and [application integration guide](docs/software-sdk-integration.md). Launch automatically fills the current merchant address and existing product data into direct connection code and English AI prompts. No manual product-ID entry or configuration-file generation is required. Install only the Launch package for your language. Each package embeds the pinned official SDK and manages its adapter and SDK linking. The server itself does not depend on the client packages.

The administration **程序接入 → 接入检查与代码** page reads the current merchant address, existing product mapping and signing public keys, validates them, and provides a ready-to-use connection function for each language. Start with the [manual nine-language guide](docs/manual-integration.md), [Chinese quick start](docs/application-quickstart.zh-CN.md), [versioned integration packages](docs/integration-packages.md), and [automated/external acceptance workflow](docs/acceptance.md).

The current source baseline is **Launch server 0.3.5-beta → Launch SDK 0.1.7 → embedded Licentivo SDK 0.11.7**. The [public SDK v0.1.7 release](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.7) describes device names and optional system/hardware details. Review the [application integration guide](docs/software-sdk-integration.md) and [Licentivo contract](docs/licentivo-contract.md) for the required backend endpoints and migration flow. Check the version and checksums inside each publication/installation report; older reports do not certify a newer package or uncommitted local changes. External merchant/payment/licensing acceptance remains separate.

**运营工具 → AI 快速接入** generates an English prompt from your language, platform, project type and protected feature. Select the existing software and environment to include the automatically filled direct initialization code, or generate a clearly marked unconfigured draft. The prompt includes the exact versioned package-manager command or GitHub download, links to the language README and integration guides, the `Connect/connect → Login/Restore → RunFeature` lifecycle, and an acceptance checklist. No configuration files or manual internal product-ID entry are required. Copy or download the prompt for your own AI coding tool; Launch never automatically sends it to an AI service, and excludes passwords, license credentials, sessions, management keys, payment keys and server configuration. See [AI integration](docs/ai-integration.md).

## Repository layout

```text
cmd/launch/                    Application entry point and CLI
config/                        Public configuration examples
internal/config/               YAML/environment configuration
internal/storage/              GORM models, MySQL connection, schema checks
internal/domain/               UTC contract terms and integer money arithmetic
internal/security/             Passwords, sessions, and secret encryption
internal/app/                  Accounts, commerce, entitlements, and jobs
internal/payments/             Stripe, PayPal, Waffo, and Paddle adapters
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
- [Application integration and backend requirements](docs/software-sdk-integration.md)
- [Custom licensing API and integration flow](docs/external-licensing.md)
- [External services](docs/external-services.md)
- [Product requirements](Licentivo%20Launch.md)
- [Development progress and open work](docs/progress.md)
- [Historical architecture decisions](docs/m0.md)
- [Desktop interface review](docs/ui-review.md)
- [Integration OpenAPI](docs/openapi.json) (system, account, licensing, integration wizard and basic purchase APIs; not the complete administration/webhook API)

## License

Launch's own code is available under the [MIT license](LICENSE). Dependencies retain their respective licenses; see [third_party/README.md](third_party/README.md). External payment, email, translation, and licensing services have their own accounts and terms.
