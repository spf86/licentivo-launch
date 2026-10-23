# Launch C# / .NET 0.1.2

.NET 8+. Install from NuGet.org with `dotnet add package Licentivo.Launch --version 0.1.2`. Downloaded packages can also use a local NuGet feed. Official SDK 0.11.1 source compiles inside the Launch assembly. NuGet resolves the ordinary BouncyCastle.Cryptography dependency; there is no Licentivo.SDK package/project dependency.

## Direct initialization and manual lifecycle

Licentivo.Launch.Application.Connect(merchantOrigin, launchProductID, licentivoProductID, trustedKeys, environment="live", store=null, cacheDirectory=null) manages all runtime setup. Launch supplies the filled LaunchConnection.Open function. The consumer does not construct an official SDK client.

```csharp
await using var app = LaunchConnection.Open();
await app.Login(email, password);
await app.RunFeature("export", async () => { await ExportReport(); });
```

Await Restore(), Logout() and ReleaseDevice(). DisposeAsync performs normal close. Keep asynchronous networking off the UI thread. WindowsDPAPIStore supports explicitly remembered login. A missing protected store defaults to memory; other platforms may supply their own ISessionStore.

Launch automatically supplies the current merchant URL, existing product mapping and signing public keys when you select the software in its integration page. Copy its connection function, or use the values already shown there when writing code manually. Do not ask the integrator to type two internal product IDs. No launch-config.json or product.json file is required. The merchant URL is mandatory; the official licensing origin is fixed inside the package to https://www.licentivo.com.

The connection function initializes an application; it does not authorize business work. Login obtains only the signed-in customer's credential. Restore checks account expiry, identity, product and environment. Put every protected operation inside the feature guard. Do not substitute merchant order state or a locally saved boolean for the SDK's signed checks.

Normal close stops the runtime. Logout clears local account/cache state and follows this package's remote-session contract. Device release is a separate explicit operation, and failure must be reported. Sessions default to memory. Remembered login needs a protected store; never persist merchant passwords. Scope private SDK cache directories by origin, product, environment and account, and exclude them from installers and logs. Session encryption does not encrypt every official SDK device file.

Only transport failures or HTTP 5xx may attempt an existing cryptographically verified offline grant. Explicit 401/403/404, session/account mismatch, expiration or suspension deny access without offline fallback. Pending retries are finite and honor Retry-After; cancellation and rate limiting never authorize work. First activation and mandatory-online policies retain their network requirements.

Manual development is supported without AI. The combined source bundle includes docs/manual-integration.md with all nine language flows, secure storage, error handling and acceptance procedures. Local package/runtime checks are distinct from external payment and licensing acceptance. Check the current publication status at https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status.json before using public registry installation commands.
