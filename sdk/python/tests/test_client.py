import io
import json
import os
from pathlib import Path
import tempfile
import unittest
import urllib.error
import uuid
from datetime import datetime, timedelta, timezone
from licentivo_launch import Application, APIError, MemoryStore, WindowsDPAPIStore
PRODUCT='11111111-1111-4111-8111-111111111111'
REMOTE='22222222-2222-4222-8222-222222222222'
CONNECTION='33333333-3333-4333-8333-333333333333'
CONFIG={'product_id':REMOTE,'trusted_keys':{'key':'public'}}
TOKEN='ephemeral-identity-'*4
class Runtime:
    def __init__(self,config): self.config,self.released,self.closed=config,False,False
    def start(self): pass
    def run_feature(self,feature,action):
        if feature!='export': raise ValueError('SDK denied')
        return action()
    def logout(self):
        self.released=True
        Path(self.config['cache_path']+'.logout.json').write_text('SDK-owned journal')
        return {'local_complete':True,'revocation_pending':False}
    def close(self): self.closed=True
class Merchant:
    def __init__(self): self.requests,self.offline,self.logins,self.block_token,self.token_prefix=[],False,0,None,'account-'+uuid.uuid4().hex+'-'
    def open(self,request,timeout):
        self.requests.append(request)
        if request.full_url.endswith('/auth/logout') and self.block_token and request.get_header('Authorization') == 'Bearer ' + self.block_token: raise urllib.error.URLError('old logout unavailable')
        if self.offline: raise urllib.error.URLError('offline')
        if request.full_url.endswith('/credential'): raise AssertionError('legacy key endpoint')
        if request.full_url.endswith('/auth/login'):
            self.logins += 1
            data={'access_token':self.token_prefix+str(self.logins),'user':{'id':'alice'},'expires_at':(datetime.now(timezone.utc)+timedelta(hours=1)).isoformat()}
        elif request.full_url.endswith('/me'): data={'user':{'id':'alice'}}
        elif request.full_url.endswith('/identity'): data={'product_id':PRODUCT,'licentivo_product_id':REMOTE,'environment':'sandbox','launch_connection_id':CONNECTION,'identity_token':TOKEN,'expires_at':(datetime.now(timezone.utc)+timedelta(minutes=1)).isoformat(),'license_key':'must-not-be-persisted'}
        else: data={}
        return io.BytesIO(json.dumps({'data':data}).encode())
class FaultStore(MemoryStore):
    fail_saves = False
    fail_deletes = False
    def save(self, key, value):
        if self.fail_saves: raise OSError('atomic account store save failed')
        super().save(key,value)
    def delete(self, key):
        if self.fail_deletes: raise OSError('account store delete failed')
        super().delete(key)
class LifecycleTests(unittest.TestCase):
    def setUp(self): self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.merchant=Merchant();self.store=MemoryStore();self.runtimes=[]
    def app(self):
        def factory(config):
            runtime=Runtime(config);self.runtimes.append(runtime);return runtime
        return Application('https://merchant.example',PRODUCT,CONFIG,'sandbox',store=self.store,opener=self.merchant,cache_dir=self.temp.name,runtime_factory=factory)
    def test_ephemeral_identity_and_sdk_feature_decision(self):
        app=self.app();app.login('a','b');saved=self.store.load(app.scope)
        self.assertNotIn(TOKEN.encode(),saved);self.assertNotIn(b'license_key',saved)
        self.assertEqual('',self.runtimes[0].config['license_key']);self.assertEqual(TOKEN,self.runtimes[0].config['identity_token'])
        self.assertEqual(42,app.run_feature('export',lambda:42))
        with self.assertRaises(ValueError): app.run_feature('other',lambda:self.fail('bypassed'))
        app.close()
    def test_logout_releases_and_preserves_sdk_journal(self):
        app=self.app();app.login('a','b');path=app.cache_path;self.assertTrue(app.logout());self.assertTrue(self.runtimes[0].released)
        self.assertIsNone(self.store.load(app.scope));self.assertTrue(Path(str(path)+'.logout.json').exists())
        with self.assertRaises(APIError): app.run_feature('export',lambda:None)
    def test_fresh_process_logout_reopens_sdk_without_identity(self):
        app=self.app();app.login('a','b');app.close();self.assertFalse(self.runtimes[0].released)
        other=self.app();self.assertTrue(other.logout());self.assertTrue(self.runtimes[1].released);self.assertEqual('',self.runtimes[1].config['identity_token'])
    def test_account_logout_survives_restart_without_login_restore(self):
        app=self.app();app.login('a','b');self.merchant.offline=True
        self.assertFalse(app.logout())
        self.assertIsNone(app.session)
        self.assertTrue(json.loads(self.store.load(app.scope))['logout_pending'])
        with self.assertRaises(APIError): app.run_feature('export',lambda:self.fail('signed-out work'))
        app.close();other=self.app();before=len(self.merchant.requests)
        with self.assertRaises(APIError) as failure: other.restore()
        self.assertEqual('ACCOUNT_LOGOUT_PENDING',failure.exception.code)
        self.assertTrue(all(r.full_url.endswith('/auth/logout') for r in self.merchant.requests[before:]))
        self.merchant.offline=False;before=len(self.merchant.requests)
        self.assertFalse(other.restore())
        self.assertTrue(any(r.full_url.endswith('/auth/logout') for r in self.merchant.requests[before:]))
        self.assertIsNone(self.store.load(other.scope));other.close()
    def test_launch_outage_does_not_decide_offline_permission(self):
        app=self.app();app.login('a','b');app.close();self.merchant.offline=True
        with self.assertRaises(APIError) as error: self.app().restore()
        self.assertEqual('NETWORK_UNAVAILABLE',error.exception.code);self.assertEqual(1,len(self.runtimes))
    def test_identity_context_mismatch_cannot_open_sdk(self):
        app=self.app();app.config['product_id']='44444444-4444-4444-8444-444444444444'
        with self.assertRaises(APIError): app.login('a','b')
        self.assertFalse(self.runtimes)
    def test_store_save_failure_and_offline_logout_cannot_restore_old_login(self):
        self.store=FaultStore();app=self.app();app.login('a','b');scope=app.scope
        self.store.fail_saves=True;self.merchant.offline=True
        with self.assertRaises(APIError) as error: app.logout()
        self.assertEqual('ACCOUNT_LOGOUT_STORAGE_FAILED',error.exception.code)
        self.assertIsNone(app.runtime);self.assertIsNone(self.store.load(scope));app.close()
        self.store.fail_saves=False;other=self.app();before=len(self.merchant.requests)
        self.assertFalse(other.restore());self.assertEqual(before,len(self.merchant.requests));other.close()
    def test_total_storage_failure_reports_error_and_stops_locally(self):
        self.store=FaultStore();app=self.app();app.login('a','b')
        self.store.fail_saves=self.store.fail_deletes=True;self.merchant.offline=True
        with self.assertRaises(APIError) as error: app.logout()
        self.assertEqual('ACCOUNT_LOGOUT_STORAGE_FAILED',error.exception.code);self.assertIsNone(app.runtime);app.close()
        other=self.app();before=len(self.merchant.requests)
        with self.assertRaises(APIError) as error: other.restore()
        self.assertEqual('ACCOUNT_LOGOUT_STORAGE_FAILED',error.exception.code);self.assertTrue(all(r.full_url.endswith('/auth/logout') for r in self.merchant.requests[before:]));other.close()
    def test_completed_old_logout_never_adopts_newer_login(self):
        old=self.app();old.login('a','b');self.assertTrue(old.logout());new=self.app();new.login('a','b');token=new.session['access_token']
        self.assertTrue(old.logout());self.assertEqual(token,json.loads(self.store.load(new.scope))['access_token']);self.assertTrue(new.restore());old.close();new.close()
    def test_stale_instance_preserves_new_login_and_its_own_retry(self):
        a=self.app();a.login('a','b');old=a.session['access_token'];b=self.app();b.login('a','b');new=b.session['access_token']
        self.merchant.block_token=old;self.assertFalse(a.logout())
        self.assertEqual(new,json.loads(self.store.load(b.scope))['access_token'])
        self.assertFalse(json.loads(self.store.load(b.scope)).get('logout_pending',False))
        a.close();self.merchant.block_token=None;c=self.app();count=len(self.runtimes)
        self.assertTrue(c.restore());self.assertEqual(count+1,len(self.runtimes))
        self.assertEqual(new,c.session['access_token']);self.assertIsNone(self.store.load(c._pending_key()));c.close();b.close()
    @unittest.skipUnless(os.name=='nt','Windows DPAPI')
    def test_dpapi_integrity_and_confidentiality(self):
        store=WindowsDPAPIStore(self.temp.name);secret=b'account-secret';store.save('scope',secret)
        self.assertEqual(secret,store.load('scope'));path=next(Path(self.temp.name).glob('*.dpapi'));self.assertNotIn(secret,path.read_bytes())
        path.write_bytes(b'corrupt')
        with self.assertRaises(OSError): store.load('scope')
if __name__=='__main__': unittest.main()
