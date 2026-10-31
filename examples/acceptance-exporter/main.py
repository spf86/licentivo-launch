"""Real Launch account + signed SDK permission + protected CSV export.

Install the local package with: python -m pip install ./sdk/python
The public input is the result of Launch's authenticated integration check.
Passwords come from a prompt or LAUNCH_DEMO_PASSWORD, never command arguments.
"""
import argparse
import base64
import csv
import getpass
import hashlib
import json
import os
from pathlib import Path
import secrets
import sys
import tempfile
import uuid

from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from licentivo_launch import Application, WindowsDPAPIStore


class LinuxEncryptedStore:
    """Owner-only, encrypted, machine/user-bound example account store."""
    def __init__(self, directory):
        self.directory = Path(directory).resolve()
        self.directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.directory.chmod(0o700)
        key_path = self.directory / 'account.key'
        try:
            fd = os.open(key_path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        except FileExistsError:
            pass
        else:
            with os.fdopen(fd, 'wb') as stream:
                stream.write(secrets.token_bytes(32))
        key_path.chmod(0o600)
        key = key_path.read_bytes()
        if len(key) != 32:
            raise ValueError('Invalid account store key')
        machine = Path('/etc/machine-id').read_bytes().strip()
        self.aead = AESGCM(hashlib.sha256(key + machine + str(os.getuid()).encode()).digest())

    def _path(self, key):
        return self.directory / (hashlib.sha256(key.encode()).hexdigest() + '.bin')

    def load(self, key):
        try:
            raw = self._path(key).read_bytes()
        except FileNotFoundError:
            return None
        return self.aead.decrypt(raw[:12], raw[12:], key.encode())

    def save(self, key, value):
        nonce = secrets.token_bytes(12)
        raw = nonce + self.aead.encrypt(nonce, value, key.encode())
        fd, temp = tempfile.mkstemp(prefix='account-', dir=self.directory)
        try:
            with os.fdopen(fd, 'wb') as stream:
                stream.write(raw)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(temp, self._path(key))
        finally:
            if os.path.exists(temp):
                os.unlink(temp)

    def delete(self, key):
        self._path(key).unlink(missing_ok=True)


def connect(public, state, licentivo_origin=''):
    config = public['config']
    product = public['product_config']
    if config['licentivo_product_id'] != product['product_id']:
        raise ValueError('Public product mapping mismatch')
    store = WindowsDPAPIStore(state / 'account') if os.name == 'nt' else LinuxEncryptedStore(state / 'account')
    if licentivo_origin:
        # Explicit self-hosted acceptance target. The SDK still validates the
        # origin, sandbox HTTP opt-in, signatures and device/account context.
        return Application(
            config['merchant_origin'], config['launch_product_id'],
            product | {'base_url': licentivo_origin, 'allow_http': config.get('allow_http', False)},
            environment=config['environment'], allow_http=config.get('allow_http', False),
            store=store, cache_dir=state / 'runtime',
        )
    return Application.connect(
        config['merchant_origin'], config['launch_product_id'], product['product_id'],
        product['trusted_keys'], environment=config['environment'],
        allow_http=config.get('allow_http', False), store=store,
        cache_dir=state / 'runtime',
    )


def export_csv(destination):
    destination.parent.mkdir(parents=True, exist_ok=True)
    with destination.open('w', encoding='utf-8', newline='') as stream:
        writer = csv.writer(stream)
        writer.writerow(['item', 'quantity', 'unit_price', 'total'])
        writer.writerows([['Launch acceptance report', 2, 125, 250], ['SDK protected export', 3, 50, 150]])
    return {'output': str(destination.resolve()), 'sha256': hashlib.sha256(destination.read_bytes()).hexdigest(), 'rows': 2}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['login', 'restore', 'export', 'status', 'logout', 'release'])
    parser.add_argument('--config', type=Path, required=True)
    parser.add_argument('--state-dir', type=Path, required=True)
    parser.add_argument('--email', default=os.environ.get('LAUNCH_DEMO_EMAIL', ''))
    parser.add_argument('--feature', default='export')
    parser.add_argument('--output', type=Path, default=Path('protected-report.csv'))
    parser.add_argument('--metered', action='store_true', help='Use the official SDK reserve/commit flow')
    parser.add_argument('--operation-id', default='', help='Retain and reuse the same UUID for a metered retry')
    parser.add_argument('--licentivo-origin', default='', help='Explicit self-hosted Licentivo origin for isolated acceptance')
    args = parser.parse_args()
    public = json.loads(args.config.read_text(encoding='utf-8-sig'))
    state = args.state_dir.resolve()
    state.mkdir(mode=0o700, parents=True, exist_ok=True)
    with connect(public, state, args.licentivo_origin) as app:
        if args.action in ('logout', 'release'):
            confirmed = app.release_device() if args.action == 'release' else app.logout()
            print(json.dumps({'action': args.action, 'confirmed': bool(confirmed)}))
            return 0 if confirmed else 2
        if args.action == 'login':
            email = args.email or input('Email: ').strip()
            password = os.environ.get('LAUNCH_DEMO_PASSWORD') or getpass.getpass()
            app.login(email, password)
        else:
            restored = app.restore()
            if not restored:
                print(json.dumps({'action': args.action, 'restored': False, 'code': 'LOGIN_REQUIRED'}))
                return 3
        # Force an online refresh so an old offline grant cannot pass acceptance.
        app.runtime.refresh_online()
        status = app.runtime.status()
        result = {'action': args.action, 'state': status['state'], 'allowed': status['allowed'],
                  'code': status['code'], 'device': status.get('device', {}),
                  'lease_valid_until': status['lease_valid_until']}
        if args.action == 'export':
            business = {}
            def callback():
                business.update(export_csv(args.output))
                return dict(business)
            if args.metered:
                operation = args.operation_id
                if not operation or str(uuid.UUID(operation)) != operation:
                    raise ValueError('Metered export requires a retained UUID --operation-id')
                app.runtime.run_metered_feature(args.feature, 1, operation, callback)
                result['operation_id'] = operation
            else:
                app.run_feature(args.feature, callback)
            result['callback_executed'] = bool(business)
            result['business'] = business
        print(json.dumps(result, ensure_ascii=True))
        return 0


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        # No arbitrary server response, credentials or tokens in command output.
        code = getattr(error, 'code', '') or type(error).__name__
        print(json.dumps({'error': code, 'request_id': getattr(error, 'request_id', '')}), file=sys.stderr)
        sys.exit(1)
