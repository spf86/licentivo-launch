# Launch Java 0.1.1

JDK 17+. Add Maven/Gradle dependency `com.licentivo.launch:launch-client:0.1.1` from Maven Central, or use only launch-client-0.1.1.jar from the matching GitHub release on your classpath. SDK 0.11.0 classes and notices are inside this JAR. No second SDK JAR or Maven dependency is required. Standard Java libraries provide the runtime dependencies.

## Direct initialization and manual lifecycle

Launch.connect(merchantOrigin, launchProductID, licentivoProductID, trustedKeys, environment) initializes public parameters directly. Launch provides the filled LaunchConnection.open function.

```java
try (var app = LaunchConnection.open()) {
    app.login(email, passwordChars);
    app.runFeature("export", () -> exportReport());
}
```

Use char[] for the password. restore() without a saved session fails with NO_SESSION; show login and do not treat other failures as authorization. logout() and releaseDevice() are distinct from close(). EncryptedFileStore uses AES-GCM with a caller-supplied OS-vault key. Advanced applications can use the constructor accepting a Store to remember sessions.

Launch automatically supplies the current merchant URL, existing product mapping and signing public keys when you select the software in its integration page. Copy its connection function, or use the values already shown there when writing code manually. Do not ask the integrator to type two internal product IDs. No launch-config.json or product.json file is required. The merchant URL is mandatory; the official licensing origin is fixed inside the package to https://www.licentivo.com.

The connection function initializes an application; it does not authorize business work. Login obtains only the signed-in customer's credential. Restore checks account expiry, identity, product and environment. Put every protected operation inside the feature guard. Do not substitute merchant order state or a locally saved boolean for the SDK's signed checks.

Normal close stops the runtime. Logout clears local account/cache state and follows this package's remote-session contract. Device release is a separate explicit operation, and failure must be reported. Sessions default to memory. Remembered login needs a protected store; never persist merchant passwords. Scope private SDK cache directories by origin, product, environment and account, and exclude them from installers and logs. Session encryption does not encrypt every official SDK device file.

Only transport failures or HTTP 5xx may attempt an existing cryptographically verified offline grant. Explicit 401/403/404, session/account mismatch, expiration or suspension deny access without offline fallback. Pending retries are finite and honor Retry-After; cancellation and rate limiting never authorize work. First activation and mandatory-online policies retain their network requirements.

Manual development is supported without AI. The combined source bundle includes docs/manual-integration.md with all nine language flows, secure storage, error handling and acceptance procedures. Local package/runtime checks are distinct from external payment and licensing acceptance. Check the current publication status at https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status.json before using public registry installation commands.
