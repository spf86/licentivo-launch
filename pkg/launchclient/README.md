# Launch Go 0.1.1

Go 1.26+. The public module is github.com/spf86/licentivo-launch-sdk; import github.com/spf86/licentivo-launch-sdk/pkg/launchclient. Install with go get github.com/spf86/licentivo-launch-sdk/pkg/launchclient@v0.1.1. A downloaded module ZIP can also be used with a local require/replace for the same module path. The official SDK 0.11.0 is embedded under Go's private internal boundary. No official SDK module, adapter copy or SDK import is needed.

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

Launch automatically supplies the current merchant URL, existing product mapping and signing public keys when you select the software in its integration page. Copy its connection function, or use the values already shown there when writing code manually. Do not ask the integrator to type two internal product IDs. No launch-config.json or product.json file is required. The merchant URL is mandatory; the official licensing origin is fixed inside the package to https://www.licentivo.com.

The connection function initializes an application; it does not authorize business work. Login obtains only the signed-in customer's credential. Restore checks account expiry, identity, product and environment. Put every protected operation inside the feature guard. Do not substitute merchant order state or a locally saved boolean for the SDK's signed checks.

Normal close stops the runtime. Logout clears local account/cache state and follows this package's remote-session contract. Device release is a separate explicit operation, and failure must be reported. Sessions default to memory. Remembered login needs a protected store; never persist merchant passwords. Scope private SDK cache directories by origin, product, environment and account, and exclude them from installers and logs. Session encryption does not encrypt every official SDK device file.

Only transport failures or HTTP 5xx may attempt an existing cryptographically verified offline grant. Explicit 401/403/404, session/account mismatch, expiration or suspension deny access without offline fallback. Pending retries are finite and honor Retry-After; cancellation and rate limiting never authorize work. First activation and mandatory-online policies retain their network requirements.

Manual development is supported without AI. The combined source bundle includes docs/manual-integration.md with all nine language flows, secure storage, error handling and acceptance procedures. Local package/runtime checks are distinct from external payment and licensing acceptance. Check the current publication status at https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status.json before using public registry installation commands.
