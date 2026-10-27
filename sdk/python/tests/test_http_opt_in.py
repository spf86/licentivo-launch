import tempfile
import unittest
from licentivo_launch import Application


class HTTPOptInTests(unittest.TestCase):
    def test_remote_http_requires_explicit_sandbox_opt_in(self):
        product = "11111111-1111-4111-8111-111111111111"
        remote_product = "22222222-2222-4222-8222-222222222222"
        keys = {"test-key": "A" * 43}
        for origin in ("http://192.168.1.20:18080", "http://customer-test-server:18080"):
            with self.assertRaises(ValueError):
                Application.connect(origin, product, remote_product, keys, environment="sandbox")
            with self.assertRaises(ValueError):
                Application.connect(origin, product, remote_product, keys, environment="live", allow_http=True)
            with tempfile.TemporaryDirectory() as cache:
                client = Application.connect(origin, product, remote_product, keys,
                                             environment="sandbox", allow_http=True, cache_dir=cache)
                client.close()
