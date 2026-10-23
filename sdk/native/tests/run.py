"""Local signed-license fixture for native lifecycle drivers; no payment services."""
import argparse
import base64
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import time
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding, PublicFormat

parser = argparse.ArgumentParser()
parser.add_argument('--c', required=True, type=Path)
parser.add_argument('--cpp', required=True, type=Path)
args = parser.parse_args()
private = Ed25519PrivateKey.generate()
b64 = lambda value: base64.urlsafe_b64encode(value).decode().rstrip('=')
public = b64(private.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw))
product = '22222222-2222-4222-8222-222222222222'
launch_product = '11111111-1111-4111-8111-111111111111'
state = {'mode': 'online', 'user': 0, 'credential_calls': 0}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_): pass
    def reply(self, status, body, retry_after=None):
        payload = json.dumps(body).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        if retry_after is not None: self.send_header('Retry-After', str(retry_after))
        self.end_headers()
        self.wfile.write(payload)
    def error(self, code, status=403): self.reply(status, {'error': {'code': code}}, 3 if code == 'ENTITLEMENT_PENDING' else None)
    def data(self, value): self.reply(200, {'data': value})
    def do_GET(self):
        if self.path.startswith('/api/v1/fixture/'):
            if state['mode'] == 'pending_server_error': assert state['credential_calls'] == 1, state
            if state['mode'] == 'ENTITLEMENT_PENDING':
                assert state['credential_calls'] == 3, state
                assert time.monotonic() - state['pending_started'] >= 5.9, state
            state['mode'] = self.path.rsplit('/', 1)[1]
            state['credential_calls'] = 0
            state['pending_started'] = time.monotonic()
            return self.data({})
        if state['mode'] == 'offline': return self.error('SERVICE_UNAVAILABLE', 503)
        if state['mode'] == 'denied': return self.error('UNAUTHORIZED', 401)
        if self.path == '/api/v1/me': return self.data({'user': {'id': str(state['user'])}})
        self.error('NOT_FOUND', 404)
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))) or '{}')
        if state['mode'] == 'offline': return self.error('SERVICE_UNAVAILABLE', 503)
        if self.path == '/api/v1/auth/login':
            state['user'] += 1
            return self.data({'access_token': 'secret-bearer', 'expires_at': '2099-01-01T00:00:00Z', 'user': {'id': str(state['user'])}})
        if self.path == '/api/v1/auth/logout': return self.data({})
        if self.path.endswith('/credential'):
            state['credential_calls'] += 1
            if state['mode'] == 'pending_server_error': return self.error('ENTITLEMENT_PENDING', 503)
            if state['mode'].startswith('ENTITLEMENT_'): return self.error(state['mode'], 404 if state['mode'] == 'ENTITLEMENT_NOT_PURCHASED' else 403)
            credential = {'product_id': launch_product, 'license_key': 'lv_lic_' + 'A' * 43, 'version': 1, 'valid_until': '2099-01-01T00:00:00Z'}
            if state['mode'] == 'bad_product': credential['product_id'] = product
            if state['mode'] == 'bad_key': credential['license_key'] = ''
            if state['mode'] == 'bad_version': credential['version'] = 0
            if state['mode'] == 'missing_version': del credential['version']
            if state['mode'] == 'fraction_version': credential['version'] = 1.5
            return self.data(credential)
        if self.path in ('/api/v1/activate', '/api/v1/validate', '/api/v1/heartbeat'):
            now = int(time.time())
            claims = {'version': 1, 'key_id': 'fixture', 'tenant_id': '33333333-3333-4333-8333-333333333333',
                      'product_id': product, 'license_id': '44444444-4444-4444-8444-444444444444',
                      'device_id': body['device_id'], 'activation_id': '55555555-5555-4555-8555-555555555555',
                      'mode': 'timed', 'features': ['export'], 'max_devices': 2, 'issued_at': now, 'expires_at': now + 3600,
                      'runtime_policy': {'offline_mode': 'limited', 'heartbeat_seconds': 0, 'offline_seconds': 3600}}
            payload = json.dumps(claims, separators=(',', ':')).encode()
            lease = {'version': 1, 'key_id': 'fixture', 'payload': b64(payload), 'signature': b64(private.sign(b'LicenovaLease/v1\n' + payload))}
            return self.data({'lease': lease, 'activation_id': claims['activation_id'], 'heartbeat_seconds': 0, 'device_key': 'lv_dev_' + 'B' * 43})
        if self.path == '/api/v1/deactivate': return self.data({})
        self.error('NOT_FOUND', 404)


server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
origin = f'http://127.0.0.1:{server.server_port}'
try:
    with tempfile.TemporaryDirectory(prefix='launch-native-fixture-') as directory:
        config = Path(directory) / 'product.json'
        config.write_text(json.dumps({'base_url': origin, 'product_id': product, 'trusted_keys': {'fixture': public}, 'allow_http': True}), encoding='utf-8')
        environment = os.environ | {'LOCALAPPDATA': directory, 'XDG_CACHE_HOME': directory}
        subprocess.run([str(args.c.resolve()), origin, str(config)], check=True, timeout=120, env=environment)
        state['mode'] = 'online'
        subprocess.run([str(args.cpp.resolve()), origin, str(config)], check=True, timeout=60, env=environment)
        print('C and C++ package lifecycle passed with the real official signed SDK.')
finally:
    server.shutdown()
    server.server_close()
