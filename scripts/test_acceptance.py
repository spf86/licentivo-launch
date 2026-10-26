"""Safety and evidence tests for the external probe; no real external calls."""
import base64
from datetime import datetime, timezone, timedelta
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace

import acceptance


class AcceptanceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.config = {"schema_version": 1, "merchant_origin": "https://shop.example", "environment": "sandbox",
                       "launch_product_id": "11111111-1111-4111-8111-111111111111",
                       "licentivo_product_id": "22222222-2222-4222-8222-222222222222"}

    def save(self, value):
        path = self.root / "config.json"
        path.write_text(json.dumps(value), encoding="utf-8")
        return path

    def test_sandbox_only_and_no_credential_redirect(self):
        for change in ({"environment": "live"}, {"merchant_origin": "http://public.example"},
                       {"merchant_origin": "https://user:password@shop.example"},
                       {"merchant_origin": "https://shop.example/path"}):
            with self.subTest(change=change), self.assertRaises(acceptance.ProbeError):
                acceptance.configuration(self.save(self.config | change))
        self.assertIsNone(acceptance.NoRedirect().redirect_request(None, None, 307, "", {}, "https://elsewhere.example"))

    def test_public_config_refuses_secrets_and_wrong_product(self):
        public = {"product_id": self.config["licentivo_product_id"],
                  "trusted_keys": {"test-key": base64.urlsafe_b64encode(bytes(range(32))).decode().rstrip("=")}}
        self.assertEqual(acceptance.public_product(self.save(public), public["product_id"])["timeout_seconds"], 15)
        for change in ({"license_key": "never-export"}, {"cache_path": "secret"}, {"base_url": "https://elsewhere.example"},
                       {"product_id": self.config["launch_product_id"]}, {"trusted_keys": {"kid": "bad"}}):
            with self.subTest(change=change), self.assertRaises(acceptance.ProbeError):
                acceptance.public_product(self.save(public | change), public["product_id"])

    def test_offline_server_report_is_failed_and_redacted(self):
        report_path = self.root / "report.json"
        with patch.object(acceptance.Merchant, "call", side_effect=acceptance.ProbeError("MERCHANT_UNREACHABLE")), patch("sys.stdout", new_callable=io.StringIO):
            result = acceptance.main(["preflight", "--config", str(self.save(self.config)), "--report", str(report_path)])
        report = json.loads(report_path.read_text())
        self.assertEqual(result, 1)
        self.assertEqual(report["result"], "failed")
        self.assertFalse(report["acceptance_complete"])
        self.assertEqual(report["checks"][-1]["code"], "MERCHANT_UNREACHABLE")
        self.assertNotIn("password", report_path.read_text())

    def test_provider_error_cannot_leak_secret(self):
        error = acceptance.ProbeError("license_key=secret and raw provider response")
        self.assertEqual(str(error), "REQUEST_FAILED")

    def test_config_schema_and_unexpected_fields_rejected(self):
        for change in ({"schema_version": 2}, {"schema_version": True}, {"management_key": "never-export"}):
            with self.subTest(change=change), self.assertRaises(acceptance.ProbeError):
                acceptance.configuration(self.save(self.config | change))

    def test_incompatible_actions_are_rejected_before_network(self):
        config = str(self.save(self.config))
        cases = [
            ["preflight", "--activate"],
            ["checkout", "--activate"],
            ["preflight", "--order-id", self.config["launch_product_id"]],
            ["verify", "--provider", "stripe"],
            ["checkout"],
            ["verify", "--activate"],
            ["verify", "--expect-order", "paid"],
        ]
        for flags in cases:
            with self.subTest(flags=flags), patch.object(acceptance.Merchant, "call") as call, patch("sys.stderr", new_callable=io.StringIO):
                with self.assertRaises(SystemExit) as error:
                    acceptance.main(flags + ["--config", config, "--report", str(self.root / "unused.json")])
                self.assertEqual(error.exception.code, 2)
                call.assert_not_called()

    def test_negative_purchase_state_is_not_runtime_denial_evidence(self):
        merchant = acceptance.Merchant("https://shop.example")
        args = SimpleNamespace(expect="expired", wait_seconds=0, order_id=None, activate=False)
        status = {"product_id": self.config["launch_product_id"], "environment": "sandbox", "status": "expired", "code": "ENTITLEMENT_EXPIRED", "next_action": "renew"}
        report = {"checks": []}
        with patch.object(merchant, "call", return_value=status) as call:
            acceptance.verify(merchant, self.config, args, report, {"id": "user"})
        self.assertEqual(call.call_count, 1)
        self.assertEqual(report["licensing_scope"], "not_called")
        self.assertEqual(report["checks"][-1]["name"], "purchase_state_only")

    def test_unknown_or_inconsistent_status_never_passes(self):
        merchant = acceptance.Merchant("https://shop.example")
        args = type("Args", (), {"expect": "active", "wait_seconds": 0, "order_id": None, "activate": False})()
        base = {"product_id": self.config["launch_product_id"], "environment": "sandbox", "status": "ready", "code": "ENTITLEMENT_READY", "next_action": "activate", "synchronized": True}
        for change in ({"status": "unknown"}, {"code": "ENTITLEMENT_EXPIRED"}, {"synchronized": False}, {"next_action": "renew"}):
            with self.subTest(change=change), patch.object(merchant, "call", return_value=base | change):
                with self.assertRaisesRegex(acceptance.ProbeError, "ENTITLEMENT_STATUS_MISMATCH"):
                    acceptance.verify(merchant, self.config, args, {"checks": []}, {"id": "user"})

    def test_server_failure_is_not_negative_purchase_evidence(self):
        merchant = acceptance.Merchant("https://shop.example")
        args = SimpleNamespace(expect="expired", wait_seconds=0, order_id=None, activate=False)
        with patch.object(merchant, "call", side_effect=acceptance.ProbeError("ENTITLEMENT_EXPIRED", 500)):
            with self.assertRaises(acceptance.ProbeError):
                acceptance.verify(merchant, self.config, args, {"checks": []}, {"id": "user"})

    def test_activation_requires_synchronous_online_confirmation(self):
        state = {"product_id": self.config["launch_product_id"], "environment": "sandbox", "status": "ready", "code": "ENTITLEMENT_READY", "next_action": "activate", "synchronized": True}
        identity = self.identity()
        args = SimpleNamespace(expect="active", wait_seconds=0, order_id=None, activate=True, product_config="unused", cache_dir=self.root, feature="export")
        for online in (False, True):
            events = []
            class Client:
                def __init__(self, config): pass
                def __enter__(self): return self
                def __exit__(self, *args): pass
                def start(self): events.append("start")
                def logout(self):
                    events.append("logout")
                    return {"local_complete": True, "revocation_pending": False}
                def refresh_online(self): events.append("refresh")
                def status(self): return {"allowed": True, "state": "active" if online else "offline_valid"}
                def run_feature(self, feature, callback):
                    events.append("feature")
                    callback()
            merchant = acceptance.Merchant("https://shop.example")
            report = {"checks": []}
            with patch.object(merchant, "call", side_effect=[state, identity, state]), patch.object(acceptance, "public_product", return_value={}), patch.object(acceptance.importlib, "import_module", return_value=SimpleNamespace(Client=Client)):
                if online:
                    local_config = self.config | {"merchant_origin": "http://127.0.0.1:18091"}
                    acceptance.verify(merchant, local_config, args, report, {"id": "user"})
                    self.assertEqual(events, ["start", "refresh", "feature", "logout"])
                    self.assertEqual(report["checks"][-2]["name"], "official_sdk_online_confirmation")
                    self.assertEqual(report["scope"], "local_merchant_external_licensing_probe")
                    self.assertEqual(report["licensing_scope"], "official_external_service")
                else:
                    with self.assertRaisesRegex(acceptance.ProbeError, "SDK_ONLINE_CONFIRMATION_REQUIRED"):
                        acceptance.verify(merchant, self.config, args, report, {"id": "user"})
                    self.assertEqual(events, ["start", "refresh", "logout"])
            self.assertNotIn("test-secret", json.dumps(report))

    def identity(self):
        return {"product_id": self.config["launch_product_id"], "licentivo_product_id": self.config["licentivo_product_id"],
                "launch_connection_id": "33333333-3333-4333-8333-333333333333", "environment": "sandbox",
                "identity_token": "test-secret-assertion" * 3,
                "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=120)).isoformat()}

    def test_pending_purchase_polling_is_bounded_and_assertion_is_redacted(self):
        state = {"product_id": self.config["launch_product_id"], "environment": "sandbox", "status": "pending", "code": "ENTITLEMENT_PENDING", "next_action": "retry"}
        args = SimpleNamespace(expect="active", wait_seconds=120, order_id=None, activate=False)
        merchant = acceptance.Merchant("https://shop.example")
        with patch.object(merchant, "call", return_value=state) as call, patch.object(acceptance.time, "sleep") as sleep:
            with self.assertRaisesRegex(acceptance.ProbeError, "ENTITLEMENT_PENDING"):
                acceptance.verify(merchant, self.config, args, {"checks": []}, {"id": "user"})
            self.assertEqual(call.call_count, 5)
            self.assertEqual(sleep.call_count, 4)
        state.update(status="ready", code="ENTITLEMENT_READY", next_action="activate", synchronized=True)
        for change in ({"environment": "live"}, {"launch_connection_id": "secret-response"}, {"identity_token": ""}, {"expires_at": "2000-01-01T00:00:00Z"}):
            report = {"checks": []}
            with patch.object(merchant, "call", side_effect=[state, self.identity() | change]):
                with self.assertRaisesRegex(acceptance.ProbeError, "IDENTITY_CONTEXT_MISMATCH"):
                    acceptance.verify(merchant, self.config, args, report, {"id": "user"})
            self.assertNotIn("test-secret", json.dumps(report))


if __name__ == "__main__":
    unittest.main()
