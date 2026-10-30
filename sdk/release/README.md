# Licentivo Launch SDKs

**Add merchant-account login and software licensing to your app in just one day.**

With your existing application and Launch store configured, install one package,
copy the generated connection function, and add login plus protected business
actions. Follow the [one-day integration plan](docs/one-day-integration.md) and
[manual integration guide](docs/manual-integration.md), then verify the sandbox
purchase-to-use flow before production rollout.

The public [v0.1.7 Release](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.7) describes Launch package 0.1.7 with embedded official SDK 0.11.7. Check the [application integration guide](docs/software-sdk-integration.md) for the account-exchange flow and backend requirements. Match artifact checksums and source revisions when checking whether a public package contains a local fix.

[Website](https://www.licentivo.com/en/launch.html) · [Documentation](https://www.licentivo.com/en/launch-docs.html) · [Source code](https://github.com/spf86/licentivo-launch-sdk)

Account login and license-protected application operations in **Go, Python, C#,
Java, JavaScript/TypeScript, Rust, Ruby, C and C++**. MIT licensed.

Install only the Launch package for your language. Each package embeds the
reviewed Licentivo SDK 0.11.7 and manages its adapter and SDK linking. Ordinary
language dependencies still use their package manager.

The Launch merchant address is required at initialization. Select your existing
software in **Launch → 程序接入 → 接入检查与代码** to copy a connection function with
the address, existing product mapping and public signing keys already filled in.
The licensing service is fixed to `https://www.licentivo.com`. No client
configuration file or manual entry of two internal product IDs is required.

## Install into your application

Version: **0.1.7**, with official SDK **0.11.7**, paired with the Launch server **0.3.5-beta** account-exchange source baseline. The commands below target this release. Check the version, source revision and checksums inside [publication status](docs/registry-status.json) and [verification receipts](docs/registry-verification.json); historical reports do not certify a newer package. The [0.1.7 release notes](docs/releases/0.1.7.md) describe this update.

This update includes device-name/system-information reporting and native portability changes while retaining account ownership, recovery and logout retry behavior. Install one Launch package; it includes its reviewed Licentivo runtime. Confirm the matching backend and database revision before deploying the reviewed device-information clients.

| Language | Registry command / distribution | API reference |
| --- | --- | --- |
| Go | `go get github.com/spf86/licentivo-launch-sdk/pkg/launchclient@v0.1.7` | [Go](pkg/launchclient/README.md) |
| Python | `python -m pip install "licentivo-launch==0.1.7"` | [Python](sdk/python/README.md) |
| C# | `dotnet add package Licentivo.Launch --version 0.1.7` | [.NET](sdk/csharp/README.md) |
| Java | Maven/Gradle: `com.licentivo.launch:launch-client:0.1.7` | [Java](sdk/java/README.md) |
| Node.js / TypeScript | `npm install @licentivo/launch@0.1.7` | [Node.js](sdk/javascript/README.md) |
| Rust | `cargo add licentivo-launch@=0.1.7` | [Rust](sdk/rust/README.md) |
| Ruby | `gem install licentivo-launch --version 0.1.7` | [Ruby](sdk/ruby/README.md) |
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
package's feature guard. The wrapper obtains a short-lived identity assertion
from Launch's `/identity` endpoint and passes it in memory to the official SDK
for `/api/v1/launch/exchange` on Licentivo. The removed `/credential` endpoint
returns HTTP 410 (`LICENSE_CREDENTIAL_REMOVED`); it no longer supplies keys.
Launch Restore confirms the account online and obtains a fresh assertion.
Applications needing offline startup use the official SDK's recovery contract;
explicit denial must not fall back to offline authorization.

The [Launch server](https://github.com/spf86/licentivo-launch) manages merchant
accounts, payments and entitlement delivery. This repository contains client
libraries, public examples and release tools. It contains no server startup
configuration, merchant credentials or payment keys.

## Verify and release

See [maintainer release instructions](docs/releasing.md). Public registry
installation checks and local signed SDK fixtures do not establish external
merchant/payment/license acceptance; see [acceptance procedures](docs/acceptance.md).
