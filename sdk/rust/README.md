# Launch Rust 0.1.1

Rust 1.85+. Install from crates.io with `cargo add licentivo-launch@=0.1.1`. A downloaded .crate or source ZIP can also be used as a local path dependency. SDK 0.11.0 Rust bindings and C11 sources are embedded. build.rs compiles and links the core automatically; no separate licentivo crate or SDK link path is needed.

Windows uses the GNU/MinGW Rust toolchain. The Windows distribution bundles curl and copies DLLs beside Cargo executables/tests. Other operating systems use normal C11, curl and OpenSSL development libraries. Preserve Cargo.lock. Ordinary Rust dependencies are resolved by Cargo.

## Direct initialization and manual lifecycle

Application::connect(merchant_origin, launch_product_id, licentivo_product_id, trusted_key_pairs, environment) selects the private cache directory and runtime internally. Launch supplies the filled connect_launch function.

```rust
let mut app = connect_launch()?;
app.login(&email, &password)?;
app.run_feature("export", || { export_report()?; Ok(()) })?;
app.close();
```

Protected callbacks return Result<T, Failure>. restore(), logout() and release_device() require explicit error handling. Drop also performs normal close. EncryptedFileStore requires an OS-vault key; advanced applications can configure it through with_components. It does not persist the encryption key itself.

Launch automatically supplies the current merchant URL, existing product mapping and signing public keys when you select the software in its integration page. Copy its connection function, or use the values already shown there when writing code manually. Do not ask the integrator to type two internal product IDs. No launch-config.json or product.json file is required. The merchant URL is mandatory; the official licensing origin is fixed inside the package to https://www.licentivo.com.

The connection function initializes an application; it does not authorize business work. Login obtains only the signed-in customer's credential. Restore checks account expiry, identity, product and environment. Put every protected operation inside the feature guard. Do not substitute merchant order state or a locally saved boolean for the SDK's signed checks.

Normal close stops the runtime. Logout clears local account/cache state and follows this package's remote-session contract. Device release is a separate explicit operation, and failure must be reported. Sessions default to memory. Remembered login needs a protected store; never persist merchant passwords. Scope private SDK cache directories by origin, product, environment and account, and exclude them from installers and logs. Session encryption does not encrypt every official SDK device file.

Only transport failures or HTTP 5xx may attempt an existing cryptographically verified offline grant. Explicit 401/403/404, session/account mismatch, expiration or suspension deny access without offline fallback. Pending retries are finite and honor Retry-After; cancellation and rate limiting never authorize work. First activation and mandatory-online policies retain their network requirements.

Manual development is supported without AI. The combined source bundle includes docs/manual-integration.md with all nine language flows, secure storage, error handling and acceptance procedures. Local package/runtime checks are distinct from external payment and licensing acceptance. Check the current publication status at https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status.json before using public registry installation commands.
