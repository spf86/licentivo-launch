"""Serialize account vault transitions across instances and processes."""
from contextlib import contextmanager
from functools import wraps
import hashlib
import os
from pathlib import Path
import threading
import time

_mutex = threading.Lock()
_locks = {}
_held = threading.local()

def _intent_path(scope, token):
    root = Path(os.environ.get('LOCALAPPDATA', str(Path.home() / '.cache'))) / 'LicentivoLaunch/account-intents/python'
    return root / (hashlib.sha256(scope.encode()).hexdigest() + '.' + hashlib.sha256(token.encode()).hexdigest() + '.signedout')
def remember_intent(scope, token):
    if not token: return
    path = _intent_path(scope, token)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    with path.open('wb') as stream:
        stream.write(b'1'); stream.flush(); os.fsync(stream.fileno())
def has_intent(scope, token):
    try: _intent_path(scope, token).stat(); return True
    except FileNotFoundError: return False
def clear_intent(scope, token):
    if token: _intent_path(scope, token).unlink(missing_ok=True)

@contextmanager
def account_lock(scope):
    with _mutex:
        lock = _locks.setdefault(scope, threading.RLock())
    with lock:
        held = getattr(_held, 'scopes', None)
        if held is None:
            held = _held.scopes = set()
        if scope in held:
            yield
            return
        root = Path(os.environ.get('LOCALAPPDATA', str(Path.home() / '.cache'))) / 'LicentivoLaunch/account-locks/python'
        root.mkdir(parents=True, exist_ok=True, mode=0o700)
        path = root / (hashlib.sha256(scope.encode()).hexdigest() + '.lock')
        with path.open('a+b') as stream:
            if stream.seek(0, 2) == 0:
                stream.write(b'\0')
                stream.flush()
            deadline = time.monotonic() + 5
            while True:
                try:
                    stream.seek(0)
                    if os.name == 'nt':
                        import msvcrt
                        msvcrt.locking(stream.fileno(), msvcrt.LK_NBLCK, 1)
                    else:
                        import fcntl
                        fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except OSError:
                    if time.monotonic() >= deadline:
                        raise RuntimeError('ACCOUNT_LOCK_BUSY') from None
                    time.sleep(0.02)
            held.add(scope)
            try:
                yield
            finally:
                held.remove(scope)
                stream.seek(0)
                if os.name == 'nt':
                    msvcrt.locking(stream.fileno(), msvcrt.LK_UNLCK, 1)
                else:
                    fcntl.flock(stream, fcntl.LOCK_UN)

def account_serial(action):
    @wraps(action)
    def guarded(self, *args, **kwargs):
        with self._lifecycle_lock:
            if action.__name__ == 'logout':
                saved = self._pending_logout or self.session
                if saved:
                    try: remember_intent(self.scope, saved['access_token'])
                    except OSError: pass
            try:
                with account_lock(self.scope):
                    return action(self, *args, **kwargs)
            except Exception:
                if action.__name__ == 'logout':
                    if self.runtime:
                        try: self.runtime.logout() if hasattr(self.runtime, 'logout') else self.runtime.deactivate()
                        except Exception: pass
                        self.runtime.close()
                    self.runtime = None
                    saved = self._pending_logout or self.session
                    if saved:
                        self._pending_logout = saved | {'version': 3, 'logout_pending': True}
                        self.session = None
                raise
    return guarded
