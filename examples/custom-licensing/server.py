"""Durable protocol adapter example. Supply your own licensing backend hooks.

This module does not generate licenses or implement runtime authorization.
Put production instances behind HTTPS and protect the SQLite file.
"""
import argparse
from contextlib import contextmanager
import base64
import hashlib
import hmac
import importlib
import json
import os
import re
import sqlite3
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PROTOCOL = "launch-external-licensing-v1"
REFERENCE = re.compile(r"^[A-Za-z0-9_-]{1,128}$")
EXTERNAL = re.compile(r"^[A-Za-z0-9_.-]{1,128}$")


class BackendRequired:
    @staticmethod
    def apply_snapshot(snapshot):
        # Apply exact target dates/state in YOUR system using event_id as its
        # durable idempotency key. Return only after the result is confirmed.
        raise NotImplementedError("configure CUSTOM_LICENSING_BACKEND")

    @staticmethod
    def delivery(snapshot):
        # Return {"type": "license_key"|"activation_url"|"instructions",
        #         "value": "...", "expires_at": None|"UTC timestamp"}.
        # Read an existing result; do not create a new license here.
        raise NotImplementedError("configure CUSTOM_LICENSING_BACKEND")


class Bridge:
    def __init__(self, filename, key, key_id, connection, environment, backend):
        if len(key) < 32 or environment not in ("sandbox", "live"):
            raise ValueError("invalid key or environment")
        self.filename, self.key, self.key_id = filename, key, key_id
        self.connection, self.environment, self.backend = connection, environment, backend
        self.stopping = threading.Event()
        with self.db() as db:
            db.executescript("""
              CREATE TABLE IF NOT EXISTS nonces (nonce TEXT PRIMARY KEY, seen REAL NOT NULL);
              CREATE TABLE IF NOT EXISTS events (
                id TEXT PRIMARY KEY, ref TEXT NOT NULL, version INTEGER NOT NULL,
                body BLOB NOT NULL, hash TEXT NOT NULL, status TEXT NOT NULL,
                error TEXT, lease REAL NOT NULL DEFAULT 0,
                available REAL NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0);
              CREATE TABLE IF NOT EXISTS latest (ref TEXT PRIMARY KEY, version INTEGER NOT NULL, body BLOB NOT NULL);
            """)

    @contextmanager
    def db(self):
        db = sqlite3.connect(self.filename, timeout=15)
        db.row_factory = sqlite3.Row
        db.execute("PRAGMA journal_mode=WAL")
        try:
            with db:
                yield db
        finally:
            db.close()

    def signed(self, method, target, headers, raw):
        try:
            stamp, nonce = headers["X-Launch-Timestamp"], headers["X-Launch-Nonce"]
            if headers["X-Launch-Key-Id"] != self.key_id or abs(time.time() - int(stamp)) > 300:
                return False
            if not re.fullmatch(r"[A-Za-z0-9_-]{22,128}", nonce):
                return False
            message = "\n".join(("v1", self.key_id, stamp, nonce, method, target, hashlib.sha256(raw).hexdigest()))
            digest = base64.urlsafe_b64encode(hmac.digest(self.key, message.encode(), "sha256")).decode().rstrip("=")
            if not hmac.compare_digest("v1=" + digest, headers["X-Launch-Signature"]):
                return False
            with self.db() as db:
                db.execute("DELETE FROM nonces WHERE seen < ?", (time.time() - 600,))
                db.execute("INSERT INTO nonces VALUES (?, ?)", (nonce, time.time()))
            return True
        except (ValueError, TypeError, KeyError, sqlite3.IntegrityError):
            return False

    def receipt(self, row):
        body = json.loads(row["body"])
        return dict(event_id=row["id"], connection_ref=self.connection,
                    environment=self.environment, entitlement_ref=row["ref"],
                    version=row["version"], payload_sha256=row["hash"],
                    status=row["status"], error_code=row["error"])

    def work(self):
        while not self.stopping.wait(1):
            with self.db() as db:
                db.execute("BEGIN IMMEDIATE")
                row = db.execute("""SELECT * FROM events e WHERE status='pending'
                   AND lease < ? AND available <= ? AND NOT EXISTS (
                     SELECT 1 FROM events prior WHERE prior.ref=e.ref
                     AND prior.version<e.version AND prior.status<>'applied')
                   ORDER BY rowid LIMIT 1""", (time.time(), time.time())).fetchone()
                if row is None:
                    continue
                db.execute("UPDATE events SET lease=?, attempts=attempts+1 WHERE id=?", (time.time() + 60, row["id"]))
            snapshot = json.loads(row["body"])
            try:
                self.backend.apply_snapshot(snapshot)
                with self.db() as db:
                    current = db.execute("SELECT version FROM latest WHERE ref=?", (row["ref"],)).fetchone()
                    if current and current["version"] > row["version"]:
                        raise ValueError("state version conflict")
                    db.execute("INSERT INTO latest VALUES (?, ?, ?) ON CONFLICT(ref) DO UPDATE SET version=excluded.version,body=excluded.body",
                               (row["ref"], row["version"], row["body"]))
                    db.execute("UPDATE events SET status='applied',error=NULL,lease=0 WHERE id=?", (row["id"],))
            except Exception as error:
                # Never log exception text: a merchant backend may include keys.
                terminal = isinstance(error, (NotImplementedError, ValueError)) or row["attempts"] >= 11
                code = "BACKEND_NOT_CONFIGURED" if isinstance(error, NotImplementedError) else "BACKEND_REVIEW_REQUIRED"
                with self.db() as db:
                    db.execute("UPDATE events SET status=?,error=?,lease=0,available=? WHERE id=?",
                               ("manual_review" if terminal else "pending", code,
                                time.time() + min(2 ** min(row["attempts"], 10), 1024), row["id"]))


def handler(bridge):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass  # Do not put delivery values, signatures or request bodies in logs.

        def reply(self, code, data=None, error=None):
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(json.dumps({"error": {"code": error, "message": "Request could not be completed"}} if error else {"data": data}, separators=(",", ":")).encode())

        def do_GET(self):
            self.handle_api()

        def do_POST(self):
            self.handle_api()

        def handle_api(self):
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if length < 0 or length > 65536:
                    return self.reply(413, error="REQUEST_TOO_LARGE")
                raw = self.rfile.read(length)
                if not bridge.signed(self.command, self.path, self.headers, raw):
                    return self.reply(401, error="SIGNATURE_INVALID")
                if self.command == "GET" and self.path == "/launch/v1/capabilities":
                    return self.reply(200, dict(protocol=PROTOCOL, connection_ref=bridge.connection,
                        environment=bridge.environment, purchase_types=["perpetual", "fixed_term"],
                        delivery_types=["license_key", "activation_url", "instructions"], devices=False, identity_exchange=False))
                if self.command == "POST" and self.path == "/launch/v1/events":
                    body = json.loads(raw)
                    required = {"schema", "event_id", "connection_ref", "environment", "type", "change_reason", "account_ref", "entitlement_ref", "launch_product_id", "launch_plan_version_id", "product_ref", "plan_ref", "version", "desired_state", "valid_from", "valid_until"}
                    if set(body) != required or body["schema"] != PROTOCOL or body["type"] != "entitlement.snapshot" or not isinstance(body["version"], int) or isinstance(body["version"], bool) or body["version"] < 1 or body["desired_state"] not in ("active", "suspended", "expired", "revoked"):
                        return self.reply(422, error="INVALID_SNAPSHOT")
                    if body["connection_ref"] != bridge.connection or body["environment"] != bridge.environment:
                        return self.reply(403, error="SCOPE_MISMATCH")
                    if any(not REFERENCE.fullmatch(str(body[k])) for k in ("event_id", "account_ref", "entitlement_ref")) or any(not EXTERNAL.fullmatch(str(body[k])) for k in ("product_ref", "plan_ref")):
                        return self.reply(422, error="INVALID_SNAPSHOT")
                    with bridge.db() as db:
                        db.execute("BEGIN IMMEDIATE")
                        saved = db.execute("SELECT * FROM events WHERE id=?", (body["event_id"],)).fetchone()
                        digest = hashlib.sha256(raw).hexdigest()
                        if saved:
                            if saved["hash"] != digest:
                                return self.reply(409, error="IDEMPOTENCY_CONFLICT")
                            return self.reply(200, bridge.receipt(saved))
                        conflict = db.execute("SELECT 1 FROM events WHERE ref=? AND version>=?", (body["entitlement_ref"], body["version"])).fetchone()
                        if conflict:
                            return self.reply(409, error="STATE_VERSION_CONFLICT")
                        db.execute("INSERT INTO events(id,ref,version,body,hash,status) VALUES(?,?,?,?,?,'pending')",
                                   (body["event_id"], body["entitlement_ref"], body["version"], raw, digest))
                        saved = db.execute("SELECT * FROM events WHERE id=?", (body["event_id"],)).fetchone()
                    return self.reply(202, bridge.receipt(saved))
                match = re.fullmatch(r"/launch/v1/events/([A-Za-z0-9_-]{1,128})", self.path)
                if self.command == "GET" and match:
                    with bridge.db() as db:
                        row = db.execute("SELECT * FROM events WHERE id=?", (match[1],)).fetchone()
                    return self.reply(200, bridge.receipt(row)) if row else self.reply(404, error="EVENT_NOT_FOUND")
                match = re.fullmatch(r"/launch/v1/entitlements/([A-Za-z0-9_-]{1,128})/delivery", self.path)
                if self.command == "GET" and match:
                    with bridge.db() as db:
                        row = db.execute("SELECT body FROM latest WHERE ref=?", (match[1],)).fetchone()
                    if not row:
                        return self.reply(404, error="ENTITLEMENT_NOT_FOUND")
                    snapshot = json.loads(row["body"])
                    value = bridge.backend.delivery(snapshot) if snapshot["desired_state"] == "active" else None
                    return self.reply(200, dict(connection_ref=bridge.connection, environment=bridge.environment,
                        entitlement_ref=match[1], version=snapshot["version"], status="ready" if value else "unavailable", delivery=value))
                return self.reply(404, error="NOT_FOUND")
            except (ValueError, TypeError, KeyError, UnicodeError):
                return self.reply(422, error="INVALID_INPUT")
            except Exception:
                return self.reply(503, error="SERVICE_UNAVAILABLE")
    return Handler


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--retry-event", help="Locally requeue an original manual_review event after fixing its backend")
    args = parser.parse_args()
    module = os.environ.get("CUSTOM_LICENSING_BACKEND", "")
    bridge = Bridge(os.environ["CUSTOM_LICENSING_DATABASE"],
        base64.b64decode(os.environ["CUSTOM_LICENSING_KEY"], validate=True),
        os.environ.get("CUSTOM_LICENSING_KEY_ID", "key-1"),
        os.environ["CUSTOM_LICENSING_CONNECTION_REF"],
        os.environ.get("CUSTOM_LICENSING_ENVIRONMENT", "sandbox"),
        importlib.import_module(module) if module else BackendRequired())
    if args.retry_event:
        with bridge.db() as db:
            db.execute("UPDATE events SET status='pending',error=NULL,available=0,lease=0,attempts=0 WHERE id=? AND status='manual_review'", (args.retry_event,))
    else:
        worker = threading.Thread(target=bridge.work, daemon=True)
        worker.start()
        try:
            ThreadingHTTPServer(("127.0.0.1", int(os.environ.get("CUSTOM_LICENSING_PORT", "18883"))), handler(bridge)).serve_forever()
        finally:
            bridge.stopping.set()
