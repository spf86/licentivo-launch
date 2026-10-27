# HTTP testing on customer servers and private networks

[Choose your language](http-testing/README.md)

All server addresses, IPs and hostnames in this guide belong to the customer's own Launch deployment. Customers do not join Licentivo's or the developer's private network. Both public test servers and the customer's private-network servers are supported.

The SDK uses the protocol in `merchant_origin`: `https://` uses HTTPS; `http://` is allowed only by the test permissions below. A failed HTTPS request never falls back to HTTP. A switch does not rewrite addresses or install a TLS certificate.

## Two independent switches, both off by default

| Switch | Where the customer changes it | Effect |
| --- | --- | --- |
| **Allow HTTP (testing)** for one API key | Licentivo → Development and integration → API Key → Create API Key or Access settings | Allows this specific key to create or update Launch account connections for HTTP merchant origins. Read-only keys do not gain write permission. Rotating a key preserves this setting. |
| **Allow SDK HTTP (test environment only)** | The customer's Launch → System settings → Website and security → SSL certificates and access protocol | Allows this Launch installation to accept HTTP SDK account requests and generate HTTP test code. HTTP runtime identity exchange is restricted to `sandbox`; `live` requires HTTPS. |

Customers control both switches themselves. No Licentivo platform administrator or platform-wide development mode is required. These settings affect only the customer's Launch address. Launch and the official licensing SDK still call Licentivo over HTTPS.

## Set up a customer test server

1. Update the Licentivo server, Launch server and SDK package. Older SDKs that only permit localhost HTTP must be upgraded; server settings cannot change an installed SDK's URL validation.
2. In the customer's Licentivo workspace, create a dedicated **read/write** test key or open an active key's **Access settings**, and select **Allow HTTP (testing)**.
3. In the customer's Launch **Website and security** settings, enter an address reachable by the test clients, such as `http://192.168.1.20:18080`, `http://test-server:18080` or the customer's test domain. Enable **Allow SDK HTTP (test environment only)** and save. Use an origin without a path, query, fragment or trailing slash.
4. Reconnect Launch to Licentivo with that key. Reading products and policies alone does not establish the account connection; also confirm that the account authorization connection is ready.
5. In **Application integration → Integration checks and code**, select **sandbox**, run the checks, and copy the newly generated code. After successful checks it automatically includes the HTTP test option. Use test accounts and test purchases.

A public IP or public DNS record is not required. Test clients must resolve and reach the configured Launch address. `localhost` means the machine running the SDK; a different machine must use the server's IP or internal hostname. Licentivo records the origin and pinned signing keys, without calling back or fetching keys from that origin. Launch and the online licensing client still need outbound access to Licentivo's HTTPS endpoints. An entirely disconnected private network cannot run online account licensing.

## How the SDK chooses HTTP or HTTPS

The generated public Launch configuration for an enabled customer-server test is equivalent to:

```json
{
  "schema_version": 1,
  "merchant_origin": "http://192.168.1.20:18080",
  "launch_product_id": "11111111-1111-4111-8111-111111111111",
  "licentivo_product_id": "22222222-2222-4222-8222-222222222222",
  "environment": "sandbox",
  "allow_http": true
}
```

The address selects the protocol. `allow_http: true` permits the HTTP merchant origin only in `sandbox`; it never downgrades an HTTPS URL. The official licensing product configuration remains HTTPS. Equivalent direct-call options for all nine languages are:

| SDK | Explicit HTTP test option |
| --- | --- |
| Go | `launchclient.Config{Environment: "sandbox", AllowHTTP: true}` |
| Python | `Application.connect(..., environment="sandbox", allow_http=True)` |
| JavaScript / TypeScript | `Application.connect(..., {environment: "sandbox", allowHTTP: true})` |
| C# | `Application.Connect(..., "sandbox", allowHttp: true)` |
| Java | `Launch.connect(..., "sandbox", true)` |
| Rust | `Application::connect_with_http(..., "sandbox", true)` |
| Ruby | `Application.connect(..., environment: 'sandbox', allow_http: true)` |
| C | `launch_application_connect_http(..., "sandbox", 1, 0, NULL)` |
| C++ | `Application::ConnectHTTP(..., "sandbox")` |

These options require an updated SDK build. The SDK never receives or queries the merchant's management API key. Launch exports public configuration and also checks its own HTTP switch on the server, so editing a client flag alone cannot enable HTTP access when that switch is off. Legacy constructors retain their existing sandbox localhost exception for compatibility; this does not bypass the server's default-off switch.

## Close testing or move to HTTPS

HTTP does not encrypt passwords, account tokens or other data. The customer's private network can still be monitored or compromised. Entering an API key into Launch administration over HTTP also exposes the browser-to-Launch request, even when Launch-to-Licentivo calls use HTTPS. Use dedicated test credentials; use HTTPS for production and sensitive administration.

After configuring a working HTTPS listener or TLS proxy, update Launch's effective merchant origin, reconnect Licentivo as needed, then regenerate and update the clients' integration configuration. A copied or compiled HTTP URL does not change automatically. Disable both HTTP switches when testing ends. `live` SDK identity exchange requires HTTPS regardless of either switch.

Closing Launch's switch rejects subsequent HTTP SDK requests with `SDK_HTTP_DISABLED`; HTTP identity requests in `live` return `SDK_HTTPS_REQUIRED`. Existing signed offline permissions still follow Licentivo's normal expiry and revocation rules. Closing the API key's HTTP permission blocks future HTTP connection creation or updates by that key; it does not delete existing connections, revoke purchases or invalidate issued permissions. Disable a connection separately if it must stop authorizing.

## Diagnose a failed connection

Launch shows the failing phase (reading products or policies, querying connections, or creating a sandbox/live connection), the specific reason, and Licentivo's error code and request ID when provided. Saved failures remain visible after a page refresh. Error messages never display the management API key.

| Error | Action |
| --- | --- |
| `LAUNCH_HTTPS_REQUIRED` | Enable HTTP for the exact API key used by Launch, or configure a working HTTPS merchant URL. |
| `LICENSE_KEY_INVALID` | Check the complete key, expiry, revocation, and the status of its owner and workspace. |
| `LICENSE_KEY_FORBIDDEN` | Use a valid Owner's read/write key. HTTP permission does not add write permission. |
| `SDK_HTTP_DISABLED` | Enable the SDK HTTP test switch in the customer's own Launch, or use HTTPS. |
| `SDK_HTTPS_REQUIRED` | Use HTTPS for `live`; HTTP tests must use `sandbox`. |
| `LICENTIVO_TIMEOUT` / `LICENTIVO_NETWORK_ERROR` | Check outbound networking, DNS, firewall and proxy settings on the customer's test machine. |
| `LICENTIVO_TLS_ERROR` | Check the system clock, trusted certificates and HTTPS proxy. Keep TLS verification enabled. |
