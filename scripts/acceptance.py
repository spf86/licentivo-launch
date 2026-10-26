"""Sandbox acceptance probes. Never substitutes a mock for an external result.

Run with --help. Passwords are read from a prompt or LAUNCH_ACCEPTANCE_PASSWORD.
Reports contain only allowlisted evidence, never tokens, license keys or raw errors.
"""
import argparse
import base64
from datetime import datetime, timezone
import getpass
import hashlib
import importlib
import json
import os
from pathlib import Path
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

LIMIT = 1 << 20
EXPECTED_CODES = {
    "not_purchased": "ENTITLEMENT_NOT_PURCHASED",
    "pending": "ENTITLEMENT_PENDING",
    "expired": "ENTITLEMENT_EXPIRED",
    "suspended": "ENTITLEMENT_SUSPENDED",
    "verification_required": "EMAIL_VERIFICATION_REQUIRED",
}
EXPECTED_ACTIONS = {"active": "activate", "not_purchased": "purchase", "pending": "retry",
                    "expired": "renew", "suspended": "contact_support"}


class ProbeError(Exception):
    def __init__(self, code, status=0, retry_after=0):
        # Do not echo arbitrary provider/HTTP responses into the evidence file.
        self.code = code if re.fullmatch(r"[A-Z][A-Z0-9_]{0,79}", str(code)) else "REQUEST_FAILED"
        self.status = status
        self.retry_after = retry_after
        super().__init__(self.code)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def identifier(value):
    try:
        return str(uuid.UUID(value))
    except (ValueError, TypeError, AttributeError):
        raise ProbeError("INVALID_IDENTIFIER") from None


def read_json(path):
    with Path(path).open("rb") as stream:
        raw = stream.read(LIMIT + 1)
    if len(raw) > LIMIT:
        raise ProbeError("CONFIG_TOO_LARGE")
    try:
        value = json.loads(raw)
    except (ValueError, UnicodeError):
        raise ProbeError("INVALID_CONFIG") from None
    if not isinstance(value, dict):
        raise ProbeError("INVALID_CONFIG")
    return value


def configuration(path):
    config = read_json(path)
    fields = {"schema_version", "merchant_origin", "environment", "launch_product_id", "licentivo_product_id"}
    if set(config) != fields or type(config.get("schema_version")) is not int or config["schema_version"] != 1:
        raise ProbeError("UNSUPPORTED_CONFIG_SCHEMA")
    if config.get("environment") != "sandbox":
        raise ProbeError("SANDBOX_REQUIRED")
    origin = str(config.get("merchant_origin", "")).rstrip("/")
    parsed = urllib.parse.urlsplit(origin)
    if (not parsed.hostname or parsed.username or parsed.password or parsed.path
            or parsed.query or parsed.fragment or parsed.scheme not in ("https", "http")):
        raise ProbeError("INVALID_MERCHANT_ORIGIN")
    if parsed.scheme == "http" and parsed.hostname not in ("localhost", "127.0.0.1", "::1"):
        raise ProbeError("HTTPS_REQUIRED")
    return {"merchant_origin": origin, "environment": "sandbox",
            "launch_product_id": identifier(config.get("launch_product_id")),
            "licentivo_product_id": identifier(config.get("licentivo_product_id"))}


def public_product(path, remote_id):
    config = read_json(path)
    allowed = {"product_id", "trusted_keys", "base_url", "app_version"}
    if set(config) - allowed:
        raise ProbeError("PUBLIC_CONFIG_HAS_UNSUPPORTED_FIELDS")
    if identifier(config.get("product_id")) != remote_id:
        raise ProbeError("PRODUCT_MAPPING_MISMATCH")
    if config.get("base_url", "https://www.licentivo.com").rstrip("/") != "https://www.licentivo.com":
        raise ProbeError("UNEXPECTED_LICENSING_ORIGIN")
    keys = config.get("trusted_keys")
    if not isinstance(keys, dict) or not keys or len(keys) > 32:
        raise ProbeError("INVALID_PUBLIC_KEYS")
    for kid, value in keys.items():
        if (not isinstance(kid, str) or not kid or not isinstance(value, str)
                or not re.fullmatch(r"[A-Za-z0-9_-]{43}", value)):
            raise ProbeError("INVALID_PUBLIC_KEYS")
        try:
            decoded = base64.urlsafe_b64decode(value + "=")
        except ValueError:
            raise ProbeError("INVALID_PUBLIC_KEYS") from None
        if len(decoded) != 32 or base64.urlsafe_b64encode(decoded).decode().rstrip("=") != value:
            raise ProbeError("INVALID_PUBLIC_KEYS")
    config["timeout_seconds"] = 15
    return config


class Merchant:
    def __init__(self, origin):
        self.origin = origin
        self.token = ""
        self.http = urllib.request.build_opener(NoRedirect())

    def call(self, method, path, body=None):
        headers = {"Accept": "application/json", "Content-Type": "application/json"}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        request = urllib.request.Request(self.origin + path, None if body is None else json.dumps(body).encode(), headers, method=method)
        try:
            response = self.http.open(request, timeout=15)
        except urllib.error.HTTPError as error:
            response = error
        except (OSError, urllib.error.URLError):
            raise ProbeError("MERCHANT_UNREACHABLE") from None
        with response:
            raw = response.read(LIMIT + 1)
            if len(raw) > LIMIT:
                raise ProbeError("RESPONSE_TOO_LARGE")
            try:
                envelope = json.loads(raw)
            except (ValueError, UnicodeError):
                raise ProbeError("INVALID_RESPONSE", response.code) from None
            if not isinstance(envelope, dict):
                raise ProbeError("INVALID_RESPONSE", response.code)
            if response.code != 200:
                retry = response.headers.get("Retry-After", "0")
                error = envelope.get("error", {})
                code = error.get("code", "REQUEST_FAILED") if isinstance(error, dict) else "REQUEST_FAILED"
                raise ProbeError(code, response.code, int(retry) if retry.isdigit() else 0)
            return envelope.get("data")


def record(report, name, result="passed", **evidence):
    report["checks"].append({"name": name, "result": result, **evidence})


def preflight(merchant, config, report):
    ready = merchant.call("GET", "/readyz")
    if not isinstance(ready, dict) or ready.get("status") != "ready":
        raise ProbeError("DATABASE_NOT_READY")
    record(report, "database_ready")
    site = merchant.call("GET", "/api/v1/site?environment=sandbox")
    if not isinstance(site, dict):
        raise ProbeError("INVALID_SITE_RESPONSE")
    # A catalog or HTTP health check never establishes payment/issuance success.
    providers = [p for p in site.get("payment_providers", []) if p in ("stripe", "paypal", "waffo", "paddle")]
    record(report, "sandbox_catalog", providers=providers)


def verify(merchant, config, args, report, user):
    product = config["launch_product_id"]
    prefix = "/api/v1/software/products/" + product
    def observe(allow_pending=False):
        try:
            state = merchant.call("GET", prefix + "/status?environment=sandbox")
        except ProbeError as error:
            if args.expect != "verification_required" or error.code != "EMAIL_VERIFICATION_REQUIRED" or error.status != 403:
                raise
            record(report, "entitlement_status_denied", code=error.code, http_status=403)
            return
        if not isinstance(state, dict) or state.get("product_id") != product or state.get("environment") != "sandbox":
            raise ProbeError("STATUS_CONTEXT_MISMATCH")
        expectation = "pending" if allow_pending and args.expect == "active" and state.get("status") == "pending" else args.expect
        if (state.get("status") != ("ready" if expectation == "active" else expectation)
                or state.get("code") != ("ENTITLEMENT_READY" if expectation == "active" else EXPECTED_CODES.get(expectation))
                or state.get("next_action") != EXPECTED_ACTIONS.get(expectation)
                or expectation == "active" and state.get("synchronized") is not True):
            raise ProbeError("ENTITLEMENT_STATUS_MISMATCH")
        record(report, "entitlement_status", observed=state["status"], code=state["code"])
        return state

    state = observe(allow_pending=True)
    identity = None
    report.setdefault("licensing_scope", "not_called")
    if args.expect == "active":
        deadline = time.monotonic() + args.wait_seconds
        attempts = 1
        while state and state.get("status") == "pending":
            delay = state.get("retry_after_seconds", 3)
            if type(delay) is not int or delay < 0:
                raise ProbeError("INVALID_RETRY_AFTER")
            delay = max(3, delay)
            if attempts >= 5 or time.monotonic() + delay >= deadline:
                raise ProbeError("ENTITLEMENT_PENDING", 403)
            time.sleep(delay)
            attempts += 1
            state = observe(allow_pending=True)
        identity = merchant.call("POST", prefix + "/identity", {"environment": "sandbox"})
        if (not isinstance(identity, dict) or identity.get("product_id") != product
                or identity.get("licentivo_product_id") != config["licentivo_product_id"]
                or identity.get("environment") != "sandbox"
                or not isinstance(identity.get("identity_token"), str)
                or not 32 <= len(identity["identity_token"]) <= 12288):
            raise ProbeError("IDENTITY_CONTEXT_MISMATCH")
        try:
            identifier(identity.get("launch_connection_id", ""))
            expires = datetime.fromisoformat(identity.get("expires_at", "").replace("Z", "+00:00"))
            remaining = (expires - datetime.now(timezone.utc)).total_seconds()
            if not 0 < remaining <= 150:
                raise ValueError()
        except (ValueError, TypeError, ProbeError, AttributeError):
            raise ProbeError("IDENTITY_CONTEXT_MISMATCH") from None
        record(report, "identity_assertion_received")
        observe()
    else:
        # A negative Launch purchase state is not evidence of runtime denial.
        # Identity assertions only prove login and may legitimately still exist.
        record(report, "purchase_state_only", observed=args.expect)
    if args.order_id:
        order = merchant.call("GET", "/api/v1/orders/" + identifier(args.order_id))
        if not isinstance(order, dict) or order.get("product_id") != product or order.get("environment") != "sandbox":
            raise ProbeError("ORDER_CONTEXT_MISMATCH")
        if args.expect_order and order.get("status") != args.expect_order:
            raise ProbeError("ORDER_STATE_MISMATCH")
        if (order.get("status") not in ("paid", "refunded", "pending", "refunding", "closed")
                or order.get("fulfillment") not in ("not_paid", "pending_authorization", "active", "manual_review", "refunded")):
            raise ProbeError("INVALID_ORDER_STATE")
        record(report, "order", order_id=identifier(args.order_id), status=order.get("status"), fulfillment=order.get("fulfillment"))
    if args.activate:
        if args.expect != "active" or identity is None or not args.product_config:
            raise ProbeError("ACTIVATION_REQUIRES_ACTIVE_PRODUCT_CONFIG")
        sdk_config = public_product(args.product_config, config["licentivo_product_id"])
        try:
            sdk = importlib.import_module("licentivo_launch._vendor.licentivo")
        except ImportError:
            raise ProbeError("INSTALL_LAUNCH_PYTHON_PACKAGE") from None
        scope = hashlib.sha256("\n".join((config["merchant_origin"], user["id"], product, "sandbox")).encode()).hexdigest()
        cache = Path(args.cache_dir).resolve() / scope
        cache.mkdir(parents=True, exist_ok=True, mode=0o700)
        sdk_config.update(license_key="", identity_token=identity["identity_token"],
                          launch_connection_id=identity["launch_connection_id"], environment="sandbox",
                          cache_path=str(cache / "lease.json"))
        performed = []
        report["licensing_scope"] = "official_external_service"
        if urllib.parse.urlsplit(config["merchant_origin"]).hostname in ("localhost", "127.0.0.1", "::1"):
            report["scope"] = "local_merchant_external_licensing_probe"
        try:
            with sdk.Client(sdk_config) as client:
                try:
                    client.start()
                    # start() may legally use an old offline grant. This probe must
                    # prove an online round trip before claiming activation evidence.
                    client.refresh_online()
                    online = client.status()
                    if not isinstance(online, dict) or online.get("allowed") is not True or online.get("state") != "active":
                        raise ProbeError("SDK_ONLINE_CONFIRMATION_REQUIRED")
                    record(report, "official_sdk_online_confirmation")
                    client.run_feature(args.feature, lambda: performed.append(True))
                finally:
                    result = client.logout()
                    if not isinstance(result, dict) or result.get("local_complete") is not True or result.get("storage_error"):
                        raise ProbeError("SDK_LOGOUT_STORAGE_FAILED")
                    if result.get("revocation_pending"):
                        raise ProbeError("SDK_LOGOUT_PENDING")
                    report["sdk_logout_confirmed"] = True

        except Exception as error:
            raise ProbeError(getattr(error, "code", "SDK_AUTHORIZATION_FAILED")) from None
        if performed != [True]:
            raise ProbeError("PROTECTED_OPERATION_NOT_EXECUTED")
        record(report, "official_sdk_protected_operation", feature=args.feature)


def run(args, report):
    config = configuration(args.config)
    report.update(environment="sandbox", merchant_origin=config["merchant_origin"], product_id=config["launch_product_id"], licensing_scope="not_called")
    if urllib.parse.urlsplit(config["merchant_origin"]).hostname in ("localhost", "127.0.0.1", "::1"):
        report["scope"] = "local_sandbox_probe"
    merchant = Merchant(config["merchant_origin"])
    preflight(merchant, config, report)
    if args.command == "preflight":
        return
    email = os.environ.get("LAUNCH_ACCEPTANCE_EMAIL") or input("Sandbox customer email: ").strip()
    password = os.environ.get("LAUNCH_ACCEPTANCE_PASSWORD") or getpass.getpass("Sandbox customer password: ")
    grant = merchant.call("POST", "/api/v1/auth/login", {"email": email, "password": password, "kind": "desktop"})
    password = ""
    if not isinstance(grant, dict) or not grant.get("access_token") or not isinstance(grant.get("user"), dict) or not grant["user"].get("id"):
        raise ProbeError("INVALID_SESSION")
    merchant.token = grant["access_token"]
    try:
        record(report, "customer_login")
        if args.command == "checkout":
            if not args.plan_version_id or not args.provider or not args.operation_id:
                raise ProbeError("CHECKOUT_ARGUMENTS_REQUIRED")
            operation = identifier(args.operation_id)
            order = merchant.call("POST", "/api/v1/orders", {"product_id": config["launch_product_id"],
                "plan_version_id": identifier(args.plan_version_id), "environment": "sandbox", "provider": args.provider,
                "purchase_periods": 1, "idempotency_key": "acceptance-" + operation})
            if not isinstance(order, dict) or order.get("environment") != "sandbox" or order.get("product_id") != config["launch_product_id"]:
                raise ProbeError("ORDER_CONTEXT_MISMATCH")
            record(report, "sandbox_checkout_created", order_id=identifier(order.get("id")), operation_id=operation)
            # Do not retain checkout bearer URLs in evidence. Open the owned order in the account UI.
            print("Complete this sandbox order in the merchant account center: " + identifier(order.get("id")))
        else:
            verify(merchant, config, args, report, grant["user"])
    finally:
        try:
            merchant.call("POST", "/api/v1/auth/logout", {})
            record(report, "probe_session_revoked")
        except ProbeError as error:
            record(report, "probe_session_revoked", "failed", code=error.code)
        merchant.token = ""


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("preflight", "checkout", "verify"))
    parser.add_argument("--config", required=True, help="Public launch-config.json exported by the integration wizard; sandbox only")
    parser.add_argument("--report", required=True, type=Path, help="New JSON evidence file (refuses overwrite)")
    parser.add_argument("--expect", choices=("active", *EXPECTED_CODES), default="active")
    parser.add_argument("--order-id")
    parser.add_argument("--expect-order", choices=("paid", "refunded", "pending", "refunding", "closed"))
    parser.add_argument("--activate", action="store_true", help="Synchronously refresh official SDK online, then execute a protected unmetered no-op")
    parser.add_argument("--product-config", type=Path)
    parser.add_argument("--feature", default="export", help="Use an unmetered feature in a dedicated test policy")
    parser.add_argument("--cache-dir", type=Path, default=Path(".local/acceptance/sdk-cache"))
    parser.add_argument("--wait-seconds", type=int, default=30)
    parser.add_argument("--provider", choices=("stripe", "paypal", "waffo", "paddle"))
    parser.add_argument("--plan-version-id")
    parser.add_argument("--operation-id", help="Stable UUID; reuse for retrying the same sandbox checkout")
    args = parser.parse_args(argv)
    if not 0 <= args.wait_seconds <= 120:
        parser.error("--wait-seconds must be between 0 and 120")
    if args.expect_order and not args.order_id:
        parser.error("--expect-order requires --order-id")
    if args.command != "verify" and (args.activate or args.order_id or args.expect_order or args.product_config or args.expect != "active"):
        parser.error("activation and entitlement/order assertions require the verify command")
    checkout_options = (args.provider, args.plan_version_id, args.operation_id)
    if args.command == "checkout" and not all(checkout_options):
        parser.error("checkout requires --provider, --plan-version-id and --operation-id")
    if args.command != "checkout" and any(checkout_options):
        parser.error("checkout creation options require the checkout command")
    if args.activate and (args.expect != "active" or not args.product_config):
        parser.error("--activate requires --expect active and --product-config")
    if args.report.exists():
        parser.error("report already exists; choose a new evidence path")
    report = {"schema_version": 1, "scope": "external_sandbox_probe", "command": args.command,
              "started_at": datetime.now(timezone.utc).isoformat(), "checks": [],
              "acceptance_complete": False,
              "limitations": ["A passing probe does not certify the full acceptance matrix.",
                              "Provider payment, refund settlement, restart, offline and other-device behavior require their recorded scenarios."]}
    result = 0
    try:
        run(args, report)
    except ProbeError as error:
        record(report, "probe", "failed", code=error.code, http_status=error.status)
        result = 1
    except (OSError, ValueError, KeyError, TypeError, EOFError):
        record(report, "probe", "failed", code="LOCAL_CONFIGURATION_OR_IO_ERROR")
        result = 1
    except KeyboardInterrupt:
        record(report, "probe", "failed", code="INTERRUPTED")
        result = 130
    report["completed_at"] = datetime.now(timezone.utc).isoformat()
    report["result"] = "failed" if result or any(c["result"] == "failed" for c in report["checks"]) else "probe_passed"
    args.report.parent.mkdir(parents=True, exist_ok=True)
    with args.report.open("x", encoding="utf-8") as stream:
        json.dump(report, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    print(report["result"] + ": " + str(args.report.resolve()))
    return result or (1 if report["result"] == "failed" else 0)


if __name__ == "__main__":
    sys.exit(main())
