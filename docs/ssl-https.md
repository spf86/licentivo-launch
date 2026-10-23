# SSL certificates and HTTP/HTTPS switching

Launch supports TLS termination at Caddy, Nginx, a load balancer, or a cloud platform, and an optional built-in HTTPS listener. Only certificates for the built-in listener are managed inside Launch. Manage a reverse proxy's certificates at the proxy.

## Enable built-in HTTPS

Set a listener different from the HTTP listener in `config.yaml`:

```yaml
http_addr: "0.0.0.0:8080"
https_addr: "0.0.0.0:8443"
public_url: "http://example.com:8080"
```

Listener addresses are startup configuration, so adding/changing a port requires a restart. Subsequent certificate replacement and public-protocol switching can be performed by an administrator without restarting.

## Administration workflow

1. Open **System settings > SSL certificates and access protocol**.
2. Upload a PEM certificate chain and matching private key. Put the leaf certificate first, followed by intermediates. The key must not require an interactive passphrase.
3. Launch validates key matching, certificate validity, and server-authentication usage. When HTTPS is already active it also validates the public hostname.
4. Select HTTPS and enter the complete public origin, for example `https://example.com:8443` for the listener above. The hostname is checked against the certificate again.
5. After saving, the page redirects to the new origin. HTTP redirects to HTTPS; generated links, session cookies, payment return URLs, and webhook URLs use the new public origin.

HTTP can be selected again from the same settings. It carries login and administration traffic in plaintext, so reserve it for local development or a controlled diagnostic network.

## Storage and operations

- The certificate and key are stored together as an encrypted MySQL secret. Read APIs expose certificate metadata, domains, and validity, not PEM contents.
- New TLS connections use a replacement certificate immediately; existing connections finish naturally.
- Removing or changing `LAUNCH_MASTER_KEY` makes saved secrets, including TLS keys, unreadable.
- Readiness/release checks show the public protocol, listener state, expiration, and certificates expiring within 30 days.
- When a proxy handles TLS, keep its key at the proxy and set `LAUNCH_PUBLIC_URL` to the actual public HTTPS origin.

The administration labels above are English descriptions of the current settings.
