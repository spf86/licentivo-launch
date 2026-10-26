import base64
import hashlib
import hmac
import json
from pathlib import Path
import secrets
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
from http.server import ThreadingHTTPServer

from server import Bridge, handler, PROTOCOL


class Backend:
    def __init__(self):
        self.applied = []

    def apply_snapshot(self, snapshot):
        self.applied.append(snapshot["event_id"])

    def delivery(self, snapshot):
        return {"type": "instructions", "value": "Fixture delivery", "expires_at": None}


class ReceiverTests(unittest.TestCase):
    def test_signature_persistence_idempotency_and_delivery(self):
        with tempfile.TemporaryDirectory() as directory:
            key, backend = bytes([67]) * 32, Backend()
            filename = str(Path(directory) / "events.sqlite3")
            bridge = Bridge(filename, key, "key-1", "merchant", "sandbox", backend)
            http = ThreadingHTTPServer(("127.0.0.1", 0), handler(bridge))
            listener = threading.Thread(target=http.serve_forever, daemon=True)
            worker = threading.Thread(target=bridge.work, daemon=True)
            listener.start()
            worker.start()
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

            def request(method, path, value=None, signing_key=key):
                raw = json.dumps(value, separators=(",", ":")).encode() if value is not None else b""
                stamp, nonce = str(int(time.time())), secrets.token_urlsafe(16)
                canonical = "\n".join(("v1", "key-1", stamp, nonce, method, path, hashlib.sha256(raw).hexdigest()))
                signature = "v1=" + base64.urlsafe_b64encode(hmac.digest(signing_key, canonical.encode(), "sha256")).decode().rstrip("=")
                headers = {"X-Launch-Key-Id": "key-1", "X-Launch-Timestamp": stamp, "X-Launch-Nonce": nonce, "X-Launch-Signature": signature, "Content-Type": "application/json"}
                req = urllib.request.Request(f"http://127.0.0.1:{http.server_port}{path}", raw if value is not None else None, headers, method=method)
                with opener.open(req, timeout=5) as response:
                    return response.status, json.load(response)["data"]

            try:
                self.assertEqual(request("GET", "/launch/v1/capabilities")[1]["protocol"], PROTOCOL)
                snapshot = dict(schema=PROTOCOL, event_id="7bf84b91-2749-4b01-a6a7-bd527c488ee8", connection_ref="merchant", environment="sandbox", type="entitlement.snapshot", change_reason="purchase", account_ref="account", entitlement_ref="purchase", launch_product_id="c6dd463e-50f7-4b76-8239-0a8ec6fc304b", launch_plan_version_id="5459a073-9362-4b46-949d-9b0c5d84421a", product_ref="editor", plan_ref="basic", version=1, desired_state="active", valid_from="2026-10-25T00:00:00Z", valid_until=None)
                self.assertEqual(request("POST", "/launch/v1/events", snapshot)[0], 202)
                deadline = time.monotonic() + 5
                while time.monotonic() < deadline:
                    receipt = request("GET", "/launch/v1/events/" + snapshot["event_id"])[1]
                    if receipt["status"] == "applied":
                        break
                    time.sleep(0.05)
                self.assertEqual(receipt["status"], "applied")
                self.assertEqual(request("POST", "/launch/v1/events", snapshot)[0], 200)
                self.assertEqual(len(backend.applied), 1)
                self.assertEqual(request("GET", "/launch/v1/entitlements/purchase/delivery")[1]["delivery"]["value"], "Fixture delivery")
                with self.assertRaises(urllib.error.HTTPError) as conflict:
                    request("POST", "/launch/v1/events", {**snapshot, "version": 2})
                self.assertEqual(conflict.exception.code, 409)
                with self.assertRaises(urllib.error.HTTPError) as forbidden:
                    request("GET", "/launch/v1/capabilities", signing_key=bytes([68]) * 32)
                self.assertEqual(forbidden.exception.code, 401)
                restored = Bridge(filename, key, "key-1", "merchant", "sandbox", backend)
                with restored.db() as db:
                    self.assertEqual(db.execute("SELECT COUNT(*) FROM events WHERE status='applied'").fetchone()[0], 1)
            finally:
                bridge.stopping.set()
                worker.join(timeout=5)
                http.shutdown()
                http.server_close()
                listener.join(timeout=5)


if __name__ == "__main__":
    unittest.main()
