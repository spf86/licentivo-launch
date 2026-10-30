# Launch application integration packages

Launch **0.1.7** embeds official Licentivo SDK **0.11.7** for all **nine languages**. Install only the Launch package for your language. Its implementation, default adapter, license notices and SDK linking configuration are included. TypeScript declarations belong to JavaScript; the trusted Go Web backend is an extra example. Public sources, registry commands and per-language availability are maintained in the [Launch SDK repository](https://github.com/spf86/licentivo-launch-sdk) and its [publication status](https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status.json).

The 0.1.7 update carries the upstream C/C++ CMake fix for curl config-package and FindCURL consumers. The licensing protocol, application APIs and account lifecycle are unchanged. Existing applications can upgrade the Launch package and rebuild without changing their integration code. The [0.1.7 publication record](https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/publication-0.1.7.md) tracks public availability and clean installation verification separately from local builds.

| Language | Single Launch artifact | Package source / API guide |
| --- | --- | --- |
| Go | Self-contained module ZIP | [pkg/launchclient](../pkg/launchclient/README.md) |
| Python | licentivo_launch-0.1.7-py3-none-any.whl | [sdk/python](../sdk/python/README.md) |
| C# / .NET 8 | Licentivo.Launch.0.1.7.nupkg | [sdk/csharp](../sdk/csharp/README.md) |
| Java 17 | launch-client-0.1.7.jar and POM | [sdk/java](../sdk/java/README.md) |
| JavaScript / Node.js | licentivo-launch-0.1.7.tgz | [sdk/javascript](../sdk/javascript/README.md) |
| Rust | licentivo-launch-0.1.7.crate or source ZIP | [sdk/rust](../sdk/rust/README.md) |
| Ruby | Platform Launch gem or source gem | [sdk/ruby](../sdk/ruby/README.md) |
| C | Launch native CMake installation ZIP | [sdk/native](../sdk/native/README.md) |
| C++ | Same native archive, C++17 API | [sdk/native](../sdk/native/README.md) |

## Install from a public registry

Public 0.1.7 installation checks passed for all nine languages: Go, Python, C#, Java, Node.js/TypeScript, Rust, Ruby, C and C++. The registry commands and native downloads below are publicly available and verified in independent consumers. The publication status records exact package checksums, installation results and platform limits.

```sh
go get github.com/spf86/licentivo-launch-sdk/pkg/launchclient@v0.1.7
python -m pip install licentivo-launch==0.1.7
npm install @licentivo/launch@0.1.7
dotnet add package Licentivo.Launch --version 0.1.7
cargo add licentivo-launch@=0.1.7
gem install licentivo-launch --version 0.1.7
```

Java uses Maven/Gradle coordinates `com.licentivo.launch:launch-client:0.1.7`.
C/C++ use the ABI-compatible native archive from the [versioned GitHub release](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.7).
See the publication status for registry propagation and verification results.

## Install a downloaded distribution

Use the artifacts identified in integration-packages.json. Each language records sdk_managed: true and requires_separate_official_sdk: false. Verify SHA256SUMS before installation.

```powershell
python -m pip install ./licentivo_launch-0.1.7-py3-none-any.whl
npm install ./licentivo-launch-0.1.7.tgz
dotnet add YOUR_PROJECT package Licentivo.Launch --version 0.1.7 --source YOUR_LOCAL_FEED
gem install --local ./licentivo-launch-0.1.7-x64-mingw-ucrt.gem
```

Java uses only the Launch JAR on its classpath; its POM has no official SDK dependency. Offline Go consumers can extract the module ZIP and use a local require/replace for `github.com/spf86/licentivo-launch-sdk`. Offline Rust consumers can extract the crate or source ZIP and add a local licentivo-launch path dependency. No official SDK package or checkout is needed.

For C/C++, extract the native installation ZIP, add its prefix to CMake and use:

```cmake
find_package(LicentivoLaunch CONFIG REQUIRED)
add_executable(myapp main.c)
licentivo_launch_link(myapp)
# C++17 target: licentivo_launch_link(myapp CXX)
```

Launch resolves its own SDK, transitive libraries and bundled Windows DLLs. Rust's build.rs compiles its embedded C core and copies bundled DLLs beside Cargo executables. Ruby's platform gem includes its private native core and runtime libraries; the source gem compiles the core automatically.

Normal language toolchains and ordinary dependencies still apply: pip resolves cryptography, NuGet resolves BouncyCastle.Cryptography, and Cargo resolves ordinary Rust crates. Native archives cover Windows amd64 GNU and Linux amd64. Windows distributions include curl. Other operating systems use the complete source with a C11 compiler and normal OS curl/OpenSSL development libraries. SDK 0.11.7's Windows native core requires GNU/MinGW.

## Initialize the application

Select the existing software in Launch. It automatically reads the current merchant URL, the existing plan/product mapping and official signing public keys, and supplies a filled connection function. Copy this function into the application; no manual internal-ID entry or configuration file generation is required. Server startup still uses private config.yaml, which never ships with the application.

Use the package entry point, such as launchclient.Connect, Python Application.connect, C# Application.Connect or Node Application.connect. The built-in factory creates and manages the SDK. Custom factories are optional testing/integration hooks, not installation steps. Exact APIs are in each package README and example.

Launch handles account login, scoped credentials, bounded pending retries and restoration. The embedded SDK verifies signatures and authorizes each protected operation. Normal close, logout and device release remain distinct. Web applications check permissions on a trusted backend; Node/Electron code belongs in the main or server process.

Manual development instructions remain in [manual-integration.md](manual-integration.md) and every language README. The administration AI integration page generates English instructions for the same direct initialization flow. It excludes passwords, management keys and runtime customer/device credentials and does not call an AI service. See [AI integration](ai-integration.md).

## Build distributions as a maintainer

Only maintainers refreshing the embedded SDK need reviewed upstream sources. Consumers skip this section.

```powershell
./scripts/prepare-examples.ps1 -Source 'C:\path\to\reviewed-sdk\sdk'
./scripts/build-integration-packages.ps1 -Output artifacts/NEW_DIRECTORY -BuildRustCrate -BuildNative -CurlRoot PATH_TO_CURL -CCompiler PATH_TO_GCC
```

Supply tool paths with -Python, -DotNet, -JavaHome, -Ruby, -Cargo and -CMake as needed. The builder verifies the upstream manifest, then prepare-managed-sdk.mjs embeds the sources and records source/destination SHA-256 values in sdk/managed-sdk-manifest.json. Module locations/imports change where necessary; cryptographic verification stays in the reviewed implementation. MIT licenses and notices accompany every embedded copy.

Each build creates a new directory with per-language artifacts, source archives, a combined source bundle, inventory and checksums. -SourceOnly explicitly emits source artifacts; skipped targets are not tested binary releases. The combined bundle preserves package, example, documentation and test paths without requiring examples/vendor.

## Verify installations and behavior

scripts/test-managed-installations.ps1 installs the distribution into fresh consumer projects. It verifies all nine targets without a separate official SDK package, SDK source path, copied adapter or manual SDK link configuration. It compiles and executes the exact generated direct connection snippets, including escaped public data, and reports configuration_files_required: false. Its report is separate from the runtime suite.

scripts/test-integration-packages.ps1 runs lifecycle regressions and locally signed real-SDK fixtures. Supply tool paths and a configured native test build:

```powershell
./scripts/test-integration-packages.ps1 -Python python -DotNet dotnet -JavaHome $env:JAVA_HOME -Ruby ruby -Cargo cargo -NativeBuildDirectory .cache/launch-native-build
```

Repository source builds use normal OS native dependencies; Windows maintainers can supply LICENTIVO_CURL_ROOT. This is a source-build tool setting, not a separate SDK install. With different Rust/C++ MinGW installations, -NativeRuntimeDirectories scopes C++ DLL directories to native checks. Missing prerequisites fail the affected target. all_nine_passed requires all nine checks to pass.

## Storage and acceptance

Sessions default to memory. Remembered login needs a protected store. AES-GCM stores require a 32-byte key obtained from an OS vault and never stored beside the ciphertext. Passwords are never persisted. Go, Python, C# and native C/C++ offer Windows current-user DPAPI; Java, Node, Rust and Ruby offer encrypted stores with caller-managed keys.

SDK device files retain their upstream format. Session encryption does not encrypt every SDK file. Restrict scoped account/product/environment cache directories and exclude them from installers, logs and version control. Identity/configuration changes cannot reuse another account's cache.

Explicit session/entitlement denial cannot fall back offline. Only transport failures or HTTP 5xx may use an existing cryptographically verified grant within its signed policy. First activation, floating seats and policies forbidding offline operation retain their connection requirements.

Local reports never prove real payment, issuance or external activation. That environment was deferred; see [acceptance.md](acceptance.md). Registry publication and non-Windows release qualification require separate work and evidence.
