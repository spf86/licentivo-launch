# Launch C / C++ 0.1.2

C11/C++17 and CMake. Download the Windows amd64 GNU or Linux amd64 native installation ZIP from [GitHub Releases](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.2), verify SHA256SUMS, then add only that Launch prefix to CMake. SDK 0.11.1 core, adapters, import targets and notices are included. The Windows archive bundles curl. Other platforms can build the complete source using standard C11/curl/OpenSSL development libraries.

```cmake
find_package(LicentivoLaunch CONFIG REQUIRED)
add_executable(myapp main.c)
licentivo_launch_link(myapp)
# C++ target: licentivo_launch_link(myapp CXX)
```

The helper handles SDK/transitive linkage and bundled Windows runtime DLLs. No second find_package for an official SDK or copied adapter is required.

Version 0.1.2 embeds the SDK 0.11.1 CMake fix: installed SDK targets resolve curl through the consumer's public `CURL::libcurl` target, including when the SDK was built with curl's config package and the consumer uses FindCURL. Consumers do not need to add curl compatibility aliases. Match the compiler, architecture and curl runtime/development dependencies to the downloaded native package. The licensing protocol and client API are unchanged.

## Direct initialization

C uses launch_application_connect(origin, launchProductID, licentivoProductID, trustedKeysJSON, environment, remember, store). trustedKeysJSON is an in-memory object string. Launch supplies the filled connect_launch function; no JSON file is created or read. C++ uses Application::Connect with the same public values and supplies an owning connectLaunch function.

## Manual lifecycle

C: call launch_application_login, then launch_application_run_feature around every business callback. The callback returns 1 only on business success. Check launch_application_error on failures. launch_application_restore is three-state: 1 ready, 0 no session, -1 failed/denied. Check logout and release_device return values. launch_application_close frees the application; ln_free_string frees returned status text.

C++: call Login and RunFeature; handle exceptions. Restore returns false without a session and throws on a failed restoration. Logout and ReleaseDevice are explicit actions. RAII destruction performs normal close; callback exceptions propagate and cannot execute unlicensed work.

Windows remember=1 supports current-user DPAPI. Other platforms must supply secure store callbacks; no plaintext persistence fallback is provided. C++ follows the same native store semantics.

Launch automatically supplies the current merchant URL, existing product mapping and signing public keys when you select the software in its integration page. Copy its connection function, or use the values already shown there when writing code manually. Do not ask the integrator to type two internal product IDs. No launch-config.json or product.json file is required. The merchant URL is mandatory; the official licensing origin is fixed inside the package to https://www.licentivo.com.

The connection function initializes an application; it does not authorize business work. Login obtains only the signed-in customer's credential. Restore checks account expiry, identity, product and environment. Put every protected operation inside the feature guard. Do not substitute merchant order state or a locally saved boolean for the SDK's signed checks.

Normal close stops the runtime. Logout clears local account/cache state and follows this package's remote-session contract. Device release is a separate explicit operation, and failure must be reported. Sessions default to memory. Remembered login needs a protected store; never persist merchant passwords. Scope private SDK cache directories by origin, product, environment and account, and exclude them from installers and logs. Session encryption does not encrypt every official SDK device file.

Only transport failures or HTTP 5xx may attempt an existing cryptographically verified offline grant. Explicit 401/403/404, session/account mismatch, expiration or suspension deny access without offline fallback. Pending retries are finite and honor Retry-After; cancellation and rate limiting never authorize work. First activation and mandatory-online policies retain their network requirements.

Manual development is supported without AI. The combined source bundle includes docs/manual-integration.md with all nine language flows, secure storage, error handling and acceptance procedures. Local package/runtime checks are distinct from external payment and licensing acceptance. Check the current publication status at https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status.json before using public registry installation commands.
