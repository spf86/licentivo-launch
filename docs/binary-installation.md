# Install a binary release

Download a package from [Licentivo Launch releases](https://github.com/spf86/licentivo-launch/releases). Go, Node.js, and a compiler are not required. You still need MySQL 8.0.16+ with an empty database and an account permitted to create/alter application tables.

## Choose your package

| Operating system / processor | Package suffix |
| --- | --- |
| Windows, Intel/AMD x64 | [windows_amd64.zip](https://github.com/spf86/licentivo-launch/releases/download/v0.2.0-beta.1/licentivo-launch_0.2.0-beta.1_windows_amd64.zip) |
| Windows, ARM64 | [windows_arm64.zip](https://github.com/spf86/licentivo-launch/releases/download/v0.2.0-beta.1/licentivo-launch_0.2.0-beta.1_windows_arm64.zip) |
| Linux, x86-64 | [linux_amd64.tar.gz](https://github.com/spf86/licentivo-launch/releases/download/v0.2.0-beta.1/licentivo-launch_0.2.0-beta.1_linux_amd64.tar.gz) |
| Linux, AArch64 | [linux_arm64.tar.gz](https://github.com/spf86/licentivo-launch/releases/download/v0.2.0-beta.1/licentivo-launch_0.2.0-beta.1_linux_arm64.tar.gz) |

`amd64` means x86-64 and works on compatible Intel and AMD processors. `arm64` means 64-bit ARM. Use the operating-system and architecture combination that matches your machine. MySQL may run locally or on a reachable separate server.

## Windows

Extract the ZIP, open PowerShell in the extracted folder containing `launch.exe`, and run:

```powershell
./launch.exe init
notepad .local/dev/launch-config.json
```

Set `LAUNCH_DATABASE_DSN` to your own database connection, for example:

```text
launch:YOUR_DATABASE_PASSWORD@tcp(127.0.0.1:3306)/launch
```

Keep the automatically generated `LAUNCH_MASTER_KEY`. Save the file and start:

```powershell
./launch.exe dev
```

## Linux

Extract the archive for your architecture and enter its directory:

```sh
tar -xzf licentivo-launch_0.2.0-beta.1_linux_amd64.tar.gz
cd licentivo-launch_0.2.0-beta.1_linux_amd64
chmod +x launch
./launch init
```

For ARM64, substitute `arm64` in the archive and directory names. Edit `.local/dev/launch-config.json` with your MySQL DSN, retain the generated master key, then run:

```sh
./launch dev
```

The Linux archives preserve the executable permission. The explicit `chmod` command also handles files copied through a filesystem that dropped that permission.

## Initial administrator and required password change

Open **http://localhost:18080/#/login** on the server. The first start against a new database automatically creates:

| Field | Value |
| --- | --- |
| Email | `admin@example.com` |
| Initial password | `LaunchAdmin123!` |

The first login opens **Change the initial password**. Enter the current password and a different new password of 12-128 bytes, confirm it, and save. Sign in again with your new password. Administration and desktop sessions remain blocked until the change is complete. The old password and old sessions stop working after the change.

Complete this first-login step on localhost before exposing the public site. Existing installations keep their current administrator accounts and passwords. Optional `LAUNCH_ADMIN_EMAIL` and `LAUNCH_ADMIN_PASSWORD` values can select the initial account on a fresh database only; they do not reset an installed account.

## What the commands do

- `launch init` creates `.local/dev/launch-config.json` with a fresh master key. It refuses to overwrite an existing file.
- `launch dev` loads that private file, connects to MySQL, maintains the schema, creates the initial account once, and starts the server. It does not start MySQL.
- `launch check` checks database readiness. Set `LAUNCH_CONFIG_FILE` explicitly for this command, or supply the equivalent process environment.
- `launch version` prints the release version.
- `Ctrl+C` stops a foreground server.

Replace `launch` with `./launch` on Linux or `./launch.exe` on Windows. Keep private configuration, the master key, database data, and `.local/uploads` between upgrades. Never copy one installation's private configuration into a public package.

## Verify downloads

The release includes `SHA256SUMS.txt`. On Windows:

```powershell
Get-FileHash .\licentivo-launch_0.2.0-beta.1_windows_amd64.zip -Algorithm SHA256
```

Compare its hash with the corresponding line in `SHA256SUMS.txt`. On Linux, in a directory containing all four archives and the checksum file:

```sh
sha256sum -c SHA256SUMS.txt
```

If you downloaded only one archive, verify its corresponding line:

```sh
grep 'linux_amd64.tar.gz$' SHA256SUMS.txt | sha256sum -c -
```

Use `linux_arm64` for that package. Checksums detect download changes; keep the files together from the same release.

## Configure your storefront

After changing the password, set the brand/products/plans, connect your Licentivo management key, configure Resend and payment providers, and perform test purchases and software activation. Use HTTPS for a public deployment and a writable persistent upload directory.

See the [full installation guide](https://github.com/spf86/licentivo-launch/blob/main/docs/installation.md), [configuration reference](https://github.com/spf86/licentivo-launch/blob/main/docs/configuration.md), and [Licentivo](https://www.licentivo.com/).

## Release scope

Version 0.2.0-beta.1 is a development beta. Native Windows amd64 startup and isolated MySQL account/commerce tests are checked. Windows arm64 and Linux amd64/arm64 packages are cross-built; they are not represented as natively tested on every target. Linux binaries use `CGO_ENABLED=0`. Real payment/email/translation/licensing acceptance and production deployment remain deployment-specific work; live-sales gates are retained.
