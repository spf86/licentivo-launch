"""Merchant account login -> own credential -> SDK signed feature check."""
import argparse
import getpass
import hashlib
import json
import os
from pathlib import Path
import sys
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'vendor/sdk/python'))
from licentivo import Client


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Merchant:
    def __init__(self, origin, environment):
        parsed = urllib.parse.urlsplit(origin)
        if (not parsed.hostname or parsed.username or parsed.password or parsed.path not in ('', '/')
                or parsed.query or parsed.fragment or parsed.scheme not in ('https', 'http')):
            raise ValueError('Merchant URL must be an origin')
        if parsed.scheme != 'https' and not (environment == 'sandbox' and parsed.hostname in ('localhost', '127.0.0.1', '::1')):
            raise ValueError('HTTPS required')
        self.origin, self.token = origin.rstrip('/'), ''
        self.http = urllib.request.build_opener(NoRedirect())

    def call(self, path, body):
        headers = {'Content-Type': 'application/json'}
        if self.token:
            headers['Authorization'] = 'Bearer ' + self.token
        request = urllib.request.Request(self.origin + '/api/v1' + path,
                                         json.dumps(body).encode(), headers, method='POST')
        try:
            with self.http.open(request, timeout=15) as response:
                raw = response.read(1048577)
                if len(raw) > 1048576:
                    raise ValueError('Response too large')
                return json.loads(raw)['data']
        except urllib.error.HTTPError as error:
            # Preserve only the safe business code, never a raw response/key.
            raw = error.read(1048576)
            code = json.loads(raw).get('error', {}).get('code', 'REQUEST_FAILED')
            raise RuntimeError('Launch: ' + code) from None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('merchant_origin')
    parser.add_argument('launch_product_id')
    parser.add_argument('product_config', type=Path)
    parser.add_argument('--environment', choices=('sandbox', 'live'), default='live')
    parser.add_argument('--sign-out', action='store_true', help='Clear only this account SDK cache at exit')
    parser.add_argument('--release-device', action='store_true', help='Explicitly deactivate this device')
    args = parser.parse_args()
    merchant = Merchant(args.merchant_origin, args.environment)
    grant = merchant.call('/auth/login', {'email': input('Email: ').strip(),
                                        'password': getpass.getpass(), 'kind': 'desktop'})
    merchant.token = grant['access_token']
    cache_path = None
    try:
        credential = merchant.call('/software/products/' + urllib.parse.quote(args.launch_product_id, safe='') + '/credential',
                                   {'environment': args.environment})
        if credential['product_id'] != args.launch_product_id:
            raise ValueError('Credential product mismatch')
        config = json.loads(args.product_config.read_text(encoding='utf-8'))
        scope = hashlib.sha256(('\n'.join((merchant.origin, grant['user']['id'],
                                         args.launch_product_id, args.environment))).encode()).hexdigest()
        cache_root = Path(os.getenv('LOCALAPPDATA') or os.getenv('XDG_CACHE_HOME') or Path.home() / '.cache')
        account_dir = cache_root / 'LicentivoLaunch' / scope
        account_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        cache_path = account_dir / 'lease.json'
        config.update(license_key=credential['license_key'], cache_path=str(cache_path))
        with Client(config) as client:
            client.start(lambda _: print('Authorization changed; refresh application status', file=sys.stderr))
            client.run_feature('export', lambda: Path('licensed-report.txt').write_text('Protected report\n', encoding='utf-8'))
            print('Protected export completed')
            if args.release_device:
                client.deactivate()
    finally:
        try:
            merchant.call('/auth/logout', {})
        finally:
            merchant.token = ''
            if args.sign_out and cache_path:
                cache_path.unlink(missing_ok=True)
                Path(str(cache_path) + '.credential.json').unlink(missing_ok=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(str(error) + '\nView purchases and authorization status in the merchant account center.', file=sys.stderr)
        sys.exit(1)
