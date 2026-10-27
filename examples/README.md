# Application integration examples

For merchant-owned licensing services, start with [the custom HTTP adapter](custom-licensing/README.md) and [protocol guide](../docs/external-licensing.md). The nine application packages below specifically integrate Licentivo; custom services retain their own runtime SDK.

> These local account-exchange changes are unpublished. Launch computes purchased dates; the official SDK owns runtime permission and logout recovery. See docs/account-purchase-boundary.md.

Read the [application integration guide](../docs/software-sdk-integration.md) for the API sequence, security rules, single-package installation, and language-specific commands.

For a guided setup, open administration **程序接入**, select existing software and copy its automatically filled direct Connect/connect function. No product ID entry or configuration files are required. Manual developers can follow [the nine-language guide](../docs/manual-integration.md). See the [Chinese quick start](../docs/application-quickstart.zh-CN.md), [versioned packages](../docs/integration-packages.md) and [acceptance tools](../docs/acceptance.md).

All nine official SDK languages have reusable application lifecycle wrappers. Sessions default to memory; remembered login requires the package’s protected storage implementation or a caller-supplied secure store. Ordinary close, logout and device release are distinct. Exact flags and API signatures are in each package README.

The official Licentivo SDK handles machine identity, signatures, activation, heartbeats, offline cache, and metering. Launch's account adapter signs in to the merchant service and retrieves only the authenticated account's product credential.

| Technology | Reference implementation |
| --- | --- |
| C | `../sdk/native` and `c/main.c` |
| C++ | `../sdk/native` and `cpp/main.cpp` |
| C#/.NET 8+ | `csharp/Program.cs` |
| Go | `../pkg/launchclient` and the separate module in `go/` |
| Python | `python/main.py` |
| Java | `java/Main.java` and `java/run.ps1` |
| JavaScript / Node.js (TypeScript declarations included) | `javascript/main.mjs` |
| Rust | `rust/src/main.rs` |
| Ruby | `ruby/main.rb` |
| Web backend | Separate module in `web-backend/` |

## Install Launch

Install only the Launch package for your language, following its README. All nine packages embed SDK 0.11.5, the default adapter and SDK linking configuration. No official SDK source checkout, extra official package or adapter copy is required. Windows Ruby/Rust/native distributions bundle curl; source builds on other platforms use their normal C11 and OS curl/OpenSSL tools.

The server startup configuration is private config.yaml. Application initialization uses public parameters directly; config files are not a prerequisite. The full CLI harnesses are reference/test utilities; use the direct Connect/connect function in normal applications.

Installers contain only public product configuration. End users sign in rather than manually entering license keys. Credentials and cache state are isolated by merchant origin, account, product, and payment environment; logout clears the current account's local session and scoped cache.

These are local reference implementations. Before a release, test real registration/verification, payment-to-activation, pending issuance, identity exchange and SDK-owned logout recovery, restart, denied access, renewal, upgrade, logout, account switching, device limits, and offline expiry. Record OS, compiler/framework, and API/SDK versions. Successful compilation is not production acceptance.
