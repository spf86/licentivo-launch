import test from 'node:test';
import assert from 'node:assert/strict';
import {generateKeyPairSync,sign,createHash} from 'node:crypto';
import {mkdtemp,writeFile,rm} from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {pathToFileURL} from 'node:url';
import {Application,MemoryStore,LaunchError} from '../src/index.mjs';

test('real official SDK verifies signed cache, gates feature, rejects tamper',async t=>{
  const source=new URL('../src/vendor/licentivo.mjs',import.meta.url);
  const {Client,machineDeviceID}=await import(source.href);
  const root=await mkdtemp(join(tmpdir(),'launch-node-runtime-'));t.after(()=>rm(root,{recursive:true,force:true}));
  const {publicKey,privateKey}=generateKeyPairSync('ed25519');
  const launch={schema_version:1,merchant_origin:'https://merchant.example',launch_product_id:'11111111-1111-4111-8111-111111111111',licentivo_product_id:'22222222-2222-4222-8222-222222222222',environment:'sandbox'};
  const product={product_id:launch.licentivo_product_id,base_url:'http://127.0.0.1:1',allow_http:true,trusted_keys:{test:publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64url')}};
  const user={id:'33333333-3333-4333-8333-333333333333'},key='lv_lic_'+'L'.repeat(43),now=Math.floor(Date.now()/1000);
  const claims={version:1,key_id:'test',tenant_id:user.id,product_id:product.product_id,license_id:user.id,device_id:machineDeviceID(product.product_id),activation_id:user.id,mode:'perpetual',features:['export'],max_devices:2,issued_at:now-1,expires_at:now+3600,runtime_policy:{heartbeat_seconds:0,offline_mode:'permanent',offline_seconds:0}};
  const raw=Buffer.from(JSON.stringify(claims));
  const lease={version:1,key_id:'test',payload:raw.toString('base64url'),signature:sign(null,Buffer.concat([Buffer.from('LicenovaLease/v1\n'),raw]),privateKey).toString('base64url')};
  const cache={version:1,license_hash:createHash('sha256').update(key).digest('hex'),observed_at:now,lease};
  let first=true,cachePath,down=false;
  const runtimeFactory=async config=>{cachePath=config.cache_path;if(first){first=false;await writeFile(cachePath,JSON.stringify(cache));}return new Client(config);};
  const transport=async request=>{
    if(down)throw new LaunchError('NETWORK_UNAVAILABLE');
    let data={};if(request.url.endsWith('/auth/login'))data={user,access_token:'T'.repeat(43),expires_at:new Date(Date.now()+3600000).toISOString()};
    else if(request.url.endsWith('/me'))data={user};else if(request.url.endsWith('/credential'))data={product_id:launch.launch_product_id,license_key:key,version:1};
    return {status:200,headers:{},body:JSON.stringify({data})};
  };
  const store=new MemoryStore(),options={store,transport,runtimeFactory,cacheDir:root};
  const app=new Application(launch,product,options);await app.login('test','secret');assert.equal(await app.runFeature('export',()=>42),42);await app.close();
  down=true;const restored=new Application(launch,product,options);assert.equal((await restored.restore()).offlineFallback,true);assert.equal(await restored.runFeature('export',()=>43),43);await restored.close();
  cache.lease.signature='A'.repeat(86);await writeFile(cachePath,JSON.stringify(cache));
  const tampered=new Application(launch,product,options);await assert.rejects(tampered.restore(),{code:'OFFLINE_UNAVAILABLE'});let ran=false;await assert.rejects(tampered.runFeature('export',()=>ran=true),{code:'LOGIN_REQUIRED'});assert.equal(ran,false);await tampered.close();
});
