import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,rm,writeFile,readFile,readdir} from 'node:fs/promises';
import {tmpdir} from 'node:os';import {join} from 'node:path';
import {Application,MemoryStore,EncryptedFileStore,LaunchError,validateConfig} from '../src/index.mjs';
import {randomUUID} from 'node:crypto';
const launch={schema_version:1,merchant_origin:'https://merchant.example',launch_product_id:'11111111-1111-4111-8111-111111111111',licentivo_product_id:'22222222-2222-4222-8222-222222222222',environment:'sandbox'};
const product={product_id:launch.licentivo_product_id,trusted_keys:{pin:Buffer.alloc(32,1).toString('base64url')}};
const user={id:'33333333-3333-4333-8333-333333333333'};
const identity={product_id:launch.launch_product_id,licentivo_product_id:product.product_id,launch_connection_id:'44444444-4444-4444-8444-444444444444',environment:'sandbox',identity_token:'EPHEMERAL-IDENTITY-'.repeat(4),expires_at:new Date(Date.now()+60000).toISOString()};
const reply=data=>({status:200,body:JSON.stringify({data}),headers:{}});
class Store extends MemoryStore{record;scope;failSaves=false;async save(scope,value){if(this.failSaves)throw new LaunchError('TEST_STORAGE_FAILED');this.record=structuredClone(value);this.scope=scope;return super.save(scope,value);}}
async function fixture(t){const root=await mkdtemp(join(tmpdir(),'launch-identity-'));t.after(()=>rm(root,{recursive:true,force:true}));const store=new Store(),calls=[],runtimes=[];const token=randomUUID().replaceAll('-','')+'T'.repeat(11);let offline=false,pending=false;const transport=async request=>{calls.push(request);if(offline)throw new LaunchError('NETWORK_UNAVAILABLE');assert.equal(request.url.endsWith('/credential'),false);if(request.url.endsWith('/auth/login'))return reply({user,access_token:token,expires_at:new Date(Date.now()+3600000).toISOString()});if(request.url.endsWith('/me'))return reply({user});if(request.url.endsWith('/identity'))return reply(identity);return reply({});};const runtimeFactory=config=>{const runtime={config,released:0,closed:0,start:async()=>{},runFeature:async(f,a)=>{if(f!=='export')throw new LaunchError('SDK_DENIED');return a();},logout:async()=>{runtime.released++;await writeFile(config.cache_path+'.logout.json','SDK-owned journal');return {local_complete:true,revocation_pending:pending};},deactivate:async()=>{},close:async()=>{runtime.closed++;}};runtimes.push(runtime);return runtime;};const options={store,transport,runtimeFactory,cacheDir:root};const app=new Application(launch,product,options);t.after(()=>app.close());return {app,options,store,runtimes,calls,setOffline:value=>offline=value,setPending:value=>pending=value};}
test('identity is ephemeral and feature permission comes from SDK',async t=>{const f=await fixture(t);await f.app.login('user','password');assert.equal(f.runtimes[0].config.license_key,'');assert.equal(f.runtimes[0].config.identity_token,identity.identity_token);assert.equal(JSON.stringify(f.store.record).includes(identity.identity_token),false);assert.equal(await f.app.runFeature('export',()=>42),42);await assert.rejects(f.app.runFeature('other',()=>assert.fail('bypassed')),{code:'SDK_DENIED'});});

test('startup denial survives failing runtime shutdown and logout reconstruction',async t=>{
 for(const code of ['LAUNCH_ENTITLEMENT_REQUIRED','DEVICE_LIMIT_REACHED'])for(const brokenClose of [false,true])for(const offlineLogout of [false,true]){
  const f=await fixture(t),denied=new LaunchError(code,403);let closed=false;
  const app=new Application(launch,product,{...f.options,runtimeFactory:config=>{
   if(!config.identity_token)throw new Error('no device credential was issued');
   return {start:async()=>{if(offlineLogout)f.setOffline(true);throw denied;},close:async()=>{closed=true;if(brokenClose)throw new Error('SDK shutdown failed');}};
  }});
  t.after(()=>app.close());
  await assert.rejects(app.login('a','b'),error=>error===denied);
  assert.equal(closed,true);
  assert.ok(f.calls.some(r=>r.url.endsWith('/auth/logout')));
  await assert.rejects(app.runFeature('export',()=>assert.fail('denied startup allowed business')),{code:'LOGIN_REQUIRED'});
  if(offlineLogout){assert.equal(f.store.record.records[0].logout_pending,true);assert.equal(JSON.stringify(f.store.record).includes(identity.identity_token),false);}
  else assert.equal(await f.store.load(f.store.scope),null);
 }
});

test('account logout survives restart and cannot be restored as a login',async t=>{
  const f=await fixture(t);await f.app.login('a','b');f.setOffline(true);
  assert.equal(await f.app.logout(),false);assert.equal(f.store.record.logout_pending,true);
  await assert.rejects(f.app.runFeature('export',()=>assert.fail('signed-out work')),{code:'LOGIN_REQUIRED'});
  await f.app.close();const restarted=new Application(launch,product,f.options);t.after(()=>restarted.close());
  const before=f.calls.length;await assert.rejects(restarted.restore(),{code:'ACCOUNT_LOGOUT_PENDING'});
  assert.ok(f.calls.slice(before).every(request=>request.url.endsWith('/auth/logout')));
  f.setOffline(false);const recovery=f.calls.length;assert.equal(await restarted.restore(),null);
  assert.ok(f.calls.slice(recovery).some(request=>request.url.endsWith('/auth/logout')));
  assert.equal(await f.store.load(f.store.scope),null);
});

test('already revoked account logout is confirmed by 401',async t=>{
  const f=await fixture(t);await f.app.login('a','b');await f.app.close();
  const restarted=new Application(launch,product,{...f.options,transport:async request=>request.url.endsWith('/auth/logout')?{status:401,body:JSON.stringify({error:{code:'UNAUTHENTICATED'}}),headers:{}}:f.options.transport(request)});
  t.after(()=>restarted.close());assert.equal(await restarted.logout(),true);assert.equal(await f.store.load(f.store.scope),null);
});

test('202 logout is retained across restart until confirmed by 200',async t=>{
  const f=await fixture(t);await f.app.login('a','b');await f.app.close();let status=202;
  const options={...f.options,transport:async request=>request.url.endsWith('/auth/logout')?{status,headers:{},body:JSON.stringify({data:{accepted:true}})}:f.options.transport(request)};
  const next=new Application(launch,product,options);t.after(()=>next.close());assert.equal(await next.logout(),false);
  const scope=f.store.scope;assert.equal((await f.store.load(scope)).logout_pending,true);await next.close();
  const restarted=new Application(launch,product,options);t.after(()=>restarted.close());const before=f.calls.length;
  await assert.rejects(restarted.restore(),{code:'ACCOUNT_LOGOUT_PENDING'});status=200;assert.equal(await restarted.restore(),null);
  assert.equal(f.calls.length,before);assert.equal(await f.store.load(scope),null);
});

test('malformed 200 logout bodies retain account retry records',async t=>{
  for(const invalid of ['null','{"data":null}','{"data":[]}','{"error":{"code":"UNEXPECTED"}}']){
    const f=await fixture(t);let body=invalid;
    const app=new Application(launch,product,{...f.options,transport:async request=>request.url.endsWith('/auth/logout')?{status:200,headers:{},body}:f.options.transport(request)});
    t.after(()=>app.close());await app.login('a','b');assert.equal(await app.logout(),false);
    await assert.rejects(app.runFeature('export',()=>assert.fail('signed-out work')),{code:'LOGIN_REQUIRED'});
    body=JSON.stringify({data:{completed:true}});assert.equal(await app.logout(),true);assert.equal(await app.restore(),null);
  }
});

test('transport policy denials preserve a valid account for explicit logout',async t=>{
  for(const code of ['SDK_HTTP_DISABLED','SDK_HTTPS_REQUIRED']){
    const f=await fixture(t);
    const app=new Application(launch,product,{...f.options,transport:async request=>request.url.endsWith('/me')?{status:403,headers:{},body:JSON.stringify({error:{code}})}:f.options.transport(request)});
    t.after(()=>app.close());await app.login('a','b');const scope=f.store.scope,saved=await f.store.load(scope),count=f.runtimes.length;
    await assert.rejects(app.restore(),{code,status:403});assert.deepEqual(await f.store.load(scope),saved);assert.equal(f.runtimes.length,count);
    await assert.rejects(app.runFeature('export',()=>assert.fail('configuration denial allowed work')),{code:'LOGIN_REQUIRED'});
    assert.equal(await app.logout(),true);assert.equal(await f.store.load(scope),null);
  }
});

test('malformed current user preserves account for explicit logout',async t=>{
  for(const data of [{},{user:null},{user:{}},{user:{id:''}},{user:{id:null}},{user:{id:42}},{user:[]},{user:'invalid'}]){
    const f=await fixture(t);let logoutBearer;
    const app=new Application(launch,product,{...f.options,transport:async request=>{
      if(request.url.endsWith('/me'))return reply(data);
      if(request.url.endsWith('/auth/logout'))logoutBearer=request.headers.Authorization;
      return f.options.transport(request);
    }});
    t.after(()=>app.close());await app.login('a','b');const scope=f.store.scope,saved=await f.store.load(scope),count=f.runtimes.length;
    await assert.rejects(app.restore(),{code:'INVALID_RESPONSE'});
    assert.deepEqual(await f.store.load(scope),saved);assert.equal(f.runtimes.length,count);
    await assert.rejects(app.runFeature('export',()=>assert.fail('failed restore executed work')),{code:'LOGIN_REQUIRED'});
    assert.equal(await app.logout(),true);assert.equal(logoutBearer,'Bearer '+saved.token);assert.equal(await f.store.load(scope),null);
  }
});

test('a different current user still clears the remembered account',async t=>{
  const f=await fixture(t);
  const app=new Application(launch,product,{...f.options,transport:request=>request.url.endsWith('/me')?reply({user:{id:'55555555-5555-4555-8555-555555555555'}}):f.options.transport(request)});
  t.after(()=>app.close());await app.login('a','b');const scope=f.store.scope;
  await assert.rejects(app.restore(),{code:'SESSION_MISMATCH'});assert.equal(await f.store.load(scope),null);
});
test('explicit logout releases device and retains durable SDK journal',async t=>{const f=await fixture(t);await f.app.login('a','b');const path=f.runtimes[0].config.cache_path;assert.equal(await f.app.logout(),true);assert.equal(f.runtimes[0].released,1);assert.equal(await readFile(path+'.logout.json','utf8'),'SDK-owned journal');assert.equal(await f.store.load(f.store.scope),null);await assert.rejects(f.app.runFeature('export',()=>0),{code:'LOGIN_REQUIRED'});});
test('ordinary close preserves login; fresh-process logout recovers SDK',async t=>{const f=await fixture(t);await f.app.login('a','b');await f.app.close();assert.equal(f.runtimes[0].released,0);const next=new Application(launch,product,f.options);assert.equal(await next.logout(),true);assert.equal(f.runtimes[1].released,1);assert.equal(f.runtimes[1].config.identity_token,'');await next.close();});
test('Launch outage never grants offline permission',async t=>{const f=await fixture(t);await f.app.login('a','b');f.setOffline(true);await assert.rejects(f.app.restore(),{code:'NETWORK_UNAVAILABLE'});assert.equal(f.runtimes.length,1);});
test('pending device release retries the SDK until confirmed, including after restart',async t=>{
  const f=await fixture(t);
  let retries=0, available=false;
  const openStore=()=>new EncryptedFileStore(join(f.options.cacheDir,'accounts'),Buffer.alloc(32,11));
  const options={...f.options,store:openStore(),runtimeFactory:config=>{
    const runtime=f.options.runtimeFactory(config);
    runtime.retryPendingLogouts=async()=>{retries++;if(!available)throw Error('device service offline');};
    return runtime;
  }};
  const app=new Application(launch,product,options);
  await app.login('a','b');
  const scope=['launch-account-v2',launch.merchant_origin,launch.launch_product_id,launch.licentivo_product_id,launch.environment].join('\n');
  const path=f.runtimes[0].config.cache_path;
  f.setPending(true);
  await assert.rejects(app.releaseDevice(),{code:'DEVICE_REVOCATION_PENDING'});
  assert.equal(await options.store.load(scope),null);
  await assert.rejects(app.releaseDevice(),{code:'DEVICE_REVOCATION_PENDING'});
  await app.close();
  const wrongCache=new Application(launch,product,{...options,cacheDir:join(f.options.cacheDir,'other'),store:openStore()});
  await assert.rejects(wrongCache.releaseDevice(),{code:'DEVICE_REVOCATION_PENDING'});
  assert.equal(retries,1);
  await wrongCache.close();
  const restarted=new Application(launch,product,{...options,store:openStore()});
  t.after(()=>restarted.close());
  await assert.rejects(restarted.releaseDevice(),{code:'DEVICE_REVOCATION_PENDING'});
  await assert.rejects(restarted.restore(),{code:'ACCOUNT_LOGOUT_PENDING'});
  await assert.rejects(restarted.runFeature('export',()=>assert.fail('restored a signed-out account')),{code:'LOGIN_REQUIRED'});
  available=true;
  await restarted.releaseDevice();
  assert.equal(retries,4);
  assert.ok(f.runtimes.every(r=>r.config.cache_path===path));
  assert.equal(f.calls.filter(r=>r.url.endsWith('/auth/logout')).length,1);
  assert.equal(await openStore().load(scope+'\naccount-logout-queue-v1'),null);
});
test('restore preserves the account across retryable SDK failures and clears definitive denials',async t=>{
  const f=await fixture(t);
  let failure;
  const app=new Application(launch,product,{...f.options,runtimeFactory:config=>{
    const runtime=f.options.runtimeFactory(config);
    runtime.start=async()=>{if(failure)throw failure;};
    return runtime;
  }});
  t.after(()=>app.close());
  await app.login('a','b');
  const scope=f.store.scope,saved=await f.store.load(scope);
  for(const error of [new LaunchError('NETWORK_UNAVAILABLE'),Object.assign(new TypeError('fetch failed'),{transient:true}),new LaunchError('SERVICE_UNAVAILABLE',503)]){
    failure=error;
    await assert.rejects(app.restore(),value=>value===error);
    assert.deepEqual(await f.store.load(scope),saved);
    await assert.rejects(app.runFeature('export',()=>assert.fail('failed restore allowed work')),{code:'LOGIN_REQUIRED'});
  }
  failure=null;
  assert.ok(await app.restore());
  failure=new LaunchError('LAUNCH_ENTITLEMENT_REVOKED',403);
  await assert.rejects(app.restore(),value=>value===failure);
  assert.equal(await f.store.load(scope),null);
});
test('failed device cleanup retries logout before trusting the ticket journal',async t=>{
  for(const failure of ['response_lost','storage_error']) {
    const f=await fixture(t);
    let outcome=failure, logouts=0, retries=0;
    const options={...f.options,runtimeFactory:config=>{
      const runtime=f.options.runtimeFactory(config);
      runtime.logout=async()=>{
        logouts++;
        if(outcome==='response_lost') throw new Error('lost response');
        if(outcome==='storage_error') return {local_complete:false,storage_error:'write failed',revocation_pending:false};
        return {local_complete:true,revocation_pending:true};
      };
      runtime.retryPendingLogouts=async()=>{retries++;};
      return runtime;
    }};
    const app=new Application(launch,product,options);
    await app.login('a','b');
    const scope=f.store.scope;
    await assert.rejects(app.releaseDevice());
    assert.equal(await f.store.load(scope),null);
    await app.close();
    const restart=new Application(launch,product,options);
    t.after(()=>restart.close());
    await assert.rejects(restart.releaseDevice());
    assert.equal(logouts,2);
    assert.equal(retries,0);
    outcome='ticket_saved';
    await assert.rejects(restart.releaseDevice(),{code:'DEVICE_REVOCATION_PENDING'});
    await restart.releaseDevice();
    assert.equal(logouts,3);
    assert.equal(retries,1);
    assert.equal(f.calls.filter(r=>r.url.endsWith('/auth/logout')).length,1);
  }
});
test('public configuration rejects private/runtime fields',()=>{assert.throws(()=>validateConfig(launch,{...product,license_key:'private'}),{code:'PUBLIC_CONFIG_INVALID'});assert.throws(()=>validateConfig({...launch,merchant_origin:'http://other.example'},product),{code:'INVALID_CONFIG'});});
test('account store authenticates and encrypts its own secrets',async t=>{const root=await mkdtemp(join(tmpdir(),'launch-identity-store-'));t.after(()=>rm(root,{recursive:true,force:true}));const store=new EncryptedFileStore(root,Buffer.alloc(32,7));await store.save('scope',{token:'secret-account-token'});assert.deepEqual(await store.load('scope'),{token:'secret-account-token'});const [name]=await readdir(root);assert.equal((await readFile(join(root,name))).includes('secret-account-token'),false);await writeFile(join(root,name),'corrupt');await assert.rejects(store.load('scope'),{code:'INVALID_SAVED_SESSION'});});

test('failed saves plus offline logout cannot resurrect the old account',async t=>{
  const f=await fixture(t);await f.app.login('a','b');const scope=f.store.scope;f.store.failSaves=true;f.setOffline(true);
  await assert.rejects(f.app.logout(),{code:'ACCOUNT_LOGOUT_STORAGE_FAILED'});assert.equal(await f.store.load(scope),null);
  await assert.rejects(f.app.runFeature('export',()=>assert.fail('signed-out callback')),{code:'LOGIN_REQUIRED'});
  await f.app.close();f.store.failSaves=false;const restarted=new Application(launch,product,f.options);t.after(()=>restarted.close());const before=f.calls.length;
  assert.equal(await restarted.restore(),null);assert.equal(f.calls.length,before);
});
test('a completed logout never adopts a newer stored login on retry',async t=>{
  const f=await fixture(t);await f.app.login('a','b');await f.app.logout();
  const newer=new Application(launch,product,f.options);t.after(()=>newer.close());await newer.login('a','b');const scope=f.store.scope;
  assert.equal(await f.app.logout(),true);assert.ok(await f.store.load(scope));assert.ok(await newer.restore());
});
test('a device release retry cannot deactivate a newer login using the same cache',async t=>{
  const f=await fixture(t);
  let grants=0;
  const options={...f.options,transport:request=>request.url.endsWith('/auth/login')
    ? reply({user,access_token:(++grants===1?'A':'B').repeat(43),expires_at:new Date(Date.now()+3600000).toISOString()})
    : f.options.transport(request)};
  const old=new Application(launch,product,options), newer=new Application(launch,product,options);
  t.after(()=>old.close());t.after(()=>newer.close());
  await old.login('a','b');await newer.login('a','b');
  const scope=f.store.scope, saved=await f.store.load(scope);
  f.setPending(true);
  await assert.rejects(old.releaseDevice(),{code:'DEVICE_REVOCATION_PENDING'});
  const count=f.runtimes.length;
  await assert.rejects(old.releaseDevice(),{code:'DEVICE_REVOCATION_PENDING'});
  assert.equal(f.runtimes.length,count);
  assert.deepEqual(await f.store.load(scope),saved);
  assert.ok(await newer.restore());
  f.setPending(false);
  assert.equal(await newer.logout(),false);
  const recovery=new Application(launch,product,options);
  t.after(()=>recovery.close());
  await recovery.releaseDevice();
  await newer.login('a','b');
  const current=await f.store.load(scope), countAfterRecovery=f.runtimes.length;
  await old.releaseDevice();
  assert.equal(f.runtimes.length,countAfterRecovery);
  assert.deepEqual(await f.store.load(scope),current);
});

test('late old instance retains its retry without overwriting the new login',async t=>{
  const f=await fixture(t);let login=0,blockOld=false;const old='A'.repeat(43),newer='B'.repeat(43);
  const options={...f.options,transport:async request=>{
    if(request.url.endsWith('/auth/login'))return reply({user,access_token:++login===1?old:newer,expires_at:new Date(Date.now()+3600000).toISOString()});
    if(request.url.endsWith('/auth/logout')&&request.headers.Authorization==='Bearer '+old&&blockOld)throw new LaunchError('NETWORK_UNAVAILABLE');
    return f.options.transport(request);
  }};
  const a=new Application(launch,product,options),b=new Application(launch,product,options);t.after(()=>a.close());t.after(()=>b.close());
  await a.login('a','b');await b.login('a','b');const scope=f.store.scope;blockOld=true;assert.equal(await a.logout(),false);
  assert.equal((await f.store.load(scope)).token,newer);assert.equal((await f.store.load(scope)).logout_pending,undefined);
  blockOld=false;await a.close();const c=new Application(launch,product,options);t.after(()=>c.close());const count=f.runtimes.length;
  assert.ok(await c.restore());assert.equal(f.runtimes.length,count+1);assert.equal((await f.store.load(scope)).token,newer);
  assert.equal(await f.store.load(scope+'\naccount-logout-queue-v1'),null);
});

test('application delegates metered tasks to the same runtime without an ordinary fallback',async t=>{
 const f=await fixture(t);
 await assert.rejects(f.app.runMeteredFeature('export',3,'job-1',()=>assert.fail('signed out')),{code:'LOGIN_REQUIRED'});
 await f.app.login('alice','password');
 await assert.rejects(f.app.runMeteredFeature('export',3,'job-1',()=>assert.fail('ordinary fallback')),{code:'METERING_UNSUPPORTED'});
 const runtime=f.runtimes[0],calls=[];let completed=false;
 runtime.runMeteredFeature=async(feature,quantity,id,action)=>{calls.push([feature,quantity,id]);if(completed)return;const result=await action();completed=true;return result;};
 runtime.resolveMeteredFeature=async(...args)=>{calls.push(args)};
 assert.equal(await f.app.runMeteredFeature('export',3,'job-1',()=>42),42);
 assert.equal(await f.app.runMeteredFeature('export',3,'job-1',()=>assert.fail('replayed')),undefined);
 await f.app.resolveMeteredFeature('job-1',true);
 assert.deepEqual(calls,[['export',3,'job-1'],['export',3,'job-1'],['job-1',true]]);
 completed=false;const error=new Error('business error');
 await assert.rejects(f.app.runMeteredFeature('export',1,'job-2',()=>{throw error}),value=>value===error);
});
