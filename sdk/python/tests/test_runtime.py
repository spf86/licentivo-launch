import base64
import json
from pathlib import Path
import tempfile
import time
import unittest
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives.serialization import Encoding,PublicFormat
from licentivo_launch._vendor.licentivo import Client, verify

class OfficialDeviceDetailsTests(unittest.TestCase):
    def test_device_details_reach_status_and_account_exchange(self):
        key = Ed25519PrivateKey.generate()
        public = base64.urlsafe_b64encode(key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)).decode().rstrip('=')
        with tempfile.TemporaryDirectory(prefix='launch-device-details-') as directory:
            client = Client(dict(product_id='11111111-1111-4111-8111-111111111111', trusted_keys={'key': public}, identity_token='test-identity', cache_path=str(Path(directory) / 'lease.json')))
            try:
                info = client.status()['device']
                self.assertTrue(info['name'].strip())
                self.assertRegex(info['code'], r'^[A-F0-9]{4}-[A-F0-9]{4}$')
                self.assertTrue(info['platform'] and info['system_type'])
                self.assertNotIn('device_id', info)
                body = client._body()
                self.assertEqual(body['device_name'], info['name'])
                self.assertEqual(body['device_info']['system_type'], info['system_type'])
                self.assertEqual(body['identity_token'], 'test-identity')
                self.assertEqual(body['license_key'], '')
            finally:
                client.close()

class OfficialSignatureTests(unittest.TestCase):
    def test_official_sdk_checks_signature(self):
        key=Ed25519PrivateKey.generate();encode=lambda raw:base64.urlsafe_b64encode(raw).decode().rstrip('=')
        product='11111111-1111-4111-8111-111111111111';device='a'*64;now=int(time.time())
        claims=dict(version=1,key_id='key',tenant_id=product,product_id=product,license_id=product,activation_id=product,device_id=device,mode='timed',features=['export'],max_devices=1,issued_at=now,expires_at=now+600)
        raw=json.dumps(claims,separators=(',',':')).encode();lease=dict(version=1,key_id='key',payload=encode(raw),signature=encode(key.sign(b'LicenovaLease/v1\n'+raw)))
        pins={'key':encode(key.public_key().public_bytes(Encoding.Raw,PublicFormat.Raw))}
        self.assertEqual(['export'],verify(lease,pins,product,device)['features'])
        with self.assertRaises(Exception): verify(lease|{'signature':'A'*86},pins,product,device)
