# Launch C / C++ 0.1.6

> Local account-exchange changes are unpublished. Login uses a short-lived identity assertion; Launch calculates purchased dates and Licentivo owns runtime permission. See the repository guide docs/account-purchase-boundary.md.

C11/C++17 and CMake. Download the Windows amd64 GNU or Linux amd64 native installation ZIP from [GitHub Releases](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.6), verify SHA256SUMS, then add only that Launch prefix to CMake. SDK 0.11.6 core, adapters, import targets and notices are included. The Windows archive bundles curl. Other platforms can build the complete source using standard C11/curl/OpenSSL development libraries.

```cmake
find_package(LicentivoLaunch CONFIG REQUIRED)
add_executable(myapp main.c)
licentivo_launch_link(myapp)
# C++ target: licentivo_launch_link(myapp CXX)
```

The helper handles SDK/transitive linkage and bundled Windows runtime DLLs. No second find_package for an official SDK or copied adapter is required.

Version 0.1.6 embeds the SDK 0.11.6 CMake fix: installed SDK targets resolve curl through the consumer's public `CURL::libcurl` target, including when the SDK was built with curl's config package and the consumer uses FindCURL. Consumers do not need to add curl compatibility aliases. Match the compiler, architecture and curl runtime/development dependencies to the downloaded native package. The licensing protocol and client API are unchanged.

## Direct initialization

C uses launch_application_connect(origin, launchProductID, licentivoProductID, trustedKeysJSON, environment, remember, store). trustedKeysJSON is an in-memory object string. Launch supplies the filled connect_launch function; no JSON file is created or read. C++ uses Application::Connect with the same public values and supplies an owning connectLaunch function.

## Manual lifecycle

C: call launch_application_login, then launch_application_run_feature around every business callback. The callback returns 1 only on business success. Check launch_application_error on failures. launch_application_restore is three-state: 1 ready, 0 no session, -1 failed/denied. Check logout and release_device return values. launch_application_close frees the application; ln_free_string frees returned status text.

C++: call Login and RunFeature; handle exceptions. Restore returns false without a session and throws on a failed restoration. Logout and ReleaseDevice are explicit actions. RAII destruction performs normal close; callback exceptions propagate and cannot execute unlicensed work.

Windows remember=1 supports current-user DPAPI. Other platforms must supply secure store callbacks; no plaintext persistence fallback is provided. C++ follows the same native store semantics.

Launch automatically supplies the current merchant URL, existing product mapping and signing public keys when you select the software in its integration page. Copy its connection function, or use the values already shown there when writing code manually. Do not ask the integrator to type two internal product IDs. No launch-config.json or product.json file is required. The merchant URL is mandatory; the official licensing origin is fixed inside the package to https://www.licentivo.com.

The connection function creates the account adapter. Login obtains a short-lived identity assertion, which is passed only in memory to the official SDK with the connection and environment. Remembered account records contain public routing context and the Launch account session; they never store the assertion, license key or signed runtime permission. Every protected action still passes the official SDK.

Normal close preserves remembered login and device binding. Explicit Logout first marks the protected account record as signed out, delegates device logout to the official SDK and stops local use. Until Launch confirms sign-out (HTTP 200 or an already-invalid session via HTTP 401), a protected account logout task is retained. Login, Restore and subsequent Logout retry that task; they never restore it as a login. Process-restart recovery requires a persistent protected Store; MemoryStore lasts only for the current process. ReleaseDevice follows the same flow. Keep the official SDK cache and .logout.json journal intact; that SDK alone manages its device-release tickets.

Launch Restore confirms the account online and obtains a new identity assertion. It never selects offline permission during a Launch outage. Applications requiring offline startup use the official SDK recovery contract directly. The official SDK alone owns signed permission, device credentials, offline expiry and logout recovery.

Manual development is supported without AI. The combined source bundle includes docs/manual-integration.md with all nine language flows, secure storage, error handling and acceptance procedures. Local package/runtime checks are distinct from external payment and licensing acceptance. Check the current publication status at https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status.json before using public registry installation commands.


<!-- account-store-ownership-2026-10-24 -->
## Local account logout and session ownership (unpublished)

Account logout tasks use a separate key in the configured secure store. Custom stores must support multiple opaque keys, atomic value replacement and isolation to the current OS user and host. Account lifecycle operations are coordinated across local instances/processes, independently of the licensing cache directory.

Logout also records a local, session-specific account exit intent using hashes of the account scope and bearer; it stores no bearer or licensing grant there. Restore checks that intent before requesting identity, so a failed vault write or lifecycle-lock timeout cannot turn an interrupted logout into a remembered login. A storage failure is reported explicitly. If both vault persistence and local intent storage are unavailable, handle the failure and retry before terminating; the SDK does not claim durable completion.

An old instance only changes its own account record and logout task. Repeating a completed logout does not adopt a newer remembered account. Retrying other sessions' account tasks never opens their licensing runtime. Licentivo owns device revocation, signed leases, offline permission and its shared cache files; ordinary close still preserves remembered login.

## HTTP test opt-in on customer servers

The merchant address selects HTTP or HTTPS; failed HTTPS requests never downgrade to HTTP. For an HTTP customer test server, the customer must enable HTTP on the specific Licentivo API key and enable **Allow SDK HTTP (test environment only)** in their own Launch website/security settings. Use `sandbox` and the explicit client option C: `launch_application_connect_http(..., "sandbox", 1, 0, NULL)`; C++: `Application::ConnectHTTP(..., "sandbox")`. The generated test integration code includes it after successful checks. `live` requires HTTPS. Old SDK versions must be upgraded; server switches cannot update compiled client URLs. No access to Licentivo’s private network is needed.

See the [setup, failure and HTTPS migration guide in 14 languages](../../docs/http-testing/README.md) ([简体中文](../../docs/http-testing.zh-CN.md)). HTTP does not encrypt credentials; use dedicated test data.
