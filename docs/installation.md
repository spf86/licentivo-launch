# Installation and operations

This guide starts with a ready-to-run Licentivo Launch release package. The project is a development release; complete the external-service acceptance checks before collecting live payments.

## Download the server package

Choose a Windows or Linux package for amd64 or arm64 from [GitHub Releases](https://github.com/spf86/licentivo-launch/releases/tag/v0.2.0-beta.2), then extract it. The package includes the executable, installation guide, public configuration example and license notices.

You need MySQL 8.0.16+, an empty database and an account permitted to create/alter application tables. Go, Git, Node.js and a compiler are not required to run the package. The [binary guide](binary-installation.md) lists direct downloads, architecture selection and checksum verification.

Use the extracted directory as the working directory for the commands below. For public hosting, also prepare a domain, HTTPS and persistent upload storage. Service accounts can be connected after the site starts.

## Prepare MySQL

Create an empty database, for example `launch`, and a dedicated user through your normal MySQL administration tools. Use a unique strong password. Point the DSN at the intended server:

```text
launch:YOUR_DATABASE_PASSWORD@tcp(DB_HOST:3306)/launch?parseTime=true&loc=UTC&charset=utf8mb4
```

No SQL schema files are required. GORM's Go models create and update tables when the application starts. Back up an existing database before upgrading because startup can change its schema.

## First run on Windows


```powershell
./launch.exe init
```

Edit `config.yaml` in the extracted directory:

1. Replace `database_dsn` with your own connection string.
2. Keep the automatically generated `master_key`.
3. Check the listener, public URL, and upload directory.

Start the application:

```powershell
./launch.exe serve
```

Open `http://localhost:18080/#/login`. A new database automatically receives the initial account `admin@example.com` with password `LaunchAdmin123!`. The first login requires a different password of 12-128 bytes. Sign in again with the replacement password to access administration. Complete this step on localhost before exposing a public origin. No restart after password change is required. Existing administrators are not reset.

For subsequent starts, return to the same extracted directory and run `./launch.exe serve`. The existing configuration, database and administrator password are retained. Source development is covered in the optional build section below.

## First run on Linux

```sh
./launch init
```

Edit `config.yaml` as described above, then:

```sh
./launch serve
```

Open `http://localhost:18080/#/login`, sign in with `admin@example.com` / `LaunchAdmin123!`, and complete the required password change. The default listener is loopback. The Linux binaries are cross-built with CGO disabled; Linux and ARM64 runtime acceptance remains dependent on the target machine.

## Environment-only configuration

For containers and service managers, process variables can override or replace YAML settings. For a foreground development session on Linux:

```sh
export LAUNCH_ENV=development
export LAUNCH_HTTP_ADDR=127.0.0.1:8080
export LAUNCH_PUBLIC_URL=http://localhost:8080
export LAUNCH_DATABASE_DSN='launch:YOUR_DATABASE_PASSWORD@tcp(127.0.0.1:3306)/launch?parseTime=true&loc=UTC&charset=utf8mb4'
export LAUNCH_MASTER_KEY='YOUR_PERSISTENT_GENERATED_MASTER_KEY'
export LAUNCH_UPLOAD_DIR='/srv/licentivo-launch/uploads'
./launch serve
```

Create the upload directory with write access for the application user. Use a private YAML file or an access-restricted service environment file to avoid storing real secrets in shell history. The binary does not automatically source `.env`.

## Configure the merchant workflow

1. Set the brand and support contact information.
2. Optionally configure Resend or generic SMTP under **System settings > Email and registration**, send a test email, and choose whether registration requires email verification. Without email, customers can register and purchase directly.
3. Connect one Licentivo read/write management API key and read the product/policy catalog.
4. Create local products and select a Licentivo policy for each sales plan.
5. Publish plans to Launch's sandbox payment environment.
6. Configure the desired Stripe, PayPal, Waffo, or Paddle test accounts and their webhook endpoints. See [Waffo and Paddle](waffo-paddle.md) for provider-specific steps.
7. Test purchase, payment verification, license synchronization, application login, renewal, upgrade, refund, and device operations.
8. Configure DeepL if automatic customer-content translation is needed.
9. Complete deployment-specific acceptance checks before enabling a public live-sales deployment.

Detailed service links and instructions are in [external-services.md](external-services.md). Desktop/API integration is in [software-sdk-integration.md](software-sdk-integration.md).

## Public deployment

Run the executable as a dedicated unprivileged operating-system user under your service manager. Set its working directory explicitly and keep its private configuration outside the release directory or in another persistent location. Use absolute paths for production uploads and configuration.

A typical reverse-proxy deployment uses:

```yaml
environment: production
http_addr: "127.0.0.1:8080"
public_url: "https://shop.example.com"
upload_dir: "/srv/licentivo-launch/uploads"
database_dsn: "launch:YOUR_DATABASE_PASSWORD@tcp(127.0.0.1:3306)/launch?parseTime=true&loc=UTC&charset=utf8mb4"
master_key: "KEEP_THE_EXISTING_INSTANCE_MASTER_KEY"
```

Save this as `/etc/licentivo-launch/config.yaml`, fill the real database connection and retain the existing master key. Terminate TLS at Caddy, Nginx, a load balancer, or a managed ingress. Route the public origin to the application and preserve webhook request bodies and authentication headers. Keep MySQL private.

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
ExecStart=/opt/licentivo-launch/launch serve --config /etc/licentivo-launch/config.yaml
Restart=on-failure
RestartSec=5
UMask=0077
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

This service definition is an example requiring validation on the target host. Windows deployments can use a service manager that runs the same `launch.exe serve` command with the intended working directory and environment. Avoid changing unrelated database services.

For built-in HTTPS, configure a separate `LAUNCH_HTTPS_ADDR`, upload a certificate through system settings, and follow [ssl-https.md](ssl-https.md).

## Building from source (optional)

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

## Docker Compose development template

```powershell
Copy-Item .env.example .env
# Edit .env and supply independent MySQL passwords and a generated master key.
docker compose up --build -d
```

The template builds the Go server, waits for the MySQL health check, and publishes the application at `http://localhost:8080`. MySQL data is stored in the `mysql-data` volume. The initial administrator is created automatically; use the first-login password-change flow. Optional `LAUNCH_ADMIN_EMAIL` and `LAUNCH_ADMIN_PASSWORD` override the initial account only on a new database.

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
| Missing configuration | Run `launch init` and edit `config.yaml` |
| Credentials cannot be decrypted | Restore the original instance master key; do not generate a replacement for an existing database |
| Payment confirmed but license pending | Inspect licensing jobs and the read/write Licentivo key; query/retry the original operation |
| Upload failure | Check `LAUNCH_UPLOAD_DIR`, storage capacity, filesystem permissions, and container mounts |
| `.env` changes have no effect outside Compose | Edit `config.yaml`, use `--config PATH`, or supply process variables |

`launch check` performs a non-mutating connectivity/schema check and automatically reads `config.yaml`. An explicit path is also supported:

```powershell
./launch.exe check --config ./config.yaml
```

## Backup and restore

Use a unique backup passphrase, retain the instance master key securely, and store encrypted backups away from the server. The backup file includes database records and required recovery configuration. Copy the upload directory separately; uploaded installer/document files are not embedded in the database backup.

With your private configuration selected:

```powershell
$env:LAUNCH_BACKUP_PASSPHRASE = 'YOUR_SEPARATE_BACKUP_PASSPHRASE'
./launch.exe backup 'D:\Backups\launch.launchbackup'
```

For restore, configure a **new empty database**, the required recovery settings, and the same backup passphrase, then:

```powershell
./launch.exe restore 'D:\Backups\launch.launchbackup'
```

Restore the uploads and verify decryption, accounts, history, and license associations. The CLI also writes a private `PATH.secrets.yaml` recovery configuration next to the archive. Protect this file: it contains credentials and is ignored by Git. A restored instance must use the original master key; the restore command can recover it from the encrypted backup when the configured master key is empty, and rejects a different configured key. The application keeps restored transactions paused until an administrator reconciles payments/refunds/licensing changes after the backup point and explicitly releases the pause. Rehearse restoration before relying on a backup.

## Upgrades

1. Back up the database, persistent configuration/master key, and upload directory.
2. Review changes and test the new build against a restored staging database.
3. Retain the old executable and a compatible database backup.
4. Stop the intended application instance and replace the executable.
5. Restart with the same configuration and master key; GORM may update tables.
6. Verify health, readiness, login, queues, payments, and license synchronization.

Do not assume that an executable rollback can undo schema/data changes. Plan database restoration and payment reconciliation where necessary. Launch must remain available to process expiration and reconciliation jobs; long outages can delay remote suspension at a local sales deadline.
