# Merchant-owned licensing adapter example

This Python 3.10+ example implements Launch's external HTTP bridge with SQLite event/nonce persistence and a recovery worker. It does **not** generate licenses, validate protected features or replace your licensing system.

Implement a Python module with these hooks:

```python
def apply_snapshot(snapshot):
    # Call your licensing system with snapshot["event_id"] as its durable
    # idempotency key. Apply desired_state and the absolute purchased dates.
    # Return only after confirming the original operation was applied.
    # Raise ValueError for a result requiring manual review.
    ...

def delivery(snapshot):
    # Read an existing result owned by this purchase; do not issue here.
    return {"type": "instructions", "value": "Your own delivery instructions", "expires_at": None}
```

Replace the `...` with real integration before configuring a backend. If no backend module is configured, the bundled hooks raise `NotImplementedError`; events enter manual review and never claim an authorization was issued. The example worker can retry a hook after a crash or uncertain error, so your licensing backend must deduplicate/recover by the same event ID.

Configure the adapter using environment variables, keeping the key out of source control and shell history:

- `CUSTOM_LICENSING_KEY`: standard Base64 encoding of 32–128 random bytes, matching Launch.
- `CUSTOM_LICENSING_KEY_ID`: `key-1`, or your configured key identifier.
- `CUSTOM_LICENSING_CONNECTION_REF`: matching connection reference.
- `CUSTOM_LICENSING_ENVIRONMENT`: `sandbox` or `live`.
- `CUSTOM_LICENSING_DATABASE`: a private writable SQLite path outside your checkout; persist it across restarts.
- `CUSTOM_LICENSING_BACKEND`: importable module implementing the hooks above.
- `CUSTOM_LICENSING_PORT`: local listen port, default `18883`.

Run `python server.py`. For local sandbox testing, configure `http://127.0.0.1:18883` in Launch and explicitly enable its loopback HTTP exception. For production use HTTPS termination, process supervision, protected database backups and your backend's own SDK/runtime verification. Advertise only purchase/delivery types your backend actually supports; adapt `capabilities` accordingly.

After fixing a backend problem, `python server.py --retry-event ORIGINAL_EVENT_UUID` locally requeues the same stored payload. In Launch's authorization-task page, retry that original event too. Do not change IDs or add time again to recover a response.

The example is a starting point, not production qualification. Its worker lease is 60 seconds; bound hook calls to a shorter timeout or renew the lease. Add monitoring, rotation procedures, stricter business/schema checks and your deployment's operational controls before accepting live purchases. The [integration guide](../../docs/external-licensing.md) and [OpenAPI](../../docs/external-licensing-openapi.json) define the protocol. Never use a Launch status response as permission to execute protected code.
