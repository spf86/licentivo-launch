# Licentivo Launch SDKs

Version 0.1.6 is distributed through the GitHub Release assets below. Check [publication status](docs/publication-0.1.6.json) for package registry availability.

[Website](https://www.licentivo.com/en/launch.html) · [Documentation](https://www.licentivo.com/en/launch-docs.html) · [Source code](https://github.com/spf86/licentivo-launch-sdk)

Account login and license-protected application operations in **Go, Python, C#,
Java, JavaScript/TypeScript, Rust, Ruby, C and C++**. MIT licensed.

Install only the Launch package for your language. Each package embeds the
reviewed Licentivo SDK 0.11.6 and manages its adapter and SDK linking. Ordinary
language dependencies still use their package manager.

The Launch merchant address is required at initialization. Select your existing
software in **Launch → 程序接入 → 接入检查与代码** to copy a connection function with
the address, existing product mapping and public signing keys already filled in.
The licensing service is fixed to `https://www.licentivo.com`. No client
configuration file or manual entry of two internal product IDs is required.

## Install into your application

Version: **0.1.6**, with official SDK **0.11.6**. The commands below target this release. Publication and fresh-consumer results are recorded in [publication status](docs/registry-status.json) and [verification receipts](docs/registry-verification.json); the [0.1.6 release record](docs/publication-0.1.6.md) describes this update.

This update strengthens account logout persistence, restore coordination and the matching official runtime protections across all nine languages. Install one Launch package; it includes its reviewed Licentivo runtime.

| Language | Registry command / distribution | API reference |
| --- | --- | --- |
| Go | `go get github.com/spf86/licentivo-launch-sdk/pkg/launchclient@v0.1.6` | [Go](pkg/launchclient/README.md) |
| Python | `python -m pip install "licentivo-launch==0.1.6"` | [Python](sdk/python/README.md) |
| C# | `dotnet add package Licentivo.Launch --version 0.1.6` | [.NET](sdk/csharp/README.md) |
| Java | Maven/Gradle: `com.licentivo.launch:launch-client:0.1.6` | [Java](sdk/java/README.md) |
| Node.js / TypeScript | `npm install @licentivo/launch@0.1.6` | [Node.js](sdk/javascript/README.md) |
| Rust | `cargo add licentivo-launch@=0.1.6` | [Rust](sdk/rust/README.md) |
| Ruby | `gem install licentivo-launch --version 0.1.6` | [Ruby](sdk/ruby/README.md) |
| C / C++ | Versioned CMake packages in [GitHub Releases](https://github.com/spf86/licentivo-launch-sdk/releases) | [C/C++](sdk/native/README.md) |

Go requires 1.26+, Python 3.10+, .NET 8+, Java 17+, Node.js 20.3+, Rust 1.85+,
Ruby 3.1+, C11 or C++17. Native packages must match the compiler ABI. Windows
GNU/MinGW is the qualified native toolchain; other platforms require their
normal curl/OpenSSL development libraries when building from source.

## Write the integration manually or with AI

The [manual nine-language guide](docs/manual-integration.md) covers installation,
direct initialization, login, feature guards, restore, logout, device release,
secure stores and errors. Each language has its own README and runnable examples.
AI is optional; Launch's [English prompts](docs/ai-integration.md) describe the
same APIs and automatically filled parameters.

Initialization does not authorize a protected operation. Log in with the
customer's merchant account, then execute protected business code through the
package's feature guard. Preserve the SDK lifecycle for heartbeats and use only
verified, policy-valid grants for offline operation. Explicit denial must not
fall back to offline authorization.

The [Launch server](https://github.com/spf86/licentivo-launch) manages merchant
accounts, payments and entitlement delivery. This repository contains client
libraries, public examples and release tools. It contains no server startup
configuration, merchant credentials or payment keys.

## Verify and release

See [maintainer release instructions](docs/releasing.md). Public registry
installation checks and local signed SDK fixtures do not establish external
merchant/payment/license acceptance; see [acceptance procedures](docs/acceptance.md).
