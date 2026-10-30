# Launch Go 0.1.7

> Local account-exchange changes are unpublished. Login uses a short-lived identity assertion; Launch calculates purchased dates and Licentivo owns runtime permission. See the repository guide docs/account-purchase-boundary.md.

Go 1.26+. The public module is github.com/spf86/licentivo-launch-sdk; import github.com/spf86/licentivo-launch-sdk/pkg/launchclient. Install with go get github.com/spf86/licentivo-launch-sdk/pkg/launchclient@v0.1.7. A downloaded module ZIP can also be used with a local require/replace for the same module path. The official SDK 0.11.7 is embedded under Go's private internal boundary. No official SDK module, adapter copy or SDK import is needed.

## Direct initialization

Use launchclient.Connect(merchantOrigin, launchProductID, licentivoProductID, trustedKeys, launchclient.Config{Environment: environment}). Launch's integration page provides the fully filled connectLaunch function, so no ID entry or configuration file is needed. Empty/invalid merchant origins fail. HTTP loopback is permitted only for sandbox. Application runtime/cache defaults are handled internally.

## Manual lifecycle

```go
app, err := connectLaunch()
if err != nil { return err }
defer app.Close()
if _, err := app.Login(ctx, email, password); err != nil { return err }
return app.RunFeature("export", func() error { return exportReport() })
```

Restore(ctx) returns state/error; handle a missing session with login and treat explicit denial as failure. Logout(ctx) and ReleaseDevice(ctx) are explicit actions. Keep the application-lifetime context alive for SDK heartbeats. Nil Store is memory-only; WindowsDPAPIStore supports remembered login. Trusted backends can use Client and NewRuntime to isolate one runtime per authenticated account. RuntimeAPIError exposes safe official error metadata without another SDK import.

RunFeature invokes business actions without holding the Application lifecycle mutex. An action can call Close, Logout or another RunFeature. Work already in progress handles its own cancellation. Custom Runtime implementations must synchronize their runtime state when lifecycle operations overlap feature calls.

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

The merchant address selects HTTP or HTTPS; failed HTTPS requests never downgrade to HTTP. For an HTTP customer test server, the customer must enable HTTP on the specific Licentivo API key and enable **Allow SDK HTTP (test environment only)** in their own Launch website/security settings. Use `sandbox` and the explicit client option `launchclient.Config{Environment: "sandbox", AllowHTTP: true}`. The generated test integration code includes it after successful checks. `live` requires HTTPS. Old SDK versions must be upgraded; server switches cannot update compiled client URLs. No access to Licentivo’s private network is needed.

See the [setup, failure and HTTPS migration guide in 14 languages](../../docs/http-testing/README.md) ([简体中文](../../docs/http-testing.zh-CN.md)). HTTP does not encrypt credentials; use dedicated test data.
