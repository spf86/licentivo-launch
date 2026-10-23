# Launch Ruby 0.1.2

Ruby 3.1+. Install only the Launch gem matching the platform:

```sh
gem install licentivo-launch --version 0.1.2
```

SDK 0.11.1 Ruby code, its private native core and Windows curl DLL are included. RubyGems resolves the ordinary base64 dependency. The generic source gem builds its own embedded core automatically and needs normal C11/curl/OpenSSL tools for the platform. No official SDK gem or external native-library override is needed.

## Direct initialization and manual lifecycle

LicentivoLaunch::Application.connect(merchant_origin, launch_product_id, licentivo_product_id, trusted_keys, environment: 'live', **options) accepts public parameters directly. Launch provides the filled connect_launch function.

```ruby
app = connect_launch
begin
  app.login(email, password)
  result = app.run_feature('export') { export_report }
ensure
  app.close
end
```

restore, logout and release_device must handle errors. CancellationToken is cooperative; cancellation cannot become offline authorization. EncryptedFileStore uses a caller-supplied OS-vault key. It does not store that key beside the ciphertext.

Launch automatically supplies the current merchant URL, existing product mapping and signing public keys when you select the software in its integration page. Copy its connection function, or use the values already shown there when writing code manually. Do not ask the integrator to type two internal product IDs. No launch-config.json or product.json file is required. The merchant URL is mandatory; the official licensing origin is fixed inside the package to https://www.licentivo.com.

The connection function initializes an application; it does not authorize business work. Login obtains only the signed-in customer's credential. Restore checks account expiry, identity, product and environment. Put every protected operation inside the feature guard. Do not substitute merchant order state or a locally saved boolean for the SDK's signed checks.

Normal close stops the runtime. Logout clears local account/cache state and follows this package's remote-session contract. Device release is a separate explicit operation, and failure must be reported. Sessions default to memory. Remembered login needs a protected store; never persist merchant passwords. Scope private SDK cache directories by origin, product, environment and account, and exclude them from installers and logs. Session encryption does not encrypt every official SDK device file.

Only transport failures or HTTP 5xx may attempt an existing cryptographically verified offline grant. Explicit 401/403/404, session/account mismatch, expiration or suspension deny access without offline fallback. Pending retries are finite and honor Retry-After; cancellation and rate limiting never authorize work. First activation and mandatory-online policies retain their network requirements.

Manual development is supported without AI. The combined source bundle includes docs/manual-integration.md with all nine language flows, secure storage, error handling and acceptance procedures. Local package/runtime checks are distinct from external payment and licensing acceptance. Check the current publication status at https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-status.json before using public registry installation commands.
