# Launch protected CSV exporter

A small real client for Launch 0.3.6-beta / Launch Python 0.1.8. It uses the
embedded official SDK, signed permission checks, real device registration, and
an actual CSV-writing callback. It does not create payments or alter entitlements.

Install with `python -m pip install ./sdk/python`. Save the public result of
`POST /api/v1/admin/integration/check` as `public.json`; it contains no password,
management key, identity assertion or device credential. Use a separate state
directory for each installation. Account sessions use Windows DPAPI or the
included owner-only, encrypted Linux example store; the SDK owns its runtime cache.

```sh
python examples/acceptance-exporter/main.py login --config public.json --state-dir ./demo-state --email customer@example.com
python examples/acceptance-exporter/main.py export --config public.json --state-dir ./demo-state --feature print --output protected-report.csv
python examples/acceptance-exporter/main.py logout --config public.json --state-dir ./demo-state
```

The password is prompted without echo; unattended acceptance may supply
`LAUNCH_DEMO_EMAIL` and `LAUNCH_DEMO_PASSWORD` in a private process environment.
Each invocation closes normally, preserving remembered login. `restore` in a
new process verifies it. Explicit `logout`/`release` require confirmed completion.
The demo deliberately refreshes online on each run; it is not an offline-startup UI.

For a metered feature add `--metered --operation-id YOUR_RETAINED_UUID` to
`export`. The official SDK reserves and commits usage around the real callback.
Reuse the UUID when recovering the same operation; never generate another UUID
just because a response was lost. The app prints only safe status and output hashes.

For the isolated self-hosted joint test, add
`--licentivo-origin http://127.0.0.1:18091` to each command on the server.
This explicitly selects that installation while retaining the real SDK's URL,
signature, account and feature checks. HTTP requires a sandbox configuration
whose `allow_http` is enabled. Leaving this option out uses the normal Licentivo
service. The workspace fixes are source builds; the public package version alone
does not identify whether those fixes are present.
