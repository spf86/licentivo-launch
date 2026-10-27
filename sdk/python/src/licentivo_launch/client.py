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
from threading import RLock
from ._account_lock import account_serial, remember_intent, has_intent, clear_intent


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
                 store=None, cache_dir=None, runtime_factory=None, opener=None, sleep=time.sleep, allow_http=False):
        self._lifecycle_lock = RLock()
        parsed = urllib.parse.urlsplit(origin)
        if (environment not in ("sandbox", "live") or not parsed.hostname or parsed.username is not None
                or parsed.password is not None or parsed.path not in ("", "/") or parsed.query or parsed.fragment
                or parsed.scheme not in ("http", "https")):
            raise ValueError("Use a valid merchant origin and sandbox/live environment")
        if not isinstance(allow_http, bool) or allow_http and environment != "sandbox":
            raise ValueError("HTTP opt-in is restricted to sandbox")
        if parsed.scheme != "https" and not (environment == "sandbox" and (allow_http or parsed.hostname in ("localhost", "127.0.0.1", "::1"))):
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
        self.scope = "\n".join(("launch-account-v2", self.origin, self.product_id, environment, self.config["product_id"]))
        self.runtime_factory, self.sleep = runtime_factory, sleep
        self.opener = opener or urllib.request.build_opener(_NoRedirect())
        self.session, self.runtime, self.cache_path = None, None, None
        self._pending_logout = None
        self._account_cleared = False
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
        kwargs["allow_http"] = launch.get("allow_http", False)
        return cls(launch["merchant_origin"], launch["launch_product_id"], product, launch["environment"], **kwargs)

    def _call(self, path, body, token="", method="POST"):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request(self.origin + "/api/v1" + path, None if method == "GET" else json.dumps(body).encode(), headers, method=method)
        try:
            with self.opener.open(request, timeout=15) as response:
                if response.status != 200:
                    raise APIError("REQUEST_FAILED", response.status)
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

    def _credential(self, token, attempts=1):
        own = self._call('/software/products/' + self.product_id + '/identity', {'environment': self.environment}, token)
        if (own.get('product_id') != self.product_id or own.get('licentivo_product_id') != self.config['product_id']
                or own.get('environment') != self.environment or not re.fullmatch(r'[a-fA-F0-9-]{36}', own.get('launch_connection_id', ''))
                or not isinstance(own.get('identity_token'), str) or not 32 <= len(own['identity_token']) <= 12288):
            raise APIError('IDENTITY_MISMATCH')
        return own

    def _cache_for(self, session):
        scope = hashlib.sha256((self.scope + "\n" + session["user_id"]).encode()).hexdigest()
        return self.cache_dir / scope / "lease.json"

    def _start(self, session, identity=None):
        if self.closed: raise RuntimeError('Application is closed')
        if self.runtime: self.runtime.close()
        self.runtime = None
        directory = self._cache_for(session).parent
        directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        if os.name != 'nt': directory.chmod(0o700)
        self.cache_path = directory / 'lease.json'
        identity = identity or session['identity_context']
        config = self.config | {'license_key': '', 'identity_token': identity.get('identity_token', ''),
                                'launch_connection_id': identity['launch_connection_id'], 'environment': self.environment,
                                'cache_path': str(self.cache_path)}
        runtime = self.runtime_factory(config)
        try: runtime.start()
        except Exception:
            runtime.close()
            raise
        self.runtime, self.session = runtime, session
        self._account_cleared = False
        return self

    @account_serial
    def login(self, email, password, *, pending_attempts=3):
        if self.closed: raise RuntimeError('Application is closed')
        if not self.logout(): raise APIError('LOGOUT_PENDING')
        grant = self._call('/auth/login', {'email': email, 'password': password, 'kind': 'desktop'})
        token = grant['access_token']
        self.session = {'version': 2, 'scope': self.scope, 'access_token': token,
                        'user_id': grant.get('user', {}).get('id', ''), 'expires_at': grant.get('expires_at', ''),
                        'identity_context': {}}
        try:
            identity = self._credential(token)
            context = {k: identity[k] for k in ('product_id', 'licentivo_product_id', 'launch_connection_id', 'environment', 'expires_at')}
            session = {'version': 2, 'scope': self.scope, 'access_token': token, 'user_id': grant['user']['id'],
                       'expires_at': grant['expires_at'], 'identity_context': context}
            self.session = session
            self._start(session, identity)
            self.store.save(self.scope, json.dumps(session).encode())
            return self
        except Exception:
            self.logout()
            raise

    def _saved(self):
        if self.closed: raise RuntimeError('Application is closed')
        raw = self.store.load(self.scope)
        if raw is None: return None
        try:
            saved = json.loads(raw)
            return self._validate_saved(saved)
        except (ValueError, KeyError, TypeError): raise APIError('INVALID_SAVED_SESSION') from None

    def _validate_saved(self, saved):
        try:
            if saved.get('logout_pending') is True:
                if saved.get('version') not in (2, 3) or saved.get('scope') != self.scope or not isinstance(saved.get('access_token'), str) or not saved['access_token']:
                    raise ValueError()
                return saved
            context = saved['identity_context']
            if (saved['version'] != 2 or saved['scope'] != self.scope or not saved['user_id']
                    or not isinstance(saved['access_token'], str) or context.get('identity_token')
                    or context['product_id'] != self.product_id or context['environment'] != self.environment): raise ValueError()
            return saved
        except (ValueError, KeyError, TypeError): raise APIError('INVALID_SAVED_SESSION') from None

    @account_serial
    def restore(self):
        pending = self._pending_logout
        if self.runtime: self.runtime.close()
        self.runtime, self.session = None, None
        saved = pending or self._saved()
        tasks = self._pending_accounts()
        saved = next((task for task in tasks if saved is None or task['access_token'] == saved['access_token']), saved)
        if saved is None: return False
        if has_intent(self.scope, saved['access_token']): saved = saved | {'version':3,'logout_pending':True}
        if saved.get('logout_pending'):
            self._pending_logout = saved
            if not self.logout(): raise APIError('ACCOUNT_LOGOUT_PENDING')
            return False
        self._retry_other_accounts(saved['access_token'])
        expires = datetime.fromisoformat(saved['expires_at'].replace('Z', '+00:00'))
        if expires.tzinfo is None or expires <= datetime.now(timezone.utc):
            self._forget(saved)
            raise APIError('SESSION_EXPIRED')
        try:
            me = self._call('/me', None, saved['access_token'], method='GET')
            user = me.get('user')
            if not isinstance(user, dict) or not isinstance(user.get('id'), str) or not user['id']:
                raise APIError('INVALID_RESPONSE')
            if user['id'] != saved['user_id']: raise APIError('SESSION_REJECTED', 401)
            identity = self._credential(saved['access_token'])
        except APIError as error:
            if error.status == 401 or error.status == 403 and error.code not in ('SDK_HTTP_DISABLED', 'SDK_HTTPS_REQUIRED'):
                self._forget(saved)
            raise
        saved['identity_context'] = {k: identity[k] for k in ('product_id', 'licentivo_product_id', 'launch_connection_id', 'environment', 'expires_at')}
        self._start(saved, identity)
        try:
            self.store.save(self.scope, json.dumps(saved).encode())
        except Exception:
            self._forget(saved)
            raise
        return True

    def run_feature(self, name, action):
        if not self.runtime:
            raise APIError("LOGIN_REQUIRED")
        return self.runtime.run_feature(name, action)

    def release_device(self):
        return self.logout()

    def _forget(self, saved=None):
        saved = saved or self._pending_logout or self.session
        try:
            if self.runtime: self.runtime.close()
        finally:
            self.runtime, self.session = None, None
            self._pending_logout = None
            self._account_cleared = True
            if saved: self._delete_owned(saved)
        # The official SDK exclusively owns its cache and durable logout journal.

    def _pending_key(self): return self.scope + '\naccount-logout-queue-v1'

    def _pending_accounts(self):
        raw = self.store.load(self._pending_key())
        if raw is None: return []
        try:
            queue = json.loads(raw)
            if queue['version'] != 1 or not isinstance(queue['records'], list) or len(queue['records']) > 64: raise ValueError()
            for record in queue['records']:
                if record.get('logout_pending') is not True: raise ValueError()
                self._validate_saved(record)
            return queue['records']
        except (ValueError, KeyError, TypeError): raise APIError('INVALID_SAVED_SESSION') from None

    def _update_pending(self, saved, remove=False):
        records = [value for value in self._pending_accounts() if value['access_token'] != saved['access_token']]
        if not remove: records.append(saved | {'version': 3, 'logout_pending': True})
        if len(records) > 64 or len(json.dumps({'version': 1, 'records': records}).encode()) > 60000: raise APIError('ACCOUNT_LOGOUT_QUEUE_FULL')
        if records: self.store.save(self._pending_key(), json.dumps({'version': 1, 'records': records}).encode())
        else: self.store.delete(self._pending_key())

    def _owns(self, saved):
        raw = self.store.load(self.scope)
        return raw is not None and json.loads(raw).get('access_token') == saved.get('access_token')

    def _delete_owned(self, saved):
        if self._owns(saved): self.store.delete(self.scope)

    def _mark_logout(self, saved):
        try: remember_intent(self.scope, saved['access_token'])
        except OSError: pass
        queue_failure = None
        try: self._update_pending(saved)
        except Exception as error: queue_failure = error
        if self._owns(saved):
            try:
                self.store.save(self.scope, json.dumps(saved).encode())
                return
            except Exception:
                try: self._delete_owned(saved)
                except Exception: pass
        if queue_failure: raise APIError('ACCOUNT_LOGOUT_STORAGE_FAILED') from None

    def _finish_account(self, saved):
        self._delete_owned(saved)
        self._update_pending(saved, remove=True)
        clear_intent(self.scope, saved['access_token'])

    def _retry_other_accounts(self, skip):
        for saved in self._pending_accounts():
            if saved['access_token'] == skip: continue
            try: self._call('/auth/logout', {}, saved['access_token'])
            except APIError as error:
                if error.status != 401: raise APIError('ACCOUNT_LOGOUT_PENDING') from None
            self._finish_account(saved)

    @account_serial
    def logout(self):
        if self._account_cleared and not self._pending_logout and not self.session:
            self._retry_other_accounts(None)
            return True
        try: saved = self._pending_logout or self.session or self._saved()
        except Exception:
            self._forget()
            raise
        if saved is None: saved = next(iter(self._pending_accounts()), None)
        failure, persist_failure, other_failure, pending = None, None, None, False
        try: self._retry_other_accounts(saved['access_token'] if saved else None)
        except Exception as error: other_failure = error
        if saved:
            saved['logout_pending'] = True
            saved['version'] = 3
            self._pending_logout, self.session = saved, None
            try: self._mark_logout(saved)
            except Exception as error: persist_failure = error
        try:
            active = self._saved()
            if self.runtime is None and saved and saved.get('identity_context') and (active is None or active['access_token'] == saved['access_token']):
                identity = saved['identity_context']
                self.runtime = self.runtime_factory(self.config | {'license_key': '', 'identity_token': '',
                    'launch_connection_id': identity['launch_connection_id'], 'environment': self.environment,
                    'cache_path': str(self._cache_for(saved))})
            if self.runtime:
                result = self.runtime.logout() if hasattr(self.runtime, 'logout') else self.runtime.deactivate()
                pending = isinstance(result, dict) and result.get('revocation_pending', False)
                if isinstance(result, dict) and result.get('storage_error'): raise APIError('SDK_LOGOUT_STORAGE_FAILED')
        except Exception as error: failure = error
        finally:
            try:
                if self.runtime: self.runtime.close()
            except Exception as error:
                if failure is None: failure = error
            finally: self.runtime = None
        confirmed = True
        if saved:
            try: self._call('/auth/logout', {}, saved['access_token'])
            except APIError as error: confirmed = error.status == 401
        if confirmed:
            if saved: self._finish_account(saved)
            self._forget(saved)
        if not confirmed and persist_failure: raise persist_failure
        if failure: raise failure
        if other_failure: raise other_failure
        return confirmed and not pending

    def close(self):
        with self._lifecycle_lock:
            if self.closed:
                return
            runtime = self.runtime
            self.runtime, self.session, self.closed = None, None, True
            if runtime:
                runtime.close()

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()
