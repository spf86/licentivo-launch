"""Real official SDK integration against a local Ed25519 licensing fixture.

Missing official SDK/cryptography dependencies fail discovery; nothing is skipped.
No production licensing or payment service is contacted.
"""
import base64
from datetime import datetime, timedelta, timezone
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat
from licentivo_launch._vendor.licentivo import Client as OfficialClient
from licentivo_launch import Application, APIError, MemoryStore


PRODUCT = "11111111-1111-4111-8111-111111111111"
REMOTE = "22222222-2222-4222-8222-222222222222"
USER = "33333333-3333-4333-8333-333333333333"
ACTIVATION = "44444444-4444-4444-8444-444444444444"


def b64(raw):
    return base64.urlsafe_b64encode(raw).decode("ascii").rstrip("=")


class OfficialRuntimeTests(unittest.TestCase):
    def setUp(self):
        # All traffic must stay on this fixture even if a developer uses proxies.
        proxy_env = {name: "" for name in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY",
                                           "http_proxy", "https_proxy", "all_proxy")}
        proxy_env.update(NO_PROXY="localhost,127.0.0.1,::1", no_proxy="localhost,127.0.0.1,::1")
        proxies = patch.dict(os.environ, proxy_env)
        proxies.start()
        self.addCleanup(proxies.stop)
        self.temporary = tempfile.TemporaryDirectory(prefix="launch-python-runtime-")
        self.addCleanup(self.temporary.cleanup)
        self.private_key = Ed25519PrivateKey.generate()
        self.offline = threading.Event()
        self.requests = []
        self.apps = []
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def respond(self, status, envelope):
                payload = json.dumps(envelope).encode("utf-8")
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def handle_fixture(self):
                owner.requests.append((self.command, self.path))
                raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
                body = json.loads(raw) if raw else {}
                if owner.offline.is_set():
                    return self.respond(503, {"error": {"code": "SERVICE_UNAVAILABLE"}})
                if self.path == "/api/v1/auth/login":
                    return self.respond(200, {"data": {"access_token": "T" * 43,
                        "expires_at": (datetime.now(timezone.utc) + timedelta(hours=1)).isoformat(),
                        "user": {"id": USER}}})
                if self.path == "/api/v1/auth/logout":
                    return self.respond(200, {"data": {}})
                if self.path == "/api/v1/me":
                    return self.respond(200, {"data": {"user": {"id": USER}, "session_id": ACTIVATION}})
                if self.path == "/api/v1/software/products/" + PRODUCT + "/credential":
                    return self.respond(200, {"data": {"product_id": PRODUCT,
                        "license_key": "lv_lic_" + "L" * 43, "version": 1, "valid_until": None}})
                if self.path in ("/api/v1/activate", "/api/v1/validate", "/api/v1/heartbeat"):
                    now = int(time.time())
                    claims = {"version": 1, "key_id": "test", "tenant_id": USER,
                        "product_id": REMOTE, "license_id": USER, "device_id": body["device_id"],
                        "activation_id": ACTIVATION, "mode": "timed", "features": ["export"],
                        "max_devices": 2, "issued_at": now, "expires_at": now + 3600,
                        "runtime_policy": {"heartbeat_seconds": 0, "offline_mode": "limited", "offline_seconds": 3600}}
                    payload = json.dumps(claims, separators=(",", ":")).encode("utf-8")
                    lease = {"version": 1, "key_id": "test", "payload": b64(payload),
                        "signature": b64(owner.private_key.sign(b"LicenovaLease/v1\n" + payload))}
                    return self.respond(200, {"data": {"lease": lease, "activation_id": ACTIVATION,
                        "heartbeat_seconds": 0, "device_key": "lv_dev_" + "D" * 43}})
                return self.respond(404, {"error": {"code": "NOT_FOUND"}})

            do_GET = handle_fixture
            do_POST = handle_fixture

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.stop_server)
        # Close SDK refresh workers before shutting down the fixture/deleting cache.
        self.addCleanup(self.close_apps)
        self.origin = "http://127.0.0.1:" + str(self.server.server_port)
        self.config = {"product_id": REMOTE, "base_url": self.origin, "allow_http": True,
            "timeout_seconds": 1, "trusted_keys": {"test": b64(self.private_key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw))}}
        self.store = MemoryStore()

    def close_apps(self):
        for app in self.apps:
            app.close()

    def stop_server(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)

    def application(self):
        app = Application(self.origin, PRODUCT, self.config, "sandbox", store=self.store,
                          cache_dir=self.temporary.name)
        # No fake verifier or runtime injection is used in this suite.
        self.assertIs(app.runtime_factory, OfficialClient)
        self.apps.append(app)
        return app

    def test_official_signed_online_restart_offline_feature_denial_and_tamper(self):
        first = self.application()
        first.login("test@example.test", "test-only-password")
        self.assertEqual(first.runtime.status()["state"], "active")
        self.assertIn(("POST", "/api/v1/activate"), self.requests)
        self.assertEqual(first.run_feature("export", lambda: 42), 42)
        denied = []
        with self.assertRaises(RuntimeError):
            first.run_feature("not_entitled", lambda: denied.append(True))
        self.assertEqual(denied, [])
        cache_path = Path(first.cache_path)
        self.assertTrue(cache_path.is_file())
        first.close()
        self.assertIsNotNone(self.store.load(first.scope))

        # A fresh wrapper and real SDK instance must load and verify the disk lease.
        self.offline.set()
        restored = self.application()
        self.assertTrue(restored.restore())
        self.assertEqual(restored.runtime.status()["state"], "offline_valid")
        self.assertEqual(restored.cache_path, cache_path)
        self.assertEqual(restored.run_feature("export", lambda: 43), 43)
        with self.assertRaises(RuntimeError):
            restored.run_feature("not_entitled", lambda: denied.append(True))
        self.assertEqual(denied, [])
        restored.close()

        cache = json.loads(cache_path.read_text(encoding="utf-8"))
        signature = bytearray(base64.urlsafe_b64decode(cache["lease"]["signature"] + "=="))
        signature[0] ^= 1
        cache["lease"]["signature"] = b64(signature)
        cache_path.write_text(json.dumps(cache), encoding="utf-8")
        tampered = self.application()
        with self.assertRaises(APIError) as error:
            tampered.restore()
        self.assertEqual(error.exception.code, "OFFLINE_UNAVAILABLE")
        self.assertIsNone(tampered.runtime)
        with self.assertRaises(APIError) as error:
            tampered.run_feature("export", lambda: denied.append(True))
        self.assertEqual(error.exception.code, "LOGIN_REQUIRED")
        self.assertEqual(denied, [])


if __name__ == "__main__":
    unittest.main()
