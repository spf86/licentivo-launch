# Installation and operations

This guide covers a source installation of Licentivo Launch. The project is a development release; complete the external-service acceptance checks before collecting live payments.

## Requirements

- Go 1.26+ to build the server.
- MySQL 8.0.16+ and an empty application database.
- A dedicated database account that can create and alter application tables.
- A persistent directory for uploads and a securely retained instance master key.
- A domain and HTTPS for public deployments.
- Merchant-owned Licentivo, payment, email, and optional DeepL accounts.

The embedded frontend needs no separate web build or Node server. Node.js is only needed for optional JavaScript syntax checks.

## Download and build

```sh
git clone https://github.com/spf86/licentivo-launch.git
cd licentivo-launch
go build -trimpath -o bin/launch ./cmd/launch
```

On Windows:

```powershell
git clone https://github.com/spf86/licentivo-launch.git
Set-Location licentivo-launch
go build -trimpath -o bin/launch.exe ./cmd/launch
```

This repository publishes source. Do not assume a prebuilt executable or container release exists unless one is listed in the repository's Releases section.

## Prepare MySQL

Create an empty database, for example `launch`, and a dedicated user through your normal MySQL administration tools. Use a unique strong password. Point the DSN at the intended server:

```text
launch:YOUR_DATABASE_PASSWORD@tcp(DB_HOST:3306)/launch
```

No SQL schema files are required. GORM's Go models create and update tables when the application starts. Back up an existing database before upgrading because startup can change its schema.

## First run on Windows

```powershell
New-Item -ItemType Directory -Path .local/dev -Force | Out-Null
Copy-Item config/launch-config.example.json .local/dev/launch-config.json
./bin/launch.exe keygen
./bin/launch.exe keygen
```

Edit `.local/dev/launch-config.json`:

1. Replace `LAUNCH_DATABASE_DSN` with your own connection string.
2. Put the first generated key in `LAUNCH_MASTER_KEY`.
3. Put the second, independently generated value in `LAUNCH_SETUP_TOKEN`.
4. Check the listener, public URL, and upload directory.

Start the application:

```powershell
./bin/launch.exe dev
```

Open `http://localhost:18080/#/setup`. Use the setup token and create your own administrator. A fresh installation has no default administrator or password. After setup, remove `LAUNCH_SETUP_TOKEN` from the private file and restart.

For daily development, run `go run ./cmd/launch dev` or `./scripts/dev.ps1`. Both use the existing configured database. The script builds the executable; it does not initialize a disposable MySQL instance.

## First run on Linux or macOS

```sh
mkdir -p .local/dev
cp config/launch-config.example.json .local/dev/launch-config.json
./bin/launch keygen
./bin/launch keygen
```

Edit the copied JSON file as described above, then:

```sh
./bin/launch dev
```

Complete `http://localhost:18080/#/setup`, remove the setup token from the private configuration, and restart. Local build instructions are provided for these systems; the historical verification record includes a Linux cross-build, not a full Linux deployment acceptance test.

## Environment-only configuration

Instead of JSON, set process variables. For a foreground development session on Linux:

```sh
export LAUNCH_ENV=development
export LAUNCH_HTTP_ADDR=127.0.0.1:8080
export LAUNCH_PUBLIC_URL=http://localhost:8080
export LAUNCH_DATABASE_DSN='launch:YOUR_DATABASE_PASSWORD@tcp(127.0.0.1:3306)/launch'
export LAUNCH_MASTER_KEY='YOUR_PERSISTENT_GENERATED_MASTER_KEY'
export LAUNCH_SETUP_TOKEN='YOUR_SEPARATE_GENERATED_SETUP_TOKEN'
export LAUNCH_UPLOAD_DIR='/srv/licentivo-launch/uploads'
./bin/launch serve
```

Create the upload directory with write access for the application user. Avoid storing real secrets in shell history; use an access-restricted service environment file or private JSON file for an installed service. The binary does not automatically source `.env`.

## Configure the merchant workflow

1. Set the brand and support contact information.
2. Configure verified Resend email delivery and send a test email.
3. Connect one Licentivo read/write management API key and read the product/policy catalog.
4. Create local products and select a Licentivo policy for each sales plan.
5. Publish plans to Launch's sandbox payment environment.
6. Configure Stripe and PayPal test accounts and their webhook endpoints.
7. Test purchase, payment verification, license synchronization, application login, renewal, upgrade, refund, and device operations.
8. Configure DeepL if automatic customer-content translation is needed.
9. Complete deployment-specific acceptance checks before enabling a public live-sales deployment.

Detailed service links and instructions are in [external-services.md](external-services.md). Desktop/API integration is in [software-sdk-integration.md](software-sdk-integration.md).

## Public deployment

Run the executable as a dedicated unprivileged operating-system user under your service manager. Set its working directory explicitly and keep its private configuration outside the release directory or in another persistent location. Use absolute paths for production uploads and configuration.

A typical reverse-proxy deployment uses:

```text
LAUNCH_ENV=production
LAUNCH_HTTP_ADDR=127.0.0.1:8080
LAUNCH_PUBLIC_URL=https://shop.example.com
LAUNCH_UPLOAD_DIR=/srv/licentivo-launch/uploads
LAUNCH_CONFIG_FILE=/etc/licentivo-launch/config.json
```

The private JSON file or service environment must also supply the database DSN and existing master key. Terminate TLS at Caddy, Nginx, a load balancer, or a managed ingress. Route the public origin to the application and preserve webhook request bodies and authentication headers. Keep MySQL private.

A minimal systemd service example follows. Replace paths and create the `launch` user, configuration, and writable upload directory before enabling it:

```ini
[Unit]
Description=Licentivo Launch
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=launch
Group=launch
WorkingDirectory=/opt/licentivo-launch
Environment=LAUNCH_CONFIG_FILE=/etc/licentivo-launch/config.json
ExecStart=/opt/licentivo-launch/launch serve
Restart=on-failure
RestartSec=5
UMask=0077
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

This service definition is an example requiring validation on the target host. Windows deployments can use a service manager that runs the same `launch.exe serve` command with the intended working directory and environment. Avoid changing unrelated database services.

For built-in HTTPS, configure a separate `LAUNCH_HTTPS_ADDR`, upload a certificate through system settings, and follow [ssl-https.md](ssl-https.md).

## Docker Compose development template

```powershell
Copy-Item .env.example .env
# Edit .env and supply independent MySQL passwords and a generated master key.
docker compose up --build -d
```

The template builds the Go server, waits for the MySQL health check, and publishes the application at `http://localhost:8080`. MySQL data is stored in the `mysql-data` volume. Set a separate setup token for first-time administrator creation.

The application filesystem is read-only and the default upload directory is not a persistent writable mount. Before using installer or document-image uploads, add a dedicated upload bind mount or volume, set `LAUNCH_UPLOAD_DIR` to its container path, and grant write access to the image's UID/GID `65532`. For a bind mount, prepare permissions on the Docker host. Retain that directory with your database backups.

Compose/MySQL 8.4 container operation has not yet been validated in the recorded development environment. Treat this as a development scaffold. Validate mounts, permissions, HTTPS, health checks, and image pinning before public deployment. Never use `docker compose down -v` when you need to retain the database volume.

## Health and troubleshooting

| Check or symptom | Meaning or action |
| --- | --- |
| `GET /healthz` returns 200 | The HTTP process is alive |
| `GET /readyz` returns 200 | MySQL is reachable and required tables exist |
| `/readyz` returns 503 | Database missing, unreachable, or schema unavailable; this does not indicate successful business setup |
| Startup cannot connect to MySQL | Check the configured host/port, network route, server listener, account permissions, and DSN |
| Address already in use | Stop the intended old instance or choose another listener and matching public URL |
| Missing local development configuration | Create `.local/dev/launch-config.json` from the public example before using `dev` |
| Credentials cannot be decrypted | Restore the original instance master key; do not generate a replacement for an existing database |
| Payment confirmed but license pending | Inspect licensing jobs and the read/write Licentivo key; query/retry the original operation |
| Upload failure | Check `LAUNCH_UPLOAD_DIR`, storage capacity, filesystem permissions, and container mounts |
| `.env` changes have no effect outside Compose | Supply process variables or select a JSON file with `LAUNCH_CONFIG_FILE` |

`launch check` performs a non-mutating connectivity/schema check. Select the same private configuration explicitly if you are not using `dev`:

```powershell
$env:LAUNCH_CONFIG_FILE = (Resolve-Path .local/dev/launch-config.json).Path
./bin/launch.exe check
```

## Backup and restore

Use a unique backup passphrase, retain the instance master key securely, and store encrypted backups away from the server. The backup file includes database records and required recovery configuration. Copy the upload directory separately; uploaded installer/document files are not embedded in the database backup.

With your private configuration selected:

```powershell
$env:LAUNCH_BACKUP_PASSPHRASE = 'YOUR_SEPARATE_BACKUP_PASSPHRASE'
./bin/launch.exe backup 'D:\Backups\launch.launchbackup'
```

For restore, configure a **new empty database**, the required recovery settings, and the same backup passphrase, then:

```powershell
./bin/launch.exe restore 'D:\Backups\launch.launchbackup'
```

Restore the uploads and verify decryption, accounts, history, and license associations. The CLI also writes a private `PATH.secrets.json` recovery configuration next to the archive. Protect this file: it contains credentials and is ignored by Git. A restored instance must use the original master key; the restore command can recover it from the encrypted backup when the configured master key is empty, and rejects a different configured key. The application keeps restored transactions paused until an administrator reconciles payments/refunds/licensing changes after the backup point and explicitly releases the pause. Rehearse restoration before relying on a backup.

## Upgrades

1. Back up the database, persistent configuration/master key, and upload directory.
2. Review changes and test the new build against a restored staging database.
3. Retain the old executable and a compatible database backup.
4. Stop the intended application instance and replace the executable.
5. Restart with the same configuration and master key; GORM may update tables.
6. Verify health, readiness, login, queues, payments, and license synchronization.

Do not assume that an executable rollback can undo schema/data changes. Plan database restoration and payment reconciliation where necessary. Launch must remain available to process expiration and reconciliation jobs; long outages can delay remote suspension at a local sales deadline.
