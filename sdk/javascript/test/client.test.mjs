import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,rm,readFile,writeFile,readdir} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {createServer} from 'node:http';
import {Application,MemoryStore,EncryptedFileStore,LaunchError,validateConfig,defaultTransport} from '../src/index.mjs';

const launch={schema_version:1,merchant_origin:'https://merchant.example',launch_product_id:'11111111-1111-4111-8111-111111111111',licentivo_product_id:'22222222-2222-4222-8222-222222222222',environment:'sandbox'};
const product={product_id:launch.licentivo_product_id,trusted_keys:{test:Buffer.alloc(32,1).toString('base64url')}};
const user={id:'33333333-3333-4333-8333-333333333333',email:'unit@example.test'};
const credential={product_id:launch.launch_product_id,license_key:'lv_lic_'+ 'L'.repeat(43),version:1,valid_until:new Date(Date.now()+3600000).toISOString()};
const grant={user,access_token:'T'.repeat(43),expires_at:new Date(Date.now()+3600000).toISOString()};
const reply=data=>({status:200,headers:{},body:JSON.stringify({data})});
const error=(code,status=403,retry='3')=>({status,headers:{'retry-after':retry},body:JSON.stringify({error:{code}})});
class Store extends MemoryStore {scope;record;async load(scope){this.scope=scope;return super.load(scope);}async save(scope,record){this.scope=scope;this.record=structuredClone(record);return super.save(scope,record);}}
async function setup(t){
  const root=await mkdtemp(join(tmpdir(),'launch-node-'));t.after(()=>rm(root,{recursive:true,force:true}));
  const store=new Store(),calls=[],runtimes=[],waits=[];let failure=null,pending=0,offline=true;
  const transport=async req=>{calls.push(req);if(req.url.endsWith('/auth/logout'))return reply({});if(failure && !(req.url.endsWith('/me') && failure.body?.includes('ENTITLEMENT_PENDING'))){if(failure instanceof Error)throw failure;return failure;}if(req.url.endsWith('/auth/login'))return reply(grant);if(req.url.endsWith('/me'))return reply({user,session_id:'opaque'});if(pending-->0)return error('ENTITLEMENT_PENDING');return reply(credential);};
  const factory=config=>{const runtime={config,started:0,closed:0,released:0,status:()=>({allowed:offline,state:offline?'offline_valid':'verification_required'}),start:async()=>{runtime.started++;},runFeature:async(feature,action)=>{if(feature!=='export')throw new LaunchError('FEATURE_NOT_ALLOWED');return action();},deactivate:async()=>{runtime.released++;},close:async()=>{runtime.closed++;}};runtimes.push(runtime);return runtime;};
  const options={store,transport,runtimeFactory:factory,cacheDir:root,sleep:async ms=>{waits.push(ms);}};
  const app=new Application(launch,product,options);t.after(()=>app.close());
  return {app,store,calls,runtimes,waits,root,options,setFailure:v=>failure=v,setPending:v=>pending=v,setOffline:v=>offline=v};
}
test('login, feature guard, close, restore, release and logout contract',async t=>{
  const f=await setup(t);await f.app.login('a','secret');assert.equal(f.calls[0].headers.Authorization,undefined);
  assert.equal(JSON.parse(f.calls[0].body).kind,'desktop');assert.equal(JSON.parse(f.calls[1].body).environment,'sandbox');
  assert.equal(await f.app.runFeature('export',()=>42),42);let ran=false;await assert.rejects(f.app.runFeature('other',()=>ran=true),{code:'FEATURE_NOT_ALLOWED'});assert.equal(ran,false);
  await assert.rejects(f.app.runFeature('export',()=>f.app.close()),{code:'WORKFLOW_BUSY'});
  await f.app.close();assert.ok(await f.store.load(f.store.scope));
  const next=new Application(launch,product,f.options);assert.equal((await next.restore()).offlineFallback,false);await next.releaseDevice();assert.equal(f.runtimes[1].released,1);assert.equal(await f.store.load(f.store.scope),null);await next.close();
});
for(const [status,code] of [[401,'AUTH_REQUIRED'],[403,'ENTITLEMENT_EXPIRED'],[403,'ENTITLEMENT_SUSPENDED'],[404,'ENTITLEMENT_NOT_PURCHASED']])test(`explicit ${status} ${code} erases cache and denies offline`,async t=>{
  const f=await setup(t);await f.app.login('a','b');await writeFile(f.runtimes[0].config.cache_path,'signed-cache-placeholder');f.setFailure(error(code,status));
  await assert.rejects(f.app.restore(),{code});assert.equal(await f.store.load(f.store.scope),null);assert.equal(f.runtimes.length,1);await assert.rejects(readFile(f.runtimes[0].config.cache_path),{code:'ENOENT'});
});
test('only transport/5xx enters verified SDK offline mode; cancellation does not',async t=>{
  const f=await setup(t);await f.app.login('a','b');f.setFailure(new LaunchError('NETWORK_UNAVAILABLE'));
  assert.equal((await f.app.restore()).offlineFallback,true);f.setOffline(false);await assert.rejects(f.app.restore(),{code:'OFFLINE_UNAVAILABLE'});
  f.setOffline(true);f.setFailure({status:503,headers:{},body:'service temporarily unavailable'});assert.equal((await f.app.restore()).offlineFallback,true);
  f.setFailure(error('RATE_LIMITED',429));await assert.rejects(f.app.restore(),{code:'RATE_LIMITED'});
  const controller=new AbortController();controller.abort();const count=f.runtimes.length;await assert.rejects(f.app.restore({signal:controller.signal}),{code:'CANCELLED'});assert.equal(f.runtimes.length,count);
});
test('pending is bounded and honors Retry-After without offline fallback',async t=>{
  const f=await setup(t);f.setPending(2);await f.app.login('a','b');assert.deepEqual(f.waits,[3000,3000]);
  f.setFailure(error('ENTITLEMENT_PENDING',403,'9'));await assert.rejects(f.app.restore(),{code:'ENTITLEMENT_PENDING'});assert.deepEqual(f.waits.slice(2),[9000,9000,9000,9000]);assert.equal(f.runtimes.length,1);
  f.setFailure(error('ENTITLEMENT_PENDING',403,'31'));const waits=f.waits.length;await assert.rejects(f.app.restore(),{code:'ENTITLEMENT_PENDING'});assert.equal(f.waits.length,waits);
});
test('expired saved sessions and account mismatch are rejected before SDK activation',async t=>{
  const f=await setup(t);await f.app.login('a','b');const record=f.store.record;record.expires_at='2020-01-01T00:00:00Z';await f.store.save(f.store.scope,record);await assert.rejects(f.app.restore(),{code:'SESSION_EXPIRED'});assert.equal(f.runtimes.length,1);
  await f.app.login('a','b');f.setFailure(reply({user:{id:'44444444-4444-4444-8444-444444444444'}}));await assert.rejects(f.app.restore(),{code:'SESSION_MISMATCH'});assert.equal(await f.store.load(f.store.scope),null);
});
test('store scopes isolate environment and merchant; public config rejects secrets',async t=>{
  const f=await setup(t);await f.app.login('a','b');
  for(const changed of [{environment:'live'},{merchant_origin:'https://other.example'}]){const app=new Application({...launch,...changed},product,f.options);assert.equal(await app.restore(),null);await app.close();}
  assert.throws(()=>validateConfig(launch,{...product,management_token:'secret'}),{code:'PUBLIC_CONFIG_INVALID'});
  assert.throws(()=>validateConfig(launch,{...product,license_key:'secret'}),{code:'PUBLIC_CONFIG_INVALID'});
  assert.throws(()=>validateConfig({...launch,merchant_origin:'http://untrusted.example'},product),{code:'INVALID_CONFIG'});
  assert.throws(()=>new Application(launch,product,{store:{persistent:true}}),{code:'SECURE_STORE_REQUIRED'});
});
test('encrypted file store is authenticated, confidential and scoped',async t=>{
  const root=await mkdtemp(join(tmpdir(),'launch-encryption-'));t.after(()=>rm(root,{recursive:true,force:true}));
  const store=new EncryptedFileStore(root,Buffer.alloc(32,7));await store.save('scope',{token:'secret-token'});assert.deepEqual(await store.load('scope'),{token:'secret-token'});
  const file=join(root,(await readdir(root))[0]),raw=await readFile(file);assert.equal(raw.includes(Buffer.from('secret-token')),false);raw[raw.length-1]^=1;await writeFile(file,raw);await assert.rejects(store.load('scope'),{code:'INVALID_SAVED_SESSION'});assert.equal(await store.load('different-scope'),null);
});
test('actual HTTP transport refuses redirects before sending bearer to target',async t=>{
  let leaked=false;const target=createServer((req,res)=>{leaked=true;res.end('{}');});await new Promise(r=>target.listen(0,'127.0.0.1',r));t.after(()=>new Promise(r=>target.close(r)));
  const source=createServer((req,res)=>{res.writeHead(302,{Location:`http://127.0.0.1:${target.address().port}/steal`});res.end();});await new Promise(r=>source.listen(0,'127.0.0.1',r));t.after(()=>new Promise(r=>source.close(r)));
  await assert.rejects(defaultTransport({url:`http://127.0.0.1:${source.address().port}/`,method:'GET',headers:{Authorization:'Bearer secret'}}),{code:'REDIRECT_REJECTED'});assert.equal(leaked,false);
});
