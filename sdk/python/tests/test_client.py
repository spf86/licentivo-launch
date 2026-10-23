import io
import json
import os
from pathlib import Path
import tempfile
import unittest
import urllib.error
from datetime import datetime, timedelta, timezone
from email.utils import format_datetime
from licentivo_launch import Application, APIError, MemoryStore, WindowsDPAPIStore

PRODUCT = "11111111-1111-4111-8111-111111111111"
CONFIG = {"product_id": "22222222-2222-4222-8222-222222222222", "trusted_keys": {"key": "public"}}


class Runtime:
    def __init__(self, config): self.config, self.started, self.closed, self.released = config, False, False, False
    def start(self): self.started = True
    def status(self): return {"allowed": Path(self.config["cache_path"]).exists()}
    def run_feature(self, feature, action):
        if feature != "export": raise ValueError("SDK denied feature")
        return action()
    def close(self): self.closed = True
    def deactivate(self): self.released = True


class Response(io.BytesIO):
    pass


class Merchant:
    def __init__(self, codes=()): self.requests, self.codes, self.mode, self.retry_after, self.status, self.credential = [], list(codes), "online", "3", None, None
    def open(self, request, timeout):
        self.requests.append(request)
        if self.mode == "offline": raise urllib.error.URLError("offline")
        if request.full_url.endswith("/me"):
            if self.mode == "denied": raise urllib.error.HTTPError(request.full_url, 401, "failure", {}, io.BytesIO(b'{"error":{"code":"SESSION_REJECTED"}}'))
            data = {"user": {"id": "alice"}}
        elif request.full_url.endswith("/auth/login"):
            data = {"access_token": "secret-bearer", "user": {"id": "alice"}, "expires_at": "2099-01-01T00:00:00Z"}
        elif request.full_url.endswith("/credential"):
            if self.codes:
                code = self.codes.pop(0)
                raise urllib.error.HTTPError(request.full_url, self.status or (404 if code == "ENTITLEMENT_NOT_PURCHASED" else 403), "failure", {"Retry-After": self.retry_after}, io.BytesIO(json.dumps({"error": {"code": code}}).encode()))
            data = self.credential if self.credential is not None else {"product_id": PRODUCT, "license_key": "lv_lic_" + "A" * 43, "version": 1, "valid_until": None}
        else: data = {}
        return Response(json.dumps({"data": data}).encode())


class LifecycleTests(unittest.TestCase):
    def setUp(self): self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
    def app(self, **kwargs): return Application("https://merchant.example", PRODUCT, CONFIG, cache_dir=self.temp.name, runtime_factory=Runtime, **kwargs)

    def test_restore_outage_needs_previously_verified_sdk_cache(self):
        merchant, store = Merchant(), MemoryStore()
        first = self.app(opener=merchant, store=store); first.login("a", "b"); path = first.cache_path; path.write_text("SDK fixture"); first.close()
        merchant.mode = "offline"; second = self.app(opener=merchant, store=store)
        self.assertTrue(second.restore()); self.assertEqual(path, second.cache_path)
        self.assertEqual("ran", second.run_feature("export", lambda: "ran"))
        with self.assertRaises(ValueError): second.run_feature("denied", lambda: self.fail("bypassed SDK"))
        second.logout(); self.assertIsNone(store.load(second.scope)); self.assertIsNone(second.runtime)

    def test_restore_outage_without_cache_cannot_activate(self):
        merchant, store = Merchant(), MemoryStore(); first = self.app(opener=merchant, store=store); first.login("a", "b"); first.close(); merchant.mode = "offline"
        second = self.app(opener=merchant, store=store)
        with self.assertRaises(APIError) as error: second.restore()
        self.assertEqual("OFFLINE_UNAVAILABLE", error.exception.code)
        with self.assertRaises(APIError): second.run_feature("export", lambda: self.fail("unauthorized"))

    def test_expiry_or_explicit_denial_purges_cached_account(self):
        for mode in ("denied", "expired", "ENTITLEMENT_EXPIRED", "ENTITLEMENT_SUSPENDED", "ENTITLEMENT_NOT_PURCHASED"):
            merchant, store = Merchant(), MemoryStore(); first = self.app(opener=merchant, store=store); first.login("a", "b"); path = first.cache_path; path.write_text("SDK fixture"); first.close()
            if mode == "expired":
                saved = json.loads(store.load(first.scope)); saved["expires_at"] = "2000-01-01T00:00:00Z"; store.save(first.scope, json.dumps(saved).encode())
            elif mode == "denied": merchant.mode = mode
            else: merchant.codes = [mode]
            second = self.app(opener=merchant, store=store)
            with self.assertRaises(APIError): second.restore()
            self.assertFalse(path.exists()); self.assertIsNone(store.load(first.scope))
            with self.assertRaises(APIError): second.run_feature("export", lambda: self.fail("denial bypassed"))

    def test_logout_without_restore_cleans_saved_cache(self):
        merchant, store = Merchant(), MemoryStore(); first = self.app(opener=merchant, store=store); first.login("a", "b"); path = first.cache_path; path.write_text("SDK fixture"); first.close()
        second = self.app(opener=merchant, store=store); second.logout()
        self.assertFalse(path.exists()); self.assertIsNone(store.load(first.scope))

    def test_pending_retry_and_terminal_error(self):
        sleeps, merchant = [], Merchant(["ENTITLEMENT_PENDING", "ENTITLEMENT_PENDING"])
        app = self.app(opener=merchant, sleep=sleeps.append); app.login("a", "b")
        self.assertEqual([3, 3], sleeps); app.close()
        for code in ("ENTITLEMENT_EXPIRED", "ENTITLEMENT_SUSPENDED", "ENTITLEMENT_NOT_PURCHASED", "ENTITLEMENT_PENDING"):
            merchant = Merchant([code] * 5); app = self.app(opener=merchant, sleep=lambda _: None)
            with self.assertRaises(APIError) as caught: app.login("a", "b", pending_attempts=2)
            self.assertEqual(code, caught.exception.code)
            self.assertNotIn("secret", str(caught.exception))
            calls = sum(r.full_url.endswith("/credential") for r in merchant.requests)
            self.assertEqual(2 if code == "ENTITLEMENT_PENDING" else 1, calls)
            self.assertIsNone(app.runtime)

    def test_scope_isolation_and_malformed_record(self):
        store = MemoryStore(); app = self.app(opener=Merchant(), store=store); app.login("a", "b"); app.close()
        alternate = Application("https://other.example", PRODUCT, CONFIG, store=store, cache_dir=self.temp.name, runtime_factory=Runtime)
        self.assertFalse(alternate.restore())
        store.save(alternate.scope, b'{"access_token":"sensitive"}')
        with self.assertRaises(APIError) as caught: alternate.restore()
        self.assertEqual("INVALID_SAVED_SESSION", caught.exception.code)

    def test_credential_contract_and_server_error_do_not_start_runtime(self):
        for update in ({"version": 0}, {"version": 1.5}, {"version": True}, {"version": None}, {"product_id": "wrong"}, {"license_key": ""}):
            merchant = Merchant(); merchant.credential = {"product_id": PRODUCT, "license_key": "key", "version": 1} | update
            app = self.app(opener=merchant)
            with self.assertRaises(APIError) as caught: app.login("a", "b")
            self.assertEqual("CREDENTIAL_MISMATCH", caught.exception.code); self.assertIsNone(app.runtime)
        merchant = Merchant(["ENTITLEMENT_PENDING"]); merchant.status = 503
        app = self.app(opener=merchant, sleep=lambda _: self.fail("5xx treated as business pending"))
        with self.assertRaises(APIError) as caught: app.login("a", "b")
        self.assertEqual(503, caught.exception.status); self.assertIsNone(app.runtime)

    def test_long_retry_after_never_retries_early(self):
        for value in ("120", format_datetime(datetime.now(timezone.utc) + timedelta(seconds=120), usegmt=True)):
            merchant = Merchant(["ENTITLEMENT_PENDING"]); merchant.retry_after = value
            app = self.app(opener=merchant, sleep=lambda _: self.fail("retried before server delay"))
            with self.assertRaises(APIError) as caught: app.login("a", "b")
            self.assertGreater(caught.exception.retry_after, 30)
            self.assertEqual(1, sum(r.full_url.endswith("/credential") for r in merchant.requests))

    def test_runtime_start_failure_never_authorizes(self):
        class Failed(Runtime):
            def start(self): raise ValueError("SDK rejected expired lease")
        app = Application("https://merchant.example", PRODUCT, CONFIG, opener=Merchant(), cache_dir=self.temp.name, runtime_factory=Failed)
        with self.assertRaises(ValueError): app.login("a", "b")
        with self.assertRaises(APIError): app.run_feature("export", lambda: self.fail("unauthorized"))

    def test_invalid_origin_and_environment(self):
        for origin, env in (("http://example.com", "sandbox"), ("https://u:p@example.com", "live"), ("https://example.com/path", "live"), ("https://example.com", "typo")):
            with self.assertRaises(ValueError): Application(origin, PRODUCT, CONFIG, env, runtime_factory=Runtime)

    @unittest.skipUnless(os.name == "nt", "Windows DPAPI")
    def test_dpapi_round_trip_and_tamper(self):
        store = WindowsDPAPIStore(self.temp.name); secret = b'{"access_token":"must-not-be-plaintext"}'
        store.save("key", secret); self.assertEqual(secret, store.load("key"))
        path = next(Path(self.temp.name).glob("*.dpapi")); self.assertNotIn(b"must-not-be-plaintext", path.read_bytes())
        path.write_bytes(b"corrupt")
        with self.assertRaises(OSError): store.load("key")
        store.delete("key"); self.assertIsNone(store.load("key"))


if __name__ == "__main__": unittest.main()
