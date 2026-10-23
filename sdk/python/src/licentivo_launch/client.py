import ctypes
from datetime import datetime, timezone
from email.utils import parsedate_to_datetime
import hashlib
import json
import math
import os
from pathlib import Path
import re
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


class APIError(Exception):
    """Safe business error; never includes the raw response, bearer or credential."""
    def __init__(self, code, status=0, retry_after=0):
        self.code = code if re.fullmatch(r"[A-Za-z0-9_]{1,80}", str(code)) else "REQUEST_FAILED"
        self.status = status
        self.retry_after = max(0, retry_after)
        self.retryable = self.code == "ENTITLEMENT_PENDING" or status == 429 or status >= 500
        super().__init__("Launch: " + self.code)


class MemoryStore:
    """Default: secrets stay in process. Supply load/save/delete methods for an OS vault."""
    persistent = False

    def __init__(self):
        self._values = {}

    def load(self, key):
        return self._values.get(key)

    def save(self, key, value):
        self._values[key] = bytes(value)

    def delete(self, key):
        self._values.pop(key, None)


class WindowsDPAPIStore:
    """Current-user DPAPI with atomic ciphertext writes and no plaintext fallback."""
    persistent = True

    def __init__(self, directory=None):
        if os.name != "nt":
            raise RuntimeError("Remembered login needs an explicit OS-protected store on this platform")
        self.directory = Path(directory or Path(os.environ["LOCALAPPDATA"]) / "LicentivoLaunch" / "sessions")
        self.directory.mkdir(parents=True, exist_ok=True)

    def _path(self, key):
        return self.directory / (hashlib.sha256(key.encode()).hexdigest() + ".dpapi")

    @staticmethod
    def _crypt(value, decrypt=False):
        from ctypes import wintypes

        class Blob(ctypes.Structure):
            _fields_ = [("size", wintypes.DWORD), ("data", ctypes.POINTER(ctypes.c_ubyte))]

        crypt = ctypes.WinDLL("crypt32", use_last_error=True)
        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.LocalFree.argtypes = [ctypes.c_void_p]
        kernel.LocalFree.restype = ctypes.c_void_p
        raw = ctypes.create_string_buffer(value)
        source, output = Blob(len(value), ctypes.cast(raw, ctypes.POINTER(ctypes.c_ubyte))), Blob()
        fn = crypt.CryptUnprotectData if decrypt else crypt.CryptProtectData
        fn.argtypes = [ctypes.POINTER(Blob), ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p,
                       ctypes.c_void_p, wintypes.DWORD, ctypes.POINTER(Blob)]
        fn.restype = wintypes.BOOL
        if not fn(ctypes.byref(source), None, None, None, None, 1, ctypes.byref(output)):
            raise OSError(ctypes.get_last_error(), "OS secret protection failed")
        try:
            return ctypes.string_at(output.data, output.size)
        finally:
            kernel.LocalFree(output.data)

    def load(self, key):
        path = self._path(key)
        if not path.exists():
            return None
        if path.stat().st_size > 65536:
            raise ValueError("Invalid remembered session")
        return self._crypt(path.read_bytes(), decrypt=True)

    def save(self, key, value):
        encrypted = self._crypt(value)
        descriptor, name = tempfile.mkstemp(prefix=".session-", dir=self.directory)
        try:
            with os.fdopen(descriptor, "wb") as stream:
                stream.write(encrypted)
            os.replace(name, self._path(key))
        finally:
            if os.path.exists(name):
                os.unlink(name)

    def delete(self, key):
        self._path(key).unlink(missing_ok=True)


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


class Application:
    """Reconcile accounts online; allow verified signed caches during outages.

    Official SDK device credentials use its permission-restricted, unencrypted
    cache format. Launch remembered sessions use the selected secure store.
    """
    def __init__(self, origin, product_id, product_config, environment="live", *,
                 store=None, cache_dir=None, runtime_factory=None, opener=None, sleep=time.sleep):
        parsed = urllib.parse.urlsplit(origin)
        if (environment not in ("sandbox", "live") or not parsed.hostname or parsed.username is not None
                or parsed.password is not None or parsed.path not in ("", "/") or parsed.query or parsed.fragment
                or parsed.scheme not in ("http", "https")):
            raise ValueError("Use a valid merchant origin and sandbox/live environment")
        if parsed.scheme != "https" and not (environment == "sandbox" and parsed.hostname in ("localhost", "127.0.0.1", "::1")):
            raise ValueError("HTTPS required")
        if str(uuid.UUID(product_id)) != product_id.lower():
            raise ValueError("Invalid Launch product ID")
        if runtime_factory is None:
            from ._vendor.licentivo import Client
            runtime_factory = Client
        self.origin, self.product_id, self.environment = origin.rstrip("/"), product_id, environment
        self.config = dict(product_config)
        if not self.config.get("product_id") or not self.config.get("trusted_keys"):
            raise ValueError("Public runtime product and signing keys are required")
        self.store = store if store is not None else MemoryStore()
        self.cache_dir = Path(cache_dir or Path(os.getenv("LOCALAPPDATA") or os.getenv("XDG_CACHE_HOME") or Path.home() / ".cache") / "LicentivoLaunch")
        self.scope = "\n".join((self.origin, self.product_id, environment, self.config["product_id"]))
        self.runtime_factory, self.sleep = runtime_factory, sleep
        self.opener = opener or urllib.request.build_opener(_NoRedirect())
        self.session, self.runtime, self.cache_path = None, None, None
        self.closed = False

    @classmethod
    def connect(cls, merchant_origin, product_id, licentivo_product_id, trusted_keys,
                environment="live", **kwargs):
        """Initialize directly; the merchant URL is required and never inferred."""
        return cls(merchant_origin, product_id, {
            "base_url": "https://www.licentivo.com", "product_id": licentivo_product_id,
            "trusted_keys": trusted_keys,
        }, environment, **kwargs)

    @classmethod
    def from_files(cls, launch_config, product_config, **kwargs):
        launch = json.loads(Path(launch_config).read_text(encoding="utf-8-sig"))
        product = json.loads(Path(product_config).read_text(encoding="utf-8-sig"))
        if launch.get("schema_version") != 1 or launch.get("licentivo_product_id") != product.get("product_id"):
            raise ValueError("Launch and SDK configuration mismatch")
        return cls(launch["merchant_origin"], launch["launch_product_id"], product, launch["environment"], **kwargs)

    def _call(self, path, body, token="", method="POST"):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request(self.origin + "/api/v1" + path, None if method == "GET" else json.dumps(body).encode(), headers, method=method)
        try:
            with self.opener.open(request, timeout=15) as response:
                raw = response.read(1048577)
                if len(raw) > 1048576:
                    raise APIError("INVALID_RESPONSE")
                data = json.loads(raw)["data"]
                if not isinstance(data, dict):
                    raise APIError("INVALID_RESPONSE")
                return data
        except urllib.error.HTTPError as error:
            try:
                code = json.loads(error.read(65536)).get("error", {}).get("code", "REQUEST_FAILED")
                retry = error.headers.get("Retry-After", "0")
                try:
                    delay = max(0, int(retry))
                except ValueError:
                    try:
                        delay = max(0, math.ceil((parsedate_to_datetime(retry) - datetime.now(timezone.utc)).total_seconds()))
                    except (ValueError, TypeError, OverflowError):
                        delay = 0
            except (ValueError, TypeError, AttributeError):
                code, delay = "REQUEST_FAILED", 0
            raise APIError(code, error.code, delay) from None
        except (urllib.error.URLError, TimeoutError, OSError):
            raise APIError("NETWORK_UNAVAILABLE") from None
        except (ValueError, KeyError, TypeError):
            raise APIError("INVALID_RESPONSE") from None

    def _credential(self, token, attempts):
        for index in range(attempts):
            try:
                data = self._call("/software/products/" + self.product_id + "/credential", {"environment": self.environment}, token)
                if (data.get("product_id") != self.product_id or not isinstance(data.get("license_key"), str)
                        or not data["license_key"] or type(data.get("version")) is not int or data["version"] < 1):
                    raise APIError("CREDENTIAL_MISMATCH")
                return data["license_key"]
            except APIError as error:
                if error.code != "ENTITLEMENT_PENDING" or error.status not in (403, 409) or index + 1 == attempts or error.retry_after > 30:
                    raise
                self.sleep(max(3, error.retry_after or min(2 ** index, 4)))

    def _cache_for(self, session):
        scope = hashlib.sha256((self.scope + "\n" + session["user_id"]).encode()).hexdigest()
        return self.cache_dir / scope / "lease.json"

    def _start(self, session, offline=False):
        if self.closed:
            raise RuntimeError("Application is closed")
        if self.runtime:
            self.runtime.close()
            self.runtime = None
        directory = self._cache_for(session).parent
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        if os.name != "nt":
            directory.chmod(0o700)
        self.cache_path = directory / "lease.json"
        config = self.config | {"license_key": session["license_key"], "cache_path": str(self.cache_path)}
        runtime = self.runtime_factory(config)
        try:
            if offline and not runtime.status()["allowed"]:
                raise APIError("OFFLINE_UNAVAILABLE")
            runtime.start()
        except Exception:
            runtime.close()
            raise
        self.runtime, self.session = runtime, session
        return self

    def login(self, email, password, *, pending_attempts=3):
        if self.closed:
            raise RuntimeError("Application is closed")
        if not 1 <= pending_attempts <= 5:
            raise ValueError("pending_attempts must be between 1 and 5")
        self.logout()
        grant = self._call("/auth/login", {"email": email, "password": password, "kind": "desktop"})
        token = grant["access_token"]
        try:
            session = {"version": 1, "scope": self.scope, "access_token": token,
                       "user_id": grant["user"]["id"], "expires_at": grant["expires_at"],
                       "license_key": self._credential(token, pending_attempts)}
            self._start(session)
            self.store.save(self.scope, json.dumps(session).encode())
            return self
        except Exception:
            try:
                self._call("/auth/logout", {}, token)
            except APIError:
                pass
            if self.runtime:
                self.runtime.close()
            self.runtime, self.session = None, None
            raise

    def _saved(self):
        if self.closed:
            raise RuntimeError("Application is closed")
        raw = self.store.load(self.scope)
        if raw is None:
            return None
        try:
            session = json.loads(raw)
            if (session["version"] != 1 or session["scope"] != self.scope or not session["user_id"]
                    or not session["license_key"] or not isinstance(session["access_token"], str)):
                raise ValueError()
        except (ValueError, KeyError, TypeError):
            raise APIError("INVALID_SAVED_SESSION") from None
        return session

    def restore(self):
        """Reconcile online; only an outage permits an already verified SDK cache."""
        if self.runtime:
            self.runtime.close()
        self.runtime, self.session = None, None
        session = self._saved()
        if session is None:
            return False
        try:
            expires = datetime.fromisoformat(session["expires_at"].replace("Z", "+00:00"))
            if expires.tzinfo is None:
                raise ValueError()
        except (ValueError, KeyError, TypeError, AttributeError):
            self._forget(session)
            raise APIError("INVALID_SAVED_SESSION") from None
        if expires <= datetime.now(timezone.utc):
            self._forget(session)
            raise APIError("SESSION_EXPIRED")
        try:
            data = self._call("/me", None, session["access_token"], method="GET")
            if data.get("user", {}).get("id") != session["user_id"]:
                raise APIError("SESSION_REJECTED", 401)
            session["license_key"] = self._credential(session["access_token"], 3)
        except APIError as error:
            if error.code == "NETWORK_UNAVAILABLE" or error.status >= 500:
                self._start(session, offline=True)
                return True
            if error.code != "ENTITLEMENT_PENDING" and (error.status in (401, 403, 404) or error.code in ("ENTITLEMENT_EXPIRED", "ENTITLEMENT_SUSPENDED", "ENTITLEMENT_NOT_PURCHASED")):
                self._forget(session)
            raise
        self._start(session)
        try:
            self.store.save(self.scope, json.dumps(session).encode())
        except Exception:
            self.runtime.close()
            self.runtime, self.session = None, None
            raise
        return True

    def run_feature(self, name, action):
        if not self.runtime:
            raise APIError("LOGIN_REQUIRED")
        return self.runtime.run_feature(name, action)

    def release_device(self):
        if not self.runtime:
            raise APIError("LOGIN_REQUIRED")
        self.runtime.deactivate()
        self.logout()

    def _forget(self, saved=None):
        if self.runtime:
            self.runtime.close()
        self.runtime, self.session = None, None
        self.store.delete(self.scope)
        cache = self._cache_for(saved) if saved else self.cache_path
        if cache:
            cache.unlink(missing_ok=True)
            Path(str(cache) + ".credential.json").unlink(missing_ok=True)

    def logout(self):
        try:
            saved = self.session or self._saved()
        except Exception:
            self._forget()
            raise
        token = saved.get("access_token", "") if saved else ""
        self._forget(saved)
        if token:
            try:
                self._call("/auth/logout", {}, token)
            except APIError:
                # Local logout succeeds offline; server bearer expires normally.
                pass

    def close(self):
        if self.closed:
            return
        if self.runtime:
            self.runtime.close()
        self.runtime, self.session, self.closed = None, None, True

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()
