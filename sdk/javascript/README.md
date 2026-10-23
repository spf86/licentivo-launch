# Launch Node.js 0.1.1

Node.js 20.3+. Install only Launch from npm:

```sh
npm install @licentivo/launch@0.1.1
```

Import from @licentivo/launch. Official SDK 0.11.0 modules, default adapter and TypeScript declarations are inside the package. There is no official SDK peer dependency.

## Direct initialization and manual lifecycle

Application.connect(merchantOrigin, launchProductID, licentivoProductID, trustedKeys, {environment, ...options}) returns the application. Launch provides the filled connectLaunch function.

```javascript
const app = connectLaunch();
try {
  await app.login(email, password);
  const result = await app.runFeature('export', () => exportReport());
} finally {
  await app.close();
}
```

restore() returns null without a saved session. logout() always attempts local cleanup; its boolean indicates whether remote logout was confirmed. releaseDevice() must report failure. Lifecycle calls are serialized, and protected callbacks cannot reenter them. EncryptedFileStore uses AES-GCM with an application-supplied 32-byte key obtained from the OS vault. Run this package in Node, an Electron main process or a trusted backend; do not expose credentials to browsers/renderers.

Launch automatically supplies the current merchant URL, existing product mapping and signing public keys when you select the software in its integration page. Copy its connection function, or use the values already shown there when writing code manually. Do not ask the integrator to type two internal product IDs. No launch-config.json or product.json file is required. The merchant URL is mandatory; the official licensing origin is fixed inside the package to https://www.licentivo.com.

The connection function initializes an application; it does not authorize business work. Login obtains only the signed-in customer's credential. Restore checks account expiry, identity, product and environment. Put every protected operation inside the feature guard. Do not substitute merchant order state or a locally saved boolean for the SDK's signed checks.

Normal close stops the runtime. Logout clears local account/cache state and follows this package's remote-session contract. Device release is a separate explicit operation, and failure must be reported. Sessions default to memory. Remembered login needs a protected store; never persist merchant passwords. Scope private SDK cache directories by origin, product, environment and account, and exclude them from installers and logs. Session encryption does not encrypt every official SDK device file.

Only transport failures or HTTP 5xx may attempt an existing cryptographically verified offline grant. Explicit 401/403/404, session/account mismatch, expiration or suspension deny access without offline fallback. Pending retries are finite and honor Retry-After; cancellation and rate limiting never authorize work. First activation and mandatory-online policies retain their network requirements.

Manual development is supported without AI. The combined source bundle includes docs/manual-integration.md with all nine language flows, secure storage, error handling and acceptance procedures. Local package/runtime checks are distinct from external payment and licensing acceptance. Check the current publication status at https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status.json before using public registry installation commands.
