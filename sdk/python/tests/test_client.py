import io
import json
import os
from pathlib import Path
import tempfile
import threading
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
    def __init__(self): self.requests,self.offline,self.logins,self.block_token,self.token_prefix=[],False,0,None,'account-'+uuid.uuid4().hex+'-';self.logout_status=200;self.transport_denial=None
    me_data={'user':{'id':'alice'}}
    def open(self,request,timeout):
        self.requests.append(request)
        if request.full_url.endswith('/auth/logout') and self.block_token and request.get_header('Authorization') == 'Bearer ' + self.block_token: raise urllib.error.URLError('old logout unavailable')
        if self.offline: raise urllib.error.URLError('offline')
        if request.full_url.endswith('/me') and self.transport_denial:
            raise urllib.error.HTTPError(request.full_url,403,'Forbidden',{},io.BytesIO(json.dumps({'error':{'code':self.transport_denial}}).encode()))
        if request.full_url.endswith('/credential'): raise AssertionError('legacy key endpoint')
        if request.full_url.endswith('/auth/login'):
            self.logins += 1
            data={'access_token':self.token_prefix+str(self.logins),'user':{'id':'alice'},'expires_at':(datetime.now(timezone.utc)+timedelta(hours=1)).isoformat()}
        elif request.full_url.endswith('/me'): data=self.me_data
        elif request.full_url.endswith('/identity'): data={'product_id':PRODUCT,'licentivo_product_id':REMOTE,'environment':'sandbox','launch_connection_id':CONNECTION,'identity_token':TOKEN,'expires_at':(datetime.now(timezone.utc)+timedelta(minutes=1)).isoformat(),'license_key':'must-not-be-persisted'}
        else: data={}
        response=io.BytesIO(json.dumps({'data':data}).encode())
        response.status=self.logout_status if request.full_url.endswith('/auth/logout') else 200
        return response
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
    def test_close_during_startup_cannot_leave_an_active_runtime(self):
        for operation in ('login','restore'):
            with self.subTest(operation=operation):
                if operation=='restore':
                    seed=self.app();seed.login('a','b');seed.close()
                app=self.app();self.addCleanup(app.close)
                started=threading.Event();release=threading.Event()
                close_attempted=threading.Event();close_done=threading.Event();failures=[]
                class BlockingRuntime(Runtime):
                    def start(self):
                        started.set()
                        if not release.wait(5): raise RuntimeError('startup was not released')
                def factory(config):
                    runtime=BlockingRuntime(config);self.runtimes.append(runtime);return runtime
                app.runtime_factory=factory
                def start_application():
                    try: app.login('a','b') if operation=='login' else app.restore()
                    except BaseException as error: failures.append(error)
                def close_application():
                    close_attempted.set()
                    try: app.close()
                    except BaseException as error: failures.append(error)
                    finally: close_done.set()
                startup=threading.Thread(target=start_application,daemon=True)
                closer=threading.Thread(target=close_application,daemon=True)
                startup.start()
                try:
                    self.assertTrue(started.wait(5),'SDK startup did not begin')
                    closer.start();self.assertTrue(close_attempted.wait(5))
                    close_done.wait(0.1)
                finally:
                    release.set();startup.join(5)
                    if closer.ident is not None: closer.join(5)
                self.assertFalse(startup.is_alive());self.assertFalse(closer.is_alive())
                self.assertEqual([],failures)
                self.assertTrue(app.closed);self.assertIsNone(app.runtime);self.assertIsNone(app.session)
                self.assertTrue(self.runtimes[-1].closed)
                self.assertFalse(self.runtimes[-1].released)
                self.assertIsNotNone(self.store.load(app.scope),'ordinary close lost remembered login')
                with self.assertRaises(APIError):
                    app.run_feature('export',lambda:self.fail('closed application executed protected work'))
    def test_close_failure_still_disables_application(self):
        app=self.app();self.addCleanup(app.close);app.login('a','b');runtime=self.runtimes[-1];calls=[]
        def failing_close():
            calls.append('close');runtime.closed=True
            raise OSError('SDK shutdown failed')
        runtime.close=failing_close
        with self.assertRaises(OSError): app.close()
        self.assertTrue(app.closed);self.assertIsNone(app.runtime);self.assertIsNone(app.session)
        with self.assertRaises(APIError):
            app.run_feature('export',lambda:self.fail('failed close retained protected work'))
        app.close();self.assertEqual(['close'],calls)
        self.assertIsNotNone(self.store.load(app.scope));self.assertFalse(runtime.released)
    def test_logout_still_revokes_account_when_sdk_close_fails(self):
        app=self.app();self.addCleanup(app.close);app.login('a','b');runtime=self.runtimes[-1]
        token=app.session['access_token'];calls=[]
        def failing_close():
            calls.append('close');runtime.closed=True
            raise OSError('SDK shutdown failed')
        runtime.close=failing_close
        with self.assertRaises(OSError): app.logout()
        self.assertTrue(runtime.released);self.assertEqual(['close'],calls)
        self.assertIsNone(app.runtime);self.assertIsNone(app.session)
        self.assertIsNone(self.store.load(app.scope),'SDK close failure prevented account cleanup')
        self.assertTrue(self.merchant.requests[-1].full_url.endswith('/auth/logout'))
        self.assertEqual('Bearer '+token,self.merchant.requests[-1].get_header('Authorization'))
        self.assertTrue(app.logout(),'completed logout could not be retried')
        with self.assertRaises(APIError):
            app.run_feature('export',lambda:self.fail('failed logout retained protected work'))
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
    def test_accepted_logout_is_not_confirmed_and_survives_restart(self):
        app=self.app();self.addCleanup(app.close);app.login('a','b');self.merchant.logout_status=202
        self.assertFalse(app.logout())
        self.assertTrue(json.loads(self.store.load(app.scope))['logout_pending'])
        self.assertIsNone(app.runtime);app.close();other=self.app();self.addCleanup(other.close)
        before=len(self.merchant.requests)
        with self.assertRaises(APIError) as error: other.restore()
        self.assertEqual('ACCOUNT_LOGOUT_PENDING',error.exception.code)
        self.merchant.logout_status=200;self.assertFalse(other.restore())
        self.assertTrue(all(r.full_url.endswith('/auth/logout') for r in self.merchant.requests[before:]))
        self.assertIsNone(self.store.load(other.scope))
    def test_transport_policy_denial_preserves_account_for_logout(self):
        for code in ('SDK_HTTP_DISABLED','SDK_HTTPS_REQUIRED'):
            with self.subTest(code=code):
                app=self.app();self.addCleanup(app.close);app.login('a','b')
                saved=self.store.load(app.scope);count=len(self.runtimes);self.merchant.transport_denial=code
                with self.assertRaises(APIError) as error: app.restore()
                self.assertEqual(code,error.exception.code);self.assertEqual(saved,self.store.load(app.scope))
                self.assertEqual(count,len(self.runtimes));self.assertIsNone(app.runtime)
                self.assertTrue(app.logout());self.assertIsNone(self.store.load(app.scope));self.merchant.transport_denial=None
    def test_malformed_current_user_preserves_account_for_logout(self):
        for body in ({},{'user':None},{'user':{}},{'user':{'id':''}},{'user':{'id':None}},
                     {'user':{'id':42}},{'user':[]},{'user':'invalid'}):
            with self.subTest(body=body):
                app=self.app();self.addCleanup(app.close);app.login('a','b')
                saved=self.store.load(app.scope);token=app.session['access_token'];count=len(self.runtimes)
                self.merchant.me_data=body
                with self.assertRaises(APIError) as error: app.restore()
                self.assertEqual('INVALID_RESPONSE',error.exception.code)
                self.assertEqual(saved,self.store.load(app.scope));self.assertEqual(count,len(self.runtimes))
                self.assertIsNone(app.runtime)
                with self.assertRaises(APIError):
                    app.run_feature('export',lambda:self.fail('failed restore executed work'))
                self.assertTrue(app.logout())
                logout=self.merchant.requests[-1]
                self.assertTrue(logout.full_url.endswith('/auth/logout'))
                self.assertEqual('Bearer '+token,logout.get_header('Authorization'))
                self.assertIsNone(self.store.load(app.scope))
    def test_different_current_user_still_clears_account(self):
        app=self.app();self.addCleanup(app.close);app.login('a','b');self.merchant.me_data={'user':{'id':'bob'}}
        with self.assertRaises(APIError) as error: app.restore()
        self.assertEqual('SESSION_REJECTED',error.exception.code)
        self.assertIsNone(self.store.load(app.scope));self.assertIsNone(app.runtime)
    def test_launch_outage_does_not_decide_offline_permission(self):
        app=self.app();app.login('a','b');app.close();self.merchant.offline=True
        with self.assertRaises(APIError) as error: self.app().restore()
        self.assertEqual('NETWORK_UNAVAILABLE',error.exception.code);self.assertEqual(1,len(self.runtimes))
    def test_restore_storage_failure_stops_the_started_runtime(self):
        for fail_deletes in (False, True):
            with self.subTest(fail_deletes=fail_deletes):
                self.store=FaultStore()
                app=self.app();self.addCleanup(app.close);app.login('a','b');app.close()
                restored=self.app();self.addCleanup(restored.close)
                self.store.fail_saves=True;self.store.fail_deletes=fail_deletes
                with self.assertRaises(OSError): restored.restore()
                self.assertTrue(self.runtimes[-1].closed)
                self.assertIsNone(restored.runtime)
                self.assertIsNone(restored.session)
                with self.assertRaises(APIError) as error:
                    restored.run_feature('export',lambda:self.fail('failed restore executed protected work'))
                self.assertEqual('LOGIN_REQUIRED',error.exception.code)
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
