import test from 'node:test';import assert from 'node:assert/strict';import {generateKeyPairSync,sign} from 'node:crypto';import {verifyLease} from '../src/vendor/licentivo.mjs';
import {Client} from '../src/vendor/licentivo.mjs';
import {mkdtemp,rm} from 'node:fs/promises';
import {join,resolve,sep} from 'node:path';
import {tmpdir} from 'node:os';

test('embedded SDK reports recognizable device details in status and account exchange',async()=>{
 const base=resolve(tmpdir()),directory=await mkdtemp(join(base,'launch-device-details-'));
 if(!resolve(directory).startsWith(base+sep))throw Error('Temporary directory escaped its root');
 try{
  const {publicKey}=generateKeyPairSync('ed25519');
  const client=new Client({product_id:'11111111-1111-4111-8111-111111111111',trusted_keys:{key:publicKey.export({type:'spki',format:'der'}).subarray(-32).toString('base64url')},identity_token:'test-identity',cache_path:join(directory,'lease.json')});
  const info=client.status().device;
  assert.ok(info.name.trim());
  assert.match(info.code,/^[A-F0-9]{4}-[A-F0-9]{4}$/);
  assert.ok(info.platform&&info.system_type);
  assert.equal(info.device_id,undefined);
  const body=client.body();
  assert.equal(body.device_name,info.name);
  assert.equal(body.device_info.system_type,info.system_type);
  assert.equal(body.identity_token,'test-identity');
  assert.equal(body.license_key,'');
 }finally{await rm(directory,{recursive:true,force:true});}
});
test('official SDK verifies signatures and rejects tampering',()=>{const {privateKey,publicKey}=generateKeyPairSync('ed25519');const key=publicKey.export({type:'spki',format:'der'}).subarray(-32).toString('base64url');const now=Math.floor(Date.now()/1000),id='11111111-1111-4111-8111-111111111111',device='a'.repeat(64);const claims={version:1,key_id:'key',tenant_id:id,product_id:id,license_id:id,activation_id:id,device_id:device,mode:'timed',features:['export'],max_devices:1,issued_at:now,expires_at:now+600};const raw=Buffer.from(JSON.stringify(claims)),lease={version:1,key_id:'key',payload:raw.toString('base64url'),signature:sign(null,Buffer.concat([Buffer.from('LicenovaLease/v1\n'),raw]),privateKey).toString('base64url')};assert.deepEqual(verifyLease(lease,{key},id,device).features,['export']);assert.throws(()=>verifyLease({...lease,signature:'A'.repeat(86)},{key},id,device));});
